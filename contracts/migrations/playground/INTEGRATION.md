# Authenticated Playground

Apply migration 302 after migrations 295, 297, 299, 300 and 301. It adds a run metadata table, a Playground journal origin, a grant for projected live raw events and a cancel fence in the settlement dispatch transaction. The journal and run share the request UUID. The separate run UUID is the UI handle. No prompt, response or native frame is stored in PostgreSQL by this module.

## Production composition

Compose the protected control routes after the identity identity and catalog catalog providers. `v2` is the existing group at `/api/v2`. Use the same initialized, settlement-backed GatewayService and adapter map that serve the standard API.

```go
dispatch := service.NewGatewayActionDispatch(routes.GatewayActionHandlers(handlers))
common := []gin.HandlerFunc{middleware.GatewayProtocol(), handler.InboundEndpointMiddleware()}
if handlers.MCP != nil {
    common = append(common, handlers.MCP.GatewayExtensionMiddleware())
}
native := playground.NativeRoutes(dispatch, compositeResolver, handlers.Gateway.ActionRequirement, common...)
g := &playground.Gateway{
    DB: db, Access: identityStore, Scopes: identityStore,
    Keys: apiKeyService, Subscriptions: subscriptionService,
    Catalog: catalogReader, Policies: policyStore, Plans: gatewayService, Routes: native,
}
h := playground.New(g, cfg.TrustForwardedIPForAPIKeyACL())
playground.Register(v2, h, gin.HandlerFunc(jwtAuth), panelRateLimiter.Global(),
    panelRateLimiter.Heavy(), gin.HandlerFunc(auditLog))
```

Keep body logging disabled on the Playground and private native routes. Audit actor, project, run, admission, outcome and content-access metadata. Runtime owns wiring and split-mode transport of this private execution entry. In a split deployment the gateway process must own admission, dispatch and reservation. Authenticate the internal handoff as well as the end-user session, propagate its cancellation and keep both processes on the same journal. The native router receives its key through server-owned context and must remain private.

The selected key resolves in the active project. For an owned key, consent confirms the owner's funding. A delegated caller needs `keys.act_as` and must name the matching `payer_user_id`. The gateway validates credential status, key/model/action restrictions, IP, group, balance/subscription eligibility and active membership. The policies scenario uses the resolved native model, requested protocol, serialized byte length as input token reservation estimate and enforced native output bound. Candidate capture binds verified connections endpoints from the key's group to their current verification IDs. The selected endpoint is the only permitted upstream for this invocation. A changed policy, verification, scope, cancellation, or a scheduler choice outside that selection closes the send permit. There is no automatic replacement generation.

Wallet remains the settlement authority. `GatewayRequestPlan.ReserveBudget` executes once before the first event. A key with an active period budget records a physical hold. Other keys use the existing wallet/subscription rules. The standard native adapter records usage and its receipt, using the admitted request UUID. Only a persisted receipt establishes settlement. Pricing, charge, quantity evidence and ledger entries are available through requests's permission-projected `detail_url`. The run's `settlement_state` is refreshed from the linked receipt.

## Console integration

- Submit one POST per model to `P/playground/runs` with session credentials and a fresh `Idempotency-Key`. Display each estimate before obtaining cost consent. Each comparison side has its own request, hold, stream and cancel endpoint.
- Supply the scoped catalog model ID, downstream key ID, protocol, `action: "text.chat"`, native `input` and `funding_acknowledged: true`. IDs are JSON strings. Use `options.stream: false` and `Accept: application/json` for a synchronous native output. Streaming uses `Accept: text/event-stream`. The outer request is limited to 256 KiB. The default output bound is 1024 tokens and the maximum is 32768. Each call permits one completion.
- Inspect the actual content type. Successful streaming opens with `started`, then emits `delta` (text, reasoning or tool arguments), `usage`, optional `diagnostic` and a single `finished` or `failed`. Each event has the same `run_id` and `request_id`, a strictly increasing `seq` and matching SSE `id`. Keep tool fragments by index and call ID. Usage quantities are decimal strings, including observed zero. Missing evidence remains absent. `include_raw_events` requires `playground.raw` and adds allowlisted native frames.
- HTTP 200 with JSON may be an existing RunSummary even for a streaming POST. Same key and canonical body return the existing handle. A changed body or replay source returns 409. To reconnect after a lost acknowledgement, GET `P/playground/runs/by-idempotency-key` with the same header, or use the run's status URL. Only the submitting actor can look up or cancel a run.
- Stop via POST `P/playground/runs/{id}/cancel` with `{}`. HTTP 202 acknowledges the stored cancel request. Disconnect also cancels the native context. A different instance observes the cancel within the 250 ms polling interval plus database latency. Cancellation races the dispatch permit inside PostgreSQL. A provider send with uncertain completion is `unknown`. A process lost while running becomes `unknown` by the five-minute deadline. Query the existing run and its receipt after an uncertain outcome.
- Replay through `POST P/requests/{id}/replay` with a distinct idempotency key, the selected key, new native input and renewed consent. The scoped request metadata supplies the model and protocol. Both `requests.replay` and `playground.run` are checked. A replay creates a separately funded request.
- By default the client owns the active view and the server stores only metadata. With `options.retain_text_preview`, explicit consent and `requests.content.read`, this process can serve up to 8 KiB of streamed text to the same actor for 30 seconds. Set `h.PreviewTTL` before registration to disable previews (zero) or select a TTL of up to five minutes. There are at most 128 local previews. On another instance or after restart, they are absent. No input, reasoning, tool arguments or raw events enter previews.

## Legacy exit and verification

Runtime should switch the console to authenticated v2, announce the transition for the operator-funded `/playground/chat` forwarder, measure remaining legacy traffic, then remove its public registration and operator credential. Binary rollback can remove v2 registration while preserving run identities and receipt links. Coordinate schema contraction with the journal, billing and request readers.

From `backend`, run `go build ./...`, `go vet ./...` and `go test ./...`. With a disposable migrated PostgreSQL `job_test` URI in `UPSTREAM_JOB_TEST_DSN` and Redis in `AFFINITY_REDIS_TEST_ADDR`, run `go test -tags=integration -count=1 ./internal/control/playground`. The fixture replaces external provider output and catalog discovery. It uses the production policy, journal, dispatch permit, budget repository, settlement ledger and scoped Gin routes. Native variant fixtures live in `contracts/protocol/playground/native.json`. At aggregate compose time regenerate OpenAPI and v2 TypeScript declarations from the fragment.
