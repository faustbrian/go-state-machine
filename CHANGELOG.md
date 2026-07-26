# Changelog

All notable changes to this project are documented in this file. The format is
based on Keep a Changelog, and the project follows Semantic Versioning.

## [Unreleased]

### Fixed

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
