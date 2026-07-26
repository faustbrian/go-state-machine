// Package diagram exports compiled transition graphs for documentation.
package diagram

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
)

// Renderer converts typed states and events through explicit label functions.
type Renderer[S statemachine.State, E statemachine.Event, C any] struct {
	stateLabel func(S) string
	eventLabel func(E) string
	limits     Limits
}

// ErrMissingLabeler reports an absent state or event label function.
var ErrMissingLabeler = errors.New("diagram: state and event labelers are required")

var (
	// ErrLimitExceeded reports graph, label, or output data above configured bounds.
	ErrLimitExceeded = errors.New("diagram: limit exceeded")
	// ErrInvalidGraph reports an imported graph with inconsistent references.
	ErrInvalidGraph = errors.New("diagram: invalid graph")
)

// Limits bound imported graph structure, rendered labels, and output bytes.
type Limits struct {
	MaxStates               int
	MaxTransitions          int
	MaxSourcesPerTransition int
	MaxLabelBytes           int
	MaxOutputBytes          int
}

// DefaultLimits returns conservative documentation export bounds.
func DefaultLimits() Limits {
	return Limits{
		MaxStates: 10_000, MaxTransitions: 50_000,
		MaxSourcesPerTransition: 10_000, MaxLabelBytes: 4_096,
		MaxOutputBytes: 16 << 20,
	}
}

// New constructs a renderer without relying on implicit string formatting.
func New[S statemachine.State, E statemachine.Event, C any](stateLabel func(S) string, eventLabel func(E) string) (*Renderer[S, E, C], error) {
	return NewWithLimits[S, E, C](stateLabel, eventLabel, DefaultLimits())
}

// NewWithLimits constructs a renderer with explicit hostile-input bounds.
func NewWithLimits[S statemachine.State, E statemachine.Event, C any](stateLabel func(S) string, eventLabel func(E) string, limits Limits) (*Renderer[S, E, C], error) {
	if stateLabel == nil || eventLabel == nil {
		return nil, ErrMissingLabeler
	}
	if limits.MaxStates <= 0 || limits.MaxTransitions <= 0 ||
		limits.MaxSourcesPerTransition <= 0 || limits.MaxLabelBytes <= 0 ||
		limits.MaxOutputBytes <= 0 || limits.MaxStates > 1<<30 ||
		limits.MaxTransitions > 1<<30 || limits.MaxSourcesPerTransition > 1<<30 ||
		limits.MaxLabelBytes > 1<<30 || limits.MaxOutputBytes > 1<<30 {
		return nil, ErrLimitExceeded
	}
	return &Renderer[S, E, C]{stateLabel: stateLabel, eventLabel: eventLabel, limits: limits}, nil
}

// MermaidChecked validates an imported graph and bounds work before rendering.
func (renderer *Renderer[S, E, C]) MermaidChecked(graph statemachine.Graph[S, E, C]) (string, error) {
	checked, err := renderer.checkedRenderer(graph)
	if err != nil {
		return "", err
	}
	return checked.Mermaid(graph), nil
}

// GraphvizChecked validates an imported graph and bounds work before rendering.
func (renderer *Renderer[S, E, C]) GraphvizChecked(graph statemachine.Graph[S, E, C]) (string, error) {
	checked, err := renderer.checkedRenderer(graph)
	if err != nil {
		return "", err
	}
	return checked.Graphviz(graph), nil
}

// Mermaid exports a stateDiagram-v2 document in definition order.
func (renderer *Renderer[S, E, C]) Mermaid(graph statemachine.Graph[S, E, C]) string {
	var output strings.Builder
	output.WriteString("stateDiagram-v2\n")
	indexes := renderer.writeMermaidStates(&output, graph)
	fmt.Fprintf(&output, "  [*] --> s%d\n", indexes[graph.Initial])
	for _, transition := range graph.Transitions {
		label := mermaidText(renderer.eventLabel(transition.Event) + " [" + string(transition.ID) + "]")
		if transition.Wildcard {
			fmt.Fprintf(&output, "  wildcard --> s%d: %s\n", indexes[transition.To], label)
			continue
		}
		for _, source := range transition.Sources {
			fmt.Fprintf(&output, "  s%d --> s%d: %s\n", indexes[source], indexes[transition.To], label)
		}
	}
	for index, state := range graph.States {
		if state.Terminal {
			fmt.Fprintf(&output, "  s%d --> [*]\n", index)
		}
	}
	return output.String()
}

