# Backend target architecture

This design implements the charter and plan sections 6–9, 12–14. `CURRENT_STATE.md` supplies the source baseline. `API_CONTRACTS.md` defines the frontend wire contract. `WORK_PACKAGES.md` assigns the migration and implementation owners.

## Runtime and composition

Keep one Go module, PostgreSQL and Redis. The main binary supports `all`, `gateway`, `control`, and `worker` modes. `all` starts all components through the same constructors. `gateway` serves standard inference and published policy snapshots. `control` serves console/public APIs. `worker` claims persistent jobs, receipts and outbox records. An accepted durable job remains recoverable across process restarts.

Use the current Ent client and SQL transaction infrastructure during extraction. PostgreSQL owns financial facts, request admission, policy publication, credential metadata and durable job identity. Redis owns bounded caches, cancellation notification, affinity and distributed coordination whose durable recovery is described by its owner. Object storage owns authorized artifacts and backups.

Published policy/catalog snapshots include `version`, `published_at`, `expires_at`, and `revocation_epoch`. Publication emits a transactional outbox event. Gateway caches poll invalidation at most every five seconds and reject new admissions when the access snapshot exceeds 60 seconds of allowed staleness. High-risk control mutations read current authorization synchronously. Credential revocation is checked immediately before dispatch. Existing streams hold their admitted pricing version and receive explicit cancellation when required by a revocation action.

## Module ownership

| Module | Authoritative responsibility | Public contract | Existing source to absorb |
| --- | --- | --- | --- |
| `modules/identity` | Users, console authentication, API keys, workspace/project ownership, roles and actions | `Authenticator`, `AccessEvaluator`, `ScopeResolver` | Auth/key/workspace/passkey services and middleware |
| `modules/catalog` | Model identities, aliases, endpoint capabilities, evidence and lifecycle | `CatalogReader`, `CapabilityResolver`, `CatalogPublisher` | ModelRegistry, VisibleModelResolver, metadata and plaza projection |
| `modules/pricing` | Sale-price versions, upstream rates, eligibility and one pricing resolver | `PriceResolver`, `QuoteCalculator` | ModelPricingResolver, BillingService price calculations, channel/group price selection |
| `modules/credentials` | Secret references/versions, scope, refresh, rotation, revocation | `CredentialResolver`, `CredentialManager` | CredentialSecretService, refreshers and account-secret persistence |
| `modules/connections` | Provider schema, endpoint/network configuration, discovery, pools and account eligibility | `ConnectionReader`, `ConnectionVerifier`, `PoolReader` | Account/group/proxy configuration and account test/discovery orchestration |
| `gateway/routing` | Effective policy, hard filtering, ordering, affinity decision, simulation | `Planner`, `PolicyStore`, `SnapshotReader` | OpenAI scheduler, gateway scheduling and composite routing |
| `gateway/execution` | Admission, logical request, attempt control, response lifecycle, cancellation and cleanup | `Executor`, `AttemptController`, `ExecutionStore` | GatewayRequestPlan, AttemptBudget, failover loop and handler orchestration |
| `gateway/protocol` | Ingress codecs, native error/terminal encoding, conversion plans and event semantics | `IngressCodec`, `EventSink`, `ConversionPlanner` | Gateway handler parsing, apicompat and protocol-specific stream handling |
| `providers/<provider>` | Authorized outbound actions and usage extraction | `TextAdapter`, `JobAdapter`, `RealtimeAdapter`, `DiscoveryAdapter` | Provider service/client implementations |
| `modules/metering` | Normalized usage evidence, immutable receipt assembly and evidence reconciliation | `EvidenceAssembler`, `ReceiptSink` | Common/OpenAI usage assemblers and usage evidence |
| `modules/billing` | Persistent reservations, settlement identity, ledger transaction and reconciliation | `Admission`, `Settlement`, `FinancialReader` | UsageSettlementService, usage billing and key budgets |
| `modules/wallet` | Funding-account mapping to user wallet, transfers, recharge and adjustments | `FundingResolver`, `WalletReader`, `WalletCommands` | User balance, redemption, affiliate and adjustment logic |
| `modules/subscriptions` | Plans, entitlements, consumption windows and renewals | `EntitlementResolver`, `SubscriptionCommands` | Subscription services and group entitlements |
| `modules/payments` | Orders, payment events, verified fulfillment and refunds | `OrderCommands`, `PaymentEventHandler` | Existing payment service and `internal/payment` adapters |
| `modules/tasks` | Durable job identity, fenced ownership, task queries, retry/cancel and artifact access | `TaskCommands`, `TaskReader`, `ArtifactAuthorizer` | UpstreamJob, ImageTask, BatchImageWorker |
| `modules/ops` | Request-detail projection, aggregate health, metrics, incidents, alerts, backup/restore | `RequestReader`, `HealthReader`, `RestoreCommands` | Ops, usage views, backup service, audit projections |
| `modules/audit` | Append-only action evidence, access-controlled exports and retention | `AuditSink`, `AuditReader` | Existing audit/security audit implementations |
| `modules/extensions` | Plugin v2 lifecycle, capability checks and host integration | Versioned plugin host contracts | Existing plugin manager/package/manifest/runtime |
| `control` | Console/public transport, DTO projections, action authorization and route binding | HTTP v2 | New page handlers and legacy management adapters |
| `platform` | SQL/Redis adapters, clocks, IDs, secret encryption, egress, telemetry, object store | Infrastructure implementations | Current repository/pkg implementations and live attestation |

