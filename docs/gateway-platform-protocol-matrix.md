# Gateway platform and protocol matrix

This is a source contract for the local gateway, not a live vendor certification.
It describes the registered routes and their existing dispatch/qualification gates.
Registration, protocol conversion, account model evidence, streaming support and
billable completion are independent conditions. A static descriptor is only an
upper bound; no row grants an account permission or changes a selling price.

## Current concrete platforms

The current descriptor set has 18 concrete platforms. Composite is a group routing
mode, not an account platform. N = native path; B = protocol bridge; conditional
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
| volcengine | Runtime API key, exact deployment/ep target | Chat and native Responses; tools conditional | N / rejected / rejected | SSE adapter locally covered; no Anthropic Messages endpoint exists for CN, so that cell is a hard conflict |
| qianfan | Domestic API key, v2 base | Chat and native Responses; tools conditional | N / rejected / rejected | Nonstream/SSE adapter; /v2/responses is live while /v2/messages answers 404, so Messages is a hard conflict |
| hunyuan | Compatibility API key | Nonstream chat/tools; native Responses and Anthropic Messages on the same TokenHub site | N / rejected / rejected | Unverified SSE is rejected before upstream; no invented discovery or balance; the Anthropic path authenticates with x-api-key |
| mistral | API key, original IDs including fine-tuned IDs | Chat only; tools conditional | rejected / rejected / rejected | SSE locally covered; missing usage cannot settle as trusted zero; no /v1/responses and no /v1/messages exist, so both cells are hard conflicts |
| bedrock | Regional runtime API key or IAM SigV4 | Claude Invoke or non-Claude Converse; tools conditional | B / rejected / rejected | Messages/Responses/Chat POST admitted; ConverseStream/Claude streams; region/profile/ARN and catalog evidence still required |

The new chat-only ingress guard admits Chat Completions POST plus model/usage
reads; Bedrock additionally admits Messages and Responses POST. It preserves the
existing 404 OpenAI-style not_found_error on other guarded paths, including the
existing wording. Gemini-native failures retain their Google shape and older
platforms retain their own dispatch behavior. This document does not normalize
those errors or expand old protocols to make the table uniform.

## Core endpoint contracts

| Method | Path / aliases | Effective qualification | Nonstream, stream and tools |
| --- | --- | --- | --- |
| POST | /v1/messages; /messages | Existing OpenAI-compatible dispatch or Gateway Messages; Hunyuan binds the native Anthropic path on the published MessagesPOST fact, every other chat-only platform rejects | Anthropic envelope; stream/tools adapter-specific |
| POST | /v1/messages/count_tokens; /messages/count_tokens | OpenAI bridge, Grok/Cursor estimates, existing Anthropic-compatible path; new restricted platforms reject | Token count, no generation bill; not a generic Messages grant |
| POST | /v1/chat/completions; /chat/completions | Concrete or resolved Composite target, exact catalog endpoint evidence | Native/bridge; Hunyuan stream explicitly unsupported |
| POST | /v1/responses; /responses; /backend-api/codex/responses | Descriptor plus actual account mode/adapter; a chat-only platform whose row publishes ResponsesPOST binds the shared responses trunk, the rest reject | Native/bridge with protocol-specific tools and SSE |
| GET | Same Responses paths | WebSocket dispatcher, account WS qualification | OpenAI native, Grok bridge; no universal WS support |
| POST | Responses /compact | Existing forwardable subpath and compact-account gate | OpenAI compact only; not equivalent to normal Responses |
| POST | Responses /input_tokens | Existing token-count subpath; native/compatible handler dispatch | Input-token result, no normal generation claim |
| POST | Responses other subpaths | IsForwardableOpenAIResponsesRequestPath | Unknown/action-unsafe paths reject before scheduling |
| POST | /v1/embeddings; /embeddings | Platform action matrix: OpenAI, Bedrock, Qianwen, Qianfan, Hunyuan and Volcengine bind an embeddings adapter; every other platform rejects before scheduling | No streaming/tools promise |
| POST | /v1/rerank; /rerank | Platform action matrix: Qianwen, Qianfan and Bedrock bind a rerank adapter; OpenAI and every other platform reject | Ranked results, no generation claim |
| POST | /v1/images/variations; /images/variations | Platform action matrix: OpenAI and Bedrock bind a variation adapter; chat-only platforms reject | OpenAI variation shape only |
| GET | /v1/models; /models | Existing membership and allowlist first | Optional capabilities below; no generation |
| GET | /v1/models/:model; /models/:model | Exact visible entry or model_not_found | Same fields as listing |
| GET | models with nonempty client_version; /backend-api/codex/models | Existing Codex manifest branch | Original manifest/ETag shape, no capabilities addition |

