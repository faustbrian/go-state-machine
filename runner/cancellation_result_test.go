package runner_test

import (
	"context"
	"errors"
	"testing"

	statemachine "github.com/faustbrian/go-state-machine/v2"
	"github.com/faustbrian/go-state-machine/v2/runner"
)

func TestExecuteRecordsReturnedCancellationWithoutClassifying(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			handled, classified, recorded := 0, 0, 0
			var observed runner.Record
			executor, err := runner.New(handlerFunc(func(context.Context, statemachine.Effect) error {
				handled++
				return cause
			}), runner.Options{
				Classify: func(error) runner.Outcome {
					classified++
					return runner.OutcomeRetryable
				},
				Recorder: recorderFunc(func(_ context.Context, record runner.Record) error {
					recorded++
					observed = record
					return nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			records, err := executor.Execute(context.Background(), []statemachine.Effect{{Kind: "first"}, {Kind: "later"}})
			var effectErr *runner.EffectError
			if !errors.As(err, &effectErr) || !errors.Is(err, cause) ||
				effectErr.Outcome != runner.OutcomeCanceled || effectErr.Index != 0 || effectErr.Kind != "first" {
				t.Fatalf("returned cancellation classification: %v", err)
			}
			if len(records) != 1 || records[0].Index != 0 || records[0].Effect.Kind != "first" ||
				records[0].Outcome != runner.OutcomeCanceled || !errors.Is(records[0].Err, cause) ||
				observed.Outcome != runner.OutcomeCanceled || !errors.Is(observed.Err, cause) ||
				handled != 1 || recorded != 1 || classified != 0 {
				t.Fatalf("attempt recording: records=%v observed=%v handled=%d recorded=%d classified=%d", records, observed, handled, recorded, classified)
			}
		})
	}
}
