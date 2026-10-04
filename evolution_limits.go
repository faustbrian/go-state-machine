package statemachine

// EvolutionLimits bound direct compilation and migration of caller-supplied
// versioned history. Every field must be positive when limits are explicit.
type EvolutionLimits struct {
	MaxMigrations          int
	MaxHistoryEntries      int
	MaxMigrationSteps      int
	MaxCarriedEffects      int
	MaxEffectPayloadBytes  int
	MaxCarriedPayloadBytes int
	MaxVersionBytes        int
}

// DefaultEvolutionLimits returns finite direct-migration budgets.
func DefaultEvolutionLimits() EvolutionLimits {
	return EvolutionLimits{
		MaxMigrations: 256, MaxHistoryEntries: 10_000,
		MaxMigrationSteps: 100_000, MaxCarriedEffects: 10_000,
		MaxEffectPayloadBytes: 1 << 20, MaxCarriedPayloadBytes: 16 << 20,
		MaxVersionBytes: 256,
	}
}

func (limits EvolutionLimits) valid() bool {
	return limits.MaxMigrations > 0 && limits.MaxHistoryEntries > 0 &&
		limits.MaxMigrationSteps > 0 && limits.MaxCarriedEffects > 0 &&
		limits.MaxEffectPayloadBytes > 0 && limits.MaxCarriedPayloadBytes > 0 &&
		limits.MaxVersionBytes > 0
}
