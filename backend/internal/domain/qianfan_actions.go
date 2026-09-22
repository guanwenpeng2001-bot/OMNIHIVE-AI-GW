package domain

import "strings"

// Baidu Qianfan (百度智能云千帆 ModelBuilder) action authority rows.
//
// Evidence was verified against the official Qianfan API reference
// (cloud.baidu.com/doc/qianfan-api, page data captured 2026-09-21) plus live
// unauthenticated endpoint probes on 2026-09-21. Qianfan mixes three credential
// surfaces, so no cell reuses one header for all paths:
//   - ModelBuilder data plane (qianfan.baidubce.com): Authorization: Bearer <API Key>
//   - Baidu AI Search (/v2/ai_search/*): X-Appbuilder-Authorization: Bearer <AppBuilder Key>
//   - Baidu speech products (tsn.baidu.com TTS, aip.baidubce.com voice clone):
//     their own product hosts and their own API Key handling, never the
//     ModelBuilder path.
//
// A route probe distinguishes a live route (401/400 auth errors) from an absent
// one (404 ResourceNotFound / 404 page not found).
//
// Runtime binding (checked 2026-09-22): the Qianfan product entry stays a
// chat-only entry because the OpenAI-shaped /v2 base defines it, and the same
// row publishes ResponsesPOST so POST /v2/responses binds the shared OpenAI
// responses trunk on that protocol fact. There is no Anthropic Messages endpoint
// to bind: POST /v2/messages answers 404, so that cell is a hard conflict. Every
// other cell is classified by its own evidence row, and the chat-only protocol
// guard, accountCatalogModelEndpoints and SupportsOpenAIEndpointCapability read
// the same action matrix.

// qianfanActionAdapters binds the ModelBuilder and Baidu speech cells a Qianfan
// API-key account can execute. The chat trunk and the media/vector/speech entries
// were promoted by multimodal-ingress, which registers every named key in
// routes/gateway.go GatewayActionHandlers and admits them through the
// evidence-driven chat-only gates; count_tokens uses the shared non-billable
// local estimation adapter because no /v2 token-count route exists.
var qianfanActionAdapters = map[string]string{
	"chat":           "openai.chat",
	"responses":      "openai.responses",
	"count_tokens":   "openai.count_tokens",
	"embeddings":     "qianfan.embeddings",
	"rerank":         "qianfan.rerank",
	"image.generate": "qianfan.images",
	"image.edit":     "qianfan.image.edit",
	"video.generate": "qianfan.video.generate",
	"video.status":   "qianfan.video.status",
	"video.content":  "qianfan.video.content",
	"audio.tts":      "qianfan.audio.tts",
	"voice":          "qianfan.voice",
	"search":         "qianfan.search",
}

