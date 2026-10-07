# Changelog

All notable changes to this project are documented in this file. The format is
based on Keep a Changelog, and the project follows Semantic Versioning.

## [Unreleased] (v2.0.0)

The root source tree now uses the `/v2` module path. Version 2 is an unpublished
release candidate requiring release qualification before publication. Actual
public consumer verification and affected owned adoption follow publication
before package security closure. Existing consumers must remain on released v1
without local `replace` directives until v2 is published.

### Security

- Refuse oversized definition effects, payloads, sources, and guards before
  copying or constructing the compiled graph. Resource-limit diagnostics now
  take precedence over semantic diagnostics for rejected definitions.
  Compilation also defaults to 16 MiB of copied effect payloads and 100,000
  aggregate elements, charging repeated occurrences separately. Configure
  `Limits.MaxCompiledEffectPayloadBytes` and `Limits.MaxCompiledElements` for
  larger finite definitions; zero selects the default for these new fields.

- Bound direct definition-evolution compilation and migration by graph,
  history, step, version, effect-count, and payload budgets before callbacks
  or output allocation. Previously accepted oversized migrations now return
  `ErrLimitExceeded`; applications needing larger finite limits can use
  `CompileEvolutionWithLimits`.

- Bound direct effect execution by count, per-payload bytes, and aggregate
  payload bytes before allocation or callbacks. Applications with larger
  compiled plans must configure finite `runner.Options.Limits` explicitly;
  cancellation no longer records effects that were never attempted.

- Enforce machine and encoded-size limits on PostgreSQL state, history, and
  outbox boundaries before database work, redact persisted callback failures,
  and make default public error strings safe to log without exposing payloads
  or wrapped causes. Applications using custom machine limits should construct
  the PostgreSQL store with `postgres.NewWithLimits`.

- Reject malformed negative persisted versions and nonpositive history or
  transition sequences before decoding, returning results, or additional
  writes. Zero instance and snapshot versions remain valid; outbox claims
  retain positive-sequence admission before leasing.

## [1.0.2] - 2026-10-07

### Changed

- Refresh the shared CI workflow while retaining the configured verification
  CLI and PostgreSQL integration checks. State-machine APIs and runtime
  behavior are unchanged.

## [1.0.1] - 2026-10-06

### Changed

- Raise the minimum Go version from 1.26.6 to 1.27.0. Upgrade fixed
  toolchains before adopting this release, or permit Go toolchain selection.

- Upgrade the PostgreSQL driver to pgx v5.11.0 for connection and
  cancellation cleanup fixes. Caller-owned pools using text results now
  receive timestamps in the client-local zone without changing the instant;
  set the timestamptz codec's ScanLocation when a fixed zone is required.
  Review libpq-compatible URI parsing changes for custom connection strings.
  Also review keyword/value backslash handling, especially Windows TLS
  paths, against pgx v5.11.0 configuration guidance.

- Adopt the checksum-verified `go-library-tools` v1.4.0 CLI and immutable W14
  reusable workflow, including strict online specification validation, without
  changing the state-machine API or runtime behavior.

- Adopt the checksum-verified `go-library-tools` v1.3.0 CLI, schema-v2
  cohesion metadata, a local cohesion gate, and immutable shared CI cohesion
  enforcement without changing the state-machine API or runtime behavior.

- Adopt `go-library-tools` v1.0.6 while retaining repository-owned policy,
  evidence, fixtures, and API baselines.

### Fixed

- Preserve atomic PostgreSQL history and outbox persistence with caller-owned
  simple-protocol pools by passing JSON history as text rather than bytea.
  Binary effect payloads and the persisted result schema remain unchanged.

### Documentation

- Add package-specific performance, operations, and troubleshooting guides;
  complete support, security, examples, compatibility, and license navigation;
  and move delivery metadata beyond `not-started` while fresh verification and
  the next patch release remain pending.

- Add the canonical v1 install command, supported Go version, stable maturity,
  and package-selection guidance for State Machine, Workflow, and Temporal.

- Link ecosystem and Domain utilities family guidance to the immutable v1.4.0
  documentation release.

- Link package discovery to the immutable v1.3.0 Golib ecosystem index and
  Domain utilities family guidance.

- Define caller ownership, validity, concurrency, replacement, and shutdown
  responsibilities for every collaborator retained by compiled machines and
  optional runtime packages.

## [1.0.0] - 2026-08-26

### Changed

- Upgrade `moby/go-archive` and `golang.org/x/crypto` to their current
  security-fixed releases and reconcile the resulting indirect dependency
  graph.

- Exclude intentional nested modules from root local-proxy archives so local,
  bootstrap, CI, and public module checksums describe the same source
  boundary.

- Track the pinned documentation-tool lockfile so clean CI checkouts install
  the exact validated cspell dependency.

- Reconcile standalone dependency checksums against deterministic current
  module archives so CI, local verification, and release consumers resolve
  identical content.

- Harden standalone documentation validation with deterministic spelling and
  link checks, package-specific documentation gates, and repository-local
  contributor guidance.

### Changed

- Publish the module from its standalone `github.com/faustbrian/go-state-machine` identity while preserving its documented API and behavior.

### Documentation

- Link the package README to the repository documentation index.

### Fixed

- Validate benchmark output with the standard shell toolchain so clean Linux
  CI runners do not require an undeclared ripgrep installation.
- Upgrade `golang.org/x/text` to v0.41.0 so the dependency graph no longer
  contains GO-2026-5970.
- Bound reachability analysis by the compiled state count while continuing
  past unknown destinations and reporting every disconnected state.
- Reuse and reset an injected PostgreSQL service during repository
  verification while retaining Testcontainers fallback for standalone runs.

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Added

- Immutable typed state-machine compilation with structured diagnostics.
- Deterministic exact and wildcard transition selection with pure guards.
- Ordered exit, transition, and entry effect plans.
- Replay, persisted-history validation, snapshots, and version migrations.
- Mermaid and Graphviz graph export.
- Explicit effect runner with cancellation, panic, and retry classification.
- Memory and PostgreSQL stores with reusable conformance tests.
- Atomic PostgreSQL state, history, and outbox writes.
- Leased at-least-once outbox publication and dead-letter handling.
- Property, model, fuzz, race, integration, and benchmark evidence.

[Unreleased]: https://github.com/faustbrian/go-state-machine/compare/v1.0.2...HEAD
[1.0.0]: https://github.com/faustbrian/go-state-machine/releases/tag/v1.0.0
