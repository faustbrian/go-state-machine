//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
	"github.com/faustbrian/golib/pkg/state-machine/outbox"
	storepostgres "github.com/faustbrian/golib/pkg/state-machine/postgres"
	"github.com/faustbrian/golib/pkg/state-machine/statemachinetest"
	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func postgresConnectionString(tb testing.TB, ctx context.Context) string {
	tb.Helper()
	for _, variable := range []string{"POSTGRES_URL", "TEST_DATABASE_URL", "DATABASE_URL"} {
		if connectionString := os.Getenv(variable); connectionString != "" {
			return connectionString
		}
	}

	version := os.Getenv("STATE_MACHINE_POSTGRES_VERSION")
	if version == "" {
		version = "18"
	}
	container, err := tcpostgres.Run(ctx, "postgres:"+version+"-alpine",
		tcpostgres.WithDatabase("state_machine"),
		tcpostgres.WithUsername("state_machine"),
		tcpostgres.WithPassword("state_machine"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		tb.Fatalf("start PostgreSQL %s: %v", version, err)
	}
	tb.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			tb.Errorf("terminate PostgreSQL: %v", err)
		}
	})
	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		tb.Fatalf("connection string: %v", err)
	}

	return connectionString
}

func resetStateMachineSchema(tb testing.TB, ctx context.Context, pool *pgxpool.Pool) {
	tb.Helper()
	if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS state_machine CASCADE"); err != nil {
		tb.Fatalf("reset state-machine schema: %v", err)
	}
}

