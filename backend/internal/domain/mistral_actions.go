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
