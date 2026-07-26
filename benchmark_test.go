package statemachine_test

import (
	"context"
	"fmt"
	"testing"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
)

func BenchmarkCompilation(b *testing.B) {
	for _, stateCount := range []int{10, 1_000} {
		b.Run(fmt.Sprintf("states_%d", stateCount), func(b *testing.B) {
			definition := chainDefinition(stateCount)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := statemachine.Compile(definition); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkHotTransition(b *testing.B) {
	machine, err := statemachine.Compile(validDefinition())
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	for range b.N {
		if _, err := machine.Transition(ctx, orderPending, orderPay, 1, statemachine.Metadata{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGuardSets(b *testing.B) {
	definition := validDefinition()
	definition.Transitions[0].Guards = make([]statemachine.Guard[int], 50)
	for index := range definition.Transitions[0].Guards {
		definition.Transitions[0].Guards[index] = func(context.Context, int) *statemachine.Rejection { return nil }
	}
	machine, err := statemachine.Compile(definition)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	for range b.N {
		if _, err := machine.Transition(ctx, orderPending, orderPay, 1, statemachine.Metadata{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReplay(b *testing.B) {
	machine, err := statemachine.Compile(statemachine.Definition[bool, bool, struct{}]{
		Version: "v1", Initial: false,
		States: []statemachine.StateDefinition[bool]{{State: false}, {State: true}},
		Transitions: []statemachine.TransitionDefinition[bool, bool, struct{}]{
			{ID: "on", Sources: []bool{false}, Event: true, To: true},
			{ID: "off", Sources: []bool{true}, Event: false, To: false},
		},
	})
	if err != nil {
		b.Fatal(err)
	}
	inputs := make([]statemachine.Input[bool, struct{}], 100)
	for index := range inputs {
		inputs[index].Event = index%2 == 0
	}
	ctx := context.Background()
	b.ReportAllocs()
	for range b.N {
		if _, err := machine.Replay(ctx, inputs); err != nil {
			b.Fatal(err)
		}
	}
}

func chainDefinition(stateCount int) statemachine.Definition[int, int, struct{}] {
	states := make([]statemachine.StateDefinition[int], stateCount)
	transitions := make([]statemachine.TransitionDefinition[int, int, struct{}], 0, stateCount-1)
	for state := range stateCount {
		states[state] = statemachine.StateDefinition[int]{State: state, Terminal: state == stateCount-1}
		if state != stateCount-1 {
			transitions = append(transitions, statemachine.TransitionDefinition[int, int, struct{}]{
				ID:      statemachine.TransitionID(fmt.Sprintf("step-%d", state)),
				Sources: []int{state}, Event: state, To: state + 1,
			})
		}
	}
	return statemachine.Definition[int, int, struct{}]{
		Version: "v1", Initial: 0, States: states, Transitions: transitions,
	}
}
