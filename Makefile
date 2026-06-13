BINARY ?= smith
GO ?= go
GOFLAGS ?=
LDFLAGS ?= -s -w
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
GOLANGCI_LINT_VERSION ?= v2.12

.PHONY: build test lint fmt clean schema-guard schema-baseline

build:
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) -trimpath -ldflags '$(LDFLAGS) -X github.com/tonitienda/agent-smith/internal/version.Version=$(VERSION) -X github.com/tonitienda/agent-smith/internal/version.Commit=$(COMMIT)' -o $(BINARY) ./cmd/smith

test:
	$(GO) test $(GOFLAGS) ./...

lint:
	$(GO) vet $(GOFLAGS) ./...
	@if command -v golangci-lint >/dev/null 2>&1; then \
		tmp=$$(mktemp); \
		if golangci-lint run >$$tmp 2>&1; then \
			rm -f $$tmp; \
		elif grep -q "lower than the targeted Go version" $$tmp; then \
			cat $$tmp; \
			echo "golangci-lint binary is too old for this Go toolchain; install $(GOLANGCI_LINT_VERSION) or rely on CI for the full lint suite"; \
			rm -f $$tmp; \
		else \
			cat $$tmp; \
			rm -f $$tmp; \
			exit 1; \
		fi; \
	else \
		echo "golangci-lint not installed; install $(GOLANGCI_LINT_VERSION) or rely on CI for the full lint suite"; \
	fi

fmt:
	$(GO) fmt ./...

# schema-guard checks that the content-block schema has not changed
# non-additively (PRD D2). Runs in CI via `make test` as a unit test; this
# target is the same check on demand.
schema-guard:
	$(GO) run ./cmd/schema-guard

# schema-baseline regenerates the committed schema baseline and generated golden
# corpus after an intentional *additive* change. It refuses to record a breaking
# change. See docs/schema/EVOLUTION.md.
schema-baseline:
	$(GO) run ./cmd/schema-guard -update

clean:
	rm -f $(BINARY) ticket-sync
