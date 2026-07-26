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

Recommended upgrade sequence:

1. deploy code that understands old and new serialized identifiers;
2. stop writes for the instance or use an application-owned migration lock;
3. load and validate snapshot/history;
4. migrate them to the target version;
5. validate the migrated continuity;
6. persist through an application-owned migration transaction;
7. enable the new compiled definition.

The library does not guess whether two definitions are semantically compatible.
