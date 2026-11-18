# Backend execution queue

This queue assigns the complete backend scope of plan F01–F17 and B01–B08. Start **BE-01, BE-02 and BE-03 together**. They own disjoint source, test and migration files. They establish the three prerequisites for deployable features: server-evaluated scope, exact financial values, and protected gateway retry/stream behavior.

## Scheduling and integration

Each row below is a dispatch group of at most three agents. A later group may start a package as soon as its listed dependencies have landed and its file ownership is free. IDs identify implementation ownership. The team coordinator serializes integration and generated outputs. `astra` means `codex/gpt-6.2-astra` (xhigh). `sol` means `codex/gpt-6.2-sol` (high/max).

| Group | Slot 1 | Slot 2 | Slot 3 |
| --- | --- | --- | --- |
| A | BE-01 Identity, projects and access | BE-02 Money and historic fixtures | BE-03 Attempt and stream lifecycle |
| B | BE-04 Connections, credentials and pools | BE-05 Catalog facts and public projection | BE-06 Durable execution identity |
| C | BE-07 Exact pricing and quotes | BE-08 Production/simulation route planner | BE-09 Fenced restore operation |
| D | BE-10 Policy publication and simulator | BE-11 Exact settlement and wallet transaction | BE-12 Protocol and provider extraction |
| E | BE-13 Authenticated Playground | BE-14 Request detail and explain | BE-15 Image task center |
| F | BE-16 Cost and quota workbench | BE-17 Scoped BYOK pilot | BE-18 Public status and incidents |
| G | BE-19 Payments, subscriptions and remaining financial writers | BE-20 Plugin v2 and extension host | BE-21 Audit, security and operations |
| H | BE-22 Run modes and legacy route completion | BE-23 Migration and cutover tooling | BE-24 Release verification |
| I | BE-25 Verified audio/video tasks | BE-26 Provider/task plugin capabilities | BE-27 Catalog verification automation |
| J | BE-28 Complete BYOK self-service | BE-29 Workspace telemetry exports | BE-30 Contract and compatibility retirement |

Groups A–H deliver P0/P1 and the original retained functionality. Groups I–J assign explicit post-GA P2 work and compatibility retirement. BE-24 can prepare fixtures in parallel with BE-22/23, then executes its release gates after both merge. The same dependency rule applies to BE-30. Independent release assets, module renaming and brand migration are already owned by the independence workstream. Reuse that branch's manifest, plugin decisions and release identity.

### File and migration ownership

- Paths in package entries are relative to `backend/`, except `contracts/` and `docs/` which are repository-root directories. A module owns its descendants. A legacy basename under `internal/service` includes the listed file and its matching test, without granting the entire service directory. Newly introduced code uses the package's named directory or a uniquely prefixed sibling test.
- Each API-owning package supplies `api/v2/fragments/BE-NN.yaml`, `contracts/api/BE-NN/` fixtures, a module-specific `Register` function and its own constructor/provider set. Only the integration coordinator edits the aggregate route list, composition root, `wire.go` aggregates, generated `wire_gen.go`, shared Ent generated files, composed OpenAPI and generated frontend types. Integrate each package into the real entry points immediately after merge and run the smoke route before marking it finished. The coordinator owns that integration step even when the package has one agent.
- The latest SQL prefix at the measured base is 289. Allocate one **new unique** number per migration at merge. Reserve 290–319 for expand files corresponding to BE-01–30 (`289 + package number`), skipping unused slots. Later required DDL receives distinct coordinator-assigned numbers checked against the actual integration tip. Each migration belongs to its package. Rebuild any aggregate metadata centrally. Historical SQL, numbering allowances and checksums remain immutable.
- Reserve `internal/legacy/<package-id>/` and `contracts/migrations/<package-id>/` per package. A package's migration adapter carries its old/new caller inventory. The merge coordinator orders same-table changes and verifies old readers, mixed writers, indexes and rollback floor. Code that touches a legacy file claimed by an earlier package receives an explicit handoff in its ownership entry.
- Every workflow below includes its API action checks, cross-scope direct HTTP tests, version conflict, idempotency and production-route verification where applicable. Use the success/error envelope, currency strings, safe event fields, cursor rules and fixture shapes in `API_CONTRACTS.md`. `P/W/U/A` below expand exactly as in that document.
- Every package includes build/vet and its targeted tests under the appropriate Go build tags. Financial, queue, authorization-cache, replay and migration invariants require actual PostgreSQL and Redis integration coverage. Test-only mock adapters are confined to fixtures. Public data, rankings, dashboards, statuses and seeded records require a real source or an explicit unavailable state.
- Expand–migrate–contract entries describe the owning package's whole change. Destructive contracts are recorded for BE-30 and executed only after old binaries, readers and leases have exited. Read-only comparison is available before every financial cutover. Each monetary effect always has one authoritative writer.

## Group A

### BE-01 · Identity, project ownership and shared API-key policy · astra

**Goal and owners.** Deliver B03/F08 and the access portion of F16. Own `internal/modules/identity/`, `internal/core/{scope,fault}/`, `internal/control/identity/`, `internal/server/middleware/{api_key_auth,api_key_auth_google,workspace_scope,workspace_resources}.go` and neighbors' auth fixtures under `contracts/protocol/auth/`. Preserve protocol-specific credential extraction, Gemini query-key compatibility, usage/task read exemptions, IP checks, delegated action evaluation, TOTP and Passkey identity. Move ordinary and Google checks through one authority.

