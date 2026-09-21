package domain

import "strings"

// Cursor action authority rows.
//
// Cursor is an agent product, not a model API supplier. Its verified data plane
// inside this fork is the Connect AgentService/Run protocol
// (internal/pkg/cursor) plus the configured relay form, and its published model
// facts are catalogued in the Cursor capability snapshot. Nothing in that
// surface is an embeddings, rerank, image, video, audio, voice, realtime or
// standalone search API.
//
// Evidence base, live re-review 2026-09-21. Cursor's first-party API
// documentation was fetched directly on 2026-09-21 (raw markdown copies are
// stored with the actions-cursor evidence set under
// main-work/.pi/actions-cursor/live-20260921/), and the repository-internal
// facts were re-reviewed against their saved artifacts:
//   - https://cursor.com/docs/api (fetched 2026-09-21): the published API
//     surface is Admin, Analytics, AI Code Tracking, Bugbot, Cloud Agents,
//     Origin and the SDKs, and the page states that the Cloud Agents API and
//     SDKs "are not a standalone model-inference or chat-completions API".
//   - https://cursor.com/docs/cloud-agent/api/endpoints (fetched 2026-09-21):
//     the Cloud Agents API covers agent/run lifecycle, run streaming, cancel,
//     usage, model listing and artifact download, and accepts image *inputs*
//     (prompt.images); it publishes no embeddings, rerank, image generation,
//     video, audio, voice, realtime or standalone search endpoint.
//   - backend/internal/service/cursor_model_capabilities.md, verified
//     2026-09-13 against the first-party Cursor model/pricing pages: image and
//     audio support must not be advertised without Cursor specific evidence.
//   - docs/gateway-platform-protocol-matrix.md, Cursor row: OAuth/upstream
//     account forms, Cursor conversion, bridge/rejected/rejected.
//   - The wired adapters themselves: internal/pkg/cursor (Run contract, NAL
//     field layout, text-only content codec, KV blobs, MCP tool exchange),
//     internal/service/cursor_* and routes/gateway.go "cursor.count_tokens".
//   - The archived real-machine delivery record .pi/cursor-delivery-summary.md
//     (2026-09-13) with the three-protocol, tool-call, PKCE, usage-window,
//     composite and pricing verifications.
//
// Visual and file input, the platform focus of this task, resolves as follows:
// Cursor's first-party agent surfaces publish image inputs (Cloud Agents
// prompt.images, SDK SDKImage), but the Run encoding this gateway speaks has no
// verified image or attachment field, so a native Cursor account keeps
// rejecting media content parts on every ingress (internal/pkg/cursor/
// chat_content.go) instead of claiming vision, and a relay account forwards
// the body to its configured upstream. Media input stays unverified on the
// native path with that evidence recorded here rather than written as
// unsupported; carrying it would need a Run wire sample with an image field.
//
// Missing credentials were never treated as absence of a capability, and no
// cell was written as unsupported.

// cursorPublicAPIAnchor is the first-party statement that Cursor's published
// API surface is not a model-inference API. It is the shared anchor for every
// absence row whose claim is "no such endpoint exists".
const cursorPublicAPIAnchor = "Cursor's first-party APIs overview (https://cursor.com/docs/api, verified 2026-09-21) publishes only the Admin, Analytics, AI Code Tracking, Bugbot, Cloud Agents, Origin and SDK surfaces, and states that the Cloud Agents API and SDKs are not a standalone model-inference or chat-completions API."

// cursorCloudAgentAnchor is the first-party inventory of Cursor's only public
// task API: agent runs with cancel, usage, models and artifacts, and image
// inputs. It bounds what "Cursor has a task API" may be read to mean.
const cursorCloudAgentAnchor = "The Cloud Agents API reference (https://cursor.com/docs/cloud-agent/api/endpoints, verified 2026-09-21) documents only the agent/run lifecycle, run streaming, run cancel, agent usage, model listing and artifact download on api.cursor.com, and accepts image inputs (prompt.images) without publishing any generation, editing, embedding, rerank, speech, voice, realtime or standalone search endpoint."

// cursorSnapshotEvidence is the shared anchor for the saved first-party Cursor
// capability snapshot used by the absence rows below.
const cursorSnapshotEvidence = "The saved Cursor capability snapshot (backend/internal/service/cursor_model_capabilities.md, verified 2026-09-13) forbids advertising non-text modalities without Cursor-specific evidence."

// cursorConnectEvidence names the Connect families this fork already speaks, so
// an absence claim never rests on the public API inventory alone.
const cursorConnectEvidence = "The verified Cursor Connect families remain agent.v1.AgentService/Run (internal/pkg/cursor) and aiserver.v1.DashboardService (internal/service/account_usage_service.go)."

