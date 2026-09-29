package domain

import "strings"

// Tencent Hunyuan / TokenHub (Tencent Cloud product 1823) action authority rows.
//
// Evidence inventory: first-party Tencent Cloud / TokenHub documentation, pages
// captured 2026-09-21 (per-page anchors below; the saved survey is
// recon/hunyuan-platform-migration.md, evidence rows E1-E17).
//
//   - 1823/130077 (调用指南 index and its PDF export) enumerates the data-plane
//     families: API 使用说明, 语言模型 (chat/Responses/Messages plus 批量推理 and
//     深度思考), 图像生成, 视频生成, 3D 生成, 多模态理解, 向量模型, 联网搜索 and
//     API 错误码. It publishes no rerank, no standalone search, no speech
//     translation, no image variation/batch and no WebSocket/realtime entry.
//   - 1823/130078 (API 使用说明) and 1823/130079 (语言模型调用概览): four TokenHub
//     hosts, Authorization: Bearer on the OpenAI paths, x-api-key on the
//     Anthropic path, GET /v1/models with status=online, and a per-model
//     protocol table (Hy3 lists Chat Completions, Responses and Anthropic).
//   - 1823/135745 image generation, 1823/136578 panorama and 3D scene,
//     1823/135744 image family list, 1823/138195 and 1823/135742 video families,
//     1823/137202 Hy-Video-1.5 submit/query, 1823/137181 HY-3D submit/query,
//     1823/135791 and 1823/135792 speech recognition, 1823/135796 speech
//     synthesis, 1823/135798 MiniMax 音色复刻.
//   - 1823/133515 text and multimodal embeddings, 1823/132358 in-request web
//     search, 1823/130849 batch inference over /v1/batches (chat JSONL only),
//     1823/133813 Responses compatibility mode (HTTP/SSE only).
//   - TokenHub plan data planes, captured 2026-09-23: Coding Plan publishes
//     https://api.lkeap.cloud.tencent.com/coding/v3 (OpenAI-compatible) and
//     https://api.lkeap.cloud.tencent.com/coding/anthropic (Anthropic-compatible),
//     Token Plan 个人版 publishes https://api.lkeap.cloud.tencent.com/plan/v3, and
//     Tencent documents that plan keys (sk-sp- / sk-tp-) are not interchangeable
//     with pay-as-you-go keys (cloud.tencent.com/document/product/1823/130060 and
//     /130092). A plan account therefore resolves a different default base URL; it
//     changes no cell status in this matrix.
//
// Runtime binding (checked 2026-09-22): the Hunyuan platform row keeps
// ChatOnly=true because the OpenAI-shaped TokenHub base defines the entry, and
// the same row now publishes ResponsesPOST and MessagesPOST as native protocol
// paths. The action authority admits the responses and messages cells on those
// protocol facts, and the chat-only catalog admits the matching endpoint
// families for text models, so a Hunyuan API-key account can execute the native
// adapters in handler/hunyuan_actions.go and service/hunyuan_actions.go together
// with the shared text trunk. server/routes/gateway_protocol_matrix_test.go
// enforces that an implemented cell must resolve a catalog binding, so a cell
// may only be bound here when that binding holds.
//
// Dispatch keys this adapter needs registered in
// service.GatewayActionHandlers (routes are registered once by
// multimodal-ingress, never from this branch):
//
//	hunyuan.images             -> OpenAIGatewayHandler.HunyuanImageGeneration
//	hunyuan.image.edit         -> OpenAIGatewayHandler.HunyuanImageEdit
//	hunyuan.video.generate     -> OpenAIGatewayHandler.HunyuanVideoGeneration
//	hunyuan.video.status       -> OpenAIGatewayHandler.HunyuanVideoStatus
//	hunyuan.video.content      -> OpenAIGatewayHandler.HunyuanVideoContent
//	hunyuan.audio.tts          -> OpenAIGatewayHandler.HunyuanAudioSpeech
//	hunyuan.audio.stt          -> OpenAIGatewayHandler.HunyuanAudioTranscriptions
//	hunyuan.voice              -> OpenAIGatewayHandler.HunyuanVoiceClone
//	hunyuan.embeddings         -> OpenAIGatewayHandler.HunyuanEmbeddings
//
// Native TokenHub paths need their own keys because each maps to a different
// handler and the shared route classifier does not claim /wand/* or /api/3d/*
// paths:
//
//	hunyuan.image.panorama     -> HunyuanImagePanorama (/wand/hunyuan-image/world2-panorama)
//	hunyuan.video.scene        -> HunyuanVideoScene (/wand/hunyuan-video/world2-scene)
//	hunyuan.transcribe.async   -> HunyuanAudioTranscriptionCreate (/wand/asrproxy/async_create)
//	hunyuan.transcribe.task    -> HunyuanAudioTranscriptionTask (/wand/asrproxy/tasks/{id})
//	hunyuan.embeddings.multimodal -> HunyuanMultimodalEmbeddings (/embeddings/multimodal)
//	hunyuan.hy3d.submit        -> Hunyuan3DSubmit (/api/3d/submit)
//	hunyuan.hy3d.query         -> Hunyuan3DQuery (/api/3d/query)
//
// The text protocol cells need no entry here: they bind the shared
// openai.messages and openai.responses keys the ingress already registers.

