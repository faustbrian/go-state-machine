// Package statemachine provides immutable, deterministic, typed state
// machines. Transition lookup is pure: effects are returned as data and are
// never executed by a compiled machine.
package statemachine

import (
	"context"
	"errors"
	"fmt"
)

// State is the set of values accepted as machine states.
type State interface {
	comparable
}

// Event is the set of values accepted as machine events.
type Event interface {
	comparable
}

// TransitionID is a stable identifier for a transition.
type TransitionID string

// Version is a stable identifier for a compiled definition.
type Version string

// Metadata relates an event to the work that caused and contains it.
type Metadata struct {
	CorrelationID string
	CausationID   string
}

// Effect is inert data describing work planned by a transition.
type Effect struct {
	Kind    string
	Payload []byte
}

// StateDefinition describes a state and its ordered entry and exit effects.
type StateDefinition[S State] struct {
	State    S
	Terminal bool
	Entry    []Effect
	Exit     []Effect
}

// Rejection is a structured explanation for a guard declining a transition.
type Rejection struct {
	Code    string
	Message string
}

// Guard decides whether a transition is eligible. Guards must not perform
// side effects.
type Guard[C any] func(context.Context, C) *Rejection

// CheckedGuard may reject a transition or report an operational failure.
// Returned errors are preserved for errors.Is without being rendered.
type CheckedGuard[C any] func(context.Context, C) (*Rejection, error)

// TransitionDefinition describes one exact-source or wildcard transition.
type TransitionDefinition[S State, E Event, C any] struct {
	ID            TransitionID
	Sources       []S
	Wildcard      bool
	Event         E
	To            S
	Guards        []Guard[C]
	CheckedGuards []CheckedGuard[C]
	Effects       []Effect
}

// Definition is the mutable input consumed by Compile.
type Definition[S State, E Event, C any] struct {
	Version     Version
	Initial     S
	States      []StateDefinition[S]
	Transitions []TransitionDefinition[S, E, C]
	// CloneContext isolates mutable context from each guard invocation. Callers
	// using reference-bearing context MUST provide a deep clone function.
	CloneContext func(C) C
}

// Result is the complete output of a pure transition calculation.
type Result[S State, E Event] struct {
	DefinitionVersion Version
	Previous          S
	Next              S
	Event             E
	TransitionID      TransitionID
	Metadata          Metadata
	Effects           []Effect
}

// Machine is an immutable compiled state machine safe for concurrent use.
type Machine[S State, E Event, C any] struct {
	version      Version
	initial      S
	states       map[S]StateDefinition[S]
	stateList    []StateDefinition[S]
	exact        map[S]map[E]TransitionDefinition[S, E, C]
	wildcard     map[E]TransitionDefinition[S, E, C]
	transitions  []TransitionDefinition[S, E, C]
	cloneContext func(C) C
	limits       Limits
}

// ErrNoTransition reports that no eligible transition exists.
var ErrNoTransition = errors.New("statemachine: no transition")

// ErrUnknownState reports that the current state is absent from the compiled
// definition.
var ErrUnknownState = errors.New("statemachine: unknown current state")

// ErrTerminalState reports that terminal states cannot transition, including
// through wildcard transitions.
var ErrTerminalState = errors.New("statemachine: terminal state")

// ErrGuardRejected reports that a selected transition was declined by a
// guard. Exact transitions never fall through to a wildcard after rejection.
var ErrGuardRejected = errors.New("statemachine: guard rejected transition")

// ErrGuardPanic reports that a guard panicked. The panic value is deliberately
// omitted because it may contain sensitive application data.
var ErrGuardPanic = errors.New("statemachine: guard panicked")

// ErrGuardFailed reports an operational guard failure distinct from a domain
// rejection. The underlying error is available through errors.Is/errors.As.
var ErrGuardFailed = errors.New("statemachine: guard failed")

// ErrContextClonePanic reports a contained context-cloner panic. Panic values
// are omitted because they may contain sensitive application data.
var ErrContextClonePanic = errors.New("statemachine: context clone panicked")

