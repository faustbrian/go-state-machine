# Replay and debugging

`Machine.Replay` starts at the compiled initial state. `ReplayFrom` starts at a
snapshot state. Both call the same pure transition logic as live calculation,
so guards and transition precedence remain identical.

```go
replay, err := machine.Replay(ctx, []statemachine.Input[Event, Context]{
    {Event: Created, Context: account, Metadata: metadata1},
    {Event: Paid, Context: account, Metadata: metadata2},
})
```

A `ReplayError` exposes the failing input index and unwraps the transition
error, but does not render event or context values.

Persisted history is different from event replay: it contains selected results,
not guard inputs. Use `ValidateHistory(snapshot, entries)` to verify instance
identity, exact sequence continuity, transition/version identity, and prior
state continuity. It returns the reconstructed final snapshot without running
guards or effects. After any required migration, use
`machine.ValidateHistory(snapshot, entries)` to additionally reject definition
versions, transition identifiers, event identifiers, or destinations that are
incompatible with the exact compiled machine.

Debugging procedure:

1. capture the compiled definition version and `machine.Graph()`;
2. inspect `DiagnosticsError` if compilation failed;
3. preserve correlation and causation IDs from the failing input;
4. reproduce with `Transition` or `Replay` and the same typed context;
5. compare the chosen transition and planned effect order;
6. validate persisted history independently;
7. never re-run effects merely to inspect selection.

Resource limits bound replay length and metadata. Tighten them with
`CompileWithLimits` for untrusted inputs.
