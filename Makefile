BINARY ?= smith
GO ?= go
GOFLAGS ?=
LDFLAGS ?= -s -w
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

.PHONY: build test lint fmt clean

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
			echo "golangci-lint binary is too old for this Go toolchain; CI runs the pinned full lint suite"; \
			rm -f $$tmp; \
		else \
			cat $$tmp; \
			rm -f $$tmp; \
			exit 1; \
		fi; \
	else \
		echo "golangci-lint not installed; CI runs the pinned full lint suite"; \
	fi

fmt:
	$(GO) fmt ./...

clean:
	rm -f $(BINARY) ticket-sync
