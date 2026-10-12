// Package outbox provides explicit at-least-once publication of durable effect
// records. A successful publish followed by an acknowledgement failure can be
// delivered again; callers must provide idempotent consumers or deduplication.
// Start with NewRelay and explicitly call RunOnce. The package owns no polling
// worker, transport, durable store, lease renewal or collaborator shutdown.
package outbox

import (
	"context"
	"errors"
	"time"

	statemachine "github.com/faustbrian/go-state-machine/v2"
)

// Message is one durable planned effect ready for publication.
type Message struct {
	// ID is the stable message identity applications can use for deduplication.
	ID string
	// InstanceID relates publication to its originating machine instance.
	InstanceID statemachine.InstanceID
	// Sequence identifies the committed transition, not global publication order.
	Sequence uint64
	// Index identifies the effect within its committed plan.
	Index int
	// Effect contains opaque publish data; publishers receive a payload copy.
	Effect statemachine.Effect
	// OccurredAt is the originating transition timestamp, not publication time.
	OccurredAt time.Time
	// Attempts is the store-reported claim count, including the current attempt.
	Attempts int
}

// Claim is a message protected by a time-bounded lease.
type Claim struct {
	// Message is the durable effect selected by the store.
	Message Message
	// Token fences claim generations; treat it as an application-owned lease capability.
	Token string
	// LeasedUntil controls recovery according to the store, not relay renewal.
	LeasedUntil time.Time
}

// LeaseRef identifies a claimed message without carrying its payload.
type LeaseRef struct {
	// ID selects the durable message being completed or rescheduled.
	ID string
	// Token must still identify the current unfinished claim generation.
	Token string
}

// ClaimRequest controls one bounded claim operation.
type ClaimRequest struct {
	// Owner is a nonempty caller-selected worker identity.
	Owner string
	// Limit bounds the requested batch; provider maxima may be stricter.
	Limit int
	// LeaseDuration is positive; callers fit publication latency within their recovery policy.
	LeaseDuration time.Duration
}

// Store owns durable leases and publication outcomes.
type Store interface {
	// Claim atomically leases eligible records and returns them in provider order.
	// Providers define maxima, expiry eligibility and token-generation requirements.
	Claim(context.Context, ClaimRequest) ([]Claim, error)
	// MarkPublished acknowledges a still-recognized claim after publication.
	// Failure after publication may result in duplicate delivery.
	MarkPublished(context.Context, LeaseRef, time.Time) error
	// Retry releases the recognized claim for a future at-least-once attempt.
	Retry(context.Context, LeaseRef, time.Time, error) error
	// DeadLetter removes the recognized claim from ordinary eligibility.
	DeadLetter(context.Context, LeaseRef, time.Time, error) error
}

// Publisher sends one message to an application-selected transport.
type Publisher interface {
	// Publish sends a copied message under ctx. Consumers need stable-ID deduplication
	// or idempotency; successful return precedes durable acknowledgement.
	Publish(context.Context, Message) error
}

// FailureClass controls retry versus dead-letter behavior.
type FailureClass string

const (
	// FailureRetryable selects durable rescheduling after publication failure.
	FailureRetryable FailureClass = "retryable"
	// FailurePermanent selects dead-lettering after publication failure.
	FailurePermanent FailureClass = "permanent"
)

// RelayOptions defines all nondeterministic relay dependencies explicitly.
type RelayOptions struct {
	// Store is a borrowed durable claim/outcome owner; the relay does not close it.
	Store Store
	// Publisher is borrowed and called serially within one pass.
	Publisher Publisher
	// Clock supplies completion and retry scheduling time; it is required, not defaulted.
	Clock func() time.Time
	// Classify selects retryable failure; every other value is treated as permanent.
	Classify func(error) FailureClass
	// RetryDelay supplies delay for the store-reported attempt count; no delay is slept here.
	RetryDelay func(attempt int) time.Duration
}

// Relay publishes serially within each pass, not in a global or effect-index
// order. Concurrent passes require concurrency-safe borrowed collaborators.
// Only publisher panics are contained; policy/store/clock panics propagate.
type Relay struct {
	store      Store
	publisher  Publisher
	clock      func() time.Time
	classify   func(error) FailureClass
	retryDelay func(int) time.Duration
}

// Result summarizes one bounded relay pass.
type Result struct {
	// Claimed counts the entire claimed batch, including unprocessed records on failure.
	Claimed int
	// Published counts successful durable acknowledgements, not just transport sends.
	Published int
	// Retried counts successful durable reschedules in this pass.
	Retried int
	// DeadLettered counts successful durable dead-letter outcomes in this pass.
	DeadLettered int
}

var (
	// ErrInvalidOptions reports missing relay dependencies.
	ErrInvalidOptions = errors.New("outbox: invalid relay options")
	// ErrPublisherPanic reports a contained publisher panic.
	ErrPublisherPanic = errors.New("outbox: publisher panicked")
	// ErrInvalidClaim reports an invalid owner, limit, lease, or reference.
	ErrInvalidClaim = errors.New("outbox: invalid claim")
	// ErrLeaseLost reports a claim no longer recognized by the provider. Expiry
	// enforcement differs by store; PostgreSQL fences replacement/completion tokens.
	ErrLeaseLost = errors.New("outbox: lease lost")
)

// OperationError identifies the durable operation that stopped a relay pass.
type OperationError struct {
	// Operation is claim or record outcome; Error does not render it.
	Operation string
	// MessageID identifies outcome-record failure; it can contain application data.
	MessageID string
	// Cause is the raw store error, exposed through Unwrap for trusted inspection.
	Cause error
}

// Error returns fixed failure text without Operation, MessageID or Cause.
func (err *OperationError) Error() string {
	return "outbox: operation failed"
}

// Unwrap exposes raw causes to errors.Is/errors.As; causes may be sensitive.
func (err *OperationError) Unwrap() error {
	return err.Cause
}

// NewRelay requires every collaborator and supplies no defaults. It rejects
// literal nil interfaces, not typed nil values. Collaborators remain caller-owned.
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
// each outcome. It never retries within the same pass or renews a lease. Returned
// counters include only recorded outcomes, while Claimed includes the whole batch.
// Cancellation or recording failure leaves remaining claims for store recovery;
// already published work cannot be rolled back. Contexts must be non-nil.
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
		// Transport success precedes durable acknowledgement; a crash or record
		// failure here can replay the same message after lease recovery.
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
