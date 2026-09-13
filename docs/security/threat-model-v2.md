# State Machine Threat Model v2

Status: active

Owner: go-state-machine maintainers

Review condition: any new persistence backend, executable effect path,
untrusted parser, implicit I/O, or change to replay, leasing, or limits

## Scope and assets

The library protects compiled transition definitions, durable instance state,
ordered history, snapshots, outbox leases, effect payloads, correlation data,
and database availability. State, event, metadata, rejection text, effect data,
identifiers, codec output, callback errors, and persisted rows can contain
attacker-controlled or sensitive values.

The pure machine performs no network, filesystem, process, or environment I/O.
PostgreSQL access, effect execution, publication, codecs, clocks, ID sources,
guards, migration hooks, recorders, and classifiers are explicit caller-owned
boundaries.

## Controls

| Threat | Control and evidence boundary |
| --- | --- |
| Oversized definitions, replay, metadata, effects, or payloads | `statemachine.Limits` bounds compilation and execution. `postgres.NewWithLimits` copies those machine limits and adds encoded identifier, state, event, and aggregate-result bounds before database work. PostgreSQL reads are checked before decode or return. |
| Forged persistence result | Stores require identity fields, optimistic lock version, and previous-state equality. PostgreSQL updates state, history, and outbox in one transaction. Callers still validate imported history against the compiled machine. |
| Replay, duplicate publication, and reordering | History sequences are unique and ascending. Conditional state updates reject stale writers. Outbox rows are unique per instance, sequence, and effect index; claims are ordered and leased with `SKIP LOCKED`. Publication is explicitly at least once, so consumers must deduplicate. |
| SQL injection | Values are query parameters. The only interpolated identifier is a schema accepted by a strict lowercase identifier allowlist. |
| Secret or payload disclosure | Public error strings expose stable classifications and indexes only; structured fields and wrapped causes remain available through deliberate type inspection. PostgreSQL stores a fixed redaction marker instead of callback error text. Panic values are discarded. |
| Cancellation and lifecycle failure | Every database call receives the operation context. Deferred rollback uses a separate five-second cleanup context so canceled work can release transaction resources without an unbounded background operation. Caller callbacks must cooperate with context cancellation. |
| Races and lost updates | Compiled machines are immutable. Memory writes are mutex-owned. PostgreSQL uses one conditional update and transaction per transition. Stale writers return `ErrStoreConflict` and must reload and recalculate. |
| Poisoned or corrupt durable data | Page and claim counts are bounded; decoded result and outbox fields are size-checked. History validation checks instance, sequence, state, definition, transition, event, and destination continuity. |

## Explicitly accepted risks

### AR-1: caller callbacks can ignore cancellation

- Severity: medium.
- Owner: adopting application.
- Rationale: Go cannot safely preempt a guard, codec, handler, publisher,
  recorder, classifier, clock, ID source, or migration callback.
- Mitigation: callbacks receive contexts where blocking is supported; adopters
  must bound their own I/O and keep pure callbacks finite. The library never
  starts a hidden goroutine to fake preemption.
- Review condition: an owned callback runtime or sandbox is introduced, or a
  callback is found to block shutdown in a supported integration.

### AR-2: privileged direct database writers can insert oversized rows

- Severity: medium.
- Owner: database operator and adopting application.
- Rationale: per-store limits can differ, so shared DDL cannot encode every
  caller's configured bound. A database administrator can also bypass normal
  application invariants by definition.
- Mitigation: grant write access only to the application role; all library
  writes and reads enforce copied limits; validate imported history before use;
  monitor table growth and reject unexpected writer roles.
- Review condition: multiple writers share the tables, database imports become
  supported, or schema-level fixed ceilings become part of the compatibility
  contract.

### AR-3: at-least-once delivery can duplicate external effects

- Severity: medium.
- Owner: outbox consumer owner.
- Rationale: a publish may succeed before acknowledgement or lease completion,
  and distributed exactly-once delivery is not claimed.
- Mitigation: deduplicate using the stable message ID or the tuple of instance,
  sequence, and effect index; make handlers idempotent; dead-letter permanent
  failures.
- Review condition: a consumer cannot implement idempotency or the library
  claims exactly-once delivery.

### AR-4: structured error fields remain sensitive

- Severity: medium.
- Owner: adopting application.
- Rationale: callers need typed diagnostics, transition identifiers, rejection
  details, and wrapped causes for programmatic handling, while `Error()` must be
  safe for default logging.
- Mitigation: log only the stable error string and explicitly allowlist any
  structured field; never serialize error structs or unwrap chains into public
  telemetry by default.
- Review condition: a standard safe diagnostic projection is added or a common
  logging integration automatically expands structured errors.

## Out-of-scope trust failures

A compromised process can read in-memory typed values and callback inputs. A
compromised database administrator can read durable plaintext state and effect
payloads. Applications needing confidentiality at either boundary must encrypt
values before passing them to this library and own key management separately.
