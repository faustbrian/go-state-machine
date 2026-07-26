// Package postgres provides durable PostgreSQL state machine persistence.
// CompareAndTransition commits current state, history, and outbox records in
// one database transaction.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Codec converts stable typed identifiers to their durable representation.
type Codec[T any] struct {
	Encode func(T) (string, error)
	Decode func(string) (T, error)
}

// TextCodec persists the underlying value of a string-based identifier.
func TextCodec[T ~string]() Codec[T] {
	return Codec[T]{
		Encode: func(value T) (string, error) { return string(value), nil },
		Decode: func(value string) (T, error) { return T(value), nil },
	}
}

// Options defines explicit PostgreSQL dependencies.
type Options[S statemachine.State, E statemachine.Event] struct {
	Pool       *pgxpool.Pool
	Schema     string
	StateCodec Codec[S]
	EventCodec Codec[E]
	NewID      func() string
	Clock      func() time.Time
}

// Store persists typed state machine instances.
type Store[S statemachine.State, E statemachine.Event] struct {
	pool       database
	schema     string
	stateCodec Codec[S]
	eventCodec Codec[E]
	newID      func() string
	clock      func() time.Time
	marshal    func(any) ([]byte, error)
}

// ErrInvalidOptions reports missing dependencies or an unsafe schema name.
var ErrInvalidOptions = errors.New("postgres: invalid options")

var schemaPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// New validates dependencies and constructs a PostgreSQL store.
func New[S statemachine.State, E statemachine.Event](options Options[S, E]) (*Store[S, E], error) {
	if options.Schema == "" {
		options.Schema = "public"
	}
	if options.Pool == nil || !schemaPattern.MatchString(options.Schema) ||
		options.StateCodec.Encode == nil || options.StateCodec.Decode == nil ||
		options.EventCodec.Encode == nil || options.EventCodec.Decode == nil ||
		options.NewID == nil || options.Clock == nil {
		return nil, ErrInvalidOptions
	}
	return &Store[S, E]{
		pool: poolDatabase{pool: options.Pool}, schema: options.Schema,
		stateCodec: options.StateCodec, eventCodec: options.EventCodec,
		newID: options.NewID, clock: options.Clock, marshal: json.Marshal,
	}, nil
}

// Capabilities reports PostgreSQL's transactional guarantees.
func (store *Store[S, E]) Capabilities() statemachine.StoreCapabilities {
	return statemachine.StoreCapabilities{
		AtomicCompareAndTransition: true,
		AppendOnlyHistory:          true,
		Snapshots:                  true,
		AtomicOutbox:               true,
	}
}

