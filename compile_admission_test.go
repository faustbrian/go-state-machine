package statemachine

import (
	"context"
	"testing"
)

func TestCompileAdmissionCollectionLimits(t *testing.T) {
	guard := func(context.Context, struct{}) *Rejection { return nil }
	checked := func(context.Context, struct{}) (*Rejection, error) { return nil, nil }
	for _, test := range []struct {
		name   string
		change func(*Definition[string, string, struct{}])
	}{
		{"entry count", func(d *Definition[string, string, struct{}]) {
			d.States[0].Entry = []Effect{{Kind: "one"}, {Kind: "two"}}
		}},
		{"exit count", func(d *Definition[string, string, struct{}]) {
			d.States[0].Exit = []Effect{{Kind: "one"}, {Kind: "two"}}
		}},
		{"transition count", func(d *Definition[string, string, struct{}]) {
			d.Transitions[0].Effects = []Effect{{Kind: "one"}, {Kind: "two"}}
		}},
		{"entry bytes", func(d *Definition[string, string, struct{}]) {
			d.States[0].Entry = []Effect{{Kind: "one", Payload: []byte{1, 2}}}
		}},
		{"exit bytes", func(d *Definition[string, string, struct{}]) {
			d.States[0].Exit = []Effect{{Kind: "one", Payload: []byte{1, 2}}}
		}},
		{"transition bytes", func(d *Definition[string, string, struct{}]) {
			d.Transitions[0].Effects = []Effect{{Kind: "one", Payload: []byte{1, 2}}}
		}},
		{"sources", func(d *Definition[string, string, struct{}]) {
			d.Transitions[0].Sources = []string{"ready", "done"}
		}},
		{"guards", func(d *Definition[string, string, struct{}]) {
			d.Transitions[0].Guards = []Guard[struct{}]{guard, guard}
		}},
		{"checked guards", func(d *Definition[string, string, struct{}]) {
			d.Transitions[0].CheckedGuards = []CheckedGuard[struct{}]{checked, checked}
		}},
		{"combined guards", func(d *Definition[string, string, struct{}]) {
			d.Transitions[0].Guards = []Guard[struct{}]{guard}
			d.Transitions[0].CheckedGuards = []CheckedGuard[struct{}]{checked}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition := compileAdmissionDefinition()
			test.change(&definition)
			machine, err := CompileWithLimits(definition, compileAdmissionLimits())
			if machine != nil {
				t.Fatal("over-limit definition returned a machine")
			}
			assertDiagnostic(t, err, DiagnosticLimitExceeded)
		})
	}
}

func TestCompileAdmissionPreservesInclusiveCopies(t *testing.T) {
	definition := compileAdmissionDefinition()
	definition.States[0].Entry = []Effect{{Kind: "entry", Payload: []byte{1}}}
	definition.States[0].Exit = []Effect{{Kind: "exit", Payload: []byte{2}}}
	definition.Transitions[0].Effects = []Effect{{Kind: "transition", Payload: []byte{3}}}
	calls := 0
	definition.Transitions[0].Guards = []Guard[struct{}]{func(context.Context, struct{}) *Rejection {
		calls++
		return nil
	}}
	machine, err := CompileWithLimits(definition, compileAdmissionLimits())
	if err != nil || machine == nil {
		t.Fatalf("inclusive definition refused: %v", err)
	}
	if calls != 0 {
		t.Fatal("compilation invoked a guard")
	}
	definition.States[0].Entry[0].Payload[0] = 9
	definition.States[0].Exit[0].Payload[0] = 9
	definition.Transitions[0].Effects[0].Payload[0] = 9
	definition.Transitions[0].Sources[0] = "done"
	graph := machine.Graph()
	if graph.States[0].Entry[0].Payload[0] != 1 || graph.States[0].Exit[0].Payload[0] != 2 ||
		graph.Transitions[0].Effects[0].Payload[0] != 3 || graph.Transitions[0].Sources[0] != "ready" {
		t.Fatal("admitted graph retained mutable input storage")
	}
	graph.States[0].Entry[0].Payload[0] = 8
	if machine.Graph().States[0].Entry[0].Payload[0] != 1 {
		t.Fatal("graph export exposed compiled storage")
	}
}

func TestCompileAdmissionAcceptsCheckedGuardLimits(t *testing.T) {
	for _, combined := range []bool{false, true} {
		definition := compileAdmissionDefinition()
		limits := compileAdmissionLimits()
		calls := 0
		definition.Transitions[0].CheckedGuards = []CheckedGuard[struct{}]{func(context.Context, struct{}) (*Rejection, error) {
			calls++
			return nil, nil
		}}
		if combined {
			limits.MaxGuardsPerTransition = 2
			definition.Transitions[0].Guards = []Guard[struct{}]{func(context.Context, struct{}) *Rejection {
				calls++
				return nil
			}}
		}
		machine, err := CompileWithLimits(definition, limits)
		if err != nil || machine == nil || calls != 0 {
			t.Fatalf("inclusive guard admission: machine=%v error=%v calls=%d", machine != nil, err, calls)
		}
		result, err := machine.Transition(context.Background(), "ready", "go", struct{}{}, Metadata{})
		wantCalls := 1
		if combined {
			wantCalls = 2
		}
		if err != nil || result.Next != "done" || calls != wantCalls {
			t.Fatalf("admitted guard execution: next=%s error=%v calls=%d", result.Next, err, calls)
		}
	}
}

func compileAdmissionDefinition() Definition[string, string, struct{}] {
	return Definition[string, string, struct{}]{
		Version: "v1", Initial: "ready",
		States: []StateDefinition[string]{{State: "ready"}, {State: "done"}},
		Transitions: []TransitionDefinition[string, string, struct{}]{
			{ID: "notification", Sources: []string{"ready"}, Event: "go", To: "done"},
		},
	}
}

func compileAdmissionLimits() Limits {
	limits := DefaultLimits()
	limits.MaxSourcesPerTransition = 1
	limits.MaxGuardsPerTransition = 1
	limits.MaxEffectsPerPhase = 1
	limits.MaxEffectPayloadBytes = 1
	return limits
}