### Layout

```text
backend/
  cmd/omnihive/                 # composition and run modes
  cmd/omnihivectl/              # inventory, migration, reconciliation, diagnostics
  api/v2/                      # OpenAPI source and generated transport types
  internal/
    core/{money,scope,fault}/   # small immutable shared value types
    modules/<domain>/
      model.go                 # domain values and invariants
      ports.go                 # dependencies owned by the consuming application
      service.go               # application operations
      postgres/                # module persistence adapters
      redis/                   # module coordination adapters where required
    gateway/{execution,protocol,routing}/
    providers/<provider>/
    control/{identity,catalog,connections,routing,playground,requests,billing,tasks,status}/
    platform/{database,redis,clock,secrets,egress,telemetry,objects}/
    legacy/                    # bounded compatibility adapters with caller manifest
  pkg/pluginapi/v2/
contracts/{protocol,capabilities,api,migrations}/
```

During extraction, keep legacy files in their current package until all callers move. A move is complete when normal HTTP/worker entry points call the module authority and obsolete policy implementations are removed. Generated output is rebuilt centrally after source schema and Wire changes merge.

## Dependency rules

1. `core` imports the standard library and the pinned decimal library. It contains values and validation.
2. Module domain values import `core`. Application services import their own domain and consumer-owned ports. Persistence adapters import their owning module, `core`, Ent/SQL/Redis and platform utilities.
3. `pricing` consumes catalog snapshots through its port. `billing` consumes pricing, funding and entitlement contracts. `metering` consumes immutable pricing and receipt contracts. Providers supply evidence through an event/result contract.
4. Routing consumes authorized scope, catalog, price, account eligibility and health snapshots. Its planner is a pure function. Snapshot loading, slot acquisition, budget mutation and secret resolution are separate operations owned by execution or their domain services.
5. Execution orchestrates identity, routing, credential resolution, adapters, admission and receipt submission. Providers receive only the already-authorized action, connection, ephemeral credential and provider request. Provider packages may import protocol values, `core` and platform HTTP utilities.
6. Payment fulfillment calls wallet/subscription application commands inside the financial unit of work or emits an outbox event with idempotent effect identity. Financial SQL remains owned by billing/wallet transaction adapters.
7. Task execution calls the same admission, execution and settlement contracts used by synchronous inference. Ops projections read domain query ports. Detail assembly performs projection and redaction.
8. HTTP DTOs remain in `control`/`api`. Modules accept typed commands. Gin, HTTP response writers and generated API DTOs stay in transport code.
9. Only the composition root imports all concrete implementations. The migration exception is a named adapter importing `service`/`repository`, with an explicit caller list and deletion package in `WORK_PACKAGES.md`.
10. A dependency test checks package imports, provider-to-finance access, Gin in domain packages and legacy adapter references. Cycle detection runs with `go list` and compilation.

