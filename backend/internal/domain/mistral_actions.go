package domain

import "strings"

// Mistral action authority rows. Mistral is a chat-only entry: its data plane is
// the OpenAI-shaped /v1 chat trunk plus dedicated vector and audio endpoints, and
// it publishes neither the OpenAI Responses protocol nor the Anthropic Messages
// protocol.
//
// Evidence: the official specification (mistralai/platform-docs-public
// openapi.yaml, retrieved 2026-09-21) enumerates /v1/chat/completions,
// /v1/embeddings, /v1/audio/transcriptions, /v1/audio/speech and /v1/audio/voices
// and publishes no /v1/responses and no /v1/messages path. The same surface is
// implemented in service/mistral_actions.go, whose header fixes the endpoint
// shapes against that specification. Both cells are therefore verified absences,
// not unbound adapters: no native or faithful contract exists to bind, and the
// gateway must answer with an explicit action rejection instead of a silent 404.

// mistralHardConflicts records the protocol cells Mistral provably does not
// publish, with the dated first-party evidence that produced the verdict.
var mistralHardConflicts = map[string]string{
	"messages":  "Mistral publishes no Anthropic Messages endpoint: the official specification (mistralai/platform-docs-public openapi.yaml, retrieved 2026-09-21) enumerates /v1/chat/completions, /v1/embeddings, /v1/audio/transcriptions, /v1/audio/speech and /v1/audio/voices with no /v1/messages path, so no native or faithful Messages contract exists to bind",
	"responses": "Mistral publishes no OpenAI Responses endpoint: the official specification (mistralai/platform-docs-public openapi.yaml, retrieved 2026-09-21) enumerates no /v1/responses path, and the chat trunk is the platform's only text protocol, so no native or faithful Responses contract exists to bind",
}

// mistralAbsentProtocolCell records a verified absence of the chat-only Mistral
// entry. It only ever replaces an unbound cell: a bound adapter (the chat trunk)
// always wins, and every other cell keeps the generic resolution.
func mistralAbsentProtocolCell(descriptor ActionDescriptor, action, adapter string) (ActionDescriptor, bool) {
	if strings.TrimSpace(adapter) != "" {
		return descriptor, false
	}
	evidence, conflict := mistralHardConflicts[action]
	if !conflict {
		return descriptor, false
	}
	descriptor.Status, descriptor.Adapter = ActionHardConflict, ""
	descriptor.Evidence = evidence
	return descriptor, true
}

// Dispatch keys this adapter needs registered in service.GatewayActionHandlers
// (routes are registered once by the multimodal ingress, never from this
// branch). The chat trunk needs no entry: it binds the shared openai.chat key.
//
//	mistral.embeddings  -> OpenAIGatewayHandler.MistralEmbeddings
//	mistral.audio.stt   -> OpenAIGatewayHandler.MistralAudioTranscriptions
//	mistral.audio.tts   -> OpenAIGatewayHandler.MistralAudioSpeech
//	mistral.voice       -> OpenAIGatewayHandler.MistralVoices

// mistralActionAdapters binds the Mistral cells an API-key account can execute
// today. Every entry is implemented by service/mistral_actions.go and wrapped by
// handler/mistral_actions.go; the chat cell keeps the existing OpenAI-shaped
// trunk because the official specification publishes no second text protocol.
var mistralActionAdapters = map[string]string{
	"chat":       "openai.chat",
	"embeddings": "mistral.embeddings",
	"audio.stt":  "mistral.audio.stt",
	"audio.tts":  "mistral.audio.tts",
	"voice":      "mistral.voice",
}

