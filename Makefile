# BootLore — developer task runner.
#
# Test taxonomy (see docs/testing.md):
#   unit         fast, isolated, no build tags            -> go test ./...
#   integration  cross-package, journal fixtures on disk  -> -tags=integration
#   system       end-to-end, builds & runs the binary     -> -tags=system

GO      ?= go
BIN     := bin/bootlore
PKG     := ./...

# Install layout. PREFIX defaults to /usr so the binary lands in /usr/bin,
# exactly where the shipped systemd unit's ExecStart points; override any of
# these (and, if you move the binary, the unit's ExecStart) as needed.
# DESTDIR supports staged installs for packaging.
PREFIX  ?= /usr
DESTDIR ?=
BINDIR  ?= $(PREFIX)/bin
MANDIR  ?= $(PREFIX)/share/man/man1
UNITDIR ?= $(PREFIX)/lib/systemd/system

# Use `make V=1 ...` to see the underlying commands.
ifeq ($(V),1)
Q :=
else
Q := @
endif

.DEFAULT_GOAL := help

.PHONY: help build run fmt fmt-check vet lint tidy man \
        test test-unit test-integration test-system test-all cover clean ci \
        install uninstall

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

man: ## View the manual page (man/bootlore.1)
	$(Q)man -l man/bootlore.1

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

install: ## Install binary, man page and systemd unit (build first: make build && sudo make install)
	$(Q)test -x $(BIN) || { echo "Build first as your normal user:  make build"; echo "then install as root:            sudo make install"; exit 1; }
	$(Q)install -Dm755 $(BIN) $(DESTDIR)$(BINDIR)/bootlore
	$(Q)install -Dm644 man/bootlore.1 $(DESTDIR)$(MANDIR)/bootlore.1
	$(Q)install -Dm644 systemd/bootlore.service $(DESTDIR)$(UNITDIR)/bootlore.service
	$(Q)echo "Installed bootlore to $(DESTDIR)$(BINDIR)/bootlore"
	$(Q)echo "The optional service is installed but not enabled. See the README to"
	$(Q)echo "create the 'bootlore' account, then: systemctl enable bootlore.service"

uninstall: ## Remove the installed binary, man page and systemd unit (run as root)
	$(Q)rm -f $(DESTDIR)$(BINDIR)/bootlore
	$(Q)rm -f $(DESTDIR)$(MANDIR)/bootlore.1
	$(Q)rm -f $(DESTDIR)$(UNITDIR)/bootlore.service
	$(Q)echo "Removed bootlore. If you enabled the service, disable it first with"
	$(Q)echo "systemctl disable --now bootlore.service, and remove the account if unused."

ci: lint test-all ## Everything the CI pipeline runs
