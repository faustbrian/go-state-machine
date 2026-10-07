package statemachine_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	statemachine "github.com/faustbrian/go-state-machine/v2"
)

func TestPublicErrorsDoNotRenderAttackerControlledValues(t *testing.T) {
	t.Parallel()

	const sensitive = "attacker-payload-customer-token"

	t.Run("diagnostics", func(t *testing.T) {
		_, err := statemachine.Compile(statemachine.Definition[string, string, struct{}]{
			Version: "v1", Initial: sensitive,
			States: []statemachine.StateDefinition[string]{{State: sensitive}, {State: sensitive}},
		})
		if err == nil || strings.Contains(err.Error(), sensitive) {
			t.Fatalf("diagnostic error exposed sensitive value: %v", err)
		}
	})

	t.Run("constructed diagnostics", func(t *testing.T) {
		err := &statemachine.DiagnosticsError{Diagnostics: []statemachine.Diagnostic{{
			Code: sensitive, Message: sensitive, TransitionID: sensitive,
		}}}
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("constructed diagnostic exposed sensitive value: %q", err.Error())
		}
	})

	t.Run("history", func(t *testing.T) {
		err := &statemachine.HistoryError{Index: 1, Failure: sensitive}
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("history error exposed sensitive value: %q", err.Error())
		}
	})

	t.Run("constructed migration", func(t *testing.T) {
		err := &statemachine.MigrationError{From: sensitive, To: sensitive, Field: sensitive}
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("migration error exposed sensitive value: %q", err.Error())
		}
	})

	t.Run("guard rejection", func(t *testing.T) {
		err := &statemachine.GuardRejectedError{
			TransitionID: sensitive,
			Rejection:    statemachine.Rejection{Code: sensitive, Message: sensitive},
		}
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("guard rejection exposed sensitive value: %q", err.Error())
		}
	})

	t.Run("guard panic", func(t *testing.T) {
		err := &statemachine.GuardPanicError{TransitionID: sensitive}
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("guard panic exposed sensitive value: %q", err.Error())
		}
	})

	t.Run("guard failure", func(t *testing.T) {
		cause := errors.New(sensitive)
		err := &statemachine.GuardFailedError{TransitionID: sensitive, Cause: cause}
		if !errors.Is(err, cause) || strings.Contains(err.Error(), sensitive) {
			t.Fatalf("guard failure = %q, want redacted wrapped cause", err.Error())
		}
	})

	t.Run("replay", func(t *testing.T) {
		cause := errors.New(sensitive)
		err := &statemachine.ReplayError{Index: 1, Cause: cause}
		if !errors.Is(err, cause) || strings.Contains(err.Error(), sensitive) {
			t.Fatalf("replay error = %q, want redacted wrapped cause", err.Error())
		}
	})

	t.Run("migration", func(t *testing.T) {
		cause := errors.New(sensitive)
		evolution, err := statemachine.CompileEvolution([]statemachine.Migration[string, string]{
			{From: sensitive, To: "v2", State: func(string) (string, error) { return "", cause }},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = evolution.Migrate(context.Background(), statemachine.Snapshot[string]{
			DefinitionVersion: sensitive,
		}, nil, "v2")
		if !errors.Is(err, cause) || strings.Contains(err.Error(), sensitive) {
			t.Fatalf("migration error = %q, want redacted wrapped cause", err.Error())
		}
	})
}