// hunyuanActionAdapters binds the TokenHub cells a Hunyuan API-key account can
// execute today. The media, vector and voice entries were promoted by
// multimodal-ingress, which registers each named key in
// routes/gateway.go GatewayActionHandlers and admits them through the
// evidence-driven chat-only gates; count_tokens uses the shared non-billable
// local estimation adapter because TokenHub publishes no token-count endpoint
// (1823/130077). The text protocol cells bind the shared OpenAI trunk because
// TokenHub serves Chat Completions, Responses and Anthropic Messages on one
// site, so no second forwarder with its own token accounting is introduced.
var hunyuanActionAdapters = map[string]string{
	"chat":           "openai.chat",
	"messages":       "openai.messages",
	"responses":      "openai.responses",
	"count_tokens":   "openai.count_tokens",
	"embeddings":     "hunyuan.embeddings",
	"image.generate": "hunyuan.images",
	"image.edit":     "hunyuan.image.edit",
	"video.generate": "hunyuan.video.generate",
	"video.status":   "hunyuan.video.status",
	"video.content":  "hunyuan.video.content",
	"audio.tts":      "hunyuan.audio.tts",
	"audio.stt":      "hunyuan.audio.stt",
	"voice":          "hunyuan.voice",
}

// hunyuanActionEvidence documents the adapter and first-party protocol evidence
// for every Hunyuan cell whose adapter exists in this branch.
var hunyuanActionEvidence = map[string]string{
	"count_tokens":   "The TokenHub 调用指南 index publishes no token-count endpoint (1823/130077, captured 2026-09-21); the shared openai.count_tokens adapter answers with the non-billable local estimation instead of forwarding a request TokenHub would 404",
	"chat":           "openai.chat trunk to POST {base}/v1/chat/completions with Authorization: Bearer; TokenHub 1823/130079 lists Hy3 on Chat Completions and 1823/130078 fixes the four hosts, the Bearer/x-api-key split and GET /v1/models (captured 2026-09-21). service/openai_gateway_chat_completions_raw.go keeps an unobserved stream unbilled instead of reporting zero usage. Coding Plan accounts use the plan data plane instead: Tencent publishes https://api.lkeap.cloud.tencent.com/coding/v3 as the Coding Plan OpenAI-compatible base and https://api.lkeap.cloud.tencent.com/plan/v3 for Token Plan 个人版, with plan keys documented as not interchangeable with pay-as-you-go keys (1823/130092 and 1823/130060, captured 2026-09-23); Account.GetOpenAIBaseURL and service/hunyuan_adapter.go resolve that base from credentials.account_mode when base_url is empty, while an explicit base_url still wins",
	"messages":       "openai.messages trunk on the TokenHub Anthropic path: POST {base}/v1/messages authenticated with x-api-key, where {base} is the same version-suffixed TokenHub site as the chat trunk (the version-aware target builder resolves {host}/v1/messages, never a doubled /v1). 1823/130079 lists Hy3 on Chat Completions, Responses and Anthropic and 1823/130078 fixes the four hosts plus the Bearer/x-api-key split (captured 2026-09-21). An account with api_protocol=anthropic forwards with zero conversion through Account.GetAnthropicProtocolBaseURL; every other protocol setting keeps the shared Anthropic→text conversion, so no second messages forwarder with separate token accounting exists. Coding Plan accounts resolve the plan-specific Anthropic-compatible root https://api.lkeap.cloud.tencent.com/coding/anthropic instead (1823/130092, captured 2026-09-23), so the mode changes the base URL only and not this cell's status",
	"responses":      "openai.responses trunk on the TokenHub Responses compatibility mode: POST {base}/responses over HTTP/SSE, derived by the shared trunk from the same version-suffixed TokenHub base (1823/133813 documents the compatibility mode and 1823/130079 lists Responses in the per-model protocol table, captured 2026-09-21). This path authenticates with Authorization: Bearer and its usage fields are billed by the shared Responses settlement",
	"embeddings":     "hunyuan.embeddings handler to POST {base}/v1/embeddings (OpenAI-compatible text vectorization) and POST {base}/v1/embeddings/multimodal for the fused text/image/video vector (1823/133515, captured 2026-09-21); usage.prompt_tokens/total_tokens billing",
	"image.generate": "hunyuan.images handler to POST {base}/v1/wand/hunyuan-image/v3-generation (Hy-Image-3.0, synchronous, data[].url with a 12-hour temporary COS URL, size/images/seed/footnote/revise parameters) plus POST {base}/v1/wand/hunyuan-image/world2-panorama for the Hy-World-2.1 360 panorama, which answers data[].super_resolution_image_url and cover_image_url (1823/135745, 1823/136578 and the 1823/135744 family list, captured 2026-09-21); per-image billing from the counted outputs",
	"image.edit":     "hunyuan.image.edit handler on the same POST {base}/v1/wand/hunyuan-image/v3-generation endpoint: the documented 参考生图 form takes 0-3 source images in images[] next to the required prompt and returns the identical data[].url envelope, which is the faithful TokenHub bridge for the OpenAI /images/edits shape (1823/135745 and the 1823/135744 instruction-based editing rows, captured 2026-09-21); the vendor publishes no /images/edits path",
	"video.generate": "hunyuan.video.generate handler to POST {base}/v1/wand/hunyuan-video/generation (Hy-Video-1.5 text/image-to-video) and POST {base}/v1/wand/hunyuan-video/world2-scene (Hy-World-2.1 3D scene); submission answers {task_id, created, request_id} and a rejected job answers HTTP 200 with {\"task_id\":\"\",\"status\":\"failed\"} (1823/137202, 1823/136578, captured 2026-09-21). No cancel endpoint is documented, so cancellation only stops the gateway from starting new work",
	"video.status":   "hunyuan.video.status handler to GET {base}/v1/wand/hunyuan-video/tasks/{task_id}, owner-scoped through the create-time account binding; status queued/running/succeeded/failed with videos[].url on success (1823/137202, captured 2026-09-21). The settled vendor URL is rewritten to the gateway content proxy so it never leaves the gateway",
	"video.content":  "hunyuan.video.content handler: owner-scoped task re-read through the bound account, then a https-only download of videos[].url with Range passthrough and no upstream credential on the media host (1823/137202 result shape, captured 2026-09-21)",
	"audio.tts":      "hunyuan.audio.tts handler to POST {base}/v1/wand/minimax-tts/sync_tts (MiniMax Speech family hosted by TokenHub) with extra_info.usage_characters reported as the million-character unit and base_resp.status_code as the vendor status (1823/135796, captured 2026-09-21)",
	"audio.stt":      "hunyuan.audio.stt handler for synchronous POST {base}/v1/wand/asrproxy/sync_transcribe and asynchronous POST {base}/v1/wand/asrproxy/async_create plus GET {base}/v1/wand/asrproxy/tasks/{id}; output.duration_ms drives the hour-based unit and the asynchronous task bills once when a poll first observes status=completed (1823/135791, 1823/135792, captured 2026-09-21)",
	"voice":          "hunyuan.voice handler to POST {base}/v1/wand/minimax-tts/sync_clone with model=minimax-voice-clone, audio_url or audio_data plus the optional demo text and tts_model; the response carries voice_id, input_sensitive and the optional demo_audio. The vendor charges the clone fee on the first synthesis that uses the returned voice_id, so only the demo audio is priced at the gateway (1823/135798, captured 2026-09-21)",
}