// Migrate creates the store schema with additive, idempotent DDL.
func (store *Store[S, E]) Migrate(ctx context.Context) error {
	_, err := store.pool.Exec(ctx, fmt.Sprintf(`
CREATE SCHEMA IF NOT EXISTS %[1]s;
CREATE TABLE IF NOT EXISTS %[1]s.state_machine_instances (
    id text PRIMARY KEY,
    state text NOT NULL,
    initial_state text NOT NULL,
    definition_version text NOT NULL,
    initial_definition_version text NOT NULL,
    lock_version bigint NOT NULL CHECK (lock_version >= 0)
);
CREATE TABLE IF NOT EXISTS %[1]s.state_machine_history (
    instance_id text NOT NULL REFERENCES %[1]s.state_machine_instances(id),
    sequence bigint NOT NULL CHECK (sequence > 0),
    result jsonb NOT NULL,
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (instance_id, sequence)
);
CREATE TABLE IF NOT EXISTS %[1]s.state_machine_snapshots (
    instance_id text PRIMARY KEY REFERENCES %[1]s.state_machine_instances(id),
    state text NOT NULL,
    definition_version text NOT NULL,
    lock_version bigint NOT NULL CHECK (lock_version >= 0),
    created_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS %[1]s.state_machine_outbox (
    id text PRIMARY KEY,
    instance_id text NOT NULL,
    sequence bigint NOT NULL,
    effect_index integer NOT NULL CHECK (effect_index >= 0),
    kind text NOT NULL,
    payload bytea NOT NULL,
    occurred_at timestamptz NOT NULL,
    available_at timestamptz NOT NULL,
    lease_owner text,
    lease_token text,
    leased_until timestamptz,
    published_at timestamptz,
    dead_lettered_at timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error text,
    UNIQUE (instance_id, sequence, effect_index),
    FOREIGN KEY (instance_id, sequence)
        REFERENCES %[1]s.state_machine_history(instance_id, sequence)
);
ALTER TABLE %[1]s.state_machine_outbox
    ADD COLUMN IF NOT EXISTS available_at timestamptz,
    ADD COLUMN IF NOT EXISTS lease_owner text,
    ADD COLUMN IF NOT EXISTS lease_token text,
    ADD COLUMN IF NOT EXISTS leased_until timestamptz,
    ADD COLUMN IF NOT EXISTS dead_lettered_at timestamptz;
UPDATE %[1]s.state_machine_outbox
SET available_at = occurred_at
WHERE available_at IS NULL;
ALTER TABLE %[1]s.state_machine_outbox
    ALTER COLUMN available_at SET NOT NULL;
CREATE INDEX IF NOT EXISTS state_machine_outbox_ready_idx
    ON %[1]s.state_machine_outbox (available_at, occurred_at, id)
    WHERE published_at IS NULL AND dead_lettered_at IS NULL;
`, store.schema))
	if err != nil {
		return fmt.Errorf("postgres: migrate: %w", err)
	}
	return nil
}

// Create inserts an instance at lock version zero.
func (store *Store[S, E]) Create(ctx context.Context, instance statemachine.Instance[S]) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if instance.ID == "" || instance.DefinitionVersion == "" || instance.LockVersion != 0 {
		return statemachine.ErrInvalidStoreInput
	}
	state, err := store.stateCodec.Encode(instance.State)
	if err != nil {
		return fmt.Errorf("postgres: encode state: %w", err)
	}
	tag, err := store.pool.Exec(ctx, fmt.Sprintf(`
INSERT INTO %s.state_machine_instances
    (id, state, initial_state, definition_version,
     initial_definition_version, lock_version)
VALUES ($1, $2, $2, $3, $3, 0)
ON CONFLICT (id) DO NOTHING`, store.schema), instance.ID, state, instance.DefinitionVersion)
	if err != nil {
		return fmt.Errorf("postgres: create instance: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return statemachine.ErrStoreExists
	}
	return nil
}

// Load returns the current state and lock version.
func (store *Store[S, E]) Load(ctx context.Context, id statemachine.InstanceID) (statemachine.Instance[S], error) {
	var encodedState string
	var version string
	var lockVersion int64
	err := store.pool.QueryRow(ctx, fmt.Sprintf(`
SELECT state, definition_version, lock_version
FROM %s.state_machine_instances WHERE id = $1`, store.schema), id).Scan(&encodedState, &version, &lockVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return statemachine.Instance[S]{}, statemachine.ErrStoreNotFound
	}
	if err != nil {
		return statemachine.Instance[S]{}, fmt.Errorf("postgres: load instance: %w", err)
	}
	state, err := store.stateCodec.Decode(encodedState)
	if err != nil {
		return statemachine.Instance[S]{}, fmt.Errorf("postgres: decode state: %w", err)
	}
	return statemachine.Instance[S]{
		ID: id, State: state, DefinitionVersion: statemachine.Version(version), LockVersion: uint64(lockVersion),
	}, nil
}

type resultDocument struct {
	DefinitionVersion string                `json:"definition_version"`
	Previous          string                `json:"previous"`
	Next              string                `json:"next"`
	Event             string                `json:"event"`
	TransitionID      string                `json:"transition_id"`
	Metadata          statemachine.Metadata `json:"metadata"`
	Effects           []statemachine.Effect `json:"effects"`
}

