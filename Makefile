# BootLore — developer task runner.
#
# Test taxonomy (see docs/testing.md):
#   unit         fast, isolated, no build tags            -> go test ./...
#   integration  cross-package, journal fixtures on disk  -> -tags=integration
#   system       end-to-end, builds & runs the binary     -> -tags=system

GO      ?= go
BIN     := bin/bootlore
PKG     := ./...

# Use `make V=1 ...` to see the underlying commands.
ifeq ($(V),1)
Q :=
else
Q := @
endif

.DEFAULT_GOAL := help

.PHONY: help build run fmt fmt-check vet lint tidy \
        test test-unit test-integration test-system test-all cover clean ci

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

build: ## Build the bootlore binary into ./bin
	$(Q)$(GO) build -o $(BIN) ./cmd/bootlore

run: build ## Build then run (pass args with ARGS="collect --input f.json")
	$(Q)./$(BIN) $(ARGS)

fmt: ## Format all Go code
	$(Q)gofmt -s -w .

fmt-check: ## Fail if any file is not gofmt-clean
	$(Q)out=$$(gofmt -s -l .); if [ -n "$$out" ]; then \
		echo "These files are not formatted (run 'make fmt'):"; echo "$$out"; exit 1; fi

vet: ## Run go vet across all build tags
	$(Q)$(GO) vet $(PKG)
	$(Q)$(GO) vet -tags=integration,system ./test/...

lint: fmt-check vet ## Run format check + go vet

tidy: ## Tidy and verify go.mod / go.sum
	$(Q)$(GO) mod tidy

test: test-unit ## Alias for unit tests (fast feedback)

test-unit: ## Run unit tests (race-enabled)
	$(Q)$(GO) test -race $(PKG)

test-integration: ## Run integration tests
	$(Q)$(GO) test -race -tags=integration ./test/integration/...

test-system: ## Run end-to-end system tests
	$(Q)$(GO) test -tags=system ./test/system/...

test-all: ## Run unit + integration + system tests in one pass
	$(Q)$(GO) test -race -tags=integration,system $(PKG)

cover: ## Unit+integration coverage profile (coverage.out) + total summary
	$(Q)$(GO) test -race -tags=integration -covermode=atomic -coverpkg=$(PKG) -coverprofile=coverage.out $(PKG)
	$(Q)$(GO) tool cover -func=coverage.out | tail -n 1

clean: ## Remove build and coverage artifacts
	$(Q)rm -rf bin coverage.out

ci: lint test-all ## Everything the CI pipeline runs
