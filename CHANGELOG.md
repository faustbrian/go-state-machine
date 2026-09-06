# Changelog

All notable changes to this project are documented in this file. The format is
based on Keep a Changelog, and the project follows Semantic Versioning.

## [Unreleased]

### Changed

- Adopt the checksum-verified `go-library-tools` v1.4.0 CLI and immutable W14
  reusable workflow, including strict online specification validation, without
  changing the state-machine API or runtime behavior.

- Adopt the checksum-verified `go-library-tools` v1.3.0 CLI, schema-v2
  cohesion metadata, a local cohesion gate, and immutable shared CI cohesion
  enforcement without changing the state-machine API or runtime behavior.

- Adopt `go-library-tools` v1.0.6 while retaining repository-owned policy,
  evidence, fixtures, and API baselines.

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

[Unreleased]: https://github.com/faustbrian/go-state-machine/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/faustbrian/go-state-machine/releases/tag/v1.0.0
