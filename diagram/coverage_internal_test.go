package diagram

import (
	"errors"
	"strings"
	"testing"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
)

func TestNewWithLimitsRejectsEachInvalidBoundary(t *testing.T) {
	t.Parallel()

	label := func(value string) string { return value }
	valid := Limits{
		MaxStates: 1, MaxTransitions: 1, MaxSourcesPerTransition: 1,
		MaxLabelBytes: 1, MaxOutputBytes: 1,
	}
	tests := []struct {
		name   string
		mutate func(*Limits)
	}{
		{name: "zero states", mutate: func(limits *Limits) { limits.MaxStates = 0 }},
		{name: "zero transitions", mutate: func(limits *Limits) { limits.MaxTransitions = 0 }},
		{name: "zero sources", mutate: func(limits *Limits) { limits.MaxSourcesPerTransition = 0 }},
		{name: "zero label bytes", mutate: func(limits *Limits) { limits.MaxLabelBytes = 0 }},
		{name: "zero output bytes", mutate: func(limits *Limits) { limits.MaxOutputBytes = 0 }},
		{name: "states above maximum", mutate: func(limits *Limits) { limits.MaxStates = 1<<30 + 1 }},
		{name: "transitions above maximum", mutate: func(limits *Limits) { limits.MaxTransitions = 1<<30 + 1 }},
		{name: "sources above maximum", mutate: func(limits *Limits) { limits.MaxSourcesPerTransition = 1<<30 + 1 }},
		{name: "label bytes above maximum", mutate: func(limits *Limits) { limits.MaxLabelBytes = 1<<30 + 1 }},
		{name: "output bytes above maximum", mutate: func(limits *Limits) { limits.MaxOutputBytes = 1<<30 + 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := valid
			test.mutate(&limits)
			if _, err := NewWithLimits[string, string, struct{}](label, label, limits); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("NewWithLimits() error = %v, want ErrLimitExceeded", err)
			}
		})
	}

	maximum := Limits{
		MaxStates: 1 << 30, MaxTransitions: 1 << 30,
		MaxSourcesPerTransition: 1 << 30, MaxLabelBytes: 1 << 30,
		MaxOutputBytes: 1 << 30,
	}
	if _, err := NewWithLimits[string, string, struct{}](label, label, maximum); err != nil {
		t.Fatalf("NewWithLimits() at inclusive maximum: %v", err)
	}
}

func TestRenderersPreserveEveryStateSourceAndTransition(t *testing.T) {
	t.Parallel()

	renderer, err := New[string, string, struct{}](func(value string) string { return value }, func(value string) string { return value })
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	graph := statemachine.Graph[string, string, struct{}]{
		Initial: "a",
		States: []statemachine.StateDefinition[string]{
			{State: "a"}, {State: "b"}, {State: "c", Terminal: true},
		},
		Transitions: []statemachine.TransitionDefinition[string, string, struct{}]{
			{ID: "reset", Wildcard: true, Event: "reset", To: "a"},
			{ID: "advance", Sources: []string{"a", "b"}, Event: "go", To: "c"},
		},
	}
	wantMermaid := "stateDiagram-v2\n" +
		"  state \"a\" as s0\n" +
		"  state \"b\" as s1\n" +
		"  state \"c\" as s2\n" +
		"  [*] --> s0\n" +
		"  wildcard --> s0: reset [reset]\n" +
		"  s0 --> s2: go [advance]\n" +
		"  s1 --> s2: go [advance]\n" +
		"  s2 --> [*]\n"
	if got := renderer.Mermaid(graph); got != wantMermaid {
		t.Fatalf("Mermaid() =\n%s\nwant:\n%s", got, wantMermaid)
	}
	wantGraphviz := "digraph state_machine {\n" +
		"  rankdir=LR;\n" +
		"  initial [shape=point];\n" +
		"  s0 [label=\"a\", shape=circle];\n" +
		"  s1 [label=\"b\", shape=circle];\n" +
		"  s2 [label=\"c\", shape=doublecircle];\n" +
		"  initial -> s0;\n" +
		"  wildcard -> s0 [label=\"reset [reset]\", style=dashed];\n" +
		"  s0 -> s2 [label=\"go [advance]\"];\n" +
		"  s1 -> s2 [label=\"go [advance]\"];\n" +
		"}\n"
	if got := renderer.Graphviz(graph); got != wantGraphviz {
		t.Fatalf("Graphviz() =\n%s\nwant:\n%s", got, wantGraphviz)
	}
}

