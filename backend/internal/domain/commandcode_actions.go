package domain

// Command Code Provider API action authority rows.
//
// Evidence: the first-party Provider API reference
// (https://commandcode.ai/docs/provider, captured 2026-09-21) documents exactly
// four endpoints under https://api.commandcode.ai/provider/v1 - POST
// /chat/completions (OpenAI Chat Completions), POST /responses (OpenAI
// Responses), POST /messages (Anthropic Messages) and GET /models. The per-model
// `supported_endpoints` field is the runtime authority for which route serves a
// model, which is why this file records endpoint selection rather than a static
// capability grant.
//
// The tables were authored by the actions-commandcode branch and are bound to the
// authoritative matrix here by multimodal-ingress, so Composite resolution and
// the account model binding read the same cells the platform adapter publishes.

// commandCodeBinding names the serving adapter and its protocol evidence.
type commandCodeBinding struct {
	adapter  string
	evidence string
}

// commandCodeImplemented and commandCodeHardConflicts are the platform cells.
var (
	commandCodeImplemented = map[string]commandCodeBinding{
		"chat": {
			adapter:  "openai.chat",
			evidence: "POST /provider/v1/chat/completions, OpenAI Chat Completions schema; account api_protocol/protocol_rules select this endpoint for non-Claude models (official Provider API reference, 2026-09-21)",
		},
		"messages": {
			adapter:  "openai.messages",
			evidence: "POST /provider/v1/messages, Anthropic Messages schema; Claude models are served only here, non-Claude models use the Chat Completions bridge (official Provider API reference, 2026-09-21)",
		},
		"responses": {
			adapter:  "openai.responses",
			evidence: "POST /provider/v1/responses executes natively when selected by account protocol, matching protocol rule, or discovered supported_endpoints; missing metadata keeps the Chat fallback and Claude defaults to Messages (official Provider API reference, 2026-10-01)",
		},
		"count_tokens": {
			adapter:  "openai.count_tokens",
			evidence: "local token-estimation bridge; the Provider API reference (2026-09-21) documents no token-count endpoint, so nothing is forwarded upstream and nothing is billed",
		},
	}
	commandCodeHardConflicts = map[string]string{
		"video.cancel":        "the Provider API reference (2026-09-21) documents only chat/completions, responses, messages and models; no video job API exists to cancel",
		"responses.compact":   "the Provider API reference (2026-09-21) documents four endpoints only; no Responses compact subpath exists",
		"generate_content":    "Gemini generativelanguage surface; the Provider API exposes no Google-native endpoint (2026-09-21)",
		"embeddings":          "no embeddings endpoint appears in the documented endpoint table; the catalog is a chat/responses proxy (2026-09-21)",
		"rerank":              "no rerank endpoint is documented for the Provider API (2026-09-21)",
		"image.generate":      "the FAQ states a message may carry text and images only, so images are input; no image generation endpoint is documented (2026-09-21)",
		"image.edit":          "no image editing endpoint is documented; the FAQ admits no image output at all (2026-09-21)",
		"image.variation":     "no /images/variations equivalent is documented; the FAQ admits no image output at all (2026-09-21)",
		"image.async":         "no asynchronous image task endpoint is documented; the four documented endpoints are all synchronous request/stream (2026-09-21)",
		"image.batch":         "no batch image endpoint is documented (2026-09-21)",
		"video.generate":      "no video endpoint is documented, and the FAQ rejects document and file parts that a video action would need (2026-09-21)",
		"video.edit":          "no video endpoint is documented (2026-09-21)",
		"video.extend":        "no video endpoint is documented (2026-09-21)",
		"video.status":        "no video task surface exists, so there is no task to query (2026-09-21)",
		"video.content":       "no video task surface exists, so there is no content to download (2026-09-21)",
		"audio.tts":           "the FAQ states audio parts are rejected by the schema and no audio endpoint is documented (2026-09-21)",
		"audio.stt":           "the FAQ states audio parts are rejected by the schema and no audio endpoint is documented (2026-09-21)",
		"audio.translate":     "the FAQ states audio parts are rejected by the schema; no /audio/translations equivalent is documented (2026-09-21)",
		"voice":               "no voice or custom-voice endpoint is documented (2026-09-21)",
		"realtime":            "no realtime or WebSocket endpoint is documented; the four documented endpoints are HTTP request/SSE (2026-09-21)",
		"search":              "no standalone search endpoint is documented; web_search exists only as a Responses built-in tool forwarded upstream for a verdict, which the responses cell already covers (2026-09-21)",
		"alpha_search":        "OpenAI Codex proprietary surface; no Provider API equivalent is documented (2026-09-21)",
		"live":                "OpenAI Codex live surface; no Provider API equivalent is documented (2026-09-21)",
		"responses.websocket": "the Provider API is HTTP/SSE only; no Responses-over-WebSocket endpoint is documented (2026-09-21)",
		"native":              "the adapter surface is the four documented /provider/v1 endpoints; this platform exposes no provider-native /api/v1 family (2026-09-21)",
	}
)

// commandCodeActionCredentialPolicy and the remaining policies describe the one
// credential surface the Provider API documents: Bearer on any route, x-api-key
// on /messages, with a frozen request price and upstream-reported usage.
const (
	commandCodeActionCredentialPolicy = "commandcode API-key account credential owner; Bearer is documented on any route and x-api-key on /messages"
	commandCodeActionModelPolicy      = "resolved account model evidence, supported_endpoints and task limits"
	commandCodeActionUsagePolicy      = "upstream usage in the stream terminal event with frozen request pricing; explicit official free models settle at zero cost"
	commandCodeActionLifecycle        = "request plan"
)

// commandCodeActionDescriptor finalizes the tri-state for every Command Code
// cell. Implemented cells carry the adapter, evidence and policy bindings;
// verified absences become hard conflicts with evidence so Composite target
// selection never routes an action this platform cannot serve.
func commandCodeActionDescriptor(descriptor ActionDescriptor, action string) ActionDescriptor {
	if binding, implemented := commandCodeImplemented[action]; implemented {
		descriptor.Status, descriptor.Adapter = ActionImplemented, binding.adapter
		descriptor.Evidence = binding.evidence
		descriptor.CredentialPolicy = commandCodeActionCredentialPolicy
		descriptor.ModelPolicy = commandCodeActionModelPolicy
		descriptor.UsagePolicy = commandCodeActionUsagePolicy
		descriptor.Lifecycle = commandCodeActionLifecycle
		if action == "count_tokens" {
			descriptor.UsagePolicy = "non-billable local token estimation; no Provider API token-count endpoint documented"
			descriptor.Lifecycle = "synchronous request"
		}
		return descriptor
	}
	if evidence, conflict := commandCodeHardConflicts[action]; conflict {
		descriptor.Status = ActionHardConflict
		descriptor.Evidence = evidence
		return descriptor
	}
	descriptor.Evidence = "No verified Command Code gateway adapter for this action cell (evidence reviewed 2026-09-21)"
	return descriptor
}
