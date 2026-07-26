package statemachine

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCompileReportsRemainingDiagnosticClasses(t *testing.T) {
	t.Parallel()

	type state string
	type event string
	base := func() Definition[state, event, struct{}] {
		return Definition[state, event, struct{}]{
			Version: "v1", Initial: "a",
			States: []StateDefinition[state]{{State: "a"}, {State: "b", Terminal: true}},
			Transitions: []TransitionDefinition[state, event, struct{}]{
				{ID: "go", Sources: []state{"a"}, Event: "go", To: "b"},
			},
		}
	}
	tests := []struct {
		name   string
		code   DiagnosticCode
		mutate func(*Definition[state, event, struct{}])
	}{
		{"missing initial", DiagnosticMissingInitial, func(definition *Definition[state, event, struct{}]) { definition.Initial = "missing" }},
		{"unknown destination", DiagnosticUnknownState, func(definition *Definition[state, event, struct{}]) { definition.Transitions[0].To = "missing" }},
		{"unknown source", DiagnosticUnknownState, func(definition *Definition[state, event, struct{}]) {
			definition.Transitions[0].Sources = []state{"missing"}
		}},
		{"missing transition ID", DiagnosticMissingTransitionID, func(definition *Definition[state, event, struct{}]) { definition.Transitions[0].ID = "" }},
		{"duplicate transition ID", DiagnosticDuplicateTransition, func(definition *Definition[state, event, struct{}]) {
			definition.Transitions = append(definition.Transitions, TransitionDefinition[state, event, struct{}]{ID: "go", Sources: []state{"a"}, Event: "other", To: "b"})
		}},
		{"missing source", DiagnosticMissingSource, func(definition *Definition[state, event, struct{}]) { definition.Transitions[0].Sources = nil }},
		{"wildcard sources", DiagnosticInvalidWildcard, func(definition *Definition[state, event, struct{}]) { definition.Transitions[0].Wildcard = true }},
		{"missing state effect kind", DiagnosticMissingEffectKind, func(definition *Definition[state, event, struct{}]) { definition.States[0].Exit = []Effect{{}} }},
		{"missing transition effect kind", DiagnosticMissingEffectKind, func(definition *Definition[state, event, struct{}]) { definition.Transitions[0].Effects = []Effect{{}} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := base()
			test.mutate(&definition)
			_, err := Compile(definition)
			var diagnostics *DiagnosticsError
			if !errors.As(err, &diagnostics) || !diagnostics.Has(test.code) {
				t.Fatalf("error = %v, want diagnostic %s", err, test.code)
			}
			if diagnostics.Error() == "" || diagnostics.Has("not-present") {
				t.Fatalf("invalid diagnostics behavior: %v", diagnostics)
			}
		})
	}
}

func TestCompileReportsNestedAndInvalidLimits(t *testing.T) {
	t.Parallel()

	type state string
	type event string
	definition := Definition[state, event, struct{}]{
		Version: "v1", Initial: "a",
		States: []StateDefinition[state]{
			{State: "a", Exit: []Effect{{Kind: "one"}, {Kind: "two"}}},
			{State: "b", Terminal: true},
		},
		Transitions: []TransitionDefinition[state, event, struct{}]{
			{ID: "go", Sources: []state{"a", "a"}, Event: "go", To: "b",
				Guards:  []Guard[struct{}]{func(context.Context, struct{}) *Rejection { return nil }, func(context.Context, struct{}) *Rejection { return nil }},
				Effects: []Effect{{Kind: "large", Payload: []byte("12345")}}},
		},
	}
	limits := Limits{
		MaxStates: 2, MaxTransitions: 2, MaxSourcesPerTransition: 1,
		MaxGuardsPerTransition: 1, MaxEffectsPerPhase: 1,
		MaxEffectPayloadBytes: 4, MaxMetadataBytes: 4, MaxReplayInputs: 4,
	}
	_, err := CompileWithLimits(definition, limits)
	var diagnostics *DiagnosticsError
	if !errors.As(err, &diagnostics) || !diagnostics.Has(DiagnosticLimitExceeded) {
		t.Fatalf("error = %v, want limit diagnostics", err)
	}
	_, err = CompileWithLimits(definition, Limits{})
	if !errors.As(err, &diagnostics) || !diagnostics.Has(DiagnosticLimitExceeded) {
		t.Fatalf("invalid limits error = %v", err)
	}
}