// CompareAndTransition atomically updates state, appends history, and inserts
// one outbox row per planned effect.
func (store *Store[S, E]) CompareAndTransition(ctx context.Context, id statemachine.InstanceID, expected uint64, result statemachine.Result[S, E], occurredAt time.Time) (statemachine.Instance[S], statemachine.HistoryEntry[S, E], error) {
	document, encoded, err := store.encodeResult(result)
	if err != nil {
		return statemachine.Instance[S]{}, statemachine.HistoryEntry[S, E]{}, err
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return statemachine.Instance[S]{}, statemachine.HistoryEntry[S, E]{}, fmt.Errorf("postgres: begin transition: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var nextLock int64
	err = tx.QueryRow(ctx, fmt.Sprintf(`
UPDATE %s.state_machine_instances
SET state = $1, definition_version = $2, lock_version = lock_version + 1
WHERE id = $3 AND lock_version = $4 AND state = $5
RETURNING lock_version`, store.schema), document.Next, document.DefinitionVersion, id, expected, document.Previous).Scan(&nextLock)
	if errors.Is(err, pgx.ErrNoRows) {
		return statemachine.Instance[S]{}, statemachine.HistoryEntry[S, E]{}, store.conflictReason(ctx, tx, id)
	}
	if err != nil {
		return statemachine.Instance[S]{}, statemachine.HistoryEntry[S, E]{}, fmt.Errorf("postgres: update instance: %w", err)
	}
	_, err = tx.Exec(ctx, fmt.Sprintf(`
INSERT INTO %s.state_machine_history
    (instance_id, sequence, result, occurred_at)
VALUES ($1, $2, $3, $4)`, store.schema), id, nextLock, encoded, occurredAt)
	if err != nil {
		return statemachine.Instance[S]{}, statemachine.HistoryEntry[S, E]{}, fmt.Errorf("postgres: append history: %w", err)
	}
	for index, effect := range result.Effects {
		outboxID := store.newID()
		if outboxID == "" {
			return statemachine.Instance[S]{}, statemachine.HistoryEntry[S, E]{}, fmt.Errorf("%w: empty outbox ID", statemachine.ErrInvalidStoreInput)
		}
		payload := effect.Payload
		if payload == nil {
			payload = []byte{}
		}
		_, err = tx.Exec(ctx, fmt.Sprintf(`
INSERT INTO %s.state_machine_outbox
    (id, instance_id, sequence, effect_index, kind, payload, occurred_at,
     available_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7)`, store.schema), outboxID, id, nextLock, index, effect.Kind, payload, occurredAt)
		if err != nil {
			return statemachine.Instance[S]{}, statemachine.HistoryEntry[S, E]{}, fmt.Errorf("postgres: insert outbox: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return statemachine.Instance[S]{}, statemachine.HistoryEntry[S, E]{}, fmt.Errorf("postgres: commit transition: %w", err)
	}
	instance := statemachine.Instance[S]{
		ID: id, State: result.Next, DefinitionVersion: result.DefinitionVersion, LockVersion: uint64(nextLock),
	}
	entry := statemachine.HistoryEntry[S, E]{
		InstanceID: id, Sequence: uint64(nextLock), Result: cloneResult(result), OccurredAt: occurredAt,
	}
	return instance, entry, nil
}

func (store *Store[S, E]) conflictReason(ctx context.Context, tx transaction, id statemachine.InstanceID) error {
	var exists bool
	err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT EXISTS (
    SELECT 1 FROM %s.state_machine_instances WHERE id = $1
)`, store.schema), id).Scan(&exists)
	if err != nil {
		return fmt.Errorf("postgres: inspect conflict: %w", err)
	}
	if !exists {
		return statemachine.ErrStoreNotFound
	}
	return statemachine.ErrStoreConflict
}

// History returns entries with sequence strictly greater than after.
func (store *Store[S, E]) History(ctx context.Context, id statemachine.InstanceID, after uint64, limit int) ([]statemachine.HistoryEntry[S, E], error) {
	if limit < 0 || limit > statemachine.MaxHistoryPageLimit {
		return nil, statemachine.ErrInvalidStoreInput
	}
	if limit == 0 {
		limit = statemachine.DefaultHistoryPageLimit
	}
	rows, err := store.pool.Query(ctx, fmt.Sprintf(`
SELECT sequence, result, occurred_at
FROM %s.state_machine_history
WHERE instance_id = $1 AND sequence > $2
ORDER BY sequence LIMIT $3`, store.schema), id, after, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: query history: %w", err)
	}
	defer rows.Close()
	entries := make([]statemachine.HistoryEntry[S, E], 0)
	for rows.Next() {
		var sequence int64
		var encoded []byte
		var occurredAt time.Time
		if err := rows.Scan(&sequence, &encoded, &occurredAt); err != nil {
			return nil, fmt.Errorf("postgres: scan history: %w", err)
		}
		result, err := store.decodeResult(encoded)
		if err != nil {
			return nil, err
		}
		entries = append(entries, statemachine.HistoryEntry[S, E]{
			InstanceID: id, Sequence: uint64(sequence), Result: result, OccurredAt: occurredAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: iterate history: %w", err)
	}
	if len(entries) == 0 {
		var exists bool
		if err := store.pool.QueryRow(ctx, fmt.Sprintf(`SELECT EXISTS (
    SELECT 1 FROM %s.state_machine_instances WHERE id = $1
)`, store.schema), id).Scan(&exists); err != nil {
			return nil, fmt.Errorf("postgres: inspect history instance: %w", err)
		}
		if !exists {
			return nil, statemachine.ErrStoreNotFound
		}
	}
	return entries, nil
}

// SaveSnapshot stores a replay boundary after verifying it against history.
func (store *Store[S, E]) SaveSnapshot(ctx context.Context, snapshot statemachine.Snapshot[S]) error {
	state, err := store.stateCodec.Encode(snapshot.State)
	if err != nil {
		return fmt.Errorf("postgres: encode snapshot state: %w", err)
	}
	var expectedState, expectedVersion string
	if snapshot.LockVersion == 0 {
		err = store.pool.QueryRow(ctx, fmt.Sprintf(`
SELECT initial_state, initial_definition_version
FROM %s.state_machine_instances WHERE id = $1`, store.schema), snapshot.InstanceID).Scan(&expectedState, &expectedVersion)
	} else {
		err = store.pool.QueryRow(ctx, fmt.Sprintf(`
SELECT result->>'next', result->>'definition_version'
FROM %s.state_machine_history
WHERE instance_id = $1 AND sequence = $2`, store.schema), snapshot.InstanceID, snapshot.LockVersion).Scan(&expectedState, &expectedVersion)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return statemachine.ErrStoreNotFound
	}
	if err != nil {
		return fmt.Errorf("postgres: validate snapshot: %w", err)
	}
	if state != expectedState || string(snapshot.DefinitionVersion) != expectedVersion {
		return statemachine.ErrInvalidStoreInput
	}
	tag, err := store.pool.Exec(ctx, fmt.Sprintf(`
INSERT INTO %s.state_machine_snapshots
    (instance_id, state, definition_version, lock_version, created_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (instance_id) DO UPDATE SET
    state = EXCLUDED.state,
    definition_version = EXCLUDED.definition_version,
    lock_version = EXCLUDED.lock_version,
    created_at = EXCLUDED.created_at
WHERE %s.state_machine_snapshots.lock_version <= EXCLUDED.lock_version`, store.schema, store.schema),
		snapshot.InstanceID, state, snapshot.DefinitionVersion, snapshot.LockVersion, snapshot.CreatedAt)
	if err != nil {
		return fmt.Errorf("postgres: save snapshot: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return statemachine.ErrStoreConflict
	}
	return nil
}

// LoadSnapshot returns the latest saved replay boundary.
func (store *Store[S, E]) LoadSnapshot(ctx context.Context, id statemachine.InstanceID) (statemachine.Snapshot[S], error) {
	var encodedState, version string
	var lockVersion int64
	var createdAt time.Time
	err := store.pool.QueryRow(ctx, fmt.Sprintf(`
SELECT state, definition_version, lock_version, created_at
FROM %s.state_machine_snapshots WHERE instance_id = $1`, store.schema), id).Scan(&encodedState, &version, &lockVersion, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return statemachine.Snapshot[S]{}, statemachine.ErrStoreNotFound
	}
	if err != nil {
		return statemachine.Snapshot[S]{}, fmt.Errorf("postgres: load snapshot: %w", err)
	}
	state, err := store.stateCodec.Decode(encodedState)
	if err != nil {
		return statemachine.Snapshot[S]{}, fmt.Errorf("postgres: decode snapshot state: %w", err)
	}
	return statemachine.Snapshot[S]{
		InstanceID: id, State: state, DefinitionVersion: statemachine.Version(version),
		LockVersion: uint64(lockVersion), CreatedAt: createdAt,
	}, nil
}

func (store *Store[S, E]) encodeResult(result statemachine.Result[S, E]) (resultDocument, []byte, error) {
	previous, err := store.stateCodec.Encode(result.Previous)
	if err != nil {
		return resultDocument{}, nil, fmt.Errorf("postgres: encode previous state: %w", err)
	}
	next, err := store.stateCodec.Encode(result.Next)
	if err != nil {
		return resultDocument{}, nil, fmt.Errorf("postgres: encode next state: %w", err)
	}
	event, err := store.eventCodec.Encode(result.Event)
	if err != nil {
		return resultDocument{}, nil, fmt.Errorf("postgres: encode event: %w", err)
	}
	if result.DefinitionVersion == "" {
		return resultDocument{}, nil, statemachine.ErrInvalidStoreInput
	}
	document := resultDocument{
		DefinitionVersion: string(result.DefinitionVersion), Previous: previous,
		Next: next, Event: event, TransitionID: string(result.TransitionID),
		Metadata: result.Metadata, Effects: cloneEffects(result.Effects),
	}
	marshal := store.marshal
	if marshal == nil {
		marshal = json.Marshal
	}
	encoded, err := marshal(document)
	if err != nil {
		return resultDocument{}, nil, fmt.Errorf("postgres: encode result: %w", err)
	}
	return document, encoded, nil
}

func (store *Store[S, E]) decodeResult(encoded []byte) (statemachine.Result[S, E], error) {
	var document resultDocument
	if err := json.Unmarshal(encoded, &document); err != nil {
		return statemachine.Result[S, E]{}, fmt.Errorf("postgres: decode result: %w", err)
	}
	previous, err := store.stateCodec.Decode(document.Previous)
	if err != nil {
		return statemachine.Result[S, E]{}, fmt.Errorf("postgres: decode previous state: %w", err)
	}
	next, err := store.stateCodec.Decode(document.Next)
	if err != nil {
		return statemachine.Result[S, E]{}, fmt.Errorf("postgres: decode next state: %w", err)
	}
	event, err := store.eventCodec.Decode(document.Event)
	if err != nil {
		return statemachine.Result[S, E]{}, fmt.Errorf("postgres: decode event: %w", err)
	}
	return statemachine.Result[S, E]{
		DefinitionVersion: statemachine.Version(document.DefinitionVersion),
		Previous:          previous, Next: next, Event: event,
		TransitionID: statemachine.TransitionID(document.TransitionID),
		Metadata:     document.Metadata, Effects: cloneEffects(document.Effects),
	}, nil
}

func cloneResult[S statemachine.State, E statemachine.Event](result statemachine.Result[S, E]) statemachine.Result[S, E] {
	result.Effects = cloneEffects(result.Effects)
	return result
}

func cloneEffects(effects []statemachine.Effect) []statemachine.Effect {
	if effects == nil {
		return nil
	}
	cloned := make([]statemachine.Effect, len(effects))
	for index, effect := range effects {
		cloned[index] = effect
		cloned[index].Payload = append([]byte(nil), effect.Payload...)
	}
	return cloned
}
