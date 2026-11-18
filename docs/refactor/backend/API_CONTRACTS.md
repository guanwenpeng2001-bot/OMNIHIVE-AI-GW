# Console and public API contracts

This is the target v2 contract for frontend implementation. Examples are deterministic wire fixtures. The deployed handlers obtain data from the modules and database identified in the architecture. The standard inference APIs and existing payment callback routes retain their native wire formats.

## Common types, authorization and transport

`P` expands to `/api/v2/workspaces/{workspace_id}/projects/{project_id}`, `W` to `/api/v2/workspaces/{workspace_id}`, `U` to `/api/v2/public`, and `A` to `/api/v2/admin`. Examples following an endpoint show the `data` member of a successful management response. POST with an empty request uses `{}`. GET has no request body. The existing same-origin console authentication and configured CSRF protections apply to protected endpoints. Server auth establishes the principal and verifies membership, action, project relationship and object ownership on every read/write, event subscription and artifact access. All tenant responses use `Cache-Control: private, no-store`. Public projections have a separate cache key without tenant data. Policy can disable anonymous catalog access.

| Rule | Wire contract |
| --- | --- |
| Envelope | `{"code":0,"message":"success","data":{"id":"prj_4"}}`. Creation uses HTTP 201. Queued operations use HTTP 202 with `message:"accepted"`. `X-Request-ID` identifies the control request |
| IDs | Opaque JSON strings, including historical integer IDs rendered as decimal strings. Model IDs on URL paths use the opaque catalog entry ID. The provider-native model string can contain slashes |
| Money | `{"currency":"USD","amount":"0.00007813"}`. Settled amounts use eight decimal places. Rates include `amount` (up to 18 fractional digits), `unit` and positive integer-string `per`. Directional adjustments use signed amount |
| Quantities | Integer counts including token, byte, and sample counts use unsigned decimal strings. HTTP status, bounded page size, elapsed milliseconds and bounded percent values use JSON numbers. Fractional unit quantities use decimal strings |
| Time | RFC3339 UTC instants. Filters are half-open `[from,to)`. `timezone` is an IANA name for calendar aggregation and budget reset. Durations explicitly name their unit |
| Lists | `{"items":[],"next_cursor":null,"as_of":"2026-11-19T10:00:00Z"}`. Default limit 25, range 1–100. Cursor is opaque, scoped and bound to filters/sort/as_of. Stable ID breaks sort ties. Lists return an empty array for a valid empty result |
| State | Nullable unknown amounts/metrics include their section's evidence/state code. Permission-based omission is listed in `redactions`. `0` is an observation. Excess historical gaps are described by `data_completeness` |
| Writes | Resource updates require `If-Match: "<version>"`. A mismatch returns 412 with current version. Non-idempotent creates, publication, retry, cancellation, rotation, export and billing operations require `Idempotency-Key`. The operation scope includes principal, workspace, project, method, route and canonical fingerprint. Duplicate matching input returns its original resource/status. Same key/different input returns 409 |
| Errors | `{"code":422,"message":"Select a supported protocol.","reason":"UNSUPPORTED_ACTION","metadata":{"request_id":"ctl_1","category":"validation","retry":"never","fields":[{"path":"/protocol","reason":"UNSUPPORTED_ACTION"}],"operation_id":null,"status_url":null},"data":null}` |
| Error mapping | 400 malformed input, 401 expired/missing identity, 403 missing action on a visible scope, 404 unknown or inaccessible object, 409 incompatible state/idempotency conflict, 412 stale version, 422 invalid domain input, 429 rate/capacity, 503 stale mandatory facts or unavailable dependency. Budget denial carries `BUDGET_EXHAUSTED` and HTTP 402 on v2. Legacy protocol errors use protocol mappings |
| Uncertain result | `retry:"query_status"` includes an authorized operation/status URL. A caller reuses the same operation identity on transport loss. No automatic second charge or upstream generation |
| Mutations returning operations | `{"operation_id":"op_9","state":"queued","status_url":"/api/v2/workspaces/w1/operations/op_9","resource_id":"cn_2"}`. `GET W/operations/{id}` returns that object with `state` = queued/running/succeeded/failed/unknown, `result` object or null, and `error` safe fault or null. Only authorized actor or delegated role may query |
| List/query grammar | Arrays in query parameters repeat the key. Unknown filters, invalid timestamps, conflicting filters and unsupported sorts return 422 with a field path. A snapshot/cursor past retention returns 410 `SNAPSHOT_EXPIRED` |

All action names in this document are server-checked grants. Resource links returned for operations, results, request details and artifacts always include canonical scope.

## Scope and keys

| Endpoint | Grant | Request and response data |
| --- | --- | --- |
| `GET /api/v2/me/scopes` | authenticated membership | Query `cursor,limit`. Item `{"workspace_id":"w1","name":"Research","projects":[{"id":"p1","name":"Default"}],"projects_next_cursor":null}`; continue with `GET W/projects` |
| `GET W/projects`, `POST W/projects` | `projects.read`, `projects.create` | List items or create body `{"name":"Search"}` → `{"id":"p2","workspace_id":"w1","name":"Search","version":"1"}` |
| `GET P/access` | membership | `{"actor_id":"u1","workspace_id":"w1","project_id":"p1","actions":["requests.read","playground.run"],"scope_version":"a7","expires_at":"2026-11-19T10:01:00Z"}` |
| `GET P/keys`, `POST P/keys` | `keys.read`, `keys.create` | List safe key metadata. Create `{"name":"Search","model_access":{"mode":"allow_list","ids":["m1"]},"actions":["text.chat"],"expires_at":null}` → `{"id":"k1","name":"Search","prefix":"oh_…","credential":"<one-time-generated-value>","owner_id":"u1","version":"1"}`. Creation shows the credential once; a repeated create returns metadata plus `credential:null` |
| `PUT W/members/{id}/grants` | `members.grants.write` | `{"project_ids":["p1"],"actions":["requests.read","tasks.read"]}` → `{"member_id":"u7","version":"4","effective_at":"2026-11-19T10:00:00Z"}` |

