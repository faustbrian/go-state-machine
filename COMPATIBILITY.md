# Compatibility Policy

Root releases follow semantic versioning and use `v<version>` tags. Released
v1 remains available at `github.com/faustbrian/go-state-machine`. The current
source tree prepares the planned `github.com/faustbrian/go-state-machine/v2`
module because persistence bounds and safe default error strings change
previously accepted and observable behavior.

Version 2 is preview, unpublished, and non-releasable. Publication is blocked
until all release gates pass, the v2 API baseline is reviewed, owned consumers
are migrated and verified against a published v2 candidate without local
`replace` directives, and explicit release authority is provided. Until then,
applications must install and import released v1.

`api/baseline.txt` is the byte-preserved released-v1 API record. `api/v2.txt`
is the active compatibility baseline for the planned v2 source; verification
must never regenerate the v1 record from v2 source.

Before `v1`, minor releases MAY contain reviewed breaking changes, but every
break MUST be documented with migration guidance. Patch releases MUST remain
backward compatible. At and after `v1`, incompatible exported API or documented
behavior changes require a new major version.

Compatibility includes exported Go APIs, error classification, serialization,
protocol behavior, persistence schemas, environment variables, command output,
resource ownership, ordering, retry/idempotency semantics, and documented
defaults. A compile-compatible change can still be behaviorally breaking.

Migration to v2 requires changing imports to the `/v2` path, reviewing any
reliance on detailed `Error()` strings or PostgreSQL `last_error` contents, and
selecting persistence limits with `postgres.NewWithLimits` when machine limits
differ from the defaults. Rollback remains a dependency change back to a
released v1 version; shared persisted rows must satisfy both versions' schema
and value expectations during any overlap.

Specification-backed modules MUST NOT diverge from their declared standards.
Ambiguities require documented decisions and stable tests. Deprecated APIs
follow [`DEPRECATION.md`](DEPRECATION.md).
