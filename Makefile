# Vault dev-environment verification targets.
# All targets work on an empty or near-empty module: they skip gracefully
# when there are no Go sources / no cmd/vault yet.

GO ?= go

.PHONY: fmt-check vet test build verify

fmt-check:
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then \
		echo "FAIL fmt-check: gofmt needed on:"; echo "$$out"; exit 1; \
	fi; \
	echo "OK   fmt-check (nothing to format)"

vet:
	@if [ -z "$$(find . -name '*.go' -not -path './vendor/*' -print -quit)" ]; then \
		echo "SKIP vet (no Go files yet)"; \
	else \
		$(GO) vet ./... && echo "OK   vet"; \
	fi

test:
	@if [ -z "$$(find . -name '*.go' -not -path './vendor/*' -print -quit)" ]; then \
		echo "SKIP test (no Go files yet)"; \
	else \
		$(GO) test ./... -count=1; \
	fi

build:
	@if [ ! -d cmd/vault ]; then \
		echo "SKIP build (cmd/vault not present yet)"; \
	else \
		mkdir -p bin && $(GO) build -o bin/vault ./cmd/vault && echo "OK   build (bin/vault)"; \
	fi

# Runs fmt-check, vet, test, build in order; stops at the first failure.
.NOTPARALLEL: verify
verify: fmt-check vet test build
	@echo "verify: all checks passed"
