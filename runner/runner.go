// Package runner explicitly executes effect plans produced by a state machine.
// It provides ordered, at-most-once invocation within one Execute call; it
// does not claim exactly-once delivery. Start with New; no worker, persistence,
// scheduler or automatic retry is owned. Collaborators are borrowed and must
// cooperate with cancellation and concurrent independent use.
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
	// OutcomeSucceeded reports a handler returning nil, not durable delivery proof.
	OutcomeSucceeded Outcome = "succeeded"
	// OutcomeRetryable reports an operational failure eligible for caller-owned retry.
	OutcomeRetryable Outcome = "retryable"
	// OutcomePermanent reports an operational failure not selected for retry.
	OutcomePermanent Outcome = "permanent"
	// OutcomeCanceled reports a handler returning cancellation/deadline error.
	OutcomeCanceled Outcome = "canceled"
	// OutcomePanicked reports a contained handler panic without retaining its value.
	OutcomePanicked Outcome = "panicked"
)

// Handler performs application-owned work for an explicit effect.
type Handler interface {
	// Handle performs one effect attempt using a private payload copy.
	// Respect ctx cooperatively; a returned error cannot undo completed external work.
	Handle(context.Context, statemachine.Effect) error
}

// Recorder persists or observes the result of an effect attempt.
type Recorder interface {
	// Record receives every completed attempt before execution proceeds.
	// Failure stops the plan after the attempt; retain/copy payloads with care.
	Record(context.Context, Record) error
}

// Record describes one completed effect attempt.
type Record struct {
	// Index is the zero-based plan position actually attempted.
	Index int
	// Effect is a copied plan value; its payload may be shared with a Recorder.
	Effect statemachine.Effect
	// Outcome classifies the attempt, not durable external deduplication.
	Outcome Outcome
	// StartedAt is the injected clock value before handler entry.
	StartedAt time.Time
	// FinishedAt is the injected clock value after the handler returns or panics.
	FinishedAt time.Time
	// Err is the raw handler error or contained-panic sentinel; it may be sensitive.
	Err error
}

// Classifier decides whether a handler failure may be retried safely.
type Classifier func(error) Outcome

// Options defines injectable runner dependencies.
type Options struct {
	// Clock defaults to time.Now; it is borrowed and panics are not contained.
	Clock func() time.Time
	// Classify defaults to permanent failure and is called only for non-cancellation errors.
	Classify Classifier
	// Recorder is optional; nil disables recording, not returned attempt records.
	Recorder Recorder
	// Limits is copied; all-zero selects defaults and partial values are invalid.
	Limits Limits
}

// Limits bounds one explicit execution plan. All maxima are inclusive.
type Limits struct {
	// MaxEffects bounds plan occurrences before any callback or proportional copy.
	MaxEffects int
	// MaxEffectPayloadBytes bounds each supplied effect payload.
	MaxEffectPayloadBytes int
	// MaxTotalPayloadBytes counts all payload occurrences, including aliased input buffers.
	MaxTotalPayloadBytes int
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

// Runner executes each plan serially; independent calls can overlap when the
// handler, clock, classifier and recorder are concurrency-safe. Use New and a
// non-nil context. Only handler panics are contained; other seam panics propagate.
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
	// Index is the zero-based failed attempt included in returned records.
	Index int
	// Kind preserves the supplied effect label; field access is not redacted.
	Kind string
	// Outcome reports canceled, panicked, retryable, or permanent failure.
	Outcome Outcome
	// Cause exposes the raw failure for trusted inspection and unwrapping.
	Cause error
}

// Error includes the failed effect index, not Kind, Outcome or cause values.
func (err *EffectError) Error() string {
	return fmt.Sprintf("runner: effect %d failed", err.Index)
}

// Unwrap exposes the handler failure.
func (err *EffectError) Unwrap() error {
	return err.Cause
}

// RecorderError reports that result recording failed after an attempt.
type RecorderError struct {
	// Index identifies the already attempted effect whose recording failed.
	Index int
	// Cause is the raw recording error; prior external work is not rolled back.
	Cause error
}

// Error includes the attempted effect index without the recording cause.
func (err *RecorderError) Error() string {
	return fmt.Sprintf("runner: record effect %d failed", err.Index)
}

// Unwrap exposes raw causes to errors.Is/errors.As; causes may be sensitive.
func (err *RecorderError) Unwrap() error {
	return err.Cause
}

// New rejects a literal nil handler and invalid partial limits; typed-nil
// interfaces are not detected. Clock and classifier default when nil. Borrowed
// collaborators are not closed, cloned or given background execution.
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

// Execute preflights complete count/payload budgets before any callback, invokes
// effects in order, and stops after failure or cancellation. Keep the input plan
// and payloads stable until return; copying happens per attempt, not at entry.
// It returns records for completed attempts, including the failed attempt;
// unattempted work is not
// recorded. Handler and record payloads are separate copies, but recorder and
// returned record share the record buffer. RecorderError can follow successful
// external work; no automatic retry, rollback or durable deduplication occurs.
// Same-runner nesting is rejected only when the propagated context retains its
// runner marker. A fresh context bypasses that detection.
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
		// Record after the external attempt, before advancing: recorder failure
		// must not be mistaken for proof that the effect did not happen.
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
