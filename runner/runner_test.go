package runner_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
	"github.com/faustbrian/golib/pkg/state-machine/runner"
	"github.com/faustbrian/golib/pkg/state-machine/statemachinetest"
)

type handlerFunc func(context.Context, statemachine.Effect) error

func (function handlerFunc) Handle(ctx context.Context, effect statemachine.Effect) error {
	return function(ctx, effect)
}

func TestExecuteClassifiesFailureAndStops(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("temporarily unavailable")
	called := 0
	executor, err := runner.New(
		handlerFunc(func(context.Context, statemachine.Effect) error {
			called++
			return wantErr
		}),
		runner.Options{Classify: func(err error) runner.Outcome {
			if errors.Is(err, wantErr) {
				return runner.OutcomeRetryable
			}
			return runner.OutcomePermanent
		}},
	)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	records, err := executor.Execute(context.Background(), []statemachine.Effect{
		{Kind: "first"}, {Kind: "must-not-run"},
	})
	var effectErr *runner.EffectError
	if !errors.As(err, &effectErr) || effectErr.Outcome != runner.OutcomeRetryable {
		t.Fatalf("error = %#v, want retryable EffectError", err)
	}
	if called != 1 || len(records) != 1 || records[0].Outcome != runner.OutcomeRetryable {
		t.Fatalf("called = %d, records = %#v", called, records)
	}
}

func TestExecuteRejectsReentrantExecution(t *testing.T) {
	t.Parallel()

	var executor *runner.Runner
	nested := false
	executor, _ = runner.New(
		handlerFunc(func(ctx context.Context, _ statemachine.Effect) error {
			if nested {
				return nil
			}
			nested = true
			_, err := executor.Execute(ctx, []statemachine.Effect{{Kind: "nested"}})
			return err
		}),
		runner.Options{},
	)

	_, err := executor.Execute(context.Background(), []statemachine.Effect{{Kind: "outer"}})
	if !errors.Is(err, runner.ErrReentrant) {
		t.Fatalf("error = %v, want ErrReentrant", err)
	}
}

func TestExecuteContainsHandlerPanic(t *testing.T) {
	t.Parallel()

	executor, _ := runner.New(
		handlerFunc(func(context.Context, statemachine.Effect) error {
			panic("sensitive payload")
		}),
		runner.Options{},
	)

	_, err := executor.Execute(context.Background(), []statemachine.Effect{{Kind: "panic"}})
	if !errors.Is(err, runner.ErrHandlerPanic) {
		t.Fatalf("error = %v, want ErrHandlerPanic", err)
	}
	if err.Error() != "runner: effect 0 (panic) ended panicked: runner: effect handler panicked" {
		t.Fatalf("error disclosed panic value: %q", err)
	}
}

func TestRunnerSupportsConcurrentIndependentExecutions(t *testing.T) {
	t.Parallel()

	var handled atomic.Int32
	executor, err := runner.New(handlerFunc(func(context.Context, statemachine.Effect) error {
		handled.Add(1)
		return nil
	}), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for range 100 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := executor.Execute(context.Background(), []statemachine.Effect{{Kind: "one"}}); err != nil {
				t.Errorf("execute: %v", err)
			}
		}()
	}
	wait.Wait()
	if handled.Load() != 100 {
		t.Fatalf("handled = %d, want 100", handled.Load())
	}
}

type recorderFunc func(context.Context, runner.Record) error

func TestRunnerContract(t *testing.T) {
	statemachinetest.RunnerContract(t, func(handler runner.Handler, options runner.Options) (statemachinetest.EffectExecutor, error) {
		return runner.New(handler, options)
	})
}

func (function recorderFunc) Record(ctx context.Context, record runner.Record) error {
	return function(ctx, record)
}

func TestExecuteRunsAndRecordsEffectsInOrder(t *testing.T) {
	t.Parallel()

	var handled []string
	var recorded []runner.Record
	executor, err := runner.New(
		handlerFunc(func(_ context.Context, effect statemachine.Effect) error {
			handled = append(handled, effect.Kind)
			return nil
		}),
		runner.Options{
			Clock: func() time.Time { return time.Unix(123, 0).UTC() },
			Recorder: recorderFunc(func(_ context.Context, record runner.Record) error {
				recorded = append(recorded, record)
				return nil
			}),
		},
	)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	records, err := executor.Execute(context.Background(), []statemachine.Effect{
		{Kind: "first"}, {Kind: "second"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(handled) != 2 || handled[0] != "first" || handled[1] != "second" {
		t.Fatalf("handled = %v", handled)
	}
	if len(records) != 2 || len(recorded) != 2 || records[1].Index != 1 {
		t.Fatalf("records = %#v, recorded = %#v", records, recorded)
	}
	if records[0].Outcome != runner.OutcomeSucceeded || !records[0].FinishedAt.Equal(time.Unix(123, 0)) {
		t.Fatalf("first record = %#v", records[0])
	}
}
