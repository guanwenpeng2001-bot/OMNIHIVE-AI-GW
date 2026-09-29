package domain

import "strings"

// Volcengine Ark action authority rows. Every cell was verified against
// first-party Volcengine/ByteDance material captured on 2026-09-21 (see the
// actions-volcengine report for the saved evidence set):
//
//   - Ark API reference index (docs.volcengine.com/docs/82379/1298459, "Base URL
//     及鉴权" navigation): the data plane publishes Chat API, Responses API,
//     Files API, video generation (create/retrieve/list/cancel-or-delete),
//     image generation (+ streaming events), 3D generation, the Embeddings API
//     (text + multimodal vectorization), batch inference (Batch API (Chat) and
//     the management BatchInferenceJob actions), tokenizer, context caching and
//     the Application (Bot) API. It publishes no rerank, no speech, no image
//     variation, no asynchronous image job and no batch image endpoint.
//   - Image generation API (docs/82379/1541523): POST {root}/images/generations
//     with model, prompt, image (single or multi reference), size (1K/2K/4K or
//     WxH), response_format (url/b64_json), watermark, stream,
//     sequential_image_generation(+options.max_images); response carries
//     data[].url|b64_json and usage.image_count.
//   - Video generation API (docs/82379/1520757 create, /1521309 retrieve,
//     /1521720 cancel-or-delete): POST {root}/contents/generations/tasks with a
//     multimodal content array (text, image_url with role first_frame/last_frame,
//     video_url for 编辑视频/延长视频), GET .../tasks/{id} returning
//     status queued|running|cancelled|succeeded|failed|expired plus
//     content.video_url, resolution, duration, ratio, and DELETE for queued
//     cancellation or finished-record deletion.
//   - Embeddings API (docs/82379/1521765 text, docs/82379/1409290 multimodal):
//     POST {root}/embeddings and POST {root}/embeddings/multimodal, both with
//     `Authorization: Bearer <ARK_API_KEY>`.
//
// Credential and region shape: an Ark data-plane account is an API key account
// (Authorization: Bearer <api_key>) whose target is an Endpoint ID (ep-*) or a
// model ID; the region and any relay prefix live in the account base URL, never
// in a credential guess. AK/SK signing belongs to the management/control plane
// and is deliberately not used here.
//
// Integration prerequisites for the implemented media/embedding cells (verified
// by running the gateway handler paths on 2026-09-21): the adapters and these
// cells are in place, and service accountImplementsGatewayAction already resolves
// them, but a Volcengine account still cannot execute them while the platform
// descriptor keeps ChatOnly=true. That flag locks three layers at once
// (service/account.go SupportsOpenAIEndpointCapability, service/
// account_model_catalog.go accountCatalogModelEndpoints and service/
// gateway_forward_model_binding.go requestCatalogEndpoint), and the media request
// plan additionally requires the grok_media_generation catalog endpoint. The
// dispatch keys the ingress must register for these handlers are listed in
// volcengineActionAdapters below.

// volcengineEvidenceCaptured is the date this matrix was verified against the
// first-party material listed above.
const volcengineEvidenceCaptured = "captured 2026-09-21"

// volcengineActionAdapters binds the Ark cells this branch can actually serve
// today. internal/server/routes/gateway_protocol_matrix_test.go enforces the
// repository-wide invariant that an implemented cell must also resolve a
// catalog account binding, so a cell may only appear here when a Volcengine
// account can execute it. The names are the dispatch keys the ingress registers
// for these handlers (service.GatewayActionHandlers).
var volcengineActionAdapters = map[string]string{
	"chat": "openai.chat",
}

