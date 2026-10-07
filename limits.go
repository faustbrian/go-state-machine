package statemachine

import "errors"

// Limits bound hostile or accidentally oversized definitions and execution
// inputs. Fields must be positive, except the two aggregate compile limits,
// whose zero values select their DefaultLimits allowances.
type Limits struct {
	MaxStates               int
	MaxTransitions          int
	MaxSourcesPerTransition int
	MaxGuardsPerTransition  int
	MaxEffectsPerPhase      int
	MaxEffectPayloadBytes   int
	// MaxCompiledEffectPayloadBytes counts every copied payload occurrence,
	// including shared input buffers, across the complete definition.
	MaxCompiledEffectPayloadBytes int
	// MaxCompiledElements counts states, transitions, sources, guards, and
	// effects across the complete definition, including repeated occurrences.
	MaxCompiledElements int
	MaxMetadataBytes    int
	MaxReplayInputs     int
}

// DefaultLimits returns conservative bounds suitable for general use.
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
