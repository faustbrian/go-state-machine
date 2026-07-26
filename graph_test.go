package statemachine_test

import (
	"testing"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
)

func TestGraphPreservesDefinitionOrderAndWildcardEdges(t *testing.T) {
	t.Parallel()

	machine, err := statemachine.Compile(validDefinition())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	graph := machine.Graph()
	if graph.Version != "v1" || graph.Initial != orderPending {
		t.Fatalf("graph identity = %#v", graph)
	}
	if len(graph.States) != 2 || graph.States[0].State != orderPending || graph.States[1].State != orderPaid {
		t.Fatalf("states = %#v", graph.States)
	}
	if len(graph.Transitions) != 2 || graph.Transitions[0].ID != "pay" || !graph.Transitions[1].Wildcard {
		t.Fatalf("transitions = %#v", graph.Transitions)
	}

	graph.Transitions[0].Sources[0] = orderPaid
	second := machine.Graph()
	if second.Transitions[0].Sources[0] != orderPending {
		t.Fatal("graph result aliases compiled definition")
	}
}
