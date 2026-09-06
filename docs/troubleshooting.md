# Troubleshooting

## Compilation fails

Use `errors.As` to inspect `*statemachine.DiagnosticsError` and its complete
diagnostic list. Correct duplicate or ambiguous transitions, unknown targets,
unreachable states, terminal outgoing edges, invalid effects, or exceeded
limits in the definition; do not choose a transition from a partially valid
graph.

## A transition is rejected or missing

Use `errors.Is` to distinguish `ErrUnknownState`, `ErrTerminalState`,
`ErrNoTransition`, `ErrGuardRejected`, `ErrGuardFailed`, `ErrGuardPanic`, and
context cancellation. An exact transition that rejects does not fall back to a
wildcard. Inspect the compiled graph, current state, event, definition version,
and structured rejection without logging sensitive typed context.

## Persistence reports a conflict

`ErrStoreConflict` reports a stored lock or snapshot-version conflict. After a
compare-and-transition conflict, reload the instance and recalculate against
its new state; another writer may have advanced the optimistic lock. Do not
retry the previous `Result`, because its prior state and effect plan may be
stale. After a snapshot conflict, load the latest snapshot rather than moving
its persisted lock version backward.

## Outbox messages repeat or stop

Duplicate delivery is expected when publication succeeds but acknowledgement
does not. Deduplicate by stable message identity. For stopped delivery, inspect
backlog age, lease ownership and expiry, retry classification, dead letters,
publisher errors, and worker cancellation. `ErrLeaseLost` means the claim is no
longer authoritative; do not acknowledge it again.

## Replay is slow or history validation fails

Page history within `MaxHistoryPageLimit`, snapshot at a validated lock version,
and use `ReplayFrom` to bound replay length. A `HistoryError` identifies the
first invalid entry and failure class. Preserve the evidence and repair the
source record or migration; do not skip the entry or rewrite immutable history.

## Shutdown hangs or loses work

The library owns no worker lifecycle. Stop the application's worker loop,
cancel in-flight contexts, wait for handlers and publishers to return, and close
the caller-owned database pool last. A context cancellation cannot reverse an
external side effect that completed before the collaborator returned.