// qianfanAdapterEvidence documents the adapter this branch implements and its
// first-party protocol evidence. It is applied when a cell is bound to the
// adapter named in its key.
var qianfanAdapterEvidence = map[string]string{
	"chat":           "openai.chat trunk through buildQianfanRequest to POST https://qianfan.baidubce.com/v2/chat/completions with Authorization: Bearer bce-v3/ALTAK (official API Key 鉴权 doc and 文本生成 reference, captured 2026-09-21; live probe 401 invalid_iam_token). Token Plan accounts use the plan data plane instead: the official OpenAI-compatible base is https://qianfan.baidubce.com/v2/tokenplan/personal (企业版 https://qianfan.baidubce.com/v2/tokenplan/team) with a dedicated API key isolated from pay-as-you-go keys (千帆《Token Plan 个人版》cloud.baidu.com/doc/qianfan/s/Dmrabu8b6、《Token Plan 企业版》/ymq8wwch2, captured 2026-09-23); buildQianfanRequest resolves the personal base from credentials.account_mode when base_url is empty and accepts the tokenplan path as a version root, while an explicit base_url still wins",
	"responses":      "openai.responses trunk on the native Qianfan Responses API: POST https://qianfan.baidubce.com/v2/responses with Authorization: Bearer bce-v3/ALTAK, derived by the shared trunk from the version-suffixed /v2 base (official 创建模型响应 doc and live probe 2026-09-21: 401 invalid_iam_token, i.e. a live route; the shared Responses settlement bills the upstream usage fields)",
	"count_tokens":   "No /v2/count_tokens or /v2/tokenizer route exists (404 live probes 2026-09-21); the shared openai.count_tokens adapter answers with the non-billable local estimation and never sends an upstream count request",
	"embeddings":     "qianfan.embeddings handler to POST https://qianfan.baidubce.com/v2/embeddings with the OpenAI-compatible embedding envelope (official 向量 doc, captured 2026-09-21; live probe 401 invalid_iam_token)",
	"rerank":         "qianfan.rerank handler to POST https://qianfan.baidubce.com/v2/rerank returning object=reranker_list with results[].relevance_score (official 重排序 doc, captured 2026-09-21; live probe 401 invalid_iam_token)",
	"image.generate": "qianfan.images handler to POST https://qianfan.baidubce.com/v2/images/generations for the documented ernie-image/qwen-image/flux.1-schnell family, plus the verified model-category paths /v2/ernie-image/images/generations and /v2/musesteamer/images/generations (official 通用图像生成 / Ernie-Image-Turbo / 蒸汽机 Air-Image docs, captured 2026-09-21; live probes 401)",
	"image.edit":     "qianfan.images handler to POST https://qianfan.baidubce.com/v2/images/edits with the documented JSON shape (model, image, mask, feature erase|repaint|variation, prompt) for ernie-irag-edit and qwen-image-edit (official 通用图像编辑 doc, captured 2026-09-21; live probe 401 invalid_iam_token)",
	"video.generate": "qianfan.video.generate handler to POST https://qianfan.baidubce.com/video/generations (host root, not /v2) with the documented content[] parts and deferred billing until a terminal status poll (official 创建视频生成任务 doc, captured 2026-09-21; live probe 401 invalid_iam_token)",
	"video.status":   "qianfan.video.status owner-bound GET https://qianfan.baidubce.com/video/generations?task_id= with status pending|queued|running|succeeded|failed and content.video_url (official 查询视频生成任务 doc, captured 2026-09-21; live probe 401 invalid_iam_token)",
	"video.content":  "qianfan.video.content owner-bound re-query of the task then proxy download of content.video_url through the bound account so the CDN URL never leaves the gateway (official 查询视频生成任务 doc result URL shape, captured 2026-09-21)",
	"audio.tts":      "qianfan.audio.tts handler to POST https://tsn.baidu.com/text2audio with Authorization: Bearer <API Key> and the documented form fields tex/cuid/ctp/lan/per/aue/spd returning binary audio (official 短文本语音合成 doc, captured 2026-09-21; live probe HTTP 200 IAM error envelope on the speech host)",
	"voice":          "qianfan.voice handler to POST https://aip.baidubce.com/rest/2.0/speech/publiccloudspeech/v1/voice/clone/{create,list} with the API Key on the Authorization header (official 大模型声音复刻 docs, captured 2026-09-21; live probes 400 status 11014 with an unauthenticated credential)",
	"search":         "qianfan.search handler to POST https://qianfan.baidubce.com/v2/ai_search/web_search authenticated by X-Appbuilder-Authorization instead of the ModelBuilder bearer (official 百度搜索 doc, captured 2026-09-21; live probes 401 code 216003 with a distinct AppBuilder auth parser)",
}

// qianfanHardConflicts records actions verified as absent from the Qianfan
// surface and without a faithful bridge. These must not be selected as
// Composite targets for this platform.
var qianfanHardConflicts = map[string]string{
	"messages":            "POST /v2/messages returns 404 ResourceNotFound (live probe 2026-09-21); the pay-as-you-go /v2 ModelBuilder surface publishes no Anthropic Messages endpoint (the official /v2 API reference covers chat/completions, responses, embeddings, rerank, images and video, captured 2026-09-21), and the OpenAI-compatible /v2/chat/completions trunk already serves chat. A verified absence rather than a deferred integration: no native or faithful Messages contract exists to bind on that surface. Token Plan additionally publishes an Anthropic-compatible base, https://qianfan.baidubce.com/anthropic/tokenplan/personal (千帆《Token Plan 个人版》, captured 2026-09-23), so re-adjudicating this cell for plan accounts is a product decision; the status stays unchanged here",
	"responses.compact":   "POST /v2/responses/compact returns 404 page not found (live probe 2026-09-21); compact is an OpenAI Responses extension with no Qianfan equivalent",
	"generate_content":    "Gemini generativelanguage surface; the Qianfan v2 probe set (2026-09-21) publishes no equivalent endpoint",
	"image.variation":     "POST /v2/images/variations returns 404 page not found (live probe 2026-09-21); the documented iRAG variation feature is an instruction-driven images/edits mode that requires a prompt, so the OpenAI variations shape has no faithful bridge",
	"image.async":         "POST /v2/images/generations/async and /v2/images/edits/async return 404 page not found (live probes 2026-09-21); Qianfan image generation is documented as synchronous",
	"video.edit":          "POST /video/edits returns 404 ResourceNotFound (live probe 2026-09-21); the documented Qianfan video families expose generation, query and cancel only",
	"video.extend":        "POST /video/extensions returns 404 ResourceNotFound (live probe 2026-09-21); no video extension endpoint is documented",
	"alpha_search":        "OpenAI Codex proprietary /alpha/search surface; Qianfan's search is the AppBuilder /v2/ai_search family already served by the search action (2026-09-21)",
	"live":                "OpenAI Codex live-calls surface; Qianfan documents no equivalent realtime call endpoint (POST /v2/realtime returned 404 on 2026-09-21)",
	"responses.websocket": "Qianfan documents POST /v2/responses as HTTP/SSE only with /v2/responses/compact absent (404 probe 2026-09-21); Baidu's speech WebSocket surface is a different product, not a Responses-over-WebSocket endpoint",
}

