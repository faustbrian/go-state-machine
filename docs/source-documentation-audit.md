# Source documentation audit

## Scope and reproducible inventory

Documentation-only audit of `0aa8bb704d411d53882e635e3c21eeb35e1f30da`.
Reproduce files with `git ls-files '*.go'`, modules with
`git ls-files '*go.mod'`, and assurance entry points with
`rg '^func (Test|Fuzz|Benchmark|Example)' --glob '*_test.go'`.
All 63 tracked Go files were reviewed: 17 implementation/helper files and
46 assurance files. One module, `github.com/faustbrian/go-state-machine/v2`,
owns root, `diagram`, `memory`, `outbox`, `postgres`, `runner`, and
`statemachinetest`. No nested modules or generated sources/generation directives
are present. Two PostgreSQL integration files require the `integration` tag;
the ordinary documentation target does not enable it or provision a database.
Existing package comments are canonical in their implementation files. The native
AST inventory covers 373 public declarations/fields/methods and 13 exported
methods on private receivers; all 63 source files parse with comments.

## Initial missing documentation versus investigated contracts

The initial source checker reports 217 missing exported contracts across
14 files, primarily public fields, constants, interface methods and typed
error methods. The substantive investigation covers all exported declarations,
private coordination, callbacks, limits, serialization and persistence:

- Compilation copies collection structure and payload occurrences only.
  Comparable state/event values, function closures and collaborators remain
  shallow or borrowed. Public Graph fields are mutable inspection copies, not
  immutable values. Concurrent safety and determinism depend on callback seams;
  typed identifiers must remain comparable and stable during use.
- Exact transitions win before wildcard selection; rejection never falls
  through. Terminal states reject live transitions. Guards run synchronously
  in ordinary-then-checked order with a fresh caller-provided context clone.
  Context checks surround entry/guard boundaries, not a final successful guard;
  blocking callbacks cannot be preempted. Only guard/cloner panics are contained.
- Compile collection and aggregate budgets are charged before proportional
  copying, counting shared buffers repeatedly. Bounds are not total heap or
  callback-cost guarantees. Only the two aggregate compile zeros select
  defaults; runner/evolution all-zero configurations have different rules.
- Replay re-executes guards and returns no partial result on failure. History
  validation does not run guards/effects or authenticate stored data. The free
  validator checks continuity; the machine method additionally checks version,
  selected transition ID and destination. It does not validate stored effects,
  guard eligibility, or terminal/unknown source-state eligibility for wildcard
  records. A successful topology check is not proof of a valid live execution.
- Migration copies carried payloads but leaves typed referents shallow. It
  keeps transition IDs/effects/sequence/timestamps, does not persist output or
  infer semantic compatibility, and may re-invoke hooks on retry. Hook errors
  preserve causes; hook panics propagate. Cancellation after hooks yields no
  partial output, but cannot undo hook side effects.
- Memory writes and reads have one mutex owner; returned payloads are copied.
  Cancellation is checked before locking, not while waiting or at commit.
  History is unbounded retained process memory despite bounded pages; memory
  provides no crash durability/outbox. uint64 sequence arithmetic is not a
  saturating or overflow-rejection scheme; PostgreSQL uses signed bigint.
- Stores trust supplied results rather than re-running machine validation.
  PostgreSQL additionally checks result shape and finite encoded sizes. Codec
  work and driver buffering occur before some output-length checks; these
  limits cannot bound arbitrary callback allocations or corrupt driver rows.
- PostgreSQL atomically commits state/history/outbox; a commit error can leave
  the caller uncertain about the durable outcome. Deferred rollback uses its
  own bounded context but is best-effort, not proof that any error undid a
  transaction. Applications reconcile uncertain outcomes before retrying.
- Outbox claiming expires leases for **reclaim eligibility**. Completion SQL
  checks the still-current token and unfinished status, not leased-until time:
  expiry alone does not reject an acknowledgement before reclamation. Tokens
  must be unique across generations; NewID correctness is caller-owned.
  SKIP LOCKED and store sort order do not establish global/effect-index order.
- Relay success is recorded after external publication. A crash or recording
  failure can duplicate delivery; remaining claims retain leases until recovery.
  RunOnce returns partial durable counters and does not renew leases or schedule
  itself. Only publisher panics are contained; policy/store callbacks are trusted.
- Runner performs each reached effect once per call, not durable deduplication.
  Failure/cancellation returns completed attempt records; recorder failure can
  follow a successful external effect. Handlers get separate payload copies,
  but recorder and returned record can share the record payload. Only handler
  panics are contained; clocks/classifiers/recorders can block or panic.
