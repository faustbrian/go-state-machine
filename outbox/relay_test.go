package outbox_test

import (
	"context"
	"errors"
	"testing"
	"time"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
	"github.com/faustbrian/golib/pkg/state-machine/outbox"
)

type fakeStore struct {
	claims       []outbox.Claim
	published    []outbox.LeaseRef
	retried      []outbox.LeaseRef
	deadLettered []outbox.LeaseRef
}

func (store *fakeStore) Claim(context.Context, outbox.ClaimRequest) ([]outbox.Claim, error) {
	return append([]outbox.Claim(nil), store.claims...), nil
}

func (store *fakeStore) MarkPublished(_ context.Context, ref outbox.LeaseRef, _ time.Time) error {
	store.published = append(store.published, ref)
	return nil
}

func (store *fakeStore) Retry(_ context.Context, ref outbox.LeaseRef, _ time.Time, _ error) error {
	store.retried = append(store.retried, ref)
	return nil
}

func (store *fakeStore) DeadLetter(_ context.Context, ref outbox.LeaseRef, _ time.Time, _ error) error {
	store.deadLettered = append(store.deadLettered, ref)
	return nil
}

type publisherFunc func(context.Context, outbox.Message) error

func (function publisherFunc) Publish(ctx context.Context, message outbox.Message) error {
	return function(ctx, message)
}

func TestRelayRecordsPublicationRetryAndDeadLetterOutcomes(t *testing.T) {
	t.Parallel()

	retryable := errors.New("broker unavailable")
	permanent := errors.New("invalid destination")
	store := &fakeStore{claims: []outbox.Claim{
		{Message: outbox.Message{ID: "1", Effect: statemachine.Effect{Kind: "publish"}}, Token: "lease-1"},
		{Message: outbox.Message{ID: "2", Effect: statemachine.Effect{Kind: "retry"}}, Token: "lease-2"},
		{Message: outbox.Message{ID: "3", Effect: statemachine.Effect{Kind: "reject"}}, Token: "lease-3"},
	}}
	relay, err := outbox.NewRelay(outbox.RelayOptions{
		Store: store,
		Publisher: publisherFunc(func(_ context.Context, message outbox.Message) error {
			switch message.Effect.Kind {
			case "retry":
				return retryable
			case "reject":
				return permanent
			default:
				return nil
			}
		}),
		Clock: func() time.Time { return time.Unix(100, 0).UTC() },
		Classify: func(err error) outbox.FailureClass {
			if errors.Is(err, retryable) {
				return outbox.FailureRetryable
			}
			return outbox.FailurePermanent
		},
		RetryDelay: func(int) time.Duration { return time.Minute },
	})
	if err != nil {
		t.Fatalf("new relay: %v", err)
	}

	result, err := relay.RunOnce(context.Background(), outbox.ClaimRequest{
		Owner: "worker-1", Limit: 10, LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if result.Claimed != 3 || result.Published != 1 || result.Retried != 1 || result.DeadLettered != 1 {
		t.Fatalf("result = %#v", result)
	}
	if len(store.published) != 1 || len(store.retried) != 1 || len(store.deadLettered) != 1 {
		t.Fatalf("store outcomes = %#v, %#v, %#v", store.published, store.retried, store.deadLettered)
	}
}
