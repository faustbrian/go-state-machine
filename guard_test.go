package statemachine_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
)

func TestTransitionReturnsStructuredGuardRejection(t *testing.T) {
	t.Parallel()

	definition := validDefinition()
	definition.Transitions[0].Guards = []statemachine.Guard[int]{
		func(context.Context, int) *statemachine.Rejection {
			return &statemachine.Rejection{Code: "insufficient_funds", Message: "balance is too low"}
		},
	}
	machine, err := statemachine.Compile(definition)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	_, err = machine.Transition(context.Background(), orderPending, orderPay, 42, statemachine.Metadata{})
	var rejected *statemachine.GuardRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("error = %T, want *GuardRejectedError", err)
	}
	if rejected.TransitionID != "pay" || rejected.Rejection.Code != "insufficient_funds" {
		t.Fatalf("rejection = %#v", rejected)
	}
}

func TestTransitionContainsGuardPanic(t *testing.T) {
	t.Parallel()

	definition := validDefinition()
	definition.Transitions[0].Guards = []statemachine.Guard[int]{
		func(context.Context, int) *statemachine.Rejection {
			panic("secret event payload")
		},
	}
	machine, err := statemachine.Compile(definition)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	_, err = machine.Transition(context.Background(), orderPending, orderPay, 42, statemachine.Metadata{})
	var guardPanic *statemachine.GuardPanicError
	if !errors.As(err, &guardPanic) {
		t.Fatalf("error = %T, want *GuardPanicError", err)
	}
	if guardPanic.TransitionID != "pay" {
		t.Fatalf("transition ID = %q, want pay", guardPanic.TransitionID)
	}
	if guardPanic.Error() != "statemachine: guard panicked in transition pay" {
		t.Fatalf("error disclosed panic value: %q", guardPanic.Error())
	}
}