### Permission map for console implementers

| Resource | Actions checked by the server |
| --- | --- |
| Connection inventory and wizard | Schema/list/detail use `connections.read`. Create/patch use `connections.edit`. Pool assignment also requires `pools.assign`. Verify uses `connections.verify`. Preview uses `connections.edit`. Activate/bulk-apply require `connections.activate` and each affected edit/assignment grant |
| Secret lifecycle | Set/clear use `credentials.write`. Keep requires edit access on the connection. Rotation requires `credentials.rotate`. Secret metadata/events require `credentials.read_metadata`. Separate grants cover full revocation and tenant BYOK management |
| Route publication | A policy is visible only in the requested scope. `routing.edit`, `routing.simulate` and `routing.publish` are independent. Simulating an alternate key also requires acting on behalf of that key in its project |
| Consumption and detail | `requests.read` grants authorized metadata. `connections.read` permits connection labels, `billing.ledger.read` permits payer/ledger detail. `requests.content.read` and retention consent are required for retained content. Metadata export uses `requests.export` and the same field projection |
| Funding and execution | Run/task creation requires a usable downstream key and the corresponding run/create action. Payer, entitlement and project budget are resolved by the server. Replay, paid retry, and adjustments have their own actions |
| Public views | A public credential-free request is evaluated against publication settings. Private endpoint identities, user-bound rates, scopes and sample breakdowns are absent from public responses |

## Catalog, comparison and quotes

| Endpoint | Access | Input | Output |
| --- | --- | --- | --- |
| `GET U/models`, `GET P/models` | Public policy / `models.read` | `q,provider,protocol,action,modality,region,retention,stability,sort,cursor,limit`; scoped endpoint additionally supports `accessible,routable`. Sort name/price_input/context/ttft | List of ModelSummary |
| `GET U/models/{id}`, `GET P/models/{id}` | Same | Optional `catalog_version` | ModelDetail |
| `POST U/models/compare`, `POST P/models/compare` | Same | CompareInput below, one to three distinct IDs | CompareResult |
| `POST P/quotes` | `models.read` | One model, action, scenario and optional key ID | Quote with effective pricing scope and version |

ModelSummary data example:

```json
{"id":"m1","model":"provider/text-1","display_name":"Text 1","provider":"provider","modalities":["text"],"protocols":["openai_chat"],"actions":["text.chat"],"context_tokens":"128000","maturity":"stable","catalog_version":"cat_42","catalog_state":"listed","access_state":"allowed","route_state":"eligible","price_state":"priced","input_rate":{"currency":"USD","amount":"1.25","unit":"input_token","per":"1000000"},"output_rate":{"currency":"USD","amount":"10","unit":"output_token","per":"1000000"},"observed_at":"2026-11-19T09:58:00Z"}
```

Public `access_state` and `route_state` are `requires_scope`. Allowed scoped states are `allowed|denied|unknown` and `eligible|ineligible|unknown`. Price states are `priced|free|unpriced|error`. ModelDetail wraps a summary and adds evidence:

```json
{"model":{"id":"m1","model":"provider/text-1","display_name":"Text 1","provider":"provider","modalities":["text"],"protocols":["openai_chat"],"actions":["text.chat"],"context_tokens":"128000","maturity":"stable","catalog_version":"cat_42","catalog_state":"listed","access_state":"allowed","route_state":"eligible","price_state":"priced","input_rate":{"currency":"USD","amount":"1.25","unit":"input_token","per":"1000000"},"output_rate":{"currency":"USD","amount":"10","unit":"output_token","per":"1000000"},"observed_at":"2026-11-19T09:58:00Z"},"aliases":[],"capabilities":[{"action":"text.chat","protocol":"openai_chat","mode":"native","tools":"supported","stream":"supported","source":"verified_probe","verified_at":"2026-11-18T12:00:00Z","expires_at":"2026-11-25T12:00:00Z","adapter_version":"ad_3","limitations":[]}],"endpoints":[{"id":"ep1","provider":"provider","regions":["DE"],"training":"unknown","content_retention":"unknown","safety_retention_days":null,"evidence_source":"provider_declaration"}],"price_version":"pr_9","additional_rates":[],"metrics":{"window_start":"2026-11-18T10:00:00Z","window_end":"2026-11-19T10:00:00Z","source":"gateway_observations","requests":"0","success_rate":null,"ttft_p50_ms":null,"output_tokens_per_second":null,"state":"insufficient_samples"},"benchmarks":[],"documentation_url":"/docs/models/m1"}
```

A benchmark cell has `benchmark_id,value,unit,source,verified_at`. Omit unverified cells. `additional_rates` includes tier bounds, time bands, cache and media dimensions when available.

CompareInput and CompareResult:

```json
{"model_ids":["m1","m2"],"protocol":"openai_chat","action":"text.chat","scenario":{"input_tokens":"1000","cached_input_tokens":"0","output_tokens":"100","requests":"1"},"price_at":"2026-11-19T10:00:00Z"}
```

