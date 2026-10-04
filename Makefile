# Vault dev-environment verification targets.
#
# Skip policy: vet / test / build SKIP only when the repo has ZERO .go files.
# As soon as any .go file exists they run for real and fail the target on any
# error. Every SKIP line prints its reason.

GO ?= go
# Prints the first .go file found (empty when the repo has zero .go files).
GO_PROBE := $(shell find . -name '*.go' -not -path './vendor/*' -print -quit 2>/dev/null)

.PHONY: fmt-check vet test build verify

fmt-check:
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then \
		echo "FAIL fmt-check: gofmt needed on:"; echo "$$out"; exit 1; \
	fi; \
	echo "OK   fmt-check (nothing to format)"

vet:
	@if [ -z "$(GO_PROBE)" ]; then \
		echo "SKIP vet (reason: repo has zero .go files)"; \
	else \
		$(GO) vet ./... && echo "OK   vet"; \
	fi

test:
	@if [ -z "$(GO_PROBE)" ]; then \
		echo "SKIP test (reason: repo has zero .go files)"; \
	else \
		$(GO) test ./... -count=1 && echo "OK   test"; \
	fi

build:
	@if [ -z "$(GO_PROBE)" ]; then \
		echo "SKIP build (reason: repo has zero .go files)"; \
	else \
		mkdir -p bin && $(GO) build -o bin/vault ./cmd/vault && echo "OK   build (bin/vault)"; \
	fi

# Runs fmt-check, vet, test, build in order; stops at the first failure.
.NOTPARALLEL: verify
verify: fmt-check vet test build
	@echo "verify: all checks passed"