// cursorImageInputAnchor records the visual/file-input boundary of this
// platform task: Cursor's first-party agent surfaces do accept images, while the
// Run contract this gateway speaks has no verified image field, so native
// Cursor accounts must fail closed on media parts instead of forwarding them as
// text. Relay accounts pass the request through and their configured upstream
// decides.
const cursorImageInputAnchor = "Cursor does publish image inputs in its first-party agent surfaces (Cloud Agents API prompt.images, max 5 images of 15 MB, png/jpeg/gif/webp, https://cursor.com/docs/cloud-agent/api/endpoints, and the TypeScript SDK SDKImage {url} or {data,mimeType}, https://cursor.com/docs/sdk/typescript.md, both verified 2026-09-21), but no image or attachment field is verified in the Run encoding this gateway speaks (internal/pkg/cursor/nal.go, real-machine records 2026-09-12/2026-09-13), so native Cursor accounts reject media content parts (internal/pkg/cursor/chat_content.go) and relay accounts pass the request through to their configured upstream unchanged."

// cursorAbsenceEvidence renders one absence row: the verified claim, its
// first-party anchors and the review date.
func cursorAbsenceEvidence(claim string, anchors ...string) string {
	return claim + " " + strings.Join(anchors, " ") + " Hard conflict recorded 2026-09-21."
}

// cursorActionAdapters names the adapter that serves each implemented Cursor
// cell. The chat/messages/responses trunks stay the shared gateway trunks, so
// Cursor is never renormalised into the OpenAI trunk. cursorActionMatrixTest
// asserts these stay identical to the adapters the shared matrix already
// resolves for Cursor.
var cursorActionAdapters = map[string]string{
	"chat":         "gateway.chat",
	"messages":     "gateway.messages",
	"responses":    "gateway.responses",
	"count_tokens": "cursor.count_tokens",
}

// cursorActionEvidence documents the serving adapter and its saved evidence for
// every implemented Cursor cell.
var cursorActionEvidence = map[string]string{
	"chat":         "CursorGatewayService.ForwardAsChatCompletions on the shared gateway trunk (Cursor keeps gateway.chat, never openai.chat); text content parts are flattened to the Run text contract and media content parts fail closed for native accounts (internal/pkg/cursor/chat_content.go), while relay accounts forward the body to their configured upstream; native Connect AgentService Run contract in internal/pkg/cursor/client.go and internal/pkg/cursor/nal.go, verified on real accounts 2026-09-12/2026-09-13 (.pi/cursor-delivery-summary.md); " + cursorPublicAPIAnchor + " The trunk therefore keeps serving this cell; reviewed 2026-09-21",
	"messages":     "CursorGatewayService.ForwardAsAnthropic on the shared messages trunk; three-protocol real-machine verification recorded 2026-09-13 (.pi/cursor-delivery-summary.md) with Anthropic event shapes and tool/thinking translation; non-text content blocks are rejected by cursorNormalizeRequest; reviewed 2026-09-21",
	"responses":    "CursorGatewayService.ForwardAsResponses on the shared responses trunk; PlatformDescriptor responsesPost=bridge and docs/gateway-platform-protocol-matrix.md Cursor row; streamed response.created/response.completed shapes recorded 2026-09-13; reviewed 2026-09-21",
	"count_tokens": "cursor.count_tokens local estimator (internal/handler/openai_gateway_count_tokens.go CursorCountTokens, registered in routes/gateway.go, exercised by routes/cursor_count_tokens_test.go); no Cursor token-count endpoint exists, so the cell is a non-billable local estimate; reviewed 2026-09-21",
}