// qianfanUnverifiedEvidence records every cell that is not bound to an adapter
// today, together with the verification result that produced the status. Cells
// whose adapter already exists in this branch name that adapter and the runtime
// gate that blocks the bind, so the cell is never a bare "unsupported".
var qianfanUnverifiedEvidence = map[string]string{
	"embeddings":      "Adapter qianfan.embeddings is implemented in this branch (service/qianfan_actions.go) and verified against POST https://qianfan.baidubce.com/v2/embeddings (official 向量 doc, 2026-09-21; live probe 401 invalid_iam_token), but the platform is declared chat-only, so accountCatalogModelEndpoints (service/account_model_catalog.go:373) and SupportsOpenAIEndpointCapability (service/account.go:1901) deny non-chat endpoints and the routes chat-only guard rejects /v1/embeddings before dispatch; the cell binds with one adapter line once the platform classification decision lands",
	"rerank":          "Adapter qianfan.rerank is implemented in this branch (service/qianfan_actions.go) and verified against POST https://qianfan.baidubce.com/v2/rerank (official 重排序 doc, 2026-09-21; live probe 401 invalid_iam_token); no standard /v1/rerank route serves a chat-only platform entry yet and the cell stays unbound pending the platform classification decision",
	"image.generate":  "Adapter qianfan.images is implemented in this branch (handler/qianfan_actions.go) and verified against POST https://qianfan.baidubce.com/v2/images/generations plus /v2/ernie-image/images/generations and /v2/musesteamer/images/generations (official docs 2026-09-21; live probes 401); /v1/images/generations is rejected for the chat-only platform entry by TestChatOnlyPlatformProtocolGuard, so the cell binds once the classification decision lands",
	"image.edit":      "Adapter qianfan.images (edit path) is implemented in this branch and verified against POST https://qianfan.baidubce.com/v2/images/edits with the documented JSON shape (official 通用图像编辑 doc, 2026-09-21; live probe 401 invalid_iam_token); /v1/images/edits stays rejected for the chat-only platform entry, so the cell stays unbound pending the classification decision",
	"video.generate":  "Adapter qianfan.video.generate is implemented in this branch and verified against POST https://qianfan.baidubce.com/video/generations (official 创建视频生成任务 doc, 2026-09-21; live probe 401 invalid_iam_token); /v1/videos and /v1/videos/generations are rejected for the chat-only platform entry, so the cell stays unbound pending the classification decision",
	"video.status":    "Adapter qianfan.video.status is implemented in this branch with owner-bound task resolution and verified against GET https://qianfan.baidubce.com/video/generations?task_id= (official 查询视频生成任务 doc, 2026-09-21; live probe 401 invalid_iam_token); the video lookup routes are rejected for the chat-only platform entry until the classification decision lands",
	"video.content":   "Adapter qianfan.video.content is implemented in this branch (owner-bound re-query plus CDN proxy download) against the documented content.video_url result shape (2026-09-21); the video content routes are rejected for the chat-only platform entry until the classification decision lands",
	"audio.tts":       "Adapter qianfan.audio.tts is implemented in this branch and verified against POST https://tsn.baidu.com/text2audio with Authorization: Bearer <API Key> (official 短文本语音合成 doc, 2026-09-21; live probe HTTP 200 IAM error envelope); /v1/audio/speech is rejected for the chat-only platform entry by TestChatOnlyPlatformProtocolGuard, so the cell stays unbound pending the classification decision",
	"voice":           "Adapter qianfan.voice is implemented in this branch and verified against the Baidu speech host POST /rest/2.0/speech/publiccloudspeech/v1/voice/clone/{create,list} (official 大模型声音复刻 docs, 2026-09-21; live probes 400 status 11014); /v1/custom-voices is not served for a chat-only platform entry, so the cell stays unbound pending the classification decision",
	"search":          "Adapter qianfan.search is implemented in this branch and verified against POST https://qianfan.baidubce.com/v2/ai_search/web_search with the distinct X-Appbuilder-Authorization credential (official 百度搜索 doc, 2026-09-21; live probes 401 code 216003); /v1/web_search is not served for a chat-only platform entry, so the cell stays unbound pending the classification decision",
	"image.batch":     "The official 批量预测任务 doc (captured 2026-09-21) lists /v2/chat/completions, /v2/images/generations and /v2/images/edits as batch interfaces, but the shared files/batches resource adapter does not bind Qianfan yet",
	"audio.stt":       "No Qianfan audio route exists (POST /v2/audio/transcriptions 404 on 2026-09-21); Baidu's speech product serves short ASR on vop.baidu.com/server_api (probe 2026-09-21: 200 err_no 3314 on an empty body) as a separate credential surface, so an STT bridge remains a candidate, not a verified capability",
	"audio.translate": "No Qianfan audio route exists (POST /v2/audio/translations 404 on 2026-09-21); Baidu speech translation lives on another product surface whose endpoint is not yet verified",
	"realtime":        "No Qianfan realtime route exists (POST /v2/realtime 404 on 2026-09-21); Baidu realtime ASR is a WebSocket product surface (wss://vop.baidu.com/realtime_asr, official doc captured 2026-09-21) that needs its own adapter and credential handling",
	"native":          "Qianfan's native surface is the OpenAI-compatible /v2 family already served by the chat, embeddings, rerank and media actions (2026-09-21); no separate native umbrella path is bound for this platform",
}

