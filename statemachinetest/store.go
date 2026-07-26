// Package statemachinetest provides reusable conformance tests for state
// machine integrations.
package statemachinetest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
)

// StoreFixture supplies typed values used by StoreContract.
type StoreFixture[S statemachine.State, E statemachine.Event] struct {
	Instance statemachine.Instance[S]
	Result   statemachine.Result[S, E]
}

// StoreContract verifies the portable Store contract. Backend packages should
// call it from their own test suite in addition to backend-specific tests.
func StoreContract[S statemachine.State, E statemachine.Event](
	t *testing.T,
	factory func() statemachine.Store[S, E],
	fixture StoreFixture[S, E],
) {
	t.Helper()

	t.Run("capabilities", func(t *testing.T) {
		capabilities := factory().Capabilities()
		if !capabilities.AtomicCompareAndTransition || !capabilities.AppendOnlyHistory || !capabilities.Snapshots {
			t.Fatalf("required capabilities missing: %#v", capabilities)
		}
	})

	t.Run("lifecycle", func(t *testing.T) {
		ctx := context.Background()
		store := factory()
		if err := store.Create(ctx, fixture.Instance); err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := store.Create(ctx, fixture.Instance); !errors.Is(err, statemachine.ErrStoreExists) {
			t.Fatalf("duplicate create error = %v, want ErrStoreExists", err)
		}
		loaded, err := store.Load(ctx, fixture.Instance.ID)
		if err != nil || !reflect.DeepEqual(loaded, fixture.Instance) {
			t.Fatalf("load = %#v, %v", loaded, err)
		}

		at := time.Unix(456, 0).UTC()
		instance, entry, err := store.CompareAndTransition(ctx, fixture.Instance.ID, 0, fixture.Result, at)
		if err != nil {
			t.Fatalf("compare and transition: %v", err)
		}
		if instance.State != fixture.Result.Next || instance.LockVersion != 1 || entry.Sequence != 1 {
			t.Fatalf("instance = %#v, entry = %#v", instance, entry)
		}
		if _, _, err := store.CompareAndTransition(ctx, fixture.Instance.ID, 0, fixture.Result, at); !errors.Is(err, statemachine.ErrStoreConflict) {
			t.Fatalf("stale transition error = %v, want ErrStoreConflict", err)
		}

		history, err := store.History(ctx, fixture.Instance.ID, 0, 1)
		if err != nil || len(history) != 1 || !reflect.DeepEqual(history[0].Result, fixture.Result) {
			t.Fatalf("history = %#v, %v", history, err)
		}
		snapshot := statemachine.Snapshot[S]{
			InstanceID:        fixture.Instance.ID,
			State:             fixture.Result.Next,
			DefinitionVersion: fixture.Result.DefinitionVersion,
			LockVersion:       1,
			CreatedAt:         at,
		}
		if err := store.SaveSnapshot(ctx, snapshot); err != nil {
			t.Fatalf("save snapshot: %v", err)
		}
		loadedSnapshot, err := store.LoadSnapshot(ctx, fixture.Instance.ID)
		if err != nil || loadedSnapshot.InstanceID != snapshot.InstanceID ||
			loadedSnapshot.State != snapshot.State ||
			loadedSnapshot.DefinitionVersion != snapshot.DefinitionVersion ||
			loadedSnapshot.LockVersion != snapshot.LockVersion ||
			!loadedSnapshot.CreatedAt.Equal(snapshot.CreatedAt) {
			t.Fatalf("snapshot = %#v, %v", loadedSnapshot, err)
		}
	})

	t.Run("not_found", func(t *testing.T) {
		ctx := context.Background()
		store := factory()
		if _, err := store.Load(ctx, ""); !errors.Is(err, statemachine.ErrStoreNotFound) {
			t.Fatalf("load error = %v, want ErrStoreNotFound", err)
		}
		if _, err := store.LoadSnapshot(ctx, ""); !errors.Is(err, statemachine.ErrStoreNotFound) {
			t.Fatalf("snapshot error = %v, want ErrStoreNotFound", err)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := factory().Create(ctx, fixture.Instance); !errors.Is(err, context.Canceled) {
			t.Fatalf("create error = %v, want context.Canceled", err)
		}
	})
}