**API.** Scope/key routes under “Scope and keys” in `API_CONTRACTS.md`, including `GET P/access → {actor_id,actions,scope_version,expires_at}`, project CRUD entry points and per-project restricted key creation. Seed required action registry and operation query authorization. Implement `GET W/operations/{id}` with module-owned operation readers registered at composition.

**Database.** Expand projects, scoped grants and project-resource mapping with same-workspace FK constraints and unique default project per workspace. Migrate each provably owned resource to its default project with checkpoints; quarantine ambiguous rows. Keep existing API keys, passkey RP IDs, wallet owner and member IDs. Contract legacy default-project fallbacks after coverage reports reach zero.

**Tests.** Compare current regular/Gemini middleware fixtures, scoped lookup/exports/jobs/cache, mixed-version revoke propagation, expired keys, 403 vs 404, zero-balance diagnostic access, concurrent first default-project creation, cleanup after workspace changes. **Depends** on baseline only.

### BE-02 · Exact Money, rate and historical fixtures · astra

**Goal and owners.** Own `internal/core/money/`, `contracts/migrations/BE-02/`, and `contracts/api/BE-02/`. Implement Amount/Money/Rate/Quantity, SQL scanner/valuer, strict string JSON, bounds checks and fixed-scale rounding. Reuse the existing decimal dependency. Capture historical raw/frozen/charged values and legacy fingerprints from finite fixtures. Provide comparison functions for BE-07 and BE-11.

**API.** Shared schema `Money={currency,amount:string}`, `Rate={currency,amount,unit,per:string}`. This package introduces no endpoint. Golden input `0.000078125` gives charge `0.00007813` and reservation ceiling `0.00007813`.

**Database.** Zero DDL. Test read/write against existing `NUMERIC(20,8)`, `(20,10)`, `(20,2)` and proposed exact types. Emit signed zero, extreme bounds, malformed/exponent and mixed currency behavior.

**Tests.** Positive/negative half ties, exact multiplication/tier boundaries, nonterminating unit conversion rules, oversized user input, cache and JSON round trip, SQL numeric text, v1 fingerprint replay. **Depends** on baseline only.

### BE-03 · Attempt controller and response lifecycle · astra

**Goal and owners.** Deliver B01/B02 and first protected protocol slice of F07. Own `internal/gateway/execution/{attempt,lifecycle}/`, `internal/handler/{failover_loop,gateway_handler,openai_gateway_handler}.go`, `internal/service/attempt_budget.go`, and `contracts/protocol/lifecycle/`. Migrate Responses, Messages and WS turn loops to the common decision rules while preserving native wire encoders and the existing plan/budget. Preserve heartbeat-only, semantic output, terminal, OAuth retry windows, deadline, affinity and canceled requests.

**API.** Existing OpenAI/Anthropic/Gemini standard routes preserve native JSON/SSE/WS behavior. Internal `AttemptDecision={attempt_number,action,reason,excluded,acceptance,remaining,deadline}`. No console route yet.

**Database.** Zero DDL. Keep current budget/receipt ownership. BE-06 attaches durable execution records through new ports.

**Tests.** Replay identical failure sequences over all three loops, before/after first semantic output, uncertain submission, first heartbeat, failed terminal emission, double terminal, WS next turn/reconnect, deadline and same-account retries. Record per-protocol fixture outcomes and local overhead baseline. **Depends** on baseline only.

## Group B

### BE-04 · Provider schemas, connection wizard and credential versions · astra

**Goal and owners.** Deliver F09/F11. Own `internal/modules/{connections,credentials}/`, `internal/control/connections/`, `internal/service/{credential_secret,account_test_service}.go`, `internal/repository/credential_secret_repo.go`, and the existing admin credential-secret handler. Create typed provider schemas, draft/verify/preview/activate, pool assignments, per-item bulk preview/apply and fenced rotation. Keep existing account and proxy persistence behind one adapter.

**API.** All “Upstream connection wizard and credentials” routes. `GET W/provider-schemas → {items}`; `POST W/connections → Connection`; verify/rotate return Operation, activation returns confirmed version, clear/keep/set are explicit.

**Database.** Expand connection mappings, versioned encrypted credential rows, draft/verification snapshots, pool membership and safe lifecycle events. Preserve existing secret references and old account IDs. Migrate inline secrets only after successful encryption/decryption verification; retain quarantine status for unconvertible records. Contract old secret writes after traffic drains. Draft storage contains safe fields only.

**Tests.** Existing account/wizard behavior, five real workflow steps, stale verification, encrypted snapshots, revoked secret resolution, overlapping refresh/rotation, egress/redirect policy, heterogeneous bulk updates, cross-workspace pools and role projections. **Depends** BE-01.

### BE-05 · Capability evidence and public catalog projection · sol

**Goal and owners.** Deliver B06/F05/F14 base. Own `internal/modules/catalog/`, `internal/control/catalog/models/`, `internal/service/{model_registry,model_registry_metadata,upstream_models_metadata,model_plaza_service}.go`, `internal/handler/public_model_metadata.go`, and `contracts/capabilities/catalog/`. Keep acquisition, merge and sanitized publication separate. Represent the independent catalog/access/route/price states.

