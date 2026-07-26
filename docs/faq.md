# FAQ

## Why are states and events generic comparable types?

Applications keep compile-time distinctions instead of passing arbitrary
strings. Comparable values also support deterministic lookup maps.

## Are effects executed by `Transition`?

No. It returns copied inert plans. Use `runner` or durable `outbox` explicitly.

## What happens on a self-transition?

It is an external self-transition: exit, transition, and entry effects are
planned in that order.

## Can a rejected exact transition fall back to a wildcard?

No. Precedence selects the exact edge first; its rejection is the result.

## Can two guarded transitions share a state and event?

No. They could both accept, making selection ambiguous. Put ordered guard
conditions on one transition or model distinct events.

## Does the outbox provide exactly-once publication?

No. Publication followed by a failed acknowledgement can be delivered again.
Use stable IDs and idempotent consumers.

## When should I snapshot?

Snapshot after a committed lock version when replay cost justifies it. The
store validates the snapshot against initial state or append-only history.

## How do I rename a state?

Keep serialized identifiers stable, or add a `Migration` step and migrate both
snapshot and history before enabling the target definition.

## Can guards call services?

They technically receive a context, but the contract prohibits side effects
and nondeterministic I/O. Preload required facts into typed context.

## Why does compilation reject unreachable states?

Unreachable nodes usually indicate stale or incomplete domain definitions and
make diagrams and evolution misleading.