// cursorHardConflicts records Cursor cells whose native or faithful bridge
// equivalent was verified absent. Absence rests on the live first-party API
// inventory (2026-09-21), the saved snapshot and the in-repo protocol matrix; a
// Composite group must route these actions to a platform that implements them
// instead of substituting another provider.
var cursorHardConflicts = map[string]string{
	"responses.compact": cursorAbsenceEvidence(
		"Cursor serves Responses as a bridge over the verified Run protocol and has no compact subpath: PlatformDescriptor responsesCompact=false (domain/platform_capabilities.go) and docs/gateway-platform-protocol-matrix.md records bridge/rejected/rejected for Cursor.",
		cursorPublicAPIAnchor),
	"generate_content": cursorAbsenceEvidence(
		"Google generativelanguage surface: neither the published API nor the wired Connect families expose a generativelanguage endpoint.",
		cursorPublicAPIAnchor, cursorConnectEvidence),
	"embeddings": cursorAbsenceEvidence(
		"No embeddings endpoint exists in Cursor's published or verified surface; the Run data plane carries text and tool turns only (internal/pkg/cursor/nal.go).",
		cursorPublicAPIAnchor, cursorCloudAgentAnchor, cursorSnapshotEvidence),
	"rerank": cursorAbsenceEvidence(
		"No rerank or scoring endpoint exists; the agent protocol has no scoring or passage-ordering API.",
		cursorPublicAPIAnchor, cursorCloudAgentAnchor),
	"image.generate": cursorAbsenceEvidence(
		"No text-to-image generation endpoint exists: Cursor accepts image inputs and returns workspace artifacts, but publishes no generation endpoint. The image generations in the 2026-09-13 delivery record belong to OpenAI accounts reached through this gateway, not to Cursor.",
		cursorPublicAPIAnchor, cursorCloudAgentAnchor, cursorImageInputAnchor, cursorSnapshotEvidence),
	"image.edit": cursorAbsenceEvidence(
		"No image editing endpoint exists; Cursor's image contract is prompt input plus artifact output, and the OpenAI /images/edits shape has no Cursor counterpart.",
		cursorPublicAPIAnchor, cursorCloudAgentAnchor),
	"image.variation": cursorAbsenceEvidence(
		"No image variation endpoint exists; the OpenAI /images/variations shape is a proprietary extension.",
		cursorPublicAPIAnchor, cursorCloudAgentAnchor),
	"image.async": cursorAbsenceEvidence(
		"No asynchronous image task exists: the only asynchronous task family Cursor publishes is the Cloud Agents agent/run lifecycle, which carries no image job, so there is no upstream task to bind an owner key, version or receipt to.",
		cursorPublicAPIAnchor, cursorCloudAgentAnchor),
	"image.batch": cursorAbsenceEvidence(
		"No batch image API exists; the OpenAI batch image family has no Cursor counterpart.",
		cursorPublicAPIAnchor, cursorCloudAgentAnchor),
	"video.generate": cursorAbsenceEvidence(
		"No video synthesis endpoint exists; the agent Run protocol produces text, thinking and tool turns only.",
		cursorPublicAPIAnchor, cursorCloudAgentAnchor, cursorSnapshotEvidence),
	"video.edit": cursorAbsenceEvidence(
		"No video editing endpoint exists.",
		cursorPublicAPIAnchor, cursorCloudAgentAnchor),
	"video.extend": cursorAbsenceEvidence(
		"No video extension endpoint exists.",
		cursorPublicAPIAnchor, cursorCloudAgentAnchor),
	"video.status": cursorAbsenceEvidence(
		"No video job API exists, so there is no task to poll; Cloud Agents durable runs are agent runs, not video jobs.",
		cursorPublicAPIAnchor, cursorCloudAgentAnchor),
	"video.content": cursorAbsenceEvidence(
		"No video result API exists, so there is no media to download or proxy; Cloud Agents artifact download returns workspace artifacts rather than generated video.",
		cursorPublicAPIAnchor, cursorCloudAgentAnchor),
	"audio.tts": cursorAbsenceEvidence(
		"No text-to-speech endpoint exists; the OpenAI /audio/speech shape has no Cursor counterpart.",
		cursorPublicAPIAnchor, cursorCloudAgentAnchor),
	"audio.stt": cursorAbsenceEvidence(
		"No speech-to-text endpoint exists; the OpenAI /audio/transcriptions shape has no Cursor counterpart.",
		cursorPublicAPIAnchor, cursorCloudAgentAnchor),
	"audio.translate": cursorAbsenceEvidence(
		"No speech translation endpoint exists; the OpenAI /audio/translations shape has no Cursor counterpart.",
		cursorPublicAPIAnchor, cursorCloudAgentAnchor),
	"voice": cursorAbsenceEvidence(
		"No voice cloning or custom-voice API exists, so there is no voice resource to own or enumerate.",
		cursorPublicAPIAnchor, cursorCloudAgentAnchor),
	"realtime": cursorAbsenceEvidence(
		"No OpenAI-style Realtime session exists: Cursor's Run stream is a Connect/gRPC duplex over HTTP/2 (internal/pkg/cursor/client.go, internal/pkg/cursor/connectrpc.go) and the Cloud Agents run stream is server-sent agent events; PlatformDescriptor responsesWS=rejected for Cursor.",
		cursorPublicAPIAnchor, cursorCloudAgentAnchor),
	"search": cursorAbsenceEvidence(
		"Cursor's web search is an agent-internal tool declared through MCP tool definitions and settled inside the Run usage (internal/pkg/cursor/tools.go), so there is no standalone search endpoint to bridge to the standard search ingress.",
		cursorPublicAPIAnchor, cursorCloudAgentAnchor),
	"alpha_search": cursorAbsenceEvidence(
		"OpenAI Codex proprietary /alpha/search surface, with no Cursor counterpart.",
		cursorPublicAPIAnchor, cursorCloudAgentAnchor),
	"live": cursorAbsenceEvidence(
		"OpenAI Codex realtime calls surface, with no Cursor counterpart; the Connect Run duplex is already served by the chat/messages/responses cells.",
		cursorPublicAPIAnchor, cursorCloudAgentAnchor),
	"responses.websocket": cursorAbsenceEvidence(
		"PlatformDescriptor responsesWS=rejected for Cursor (domain/platform_capabilities.go), and no Cursor Responses-over-WebSocket route is published.",
		cursorPublicAPIAnchor, cursorCloudAgentAnchor),
}