## Core contracts

The signatures below express contracts. Concrete transport/body types remain protocol-specific where semantics require it.

```go
type AccessEvaluator interface {
    Evaluate(context.Context, Principal, ResourceAction) (AccessDecision, error)
}

type CapabilityResolver interface {
    Resolve(CatalogSnapshot, AuthorizedScope, RequestFeatures) (EffectiveCapability, error)
}

type Planner interface {
    Plan(RequestFeatures, EffectivePolicy, CandidateSnapshot) (DecisionTrace, error)
}

type CredentialResolver interface {
    Resolve(context.Context, AuthorizedCredentialRef, Version) (CredentialLease, error)
}

type Admission interface {
    Reserve(context.Context, AdmissionCommand) (AdmissionReceipt, error)
    Extend(context.Context, ReservationExtension) (AdmissionReceipt, error)
    Release(context.Context, ReservationIdentity) error
}

type Settlement interface {
    Persist(context.Context, FrozenReceipt) (ReceiptAcknowledgement, error)
    Settle(context.Context, SettlementIdentity) (SettlementResult, error)
}

type Executor interface {
    Execute(context.Context, AuthorizedRequest, EventSink) (ExecutionResult, error)
}

type IngressCodec interface {
    Decode(context.Context, IngressRequest) (DecodedRequest, error)
    NewSink(IngressResponse) EventSink
}

type TextAdapter interface {
    Execute(context.Context, Dispatch, ProviderRequest, EventSink) (ProviderResult, error)
}

type JobAdapter interface {
    Submit(context.Context, Dispatch, JobSubmission) (ProviderJob, error)
    Poll(context.Context, Dispatch, ProviderJobIdentity) (ProviderJob, error)
}

type CancelableJobAdapter interface {
    Cancel(context.Context, Dispatch, ProviderJobIdentity) (CancellationResult, error)
}
```

`AuthorizedScope` freezes workspace, project, API key, acting member, key owner, funding account and permission epoch. `RequestFeatures` carries model/action/protocol, required capabilities, token/media bounds, retention/region constraints, affinity hash and deadline. Secret bytes and prompt text have dedicated transient objects with explicit ownership and redaction.

`CredentialLease` includes credential version and a release operation. Rotation creates and verifies a version, switches new requests, drains leases, then revokes the predecessor. Forced revocation explicitly cancels affected active work. Refresh compares the expected version and lease fencing token before updating.

`EventSink` records transport commitment, semantic-output commitment and terminal status. Heartbeats can commit headers while leaving generation replay rules dependent on semantic commitment and upstream acceptance. Terminal emission is once-only. WS connections own sessions, with a separate request/receipt identity per logical turn.

`AttemptController` is built around existing AttemptBudget and FailoverState behavior. It records deadline, total attempts, same-account attempts, exclusion reasons, backoff and upstream acceptance (`not_sent`, `accepted`, `unknown`). A new generation after uncertain acceptance normally requires provider idempotency or a reconciled submission result. Native Responses WS passthrough and SSE may replay once on a different account within the original budget after a silent first-output timeout closes or cancels the prior upstream with zero upstream semantic bytes and zero downstream bytes, recording `first_output_timeout_abandoned` and `abandoned_uncertain` with unknown acceptance and zero charge or receipt for the abandoned attempt. Protocol encoders retain native terminal and error forms.

