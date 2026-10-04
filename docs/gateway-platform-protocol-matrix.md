# Gateway platform and protocol matrix

This is a source contract for the local gateway, not a live vendor certification.
It describes the registered routes and their existing dispatch/qualification gates.
Registration, protocol conversion, account model evidence, streaming support and
billable completion are independent conditions. A static descriptor is only an
upper bound; no row grants an account permission or changes a selling price.

Customer examples are in the [API usage guide](usage-guide.md). Public-contract
corrections below were checked against `43e61400d` on 2026-10-03; they do not
recertify every historical platform claim or deployment.

## Existing concrete platforms

The table below retains its historical concrete-platform scope. Compatible
suppliers, regional products, modes and selectable plans are defined by the
[current registry](../backend/internal/domain/data/compatible_providers/index.json),
not a frozen ID count. Their exact bases, authentication, discovery and declared
bridges are in the [administrator provider reference](provider-configuration.md).
Supplier-family cards can group multiple route IDs without migrating accounts.
A registry entry is not an additional native media adapter. Copilot and Nous use
manual credentials without automatic OAuth or refresh; migration 283 is a
historical migration, not the current registry inventory.

`credentials.billing_plan` stores an operator-selected product, not proof of a
purchase, quota or model entitlement. Omitted updates retain it; null/empty clears
it. Independent transport modes (AtlasCloud Coding and Qiniu subscription) keep
their own endpoint/key contracts. Plan selections gate PAYG queries without
changing inference transport. See [plan configuration](provider-configuration.md#套餐选择保存与清除).

Manual administrator queries distinguish `wallet_balance`, `project_budget`,
`consumed_usage` and `unsupported`. AtlasCloud PAYG reads account balance;
Shengsuanyun enterprise returns project budget with period and unlimited state;
Qiniu PAYG returns consumed usage for an explicit day-granularity window (at most
31 days), preserving units such as kToken. Neither plan-only selections nor
AtlasCloud Coding/Qiniu subscription inherit these PAYG query permissions.
Missing readings are not zero. Local ledger usage, upstream wallet and upstream
plan quota are separate. These admin routes use administrator authentication:
`GET /api/v1/admin/cn-providers/accounts/:id/balance` and
`GET /api/v1/admin/cn-providers/accounts/:id/usage`; they are not public inference
endpoints. See [query contracts and errors](provider-configuration.md#手工余额查询).
Direct URL + key configuration and CC Switch client-side mapping are independent;
model discovery does not prove an inference action is authorized.

| Compatible ID / auth mode | Upstream / discovery | Public gateway behavior and limits |
| --- | --- | --- |
| `github_copilot` / `fine_grained_pat` (also the empty auth-mode default) | Chat POST `https://api.githubcopilot.com/v1/chat/completions`; catalog GET `https://api.githubcopilot.com/models` | Manual fine-grained PAT, Bearer, own `Copilot-Integration-Id: omnihive`; catalog-only `X-GitHub-Api-Version: 2026-07-01`. Messages, Responses and Gemini bridge to Chat. |
| `nous_research` / `invocation_token` | Chat POST `https://inference-api.nousresearch.com/v1/chat/completions`; catalog GET `https://inference-api.nousresearch.com/v1/models` | Manual invocation JWT, Bearer; replace on expiry, no automatic refresh. Messages, Responses and Gemini bridge to Chat. |
| `nous_research` / `official_proxy` | Chat POST `{explicit-prefix}/chat/completions`; catalog GET `{explicit-prefix}/models` | Explicit portable operator URL prefix, no automatic `/v1`; optional operator frontdoor Bearer key, empty sends no Authorization. Same Chat bridges. |

These are two IDs with one internal `account_mode=payg` each and three authentication choices; routing `payg` does not establish vendor billing. Special endpoint hooks control catalog URLs, so a generic Chat-base join is incorrect for Copilot. Offline contract tests cover streaming, nonstreaming and tools through the public bridges; live admission requires an authorized operator key and is not vendor certification. Use single-account editing for mode changes, including referenced credentials; changes require an explicit fresh secret or explicit clearing for a keyless proxy, while same-mode edits preserve account-bound secret references. No client token-store access, shared refresh grant, balance probe, media adapter or local-only dependency is part of these contracts.

For native Tencent Intl plans, personal and enterprise share Chat/Messages hosts but personal must not inherit enterprise Responses. Enterprise Pro/Lite requires explicit `api_base_urls.responses=https://tokenhub-intl.tencentcloudmaas.com/plan/v3`; CN enterprise retains its separate default. RelayCode remains unresolved independently of implemented RelaxyCode; ETok.ai is a historically removed preset, not a shutdown claim.

Composite is a group routing mode, not an account platform.
N = native path; B = protocol bridge; conditional
means the account mode, model, measured endpoint evidence and request gates must
also allow it. Stream/tool entries describe implemented paths, not every model.

| Platform | Account forms / deployment boundary | Chat and tools | Responses POST / WS / compact | Stream and qualification limits |
| --- | --- | --- | --- | --- |
| anthropic | API key, OAuth/setup-token, service account; legacy Bedrock account remains here | Messages N; Chat B | B / rejected / rejected | Claude stream/tool conversion; old Bedrock credentials are not migrated |
| gemini | API key and OAuth scopes | Gemini N; Messages/Chat compatibility depends on adapter | descriptor rejects / rejected / rejected | generateContent, streamGenerateContent and countTokens individually qualified |
| openai | API key, OAuth/Codex; shadow requires credential parent | N; tools depend on model | N / N / conditional | Native or configured chat bridge; catalog evidence and account capability gates remain mandatory |
| antigravity | OAuth and supported API-key deployment | CloudCode/Gemini conversion | B / rejected / rejected | Forced routes remain distinct; mixed scheduling is an account setting |
| grok | API key and OAuth have different transports | N or OAuth adapter | N / B / rejected | Images/video/voice/search are separately gated actions, not implied by text support |
| kimi | API key, PAYG versus Coding profile | Account-selected protocol; tools conditional | N conditional / rejected / rejected | Moonshot and Kimi Coding specifications are not interchangeable |
| zhipu | API key, PAYG/Coding | Chat N; Messages compatibility | B / rejected / rejected | Exact deployment/model and endpoint evidence required |
| deepseek | API key | Chat N; tools conditional | N conditional / rejected / rejected | Explicit account protocol decides native Responses versus chat bridge |
| minimax | API key, PAYG/Coding | Account-selected protocol | N conditional / rejected / rejected | Native protocol and model qualification decide effective path |
| opencode_go | API key, Go/Zen | Model-selected native protocol | N conditional / rejected / rejected | Provider-qualified IDs are retained |
| cursor | OAuth/upstream account forms | Cursor conversion | B / rejected / rejected | Cursor-specific count-token estimate and model mapping; no generic provider grant |
| qianwen | API key, domestic/international and account mode | Compatible chat and bounded native actions | N conditional / rejected / rejected | Native text/image/audio/video/files actions have separate gates below |
| commandcode | API key | Model-selected native protocol | B / rejected / rejected | Keep model routing and upstream IDs; no static capability-based grant |
| volcengine | Runtime API key, exact deployment/ep target | Chat and native Responses; Messages B; tools conditional | N / rejected / rejected | Public Messages bridges through the existing text trunk; absence of a native PAYG Messages URL does not reject the public bridge |
| qianfan | Domestic API key, v2 base | Chat and native Responses; Messages B; tools conditional | N / rejected / rejected | Public Messages bridges through the existing text trunk; the native /v2/messages result does not determine public Messages support |
| hunyuan | Compatibility API key | Nonstream chat/tools; native Responses and Anthropic Messages on the same TokenHub site | N / rejected / rejected | Unverified SSE is rejected before upstream; no invented discovery or balance; the Anthropic path authenticates with x-api-key |
| mistral | API key, original IDs including fine-tuned IDs | Chat N; Messages B; tools conditional | B / rejected / rejected | Public Messages/Responses bridge to Chat; no native Messages/Responses URL is implied; missing usage cannot settle as trusted zero |
| openrouter | API key only; Bearer at account base URL, default https://openrouter.ai/api/v1 | Raw Chat N; Messages N; Google generateContent B through Chat; tools conditional | N / rejected / rejected | Nonstream/SSE text; adaptive or matching fixed protocol, exact provider/variant IDs; passive monitoring only; media limits below |
| bedrock | Regional runtime API key or IAM SigV4 | Claude Invoke or non-Claude Converse; tools conditional | B / rejected / rejected | Messages/Responses/Chat POST admitted; ConverseStream/Claude streams; region/profile/ARN and catalog evidence still required |

The current action dispatcher resolves the concrete target and its registered
adapter per action, then retains account/model qualification. A provider's lack
of a native Messages or Responses URL does not by itself reject an implemented
public bridge. Unsupported actions are rejected; error envelopes follow the
public protocol and the selected handler rather than one universal body shape.

## Coding quota and model synchronization

Domestic Ark Coding personal (reference implementation, not verified online) / team uses the existing administrator route
`GET /api/v1/admin/cn-providers/accounts/:id/quota`. Query credentials are
`volcengine_access_key_id` and `volcengine_secret_access_key`, independent of the
inference key and never echoed back. Omitted updates preserve them; explicit null
clears them. `volcengine_plan_edition` is personal/team (default personal), and
`volcengine_seat_id` is required for team queries; no automatic GetSeatInfo lookup. The domestic Coding profile is eligible; BytePlus personal and personal Ark Agent queries remain unverified. BytePlus Team uses existing billing_plan=team plus separate BytePlus cloud keys and an explicit seat, against its fixed international control plane. Domestic team Agent requires an explicit seat and separate AK/SK; reported daily/5h/week/month windows remain independent.

Returned 5h/weekly/monthly windows contain used percentages and optional reset
times, not absolute used/total or cash. Missing windows and failed queries are not
zero; failures retain the last successful snapshot with its original time. Check
success and status, including missing_credentials, invalid_credentials,
permission_denied, unsupported_profile, no_verified_query, not_subscribed, no_seat,
invalid_response, upstream_error and url_rejected; HTTP 200 alone is insufficient.
The control-plane host is fixed and signed with query AK/SK; it does not receive
inference credentials or inherit inference header overrides. See the
[configuration and status contract](provider-configuration.md#火山-ark-coding-套餐用量).

Model synchronization distinguishes configuration, credentials, account type,
unadapted platform, unconfirmed discovery, 404 endpoint, 405 method, 403 permission,
429 rate limit, network, upstream and internal errors. A disabled discovery mode
is unconfirmed unless explicit supplier evidence establishes absence; no model
endpoint is guessed. Failure preserves saved mappings, whitelist, selection and
last successful catalog. Only actual discovered_models are discovery evidence;
candidate_models and platform presets remain candidates, and selecting one is
manual configuration, not discovery success. Candidates cannot alone establish a
ready catalog or populate eligible_models. See [model synchronization](provider-configuration.md#模型同步候选与失败处理).


The account query UI also mounts Atlas daily model usage alongside its PAYG wallet, Azure public-cloud subscription/region quotas, GitHub personal billing and Together organization billing. These are separate scopes, never interchangeable wallet balances. Azure/GitHub use optional independent management tokens; missing setup remains saveable and causes zero outbound query traffic. Tencent domestic enterprise Token Plan uses separate cloud Secret ID/Key plus region/team, preserving exact package/cycle/billing strings and raw item units. MiniMax's new CN Token Plan only applies to its exact official profile; no cross-region fallback. See [configuration and evidence](provider-configuration.md#独立管理查询与新增套餐).

AiHubMix, DMX China and Qiniu PAYG now use verified OpenAI-compatible model listing. Other DMX regions and Qiniu subscription remain unconfirmed. Candidate models and manual declarations do not count as discovered or account-eligible models; failed sync/save preserves mappings.


## Core endpoint contracts

| Method | Path / aliases | Effective qualification | Nonstream, stream and tools |
| --- | --- | --- | --- |
| POST | /v1/messages; /messages | Existing OpenAI-compatible dispatch or Gateway Messages; Hunyuan binds the native Anthropic path on the published MessagesPOST fact; OpenRouter binds native Messages with adaptive or matching protocol | Anthropic envelope; stream/tools adapter-specific |
| POST | /v1/messages/count_tokens; /messages/count_tokens | OpenAI bridge, Grok/Cursor estimates, existing Anthropic-compatible path; new restricted platforms reject | Token count, no generation bill; not a generic Messages grant |
| POST | /v1/chat/completions; /chat/completions | Concrete or resolved Composite target, exact catalog endpoint evidence | Native/bridge; Hunyuan stream explicitly unsupported |
| POST | /v1/responses; /responses; /backend-api/codex/responses | Descriptor plus actual account mode/adapter; a chat-only platform whose row publishes ResponsesPOST binds the shared responses trunk, the rest reject | Native/bridge with protocol-specific tools and SSE |
| GET | Same Responses paths | WebSocket dispatcher, account WS qualification | OpenAI native, Grok bridge; no universal WS support |
| POST | Responses /compact | Existing forwardable subpath and compact-account gate | OpenAI compact only; not equivalent to normal Responses |
| POST | Responses /input_tokens | Existing token-count subpath; native/compatible handler dispatch; OpenRouter uses a local estimate without an upstream count request | Input-token result, no normal generation claim |
| POST | Responses other subpaths | IsForwardableOpenAIResponsesRequestPath | Unknown/action-unsafe paths reject before scheduling |
| POST | /v1/embeddings; /embeddings | Platform action matrix: OpenAI, Bedrock, Qianwen, Qianfan, Hunyuan, Volcengine, Mistral and OpenRouter bind an embeddings adapter; other platforms retain their action gates | No streaming/tools promise |
| POST | /v1/rerank; /rerank | Platform action matrix: Qianwen, Qianfan, Bedrock and OpenRouter bind a rerank adapter; OpenAI and other platforms retain their action gates | Ranked results, no generation claim |
| POST | /v1/images/variations; /images/variations | Platform action matrix: OpenAI and Bedrock bind a variation adapter; chat-only platforms reject | OpenAI variation shape only |
| GET | /v1/models; /models | Existing membership and allowlist first | Optional capabilities below; no generation |
| GET | /v1/models/:model; /models/:model | Exact visible entry or model_not_found | Same fields as listing |
| GET | models with nonempty client_version; /backend-api/codex/models | Existing Codex manifest branch | Original manifest/ETag shape, no capabilities addition |

## Proprietary actions (not a wildcard support claim)

The method/path inventory below records wildcard *registrations*. Actual actions
are enumerated here. A wildcard registration never means all vendor APIs exist.

| Method | Action path | Existing behavior and limits |
| --- | --- | --- |
| POST | /v1beta/models/{model}:generateContent | Native Gemini/Antigravity/OpenCode adapters or the registered public Google bridge; underlying text action and model remain qualified |
| POST | /v1beta/models/{model}:streamGenerateContent | Same qualified model and adapter; use alt=sse for SSE; no universal streaming grant |
| POST | /v1beta/models/{model}:countTokens | Gemini scope/fallback-specific count; no inference support implied |
| POST | /v1beta/models/{model}:embedContent; :batchEmbedContents | Public Gemini bridge converts to embeddings where that action is bound and qualified; not a universal native Gemini grant |
| POST | /v1beta/models/{model}:unknown | Existing action whitelist rejects; not an extension mechanism |
| POST | /v1/alpha/search; /alpha/search; /backend-api/codex/alpha/search | OpenAI/Codex-specific qualification |
| POST | /v1/live; /backend-api/codex/realtime/calls | OpenAI Live account capability and session model; multipart supported by existing path |
| GET | /v1/live/:call_id; /backend-api/codex/:call_id | Live sideband ownership/capability checks |
| POST | /v1/images/generations | Registered image.generate adapter after target/model qualification; includes OpenAI/Grok and separately bound provider adapters, not the narrower async subset |
| POST | /v1/images/edits | Registered image.edit adapter; OpenAI/Grok and other separately bound providers have distinct JSON/multipart contracts |
| POST | /v1/images/generations/async | Existing persistent asynchronous image service |
| POST | /v1/images/edits/async | Existing persistent asynchronous edit service |
| GET | /v1/images/tasks/:task_id | Owned image task state, no new inference |
| POST | /v1/images/batches | Existing batch image job service; per-item qualification |
| GET | /v1/images/batches | Owned batch list |
| GET | /v1/images/batches/models | Batch-specific eligible models |
| GET | /v1/images/batches/:id | Owned batch detail |
| GET | /v1/images/batches/:id/items | Owned item list |
| GET | /v1/images/batches/:id/items/:custom_id/content | Owned item output |
| GET | /v1/images/batches/:id/download | Owned batch output archive |
| POST | /v1/images/batches/:id/cancel | Existing cancellation lifecycle, no refund inference |
| DELETE | /v1/images/batches/:id | Owned record deletion |
| DELETE | /v1/images/batches/:id/outputs | Owned output deletion |
| POST | /v1/videos; /v1/videos/generations | Grok creation; OpenAI sora-2 and OpenRouter owner-bound job submission through the shared upstream job trunk |
| POST | /v1/videos/edits | Grok video edit; OpenAI edit inherits the owner-checked source video |
| POST | /v1/videos/extensions | Grok video extension; OpenAI extension bills the documented segment |
| GET | /v1/videos/:request_id | Grok status lookup; owner-bound OpenAI or OpenRouter durable job state |
| GET | /v1/videos/:request_id/content | Grok content lookup; owner-bound OpenAI or OpenRouter content stream |
| GET | /v1/videos/generations/:request_id | Generation status |
| GET | /v1/videos/generations/:request_id/content | Generation content |
| GET | /v1/videos/edits/:request_id | Edit status |
| GET | /v1/videos/edits/:request_id/content | Edit content |
| GET | /v1/videos/extensions/:request_id | Extension status |
| GET | /v1/videos/extensions/:request_id/content | Extension content |
| POST | /v1/tts; /tts | Grok TTS, account-specific native transport |
| POST | /v1/stt; /stt | Grok STT, account-specific native transport |
| POST | /v1/audio/speech; /audio/speech | Hunyuan, Qianfan, Qianwen, Mistral and OpenRouter bind speech adapters; schemas/output differ (Qianwen WAV, Mistral may return base64 JSON); Grok uses its dedicated /v1/tts entry; OpenAI speech has no action binding |
| POST | /v1/audio/transcriptions; /audio/transcriptions | Hunyuan, Qianwen, Mistral and OpenRouter bind recognition adapters with platform-specific multipart/format limits; OpenAI transcription has no action binding |
| POST | /v1/audio/translations; /audio/translations | OpenAI audio translation adapter; every other platform rejects before scheduling |
| POST | /v1/custom-voices; /custom-voices | Grok custom voice creation |
| GET | /v1/custom-voices; /custom-voices | Grok custom voice list |
| GET | /v1/custom-voices/:voice_id; /custom-voices/:voice_id | Grok voice metadata |
| GET | /v1/custom-voices/:voice_id/audio; /custom-voices/:voice_id/audio | Grok voice preview audio |
| PATCH | /v1/custom-voices/:voice_id; /custom-voices/:voice_id | Grok voice update |
| DELETE | /v1/custom-voices/:voice_id; /custom-voices/:voice_id | Grok voice deletion |
| GET | /v1/realtime; /realtime | Grok realtime websocket |
| POST | /v1/web_search; /web_search | Grok web search |
| POST | /v1/x_search; /x_search | Grok X search |
| POST | /api/v1/services/aigc/text-generation/generation | Qianwen native text conversations, exact request contract |
| POST | /api/v1/services/aigc/multimodal-generation/generation | Qianwen multimodal conversation; bounded synchronous Qwen image generation/edit and native audio model subsets |
| POST | /api/v1/services/aigc/video-generation/video-synthesis | Qianwen persistent video job; explicit rate and owned task required |
| POST | /api/v1/services/aigc/video-generation/3d-generation | Qianwen persistent 3D job; no synchronous completion fiction |
| POST | /api/v1/services/audio/asr/transcription | Qianwen persistent ASR job |
| GET | /api/v1/tasks/{task_id} | Owned durable Qianwen video/3D/ASR state; client polling does not create another job |
| GET | /api-ws/v1/realtime | Bounded Qianwen TTS realtime subset; model/usage/settlement gates, not generic Omni support |
| POST | /api/v1/realtime | Structured unsupported gate |
| POST | /api/v1/apps/{app_id}/completion | RAG ownership/billing absent: structured unsupported gate |
| POST | /api/v1/indices/knowledge/search | Knowledge/RAM ownership absent: structured unsupported gate |
| POST | /compatible-mode/v1/files | Owned Qianwen batch input upload when job service is configured; otherwise structured gate |
| GET | /compatible-mode/v1/files | Owned file list with bounded pagination |
| GET | /compatible-mode/v1/files/{id} | Owned file metadata |
| GET | /compatible-mode/v1/files/{id}/content | Owned bounded content |
| DELETE | /compatible-mode/v1/files/{id} | Owned file deletion |
| POST | /compatible-mode/v1/batches | Persistent owned batch submission; per-custom_id settlement |
| GET | /compatible-mode/v1/batches | Owned batch list |
| GET | /compatible-mode/v1/batches/{id} | Owned batch state |
| POST | /compatible-mode/v1/batches/{id}/cancel | Existing durable cancellation path |
| POST | /api/v1/files | Native alias remains unsupported; not the compatible file upload |
| POST | /api/v1/batches | Native alias remains unsupported; not the compatible batch submission |

## OpenRouter protocol and media limits

OpenRouter is an API-key account, with Bearer authentication through the configured
base URL and account egress. OAuth, setup-token, IAM/Bedrock and service-account
credentials do not acquire OpenRouter adapters. Exact model IDs, including provider
prefixes and colon variants, survive account mapping; group/key/model admission
still applies. Missing discovery metadata does not by itself reject a manually
configured exact model. Static support is not a guarantee of model entitlement or
paid provider verification.

Adaptive text accounts select native Messages, native Responses or raw Chat for
the caller's protocol; a conflicting fixed protocol is rejected locally. Native
Messages and Responses preserve opaque reasoning/signature content in client-sent
history. Google generateContent and streamGenerateContent use Chat compatibility,
including the source-bound opaque thoughtSignature continuation carrier; they do
not call native Google endpoints. The relay does not invent `store` or
`previous_response_id`: `store` alone is relayed, while public Responses continuation
still passes the existing ownership/account gate and does not grant OpenRouter
server-side state support. Responses WebSocket, compact, Messages count_tokens,
Google countTokens and Live are unsupported; Responses input_tokens is only a local
estimate. OpenRouter remains passive-only for channel monitoring, with no active
probe or quota-monitor write grant. Account key-usage observation does not change
those monitor permissions.

Eight media action cells are implemented, using the registered routes above:

| Action | OpenRouter contract |
| --- | --- |
| image.generate | Synchronous raster generation; public images/generations maps to upstream images. Output is b64_json with png, jpeg or webp; optional n is an integer from 1 to 10. Image URL output, SVG and SSE are rejected. |
| audio.tts | Speech accepts mp3 or pcm and returns nonempty audio. Other output formats and SSE are rejected; quantity is submitted text characters. |
| audio.stt | One multipart file up to 25 MB; json or verbose_json output. JSON/base64/remote-URL input and text/srt/vtt output are unsupported; a successful result requires measured positive usage.seconds. |
| embeddings | Float or base64 vectors, with finite values, consistent dimensions and valid result indices; observed prompt tokens are required for settlement. |
| rerank | Query/documents/top_n contract with finite scores and valid unique indices; observed total tokens, not search_units, supply the token quantity. |
| video.generate | Durable owner-bound submission with exact model and frozen pricing; a submission acknowledgement never settles, and an idempotent repeat reuses the job. previous_job_id and video continuation inputs are rejected. |
| video.status | Pending/in-progress stay uncharged; completed can settle once. Failed/cancelled/expired jobs do not debit. Automatic settlement requires the frozen explicit group/channel per-request tariff, finite nonnegative upstream cost and one validated artifact; otherwise completion remains unpriced/needs_review with no debit. An explicit zero tariff is valid. |
| video.content | Owner-bound succeeded job only; reloads the active API-key account and checks unchanged egress. Downloads use the validated account base and escaped job ID, never stored polling/result URLs; redirects and nonzero content index are rejected. Content must be video, bounded at 512 MiB. |

Image edits/variations, audio translation, video edits/extensions/cancellation and
other unbound actions remain unsupported. Synchronous media validates actual output
and measured quantity before recording usage; provider `usage.cost` is not the
customer tariff. Video requested seconds are translated to duration and do not
establish per-second billing. Completed video needing settlement review can still
serve owned content. These are local implementation contracts, not live model or
credential certification.

## Public Models projection

Ordinary catalog-backed entries expose known guaranteed facts as optional
top-level fields after authorization: `context_window`, `context_length`,
`max_input_tokens`, `max_output_tokens`, `supports_reasoning`,
`supported_reasoning_levels`, `supports_function_calling`, `input_modalities`
and `output_modalities`. They do not contain the rich `capabilities` object.
The separate `/v1/models/catalog` resource contains capabilities with
`guaranteed`, `available`, `unknown` and source evidence, plus aliases, model
version and authorized endpoint hints. Missing facts are unknown, not false or
zero; metadata cannot add models to the authorized set. These facts do not
guarantee available capacity or successful execution.

Stored specifications reuse the existing directory account read per request;
Composite ownership and credential-parent lookups retain their existing behavior.
Each account metadata snapshot is decoded once, independent of model count. The
read does not refresh registries, call upstream discovery, write storage or alter
qualification. Metadata-only rows cannot enter the list. Legacy repositories and
fallback lists retain their original form. Pinned upstream Models and Codex
client_version manifests retain their existing fields and ETag behavior. Native
Gemini and dedicated Antigravity lists retain their existing serializers.

Capability intersection describes stored facts for the exits accepted by the
listing's first qualifying endpoint. It does not assert that every protocol is
available. Unmatched identity profiles and shadow metadata are conservatively omitted.
Neither `supported_methods` nor metadata completeness is published as endpoint
permission. The original model ID, order and every existing field remain intact.

## Diagnostics

Public error writers keep their protocol envelopes. Ops logging reads those
errors through parseOpsErrorResponse / parseOpsSSEFailure, then uses the existing
normalization and owner/source classification; administrative DTOs consume the
stored Ops error fields. SSE and JSON reuse opsJSONScalarString for code conversion.
Account attribution stays in request context / administrative diagnostics. There
is no new error-code namespace, trace field, diagnostic storage or public raw body.

## Future scope

| Product | State |
| --- | --- |
| WorkBuddy domestic | Not registered; host/auth/model/usage/points and account evidence remain future work |
| WorkBuddy international | Not registered; separate region contract and explicit authorization still required |

## Verification and limits

`TestGatewayProtocolMatrixDocumentation` compares every actual Gin registration
(method plus full path) to the inventory below and checks the concrete platform set.
`TestGatewayUnsupportedProtocolContracts` sends streaming/nonstreaming bodies,
with and without tools, through the real registered restricted-platform routes;
rejection must occur before transport or billing. Existing Composite, pinned,
Codex, Grok and resource tests are included under the matrix roots. Account model
binding tests separately cover missing/incorrect endpoint evidence across account
forms; they do not call a vendor or certify supported stream/tool semantics.
The platform flow fixtures in internal/service cover the concrete adapters.
No local fixture result is a real-account/vendor, deployment or production pass.

## Registered method/path inventory

This appendix intentionally includes registered but locally rejected operations.
It is an exact route inventory, not an assertion that every platform can execute
these methods. The action and platform tables above constrain it.

| Registered route | Contract scope |
| --- | --- |
| `DELETE /api-ws/v1/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `DELETE /api/v1/apps` | DashScope exact action/resource gate; see individual actions above |
| `DELETE /api/v1/apps/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `DELETE /api/v1/batches` | DashScope exact action/resource gate; see individual actions above |
| `DELETE /api/v1/batches/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `DELETE /api/v1/files` | DashScope exact action/resource gate; see individual actions above |
| `DELETE /api/v1/files/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `DELETE /api/v1/indices` | DashScope exact action/resource gate; see individual actions above |
| `DELETE /api/v1/indices/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `DELETE /api/v1/realtime` | DashScope exact action/resource gate; see individual actions above |
| `DELETE /api/v1/realtime/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `DELETE /api/v1/services/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `DELETE /api/v1/tasks` | DashScope exact action/resource gate; see individual actions above |
| `DELETE /api/v1/tasks/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `DELETE /compatible-mode/v1/batches` | DashScope exact action/resource gate; see individual actions above |
| `DELETE /compatible-mode/v1/batches/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `DELETE /compatible-mode/v1/files` | DashScope exact action/resource gate; see individual actions above |
| `DELETE /compatible-mode/v1/files/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `DELETE /custom-voices/:voice_id` | Existing gateway handler; platform, account and endpoint qualification |
| `DELETE /images/batches/:id` | Existing gateway handler; platform, account and endpoint qualification |
| `DELETE /images/batches/:id/outputs` | Existing gateway handler; platform, account and endpoint qualification |
| `DELETE /v1/custom-voices/:voice_id` | Existing gateway handler; platform, account and endpoint qualification |
| `DELETE /v1/images/batches/:id` | Existing gateway handler; platform, account and endpoint qualification |
| `DELETE /v1/images/batches/:id/outputs` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /antigravity/models` | Dedicated/forced Antigravity path; original serializer and qualification |
| `GET /antigravity/v1/models` | Dedicated/forced Antigravity path; original serializer and qualification |
| `GET /antigravity/v1/usage` | Dedicated/forced Antigravity path; original serializer and qualification |
| `GET /antigravity/v1beta/models` | Dedicated/forced Antigravity path; original serializer and qualification |
| `GET /antigravity/v1beta/models/:model` | Dedicated/forced Antigravity path; original serializer and qualification |
| `GET /api-ws/v1/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `GET /api/v1/apps` | DashScope exact action/resource gate; see individual actions above |
| `GET /api/v1/apps/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `GET /api/v1/batches` | DashScope exact action/resource gate; see individual actions above |
| `GET /api/v1/batches/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `GET /api/v1/files` | DashScope exact action/resource gate; see individual actions above |
| `GET /api/v1/files/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `GET /api/v1/indices` | DashScope exact action/resource gate; see individual actions above |
| `GET /api/v1/indices/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `GET /api/v1/realtime` | DashScope exact action/resource gate; see individual actions above |
| `GET /api/v1/realtime/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `GET /api/v1/services/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `GET /api/v1/tasks` | DashScope exact action/resource gate; see individual actions above |
| `GET /api/v1/tasks/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `GET /backend-api/codex/:call_id` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /backend-api/codex/models` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /backend-api/codex/responses` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /compatible-mode/v1/batches` | DashScope exact action/resource gate; see individual actions above |
| `GET /compatible-mode/v1/batches/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `GET /compatible-mode/v1/files` | DashScope exact action/resource gate; see individual actions above |
| `GET /compatible-mode/v1/files/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `GET /custom-voices` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /custom-voices/:voice_id` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /custom-voices/:voice_id/audio` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /images/batches` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /images/batches/:id` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /images/batches/:id/download` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /images/batches/:id/items` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /images/batches/:id/items/:custom_id/content` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /images/batches/models` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /images/tasks/:task_id` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /live/:call_id` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /models` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /models/:model` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /models/catalog` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /models/catalog/:model` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /realtime` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /responses` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /sub2api/billing` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /usage` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/custom-voices` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/custom-voices/:voice_id` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/custom-voices/:voice_id/audio` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/images/batches` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/images/batches/:id` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/images/batches/:id/download` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/images/batches/:id/items` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/images/batches/:id/items/:custom_id/content` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/images/batches/models` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/images/tasks/:task_id` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/live/:call_id` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/models` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/models/catalog` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/models/catalog/:model` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/models/:model` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/realtime` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/responses` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/sub2api/billing` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/usage` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/videos/:request_id` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/videos/:request_id/content` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/videos/edits/:request_id` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/videos/edits/:request_id/content` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/videos/extensions/:request_id` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/videos/extensions/:request_id/content` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/videos/generations/:request_id` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1/videos/generations/:request_id/content` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1beta/models` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /v1beta/models/:model` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /videos/:request_id` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /videos/:request_id/content` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /videos/edits/:request_id` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /videos/edits/:request_id/content` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /videos/extensions/:request_id` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /videos/extensions/:request_id/content` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /videos/generations/:request_id` | Existing gateway handler; platform, account and endpoint qualification |
| `GET /videos/generations/:request_id/content` | Existing gateway handler; platform, account and endpoint qualification |
| `PATCH /api-ws/v1/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `PATCH /api/v1/apps` | DashScope exact action/resource gate; see individual actions above |
| `PATCH /api/v1/apps/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `PATCH /api/v1/batches` | DashScope exact action/resource gate; see individual actions above |
| `PATCH /api/v1/batches/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `PATCH /api/v1/files` | DashScope exact action/resource gate; see individual actions above |
| `PATCH /api/v1/files/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `PATCH /api/v1/indices` | DashScope exact action/resource gate; see individual actions above |
| `PATCH /api/v1/indices/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `PATCH /api/v1/realtime` | DashScope exact action/resource gate; see individual actions above |
| `PATCH /api/v1/realtime/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `PATCH /api/v1/services/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `PATCH /api/v1/tasks` | DashScope exact action/resource gate; see individual actions above |
| `PATCH /api/v1/tasks/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `PATCH /compatible-mode/v1/batches` | DashScope exact action/resource gate; see individual actions above |
| `PATCH /compatible-mode/v1/batches/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `PATCH /compatible-mode/v1/files` | DashScope exact action/resource gate; see individual actions above |
| `PATCH /compatible-mode/v1/files/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `PATCH /custom-voices/:voice_id` | Existing gateway handler; platform, account and endpoint qualification |
| `PATCH /v1/custom-voices/:voice_id` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /alpha/search` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /antigravity/v1/messages` | Dedicated/forced Antigravity path; original serializer and qualification |
| `POST /antigravity/v1/messages/count_tokens` | Dedicated/forced Antigravity path; original serializer and qualification |
| `POST /antigravity/v1beta/models/*modelAction` | Dedicated/forced Antigravity path; original serializer and qualification |
| `POST /api-ws/v1/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `POST /api/v1/apps` | DashScope exact action/resource gate; see individual actions above |
| `POST /api/v1/apps/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `POST /api/v1/batches` | DashScope exact action/resource gate; see individual actions above |
| `POST /api/v1/batches/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `POST /api/v1/files` | DashScope exact action/resource gate; see individual actions above |
| `POST /api/v1/files/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `POST /api/v1/indices` | DashScope exact action/resource gate; see individual actions above |
| `POST /api/v1/indices/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `POST /api/v1/realtime` | DashScope exact action/resource gate; see individual actions above |
| `POST /api/v1/realtime/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `POST /api/v1/services/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `POST /api/v1/tasks` | DashScope exact action/resource gate; see individual actions above |
| `POST /api/v1/tasks/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `POST /audio/speech` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /audio/transcriptions` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /audio/translations` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /backend-api/codex/alpha/search` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /backend-api/codex/realtime/calls` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /backend-api/codex/responses` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /backend-api/codex/responses/*subpath` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /chat/completions` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /compatible-mode/v1/batches` | DashScope exact action/resource gate; see individual actions above |
| `POST /compatible-mode/v1/batches/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `POST /compatible-mode/v1/files` | DashScope exact action/resource gate; see individual actions above |
| `POST /compatible-mode/v1/files/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `POST /custom-voices` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /embeddings` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /images/batches` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /images/batches/:id/cancel` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /images/edits` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /images/edits/async` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /images/generations` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /images/generations/async` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /images/variations` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /live` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /messages` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /messages/count_tokens` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /rerank` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /responses` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /responses/*subpath` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /stt` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /tts` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/alpha/search` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/audio/speech` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/audio/transcriptions` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/audio/translations` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/chat/completions` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/custom-voices` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/embeddings` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/images/batches` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/images/batches/:id/cancel` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/images/edits` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/images/edits/async` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/images/generations` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/images/generations/async` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/images/variations` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/live` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/messages` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/messages/count_tokens` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/rerank` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/responses` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/responses/*subpath` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/stt` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/tts` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/videos` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/videos/edits` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/videos/extensions` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/videos/generations` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/web_search` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1/x_search` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /v1beta/models/*modelAction` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /videos` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /videos/edits` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /videos/extensions` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /videos/generations` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /web_search` | Existing gateway handler; platform, account and endpoint qualification |
| `POST /x_search` | Existing gateway handler; platform, account and endpoint qualification |
| `PUT /api-ws/v1/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `PUT /api/v1/apps` | DashScope exact action/resource gate; see individual actions above |
| `PUT /api/v1/apps/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `PUT /api/v1/batches` | DashScope exact action/resource gate; see individual actions above |
| `PUT /api/v1/batches/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `PUT /api/v1/files` | DashScope exact action/resource gate; see individual actions above |
| `PUT /api/v1/files/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `PUT /api/v1/indices` | DashScope exact action/resource gate; see individual actions above |
| `PUT /api/v1/indices/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `PUT /api/v1/realtime` | DashScope exact action/resource gate; see individual actions above |
| `PUT /api/v1/realtime/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `PUT /api/v1/services/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `PUT /api/v1/tasks` | DashScope exact action/resource gate; see individual actions above |
| `PUT /api/v1/tasks/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `PUT /compatible-mode/v1/batches` | DashScope exact action/resource gate; see individual actions above |
| `PUT /compatible-mode/v1/batches/*nativePath` | DashScope exact action/resource gate; see individual actions above |
| `PUT /compatible-mode/v1/files` | DashScope exact action/resource gate; see individual actions above |
| `PUT /compatible-mode/v1/files/*nativePath` | DashScope exact action/resource gate; see individual actions above |

## Streaming interruption, terminal frames and the retry boundary

No registered provider stream offers a universal resume. Once a streaming
response has started, the gateway cannot switch upstream and continue the same
client stream: the HTTP status is already committed as `200`.

- When a stream breaks after the first committed byte (upstream read error,
  failover exhaustion, deadline), the gateway appends a protocol-standard terminal
  frame in place: `event: response.failed` for `/v1/responses` (with a `response`
  object whose `status` is `failed`), `event: error` for OpenAI chat completions,
  and the Anthropic-shaped `event: error`/`data: {"type":"error",...}` frame for
  `/v1/messages`. The client must treat that frame as the end of the stream.
- Partial output is never replayed as if it were the complete answer. A client
  that wants the full response has to send a new request; the gateway cannot
  promise that the upstream did not already bill the interrupted attempt.
- Do not assume `Idempotency-Key` deduplicates public generation requests.
  `GatewayGenerationIdempotency` exists with isolated tests, but the current
  production gateway route chain does not install it. Its replay/409 behavior
  is therefore not a public generation guarantee. Resource-specific contracts
  such as image-batch submission remain separate; a new generation may incur
  another upstream action and charge.
- The gateway does not buffer unbounded SSE to make interrupted streams replayable,
  and it does not claim exactly-once upstream side effects.

## Public model catalog migration

Ordinary `/v1/models` and `/v1/models/:model` use OpenAI model entries by default,
including for non-OpenAI providers. `anthropic-version` selects Anthropic entries
and list pagination (`limit`, `after_id`, `before_id`; `after`/`before` are aliases).
Both minimal and rich `/v1/models/catalog` (and `/v1/models/catalog/:model`) resolve
one authorized revision; rich cursors bind that revision and Key. Refresh after a
cursor invalidation. `catalog` is reserved; use the rich single-model route to
retrieve a model whose public ID is `catalog`. Responses are private and no-store.
Rich capabilities, aliases and uncertainty belong to the rich catalog; ordinary
entries also carry the known top-level client facts described above. Creation time
is omitted when no trusted persisted value exists, with rich `created_at_source`
set to `unavailable`; no refresh or account timestamp is substituted.

Codex manifests use `/backend-api/codex/models`. Nonempty `client_version` on the
ordinary collection currently delegates through `GatewayModelsHandler`.
The 2027-03-21 cutoff exists in a separate legacy helper that is not called by
that production dispatcher; do not promise automatic expiry or Sunset/Link
headers from that helper. User-Agent does not select a manifest.
A pinned Codex manifest does not establish ordinary model
visibility. Gemini keeps native query `key` authentication on `/v1beta/`; header
`x-goog-api-key` is recommended. Query keys are unsupported on `/v1`.

A public ID containing `/` must encode that delimiter as `%2F` in the single
model-ID path segment, for example `/v1/models/provider%2Fmodel` or
`/v1/models/catalog/provider%2Fmodel`. Literal `/` continues to separate route
segments. Gin matches the raw encoded path and then decodes parameter values;
this engine-wide rule also applies to non-model path parameters. Static route
segments must use their documented canonical spelling (write `/models`, not
`/mo%64els`). Resource handlers retain their existing ID validation.

### Resource and native action entries (2026-09-28)

| Method/path | Qualification |
| --- | --- |
| `DELETE /files/:file_id` | Standard owned resource; DeleteFile; shared gateway authentication and body limit |
| `DELETE /messages/batches/:message_batch_id` | Standard owned resource; DeleteMessageBatch; shared gateway authentication and body limit |
| `DELETE /v1/files/:file_id` | Standard owned resource; DeleteFile; shared gateway authentication and body limit |
| `DELETE /v1/messages/batches/:message_batch_id` | Standard owned resource; DeleteMessageBatch; shared gateway authentication and body limit |
| `GET /batches/:batch_id` | Standard owned resource; GetBatch; shared gateway authentication and body limit |
| `GET /batches` | Standard owned resource; ListBatches; shared gateway authentication and body limit |
| `GET /files/:file_id/content` | Standard owned resource; DownloadFileContent; shared gateway authentication and body limit |
| `GET /files/:file_id` | Standard owned resource; GetFile; shared gateway authentication and body limit |
| `GET /files` | Standard owned resource; ListFiles; shared gateway authentication and body limit |
| `GET /messages/batches/:message_batch_id/results` | Standard owned resource; MessageBatchResults; shared gateway authentication and body limit |
| `GET /messages/batches/:message_batch_id` | Standard owned resource; GetMessageBatch; shared gateway authentication and body limit |
| `GET /messages/batches` | Standard owned resource; ListMessageBatches; shared gateway authentication and body limit |
| `GET /v1/batches/:batch_id` | Standard owned resource; GetBatch; shared gateway authentication and body limit |
| `GET /v1/batches` | Standard owned resource; ListBatches; shared gateway authentication and body limit |
| `GET /v1/files/:file_id/content` | Standard owned resource; DownloadFileContent; shared gateway authentication and body limit |
| `GET /v1/files/:file_id` | Standard owned resource; GetFile; shared gateway authentication and body limit |
| `GET /v1/files` | Standard owned resource; ListFiles; shared gateway authentication and body limit |
| `GET /v1/messages/batches/:message_batch_id/results` | Standard owned resource; MessageBatchResults; shared gateway authentication and body limit |
| `GET /v1/messages/batches/:message_batch_id` | Standard owned resource; GetMessageBatch; shared gateway authentication and body limit |
| `GET /v1/messages/batches` | Standard owned resource; ListMessageBatches; shared gateway authentication and body limit |
| `POST /batches/:batch_id/cancel` | Standard owned resource; CancelBatch; shared gateway authentication and body limit |
| `POST /batches` | Standard owned resource; CreateBatch; shared gateway authentication and body limit |
| `POST /files` | Standard owned resource; UploadFile; shared gateway authentication and body limit |
| `POST /messages/batches/:message_batch_id/cancel` | Standard owned resource; CancelMessageBatch; shared gateway authentication and body limit |
| `POST /messages/batches` | Standard owned resource; CreateMessageBatch; shared gateway authentication and body limit |
| `POST /moderations` | Standard owned resource; Moderations; shared gateway authentication and body limit |
| `POST /tools/search_pro` | Native Kimi payg search; search action dispatch, request plan and per-call usage |
| `POST /tools/search` | Native Kimi payg search; search action dispatch, request plan and per-call usage |
| `POST /v1/batches/:batch_id/cancel` | Standard owned resource; CancelBatch; shared gateway authentication and body limit |
| `POST /v1/batches` | Standard owned resource; CreateBatch; shared gateway authentication and body limit |
| `POST /v1/files` | Standard owned resource; UploadFile; shared gateway authentication and body limit |
| `POST /v1/messages/batches/:message_batch_id/cancel` | Standard owned resource; CancelMessageBatch; shared gateway authentication and body limit |
| `POST /v1/messages/batches` | Standard owned resource; CreateMessageBatch; shared gateway authentication and body limit |
| `POST /v1/moderations` | Standard owned resource; Moderations; shared gateway authentication and body limit |
| `POST /v1/tools/search_pro` | Native Kimi payg search; search action dispatch, request plan and per-call usage |
| `POST /v1/tools/search` | Native Kimi payg search; search action dispatch, request plan and per-call usage |
| `POST /v1/videos/:request_id/cancel` | Qianfan owner-bound task cancellation; Qianwen owner-bound PENDING cancellation request, whose acknowledgement is nonterminal and requires continued polling |
| `POST /videos/:request_id/cancel` | Same Qianfan/Qianwen qualification as the /v1 alias; cancellation acknowledgement is not a universal completed-cancellation guarantee |
