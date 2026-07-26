package diagram

import (
	"errors"
	"strings"
	"testing"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
)

func TestRendererValidationWildcardAndEscaping(t *testing.T) {
	t.Parallel()

	if _, err := New[string, string, struct{}](nil, func(value string) string { return value }); !errors.Is(err, ErrMissingLabeler) {
		t.Fatalf("missing state labeler error = %v", err)
	}
	if _, err := New[string, string, struct{}](func(value string) string { return value }, nil); !errors.Is(err, ErrMissingLabeler) {
		t.Fatalf("missing event labeler error = %v", err)
	}
	renderer, _ := New[string, string, struct{}](func(value string) string { return value }, func(value string) string { return value })
	graph := statemachine.Graph[string, string, struct{}]{
		Version: "v1", Initial: "a\"b",
		States: []statemachine.StateDefinition[string]{{State: "a\"b"}, {State: "done", Terminal: true}},
		Transitions: []statemachine.TransitionDefinition[string, string, struct{}]{
			{ID: "fallback", Wildcard: true, Event: "go:\nnow", To: "done"},
		},
	}
	mermaid := renderer.Mermaid(graph)
	if !strings.Contains(mermaid, "wildcard --> s1") || strings.Contains(mermaid, "go:\n") || !strings.Contains(mermaid, "&#58;") {
		t.Fatalf("mermaid = %s", mermaid)
	}
	dot := renderer.Graphviz(graph)
	if !strings.Contains(dot, "style=dashed") || !strings.Contains(dot, `a\"b`) {
		t.Fatalf("graphviz = %s", dot)
	}
}

func TestCheckedRendererRemainingValidationPaths(t *testing.T) {
	t.Parallel()

	label := func(value string) string { return value }
	if _, err := NewWithLimits[string, string, struct{}](label, label, Limits{}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("invalid limits error = %v", err)
	}
	renderer, _ := NewWithLimits[string, string, struct{}](label, label, Limits{
		MaxStates: 2, MaxTransitions: 1, MaxSourcesPerTransition: 1,
		MaxLabelBytes: 16, MaxOutputBytes: 2_048,
	})
	valid := statemachine.Graph[string, string, struct{}]{
		Version: "v1", Initial: "a",
		States: []statemachine.StateDefinition[string]{{State: "a"}, {State: "b", Terminal: true}},
		Transitions: []statemachine.TransitionDefinition[string, string, struct{}]{
			{ID: "go", Sources: []string{"a"}, Event: "go", To: "b"},
		},
	}
	if output, err := renderer.MermaidChecked(valid); err != nil || output == "" {
		t.Fatalf("checked Mermaid = %q, %v", output, err)
	}
	if output, err := renderer.GraphvizChecked(valid); err != nil || output == "" {
		t.Fatalf("checked Graphviz = %q, %v", output, err)
	}
	tests := []statemachine.Graph[string, string, struct{}]{
		{Initial: "a", States: append(append([]statemachine.StateDefinition[string](nil), valid.States...), statemachine.StateDefinition[string]{State: "c"})},
		{Initial: "a", States: valid.States, Transitions: append(append([]statemachine.TransitionDefinition[string, string, struct{}](nil), valid.Transitions...), valid.Transitions[0])},
		{Initial: "a", States: []statemachine.StateDefinition[string]{{State: "a"}, {State: "a"}}},
		{Initial: "missing", States: valid.States},
		{Initial: "a", States: valid.States, Transitions: []statemachine.TransitionDefinition[string, string, struct{}]{{ID: "go", Sources: []string{"a"}, Event: "go", To: "missing"}}},
		{Initial: "a", States: valid.States, Transitions: []statemachine.TransitionDefinition[string, string, struct{}]{{ID: "go", Wildcard: true, Sources: []string{"a"}, Event: "go", To: "b"}}},
		{Initial: "a", States: valid.States, Transitions: []statemachine.TransitionDefinition[string, string, struct{}]{{ID: "go", Event: "go", To: "b"}}},
	}
	for index, graph := range tests {
		if _, err := renderer.MermaidChecked(graph); err == nil {
			t.Fatalf("invalid graph %d accepted", index)
		}
	}
	longTransitionLabel := valid
	longTransitionLabel.Transitions = append([]statemachine.TransitionDefinition[string, string, struct{}](nil), valid.Transitions...)
	longTransitionLabel.Transitions[0].Event = "a-label-that-is-too-long"
	if _, err := renderer.MermaidChecked(longTransitionLabel); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("transition label limit error = %v", err)
	}
	wildcard := valid
	wildcard.Transitions = []statemachine.TransitionDefinition[string, string, struct{}]{
		{ID: "go", Wildcard: true, Event: "go", To: "b"},
	}
	if _, err := renderer.MermaidChecked(wildcard); err != nil {
		t.Fatalf("valid wildcard error = %v", err)
	}
	tiny, _ := NewWithLimits[string, string, struct{}](label, label, Limits{
		MaxStates: 2, MaxTransitions: 1, MaxSourcesPerTransition: 1,
		MaxLabelBytes: 16, MaxOutputBytes: 200,
	})
	if _, err := tiny.MermaidChecked(valid); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("output limit error = %v", err)
	}
	edgeTiny, _ := NewWithLimits[string, string, struct{}](label, label, Limits{
		MaxStates: 2, MaxTransitions: 1, MaxSourcesPerTransition: 1,
		MaxLabelBytes: 16, MaxOutputBytes: 500,
	})
	if _, err := edgeTiny.MermaidChecked(valid); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("edge output limit error = %v", err)
	}
}
