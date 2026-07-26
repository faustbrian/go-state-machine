package statemachine

import "fmt"

// HistoryFailure classifies corruption without rendering event or state data.
type HistoryFailure string

const (
	HistorySequenceMismatch   HistoryFailure = "sequence_mismatch"
	HistoryStateMismatch      HistoryFailure = "state_mismatch"
	HistoryInstanceMismatch   HistoryFailure = "instance_mismatch"
	HistoryMissingIdentity    HistoryFailure = "missing_identity"
	HistoryLimitExceeded      HistoryFailure = "limit_exceeded"
	HistoryDefinitionMismatch HistoryFailure = "definition_mismatch"
)

// HistoryError identifies the zero-based entry that violates replay
// continuity. Index is -1 when the snapshot itself is malformed.
type HistoryError struct {
	Index   int
	Failure HistoryFailure
}

// ValidateHistory verifies structural continuity and proves that every stored
// result is compatible with this exact compiled definition. History from an
// older definition must be migrated before validation.
func (machine *Machine[S, E, C]) ValidateHistory(snapshot Snapshot[S], entries []HistoryEntry[S, E]) (Snapshot[S], error) {
	if snapshot.DefinitionVersion != machine.version {
		return Snapshot[S]{}, &HistoryError{Index: -1, Failure: HistoryDefinitionMismatch}
	}
	final, err := ValidateHistoryWithLimit(snapshot, entries, machine.limits.MaxReplayInputs)
	if err != nil {
		return Snapshot[S]{}, err
	}
	for index, entry := range entries {
		result := entry.Result
		if result.DefinitionVersion != machine.version {
			return Snapshot[S]{}, &HistoryError{Index: index, Failure: HistoryDefinitionMismatch}
		}
		transition, exists := machine.exact[result.Previous][result.Event]
		if !exists {
			transition, exists = machine.wildcard[result.Event]
		}
		if !exists || transition.ID != result.TransitionID || transition.To != result.Next {
			return Snapshot[S]{}, &HistoryError{Index: index, Failure: HistoryDefinitionMismatch}
		}
	}
	return final, nil
}

func (err *HistoryError) Error() string {
	return fmt.Sprintf("statemachine: history entry %d: %s", err.Index, err.Failure)
}

// ValidateHistory verifies append-only continuity from snapshot and returns
// the reconstructed final snapshot without executing effects or guards.
func ValidateHistory[S State, E Event](snapshot Snapshot[S], entries []HistoryEntry[S, E]) (Snapshot[S], error) {
	return ValidateHistoryWithLimit(snapshot, entries, DefaultLimits().MaxReplayInputs)
}

// ValidateHistoryWithLimit verifies history with an explicit entry bound.
func ValidateHistoryWithLimit[S State, E Event](snapshot Snapshot[S], entries []HistoryEntry[S, E], limit int) (Snapshot[S], error) {
	if snapshot.InstanceID == "" || snapshot.DefinitionVersion == "" {
		return Snapshot[S]{}, &HistoryError{Index: -1, Failure: HistoryMissingIdentity}
	}
	if limit <= 0 || len(entries) > limit {
		return Snapshot[S]{}, &HistoryError{Index: -1, Failure: HistoryLimitExceeded}
	}
	current := snapshot
	for index, entry := range entries {
		if entry.InstanceID != snapshot.InstanceID {
			return Snapshot[S]{}, &HistoryError{Index: index, Failure: HistoryInstanceMismatch}
		}
		if entry.Sequence != current.LockVersion+1 {
			return Snapshot[S]{}, &HistoryError{Index: index, Failure: HistorySequenceMismatch}
		}
		if entry.Result.DefinitionVersion == "" || entry.Result.TransitionID == "" {
			return Snapshot[S]{}, &HistoryError{Index: index, Failure: HistoryMissingIdentity}
		}
		if entry.Result.Previous != current.State {
			return Snapshot[S]{}, &HistoryError{Index: index, Failure: HistoryStateMismatch}
		}
		current.State = entry.Result.Next
		current.DefinitionVersion = entry.Result.DefinitionVersion
		current.LockVersion = entry.Sequence
		current.CreatedAt = entry.OccurredAt
	}
	return current, nil
}