// hunyuanHardConflicts records actions verified absent from the TokenHub
// product-1823 data plane, with no faithful bridge. They stay out of Composite
// target selection for Hunyuan: the gateway rejects the route with an explicit
// action error instead of a silent 404, and a Composite group must route the
// action to a platform that implements it.
var hunyuanHardConflicts = map[string]string{
	"responses.compact":   "TokenHub Responses compatibility mode is documented as an OpenAI Responses mapping served over HTTP/SSE and publishes no compact subpath (1823/133813 and the 1823/130077 index, captured 2026-09-21); compact is an OpenAI proprietary extension",
	"generate_content":    "Gemini generativelanguage protocol surface; the TokenHub 调用指南 index (1823/130077) and the API 使用说明 (1823/130078) publish no Google-protocol endpoint (captured 2026-09-21)",
	"rerank":              "The vector family documents text embeddings and multimodal embeddings only, with no rerank endpoint (1823/133515), the model list carries no rerank model (1823/130051) and the 调用指南 index has no rerank family (1823/130077); all captured 2026-09-21",
	"image.variation":     "Hy-Image-3.0 requires prompt and uses images[] as 参考生图 reference images, so there is no prompt-free variation semantic and no /images/variations path (1823/135745 and the 1823/135744 family list, captured 2026-09-21)",
	"image.batch":         "TokenHub batch inference is a JSONL /v1/batches job family whose lines are /v1/chat/completions requests (documented model glm-5.1) and the image families publish no batch image job (1823/130849, 1823/135744, 1823/130077, captured 2026-09-21); the standard /images/batches contract belongs to the gateway resource-actions task",
	"video.extend":        "Hy-Video-1.5 documents generation and task query only, with no cancel or extension parameter (1823/137202), and the Kling 生视频 guide defines the contents[] material types (prompt, base_video, first/last frame, reference images, elements) without a continuation parameter (1823/135742, captured 2026-09-21); the retired 视频编辑-1.0 model is not an extension surface",
	"audio.translate":     "The 调用指南 index publishes no speech-translation family (1823/130077, captured 2026-09-21); TokenHub translation is the Hy-MT2 text model family served through chat (1823/132252), which is a different contract from the OpenAI /audio/translations speech endpoint",
	"realtime":            "TokenHub publishes no WebSocket or realtime session endpoint; the data plane is HTTP/SSE (1823/130077 index and 1823/130079 protocol overview, captured 2026-09-21), and Tencent realtime audio/video is a separate TRTC product with its own credential",
	"search":              "No standalone /v1/web_search or /v1/x_search endpoint exists (1823/130077 index, captured 2026-09-21); web search is the in-request Chat web_search_options and Responses tools[{type:web_search}] capability (1823/132358) already carried by the chat and responses cells",
	"alpha_search":        "OpenAI Codex proprietary /alpha/search surface; the TokenHub data plane publishes no equivalent (1823/130077, captured 2026-09-21)",
	"live":                "OpenAI Codex live-calls surface; TokenHub publishes no live-call endpoint (1823/130077, captured 2026-09-21)",
	"responses.websocket": "TokenHub Responses compatibility mode is documented as HTTP/SSE only, with no Responses-over-WebSocket endpoint (1823/133813 and 1823/130077, captured 2026-09-21)",
}