```json
{"catalog_version":"cat_42","as_of":"2026-11-19T10:00:00Z","rows":[{"model_id":"m1","capabilities":{"tools":"supported","stream":"supported"},"quote":{"state":"estimated","price_version":"pr_9","estimated_charge":{"currency":"USD","amount":"0.00225000"},"components":[{"kind":"input","quantity":"1000","raw_amount":"0.00125"},{"kind":"output","quantity":"100","raw_amount":"0.001"}],"rounding_adjustment":"0.00000000"},"metrics":{"state":"insufficient_samples","requests":"0","ttft_p50_ms":null},"benchmarks":[]},{"model_id":"m2","capabilities":{"tools":"unknown","stream":"unknown"},"quote":{"state":"unpriced","price_version":null,"estimated_charge":null,"components":[],"rounding_adjustment":null},"metrics":{"state":"insufficient_samples","requests":"0","ttft_p50_ms":null},"benchmarks":[]}]}
```

`POST P/quotes` uses `{"model_id":"m1","api_key_id":"k1","protocol":"openai_chat","action":"text.chat","scenario":{"input_tokens":"1000","cached_input_tokens":"0","output_tokens":"100","requests":"1"}}` and returns the same Quote shape. Cached input is a subset of input; the calculator splits the two rate bands. Public estimates use publication-approved reference prices. Scoped estimates use authorized versions and disclose source.

## Playground

`POST P/playground/runs` requires `playground.run`, an owned/delegated downstream key ID, confirmed funding eligibility and an idempotency key. This operation opens an authenticated POST response stream with `Accept: text/event-stream`. The submitted `input` has the selected native protocol shape. Supported first-release protocols are `openai_chat`, `openai_responses`, `anthropic_messages` and `gemini_generate`, gated by catalog capability. The server validates the selected model against the selected key and bounds all quantities. The browser sends session credentials through the existing client and retains the prompt locally for this view.

```json
{"api_key_id":"k1","model_id":"m1","protocol":"openai_chat","action":"text.chat","input":{"messages":[{"role":"user","content":"Hello"}],"max_tokens":128},"options":{"stream":true,"include_raw_events":false},"funding_acknowledged":true}
```

Before the first event, errors use the management envelope. Each SSE event carries a run ID, server request ID and strictly increasing integer sequence. Example response:

```text
event: started
id: 1
data: {"seq":1,"run_id":"run1","request_id":"r1","protocol":"openai_chat","price_version":"pr_9"}

event: delta
id: 2
data: {"seq":2,"run_id":"run1","request_id":"r1","kind":"text","index":0,"text":"Hello"}

event: finished
id: 3
data: {"seq":3,"run_id":"run1","request_id":"r1","outcome":"succeeded","settlement_state":"pending","detail_url":"/api/v2/workspaces/w1/projects/p1/requests/r1"}
```

If `options.stream=false`, provide `Accept: application/json`. The synchronous response data is `{"run_id":"run1","request_id":"r1","outcome":"succeeded","protocol":"openai_chat","output":{"choices":[{"index":0,"message":{"role":"assistant","content":"Hello"}}]},"settlement_state":"pending","detail_url":"/api/v2/workspaces/w1/projects/p1/requests/r1"}`. The protocol determines the shape of `output`; live content follows the same retention settings.

Tool fragments use `event: delta`, `kind:"tool_arguments"`, `tool_call_id`, `index`, `name`, and `fragment`. Usage events carry typed quantities and evidence level. `diagnostic` events carry conversion codes and affected field paths. Authorized raw events are streamed live under `raw_event` after safe projection. A terminal failure uses `event: failed` with `reason,retry,outcome,detail_url`. A successful stream has one terminal event. Partial output is retained in the active view. Disconnect propagates cancellation. A failed connection after admission queries the existing operation using its idempotency identity. An identical repeated POST for an admitted run returns HTTP 200 with JSON RunSummary, including its status and detail URLs. The requester selects the decoder by response Content-Type; resume-by-re-execution is unavailable.

| Endpoint | Input | Result |
| --- | --- | --- |
| `GET P/playground/runs/{id}` | `playground.read` | `{"run_id":"run1","request_id":"r1","state":"running","last_seq":2,"content_retained":false,"status_url":"/api/v2/workspaces/w1/projects/p1/playground/runs/run1","detail_url":"/api/v2/workspaces/w1/projects/p1/requests/r1"}` |
| `GET P/playground/runs/by-idempotency-key` | Same actor, original key in `Idempotency-Key` header | Same RunSummary, or 404 |
| `POST P/playground/runs/{id}/cancel` | `playground.cancel`, `{}` | 202 `{"run_id":"run1","request_id":"r1","state":"cancel_requested","settlement_state":"pending"}`. Check RunSummary for final outcome |
| `POST P/requests/{id}/replay` | `requests.replay`, `{"api_key_id":"k1","input":{"messages":[{"role":"user","content":"Hello"}]},"funding_acknowledged":true}` | New charged run, SSE contract above. A distinct idempotency key identifies this new request |

For side-by-side execution, submit one independent run per authorized model with a distinct key, cost consent, stream and cancellation handle. A compare quote displays both estimates before submitting. An existing run is never continued by replaying a POST after a stream disconnect.

## Upstream connection wizard and credentials

Use workspace scope for admin-managed connection inventory. `connections.*`, `credentials.*` and `pools.*` are separate permissions. A workspace owner receives only explicitly granted actions. Alias `C` below means `W/connections`.

