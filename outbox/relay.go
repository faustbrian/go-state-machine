// Package outbox provides explicit at-least-once publication of durable effect
// records. A successful publish followed by an acknowledgement failure can be
// delivered again; callers must provide idempotent consumers or deduplication.
package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
)

// Message is one durable planned effect ready for publication.
type Message struct {
	ID         string
	InstanceID statemachine.InstanceID
	Sequence   uint64
	Index      int
	Effect     statemachine.Effect
	OccurredAt time.Time
	Attempts   int
}

// Claim is a message protected by a time-bounded lease.
type Claim struct {
	Message     Message
	Token       string
	LeasedUntil time.Time
}

// LeaseRef identifies a claimed message without carrying its payload.
type LeaseRef struct {
	ID    string
	Token string
}

// ClaimRequest controls one bounded claim operation.
type ClaimRequest struct {
	Owner         string
	Limit         int
	LeaseDuration time.Duration
}

// Store owns durable leases and publication outcomes.
type Store interface {
	Claim(context.Context, ClaimRequest) ([]Claim, error)
	MarkPublished(context.Context, LeaseRef, time.Time) error
	Retry(context.Context, LeaseRef, time.Time, error) error
	DeadLetter(context.Context, LeaseRef, time.Time, error) error
}

// Publisher sends one message to an application-selected transport.
type Publisher interface {
	Publish(context.Context, Message) error
}

// FailureClass controls retry versus dead-letter behavior.
type FailureClass string

const (
	FailureRetryable FailureClass = "retryable"
	FailurePermanent FailureClass = "permanent"
)

// RelayOptions defines all nondeterministic relay dependencies explicitly.
type RelayOptions struct {
	Store      Store
	Publisher  Publisher
	Clock      func() time.Time
	Classify   func(error) FailureClass
	RetryDelay func(attempt int) time.Duration
}

// Relay publishes claimed messages serially in store order.
type Relay struct {
	store      Store
	publisher  Publisher
	clock      func() time.Time
	classify   func(error) FailureClass
	retryDelay func(int) time.Duration
}

// Result summarizes one bounded relay pass.
type Result struct {
	Claimed      int
	Published    int
	Retried      int
	DeadLettered int
}

var (
	// ErrInvalidOptions reports missing relay dependencies.
	ErrInvalidOptions = errors.New("outbox: invalid relay options")
	// ErrPublisherPanic reports a contained publisher panic.
	ErrPublisherPanic = errors.New("outbox: publisher panicked")
	// ErrInvalidClaim reports an invalid owner, limit, lease, or reference.
	ErrInvalidClaim = errors.New("outbox: invalid claim")
	// ErrLeaseLost reports an expired, replaced, or completed lease.
	ErrLeaseLost = errors.New("outbox: lease lost")
)

// OperationError identifies the durable operation that stopped a relay pass.
type OperationError struct {
	Operation string
	MessageID string
	Cause     error
}

func (err *OperationError) Error() string {
	return fmt.Sprintf("outbox: %s message %s: %v", err.Operation, err.MessageID, err.Cause)
}

func (err *OperationError) Unwrap() error {
	return err.Cause
}

// NewRelay validates dependencies and constructs a relay.
func NewRelay(options RelayOptions) (*Relay, error) {
	if options.Store == nil || options.Publisher == nil || options.Clock == nil ||
		options.Classify == nil || options.RetryDelay == nil {
		return nil, ErrInvalidOptions
	}
	return &Relay{
		store: options.Store, publisher: options.Publisher, clock: options.Clock,
		classify: options.Classify, retryDelay: options.RetryDelay,
	}, nil
}

// RunOnce claims a bounded batch, publishes it serially, and durably records
// each outcome. It never retries within the same pass.
func (relay *Relay) RunOnce(ctx context.Context, request ClaimRequest) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	claims, err := relay.store.Claim(ctx, request)
	if err != nil {
		return Result{}, &OperationError{Operation: "claim", Cause: err}
	}
	result := Result{Claimed: len(claims)}
	for _, claim := range claims {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		publishErr := relay.publish(ctx, claim.Message)
		ref := LeaseRef{ID: claim.Message.ID, Token: claim.Token}
		now := relay.clock()
		switch {
		case publishErr == nil:
			err = relay.store.MarkPublished(ctx, ref, now)
			if err == nil {
				result.Published++
			}
		case relay.classify(publishErr) == FailureRetryable:
			availableAt := now.Add(relay.retryDelay(claim.Message.Attempts))
			err = relay.store.Retry(ctx, ref, availableAt, publishErr)
			if err == nil {
				result.Retried++
			}
		default:
			err = relay.store.DeadLetter(ctx, ref, now, publishErr)
			if err == nil {
				result.DeadLettered++
			}
		}
		if err != nil {
			return result, &OperationError{Operation: "record outcome", MessageID: claim.Message.ID, Cause: err}
		}
	}
	return result, nil
}

func (relay *Relay) publish(ctx context.Context, message Message) (publishErr error) {
	defer func() {
		if recover() != nil {
			publishErr = ErrPublisherPanic
		}
	}()
	return relay.publisher.Publish(ctx, cloneMessage(message))
}

func cloneMessage(message Message) Message {
	message.Effect.Payload = append([]byte(nil), message.Effect.Payload...)
	return message
}
