# Goal: Deterministic State Machines for Go

## Objective

Build `state-machine` as an open-source library for explicit, deterministic,
typed state transitions with guards, planned effects, history, and optional
durable persistence.

It MUST model state machines rather than become a general workflow engine,
scheduler, rule engine, queue, saga framework, or hidden dependency-injection
container.

## Core Principles

- Machine definitions MUST be immutable after compilation.
- States and events MUST be strongly typed without forcing stringly typed
  application code.
- Transition selection MUST be deterministic and independently testable.
- Guards MUST be side-effect free.
- Effects MUST be explicit outputs or explicitly invoked handlers, never hidden
  work performed during transition lookup.
- Invalid and ambiguous machines MUST fail at construction or compilation.
- Clocks, identifiers, persistence, and effect dispatch MUST be injectable.
- The root package MUST remain usable without database or queue dependencies.

## Required Model

The package MUST support:

- typed states, events, transition identifiers, and machine versions;
- source and destination states, self-transitions, terminal states, and
  wildcard transitions with unambiguous precedence;
- guards with typed context and structured rejection reasons;
- entry, exit, and transition effects represented as ordered plans;
- immutable compiled definitions and inspectable transition graphs;
- transition results containing prior state, next state, event, chosen
  transition, metadata, and planned effects;
- event metadata including correlation and causation identifiers;
- deterministic replay from an initial state and event sequence;
- versioned definition evolution and migration hooks; and
- visualization/export suitable for Mermaid, Graphviz, and documentation.

Hierarchical states, parallel regions, history states, and timed transitions MAY
be added only as complete, formally documented semantics. They MUST NOT be
approximated through surprising implicit behavior.

## Execution And Effects

The pure transition engine MUST calculate a transition without performing IO.
An optional runner MAY execute planned effects with explicit ordering,
cancellation, retry classification, and result recording.

Exactly-once effects MUST NOT be claimed. Durable integrations SHOULD use
`outbox`, `idempotency`, `retry`, `queue`, and `correlation` to
provide observable at-least-once execution and safe deduplication.

Reentrant transitions, nested dispatch, guard panics, effect panics, and
cancellation MUST have documented behavior.

## Persistence

Provide an optional store contract with:

- current state, machine definition version, and optimistic lock version;
- append-only transition history;
- atomic compare-and-transition behavior where supported;
- snapshot and replay support; and
- explicit capability reporting.

First-party memory and PostgreSQL stores SHOULD be provided. PostgreSQL writes
MUST define the atomic boundary between state, history, and outbox records.

## Package Structure

Prefer focused packages such as:

- `statemachine` for definitions, compilation, and pure transitions;
- `runner` for explicit effect execution;
- `memory` and `postgres` for optional persistence;
- `outbox` for durable effect publication integration;
- `diagram` for graph exports; and
- `statemachinetest` for reusable store and runner conformance tests.

## Testing And Quality

- Meaningful 100% statement coverage is REQUIRED.
- Table and property tests MUST cover transition precedence and graph
  validation.
- Model-based tests MUST compare execution and replay.
- Fuzz tests MUST cover serialized events, definitions, history, and imports.
- Race tests MUST cover compiled machines, stores, runners, and concurrent
  transitions.
- Integration tests MUST exercise real PostgreSQL transaction and locking
  behavior.
- Mutation testing MUST prove guards, transition selection, and effect-order
  assertions are meaningful.
- Benchmarks MUST measure compilation, hot transitions, guard sets, replay,
  history growth, and contended persistence.

## Documentation And Delivery

Documentation MUST provide a quick start, complete API reference, diagrams,
guard/effect guidance, persistence examples, definition-version migration,
outbox integration, replay/debugging, concurrency semantics, adoption guide,
FAQ, and boundaries versus workflows, sagas, and rule engines.

CI MUST enforce formatting, vetting, strict linting, tests, race tests, fuzz
smoke tests, coverage, mutation, vulnerability and dependency review,
documentation validation, examples, and benchmark tracking. Every gate MUST be
runnable locally.

## Completion Criteria

Completion requires the pure engine, compiler, diagnostics, explicit effects,
optional persistence, replay, diagrams, conformance tests, documentation, CI,
benchmarks, and meaningful 100% coverage. A map of callbacks keyed by state does
not satisfy this goal.