**API.** `GET U/models[/id]`, `GET P/models[/id] → ModelSummary list or ModelDetail`. Filter/sort, effective scope, verified endpoint actions, region/privacy evidence, sample counts and fresh/stale states. Existing public/catalog discovery policy remains authoritative.

**Database.** Expand versioned capability evidence, publication and snapshot provenance. Import existing registry, discovery and visibility sources with source/maturity. Preserve last successful evidence on refresh failure. Contract duplicate public formatting when legacy plaza also reads this projection.

**Tests.** Unknown vs false/zero, publication-safe redaction, stale discovery, model/endpoint difference, anonymous/authorized perspectives, filtering and stable cursor, sparse metrics, existing provider registry fixtures. **Depends** BE-01, BE-02 schema.

### BE-06 · Durable execution identity and attempt intent · astra

**Goal and owners.** Own `internal/gateway/execution/journal/`, `internal/service/gateway_request_plan.go`, `internal/handler/gateway_request_plan.go`, `contracts/protocol/execution/`. Deliver pre-dispatch identity, idempotency reservation, request/attempt links, release obligations, missing-receipt reconciliation candidates and the B08 factory handoff. Keep per-turn and per-job identities.

**API.** Internal `AdmittedExecution={request_id,scope,attempt_budget,versions,reservation_ref}`, `Attempt={id,request_id,acceptance,outcome}`. New page endpoints consume these through BE-13/14. Existing standard endpoints preserve protocol response format.

**Database.** Expand scoped request/attempt journal and idempotency registration keyed by actor, workspace, route, key and payload fingerprint. Link known legacy IDs through explicit mapping. Migrate one HTTP slice and one WS turn per normal route. Expired pending execution with dispatched attempt and no receipt enters reconciliation. Contract legacy plan wrappers when callers migrate.

**Tests.** Kill before dispatch, during dispatch, before receipt, lost DB acknowledgement, duplicate idempotency key with equal/different payload, WS turn rollover, missing/known-zero use, cleanup after timeout/panic. **Depends** BE-01, BE-03.

## Group C

### BE-07 · Exact pricing authority, quotes and comparisons · astra

**Goal and owners.** Deliver B04 and F05. Own `internal/modules/pricing/`, `internal/control/catalog/{compare,quotes}/`, `internal/service/{model_pricing_resolver,billing_service,billing_pricing_snapshot,pricing_service}.go`. BE-11 owns usage assembler call sites. This package builds the single exact calculator consumed there and by catalog/quotes.

**API.** `POST U/models/compare`, `POST P/models/compare → {catalog_version,as_of,rows:[{model_id,capabilities,quote,metrics,benchmarks}]}`, `POST P/quotes → Quote`. Inputs, units, cache subset, upper bounds and scenario counts use the catalog contract.

**Database.** Expand immutable price versions with source/units, exact rates and lookup precedence. Snapshot existing group/channel/time-band/tier/media prices with effective dates. Dual-calculate read-only against historical fixtures. Contract duplicate resolver paths after BE-11 switches all calls.

**Tests.** Free/unpriced, cache tiers, media/search/reasoning, long context, group/channel precedence, frozen price replay, deterministic quote totals, unknown benchmark/metrics and legacy result comparison. **Depends** BE-02, BE-05.

### BE-08 · Shared routing planner and production trace · astra

**Goal and owners.** Deliver F02/F03/F04/F16 rules. Own `internal/gateway/routing/{planner,snapshot,trace}/`, `internal/service/{openai_account_scheduler,gateway_scheduling}.go`, `contracts/capabilities/routing/`. Lift eligibility/ranking from existing production scheduler and delegate slot acquisition to execution. Preserve existing affinity and weighted ordering, add policy origin, hard intersection, funding/eligibility and candidate trace.

**API.** Internal `Plan(features,effectivePolicy,snapshot) → DecisionTrace`, shared by production and BE-10. Serialization contract includes `mode,snapshot_id,seed,origins,stages,candidates,selected,uncertainties`. Start with existing frozen price inputs via a snapshot adapter, then bind BE-07 before paid cutover.

**Database.** Expand bounded redacted decision trace/evidence reference storage linked to BE-06. Shadow-compute decisions with fixed snapshots/seeds, compare by scope and action. Contract secondary scheduler policy code after all providers route through the common planner.

**Tests.** Identical snapshot/seed gives same ranking, strict region/retention/price and fallback boundaries, unknown quota, sticky override, low sample TTFT, time-window balance, cost ranking, slot race and first three trace reasons. Count simulation attempts, leases, secret reads and charges as zero. **Depends** BE-01, BE-03, BE-05, BE-06.

### BE-09 · Restore operation identity and fencing · astra

**Goal and owners.** Deliver F12. Own `internal/modules/ops/restore/`, `internal/control/restore/`, `internal/service/backup_service.go` and matching restore-specific handler adapters. Use an operation journal outside the target being restored, lease generation/CAS, source digest, pre-restore snapshot, isolated rehearsal and post-restore verification.

**API.** `POST A/restores/previews → {id,verified,impact,expires_at}`, `POST A/restores → {id,generation,state,stage,target,report}`, `GET A/restores/{id}`. High-risk production execution uses explicit grant and existing step-up mechanism.

