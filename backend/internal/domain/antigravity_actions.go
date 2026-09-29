package domain

// Antigravity action authority rows. Antigravity is Google's Antigravity IDE
// backend: the only data-plane surface this repository verifies is the CloudCode
// v1internal endpoint family (generateContent, streamGenerateContent, and the
// management calls loadCodeAssist, onboardUser, fetchAvailableModels,
// setUserSettings, fetchUserInfo) reached with the account's own OAuth token plus
// a project identifier. Credentials are never a Gemini API key: the project and
// the OAuth account are the identity, and this file must not borrow Gemini API-key
// capability to fill a cell.
//
// Evidence inventory (checked 2026-09-21): the upstream client in
// internal/pkg/antigravity/client.go builds exactly /v1internal:<action>, and the
// saved wire samples in internal/pkg/antigravity/client_test.go pin the request
// shapes for v1internal:generateContent, v1internal:streamGenerateContent?alt=sse,
// v1internal:loadCodeAssist and v1internal:fetchAvailableModels.
// docs/gateway-platform-protocol-matrix.md records the same row as
// "CloudCode/Gemini conversion" with Responses POST bridged and Responses
// WebSocket/compact rejected. The per-cell register is
// backend/dev-docs/antigravity-action-families.md.

// antigravityActionEvidence documents the serving adapter and its verified
// protocol evidence for implemented Antigravity actions.
var antigravityActionEvidence = map[string]string{
	"chat":             "gateway.chat over Antigravity's own OAuth account and project identity (AntigravityGatewayService.ForwardAsChatCompletions converts an OpenAI chat request to v1internal Gemini contents, 2026-09-21)",
	"messages":         "gateway.messages over Antigravity's own OAuth account and project identity (AntigravityGatewayService.Forward transforms Claude Messages to v1internal via antigravity.TransformClaudeToGemini, 2026-09-21)",
	"responses":        "gateway.responses over Antigravity's own OAuth account and project identity (AntigravityGatewayService.ForwardAsResponses; docs/gateway-platform-protocol-matrix.md lists Responses POST as a bridge for the antigravity row, 2026-09-21)",
	"count_tokens":     "gateway.count_tokens; CloudCode exposes no countTokens data-plane endpoint, so the adapter answers 404 for Antigravity accounts (internal/service/gateway_count_tokens.go, 2026-09-21) and clients fall back to local estimation; non-billable",
	"generate_content": "gateway.generate_content over Antigravity's own OAuth account and project identity (AntigravityGatewayService.ForwardGemini wraps the native Gemini body into v1internal:streamGenerateContent?alt=sse, wire shape pinned by internal/pkg/antigravity/client_test.go, 2026-09-21)",
	"image.generate":   "antigravity.images bridge (ParseAntigravityImagesRequest/ForwardImages) on the same verified v1internal generateContent path with a gemini-*-image model (model default mapping in internal/domain/constants.go; image billing tier from generationConfig.imageConfig.imageSize, 2026-09-21); no separate upstream image endpoint is invented",
	"image.edit":       "antigravity.images bridge on the same verified v1internal generateContent path: the source image from images[].image_url or a multipart upload is normalized to an inline base64 part next to the edit instruction (Gemini instruction-based image editing, 2026-09-21); no upstream /images/edits endpoint is invented",
}