// volcenginePendingAdapters names the Ark adapters that are implemented and
// covered by service-level wire tests in this branch
// (backend/internal/service/volcengine_actions_test.go) but cannot execute yet:
// the platform descriptor keeps ChatOnly=true, which locks
// service/account.go SupportsOpenAIEndpointCapability,
// service/account_model_catalog.go accountCatalogModelEndpoints and
// service/gateway_forward_model_binding.go requestCatalogEndpoint for every
// non-chat endpoint. Promoting a cell means moving its entry into
// volcengineActionAdapters in the same change that removes the platform lock
// and registers the dispatch key in routes/gateway.go.
var volcenginePendingAdapters = map[string]string{
	"embeddings":     "volcengine.embeddings",
	"image.generate": "volcengine.images",
	"image.edit":     "volcengine.images",
	"video.generate": "volcengine.video.generate",
	"video.edit":     "volcengine.video.edit",
	"video.extend":   "volcengine.video.extend",
	"video.status":   "volcengine.video.status",
	"video.content":  "volcengine.video.content",
}

// volcengineActionEvidence documents the serving adapter and its first-party
// protocol evidence for implemented Volcengine actions.
var volcengineActionEvidence = map[string]string{
	"chat":           "Frozen Ark chat runtime contract (service/volcengine_adapter.go prepareVolcengineChatRequest): Bearer API key on {root}/chat/completions with the resolved Endpoint ID/model target (docs/82379/1298459 Base URL 及鉴权, " + volcengineEvidenceCaptured + ")",
	"embeddings":     "volcengine.embeddings handler to POST {root}/embeddings (text vectorization, docs/82379/1521765) and POST {root}/embeddings/multimodal for text/image/video input objects (多模态向量化 API, docs/82379/1409290); usage.prompt_tokens/total_tokens billing (" + volcengineEvidenceCaptured + ")",
	"image.generate": "volcengine.images handler to POST {root}/images/generations (doubao-seedream-* / 即梦; 文生图 plus 组图生成 through sequential_image_generation, docs/82379/1541523, " + volcengineEvidenceCaptured + ")",
	"image.edit":     "volcengine.images handler on the same POST {root}/images/generations endpoint with the reference `image` field (单张图生图 / 多参考图生图, docs/82379/1541523, " + volcengineEvidenceCaptured + "); the official image API publishes no separate /images/edits path",
	"video.generate": "volcengine.video.generate handler to POST {root}/contents/generations/tasks (doubao-seedance-* 文生视频 / 首帧 / 首尾帧, docs/82379/1520757, " + volcengineEvidenceCaptured + ")",
	"video.edit":     "volcengine.video.edit handler to POST {root}/contents/generations/tasks with a video_url content item (Seedance 编辑视频, docs/82379/1520757, " + volcengineEvidenceCaptured + ")",
	"video.extend":   "volcengine.video.extend handler to POST {root}/contents/generations/tasks with a video_url content item (Seedance 延长视频, docs/82379/1520757, " + volcengineEvidenceCaptured + ")",
	"video.status":   "volcengine.video.status handler to GET {root}/contents/generations/tasks/{id}, owner-scoped through the create-time account binding; status/content.video_url/resolution/duration per docs/82379/1521309 (" + volcengineEvidenceCaptured + ")",
	"video.content":  "volcengine.video.content handler: owner-scoped status read then https-only download of content.video_url with Range passthrough; the 24h task video URL is rewritten to the gateway proxy (docs/82379/1521309, " + volcengineEvidenceCaptured + ")",
}

