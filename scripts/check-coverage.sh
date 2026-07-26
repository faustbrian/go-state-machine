#!/usr/bin/env bash
set -euo pipefail

go test -tags=integration \
  -coverpkg=.,./diagram,./memory,./outbox,./postgres,./runner \
  -coverprofile=coverage.out ./...
coverage="$(go tool cover -func=coverage.out | awk '/^total:/ {print $3}')"
if [[ "$coverage" != "100.0%" ]]; then
  echo "production statement coverage is $coverage, want 100.0%" >&2
  exit 1
fi
