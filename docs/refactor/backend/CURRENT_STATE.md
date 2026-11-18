# Backend source inventory

Measured on 2026-11-19 in branch `codex/architecture` at `e385977f2464a6943f46dc7eb74acc29e928ec0a`. The binding inputs are `docs/refactor/CHARTER.md` and all 1,394 lines of `docs/refactor/PLAN_v1.0.md`. Paths below are repository-relative. Counts describe this checkout. Runtime coverage and production behavior require the execution tests in `WORK_PACKAGES.md`.

## Measurement method

The inventory uses `rg --files`, `wc -l`, and source reads. Go counts exclude generated Ent output outside `backend/internal`. Production means a Go file whose name ends with `.go` and excludes `_test.go`. Line counts include comments and blank lines. Test-file counts measure regression assets.

```sh
git rev-parse HEAD
rg --files backend/internal -g '*.go'
rg --files backend/internal -g '*_test.go'
rg --files backend/internal -g '*.go' -g '!**/*_test.go' | xargs wc -l
rg -n 'float64' backend/internal -g '*.go' -g '!**/*_test.go'
rg -n 'field.Float|SchemaType' backend/ent/schema
rg -n 'go:build' backend/internal -g '*_test.go'
```

## Package map

`backend/internal` contains 3,533 Go files, including 1,401 production files and 2,132 test files. Production source totals 498,636 lines.

| Area | Production files | Production lines | Test files | Current responsibility |
| --- | ---: | ---: | ---: | --- |
| `service` | 786 | 304,669 | 1,139 | Domain entities, application orchestration, upstream HTTP, scheduling, pricing, settlement, auth, tasks, Ops and Wire composition share one package |
| `handler` | 200 | 79,497 | 372 | Gin ingress, admin/user DTOs, gateway lifecycle, protocol handling and page aggregation |
| `repository` | 167 | 57,851 | 316 | Ent and direct SQL, Redis caches, leases, queues, transaction boundaries and outboxes |
| `pkg` | 111 | 28,660 | 149 | Protocol bridges, provider clients, HTTP/TLS, response/errors, statistics, secrets and network utilities |
| `server` | 42 | 6,457 | 62 | Route registration, auth/workspace middleware, readiness and server composition |
| `securityaudit` | 21 | 5,582 | 16 | Security audit analysis and evidence |
| `config` | 3 | 3,982 | 11 | Configuration and validation |
| `payment` | 15 | 3,835 | 15 | Payment registry, amount/currency helpers and channel adapters |
| `domain` | 26 | 3,360 | 23 | Provider identity/actions, capability definitions, model allowlists, workspace scope and compatible-provider JSON catalog |
| `setup` | 3 | 1,427 | 3 | Installation/setup |
| `testutil` | 10 | 1,053 | 1 | Test infrastructure |
| `util` | 5 | 940 | 6 | Utilities and redaction |
| `web` | 6 | 640 | 6 | Embedded frontend and web serving |
| `platform` | 3 | 330 | 1 | Live attestation only at this snapshot |
| `middleware` | 1 | 205 | 2 | Additional middleware |
| `model` | 2 | 148 | 2 | Small shared models |
| Integration-only directories | 0 | 0 | 8 | `datamanagementintegration`, `integration`, `integrationtests`, `securityintegration` |

The target `platform` infrastructure module will absorb existing database/network/clock implementations from their actual owners. Its present contents cover attestation. `domain` currently contains provider and scope definitions while most business entities remain in `service`.

### Persistence and composition

- Ent has 51 Go schema files including one test. Existing entities cover workspaces/members, accounts/groups, model access, credential secrets, API-key credentials, usage, subscriptions, payment orders, identity sessions and batch images. Direct SQL also owns durable settlement receipts, upstream jobs, reservation and outbox storage. Schema discovery must include migrations and repositories together.
- `backend/migrations/254_usage_settlement_receipts.sql` defines receipt identity `UNIQUE(request_id, api_key_id)`, pending indexing and immutable recovery payloads. `backend/migrations/269_add_credential_secret_references.sql` adds encrypted secret references and database guards against cross-account/workspace rebinding.
- `backend/internal/server/routes/gateway.go` has 513 lines of protocol/platform/action wiring. Existing standard inference routes and their middlewares are the compatibility entry points.
- `backend/internal/server/routes/user.go`, `admin.go`, `payment.go`, `model_access.go`, and `model_plaza.go` expose existing management functions. The management response is `{code,message,reason?,metadata?,data?}` in `backend/internal/pkg/response/response.go:15`. Existing clients rely on this envelope.
- The checkout pins `github.com/shopspring/decimal v1.4.0` in `backend/go.mod`. The independence workstream owns module identity changes and regenerated imports.