// qianfanActionDescriptor finalizes the tri-state for every Qianfan cell:
// implemented cells carry adapter, evidence and policy bindings; verified
// absences become hard conflicts with evidence; the remaining cells stay
// explicitly unverified with the verification result that produced the status.
func qianfanActionDescriptor(descriptor ActionDescriptor, action, adapter string) ActionDescriptor {
	bound, implemented := qianfanActionAdapters[action]
	if serving := strings.TrimSpace(adapter); serving != "" {
		bound = serving
		implemented = true
	}
	if !implemented {
		if evidence, conflict := qianfanHardConflicts[action]; conflict {
			descriptor.Status = ActionHardConflict
			descriptor.Evidence = evidence
			return descriptor
		}
		if evidence, ok := qianfanUnverifiedEvidence[action]; ok {
			descriptor.Evidence = evidence
			return descriptor
		}
		descriptor.Evidence = "No verified Qianfan gateway adapter for this action cell (verified 2026-09-21)"
		return descriptor
	}
	adapter = bound
	descriptor.Status, descriptor.Adapter = ActionImplemented, adapter
	if evidence, ok := qianfanAdapterEvidence[action]; ok {
		descriptor.Evidence = evidence
	} else {
		descriptor.Evidence = "Existing gateway adapter: " + adapter
	}
	descriptor.CredentialPolicy = "qianfan API-key account credential owner; ModelBuilder Bearer API Key on qianfan.baidubce.com"
	descriptor.ModelPolicy = "resolved account model evidence and task limits"
	descriptor.UsagePolicy = "native Qianfan usage fields with frozen request pricing"
	descriptor.Lifecycle = "request plan"
	switch action {
	case "search":
		descriptor.CredentialPolicy = "qianfan AppBuilder API Key only (X-Appbuilder-Authorization); the ModelBuilder bearer is never sent to /v2/ai_search"
	case "audio.tts":
		descriptor.CredentialPolicy = "qianfan API-key account credential on the Baidu speech host tsn.baidu.com (Authorization: Bearer API Key), not the ModelBuilder /v2 base URL"
		descriptor.UsagePolicy = "synthesized characters priced on the shared tts million-character unit"
	case "voice":
		descriptor.CredentialPolicy = "qianfan API-key account credential on the Baidu speech host aip.baidubce.com voice clone surface"
		descriptor.UsagePolicy = "per-enrollment management action; carries no billable audio units (grok/minimax voice-clone precedent)"
	case "embeddings", "rerank":
		descriptor.UsagePolicy = "upstream prompt/total token usage"
		descriptor.Lifecycle = "synchronous request"
	case "image.generate", "image.edit":
		descriptor.UsagePolicy = "per-image unit with the frozen request price and output size tier"
	case "video.generate":
		descriptor.UsagePolicy = "deferred per-second video price, claimed once when a poll first observes a succeeded task"
		descriptor.Lifecycle = "request plan and upstream task binding"
	case "video.status", "video.content":
		descriptor.Lifecycle = "upstream task durable read under owner key and workspace"
	}
	return descriptor
}