| Endpoint | Request | Response data |
| --- | --- | --- |
| `GET W/provider-schemas` | Optional `provider,auth_type` | `{"items":[{"provider":"provider","schema_version":"3","auth_types":["api_key"],"fields":[{"path":"endpoint","type":"https_url","required":true,"component":"EndpointInput"},{"path":"credential","type":"secret","required":true,"component":"SecretInput"}],"supported_actions":["text.chat"]}],"next_cursor":null,"as_of":"2026-11-19T10:00:00Z"}` |
| `GET C`, `GET C/{id}` | `provider,state,pool_id,cursor,limit` on list | List or Connection object below |
| `POST C` | `{"provider":"provider","schema_version":"3","name":"Production","endpoint":"https://api.example.org","auth_type":"api_key","network_policy_id":"net1"}` | 201 Connection, initially `draft` |
| `PATCH C/{id}` | `{"name":"Production EU","network_policy_id":"net1","model_ids":["m1"],"pool_ids":["pool1"],"limits":{"concurrency":8}}` | Updated Connection. Omitted fields retain current values. Explicit empty list clears assignments after validation |
| `PUT C/{id}/credential` | `{"operation":"set","type":"api_key","secret":"<entered-value>"}`; alternatives `{"operation":"keep"}`, `{"operation":"clear"}` | `{"connection_id":"cn1","credential":{"version":"2","state":"unverified","fingerprint":"fp_9","last_four":"1234"},"version":"5"}`. Clear disables dependent dispatch and preserves audit |
| `POST C/{id}/verify` | `{"checks":["network","authorization","discover_models"],"credential_version":"2"}` | 202 Operation, terminal `result:{"verification_id":"vr1","checks":[{"name":"network","state":"passed","reason":null}],"models":["m1"],"verified_at":"2026-11-19T10:00:00Z","expires_at":"2026-11-20T10:00:00Z"}` |
| `POST C/{id}/preview` | `{"model_ids":["m1"],"pool_ids":["pool1"],"limits":{"concurrency":8}}` | `{"preview_id":"pv1","base_version":"5","adds":["pool1"],"removes":[],"blocked":[],"expires_at":"2026-11-19T10:05:00Z"}` |
| `POST C/{id}/activate` | `{"preview_id":"pv1","verification_id":"vr1","expected_credential_version":"2"}` | 200 Connection with `state:"active"`, or 409 stale verification/preview |
| `POST C/bulk-previews` | `{"ids":["cn1","cn2"],"patch":{"pool_ids":["pool1"]}}` | `{"preview_id":"bp1","items":[{"id":"cn1","base_version":"6","state":"applicable"},{"id":"cn2","base_version":"2","state":"blocked","reason":"INCOMPATIBLE_PROVIDER"}],"expires_at":"2026-11-19T10:05:00Z"}` |
| `POST C/bulk-apply` | `{"preview_id":"bp1","selected_ids":["cn1"]}` | 202 Operation, terminal `result:{"items":[{"id":"cn1","state":"applied","version":"7"}]}`. Per-item CAS/conflicts are explicit |
| `POST C/{id}/rotations` | `{"expected_credential_version":"2","replacement":{"type":"api_key","secret":"<entered-value>"}}` | 202 Operation; progress `validating\|switching\|draining\|revoked\|failed`. Existing version serves until validation succeeds |
| `GET C/{id}/events` | `credentials.read_metadata`, cursor | List items `{"id":"ev1","type":"credential.rotated","version":"3","at":"2026-11-19T10:00:00Z","actor_id":"u7"}` |
| `GET W/pools`, `POST W/pools` | Read/write grants. Create `{"name":"Primary EU","connection_ids":["cn1"],"max_concurrency":8}` | List or `{"id":"pool1","name":"Primary EU","connection_ids":["cn1"],"max_concurrency":8,"version":"1"}` |

Connection response:

```json
{"id":"cn1","workspace_id":"w1","name":"Production","provider":"provider","schema_version":"3","auth_type":"api_key","endpoint":"https://api.example.org","network_policy_id":"net1","state":"draft","credential":{"version":"2","state":"unverified","fingerprint":"fp_9","last_four":"1234"},"model_ids":[],"pool_ids":[],"limits":{"concurrency":8},"last_verification":null,"quota":{"state":"unknown","remaining":null,"last_success_at":null},"version":"5","allowed_actions":["connections.verify","connections.edit"]}
```

Verification checks report `passed|failed|unknown|unsupported` with stable reason codes. A stale quota observation includes its observation time. OAuth/service-account providers return a schema-selected, existing authorization workflow under the same actor/workspace and short-lived state identity. Credential-setting bodies go directly to the secret store and stay outside drafts, logs, audit, URLs and query cache.

## Route policy editing, simulation and explain

Alias `R` means `W/routing/policies`. `scope` is workspace or a project in this workspace, optionally narrowed to a key. Each version freezes inherited origin/version and plural facts about privacy/region. Candidate control, hard limits and preferences are separate fields.

```json
{"name":"EU stable","scope":{"type":"project","id":"p1"},"constraints":{"regions":{"mode":"allow_list","values":["DE","FR"]},"retention":"strict_zero","training":"prohibited","max_input_rate":{"currency":"USD","amount":"2","unit":"input_token","per":"1000000"},"required_capabilities":["tools"]},"candidates":{"pool_ids":["pool1"],"denied_provider_ids":[],"allow_expansion":false},"preferences":{"template":"stable","ordered_provider_ids":[]},"fallback":{"endpoint":true,"model_ids":[],"byok_to_platform":false}}
```

