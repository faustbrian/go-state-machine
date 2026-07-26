package statemachine_test

import (
	"errors"
	"testing"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
)

func TestCompileRejectsInvalidGraphs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		definition statemachine.Definition[orderState, orderEvent, int]
		code       statemachine.DiagnosticCode
	}{
		{
			name: "missing version",
			definition: statemachine.Definition[orderState, orderEvent, int]{
				Initial: orderPending,
				States:  []statemachine.StateDefinition[orderState]{{State: orderPending}},
			},
			code: statemachine.DiagnosticMissingVersion,
		},
		{
			name: "duplicate state",
			definition: statemachine.Definition[orderState, orderEvent, int]{
				Version: "v1",
				Initial: orderPending,
				States: []statemachine.StateDefinition[orderState]{
					{State: orderPending},
					{State: orderPending},
				},
			},
			code: statemachine.DiagnosticDuplicateState,
		},
		{
			name: "ambiguous exact transition",
			definition: validDefinition(
				statemachine.TransitionDefinition[orderState, orderEvent, int]{
					ID: "pay-again", Sources: []orderState{orderPending}, Event: orderPay, To: orderPaid,
				},
			),
			code: statemachine.DiagnosticAmbiguousTransition,
		},
		{
			name: "ambiguous wildcard transition",
			definition: validDefinition(
				statemachine.TransitionDefinition[orderState, orderEvent, int]{
					ID: "fallback", Wildcard: true, Event: orderPay, To: orderPaid,
				},
			),
			code: statemachine.DiagnosticAmbiguousWildcard,
		},
		{
			name: "outgoing terminal transition",
			definition: statemachine.Definition[orderState, orderEvent, int]{
				Version: "v1",
				Initial: orderPaid,
				States: []statemachine.StateDefinition[orderState]{
					{State: orderPaid, Terminal: true},
				},
				Transitions: []statemachine.TransitionDefinition[orderState, orderEvent, int]{
					{ID: "invalid", Sources: []orderState{orderPaid}, Event: orderPay, To: orderPaid},
				},
			},
			code: statemachine.DiagnosticTerminalTransition,
		},
		{
			name: "unreachable state",
			definition: statemachine.Definition[orderState, orderEvent, int]{
				Version: "v1",
				Initial: orderPending,
				States: []statemachine.StateDefinition[orderState]{
					{State: orderPending},
					{State: orderPaid},
				},
			},
			code: statemachine.DiagnosticUnreachableState,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := statemachine.Compile(test.definition)
			if err == nil {
				t.Fatal("compile succeeded, want diagnostic error")
			}
			var diagnostics *statemachine.DiagnosticsError
			if !errors.As(err, &diagnostics) {
				t.Fatalf("error = %T, want *DiagnosticsError", err)
			}
			if !diagnostics.Has(test.code) {
				t.Fatalf("diagnostics = %#v, want code %q", diagnostics.Diagnostics, test.code)
			}
		})
	}
}

func TestCompileDoesNotTreatTerminalWildcardAsReachable(t *testing.T) {
	t.Parallel()

	_, err := statemachine.Compile(statemachine.Definition[string, string, struct{}]{
		Version: "v1", Initial: "closed",
		States: []statemachine.StateDefinition[string]{
			{State: "closed", Terminal: true},
			{State: "impossible", Terminal: true},
		},
		Transitions: []statemachine.TransitionDefinition[string, string, struct{}]{
			{ID: "wildcard", Wildcard: true, Event: "reopen", To: "impossible"},
		},
	})
	var diagnostics *statemachine.DiagnosticsError
	if !errors.As(err, &diagnostics) || !diagnostics.Has(statemachine.DiagnosticUnreachableState) {
		t.Fatalf("error = %v, want unreachable state diagnostic", err)
	}
}

func validDefinition(additional ...statemachine.TransitionDefinition[orderState, orderEvent, int]) statemachine.Definition[orderState, orderEvent, int] {
	transitions := []statemachine.TransitionDefinition[orderState, orderEvent, int]{
		{ID: "pay", Sources: []orderState{orderPending}, Event: orderPay, To: orderPaid},
		{ID: "wildcard-pay", Wildcard: true, Event: orderPay, To: orderPaid},
	}
	transitions = append(transitions, additional...)

	return statemachine.Definition[orderState, orderEvent, int]{
		Version: "v1",
		Initial: orderPending,
		States: []statemachine.StateDefinition[orderState]{
			{State: orderPending},
			{State: orderPaid, Terminal: true},
		},
		Transitions: transitions,
	}
}
