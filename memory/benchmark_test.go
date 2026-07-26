package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
	"github.com/faustbrian/golib/pkg/state-machine/memory"
)

func BenchmarkHistoryGrowth(b *testing.B) {
	ctx := context.Background()
	store := memory.New[bool, bool]()
	if err := store.Create(ctx, statemachine.Instance[bool]{ID: "machine", DefinitionVersion: "v1"}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := range b.N {
		previous := index%2 != 0
		_, _, err := store.CompareAndTransition(ctx, "machine", uint64(index), statemachine.Result[bool, bool]{
			DefinitionVersion: "v1", Previous: previous, Next: !previous,
			Event: !previous, TransitionID: "toggle",
		}, time.Time{})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkContendedPersistence(b *testing.B) {
	ctx := context.Background()
	store := memory.New[bool, bool]()
	if err := store.Create(ctx, statemachine.Instance[bool]{ID: "machine", DefinitionVersion: "v1"}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.RunParallel(func(parallel *testing.PB) {
		for parallel.Next() {
			for {
				instance, err := store.Load(ctx, "machine")
				if err != nil {
					b.Error(err)
					return
				}
				_, _, err = store.CompareAndTransition(ctx, "machine", instance.LockVersion, statemachine.Result[bool, bool]{
					DefinitionVersion: "v1", Previous: instance.State, Next: !instance.State,
					Event: !instance.State, TransitionID: "toggle",
				}, time.Time{})
				if err == nil {
					break
				}
				if !errors.Is(err, statemachine.ErrStoreConflict) {
					b.Error(err)
					return
				}
			}
		}
	})
}
