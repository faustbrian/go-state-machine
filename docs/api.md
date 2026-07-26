# API reference

This page groups the complete exported surface by responsibility. GoDoc remains
the source for exact signatures.

## Definitions and compilation

- `State` and `Event` constrain typed identifiers to comparable values.
- `TransitionID`, `Version`, and `InstanceID` are stable persisted identities.
- `Definition`, `StateDefinition`, and `TransitionDefinition` describe an
  uncompiled graph.
- `Compile` validates with `DefaultLimits`.
- `CompileWithLimits` validates with application-selected `Limits`.
- `Machine` is the immutable compiled result.
- `Graph` is a deep, ordered inspection copy returned by `Machine.Graph`.
- `Diagnostic`, `DiagnosticCode`, and `DiagnosticsError` report construction
  defects without requiring error-string parsing.

Compile diagnostics cover missing identities, duplicate states or transitions,
unknown states, invalid wildcard sources, exact and wildcard ambiguity,
terminal outgoing transitions, unreachable states, invalid effects, and limit
violations.

## Transition calculation

- `Guard[C]` returns either `nil` or a structured `Rejection`.
- `Metadata` carries correlation and causation identifiers.
- `Effect` is inert kind/payload data.
- `Result` contains definition version, prior and next state, event, chosen
  transition, metadata, and the ordered effect plan.
- `Machine.Transition` calculates one result without I/O.

Expected transition failures are discoverable with `errors.Is` and
`errors.As`: `ErrUnknownState`, `ErrTerminalState`, `ErrNoTransition`,
`ErrGuardRejected`, `GuardRejectedError`, `ErrGuardPanic`, `GuardPanicError`,
and `ErrLimitExceeded`.

## Replay, history, and evolution

- `Input`, `ReplayResult`, and `ReplayError` support deterministic execution
  replay through `Machine.Replay` and `Machine.ReplayFrom`.
- `HistoryEntry`, `Snapshot`, `HistoryFailure`, and `HistoryError` support
  persisted-history validation.
- `ValidateHistory` and `ValidateHistoryWithLimit` detect continuity and
  identity corruption without executing effects.
- `Machine.ValidateHistory` additionally proves compatibility with the exact
  compiled definition after any required migration.
- `Migration`, `Evolution`, `CompileEvolution`, and `MigrationError` provide
  explicit state/event renames across definition versions.
- `ErrInvalidEvolution` and `ErrMissingMigration` identify invalid graphs and
  migration gaps.

## Persistence

- `Instance` contains current state, definition version, and lock version.
- `Store` defines create, load, bounded history paging,
  compare-and-transition, and snapshot behavior. `DefaultHistoryPageLimit` and
  `MaxHistoryPageLimit` define portable page bounds.
- `StoreCapabilities` reports atomic comparison, append-only history,
  snapshots, and atomic outbox support.
- Store errors are `ErrStoreNotFound`, `ErrStoreExists`, `ErrStoreConflict`, and
  `ErrInvalidStoreInput`.

The `memory.Store` and `postgres.Store` implement the contract. The PostgreSQL
package also exports `Codec`, `TextCodec`, `Options`, `New`, `Migrate`, and
`ErrInvalidOptions`.

## Effect execution

The `runner` package exports `Handler`, `Recorder`, `Options`, `Runner`,
`Record`, `Outcome`, and `Classifier`. Construction and execution errors are
`ErrMissingHandler`, `ErrReentrant`, `ErrHandlerPanic`, `EffectError`, and
`RecorderError`.

The `outbox` package exports `Message`, `Claim`, `ClaimRequest`, `LeaseRef`,
`Store`, `Publisher`, `RelayOptions`, `Relay`, `Result`, and `FailureClass`.
Errors are `ErrInvalidOptions`, `ErrInvalidClaim`, `ErrLeaseLost`,
`ErrPublisherPanic`, and `OperationError`.

## Diagrams and conformance

`diagram.New` and `NewWithLimits` construct a typed `Renderer`.
`Renderer.Mermaid` and `Renderer.Graphviz` emit deterministic text for trusted
compiled graphs. `MermaidChecked` and `GraphvizChecked` validate and bound
imported graphs. Diagram errors report missing labelers, invalid graphs, and
limit violations.

`statemachinetest.StoreContract` and `RunnerContract` are reusable backend and
wrapper tests. `StoreFixture`, `RunnerFactory`, and `EffectExecutor` provide
their typed setup contracts.
