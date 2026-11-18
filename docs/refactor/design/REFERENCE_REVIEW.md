# OmniHive Reference Review & Visual Benchmark Synthesis

**Review Date:** 2026-11-19 (Asia/Shanghai)  
**Scope & Evidence:** Public pages inspected via Playwright (`1440×900` CSS viewport) with screenshots persisted to `C:\Users\Administrator\.playwright-mcp\ui-refs\` and mirrored into `docs/refactor/ui-refs/`. No private login gates or anti-bot challenges were bypassed. Marketing metrics, model counts, and vendor pricing shown in captures are treated strictly as UI layout evidence, never copied as OmniHive claims or mock data.

---

## 1. Primary North-Star Benchmarks

### 1.1 OpenRouter — Model Catalog (`openrouter.ai/models`)
- **Screenshot:** [openrouter-models.png](../ui-refs/openrouter-models.png)
- **Observed Anatomy:** Persistent 220px left facet rail (`Input modalities`, `Context length`, `Prompt pricing`, `Series`, `Categories`, `Supported parameters`, `Zero data retention`, `In-region routing`, `Tool calling`); horizontal modality tab strip with live counts (`All`, `Text 478`, `Image 71`, `Video 35`, ...); top toolbar pairing instant search, global `⌘K` trigger, sort dropdown, variant collapse (`All variants`), `Pinned` filter, and `List | Table` segmented switch; right-aligned tabular numeric columns (`Weekly usage`, `Input`, `Output`, `Context`, `Latency`, `Throughput`, `Released`) with explicit `—` for missing metrics and `from $0.05` for variable/media pricing.
- **Borrow:**
  - Above-the-fold catalog ergonomics: search, modality tabs, facets, and the first 10+ model rows visible immediately without a decorative hero banner.
  - `List | Table` view toggle plus variant grouping (`batch`, `fast`, quantized variants) to prevent catalog spam.
  - Tabular monospace alignment on every numeric column and explicit `—` when telemetry is absent.
  - Global `⌘K` search affordance in the top bar.
- **Avoid / Boundary:**
  - Desktop-only wide tables without a responsive card/summary fallback at `360px`/`768px`.
  - Implying that model-level maximum context or capability applies uniformly across every upstream endpoint.

### 1.2 Vercel AI Gateway — Model Directory (`vercel.com/ai-gateway/models`)
- **Screenshot:** [vercel-models.png](../ui-refs/vercel-models.png)
- **Observed Anatomy:** High-contrast alabaster surface (`#FFFFFF` / `#FAFAFA`), `1px` hairline row dividers, canonical `provider/model-slug` monospace identifiers (`openai/gpt-6.2-sol`), stacked price cells with expandable endpoint variants (`$2/M` + `+2 more`), overlapping provider avatar stack (`Providers`), explicit boolean governance columns (`ZDR ✓`, `No Training ✓`, `Free AI Gateway Credit ✕`), and compact capability icon strips with overflow count (`+5`).
- **Borrow:**
  - Canonical `provider/model` slug presentation with provider badge.
  - `+N more` expandable multi-endpoint price disclosure so tiered/cache/provider price differences are never flattened into a misleading single number.
  - First-class governance columns (`ZDR`, `No Training`, `Region`)—extended in OmniHive with a third explicit `Unknown / Unverified (—)` state rather than binary `✓/✕` only.
  - Compact capability icon row paired with accessible text tooltips and filterable labels.
- **Avoid / Boundary:**
  - Dedicating 360px of vertical height to a marketing headline above an operational directory when inside the console.

### 1.3 OpenCode Console — Zen & Go (`opencode.ai`)
- **Screenshots:** [opencode-zen.png](../ui-refs/opencode-zen.png) · [opencode-go.png](../ui-refs/opencode-go.png)
- **Observed Anatomy:** Deep obsidian-charcoal canvas (`#0B0C0E`–`#131519`), full-bleed `1px` architectural grid hairlines dividing sections, compact uppercase monospace top navigation (`MODELS`, `SUBSCRIPTIONS`, `TEAMS`, `ENTERPRISE`), interactive model enable/disable toggle cards with crisp contrast, monochrome provider logo strip, `INDIVIDUALS | TEAMS` segmented switcher, and restrained two-tier pricing cards (`$10 per month` / `$40 per month`) with clean mono numerals.
- **Borrow:**
  - Console structural grammar: neutral charcoal surfaces, `1px` structural borders instead of heavy drop shadows or frosted glass, and uppercase `11px` mono section/navigation labels.
  - Interactive toggle-card pattern for enabling/disabling models or pool bindings in workspace/admin configuration.
  - Plan comparison discipline: monochrome provider icon strip, clear audience toggle, and scannable allowance bullets.
