# T1 catalog contract — v0.3.2

Applies to `GET /api/v2/public/models` and `GET /api/v2/workspaces/:w/projects/:p/models`, and their detail/compare projections. List summaries remain under `data.items[]`; detail summaries remain under `data.model`.

| Summary field | Values / meaning |
| --- | --- |
| `availability_state` | New optional string (`omitempty`): `live` when an existing eligible channel/pool offer backs this entry; `catalog_only` for inventory without such an offer. This is descriptive snapshot state, not inference authorization. |
| `catalog_state` | `listed` for both states. |
| `price_state` | Existing `priced`, `free`, `unpriced`, `error`. Catalog-only entries are `unpriced`, with null `input_rate` / `output_rate`; catalog-only offers never overwrite or dilute a live offer's price. |
| `access_state` | Public: `requires_scope`. Scoped: existing `allowed` / `denied` rules based on bindable groups; inventory alone never grants access. |
| `route_state` | Public: `requires_scope`. Scoped catalog-only: `unknown`; live scoped offers retain `eligible`. Catalog-only entries never contribute eligible billing groups. |
| `modalities` | Sorted, unique strings: `text`, `image`, `video`, `audio`, `embedding`, `3d`; existing `pdf` is preserved. Unknown facts remain `[]`. |
| `actions` | Sorted, unique descriptive actions. The task mapping below adds capabilities even when no gateway adapter supports their invocation. Existing adapter action labels remain compatible. |
| `display_name`, `context_tokens` | Approved registry facts; consistent names across profiles are retained. Context remains a decimal string or null; conflicting/unknown context is not invented. |

| Approved task type | Added modality | Added action |
| --- | --- | --- |
| `text_generation`, `code_generation` | `text` | `text.chat` |
| `image_generation` | `image` | `image.generate` |
| `video_generation` | `video` | `video.generate` |
| `audio_generation` | `audio` | `audio.generate` |
| `speech` | `audio` | `audio.speech` |
| `transcription` | `audio`, `text` | `audio.transcription` |
| `embedding` | `embedding` | `text.embeddings` |
| `realtime` | `audio`, `text` | `realtime` |
| `three_dimensional_generation` | `3d` | `3d.generate` |

Explicit registry input/output modalities are unioned with the task modalities. Existing method-derived actions such as `image.edit`, `text.count_tokens`, `audio.tts`, and `audio.stt` may also appear. Neither `actions` nor `modalities` promises an executable endpoint. Existing `modality` and `action` query filters work for catalog-only entries. There is no new availability query parameter.

`reference_price` remains an optional **detail-level** object, not a list summary field. Existing pi reference prices are populated when available (`state: "reference"`, `source: "pi-ai"`); they never become operator tariffs or quotes. Missing reference prices remain omitted.

Inventory comes from the frozen approved registry (presets plus exact capability records) for concrete platforms configured by visible active groups, their active channels, or their active accounts. Composite groups expand only those linked platforms. Scheduling pauses, account allowlists, quota, and missing tariffs do not erase inventory. Public exclusive groups remain excluded; scoped group/channel/account and credential-parent project boundaries remain enforced. No configured groups/platforms means no registry-wide expansion.

Public facts/capabilities remain `source: "approved_registry"`; account metadata, endpoints, metrics, and unpublished account discovery IDs remain private. Operator-published names remain listed through outages. Scoped views additionally retain identity-valid discovered models, including unsupported modalities, while hiding mapped upstream targets. These observations never make a model eligible.

The Model Plaza frontend uses v2. `/api/v1/model-plaza` retains its existing live group/pricing contract and exact fixture counts. Gateway `VisibleModelResolver`, account eligibility/allowlists, and tariff enforcement are unchanged.

Discovery: Kimi Coding CN/global now request the same-profile `/coding/v1/models` with saved API-key authentication, preserving pagination and provider errors. Native `supports_reasoning` / `supports_vision` metadata is decoded. Ark PAYG now requests its documented `/api/v3/models`; unconfirmed coding/regional profiles stay closed. Discovery IDs are preserved independently of endpoint eligibility; an OAuth subscription's limited response is not inflated into full account entitlement.

Endpoint references: [Kimi model configuration](https://www.kimi.com/en/help/kimi-code/models), [Ark Models API](https://docs.volcengine.com/docs/ark/models-api?lang=en). No live account credentials or generation requests were used for verification.
