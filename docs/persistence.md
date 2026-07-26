# Persistence

The root `Store` contract keeps persistence optional. Every store exposes its
guarantees through `Capabilities`.

## Memory

```go
store := memory.New[OrderState, OrderEvent]()
err := store.Create(ctx, statemachine.Instance[OrderState]{
    ID: "order-42", State: Pending, DefinitionVersion: "v1",
})
```

`memory.Store` is concurrency safe and atomically updates current state and
append-only history under one lock. It is intended for tests, development, and
ephemeral processes, not durable recovery.

## PostgreSQL

```go
store, err := postgres.New(postgres.Options[OrderState, OrderEvent]{
    Pool: pool,
    Schema: "state_machine",
    StateCodec: postgres.TextCodec[OrderState](),
    EventCodec: postgres.TextCodec[OrderEvent](),
    NewID: newUUID,
    Clock: time.Now,
})
if err != nil { /* handle */ }
if err := store.Migrate(ctx); err != nil { /* handle */ }
```

Custom codecs let applications preserve stable serialized identifiers even if
Go symbol names change.

`CompareAndTransition` performs one database transaction:

1. update the instance only when lock version and prior state match;
2. append the transition result at the new sequence;
3. insert one outbox record per planned effect;
4. commit all records together.

Any failure rolls the whole transaction back. A stale lock returns
`ErrStoreConflict`; callers must reload and recalculate. PostgreSQL does not
execute a previously calculated result after silently rebasing it.

Snapshots are validated against initial state or the history entry at their
lock version. `History(after, limit)` returns ascending append-only entries.
A zero limit uses `DefaultHistoryPageLimit`; values above
`MaxHistoryPageLimit` are rejected.
Use `ValidateHistory` before trusting imported or externally transferred
history.

Call `statemachinetest.StoreContract` from custom store tests. The contract does
not replace backend-specific transaction, crash, and contention tests.