// hunyuanUnverifiedReasons records the cells that are not bound to an adapter
// today together with the verification result that produced the status. A cell
// whose adapter exists elsewhere names that adapter and the gate blocking the
// bind, so no cell ever reads as a bare \"unsupported\".
var hunyuanUnverifiedReasons = map[string]string{
	"image.async": "Vidu-Image-q2 is documented as an asynchronous image family in the TokenHub image list (1823/135744) and the international product pages publish POST /wand/vidu-image/generation plus GET /wand/vidu-image/tasks/{task_id} returning creations[].url (intl 1300/83709, captured 2026-09-21); no async image job adapter or upstream-job family is shipped by this branch, and the standard /images/generations/async contract is owned by the upstream job work",
	"video.edit":  "The Kling omni-video family is the documented TokenHub video-edit surface: POST /wand/kling/omni-video with a contents[] item {type: base_video, url} plus the instruction prompt, and GET /wand/kling/tasks/{task_id} for the task (1823/135742, captured 2026-09-21). The response envelope is documented as following the Kling structure \"以实际返回为准\" with no saved wire sample, and mapping the OpenAI /videos/edits shape onto contents[] is a bridge this branch does not ship; the capability is real, so the cell is unverified rather than a hard conflict",
	"native":      "TokenHub's native families are the /wand/* and /api/3d/* paths plus /embeddings/multimodal (1823/130077 family index, captured 2026-09-21). The shared route classifier reserves the native action for the DashScope-style /api/v1, /api-ws/v1 and /compatible-mode/v1 paths, which TokenHub does not publish, so those families are reached through the per-action keys listed at the top of this file plus their own native path registrations instead of a native umbrella cell",
}

