package statemachine

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEveryCompileLimitMustRemainPositive(t *testing.T) {
	t.Parallel()

	if !DefaultLimits().valid() {
		t.Fatal("DefaultLimits().valid() = false")
	}
	definition := Definition[string, string, struct{}]{
		Version: "v1", Initial: "done",
		States: []StateDefinition[string]{{State: "done", Terminal: true}},
	}
	tests := []struct {
		name string
		zero func(*Limits)
	}{
		{"states", func(limits *Limits) { limits.MaxStates = 0 }},
		{"transitions", func(limits *Limits) { limits.MaxTransitions = 0 }},
		{"sources", func(limits *Limits) { limits.MaxSourcesPerTransition = 0 }},
		{"guards", func(limits *Limits) { limits.MaxGuardsPerTransition = 0 }},
		{"effects", func(limits *Limits) { limits.MaxEffectsPerPhase = 0 }},
		{"effect payload", func(limits *Limits) { limits.MaxEffectPayloadBytes = 0 }},
		{"metadata", func(limits *Limits) { limits.MaxMetadataBytes = 0 }},
		{"replay", func(limits *Limits) { limits.MaxReplayInputs = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultLimits()
			test.zero(&limits)
			if limits.valid() {
				t.Fatal("limit set with a zero field is valid")
			}
			_, err := CompileWithLimits(definition, limits)
			assertDiagnostic(t, err, DiagnosticLimitExceeded)
		})
	}
}

