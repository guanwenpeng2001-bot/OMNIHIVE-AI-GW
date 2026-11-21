# T1 v0.3.2 catalog verification

Branch: `codex/v032-be-catalog`. No push or deployment. The requested external contract note matches `docs/architecture/v032-catalog-contract.md` byte-for-byte.

Implemented full approved-registry inventory for configured platforms in v2, optional `availability_state`, scoped discovered-model observations, canonical multimodal task presentation, and action-only catalog filtering. Existing v1 pricing views, public evidence redaction, gateway membership, project boundaries, and tariff requirements remain intact. Kimi Coding CN/global and Ark PAYG discovery now use their exact listing endpoints; Kimi native reasoning/vision booleans are retained.

No T2-owned files, preset JSON, capability-release data, or manifest hashes changed. Provider-limited OAuth discoveries remain provider-limited account evidence; the separate platform inventory supplies the broader Model Plaza view.

## Commands and results

All commands ran from the worktree's `backend` directory, one package at a time, using the required gate and environment:

```bash
eval "$(/home/administrator/Documents/Codex/2026-11-19/omnihive-refactor-workspace/acceptance/wave3/bin/localdb env | sed s/55480/55493/g)"
export GOCACHE=/home/administrator/Documents/Codex/2026-11-19/omnihive-refactor-workspace/acceptance/wave3/gocache
export GOTMPDIR=/home/administrator/Documents/Codex/2026-11-19/omnihive-refactor-workspace/acceptance/wave3/gotmp
export GOTOOLCHAIN=local GOFLAGS="-mod=readonly -buildvcs=false" GOMAXPROCS=3
export QUOTES_POSTGRES_DSN="$UPSTREAM_JOB_TEST_DSN" QUOTES_REDIS_ADDR="$AFFINITY_REDIS_TEST_ADDR"
heavy=/home/administrator/Documents/Codex/2026-11-19/omnihive-refactor-workspace/acceptance/wave3/bin/heavy

for package in \
  ./internal/modules/catalog \
  './internal/modules/catalog/postgres' \
  ./internal/modules/catalog/verification \
  ./internal/legacy/catalog \
  ./internal/control/catalog/models \
  ./internal/control/catalog/quotes \
  ./internal/control/catalog/compare \
  ./internal/control/catalog/verification; do
  "$heavy" go test -tags=unit,integration "$package" -count=1 -timeout=5m
  "$heavy" go vet -tags=unit,integration "$package"
done

"$heavy" go test -tags=unit ./internal/repository -run 'ModelCatalog|CatalogScope|Configured' -count=1 -timeout=5m
"$heavy" go vet -tags=unit ./internal/repository
"$heavy" go test -tags=unit ./internal/service -run 'Plaza|Catalog|Kimi|UpstreamModel|ModelCapability|PlatformModelPreset|NativeProfile|Discovery|Registry|VisibleModel|VolcenginePlatformFlow' -count=1 -timeout=10m -json
"$heavy" go test -tags=unit ./internal/service -run 'TestModelRegistryCatalog|TestModelPlazaConfigured|TestModelPlazaComposite' -count=1 -timeout=5m
"$heavy" go vet -tags=unit ./internal/service
```

| Package / check | Final result |
| --- | --- |
| modules/catalog | PASS, 0.033s |
| modules/catalog/postgres | PASS, 0.031s |
| modules/catalog/verification | PASS, 0.639s, including PostgreSQL queue test |
| legacy/catalog | PASS, 0.172s, including complete configured OpenAI inventory with no live accounts |
| control/catalog/models | PASS, 0.208s, including storage/HTTP integration and action filters |
| control/catalog/quotes | PASS, 0.010s |
| control/catalog/compare | PASS, 0.032s; `TestBE07QuoteCompareAndMigration` explicitly ran with quote DB/Redis variables |
| control/catalog/verification | PASS, 6.240s, including verification lifecycle integration |
| repository targeted catalog tests | PASS, 1.443s, including PostgreSQL paused-account and workspace isolation |
| service broad targeted sweep | PASS, 63.429s; 2,431 tests/subtests passed, zero skipped |
| service final profile-isolation/projection checks | PASS, 0.198s |
| go vet, all ten packages above | PASS, exit 0 |
| git diff --check | PASS |

Initial regression runs caught the obsolete Kimi-discovery rejection expectation and the action-only filter's executable-capability restriction. Both were corrected; the affected tests passed on rerun. Shared `native` profile labels are also tested to prevent relabeling a capability record as another platform.

PostgreSQL used port **55493**. No production credentials or inference requests were used. The registry/preset coverage and release-validation tests passed without changing release inputs.

## Exact changed files

- `backend/internal/control/catalog/models/handler_test.go`
- `backend/internal/control/catalog/models/list.go`
- `backend/internal/legacy/catalog/catalog.go`
- `backend/internal/legacy/catalog/configured_catalog_test.go`
- `backend/internal/modules/catalog/builtin.go`
- `backend/internal/modules/catalog/model.go`
- `backend/internal/modules/catalog/postgres/projection_test.go`
- `backend/internal/modules/catalog/reader.go`
- `backend/internal/modules/catalog/tasks.go`
- `backend/internal/modules/catalog/tasks_test.go`
- `backend/internal/repository/account_repo.go`
- `backend/internal/repository/account_repo_model_catalog_platform_test.go`
- `backend/internal/service/cn_providers_test.go`
- `backend/internal/service/model_plaza_catalog.go`
- `backend/internal/service/model_plaza_catalog_test.go`
- `backend/internal/service/model_plaza_service.go`
- `backend/internal/service/model_registry_catalog_test.go`
- `backend/internal/service/model_registry_metadata.go`
- `backend/internal/service/native_provider_profiles.go`
- `backend/internal/service/upstream_models.go`
- `backend/internal/service/upstream_models_catalog_coverage_test.go`
- `backend/internal/service/upstream_models_discovery_test.go`
- `backend/internal/service/upstream_models_metadata.go`
- `backend/internal/service/volcengine_platform_flow_test.go`
- `docs/architecture/v032-catalog-contract.md`
- `docs/architecture/v032-catalog-verification.md`