**Database.** Expand independent restore journal. Import legacy backup record links. Old running jobs become review candidates on startup. Contract process-local ownership when all restore initiators use persisted generations.

**Tests.** Two control/worker instances, stale worker finishing after a new generation, crash during restore, journal survival across target replacement, isolated verification failure, no false success. Run rehearsals solely against isolated test data. **Depends** BE-01.

## Group D

### BE-10 · Versioned policy editor, simulation, publish and rollback · astra

**Goal and owners.** Own `internal/gateway/routing/policy/`, `internal/control/routing/`, and policy-specific migration adapters. Implement F02–F04/F16 version editing, dry-run, publication and approved impact. BE-22 owns retirement of the old mixed MCP handler.

**API.** All `W/routing/policies` routes including draft, simulate, publish, version history and rollback. `POST .../simulate → {simulation_id,trace,baseline,diff,expires_at}`; both traces share seed/snapshot. Simulation has a read-only provider graph.

**Database.** Expand immutable policy versions, scope assignment, draft CAS, snapshot/revocation epoch and publication outbox. Migrate group/composite routing intent field-by-field. Existing account preferences require explicit owner mapping. Contract old publication path after parity/rollback drill.

**Tests.** Parent constraints, `inherit|allow_list|deny_all`, CAS failure, concurrent publication, stale simulation, regional fallback enforcement, no side effects for simulations, deterministic explanation and publish propagation. **Depends** BE-01, BE-04, BE-07, BE-08.

### BE-11 · Exact settlement, wallet and evidence assembly · astra

**Goal and owners.** Deliver D04/D05/B04/B05. Own `internal/modules/{billing/settlement,billing/admission,metering,wallet}/`, `internal/service/{gateway_usage_billing,openai_gateway_usage,usage_billing,usage_settlement_receipt,usage_settlement_service,api_key_budget}.go`, `internal/repository/{usage_billing_repo,usage_settlement_receipt_repo,api_key_budget_repo}.go`. Migrate native and OpenAI assemblers into one receipt writer and use BE-07 for price selection.

**API.** No new public write endpoint. Publish `FinancialReader → {billing_source,state,price_version,reserved,platform_charge,receipt_state,settlement_id,ledger_entry_ids}` and `BudgetReader → {rules,blocking_rule_ids}` for BE-14/16. Standard inference retains native wire contracts.

**Database.** Expand versioned receipt/command JSON, funding-account mapping, identifiable reservation/effect, append-only balanced postings and transactional outbox. Reuse 254 settlement and 275 budget storage. Read legacy fingerprints unchanged. Backfill immutable openings at cutoff, compare exact shadow calculations, then switch one writer per cohort with consistent routing. Preserve pending v1 receipts and holds until settled. Contract old float calc paths only after BE-19 covers all writers.

**Tests.** Real PostgreSQL concurrent/admission/commit-loss cases, mixed old/new workers, queue overflow, receipt recovery without upstream access, pre-receipt uncertainty, unknown vs zero, expiring hold and settlement races, sum of signed ledger by currency, no duplicate effect, user-wallet continuity, late charge after transfer of key ownership. **Depends** BE-02, BE-06, BE-07.

### BE-12 · Gateway protocol and provider split · astra

**Goal and owners.** Complete F07/B01/B02 across ingress. Own `internal/gateway/protocol/`, `internal/providers/`, `internal/pkg/apicompat/`, `internal/service/gemini_messages_compat_service.go`, and handed-off BE-03 legacy gateway handler files. Registered provider-specific transports move here via a caller-by-caller ledger. Existing matrix in `docs/gateway-platform-protocol-matrix.md` controls supported combinations.

**API.** Preserve standard Chat, Responses, Messages, Gemini, embedding/rerank, images, existing audio/video and WS/Realtime routes. Internal `ConversionPlan={mode,source_protocol,target_protocol,diagnostics:[{field,reason,severity}]}` feeds new UI diagnostics. Protocol errors stay native.

**Database.** Add conversion diagnostic references to execution record if needed. Migrate one action/provider/account-mode tuple at a time with frozen fixture comparison. Contract old codec bodies per tuple. BE-11's pricing and receipt modules remain the financial owners.

**Tests.** Native and bridge golden matrix, tool deltas, signatures, reasoning, cache, unknown fields, stream ordering/terminal, WS turns, error formats, cancellation, backpressure, bounded size/deadline, partial usage. A stable matrix combination requires successful fixture evidence. **Depends** BE-03, BE-05, BE-06, BE-08.

## Group E

### BE-13 · Authenticated streaming Playground · astra

**Goal and owners.** Deliver F01. Own `internal/control/playground/`, `internal/modules/playground/` and `contracts/protocol/playground/`. Use the production executor under the authenticated project's selected key, with explicit payer/consent. The comparison UI runs independently tracked model calls. Legacy public Playground retirement is handed to BE-22.

**API.** `POST P/playground/runs` with protocol-native input → SSE started/delta/usage/diagnostic/finished/failed. Run summary, original-idempotency lookup, cancel and authorized replay follow the Playground contract. Unknown outcomes link to status.