// GuardRejectedError preserves the selected transition and structured reason.
type GuardRejectedError struct {
	TransitionID TransitionID
	Rejection    Rejection
}

func (err *GuardRejectedError) Error() string {
	return fmt.Sprintf("%s: %s: %s", ErrGuardRejected, err.Rejection.Code, err.Rejection.Message)
}

// Unwrap supports errors.Is(err, ErrGuardRejected).
func (err *GuardRejectedError) Unwrap() error {
	return ErrGuardRejected
}

// GuardPanicError identifies the transition whose guard panicked.
type GuardPanicError struct {
	TransitionID TransitionID
}

func (err *GuardPanicError) Error() string {
	return fmt.Sprintf("statemachine: guard panicked in transition %s", err.TransitionID)
}

// Unwrap supports errors.Is(err, ErrGuardPanic).
func (err *GuardPanicError) Unwrap() error {
	return ErrGuardPanic
}

// GuardFailedError identifies the transition without rendering its possibly
// sensitive underlying error.
type GuardFailedError struct {
	TransitionID TransitionID
	Cause        error
}

func (err *GuardFailedError) Error() string {
	return fmt.Sprintf("statemachine: guard failed in transition %s", err.TransitionID)
}

func (err *GuardFailedError) Unwrap() []error {
	return []error{ErrGuardFailed, err.Cause}
}

// Compile validates and copies a definition into an immutable machine.
func Compile[S State, E Event, C any](definition Definition[S, E, C]) (*Machine[S, E, C], error) {
	return CompileWithLimits(definition, DefaultLimits())
}

