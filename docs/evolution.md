# Definition evolution

Definition versions are persisted with current state and every history result.
Changing identifiers requires an explicit migration; changing a Go constant
name alone does not.

```go
evolution, err := statemachine.CompileEvolution([]statemachine.Migration[OrderState, OrderEvent]{
    {
        From: "v1", To: "v2",
        State: func(value OrderState) (OrderState, error) {
            if value == "pending" { return "awaiting-payment", nil }
            return value, nil
        },
        Event: func(value OrderEvent) (OrderEvent, error) {
            if value == "pay" { return "capture", nil }
            return value, nil
        },
    },
})
```

`CompileEvolution` rejects empty versions, self-edges, multiple successors, and
cycles. `Evolution.Migrate` copies and upgrades a snapshot plus each history
entry through every required step. Nil hooks are identity conversions. Missing
steps return `ErrMissingMigration`; hook failures return `MigrationError`
without rendering state or event values.

Direct evolution uses finite defaults: at most 256 migration records, 10,000
history entries, 100,000 total migration-edge applications across the snapshot
and history, 10,000 carried effects, 1 MiB per effect payload, 16 MiB of carried
payload occurrences in total, and 256 UTF-8 bytes per version identifier.
`CompileEvolutionWithLimits` accepts a complete, positive `EvolutionLimits` to
select different finite bounds; an all-zero value selects the defaults, while
partial or nonpositive limits return `ErrInvalidEvolution`. The selected limits
are copied into the compiled evolution. Over-budget inputs return
`ErrLimitExceeded` with zero snapshot and nil history before any migration hook
or output-sized allocation. An empty target returns `ErrInvalidEvolution`
first; otherwise a pre-canceled context returns its error before size checks.

Migration hooks are application-owned code. The library checks cancellation
between calls but cannot preempt a running hook, make its side effects
transactional, or bound its CPU use or returned state/event size. State and
event types are shallow `comparable` values; callers own referenced data and
large inline values. Retrying a migration may invoke the same hooks again, so
hooks with side effects need application-level idempotency.

Recommended upgrade sequence:

1. deploy code that understands old and new serialized identifiers;
2. stop writes for the instance or use an application-owned migration lock;
3. load and validate snapshot/history;
4. migrate them to the target version;
5. validate the migrated continuity;
6. persist through an application-owned migration transaction;
7. enable the new compiled definition.

The library does not guess whether two definitions are semantically compatible.
