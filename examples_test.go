package statemachine_test

import (
	"context"
	"fmt"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
	"github.com/faustbrian/golib/pkg/state-machine/memory"
)

func Example() {
	type state string
	type event string
	const (
		pending state = "pending"
		paid    state = "paid"
		pay     event = "pay"
	)
	machine, err := statemachine.Compile(statemachine.Definition[state, event, int]{
		Version: "v1", Initial: pending,
		States: []statemachine.StateDefinition[state]{
			{State: pending}, {State: paid, Terminal: true},
		},
		Transitions: []statemachine.TransitionDefinition[state, event, int]{
			{ID: "pay", Sources: []state{pending}, Event: pay, To: paid,
				Effects: []statemachine.Effect{{Kind: "receipt"}}},
		},
	})
	if err != nil {
		panic(err)
	}
	result, err := machine.Transition(context.Background(), pending, pay, 42, statemachine.Metadata{})
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Previous, result.Next, result.Effects[0].Kind)
	// Output: pending paid receipt
}

func ExampleMachine_Replay() {
	machine, _ := statemachine.Compile(statemachine.Definition[bool, bool, struct{}]{
		Version: "v1", Initial: false,
		States: []statemachine.StateDefinition[bool]{{State: false}, {State: true}},
		Transitions: []statemachine.TransitionDefinition[bool, bool, struct{}]{
			{ID: "on", Sources: []bool{false}, Event: true, To: true},
			{ID: "off", Sources: []bool{true}, Event: false, To: false},
		},
	})
	replay, err := machine.Replay(context.Background(), []statemachine.Input[bool, struct{}]{
		{Event: true}, {Event: false},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(replay.Initial, replay.Final, len(replay.Transitions))
	// Output: false false 2
}

func ExampleStore() {
	store := memory.New[string, string]()
	err := store.Create(context.Background(), statemachine.Instance[string]{
		ID: "order-1", State: "pending", DefinitionVersion: "v1",
	})
	fmt.Println(err == nil, store.Capabilities().AtomicCompareAndTransition)
	// Output: true true
}