// hunyuanActionDescriptor finalizes the tri-state for every Hunyuan cell:
// implemented cells carry adapter, evidence and policy bindings; verified
// absences become hard conflicts with evidence; the remaining cells stay
// explicitly unverified with the verification result that produced the status.
func hunyuanActionDescriptor(descriptor ActionDescriptor, action, adapter string) ActionDescriptor {
	if hosted := strings.TrimSpace(descriptor.Identity.HostedAccountType); hosted != "" && hosted != AccountTypeAPIKey {
		descriptor.Evidence = "TokenHub data-plane actions authenticate with the account API key (Authorization: Bearer, x-api-key on the Anthropic path); hosted credential type " + hosted + " carries no TokenHub data-plane credential (1823/130078, captured 2026-09-21)"
		return descriptor
	}
	bound, implemented := hunyuanActionAdapters[action]
	serving := strings.TrimSpace(adapter)
	if serving != "" {
		bound = serving
	}
	if implemented || serving != "" {
		descriptor.Status, descriptor.Adapter = ActionImplemented, bound
		if evidence, ok := hunyuanActionEvidence[action]; ok {
			descriptor.Evidence = evidence
		} else {
			descriptor.Evidence = "Existing gateway adapter: " + bound
		}
		descriptor.CredentialPolicy = "hunyuan API-key account credential owner (Authorization: Bearer on the OpenAI paths, x-api-key on the Anthropic path); base URL is one of the official TokenHub sites (or a documented Coding Plan / Token Plan site when account_mode=coding)"
		descriptor.ModelPolicy = "resolved account model evidence and TokenHub task limits"
		descriptor.UsagePolicy = "TokenHub response usage with frozen request pricing"
		descriptor.Lifecycle = "request plan"
		switch action {
		case "embeddings":
			descriptor.UsagePolicy = "TokenHub usage.prompt_tokens/total_tokens"
		case "image.generate", "image.edit":
			descriptor.UsagePolicy = "per-image unit with the frozen request price and the counted data[] outputs"
		case "video.generate":
			descriptor.UsagePolicy = "deferred one-shot billing on the first succeeded task observation: video count plus the resolution and duration captured when the task was accepted"
			descriptor.Lifecycle = "request plan and upstream task binding"
		case "video.status", "video.content":
			descriptor.UsagePolicy = "deferred one-shot billing claimed once per task across every status and content poll"
			descriptor.Lifecycle = "upstream task owner-scoped read under the owning key"
		case "audio.tts":
			descriptor.UsagePolicy = "synthesized characters on the shared tts million-character unit"
			descriptor.Lifecycle = "synchronous request"
		case "audio.stt":
			descriptor.UsagePolicy = "recognised duration on the shared stt hour unit; the asynchronous task bills once at status=completed"
			descriptor.Lifecycle = "request plan and upstream task binding"
		case "voice":
			descriptor.UsagePolicy = "the vendor charges the clone fee on the first synthesis that uses the returned voice_id, so only the optional demo audio carries gateway units"
			descriptor.Lifecycle = "synchronous request returning the reusable voice_id"
		}
		return descriptor
	}
	if evidence, conflict := hunyuanHardConflicts[action]; conflict {
		descriptor.Status = ActionHardConflict
		descriptor.Evidence = evidence
		return descriptor
	}
	if reason, known := hunyuanUnverifiedReasons[action]; known {
		descriptor.Evidence = reason
		return descriptor
	}
	descriptor.Evidence = "No verified TokenHub gateway adapter for this action cell (verified 2026-09-21)"
	return descriptor
}