- Checked diagrams bound counts/labels and conservative expansion estimates,
  not labeler allocation/CPU. They validate imported references but are not
  equivalent to Compile (no guards/reachability/ambiguity proof). Unchecked
  renderers trust their input; labeler errors/panics are application-owned.
- Typed error Error strings omit selected sensitive values; exported fields,
  diagnostic messages, unwrapped causes and arbitrary formatting/serialization
  are not universally redacted. Existing synthetic fixtures remain unchanged.

These constraints are grounded in implementation plus compile-admission,
aggregate/allocation, model, guard-clone, evolution cancellation, runner
attempt/record, memory contention/snapshot, driver-failure, signed-value,
outbox replay, and diagram boundary tests. Missing comments are repaired;
existing limitations are documented, not silently fixed or strengthened.

## Assurance sources and native documentation

The six JSON fuzz targets cover event round-trips, bounded compilation,
reference-bearing context isolation, history continuity, snapshot boundaries
and checked imported diagrams. JSON decoding itself is a separate fuzz input
boundary, not a package-defined bounded parser. Existing benchmarks cover
10/1000-state compilation, hot lookup, 50 guards, 100-input replay, growing
memory history, contended CAS retries and tagged PostgreSQL writes; no new
performance result is claimed. Three runnable examples describe root use,
replay and memory Store integration; their executable text remains unchanged.

`.golib.yaml` uses `apidiff` and `api/v2.txt`, not a prose snapshot.
The native docs target runs `scripts/check-docs.sh`: maintained paths/local
links, existing examples, and vet. Render all seven public packages separately.
API baselines, dependency graph, synthetic fixtures and dated release/security
records are immutable inputs here. No repository specification register or
external conformance edition is inferred for this application-policy package.
No tool, workflow, enforcement, release or coordination edits belong here.

## Questions and remaining boundaries

The source cannot establish whether applications need strict post-expiry lease
rejection, validated wildcard history eligibility, overflow-refusing memory
sequences, stronger callback isolation, or durable effect deduplication. Those
are explicit potential future behavioral requirements, not invented guarantees
or repairs in this documentation batch. Race/fuzz/performance, live PostgreSQL,
crash recovery, service provisioning, release and public-consumer campaigns are
not newly exercised by this prose pass.

## Complete tracked file review