## Largest source files

| File under `backend/internal` | Lines | Extraction responsibility |
| --- | ---: | --- |
| `repository/account_repo.go` | 4,788 | Account persistence, atomic updates and legacy adapters |
| `handler/openai_gateway_handler.go` | 4,434 | Responses, Messages, WebSocket turn orchestration, retries and settlement hooks |
| `service/gemini_messages_compat_service.go` | 4,037 | Gemini/Messages protocol conversion |
| `config/config.go` | 3,939 | Configuration contract |
| `handler/admin/account_handler.go` | 3,787 | Connection, credentials, model and pool mutations |
| `service/account.go` | 3,606 | Account entity and provider eligibility helpers |
| `service/account_test_service.go` | 3,270 | Provider connectivity and capability probes |
| `service/content_moderation.go` | 3,139 | Moderation policy and execution |
| `service/openai_account_scheduler.go` | 3,111 | Candidate filtering, scoring, affinity, snapshots and slot acquisition |
| `service/ratelimit_service.go` | 2,906 | Limits and coordination |
| `service/upstream_models.go` | 2,750 | Discovery and provider-specific model metadata |
| `service/gateway_scheduling.go` | 2,696 | Gateway account selection |
| `service/openai_codex_models_service.go` | 2,630 | Codex model projection |
| `service/openai_gateway_passthrough.go` | 2,557 | Upstream transport and passthrough |
| `service/openai_gateway_request_body.go` | 2,550 | Request adaptation |
| `handler/ops_error_logger.go` | 2,538 | Diagnostic recording |
| `handler/admin/setting_handler_update.go` | 2,512 | Settings mutation |
| `service/openai_ws_pool.go` | 2,459 | WebSocket connection lifecycle |
| `service/openai_gateway_response_handling.go` | 2,420 | Response and stream handling |
| `service/billing_service.go` | 2,383 | Token/cache/media and tiered pricing |
| `pkg/apicompat/chatcompletions_responses_bridge.go` | 2,345 | Chat/Responses conversion |
| `handler/gateway_handler.go` | 2,301 | Messages ingress and execution |

Additional extraction anchors are `gateway_request_plan.go` at 641 lines, `attempt_budget.go` at 395, `gateway_usage_billing.go` at 1,173, `openai_gateway_usage.go` at 1,314, `model_pricing_resolver.go` at 479, `usage_settlement_service.go` at 337, and `usage_settlement_receipt.go` at 230. All are in `backend/internal/service`.

## Existing behavior to preserve

| Area | Source evidence | Required preservation |
| --- | --- | --- |
| Request plan | `service/gateway_request_plan.go:32`, `:44`, `:115`, `:157` | Frozen catalog/pricing dependencies, shared attempt budget, one usage claim, cancellation, lease release, distinct client/receipt status |
| Attempt budget | `service/attempt_budget.go:18`, `:29`, `:44`, `:67` | Shared deadline and attempt allowance, uncertain submission handling, response commitment, per-WS-turn renewal |
| Scheduling | `service/openai_account_scheduler.go:70`, `:97`, `:386`, `:808`, `:863`, `:1809` | Affinity, weighted order, load, TTFT/error rate, cost/quota facts and eligibility |
| Catalog | `service/model_registry.go` 90 lines, `model_plaza_service.go` 417 lines | Registry snapshots, visibility, group/channel price projection and model discovery |
| Secrets | `service/credential_secret.go` 185 lines, `repository/credential_secret_repo.go` | Versioned metadata, reference conversion, rotation/revocation and ownership checks |
| Workspace | `service/workspace_service.go` 102 lines, `server/middleware/workspace_resources.go`, `ent/schema/mixins/workspace.go` | Existing tenant scope and membership |
| Tasks | `service/upstream_job_service.go` 476 lines, `image_task.go` 326, `batch_image_worker.go` 369 | Upstream ownership, stored results, batch leases, cancellation and billing recovery |
| Payments | `server/routes/payment.go:26`, `:60`, `:72`, `internal/payment/provider/` | User orders/refunds, existing webhook URLs and payment-channel behavior |