// CompileWithLimits validates and copies a definition using explicit resource
// bounds.
func CompileWithLimits[S State, E Event, C any](definition Definition[S, E, C], limits Limits) (*Machine[S, E, C], error) {
	if !limits.valid() {
		return nil, &DiagnosticsError{Diagnostics: []Diagnostic{{
			Code: DiagnosticLimitExceeded, Message: "all compile limits must be positive",
		}}}
	}
	if len(definition.States) > limits.MaxStates || len(definition.Transitions) > limits.MaxTransitions {
		return nil, &DiagnosticsError{Diagnostics: []Diagnostic{{
			Code: DiagnosticLimitExceeded, Message: "definition exceeds state or transition limit",
		}}}
	}
	machine := &Machine[S, E, C]{
		version:      definition.Version,
		initial:      definition.Initial,
		states:       make(map[S]StateDefinition[S], len(definition.States)),
		stateList:    make([]StateDefinition[S], 0, len(definition.States)),
		exact:        make(map[S]map[E]TransitionDefinition[S, E, C]),
		wildcard:     make(map[E]TransitionDefinition[S, E, C]),
		transitions:  make([]TransitionDefinition[S, E, C], 0, len(definition.Transitions)),
		cloneContext: definition.CloneContext,
		limits:       limits,
	}
	diagnostics := make([]Diagnostic, 0)
	if definition.Version == "" {
		diagnostics = append(diagnostics, Diagnostic{
			Code: DiagnosticMissingVersion, Message: "version must not be empty",
		})
	}

	for _, state := range definition.States {
		if _, exists := machine.states[state.State]; exists {
			diagnostics = append(diagnostics, Diagnostic{
				Code: DiagnosticDuplicateState, Message: fmt.Sprintf("state %v is defined more than once", state.State),
			})
			continue
		}
		state.Entry = cloneEffects(state.Entry)
		state.Exit = cloneEffects(state.Exit)
		diagnostics = append(diagnostics, limitEffects(state.Entry, "entry", limits, "")...)
		diagnostics = append(diagnostics, limitEffects(state.Exit, "exit", limits, "")...)
		diagnostics = append(diagnostics, validateEffects(state.Entry, "entry")...)
		diagnostics = append(diagnostics, validateEffects(state.Exit, "exit")...)
		machine.states[state.State] = state
		machine.stateList = append(machine.stateList, state)
	}
	if _, ok := machine.states[definition.Initial]; !ok {
		diagnostics = append(diagnostics, Diagnostic{
			Code: DiagnosticMissingInitial, Message: "initial state is not defined",
		})
	}

	transitionIDs := make(map[TransitionID]struct{}, len(definition.Transitions))
	for _, transition := range definition.Transitions {
		transition.Sources = append([]S(nil), transition.Sources...)
		transition.Guards = append([]Guard[C](nil), transition.Guards...)
		transition.CheckedGuards = append([]CheckedGuard[C](nil), transition.CheckedGuards...)
		transition.Effects = cloneEffects(transition.Effects)
		machine.transitions = append(machine.transitions, transition)
		if transition.ID == "" {
			diagnostics = append(diagnostics, Diagnostic{
				Code: DiagnosticMissingTransitionID, Message: "transition ID must not be empty",
			})
		} else if _, exists := transitionIDs[transition.ID]; exists {
			diagnostics = append(diagnostics, Diagnostic{
				Code: DiagnosticDuplicateTransition, Message: fmt.Sprintf("transition %q is defined more than once", transition.ID), TransitionID: transition.ID,
			})
		}
		transitionIDs[transition.ID] = struct{}{}
		diagnostics = append(diagnostics, diagnosticsForEffects(transition.Effects, transition.ID)...)
		if len(transition.Sources) > limits.MaxSourcesPerTransition ||
			len(transition.Guards)+len(transition.CheckedGuards) > limits.MaxGuardsPerTransition {
			diagnostics = append(diagnostics, Diagnostic{
				Code: DiagnosticLimitExceeded, Message: "transition exceeds source or guard limit", TransitionID: transition.ID,
			})
		}
		diagnostics = append(diagnostics, limitEffects(transition.Effects, "transition", limits, transition.ID)...)
		if _, exists := machine.states[transition.To]; !exists {
			diagnostics = append(diagnostics, Diagnostic{
				Code: DiagnosticUnknownState, Message: fmt.Sprintf("destination state %v is not defined", transition.To), TransitionID: transition.ID,
			})
		}
		if transition.Wildcard {
			if len(transition.Sources) != 0 {
				diagnostics = append(diagnostics, Diagnostic{
					Code: DiagnosticInvalidWildcard, Message: "wildcard transition must not declare sources", TransitionID: transition.ID,
				})
			}
			if existing, exists := machine.wildcard[transition.Event]; exists {
				diagnostics = append(diagnostics, Diagnostic{
					Code: DiagnosticAmbiguousWildcard, Message: fmt.Sprintf("transitions %q and %q match the same wildcard event", existing.ID, transition.ID), TransitionID: transition.ID,
				})
				continue
			}
			machine.wildcard[transition.Event] = transition
			continue
		}
		if len(transition.Sources) == 0 {
			diagnostics = append(diagnostics, Diagnostic{
				Code: DiagnosticMissingSource, Message: "exact transition must declare at least one source", TransitionID: transition.ID,
			})
		}
		for _, source := range transition.Sources {
			state, exists := machine.states[source]
			if !exists {
				diagnostics = append(diagnostics, Diagnostic{
					Code: DiagnosticUnknownState, Message: fmt.Sprintf("source state %v is not defined", source), TransitionID: transition.ID,
				})
			} else if state.Terminal {
				diagnostics = append(diagnostics, Diagnostic{
					Code: DiagnosticTerminalTransition, Message: fmt.Sprintf("terminal state %v has an outgoing transition", source), TransitionID: transition.ID,
				})
			}
			if machine.exact[source] == nil {
				machine.exact[source] = make(map[E]TransitionDefinition[S, E, C])
			}
			if existing, exists := machine.exact[source][transition.Event]; exists {
				diagnostics = append(diagnostics, Diagnostic{
					Code: DiagnosticAmbiguousTransition, Message: fmt.Sprintf("transitions %q and %q match the same state and event", existing.ID, transition.ID), TransitionID: transition.ID,
				})
				continue
			}
			machine.exact[source][transition.Event] = transition
		}
	}
	diagnostics = append(diagnostics, machine.unreachableDiagnostics()...)
	if len(diagnostics) != 0 {
		return nil, &DiagnosticsError{Diagnostics: diagnostics}
	}

	return machine, nil
}