**Database.** Expand safe run metadata keyed to BE-06 execution, idempotency and output retention choices. Migrate the scoped UI to the new route. Keep raw content ephemeral by default. Contract anonymous forwarding after announced migration.

**Tests.** Auth/key ownership, actual standard executor admission, SSE tool frames, native variants, user stop/disconnect, duplicate submit, same-key conflict, unknown outcome, settled details, per-run comparison cancellation and content redaction. **Depends** BE-01, BE-10, BE-11, BE-12.

### BE-14 · Request list, detail and route explanation · sol

**Goal and owners.** Deliver F02/F10 and U04. Own `internal/modules/ops/requests/`, `internal/control/requests/`. Join BE-06/08/11 facts and existing Ops/usage records. Preserve data completeness for historical rows and redact by permission.

**API.** `GET P/requests`, `GET P/requests/{id} → RequestDetail`, `GET P/requests/{id}/route-explain → {trace,data_completeness}`. Detail exposes timeline, attempts, acceptance, source/price/receipt, diagnostics and actions. `POST P/requests/{id}/exports` returns a scoped, reauthorized metadata export operation. BE-13 owns replay so its cost confirmation and new request identity share the same submission path.

**Database.** Expand scoped lookup/seek indexes and projection checkpoints. Migrate legacy request/attempt/receipt correlations only when identities match. Contract duplicate UI queries when v1 detail projection uses the same reader.

**Tests.** Multi-attempt single request, interrupted/failed/unknown paths, pre-receipt gap, legacy partial records, finance-only scope, connection identity masking, pagination and cross-project lookup/export. **Depends** BE-06, BE-08, BE-11, BE-12.

### BE-15 · Fenced task and artifact center for images · astra

**Goal and owners.** Deliver B07/F06 core. Own `internal/modules/tasks/`, `internal/control/tasks/`, `internal/service/{upstream_job_service,image_task,batch_image_worker}.go`. Introduce one metadata/transition contract for image generation and batch children. Keep provider acceptance, artifacts, batch outcome and money status separate.

**API.** `GET/POST P/tasks`, detail, items, cancel, retry, download ticket and content endpoints under “Task center and artifacts”. `POST .../retry → {task_id,parent_task_id,new_request_id,state}`. Unsupported cancel and unknown submission are explained.

**Database.** Expand scoped task identity mapping to existing upstream/batch records, fencing generation, artifact relation and retention policy. Migrate old jobs/item identities and pending leases; retain original owner/funding mappings. Contract redundant transition paths after worker drain.

**Tests.** Duplicate submission, partial batch success, lost lease, stale worker, unknown progress, canceled provider work, paid retry, artifact expiry/revocation, low-balance result access and request/settlement links. **Depends** BE-04, BE-06, BE-11, BE-12.

## Group F

### BE-16 · Cost, quota and reconciliation workbench · astra

**Goal and owners.** Deliver U05/F10. Own `internal/modules/billing/{readmodel,workbench}/`, `internal/control/billing/`. Read the same exact receipt/ledger/entitlement sources for chart, detail and export. Explain every applicable hard/soft rule with owner, reset, used/reserved/available and adjustment grant.

**API.** All cost-and-quota contract routes including activity, charges, quotas, budget update, ledger, exports, reconciliation and adjustments. Preserve existing payment-provider refund action for payment rail refunds.

**Database.** Expand currency/scope/time seek indexes, resumable read-model checkpoints, reconciliation cases and immutable adjustment effect references. Backfill source-linked rows. Preserve historical estimated/unknown state. Contract duplicate financial aggregators after totals match.

**Tests.** DST/reset, half-open filters, shared quota counted once, wrong-scoped export, soft/hard concurrent bounds, mixed currencies, null vs zero, reconciliation and append-only adjustment, immutable original paid charge. **Depends** BE-11, BE-14.

### BE-17 · Tenant BYOK pilot and funding fallback · astra

**Goal and owners.** Deliver F15 P1 with ordinary verified provider API keys and an explicit pilot allowlist. Own `internal/modules/credentials/byok/`, `internal/control/byok/`, and named BYOK adapters under `internal/gateway/execution/byok/`. Extend existing eligibility/settlement through versioned ports.

**API.** `POST/GET W/byok`, `verify|rotate|revoke`, scope and funding projection in RequestDetail/EffectiveQuota. Inputs include project/member/key/model grants, explicit platform fallback and external-cost budget treatment.

**Database.** Expand workspace ownership and grant mappings, approved fallback plan, version/rotation and source-specific cost evidence. Migrate only pilot-created credentials. Existing platform OAuth accounts keep their own ownership and authorization. Contract pilot feature guard after BE-28 rollout.

**Tests.** Cross-tenant/key/member denial, forced revocation, unknown external expense, fallback refused without funds/consent, pre-dispatch reservation and frozen price for platform attempt, separate service fee and external estimate. **Depends** BE-04, BE-08, BE-10, BE-11.

### BE-18 · Public service status and incident lifecycle · sol

**Goal and owners.** Deliver F17 status. Own `internal/modules/ops/status/`, `internal/control/status/`. Reuse Ops collector/rollup data in publication-safe components. Distinguish gateway faults, upstream errors, stream interruption and admission refusal. Record low sample/stale states.

**API.** `GET U/status`, `GET U/status/incidents[/id]`, admin create/update incidents. Public rows include sample/window/freshness and measurement definition.

