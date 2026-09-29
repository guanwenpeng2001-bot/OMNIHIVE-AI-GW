# DashScope action families: rerank, text embedding, voice enrollment, async text-to-image, VACE video edit/extend, task content

All shapes, models and prices in these adapters were verified against official
Alibaba Cloud Model Studio documentation on 2026-09-22 (evidence set:
`.pi/actions-qianwen/`). Anything without first-party evidence stays rejected
instead of guessed.

## Synchronous native resource families

`POST /api/v1/services/rerank/text-rerank/text-rerank` (gte-rerank-v2,
qwen3-vl-rerank text-only) and
`POST /api/v1/services/embeddings/text-embedding/text-embedding`
(text-embedding-v3/v4, qwen3.7-text-embedding[-flash]) preserve the native wire
shape end to end. Both map the documented `usage.total_tokens` onto input
tokens, so the shared token billing pipeline prices them exactly like imported
`dashscope-cn-standard` cards (Beijing list: rerank 0.8/0.5, embedding
0.5/0.125 CNY per 1M tokens). Admission is the adapter's explicit model
allowlist plus parameter validation; a request plan is intentionally not
installed before scheduling, the same discipline as the async video family,
because plan-gated eligibility currently requires text endpoint evidence that
these model families cannot carry yet.

`POST /api/v1/services/audio/tts/customization` admits qwen-voice-enrollment
`create`/`list`/`delete` only (the CosyVoice `voice-enrollment` model has no
verified current tariff). Creation bills per created clone at the documented
Beijing 0.01 CNY tariff and requires the account's operator FX provenance
(`extra.qianwen_pricing_sync`) before any upstream call; list/delete are the
documented free operations (`usage.count` 0). The clone count is persisted in
`BillingTier` as `qianwen_voice_v1:clones=N`, never as tokens.

## Standard-ingress bridges

`QianwenEmbeddings` forwards the OpenAI shape to compatible-mode
`/embeddings` (models text-embedding-v3/v4). `QianwenImages` forwards the flat
images shape to compatible-mode `/images/generations` (qwen-image-3.0 family
only; usage `output_image_count`/`output_width`/`output_height` feeds the
shared image billing). `QianwenRerank` converts the flat
`{model, query, documents, top_n}` shape to the native rerank body and back,
always requesting document echo. Dispatch-map wiring of these handlers belongs
to multimodal-ingress; until then the standard routes fail closed.

## Async text-to-image (family `image`)

`POST /api/v1/services/aigc/text2image/image-synthesis` with
`X-DashScope-Async: enable` joins the shared upstream job lifecycle
(submit-claim before network, timing-wheel poll, exactly-once settlement).
Beijing per-image tariffs: wan2.5-t2i-preview 0.20, wan2.2-t2i-plus 0.20,
wan2.2-t2i-flash 0.14, wanx2.1-t2i-plus 0.20, wanx2.1-t2i-turbo 0.14,
wanx2.0-t2i-turbo 0.04 CNY; settled quantity is the authoritative
`usage.image_count` with per-result URL evidence, never the requested `n`.
Migration 272 extends the family CHECK. The task status query is the shared
owner-scoped `GET /api/v1/tasks/{id}`; the provider documents no task cancel
endpoint, and no gateway-specific one is invented.

## VACE video edit/extend

`wanx2.1-vace-plus` (EN: wan2.1-vace-plus) on the same video-synthesis
endpoint with `input.function` `video_edit` (one mask URL, optional
`mask_frame_id`/`mask_type`/`expand_ratio`) or `video_extension`
(`first_clip_url`). Only the documented 720P tier is admitted; the operator
group per-second price is required like other Wan models (official anchor
0.70 CNY/s Beijing). Settlement reuses the video charge path
(`usage.video_duration`, count 1, tier evidence).

## Task content download

`GET /api/v1/tasks/{id}/content` streams a settled video result to the owning
key only — same ownership filter as the status query, upstream task IDs never
exposed. The download is an unsigned request pinned to documented
`dashscope-result-*.aliyuncs.com` hosts with no redirects and a 256 MiB bound.
