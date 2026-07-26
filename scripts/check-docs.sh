#!/usr/bin/env bash
set -euo pipefail

required=(
  README.md CHANGELOG.md LICENSE SECURITY.md SUPPORT.md CONTRIBUTING.md
  CODE_OF_CONDUCT.md docs/README.md docs/quickstart.md docs/api.md
  docs/architecture.md docs/guards-effects.md docs/persistence.md
  docs/evolution.md docs/outbox.md docs/replay-debugging.md
  docs/concurrency.md docs/adoption.md docs/boundaries.md docs/faq.md
)
for path in "${required[@]}"; do
  test -s "$path"
done

while IFS=: read -r source match; do
  link="$(sed -E 's/.*\(([^)]+)\)/\1/' <<<"$match")"
  link="${link%%#*}"
  [[ -z "$link" || "$link" == http://* || "$link" == https://* ]] && continue
  target="$(dirname "$source")/$link"
  test -e "$target" || {
    echo "broken local documentation link: $source -> $link" >&2
    exit 1
  }
done < <(rg -o --with-filename '\[[^]]+\]\([^)]+\)' README.md docs CONTRIBUTING.md CODE_OF_CONDUCT.md SECURITY.md SUPPORT.md)

go test ./... -run '^Example' -count=1
go vet ./...
