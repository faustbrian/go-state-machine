package runner_test

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	statemachine "github.com/faustbrian/go-state-machine/v2"
	"github.com/faustbrian/go-state-machine/v2/runner"
)

func TestExecuteRejectsOverLimitCountBeforeCallbacks(t *testing.T) {
	const defaultMaxEffects = 3_000

	var handled, recorded, clocked int
	executor, err := runner.New(handlerFunc(func(context.Context, statemachine.Effect) error {
		handled++
		return nil
	}), runner.Options{
		Clock: func() time.Time {
			clocked++
			return time.Unix(0, 0)
		},
		Recorder: recorderFunc(func(context.Context, runner.Record) error {
			recorded++
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	records, err := executor.Execute(context.Background(), make([]statemachine.Effect, defaultMaxEffects+1))
	if !errors.Is(err, statemachine.ErrLimitExceeded) || records != nil {
		t.Fatalf("records = %d, error = %v; want nil and limit error", len(records), err)
	}
	if handled != 0 || recorded != 0 || clocked != 0 {
		t.Fatalf("callbacks before rejection: handler=%d recorder=%d clock=%d", handled, recorded, clocked)
	}
}

func TestExecuteAcceptsDefaultInclusiveLimits(t *testing.T) {
	const mib = 1 << 20
	var handled int
	executor, err := runner.New(handlerFunc(func(context.Context, statemachine.Effect) error {
		handled++
		return nil
	}), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{2_999, 3_000} {
		records, err := executor.Execute(context.Background(), make([]statemachine.Effect, count))
		if err != nil || len(records) != count {
			t.Fatalf("count %d: records=%d error=%v", count, len(records), err)
		}
	}
	if handled != 5_999 {
		t.Fatalf("handled=%d, want 5999", handled)
	}
	for _, payloadBytes := range []int{mib - 1, mib} {
		records, err := executor.Execute(context.Background(), []statemachine.Effect{{Payload: make([]byte, payloadBytes)}})
		if err != nil || len(records) != 1 || len(records[0].Effect.Payload) != payloadBytes {
			t.Fatalf("payload %d: records=%d error=%v", payloadBytes, len(records), err)
		}
	}
	for _, lastBytes := range []int{mib - 1, mib} {
		effects := make([]statemachine.Effect, 16)
		for index := range 15 {
			effects[index].Payload = make([]byte, mib)
		}
		effects[15].Payload = make([]byte, lastBytes)
		records, err := executor.Execute(context.Background(), effects)
		if err != nil || len(records) != len(effects) {
			t.Fatalf("aggregate %d: records=%d error=%v", 15*mib+lastBytes, len(records), err)
		}
	}
}

func TestExecuteEnforcesCustomPayloadAndAggregateLimits(t *testing.T) {
	limits := runner.Limits{MaxEffects: 3, MaxEffectPayloadBytes: 10, MaxTotalPayloadBytes: 20}
	var handled, recorded, clocked int
	executor, err := runner.New(handlerFunc(func(_ context.Context, effect statemachine.Effect) error {
		handled++
		if len(effect.Payload) != 10 {
			t.Fatalf("handled payload = %d bytes, want 10", len(effect.Payload))
		}
		return nil
	}), runner.Options{
		Limits: limits,
		Clock: func() time.Time {
			clocked++
			return time.Unix(0, 0)
		},
		Recorder: recorderFunc(func(context.Context, runner.Record) error {
			recorded++
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	valid := []statemachine.Effect{{Payload: make([]byte, 10)}, {Payload: make([]byte, 10)}}
	records, err := executor.Execute(context.Background(), valid)
	if err != nil || len(records) != 2 || handled != 2 || recorded != 2 || clocked != 4 {
		t.Fatalf("at aggregate limit: records=%d error=%v handler=%d recorder=%d clock=%d", len(records), err, handled, recorded, clocked)
	}
	for _, effects := range [][]statemachine.Effect{
		{{Payload: make([]byte, 11)}},
		{{Payload: make([]byte, 10)}, {Payload: make([]byte, 10)}, {Payload: []byte{1}}},
	} {
		records, err = executor.Execute(context.Background(), effects)
		if !errors.Is(err, statemachine.ErrLimitExceeded) || records != nil || handled != 2 || recorded != 2 || clocked != 4 {
			t.Fatalf("over custom limit: records=%d error=%v handler=%d recorder=%d clock=%d", len(records), err, handled, recorded, clocked)
		}
	}
	records, err = executor.Execute(context.Background(), valid)
	if err != nil || len(records) != 2 || handled != 4 {
		t.Fatalf("runner after rejection: records=%d error=%v handled=%d", len(records), err, handled)
	}

	large := []statemachine.Effect{{Payload: make([]byte, 1<<20+1)}}
	defaultRunner, err := runner.New(handlerFunc(func(context.Context, statemachine.Effect) error { return nil }), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := defaultRunner.Execute(context.Background(), large); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("default payload limit error = %v", err)
	}
	customRunner, err := runner.New(handlerFunc(func(_ context.Context, effect statemachine.Effect) error {
		if len(effect.Payload) != len(large[0].Payload) {
			t.Fatalf("custom payload = %d bytes", len(effect.Payload))
		}
		return nil
	}), runner.Options{Limits: runner.Limits{MaxEffects: 1, MaxEffectPayloadBytes: len(large[0].Payload), MaxTotalPayloadBytes: len(large[0].Payload)}})
	if err != nil {
		t.Fatal(err)
	}
	if records, err := customRunner.Execute(context.Background(), large); err != nil || len(records) != 1 {
		t.Fatalf("custom payload: records=%d error=%v", len(records), err)
	}
}

func TestNewRejectsIncompleteLimits(t *testing.T) {
	handler := handlerFunc(func(context.Context, statemachine.Effect) error { return nil })
	for _, limits := range []runner.Limits{
		{MaxEffects: 3},
		{MaxEffects: 3, MaxEffectPayloadBytes: 10},
		{MaxEffects: -1, MaxEffectPayloadBytes: 10, MaxTotalPayloadBytes: 20},
		{MaxEffects: 0, MaxEffectPayloadBytes: 10, MaxTotalPayloadBytes: 20},
		{MaxEffects: 3, MaxEffectPayloadBytes: 0, MaxTotalPayloadBytes: 20},
	} {
		if executor, err := runner.New(handler, runner.Options{Limits: limits}); executor != nil || !errors.Is(err, runner.ErrInvalidLimits) {
			t.Fatalf("limits = %#v, runner = %#v, error = %v, want nil runner and ErrInvalidLimits", limits, executor, err)
		}
	}
}

func TestNewAcceptsInclusiveMinimumLimits(t *testing.T) {
	handler := handlerFunc(func(context.Context, statemachine.Effect) error { return nil })
	executor, err := runner.New(handler, runner.Options{Limits: runner.Limits{
		MaxEffects: 1, MaxEffectPayloadBytes: 1, MaxTotalPayloadBytes: 1,
	}})
	if err != nil || executor == nil {
		t.Fatalf("minimum limits: runner = %#v, error = %v", executor, err)
	}
	records, err := executor.Execute(context.Background(), []statemachine.Effect{
		{Kind: "one", Payload: []byte("x")},
	})
	if err != nil || len(records) != 1 || records[0].Outcome != runner.OutcomeSucceeded {
		t.Fatalf("minimum execution: records = %#v, error = %v", records, err)
	}
}

func TestExecutePreCanceledEmptyInputDoesNotCallCollaborators(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := 0
	executor, err := runner.New(handlerFunc(func(context.Context, statemachine.Effect) error {
		called++
		return nil
	}), runner.Options{Clock: func() time.Time {
		called++
		return time.Time{}
	}, Recorder: recorderFunc(func(context.Context, runner.Record) error {
		called++
		return nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	for _, effects := range [][]statemachine.Effect{nil, {}, make([]statemachine.Effect, 3_001)} {
		records, err := executor.Execute(ctx, effects)
		if !errors.Is(err, context.Canceled) || records != nil || called != 0 {
			t.Fatalf("pre-canceled empty: records=%d error=%v callbacks=%d", len(records), err, called)
		}
	}
}

func TestExecuteStopsBeforeUnattemptedEffectAfterRecorderCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var handled, recorded, clocked int
	executor, err := runner.New(handlerFunc(func(context.Context, statemachine.Effect) error {
		handled++
		return nil
	}), runner.Options{
		Clock: func() time.Time {
			clocked++
			return time.Time{}
		},
		Recorder: recorderFunc(func(context.Context, runner.Record) error {
			recorded++
			cancel()
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	records, err := executor.Execute(ctx, []statemachine.Effect{{Kind: "first"}, {Kind: "unattempted"}})
	if !errors.Is(err, context.Canceled) || len(records) != 1 || handled != 1 || recorded != 1 || clocked != 2 {
		t.Fatalf("records=%d error=%v handler=%d recorder=%d clock=%d", len(records), err, handled, recorded, clocked)
	}
}

func TestExecuteDoesNotRecordEffectWhenStartClockCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var handled, recorded, clocked int
	executor, err := runner.New(handlerFunc(func(context.Context, statemachine.Effect) error {
		handled++
		return nil
	}), runner.Options{
		Clock: func() time.Time {
			clocked++
			cancel()
			return time.Time{}
		},
		Recorder: recorderFunc(func(context.Context, runner.Record) error {
			recorded++
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	records, err := executor.Execute(ctx, []statemachine.Effect{{Kind: "unattempted"}})
	if !errors.Is(err, context.Canceled) || len(records) != 0 || handled != 0 || recorded != 0 || clocked != 1 {
		t.Fatalf("records=%d error=%v handler=%d recorder=%d clock=%d", len(records), err, handled, recorded, clocked)
	}
}

func TestExecuteRetainsCompletedEffectWhenNextStartClockCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var handled, recorded, clocked int
	executor, err := runner.New(handlerFunc(func(context.Context, statemachine.Effect) error {
		handled++
		return nil
	}), runner.Options{
		Clock: func() time.Time {
			clocked++
			if clocked == 3 {
				cancel()
			}
			return time.Time{}
		},
		Recorder: recorderFunc(func(context.Context, runner.Record) error {
			recorded++
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	records, err := executor.Execute(ctx, []statemachine.Effect{{Kind: "completed"}, {Kind: "unattempted"}})
	if !errors.Is(err, context.Canceled) || len(records) != 1 || records[0].Effect.Kind != "completed" ||
		handled != 1 || recorded != 1 || clocked != 3 {
		t.Fatalf("records=%v error=%v handler=%d recorder=%d clock=%d", records, err, handled, recorded, clocked)
	}
}

func TestExecuteRecordsAttemptWhenFinishClockCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var handled, recorded, clocked int
	executor, err := runner.New(handlerFunc(func(context.Context, statemachine.Effect) error {
		handled++
		return nil
	}), runner.Options{
		Clock: func() time.Time {
			clocked++
			if clocked == 2 {
				cancel()
			}
			return time.Time{}
		},
		Recorder: recorderFunc(func(context.Context, runner.Record) error {
			recorded++
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	records, err := executor.Execute(ctx, []statemachine.Effect{{Kind: "completed"}, {Kind: "unattempted"}})
	if !errors.Is(err, context.Canceled) || len(records) != 1 || records[0].Outcome != runner.OutcomeSucceeded ||
		handled != 1 || recorded != 1 || clocked != 2 {
		t.Fatalf("records=%v error=%v handler=%d recorder=%d clock=%d", records, err, handled, recorded, clocked)
	}
}

func TestExecuteLimitErrorDoesNotRenderInput(t *testing.T) {
	executor, err := runner.New(handlerFunc(func(context.Context, statemachine.Effect) error { return nil }), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	const marker = "sensitive-effect-kind"
	_, err = executor.Execute(context.Background(), []statemachine.Effect{{Kind: marker, Payload: make([]byte, 1<<20+1)}})
	if !errors.Is(err, statemachine.ErrLimitExceeded) || strings.Contains(err.Error(), marker) {
		t.Fatalf("limit error = %v", err)
	}
}

func TestExecuteCustomCompiledPlanNeedsMatchingRunnerLimits(t *testing.T) {
	machineLimits := statemachine.DefaultLimits()
	machineLimits.MaxEffectPayloadBytes++
	machine, err := statemachine.CompileWithLimits(statemachine.Definition[string, string, struct{}]{
		Version: "v2",
		Initial: "pending",
		States: []statemachine.StateDefinition[string]{
			{State: "pending", Exit: []statemachine.Effect{{Kind: "exit"}}},
			{State: "done", Entry: []statemachine.Effect{{Kind: "entry"}}},
		},
		Transitions: []statemachine.TransitionDefinition[string, string, struct{}]{
			{
				ID: "finish", Sources: []string{"pending"}, Event: "finish", To: "done",
				Effects: []statemachine.Effect{{Kind: "transition", Payload: make([]byte, machineLimits.MaxEffectPayloadBytes)}},
			},
		},
	}, machineLimits)
	if err != nil {
		t.Fatal(err)
	}
	result, err := machine.Transition(context.Background(), "pending", "finish", struct{}{}, statemachine.Metadata{})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	handler := handlerFunc(func(_ context.Context, effect statemachine.Effect) error {
		kinds = append(kinds, effect.Kind)
		return nil
	})
	defaultRunner, err := runner.New(handler, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if records, err := defaultRunner.Execute(context.Background(), result.Effects); !errors.Is(err, statemachine.ErrLimitExceeded) || records != nil || len(kinds) != 0 {
		t.Fatalf("default runner: records=%d error=%v kinds=%v", len(records), err, kinds)
	}
	customRunner, err := runner.New(handler, runner.Options{Limits: runner.Limits{
		MaxEffects: 3, MaxEffectPayloadBytes: machineLimits.MaxEffectPayloadBytes,
		MaxTotalPayloadBytes: machineLimits.MaxEffectPayloadBytes,
	}})
	if err != nil {
		t.Fatal(err)
	}
	records, err := customRunner.Execute(context.Background(), result.Effects)
	if err != nil || len(records) != 3 || len(kinds) != 3 ||
		kinds[0] != "exit" || kinds[1] != "transition" || kinds[2] != "entry" {
		t.Fatalf("custom runner: records=%d error=%v kinds=%v", len(records), err, kinds)
	}
}

func TestExecuteRejectsBeforeInputProportionalAllocation(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Skip("allocation characterization runs only in hosted CI")
	}
	executor, err := runner.New(handlerFunc(func(context.Context, statemachine.Effect) error {
		t.Fatal("handler called for rejected plan")
		return nil
	}), runner.Options{Clock: func() time.Time {
		t.Fatal("clock called for rejected plan")
		return time.Time{}
	}, Recorder: recorderFunc(func(context.Context, runner.Record) error {
		t.Fatal("recorder called for rejected plan")
		return nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	const mib = 1 << 20
	countExcess := make([]statemachine.Effect, 100_000)
	perPayloadExcess := make([]statemachine.Effect, 9)
	for index := range 8 {
		perPayloadExcess[index].Payload = make([]byte, mib)
	}
	perPayloadExcess[8].Payload = make([]byte, mib+1)
	aggregateExcess := make([]statemachine.Effect, 17)
	for index := range 15 {
		aggregateExcess[index].Payload = make([]byte, mib)
	}
	aggregateExcess[15].Payload = []byte{1}
	aggregateExcess[16].Payload = make([]byte, mib)

	for name, effects := range map[string][]statemachine.Effect{
		"count":     countExcess,
		"payload":   perPayloadExcess,
		"aggregate": aggregateExcess,
	} {
		t.Run(name, func(t *testing.T) {
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			records, err := executor.Execute(context.Background(), effects)
			runtime.ReadMemStats(&after)
			if !errors.Is(err, statemachine.ErrLimitExceeded) || records != nil {
				t.Fatalf("records=%d error=%v", len(records), err)
			}
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > mib {
				t.Fatalf("rejection allocated %d bytes, want <= %d", allocated, mib)
			}
		})
	}
	runtime.KeepAlive(countExcess)
	runtime.KeepAlive(perPayloadExcess)
	runtime.KeepAlive(aggregateExcess)
}