### Product gaps verified beyond route names

`backend/internal/server/routes/model_plaza.go:35` already registers `POST /api/v1/public/playground/chat`. Its implementation in `handler/mcp_grant_handler.go:588` forwards a single non-streaming Chat request to `OMNIHIVE_PLAYGROUND_BASE_URL` using an operator-configured credential. It fixes `max_tokens` to 1,024 and uses a 30-second timeout. This establishes a real forwarding path. The new Playground needs authenticated project/key attribution, shared execution, streaming, cancellation, structured tool events and receipt correlation.

`handler/mcp_grant_handler.go:512` explicitly defines routing policies as account preferences. `GetRoutingPolicies` and `UpdateRoutingPolicies` use handler-local state, and initialization supplies a monthly budget of 3,500 and provider defaults at line 525. The state resets with the process. The new routing editor needs persisted scope, published versions and production enforcement. Existing preference values need explicit owner mapping during migration.

The same 1,519-line handler seeds example grants and audit entries at lines 82–98. Its model ranking snapshot at lines 342–365 is static and labeled `curated_weekly_reference`; it supplies no live traffic sample. Production pages must consume actual audit/onboarding facts. Existing MCP metadata routes stay outside the new inference product contract and are hidden from shipped navigation according to the charter.

`service/backup_service.go` has 1,491 lines. `StartRestore` at line 933 and `executeRestore` at line 1002 mutate a backup record from a goroutine. Startup marks interrupted restores failed at lines 275–277. This path needs durable operation identity and fencing through the complete restore lifecycle.

## Repeated rules and extraction order

| Plan item | Verified locations | Classification and action |
| --- | --- | --- |
| B01 | `handler/openai_gateway_handler.go:659` and its parallel switch-count initializations at lines 1356 and 2683, `handler/failover_loop.go:125`, `service/attempt_budget.go:44` | Repeated switch/retry/exclusion bookkeeping around an existing shared budget. Extract AttemptController decisions and migrate every caller. Preserve OAuth 429, transport, affinity and WS-turn semantics |
| B02 | `handler/gateway_handler.go`, `handler/openai_gateway_handler.go`, `service/openai_gateway_response_handling.go` | Overlapping response-finalization responsibility. Introduce ResponseLifecycle with protocol-owned error/terminal encoders and semantic-output commitment |
| B03 | `server/middleware/api_key_auth.go` 450 lines, `api_key_auth_google.go` 279 | Repeated identity, workspace, IP/status, group, quota and subscription orchestration. Use one access decision service with separate credential extractors and protocol error renderers |
| B04 | `service/gateway_usage_billing.go:924`, `service/openai_gateway_usage.go:1035` | Near-identical channel/group selection. Both already call ModelPricingResolver. Move all source precedence into that authority |
| B05 | `service/gateway_usage_billing.go:622`, `service/openai_gateway_usage.go:155` | Repeated usage/log assembly around shared settlement. Introduce evidence assembly with adapter-specific extraction, preserving cache/reasoning/media and requested/effective model distinctions |
| B06 | `service/upstream_models_metadata.go` 206 lines, `model_registry_metadata.go` 54, `handler/public_model_metadata.go` 317 | Three distinct stages with overlapping metadata fields. Share evidence types and units, then retain discovery, merge and redacted projection stages |
| B07 | `service/upstream_job_service.go`, `image_task.go`, `batch_image_worker.go` | Related lifecycles with separate domain responsibilities. Share fenced ownership and terminal transition primitives, keeping provider completion, artifact completion and settlement separately visible |
| B08 | `service/gateway_request_plan.go:415`, `:428`, `handler/gateway_request_plan.go` | Thin plan-construction wrappers around shared logic. Consolidate while migrating neighboring callers |

