.PHONY: build generate test test-unit test-integration test-e2e test-e2e-local

VERSION ?= $(shell ./scripts/resolve-version.sh)
LDFLAGS ?= -s -w -X main.Version=$(VERSION)

build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -trimpath -o bin/omnihive ./cmd/server

generate:
	go generate ./ent
	go generate ./cmd/server

# Full-repository lint: every finding in the tree fails this gate, not only the ones
# added since some pinned revision.
#
# `unused` is decided per build-tag set. The unit/integration/e2e tags are mutually
# exclusive here, so no single run sees every file; those tag-scoped runs are only a
# cross-check for `unused` and are deliberately NOT part of this gate.
test: test-unit
	golangci-lint run ./...

test-unit:
	go test -tags=unit ./...

test-integration:
	go test -tags=integration ./...

test-e2e: test-e2e-local

test-e2e-local:
	go test -tags=e2e -v -timeout=300s ./internal/integration/...
