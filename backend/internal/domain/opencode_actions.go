package domain

import "strings"

// OpenCode Zen / OpenCode Go action authority rows.
//
// OpenCode is the rare case where the hosted gateway *is* the platform: Zen
// (pay-as-you-go) and Go ($10/month) each publish a fixed route table and a
// fixed model catalog, and the managed credential can only reach the routes in
// that table. The underlying vendors' full catalogs are not a hosted grant, so
// every cell below is decided against the OpenCode route table, not against the
// vendor that produced the model.
//
// Evidence, all first party and all captured 2026-09-21:
//   - https://opencode.ai/docs/zen/ and https://opencode.ai/docs/go/ — endpoint
//     tables (chat/messages/responses/Google column/System One), hosted model
//     catalogs and per-1M-token price tables.
//   - Anonymous live route probes against https://opencode.ai with no credential
//     and no billable action; status codes and response samples are saved under
//     internal/service/testdata/opencode_zen_go_action_surface.json, hosted
//     catalogs under opencode_zen_models.json and opencode_go_models.json.
//
// A probe that returns the site 404 proves the gateway has no such route — that
// is a hard conflict and needs the Composite alternative. A probe that returns
// the OpenCode AuthError (401) proves the route exists, and the accepted header
// tells which credential scheme serves it.
const openCodeActionEvidenceDate = "2026-09-21"

// openCodeActionEvidence documents the serving adapter and its first-party
// protocol evidence for every implemented OpenCode action.
var openCodeActionEvidence = map[string]string{
	"chat":             "OpenCode Zen and Go endpoint tables publish /zen/v1/chat/completions and /zen/go/v1/chat/completions for models outside the GPT/Grok/Muse Spark/Claude/Qwen groups (docs pages, " + openCodeActionEvidenceDate + "); the anonymous probe returns the gateway AuthError instead of the site 404",
	"messages":         "OpenCode Zen and Go endpoint tables publish /zen/v1/messages and /zen/go/v1/messages for the Anthropic-shaped models; the probe accepts x-api-key and answers Missing API key for Bearer (docs pages, " + openCodeActionEvidenceDate + ")",
	"responses":        "OpenCode Zen and Go endpoint tables publish /zen/v1/responses and /zen/go/v1/responses for the GPT/Grok/Muse Spark models; the probe accepts Bearer and answers Missing API key for x-api-key (docs pages, " + openCodeActionEvidenceDate + ")",
	"count_tokens":     "The gateway token-count estimate remains the adapter: OpenCode publishes no Anthropic-shaped /zen/v1/messages/count_tokens (site 404, " + openCodeActionEvidenceDate + "), and the Google-shape :countTokens is part of the generate_content ingress (free estimate, never billed)",
	"generate_content": "The Zen endpoint table publishes the Google-protocol column /zen/v1/models/{model} for its Gemini models (docs page, " + openCodeActionEvidenceDate + "); probes confirm :generateContent, :streamGenerateContent and :countTokens answer behind x-goog-api-key, an unknown model answers ModelError before auth, and every /v1beta and /zen/go/v1/models/... shape returns the site 404. The registered opencode.generate_content ingress retains this native Google column for selected Zen accounts; Go and other text models convert through their account-selected Messages/Chat/Responses trunk without adding a Go Google URL",
}