// Graph is an immutable snapshot of an inspectable compiled definition.
// Calling Machine.Graph returns a fresh deep copy.
type Graph[S State, E Event, C any] struct {
	Version     Version
	Initial     S
	States      []StateDefinition[S]
	Transitions []TransitionDefinition[S, E, C]
}

// Graph returns a deep copy of the compiled transition graph in definition
// order.
func (machine *Machine[S, E, C]) Graph() Graph[S, E, C] {
	graph := Graph[S, E, C]{
		Version:     machine.version,
		Initial:     machine.initial,
		States:      make([]StateDefinition[S], len(machine.stateList)),
		Transitions: make([]TransitionDefinition[S, E, C], len(machine.transitions)),
	}
	for index, state := range machine.stateList {
		state.Entry = cloneEffects(state.Entry)
		state.Exit = cloneEffects(state.Exit)
		graph.States[index] = state
	}
	for index, transition := range machine.transitions {
		transition.Sources = append([]S(nil), transition.Sources...)
		transition.Guards = append([]Guard[C](nil), transition.Guards...)
		transition.CheckedGuards = append([]CheckedGuard[C](nil), transition.CheckedGuards...)
		transition.Effects = cloneEffects(transition.Effects)
		graph.Transitions[index] = transition
	}
	return graph
}

// Transition calculates one transition without performing IO.
func (machine *Machine[S, E, C]) Transition(ctx context.Context, current S, event E, transitionContext C, metadata Metadata) (Result[S, E], error) {
	if err := ctx.Err(); err != nil {
		return Result[S, E]{}, err
	}
	if len(metadata.CorrelationID)+len(metadata.CausationID) > machine.limits.MaxMetadataBytes {
		return Result[S, E]{}, fmt.Errorf("%w: metadata", ErrLimitExceeded)
	}
	state, exists := machine.states[current]
	if !exists {
		return Result[S, E]{}, ErrUnknownState
	}
	if state.Terminal {
		return Result[S, E]{}, ErrTerminalState
	}
	transition, ok := machine.exact[current][event]
	if !ok {
		transition, ok = machine.wildcard[event]
	}
	if !ok {
		return Result[S, E]{}, ErrNoTransition
	}
	for _, guard := range transition.Guards {
		if err := ctx.Err(); err != nil {
			return Result[S, E]{}, err
		}
		guardContext, clonePanicked := machine.cloneGuardContext(transitionContext)
		if clonePanicked {
			return Result[S, E]{}, ErrContextClonePanic
		}
		rejection, panicked := callGuard(ctx, guard, guardContext)
		if panicked {
			return Result[S, E]{}, &GuardPanicError{TransitionID: transition.ID}
		}
		if rejection != nil {
			return Result[S, E]{}, &GuardRejectedError{
				TransitionID: transition.ID,
				Rejection:    *rejection,
			}
		}
	}
	for _, guard := range transition.CheckedGuards {
		if err := ctx.Err(); err != nil {
			return Result[S, E]{}, err
		}
		guardContext, clonePanicked := machine.cloneGuardContext(transitionContext)
		if clonePanicked {
			return Result[S, E]{}, ErrContextClonePanic
		}
		rejection, guardErr, panicked := callCheckedGuard(ctx, guard, guardContext)
		if panicked {
			return Result[S, E]{}, &GuardPanicError{TransitionID: transition.ID}
		}
		if guardErr != nil {
			return Result[S, E]{}, &GuardFailedError{TransitionID: transition.ID, Cause: guardErr}
		}
		if rejection != nil {
			return Result[S, E]{}, &GuardRejectedError{
				TransitionID: transition.ID,
				Rejection:    *rejection,
			}
		}
	}

	effects := make([]Effect, 0,
		len(machine.states[current].Exit)+len(transition.Effects)+len(machine.states[transition.To].Entry))
	effects = append(effects, cloneEffects(machine.states[current].Exit)...)
	effects = append(effects, cloneEffects(transition.Effects)...)
	effects = append(effects, cloneEffects(machine.states[transition.To].Entry)...)

	return Result[S, E]{
		DefinitionVersion: machine.version,
		Previous:          current,
		Next:              transition.To,
		Event:             event,
		TransitionID:      transition.ID,
		Metadata:          metadata,
		Effects:           effects,
	}, nil
}

