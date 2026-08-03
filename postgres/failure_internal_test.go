package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
	"github.com/faustbrian/golib/pkg/state-machine/outbox"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

func TestNewRejectsEachMissingDependency(t *testing.T) {
	t.Parallel()

	valid := Options[string, string]{
		Pool: &pgxpool.Pool{}, Schema: "state_machine",
		StateCodec: TextCodec[string](), EventCodec: TextCodec[string](),
		NewID: func() string { return "id" }, Clock: time.Now,
	}
	tests := []struct {
		name   string
		mutate func(*Options[string, string])
	}{
		{"pool", func(options *Options[string, string]) { options.Pool = nil }},
		{"unsafe schema", func(options *Options[string, string]) { options.Schema = "unsafe-name" }},
		{"state encoder", func(options *Options[string, string]) { options.StateCodec.Encode = nil }},
		{"state decoder", func(options *Options[string, string]) { options.StateCodec.Decode = nil }},
		{"event encoder", func(options *Options[string, string]) { options.EventCodec.Encode = nil }},
		{"event decoder", func(options *Options[string, string]) { options.EventCodec.Decode = nil }},
		{"identifier generator", func(options *Options[string, string]) { options.NewID = nil }},
		{"clock", func(options *Options[string, string]) { options.Clock = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := valid
			test.mutate(&options)
			if _, err := New(options); !errors.Is(err, ErrInvalidOptions) {
				t.Fatalf("New() error = %v, want ErrInvalidOptions", err)
			}
		})
	}
	valid.Schema = ""
	store, err := New(valid)
	if err != nil || store.schema != "public" {
		t.Fatalf("New() default schema = %q, %v", store.schema, err)
	}
}

