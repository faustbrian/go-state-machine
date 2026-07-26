package statemachine_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
)

func TestReplayMatchesLiveExecution(t *testing.T) {
	t.Parallel()

	type state string
	const (
		pending state = "pending"
		paid    state = "paid"
		shipped state = "shipped"
	)
	type event string
	const (
		pay  event = "pay"
		ship event = "ship"
	)

	machine, err := statemachine.Compile(statemachine.Definition[state, event, int]{
		Version: "v1",
		Initial: pending,
		States: []statemachine.StateDefinition[state]{
			{State: pending}, {State: paid}, {State: shipped, Terminal: true},
		},
		Transitions: []statemachine.TransitionDefinition[state, event, int]{
			{ID: "pay", Sources: []state{pending}, Event: pay, To: paid},
			{ID: "ship", Sources: []state{paid}, Event: ship, To: shipped},
		},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	events := []statemachine.Input[event, int]{
		{Event: pay, Context: 1, Metadata: statemachine.Metadata{CorrelationID: "order-1"}},
		{Event: ship, Context: 2, Metadata: statemachine.Metadata{CorrelationID: "order-1", CausationID: "pay-1"}},
	}

	replay, err := machine.Replay(context.Background(), events)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	first, _ := machine.Transition(context.Background(), pending, pay, 1, events[0].Metadata)
	second, _ := machine.Transition(context.Background(), paid, ship, 2, events[1].Metadata)
	if replay.Final != shipped || !reflect.DeepEqual(replay.Transitions, []statemachine.Result[state, event]{first, second}) {
		t.Fatalf("replay = %#v, want live results ending shipped", replay)
	}
}

func TestReplayFromValidatesEmptyReplayBoundary(t *testing.T) {
	t.Parallel()

	machine, err := statemachine.Compile(statemachine.Definition[string, string, struct{}]{
		Version: "v1", Initial: "pending",
		States: []statemachine.StateDefinition[string]{
			{State: "pending"},
			{State: "complete", Terminal: true},
		},
		Transitions: []statemachine.TransitionDefinition[string, string, struct{}]{
			{ID: "complete", Sources: []string{"pending"}, Event: "complete", To: "complete"},
		},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if _, err := machine.ReplayFrom(context.Background(), "corrupt", nil); !errors.Is(err, statemachine.ErrUnknownState) {
		t.Fatalf("unknown snapshot error = %v, want ErrUnknownState", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := machine.ReplayFrom(ctx, "pending", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled empty replay error = %v, want context.Canceled", err)
	}

	replay, err := machine.ReplayFrom(context.Background(), "complete", nil)
	if err != nil || replay.Initial != "complete" || replay.Final != "complete" {
		t.Fatalf("terminal snapshot replay = %#v, %v", replay, err)
	}
}