- **Avoid / Boundary:**
  - Treating subscription quota (`Go`) as interchangeable with pay-as-you-go wallet credit or general resold upstream capacity (`PLAN_v1.0.md` §3.3).

---

## 2. Extended Industry & Regional Gateway Review

### 2.1 Fireworks AI — Model Library & Fireworks Nexus
- **Screenshots:** [fireworks.png](../ui-refs/fireworks.png) · [nexus.png](../ui-refs/nexus.png)
- **Observed Anatomy:**
  - *Model Library:* Warm off-white background with ultra-clean two-line list rows: Row 1 shows provider mark + model title + `New` badge; Row 2 renders a monospace metadata ribbon (`Vision · Serverless  $0.3/M Input · $1.2/M Output · 1048576 Context`).
  - *Nexus (`fireworks.ai/product/nexus`):* Dark plum/charcoal hero pairing a routing fan-in diagram (`Your tools` → gateway node) directly above a stacked daily model-mix spend bar chart (`Open models` vs `Opus`, `$0–$12k` axis).
- **Borrow:**
  - Fireworks' two-line compact list row format for OmniHive's `List` mode and mobile catalog cards.
  - Nexus's visual pairing of **routing policy topology** and **attributed daily model-mix spend** for OmniHive's Overview and Cost & Quota pages.
