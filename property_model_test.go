package statemachine_test

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"testing/quick"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
)

func TestPropertyTransitionIsDeterministicAndExactPrecedesWildcard(t *testing.T) {
	t.Parallel()

	machine, err := statemachine.Compile(statemachine.Definition[int, int, int]{
		Version: "v1", Initial: 0,
		States: []statemachine.StateDefinition[int]{{State: 0}, {State: 1, Terminal: true}, {State: 2, Terminal: true}},
		Transitions: []statemachine.TransitionDefinition[int, int, int]{
			{ID: "exact", Sources: []int{0}, Event: 1, To: 1},
			{ID: "wildcard", Wildcard: true, Event: 1, To: 2},
		},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	property := func(value int, correlation uint64, causation uint64) bool {
		metadata := statemachine.Metadata{
			CorrelationID: fmt.Sprintf("%x", correlation),
			CausationID:   fmt.Sprintf("%x", causation),
		}
		first, firstErr := machine.Transition(context.Background(), 0, 1, value, metadata)
		second, secondErr := machine.Transition(context.Background(), 0, 1, value, metadata)
		return firstErr == nil && secondErr == nil && first.TransitionID == "exact" &&
			reflect.DeepEqual(first, second)
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 1_000}); err != nil {
		t.Fatalf("determinism property: %v", err)
	}
}

func TestModelLiveExecutionMatchesReplayForGeneratedSequences(t *testing.T) {
	t.Parallel()

	machine, err := statemachine.Compile(statemachine.Definition[bool, bool, struct{}]{
		Version: "v1", Initial: false,
		States: []statemachine.StateDefinition[bool]{{State: false}, {State: true}},
		Transitions: []statemachine.TransitionDefinition[bool, bool, struct{}]{
			{ID: "enable", Sources: []bool{false}, Event: true, To: true},
			{ID: "disable", Sources: []bool{true}, Event: false, To: false},
			{ID: "stay-disabled", Sources: []bool{false}, Event: false, To: false},
			{ID: "stay-enabled", Sources: []bool{true}, Event: true, To: true},
		},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	property := func(events []bool) bool {
		if len(events) > 1_000 {
			events = events[:1_000]
		}
		inputs := make([]statemachine.Input[bool, struct{}], len(events))
		state := false
		live := make([]statemachine.Result[bool, bool], 0, len(events))
		for index, event := range events {
			inputs[index] = statemachine.Input[bool, struct{}]{Event: event}
			result, err := machine.Transition(context.Background(), state, event, struct{}{}, statemachine.Metadata{})
			if err != nil {
				return false
			}
			live = append(live, result)
			state = result.Next
		}
		replayed, err := machine.Replay(context.Background(), inputs)
		return err == nil && replayed.Final == state && reflect.DeepEqual(replayed.Transitions, live)
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("model property: %v", err)
	}
}

func TestCompiledMachineSupportsConcurrentTransitions(t *testing.T) {
	t.Parallel()

	machine, err := statemachine.Compile(validDefinition())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var wait sync.WaitGroup
	for index := range 100 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := machine.Transition(context.Background(), orderPending, orderPay, index, statemachine.Metadata{})
			if err != nil || result.TransitionID != "pay" {
				t.Errorf("transition = %#v, %v", result, err)
			}
		}()
	}
	wait.Wait()
}

func TestPropertyReplayFromEverySnapshotPositionMatchesLiveSuffix(t *testing.T) {
	t.Parallel()

	machine, err := statemachine.Compile(statemachine.Definition[bool, bool, struct{}]{
		Version: "stable-v1", Initial: false,
		States: []statemachine.StateDefinition[bool]{{State: false}, {State: true}},
		Transitions: []statemachine.TransitionDefinition[bool, bool, struct{}]{
			{ID: "enable", Sources: []bool{false}, Event: true, To: true},
			{ID: "disable", Sources: []bool{true}, Event: false, To: false},
			{ID: "stay-disabled", Sources: []bool{false}, Event: false, To: false},
			{ID: "stay-enabled", Sources: []bool{true}, Event: true, To: true},
		},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	property := func(events []bool) bool {
		if len(events) > 200 {
			events = events[:200]
		}
		inputs := make([]statemachine.Input[bool, struct{}], len(events))
		states := make([]bool, len(events)+1)
		results := make([]statemachine.Result[bool, bool], len(events))
		for index, event := range events {
			inputs[index] = statemachine.Input[bool, struct{}]{Event: event}
			result, err := machine.Transition(context.Background(), states[index], event, struct{}{}, statemachine.Metadata{})
			if err != nil {
				return false
			}
			results[index] = result
			states[index+1] = result.Next
		}
		for position := range states {
			replay, err := machine.ReplayFrom(context.Background(), states[position], inputs[position:])
			if err != nil || replay.Final != states[len(states)-1] ||
				!reflect.DeepEqual(replay.Transitions, results[position:]) {
				return false
			}
		}
		return true
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatalf("snapshot replay property: %v", err)
	}
}
