package statemachine_test

import (
	"errors"
	"testing"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
)

func TestValidateHistoryRejectsCorruption(t *testing.T) {
	t.Parallel()

	snapshot := statemachine.Snapshot[string]{
		InstanceID: "order-1", State: "pending", DefinitionVersion: "v1", LockVersion: 0,
	}
	valid := []statemachine.HistoryEntry[string, string]{
		{
			InstanceID: "order-1", Sequence: 1,
			Result: statemachine.Result[string, string]{
				DefinitionVersion: "v1", Previous: "pending", Next: "paid",
				Event: "pay", TransitionID: "pay",
			},
		},
		{
			InstanceID: "order-1", Sequence: 2,
			Result: statemachine.Result[string, string]{
				DefinitionVersion: "v1", Previous: "paid", Next: "shipped",
				Event: "ship", TransitionID: "ship",
			},
		},
	}

	tests := []struct {
		name   string
		mutate func([]statemachine.HistoryEntry[string, string]) []statemachine.HistoryEntry[string, string]
		reason statemachine.HistoryFailure
	}{
		{"missing", func(entries []statemachine.HistoryEntry[string, string]) []statemachine.HistoryEntry[string, string] {
			entries[0].Sequence = 2
			return entries
		}, statemachine.HistorySequenceMismatch},
		{"duplicate", func(entries []statemachine.HistoryEntry[string, string]) []statemachine.HistoryEntry[string, string] {
			entries[1].Sequence = 1
			return entries
		}, statemachine.HistorySequenceMismatch},
		{"reordered", func(entries []statemachine.HistoryEntry[string, string]) []statemachine.HistoryEntry[string, string] {
			return []statemachine.HistoryEntry[string, string]{entries[1], entries[0]}
		}, statemachine.HistorySequenceMismatch},
		{"wrong prior state", func(entries []statemachine.HistoryEntry[string, string]) []statemachine.HistoryEntry[string, string] {
			entries[1].Result.Previous = "pending"
			return entries
		}, statemachine.HistoryStateMismatch},
		{"wrong instance", func(entries []statemachine.HistoryEntry[string, string]) []statemachine.HistoryEntry[string, string] {
			entries[1].InstanceID = "order-2"
			return entries
		}, statemachine.HistoryInstanceMismatch},
		{"missing version", func(entries []statemachine.HistoryEntry[string, string]) []statemachine.HistoryEntry[string, string] {
			entries[1].Result.DefinitionVersion = ""
			return entries
		}, statemachine.HistoryMissingIdentity},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			entries := append([]statemachine.HistoryEntry[string, string](nil), valid...)
			entries = test.mutate(entries)
			_, err := statemachine.ValidateHistory(snapshot, entries)
			var historyErr *statemachine.HistoryError
			if !errors.As(err, &historyErr) || historyErr.Failure != test.reason {
				t.Fatalf("error = %#v, want failure %q", err, test.reason)
			}
		})
	}

	final, err := statemachine.ValidateHistory(snapshot, valid)
	if err != nil || final.State != "shipped" || final.LockVersion != 2 {
		t.Fatalf("valid history final = %#v, %v", final, err)
	}
}

func TestMachineValidateHistoryRejectsIncompatibleDefinition(t *testing.T) {
	t.Parallel()

	machine, err := statemachine.Compile(statemachine.Definition[string, string, struct{}]{
		Version: "v1", Initial: "pending",
		States: []statemachine.StateDefinition[string]{
			{State: "pending"},
			{State: "paid", Terminal: true},
		},
		Transitions: []statemachine.TransitionDefinition[string, string, struct{}]{
			{ID: "pay-order", Sources: []string{"pending"}, Event: "pay", To: "paid"},
		},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	snapshot := statemachine.Snapshot[string]{
		InstanceID: "order-1", State: "pending", DefinitionVersion: "v1",
	}
	valid := statemachine.HistoryEntry[string, string]{
		InstanceID: "order-1", Sequence: 1,
		Result: statemachine.Result[string, string]{
			DefinitionVersion: "v1", Previous: "pending", Next: "paid",
			Event: "pay", TransitionID: "pay-order",
		},
	}
	if final, err := machine.ValidateHistory(snapshot, []statemachine.HistoryEntry[string, string]{valid}); err != nil || final.State != "paid" {
		t.Fatalf("valid compatible history = %#v, %v", final, err)
	}
	corruptSequence := valid
	corruptSequence.Sequence = 2
	if _, err := machine.ValidateHistory(snapshot, []statemachine.HistoryEntry[string, string]{corruptSequence}); err == nil {
		t.Fatal("machine validation accepted structurally corrupt history")
	}

	tests := []struct {
		name   string
		mutate func(*statemachine.Snapshot[string], *statemachine.HistoryEntry[string, string])
	}{
		{"snapshot version", func(snapshot *statemachine.Snapshot[string], _ *statemachine.HistoryEntry[string, string]) {
			snapshot.DefinitionVersion = "v0"
		}},
		{"result version", func(_ *statemachine.Snapshot[string], entry *statemachine.HistoryEntry[string, string]) {
			entry.Result.DefinitionVersion = "v2"
		}},
		{"transition identifier", func(_ *statemachine.Snapshot[string], entry *statemachine.HistoryEntry[string, string]) {
			entry.Result.TransitionID = "renamed-go-symbol"
		}},
		{"event identifier", func(_ *statemachine.Snapshot[string], entry *statemachine.HistoryEntry[string, string]) {
			entry.Result.Event = "charge"
		}},
		{"destination state", func(_ *statemachine.Snapshot[string], entry *statemachine.HistoryEntry[string, string]) {
			entry.Result.Next = "pending"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			candidateSnapshot, candidateEntry := snapshot, valid
			test.mutate(&candidateSnapshot, &candidateEntry)
			_, err := machine.ValidateHistory(candidateSnapshot, []statemachine.HistoryEntry[string, string]{candidateEntry})
			var historyErr *statemachine.HistoryError
			if !errors.As(err, &historyErr) || historyErr.Failure != statemachine.HistoryDefinitionMismatch {
				t.Fatalf("error = %#v, want definition mismatch", err)
			}
		})
	}
}