## Exact money and price design

### Representation

Use the existing pinned `shopspring/decimal` library behind `internal/core/money`. Define `Amount` as an immutable finite base-ten decimal, `Money` as `Currency + Amount`, `Rate` as `Currency + Amount + Unit + UnitQuantity`, and `Quantity` as a typed decimal or integer according to its unit. Public fields use canonical decimal strings. New rates allow 20 integer digits and 18 fractional digits, settled values allow 20 integer digits and eight fractional digits, and finite quantities allow up to 20 integer digits and ten fractional digits. Reject input that exceeds these bounds before multiplication. Enforce legacy column limits at the compatibility write boundary and return a typed overflow fault before committing.

`Money` represents exact monetary values at each documented scale. Ledger postings and wallet deltas use eight fractional digits. Price rates allow eighteen fractional digits. Intermediate calculations keep exact multiply/add operations. Convert each rate to its numerator, decimal scale and integer unit denominator. For repeating quotients such as per-hour rates multiplied by seconds, accumulate integer ratios using Go’s standard `math/big.Rat` until the final rounding boundary. Render intermediate diagnostic decimals to 18 places, retaining the source numerator/denominator for replay. Never use the decimal library’s process-wide division precision for charge decisions. `RoundCharge(8)` is the only charge rounding boundary. `Rate`, `Multiplier` and `Quantity` cannot be supplied where a settled wallet amount is required.

The monetary API exposes `Parse`, `Currency`, `String`, `Add`, `Sub`, `Compare`, `RoundCharge`, `ReserveCeil`, SQL `Scan/Value`, and JSON marshal/unmarshal. Operations validate matching currency. JSON accepts strings with optional leading minus for signed adjustments, a decimal point and digits. New APIs reject numeric JSON, exponential notation, NaN and infinity. Negative prices/usage are rejected by their domain constructor. Signed ledger entries preserve adjustment/refund direction.

### Rounding and calculation order

| Operation | Rule |
| --- | --- |
| Rate × quantity | Exact decimal multiplication, unit conversion, then frozen multipliers and tier rules |
| Line amounts | Retain high precision for audit, including cache/reasoning/media dimensions |
| Final customer charge | Sum applicable exact components, apply the frozen price-plan rules, round once to eight places using half-away-from-zero |
| Reservation | Ceil positive upper-bound cost to eight places |
| Wallet/key/subscription financial effect | Use the identical eight-place charged amount when the entitlement rules charge the same basis |
| Display allocation | Retain `raw_amount` and a single explicit `rounding_adjustment` so displayed components reconcile to charged total |
| Refund/adjustment | Reverse the original posted amount or append an explicitly authorized adjustment, retaining original currency/identity |
| Payment-provider amount | Quantize once to the provider/currency minor-unit contract, persist the original exact order amount and the submitted minor-unit amount |
| Provider fee | Preserve the existing fee-round-up behavior in `payment/fee.go` as a named versioned fee rule |
| FX conversion | Freeze source, rate, time and target currency in the order, with explicit rounding before provider submission |

Example under the current billing rule is `(10 × 0.00000125 + 5 × 0.00001000) × 1.25 = 0.000078125`, producing charge `0.00007813`. The wallet debit and quota increment both use `0.00007813`. This rule matches the existing quantization contract in `service/usage_billing.go`.

### PostgreSQL and cache strategy

