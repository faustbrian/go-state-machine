package statemachine

import (
	"context"
	"os"
	"testing"
)

func TestCompileAggregatePayloadAdmission(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Skip("default aggregate characterization runs only in hosted CI")
	}
	// Every occurrence requires its own immutable payload copy.
	payload := make([]byte, 1<<20)
	effects := make([]Effect, 17)
	for index := range effects {
		effects[index] = Effect{Kind: "notification", Payload: payload}
	}
	definition := Definition[string, string, struct{}]{
		Version: "v1", Initial: "ready",
		States: []StateDefinition[string]{{State: "ready", Entry: effects}},
	}
	machine, err := Compile(definition)
	if machine != nil {
		t.Fatal("definition beyond the default aggregate payload allowance returned a machine")
	}
	assertDiagnostic(t, err, DiagnosticLimitExceeded)
}

func TestCompileAggregateCountsCrossPhasePayloadOccurrences(t *testing.T) {
	payload := []byte{1}
	definition := compileAdmissionDefinition()
	definition.States[0].Entry = []Effect{{Kind: "entry", Payload: payload}}
	definition.States[0].Exit = []Effect{{Kind: "exit", Payload: payload}}
	definition.Transitions[0].Effects = []Effect{{Kind: "transition", Payload: payload}}
	for _, allowance := range []int{2, 3} {
		limits := compileAdmissionLimits()
		limits.MaxCompiledEffectPayloadBytes = allowance
		limits.MaxCompiledElements = 7
		machine, err := CompileWithLimits(definition, limits)
		if allowance == 2 {
			if machine != nil {
				t.Fatal("repeated payload occurrences bypassed aggregate admission")
			}
			assertDiagnostic(t, err, DiagnosticLimitExceeded)
			continue
		}
		if err != nil || machine == nil {
			t.Fatalf("inclusive cross-phase aggregate refused: %v", err)
		}
		payload[0] = 9
		graph := machine.Graph()
		if graph.States[0].Entry[0].Payload[0] != 1 || graph.States[0].Exit[0].Payload[0] != 1 ||
			graph.Transitions[0].Effects[0].Payload[0] != 1 {
			t.Fatal("aggregate admission lost immutable payload ownership")
		}
	}
}

func TestCompileAggregateCountsAllCollectionOccurrences(t *testing.T) {
	guard := func(context.Context, struct{}) *Rejection { return nil }
	checked := func(context.Context, struct{}) (*Rejection, error) { return nil, nil }
	for _, test := range []struct {
		name   string
		total  int
		change func(*Definition[string, string, struct{}])
	}{
		{"states transitions sources", 4, func(*Definition[string, string, struct{}]) {}},
		{"additional source", 5, func(d *Definition[string, string, struct{}]) {
			d.Transitions[0].Sources = []string{"ready", "done"}
		}},
		{"guards", 6, func(d *Definition[string, string, struct{}]) {
			d.Transitions[0].Guards = []Guard[struct{}]{guard, guard}
		}},
		{"checked guards", 6, func(d *Definition[string, string, struct{}]) {
			d.Transitions[0].CheckedGuards = []CheckedGuard[struct{}]{checked, checked}
		}},
		{"combined guards", 6, func(d *Definition[string, string, struct{}]) {
			d.Transitions[0].Guards = []Guard[struct{}]{guard}
			d.Transitions[0].CheckedGuards = []CheckedGuard[struct{}]{checked}
		}},
		{"entry effects", 6, func(d *Definition[string, string, struct{}]) {
			d.States[0].Entry = []Effect{{Kind: "one"}, {Kind: "two"}}
		}},
		{"exit effects", 6, func(d *Definition[string, string, struct{}]) {
			d.States[0].Exit = []Effect{{Kind: "one"}, {Kind: "two"}}
		}},
		{"transition effects", 6, func(d *Definition[string, string, struct{}]) {
			d.Transitions[0].Effects = []Effect{{Kind: "one"}, {Kind: "two"}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, allowance := range []int{test.total - 1, test.total} {
				definition := compileAdmissionDefinition()
				test.change(&definition)
				limits := compileAdmissionLimits()
				limits.MaxSourcesPerTransition = 2
				limits.MaxGuardsPerTransition = 2
				limits.MaxEffectsPerPhase = 2
				limits.MaxCompiledElements = allowance
				machine, err := CompileWithLimits(definition, limits)
				if allowance < test.total {
					if machine != nil {
						t.Fatal("collection occurrences bypassed aggregate admission")
					}
					assertDiagnostic(t, err, DiagnosticLimitExceeded)
				} else if err != nil || machine == nil {
					t.Fatalf("inclusive collection allowance refused: %v", err)
				}
			}
		})
	}
}

func TestCompileAggregateDefaultsAndInvalidLimits(t *testing.T) {
	limits := compileAdmissionLimits()
	limits.MaxCompiledEffectPayloadBytes = 0
	limits.MaxCompiledElements = 0
	if machine, err := CompileWithLimits(compileAdmissionDefinition(), limits); err != nil || machine == nil {
		t.Fatalf("omitted aggregate allowances refused: %v", err)
	}
	if limits.MaxCompiledEffectPayloadBytes != 0 || limits.MaxCompiledElements != 0 {
		t.Fatal("compile mutated caller limits")
	}
	for _, invalid := range []Limits{
		func() Limits { result := limits; result.MaxCompiledEffectPayloadBytes = -1; return result }(),
		func() Limits { result := limits; result.MaxCompiledElements = -1; return result }(),
	} {
		machine, err := CompileWithLimits(compileAdmissionDefinition(), invalid)
		if machine != nil {
			t.Fatal("negative aggregate allowance returned a machine")
		}
		assertDiagnostic(t, err, DiagnosticLimitExceeded)
	}
}
