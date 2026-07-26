#!/usr/bin/env bash
set -euo pipefail

temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
cp -R . "$temporary/repository"
mkdir -p "$temporary/baseline/memory" "$temporary/baseline/runner"
for path in machine.go replay.go history.go memory/store.go runner/runner.go; do
  cp "$path" "$temporary/baseline/$path"
done

restore_baseline() {
  for path in machine.go replay.go history.go memory/store.go runner/runner.go; do
    cp "$temporary/baseline/$path" "$temporary/repository/$path"
  done
}

expect_killed() {
  local name="$1"
  local package="$2"
  local test_name="$3"
  if (cd "$temporary/repository" && go test "$package" -run "^${test_name}$" >/dev/null 2>&1); then
    echo "mutation survived: $name" >&2
    exit 1
  fi
  restore_baseline
}

perl -0pi -e 's/transition, ok := machine\.exact\[current\]\[event\]/transition, ok := machine.wildcard[event]/' \
  "$temporary/repository/machine.go"
expect_killed transition-selection . TestTransitionPrefersExactSourceAndPlansEffectsInOrder

perl -0pi -e 's/if rejection != nil \{/if false \&\& rejection != nil {/' \
  "$temporary/repository/machine.go"
expect_killed guard-rejection . TestTransitionReturnsStructuredGuardRejection

perl -0pi -e 's/effects = append\(effects, cloneEffects\(machine\.states\[current\]\.Exit\)\.\.\.\)/effects = append(effects, cloneEffects(machine.states[transition.To].Entry)...)/' \
  "$temporary/repository/machine.go"
expect_killed effect-order . TestTransitionPrefersExactSourceAndPlansEffectsInOrder

perl -0pi -e 's/if !machine\.states\[source\]\.Terminal \{/if true {/' \
  "$temporary/repository/machine.go"
expect_killed terminal-wildcard-reachability . TestCompileDoesNotTreatTerminalWildcardAsReachable

perl -0pi -e 's/if _, exists := machine\.states\[initial\]; !exists \{/if false {/' \
  "$temporary/repository/replay.go"
expect_killed replay-snapshot-validation . TestReplayFromValidatesEmptyReplayBoundary

perl -0pi -e 's/transition\.ID != result\.TransitionID/false/' \
  "$temporary/repository/history.go"
expect_killed history-definition-compatibility . TestMachineValidateHistoryRejectsIncompatibleDefinition

perl -0pi -e 's/if instance\.LockVersion != expected \{/if false {/' \
  "$temporary/repository/memory/store.go"
expect_killed optimistic-locking ./memory TestStorePreventsLostUpdatesUnderContention

perl -0pi -e 's/active == runner/false \&\& active == runner/' \
  "$temporary/repository/runner/runner.go"
expect_killed reentrant-execution ./runner TestExecuteRejectsReentrantExecution

echo "focused mutations killed: core, replay, history, locking, runner"
