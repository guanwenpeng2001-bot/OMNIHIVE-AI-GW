package domain

// Qianwen/DashScope action authority rows. Evidence was verified against
// official Alibaba Cloud Model Studio documentation on 2026-09-22 (saved under
// the actions-qianwen evidence set); each implemented cell names the gateway
// adapter that serves it, and each hard conflict cites the verified absence.

// qianwenActionEvidence documents the serving adapter and its official
// protocol evidence for implemented Qianwen actions.
var qianwenActionEvidence = map[string]string{
	"chat":           "openai.chat trunk to dashscope compatible-mode /chat/completions (official OpenAI-compatible chat doc, 2026-09-22); native conversations via qianwen.native",
	"messages":       "openai.messages trunk with DashScope native Anthropic endpoint passthrough (apps/anthropic /v1/messages, existing verified adapter, rechecked 2026-09-22)",
	"responses":      "openai.responses trunk to compatible-mode /responses (official OpenAI-compatible Responses doc, 2026-09-22)",
	"count_tokens":   "openai.count_tokens local estimation adapter; no DashScope token-count endpoint is documented (2026-09-22); non-billable",
	"embeddings":     "qianwen.embeddings handler to compatible-mode /embeddings (official embedding compatibility doc, 2026-09-22) and native /api/v1/services/embeddings/text-embedding/text-embedding adapter",
	"rerank":         "qianwen.rerank bridge plus native /api/v1/services/rerank/text-rerank/text-rerank adapter for gte-rerank-v2/qwen3-vl-rerank (official Text Rerank API doc, 2026-09-22)",
	"image.generate": "qianwen.images bridge to compatible-mode /images/generations (qwen-image-3.0 family, official Qwen Image 3.0 API reference, 2026-09-22) and native synchronous qwen-image adapter",
	"image.edit":     "qianwen.native synchronous qwen-image-edit family adapter on multimodal-generation (official image editing models); compatible-mode documents no /images/edits endpoint (2026-09-22)",
	"image.async":    "qianwen.native asynchronous text2image family on /api/v1/services/aigc/text2image/image-synthesis with X-DashScope-Async (official Wan text-to-image V2 API reference, 2026-09-22)",
	"video.generate": "qianwen.native asynchronous Wan 2.x video-synthesis adapter, live-verified 2026-09-14 (dev-docs/qianwen-video-generation.md)",
	"video.edit":     "qianwen.native VACE wanx2.1-vace-plus video_edit function on video-synthesis (official Wan VACE guide, 2026-09-22)",
	"video.extend":   "qianwen.native VACE wanx2.1-vace-plus video_extension function on video-synthesis (official Wan VACE guide, 2026-09-22)",
	"video.status":   "qianwen.native owner-scoped durable read of /api/v1/tasks/{id} with upstream task IDs never exposed (live-verified 2026-09-14, dev-docs/qianwen-video-generation.md)",
	"video.content":  "qianwen.native owner-scoped content download of the settled provider result URL pinned to documented DashScope result hosts (official Wan API reference result URL shape, 2026-09-22)",
	"audio.tts":      "qianwen.native synchronous qwen3-tts-flash family on multimodal-generation plus realtime TTS sessions (official Qwen-TTS docs, 2026-09-22); usage.characters billing",
	"audio.stt":      "qianwen.native synchronous qwen3-asr-flash family and asynchronous paraformer transcription family (official ASR docs, 2026-09-22); usage.seconds/duration billing",
	"voice":          "qianwen.native qwen-voice-enrollment customization adapter on /api/v1/services/audio/tts/customization (official voice-clone HTTP API doc, 2026-09-22); per-clone Beijing tariff",
	"realtime":       "qianwen.native WebSocket relay to wss://dashscope.aliyuncs.com/api-ws/v1/realtime with durable session/turn receipts (docs/DASHSCOPE_REALTIME_MVP.md, region price evidence)",
	"native":         "qianwen.native umbrella adapter for DashScope-native /api/v1 and /api-ws/v1 families with per-family fail-closed gates (rechecked 2026-09-22)",
}

// qianwenHardConflicts records actions verified as absent from the DashScope
// surface on 2026-09-22. These stay out of Composite target selection for this
// platform; a composite group must route them to a platform that implements
// the action.
var qianwenHardConflicts = map[string]string{
	"responses.compact":   "compatible-mode Responses documentation (2026-09-22) lists no compact subpath; OpenAI proprietary extension without a DashScope equivalent",
	"generate_content":    "Gemini protocol surface; DashScope documents no Google-generativelanguage endpoint (2026-09-22)",
	"image.variation":     "OpenAI /images/variations shape has no DashScope equivalent; compatible-mode documents only /images/generations and no edits/variations endpoints (2026-09-22); Wanx image editing is instruction-based, not variation semantics",
	"image.batch":         "DashScope Batch API supports text chat completions only (official batch compatibility doc and dev-docs/qianwen-file-batch.md); no batch image generation API is documented (2026-09-22)",
	"audio.translate":     "No synchronous speech-translation endpoint (OpenAI /audio/translations shape) is documented; DashScope speech translation exists only as gummy realtime WebSocket sessions (2026-09-22)",
	"search":              "No standalone web_search/x_search endpoint exists; search is a chat/responses built-in tool parameter (enable_search / tools.web_search) billed inside conversation usage (2026-09-22)",
	"alpha_search":        "OpenAI Codex proprietary surface (/alpha/search); no DashScope equivalent documented (2026-09-22)",
	"live":                "OpenAI Codex realtime calls surface; DashScope realtime is served by the realtime action, not Codex live calls (2026-09-22)",
	"responses.websocket": "compatible-mode Responses is documented as HTTP/SSE only; no Responses-over-WebSocket endpoint exists (2026-09-22); DashScope WebSocket coverage is the realtime action",
}

// qianwenActionDescriptor finalizes the tri-state for every Qianwen cell:
// implemented cells carry adapter, evidence and lifecycle bindings; verified
// absences become hard conflicts with evidence instead of silent unverified.
func qianwenActionDescriptor(descriptor ActionDescriptor, action, adapter string) ActionDescriptor {
	if adapter == "" {
		if evidence, conflict := qianwenHardConflicts[action]; conflict {
			descriptor.Status = ActionHardConflict
			descriptor.Evidence = evidence
			return descriptor
		}
		descriptor.Evidence = "No verified Qianwen gateway adapter for this action cell (verified 2026-09-22)"
		return descriptor
	}
	descriptor.Status, descriptor.Adapter = ActionImplemented, adapter
	if evidence, ok := qianwenActionEvidence[action]; ok {
		descriptor.Evidence = evidence
	} else {
		descriptor.Evidence = "Existing gateway adapter: " + adapter
	}
	descriptor.CredentialPolicy = "qianwen API-key account credential owner; DashScope Bearer authentication"
	descriptor.ModelPolicy = "resolved account model evidence and task limits"
	descriptor.UsagePolicy = "native DashScope usage fields with frozen request/job pricing"
	descriptor.Lifecycle = "request plan"
	switch action {
	case "count_tokens":
		descriptor.UsagePolicy = "non-billable local token estimation; no DashScope token-count endpoint documented"
		descriptor.Lifecycle = "synchronous request"
	case "image.async", "video.generate", "video.edit", "video.extend":
		descriptor.Lifecycle = "request plan and upstream job"
	case "video.status", "video.content":
		descriptor.Lifecycle = "upstream job durable read under owner key"
	case "realtime":
		descriptor.Lifecycle = "request plan with realtime session and per-turn receipts"
	}
	return descriptor
}
