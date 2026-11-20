.PHONY: build generate test test-unit test-integration test-e2e test-e2e-local test-e2e-live

VERSION ?= $(shell ./scripts/resolve-version.sh)
LDFLAGS ?= -s -w -X main.Version=$(VERSION)

REPO_ROOT := $(abspath ..)
STRICT := $(REPO_ROOT)/scripts/go-test-strict.sh

# Strict Go test targets fail on any unexpected Test* skip and on a -run that
# matches nothing (see scripts/go-test-strict.sh). Database/Redis suites only
# run when their disposable services are supplied; CI exports the full matrix.
#
# Set TEST_DSN to a disposable PostgreSQL URL whose database is job_test, e.g.
#   make test-unit TEST_DSN='postgres://postgres:postgres@127.0.0.1:5432/job_test?sslmode=disable'
# and the derived stats_test/billing_audit_test/backup_data_test/
# be09_rehearsal_test/be09_journal_test databases plus every DSN environment
# variable are wired automatically. TEST_REDIS_ADDR selects the disposable
# Redis host:port (default 127.0.0.1:6379). Packages run with -p 1 so the
# shared job_test tables are not truncated by a concurrent package.
TEST_DSN ?=
TEST_REDIS_ADDR ?= 127.0.0.1:6379
TEST_STATS_DSN ?= $(subst /job_test?,/stats_test?,$(TEST_DSN))
TEST_BILLING_AUDIT_DSN ?= $(subst /job_test?,/billing_audit_test?,$(TEST_DSN))
TEST_BACKUP_DSN ?= $(subst /job_test?,/backup_data_test?,$(TEST_DSN))
TEST_BE09_REHEARSAL_DSN ?= $(subst /job_test?,/be09_rehearsal_test?,$(TEST_DSN))
TEST_BE09_JOURNAL_DSN ?= $(subst /job_test?,/be09_journal_test?,$(TEST_DSN))

ifneq ($(strip $(TEST_DSN)),)
UPSTREAM_JOB_TEST_DSN ?= $(TEST_DSN)
NEW_PLATFORMS_TEST_POSTGRES_DSN ?= $(TEST_DSN)
CATALOG_SCOPE_TEST_DSN ?= $(TEST_DSN)
CATALOG_EVENT_TEST_DSN ?= $(TEST_DSN)
WORKSPACE_TEST_DSN ?= $(TEST_DSN)
CREDENTIAL_DRAFT_TEST_DSN ?= $(TEST_DSN)
CREDENTIAL_SECRET_TEST_DSN ?= $(TEST_DSN)
ACCOUNT_EXTRA_TEST_DSN ?= $(TEST_DSN)
ACCOUNT_WORKFLOW_TEST_DSN ?= $(TEST_DSN)
HOSTED_IDENTITY_TEST_DSN ?= $(TEST_DSN)
USER_BILLING_TIMELINE_TEST_DSN ?= $(TEST_DSN)
API_KEY_CREDENTIAL_TEST_DSN ?= $(TEST_DSN)
OPENCODE_QUOTA_TEST_DSN ?= $(TEST_DSN)
CURSOR_TEST_POSTGRES_DSN ?= $(TEST_DSN)
CURSOR_MONITOR_TEST_POSTGRES_DSN ?= $(TEST_DSN)
COMPATIBLE_PROVIDER_TEST_POSTGRES_DSN ?= $(TEST_DSN)
PROMPT_AUDIT_TEST_POSTGRES_DSN ?= $(TEST_DSN)
QUOTES_POSTGRES_DSN ?= $(TEST_DSN)
BE09_TEST_ISOLATED_DSN ?= $(TEST_BE09_REHEARSAL_DSN)
BE09_TEST_JOURNAL_DSN ?= $(TEST_BE09_JOURNAL_DSN)
MIGRATION_INTEGRATION_TEST_DSN ?= $(TEST_DSN)
USAGE_STATS_TEST_DSN ?= $(TEST_STATS_DSN)
UPSTREAM_BILLING_AUDIT_DSN ?= $(TEST_BILLING_AUDIT_DSN)
BACKUP_DATA_TEST_DSN ?= $(TEST_BACKUP_DSN)
export UPSTREAM_JOB_TEST_DSN NEW_PLATFORMS_TEST_POSTGRES_DSN CATALOG_SCOPE_TEST_DSN
export CATALOG_EVENT_TEST_DSN WORKSPACE_TEST_DSN CREDENTIAL_DRAFT_TEST_DSN
export CREDENTIAL_SECRET_TEST_DSN ACCOUNT_EXTRA_TEST_DSN ACCOUNT_WORKFLOW_TEST_DSN
export HOSTED_IDENTITY_TEST_DSN USER_BILLING_TIMELINE_TEST_DSN
export API_KEY_CREDENTIAL_TEST_DSN OPENCODE_QUOTA_TEST_DSN
export CURSOR_TEST_POSTGRES_DSN CURSOR_MONITOR_TEST_POSTGRES_DSN
export COMPATIBLE_PROVIDER_TEST_POSTGRES_DSN PROMPT_AUDIT_TEST_POSTGRES_DSN
export QUOTES_POSTGRES_DSN BE09_TEST_ISOLATED_DSN BE09_TEST_JOURNAL_DSN
export MIGRATION_INTEGRATION_TEST_DSN
export USAGE_STATS_TEST_DSN UPSTREAM_BILLING_AUDIT_DSN BACKUP_DATA_TEST_DSN
endif

