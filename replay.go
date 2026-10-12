package statemachine

import (
	"context"
	"fmt"
)

// Input contains the typed values required to calculate one transition.
type Input[E Event, C any] struct {
	// Event is the next comparable selection input.
	Event E
	// Context is caller-owned guard data; cloning follows Definition.CloneContext.
	Context C
	// Metadata is passed through with the compiled metadata-byte bound.
	Metadata Metadata
}

// ReplayResult contains every calculated transition and the resulting state.
type ReplayResult[S State, E Event] struct {
	// Initial is the validated starting state for this replay.
	Initial S
	// Final is the last selected destination, or Initial for empty inputs.
	Final S
	// Transitions contains ordered calculated plans; no effects are executed.
	Transitions []Result[S, E]
}

// ReplayError identifies the zero-based input that failed without rendering
// the event or context, which may contain sensitive values.
type ReplayError struct {
	// Index is the zero-based failed input; no partial ReplayResult is returned.
	Index int
	// Cause is the underlying transition error and may expose application detail.
	Cause error
}

// Error includes the failed input index, not event, context or cause values.
func (err *ReplayError) Error() string {
	return fmt.Sprintf("statemachine: replay input %d failed", err.Index)
}

// Unwrap exposes the transition failure to errors.Is and errors.As.
func (err *ReplayError) Unwrap() error {
	return err.Cause
}

// Replay applies inputs from the definition initial state, re-invoking guards.
// Determinism depends on callbacks and stable typed inputs; effects stay inert.
func (machine *Machine[S, E, C]) Replay(ctx context.Context, inputs []Input[E, C]) (ReplayResult[S, E], error) {
	return machine.ReplayFrom(ctx, machine.initial, inputs)
}

// ReplayFrom applies inputs from a known state, even for an empty input slice.
// It bounds input count and uses Transition for each selection. Any failed input
// returns a zero ReplayResult with ReplayError, not a partial successful prefix.
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
