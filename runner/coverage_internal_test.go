package runner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	statemachine "github.com/faustbrian/go-state-machine/v2"
)

type internalHandler func(context.Context, statemachine.Effect) error

func (handler internalHandler) Handle(ctx context.Context, effect statemachine.Effect) error {
	return handler(ctx, effect)
}

type internalRecorder func(context.Context, Record) error

func (recorder internalRecorder) Record(ctx context.Context, record Record) error {
	return recorder(ctx, record)
}

func TestRunnerRemainingConstructionAndFailurePaths(t *testing.T) {
	t.Parallel()

	if _, err := New(nil, Options{}); !errors.Is(err, ErrMissingHandler) {
		t.Fatalf("missing handler error = %v", err)
	}
	wantErr := errors.New("handler failed")
	executor, err := New(internalHandler(func(context.Context, statemachine.Effect) error { return wantErr }), Options{
		Classify: func(error) Outcome { return OutcomeSucceeded },
	})
	if err != nil {
		t.Fatal(err)
	}
	records, err := executor.Execute(context.Background(), []statemachine.Effect{{Kind: "bad"}})
	var effectErr *EffectError
	if !errors.As(err, &effectErr) || !errors.Is(err, wantErr) || effectErr.Outcome != OutcomePermanent ||
		effectErr.Kind != "bad" || len(records) != 1 {
		t.Fatalf("records = %#v, error = %v", records, err)
	}

	recorderErr := errors.New("recorder failed")
	executor, _ = New(internalHandler(func(context.Context, statemachine.Effect) error { return nil }), Options{
		Recorder: internalRecorder(func(context.Context, Record) error { return recorderErr }),
	})
	_, err = executor.Execute(context.Background(), []statemachine.Effect{{Kind: "record"}})
	var typedRecorderErr *RecorderError
	if !errors.As(err, &typedRecorderErr) || !errors.Is(err, recorderErr) || !strings.Contains(err.Error(), "0") {
		t.Fatalf("recorder error = %v", err)
	}
}

func TestRunnerRecordsCancellationBetweenEffects(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	called, clocked, recorded := 0, 0, 0
	executor, _ := New(internalHandler(func(context.Context, statemachine.Effect) error {
		called++
		cancel()
		return nil
	}), Options{
		Clock: func() time.Time {
			clocked++
			return time.Unix(1, 0)
		},
		Recorder: internalRecorder(func(context.Context, Record) error {
			recorded++
			return nil
		}),
	})
	records, err := executor.Execute(ctx, []statemachine.Effect{{Kind: "first"}, {Kind: "second"}})
	if !errors.Is(err, context.Canceled) || called != 1 || len(records) != 1 ||
		records[0].Outcome != OutcomeSucceeded || clocked != 2 || recorded != 1 {
		t.Fatalf("called = %d, clocked = %d, recorded = %d, records = %#v, error = %v", called, clocked, recorded, records, err)
	}
}

func TestRunnerClonesEffectPayloadForHandlerAndRecord(t *testing.T) {
	t.Parallel()

	effect := statemachine.Effect{Kind: "one", Payload: []byte("original")}
	executor, _ := New(internalHandler(func(_ context.Context, handled statemachine.Effect) error {
		handled.Payload[0] = 'X'
		return nil
	}), Options{})
	records, err := executor.Execute(context.Background(), []statemachine.Effect{effect})
	if err != nil || string(effect.Payload) != "original" || string(records[0].Effect.Payload) != "original" {
		t.Fatalf("effect = %q, record = %q, error = %v", effect.Payload, records[0].Effect.Payload, err)
	}
}