| Endpoint | Request | Response data |
| --- | --- | --- |
| `GET R`, `GET R/{id}` | `routing.read`, scope/list filters | List or `{"id":"pol1","active_version":"v2","draft_version":"d3","definition":{…},"etag":"7"}`; definition is the full PolicyInput above |
| `POST R`, `PUT R/{id}/draft` | `routing.edit`, PolicyInput | `{"id":"pol1","draft_version":"d3","base_version":"v2","etag":"7","definition":{…}}` |
| `POST R/{id}/simulate` | `routing.simulate`, SimulationInput below | SimulationResult |
| `POST R/{id}/publish` | `routing.publish`, `{"draft_version":"d3","base_version":"v2","simulation_id":"sim1","change_note":"Restrict regions"}` | `{"id":"pol1","active_version":"v3","previous_version":"v2","published_at":"2026-11-19T10:00:00Z","impact":{"projects":"1","keys":"4"}}` |
| `GET R/{id}/versions` | `routing.read`, cursor | Version items `{"id":"v3","base_version":"v2","actor_id":"u7","published_at":"2026-11-19T10:00:00Z","change_note":"Restrict regions"}` |
| `POST R/{id}/rollback` | `routing.publish`, `{"from_version":"v3","restore_version":"v2","change_note":"Rollback"}` | New published version object, same shape as publish. Active version identity advances |
| `GET P/requests/{id}/route-explain` | `requests.read`, scoped projection | `{"request_id":"r1","trace":{…},"data_completeness":{"state":"complete","missing":[]}}`; trace uses the schema below with mode `execution` |

SimulationInput omits prompt, credentials and private provider responses:

```json
{"version":"d3","compare_version":null,"project_id":"p1","api_key_id":"k1","model_id":"m1","protocol":"openai_chat","action":"text.chat","required_capabilities":["tools"],"input_token_bound":"1000","output_token_bound":"128","snapshot_id":null,"seed":"9041"}
```

SimulationResult with one trace. A non-null `compare_version` also returns a full trace under `baseline` calculated from the same snapshot and seed.

```json
{"simulation_id":"sim1","expires_at":"2026-11-19T10:05:00Z","trace":{"schema_version":1,"mode":"simulation","snapshot_id":"snap1","as_of":"2026-11-19T10:00:00Z","policy_version":"d3","catalog_version":"cat_42","price_version":"pr_9","seed":"9041","origins":[{"constraint":"regions","scope_type":"workspace","scope_id":"w1","version":"wp8"}],"stages":[{"name":"region","input_count":"2","remaining_count":"1"}],"candidates":[{"id":"e1","state":"eligible","rank":1,"reasons":[],"factors":[{"name":"ttft_p50","value":"215","unit":"ms","source":"gateway_observations","window_seconds":3600,"samples":"84","observed_at":"2026-11-19T09:59:00Z","freshness":"fresh"}]},{"id":"e2","state":"excluded","rank":null,"reasons":[{"code":"REGION_UNVERIFIED","stage":"region","origin":"wp8"}],"factors":[]}],"affinity":{"state":"miss","reason":null},"selected":{"model_id":"m1","candidate_id":"e1","funding_source":"wallet"},"conversion":{"mode":"native","diagnostics":[]},"uncertainties":["CAPACITY_CAN_CHANGE"],"redactions":["connection_identity"]},"baseline":null,"diff":null}
```

With no baseline, return `baseline:null,diff:null`; with one, include both. Reason codes include `MODEL_DENIED`, `ACTION_UNSUPPORTED`, `REGION_UNVERIFIED`, `RETENTION_INELIGIBLE`, `CREDENTIAL_REVOKED`, `QUOTA_UNKNOWN`, `BUDGET_EXHAUSTED`, `CAPACITY_FULL`, `COOLDOWN`, `PRICE_CEILING`, and `FUNDING_NOT_AUTHORIZED`. Excluded candidates may carry multiple reasons, each with its owning policy/evidence source.

## Request detail

`GET P/requests` accepts `from,to,timezone,key_id,model_id,protocol,outcome,settlement_state,funding_source,reason,cursor,limit`. Each item includes `id,started_at,requested_model,protocol,outcome,settlement_state,charged_amount,detail_url`. `GET P/requests/{id}` returns a scoped composite detail. Both require `requests.read`. The finance grant controls ledger and payer details; authorized users can inspect their own consumption. Support access and cross-workspace actions record audit.