// volcengineHardConflicts records actions verified absent from the Ark data
// plane. These cells stay out of Composite target selection for Volcengine: the
// gateway rejects them with an explicit action error instead of a silent 404,
// and a Composite group must route them to a platform that implements them.
var volcengineHardConflicts = map[string]string{
	"responses.compact":   "Ark Responses API navigation (docs/82379/1298459, " + volcengineEvidenceCaptured + ") publishes create/retrieve/context/delete/streaming and no compact subpath; Responses compact is an OpenAI proprietary extension",
	"generate_content":    "Gemini protocol surface (generativelanguage.googleapis.com); the Ark data plane publishes no Google-protocol endpoint (docs/82379/1298459, " + volcengineEvidenceCaptured + ")",
	"image.variation":     "Image generation API (docs/82379/1541523, " + volcengineEvidenceCaptured + ") requires a prompt and exposes only the reference `image` field for instruction-based 图生图; no /images/variations endpoint or prompt-free variation semantics exist",
	"image.async":         "The Ark image API publishes a synchronous call plus SSE streaming events only (docs/82379/1298459 + docs/82379/1541523, " + volcengineEvidenceCaptured + "); no asynchronous image job record exists to poll",
	"image.batch":         "No batch image endpoint exists on the Ark data plane (docs/82379/1298459, " + volcengineEvidenceCaptured + "); 批量推理 is a management Action API (CreateBatchInferenceJob and siblings, docs/82379/1399517) signed with AK/SK against a TOS bucket, which is not this account's Ark API-key data plane",
	"search":              "Ark publishes Web Search only as an in-request tool (tools[{type:web_search}] on chat/Responses, docs/82379/1756990 and docs/82379/1958524, " + volcengineEvidenceCaptured + "); there is no standalone web_search/x_search endpoint",
	"alpha_search":        "OpenAI Codex proprietary surface (/alpha/search); the Ark data plane publishes no equivalent (docs/82379/1298459, " + volcengineEvidenceCaptured + ")",
	"live":                "OpenAI Codex realtime-call surface; the Ark data plane publishes no Codex live-call endpoint (docs/82379/1298459, " + volcengineEvidenceCaptured + "), and Ark realtime speech is a different product credential (see the realtime cell)",
	"responses.websocket": "Ark Responses API is documented as HTTP/SSE (docs/82379/1298459 Responses 流式响应事件, " + volcengineEvidenceCaptured + "); no Responses-over-WebSocket endpoint is published",
}

// volcengineUnverifiedReasons records the cells whose capability exists but is
// not reachable from an Ark API-key account inside this adapter. They are not
// hard conflicts: a missing credential or a wiring decision owned by another
// component is never proof that the supplier lacks the action.
var volcengineUnverifiedReasons = map[string]string{
	"messages":        "The Ark data plane publishes no Anthropic Messages endpoint for CN (docs/82379/1298459, " + volcengineEvidenceCaptured + "); the gateway Claude→chat messages bridge already lists volcengine as an OpenAI-compatible text target (handler/openai_gateway_handler.go openAICompatibleTextTargetAllowed) but the action authority gates messages on !ChatOnly, which is an action-dispatch descriptor decision",
	"responses":       "Ark publishes a native Responses API at {root}/responses (docs/82379/1298459 Responses API, " + volcengineEvidenceCaptured + ") and the shared trunk already derives that URL from a version-suffixed base, but the Volcengine descriptor still records ResponsesPOST as rejected, so registering the adapter is an action-dispatch descriptor decision",
	"count_tokens":    "The Ark data plane publishes no token-count endpoint for CN (docs/82379/1298459, " + volcengineEvidenceCaptured + "); the gateway's local estimation adapter is gated on !ChatOnly for this platform",
	"rerank":          "The Ark data plane publishes no rerank endpoint (docs/82379/1298459, " + volcengineEvidenceCaptured + "); Volcengine rerank is the VikingDB knowledge service (/api/knowledge/service/rerank, docs/82379/1399545) with its own service token, a different product credential than this Ark API-key account",
	"audio.tts":       "Doubao speech synthesis is served by the speech product on openspeech.bytedance.com with X-Api-Key + X-Api-Resource-Id (V3 HTTP/SSE unidirectional, docs/6561/1598757, " + volcengineEvidenceCaptured + "); no Ark data-plane speech endpoint exists, and that speech credential is not part of an Ark API-key account",
	"audio.stt":       "Doubao speech recognition is served by the speech product on openspeech.bytedance.com (大模型流式语音识别, docs/6561/1354869, " + volcengineEvidenceCaptured + ") with the speech credential; the Ark data plane publishes no transcription endpoint",
	"audio.translate": "Ark publishes no speech-translation endpoint (docs/82379/1298459, " + volcengineEvidenceCaptured + "); Volcengine speech translation is a speech-product realtime session, not this account's Ark data plane",
	"voice":           "声音复刻 is a speech-product capability (seed-icl-* resource ids on openspeech.bytedance.com, docs/6561/1598757, " + volcengineEvidenceCaptured + "); the Ark data plane publishes no voice enrollment endpoint and the account carries no speech credential",
	"realtime":        "Doubao 端到端实时语音 is served by the speech product at wss://openspeech.bytedance.com/api/v3/realtime/dialogue (docs/6561/1594356, " + volcengineEvidenceCaptured + ") or through RTC with a speech app id/token; the Ark data plane publishes no realtime session endpoint",
	"native":          "No separate Ark-native ingress is registered for Volcengine: this adapter serves the OpenAI-shaped Ark data plane, while the remaining native families (Files API, 3D generation, batch chat, context caching) belong to the resource/model registry work owned by other tasks (" + volcengineEvidenceCaptured + ")",
}