func TestTransitionHonorsCancellationBeforeGuards(t *testing.T) {
	t.Parallel()

	called := false
	definition := validDefinition()
	definition.Transitions[0].Guards = []statemachine.Guard[int]{
		func(context.Context, int) *statemachine.Rejection {
			called = true
			return nil
		},
	}
	machine, err := statemachine.Compile(definition)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = machine.Transition(ctx, orderPending, orderPay, 42, statemachine.Metadata{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if called {
		t.Fatal("guard called after cancellation")
	}
}

func TestTransitionContainsGuardErrorsWithoutRenderingSensitiveData(t *testing.T) {
	t.Parallel()

	sensitive := errors.New("customer-token-123")
	machine, err := statemachine.Compile(statemachine.Definition[string, string, struct{}]{
		Version: "v1", Initial: "pending",
		States: []statemachine.StateDefinition[string]{
			{State: "pending"}, {State: "paid", Terminal: true},
		},
		Transitions: []statemachine.TransitionDefinition[string, string, struct{}]{
			{
				ID: "pay", Sources: []string{"pending"}, Event: "pay", To: "paid",
				CheckedGuards: []statemachine.CheckedGuard[struct{}]{
					func(context.Context, struct{}) (*statemachine.Rejection, error) {
						return nil, sensitive
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = machine.Transition(context.Background(), "pending", "pay", struct{}{}, statemachine.Metadata{})
	if !errors.Is(err, sensitive) || !errors.Is(err, statemachine.ErrGuardFailed) {
		t.Fatalf("guard error = %v, want wrapped failure", err)
	}
	if strings.Contains(err.Error(), sensitive.Error()) {
		t.Fatalf("guard error exposed sensitive cause: %v", err)
	}
}

func TestTransitionIsolatesMutableContextBetweenGuards(t *testing.T) {
	t.Parallel()

	type guardContext struct{ Values []string }
	input := &guardContext{Values: []string{"original"}}
	machine, err := statemachine.Compile(statemachine.Definition[string, string, *guardContext]{
		Version: "v1", Initial: "pending",
		CloneContext: func(value *guardContext) *guardContext {
			return &guardContext{Values: append([]string(nil), value.Values...)}
		},
		States: []statemachine.StateDefinition[string]{
			{State: "pending"}, {State: "paid", Terminal: true},
		},
		Transitions: []statemachine.TransitionDefinition[string, string, *guardContext]{
			{
				ID: "pay", Sources: []string{"pending"}, Event: "pay", To: "paid",
				Guards: []statemachine.Guard[*guardContext]{
					func(_ context.Context, value *guardContext) *statemachine.Rejection {
						value.Values[0] = "mutated"
						return nil
					},
					func(_ context.Context, value *guardContext) *statemachine.Rejection {
						if value.Values[0] != "original" {
							return &statemachine.Rejection{Code: "context_leaked"}
						}
						return nil
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := machine.Transition(context.Background(), "pending", "pay", input, statemachine.Metadata{}); err != nil {
		t.Fatalf("transition: %v", err)
	}
	if input.Values[0] != "original" {
		t.Fatalf("caller context mutated: %#v", input)
	}
}

func TestTransitionContainsContextClonePanic(t *testing.T) {
	t.Parallel()

	machine, err := statemachine.Compile(statemachine.Definition[string, string, *string]{
		Version: "v1", Initial: "pending",
		CloneContext: func(*string) *string {
			panic("customer-token-123")
		},
		States: []statemachine.StateDefinition[string]{
			{State: "pending"}, {State: "paid", Terminal: true},
		},
		Transitions: []statemachine.TransitionDefinition[string, string, *string]{
			{
				ID: "pay", Sources: []string{"pending"}, Event: "pay", To: "paid",
				Guards: []statemachine.Guard[*string]{func(context.Context, *string) *statemachine.Rejection {
					return nil
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	value := "input"
	_, err = machine.Transition(context.Background(), "pending", "pay", &value, statemachine.Metadata{})
	if !errors.Is(err, statemachine.ErrContextClonePanic) {
		t.Fatalf("clone panic error = %v, want ErrContextClonePanic", err)
	}
	if strings.Contains(err.Error(), "customer-token-123") {
		t.Fatalf("clone panic exposed value: %v", err)
	}

	checked, err := statemachine.Compile(statemachine.Definition[string, string, *string]{
		Version: "v1", Initial: "pending", CloneContext: func(*string) *string { panic("sensitive") },
		States: []statemachine.StateDefinition[string]{{State: "pending"}, {State: "paid", Terminal: true}},
		Transitions: []statemachine.TransitionDefinition[string, string, *string]{
			{
				ID: "pay", Sources: []string{"pending"}, Event: "pay", To: "paid",
				CheckedGuards: []statemachine.CheckedGuard[*string]{func(context.Context, *string) (*statemachine.Rejection, error) {
					return nil, nil
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := checked.Transition(context.Background(), "pending", "pay", &value, statemachine.Metadata{}); !errors.Is(err, statemachine.ErrContextClonePanic) {
		t.Fatalf("checked clone panic error = %v", err)
	}
}

func TestCheckedGuardsRejectContainPanicsAndHonorCancellation(t *testing.T) {
	t.Parallel()

	definition := func(guards ...statemachine.CheckedGuard[struct{}]) statemachine.Definition[string, string, struct{}] {
		return statemachine.Definition[string, string, struct{}]{
			Version: "v1", Initial: "pending",
			States: []statemachine.StateDefinition[string]{
				{State: "pending"}, {State: "paid", Terminal: true},
			},
			Transitions: []statemachine.TransitionDefinition[string, string, struct{}]{
				{ID: "pay", Sources: []string{"pending"}, Event: "pay", To: "paid", CheckedGuards: guards},
			},
		}
	}
	rejecting, err := statemachine.Compile(definition(func(context.Context, struct{}) (*statemachine.Rejection, error) {
		return &statemachine.Rejection{Code: "blocked"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rejecting.Transition(context.Background(), "pending", "pay", struct{}{}, statemachine.Metadata{}); !errors.Is(err, statemachine.ErrGuardRejected) {
		t.Fatalf("checked rejection error = %v", err)
	}

	panicking, err := statemachine.Compile(definition(func(context.Context, struct{}) (*statemachine.Rejection, error) {
		panic("sensitive")
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := panicking.Transition(context.Background(), "pending", "pay", struct{}{}, statemachine.Metadata{}); !errors.Is(err, statemachine.ErrGuardPanic) {
		t.Fatalf("checked panic error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	secondCalled := false
	canceling, err := statemachine.Compile(definition(
		func(context.Context, struct{}) (*statemachine.Rejection, error) {
			cancel()
			return nil, nil
		},
		func(context.Context, struct{}) (*statemachine.Rejection, error) {
			secondCalled = true
			return nil, nil
		},
	))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := canceling.Transition(ctx, "pending", "pay", struct{}{}, statemachine.Metadata{}); !errors.Is(err, context.Canceled) || secondCalled {
		t.Fatalf("checked cancellation error = %v, second called = %t", err, secondCalled)
	}
}
