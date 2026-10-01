package domain

import "strings"

// Mistral publishes Chat Completions, not native Messages or Responses URLs.
// Public Messages and Responses use the existing OpenAI Chat converters; this
// bridge declaration never grants a vendor endpoint.

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
	"messages":   "openai.messages",
	"responses":  "openai.responses",
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
	"messages":   "Public Messages converted through forwardAnthropicViaRawChatCompletions to the existing Mistral Chat endpoint; no native Messages URL (mistralai/platform-docs-public openapi.yaml, retrieved 2026-09-21)",
	"responses":  "Public Responses converted through forwardResponsesViaRawChatCompletions to the existing Mistral Chat endpoint; no native Responses URL (mistralai/platform-docs-public openapi.yaml, retrieved 2026-09-21)",
	"chat":       "openai.chat trunk to POST {base}/v1/chat/completions with Authorization: Bearer, the only text protocol the official specification enumerates (mistralai/platform-docs-public openapi.yaml, retrieved 2026-09-21). service/mistral_adapter.go fixes the request normalisation and keeps an unobserved stream unbilled instead of reporting zero usage",
	"embeddings": "mistral.embeddings handler to POST {base}/v1/embeddings, the native EmbeddingRequest endpoint the official specification enumerates (mistralai/platform-docs-public openapi.yaml, retrieved 2026-09-21). service/mistral_actions.go fixes the wire shape and bridges OpenAI `dimensions` onto the native `output_dimension` because the native schema sets additionalProperties: false; usage.prompt_tokens and usage.completion_tokens must both be present before the response settles billable, so unknown usage never becomes a trusted zero",
	"audio.stt":  "mistral.audio.stt handler to multipart POST {base}/v1/audio/transcriptions, the Voxtral transcription endpoint the official specification enumerates (mistralai/platform-docs-public openapi.yaml, retrieved 2026-09-21). service/mistral_actions.go fixes the multipart field set and the streaming variant, whose SSE terminal event transcription.done carries usage.prompt_audio_seconds; that duration settles on the shared stt hour unit",
	"audio.tts":  "mistral.audio.tts handler to POST {base}/v1/audio/speech, the native SpeechRequest endpoint the official specification enumerates (mistralai/platform-docs-public openapi.yaml, retrieved 2026-09-21). service/mistral_actions.go fixes the \"audio_data\": base64 envelope and the streaming terminal event speech.audio.done, and counts the input runes on the shared tts million-character unit",
	"voice":      "mistral.voice handler to the native voice-management API {base}/v1/audio/voices and its {voice_id}/sample subresource, which the official specification enumerates (mistralai/platform-docs-public openapi.yaml, retrieved 2026-09-21). service/mistral_actions.go fixes the accepted method set and attaches no usage because voice management is not a billable generation action",
}

// mistralActionDescriptor finalizes the tri-state for every Mistral cell: the
// public text cells bind conversion adapters; other cells are bound to native
// adapters or explicitly unverified with the evidence that produced the status. It never invents an adapter and never widens a non-Mistral cell.
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
