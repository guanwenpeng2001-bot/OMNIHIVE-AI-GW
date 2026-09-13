.PHONY: build generate test test-unit test-integration test-e2e

VERSION ?= $(shell ./scripts/resolve-version.sh)
LDFLAGS ?= -s -w -X main.Version=$(VERSION)
# Keep existing lint debt visible separately while gating new findings.
LINT_BASE_REV ?= c51166151bbfda1e34b8d8b7ec77435a45648c24

build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -trimpath -o bin/server ./cmd/server

generate:
	go generate ./ent
	go generate ./cmd/server

test: test-unit
	golangci-lint run --new-from-rev=$(LINT_BASE_REV) ./...

test-unit:
	go test -tags=unit ./...

test-integration:
	go test -tags=integration ./...

test-e2e:
	./scripts/e2e-test.sh

test-e2e-local:
	go test -tags=e2e -v -timeout=300s ./internal/integration/...