## D04 financial float64 inventory

Across production `backend/internal` there are 2,351 lines containing `float64`. This includes measurements such as latency and scoring. Financial conversion uses the ledger below and the field-level scan, while metrics retain their appropriate numeric types.

| Directory | Matching lines | Files with matches |
| --- | ---: | ---: |
| `service` | 1,517 | 209 |
| `handler` | 404 | 42 |
| `repository` | 241 | 32 |
| `pkg` | 113 | 13 |
| `payment` | 22 | 8 |
| `domain` | 8 | 2 |

The remaining 46 matching lines occur in other internal directories. Every migration package must search its complete input/output chain, including DTO, JSON snapshot, cache and SQL scan representations.

| Financial responsibility | Source under `backend/internal` | Lines / float64 matching lines | Fields or behavior |
| --- | --- | --- | --- |
| Cost calculation | `service/billing_service.go` | 2,383 / 108 | Prices, CostBreakdown, multipliers, tokens/cache/media amounts |
| Price acquisition | `service/pricing_service.go` | 1,744 / 37 | Model price data and price lookup |
| Price authority | `service/model_pricing_resolver.go` | 479 / 6 | Resolved pricing and tier inputs |
| Price snapshot | `service/billing_pricing_snapshot.go` | 308 lines | `PricingSnapshot`, freeze/recalculate, unit price and charged amount |
| Channel prices | `service/channel.go` | 677 / 24 | Channel model rates, pricing intervals and overrides |
| Group prices/limits | `service/group.go` | 501 / 34 | Rate multipliers, daily/weekly/monthly limits, media/search pricing |
| Wallet | `service/user.go` | 110 / 5 | Balance, frozen balance, recharge total and notification threshold |
| Key spending | `service/api_key.go` | 170 / 12 | Quota, used amounts and rolling spend limits |
| Usage | `service/usage_log.go` | 252 / 11 | Input/output/cache/image costs, total/actual/account costs |
| Financial command | `service/usage_billing.go` | 235 / 20 | Balance, subscription, key quota/rate and account quota costs, hold/capture results |
| Receipt validation | `service/usage_settlement_receipt.go` | 230 / 5 | Float arrays and equality checks over frozen amounts |
| Budget | `service/api_key_budget.go` | 418 / 9 | Limits, usage, reservations, remaining and overrun |
| Orders/payment | `service/payment_order.go`, `payment_service.go` | 903 / 14, 385 / 10 | Amount, pay amount, refunds and fulfillment inputs |
| Subscription | `service/subscription_service.go`, `user_subscription.go` | 1,283 / 8, 233 / 7 | Consumption windows and entitlement limits |
| Affiliate | `service/affiliate_service.go` | 624 / 41 | Commission, transfer, totals and ledger snapshots |
| SQL boundary | `repository/usage_billing_repo.go:334`, `:369`, `:554`, `:580`, `:605` | 697 lines | Float parameters and scans for wallet, subscription, key and account quota |
| Reservation SQL | `repository/api_key_budget_repo.go` | 27 float64 matching lines | Transactional budget/hold amounts |
| Views/cache | `repository/billing_cache.go`, `service/billing_cache_service.go`, `pkg/usagestats/usage_log_types.go`, `handler/dto/types.go` | Inspect all financial fields | Cached balances, aggregate costs and frontend JSON numbers |
| Payment boundary | `payment/fee.go:12`, `payment/currency.go:77`, `:115`, `payment/types.go`, `payment/provider/` | 22 matching lines across payment package | Decimal helpers still accept/return floats at some edges |
| Provider cost | `pkg/xai/billing.go`, provider-specific `service/*billing*` and `*usage*` | 23 matches in XAI billing | External estimated cost and quota units must carry source/currency |

### Existing precision and fingerprint contract