```json
{
  "id":"r1","client_correlation_id":null,"workspace_id":"w1","project_id":"p1","api_key_id":"k1","actor_id":"u1","key_owner_id":"u1","funding_account_id":"fa1","trace_id":"tr1","origin":"playground","protocol":"openai_chat","action":"text.chat","requested_model":"provider/text-1","effective_model":"provider/text-1","outcome":"succeeded",
  "started_at":"2026-11-19T10:00:00Z","ended_at":"2026-11-19T10:00:01Z","timings_ms":{"queue":0,"route":3,"upstream_ttft":215,"total":1000},
  "timeline":[{"kind":"admitted","at":"2026-11-19T10:00:00Z"},{"kind":"first_output","at":"2026-11-19T10:00:00.215Z"},{"kind":"client_completed","at":"2026-11-19T10:00:01Z"},{"kind":"settled","at":"2026-11-19T10:00:01.250Z"}],
  "route":{"policy_version":"v3","snapshot_id":"snap1","candidate_id":"e1","explain_url":"/api/v2/workspaces/w1/projects/p1/requests/r1/route-explain"},
  "attempts":[{"id":"at1","number":1,"candidate_id":"e1","started_at":"2026-11-19T10:00:00Z","duration_ms":1000,"acceptance":"accepted","outcome":"succeeded","status":200,"reason":null,"semantic_output":true}],
  "conversion":{"mode":"native","diagnostics":[]},"usage":{"level":"observed","source":"provider_final_usage","units":[{"kind":"input_token","quantity":"1000"},{"kind":"output_token","quantity":"100"}]},
  "billing":{"billing_source":"wallet","state":"settled","currency":"USD","price_version":"pr_9","reservation_id":"res1","reserved":{"currency":"USD","amount":"0.00300000"},"estimate":{"currency":"USD","amount":"0.00225000"},"platform_charge":{"currency":"USD","amount":"0.00225000"},"catalog_reference":{"currency":"USD","amount":"0.00225000"},"entitlement_consumption":null,"byok_external_estimate":null,"platform_service_fee":null,"settlement_id":"st1","receipt_state":"settled","ledger_entry_ids":["le1"],"rounding_rule":"half_away_from_zero_8"},
  "task_ids":[],"diagnostics":[],"redactions":["connection_identity"],"data_completeness":{"state":"complete","missing":[]},"allowed_actions":["requests.export"]
}
```

Execution outcome is `admitted|running|succeeded|failed|interrupted|canceled|unknown`. Attempt acceptance is `not_sent|accepted|unknown`. Usage evidence level is `estimated|observed|reconciled|unknown`. Financial state is `estimated|reserved|pending|settled|needs_review|adjusted|refunded|unknown`. `receipt_state` is `unconfirmed|pending|settled|needs_review`. Funding source is `wallet|subscription|byok|mixed|unknown`. In a pre-receipt interruption, show `billing.state:"unknown",receipt_state:"unconfirmed",platform_charge:null`. A durable unpriced receipt has `state:"needs_review"`. Older executions can return `data_completeness:{"state":"partial","missing":[{"section":"route","reason":"LEGACY_NOT_RECORDED"}]}`.

An authorized metadata export uses `POST P/requests/{id}/exports` with `{"format":"json","sections":["timeline","route","attempts","usage","billing"]}`, returning a 202 Operation. Its final result includes an authorized short-lived download reference; billing and routing sections use the same field-level checks as RequestDetail.

## Cost and quota workbench

Alias `B` means `P/billing`. All queries validate payer visibility, project membership and currency. Multiple currencies are separate groups. `billing.read` covers authorized aggregates; `billing.ledger.read` covers individual postings. Reads remain available after budget exhaustion. Shared entitlement/limit IDs reference one allowance across models.

| Endpoint | Request | Response data |
| --- | --- | --- |
| `GET B/activity` | Required `from,to,timezone,currency`. Optional `key_id,model_id,member_id,funding_source,settlement_state,group_by` (day/model/key/member), cursor | Activity below |
| `GET B/charges` | Same filters except group_by | Charge list items `{"request_id":"r1","settlement_id":"st1","at":"2026-11-19T10:00:01Z","model_id":"m1","billing_source":"wallet","state":"settled","platform_charge":{"currency":"USD","amount":"0.00225000"},"price_version":"pr_9","detail_url":"/api/v2/workspaces/w1/projects/p1/requests/r1"}` |
| `GET B/quotas` | Optional `api_key_id,member_id,model_id,at`. Additional filters require grant for the identity | EffectiveQuota below |
| `PUT B/budgets/{id}` | `billing.budgets.write`, `{"mode":"hard","limit":{"currency":"USD","amount":"20.00000000"},"timezone":"Asia/Shanghai","window":"calendar_month","include_byok_external_estimates":false}` | Updated rule as in EffectiveQuota, with new version |
| `GET B/ledger` | `billing.ledger.read`, filters plus `request_id,settlement_id,funding_account_id` | List items `{"id":"le1","effect_id":"st1","kind":"charge","funding_account_id":"fa1","signed_amount":{"currency":"USD","amount":"-0.00225000"},"request_id":"r1","reverses_id":null,"posted_at":"2026-11-19T10:00:01.250Z"}` |
| `POST B/exports` | `billing.export`, `{"from":"2026-11-18T00:00:00Z","to":"2026-11-19T00:00:00Z","timezone":"UTC","currency":"USD","format":"csv"}` | 202 Operation with reauthorized, expiring artifact link in terminal result |
| `GET W/billing/reconciliation` | `billing.reconcile.read`, `state,from,to,currency,cursor` | Case items `{"id":"rc1","request_id":"r7","state":"needs_review","reason":"RECEIPT_UNCONFIRMED","difference":null,"currency":"USD","version":"1"}` |
| `POST W/billing/reconciliation/{id}/resolve` | `billing.reconcile.write`, `{"action":"attach_evidence","evidence_id":"ev9","note":"Verified against provider statement"}` | 202 Operation. Terminal result includes original case, receipt/adjustment identity and audit reference |
| `POST B/adjustments` | `billing.adjust`, step-up, `{"ledger_entry_id":"le1","amount":{"currency":"USD","amount":"0.00025000"},"reason":"billing_correction","note":"Reviewed case rc1"}` | 201 `{"id":"adj1","ledger_entry_id":"le2","reverses_id":"le1","state":"posted"}`. Refund via payment rail retains existing refund workflow |

