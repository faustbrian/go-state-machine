package statemachine

import (
	"context"
	"errors"
	"fmt"
)

// Migration converts stable state and event identifiers between adjacent
// definition versions. Nil hooks are identity conversions.
type Migration[S State, E Event] struct {
	From  Version
	To    Version
	State func(S) (S, error)
	Event func(E) (E, error)
}

// Evolution is an immutable, deterministic set of version migration hooks.
type Evolution[S State, E Event] struct {
	steps  map[Version]Migration[S, E]
	limits EvolutionLimits
}

var (
	// ErrInvalidEvolution reports malformed or cyclic migration definitions.
	ErrInvalidEvolution = errors.New("statemachine: invalid evolution")
	// ErrMissingMigration reports a gap between a stored and target version.
	ErrMissingMigration = errors.New("statemachine: missing migration")
)

// MigrationError identifies a failing hook without rendering migrated data.
type MigrationError struct {
	From  Version
	To    Version
	Field string
	Cause error
}

func (err *MigrationError) Error() string {
	return "statemachine: migration failed"
}

func (err *MigrationError) Unwrap() error {
	return err.Cause
}

// CompileEvolution validates migrations with DefaultEvolutionLimits and copies
// them into immutable lookup state. Each version may have one successor.
func CompileEvolution[S State, E Event](migrations []Migration[S, E]) (*Evolution[S, E], error) {
	return CompileEvolutionWithLimits(migrations, EvolutionLimits{})
}

// CompileEvolutionWithLimits validates migrations with explicit finite bounds.
// An all-zero limits value selects DefaultEvolutionLimits.
func CompileEvolutionWithLimits[S State, E Event](migrations []Migration[S, E], limits EvolutionLimits) (*Evolution[S, E], error) {
	if limits == (EvolutionLimits{}) {
		limits = DefaultEvolutionLimits()
	}
	if !limits.valid() {
		return nil, ErrInvalidEvolution
	}
	if len(migrations) > limits.MaxMigrations {
		return nil, ErrLimitExceeded
	}
	for _, migration := range migrations {
		if len(migration.From) > limits.MaxVersionBytes || len(migration.To) > limits.MaxVersionBytes {
			return nil, ErrLimitExceeded
		}
	}
	evolution := &Evolution[S, E]{steps: make(map[Version]Migration[S, E], len(migrations)), limits: limits}
	for _, migration := range migrations {
		if migration.From == "" || migration.To == "" || migration.From == migration.To {
			return nil, ErrInvalidEvolution
		}
		if _, exists := evolution.steps[migration.From]; exists {
			return nil, ErrInvalidEvolution
		}
		evolution.steps[migration.From] = migration
	}
	for start := range evolution.steps {
		seen := make(map[Version]bool)
		for version := start; evolution.steps[version].To != ""; version = evolution.steps[version].To {
			if seen[version] {
				return nil, ErrInvalidEvolution
			}
			seen[version] = true
		}
	}
	return evolution, nil
}

// Migrate converts a snapshot and history to target without mutating their
// owned slices. It rejects oversized inputs before invoking any migration hook
// and returns no partial output on error. Caller-owned hooks may have side
// effects and must cooperate with cancellation.
func (evolution *Evolution[S, E]) Migrate(ctx context.Context, snapshot Snapshot[S], history []HistoryEntry[S, E], target Version) (Snapshot[S], []HistoryEntry[S, E], error) {
	if target == "" {
		return Snapshot[S]{}, nil, ErrInvalidEvolution
	}
	if err := ctx.Err(); err != nil {
		return Snapshot[S]{}, nil, err
	}
	if err := evolution.preflight(ctx, snapshot, history, target); err != nil {
		return Snapshot[S]{}, nil, err
	}
	state, err := evolution.migrateState(ctx, snapshot.State, snapshot.DefinitionVersion, target)
	if err != nil {
		return Snapshot[S]{}, nil, err
	}
	if err := ctx.Err(); err != nil {
		return Snapshot[S]{}, nil, err
	}
	snapshot.State = state
	snapshot.DefinitionVersion = target
	migrated := make([]HistoryEntry[S, E], len(history))
	for index, entry := range history {
		if err := ctx.Err(); err != nil {
			return Snapshot[S]{}, nil, err
		}
		result, err := evolution.migrateResult(ctx, entry.Result, target)
		if err != nil {
			return Snapshot[S]{}, nil, fmt.Errorf("history entry %d: %w", index, err)
		}
		entry.Result = result
		migrated[index] = entry
	}
	if err := ctx.Err(); err != nil {
		return Snapshot[S]{}, nil, err
	}
	return snapshot, migrated, nil
}

