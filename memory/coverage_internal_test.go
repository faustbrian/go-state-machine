package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
)

func TestStoreRemainingValidationAndCancellation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := New[string, string]()
	invalid := []statemachine.Instance[string]{
		{DefinitionVersion: "v1"},
		{ID: "one"},
		{ID: "one", DefinitionVersion: "v1", LockVersion: 1},
	}
	for _, instance := range invalid {
		if err := store.Create(ctx, instance); !errors.Is(err, statemachine.ErrInvalidStoreInput) {
			t.Fatalf("create error = %v", err)
		}
	}
	if err := store.Create(ctx, statemachine.Instance[string]{ID: "one", State: "a", DefinitionVersion: "v1"}); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.Load(canceled, "one"); !errors.Is(err, context.Canceled) {
		t.Fatalf("load cancellation = %v", err)
	}
	if _, _, err := store.CompareAndTransition(canceled, "one", 0, statemachine.Result[string, string]{}, time.Time{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("transition cancellation = %v", err)
	}
	if _, _, err := store.CompareAndTransition(ctx, "missing", 0, statemachine.Result[string, string]{}, time.Time{}); !errors.Is(err, statemachine.ErrStoreNotFound) {
		t.Fatalf("missing transition = %v", err)
	}
	if _, _, err := store.CompareAndTransition(ctx, "one", 0, statemachine.Result[string, string]{Previous: "wrong", DefinitionVersion: "v1"}, time.Time{}); !errors.Is(err, statemachine.ErrInvalidStoreInput) {
		t.Fatalf("invalid transition = %v", err)
	}
	if _, err := store.History(canceled, "one", 0, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("history cancellation = %v", err)
	}
	if _, err := store.History(ctx, "one", 0, -1); !errors.Is(err, statemachine.ErrInvalidStoreInput) {
		t.Fatalf("history limit = %v", err)
	}
	if _, err := store.History(ctx, "one", 0, statemachine.MaxHistoryPageLimit+1); !errors.Is(err, statemachine.ErrInvalidStoreInput) {
		t.Fatalf("oversized history limit = %v", err)
	}
	if _, err := store.History(ctx, "missing", 0, 1); !errors.Is(err, statemachine.ErrStoreNotFound) {
		t.Fatalf("missing history = %v", err)
	}
	if entries, err := store.History(ctx, "one", 0, 0); err != nil || len(entries) != 0 {
		t.Fatalf("empty history = %#v, %v", entries, err)
	}
	if err := store.SaveSnapshot(canceled, statemachine.Snapshot[string]{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("snapshot cancellation = %v", err)
	}
	if err := store.SaveSnapshot(ctx, statemachine.Snapshot[string]{InstanceID: "missing"}); !errors.Is(err, statemachine.ErrStoreNotFound) {
		t.Fatalf("missing snapshot instance = %v", err)
	}
	if err := store.SaveSnapshot(ctx, statemachine.Snapshot[string]{InstanceID: "one", State: "a", LockVersion: 1}); !errors.Is(err, statemachine.ErrInvalidStoreInput) {
		t.Fatalf("invalid snapshot = %v", err)
	}
	if _, err := store.LoadSnapshot(canceled, "one"); !errors.Is(err, context.Canceled) {
		t.Fatalf("load snapshot cancellation = %v", err)
	}
}

func TestStoreSnapshotProgressAndHistoryPaging(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := New[string, string]()
	if err := store.Create(ctx, statemachine.Instance[string]{ID: "one", State: "a", DefinitionVersion: "v1"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSnapshot(ctx, statemachine.Snapshot[string]{InstanceID: "one", State: "a", DefinitionVersion: "v1"}); err != nil {
		t.Fatal(err)
	}
	_, _, err := store.CompareAndTransition(ctx, "one", 0, statemachine.Result[string, string]{
		DefinitionVersion: "v2", Previous: "a", Next: "b", Event: "go", TransitionID: "go",
	}, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSnapshot(ctx, statemachine.Snapshot[string]{
		InstanceID: "one", State: "b", DefinitionVersion: "v2", LockVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSnapshot(ctx, statemachine.Snapshot[string]{
		InstanceID: "one", State: "a", DefinitionVersion: "v1", LockVersion: 0,
	}); !errors.Is(err, statemachine.ErrStoreConflict) {
		t.Fatalf("backward snapshot = %v", err)
	}
	if entries, err := store.History(ctx, "one", 1, 1); err != nil || len(entries) != 0 {
		t.Fatalf("history after final = %#v, %v", entries, err)
	}
	loaded, err := store.LoadSnapshot(ctx, "one")
	if err != nil || loaded.State != "b" {
		t.Fatalf("snapshot = %#v, %v", loaded, err)
	}
}
