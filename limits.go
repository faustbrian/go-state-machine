package statemachine

import "errors"

// Limits bound hostile or accidentally oversized definitions and execution
// inputs. Every field must be positive.
type Limits struct {
	MaxStates               int
	MaxTransitions          int
	MaxSourcesPerTransition int
	MaxGuardsPerTransition  int
	MaxEffectsPerPhase      int
	MaxEffectPayloadBytes   int
	MaxMetadataBytes        int
	MaxReplayInputs         int
}

// DefaultLimits returns conservative bounds suitable for general use.
func DefaultLimits() Limits {
	return Limits{
		MaxStates: 10_000, MaxTransitions: 50_000,
		MaxSourcesPerTransition: 10_000, MaxGuardsPerTransition: 100,
		MaxEffectsPerPhase: 1_000, MaxEffectPayloadBytes: 1 << 20,
		MaxMetadataBytes: 16 << 10, MaxReplayInputs: 1_000_000,
	}
}

// ErrLimitExceeded reports an input larger than its compiled bound.
var ErrLimitExceeded = errors.New("statemachine: limit exceeded")

func (limits Limits) valid() bool {
	return limits.MaxStates > 0 && limits.MaxTransitions > 0 &&
		limits.MaxSourcesPerTransition > 0 && limits.MaxGuardsPerTransition > 0 &&
		limits.MaxEffectsPerPhase > 0 && limits.MaxEffectPayloadBytes > 0 &&
		limits.MaxMetadataBytes > 0 && limits.MaxReplayInputs > 0
}