func TestStoreOperationBoundaries(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("driver failed")
	t.Run("migration result", func(t *testing.T) {
		for _, returned := range []error{nil, wantErr} {
			store := fakeStore(fakeDatabase{exec: func(context.Context, string, ...any) (commandResult, error) {
				return fakeCommandResult(1), returned
			}})
			err := store.Migrate(context.Background())
			if !errors.Is(err, returned) {
				t.Fatalf("Migrate() error = %v, want %v", err, returned)
			}
		}
	})

	t.Run("create validation", func(t *testing.T) {
		valid := statemachine.Instance[string]{ID: "one", State: "a", DefinitionVersion: "v1"}
		for _, instance := range []statemachine.Instance[string]{
			{State: "a", DefinitionVersion: "v1"},
			{ID: "one", State: "a"},
			{ID: "one", State: "a", DefinitionVersion: "v1", LockVersion: 1},
		} {
			if err := fakeStore(fakeDatabase{}).Create(context.Background(), instance); !errors.Is(err, statemachine.ErrInvalidStoreInput) {
				t.Fatalf("Create(%#v) error = %v", instance, err)
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := fakeStore(fakeDatabase{}).Create(ctx, valid); !errors.Is(err, context.Canceled) {
			t.Fatalf("Create() canceled error = %v", err)
		}
		for _, test := range []struct {
			name string
			tag  commandResult
			err  error
			want error
		}{
			{"created", fakeCommandResult(1), nil, nil},
			{"duplicate", fakeCommandResult(0), nil, statemachine.ErrStoreExists},
			{"driver", nil, wantErr, wantErr},
		} {
			database := fakeDatabase{exec: func(context.Context, string, ...any) (commandResult, error) { return test.tag, test.err }}
			if err := fakeStore(database).Create(context.Background(), valid); !errors.Is(err, test.want) {
				t.Fatalf("Create() %s error = %v, want %v", test.name, err, test.want)
			}
		}
	})

	t.Run("load results", func(t *testing.T) {
		for _, test := range []struct {
			name string
			scan func(...any) error
			want error
		}{
			{"missing", func(...any) error { return pgx.ErrNoRows }, statemachine.ErrStoreNotFound},
			{"driver", func(...any) error { return wantErr }, wantErr},
			{"loaded", func(values ...any) error {
				*values[0].(*string) = "a"
				*values[1].(*string) = "v1"
				*values[2].(*int64) = 2
				return nil
			}, nil},
		} {
			database := fakeDatabase{queryRow: func(context.Context, string, ...any) row { return fakeRow{scan: test.scan} }}
			instance, err := fakeStore(database).Load(context.Background(), "one")
			if !errors.Is(err, test.want) {
				t.Fatalf("Load() %s error = %v, want %v", test.name, err, test.want)
			}
			if test.want == nil && (instance.ID != "one" || instance.State != "a" || instance.DefinitionVersion != "v1" || instance.LockVersion != 2) {
				t.Fatalf("Load() instance = %#v", instance)
			}
		}
	})
}

func TestHistorySnapshotAndCloneBoundaries(t *testing.T) {
	t.Parallel()

	for _, limit := range []int{-1, statemachine.MaxHistoryPageLimit + 1} {
		if _, err := fakeStore(fakeDatabase{}).History(context.Background(), "one", 0, limit); !errors.Is(err, statemachine.ErrInvalidStoreInput) {
			t.Fatalf("History(limit=%d) error = %v", limit, err)
		}
	}
	for _, limit := range []int{0, statemachine.MaxHistoryPageLimit} {
		var received int
		database := fakeDatabase{
			query: func(_ context.Context, _ string, values ...any) (rows, error) {
				received = values[2].(int)
				return &fakeRows{scan: func(...any) error { return nil }}, nil
			},
			queryRow: func(context.Context, string, ...any) row {
				return fakeRow{scan: func(values ...any) error { *values[0].(*bool) = true; return nil }}
			},
		}
		if _, err := fakeStore(database).History(context.Background(), "one", 0, limit); err != nil {
			t.Fatalf("History(limit=%d): %v", limit, err)
		}
		want := limit
		if want == 0 {
			want = statemachine.DefaultHistoryPageLimit
		}
		if received != want {
			t.Fatalf("History(limit=%d) query limit = %d, want %d", limit, received, want)
		}
	}

	malformedRows := &fakeRows{next: []bool{true}, scan: func(values ...any) error {
		*values[0].(*int64) = 1
		*values[1].(*[]byte) = []byte("{")
		return nil
	}}
	if _, err := fakeStore(fakeDatabase{query: func(context.Context, string, ...any) (rows, error) {
		return malformedRows, nil
	}}).History(context.Background(), "one", 0, 1); err == nil {
		t.Fatal("History() decoded malformed result")
	}

	matchingRow := func(values ...any) error {
		*values[0].(*string) = "a"
		*values[1].(*string) = "v1"
		return nil
	}
	for _, snapshot := range []statemachine.Snapshot[string]{
		{InstanceID: "one", State: "wrong", DefinitionVersion: "v1"},
		{InstanceID: "one", State: "a", DefinitionVersion: "wrong"},
	} {
		database := fakeDatabase{queryRow: func(context.Context, string, ...any) row { return fakeRow{scan: matchingRow} }}
		if err := fakeStore(database).SaveSnapshot(context.Background(), snapshot); !errors.Is(err, statemachine.ErrInvalidStoreInput) {
			t.Fatalf("SaveSnapshot(%#v) error = %v", snapshot, err)
		}
	}
	for _, affected := range []int64{0, 1} {
		database := fakeDatabase{
			queryRow: func(context.Context, string, ...any) row { return fakeRow{scan: matchingRow} },
			exec:     func(context.Context, string, ...any) (commandResult, error) { return fakeCommandResult(affected), nil },
		}
		err := fakeStore(database).SaveSnapshot(context.Background(), statemachine.Snapshot[string]{
			InstanceID: "one", State: "a", DefinitionVersion: "v1",
		})
		if (affected == 0) != errors.Is(err, statemachine.ErrStoreConflict) {
			t.Fatalf("SaveSnapshot() rows=%d error = %v", affected, err)
		}
	}
	if cloneEffects(nil) != nil {
		t.Fatal("cloneEffects(nil) returned non-nil slice")
	}
}

func TestTransitionNormalizesNilEffectPayload(t *testing.T) {
	t.Parallel()

	tx := baseTransaction()
	tx.queryRow = lockingRow
	execCalls := 0
	tx.exec = func(_ context.Context, _ string, values ...any) (commandResult, error) {
		execCalls++
		if execCalls == 2 {
			payload := values[5].([]byte)
			if payload == nil || len(payload) != 0 {
				t.Fatalf("outbox payload = %#v, want non-nil empty bytes", payload)
			}
		}
		return fakeCommandResult(1), nil
	}
	store := fakeStore(fakeDatabase{begin: func(context.Context) (transaction, error) { return tx, nil }})
	_, _, err := store.CompareAndTransition(context.Background(), "one", 0, statemachine.Result[string, string]{
		DefinitionVersion: "v1", Previous: "a", Next: "b", Event: "go", TransitionID: "go",
		Effects: []statemachine.Effect{{Kind: "publish", Payload: nil}},
	}, time.Now())
	if err != nil {
		t.Fatalf("CompareAndTransition() error: %v", err)
	}
}

func TestSnapshotSelectsExactReplayBoundaryAndReportsLoadFailures(t *testing.T) {
	t.Parallel()

	for _, lockVersion := range []uint64{0, 1} {
		var query string
		var argumentCount int
		database := fakeDatabase{
			queryRow: func(_ context.Context, sql string, values ...any) row {
				query = sql
				argumentCount = len(values)
				return fakeRow{scan: func(destinations ...any) error {
					*destinations[0].(*string) = "a"
					*destinations[1].(*string) = "v1"
					return nil
				}}
			},
			exec: func(context.Context, string, ...any) (commandResult, error) { return fakeCommandResult(1), nil },
		}
		if err := fakeStore(database).SaveSnapshot(context.Background(), statemachine.Snapshot[string]{
			InstanceID: "one", State: "a", DefinitionVersion: "v1", LockVersion: lockVersion,
		}); err != nil {
			t.Fatalf("SaveSnapshot(lock=%d): %v", lockVersion, err)
		}
		if lockVersion == 0 && (!strings.Contains(query, "initial_state") || argumentCount != 1) {
			t.Fatalf("zero-lock snapshot query = %q with %d arguments", query, argumentCount)
		}
		if lockVersion == 1 && (!strings.Contains(query, "state_machine_history") || argumentCount != 2) {
			t.Fatalf("history snapshot query = %q with %d arguments", query, argumentCount)
		}
	}

	wantErr := errors.New("load failed")
	database := fakeDatabase{queryRow: func(context.Context, string, ...any) row {
		return fakeRow{scan: func(...any) error { return wantErr }}
	}}
	if _, err := fakeStore(database).LoadSnapshot(context.Background(), "one"); !errors.Is(err, wantErr) {
		t.Fatalf("LoadSnapshot() driver error = %v", err)
	}
	database.queryRow = func(context.Context, string, ...any) row {
		return fakeRow{scan: func(values ...any) error {
			*values[0].(*string) = "corrupt"
			return nil
		}}
	}
	store := fakeStore(database)
	store.stateCodec.Decode = func(string) (string, error) { return "", wantErr }
	if _, err := store.LoadSnapshot(context.Background(), "one"); !errors.Is(err, wantErr) {
		t.Fatalf("LoadSnapshot() decode error = %v", err)
	}
}

func TestOutboxInclusiveBoundariesAndLeaseOutcomes(t *testing.T) {
	t.Parallel()

	valid := outbox.ClaimRequest{Owner: "owner", Limit: 1, LeaseDuration: time.Second}
	requests := []outbox.ClaimRequest{
		{Limit: 1, LeaseDuration: time.Second},
		{Owner: "owner", Limit: 0, LeaseDuration: time.Second},
		{Owner: "owner", Limit: maxClaimBatch + 1, LeaseDuration: time.Second},
		{Owner: "owner", Limit: 1},
	}
	for _, request := range requests {
		if _, err := fakeStore(fakeDatabase{}).Claim(context.Background(), request); !errors.Is(err, outbox.ErrInvalidClaim) {
			t.Fatalf("Claim(%#v) error = %v", request, err)
		}
	}
	var queryLimit int
	tx := baseTransaction()
	tx.query = func(_ context.Context, _ string, values ...any) (rows, error) {
		queryLimit = values[1].(int)
		return &fakeRows{scan: func(...any) error { return nil }}, nil
	}
	store := fakeStore(fakeDatabase{begin: func(context.Context) (transaction, error) { return tx, nil }})
	valid.Limit = maxClaimBatch
	if _, err := store.Claim(context.Background(), valid); err != nil || queryLimit != maxClaimBatch {
		t.Fatalf("Claim() inclusive maximum limit = %d, %v", queryLimit, err)
	}

	tx = baseTransaction()
	tx.query = candidateQuery
	store = fakeStore(fakeDatabase{begin: func(context.Context) (transaction, error) { return tx, nil }})
	claims, err := store.Claim(context.Background(), outbox.ClaimRequest{Owner: "owner", Limit: 1, LeaseDuration: time.Second})
	if err != nil || len(claims) != 1 || claims[0].Message.Attempts != 1 {
		t.Fatalf("Claim() attempts = %#v, %v", claims, err)
	}

	for _, ref := range []outbox.LeaseRef{{Token: "token"}, {ID: "id"}} {
		if err := fakeStore(fakeDatabase{}).MarkPublished(context.Background(), ref, time.Now()); !errors.Is(err, outbox.ErrInvalidClaim) {
			t.Fatalf("MarkPublished(%#v) error = %v", ref, err)
		}
	}
	wantFinishErr := errors.New("finish failed")
	for _, test := range []struct {
		name string
		tag  commandResult
		err  error
		want error
	}{
		{"published", fakeCommandResult(1), nil, nil},
		{"lease lost", fakeCommandResult(0), nil, outbox.ErrLeaseLost},
		{"driver", nil, wantFinishErr, wantFinishErr},
	} {
		database := fakeDatabase{exec: func(context.Context, string, ...any) (commandResult, error) { return test.tag, test.err }}
		err := fakeStore(database).MarkPublished(context.Background(), outbox.LeaseRef{ID: "id", Token: "token"}, time.Now())
		if !errors.Is(err, test.want) {
			t.Fatalf("MarkPublished() %s error = %v, want %v", test.name, err, test.want)
		}
	}

	exact := strings.Repeat("x", maxErrorBytes)
	if got := boundedErrorText(errors.New(exact)); got != exact {
		t.Fatalf("boundedErrorText() changed exact maximum length: %d", len(got))
	}
	if got := boundedErrorText(errors.New(exact + "x")); got != exact {
		t.Fatalf("boundedErrorText() over maximum = %d bytes", len(got))
	}
}

func TestPoolDatabaseBeginReturnsAcquisitionError(t *testing.T) {
	t.Parallel()

	config, err := pgxpool.ParseConfig("postgres://state-machine@127.0.0.1:1/state-machine?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (poolDatabase{pool: pool}).Begin(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Begin() error = %v, want context cancellation", err)
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