func TestCheckedRendererOutputEstimatesUseInclusiveExactBounds(t *testing.T) {
	t.Parallel()

	label := func(value string) string { return value }
	tests := []struct {
		name       string
		graph      statemachine.Graph[string, string, struct{}]
		labelBytes int
		exactBytes int
	}{
		{
			name: "state", labelBytes: 1, exactBytes: 262,
			graph: statemachine.Graph[string, string, struct{}]{
				Initial: "a", States: []statemachine.StateDefinition[string]{{State: "a"}},
			},
		},
		{
			name: "transition label", labelBytes: 2, exactBytes: 536,
			graph: statemachine.Graph[string, string, struct{}]{
				Initial: "a",
				States:  []statemachine.StateDefinition[string]{{State: "a"}, {State: "b"}},
				Transitions: []statemachine.TransitionDefinition[string, string, struct{}]{
					{ID: "i", Sources: []string{"a"}, Event: "g", To: "b"},
				},
			},
		},
		{
			name: "cumulative transitions", labelBytes: 2, exactBytes: 676,
			graph: statemachine.Graph[string, string, struct{}]{
				Initial: "a",
				States:  []statemachine.StateDefinition[string]{{State: "a"}, {State: "b"}},
				Transitions: []statemachine.TransitionDefinition[string, string, struct{}]{
					{ID: "i", Sources: []string{"a"}, Event: "g", To: "b"},
					{ID: "j", Sources: []string{"b"}, Event: "h", To: "a"},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := Limits{
				MaxStates: 2, MaxTransitions: 2, MaxSourcesPerTransition: 1,
				MaxLabelBytes: test.labelBytes, MaxOutputBytes: test.exactBytes,
			}
			renderer, err := NewWithLimits[string, string, struct{}](label, label, limits)
			if err != nil {
				t.Fatalf("NewWithLimits() error: %v", err)
			}
			if _, err := renderer.MermaidChecked(test.graph); err != nil {
				t.Fatalf("MermaidChecked() at exact bound: %v", err)
			}
			limits.MaxOutputBytes--
			renderer, err = NewWithLimits[string, string, struct{}](label, label, limits)
			if err != nil {
				t.Fatalf("NewWithLimits() below output estimate: %v", err)
			}
			if _, err := renderer.MermaidChecked(test.graph); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("MermaidChecked() below exact bound error = %v, want ErrLimitExceeded", err)
			}
		})
	}

	graph := statemachine.Graph[string, string, struct{}]{
		Initial: "a",
		States:  []statemachine.StateDefinition[string]{{State: "a"}, {State: "b"}},
		Transitions: []statemachine.TransitionDefinition[string, string, struct{}]{
			{ID: "i", Sources: []string{"a"}, Event: "g", To: "b"},
		},
	}
	for _, maxLabelBytes := range []int{1, 2} {
		renderer, err := NewWithLimits[string, string, struct{}](label, label, Limits{
			MaxStates: 2, MaxTransitions: 1, MaxSourcesPerTransition: 1,
			MaxLabelBytes: maxLabelBytes, MaxOutputBytes: 1_024,
		})
		if err != nil {
			t.Fatalf("NewWithLimits(%d) error: %v", maxLabelBytes, err)
		}
		_, err = renderer.MermaidChecked(graph)
		if maxLabelBytes == 1 && !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("MermaidChecked() label limit 1 error = %v, want ErrLimitExceeded", err)
		}
		if maxLabelBytes == 2 && err != nil {
			t.Fatalf("MermaidChecked() at exact label bound: %v", err)
		}
	}
}

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