func TestHistoryValidationDistinguishesIdentityAndLimitBoundaries(t *testing.T) {
	t.Parallel()

	valid := Snapshot[string]{
		InstanceID: "instance", State: "pending", DefinitionVersion: "v1",
	}
	for _, snapshot := range []Snapshot[string]{
		{State: "pending", DefinitionVersion: "v1"},
		{InstanceID: "instance", State: "pending"},
	} {
		_, err := ValidateHistoryWithLimit[string, string](snapshot, nil, 1)
		assertHistoryFailure(t, err, -1, HistoryMissingIdentity)
	}
	for _, limit := range []int{-1, 0} {
		_, err := ValidateHistoryWithLimit[string, string](valid, nil, limit)
		assertHistoryFailure(t, err, -1, HistoryLimitExceeded)
	}

	occurredAt := time.Unix(123, 0)
	entry := HistoryEntry[string, string]{
		InstanceID: "instance", Sequence: 1, OccurredAt: occurredAt,
		Result: Result[string, string]{
			DefinitionVersion: "v1", Previous: "pending", Next: "done",
			Event: "finish", TransitionID: "finish",
		},
	}
	final, err := ValidateHistoryWithLimit(valid, []HistoryEntry[string, string]{entry}, 1)
	if err != nil || final.State != "done" || final.LockVersion != 1 || !final.CreatedAt.Equal(occurredAt) {
		t.Fatalf("exact-limit history = %#v, %v", final, err)
	}
	_, err = ValidateHistoryWithLimit(valid, []HistoryEntry[string, string]{entry, entry}, 1)
	assertHistoryFailure(t, err, -1, HistoryLimitExceeded)

	machine, err := Compile(Definition[string, string, struct{}]{
		Version: "v1", Initial: "done",
		States: []StateDefinition[string]{{State: "done", Terminal: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	valid.DefinitionVersion = "v2"
	_, err = machine.ValidateHistory(valid, nil)
	assertHistoryFailure(t, err, -1, HistoryDefinitionMismatch)
}

func TestCompileAndExecutionLimitsAcceptExactBoundsAndRejectOverflow(t *testing.T) {
	t.Parallel()

	guard := func(context.Context, struct{}) *Rejection { return nil }
	definition := Definition[string, string, struct{}]{
		Version: "v1", Initial: "pending",
		States: []StateDefinition[string]{
			{State: "pending", Exit: []Effect{{Kind: "exit", Payload: []byte("1234")}}},
			{State: "done", Terminal: true, Entry: []Effect{{Kind: "entry", Payload: []byte("1234")}}},
		},
		Transitions: []TransitionDefinition[string, string, struct{}]{
			{ID: "finish", Sources: []string{"pending"}, Event: "finish", To: "done",
				Guards: []Guard[struct{}]{guard}, Effects: []Effect{{Kind: "effect", Payload: []byte("1234")}}},
		},
	}
	limits := Limits{
		MaxStates: 2, MaxTransitions: 1, MaxSourcesPerTransition: 1,
		MaxGuardsPerTransition: 1, MaxEffectsPerPhase: 1,
		MaxEffectPayloadBytes: 4, MaxMetadataBytes: 4, MaxReplayInputs: 1,
	}
	machine, err := CompileWithLimits(definition, limits)
	if err != nil {
		t.Fatalf("exact-bound compile: %v", err)
	}
	if _, err = machine.Transition(context.Background(), "pending", "finish", struct{}{}, Metadata{
		CorrelationID: "12", CausationID: "34",
	}); err != nil {
		t.Fatalf("exact metadata bound: %v", err)
	}
	if _, err = machine.Transition(context.Background(), "pending", "finish", struct{}{}, Metadata{
		CorrelationID: "123", CausationID: "45",
	}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("metadata overflow = %v, want ErrLimitExceeded", err)
	}
	if replay, replayErr := machine.Replay(context.Background(), []Input[string, struct{}]{{Event: "finish"}}); replayErr != nil || replay.Final != "done" {
		t.Fatalf("exact replay bound = %#v, %v", replay, replayErr)
	}
	if _, replayErr := machine.Replay(context.Background(), []Input[string, struct{}]{{Event: "finish"}, {Event: "finish"}}); !errors.Is(replayErr, ErrLimitExceeded) {
		t.Fatalf("replay overflow = %v, want ErrLimitExceeded", replayErr)
	}

	tests := []struct {
		name   string
		mutate func(*Definition[string, string, struct{}])
	}{
		{"sources", func(candidate *Definition[string, string, struct{}]) {
			candidate.Transitions[0].Sources = []string{"pending", "pending"}
		}},
		{"guards", func(candidate *Definition[string, string, struct{}]) {
			candidate.Transitions[0].CheckedGuards = []CheckedGuard[struct{}]{func(context.Context, struct{}) (*Rejection, error) { return nil, nil }}
		}},
		{"effect count", func(candidate *Definition[string, string, struct{}]) {
			candidate.Transitions[0].Effects = append(candidate.Transitions[0].Effects, Effect{Kind: "extra"})
		}},
		{"effect payload", func(candidate *Definition[string, string, struct{}]) {
			candidate.Transitions[0].Effects[0].Payload = []byte("12345")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := definition
			candidate.States = append([]StateDefinition[string](nil), definition.States...)
			candidate.Transitions = append([]TransitionDefinition[string, string, struct{}](nil), definition.Transitions...)
			test.mutate(&candidate)
			_, compileErr := CompileWithLimits(candidate, limits)
			assertDiagnostic(t, compileErr, DiagnosticLimitExceeded)
		})
	}
}

func TestCompileContinuesCollectingDiagnosticsAfterDuplicates(t *testing.T) {
	t.Parallel()

	t.Run("state", func(t *testing.T) {
		_, err := Compile(Definition[string, string, struct{}]{
			Version: "v1", Initial: "a",
			States: []StateDefinition[string]{{State: "a"}, {State: "a"}, {State: "b", Terminal: true}},
			Transitions: []TransitionDefinition[string, string, struct{}]{
				{ID: "invalid", Sources: []string{"b"}, Event: "stay", To: "b"},
			},
		})
		assertDiagnostic(t, err, DiagnosticDuplicateState)
		assertDiagnostic(t, err, DiagnosticTerminalTransition)
	})

	t.Run("wildcard transition", func(t *testing.T) {
		_, err := Compile(Definition[string, string, struct{}]{
			Version: "v1", Initial: "a",
			States: []StateDefinition[string]{{State: "a"}, {State: "b", Terminal: true}},
			Transitions: []TransitionDefinition[string, string, struct{}]{
				{ID: "first", Wildcard: true, Event: "go", To: "b"},
				{ID: "duplicate", Wildcard: true, Event: "go", To: "b"},
				{ID: "later", Sources: []string{"a"}, Event: "later", To: "missing"},
			},
		})
		assertDiagnostic(t, err, DiagnosticAmbiguousWildcard)
		assertDiagnostic(t, err, DiagnosticUnknownState)
	})

	t.Run("exact transition source", func(t *testing.T) {
		_, err := Compile(Definition[string, string, struct{}]{
			Version: "v1", Initial: "a",
			States: []StateDefinition[string]{{State: "a"}, {State: "b", Terminal: true}},
			Transitions: []TransitionDefinition[string, string, struct{}]{
				{ID: "first", Sources: []string{"a"}, Event: "go", To: "b"},
				{ID: "duplicate", Sources: []string{"a", "b"}, Event: "go", To: "b"},
			},
		})
		assertDiagnostic(t, err, DiagnosticAmbiguousTransition)
		assertDiagnostic(t, err, DiagnosticTerminalTransition)
	})
}

func TestReachabilityIncludesExactChainsAndNonTerminalWildcards(t *testing.T) {
	t.Parallel()

	_, err := Compile(Definition[string, string, struct{}]{
		Version: "v1", Initial: "a",
		States: []StateDefinition[string]{{State: "a"}, {State: "b"}, {State: "c", Terminal: true}},
		Transitions: []TransitionDefinition[string, string, struct{}]{
			{ID: "wildcard", Wildcard: true, Event: "go", To: "b"},
			{ID: "exact", Sources: []string{"b"}, Event: "finish", To: "c"},
		},
	})
	if err != nil {
		t.Fatalf("reachable graph: %v", err)
	}
}

func TestReachabilityContinuesAfterAnUnknownDestination(t *testing.T) {
	t.Parallel()

	_, err := Compile(Definition[string, string, struct{}]{
		Version: "v1", Initial: "a",
		States: []StateDefinition[string]{{State: "a"}, {State: "b", Terminal: true}},
		Transitions: []TransitionDefinition[string, string, struct{}]{
			{ID: "invalid", Sources: []string{"a"}, Event: "invalid", To: "missing"},
			{ID: "valid", Sources: []string{"a"}, Event: "valid", To: "b"},
		},
	})
	var diagnostics *DiagnosticsError
	if !errors.As(err, &diagnostics) || !diagnostics.Has(DiagnosticUnknownState) {
		t.Fatalf("error = %v, want unknown-state diagnostic", err)
	}
	if diagnostics.Has(DiagnosticUnreachableState) {
		t.Fatalf("valid destination was reported unreachable: %v", err)
	}
}

func TestReachabilityReportsEveryDisconnectedState(t *testing.T) {
	t.Parallel()

	_, err := Compile(Definition[string, string, struct{}]{
		Version: "v1", Initial: "a",
		States: []StateDefinition[string]{
			{State: "a", Terminal: true},
			{State: "b", Terminal: true},
			{State: "c", Terminal: true},
		},
	})
	var diagnostics *DiagnosticsError
	if !errors.As(err, &diagnostics) {
		t.Fatalf("error = %v, want diagnostics", err)
	}
	unreachable := 0
	for _, diagnostic := range diagnostics.Diagnostics {
		if diagnostic.Code == DiagnosticUnreachableState {
			unreachable++
		}
	}
	if unreachable != 2 {
		t.Fatalf("unreachable diagnostics = %d, want 2", unreachable)
	}
}

func assertDiagnostic(t *testing.T, err error, code DiagnosticCode) {
	t.Helper()
	var diagnostics *DiagnosticsError
	if !errors.As(err, &diagnostics) || !diagnostics.Has(code) {
		t.Fatalf("error = %v, want diagnostic %s", err, code)
	}
}

func assertHistoryFailure(t *testing.T, err error, index int, failure HistoryFailure) {
	t.Helper()
	var historyErr *HistoryError
	if !errors.As(err, &historyErr) || historyErr.Index != index || historyErr.Failure != failure {
		t.Fatalf("error = %#v, want history failure %s at %d", err, failure, index)
	}
}