// Graphviz exports a directed DOT document in definition order.
func (renderer *Renderer[S, E, C]) Graphviz(graph statemachine.Graph[S, E, C]) string {
	var output strings.Builder
	output.WriteString("digraph state_machine {\n")
	output.WriteString("  rankdir=LR;\n")
	output.WriteString("  initial [shape=point];\n")
	indexes := make(map[S]int, len(graph.States))
	for index, state := range graph.States {
		indexes[state.State] = index
		shape := "circle"
		if state.Terminal {
			shape = "doublecircle"
		}
		fmt.Fprintf(&output, "  s%d [label=%s, shape=%s];\n", index, strconv.Quote(renderer.stateLabel(state.State)), shape)
	}
	fmt.Fprintf(&output, "  initial -> s%d;\n", indexes[graph.Initial])
	for _, transition := range graph.Transitions {
		label := strconv.Quote(renderer.eventLabel(transition.Event) + " [" + string(transition.ID) + "]")
		if transition.Wildcard {
			fmt.Fprintf(&output, "  wildcard -> s%d [label=%s, style=dashed];\n", indexes[transition.To], label)
			continue
		}
		for _, source := range transition.Sources {
			fmt.Fprintf(&output, "  s%d -> s%d [label=%s];\n", indexes[source], indexes[transition.To], label)
		}
	}
	output.WriteString("}\n")
	return output.String()
}

func (renderer *Renderer[S, E, C]) writeMermaidStates(output *strings.Builder, graph statemachine.Graph[S, E, C]) map[S]int {
	indexes := make(map[S]int, len(graph.States))
	for index, state := range graph.States {
		indexes[state.State] = index
		fmt.Fprintf(output, "  state %s as s%d\n", strconv.Quote(renderer.stateLabel(state.State)), index)
	}
	return indexes
}

func mermaidText(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, ":", "&#58;")
	return value
}

func (renderer *Renderer[S, E, C]) checkedRenderer(graph statemachine.Graph[S, E, C]) (*Renderer[S, E, C], error) {
	if len(graph.States) > renderer.limits.MaxStates || len(graph.Transitions) > renderer.limits.MaxTransitions {
		return nil, ErrLimitExceeded
	}
	stateLabels := make(map[S]string, len(graph.States))
	estimated := int64(128)
	maximum := int64(renderer.limits.MaxOutputBytes)
	for _, state := range graph.States {
		if _, exists := stateLabels[state.State]; exists {
			return nil, ErrInvalidGraph
		}
		label := renderer.stateLabel(state.State)
		increment := int64(len(label))*6 + 128
		if len(label) > renderer.limits.MaxLabelBytes || increment > maximum-estimated {
			return nil, ErrLimitExceeded
		}
		estimated += increment
		stateLabels[state.State] = label
	}
	if _, exists := stateLabels[graph.Initial]; !exists {
		return nil, ErrInvalidGraph
	}
	eventLabels := make(map[E]string)
	for _, transition := range graph.Transitions {
		if _, exists := stateLabels[transition.To]; !exists {
			return nil, ErrInvalidGraph
		}
		if len(transition.Sources) > renderer.limits.MaxSourcesPerTransition ||
			(transition.Wildcard && len(transition.Sources) != 0) ||
			(!transition.Wildcard && len(transition.Sources) == 0) {
			return nil, ErrInvalidGraph
		}
		for _, source := range transition.Sources {
			if _, exists := stateLabels[source]; !exists {
				return nil, ErrInvalidGraph
			}
		}
		label, exists := eventLabels[transition.Event]
		if !exists {
			label = renderer.eventLabel(transition.Event)
			eventLabels[transition.Event] = label
		}
		labelBytes := len(label) + len(transition.ID)
		if labelBytes > renderer.limits.MaxLabelBytes {
			return nil, ErrLimitExceeded
		}
		edges := len(transition.Sources)
		if transition.Wildcard {
			edges = 1
		}
		increment := int64(edges) * (int64(labelBytes)*6 + 128)
		if increment > maximum-estimated {
			return nil, ErrLimitExceeded
		}
		estimated += increment
	}
	return &Renderer[S, E, C]{
		stateLabel: func(state S) string { return stateLabels[state] },
		eventLabel: func(event E) string { return eventLabels[event] },
		limits:     renderer.limits,
	}, nil
}
