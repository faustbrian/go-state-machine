package statemachinetest

import (
	"context"
	"errors"
	"testing"
	"time"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
	"github.com/faustbrian/golib/pkg/state-machine/runner"
)

// EffectExecutor is the portable execution surface verified by RunnerContract.
type EffectExecutor interface {
	Execute(context.Context, []statemachine.Effect) ([]runner.Record, error)
}

// RunnerFactory constructs an executor around an application handler.
type RunnerFactory func(runner.Handler, runner.Options) (EffectExecutor, error)

type conformanceHandler func(context.Context, statemachine.Effect) error

func (handler conformanceHandler) Handle(ctx context.Context, effect statemachine.Effect) error {
	return handler(ctx, effect)
}

// RunnerContract verifies ordering, cancellation, and panic containment.
func RunnerContract(t *testing.T, factory RunnerFactory) {
	t.Helper()

	t.Run("ordered_execution", func(t *testing.T) {
		var kinds []string
		executor, err := factory(conformanceHandler(func(_ context.Context, effect statemachine.Effect) error {
			kinds = append(kinds, effect.Kind)
			return nil
		}), runner.Options{Clock: func() time.Time { return time.Unix(1, 0) }})
		if err != nil {
			t.Fatalf("construct executor: %v", err)
		}
		records, err := executor.Execute(context.Background(), []statemachine.Effect{{Kind: "first"}, {Kind: "second"}})
		if err != nil || len(records) != 2 || len(kinds) != 2 || kinds[0] != "first" || kinds[1] != "second" {
			t.Fatalf("kinds = %v, records = %#v, error = %v", kinds, records, err)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		called := false
		executor, err := factory(conformanceHandler(func(context.Context, statemachine.Effect) error {
			called = true
			return nil
		}), runner.Options{})
		if err != nil {
			t.Fatalf("construct executor: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = executor.Execute(ctx, []statemachine.Effect{{Kind: "canceled"}})
		if !errors.Is(err, context.Canceled) || called {
			t.Fatalf("error = %v, called = %t", err, called)
		}
	})

	t.Run("panic_containment", func(t *testing.T) {
		executor, err := factory(conformanceHandler(func(context.Context, statemachine.Effect) error {
			panic("sensitive value")
		}), runner.Options{})
		if err != nil {
			t.Fatalf("construct executor: %v", err)
		}
		_, err = executor.Execute(context.Background(), []statemachine.Effect{{Kind: "panic"}})
		if !errors.Is(err, runner.ErrHandlerPanic) {
			t.Fatalf("error = %v, want ErrHandlerPanic", err)
		}
	})
}