**Database.** Expand published incident/update tables and bounded public status rollup. Migrate known verified event/metric sources. Contract independent hardcoded status sources after publication checks.

**Tests.** Zero/low sample, stale last success, logical vs attempt count, upstream 429 vs gateway error, privacy review of serialized public payload, cache validators and incident update history. **Depends** BE-06, BE-14.

## Group G

### BE-19 · Payments, entitlements and remaining financial writers · astra

**Goal and owners.** Own `internal/modules/{payments,subscriptions}/`, `internal/payment/`, `internal/service/{payment_order,payment_service,subscription_service,user_subscription,affiliate_service}.go`, and wallet extension files under `internal/modules/wallet/{recharge,redeem,affiliate}/`. Convert remaining monetary accesses in admin mutation, redemption, promotional/affiliate transfers, subscription renewal and payment settlement to typed commands. BE-11 hands over the funding transaction interfaces. Keep the existing currency-aware minor-unit and fee rules.

**API.** Existing `/api/v1/payment/{orders,plans,webhook/...}`, subscriptions, redeem, affiliate and admin payment routes retain current response contracts and callback URLs. New v2 finance readers expose `{billing_source,entitlement_consumption,platform_charge}` through BE-14/16. Provider callbacks verify signature, merchant, amount, currency and event identity before effects.

**Database.** Expand exact source/provider minor amounts, immutable order-event state, entitlement/funding references, adjustment link and effective fee rule. Migrate historical order/subscription/affiliate identity and monetary values per currency. Coalesce duplicate notifications through a unique provider event/effect. Contract direct balance writers and transitional float SQL scans after all routes pass.

**Tests.** Real PG duplicate/out-of-order callback, webhook amount/currency mismatch, payment retry, chargeback/refund, repeated redemption/affiliate transfer, step-up and delegated DTO masking, zero-/two-/three-digit currencies, exact opening balances. **Depends** BE-02, BE-11, BE-16.

### BE-20 · Independent plugin SDK and host capability enforcement · astra

**Goal and owners.** Complete F13 P1 with independence-branch handoff. Own `pkg/pluginapi/v2/`, `internal/modules/extensions/`, and any versioned host extensions assigned after comparison with that branch. Keep the verified archive/manifest/original-signature chain and owner mapping. Preserve existing OpenAI OAuth transport capability and host-enforced policy, budget and metering.

**API.** `GET A/extensions → {items:[{id,sdk_version,publisher_id,capabilities,state,digest}]}`. Existing admin install/inspect/enable paths wrap the v2 host after signature and capability checks. Operator-visible rejection includes safe reason and compatibility information.

**Database.** Expand artifact v2 digest, publisher trust metadata, v1/v2 migration mapping and granted capability records only where the independence branch has no equivalent. Migrate verified/rebuilt/resigned plugins; ambiguous publisher or host-version mapping enters disabled review. Contract old protocol runtime after deployed inventory and compatibility window support retirement.

**Tests.** Wrong product, unsupported version/capability, invalid signature, key rotation, old-manifest byte verification, trust-root bootstrap, crash/timeout/resource bound, disable during in-flight requests. **Depends** independence plugin/identity merge, BE-01, BE-12.

### BE-21 · Scoped audit, retention, controlled egress and observability · astra

**Goal and owners.** Own `internal/modules/audit/`, `internal/modules/ops/{metrics,alerts}/`, `internal/platform/{egress,telemetry,objects}/`, and audit-specific transport adapters. Consolidate existing audit/metrics and network utilities through ports consumed by connections, providers, artifacts and plugins. Cover proxy/redirect/DNS revalidation, local response-cache isolation, trace redaction and bounded metric cardinality.

**API.** Existing scoped audit, monitor and alert endpoints retain wire contracts. Published detail/action forms reference `{audit_event_id,actor_id,resource,action,at}`. Ops read roles receive redacted views. Public status stays BE-18. Export jobs recheck grants at enqueue, execution and download.

**Database.** Expand audit project/actor/resource/effect links, separate sensitive-content TTL and safe trace retention. Backfill only provable scope from legacy metadata. Contract duplicate unscoped logging/export readers after custody and deletion verification.

**Tests.** Direct cross-scope export/artifact/read; credential/prompt/URL/token redaction; cache after revoke and workspace switch; proxy and redirect policy using controlled local fixtures; tracing without sensitive baggage; bounded queues; admin/finance roles do not receive content through views. **Depends** BE-01, BE-04, BE-14, BE-15, BE-18.

## Group H

### BE-22 · Runtime modes, startup/shutdown and retained-function migration · astra

**Goal and owners.** Coordinator hands over composition during this package. Own `cmd/omnihive/`, final aggregate Wire/router source, `internal/server/routes/` registration and `internal/handler/mcp_grant_handler.go` retirement. Add all/gateway/control/worker modes, status/readiness for each, fenced outbox work and graceful HTTP/SSE/WS/job/receipt drain. Migrate retained legacy user/admin actions through current authoritative modules. Reconcile every old route against a named owner, feature gate, legacy deep link and regression fixture.

