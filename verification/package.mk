SHELL := /usr/bin/env bash

BENCH_TIME ?= 100ms
FUZZ_TIME ?= 2s

.PHONY: benchmark docs fuzz

benchmark:
	./scripts/check-benchmarks.sh "$(BENCH_TIME)"

docs:
	./scripts/check-docs.sh

fuzz:
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"