func TestTransitionErrorContractsAndSelfEffects(t *testing.T) {
	t.Parallel()

	type state string
	type event string
	ctx, cancel := context.WithCancel(context.Background())
	secondCalled := false
	machine, err := Compile(Definition[state, event, struct{}]{
		Version: "v1", Initial: "a",
		States: []StateDefinition[state]{{State: "a", Exit: []Effect{{Kind: "exit"}}, Entry: []Effect{{Kind: "entry"}}}, {State: "done", Terminal: true}},
		Transitions: []TransitionDefinition[state, event, struct{}]{
			{ID: "self", Sources: []state{"a"}, Event: "self", To: "a", Effects: []Effect{{Kind: "transition"}}},
			{ID: "cancel", Sources: []state{"a"}, Event: "cancel", To: "done", Guards: []Guard[struct{}]{
				func(context.Context, struct{}) *Rejection { cancel(); return nil },
				func(context.Context, struct{}) *Rejection { secondCalled = true; return nil },
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := machine.Transition(context.Background(), "missing", "self", struct{}{}, Metadata{}); !errors.Is(err, ErrUnknownState) {
		t.Fatalf("unknown state error = %v", err)
	}
	if _, err := machine.Transition(context.Background(), "a", "missing", struct{}{}, Metadata{}); !errors.Is(err, ErrNoTransition) {
		t.Fatalf("missing transition error = %v", err)
	}
	result, err := machine.Transition(context.Background(), "a", "self", struct{}{}, Metadata{})
	if err != nil || len(result.Effects) != 3 || result.Effects[0].Kind != "exit" || result.Effects[2].Kind != "entry" {
		t.Fatalf("self transition = %#v, %v", result, err)
	}
	if _, err := machine.Transition(ctx, "a", "cancel", struct{}{}, Metadata{}); !errors.Is(err, context.Canceled) || secondCalled {
		t.Fatalf("between-guard cancellation = %v, second called = %t", err, secondCalled)
	}
}

func TestTypedErrorsFormatAndUnwrap(t *testing.T) {
	t.Parallel()

	rejected := &GuardRejectedError{TransitionID: "go", Rejection: Rejection{Code: "no", Message: "declined"}}
	if !errors.Is(rejected, ErrGuardRejected) || !strings.Contains(rejected.Error(), "declined") {
		t.Fatalf("rejected error = %v", rejected)
	}
	panicked := &GuardPanicError{TransitionID: "go"}
	if !errors.Is(panicked, ErrGuardPanic) {
		t.Fatalf("panic error = %v", panicked)
	}
	replay := &ReplayError{Index: 2, Cause: ErrNoTransition}
	if !errors.Is(replay, ErrNoTransition) || !strings.Contains(replay.Error(), "2") {
		t.Fatalf("replay error = %v", replay)
	}
	history := &HistoryError{Index: 3, Failure: HistoryStateMismatch}
	if !strings.Contains(history.Error(), "state_mismatch") {
		t.Fatalf("history error = %v", history)
	}
}

func TestReplayAndHistoryRemainingFailures(t *testing.T) {
	t.Parallel()

	machine, err := Compile(Definition[string, string, struct{}]{
		Version: "v1", Initial: "a",
		States:      []StateDefinition[string]{{State: "a"}, {State: "b", Terminal: true}},
		Transitions: []TransitionDefinition[string, string, struct{}]{{ID: "go", Sources: []string{"a"}, Event: "go", To: "b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = machine.Replay(context.Background(), []Input[string, struct{}]{{Event: "missing"}})
	var replayErr *ReplayError
	if !errors.As(err, &replayErr) || replayErr.Index != 0 {
		t.Fatalf("replay error = %v", err)
	}
	if _, err := ValidateHistoryWithLimit(Snapshot[string]{}, []HistoryEntry[string, string](nil), 1); !errors.As(err, new(*HistoryError)) {
		t.Fatalf("invalid snapshot error = %v", err)
	}
	snapshot := Snapshot[string]{InstanceID: "one", State: "a", DefinitionVersion: "v1"}
	if _, err := ValidateHistoryWithLimit(snapshot, []HistoryEntry[string, string](nil), 0); !errors.As(err, new(*HistoryError)) {
		t.Fatalf("invalid limit error = %v", err)
	}
}