| Go file | Contract or assurance purpose |
| --- | --- |
| `benchmark_test.go` | 4 benchmarks; 10/1000-state compilation, hot lookup, 50 guards, 100-input replay |
| `compile_admission_allocation_test.go` | 2 assurance entry points; compile/evolution/replay/history boundary and model partitions |
| `compile_admission_test.go` | 3 assurance entry points; compile/evolution/replay/history boundary and model partitions |
| `compile_aggregate_admission_test.go` | 4 assurance entry points; compile/evolution/replay/history boundary and model partitions |
| `compile_aggregate_boundary_test.go` | 1 assurance entry points; compile/evolution/replay/history boundary and model partitions |
| `compile_test.go` | 2 assurance entry points; compile/evolution/replay/history boundary and model partitions |
| `coverage_internal_test.go` | 5 assurance entry points; compile/evolution/replay/history boundary and model partitions |
| `diagnostics.go` | structured defects and bounded Error string versus diagnostic data |
| `diagram/coverage_internal_test.go` | 5 assurance entry points; provider contract and internal failure/ownership partitions |
| `diagram/diagram.go` | trusted versus checked graphs, label callbacks, conservative output accounting |
| `diagram/diagram_test.go` | 2 assurance entry points; provider contract and internal failure/ownership partitions |
| `evolution.go` | bounded preflight; borrowed hooks; cancellation and no partial output |
| `evolution_admission_boundaries_test.go` | 4 assurance entry points; compile/evolution/replay/history boundary and model partitions |
| `evolution_cancellation_boundary_test.go` | 2 assurance entry points; compile/evolution/replay/history boundary and model partitions |
| `evolution_internal_test.go` | 3 assurance entry points; compile/evolution/replay/history boundary and model partitions |
| `evolution_limits.go` | explicit complete budgets and all-zero defaults |
| `evolution_limits_test.go` | 13 assurance entry points; compile/evolution/replay/history boundary and model partitions |
| `evolution_test.go` | 1 assurance entry points; compile/evolution/replay/history boundary and model partitions |
| `examples_test.go` | 3 runnable examples; root transition, replay and memory-store integration |
| `fuzz_test.go` | 6 JSON fuzz targets; round-trip, compilation, context, history, snapshot and diagrams |
| `graph_test.go` | 1 assurance entry points; compile/evolution/replay/history boundary and model partitions |
| `guard_test.go` | 7 assurance entry points; compile/evolution/replay/history boundary and model partitions |
| `history.go` | sequence/state continuity and narrower machine topology checks |
| `history_test.go` | 2 assurance entry points; compile/evolution/replay/history boundary and model partitions |
| `limits.go` | per-collection and aggregate copied-occurrence budgets |
| `limits_test.go` | 1 assurance entry points; compile/evolution/replay/history boundary and model partitions |
| `machine.go` | compile admission; exact-before-wildcard selection; copied plans and borrowed callbacks |
| `machine_test.go` | 3 assurance entry points; compile/evolution/replay/history boundary and model partitions |
| `memory/benchmark_test.go` | 2 benchmarks; growing retained history and contended CAS retry work |
| `memory/coverage_internal_test.go` | 3 assurance entry points; provider contract and internal failure/ownership partitions |
| `memory/store.go` | mutex-owned CAS and payload copying; ephemeral retention and snapshots |
| `memory/store_test.go` | 4 assurance entry points; provider contract and internal failure/ownership partitions |
| `mutation_boundaries_internal_test.go` | 7 assurance entry points; compile/evolution/replay/history boundary and model partitions |
| `outbox/coverage_internal_test.go` | 5 assurance entry points; provider contract and internal failure/ownership partitions |
| `outbox/relay.go` | serial store-ordered publication, partial counters and duplicate window |
| `outbox/relay_test.go` | 1 assurance entry points; provider contract and internal failure/ownership partitions |
| `outbox/security_test.go` | 1 assurance entry points; provider contract and internal failure/ownership partitions |
| `postgres/admission_boundaries_test.go` | 7 assurance entry points; PostgreSQL driver/tagged integration partitions |
| `postgres/coverage_internal_test.go` | 3 assurance entry points; PostgreSQL driver/tagged integration partitions |
| `postgres/database.go` | pgx seams, safe error strings and independent bounded rollback cleanup |
| `postgres/driver_contract_integration_test.go` | 2 assurance entry points; PostgreSQL driver/tagged integration partitions |
| `postgres/failure_internal_test.go` | 12 assurance entry points; PostgreSQL driver/tagged integration partitions |
| `postgres/outbox.go` | claim admission, token replacement fencing and expiry/reclaim distinction |
| `postgres/remaining_admission_test.go` | 5 assurance entry points; PostgreSQL driver/tagged integration partitions |
| `postgres/security_test.go` | 17 assurance entry points; PostgreSQL driver/tagged integration partitions |
| `postgres/signed_values_internal_test.go` | 4 assurance entry points; PostgreSQL driver/tagged integration partitions |
| `postgres/store.go` | trusted codecs, conditional write, atomic history/outbox and encoded budgets |
| `postgres/store_integration_test.go` | 2 assurance entry points; PostgreSQL driver/tagged integration partitions |
| `postgres/store_test.go` | 2 assurance entry points; PostgreSQL driver/tagged integration partitions |
| `property_model_test.go` | 4 assurance entry points; compile/evolution/replay/history boundary and model partitions |
| `replay.go` | guard re-execution, index failures, no partial replay output |
| `replay_test.go` | 2 assurance entry points; compile/evolution/replay/history boundary and model partitions |
| `runner/cancellation_result_test.go` | 1 assurance entry points; runner input, cancellation, outcome and aliasing partitions |
| `runner/coverage_internal_test.go` | 3 assurance entry points; runner input, cancellation, outcome and aliasing partitions |
| `runner/handler_admission_test.go` | 1 assurance entry points; runner input, cancellation, outcome and aliasing partitions |
| `runner/input_limits_test.go` | 13 assurance entry points; runner input, cancellation, outcome and aliasing partitions |
| `runner/runner.go` | serial attempts, borrowed seams, partial records and recovery limits |
| `runner/runner_test.go` | 6 assurance entry points; runner input, cancellation, outcome and aliasing partitions |
| `runner/security_test.go` | 1 assurance entry points; runner input, cancellation, outcome and aliasing partitions |
| `security_error_test.go` | 1 assurance entry points; compile/evolution/replay/history boundary and model partitions |
| `statemachinetest/runner.go` | wrapper factory and ordered/canceled/panicked runner contract |
| `statemachinetest/store.go` | portable backend fixture obligations and limited conformance assertions |
| `store.go` | portable CAS/history/snapshot capabilities and provider distinctions |