- Preserve current exact numeric columns and their values during expansion. Use decimal/string SQL scanning at the new boundary. Replace Ent `field.Float` financial representations using a custom decimal value/scanner as each domain migrates, followed by centralized code generation.
- New ledger postings, reservations and funding balances use `NUMERIC(28,8)`. New rate columns use `NUMERIC(38,18)`. Multipliers use `NUMERIC(20,10)`. Currency uses a checked three-letter code. Existing ten-place usage/component values retain their original precision and provenance.
- Keep historical `(20,8)`, `(20,10)` and `(20,2)` columns until their readers have migrated. Add companion exact columns only for columns whose current representation or range requires it. Payment values retain the original provider amount/currency, including zero-, two- and three-decimal currency rules.
- Version JSON pricing and receipt payloads with `schema_version` and `money_encoding`. Version Redis values and share one authoritative coordination namespace during mixed-version rollout. PostgreSQL row locks or conditional SQL updates own every hard financial reservation. Redis may expose an advisory precheck or a cached balance. A denial based only on a stale monetary cache is rechecked against PostgreSQL; a cache miss always falls back to the authoritative store.
- Read historical numeric text directly from PostgreSQL. Decode legacy JSON numbers with `json.Number` and versioned historical rules. Keep each persisted fingerprint. A reconstructed decimal receipt never replaces a legacy fingerprint.
- Backfill in bounded primary-key batches with checkpoints and audit totals by currency/funding account. Run one authoritative financial writer and a read-only decimal comparison. Review every discrepancy before switching its cohort. A changed calculation rule receives a new price version and future effective date.

### Transaction and settlement identity

Use a server-generated `request_id`, one `attempt_id` per dispatch, and one `settlement_id` per financial effect. A client correlation ID is a separate field. Business idempotency is scoped by workspace, principal/key, endpoint and key value, with a canonical request fingerprint persisted before execution.

Before dispatch, atomically persist the admitted request, frozen funding/entitlement/price choices, reservation and initial attempt intent. Mark dispatch acceptance with the best available evidence. After execution, persist the complete frozen receipt, then settle using the existing receipt/dedup transaction boundary. Financial entries, entitlement consumption, reservation capture/release and outbox events commit together. Settlement retries use the same identity and stored version.

Reuse `usage_settlement_receipts` as the recovery record and `usage_billing_dedup` as the existing effect guard during migration. Add immutable `ledger_entries` with unique effect identity and balanced debit/credit postings per currency. User wallet balance remains the existing funding source and receives an immutable opening-balance mapping at cutover. Workspace/project budgets constrain and attribute spending. A member/key ownership change leaves the frozen funding identity attached to prior executions.

Execution state and settlement state are independent. If receipt persistence remains unconfirmed, reconciliation inspects the durable execution/attempt record and provider evidence. The reconciliation operation can mark missing usage, attach observed evidence or append an authorized adjustment. It never starts another inference operation. A provider-side result lost before receipt commit remains `unknown` until evidence resolves it.

## Routing policy and explain model

The policy hierarchy is platform → workspace → project → API key → request. Hard constraints intersect. Each allow-list is explicitly `inherit`, `allow_list`, or `deny_all`. An empty `allow_list` is rejected as ambiguous. Preferences use nearest explicit setting within the effective hard constraints. Legacy empty/null representations are interpreted by their versioned adapter.

The planner performs ordered stages for identity/scope, model/action access, region/retention, account authorization, allowed funding plans, health/capacity, affinity and ranking. Model fallback and same-model endpoint fallback are separate permissions. An endpoint requiring platform payment can enter a BYOK fallback only when its frozen funding plan and reservation authorize that source.

`DecisionTrace` contains schema version, mode (`simulation` or `execution`), snapshot ID/time, policy versions and origins, sanitized request features, ordered stages, eligible/excluded candidates, reason codes, score factors, tie-break seed, chosen endpoint, conversion plan and uncertainties. Every factor includes value, unit, observation window, source and freshness. Unknown capacity remains unknown. Strict region/retention constraints require verified endpoint evidence.

Simulation loads an authorized read-only snapshot and calls the same planner used by production. It uses a deterministic seed supplied in the result and pinned snapshot versions. The simulation handler has read-only ports with no credential resolver, HTTP adapter, reservation writer or slot acquisition capability. Production records the same trace plus subsequent slot/dispatch outcomes. A capacity race appears as an execution-stage event.

