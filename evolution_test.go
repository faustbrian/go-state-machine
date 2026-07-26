package statemachine_test

import (
	"context"
	"testing"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
)

func TestEvolutionMigratesSnapshotAndHistoryThroughVersions(t *testing.T) {
	t.Parallel()

	evolution, err := statemachine.CompileEvolution([]statemachine.Migration[string, string]{
		{
			From: "v1", To: "v2",
			State: func(value string) (string, error) {
				if value == "pending" {
					return "awaiting-payment", nil
				}
				return value, nil
			},
			Event: func(value string) (string, error) {
				if value == "pay" {
					return "capture", nil
				}
				return value, nil
			},
		},
		{From: "v2", To: "v3"},
	})
	if err != nil {
		t.Fatalf("compile evolution: %v", err)
	}
	snapshot := statemachine.Snapshot[string]{
		InstanceID: "order-1", State: "pending", DefinitionVersion: "v1", LockVersion: 4,
	}
	history := []statemachine.HistoryEntry[string, string]{
		{Result: statemachine.Result[string, string]{
			DefinitionVersion: "v1", Previous: "pending", Next: "paid", Event: "pay", TransitionID: "pay-order",
		}},
	}

	migratedSnapshot, migratedHistory, err := evolution.Migrate(context.Background(), snapshot, history, "v3")
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if migratedSnapshot.State != "awaiting-payment" || migratedSnapshot.DefinitionVersion != "v3" {
		t.Fatalf("snapshot = %#v", migratedSnapshot)
	}
	if migratedHistory[0].Result.Event != "capture" || migratedHistory[0].Result.Previous != "awaiting-payment" || migratedHistory[0].Result.DefinitionVersion != "v3" {
		t.Fatalf("history = %#v", migratedHistory)
	}
}
