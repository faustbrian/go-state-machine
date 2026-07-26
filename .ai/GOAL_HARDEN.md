# Goal: Harden state-machine for Production

## Objective

Prove deterministic state behavior and safe durable execution under malformed
definitions, concurrent transitions, replay, failures, and definition upgrades.

## Definition And Transition Correctness

- Exhaustively test invalid states, unreachable states, duplicate transitions,
  ambiguous wildcards, terminal-state behavior, and conflicting guards.
- Property-test determinism: identical definition, state, event, context, and
  metadata MUST produce identical transition results.
- Verify entry, exit, and transition effect ordering for self and ordinary
  transitions.
- Test guard rejection, errors, panics, cancellation, and context mutation.
- Prove compiled definitions are immutable and safe for concurrent reuse.

## Replay And Evolution

- Model-test live execution against replay over generated event sequences.
- Detect corrupted, missing, duplicated, reordered, and incompatible history.
- Verify definition-version migrations, renamed states/events, and rejected
  incompatible upgrades.
- Test snapshot boundaries and replay from every valid snapshot position.
- Require stable serialized identifiers independent of Go symbol renames.

## Persistence And Effects

- Run every store through one conformance suite.
- Verify optimistic locking prevents lost updates under contention.
- Inject crashes before and after state write, history append, outbox insert,
  effect dispatch, and result recording.
- Prove the documented atomic boundary for PostgreSQL.
- Test duplicate effect delivery, idempotent recovery, retries, dead letters,
  cancellation, and runner restart.
- Reject reentrant or nested execution unless its semantics are explicitly
  enabled and proven.

## Hostile Input And Resource Safety

- Fuzz definition, event, context, history, and snapshot decoders.
- Bound states, transitions, guard count, metadata, history replay, graph
  traversal, and diagram output.
- Reject graph structures that trigger excessive recursion or memory use.
- Ensure errors and diagnostics do not expose sensitive event data.

## Verification Gates

- Meaningful 100% statement coverage.
- Passing race, fuzz, property, model, integration, and mutation suites.
- Stable benchmarks for compilation, transition selection, replay, durable
  writes, and contention.
- Static analysis, vulnerability scanning, dependency review, and license
  checks with no unexplained failures.
- Documentation examples compiled and tested in CI.

## Release Blockers

Release MUST be blocked by nondeterministic transition selection, ambiguous
definitions, replay divergence, lost updates, undocumented effect ordering,
unsafe crash windows, race findings, unbounded hostile input, store conformance
gaps, or meaningful coverage below 100%.