func (machine *Machine[S, E, C]) cloneGuardContext(transitionContext C) (cloned C, panicked bool) {
	if machine.cloneContext == nil {
		return transitionContext, false
	}
	defer func() {
		if recover() != nil {
			var zero C
			cloned = zero
			panicked = true
		}
	}()
	return machine.cloneContext(transitionContext), false
}

func callGuard[C any](ctx context.Context, guard Guard[C], transitionContext C) (rejection *Rejection, panicked bool) {
	defer func() {
		if recover() != nil {
			rejection = nil
			panicked = true
		}
	}()
	return guard(ctx, transitionContext), false
}

func callCheckedGuard[C any](ctx context.Context, guard CheckedGuard[C], transitionContext C) (rejection *Rejection, guardErr error, panicked bool) {
	defer func() {
		if recover() != nil {
			rejection = nil
			guardErr = nil
			panicked = true
		}
	}()
	rejection, guardErr = guard(ctx, transitionContext)
	return rejection, guardErr, false
}

func cloneEffects(effects []Effect) []Effect {
	cloned := make([]Effect, len(effects))
	for index, effect := range effects {
		cloned[index] = effect
		cloned[index].Payload = append([]byte(nil), effect.Payload...)
	}
	return cloned
}

func validateEffects(effects []Effect, phase string) []Diagnostic {
	diagnostics := make([]Diagnostic, 0)
	for _, effect := range effects {
		if effect.Kind == "" {
			diagnostics = append(diagnostics, Diagnostic{
				Code: DiagnosticMissingEffectKind, Message: phase + " effect kind must not be empty",
			})
		}
	}
	return diagnostics
}

func diagnosticsForEffects(effects []Effect, id TransitionID) []Diagnostic {
	diagnostics := validateEffects(effects, "transition")
	for index := range diagnostics {
		diagnostics[index].TransitionID = id
	}
	return diagnostics
}

func limitEffects(effects []Effect, phase string, limits Limits, id TransitionID) []Diagnostic {
	diagnostics := make([]Diagnostic, 0)
	if len(effects) > limits.MaxEffectsPerPhase {
		diagnostics = append(diagnostics, Diagnostic{
			Code: DiagnosticLimitExceeded, Message: phase + " effects exceed count limit", TransitionID: id,
		})
	}
	for _, effect := range effects {
		if len(effect.Payload) > limits.MaxEffectPayloadBytes {
			diagnostics = append(diagnostics, Diagnostic{
				Code: DiagnosticLimitExceeded, Message: phase + " effect payload exceeds byte limit", TransitionID: id,
			})
		}
	}
	return diagnostics
}

func (machine *Machine[S, E, C]) unreachableDiagnostics() []Diagnostic {
	if _, exists := machine.states[machine.initial]; !exists {
		return nil
	}
	reachable := map[S]bool{machine.initial: true}
	changed := true
	for changed {
		changed = false
		for _, transition := range machine.transitions {
			if transition.Wildcard {
				hasReachableSource := false
				for source := range reachable {
					if !machine.states[source].Terminal {
						hasReachableSource = true
						break
					}
				}
				if hasReachableSource && !reachable[transition.To] {
					reachable[transition.To] = true
					changed = true
				}
				continue
			}
			for _, source := range transition.Sources {
				if reachable[source] && !reachable[transition.To] {
					reachable[transition.To] = true
					changed = true
				}
			}
		}
	}

	diagnostics := make([]Diagnostic, 0)
	for state := range machine.states {
		if !reachable[state] {
			diagnostics = append(diagnostics, Diagnostic{
				Code: DiagnosticUnreachableState, Message: fmt.Sprintf("state %v is unreachable", state),
			})
		}
	}
	return diagnostics
}
