package outbox

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
)

type internalStore struct {
	claims    []Claim
	claimErr  error
	finishErr error
	dead      int
}

func (store *internalStore) Claim(context.Context, ClaimRequest) ([]Claim, error) {
	return store.claims, store.claimErr
}

func (store *internalStore) MarkPublished(context.Context, LeaseRef, time.Time) error {
	return store.finishErr
}

func (store *internalStore) Retry(context.Context, LeaseRef, time.Time, error) error {
	return store.finishErr
}

func (store *internalStore) DeadLetter(context.Context, LeaseRef, time.Time, error) error {
	store.dead++
	return store.finishErr
}

type internalPublisher func(context.Context, Message) error

func (publisher internalPublisher) Publish(ctx context.Context, message Message) error {
	return publisher(ctx, message)
}

func relayOptions(store Store, publisher Publisher) RelayOptions {
	return RelayOptions{
		Store: store, Publisher: publisher, Clock: func() time.Time { return time.Unix(1, 0) },
		Classify:   func(error) FailureClass { return FailurePermanent },
		RetryDelay: func(int) time.Duration { return time.Second },
	}
}

func TestRelayRemainingConstructionAndOperationErrors(t *testing.T) {
	t.Parallel()

	if _, err := NewRelay(RelayOptions{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("invalid options error = %v", err)
	}
	wantErr := errors.New("store failed")
	relay, _ := NewRelay(relayOptions(&internalStore{claimErr: wantErr}, internalPublisher(func(context.Context, Message) error { return nil })))
	_, err := relay.RunOnce(context.Background(), ClaimRequest{})
	var operationErr *OperationError
	if !errors.As(err, &operationErr) || !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "claim") {
		t.Fatalf("claim error = %v", err)
	}

	store := &internalStore{
		claims:    []Claim{{Message: Message{ID: "one"}, Token: "token"}},
		finishErr: wantErr,
	}
	relay, _ = NewRelay(relayOptions(store, internalPublisher(func(context.Context, Message) error { return nil })))
	result, err := relay.RunOnce(context.Background(), ClaimRequest{})
	if !errors.As(err, &operationErr) || operationErr.MessageID != "one" || result.Claimed != 1 {
		t.Fatalf("finish result = %#v, error = %v", result, err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := relay.RunOnce(canceled, ClaimRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled relay error = %v", err)
	}
}

func TestRelayContainsPublisherPanicAndClonesPayload(t *testing.T) {
	t.Parallel()

	payload := []byte("original")
	store := &internalStore{claims: []Claim{{
		Message: Message{ID: "one", Effect: statemachine.Effect{Payload: payload}}, Token: "token",
	}}}
	relay, _ := NewRelay(relayOptions(store, internalPublisher(func(_ context.Context, message Message) error {
		message.Effect.Payload[0] = 'X'
		panic("sensitive value")
	})))
	result, err := relay.RunOnce(context.Background(), ClaimRequest{})
	if err != nil || result.DeadLettered != 1 || store.dead != 1 || string(payload) != "original" {
		t.Fatalf("result = %#v, dead = %d, payload = %q, error = %v", result, store.dead, payload, err)
	}
}

func TestRelayStopsWhenContextCancelsBetweenClaims(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	store := &internalStore{claims: []Claim{
		{Message: Message{ID: "one"}, Token: "one"},
		{Message: Message{ID: "two"}, Token: "two"},
	}}
	relay, _ := NewRelay(relayOptions(store, internalPublisher(func(context.Context, Message) error {
		cancel()
		return nil
	})))
	result, err := relay.RunOnce(ctx, ClaimRequest{})
	if !errors.Is(err, context.Canceled) || result.Published != 1 {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestRelayRedeliversAfterPublishAcknowledgementFailure(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("acknowledgement failed")
	store := &internalStore{
		claims:    []Claim{{Message: Message{ID: "one"}, Token: "token"}},
		finishErr: wantErr,
	}
	published := 0
	relay, _ := NewRelay(relayOptions(store, internalPublisher(func(context.Context, Message) error {
		published++
		return nil
	})))
	if _, err := relay.RunOnce(context.Background(), ClaimRequest{}); !errors.Is(err, wantErr) {
		t.Fatalf("first delivery error = %v", err)
	}
	store.finishErr = nil
	result, err := relay.RunOnce(context.Background(), ClaimRequest{})
	if err != nil || result.Published != 1 || published != 2 {
		t.Fatalf("recovery result = %#v, publishes = %d, error = %v", result, published, err)
	}
}
