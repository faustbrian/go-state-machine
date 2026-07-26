#!/usr/bin/env bash
set -euo pipefail

duration="${1:-100ms}"
go test ./... -run '^$' -bench . -benchmem -benchtime="$duration" \
  | tee benchmark.txt
STATE_MACHINE_POSTGRES_VERSION="${STATE_MACHINE_POSTGRES_VERSION:-18}" \
  go test -tags=integration ./postgres -run '^$' \
  -bench '^BenchmarkPostgresDurableWrite$' -benchmem -benchtime="$duration" \
  | tee -a benchmark.txt
required=(
  BenchmarkCompilation BenchmarkHotTransition BenchmarkGuardSets
  BenchmarkReplay BenchmarkHistoryGrowth BenchmarkContendedPersistence
  BenchmarkPostgresDurableWrite
)
for benchmark in "${required[@]}"; do
  rg -q "^${benchmark}" benchmark.txt || {
    echo "missing benchmark result: $benchmark" >&2
    exit 1
  }
done

assert_budget() {
  local benchmark="$1"
  local maximum="$2"
  local value
  value="$(awk -v prefix="$benchmark" '$1 ~ ("^" prefix "(-|/)") { print $3; exit }' benchmark.txt)"
  [[ -n "$value" ]] || {
    echo "missing benchmark value: $benchmark" >&2
    exit 1
  }
  awk -v value="$value" -v maximum="$maximum" 'BEGIN { exit !(value <= maximum) }' || {
    echo "benchmark regression: $benchmark is $value ns/op, budget $maximum" >&2
    exit 1
  }
}

assert_budget BenchmarkCompilation/states_10 1000000
assert_budget BenchmarkCompilation/states_1000 100000000
assert_budget BenchmarkHotTransition 100000
assert_budget BenchmarkGuardSets 100000
assert_budget BenchmarkReplay 10000000
assert_budget BenchmarkHistoryGrowth 100000
assert_budget BenchmarkContendedPersistence 1000000
assert_budget BenchmarkPostgresDurableWrite 1000000000
