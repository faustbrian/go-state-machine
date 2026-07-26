package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
	"github.com/faustbrian/golib/pkg/state-machine/outbox"
)

type fakeCommandResult int64

func (result fakeCommandResult) RowsAffected() int64 { return int64(result) }

type fakeRow struct{ scan func(...any) error }

func (row fakeRow) Scan(destinations ...any) error { return row.scan(destinations...) }

type fakeRows struct {
	next   []bool
	index  int
	scan   func(...any) error
	err    error
	closed bool
}

func (rows *fakeRows) Close()                   { rows.closed = true }
func (rows *fakeRows) Err() error               { return rows.err }
func (rows *fakeRows) Scan(values ...any) error { return rows.scan(values...) }
func (rows *fakeRows) Next() bool {
	if rows.index >= len(rows.next) {
		return false
	}
	value := rows.next[rows.index]
	rows.index++
	return value
}

type fakeTransaction struct {
	exec     func(context.Context, string, ...any) (commandResult, error)
	query    func(context.Context, string, ...any) (rows, error)
	queryRow func(context.Context, string, ...any) row
	commit   func(context.Context) error
	rollback func(context.Context) error
}

func (tx fakeTransaction) Exec(ctx context.Context, sql string, values ...any) (commandResult, error) {
	return tx.exec(ctx, sql, values...)
}
func (tx fakeTransaction) Query(ctx context.Context, sql string, values ...any) (rows, error) {
	return tx.query(ctx, sql, values...)
}
func (tx fakeTransaction) QueryRow(ctx context.Context, sql string, values ...any) row {
	return tx.queryRow(ctx, sql, values...)
}
func (tx fakeTransaction) Commit(ctx context.Context) error   { return tx.commit(ctx) }
func (tx fakeTransaction) Rollback(ctx context.Context) error { return tx.rollback(ctx) }

type fakeDatabase struct {
	exec     func(context.Context, string, ...any) (commandResult, error)
	query    func(context.Context, string, ...any) (rows, error)
	queryRow func(context.Context, string, ...any) row
	begin    func(context.Context) (transaction, error)
}

func (database fakeDatabase) Exec(ctx context.Context, sql string, values ...any) (commandResult, error) {
	return database.exec(ctx, sql, values...)
}
func (database fakeDatabase) Query(ctx context.Context, sql string, values ...any) (rows, error) {
	return database.query(ctx, sql, values...)
}
func (database fakeDatabase) QueryRow(ctx context.Context, sql string, values ...any) row {
	return database.queryRow(ctx, sql, values...)
}
func (database fakeDatabase) Begin(ctx context.Context) (transaction, error) {
	return database.begin(ctx)
}

func fakeStore(database database) *Store[string, string] {
	return &Store[string, string]{
		pool: database, schema: "test", stateCodec: TextCodec[string](),
		eventCodec: TextCodec[string](), newID: func() string { return "id" },
		clock: time.Now, marshal: json.Marshal,
	}
}

func baseTransaction() fakeTransaction {
	return fakeTransaction{
		exec: func(context.Context, string, ...any) (commandResult, error) { return fakeCommandResult(1), nil },
		query: func(context.Context, string, ...any) (rows, error) {
			return &fakeRows{scan: func(...any) error { return nil }}, nil
		},
		queryRow: func(context.Context, string, ...any) row { return fakeRow{scan: func(...any) error { return nil }} },
		commit:   func(context.Context) error { return nil }, rollback: func(context.Context) error { return nil },
	}
}

func TestCompareAndTransitionDriverFailures(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("driver failed")
	result := statemachine.Result[string, string]{
		DefinitionVersion: "v1", Previous: "a", Next: "b", Event: "go", TransitionID: "go",
	}
	tests := []struct {
		name   string
		mutate func(*fakeTransaction)
	}{
		{"update", func(tx *fakeTransaction) {
			tx.queryRow = func(context.Context, string, ...any) row { return fakeRow{scan: func(...any) error { return wantErr }} }
		}},
		{"history", func(tx *fakeTransaction) {
			tx.queryRow = lockingRow
			tx.exec = func(context.Context, string, ...any) (commandResult, error) { return nil, wantErr }
		}},
		{"outbox", func(tx *fakeTransaction) {
			tx.queryRow = lockingRow
			calls := 0
			tx.exec = func(context.Context, string, ...any) (commandResult, error) {
				calls++
				if calls == 2 {
					return nil, wantErr
				}
				return fakeCommandResult(1), nil
			}
		}},
		{"commit", func(tx *fakeTransaction) {
			tx.queryRow = lockingRow
			tx.commit = func(context.Context) error { return wantErr }
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx := baseTransaction()
			test.mutate(&tx)
			database := fakeDatabase{begin: func(context.Context) (transaction, error) { return tx, nil }}
			transition := result
			if test.name == "outbox" {
				transition.Effects = []statemachine.Effect{{Kind: "publish"}}
			}
			_, _, err := fakeStore(database).CompareAndTransition(context.Background(), "one", 0, transition, time.Now())
			if !errors.Is(err, wantErr) {
				t.Fatalf("error = %v, want driver failure", err)
			}
		})
	}
}