`service/usage_billing.go:68` sets monetary scale to eight. `QuantizeUsageBillingAmount` at line 103 uses decimal half-away-from-zero and returns float64. `Normalize` computes a missing fingerprint from original money before quantization at line 53. Fingerprint input formatting at line 115 uses ten fractional digits. Historical receipt replay must retain the stored fingerprint and versioned decoder throughout conversion.

PostgreSQL already stores exact numerics in these schemas. The main defect boundary for migration is the Go and JSON representation.

| Existing schema | PostgreSQL precision |
| --- | --- |
| `ent/schema/user.go:49`, `api_key.go:65` | Wallet, frozen balance, key quota/spend use `decimal(20,8)` through `field.Float` |
| `ent/schema/usage_log.go:147` | Usage cost components use `decimal(20,10)`, multipliers use `decimal(10,4)` |
| `ent/schema/user_subscription.go:62`, `user_platform_quota.go:71` | Consumption and limits use `decimal(20,10)` |
| `ent/schema/group.go:50` | Multipliers use `decimal(10,4)`, monetary limits/prices commonly use `decimal(20,8)` |
| `ent/schema/payment_order.go:46`, `subscription_plan.go:40` | Prices/payment/refund amounts use `decimal(20,2)` |
| `ent/schema/batch_image_job.go:53`, `batch_image_item.go:41` | Estimate/hold/actual/billed amounts use `decimal(20,10)` |

The Ent schema scan finds these financial and dimensionless fields exposed through `field.Float`:

| Schema file under `backend/ent/schema` | Count | Field names |
| --- | ---: | --- |
| `account.go` | 1 | `rate_multiplier` |
| `api_key.go` | 8 | `quota`, `quota_used`, `rate_limit_5h`, `rate_limit_1d`, `rate_limit_7d`, `usage_5h`, `usage_1d`, `usage_7d` |
| `batch_image_item.go` | 1 | `billed_amount` |
| `batch_image_job.go` | 3 | `estimated_cost`, `hold_amount`, `actual_cost` |
| `group.go` | 22 | `rate_multiplier`, `peak_rate_multiplier`, `daily_limit_usd`, `weekly_limit_usd`, `monthly_limit_usd`, `image_rate_multiplier`, `image_price_1k`, `image_price_2k`, `image_price_4k`, `batch_image_discount_multiplier`, `batch_image_hold_multiplier`, `video_rate_multiplier`, `video_price_480p`, `video_price_720p`, `video_price_1080p`, `web_search_price_per_call`, `search_price_per_1k`, `audio_realtime_price_per_min`, `audio_tts_price_per_million_chars`, `audio_stt_price_per_hour`, `profit_min_margin`, `profit_safety_buffer` |
| `payment_order.go` | 4 | `amount`, `pay_amount`, `fee_rate`, `refund_amount` |
| `promo_code.go` | 1 | `bonus_amount` |
| `promo_code_usage.go` | 1 | `bonus_amount` |
| `redeem_code.go` | 1 | `value` |
| `subscription_plan.go` | 2 | `price`, `original_price` |
| `usage_log.go` | 11 | `image_output_cost`, `image_input_cost`, `input_cost`, `output_cost`, `cache_creation_cost`, `cache_read_cost`, `total_cost`, `actual_cost`, `rate_multiplier`, `account_rate_multiplier`, `account_stats_cost` |
| `user.go` | 4 | `balance`, `frozen_balance`, `balance_notify_threshold`, `total_recharged` |
| `user_platform_quota.go` | 6 | `daily_limit_usd`, `weekly_limit_usd`, `monthly_limit_usd`, `daily_usage_usd`, `weekly_usage_usd`, `monthly_usage_usd` |
| `user_subscription.go` | 3 | `daily_usage_usd`, `weekly_usage_usd`, `monthly_usage_usd` |

Existing price/credential/settings JSON, Redis serialization, statistics rollups, redemption values, promotions and affiliate entries are additional conversion inputs. The exhaustive field audit for BE-07/BE-11 combines `rg -n 'float64'` with `rg -n 'amount|cost|price|balance|quota|rate_multiplier|commission|refund'` on each touched package. Each discovered field receives a Money, Rate, Quantity, percentage or nonfinancial-measure classification before cutover.

## D05 receipt durability boundary

