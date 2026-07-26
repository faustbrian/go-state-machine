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
	steps map[Version]Migration[S, E]
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
	return fmt.Sprintf("statemachine: migrate %s from %s to %s: %v", err.Field, err.From, err.To, err.Cause)
}

func (err *MigrationError) Unwrap() error {
	return err.Cause
}

// CompileEvolution validates migrations and copies them into immutable lookup
// state. Each version may have exactly one successor.
func CompileEvolution[S State, E Event](migrations []Migration[S, E]) (*Evolution[S, E], error) {
	evolution := &Evolution[S, E]{steps: make(map[Version]Migration[S, E], len(migrations))}
	for _, migration := range migrations {
		if migration.From == "" || migration.To == "" || migration.From == migration.To {
			return nil, ErrInvalidEvolution
		}
		if _, exists := evolution.steps[migration.From]; exists {
			return nil, fmt.Errorf("%w: version %s has multiple successors", ErrInvalidEvolution, migration.From)
		}
		evolution.steps[migration.From] = migration
	}
	for start := range evolution.steps {
		seen := make(map[Version]bool)
		for version := start; evolution.steps[version].To != ""; version = evolution.steps[version].To {
			if seen[version] {
				return nil, fmt.Errorf("%w: migration cycle at version %s", ErrInvalidEvolution, version)
			}
			seen[version] = true
		}
	}
	return evolution, nil
}

// Migrate converts a snapshot and history to target without mutating inputs.
func (evolution *Evolution[S, E]) Migrate(ctx context.Context, snapshot Snapshot[S], history []HistoryEntry[S, E], target Version) (Snapshot[S], []HistoryEntry[S, E], error) {
	if target == "" {
		return Snapshot[S]{}, nil, ErrInvalidEvolution
	}
	state, err := evolution.migrateState(ctx, snapshot.State, snapshot.DefinitionVersion, target)
	if err != nil {
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
	return snapshot, migrated, nil
}

func (evolution *Evolution[S, E]) migrateState(ctx context.Context, state S, from Version, target Version) (S, error) {
	for from != target {
		if err := ctx.Err(); err != nil {
			return state, err
		}
		migration, exists := evolution.steps[from]
		if !exists {
			return state, fmt.Errorf("%w: from %s to %s", ErrMissingMigration, from, target)
		}
		if migration.State != nil {
			migrated, err := migration.State(state)
			if err != nil {
				return state, &MigrationError{From: migration.From, To: migration.To, Field: "state", Cause: err}
			}
			state = migrated
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
			return Result[S, E]{}, fmt.Errorf("%w: from %s to %s", ErrMissingMigration, result.DefinitionVersion, target)
		}
		if migration.State != nil {
			previous, err := migration.State(result.Previous)
			if err != nil {
				return Result[S, E]{}, &MigrationError{From: migration.From, To: migration.To, Field: "previous state", Cause: err}
			}
			next, err := migration.State(result.Next)
			if err != nil {
				return Result[S, E]{}, &MigrationError{From: migration.From, To: migration.To, Field: "next state", Cause: err}
			}
			result.Previous, result.Next = previous, next
		}
		if migration.Event != nil {
			event, err := migration.Event(result.Event)
			if err != nil {
				return Result[S, E]{}, &MigrationError{From: migration.From, To: migration.To, Field: "event", Cause: err}
			}
			result.Event = event
		}
		result.DefinitionVersion = migration.To
	}
	result.Effects = cloneEffects(result.Effects)
	return result, nil
}
