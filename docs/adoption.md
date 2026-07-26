# Adoption guide

## 1. Identify a real finite-state model

List stable states, triggering events, terminal states, and legal edges. If the
problem primarily schedules long-running tasks, branches over arbitrary rules,
coordinates services, or compensates distributed work, use a workflow, rule,
or saga system instead.

## 2. Introduce typed identifiers

Use dedicated comparable Go types. Choose serialized values that can remain
stable across code renames. Add explicit PostgreSQL codecs when the underlying
type is not string based.

## 3. Compile beside existing behavior

Model transitions and compare pure results with current decisions. Use
`machine.Graph()` in review. Do not execute effects during this shadow phase.

## 4. Move side effects into plans

Represent application work as stable `Effect.Kind` values and versioned
payloads. Keep guards pure. Add handler tests that prove ordering and failure
classification.

## 5. Choose persistence deliberately

Use `memory` only for ephemeral state. Use PostgreSQL for durable optimistic
locking, history, snapshots, and atomic outbox insertion. Run the reusable
conformance contract for custom stores.

## 6. Establish recovery

Validate history, exercise snapshot replay, document conflict retries, make
effect consumers idempotent, and test crashes before and after publish
acknowledgement.

## 7. Version changes explicitly

Persist definition versions from day one. Add migration hooks and fixtures
before renaming serialized state or event identifiers.
