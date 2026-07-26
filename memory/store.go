// Package memory provides an in-process Store implementation for tests,
// development, and ephemeral applications.
package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
)

// Store is a concurrency-safe in-memory state machine store.
type Store[S statemachine.State, E statemachine.Event] struct {
	mu        sync.RWMutex
	instances map[statemachine.InstanceID]statemachine.Instance[S]
	initial   map[statemachine.InstanceID]statemachine.Instance[S]
	history   map[statemachine.InstanceID][]statemachine.HistoryEntry[S, E]
	snapshots map[statemachine.InstanceID]statemachine.Snapshot[S]
}

// New constructs an empty store.
func New[S statemachine.State, E statemachine.Event]() *Store[S, E] {
	return &Store[S, E]{
		instances: make(map[statemachine.InstanceID]statemachine.Instance[S]),
		initial:   make(map[statemachine.InstanceID]statemachine.Instance[S]),
		history:   make(map[statemachine.InstanceID][]statemachine.HistoryEntry[S, E]),
		snapshots: make(map[statemachine.InstanceID]statemachine.Snapshot[S]),
	}
}

// Capabilities reports the atomic and replay guarantees provided by Store.
func (store *Store[S, E]) Capabilities() statemachine.StoreCapabilities {
	return statemachine.StoreCapabilities{
		AtomicCompareAndTransition: true,
		AppendOnlyHistory:          true,
		Snapshots:                  true,
		AtomicOutbox:               false,
	}
}

// Create inserts an instance at lock version zero.
func (store *Store[S, E]) Create(ctx context.Context, instance statemachine.Instance[S]) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if instance.ID == "" || instance.DefinitionVersion == "" || instance.LockVersion != 0 {
		return statemachine.ErrInvalidStoreInput
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.instances[instance.ID]; exists {
		return statemachine.ErrStoreExists
	}
	store.instances[instance.ID] = instance
	store.initial[instance.ID] = instance
	return nil
}

// Load returns the current state of an instance.
func (store *Store[S, E]) Load(ctx context.Context, id statemachine.InstanceID) (statemachine.Instance[S], error) {
	if err := ctx.Err(); err != nil {
		return statemachine.Instance[S]{}, err
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	instance, exists := store.instances[id]
	if !exists {
		return statemachine.Instance[S]{}, statemachine.ErrStoreNotFound
	}
	return instance, nil
}

// CompareAndTransition atomically checks the lock and prior state, updates the
// current state, and appends history under one mutex boundary.
func (store *Store[S, E]) CompareAndTransition(ctx context.Context, id statemachine.InstanceID, expected uint64, result statemachine.Result[S, E], occurredAt time.Time) (statemachine.Instance[S], statemachine.HistoryEntry[S, E], error) {
	if err := ctx.Err(); err != nil {
		return statemachine.Instance[S]{}, statemachine.HistoryEntry[S, E]{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	instance, exists := store.instances[id]
	if !exists {
		return statemachine.Instance[S]{}, statemachine.HistoryEntry[S, E]{}, statemachine.ErrStoreNotFound
	}
	if instance.LockVersion != expected {
		return statemachine.Instance[S]{}, statemachine.HistoryEntry[S, E]{}, statemachine.ErrStoreConflict
	}
	if result.DefinitionVersion == "" || result.Previous != instance.State {
		return statemachine.Instance[S]{}, statemachine.HistoryEntry[S, E]{}, statemachine.ErrInvalidStoreInput
	}
	result = cloneResult(result)
	instance.State = result.Next
	instance.DefinitionVersion = result.DefinitionVersion
	instance.LockVersion++
	entry := statemachine.HistoryEntry[S, E]{
		InstanceID: id,
		Sequence:   instance.LockVersion,
		Result:     result,
		OccurredAt: occurredAt,
	}
	store.instances[id] = instance
	store.history[id] = append(store.history[id], entry)
	return instance, cloneEntry(entry), nil
}

// History returns entries with a sequence strictly greater than after.
func (store *Store[S, E]) History(ctx context.Context, id statemachine.InstanceID, after uint64, limit int) ([]statemachine.HistoryEntry[S, E], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit < 0 || limit > statemachine.MaxHistoryPageLimit {
		return nil, statemachine.ErrInvalidStoreInput
	}
	if limit == 0 {
		limit = statemachine.DefaultHistoryPageLimit
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	if _, exists := store.instances[id]; !exists {
		return nil, statemachine.ErrStoreNotFound
	}
	entries := store.history[id]
	result := make([]statemachine.HistoryEntry[S, E], 0)
	for _, entry := range entries {
		if entry.Sequence <= after {
			continue
		}
		result = append(result, cloneEntry(entry))
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

// SaveSnapshot validates and stores the latest replay boundary.
func (store *Store[S, E]) SaveSnapshot(ctx context.Context, snapshot statemachine.Snapshot[S]) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	instance, exists := store.instances[snapshot.InstanceID]
	if !exists {
		return statemachine.ErrStoreNotFound
	}
	if snapshot.DefinitionVersion == "" || snapshot.LockVersion > instance.LockVersion {
		return statemachine.ErrInvalidStoreInput
	}
	expectedState := store.initial[snapshot.InstanceID].State
	expectedVersion := store.initial[snapshot.InstanceID].DefinitionVersion
	if snapshot.LockVersion != 0 {
		entry := store.history[snapshot.InstanceID][snapshot.LockVersion-1]
		expectedState = entry.Result.Next
		expectedVersion = entry.Result.DefinitionVersion
	}
	if snapshot.State != expectedState || snapshot.DefinitionVersion != expectedVersion {
		return statemachine.ErrInvalidStoreInput
	}
	if current, exists := store.snapshots[snapshot.InstanceID]; exists && snapshot.LockVersion < current.LockVersion {
		return fmt.Errorf("%w: snapshot would move backward", statemachine.ErrStoreConflict)
	}
	store.snapshots[snapshot.InstanceID] = snapshot
	return nil
}

// LoadSnapshot returns the latest saved snapshot.
func (store *Store[S, E]) LoadSnapshot(ctx context.Context, id statemachine.InstanceID) (statemachine.Snapshot[S], error) {
	if err := ctx.Err(); err != nil {
		return statemachine.Snapshot[S]{}, err
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	snapshot, exists := store.snapshots[id]
	if !exists {
		return statemachine.Snapshot[S]{}, statemachine.ErrStoreNotFound
	}
	return snapshot, nil
}

func cloneResult[S statemachine.State, E statemachine.Event](result statemachine.Result[S, E]) statemachine.Result[S, E] {
	result.Effects = append([]statemachine.Effect(nil), result.Effects...)
	for index := range result.Effects {
		result.Effects[index].Payload = append([]byte(nil), result.Effects[index].Payload...)
	}
	return result
}

func cloneEntry[S statemachine.State, E statemachine.Event](entry statemachine.HistoryEntry[S, E]) statemachine.HistoryEntry[S, E] {
	entry.Result = cloneResult(entry.Result)
	return entry
}