func lockingRow(context.Context, string, ...any) row {
	return fakeRow{scan: func(destinations ...any) error {
		*destinations[0].(*int64) = 1
		return nil
	}}
}

func TestConflictReasonQueryFailure(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("query failed")
	tx := baseTransaction()
	tx.queryRow = func(context.Context, string, ...any) row { return fakeRow{scan: func(...any) error { return wantErr }} }
	if err := fakeStore(fakeDatabase{}).conflictReason(context.Background(), tx, "one"); !errors.Is(err, wantErr) {
		t.Fatalf("conflict error = %v", err)
	}
}

func TestHistoryDriverFailures(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("rows failed")
	tests := []struct {
		name string
		rows *fakeRows
		row  row
	}{
		{"scan", &fakeRows{next: []bool{true}, scan: func(...any) error { return wantErr }}, nil},
		{"iterate", &fakeRows{scan: func(...any) error { return nil }, err: wantErr}, nil},
		{"inspect", &fakeRows{scan: func(...any) error { return nil }}, fakeRow{scan: func(...any) error { return wantErr }}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := fakeDatabase{
				query:    func(context.Context, string, ...any) (rows, error) { return test.rows, nil },
				queryRow: func(context.Context, string, ...any) row { return test.row },
			}
			if _, err := fakeStore(database).History(context.Background(), "one", 0, 1); !errors.Is(err, wantErr) {
				t.Fatalf("history error = %v", err)
			}
		})
	}
}

func TestSnapshotAndMarshalDriverFailures(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("write failed")
	database := fakeDatabase{
		queryRow: func(context.Context, string, ...any) row {
			return fakeRow{scan: func(destinations ...any) error {
				*destinations[0].(*string) = "a"
				*destinations[1].(*string) = "v1"
				return nil
			}}
		},
		exec: func(context.Context, string, ...any) (commandResult, error) { return nil, wantErr },
	}
	if err := fakeStore(database).SaveSnapshot(context.Background(), statemachine.Snapshot[string]{
		InstanceID: "one", State: "a", DefinitionVersion: "v1",
	}); !errors.Is(err, wantErr) {
		t.Fatalf("snapshot error = %v", err)
	}
	store := fakeStore(database)
	store.marshal = func(any) ([]byte, error) { return nil, wantErr }
	if _, _, err := store.encodeResult(statemachine.Result[string, string]{
		DefinitionVersion: "v1", Previous: "a", Next: "b",
	}); !errors.Is(err, wantErr) {
		t.Fatalf("marshal error = %v", err)
	}
}

func TestClaimDriverFailures(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("claim failed")
	tests := []struct {
		name   string
		mutate func(*fakeTransaction)
	}{
		{"query", func(tx *fakeTransaction) {
			tx.query = func(context.Context, string, ...any) (rows, error) { return nil, wantErr }
		}},
		{"scan", func(tx *fakeTransaction) {
			tx.query = func(context.Context, string, ...any) (rows, error) {
				return &fakeRows{next: []bool{true}, scan: func(...any) error { return wantErr }}, nil
			}
		}},
		{"iterate", func(tx *fakeTransaction) {
			tx.query = func(context.Context, string, ...any) (rows, error) {
				return &fakeRows{scan: func(...any) error { return nil }, err: wantErr}, nil
			}
		}},
		{"update", func(tx *fakeTransaction) {
			tx.query = candidateQuery
			tx.exec = func(context.Context, string, ...any) (commandResult, error) { return nil, wantErr }
		}},
		{"commit", func(tx *fakeTransaction) {
			tx.query = candidateQuery
			tx.commit = func(context.Context) error { return wantErr }
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx := baseTransaction()
			test.mutate(&tx)
			store := fakeStore(fakeDatabase{begin: func(context.Context) (transaction, error) { return tx, nil }})
			_, err := store.Claim(context.Background(), outbox.ClaimRequest{Owner: "one", Limit: 1, LeaseDuration: time.Second})
			if !errors.Is(err, wantErr) {
				t.Fatalf("claim error = %v", err)
			}
		})
	}
}

func candidateQuery(context.Context, string, ...any) (rows, error) {
	return &fakeRows{next: []bool{true}, scan: func(destinations ...any) error {
		*destinations[0].(*string) = "message"
		*destinations[1].(*string) = "instance"
		*destinations[2].(*int64) = 1
		*destinations[3].(*int) = 0
		*destinations[4].(*string) = "kind"
		*destinations[5].(*[]byte) = []byte("payload")
		*destinations[6].(*time.Time) = time.Unix(1, 0)
		*destinations[7].(*int) = 0
		return nil
	}}, nil
}
