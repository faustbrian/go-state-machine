package statemachine

import "errors"

// Limits bound hostile or accidentally oversized definitions and execution
// inputs. Fields must be positive, except the two aggregate compile limits,
// whose zero values select their DefaultLimits allowances.
type Limits struct {
	// MaxStates bounds the definition state count before allocation.
	MaxStates int
	// MaxTransitions bounds the definition transition count before allocation.
	MaxTransitions int
	// MaxSourcesPerTransition bounds exact-source occurrences on one transition.
	MaxSourcesPerTransition int
	// MaxGuardsPerTransition bounds the combined ordinary and checked guard count.
	MaxGuardsPerTransition int
	// MaxEffectsPerPhase bounds each entry, exit, or transition effect list separately.
	MaxEffectsPerPhase int
	// MaxEffectPayloadBytes bounds one copied opaque payload occurrence.
	MaxEffectPayloadBytes int
	// MaxCompiledEffectPayloadBytes counts every copied payload occurrence,
	// including shared input buffers, across the complete definition.
	MaxCompiledEffectPayloadBytes int
	// MaxCompiledElements counts states, transitions, sources, guards, and
	// effects across the complete definition, including repeated occurrences.
	MaxCompiledElements int
	// MaxMetadataBytes bounds combined correlation and causation bytes.
	MaxMetadataBytes int
	// MaxReplayInputs bounds replay inputs and machine-specific history validation.
	MaxReplayInputs int
}

// DefaultLimits returns finite package defaults, not a total heap or callback
// cost guarantee. Custom plans must fit their runner/persistence bounds too.
func DefaultLimits() Limits {
	return Limits{
		MaxStates: 10_000, MaxTransitions: 50_000,
		MaxSourcesPerTransition: 10_000, MaxGuardsPerTransition: 100,
		MaxEffectsPerPhase: 1_000, MaxEffectPayloadBytes: 1 << 20,
		MaxCompiledEffectPayloadBytes: 16 << 20, MaxCompiledElements: 100_000,
		MaxMetadataBytes: 16 << 10, MaxReplayInputs: 1_000_000,
	}
}

// ErrLimitExceeded reports an input larger than its compiled bound.
var ErrLimitExceeded = errors.New("statemachine: limit exceeded")

func (limits Limits) valid() bool {
	return limits.MaxStates > 0 && limits.MaxTransitions > 0 &&
		limits.MaxSourcesPerTransition > 0 && limits.MaxGuardsPerTransition > 0 &&
		limits.MaxEffectsPerPhase > 0 && limits.MaxEffectPayloadBytes > 0 &&
		limits.MaxCompiledEffectPayloadBytes > 0 && limits.MaxCompiledElements > 0 &&
		limits.MaxMetadataBytes > 0 && limits.MaxReplayInputs > 0
}
