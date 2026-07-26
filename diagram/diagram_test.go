package diagram_test

import (
	"errors"
	"strings"
	"testing"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
	"github.com/faustbrian/golib/pkg/state-machine/diagram"
)

func TestExportsMermaidAndGraphvizDeterministically(t *testing.T) {
	t.Parallel()

	graph := statemachine.Graph[string, string, struct{}]{
		Version: "v1",
		Initial: "new order",
		States: []statemachine.StateDefinition[string]{
			{State: "new order"}, {State: "paid", Terminal: true},
		},
		Transitions: []statemachine.TransitionDefinition[string, string, struct{}]{
			{ID: "pay-order", Sources: []string{"new order"}, Event: "pay", To: "paid"},
		},
	}
	renderer, err := diagram.New[string, string, struct{}](func(value string) string { return value }, func(value string) string { return value })
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}

	mermaid := renderer.Mermaid(graph)
	if !strings.Contains(mermaid, "[*] --> s0") || !strings.Contains(mermaid, "s0 --> s1: pay [pay-order]") || !strings.Contains(mermaid, "s1 --> [*]") {
		t.Fatalf("mermaid output:\n%s", mermaid)
	}
	dot := renderer.Graphviz(graph)
	if !strings.Contains(dot, `initial -> s0;`) || !strings.Contains(dot, `s0 -> s1 [label="pay [pay-order]"];`) || !strings.Contains(dot, `s1 [label="paid", shape=doublecircle];`) {
		t.Fatalf("graphviz output:\n%s", dot)
	}
	if renderer.Mermaid(graph) != mermaid || renderer.Graphviz(graph) != dot {
		t.Fatal("diagram export is nondeterministic")
	}
}

func TestCheckedExportsRejectInvalidAndOversizedGraphs(t *testing.T) {
	t.Parallel()

	renderer, err := diagram.NewWithLimits[string, string, struct{}](
		func(value string) string { return value },
		func(value string) string { return value },
		diagram.Limits{
			MaxStates: 2, MaxTransitions: 2, MaxSourcesPerTransition: 1,
			MaxLabelBytes: 4, MaxOutputBytes: 1_024,
		},
	)
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	graph := statemachine.Graph[string, string, struct{}]{
		Version: "v1", Initial: "toolong",
		States: []statemachine.StateDefinition[string]{{State: "toolong"}},
	}
	if _, err := renderer.MermaidChecked(graph); !errors.Is(err, diagram.ErrLimitExceeded) {
		t.Fatalf("label error = %v, want ErrLimitExceeded", err)
	}
	graph.Initial = "a"
	graph.States = []statemachine.StateDefinition[string]{{State: "a"}}
	graph.Transitions = []statemachine.TransitionDefinition[string, string, struct{}]{
		{ID: "go", Sources: []string{"missing"}, Event: "go", To: "a"},
	}
	if _, err := renderer.GraphvizChecked(graph); !errors.Is(err, diagram.ErrInvalidGraph) {
		t.Fatalf("invalid graph error = %v, want ErrInvalidGraph", err)
	}
}
