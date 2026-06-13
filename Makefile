BINARY ?= smith
GO ?= go
# GO_VERSION pins the toolchain used to build repo tooling (e.g. golangci-lint)
# so it matches the module's go directive (go.mod) and CI (.github/workflows/ci.yml).
# golangci-lint's own go.mod targets an older toolchain; without this it builds
# with that older Go and then refuses to lint a module on a newer Go.
GO_VERSION ?= 1.26.3
GOFLAGS ?=
LDFLAGS ?= -s -w
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
GOLANGCI_LINT_VERSION ?= v2.12.2
GOLANGCI_LINT_BIN ?= .cache/tools/golangci-lint/$(GOLANGCI_LINT_VERSION)/golangci-lint

.PHONY: build test vet lint lint-install fmt verify clean schema-guard schema-baseline

build:
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) -trimpath -ldflags '$(LDFLAGS) -X github.com/tonitienda/agent-smith/internal/version.Version=$(VERSION) -X github.com/tonitienda/agent-smith/internal/version.Commit=$(COMMIT)' -o $(BINARY) ./cmd/smith

test:
	$(GO) test $(GOFLAGS) ./...

vet:
	$(GO) vet $(GOFLAGS) ./...

lint: $(GOLANGCI_LINT_BIN)
	$(GOLANGCI_LINT_BIN) run

lint-install: $(GOLANGCI_LINT_BIN)

$(GOLANGCI_LINT_BIN):
	@mkdir -p $$(dirname $@)
	GOTOOLCHAIN=go$(GO_VERSION) GOBIN=$$(pwd)/$$(dirname $@) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

verify: fmt test vet lint

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
