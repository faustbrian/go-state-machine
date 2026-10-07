//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	statemachine "github.com/faustbrian/go-state-machine/v2"
	"github.com/faustbrian/go-state-machine/v2/outbox"
	storepostgres "github.com/faustbrian/go-state-machine/v2/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresSimpleProtocolPreservesDurableInstants(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	config, err := pgxpool.ParseConfig(postgresConnectionString(t, ctx))
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	config.ConnConfig.RuntimeParams["timezone"] = "Asia/Kolkata"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	resetStateMachineSchema(t, ctx, pool)
	_, clientOffset := time.Now().In(time.Local).Zone()
	t.Logf("client timestamp offset seconds: %d; server zone: Asia/Kolkata", clientOffset)

	occurred := time.Date(2024, 6, 15, 12, 34, 56, 123456000, time.FixedZone("fixture", 3*3600+30*60))
	created := occurred.Add(2 * time.Second)
	clock := occurred.Add(time.Hour)
	store := driverContractStore(t, pool, clock)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, statemachine.Instance[string]{ID: "timestamp-contract", State: "pending", DefinitionVersion: "v1"}); err != nil {
		t.Fatal(err)
	}
	result := driverContractResult()
	result.Effects = []statemachine.Effect{{Kind: "capture", Payload: []byte{0, 255, 39, 92, 65}}, {Kind: "nil", Payload: nil}, {Kind: "empty", Payload: []byte{}}}
	if instance, entry, err := store.CompareAndTransition(ctx, "timestamp-contract", 0, result, occurred); err != nil || instance.State != "paid" || instance.LockVersion != 1 || entry.Sequence != 1 {
		t.Fatal(err)
	}
	history, err := store.History(ctx, "timestamp-contract", 0, 10)
	if err != nil || len(history) != 1 || !history[0].OccurredAt.Equal(occurred) || history[0].Result.Next != "paid" || history[0].Result.Previous != "pending" || history[0].Result.DefinitionVersion != "v1" || history[0].Result.Event != result.Event || history[0].Result.TransitionID != result.TransitionID || history[0].Result.Metadata != result.Metadata || len(history[0].Result.Effects) != 3 {
		t.Fatalf("history lost fixture instant or result: %#v, %v", history, err)
	}
	for i, effect := range result.Effects {
		if history[0].Result.Effects[i].Kind != effect.Kind || !bytes.Equal(history[0].Result.Effects[i].Payload, effect.Payload) {
			t.Fatalf("history effect %d differs: %#v", i, history[0].Result.Effects[i])
		}
	}
	var documentType, persistedEvent, persistedCorrelation string
	if err := pool.QueryRow(ctx, `SELECT jsonb_typeof(result), result->>'event', result->'metadata'->>'CorrelationID' FROM state_machine.state_machine_history WHERE instance_id = 'timestamp-contract'`).Scan(&documentType, &persistedEvent, &persistedCorrelation); err != nil || documentType != "object" || persistedEvent != result.Event || persistedCorrelation != result.Metadata.CorrelationID {
		t.Fatalf("history document shape/fields: %q/%q/%q, %v", documentType, persistedEvent, persistedCorrelation, err)
	}
	if err := store.SaveSnapshot(ctx, statemachine.Snapshot[string]{InstanceID: "timestamp-contract", State: "paid", DefinitionVersion: "v1", LockVersion: 1, CreatedAt: created}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.LoadSnapshot(ctx, "timestamp-contract")
	if err != nil || !snapshot.CreatedAt.Equal(created) || snapshot.State != "paid" || snapshot.LockVersion != 1 {
		t.Fatalf("snapshot lost fixture instant or state: %#v, %v", snapshot, err)
	}
	claims, err := store.Claim(ctx, outbox.ClaimRequest{Owner: "timestamp-reader", Limit: 3, LeaseDuration: time.Minute})
	if err != nil || len(claims) != 3 {
		t.Fatalf("claims: %#v, %v", claims, err)
	}
	for i, claim := range claims {
		if !claim.Message.OccurredAt.Equal(occurred) || !claim.LeasedUntil.Equal(clock.Add(time.Minute)) || claim.Message.Effect.Kind != result.Effects[i].Kind || !bytes.Equal(claim.Message.Effect.Payload, result.Effects[i].Payload) || claim.Message.InstanceID != "timestamp-contract" || claim.Message.Index != i || claim.Token == "" {
			t.Fatalf("claim %d lost instant, payload or lease: %#v", i, claim)
		}
		var payload []byte
		var isNull bool
		if err := pool.QueryRow(ctx, `SELECT payload, payload IS NULL FROM state_machine.state_machine_outbox WHERE instance_id = 'timestamp-contract' AND effect_index = $1`, i).Scan(&payload, &isNull); err != nil || isNull || !bytes.Equal(payload, result.Effects[i].Payload) {
			t.Fatalf("raw bytea effect %d differs: %v, null=%v, %v", i, payload, isNull, err)
		}
	}
	var historyMicros, snapshotMicros, outboxMicros int64
	err = pool.QueryRow(ctx, `SELECT
    (SELECT round(extract(epoch FROM occurred_at)*1000000)::bigint FROM state_machine.state_machine_history WHERE instance_id = 'timestamp-contract'),
    (SELECT round(extract(epoch FROM created_at)*1000000)::bigint FROM state_machine.state_machine_snapshots WHERE instance_id = 'timestamp-contract'),
    (SELECT round(extract(epoch FROM occurred_at)*1000000)::bigint FROM state_machine.state_machine_outbox WHERE instance_id = 'timestamp-contract' AND effect_index = 0)`).Scan(&historyMicros, &snapshotMicros, &outboxMicros)
	if err != nil || historyMicros != occurred.UnixMicro() || snapshotMicros != created.UnixMicro() || outboxMicros != occurred.UnixMicro() {
		t.Fatalf("persisted numeric epochs differ: %d/%d/%d, %v", historyMicros, snapshotMicros, outboxMicros, err)
	}
	rollbackStore := newStoreForPool(t, pool, func() string { return "duplicate-simple-id" })
	if err := rollbackStore.Create(ctx, statemachine.Instance[string]{ID: "simple-rollback", State: "pending", DefinitionVersion: "v1"}); err != nil {
		t.Fatal(err)
	}
	duplicate := driverContractResult()
	duplicate.Effects = []statemachine.Effect{{Kind: "one"}, {Kind: "two"}}
	if _, _, err := rollbackStore.CompareAndTransition(ctx, "simple-rollback", 0, duplicate, occurred); err == nil {
		t.Fatal("duplicate outbox IDs committed through simple protocol")
	}
	instance, err := store.Load(ctx, "simple-rollback")
	if err != nil || instance.State != "pending" || instance.LockVersion != 0 {
		t.Fatalf("simple rollback state: %#v, %v", instance, err)
	}
	assertCounts(t, ctx, pool, "simple-rollback", 0, 0)
	if _, _, err := store.CompareAndTransition(ctx, "simple-rollback", 0, driverContractResult(), occurred); err != nil {
		t.Fatalf("simple pool unusable after rollback: %v", err)
	}
	assertCounts(t, ctx, pool, "simple-rollback", 1, 1)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("caller pool unusable after reads: %v", err)
	}
}

