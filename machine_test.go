package statemachine_test

import (
	"context"
	"errors"
	"testing"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
)

type orderState string

const (
	orderPending orderState = "pending"
	orderPaid    orderState = "paid"
)

type orderEvent string

const orderPay orderEvent = "pay"

func TestTransitionPrefersExactSourceAndPlansEffectsInOrder(t *testing.T) {
	t.Parallel()

	machine, err := statemachine.Compile(statemachine.Definition[orderState, orderEvent, int]{
		Version: "v1",
		Initial: orderPending,
		States: []statemachine.StateDefinition[orderState]{
			{State: orderPending, Exit: []statemachine.Effect{{Kind: "audit.exit"}}},
			{State: orderPaid, Entry: []statemachine.Effect{{Kind: "email.receipt"}}},
		},
		Transitions: []statemachine.TransitionDefinition[orderState, orderEvent, int]{
			{
				ID:      "pay-exact",
				Sources: []orderState{orderPending},
				Event:   orderPay,
				To:      orderPaid,
				Effects: []statemachine.Effect{{Kind: "capture.payment"}},
			},
			{
				ID:       "pay-fallback",
				Wildcard: true,
				Event:    orderPay,
				To:       orderPaid,
			},
		},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result, err := machine.Transition(context.Background(), orderPending, orderPay, 42, statemachine.Metadata{
		CorrelationID: "cor-1",
		CausationID:   "cause-1",
	})
	if err != nil {
		t.Fatalf("transition: %v", err)
	}

	if result.TransitionID != "pay-exact" {
		t.Fatalf("transition ID = %q, want pay-exact", result.TransitionID)
	}
	if result.DefinitionVersion != "v1" {
		t.Fatalf("definition version = %q, want v1", result.DefinitionVersion)
	}
	if result.Previous != orderPending || result.Next != orderPaid {
		t.Fatalf("states = %q -> %q, want pending -> paid", result.Previous, result.Next)
	}
	if result.Event != orderPay || result.Metadata.CorrelationID != "cor-1" {
		t.Fatalf("result did not preserve event metadata: %#v", result)
	}

	want := []string{"audit.exit", "capture.payment", "email.receipt"}
	if len(result.Effects) != len(want) {
		t.Fatalf("effects = %#v, want %v", result.Effects, want)
	}
	for index, kind := range want {
		if result.Effects[index].Kind != kind {
			t.Fatalf("effect %d = %q, want %q", index, result.Effects[index].Kind, kind)
		}
	}
}

func TestTerminalStateRejectsWildcardTransition(t *testing.T) {
	t.Parallel()

	machine, err := statemachine.Compile(validDefinition())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	_, err = machine.Transition(context.Background(), orderPaid, orderPay, 42, statemachine.Metadata{})
	if !errors.Is(err, statemachine.ErrTerminalState) {
		t.Fatalf("error = %v, want ErrTerminalState", err)
	}
}

func TestCompiledMachineDoesNotAliasDefinitionOrResults(t *testing.T) {
	t.Parallel()

	definition := validDefinition()
	definition.Transitions[0].Effects = []statemachine.Effect{{Kind: "capture", Payload: []byte("original")}}
	machine, err := statemachine.Compile(definition)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	definition.Transitions[0].Effects[0].Kind = "mutated"
	definition.Transitions[0].Effects[0].Payload[0] = 'X'

	first, err := machine.Transition(context.Background(), orderPending, orderPay, 42, statemachine.Metadata{})
	if err != nil {
		t.Fatalf("first transition: %v", err)
	}
	first.Effects[0].Kind = "mutated-result"
	first.Effects[0].Payload[0] = 'Y'

	second, err := machine.Transition(context.Background(), orderPending, orderPay, 42, statemachine.Metadata{})
	if err != nil {
		t.Fatalf("second transition: %v", err)
	}
	if second.Effects[0].Kind != "capture" || string(second.Effects[0].Payload) != "original" {
		t.Fatalf("compiled effect was mutated: %#v", second.Effects[0])
	}
}