// openCodeHardConflicts records actions verified as absent from the OpenCode
// hosted route table on 2026-09-21. These cells stay out of Composite target
// selection for this platform; a group that needs them must route to a platform
// whose matrix implements them.
var openCodeHardConflicts = map[string]string{
	"video.cancel":        "No video job exists to cancel in the hosted Zen/Go endpoint tables; neither plan publishes a video generation API (opencode.ai/docs/zen and opencode.ai/docs/go, " + openCodeActionEvidenceDate + ")",
	"responses.compact":   "OpenCode publishes no compact subpath: /zen/v1/responses/compact returns the site 404 (" + openCodeActionEvidenceDate + "); compact is an OpenAI-proprietary Responses extension",
	"embeddings":          "No embeddings route on either plan: /zen/v1/embeddings and /zen/go/v1/embeddings both return the site 404, and neither endpoint table lists an embedding model (" + openCodeActionEvidenceDate + ")",
	"rerank":              "No rerank route on either plan: /zen/v1/rerank and /zen/go/v1/rerank both return the site 404, and no rerank model appears in either catalog (" + openCodeActionEvidenceDate + ")",
	"image.generate":      "/zen/v1/images/generations returns the site 404 and the Zen/Go endpoint tables list no image model (" + openCodeActionEvidenceDate + ")",
	"image.edit":          "/zen/v1/images/edits returns the site 404; the only image handling OpenCode documents is the DeepSeek V4 Flash Vision Exp model turning images into input tokens on the chat route (" + openCodeActionEvidenceDate + ")",
	"image.variation":     "/zen/v1/images/variations returns the site 404; no variation semantics exist on either plan (" + openCodeActionEvidenceDate + ")",
	"image.async":         "No asynchronous image route: /zen/v1/images/generations and /zen/v1/batches return the site 404, and the Go page's Batch API mention is the underlying vendor's feature disabled by zero-data retention, not an OpenCode route (" + openCodeActionEvidenceDate + ")",
	"image.batch":         "No batch route: /zen/v1/batches returns the site 404 on both plans (" + openCodeActionEvidenceDate + ")",
	"video.generate":      "/zen/v1/videos and /zen/v1/video/generations both return the site 404; no video model appears in either catalog (" + openCodeActionEvidenceDate + ")",
	"video.edit":          "No video route exists to edit against: every probed /zen/v1/videos path returns the site 404 (" + openCodeActionEvidenceDate + ")",
	"video.extend":        "No video route exists to extend against: every probed /zen/v1/videos path returns the site 404 (" + openCodeActionEvidenceDate + ")",
	"video.status":        "No video task store: /zen/v1/videos/{id} returns the site 404, so no owner-scoped task read exists (" + openCodeActionEvidenceDate + ")",
	"video.content":       "No video task store: /zen/v1/videos/{id} returns the site 404, so no owner-scoped content download exists (" + openCodeActionEvidenceDate + ")",
	"audio.tts":           "/zen/v1/audio/speech returns the site 404 and neither endpoint table lists a speech model (" + openCodeActionEvidenceDate + ")",
	"audio.stt":           "/zen/v1/audio/transcriptions returns the site 404 and neither endpoint table lists a transcription model (" + openCodeActionEvidenceDate + ")",
	"audio.translate":     "/zen/v1/audio/translations returns the site 404; OpenCode publishes no speech translation route (" + openCodeActionEvidenceDate + ")",
	"voice":               "/zen/v1/audio/voices and /zen/v1/voice both return the site 404; OpenCode publishes no voice customization route (" + openCodeActionEvidenceDate + ")",
	"realtime":            "/zen/v1/realtime returns the site 404 on both plans, and the Go page's mention of a stateful Responses API describes the underlying vendor's feature being disabled by ZDR, not an OpenCode route (" + openCodeActionEvidenceDate + ")",
	"search":              "/zen/v1/search returns the site 404; OpenCode exposes no standalone web search endpoint (" + openCodeActionEvidenceDate + ")",
	"alpha_search":        "/zen/v1/alpha/search returns the site 404; the OpenAI Codex alpha search surface has no OpenCode equivalent (" + openCodeActionEvidenceDate + ")",
	"live":                "/zen/v1/live returns the site 404; the OpenAI Codex live-calls surface has no OpenCode equivalent (" + openCodeActionEvidenceDate + ")",
	"responses.websocket": "A WebSocket upgrade to /zen/v1/responses and /zen/go/v1/responses returns the site 404; both docs pages publish HTTP endpoints only and no wss:// URL (" + openCodeActionEvidenceDate + ")",
}