// volcengineActionDescriptor finalizes the tri-state for every Volcengine cell.
// Ark data-plane actions authenticate with the account's Ark API key, so any
// other hosted credential type stays unverified instead of borrowing a
// capability it cannot execute.
func volcengineActionDescriptor(descriptor ActionDescriptor, action, adapter string) ActionDescriptor {
	if hosted := strings.TrimSpace(descriptor.Identity.HostedAccountType); hosted != "" && hosted != AccountTypeAPIKey {
		descriptor.Evidence = "Volcengine Ark data-plane actions require an Ark API key account (Authorization: Bearer <api_key>); hosted credential type " + hosted + " carries no Ark data-plane credential (" + volcengineEvidenceCaptured + ")"
		return descriptor
	}
	bound, implemented := volcengineActionAdapters[action]
	if serving := strings.TrimSpace(adapter); serving != "" {
		bound = serving
	}
	if pending, waiting := volcenginePendingAdapters[action]; waiting {
		descriptor.Evidence = "Ark adapter " + pending + " is implemented and covered by the service-level wire tests, and the official evidence row is: " + volcengineActionEvidence[action] + ". The cell stays unverified because no Volcengine account can execute it while the platform descriptor keeps ChatOnly=true: the platform lock in SupportsOpenAIEndpointCapability / accountCatalogModelEndpoints / requestCatalogEndpoint excludes every non-chat endpoint, and gateway_protocol_matrix_test.go requires an implemented cell to resolve a catalog binding; promote this cell in the change that removes the platform lock"
		return descriptor
	}
	if implemented || strings.TrimSpace(adapter) != "" {
		descriptor.Status, descriptor.Adapter = ActionImplemented, bound
		if evidence, ok := volcengineActionEvidence[action]; ok {
			descriptor.Evidence = evidence
		} else {
			descriptor.Evidence = "Existing gateway adapter: " + bound
		}
		descriptor.CredentialPolicy = "volcengine Ark API key account credential owner (Authorization: Bearer <ARK api_key>); account target is an Endpoint ID (ep-*) or model ID, region and relay prefix come from the account base URL"
		descriptor.ModelPolicy = "resolved account model evidence and Ark task limits"
		descriptor.UsagePolicy = "Ark response usage with frozen request/job pricing"
		descriptor.Lifecycle = "request plan"
		switch action {
		case "image.generate", "image.edit":
			descriptor.UsagePolicy = "Ark images usage.image_count with the group image price of the resolved 1K/2K/4K size tier"
		case "embeddings":
			descriptor.UsagePolicy = "Ark usage.prompt_tokens/total_tokens"
		case "video.generate", "video.edit", "video.extend":
			descriptor.UsagePolicy = "deferred one-shot billing on the first succeeded task observation: video count, resolution tier and duration from the Ark task record"
			descriptor.Lifecycle = "request plan and upstream job"
		case "video.status", "video.content":
			descriptor.UsagePolicy = "deferred one-shot billing claimed once per task across status/content polls"
			descriptor.Lifecycle = "upstream job owner-scoped read under the owning key"
		}
		return descriptor
	}
	if evidence, conflict := volcengineHardConflicts[action]; conflict {
		descriptor.Status = ActionHardConflict
		descriptor.Evidence = evidence
		return descriptor
	}
	if reason, known := volcengineUnverifiedReasons[action]; known {
		descriptor.Evidence = reason
		return descriptor
	}
	descriptor.Evidence = "No verified Volcengine Ark adapter for this action cell (" + volcengineEvidenceCaptured + ")"
	return descriptor
}