func TestPostgresInFlightCancellationPreservesAtomicityAndPoolReuse(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	url := postgresConnectionString(t, ctx)
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 1
	config.ConnConfig.RuntimeParams["application_name"] = "state-machine-cancel-contract"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	resetStateMachineSchema(t, ctx, pool)
	store := driverContractStore(t, pool, time.Unix(1700000000, 0))
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, statemachine.Instance[string]{ID: "cancel-contract", State: "pending", DefinitionVersion: "v1"}); err != nil {
		t.Fatal(err)
	}
	blocker, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = blocker.Close(context.Background()) })
	tx, err := blocker.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if _, err := tx.Exec(ctx, `SELECT id FROM state_machine.state_machine_instances WHERE id = 'cancel-contract' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	operationCtx, cancelOperation := context.WithCancel(ctx)
	defer cancelOperation()
	done := make(chan error, 1)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		_, _, err := store.CompareAndTransition(operationCtx, "cancel-contract", 0, driverContractResult(), time.Unix(1700000000, 0))
		done <- err
	}()
	t.Cleanup(func() {
		cancelOperation()
		_ = tx.Rollback(context.Background())
		select {
		case <-joined:
		case <-time.After(20 * time.Second):
			t.Error("cancelled operation did not join during teardown")
		}
	})
	waitCtx, cancelWait := context.WithTimeout(ctx, 10*time.Second)
	defer cancelWait()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		err := blocker.QueryRow(waitCtx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE application_name = 'state-machine-cancel-contract' AND state = 'active' AND wait_event_type = 'Lock')`).Scan(&waiting)
		if err != nil {
			t.Fatalf("observe in-flight lock wait: %v", err)
		}
		if waiting {
			break
		}
		select {
		case <-waitCtx.Done():
			t.Fatal("transition never reached server-side lock wait")
		case <-ticker.C:
		}
	}
	cancelOperation()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("in-flight cancellation error: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("cancelled transition did not return within bounded cleanup window")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	instance, err := store.Load(ctx, "cancel-contract")
	if err != nil || instance.State != "pending" || instance.LockVersion != 0 {
		t.Fatalf("cancelled transition became durable: %#v, %v", instance, err)
	}
	assertCounts(t, ctx, pool, "cancel-contract", 0, 0)
	if _, _, err := store.CompareAndTransition(ctx, "cancel-contract", 0, driverContractResult(), time.Unix(1700000000, 0)); err != nil {
		t.Fatalf("fresh transition through same caller pool: %v", err)
	}
	history, err := store.History(ctx, "cancel-contract", 0, 10)
	if err != nil || len(history) != 1 || history[0].Result.Next != "paid" {
		t.Fatalf("fresh history after cancellation: %#v, %v", history, err)
	}
	assertCounts(t, ctx, pool, "cancel-contract", 1, 1)
}

func driverContractStore(t *testing.T, pool *pgxpool.Pool, clock time.Time) *storepostgres.Store[string, string] {
	t.Helper()
	var ids atomic.Uint64
	store, err := storepostgres.New(storepostgres.Options[string, string]{Pool: pool, Schema: "state_machine", StateCodec: storepostgres.TextCodec[string](), EventCodec: storepostgres.TextCodec[string](), NewID: func() string { return fmt.Sprintf("driver-contract-%d", ids.Add(1)) }, Clock: func() time.Time { return clock }})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func driverContractResult() statemachine.Result[string, string] {
	return statemachine.Result[string, string]{DefinitionVersion: "v1", Previous: "pending", Next: "paid", Event: "pay", TransitionID: "pay-order", Metadata: statemachine.Metadata{CorrelationID: "quoted '\"\\ line\n snowman ☃", CausationID: "cause"}, Effects: []statemachine.Effect{{Kind: "capture", Payload: []byte("known payload")}}}
}