AFFINITY_REDIS_TEST_ADDR ?= $(TEST_REDIS_ADDR)
ACCOUNTING_TEST_REDIS_ADDR ?= $(TEST_REDIS_ADDR)
QUOTA_TEST_REDIS_ADDR ?= $(TEST_REDIS_ADDR)
SECURITY_REDIS_TEST_ADDR ?= $(TEST_REDIS_ADDR)
PROMPT_AUDIT_TEST_REDIS_ADDR ?= $(TEST_REDIS_ADDR)
SCHEDULER_RESOURCES_REDIS_ADDR ?= $(TEST_REDIS_ADDR)
CURSOR_TEST_REDIS_ADDR ?= $(TEST_REDIS_ADDR)
QUOTES_REDIS_ADDR ?= $(TEST_REDIS_ADDR)
TEST_REDIS_URL ?= redis://$(TEST_REDIS_ADDR)/0
export AFFINITY_REDIS_TEST_ADDR ACCOUNTING_TEST_REDIS_ADDR QUOTA_TEST_REDIS_ADDR
export SECURITY_REDIS_TEST_ADDR PROMPT_AUDIT_TEST_REDIS_ADDR
export SCHEDULER_RESOURCES_REDIS_ADDR CURSOR_TEST_REDIS_ADDR QUOTES_REDIS_ADDR
export TEST_REDIS_URL

# `local` keeps developer runs usable without the CI-only dedicated fixture
# topology; CI overrides this with GO_TEST_STRICT_PROFILE=unit/integration.
GO_TEST_STRICT_PROFILE ?= local
export GO_TEST_STRICT_PROFILE

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
	$(STRICT) -tags=unit -p 1 -count=1 ./...

test-integration:
	$(STRICT) -tags=integration -p 1 -count=1 ./...

# Golden E2E fixtures: failure semantics and the user-flow client contract run
# against in-process httptest servers, so they need no external services.
test-e2e:
	$(STRICT) -tags=e2e -count=1 -timeout=300s -run '^(TestE2EFailuresAreNotSkipped|TestE2EFailureFixture|TestE2ELocalUserFlow)$$' ./internal/integration/

# Live E2E suite. Requires BASE_URL (a running gateway), CLAUDE_API_KEY,
# GEMINI_API_KEY and ADMIN_PASSWORD. The strict gate fails if they are absent
# instead of silently skipping the live coverage.
test-e2e-local test-e2e-live:
	GO_TEST_STRICT_PROFILE=e2e $(STRICT) -tags=e2e -count=1 -timeout=600s ./internal/integration/...