**API.** Preserve standard gateway routes, existing identity/TOTP/Passkey, users, accounts/proxies/groups, keys, subscription/payment/redeem/promotion/affiliate, images, audit/backup/plugins/update APIs. New v2 page routes register through module-owned `Register`. Existing health/readiness routes expose only intended diagnostics. Verify public/control/gateway route matrix under each mode. Route seeded grants/audit/rankings to actual sources or hide/disable their production presentation. Retire operator-credential Playground after its announced compatibility transition.

**Database.** Reuse module outboxes and leases. Migrate worker startup ownership to capability-specific persisted claims. Contract in-process-only job transfer and old policy/usage decisions after mixed-version drains. Shared coordination keys across versions remain until all old processes exit.

**Tests.** Combined and split-process smoke from all entry points, process death, restart and drain active streams/workers, cross-mode publication and revocation, duplicate worker financial effects, retained feature matrix with real data, no seeded business state exposed. **Depends** BE-09, BE-13 through BE-21 and independence runtime identity.

### BE-23 · Migration inventory, cohort switching and rollback · astra

**Goal and owners.** Own `cmd/omnihivectl/refactor/`, `internal/modules/migration/`, `docs/migrations/backend-v2/` and `contracts/migrations/BE-23/`. Integrate with the independence CLI through the coordinator. Produce redacted instance inventory, repeatable checkpoints, mapping exceptions, financial and resource reconciliation, per-component cutover state and recorded rollback floor.

**API.** CLI contract: `inventory --format=json`, `migrate --module --workspace --resume`, `reconcile --currency --cutoff`, `cutover --module --workspace --expected-version`, `status --format=json`. Shape `{run_id,module,checkpoint,counts,unmapped,unexplained_differences,rollback_floor,eligible_for_switch}`. Administrative preview uses the same service.

**Database.** Expand migration journal/checkpoints and single-authority cohort state. Inventory users/keys/projects, group/composite maps, credentials/keys/plugins, wallets/holds/receipts/orders/subscriptions, tasks/artifacts, config/volumes and audit. Verify physical volume and key continuity. Migrate in resumable batches, compare by scope/funder/currency, stop unexplained deltas. Contract tracked here for BE-30 after observation.

**Tests.** Previous-version snapshot upgrade, restart at checkpoints, key decryptability, unresolved mapping quarantine, wallet/opening balance, v1/v2 pending receipt replay, no double reserve/debit, in-flight task fencing, operational rollback preserving newly posted transactions. **Depends** all schema-producing P1 packages, independently prep inventory with BE-22.

### BE-24 · Full-scope release verification and cutover gate · astra

**Goal and owners.** Own `contracts/release/backend/` and `docs/runbooks/backend-v2/`. Execute six journeys, complete protocol/account/action matrix, financial failure injection, access isolation, migration/restore and load baselines. Capture measured environments and results. Route implementation fixes to each owning package.

**API.** Freeze composed v2 OpenAPI and compare actual HTTP payloads and generated TS contract with the fixtures. Test existing public inference clients and payment callbacks through original paths. Include public and scoped catalog, model compare, Playground, wizard, simulator, request detail, costs/quotas, task center and status entry points.

**Database.** Zero feature DDL. Exercise mixed-version migration, isolated restore, reconciliation and rollback with newly created transactions. Require stable unique financial effect and traceable unknown state. Record cohort stop/revert indicators for unauthorized routing, unexplained monetary deltas, duplicated effect, unrecoverable durable work or protocol divergence.

**Tests.** All P0/P1 invariants, deterministic 2-hour load with cancellation/slow streams, baseline same machine/P95 overhead target within 10%, resource return to baseline, revoke propagation target ≤60 s, isolated RPO/RTO measurement and pre/post-cutover reconciliation. Stage internal → 1/5/20/50/100% when evidence supports expansion. Targets are gates to measure and calibrate with deployment evidence. **Depends** final run on BE-22/23, independence release verification; fixture preparation can start earlier.

## Groups I and J

### BE-25 · Verified audio/video task adapters · astra

**Goal and owners.** Complete F06 P2 under `internal/modules/tasks/media/` and `internal/providers/mediajobs/`. Adapt existing verified audio/video actions, formats, task ownership, result and price units through the Task API.

**API.** `POST P/tasks` accepts enabled `kind:"audio"|"video"`; list/item/cancel/retry/artifact and RequestDetail shapes stay stable. Capabilities declare accepted format, duration and cancel support.

**Database.** Expand typed media attributes, result metadata and lifecycle mappings. Migrate existing provider job/result identities. Contract per-kind duplicate state projections after fixtures pass.

**Tests.** Provider acceptance ambiguity, cancellation, size/duration limits, audio/video unit pricing, expired result, preexisting job read. **Depends** BE-15, BE-24.

### BE-26 · Provider/task plugin actions · astra

**Goal and owners.** Complete F13 P2 under `pkg/pluginapi/v2/actions/` and `internal/modules/extensions/actions/`. Add opt-in typed provider and task capabilities to the verified host, with network, resource, credential and usage boundaries.

**API.** `GET A/extensions` adds authorized capability labels. Action availability flows into catalog and standard task/inference contracts. No new execution surface for arbitrary code in the console.

**Database.** Expand granular grants/version requirements and action-specific verification. Migrate only explicitly approved extensions. Contract provisional capabilities after maturity evidence.

