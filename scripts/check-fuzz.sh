#!/usr/bin/env bash
set -euo pipefail

duration="${1:-2s}"
targets=(
  FuzzSerializedEvent
  FuzzSerializedDefinition
  FuzzSerializedContext
  FuzzSerializedHistory
  FuzzSerializedSnapshot
  FuzzSerializedGraphImport
)
for target in "${targets[@]}"; do
  go test . -run '^$' -fuzz="^${target}$" -fuzztime="$duration" \
    -parallel=4 -timeout=2m
done