Activity response:

```json
{"from":"2026-11-18T00:00:00Z","to":"2026-11-19T00:00:00Z","timezone":"UTC","currency":"USD","as_of":"2026-11-19T00:01:00Z","data_completeness":{"state":"complete","missing":[]},"totals":{"logical_requests":"1","attempts":"2","observed_usage_requests":"1","catalog_reference":{"currency":"USD","amount":"0.00225000"},"platform_charge":{"currency":"USD","amount":"0.00225000"},"subscription_consumption":null,"byok_external_estimate":null,"platform_service_fee":null,"reserved":{"currency":"USD","amount":"0.00000000"},"pending_requests":"0","needs_review_requests":"0"},"items":[{"group":"m1","logical_requests":"1","platform_charge":{"currency":"USD","amount":"0.00225000"},"filter":{"model_id":"m1"}}],"next_cursor":null}
```

EffectiveQuota response:

```json
{"as_of":"2026-11-19T10:00:00Z","funding_account_id":"fa1","blocked":true,"blocking_rule_ids":["b1"],"rules":[{"id":"b1","version":"3","scope":{"type":"project","id":"p1"},"owner_id":"w1","shared_allowance_id":"sa1","mode":"hard","unit":"USD","limit":"20.00000000","used":"19.00000000","reserved":"1.00000000","available":"0.00000000","overrun":"0.00000000","window":"calendar_month","timezone":"Asia/Shanghai","resets_at":"2026-11-30T16:00:00Z","freshness":"fresh","observed_at":"2026-11-19T10:00:00Z","allow_extra_spend":false,"include_byok_external_estimates":false,"adjust_action":"billing.budgets.write","can_adjust":false}],"external_exposure":{"state":"bounded","risk_limit":{"currency":"USD","amount":"0.00000000"}}}
```

Window units can be requests, tokens or a currency. Unknown amounts are null with `freshness:"unknown"`. A hard constraint with insufficient evidence carries `BUDGET_UNVERIFIED`. Soft rules produce a warning and retain their scope/owner. Adjustment permission is independently evaluated.

## Task center and artifacts

Alias `T` means `P/tasks`. Task metadata, output artifacts, cancellation and retry are separate authorizations. Read paths work when the user's wallet is exhausted. Job creation uses a verified model/adapter and the same admission/receipt path as normal inference.

| Endpoint | Input | Output |
| --- | --- | --- |
| `GET T`, `GET T/{id}` | `tasks.read`, list filters `kind,state,model_id,from,to,cursor,limit` | List of task summaries or TaskDetail below. A summary contains `id,kind,state,stage,model_id,created_at,updated_at,progress,settlement_state,allowed_actions` |
| `POST T` | `tasks.create`, `{"kind":"image","model_id":"m_img","api_key_id":"k1","input":{"prompt":"A leaf","size":"1024x1024","count":1},"funding_acknowledged":true}` | 202 TaskDetail. Video/audio creation offered for verified actions only |
| `POST T/{id}/cancel` | `tasks.cancel`, `{}` | 202 `{"task_id":"t1","cancel_state":"requested","state":"running","version":"3"}`. Unsupported returns 409 `CANCEL_UNSUPPORTED` |
| `POST T/{id}/retry` | `tasks.retry`, `{"item_ids":["i2"],"funding_acknowledged":true}` | 202 `{"task_id":"t2","parent_task_id":"t1","new_request_id":"r7","state":"queued"}`. Items with unresolved submission return 409 `SUBMISSION_UNKNOWN` |
| `GET T/{id}/items` | `tasks.read`, cursor | Item `{"id":"i2","state":"failed","acceptance":"not_sent","reason":"INPUT_INVALID","retryable":false,"artifact_ids":[]}` |
| `POST T/{id}/artifacts/{artifact_id}/download` | `tasks.artifacts.read`, `{}` | `{"url":"/api/v2/workspaces/w1/projects/p1/tasks/t1/artifacts/art1/content","ticket":"opaque","expires_at":"2026-11-19T10:01:00Z","content_type":"image/png","size_bytes":"12030"}`. Send the short-lived, actor/scope-bound ticket in the X-Artifact-Ticket header |
| `GET T/{id}/artifacts/{artifact_id}/content` | Same session and X-Artifact-Ticket header | Binary content, safe Content-Disposition, no-store. Expired/removed artifact returns 410; unauthorized object returns 404 |

TaskDetail:

```json
{"id":"t1","project_id":"p1","kind":"image","source":"batch_image","model_id":"m_img","request_id":"r2","parent_task_id":null,"state":"running","stage":"provider_processing","provider_state":"accepted","artifact_state":"pending","cancel_state":"unsupported","progress":null,"progress_source":"unreported","items":{"total":"3","succeeded":"1","failed":"0","pending":"2"},"artifacts":[],"settlement_state":"reserved","estimate":{"currency":"USD","amount":"0.12000000"},"actual_charge":null,"created_at":"2026-11-19T10:00:00Z","updated_at":"2026-11-19T10:00:01Z","allowed_actions":["tasks.read"],"action_reasons":{"tasks.cancel":"CANCEL_UNSUPPORTED"},"version":"2"}
```

Artifacts contain `id,content_type,size_bytes,expires_at,state`. Parent batches expose `stage:"partially_succeeded"` with separate item outcomes. Submitted upstream, stored artifact and settled financial effect each have an independent state.

## Public status and incidents

