package runner

import (
	"context"
	"errors"
	"testing"

	statemachine "github.com/faustbrian/go-state-machine/v2"
)

// This unit boundary proves handler admission, not cancellation timing between
// Execute's preceding checkpoint and this call.
func TestHandleRequiresActiveContextBeforeAttempt(t *testing.T) {
	want := errors.New("handler failed")
	var called int
	executor, err := New(internalHandler(func(context.Context, statemachine.Effect) error {
		called++
		return want
	}), Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, panicked, attempted := executor.handle(ctx, statemachine.Effect{Kind: "unattempted"})
	if !errors.Is(got, context.Canceled) || panicked || attempted || called != 0 {
		t.Fatalf("canceled admission: err=%v panicked=%v attempted=%v calls=%d", got, panicked, attempted, called)
	}
	got, panicked, attempted = executor.handle(context.Background(), statemachine.Effect{Kind: "attempted"})
	if !errors.Is(got, want) || panicked || !attempted || called != 1 {
		t.Fatalf("active admission: err=%v panicked=%v attempted=%v calls=%d", got, panicked, attempted, called)
	}
}