// antigravityHardConflicts records actions verified absent from the CloudCode
// v1internal surface on 2026-09-21. The probe source is the endpoint inventory in
// internal/pkg/antigravity/client.go plus the saved wire samples in
// internal/pkg/antigravity/client_test.go: that surface carries no embeddings,
// rerank, batch, image-variation, video, audio, voice, realtime or search
// endpoint, and adding one would require borrowing a different supplier's
// credential. These cells stay out of Composite target selection for Antigravity;
// the gateway rejects them with a clear protocol error instead of a silent 404,
// and a Composite group must route them to a platform that implements the action.
var antigravityHardConflicts = map[string]string{
	"responses.compact":   "CloudCode v1internal (internal/pkg/antigravity/client.go, wire samples in client_test.go, checked 2026-09-21) exposes no Responses compact subpath; docs/gateway-platform-protocol-matrix.md records Responses compact as rejected for the antigravity row",
	"embeddings":          "CloudCode v1internal exposes no embedContent/embeddings endpoint (endpoint inventory in internal/pkg/antigravity/client.go, checked 2026-09-21); Antigravity OAuth credentials are not valid on the Gemini API-key embeddings endpoint",
	"rerank":              "CloudCode v1internal exposes no rerank endpoint and documents no rerank model (endpoint inventory in internal/pkg/antigravity/client.go, checked 2026-09-21)",
	"image.variation":     "CloudCode v1internal exposes no /images/variations equivalent (checked 2026-09-21); Antigravity image output is instruction-based generateContent, which does not carry OpenAI variation semantics",
	"image.async":         "CloudCode v1internal exposes no asynchronous image job endpoint (checked 2026-09-21); image output is a synchronous generateContent response",
	"image.batch":         "CloudCode v1internal exposes no batch image endpoint (checked 2026-09-21); no batch family is published for Antigravity accounts",
	"video.generate":      "CloudCode v1internal exposes no video synthesis endpoint (checked 2026-09-21); Antigravity OAuth credentials are not valid on a video API",
	"video.edit":          "CloudCode v1internal exposes no video edit endpoint (checked 2026-09-21)",
	"video.extend":        "CloudCode v1internal exposes no video extension endpoint (checked 2026-09-21)",
	"video.status":        "CloudCode v1internal exposes no asynchronous video job record to poll (checked 2026-09-21)",
	"video.content":       "CloudCode v1internal exposes no asynchronous video job content endpoint (checked 2026-09-21)",
	"audio.tts":           "CloudCode v1internal exposes no speech synthesis endpoint (checked 2026-09-21); this is not the Gemini API-key TTS surface",
	"audio.stt":           "CloudCode v1internal exposes no speech-to-text endpoint (checked 2026-09-21); this is not the Gemini API-key transcription surface",
	"audio.translate":     "CloudCode v1internal exposes no speech translation endpoint (checked 2026-09-21)",
	"voice":               "CloudCode v1internal exposes no voice clone/enrollment endpoint (checked 2026-09-21)",
	"realtime":            "CloudCode v1internal exposes no realtime WebSocket session endpoint (checked 2026-09-21); Antigravity forwards request/response generation only",
	"search":              "CloudCode v1internal exposes no standalone web_search/x_search endpoint (checked 2026-09-21); search on Antigravity exists only as an in-conversation model tool",
	"alpha_search":        "OpenAI Codex proprietary surface (/alpha/search); CloudCode v1internal exposes no equivalent (checked 2026-09-21)",
	"live":                "OpenAI Codex realtime calls surface; CloudCode v1internal exposes no equivalent (checked 2026-09-21)",
	"responses.websocket": "CloudCode v1internal serves generateContent as HTTP/SSE only (docs/gateway-platform-protocol-matrix.md records Responses WebSocket as rejected for antigravity; checked 2026-09-21)",
}

// AntigravityHardConflictEvidence reports the dated evidence for a cell verified
// absent from the Antigravity surface. Callers use it to build an explicit
// protocol error; an empty ok means the cell is not a recorded hard conflict.
func AntigravityHardConflictEvidence(action string) (string, bool) {
	evidence, ok := antigravityHardConflicts[action]
	return evidence, ok
}

// antigravityActionDescriptor finalizes the tri-state for every Antigravity cell.
// Implemented cells carry adapter, evidence and lifecycle bindings; verified
// absences become hard conflicts with evidence rather than a silent unverified.
func antigravityActionDescriptor(descriptor ActionDescriptor, action, adapter string) ActionDescriptor {
	if adapter == "" {
		if evidence, conflict := antigravityHardConflicts[action]; conflict {
			descriptor.Status = ActionHardConflict
			descriptor.Evidence = evidence
			return descriptor
		}
		if action == "image.generate" || action == "image.edit" {
			adapter = "antigravity.images"
		} else {
			descriptor.Evidence = "No verified Antigravity gateway adapter for this action cell (verified 2026-09-21)"
			return descriptor
		}
	}
	descriptor.Status, descriptor.Adapter = ActionImplemented, adapter
	if evidence, ok := antigravityActionEvidence[action]; ok {
		descriptor.Evidence = evidence
	} else {
		descriptor.Evidence = "Existing gateway adapter: " + adapter
	}
	descriptor.CredentialPolicy = "antigravity OAuth account credential owner with CloudCode project identity; API-key deployments use their configured base_url/api_key, never a Gemini API key"
	descriptor.ModelPolicy = "resolved Antigravity model mapping and task limits; image cells require a gemini-*-image model"
	descriptor.UsagePolicy = "native CloudCode usage fields with frozen request pricing; image cells bill per generated image tier"
	descriptor.Lifecycle = "request plan"
	return descriptor
}