## Proprietary actions (not a wildcard support claim)

The method/path inventory below records wildcard *registrations*. Actual actions
are enumerated here. A wildcard registration never means all vendor APIs exist.

| Method | Action path | Existing behavior and limits |
| --- | --- | --- |
| POST | /v1beta/models/{model}:generateContent | Gemini/Antigravity account and native action gate; nonstream |
| POST | /v1beta/models/{model}:streamGenerateContent | Same qualified native model; SSE |
| POST | /v1beta/models/{model}:countTokens | Gemini scope/fallback-specific count; no inference support implied |
| POST | /v1beta/models/{model}:embedContent | No advertised native action support |
| POST | /v1beta/models/{model}:batchEmbedContents | No advertised native action support |
| POST | /v1beta/models/{model}:unknown | Existing action whitelist rejects; not an extension mechanism |
| POST | /v1/alpha/search; /alpha/search; /backend-api/codex/alpha/search | OpenAI/Codex-specific qualification |
| POST | /v1/live; /backend-api/codex/realtime/calls | OpenAI Live account capability and session model; multipart supported by existing path |
| GET | /v1/live/:call_id; /backend-api/codex/:call_id | Live sideband ownership/capability checks |
| POST | /v1/images/generations | OpenAI or Grok only after Composite target resolution |
| POST | /v1/images/edits | OpenAI or Grok image edit qualification |
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
| POST | /v1/videos; /v1/videos/generations | Grok creation; OpenAI sora-2 job submission through the shared upstream job trunk |
| POST | /v1/videos/edits | Grok video edit; OpenAI edit inherits the owner-checked source video |
| POST | /v1/videos/extensions | Grok video extension; OpenAI extension bills the documented segment |
| GET | /v1/videos/:request_id | Grok status lookup; owner-bound OpenAI durable job state |
| GET | /v1/videos/:request_id/content | Grok content lookup; owner-bound OpenAI content stream |
| GET | /v1/videos/generations/:request_id | Generation status |
| GET | /v1/videos/generations/:request_id/content | Generation content |
| GET | /v1/videos/edits/:request_id | Edit status |
| GET | /v1/videos/edits/:request_id/content | Edit content |
| GET | /v1/videos/extensions/:request_id | Extension status |
| GET | /v1/videos/extensions/:request_id/content | Extension content |
| POST | /v1/tts; /tts | Grok TTS, account-specific native transport |
| POST | /v1/stt; /stt | Grok STT, account-specific native transport |
| POST | /v1/audio/speech; /audio/speech | Platform action matrix: Hunyuan and Qianfan bind an OpenAI-shaped speech adapter; Qianwen keeps its DashScope native path, Grok keeps the native /v1/tts entry and OpenAI speech stays unverified |
| POST | /v1/audio/transcriptions; /audio/transcriptions | Platform action matrix: Hunyuan speech recognition binds an adapter; OpenAI transcription stays unverified |
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

## Public Models projection

Ordinary catalog-backed lists add an optional `capabilities` object *after* the
existing member/allowlist decision. It contains only agreed stored specification
fields: context_window with context_limit_kind, max_output_tokens, input_modalities,
output_modalities, reasoning, supports_vision, supports_function_calling,
supports_prompt_caching, supports_pdf and embedding_dimensions. Missing means
unknown; false and [] remain explicit. Conflicting or absent facts on a qualified
exit are omitted. No account ID, deployment URL, identity/revision, source body,
trace, declaration reason, private routing evidence or price is serialized.

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
- `Idempotency-Key` on generation routes prevents *local* duplicate dispatch and
  duplicate billing; it does not resurrect a provider-side stream. A retry with the
  same key and the same body never starts a second generation: it returns `409`
  with a stable machine code and an `X-Idempotency-State` header
  (`interrupted` when the recorded execution stopped mid-stream, or
  `stream_not_replayable` when a streaming body was already produced). A retry
  with a different body under the same key is `409 IDEMPOTENCY_KEY_CONFLICT`.
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
Capabilities, aliases and uncertainty belong to the rich catalog. Creation time
is omitted when no trusted persisted value exists, with rich `created_at_source`
set to `unavailable`; no refresh or account timestamp is substituted.

Codex manifests use `/backend-api/codex/models`. Explicit `client_version` on the
ordinary collection delegates until 2027-03-21 UTC and emits Sunset and Link
headers. After that date it is an ordinary list query parameter. User-Agent does
not select a manifest. A pinned Codex manifest does not establish ordinary model
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
| `POST /v1/videos/:request_id/cancel` | Qianfan video.cancel; owner-bound upstream DELETE task cancellation |
| `POST /videos/:request_id/cancel` | Qianfan video.cancel; owner-bound upstream DELETE task cancellation |
