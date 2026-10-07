package postgres

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	statemachine "github.com/faustbrian/go-state-machine/v2"
	"github.com/faustbrian/go-state-machine/v2/outbox"
)

// The database seam deliberately returns values outside the owned schema's
// constraints. These tests establish defensive row admission, not SQL behavior.
func TestPersistedSignedVersions(t *testing.T) {
	for _, snapshot := range []bool{false, true} {
		for _, version := range []int64{-1, 0, 1, math.MaxInt64} {
			db := fakeDatabase{queryRow: func(context.Context, string, ...any) row {
				return fakeRow{scan: func(dest ...any) error {
					*dest[0].(*string) = "a"
					*dest[1].(*string) = "v1"
					*dest[2].(*int64) = version
					if snapshot {
						*dest[3].(*time.Time) = time.Unix(1, 0)
					}
					return nil
				}}
			}}
			store := fakeStore(db)
			decoded := false
			store.stateCodec.Decode = func(s string) (string, error) { decoded = true; return s, nil }
			var got uint64
			var err error
			if snapshot {
				result, loadErr := store.LoadSnapshot(t.Context(), "one")
				got, err = result.LockVersion, loadErr
			} else {
				result, loadErr := store.Load(t.Context(), "one")
				got, err = result.LockVersion, loadErr
			}
			if version < 0 {
				if !errors.Is(err, statemachine.ErrInvalidStoreInput) || got != 0 || decoded {
					t.Errorf("snapshot=%v: negative version crossed admission", snapshot)
				}
			} else if err != nil || got != uint64(version) || !decoded {
				t.Errorf("snapshot=%v: valid signed version was refused or changed", snapshot)
			}
		}
	}
}

func TestTransitionSignedSequenceAdmission(t *testing.T) {
	for _, sequence := range []int64{-1, 0, 1, math.MaxInt64} {
		tx := baseTransaction()
		tx.queryRow = func(context.Context, string, ...any) row {
			return fakeRow{scan: func(dest ...any) error { *dest[0].(*int64) = sequence; return nil }}
		}
		inserted, committed, rolledBack := false, false, false
		baseExec := tx.exec
		tx.exec = func(ctx context.Context, query string, values ...any) (commandResult, error) {
			inserted = true
			return baseExec(ctx, query, values...)
		}
		tx.commit = func(context.Context) error { committed = true; return nil }
		tx.rollback = func(context.Context) error { rolledBack = true; return nil }
		store := fakeStore(fakeDatabase{begin: func(context.Context) (transaction, error) { return tx, nil }})
		result := statemachine.Result[string, string]{DefinitionVersion: "v1", Previous: "a", Next: "b", Event: "go", TransitionID: "go", Effects: []statemachine.Effect{{Kind: "publish"}}}
		instance, history, err := store.CompareAndTransition(t.Context(), "one", 0, result, time.Unix(1, 0))
		if sequence <= 0 {
			if !errors.Is(err, statemachine.ErrInvalidStoreInput) || instance.LockVersion != 0 || history.Sequence != 0 || inserted || committed || !rolledBack {
				t.Error("invalid returned sequence crossed write/commit boundary")
			}
		} else if err != nil || instance.LockVersion != uint64(sequence) || history.Sequence != uint64(sequence) || !inserted || !committed {
			t.Error("positive returned sequence was refused or changed")
		}
	}
}

func TestHistorySignedSequenceAdmission(t *testing.T) {
	for _, sequence := range []int64{-1, 0, 1, math.MaxInt64} {
		rowset := &fakeRows{next: []bool{true}, scan: func(dest ...any) error {
			*dest[0].(*int64) = sequence
			*dest[1].(*[]byte) = []byte(`{"definition_version":"v1","previous":"a","next":"b","event":"go","transition_id":"go"}`)
			*dest[2].(*time.Time) = time.Unix(1, 0)
			return nil
		}}
		store := fakeStore(fakeDatabase{query: func(context.Context, string, ...any) (rows, error) { return rowset, nil }})
		decoded := false
		store.eventCodec.Decode = func(s string) (string, error) { decoded = true; return s, nil }
		entries, err := store.History(t.Context(), "one", 0, 1)
		if sequence <= 0 {
			if !errors.Is(err, statemachine.ErrInvalidStoreInput) || len(entries) != 0 || decoded || !rowset.closed {
				t.Error("invalid history sequence crossed decode/output boundary")
			}
		} else if err != nil || len(entries) != 1 || entries[0].Sequence != uint64(sequence) || !rowset.closed {
			t.Error("positive history sequence was refused or changed")
		}
	}
}

func TestClaimSignedSequencePreserved(t *testing.T) {
	for _, sequence := range []int64{-1, 0, 1, math.MaxInt64} {
		tx := baseTransaction()
		rowset := &fakeRows{next: []bool{true}, scan: func(dest ...any) error {
			*dest[0].(*string) = "id"
			*dest[1].(*string) = "one"
			*dest[2].(*int64) = sequence
			*dest[3].(*int) = 0
			*dest[4].(*string) = "publish"
			*dest[5].(*[]byte) = []byte("value")
			*dest[6].(*time.Time) = time.Unix(1, 0)
			*dest[7].(*int) = 0
			return nil
		}}
		tx.query = func(context.Context, string, ...any) (rows, error) { return rowset, nil }
		leased, committed, rolledBack := false, false, false
		baseExec := tx.exec
		tx.exec = func(ctx context.Context, query string, values ...any) (commandResult, error) {
			leased = true
			return baseExec(ctx, query, values...)
		}
		tx.commit = func(context.Context) error { committed = true; return nil }
		tx.rollback = func(context.Context) error { rolledBack = true; return nil }
		store := fakeStore(fakeDatabase{begin: func(context.Context) (transaction, error) { return tx, nil }})
		claims, err := store.Claim(t.Context(), outbox.ClaimRequest{Owner: "worker", Limit: 1, LeaseDuration: time.Second})
		if sequence <= 0 {
			if !errors.Is(err, statemachine.ErrInvalidStoreInput) || len(claims) != 0 || leased || committed || !rolledBack || !rowset.closed {
				t.Error("invalid claim sequence crossed lease boundary")
			}
		} else if err != nil || len(claims) != 1 || claims[0].Message.Sequence != uint64(sequence) || !leased || !committed || !rowset.closed {
			t.Error("positive claim sequence was refused or changed")
		}
	}
}