- **Avoid / Boundary:**
  - Serif editorial display headlines (clash with OmniHive's geometric engineering voice) and unverified `"cutting AI spend by 50% or more"` marketing claims.

### 2.2 Kilo Gateway (`kilo.ai` & `kilo.ai/gateway`)
- **Screenshots:** [kilo-gateway.png](../ui-refs/kilo-gateway.png) · [kilo.png](../ui-refs/kilo.png)
- **Observed Anatomy:** Dark dot-grid hero split 50/50 between value proposition (including BYOK without subscription) and a real tabbed code panel (`AI SDK | Python | curl`) showing exact `baseURL` and `process.env.KILO_API_KEY`, with clickable model pills underneath (`Claude Opus 4.8`, `GPT-5.5`, `Gemini 3.1 Pro`) that swap the model slug in place.
- **Borrow:**
  - Interactive SDK/cURL quickstart panel on the Homepage and Onboarding flow where clicking a real catalog model updates the `model` parameter and `baseURL` reflects the live deployment.
  - Clear explanation of BYOK vs platform-hosted balance in one sentence.
- **Avoid / Boundary:**
  - High-saturation yellow flooding top banners, cookie dialogs, and buttons simultaneously—OmniHive uses **Amber** with surgical restraint (single primary CTA, active indicator, key focus/selection).
  - Desktop agent/IDE product scope (`Kilo Desktop`, `Kilo Code`) outside Charter.

### 2.3 PipeLLM (`pipellm.ai`)
- **Screenshot:** [pipellm.png](../ui-refs/pipellm.png)
- **Observed Anatomy:** Precision dark control-plane aesthetic with `1px` framing lines, subtle corner registration ticks, warm amber-gold headline gradient, pipeline node diagram (`Relay` / `Loop` / `Lens`), and dark terminal inspection cards showing structured trace stages.
- **Borrow:**
  - Crisp `1px` technical framing and terminal/trace card styling for Request Detail and Routing Simulation evidence panels.
  - Clear conceptual separation between protocol relay, policy enforcement, and decision explainability.
- **Avoid / Boundary:**
  - Repeating marquee ticker banners and managed agent runtime (`Loop`, `managed tools attached`) which are explicitly out of scope per `CHARTER.md`.

### 2.4 AIHubMix (`aihubmix.com`)
- **Screenshot:** [aihubmix.png](../ui-refs/aihubmix.png)
- **Observed Anatomy:** Clean light-mode developer landing page with three crisp horizontal architecture cards: (1) single-key multi-provider fan-out, (2) 2-line `base_url` git-diff code snippet (`- https://api.openai.com/v1` / `+ https://.../v1`) with protocol badges (`OpenAI · Anthropic · Gemini`), and (3) multi-cloud auto-failover diagram.
- **Borrow:**
  - The 2-line `base_url` diff visualization and explicit 3-protocol switcher (`OpenAI` / `Anthropic` / `Gemini` wire compatibility) for Homepage and API Key onboarding.
- **Avoid / Boundary:**
  - `Cloud Agent` / `Ecosystem` navigation sprawl and unverifiedmodel/uptime counters unless backed by live catalog/Ops projections.

### 2.5 NVIDIA NIM (`build.nvidia.com/models`)
- **Screenshot:** [nvidia-nim.png](../ui-refs/nvidia-nim.png)
- **Observed Anatomy:** Dark charcoal catalog with structured left filter rail (`Free Endpoint`, `Partner Endpoint`, `Download Available`, `Use Case`, `Inference Providers` with per-facet counts) and 3-column dark cards showing publisher icon, distinct access/deployment badges, concise 2-line capability summary, task pill, and mono metadata footer.
- **Borrow:**
  - Explicit separation of **endpoint access tier / source** (`Platform Pool`, `Tenant BYOK`, `Trial/Free`) from **model capability tags**, plus per-option facet counts in the filter rail.
- **Avoid / Boundary:**
  - Card-only 3-column grid as the sole view (inferior to table rows for side-by-side price comparison) and neon green/purple multi-badge clutter.

### 2.6 Cloudflare Workers AI (`developers.cloudflare.com/workers-ai/models/`)
- **Screenshot:** [cloudflare-models.png](../ui-refs/cloudflare-models.png)
- **Observed Anatomy:** Light technical directory with `Last updated` provenance timestamp, inline filter bar (`Search models`, `Task Types`, `Capabilities`, `Authors`, `Newest first`), live match count (`We found 75 models`), uniform cards with uppercase mono task label (`TEXT GENERATION`), hosting provenance pill (`Cloudflare-hosted`), and a dedicated `Compare` button on every card.
- **Borrow:**
  - Explicit catalog `Last updated` timestamp, hosting/source provenance tag, and direct `Compare` toggle action on every model row/card.
- **Avoid / Boundary:**
  - Three-column documentation chrome (left docs tree + right `On this page` rail) squeezing the model catalog width.

### 2.7 ModelScope (`modelscope.cn/models`)
- **Screenshot:** [modelscope.png](../ui-refs/modelscope.png)
- **Observed Anatomy:** Hydrated Chinese model directory featuring a searchable left task taxonomy (`任务 / 框架 / 其他 / 组织` + inline `快速搜索`), capability dropdowns (`支持体验`, `支持训练`, `支持评测`, `支持部署`), monospace `org/model` titles (`moonshotai/Kimi-K3.1-Pro`), and `合集` (collection) cards grouping base/quantized variants (`Hy4-flash`, `Hy4-flash-FP8`).
- **Borrow:**
  - Search-within-facets for long provider/capability lists and model-family variant grouping so quantized/dated snapshots do not flood primary search results.
- **Avoid / Boundary:**
  - Open-weight file/framework noise (`Safetensors`, `PyTorch`, `LoRA`, community likes) irrelevant to inference API consumers, and floating promotional corner banners (`探索模型宇宙`) obscuring filters.

### 2.8 SiliconFlow (`siliconflow.cn` & `siliconflow.cn/models`)
- **Screenshots:** [siliconflow.png](../ui-refs/siliconflow.png) · [siliconflow-models.png](../ui-refs/siliconflow-models.png)
- **Observed Anatomy:** Homepage displays a full-bleed promotional campaign graphic (`限时 5 折`); `/models` presents clean Chinese facet pills (`模型类型: 对话/生图/嵌入/重排序/语音/视频`, `模型厂商` with brand icons, `应用场景: RAG/长文本处理/多模态理解`), plus quick-pick popular model chips, beneath a tall pastel hero. Authenticated console pages were skipped per rules.
- **Borrow:**
  - Crisp, natural Simplified Chinese taxonomy labels for modalities and enterprise scenarios in `zh-CN` locale.
- **Avoid / Boundary:**
  - E-commerce promotional splash banners on the homepage and oversized catalog headers that push all actual model rows below the fold.

### 2.9 Together AI (`together.ai/models`)
- **Screenshot:** [together-models.png](../ui-refs/together-models.png)
- **Observed Anatomy:** Clean uppercase monospace category pill bar (`ALL`, `CHAT`, `IMAGE`, `VISION`, `VIDEO`, `AUDIO`, `TRANSCRIBE`, `CODE`, `EMBEDDINGS`, `RERANK`, `MODERATION`) paired with `Showing 116 models` counter and provider filter, below a tall 3D glass illustration banner.
- **Borrow:**
  - Crisp mono category filter strip and explicit active result count.
- **Avoid / Boundary:**
  - Tall 3D abstract hero artwork above a functional model table.

### 2.10 Command Code (`commandcode.ai/zh`), AIGoCode (`aigocode.com`), ClinePass & GMI Cloud
- **Screenshots:** [command-code.png](../ui-refs/command-code.png) · [aigocode.png](../ui-refs/aigocode.png) · [clinepass.png](../ui-refs/clinepass.png) · [gmi.png](../ui-refs/gmi.png)
- **Observed Anatomy & Decisions:**
  - **Command Code:** Architectural `1px` wireframe grid with corner node squares on black works well for subtle hero framing, but coding-agent product scope and unverified `"10x faster / 80% fewer bugs"` claims are excluded.
  - **AIGoCode:** Explaining quota renewal cadence (`每 7 天发放`) and overage rules right next to plan pricing is helpful clarity; however, OmniHive must **never** silently fall back from subscription quota to wallet balance without explicit tenant policy consent, and avoids floating pill navbars + unverified social-proof counters.
  - **ClinePass:** Clear 3-step onboarding card rhythm, while rejecting IDE-plugin-specific positioning.
  - **GMI Cloud:** Strong typographic contrast on dark surfaces, while strictly prohibiting GMI-style background videos, heavy glow blooms, and glitch animations (`CHARTER.md`).

### 2.11 Skipped / Unreachable Sites & Benchmark Asset Note
- **302.AI (`302.ai`):** Timed out across repeated Playwright navigation attempts; skipped without proxy/challenge circumvention.
- **CherryIN (`cherryin.ai`):** Connection closed (`ERR_CONNECTION_CLOSED`); skipped.
- **鱼鱼连线:** Candidate domain (`yuyu.ai`) closed connection and authoritative public URL could not be verified without bypassing gates; skipped with zero fabricated observations.
- **`benchmark-heatmap.png` ([capture](../ui-refs/benchmark-heatmap.png)):** Inspection confirms this pre-supplied PNG is a 5-column favicon/bookmark directory of the 22 reference sites rather than a numeric heatmap table. Consequently, OmniHive's Model Compare Heatmap Matrix in `DESIGN_SYSTEM.md` is designed from first principles: tabular mono cells, explicit sample size + window + source provenance, neutral `Insufficient sample (—)` cells, and WCAG-safe amber/emerald/slate cell encoding that never relies on red/green hue alone.

---

## 3. Consolidated Binding Decisions for OmniHive Design System v1

1. **Single Accent Discipline (Honeycomb · Charcoal · Amber):** Amber (`#F59E0B` / `#D97706` light, `#F59E0B` / `#FBBF24` dark) is the **only** brand and primary action accent. Structural surfaces are neutral warm alabaster (`#F7F8FA` / `#FFFFFF`) in Light Mode (default for marketing/docs) and deep obsidian charcoal (`#0E1015` / `#14171F` / `#1A1E29`) in Dark Mode (default for console). Legacy multi-theme experiments (`control`, `slate`, `indigo`, `mono`) are retired.
2. **Architectural Borders Over Glass & Glow:** Following OpenCode Console and Vercel AI Gateway, hierarchy is established through surface luminance steps and `1px` crisp borders—never neon box-shadows, backdrop-blur soup, or animated background decoration on operational pages. Honeycomb geometry appears only in the BrandMark, EmptyState linework, and Homepage architectural diagram.
3. **Strict Typographic Role Separation:** Self-hosted `Inter` renders UI prose and CJK-paired labels; self-hosted `JetBrains Mono` with `font-variant-numeric: tabular-nums` is mandatory for all prices, token counts, latencies, percentages, model slugs, request IDs, key prefixes, and uppercase `11px` navigation eyebrows.
4. **Above-the-Fold Task First:** Operational and directory pages (Model Plaza, Requests, Connections, Billing) never waste viewport height on decorative banners. Search, active scope/filters, and primary rows appear immediately above the fold.
5. **Synthesis of OpenRouter + Vercel + Fireworks for Model Discovery:** Model Plaza combines OpenRouter's left facet rail + modality tabs + `⌘K` search, Vercel's canonical `provider/model` slugs + ZDR/retention governance columns + `+N more` price variant disclosure, and Fireworks' compact two-line summary mode for list/mobile views.
6. **Separation of Catalog, Permission, Price & Telemetry Facts:** Model maximum capability, endpoint protocol support, tenant access eligibility, confirmed price version, and observed runtime health are rendered as distinct, labeled facts—never conflated into a single badge.
7. **Numeric & Financial Honesty:** Every monetary value displays currency, unit (`per 1M tokens`, `per request`, `per image`), and settlement state (`Estimated`, `Reserved`, `Settled`, `Pending Verification`). `0`, `Free`, `Unknown (—)`, `Unconfigured`, and `Unsupported` have distinct visual and screen-reader representations; `null`/`undefined` never renders as `0`.
8. **Deploy-Specific SDK Onboarding:** Homepage and Empty/Key states use AIHubMix/Kilo-style copyable `OpenAI` / `Anthropic` / `Gemini` snippets pre-bound to the instance's live `baseURL` and real catalog model IDs, with in-place copy confirmation.
9. **Zero Unverified Claims or Agent Scope Creep:** No fabricated TPS/latency/customer totals in public pages, and no IDE/desktop agent/connector management features outside `CHARTER.md`.
10. **Complete Interactive State Coverage:** Every primitive and page specification accounts for loading skeletons, empty vs zero-filter-match, error with request ID, disabled, permission-denied, long multilingual strings, and keyboard/mobile (`360px`–`1920px`) operability.
