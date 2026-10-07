package statemachine_test

import (
	"context"
	"errors"
	"testing"

	statemachine "github.com/faustbrian/go-state-machine/v2"
)

func TestEvolutionRejectsEachZeroExplicitLimit(t *testing.T) {
	for _, test := range []struct {
		name string
		zero func(*statemachine.EvolutionLimits)
	}{
		{"migrations", func(l *statemachine.EvolutionLimits) { l.MaxMigrations = 0 }},
		{"history", func(l *statemachine.EvolutionLimits) { l.MaxHistoryEntries = 0 }},
		{"steps", func(l *statemachine.EvolutionLimits) { l.MaxMigrationSteps = 0 }},
		{"effects", func(l *statemachine.EvolutionLimits) { l.MaxCarriedEffects = 0 }},
		{"effect payload", func(l *statemachine.EvolutionLimits) { l.MaxEffectPayloadBytes = 0 }},
		{"carried payload", func(l *statemachine.EvolutionLimits) { l.MaxCarriedPayloadBytes = 0 }},
		{"version", func(l *statemachine.EvolutionLimits) { l.MaxVersionBytes = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			limits := statemachine.DefaultEvolutionLimits()
			test.zero(&limits)
			evolution, err := statemachine.CompileEvolutionWithLimits[string, string](nil, limits)
			if evolution != nil || !errors.Is(err, statemachine.ErrInvalidEvolution) {
				t.Fatalf("zero explicit %s = (nonNil=%t, err=%v), want nil/invalid", test.name, evolution != nil, err)
			}
		})
	}
}

func TestEvolutionInclusiveTargetAndHistoryVersionBounds(t *testing.T) {
	limits := statemachine.DefaultEvolutionLimits()
	limits.MaxVersionBytes = 2
	for _, migration := range []statemachine.Migration[string, string]{
		{From: "a", To: "bb"},
		{From: "aa", To: "b"},
	} {
		evolution, err := statemachine.CompileEvolutionWithLimits([]statemachine.Migration[string, string]{migration}, limits)
		if err != nil {
			t.Fatalf("inclusive version compilation: %v", err)
		}
		snapshot := statemachine.Snapshot[string]{State: "ready", DefinitionVersion: migration.From}
		history := []statemachine.HistoryEntry[string, string]{{Result: statemachine.Result[string, string]{
			DefinitionVersion: migration.From, Previous: "before", Next: "after", Event: "event",
		}}}
		got, migrated, err := evolution.Migrate(context.Background(), snapshot, history, migration.To)
		if err != nil || got.State != "ready" || got.DefinitionVersion != migration.To || len(migrated) != 1 {
			t.Fatalf("inclusive version migration = (%#v, history=%d, %v)", got, len(migrated), err)
		}
		result := migrated[0].Result
		if result.DefinitionVersion != migration.To || result.Previous != "before" || result.Next != "after" || result.Event != "event" {
			t.Fatalf("identity history migration changed result: %#v", result)
		}
		if history[0].Result.DefinitionVersion != migration.From || snapshot.DefinitionVersion != migration.From {
			t.Fatal("migration mutated caller versions")
		}
	}
}

func TestEvolutionCountsEffectsAcrossHistoryBeforeHooks(t *testing.T) {
	limits := statemachine.DefaultEvolutionLimits()
	limits.MaxCarriedEffects = 2
	hookCalls := 0
	evolution, err := statemachine.CompileEvolutionWithLimits([]statemachine.Migration[string, string]{
		{From: "a", To: "b", State: func(value string) (string, error) {
			hookCalls++
			return value + "!", nil
		}},
	}, limits)
	if err != nil {
		t.Fatal(err)
	}
	history := make([]statemachine.HistoryEntry[string, string], 3)
	for index := range history {
		history[index].Result.DefinitionVersion = "b"
		history[index].Result.Effects = []statemachine.Effect{{Kind: "work"}}
	}
	input := statemachine.Snapshot[string]{State: "ready", DefinitionVersion: "a"}
	got, migrated, err := evolution.Migrate(context.Background(), input, history[:2], "b")
	if err != nil || got.State != "ready!" || got.DefinitionVersion != "b" || len(migrated) != 2 || hookCalls != 1 {
		t.Fatalf("inclusive cumulative effect budget = (%#v, history=%d, err=%v, hooks=%d)", got, len(migrated), err, hookCalls)
	}
	hookCalls = 0
	got, migrated, err = evolution.Migrate(context.Background(), input, history, "b")
	if !errors.Is(err, statemachine.ErrLimitExceeded) || got != (statemachine.Snapshot[string]{}) || migrated != nil || hookCalls != 0 {
		t.Fatalf("cumulative effect excess = (%#v, history=%d, err=%v, hooks=%d), want zero/nil limit before hooks", got, len(migrated), err, hookCalls)
	}
}

func TestEvolutionRunsEveryNonidentityStateHook(t *testing.T) {
	evolution, err := statemachine.CompileEvolution([]statemachine.Migration[string, string]{
		{From: "a", To: "b", State: func(value string) (string, error) { return value + "1", nil }},
		{From: "b", To: "c", State: func(value string) (string, error) { return value + "2", nil }},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, migrated, err := evolution.Migrate(context.Background(), statemachine.Snapshot[string]{
		State: "ready", DefinitionVersion: "a",
	}, nil, "c")
	if err != nil || got.State != "ready12" || got.DefinitionVersion != "c" || len(migrated) != 0 {
		t.Fatalf("two nonidentity state steps = (%#v, history=%d, err=%v), want ready12/c", got, len(migrated), err)
	}
}
