# Composite Groups

Composite groups are an admin routing layer for API keys that should choose a
concrete provider from the requested model instead of binding the key to a
single provider group. They support both built-in model detection and an
admin-configured model route registry for public model aliases.

## Supported Providers

Composite groups can route to these concrete account platforms:

- Anthropic
- Gemini
- OpenAI
- Antigravity
- Grok
- Kimi
- Zhipu GLM
- DeepSeek
- MiniMax
- OpenCode Go / Zen (`opencode_go`)
- Cursor
- Qianwen
- CommandCode
- Volcengine
- Qianfan
- Hunyuan
- Mistral
- Bedrock (separate platform; legacy Anthropic Bedrock accounts remain supported)

This list follows the concrete platform descriptor set. It does not grant every
platform every protocol: Volcengine, Qianfan, Hunyuan and Mistral currently admit
Chat Completions only; Hunyuan's unverified stream path remains rejected. Bedrock
also admits Messages and Responses POST through its existing adapters. Account
model evidence, endpoint eligibility, mode and credentials still decide each
request. See the [gateway platform/protocol matrix](gateway-platform-protocol-matrix.md)
for exact registered methods, individual proprietary actions and known limits.

The selected concrete platform is used for account selection, user platform
quota checks, post-usage billing, ops error platform attribution, channel
mapping/pricing lookup, and platform usage reporting.

## Route Registry

Admins can configure routes on a composite group from the group list's
`Routes` action or through the admin API:

- `GET /api/v1/admin/groups/:id/composite-routes`
- `POST /api/v1/admin/groups/:id/composite-routes`
- `PUT /api/v1/admin/groups/:id/composite-routes/:route_id`
- `DELETE /api/v1/admin/groups/:id/composite-routes/:route_id`
- `POST /api/v1/admin/groups/:id/composite-routes/preview`

Each route belongs to one composite group and contains:

- `public_model`: model identifier the client sends.
- `match_type`: `exact` or `prefix`.
- `target_platform`: concrete provider platform of the primary target. It mirrors the first enabled entry of `targets` for rollback compatibility.
- `upstream_model`: model identifier sent upstream. If omitted, the public
  model is reused.
- `endpoint`: `any`, `messages`, `count_tokens`, `responses`,
  `chat_completions`, `embeddings`, `images`, or `gemini`.
- `priority`: lower values win after match specificity.
- `enabled`: disabled routes are ignored by runtime resolution but remain
  visible to admins.
- `targets` (optional): the ordered fallback list described below.

### Ordered fallback targets

One route may declare an ordered `targets` list (at most 16). This is the only
cross-provider fallback mechanism: targets are explicit and admin-declared,
never inferred. Each target contains:

- `target_platform` (required): a concrete provider platform, never `composite`.
- `account_type` (optional): hosted account type such as `bedrock` or a Vertex
  `service_account`; it must be valid for the target platform.
- `upstream_model` (optional): model sent upstream; omitted means the public model.
- `region` / `provider` (optional): constraints the selected account must satisfy.
  An account without the attribute does not satisfy an explicit constraint, so a
  constrained target fails closed instead of leaking traffic to an unverified account.
- `latency_preference` (optional): `default` keeps the existing account order,
  `low_latency` biases selection towards the account with the best recently
  observed first-token latency.
- `scheduling_weight` (optional, 0–10000): relative scheduling weight inside the
  same route; `0` is stored as the default `100`. Weights compare targets of one
  route only and never override hard concurrency limits.
- `affinity_ttl_seconds` (optional, 0 or 60–86400): per-target sticky-session TTL.
- `enabled`: disabled targets are skipped. A route whose targets are all disabled
  is rejected; at least one must stay enabled.

Runtime behavior:

- All targets of one route share the public model and the request action; only the
  concrete provider, upstream model, constraints and scheduling preferences differ.
- Targets are walked in the declared order. A target whose platform has no adapter
  for the request action is skipped in favour of the next enabled target; when no
  remaining target can serve that action the request fails explicitly instead of
  silently using a single target. The request never silently changes to another
  model or another public semantic.
- A route without `targets` keeps the legacy single-target behavior built from its
  own columns, so pre-existing routes are unchanged.
- Deleting or clearing `targets` (an explicit empty array) restores that legacy
  single-target behavior.

Resolution order is explicit route first, then unambiguous account-level model
ownership, then built-in detection. When more than one explicit route matches,
exact matches beat prefix matches, endpoint-specific routes beat `any`, longer
prefixes beat shorter prefixes, then lower `priority`, then lower route id.

For JSON-body endpoints, the gateway rewrites the request `model` field to the
route's `upstream_model` before dispatch. For Gemini native paths such as
`/v1beta/models/{model}:generateContent`, the gateway resolves `{model}` and
the handler forwards the resolved upstream model.

Codex Alpha Search and Live requests use the `responses` route domain. Live
requests resolve the model from `session.model`, including multipart `session`
payloads, and apply the configured `upstream_model` before dispatch.
Codex model manifest requests reuse the existing OpenAI account selection and
failover path within the Composite group.

## Built-In Detection

Built-in detection is a **fallback for unambiguous, vendor-specific model
names**. It is not prefix routing for arbitrary names, and it never overrides an
explicit route. Two forms are recognised:

1. **Provider-prefixed ids** (`<provider>/<model>`), for example `kimi/kimi-k2`.
   A leading `models/` is stripped first, so `models/gemini-2.5-flash` is treated
   as the bare vendor-specific name below and `models/minimax-01` still needs an
   explicit route.

   | Prefix | Platform |
   | --- | --- |
   | `qianwen/`, `dashscope/` | Qianwen |
   | `anthropic/`, `claude/` | Anthropic |
   | `openai/`, `chatgpt/` | OpenAI |
   | `google/`, `google-ai-studio/`, `gemini/` | Gemini |
   | `xai/`, `x-ai/`, `grok/` | Grok |
   | `kimi/`, `moonshot/` | Kimi |
   | `zhipu/`, `glm/`, `bigmodel/` | Zhipu GLM |
   | `deepseek/` | DeepSeek |
   | `minimax/` | MiniMax |

2. **Bare vendor-specific ids**:

   - `claude-*` and `anthropic.claude-*` route to Anthropic.
   - `gpt-*`, `chatgpt-*`, `codex-*`, `text-embedding-*`, `text-moderation-*`,
     `omni-moderation-*`, `dall-e-*`, `gpt-image-*`, `tts-*`, `whisper-*`, and the
     `o1`–`o5` series route to OpenAI.
   - `gemini-*` and `learnlm-*` route to Gemini.
   - `grok` and `grok-*` route to Grok.
   - `k3`, `k3-256k`, `moonshot-*` route to Kimi.
   - `abab5*`, `abab6*` and `abab7*` route to MiniMax.

**Shared model names always need an explicit route.** The bare names `qwen*`,
`deepseek-*`, `glm-*`, `kimi-*` and `minimax-*` are served by more than one
platform, so detection deliberately fails closed for them instead of guessing a
provider. Add an explicit `targets`/route entry for those names (or send the
provider-prefixed id such as `kimi/kimi-k2`).

An unrecognised or ambiguous model name fails closed with a client error instead
of guessing a provider. There is no automatic prefix routing, no "try every
provider named by the prefix" behaviour, and no implicit fallback across
platforms: only the targets an admin declared on a matching route are used.

When detection has no match, the resolver first checks whether exactly one
provider platform exposes the name through account-level model mappings. An
ambiguous name (exposed by multiple platforms) fails closed as well. Only when
neither an explicit route nor an unambiguous account mapping applies is built-in
detection used.

## Public model specifications

Catalog-backed `/models` and `/v1/models` responses preserve their existing
members, order and fields, with an optional read-only `capabilities` object.
It projects a small specification whitelist from the same account snapshots used
by the listing; it cannot create members, routes, endpoint rights or prices.
Unknown/conflicting fields are omitted, while explicit `false` and empty arrays
remain distinguishable. Account IDs, private source URLs, declaration reasons,
raw diagnostics and binding evidence are never part of this public object.

The existing pinned upstream list, Codex `client_version` manifest, native Gemini
and dedicated Antigravity serializers keep their original shapes. A model's
public specifications do not promise availability on every protocol; the exact
endpoint still goes through the original Composite and account qualification.

## Admin Workflows

- Admins can create a group with platform `composite`.
- Admins can add, edit, delete, and preview composite model routes.
- Composite groups can copy accounts from concrete provider groups.
- Concrete provider accounts can be assigned directly to composite groups from
  account create/edit and bulk account workflows.
- Subscription payment plans can bind to a composite group when that group's
  `subscription_type` is `subscription`. The plan grants access to the
  composite group; each request is still billed and quota-checked against the
  resolved concrete provider platform.
- Channel configuration exposes composite groups in concrete provider sections.
  The channel `group_ids` payload is still flat; provider-specific model
  mapping and pricing remain keyed by concrete platform.

## Bucket 2 Setup: OpenAI + Claude + Gemini + Grok

Use one composite subscription group when one customer-facing plan should expose
model aliases across OpenAI, Claude, Gemini, and Grok without issuing separate
keys per provider.

1. Create concrete provider groups for the upstream account pools, for example
   `OpenAI Paid`, `Claude Paid`, `Gemini Paid`, and `Grok Paid`.
2. Create a `composite` group with `subscription_type` set to `subscription`.
3. Assign provider accounts directly to the composite group, or copy accounts
   from the concrete provider groups during group creation.
4. Add explicit routes for public aliases that should not rely on built-in
   model detection:

   | Public model | Endpoint | Target platform | Upstream model |
   | --- | --- | --- | --- |
   | `all/gpt-5` | `responses` | `openai` | `gpt-5` |
   | `all/claude-sonnet` | `messages` | `anthropic` | `claude-sonnet-4-6` |
   | `all/gemini-pro` | `gemini` | `gemini` | `gemini-2.5-pro` |
   | `all/grok` | `responses` | `grok` | `grok-4.3` |

5. Configure channel pricing and model mapping under the concrete platforms
   named in each route. Composite routing does not create pricing records.
6. Create a subscription payment plan for the composite group.

The same composite group can also rely on built-in detection for standard model
names such as `gpt-*`, `claude-*`, `gemini-*`, and `grok-*`. Explicit routes are
recommended for bundled plan aliases because they make endpoint, provider, and
upstream model attribution reviewable in the admin UI.

## Limits

Composite routes choose a concrete provider and upstream model; they do not
create synthetic model metadata, pricing, or upstream capability records by
themselves. Keep channel pricing/model mapping configured for the concrete
provider platforms that the routes target.

The following are intentionally **not** implemented:

- Prefix-based automatic provider routing for names that are not vendor-specific
  (see [Built-In Detection](#built-in-detection)); shared names fail closed.
- Automatic multi-provider fallback chosen by the gateway on its own: every
  fallback target must be declared by an admin through `targets`.
- Direct API-key binding to several existing groups without a composite group.
- Protocol-agnostic provider decoupling or a LiteLLM-style adapter rewrite.
