.PHONY: build build-backend build-frontend test test-backend test-backend-unit test-backend-integration \
	test-frontend test-frontend-critical test-e2e test-e2e-live

FRONTEND_CRITICAL_VITEST := \
	src/i18n/__tests__/localeKeyCompleteness.spec.ts \
	src/api/__tests__/client.spec.ts \
	src/api/__tests__/tokenRefresh.spec.ts \
	src/api/__tests__/keys.bulkUpdate.spec.ts \
	src/components/keys/__tests__/BulkEditKeysModal.spec.ts \
	src/views/user/__tests__/KeysView.spec.ts \
	src/api/__tests__/channelMonitorV2.spec.ts \
	src/views/auth/__tests__/LinuxDoCallbackView.spec.ts \
	src/views/auth/__tests__/WechatCallbackView.spec.ts \
	src/views/user/__tests__/PaymentView.spec.ts \
	src/views/user/__tests__/PaymentResultView.spec.ts \
	src/views/user/__tests__/ChannelStatusView.mode.spec.ts \
	src/components/user/profile/__tests__/ProfileInfoCard.spec.ts \
	src/views/admin/__tests__/SettingsView.spec.ts \
	src/components/account/__tests__/QianwenPricingPanel.spec.ts \
	src/components/account/__tests__/CursorKeyFields.protocol.spec.ts \
	src/components/account/__tests__/credentialsBuilder.spec.ts \
	src/components/account/__tests__/credentialsBuilder.cnAdaptive.spec.ts \
	src/constants/__tests__/platforms.spec.ts \
	src/views/admin/__tests__/GroupsView.duplicate.spec.ts \
	src/views/admin/__tests__/GroupsView.compositePlatforms.spec.ts \
	src/views/admin/__tests__/GroupsView.columnSettings.spec.ts \
	src/views/admin/__tests__/GroupsView.codexManifest.spec.ts \
	src/views/admin/__tests__/GroupsView.pricingPayload.spec.ts \
	src/views/admin/__tests__/groupsVideoModelPricing.spec.ts \
	src/views/admin/__tests__/groupsImagePricing.spec.ts \
	src/utils/__tests__/intervalPrices.spec.ts \
	src/components/admin/channel/__tests__/PricingEntryCard.timePricing.spec.ts \
	src/components/admin/channel/__tests__/TimePricingSection.spec.ts \
	src/features/channel-telemetry/__tests__/designSystem.structure.spec.ts \
	src/features/channel-telemetry/__tests__/monitorFormat.spec.ts \
	src/features/channel-telemetry/__tests__/monitorZoom.spec.ts

# 一键编译前后端
build: build-backend build-frontend

# 编译后端（复用 backend/Makefile）
build-backend:
	@$(MAKE) -C backend build

# 编译前端（需要已安装依赖）
build-frontend:
	@pnpm --dir frontend run build

# 运行测试（后端 + 前端）
test: test-backend test-frontend

test-backend:
	@$(MAKE) -C backend test

# Strict Go gates (scripts/go-test-strict.sh): any unexpected Test* skip fails.
# Provide TEST_DSN/TEST_REDIS_ADDR (and, for the full unit matrix, the dedicated
# fixture topology documented in backend/Makefile) or use the CI workflow.
test-backend-unit:
	@$(MAKE) -C backend test-unit

test-backend-integration:
	@$(MAKE) -C backend test-integration

# Golden E2E fixtures (no external services required).
test-e2e:
	@$(MAKE) -C backend test-e2e

# Live E2E suite against BASE_URL with CLAUDE_API_KEY/GEMINI_API_KEY/ADMIN_PASSWORD.
test-e2e-live:
	@$(MAKE) -C backend test-e2e-live

test-frontend: test-frontend-check
	@pnpm --dir frontend run test:run

.PHONY: test-frontend-check test-shell
test-frontend-check:
	@node scripts/check-frontend-type-drift.mjs
	@pnpm --dir frontend run lint:check
	@pnpm --dir frontend run typecheck
	@pnpm --dir frontend run check:i18n

# Includes Apple container checks; run this aggregate on the macOS CI runner.
test-shell:
	@bash -n deploy/apple-container.sh
	@bash deploy/tests/apple-container-test.sh
	@sh deploy/tests/docker-compose-security-test.sh
	@python3 deploy/tests/docker-compose-lifecycle-test.py
	@python3 deploy/tests/docker-compose-bind-test.py
	@sh deploy/tests/docker-compose-gateway-env-test.sh
	@sh deploy/tests/docker-runtime-resources-test.sh
	@sh deploy/test-caddyfile-cache.sh
	@bash deploy/tests/docker-deploy-test.sh
	@bash deploy/test-docker-deploy-secrets.sh
	@bash deploy/install_verify_test.sh
	@bash deploy/tests/install-pricing-resources-test.sh
	@bash deploy/tests/install-github-token-test.sh
	@if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then \
		python3 deploy/tests/deployment-config-test.py; \
	else \
		echo "skipping deploy/tests/deployment-config-test.py (docker compose unavailable)"; \
	fi

test-frontend-critical:
	@pnpm --dir frontend exec vitest run $(FRONTEND_CRITICAL_VITEST)

# Required round gates reject skipped/missing tests as well as failures.
# Set USAGE_STATS_TEST_DSN to a disposable stats_test database URL and
# AFFINITY_REDIS_TEST_ADDR (optional AFFINITY_REDIS_TEST_PASSWORD) to isolated Redis.
# Probe only when a Python gate runs; ordinary build/test targets need no Python.
# Running a probe also rejects Windows Store aliases that cannot run Python.
PYTHON ?= $(shell python3 -c "import sys; sys.exit(sys.version_info.major != 3)" >/dev/null 2>&1 && echo python3 || (python -c "import sys; sys.exit(sys.version_info.major != 3)" >/dev/null 2>&1 && echo python))
.PHONY: test-required-round test-required-usage-coverage test-required-catalog test-required-affinity
test-required-round: test-required-usage-coverage test-required-catalog test-required-affinity

test-required-usage-coverage:
	@$(if $(strip $(PYTHON)),,$(error Python 3 is required; install python3/python or set PYTHON to its executable))$(PYTHON) -B backend/scripts/required-round-tests.py usage-coverage

test-required-catalog:
	@$(if $(strip $(PYTHON)),,$(error Python 3 is required; install python3/python or set PYTHON to its executable))$(PYTHON) -B backend/scripts/required-round-tests.py catalog

test-required-affinity:
	@$(if $(strip $(PYTHON)),,$(error Python 3 is required; install python3/python or set PYTHON to its executable))$(PYTHON) -B backend/scripts/required-round-tests.py affinity