1. `service/usage_settlement_service.go:15` defines the recovery boundary as the first successful receipt commit.
2. `Submit` at line 89 prepares and freezes a receipt, writes it through the repository, and resolves a failed acknowledgement using `Get` with the original identity. Conflicting identity/fingerprint is rejected.
3. Only the durable key/fingerprint enters the worker pool. Queue overflow or a stopped pool triggers synchronous fallback. The recovery goroutine sweeps every 15 seconds.
4. `repository/usage_settlement_receipt_repo.go` locks receipt → dedup → usage → financial effects. Settlement, usage association and effect application use the transaction. Replays inspect existing state.
5. `Recover` at `service/usage_settlement_service.go:248` visits pending identities and releases expired reservations. Recovery uses stored evidence and frozen prices.
6. A process failure after upstream execution and before receipt commit can leave missing usage. `persistFailures` is process-local. The target adds a durable pre-dispatch execution record and reconciliation state so such executions remain discoverable after restart.

Unknown usage, unknown submission, stream completion, receipt persistence and financial settlement need distinct fields. Existing zero-valued legacy usage can be ambiguous, as the comment at `usage_settlement_service.go:96` records. The new contract preserves `unknown` and `needs_review`, alongside the price and evidence source.

## Regression inventory

The following path-pattern counts overlap because individual tests cover multiple responsibilities. Each expression was applied to the full repository-relative path of every `backend` Go test. The tag count searches those files for the literal `go:build integration`. A package may run PostgreSQL tests under other conditions. Test existence establishes assets to reuse. The implementation queue requires execution with the appropriate `unit` and `integration` tags and real PostgreSQL/Redis.

| Area and filename pattern | Test files | Explicit `integration` tag | Representative assets |
| --- | ---: | ---: | --- |
| Gateway/protocol `gateway\|gemini_messages\|apicompat\|realtime\|websocket\|openai_ws\|responses_bridge` | 322 | 3 | `handler/openai_ws_interrupted_receipt_test.go`, `handler/gateway_request_plan_ingress_test.go`, `service/billing_protocol_regression_test.go` |
| Routing `schedul\|failover\|attempt_budget\|routing\|affinity` | 77 | 4 | `handler/attempt_budget_test.go`, `repository/gateway_routing_integration_test.go`, scheduler tests |
| Money/settlement `billing\|pricing\|settlement\|receipt\|budget\|quota\|ledger` | 203 | 10 | `service/usage_billing_quantize_test.go`, `billing_pricing_snapshot_test.go`, `repository/usage_billing_repo_integration_test.go`, `billing_contention_postgres_test.go` |
| Identity `auth\|workspace\|passkey\|totp\|api_key\|apikey\|model_access` | 171 | 16 | `server/middleware/api_key_auth_test.go` 1,832 lines, `api_key_auth_google_test.go` 910, `repository/workspace_scope_postgres_test.go` |
| Credential `credential\|token_refresh\|oauth\|egress` | 99 | 2 | `service/credential_secret_test.go`, `repository/credential_secret_postgres_test.go` |
| Catalog `model_registry\|model_plaza\|upstream_models\|platform_capabil` | 16 | 0 | `handler/model_plaza_handler_test.go`, `domain/platform_capabilities_test.go` |
| Tasks `batch_image\|image_task\|upstream_job` | 35 | 1 | `service/batch_image_billing_recovery_test.go`, `upstream_job_request_plan_test.go`, `repository/upstream_job_receipt_test.go` |
| Operations `ops_\|audit\|backup\|restore\|monitor` | 127 | 9 | Ops tests, backup tests, `repository/proxy_restore_probe_integration_test.go` |
| Payments `payment\|subscription\|redeem\|affiliate` | 83 | 6 | Channel signature/refund tests, `payment/amount_test.go`, `payment/fee_test.go` |

Required new regression coverage addresses durable restore fencing, all-scope policy intersection, simulation/production parity, exact-money JSON and historic receipt decoding, pre-receipt execution uncertainty, project-aware exports/artifacts, BYOK funding transitions and all new page contracts. Runtime line/branch coverage has not been measured for this document.