`GET U/status?window=24h` accepts `1h|24h|7d|30d`. `GET U/status/incidents?from=…&to=…&cursor=…` returns the public incident list. `GET U/status/incidents/{id}` returns one published incident. Anonymous responses contain publication-approved aggregates. Use UTC, stable component IDs, CORS policy for public data and `ETag`/conditional GET.

```json
{"as_of":"2026-11-19T10:00:00Z","window_start":"2026-11-18T10:00:00Z","window_end":"2026-11-19T10:00:00Z","data_lag_seconds":60,"overall":"unknown","components":[{"id":"inference","name":"Inference gateway","state":"unknown","source":"gateway_observations","sample_count":"0","minimum_sample_count":"20","success_rate":null,"ttft_p95_ms":null,"stale_after":"2026-11-19T10:05:00Z","reason":"INSUFFICIENT_SAMPLES"}],"incidents":[],"definitions":{"success_rate":"successful completed logical requests / admitted non-client-canceled logical requests","excluded_categories":["admission_denial","client_canceled"],"reported_categories":["gateway_error","upstream_error","stream_interruption"],"attempts_counted_separately":true}}
```

Incident:

```json
{"id":"inc1","title":"Elevated inference errors","state":"monitoring","severity":"degraded","component_ids":["inference"],"started_at":"2026-11-19T09:00:00Z","resolved_at":null,"updates":[{"at":"2026-11-19T09:30:00Z","state":"monitoring","message":"Error rates are recovering."}]}
```

Component states are `operational|degraded|outage|maintenance|unknown`. Publish source and sample policy with every aggregate. Unpublished identifiers, private provider reports and tenant capacity stay inside the control plane. Privileged `POST A/status/incidents` and `POST A/status/incidents/{id}/updates` require `ops.incidents.publish`; send respectively `{title,severity,component_ids,message}` or `{state,message}` and receive the Incident above.

## Additional operational contracts

| Endpoint | Grant and request | Response data |
| --- | --- | --- |
| `POST W/byok`, `GET W/byok` | `credentials.byok.manage`, create `{"provider":"provider","auth_type":"api_key","secret":"<entered-value>","allowed_project_ids":["p1"],"allowed_key_ids":["k1"],"allowed_member_ids":["u1"],"allowed_model_ids":["m1"],"allow_platform_fallback":false,"include_external_in_budget":false}` | 201 or list of `{"id":"by1","owner_workspace_id":"w1","fingerprint":"fp_1","state":"unverified","version":"1","allow_platform_fallback":false,"scope":{"project_ids":["p1"],"key_ids":["k1"],"member_ids":["u1"],"model_ids":["m1"]}}` |
| `POST W/byok/{id}/verify`, `POST W/byok/{id}/rotate`, `POST W/byok/{id}/revoke` | Same grant; verify `{}`, rotate `{"replacement":{"secret":"<entered-value>"}}`, revoke `{"mode":"drain"}` | 202 Operation, metadata-only result |
| `POST A/restores/previews` | `ops.restore.preview`, `{"backup_id":"bk1","target":"isolated","expected_digest":"sha256:abc"}` | `{"id":"rp1","verified":true,"impact":{"databases":"1"},"expires_at":"2026-11-19T11:00:00Z"}` |
| `POST A/restores`, `GET A/restores/{id}` | `ops.restore.execute` + step-up for production, `{"preview_id":"rp1","target":"isolated","rehearsal_id":null}` | 202 on create, poll `{"id":"rs1","generation":"1","state":"running","stage":"verify_counts","target":"isolated","report":null}`. Production requires a successful rehearsal ID and a fresh production-target preview |
| `POST W/observability/exports` | `ops.export.configure`, `{"destination_id":"dest1","data_classes":["timings"],"sample_rate":"0.05","retention_days":7}` | 201 `{"id":"oe1","state":"validating","version":"1","destination_id":"dest1"}` |
| `POST A/catalog/verifications` | `catalog.verify`, `{"entry_id":"m1","adapter_version":"ad_3","action":"text.chat","account_mode":"api_key"}` | 202 Operation with `result:{evidence_id,state,verified_at,expires_at}` |
| `GET A/extensions` | `extensions.read`, cursor | Entries `{"id":"ex1","sdk_version":"2","publisher_id":"pub1","capabilities":["outbound_transport"],"state":"verified","digest":"sha256:abc"}` |

P2 extends BYOK with `PATCH W/byok/{id}` for grant/fallback fields using If-Match and the safe BYOK response above, and `GET W/byok/{id}/events` using the credential lifecycle list shape. `GET W/observability/exports/{id}` adds `last_success_at,last_error_reason,queue_depth` to the export configuration response. `PATCH W/observability/exports/{id}` accepts a new destination, data classes, sampling, or `{"enabled":false}` under If-Match and returns the updated configuration.

## Frontend compatibility

Reuse the existing console API client, cancellation and session refresh. Its response unwrapping still reads `data`; decode error `reason` and structured `metadata`. Each domain emits an OpenAPI fragment and request/response fixtures at implementation. Generate TS types from composed v2 OpenAPI. Contract tests compare handler responses to the same fixtures, including null, permission-denied, version-conflict, page end, insufficient sample, expired snapshot, duplicate mutation and unknown outcome.

Legacy v1 endpoints continue through compatibility adapters during migration. Existing v1 JSON numbers retain their historical representation. New v2 money always uses strings. Existing payment callback signatures, native inference error shapes, SDK streaming frames and WS turn semantics are exercised through their original routes.
