package statemachine_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	statemachine "github.com/faustbrian/go-state-machine/v2"
)

func TestZeroEvolutionPreservesIdentityAndCopiesHistory(t *testing.T) {
	var evolution statemachine.Evolution[string, string]
	snapshot := statemachine.Snapshot[string]{State: "ready", DefinitionVersion: "v1"}
	history := []statemachine.HistoryEntry[string, string]{{Result: statemachine.Result[string, string]{
		DefinitionVersion: "v1", Previous: "ready", Next: "done", Event: "go",
		Effects: []statemachine.Effect{{Kind: "notification", Payload: []byte{1}}},
	}}}
	got, migrated, err := evolution.Migrate(context.Background(), snapshot, history, "v1")
	if err != nil || got != snapshot || !reflect.DeepEqual(migrated, history) {
		t.Fatalf("zero evolution identity: snapshot=%v history=%v error=%v", got, migrated, err)
	}
	migrated[0].Result.Next = "changed"
	migrated[0].Result.Effects[0].Payload[0] = 9
	if history[0].Result.Next != "done" || history[0].Result.Effects[0].Payload[0] != 1 {
		t.Fatal("identity migration exposed caller-owned history storage")
	}
}

func TestEvolutionCancellationFromHooksReturnsNoPartialOutput(t *testing.T) {
	for _, canceledField := range []string{"snapshot", "next", "event"} {
		t.Run(canceledField, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls []string
			hook := func(value string) (string, error) {
				calls = append(calls, value)
				if value == canceledField {
					cancel()
				}
				return "converted-" + value, nil
			}
			evolution, err := statemachine.CompileEvolution([]statemachine.Migration[string, string]{
				{From: "v1", To: "v2", State: hook, Event: hook},
			})
			if err != nil {
				t.Fatal(err)
			}
			snapshot := statemachine.Snapshot[string]{State: "snapshot", DefinitionVersion: "v1"}
			history := []statemachine.HistoryEntry[string, string]{
				{Result: statemachine.Result[string, string]{
					DefinitionVersion: "v1", Previous: "previous", Next: "next", Event: "event",
				}},
				{Result: statemachine.Result[string, string]{
					DefinitionVersion: "v1", Previous: "later", Next: "later-next", Event: "later-event",
				}},
			}
			got, migrated, err := evolution.Migrate(ctx, snapshot, history, "v2")
			if !errors.Is(err, context.Canceled) || got != (statemachine.Snapshot[string]{}) || migrated != nil {
				t.Fatalf("canceled migration returned partial output: snapshot=%v history=%v error=%v", got, migrated, err)
			}
			wantCalls := map[string][]string{
				"snapshot": {"snapshot"},
				"next":     {"snapshot", "previous", "next"},
				"event":    {"snapshot", "previous", "next", "event"},
			}[canceledField]
			if !reflect.DeepEqual(calls, wantCalls) {
				t.Fatalf("callbacks after cancellation: got=%v want=%v", calls, wantCalls)
			}
			if snapshot.State != "snapshot" || snapshot.DefinitionVersion != "v1" ||
				history[0].Result.Previous != "previous" || history[0].Result.Next != "next" ||
				history[0].Result.Event != "event" || history[0].Result.DefinitionVersion != "v1" ||
				history[1].Result.Previous != "later" {
				t.Fatal("canceled migration mutated caller-owned input")
			}
		})
	}
}