// mistralActionEvidence documents the adapter and the first-party specification
// evidence for every Mistral cell whose adapter exists in this branch. Each row
// names the endpoint the official specification enumerates and the file that
// fixes the wire shape, so no row rests on an unsourced "officially supported".
var mistralActionEvidence = map[string]string{
	"chat":       "openai.chat trunk to POST {base}/v1/chat/completions with Authorization: Bearer, the only text protocol the official specification enumerates (mistralai/platform-docs-public openapi.yaml, retrieved 2026-09-21). service/mistral_adapter.go fixes the request normalisation and keeps an unobserved stream unbilled instead of reporting zero usage",
	"embeddings": "mistral.embeddings handler to POST {base}/v1/embeddings, the native EmbeddingRequest endpoint the official specification enumerates (mistralai/platform-docs-public openapi.yaml, retrieved 2026-09-21). service/mistral_actions.go fixes the wire shape and bridges OpenAI `dimensions` onto the native `output_dimension` because the native schema sets additionalProperties: false; usage.prompt_tokens and usage.completion_tokens must both be present before the response settles billable, so unknown usage never becomes a trusted zero",
	"audio.stt":  "mistral.audio.stt handler to multipart POST {base}/v1/audio/transcriptions, the Voxtral transcription endpoint the official specification enumerates (mistralai/platform-docs-public openapi.yaml, retrieved 2026-09-21). service/mistral_actions.go fixes the multipart field set and the streaming variant, whose SSE terminal event transcription.done carries usage.prompt_audio_seconds; that duration settles on the shared stt hour unit",
	"audio.tts":  "mistral.audio.tts handler to POST {base}/v1/audio/speech, the native SpeechRequest endpoint the official specification enumerates (mistralai/platform-docs-public openapi.yaml, retrieved 2026-09-21). service/mistral_actions.go fixes the \"audio_data\": base64 envelope and the streaming terminal event speech.audio.done, and counts the input runes on the shared tts million-character unit",
	"voice":      "mistral.voice handler to the native voice-management API {base}/v1/audio/voices and its {voice_id}/sample subresource, which the official specification enumerates (mistralai/platform-docs-public openapi.yaml, retrieved 2026-09-21). service/mistral_actions.go fixes the accepted method set and attaches no usage because voice management is not a billable generation action",
}

// mistralActionDescriptor finalizes the tri-state for every Mistral cell: the
// verified protocol absences are already resolved by mistralAbsentProtocolCell
// before this finalizer runs, so a cell reaching this function is either bound to
// an adapter or explicitly unverified with the verification result that produced
// the status. It never invents an adapter and never widens a non-Mistral cell.
func mistralActionDescriptor(descriptor ActionDescriptor, action, adapter string) ActionDescriptor {
	if hosted := strings.TrimSpace(descriptor.Identity.HostedAccountType); hosted != "" && hosted != AccountTypeAPIKey {
		descriptor.Evidence = "Mistral data-plane actions authenticate with the account API key (Authorization: Bearer); hosted credential type " + hosted + " carries no Mistral data-plane credential (mistralai/platform-docs-public openapi.yaml, retrieved 2026-09-21)"
		return descriptor
	}
	bound, implemented := mistralActionAdapters[action]
	serving := strings.TrimSpace(adapter)
	if serving != "" {
		bound = serving
	}
	if implemented || serving != "" {
		descriptor.Status, descriptor.Adapter = ActionImplemented, bound
		if evidence, ok := mistralActionEvidence[action]; ok {
			descriptor.Evidence = evidence
		} else {
			descriptor.Evidence = "Existing gateway adapter: " + bound
		}
		descriptor.CredentialPolicy = "mistral API-key account credential owner (Authorization: Bearer); base URL is the account base_url or the official https://api.mistral.ai/v1"
		descriptor.ModelPolicy = "resolved account model evidence and Mistral model limits"
		descriptor.UsagePolicy = "Mistral response usage with frozen request pricing"
		descriptor.Lifecycle = "request plan"
		switch action {
		case "embeddings":
			descriptor.UsagePolicy = "Mistral usage.prompt_tokens/completion_tokens; both counters must be present before the response settles billable"
			descriptor.Lifecycle = "synchronous request"
		case "audio.stt":
			descriptor.UsagePolicy = "recognised audio seconds (usage.prompt_audio_seconds) on the shared stt hour unit; the streaming variant settles on the transcription.done event"
			descriptor.Lifecycle = "synchronous request; the stream variant relays to its terminal event"
		case "audio.tts":
			descriptor.UsagePolicy = "rune-counted input characters on the shared tts million-character unit"
			descriptor.Lifecycle = "synchronous request; the stream variant relays to its terminal event"
		case "voice":
			descriptor.UsagePolicy = "voice management is not a billable generation action, so the adapter attaches no usage"
			descriptor.Lifecycle = "synchronous request returning the voice record"
		}
		return descriptor
	}
	descriptor.Evidence = "No verified Mistral gateway adapter for this action cell (mistralai/platform-docs-public openapi.yaml, retrieved 2026-09-21)"
	return descriptor
}
