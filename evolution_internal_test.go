package statemachine

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestEvolutionRejectsInvalidGraphsAndMissingPaths(t *testing.T) {
	t.Parallel()

	tests := [][]Migration[string, string]{
		{{From: "", To: "v2"}},
		{{From: "v1", To: "v1"}},
		{{From: "v1", To: "v2"}, {From: "v1", To: "v3"}},
		{{From: "v1", To: "v2"}, {From: "v2", To: "v1"}},
	}
	for _, migrations := range tests {
		if _, err := CompileEvolution(migrations); !errors.Is(err, ErrInvalidEvolution) {
			t.Fatalf("compile error = %v, want ErrInvalidEvolution", err)
		}
	}
	evolution, err := CompileEvolution([]Migration[string, string]{{From: "v1", To: "v2"}})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = evolution.Migrate(context.Background(), Snapshot[string]{
		InstanceID: "one", State: "a", DefinitionVersion: "v1",
	}, nil, "v3")
	if !errors.Is(err, ErrMissingMigration) {
		t.Fatalf("migration error = %v, want ErrMissingMigration", err)
	}
	_, _, err = evolution.Migrate(context.Background(), Snapshot[string]{
		InstanceID: "one", State: "a", DefinitionVersion: "v1",
	}, nil, "")
	if !errors.Is(err, ErrInvalidEvolution) {
		t.Fatalf("empty target error = %v", err)
	}
}

func TestEvolutionReportsHookFailuresAndCancellation(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("cannot migrate")
	stateEvolution, _ := CompileEvolution([]Migration[string, string]{
		{From: "v1", To: "v2", State: func(string) (string, error) { return "", wantErr }},
	})
	_, _, err := stateEvolution.Migrate(context.Background(), Snapshot[string]{
		InstanceID: "one", State: "a", DefinitionVersion: "v1",
	}, nil, "v2")
	var migrationErr *MigrationError
	if !errors.As(err, &migrationErr) || !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "state") {
		t.Fatalf("state migration error = %v", err)
	}

	call := 0
	resultEvolution, _ := CompileEvolution([]Migration[string, string]{
		{From: "v1", To: "v2", State: func(value string) (string, error) {
			call++
			if call == 2 {
				return "", wantErr
			}
			return value, nil
		}},
	})
	_, _, err = resultEvolution.Migrate(context.Background(), Snapshot[string]{
		InstanceID: "one", State: "a", DefinitionVersion: "v2",
	}, []HistoryEntry[string, string]{{Result: Result[string, string]{
		DefinitionVersion: "v1", Previous: "a", Next: "b", TransitionID: "go",
	}}}, "v2")
	if !errors.As(err, &migrationErr) || migrationErr.Field != "next state" {
		t.Fatalf("next state migration error = %v", err)
	}

	eventEvolution, _ := CompileEvolution([]Migration[string, string]{
		{From: "v1", To: "v2", Event: func(string) (string, error) { return "", wantErr }},
	})
	_, _, err = eventEvolution.Migrate(context.Background(), Snapshot[string]{
		InstanceID: "one", State: "a", DefinitionVersion: "v2",
	}, []HistoryEntry[string, string]{{Result: Result[string, string]{
		DefinitionVersion: "v1", Previous: "a", Next: "b", TransitionID: "go",
	}}}, "v2")
	if !errors.As(err, &migrationErr) || migrationErr.Field != "event" {
		t.Fatalf("event migration error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = eventEvolution.Migrate(ctx, Snapshot[string]{
		InstanceID: "one", State: "a", DefinitionVersion: "v1",
	}, nil, "v2")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestEvolutionResultRemainingFailurePaths(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("previous failed")
	evolution, _ := CompileEvolution([]Migration[string, string]{
		{From: "v1", To: "v2", State: func(string) (string, error) { return "", wantErr }},
	})
	_, err := evolution.migrateResult(context.Background(), Result[string, string]{
		DefinitionVersion: "v1", Previous: "a", Next: "b",
	}, "v2")
	var migrationErr *MigrationError
	if !errors.As(err, &migrationErr) || migrationErr.Field != "previous state" {
		t.Fatalf("previous state error = %v", err)
	}

	identity, _ := CompileEvolution[string, string](nil)
	_, err = identity.migrateResult(context.Background(), Result[string, string]{DefinitionVersion: "v1"}, "v2")
	if !errors.Is(err, ErrMissingMigration) {
		t.Fatalf("missing result migration = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = evolution.migrateResult(canceled, Result[string, string]{DefinitionVersion: "v1"}, "v2")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("result cancellation = %v", err)
	}

	_, _, err = evolution.Migrate(canceled, Snapshot[string]{
		InstanceID: "one", State: "a", DefinitionVersion: "v2",
	}, []HistoryEntry[string, string]{{Result: Result[string, string]{DefinitionVersion: "v2"}}}, "v2")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("history loop cancellation = %v", err)
	}
}