// openCodeUnverifiedActions explains the cells that are neither implemented nor
// proven absent: the upstream capability is verified, but this gateway has no
// wired ingress for it yet. Naming an adapter here would advertise a path that
// no dispatch handler serves.
var openCodeUnverifiedActions = map[string]string{
	"native": "OpenCode Zen publishes the Jev/System One ingress /zen/v1/systemone: a Jev request behind Bearer is answered by the gateway AuthError/Invalid API key and an unauthenticated one by the gateway's own rate-limit error (probes " + openCodeActionEvidenceDate + "); the Go plan publishes the same path but rejects Jev models with ModelError. The gateway registers no native ingress or dispatch handler for it, so no adapter is advertised until that entry exists",
}

// openCodeActionDescriptor finalizes the tri-state for every OpenCode cell.
// Implemented cells carry adapter, credential, model, usage and lifecycle
// bindings; verified absences become hard conflicts with evidence; the
// remaining wiring gaps stay unverified with their reason instead of a silent
// empty matrix.
func openCodeActionDescriptor(descriptor ActionDescriptor, action, adapter string) ActionDescriptor {
	// opencode.* adapters speak the OpenCode-owned ingresses, which are API-key
	// surfaces: the Google ingress requires account.Type == api_key and there is
	// no OAuth or upstream-passthrough form. Shared trunk adapters keep whatever
	// hosted-credential behavior the trunk already has.
	if adapter != "" && strings.HasPrefix(adapter, "opencode.") &&
		descriptor.Identity.HostedAccountType != "" && descriptor.Identity.HostedAccountType != AccountTypeAPIKey {
		descriptor.Status = ActionUnverified
		descriptor.Evidence = "The OpenCode-owned Google ingress requires an API-key account; a " + descriptor.Identity.HostedAccountType + " credential has no verified OpenCode ingress (" + openCodeActionEvidenceDate + ")"
		return descriptor
	}
	if adapter == "" {
		if evidence, conflict := openCodeHardConflicts[action]; conflict {
			descriptor.Status = ActionHardConflict
			descriptor.Evidence = evidence
			return descriptor
		}
		if evidence, unverified := openCodeUnverifiedActions[action]; unverified {
			descriptor.Evidence = evidence
			return descriptor
		}
		descriptor.Evidence = "No verified OpenCode gateway adapter for this action cell (OpenCode surface probed " + openCodeActionEvidenceDate + ")"
		return descriptor
	}
	descriptor.Status, descriptor.Adapter = ActionImplemented, adapter
	if evidence, ok := openCodeActionEvidence[action]; ok {
		descriptor.Evidence = evidence
	} else {
		descriptor.Evidence = "Existing gateway adapter: " + adapter
	}
	descriptor.CredentialPolicy = "OpenCode API-key account credential owner; the accepted header is per ingress (Bearer for chat/responses, x-api-key for messages, x-goog-api-key for the Google surface)"
	descriptor.ModelPolicy = "hosted OpenCode catalog only (/zen/v1/models and /zen/go/v1/models, saved as opencode_zen_models.json / opencode_go_models.json); the underlying vendor catalogue is not a hosted grant"
	descriptor.UsagePolicy = "OpenCode token usage with frozen request pricing from the published per-1M-token price tables"
	descriptor.Lifecycle = "request plan"
	switch action {
	case "count_tokens":
		descriptor.UsagePolicy = "non-billable token counting"
		descriptor.Lifecycle = "synchronous request; no upstream call"
	case "generate_content":
		descriptor.UsagePolicy = "Google usageMetadata with frozen request pricing; promptTokenCount is the total input (the OpenAI-shaped settlement subtracts the cached bucket itself), candidatesTokenCount plus thoughtsTokenCount are billed output, and the :countTokens method is a free estimate that must not be billed"
		descriptor.Lifecycle = "request plan; native Zen Google SSE or converted text SSE through the existing account-selected text forwarder"
	}
	return descriptor
}
