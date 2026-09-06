# Performance

State-machine performance has two distinct boundaries: compiling a definition
and operating a compiled machine. Compile once during application setup and
reuse the immutable result across requests or workers.

## Cost model

- Compilation validates the complete state and transition graph, copies
  retained definition data, and checks reachability. Its cost grows with the
  definition rather than with later transition volume.
- A transition selects an exact or wildcard edge and then evaluates only that
  edge's guards. Guard count, caller-supplied guard work, cloned context, and
  copied effect payloads dominate per-transition cost.
- Replay repeats transition selection for every input, so cost grows with the
  replay length. Use validated snapshots to bound durable recovery work.
- The memory store copies results and serializes writes. PostgreSQL latency is
  dominated by the application pool, database round trips, transaction
  contention, history growth, and outbox inserts.
- Outbox throughput depends on claim size, publisher latency, retry policy, and
  duplicate-safe acknowledgement handling. The relay processes a claimed batch
  serially; scale with multiple workers only after measuring contention.

## Reproducible benchmarks

`benchmark_test.go`, `memory/benchmark_test.go`, and the
`BenchmarkPostgresDurableWrite` benchmark in `postgres/store_integration_test.go`
cover compilation, hot transitions, guard sets, replay, history growth,
contended persistence, and durable PostgreSQL writes.
Run the repository-owned benchmark gate from a clean checkout:

```sh
make -f verification/package.mk benchmark
```

The PostgreSQL benchmark requires Docker. The enforced latency ceilings live in
`scripts/check-benchmarks.sh`; they are regression limits for the repository's
CI environment, not production service-level objectives.

Measure representative definitions, guard work, effect payloads, history
lengths, database topology, and publisher behavior before setting an
application budget. Compare results using the same Go version, CPU, worker
count, database version, and benchmark duration.
