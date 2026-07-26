package statemachine

import (
	"context"
	"errors"
	"time"
)

// InstanceID identifies one independently persisted machine instance.
type InstanceID string

// Instance is the current durable state of one machine instance.
type Instance[S State] struct {
	ID                InstanceID
	State             S
	DefinitionVersion Version
	LockVersion       uint64
}

// HistoryEntry is one append-only committed transition.
type HistoryEntry[S State, E Event] struct {
	InstanceID InstanceID
	Sequence   uint64
	Result     Result[S, E]
	OccurredAt time.Time
}

// Snapshot identifies a replay boundary at a committed lock version.
type Snapshot[S State] struct {
	InstanceID        InstanceID
	State             S
	DefinitionVersion Version
	LockVersion       uint64
	CreatedAt         time.Time
}

// StoreCapabilities reports guarantees instead of relying on implementation
// naming or type assertions.
type StoreCapabilities struct {
	AtomicCompareAndTransition bool
	AppendOnlyHistory          bool
	Snapshots                  bool
	AtomicOutbox               bool
}

const (
	// DefaultHistoryPageLimit is used when Store.History receives limit zero.
	DefaultHistoryPageLimit = 10_000
	// MaxHistoryPageLimit bounds one Store.History allocation.
	MaxHistoryPageLimit = 100_000
)

// Store persists current state, append-only history, and snapshots. History
// uses DefaultHistoryPageLimit for zero and rejects limits above
// MaxHistoryPageLimit.
type Store[S State, E Event] interface {
	Capabilities() StoreCapabilities
	Create(context.Context, Instance[S]) error
	Load(context.Context, InstanceID) (Instance[S], error)
	CompareAndTransition(context.Context, InstanceID, uint64, Result[S, E], time.Time) (Instance[S], HistoryEntry[S, E], error)
	History(context.Context, InstanceID, uint64, int) ([]HistoryEntry[S, E], error)
	SaveSnapshot(context.Context, Snapshot[S]) error
	LoadSnapshot(context.Context, InstanceID) (Snapshot[S], error)
}

var (
	// ErrStoreNotFound reports an unknown instance or absent snapshot.
	ErrStoreNotFound = errors.New("statemachine: store record not found")
	// ErrStoreExists reports an instance that has already been created.
	ErrStoreExists = errors.New("statemachine: store instance exists")
	// ErrStoreConflict reports an optimistic lock mismatch.
	ErrStoreConflict = errors.New("statemachine: store lock conflict")
	// ErrInvalidStoreInput reports a structurally inconsistent write.
	ErrInvalidStoreInput = errors.New("statemachine: invalid store input")
)
