// Package runner explicitly executes effect plans produced by a state machine.
// It provides ordered, at-most-once invocation within one Execute call; it
// does not claim exactly-once delivery.
package runner

import (
	"context"
	"errors"
	"fmt"
	"time"

	statemachine "github.com/faustbrian/go-state-machine/v2"
)

// Outcome classifies one effect attempt.
type Outcome string

const (
	OutcomeSucceeded Outcome = "succeeded"
	OutcomeRetryable Outcome = "retryable"
	OutcomePermanent Outcome = "permanent"
	OutcomeCanceled  Outcome = "canceled"
	OutcomePanicked  Outcome = "panicked"
)

// Handler performs application-owned work for an explicit effect.
type Handler interface {
	Handle(context.Context, statemachine.Effect) error
}

// Recorder persists or observes the result of an effect attempt.
type Recorder interface {
	Record(context.Context, Record) error
}

// Record describes one completed effect attempt.
type Record struct {
	Index      int
	Effect     statemachine.Effect
	Outcome    Outcome
	StartedAt  time.Time
	FinishedAt time.Time
	Err        error
}

// Classifier decides whether a handler failure may be retried safely.
type Classifier func(error) Outcome

// Options defines injectable runner dependencies.
type Options struct {
	Clock    func() time.Time
	Classify Classifier
	Recorder Recorder
	Limits   Limits
}

// Limits bounds one explicit execution plan. All maxima are inclusive.
type Limits struct {
	MaxEffects            int
	MaxEffectPayloadBytes int
	MaxTotalPayloadBytes  int
}

// DefaultLimits bounds direct execution without caller configuration.
func DefaultLimits() Limits {
	return Limits{
		MaxEffects:            3_000,
		MaxEffectPayloadBytes: 1 << 20,
		MaxTotalPayloadBytes:  16 << 20,
	}
}

func (limits Limits) valid() bool {
	return limits.MaxEffects > 0 && limits.MaxEffectPayloadBytes > 0 &&
		limits.MaxTotalPayloadBytes > 0
}

// Runner executes plans serially. It is safe for concurrent independent calls.
type Runner struct {
	handler  Handler
	clock    func() time.Time
	classify Classifier
	recorder Recorder
	limits   Limits
}

// ErrMissingHandler reports an invalid runner construction.
var ErrMissingHandler = errors.New("runner: handler is required")

// ErrInvalidLimits reports incomplete or nonpositive execution limits.
var ErrInvalidLimits = errors.New("runner: invalid limits")

// ErrReentrant reports a nested Execute call on the same runner and context.
var ErrReentrant = errors.New("runner: reentrant execution")

// ErrHandlerPanic reports a contained handler panic. Panic values are omitted
// because they may contain sensitive application data.
var ErrHandlerPanic = errors.New("runner: effect handler panicked")

// EffectError identifies the effect attempt that stopped execution.
type EffectError struct {
	Index   int
	Kind    string
	Outcome Outcome
	Cause   error
}

func (err *EffectError) Error() string {
	return fmt.Sprintf("runner: effect %d failed", err.Index)
}

// Unwrap exposes the handler failure.
func (err *EffectError) Unwrap() error {
	return err.Cause
}

// RecorderError reports that result recording failed after an attempt.
type RecorderError struct {
	Index int
	Cause error
}

func (err *RecorderError) Error() string {
	return fmt.Sprintf("runner: record effect %d failed", err.Index)
}

func (err *RecorderError) Unwrap() error {
	return err.Cause
}

// New validates dependencies and constructs a runner.
func New(handler Handler, options Options) (*Runner, error) {
	if handler == nil {
		return nil, ErrMissingHandler
	}
	limits := options.Limits
	if limits == (Limits{}) {
		limits = DefaultLimits()
	}
	if !limits.valid() {
		return nil, ErrInvalidLimits
	}
	clock := options.Clock
	if clock == nil {
		clock = time.Now
	}
	classifier := options.Classify
	if classifier == nil {
		classifier = func(error) Outcome { return OutcomePermanent }
	}
	return &Runner{
		handler: handler, clock: clock, classify: classifier, recorder: options.Recorder,
		limits: limits,
	}, nil
}

type contextKey struct{}

// Execute invokes effects in slice order and stops after the first failure.
func (runner *Runner) Execute(ctx context.Context, effects []statemachine.Effect) ([]Record, error) {
	if active, _ := ctx.Value(contextKey{}).(*Runner); active == runner {
		return nil, ErrReentrant
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(effects) > runner.limits.MaxEffects {
		return nil, statemachine.ErrLimitExceeded
	}
	totalPayloadBytes := 0
	for _, effect := range effects {
		payloadBytes := len(effect.Payload)
		if payloadBytes > runner.limits.MaxEffectPayloadBytes ||
			payloadBytes > runner.limits.MaxTotalPayloadBytes-totalPayloadBytes {
			return nil, statemachine.ErrLimitExceeded
		}
		totalPayloadBytes += payloadBytes
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx = context.WithValue(ctx, contextKey{}, runner)
	records := make([]Record, 0, len(effects))
	for index, effect := range effects {
		if err := ctx.Err(); err != nil {
			return records, err
		}
		record := Record{Index: index, Effect: cloneEffect(effect), StartedAt: runner.clock()}
		if err := ctx.Err(); err != nil {
			return records, err
		}
		panicked, attempted, handlerErr := runner.handle(ctx, effect)
		if !attempted {
			return records, handlerErr
		}
		record.FinishedAt = runner.clock()
		switch {
		case panicked:
			record.Outcome = OutcomePanicked
			record.Err = ErrHandlerPanic
		case handlerErr == nil:
			record.Outcome = OutcomeSucceeded
		case errors.Is(handlerErr, context.Canceled) || errors.Is(handlerErr, context.DeadlineExceeded):
			record.Outcome = OutcomeCanceled
			record.Err = handlerErr
		default:
			record.Outcome = runner.classify(handlerErr)
			if record.Outcome != OutcomeRetryable && record.Outcome != OutcomePermanent {
				record.Outcome = OutcomePermanent
			}
			record.Err = handlerErr
		}
		records = append(records, record)
		if runner.recorder != nil {
			if err := runner.recorder.Record(ctx, record); err != nil {
				return records, &RecorderError{Index: index, Cause: err}
			}
		}
		if record.Outcome != OutcomeSucceeded {
			return records, &EffectError{
				Index: index, Kind: effect.Kind, Outcome: record.Outcome, Cause: record.Err,
			}
		}
		if err := ctx.Err(); err != nil {
			return records, err
		}
	}
	return records, nil
}

func (runner *Runner) handle(ctx context.Context, effect statemachine.Effect) (panicked bool, attempted bool, handlerErr error) {
	if err := ctx.Err(); err != nil {
		return false, false, err
	}
	defer func() {
		if recover() != nil {
			handlerErr = nil
			panicked = true
		}
	}()
	input := cloneEffect(effect)
	attempted = true
	return false, attempted, runner.handler.Handle(ctx, input)
}

func cloneEffect(effect statemachine.Effect) statemachine.Effect {
	effect.Payload = append([]byte(nil), effect.Payload...)
	return effect
}