func (evolution *Evolution[S, E]) preflight(ctx context.Context, snapshot Snapshot[S], history []HistoryEntry[S, E], target Version) error {
	limits := evolution.limits
	if limits == (EvolutionLimits{}) {
		limits = DefaultEvolutionLimits()
	}
	if len(history) > limits.MaxHistoryEntries || len(target) > limits.MaxVersionBytes ||
		len(snapshot.DefinitionVersion) > limits.MaxVersionBytes {
		return ErrLimitExceeded
	}
	remainingSteps := limits.MaxMigrationSteps
	checkPath := func(from Version) error {
		for from != target {
			if err := ctx.Err(); err != nil {
				return err
			}
			migration, exists := evolution.steps[from]
			if !exists {
				return ErrMissingMigration
			}
			if remainingSteps == 0 {
				return ErrLimitExceeded
			}
			remainingSteps--
			from = migration.To
		}
		return nil
	}
	if err := checkPath(snapshot.DefinitionVersion); err != nil {
		return err
	}
	carriedEffects := 0
	carriedPayloadBytes := 0
	for index, entry := range history {
		if err := ctx.Err(); err != nil {
			return err
		}
		result := entry.Result
		if len(result.DefinitionVersion) > limits.MaxVersionBytes ||
			len(result.Effects) > limits.MaxCarriedEffects-carriedEffects {
			return ErrLimitExceeded
		}
		carriedEffects += len(result.Effects)
		for _, effect := range result.Effects {
			if err := ctx.Err(); err != nil {
				return err
			}
			if len(effect.Payload) > limits.MaxEffectPayloadBytes ||
				len(effect.Payload) > limits.MaxCarriedPayloadBytes-carriedPayloadBytes {
				return ErrLimitExceeded
			}
			carriedPayloadBytes += len(effect.Payload)
		}
		if err := checkPath(result.DefinitionVersion); err != nil {
			return fmt.Errorf("history entry %d: %w", index, err)
		}
	}
	return nil
}

func (evolution *Evolution[S, E]) migrateState(ctx context.Context, state S, from Version, target Version) (S, error) {
	for from != target {
		if err := ctx.Err(); err != nil {
			return state, err
		}
		migration, exists := evolution.steps[from]
		if !exists {
			return state, ErrMissingMigration
		}
		if migration.State != nil {
			migrated, err := migration.State(state)
			if err != nil {
				return state, &MigrationError{From: migration.From, To: migration.To, Field: "state", Cause: err}
			}
			state = migrated
			if err := ctx.Err(); err != nil {
				return state, err
			}
		}
		from = migration.To
	}
	return state, nil
}

func (evolution *Evolution[S, E]) migrateResult(ctx context.Context, result Result[S, E], target Version) (Result[S, E], error) {
	for result.DefinitionVersion != target {
		if err := ctx.Err(); err != nil {
			return Result[S, E]{}, err
		}
		migration, exists := evolution.steps[result.DefinitionVersion]
		if !exists {
			return Result[S, E]{}, ErrMissingMigration
		}
		if migration.State != nil {
			previous, err := migration.State(result.Previous)
			if err != nil {
				return Result[S, E]{}, &MigrationError{From: migration.From, To: migration.To, Field: "previous state", Cause: err}
			}
			if err := ctx.Err(); err != nil {
				return Result[S, E]{}, err
			}
			next, err := migration.State(result.Next)
			if err != nil {
				return Result[S, E]{}, &MigrationError{From: migration.From, To: migration.To, Field: "next state", Cause: err}
			}
			if err := ctx.Err(); err != nil {
				return Result[S, E]{}, err
			}
			result.Previous, result.Next = previous, next
		}
		if migration.Event != nil {
			event, err := migration.Event(result.Event)
			if err != nil {
				return Result[S, E]{}, &MigrationError{From: migration.From, To: migration.To, Field: "event", Cause: err}
			}
			if err := ctx.Err(); err != nil {
				return Result[S, E]{}, err
			}
			result.Event = event
		}
		result.DefinitionVersion = migration.To
	}
	result.Effects = cloneEffects(result.Effects)
	if err := ctx.Err(); err != nil {
		return Result[S, E]{}, err
	}
	return result, nil
}
