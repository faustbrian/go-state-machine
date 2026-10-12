package statemachine

import (
	"context"
	"errors"
	"time"
)

// InstanceID identifies one independently persisted machine instance.
type InstanceID string

// Instance is the current stored state and optimistic sequence of one instance.
type Instance[S State] struct {
	// ID must be nonempty when creating an instance.
	ID InstanceID
	// State is application-selected and copied shallowly; stores do not compile it.
	State S
	// DefinitionVersion must be nonempty and records the current definition identity.
	DefinitionVersion Version
	// LockVersion is the optimistic sequence; Create requires zero.
	LockVersion uint64
}

// HistoryEntry is one append-only committed transition.
type HistoryEntry[S State, E Event] struct {
	// InstanceID identifies the owner of this committed result.
	InstanceID InstanceID
	// Sequence matches the lock version produced by the successful write.
	Sequence uint64
	// Result is the stored selection and effect plan, not proof of effect delivery.
	Result Result[S, E]
	// OccurredAt is the caller-supplied durable transition timestamp.
	OccurredAt time.Time
}

// Snapshot identifies a replay boundary at a committed lock version.
type Snapshot[S State] struct {
	// InstanceID identifies the history represented by this replay boundary.
	InstanceID InstanceID
	// State must match initial state or the committed result at LockVersion.
	State S
	// DefinitionVersion must match the definition recorded at that boundary.
	DefinitionVersion Version
	// LockVersion is the last included sequence; request History strictly after it.
	LockVersion uint64
	// CreatedAt is caller-supplied snapshot metadata, not a commit-order proof.
	CreatedAt time.Time
}

// StoreCapabilities reports guarantees instead of relying on implementation
// naming or type assertions.
type StoreCapabilities struct {
	// AtomicCompareAndTransition reports joint conditional state/history writes.
	AtomicCompareAndTransition bool
	// AppendOnlyHistory reports retained committed transition entries.
	AppendOnlyHistory bool
	// Snapshots reports validated replay-boundary storage.
	Snapshots bool
	// AtomicOutbox reports effect rows committed with state/history; it does not imply delivery.
	AtomicOutbox bool
}

const (
	// DefaultHistoryPageLimit is used when Store.History receives limit zero.
	DefaultHistoryPageLimit = 10_000
	// MaxHistoryPageLimit bounds returned entry count, not aggregate payload bytes.
	MaxHistoryPageLimit = 100_000
)

// Store persists current state, append-only history, and snapshots. History
// uses DefaultHistoryPageLimit for zero and rejects limits above
// MaxHistoryPageLimit. Contexts must be non-nil; keep typed referents and supplied
// result/payload data stable until an operation returns. Providers own their
// synchronization and document durability, budgets and cancellation differences.
type Store[S State, E Event] interface {
	// Capabilities reports provider guarantees without performing a transition.
	Capabilities() StoreCapabilities
	// Create inserts a nonempty identity/version at lock zero; duplicates are ErrStoreExists.
	Create(context.Context, Instance[S]) error
	// Load returns current state or ErrStoreNotFound for an unknown identity.
	Load(context.Context, InstanceID) (Instance[S], error)
	// CompareAndTransition checks expected lock and prior state, then atomically appends history.
	// It does not execute effects or prove that a Machine produced result.
	CompareAndTransition(context.Context, InstanceID, uint64, Result[S, E], time.Time) (Instance[S], HistoryEntry[S, E], error)
	// History returns ascending sequences strictly after the supplied boundary.
	// Zero selects the default page size; negative or excessive limits are invalid.
	History(context.Context, InstanceID, uint64, int) ([]HistoryEntry[S, E], error)
	// SaveSnapshot validates the boundary against stored initial/history state.
	// Older saved boundaries cannot replace newer ones; equal versions may be rewritten.
	SaveSnapshot(context.Context, Snapshot[S]) error
	// LoadSnapshot returns the saved boundary or ErrStoreNotFound when absent.
	LoadSnapshot(context.Context, InstanceID) (Snapshot[S], error)
}

var (
	// ErrStoreNotFound reports an unknown instance or absent snapshot.
	ErrStoreNotFound = errors.New("statemachine: store record not found")
	// ErrStoreExists reports an instance that has already been created.
	ErrStoreExists = errors.New("statemachine: store instance exists")
	// ErrStoreConflict reports a stale conditional write or backward snapshot.
	// PostgreSQL also classifies a mismatched prior-state condition as conflict.
	ErrStoreConflict = errors.New("statemachine: store lock conflict")
	// ErrInvalidStoreInput reports a structurally inconsistent write.
	ErrInvalidStoreInput = errors.New("statemachine: invalid store input")
)
