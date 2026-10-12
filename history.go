package statemachine

import "fmt"

// HistoryFailure classifies corruption without rendering event or state data.
type HistoryFailure string

const (
	// HistorySequenceMismatch reports a nonconsecutive stored lock sequence.
	HistorySequenceMismatch HistoryFailure = "sequence_mismatch"
	// HistoryStateMismatch reports a prior state inconsistent with the preceding boundary.
	HistoryStateMismatch HistoryFailure = "state_mismatch"
	// HistoryInstanceMismatch reports an entry belonging to another instance.
	HistoryInstanceMismatch HistoryFailure = "instance_mismatch"
	// HistoryMissingIdentity reports empty snapshot or result identities.
	HistoryMissingIdentity HistoryFailure = "missing_identity"
	// HistoryLimitExceeded reports invalid entry limits or excess history length.
	HistoryLimitExceeded HistoryFailure = "limit_exceeded"
	// HistoryDefinitionMismatch reports version or selected-transition topology mismatch.
	HistoryDefinitionMismatch HistoryFailure = "definition_mismatch"
)

// HistoryError identifies the zero-based entry that violates replay
// continuity. Index is -1 when the snapshot itself is malformed.
type HistoryError struct {
	// Index is the zero-based entry, or -1 for a snapshot/limit failure.
	Index int
	// Failure is a structured continuity or machine-selection classification.
	Failure HistoryFailure
}

// ValidateHistory checks continuity and this machine version, selected event
// transition ID and destination. Older versions must be migrated first. It does
// not rerun guards, verify effect payloads or authenticate history. In particular,
// wildcard records are not checked for terminal or unknown source eligibility;
// passing this check is narrower than proving a valid live Transition execution.
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

// Error includes the entry index, not state/event data or the Failure label.
func (err *HistoryError) Error() string {
	return fmt.Sprintf("statemachine: history entry %d is invalid", err.Index)
}

// ValidateHistory verifies append-only continuity from snapshot and returns
// the reconstructed final snapshot without executing effects or guards.
func ValidateHistory[S State, E Event](snapshot Snapshot[S], entries []HistoryEntry[S, E]) (Snapshot[S], error) {
	return ValidateHistoryWithLimit(snapshot, entries, DefaultLimits().MaxReplayInputs)
}

// ValidateHistoryWithLimit checks nonempty identities, instance/sequence/prior
// state continuity, and a positive entry bound. Entry definition versions may
// change. It returns zero output on failure, does not mutate entries or run
// guards/effects, and does not prove machine membership or authenticate data.
// uint64 sequence arithmetic is not overflow-rejecting; callers own that range.
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
