# Antigravity (CloudCode v1internal) action families

Antigravity is the Google Antigravity IDE backend. Its data plane is not a
public supplier API: the only surface this repository verifies is the CloudCode
`v1internal` endpoint family reached with the account's own OAuth token plus a
`project` identifier. There is therefore no first-party URL to cite; the evidence
anchors are the repository's own saved wire samples and the protocol matrix,
re-checked 2026-09-21.

## Verified surface inventory

- `internal/pkg/antigravity/client.go` builds exactly `/v1internal:<action>` and
  appends `?alt=sse` for `streamGenerateContent`.
- Saved wire samples in `internal/pkg/antigravity/client_test.go` pin
  `v1internal:generateContent` (line 37/99), `v1internal:streamGenerateContent?alt=sse`
  (line 71), `v1internal:loadCodeAssist` (line 1281) and
  `v1internal:fetchAvailableModels` (line 1476).
- Management calls also in use: `onboardUser`, `setUserSettings`, `fetchUserInfo`.
- `docs/gateway-platform-protocol-matrix.md` records the antigravity row as
  "CloudCode/Gemini conversion" with Responses POST bridged and Responses
  WebSocket/compact rejected.
- `docs/platform-runtime-verification.md` records the Antigravity API-key
  deployment as a saved `/antigravity` compatible gateway (no extrapolation to
  official CloudCode hosts).

The inventory carries no embeddings, rerank, batch, image-variation, video,
audio, voice, realtime or standalone search endpoint. Antigravity credentials are
an OAuth account plus project, or the operator's own Gemini-compatible
deployment for API-key accounts; they are not valid on the Gemini API-key
media endpoints, so no cell is filled by borrowing another supplier's
credential.

## Implemented cells

| Action | Adapter | Path |
| --- | --- | --- |
| `chat` | `gateway.chat` | `AntigravityGatewayService.ForwardAsChatCompletions` |
| `messages` | `gateway.messages` | `AntigravityGatewayService.Forward` |
| `responses` | `gateway.responses` | `AntigravityGatewayService.ForwardAsResponses` |
| `count_tokens` | `gateway.count_tokens` | `gateway_count_tokens.go` answers 404 for Antigravity accounts; clients fall back to local estimation; non-billable |
| `generate_content` | `gateway.generate_content` | `AntigravityGatewayService.ForwardGemini` on `v1internal:generateContent` |
| `image.generate` | `antigravity.images` | `ParseAntigravityImagesRequest` + `ForwardImages` on the same verified `generateContent` path with a `gemini-*-image` model |
| `image.edit` | `antigravity.images` | same path with the source image as an inline base64 part next to the edit instruction |

`native` stays honestly unverified: Antigravity publishes no separate `/api/v1`
family, and a "native" umbrella would be a rename of the same v1internal calls.

## Image actions

`image.generate` and `image.edit` are not a separate upstream endpoint. The
CloudCode `generateContent` response carries inline image parts, so the bridge
builds the same request the Antigravity OAuth path already sends
(`contents[].parts[]` with `generationConfig.responseModalities =
["TEXT","IMAGE"]` and an optional `generationConfig.imageConfig.imageSize` tier)
and converts the inline part back into the standard OpenAI images payload.

Input normalization accepts the standard entry shapes and reduces them to that
one request:

- JSON generation: `{model, prompt, n, size, response_format}`.
- JSON edit: the canonical `image` data URL or the OpenAI
  `images[].image_url` entry.
- multipart edit: the `image` file part, base64-encoded inline.

Rejected rather than silently reinterpreted: masks, more than one source image,
`images[].file_id`, non-data-URL images, streaming, `n > 1`, and any
`response_format` other than `b64_json`. Every rejection carries the reason in
the error message.

Billing reuses the shared trunk: `ForwardGemini` records `ImageCount = 1` for an
image model and the size tier from `imageConfig`, and `RecordUsage` prices it per
generated image at that tier (`calculateImageCost`). The handler performs the
same admission as the other media entries — billing eligibility before
selection, plan-gated exactly-once usage settlement after success — and never
invents a provider-specific price table.

## Hard conflicts

Recorded with dated evidence in `internal/domain/antigravity_actions.go`:
`responses.compact`, `responses.websocket`, `embeddings`, `rerank`,
`image.variation`, `image.async`, `image.batch`, `video.*`, `audio.*`, `voice`,
`realtime`, `search`, `alpha_search`, `live`. A Composite group must route these
to a platform that implements the action; the dispatch rejects them with
`400 action_not_implemented` instead of a silent 404.
