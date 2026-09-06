# Operations

The root machine, diagram renderer, runner, and memory store start no background
goroutines and expose no shutdown method. Applications own every call, context,
effect handler, publisher, database pool, and worker loop.

## Production checklist

1. Compile and validate the exact definition version before accepting work.
2. Persist stable state, event, transition, effect, and definition identifiers;
   add explicit migrations before changing serialized identities.
3. Set deadlines on transition guards, store calls, handlers, and publishers.
   Cancellation stops later library work but cannot undo external work already
   completed by a collaborator.
4. Treat `ErrStoreConflict` as a reload-and-recalculate boundary. Never apply a
   result calculated from a stale lock version.
5. Run outbox publication from an application-owned worker that calls
   `Relay.RunOnce`. Stop scheduling calls, cancel in-flight contexts, wait for
   them to return, and only then close the caller-owned PostgreSQL pool.
6. Make effect consumers idempotent. Publication is at least once because a
   crash after publish but before acknowledgement can produce a duplicate.

## Signals to monitor

- compile diagnostics and rejected transition counts by stable error class;
- optimistic-lock conflicts and retries;
- history length, snapshot age, and replay duration;
- outbox backlog, oldest ready message age, lease loss, retries, dead letters,
  and publisher latency;
- PostgreSQL transaction latency, pool saturation, and migration failures.

Do not log state contexts, effect payloads, database credentials, or correlation
identifiers unless the application has explicitly classified and sanitized
them. Use stable error categories and bounded metadata instead.

## Recovery

After a crash, load the latest validated snapshot and bounded history, validate
the history against the intended compiled definition, and resume from the
persisted lock version. Expired outbox leases may be reclaimed. Investigate
dead letters before replaying them, because permanent failures are deliberately
not rescheduled by the relay.
