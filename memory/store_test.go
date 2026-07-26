package memory_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
	"github.com/faustbrian/golib/pkg/state-machine/memory"
	"github.com/faustbrian/golib/pkg/state-machine/statemachinetest"
)

type state string
type event string

func TestStoreContract(t *testing.T) {
	statemachinetest.StoreContract(t,
		func() statemachine.Store[state, event] { return memory.New[state, event]() },
		statemachinetest.StoreFixture[state, event]{
			Instance: statemachine.Instance[state]{
				ID: "contract-1", State: "pending", DefinitionVersion: "v1",
			},
			Result: statemachine.Result[state, event]{
				DefinitionVersion: "v1", Previous: "pending", Next: "paid",
				Event: "pay", TransitionID: "pay",
			},
		},
	)
}

func TestStoreAtomicallyTransitionsAndAppendsHistory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New[state, event]()
	if err := store.Create(ctx, statemachine.Instance[state]{
		ID: "order-1", State: "pending", DefinitionVersion: "v1",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	result := statemachine.Result[state, event]{
		DefinitionVersion: "v1",
		Previous:          "pending",
		Next:              "paid",
		Event:             "pay",
		TransitionID:      "pay-order",
		Metadata:          statemachine.Metadata{CorrelationID: "cor-1"},
		Effects:           []statemachine.Effect{{Kind: "receipt", Payload: []byte("original")}},
	}
	at := time.Unix(123, 0).UTC()

	instance, entry, err := store.CompareAndTransition(ctx, "order-1", 0, result, at)
	if err != nil {
		t.Fatalf("compare and transition: %v", err)
	}
	if instance.State != "paid" || instance.LockVersion != 1 || entry.Sequence != 1 || !entry.OccurredAt.Equal(at) {
		t.Fatalf("instance = %#v, entry = %#v", instance, entry)
	}
	result.Effects[0].Payload[0] = 'X'
	entry.Result.Effects[0].Payload[0] = 'Y'

	history, err := store.History(ctx, "order-1", 0, 10)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 1 || string(history[0].Result.Effects[0].Payload) != "original" {
		t.Fatalf("history = %#v", history)
	}

	_, _, err = store.CompareAndTransition(ctx, "order-1", 0, result, at)
	if !errors.Is(err, statemachine.ErrStoreConflict) {
		t.Fatalf("stale transition error = %v, want ErrStoreConflict", err)
	}
}

func TestStorePreventsLostUpdatesUnderContention(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New[state, event]()
	if err := store.Create(ctx, statemachine.Instance[state]{
		ID: "order-1", State: "pending", DefinitionVersion: "v1",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	result := statemachine.Result[state, event]{
		DefinitionVersion: "v1", Previous: "pending", Next: "paid", Event: "pay", TransitionID: "pay",
	}

	var successes atomic.Int32
	var conflicts atomic.Int32
	var wait sync.WaitGroup
	for range 20 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, _, err := store.CompareAndTransition(ctx, "order-1", 0, result, time.Now())
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, statemachine.ErrStoreConflict):
				conflicts.Add(1)
			default:
				t.Errorf("unexpected transition error: %v", err)
			}
		}()
	}
	wait.Wait()

	if successes.Load() != 1 || conflicts.Load() != 19 {
		t.Fatalf("successes = %d, conflicts = %d", successes.Load(), conflicts.Load())
	}
}

func TestStoreRejectsSnapshotThatDoesNotMatchHistory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New[state, event]()
	if err := store.Create(ctx, statemachine.Instance[state]{
		ID: "order-1", State: "pending", DefinitionVersion: "v1",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, _, err := store.CompareAndTransition(ctx, "order-1", 0, statemachine.Result[state, event]{
		DefinitionVersion: "v1", Previous: "pending", Next: "paid", Event: "pay", TransitionID: "pay",
	}, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("transition: %v", err)
	}

	err = store.SaveSnapshot(ctx, statemachine.Snapshot[state]{
		InstanceID: "order-1", State: "corrupt", DefinitionVersion: "v1", LockVersion: 1,
	})
	if !errors.Is(err, statemachine.ErrInvalidStoreInput) {
		t.Fatalf("save corrupt snapshot error = %v, want ErrInvalidStoreInput", err)
	}
}
