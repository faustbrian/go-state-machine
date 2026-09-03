# Concurrency semantics

Compiled machines contain copied definitions and read-only lookup tables. They
are safe for concurrent `Transition`, `Replay`, and `Graph` calls. Returned
graphs, effects, and persisted history payloads are copies.

Guards receive the caller's typed context. Reference-bearing contexts MUST
configure `Definition.CloneContext` with a deep clone so each guard receives
an isolated value. Cloners themselves must be safe for concurrent use.

`memory.Store` serializes writes and uses optimistic lock versions to prevent
lost updates. `postgres.Store` performs a conditional update inside the same
transaction as history and outbox insertion. Concurrent writers must treat
`ErrStoreConflict` as a signal to reload and recalculate.

`runner.Runner` is safe for concurrent independent `Execute` calls when its
handler, recorder, classifier, and clock are safe. Nested execution using the
same runner and derived context returns `ErrReentrant`.

`outbox.Relay` processes one claimed batch serially. Multiple relay processes
may claim concurrently because PostgreSQL uses row locks and `SKIP LOCKED`.
Lease expiry permits recovery after cancellation or process failure and can
cause duplicate delivery.

## Retained collaborators

Constructed values borrow function and interface collaborators for their
complete lifetime. Callers own those collaborators and must keep them valid
until the constructed value is no longer used. The library does not clone or
dynamically replace them. Callers must not replace or mutate their behavior
while an operation is using the constructed value, and must synchronize any
collaborator state shared across concurrent operations.

- A compiled `Machine` retains every `Guard`, `CheckedGuard`, and
  `Definition.CloneContext` function. `Compile` copies definition slices and
  effect payloads, but callers remain responsible for the concurrency safety
  and lifetime of those functions and anything they capture.
- A compiled `Evolution` retains each `Migration.State` and `Migration.Event`
  function. Callers must keep their captured state valid and concurrency safe
  for every concurrent `Migrate` call.
- A `diagram.Renderer` retains its state and event label functions. Callers
  must keep their captured state valid and safe for every concurrent render.
- A `runner.Runner` retains the `Handler`, `Options.Recorder`, `Options.Clock`,
  and `Options.Classify` collaborators. They remain caller-owned and must be
  valid and concurrency safe for every `Execute` call.
- An `outbox.Relay` retains `RelayOptions.Store`, `Publisher`, `Clock`,
  `Classify`, and `RetryDelay`. They remain caller-owned and must be valid for
  every `RunOnce` call; concurrent calls require all five collaborators to
  support that concurrency.
- A `postgres.Store` retains `Options.Pool`, both codec functions,
  `Options.NewID`, and `Options.Clock`. They remain caller-owned and must be
  valid and concurrency safe for every store operation. The store never closes
  `Options.Pool`; the caller must close it only after all store and relay
  operations that use it have stopped.

`memory.Store` retains no external collaborator or resource.

Cancellation is checked before transition selection, before guards, before
effect handling, before relay publication, and by store calls. Cancellation
does not roll back work that an external handler or publisher completed before
returning.