**Tests.** Host authorization/egress/charging parity, plugin failure/timeout, unused capabilities never gain grants, version downgrade/rollback. **Depends** BE-20, BE-24.

### BE-27 · Catalog verification automation and compatibility lifecycle · sol

**Goal and owners.** Complete F14 P2 under `internal/modules/catalog/verification/`, `internal/control/catalog/verification/`. Collect reviewed provider change evidence, flag stale items, queue authorized verification and transition candidate → experimental → stable → deprecated → closed.

**API.** `POST A/catalog/verifications → Operation`; result includes evidence ID, observed/expiry timestamps and verified matrix tuple. Public ModelDetail receives evidence/maturity after publication.

**Database.** Expand verification schedule, provenance, owner, observed fixture/adapter version and deprecation records. Migrate existing recent/known source records. Contract manual-only status derivation while preserving explicit price/access publication.

**Tests.** Failed probe keeps last successful snapshot, experimental sample isolation, no implicit grants/price changes, bounded verification queue and publication rollback. **Depends** BE-05, BE-12, BE-24.

### BE-28 · Complete BYOK self-service · astra

**Goal and owners.** Expand F15 to verified provider classes under `internal/modules/credentials/byok/`, handed off from BE-17. Supply member/service-account/key grants, lifecycle, explicit funding and staged provider onboarding.

**API.** Existing `W/byok` contract plus `PATCH W/byok/{id}` for authorized grant/fallback fields using If-Match, and `GET W/byok/{id}/events` with credential metadata-only lifecycle entries. Return the safe BYOK object and scoped event list.

**Database.** Expand granular service-account/entitlement rules, migrate pilot grants after authorization review, contract pilot storage/flags when equivalent policies cover active tenants.

**Tests.** Full eligibility matrix per supported auth mode/provider, member exit and revoke propagation, old version drain, external estimate vs service fee, prevented unauthorized fallback and direct cross-tenant use. **Depends** BE-17, BE-24.

### BE-29 · Tenant-approved telemetry exports · sol

**Goal and owners.** Own `internal/modules/ops/export/`, `internal/control/observability/`. Deliver P2 scoped asynchronous destinations with bounded sampling, retry, approved data classes and controlled egress.

**API.** `POST W/observability/exports → {id,state,version,destination_id}` with explicit data classes, sampling ratio and retention. `GET W/observability/exports/{id}` returns that object plus `{last_success_at,last_error_reason,queue_depth}`. Disable/reconfigure requires a current version and export-management grant.

**Database.** Expand destination policy, scoped queue/cursor, delivery dedup and retention metadata. Migrate only explicitly configured destinations. Contract transitional destination configuration after verified delivery and purge.

**Tests.** Exporter outage/backpressure, sampling visibility, secret/prompt/error metadata redaction, destination revocation, bounded retries and isolation from financial outbox/receipt processing. **Depends** BE-21, BE-24.

### BE-30 · Compatibility contract retirement · astra

**Goal and owners.** Own `internal/legacy/retirement/`, `contracts/migrations/BE-30/`, and coordinator-approved individual legacy removals. Track deployed versions and remaining caller counts for every wrapper, old schema, old runtime identity and plugin/credential migration path.

**API.** Retire only documented v1 control projections with replacement routes, a published window and zero remaining supported callers. Native inference, historical financial/callback identity and retained user-facing operations continue through the canonical owners.

**Database.** Add contract migrations only after validated backups, no old readers/leases/receipts relying on the old format and representative restores. Preserve immutable history/fingerprints and provenance. Record minimum compatible binary/schema, per-domain reconciliation and rollback policy.

**Tests.** Current supported upgrade/recovery matrix, retire/restart/rollback checks, historical invoice and receipt lookup, orphan/read fallback scan, fixture proof for each survivor and retired API. **Depends** BE-23/24 and the actual deployment inventory/window. P2 storage cleanup waits for the individual P2 producer's successful migration.

## Acceptance map

| Plan requirement | Implementation owners | Observable result |
| --- | --- | --- |
| F01, F07, B01–B03, B08 | BE-01/03/06/12/13 | Real scoped API request/stream, correct native terminal and one request detail |
| F02–F04, F16 | BE-01/08/10/14/17 | Identical planning semantics in production and simulation, enforceable published policy and visible decision reasons |
| F05, F14, F17 model discovery | BE-05/07/18/27 | Shared public/scoped catalog, reproducible comparison, freshness and measured performance |
| F06, B07 | BE-15/25 | Task ownership, partial result, artifact access, cancellation/retry and settlement |
| F08 | BE-01/14/16/21 | Project-aware API, async, export, cache and artifact authorization |
| F09, F11, F15 | BE-04/17/28 | Real connection wizard, rotation/draining, explicit BYOK ownership and funding |
| F10, D04/D05, B04/B05 | BE-02/06/07/11/14/16/19 | Logical request, attempt, frozen price, receipt/ledger, uncertainty and reconciliation in one drilldown |
| F12, F13 | BE-09/20/26 | Fenced, rehearsed restore and verified versioned extension host |
| B06, F17 public facts | BE-05/18/22 | Evidence provenance and public status with samples, incidents and unknown states |
| P0 release, P1 completeness, P2 exports | Independence workstream + BE-21–24/29/30 | Own signed assets, supported migration/rollback, full retained route matrix and release evidence |