// cursorPendingCells records cells that are neither implemented nor verified
// absent. They stay unverified with evidence instead of being silently
// written as unsupported.
var cursorPendingCells = map[string]string{
	"native": "The native cell is the gateway's platform-native HTTP path family (/api/v1, /api-ws/v1, /compatible-mode/v1, see GatewayActionForRoute). Cursor's first-party HTTP surfaces are the Connect families this gateway already serves plus the public api.cursor.com families described by " + cursorPublicAPIAnchor + " " + cursorConnectEvidence + " The gateway registers no Cursor-native ingress path family, and route registration belongs to multimodal-ingress, so this cell stays unverified rather than claimed (reviewed 2026-09-21).",
}

// CursorAction resolves one Cursor cell to its tri-state.
//
// The platform action matrix must consult it for PlatformCursor before the
// generic adapter fallback, mirroring the array provider rows:
//
//	if identity.Platform == PlatformCursor {
//		return CursorAction(identity, action)
//	}
//
// Until that hook lands the Cursor cells keep their current tri-state, and no
// cell is claimed as implemented without a recorded evidence row.
func CursorAction(identity ActionIdentity, action string) ActionDescriptor {
	descriptor := ActionDescriptor{Identity: identity, Action: action, Status: ActionUnverified, Evidence: "No verified Cursor gateway adapter for this action cell (reviewed 2026-09-21)"}
	if identity.Platform != PlatformCursor {
		return descriptor
	}
	// Hosted identity dimensions never manufacture Cursor adapters: Bedrock and
	// service-account credentials are not Cursor account forms.
	if identity.HostedAccountType == AccountTypeBedrock || identity.HostedAccountType == AccountTypeServiceAccount {
		descriptor.Evidence = "Cursor does not use hosted Bedrock or service-account credentials, so no Cursor cell applies to this identity (reviewed 2026-09-21)"
		return descriptor
	}
	if adapter, implemented := cursorActionAdapters[action]; implemented {
		evidence, recorded := cursorActionEvidence[action]
		if !recorded {
			descriptor.Evidence = "Adapter " + adapter + " is resolved for Cursor but no evidence row is recorded, so the cell is not claimed (reviewed 2026-09-21)"
			return descriptor
		}
		descriptor.Status, descriptor.Adapter = ActionImplemented, adapter
		descriptor.Evidence = evidence
		descriptor.CredentialPolicy = cursorCredentialPolicy(identity.HostedAccountType)
		descriptor.ModelPolicy = "resolved account model evidence (Cursor picker or preset snapshot), variant resolution and task limits"
		descriptor.UsagePolicy = "turn_ended authoritative usage with an explicit estimated fallback, priced by the frozen request plan and the group model card"
		descriptor.Lifecycle = "request plan; client cancellation propagates to the Run duplex stream"
		if action == "count_tokens" {
			descriptor.UsagePolicy = "non-billable local token estimate; no Cursor token-count endpoint exists"
			descriptor.Lifecycle = "synchronous request without an upstream call"
		}
		return descriptor
	}
	if conflict, recorded := cursorHardConflicts[action]; recorded {
		descriptor.Status, descriptor.Adapter, descriptor.Evidence = ActionHardConflict, "", conflict
		return descriptor
	}
	if pending, recorded := cursorPendingCells[action]; recorded {
		descriptor.Evidence = pending
		return descriptor
	}
	return descriptor
}

// cursorCredentialPolicy distinguishes the three verified Cursor account forms.
func cursorCredentialPolicy(hostedAccountType string) string {
	switch hostedAccountType {
	case AccountTypeOAuth:
		return "Cursor auto session credentials (access token, refresh token, machine identity) with browser PKCE reauthorization and CAS refresh; native agent Run"
	case AccountTypeAPIKey:
		return "Cursor official API key plus the account machine identity; native agent Run"
	case AccountTypeUpstream:
		return "Cursor relay account: configured base URL, API key, api_protocol and header overrides; no native session credentials"
	}
	return "Cursor ingress-bound account credential owner; native agent Run for oauth and apikey accounts, configured relay for upstream accounts"
}
