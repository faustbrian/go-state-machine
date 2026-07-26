GO ?= go
GOLANGCI_LINT_VERSION ?= v2.12.2
STATICCHECK_VERSION ?= v0.7.0
GOVULNCHECK_VERSION ?= v1.6.0
ACTIONLINT_VERSION ?= v1.7.12
GO_LICENSES_VERSION ?= v1.6.0
FUZZ_TIME ?= 2s
BENCH_TIME ?= 100ms
POSTGRES_VERSION ?= 18

.PHONY: benchmark check coverage dependency-review docs format format-check \
	fuzz integration license lint mutation race staticcheck test tidy-check vet vuln \
	workflows

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

tidy-check:
	GOWORK=off $(GO) mod tidy -diff

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

integration:
	STATE_MACHINE_POSTGRES_VERSION=$(POSTGRES_VERSION) \
		$(GO) test -race -tags=integration -timeout=10m ./postgres

coverage:
	STATE_MACHINE_POSTGRES_VERSION=$(POSTGRES_VERSION) \
		./scripts/check-coverage.sh

fuzz:
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"

mutation:
	./scripts/check-mutation.sh

benchmark:
	./scripts/check-benchmarks.sh "$(BENCH_TIME)"

docs:
	./scripts/check-docs.sh

vet:
	$(GO) vet ./...

staticcheck:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

lint:
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --timeout=5m

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

dependency-review:
	$(GO) mod verify
	$(GO) list -m all >/dev/null

license:
	$(GO) run github.com/google/go-licenses@$(GO_LICENSES_VERSION) check ./...

workflows:
	$(GO) run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION) .github/workflows/*.yml

check: tidy-check format-check vet staticcheck lint test race integration \
	coverage fuzz mutation benchmark docs vuln dependency-review license workflows