For weighted policies, simulation reports selected order for the seed and configured distribution. Latency/throughput observations contain sample count and uncertainty. Public projections expose provider endpoint labels only when publication permits it. Console users see opaque candidate IDs unless granted `connections.read`.

## Request detail and error model

`RequestDetail` composes the authoritative execution record, route trace, attempt records, immutable pricing/usage evidence, receipt/ledger status and correlated task/artifact links. Its minimum data contract includes scope, actor/key owner/funder, request/model/protocol/action, client outcome, timing milestones, upstream acceptance, conversion diagnostics, price/evidence version, billed source, reservations and final settlement. Historical requests use `data_completeness` with missing sections and reasons.

The default detail contains metadata. Content is separately authorized with `requests.content.read`, retention policy and audit. Replay uses a separate command, a new request identity and explicit cost acknowledgement. Export applies the same projection policy as on-screen detail. `unknown`, `unavailable`, `redacted` and observed zero remain distinguishable.

Use a domain fault with stable `reason`, category, safe message, retry semantics, operation ID, field violations and references. Categories are `validation`, `authentication`, `authorization`, `conflict`, `budget`, `capacity`, `upstream`, `persistence`, `canceled`, and `internal`. Retry semantics are `safe`, `after_delay`, `query_status`, or `never`. `query_status` handles potentially executed operations.

The v2 HTTP envelope retains `{code,message,reason,metadata,data}` and adds typed fields inside `metadata`. Protocol codecs map the same domain fault to OpenAI, Anthropic, Gemini, SSE or WS formats. Prompts, credentials, internal stack traces and provider raw error bodies are redacted before projection. `API_CONTRACTS.md` gives the exact error JSON and HTTP mappings.

## Jobs, restoration and operations

Each task stores workspace/project, actor/key/funding identities, originating request, parent/item identities, adapter version, provider job ID, idempotency fingerprint, lease owner, generation and timestamps. Transitions use `WHERE id = ? AND generation = ? AND state = ?`. A worker that loses the lease can release local resources and report its observation, while its state update is rejected by the fence.

Separate execution (`queued`, `running`, `succeeded`, `failed`, `canceled`, `unknown`), cancellation (`unsupported`, `requested`, `confirmed`, `too_late`) and settlement states. Store `progress=null` when the provider supplies no measured progress. Retry creates a new execution identity for a safely retryable failed item. Submitted/unknown tasks first query the provider or enter reconciliation.

Restore operations have a durable operation ID, generation, target environment, verified source digest, pre-restore snapshot, validation report and audit. Use a dedicated control store outside the database being restored, or a durable host journal for single-node mode, so replacing the target database preserves operation ownership and final reporting. Default execution performs an isolated restore rehearsal before the separately authorized production restore action. The production cutover uses the same fenced operation identity.

Ops reuses existing metrics collectors and rollups. Logical requests and attempts have separate counters. Public status publishes component health, scoped aggregate inference outcomes, sample count/window, stale/unknown state and incident history. Tenant, key, connection identity and capacity internals remain under authorized console projections. Status distinguishes gateway faults, upstream failures, stream interruptions and admission refusals in its measurement definitions.

## Migration and completion gates

Every package records source callers, target authority, compatibility adapter, backfill checkpoint, cutover flag and deletion condition. Immutable migrations remain intact. New migrations expand first, then resumably migrate, switch one authority, and contract after old readers/workers drain.

Acceptance requires existing stable protocol fixtures, exact-money and historical-receipt fixtures, all-scope authorization, cancellation/stream terminal tests, concurrent idempotency, real PostgreSQL/Redis recovery, bounded load and restore evidence. The deployment retains all current user/admin, payment, subscription, redemption, affiliate, proxy, monitoring and identity functions through the module mapping. Full P2 tasks, BYOK, plugin action capability and export automation follow their explicit queue entries.