func TestPostgresStoreContractAndAtomicOutbox(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	connectionString := postgresConnectionString(t, ctx)
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	resetStateMachineSchema(t, ctx, pool)

	var ids atomic.Uint64
	newStore := func(id func() string) *storepostgres.Store[string, string] {
		store, err := storepostgres.New(storepostgres.Options[string, string]{
			Pool: pool, Schema: "state_machine",
			StateCodec: storepostgres.TextCodec[string](),
			EventCodec: storepostgres.TextCodec[string](),
			NewID:      id,
			Clock:      time.Now,
		})
		if err != nil {
			t.Fatalf("new store: %v", err)
		}
		return store
	}
	store := newStore(func() string { return fmt.Sprintf("effect-%d", ids.Add(1)) })
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	statemachinetest.StoreContract(t,
		func() statemachine.Store[string, string] { return store },
		statemachinetest.StoreFixture[string, string]{
			Instance: statemachine.Instance[string]{
				ID: "contract-1", State: "pending", DefinitionVersion: "v1",
			},
			Result: statemachine.Result[string, string]{
				DefinitionVersion: "v1", Previous: "pending", Next: "paid",
				Event: "pay", TransitionID: "pay-order",
			},
		},
	)
	if err := store.Create(ctx, statemachine.Instance[string]{}); !errors.Is(err, statemachine.ErrInvalidStoreInput) {
		t.Fatalf("invalid create error = %v", err)
	}
	if err := store.Create(ctx, statemachine.Instance[string]{
		ID: "contract-1", State: "pending", DefinitionVersion: "v1",
	}); !errors.Is(err, statemachine.ErrStoreExists) {
		t.Fatalf("duplicate create error = %v", err)
	}
	if _, err := store.Load(ctx, "missing"); !errors.Is(err, statemachine.ErrStoreNotFound) {
		t.Fatalf("missing load error = %v", err)
	}
	if _, _, err := store.CompareAndTransition(ctx, "missing", 0, statemachine.Result[string, string]{
		DefinitionVersion: "v1", Previous: "a", Next: "b", TransitionID: "go",
	}, time.Now()); !errors.Is(err, statemachine.ErrStoreNotFound) {
		t.Fatalf("missing transition error = %v", err)
	}
	if _, err := store.History(ctx, "contract-1", 0, -1); !errors.Is(err, statemachine.ErrInvalidStoreInput) {
		t.Fatalf("invalid history limit error = %v", err)
	}
	if _, err := store.History(ctx, "contract-1", 0, statemachine.MaxHistoryPageLimit+1); !errors.Is(err, statemachine.ErrInvalidStoreInput) {
		t.Fatalf("oversized history limit error = %v", err)
	}
	if _, err := store.History(ctx, "missing", 0, 1); !errors.Is(err, statemachine.ErrStoreNotFound) {
		t.Fatalf("missing history error = %v", err)
	}
	if entries, err := store.History(ctx, "contract-1", 100, 0); err != nil || len(entries) != 0 {
		t.Fatalf("history after end = %#v, %v", entries, err)
	}
	if err := store.SaveSnapshot(ctx, statemachine.Snapshot[string]{
		InstanceID: "missing", State: "a", DefinitionVersion: "v1",
	}); !errors.Is(err, statemachine.ErrStoreNotFound) {
		t.Fatalf("missing snapshot error = %v", err)
	}
	if err := store.SaveSnapshot(ctx, statemachine.Snapshot[string]{
		InstanceID: "contract-1", State: "wrong", DefinitionVersion: "v1", LockVersion: 1,
	}); !errors.Is(err, statemachine.ErrInvalidStoreInput) {
		t.Fatalf("invalid snapshot error = %v", err)
	}
	if err := store.SaveSnapshot(ctx, statemachine.Snapshot[string]{
		InstanceID: "contract-1", State: "pending", DefinitionVersion: "v1", LockVersion: 0,
	}); !errors.Is(err, statemachine.ErrStoreConflict) {
		t.Fatalf("backward snapshot error = %v", err)
	}
	if _, err := store.LoadSnapshot(ctx, "missing"); !errors.Is(err, statemachine.ErrStoreNotFound) {
		t.Fatalf("missing snapshot load error = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE state_machine.state_machine_history SET result = '[]'::jsonb WHERE instance_id = 'contract-1'`); err != nil {
		t.Fatalf("corrupt history fixture: %v", err)
	}
	if _, err := store.History(ctx, "contract-1", 0, 1); err == nil {
		t.Fatal("corrupt history decoded")
	}

	if err := store.Create(ctx, statemachine.Instance[string]{
		ID: "atomic-1", State: "pending", DefinitionVersion: "v1",
	}); err != nil {
		t.Fatalf("create atomic fixture: %v", err)
	}
	_, _, err = store.CompareAndTransition(ctx, "atomic-1", 0, statemachine.Result[string, string]{
		DefinitionVersion: "v1", Previous: "pending", Next: "paid",
		Event: "pay", TransitionID: "pay-order",
		Effects: []statemachine.Effect{{Kind: "capture"}, {Kind: "receipt"}},
	}, time.Unix(123, 0))
	if err != nil {
		t.Fatalf("atomic transition: %v", err)
	}
	assertCounts(t, ctx, pool, "atomic-1", 1, 2)
	if err := store.Create(ctx, statemachine.Instance[string]{
		ID: "contention", State: "pending", DefinitionVersion: "v1",
	}); err != nil {
		t.Fatal(err)
	}
	contentionResult := statemachine.Result[string, string]{
		DefinitionVersion: "v1", Previous: "pending", Next: "paid",
		Event: "pay", TransitionID: "pay",
	}
	start := make(chan struct{})
	var wait sync.WaitGroup
	var successes atomic.Int32
	var conflicts atomic.Int32
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, _, err := store.CompareAndTransition(ctx, "contention", 0, contentionResult, time.Now())
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, statemachine.ErrStoreConflict):
				conflicts.Add(1)
			default:
				t.Errorf("contended transition: %v", err)
			}
		}()
	}
	close(start)
	wait.Wait()
	if successes.Load() != 1 || conflicts.Load() != 1 {
		t.Fatalf("contention successes = %d, conflicts = %d", successes.Load(), conflicts.Load())
	}
	claims, err := store.Claim(ctx, outbox.ClaimRequest{
		Owner: "relay-1", Limit: 10, LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("claim effects: %v", err)
	}
	if len(claims) != 2 || claims[0].Message.Index != 0 || claims[1].Message.Index != 1 ||
		claims[0].Token == "" || claims[0].Message.Attempts != 1 {
		t.Fatalf("claims = %#v", claims)
	}
	if err := store.MarkPublished(ctx, outbox.LeaseRef{
		ID: claims[0].Message.ID, Token: claims[0].Token,
	}, time.Now()); err != nil {
		t.Fatalf("mark published: %v", err)
	}
	secondRef := outbox.LeaseRef{ID: claims[1].Message.ID, Token: claims[1].Token}
	if err := store.Retry(ctx, secondRef, time.Now().Add(-time.Second), errors.New("broker unavailable")); err != nil {
		t.Fatalf("retry: %v", err)
	}
	reclaimed, err := store.Claim(ctx, outbox.ClaimRequest{
		Owner: "relay-2", Limit: 10, LeaseDuration: time.Minute,
	})
	if err != nil || len(reclaimed) != 1 || reclaimed[0].Message.ID != claims[1].Message.ID || reclaimed[0].Message.Attempts != 2 {
		t.Fatalf("reclaimed = %#v, %v", reclaimed, err)
	}
	if err := store.MarkPublished(ctx, secondRef, time.Now()); !errors.Is(err, outbox.ErrLeaseLost) {
		t.Fatalf("stale lease error = %v, want ErrLeaseLost", err)
	}
	reclaimedRef := outbox.LeaseRef{ID: reclaimed[0].Message.ID, Token: reclaimed[0].Token}
	if err := store.Retry(ctx, reclaimedRef, time.Now().Add(-time.Second), errors.New(strings.Repeat("x", 5_000))); err != nil {
		t.Fatalf("long retry error: %v", err)
	}
	var errorLength int
	if err := pool.QueryRow(ctx, `SELECT length(last_error) FROM state_machine.state_machine_outbox WHERE id = $1`, reclaimedRef.ID).Scan(&errorLength); err != nil || errorLength != 4_096 {
		t.Fatalf("last error length = %d, %v", errorLength, err)
	}
	reclaimed, err = store.Claim(ctx, outbox.ClaimRequest{Owner: "relay-3", Limit: 1, LeaseDuration: time.Minute})
	if err != nil || len(reclaimed) != 1 {
		t.Fatalf("third claim = %#v, %v", reclaimed, err)
	}
	if err := store.DeadLetter(ctx, outbox.LeaseRef{ID: reclaimed[0].Message.ID, Token: reclaimed[0].Token}, time.Now(), errors.New("permanent")); err != nil {
		t.Fatalf("dead letter: %v", err)
	}
	if _, err := store.Claim(ctx, outbox.ClaimRequest{}); !errors.Is(err, outbox.ErrInvalidClaim) {
		t.Fatalf("invalid claim error = %v", err)
	}
	if err := store.MarkPublished(ctx, outbox.LeaseRef{}, time.Now()); !errors.Is(err, outbox.ErrInvalidClaim) {
		t.Fatalf("invalid lease error = %v", err)
	}

	rollbackStore := newStore(func() string { return "" })
	if err := rollbackStore.Create(ctx, statemachine.Instance[string]{
		ID: "rollback-1", State: "pending", DefinitionVersion: "v1",
	}); err != nil {
		t.Fatalf("create rollback fixture: %v", err)
	}
	_, _, err = rollbackStore.CompareAndTransition(ctx, "rollback-1", 0, statemachine.Result[string, string]{
		DefinitionVersion: "v1", Previous: "pending", Next: "paid",
		Event: "pay", TransitionID: "pay-order",
		Effects: []statemachine.Effect{{Kind: "capture"}},
	}, time.Unix(123, 0))
	if err == nil {
		t.Fatal("transition with invalid outbox ID succeeded")
	}
	instance, err := rollbackStore.Load(ctx, "rollback-1")
	if err != nil || instance.State != "pending" || instance.LockVersion != 0 {
		t.Fatalf("rollback instance = %#v, %v", instance, err)
	}
	assertCounts(t, ctx, pool, "rollback-1", 0, 0)

	duplicateStore := newStoreForPool(t, pool, func() string { return "duplicate-id" })
	if err := duplicateStore.Create(ctx, statemachine.Instance[string]{
		ID: "duplicate-outbox", State: "pending", DefinitionVersion: "v1",
	}); err != nil {
		t.Fatal(err)
	}
	_, _, err = duplicateStore.CompareAndTransition(ctx, "duplicate-outbox", 0, statemachine.Result[string, string]{
		DefinitionVersion: "v1", Previous: "pending", Next: "paid", Event: "pay", TransitionID: "pay",
		Effects: []statemachine.Effect{{Kind: "one"}, {Kind: "two"}},
	}, time.Now())
	if err == nil {
		t.Fatal("duplicate outbox IDs committed")
	}
	assertCounts(t, ctx, pool, "duplicate-outbox", 0, 0)

	if err := store.Create(ctx, statemachine.Instance[string]{
		ID: "empty-token", State: "pending", DefinitionVersion: "v1",
	}); err != nil {
		t.Fatal(err)
	}
	_, _, err = store.CompareAndTransition(ctx, "empty-token", 0, statemachine.Result[string, string]{
		DefinitionVersion: "v1", Previous: "pending", Next: "paid", Event: "pay", TransitionID: "pay",
		Effects: []statemachine.Effect{{Kind: "publish"}},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	emptyTokenStore := newStoreForPool(t, pool, func() string { return "" })
	if _, err := emptyTokenStore.Claim(ctx, outbox.ClaimRequest{
		Owner: "relay", Limit: 1, LeaseDuration: time.Minute,
	}); !errors.Is(err, outbox.ErrInvalidClaim) {
		t.Fatalf("empty lease token error = %v", err)
	}

	decodeCodec := storepostgres.Codec[string]{
		Encode: func(value string) (string, error) { return value, nil },
		Decode: func(value string) (string, error) {
			if value == "corrupt" {
				return "", errors.New("decode failed")
			}
			return value, nil
		},
	}
	decodeStore, err := storepostgres.New(storepostgres.Options[string, string]{
		Pool: pool, Schema: "state_machine", StateCodec: decodeCodec,
		EventCodec: storepostgres.TextCodec[string](), NewID: func() string { return "decode-id" }, Clock: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, statemachine.Instance[string]{ID: "decode", State: "a", DefinitionVersion: "v1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE state_machine.state_machine_instances SET state = 'corrupt' WHERE id = 'decode'`); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeStore.Load(ctx, "decode"); err == nil {
		t.Fatal("corrupt instance state decoded")
	}
	if err := store.Create(ctx, statemachine.Instance[string]{ID: "decode-snapshot", State: "a", DefinitionVersion: "v1"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSnapshot(ctx, statemachine.Snapshot[string]{InstanceID: "decode-snapshot", State: "a", DefinitionVersion: "v1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE state_machine.state_machine_snapshots SET state = 'corrupt' WHERE instance_id = 'decode-snapshot'`); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeStore.LoadSnapshot(ctx, "decode-snapshot"); err == nil {
		t.Fatal("corrupt snapshot state decoded")
	}

	failingCodec := storepostgres.Codec[string]{
		Encode: func(string) (string, error) { return "", errors.New("encode failed") },
		Decode: func(string) (string, error) { return "", errors.New("decode failed") },
	}
	codecStore, err := storepostgres.New(storepostgres.Options[string, string]{
		Pool: pool, Schema: "state_machine", StateCodec: failingCodec,
		EventCodec: storepostgres.TextCodec[string](), NewID: func() string { return "id" }, Clock: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := codecStore.Create(ctx, statemachine.Instance[string]{ID: "codec", State: "a", DefinitionVersion: "v1"}); err == nil {
		t.Fatal("failing state codec created instance")
	}
	if _, err := storepostgres.New(storepostgres.Options[string, string]{
		Pool: pool, StateCodec: storepostgres.TextCodec[string](), EventCodec: storepostgres.TextCodec[string](),
		NewID: func() string { return "default" }, Clock: time.Now,
	}); err != nil {
		t.Fatalf("default schema options: %v", err)
	}

	closedPool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatalf("create closed pool: %v", err)
	}
	closedPool.Close()
	closedStore := newStoreForPool(t, closedPool, func() string { return "closed" })
	assertClosedStoreErrors(t, ctx, closedStore)
}

func BenchmarkPostgresDurableWrite(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	connectionString := postgresConnectionString(b, ctx)
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		b.Fatal(err)
	}
	defer pool.Close()
	resetStateMachineSchema(b, ctx, pool)

	var ids atomic.Uint64
	store, err := storepostgres.New(storepostgres.Options[string, string]{
		Pool: pool, Schema: "state_machine",
		StateCodec: storepostgres.TextCodec[string](),
		EventCodec: storepostgres.TextCodec[string](),
		NewID:      func() string { return fmt.Sprintf("benchmark-effect-%d", ids.Add(1)) },
		Clock:      time.Now,
	})
	if err != nil {
		b.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		b.Fatal(err)
	}
	if err := store.Create(ctx, statemachine.Instance[string]{
		ID: "benchmark", State: "a", DefinitionVersion: "v1",
	}); err != nil {
		b.Fatal(err)
	}
	if _, _, err := store.CompareAndTransition(ctx, "benchmark", 0, statemachine.Result[string, string]{
		DefinitionVersion: "v1", Previous: "a", Next: "b",
		Event: "toggle", TransitionID: "toggle",
		Effects: []statemachine.Effect{{Kind: "publish"}},
	}, time.Now()); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for index := range b.N {
		previous, next := "b", "a"
		if index%2 != 0 {
			previous, next = next, previous
		}
		_, _, err := store.CompareAndTransition(ctx, "benchmark", uint64(index+1), statemachine.Result[string, string]{
			DefinitionVersion: "v1", Previous: previous, Next: next,
			Event: "toggle", TransitionID: "toggle",
			Effects: []statemachine.Effect{{Kind: "publish"}},
		}, time.Now())
		if err != nil {
			b.Fatal(err)
		}
	}
}

func newStoreForPool(t *testing.T, pool *pgxpool.Pool, id func() string) *storepostgres.Store[string, string] {
	t.Helper()
	store, err := storepostgres.New(storepostgres.Options[string, string]{
		Pool: pool, Schema: "state_machine",
		StateCodec: storepostgres.TextCodec[string](), EventCodec: storepostgres.TextCodec[string](),
		NewID: id, Clock: time.Now,
	})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}

func assertClosedStoreErrors(t *testing.T, ctx context.Context, store *storepostgres.Store[string, string]) {
	t.Helper()
	if err := store.Migrate(ctx); err == nil {
		t.Error("closed migrate succeeded")
	}
	if err := store.Create(ctx, statemachine.Instance[string]{ID: "one", State: "a", DefinitionVersion: "v1"}); err == nil {
		t.Error("closed create succeeded")
	}
	if _, err := store.Load(ctx, "one"); err == nil {
		t.Error("closed load succeeded")
	}
	if _, _, err := store.CompareAndTransition(ctx, "one", 0, statemachine.Result[string, string]{DefinitionVersion: "v1"}, time.Now()); err == nil {
		t.Error("closed transition succeeded")
	}
	if _, err := store.History(ctx, "one", 0, 1); err == nil {
		t.Error("closed history succeeded")
	}
	if err := store.SaveSnapshot(ctx, statemachine.Snapshot[string]{InstanceID: "one", State: "a", DefinitionVersion: "v1"}); err == nil {
		t.Error("closed snapshot succeeded")
	}
	if _, err := store.LoadSnapshot(ctx, "one"); err == nil {
		t.Error("closed snapshot load succeeded")
	}
	if _, err := store.Claim(ctx, outbox.ClaimRequest{Owner: "one", Limit: 1, LeaseDuration: time.Second}); err == nil {
		t.Error("closed claim succeeded")
	}
	if err := store.MarkPublished(ctx, outbox.LeaseRef{ID: "one", Token: "token"}, time.Now()); err == nil {
		t.Error("closed finish lease succeeded")
	}
}

func assertCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string, wantHistory, wantOutbox int) {
	t.Helper()
	var history, outbox int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM state_machine.state_machine_history WHERE instance_id = $1`, id).Scan(&history); err != nil {
		t.Fatalf("count history: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM state_machine.state_machine_outbox WHERE instance_id = $1`, id).Scan(&outbox); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if history != wantHistory || outbox != wantOutbox {
		t.Fatalf("history = %d, outbox = %d; want %d, %d", history, outbox, wantHistory, wantOutbox)
	}
}
