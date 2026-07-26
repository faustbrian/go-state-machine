package statemachine_test

import (
	"context"
	"encoding/json"
	"testing"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
	"github.com/faustbrian/golib/pkg/state-machine/diagram"
)

func FuzzSerializedEvent(f *testing.F) {
	f.Add([]byte(`{"Event":"pay","Context":1,"Metadata":{"CorrelationID":"order-1"}}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var input statemachine.Input[string, int]
		if json.Unmarshal(data, &input) != nil {
			return
		}
		encoded, err := json.Marshal(input)
		if err != nil {
			t.Fatalf("marshal accepted event: %v", err)
		}
		var roundTrip statemachine.Input[string, int]
		if err := json.Unmarshal(encoded, &roundTrip); err != nil {
			t.Fatalf("round-trip event: %v", err)
		}
	})
}

func FuzzSerializedDefinition(f *testing.F) {
	f.Add([]byte(`{"Version":"v1","Initial":"a","States":[{"State":"a"}]}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var definition statemachine.Definition[string, string, struct{}]
		if json.Unmarshal(data, &definition) != nil {
			return
		}
		limits := statemachine.DefaultLimits()
		limits.MaxStates = 100
		limits.MaxTransitions = 100
		_, _ = statemachine.CompileWithLimits(definition, limits)
	})
}

func FuzzSerializedContext(f *testing.F) {
	type transitionContext struct {
		Roles []string `json:"roles"`
	}
	f.Add([]byte(`{"roles":["operator"]}`))
	f.Add([]byte(`null`))
	machine, err := statemachine.Compile(statemachine.Definition[string, string, *transitionContext]{
		Version: "v1", Initial: "pending",
		CloneContext: func(value *transitionContext) *transitionContext {
			if value == nil {
				return nil
			}
			return &transitionContext{Roles: append([]string(nil), value.Roles...)}
		},
		States: []statemachine.StateDefinition[string]{{State: "pending"}, {State: "complete", Terminal: true}},
		Transitions: []statemachine.TransitionDefinition[string, string, *transitionContext]{
			{
				ID: "complete", Sources: []string{"pending"}, Event: "complete", To: "complete",
				Guards: []statemachine.Guard[*transitionContext]{func(_ context.Context, value *transitionContext) *statemachine.Rejection {
					if value == nil || len(value.Roles) == 0 {
						return &statemachine.Rejection{Code: "missing_role"}
					}
					return nil
				}},
			},
		},
	})
	if err != nil {
		f.Fatalf("compile: %v", err)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var value *transitionContext
		if json.Unmarshal(data, &value) != nil || value != nil && len(value.Roles) > 100 {
			return
		}
		_, _ = machine.Transition(context.Background(), "pending", "complete", value, statemachine.Metadata{})
	})
}

func FuzzSerializedHistory(f *testing.F) {
	f.Add([]byte(`[{"InstanceID":"one","Sequence":1,"Result":{"DefinitionVersion":"v1","Previous":"a","Next":"b","TransitionID":"go"}}]`))
	f.Add([]byte(`[]`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var history []statemachine.HistoryEntry[string, string]
		if json.Unmarshal(data, &history) != nil {
			return
		}
		_, _ = statemachine.ValidateHistoryWithLimit(statemachine.Snapshot[string]{
			InstanceID: "one", State: "a", DefinitionVersion: "v1",
		}, history, 100)
	})
}

func FuzzSerializedSnapshot(f *testing.F) {
	f.Add([]byte(`{"InstanceID":"one","State":"a","DefinitionVersion":"v1","LockVersion":0}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var snapshot statemachine.Snapshot[string]
		if json.Unmarshal(data, &snapshot) != nil {
			return
		}
		_, _ = statemachine.ValidateHistoryWithLimit(
			snapshot, []statemachine.HistoryEntry[string, string](nil), 100,
		)
	})
}

func FuzzSerializedGraphImport(f *testing.F) {
	f.Add([]byte(`{"Version":"v1","Initial":"a","States":[{"State":"a","Terminal":true}]}`))
	f.Add([]byte(`null`))
	renderer, err := diagram.New[string, string, struct{}](boundedLabel, boundedLabel)
	if err != nil {
		f.Fatalf("new renderer: %v", err)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var graph statemachine.Graph[string, string, struct{}]
		if json.Unmarshal(data, &graph) != nil || len(graph.States) > 100 || len(graph.Transitions) > 100 {
			return
		}
		_, _ = renderer.MermaidChecked(graph)
		_, _ = renderer.GraphvizChecked(graph)
	})
}

func boundedLabel(value string) string {
	if len(value) > 1_000 {
		return value[:1_000]
	}
	return value
}
