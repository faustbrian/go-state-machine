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

Cancellation is checked before transition selection, before guards, before
effect handling, before relay publication, and by store calls. Cancellation
does not roll back work that an external handler or publisher completed before
returning.
