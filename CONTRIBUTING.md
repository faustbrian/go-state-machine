# Contributing

## Before Editing

1. Read [`AGENTS.md`](AGENTS.md) and the affected module's goals and docs.
2. Run `make inventory` and the narrow baseline gate for the module.
3. Identify owned dependencies and reverse dependants in `modules.json`.
4. Preserve unrelated work and generated/corpus provenance.

## Changes

Keep commits focused and conventional. Update every affected changelog with
the behavior and migration impact. Public API changes require compatibility
evidence and documentation. Specification behavior requires a decision record,
fixture coverage, and interoperability evidence.

New direct dependencies and dependency updates must follow the
[dependency governance policy](AGENTS.md#dependencies-and-supply-chain). Package-local
update bots are forbidden; the root policy owns every module and action update.

Specification-backed changes must follow the
[specification governance contract](AGENTS.md#design), update
the affected stable decision entries, and complete the Specification Decisions
section of the pull request template. An unresolved interpretation or stale
source pin is release-blocking; peer behavior cannot silently select policy.

Required mutation gates must finish with zero surviving viable mutants.

Do not add package-local workflows, permanent replacements, machine-specific
paths, bypass flags, broad mutation exclusions, or aggregate quality metrics
that hide a failing package.

## Verification

Classify changes under [proportional assurance](AGENTS.md#proportional-assurance).
The root module collects complete native coverage in explicit evidence mode.
Failed tests, missing packages and invalid or absent profiles remain errors;
behavioral risk review replaces universal exact-percentage acceptance. Final
cancellation and immutable-preflight guards remain intact. Mutation, security
and strict aggregate checks remain required.

CI builds matched immutable workflow and tooling source
`55c50f11cc9a33a5306d71cb3dac71a9c92ed13c` with `source_bootstrap: true`,
using official public dependency and checksum authorities. This development
route does not qualify a published SDK binary. The declared v1.8.5 checksum
retains published tool metadata; that binary does not support the optional
coverage policy. For local development, build `cmd/golib` from the pinned
pristine Tools source with task-owned disposable Go caches and select it
through Make's `GOLIB` variable. Public v2 publication, clean consumers and
owned adoption remain separate from source CI.

Run during development:

```bash
make inventory
make check
```

Before submitting a repository-wide change:

```bash
make ci
```

The full scheduled and release gate is `make ci`. Report every unavailable or
failing command; do not describe partial results as release-ready.

## Adding A Module

Follow [repository structure policy](AGENTS.md#repository-structure). New modules
require an explicit purpose, ownership boundary, dependency review, package
catalog entry, full quality gates, documentation, changelog, license, security
policy, compatibility plan, and release dry-run.
