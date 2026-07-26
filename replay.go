package statemachine

import (
	"context"
	"fmt"
)

// Input contains the typed values required to calculate one transition.
type Input[E Event, C any] struct {
	Event    E
	Context  C
	Metadata Metadata
}

// ReplayResult contains every calculated transition and the resulting state.
type ReplayResult[S State, E Event] struct {
	Initial     S
	Final       S
	Transitions []Result[S, E]
}

// ReplayError identifies the zero-based input that failed without rendering
// the event or context, which may contain sensitive values.
type ReplayError struct {
	Index int
	Cause error
}

func (err *ReplayError) Error() string {
	return fmt.Sprintf("statemachine: replay input %d failed: %v", err.Index, err.Cause)
}

// Unwrap exposes the transition failure to errors.Is and errors.As.
func (err *ReplayError) Unwrap() error {
	return err.Cause
}

// Replay deterministically applies inputs from the definition's initial state.
func (machine *Machine[S, E, C]) Replay(ctx context.Context, inputs []Input[E, C]) (ReplayResult[S, E], error) {
	return machine.ReplayFrom(ctx, machine.initial, inputs)
}

// ReplayFrom deterministically applies inputs from a snapshot state.
func (machine *Machine[S, E, C]) ReplayFrom(ctx context.Context, initial S, inputs []Input[E, C]) (ReplayResult[S, E], error) {
	if err := ctx.Err(); err != nil {
		return ReplayResult[S, E]{}, err
	}
	if _, exists := machine.states[initial]; !exists {
		return ReplayResult[S, E]{}, ErrUnknownState
	}
	if len(inputs) > machine.limits.MaxReplayInputs {
		return ReplayResult[S, E]{}, fmt.Errorf("%w: replay inputs", ErrLimitExceeded)
	}
	replay := ReplayResult[S, E]{
		Initial:     initial,
		Final:       initial,
		Transitions: make([]Result[S, E], 0, len(inputs)),
	}
	for index, input := range inputs {
		result, err := machine.Transition(ctx, replay.Final, input.Event, input.Context, input.Metadata)
		if err != nil {
			return ReplayResult[S, E]{}, &ReplayError{Index: index, Cause: err}
		}
		replay.Transitions = append(replay.Transitions, result)
		replay.Final = result.Next
	}
	return replay, nil
}
