# OmniHive Design System v1 (`DESIGN_SYSTEM.md`)

**Status:** Binding Specification for Refactor (`codex/refactor`)  
**Brand Direction:** Honeycomb · Charcoal · Amber (Single-Accent Architectural Precision)  
**Primary Benchmarks:** OpenRouter (discovery & faceted tables), Vercel AI Gateway (canonical model metadata & governance columns), OpenCode Console & Go (charcoal grid shell, mono eyebrows, toggle & plan cards), Fireworks & Nexus (one-line model density, policy/budget hierarchy).

---

## 1. Design Philosophy & Brand Guardrails

1. **Architectural Clarity Over Decoration:** OmniHive is mission-critical AI gateway infrastructure. Visual authority comes from crisp `1px` structural borders, calibrated surface luminance steps, and disciplined tabular typography—never neon glows, multi-hue gradients, floating background blobs, or video/glitch effects.
2. **Amber Is the Single Accent:** Warm honey-amber (`#F59E0B` dark / `#D97706` light interactive, `#F59E0B` primary fill with dark `#141008` ink) is the **only** chromatic brand accent. It is reserved for primary calls to action, active navigation indicators, keyboard focus rings, selected states, and primary metric highlights. Over 90% of every viewport consists of neutral charcoal or warm alabaster surfaces.
3. **Restrained Honeycomb Motif:** Hexagonal honeycomb geometry appears strictly in three places: (a) the OmniHive `BrandMark`, (b) minimal `1px` linework inside `EmptyState` / `PermissionState` illustrations, and (c) the architectural routing diagram on the public Homepage. Data tables, charts, cards, and forms never carry honeycomb background textures.
4. **Dual-Theme Polarity, Shared Semantics:** Public marketing, pricing, status, and documentation pages default to **Light Mode** for crisp editorial legibility; the authenticated User and Admin Consoles default to **Dark Mode** for sustained operational focus. Users can toggle `Light / Dark / System` at any time, and every component consumes identical `--oh-*` semantic CSS custom properties.
5. **Strict Fact Separation:** Catalog capability, endpoint protocol support, workspace access permission, confirmed price version, and observed runtime telemetry are distinct domain facts. The UI never collapses them into an ambiguous badge or fabricates numbers when telemetry is absent.

---

## 2. Semantic Color Tokens (`--oh-*`)

All new UI primitives (`frontend/src/components/ui/**`) and migrated pages consume `--oh-*` variables defined in `frontend/src/styles/tokens.css` and mapped into `tailwind.config.js`. Legacy multi-theme overrides (`[data-theme='control' | 'slate' | 'indigo' | 'mono']`) are retired; legacy variable names (`--background`, `--card`, `--brand`, etc.) are aliased to `--oh-*` during page migration.

### 2.1 Surface, Text & Structural Border Tokens

| Semantic Token | Tailwind Utility | Light Mode (`:root`) | Dark Mode (`.dark`) | Usage & Contrast Contract |
| --- | --- | --- | --- | --- |
| `--oh-bg-canvas` | `bg-oh-canvas` | `#F7F8FA` | `#0E1015` | App / page background canvas |
| `--oh-bg-subtle` | `bg-oh-subtle` | `#F1F3F6` | `#12151C` | Left sidebar, table header strip, filter rail, code block header |
| `--oh-bg-surface` | `bg-oh-surface` | `#FFFFFF` | `#161922` | Primary cards, data table body, form panels |
| `--oh-bg-elevated` | `bg-oh-elevated` | `#FFFFFF` | `#1C202B` | Modals, drawers, popovers, command palette, dropdowns |
| `--oh-bg-inset` | `bg-oh-inset` | `#ECEEF2` | `#0A0C10` | Segmented control track, input well, raw JSON/code viewport |
| `--oh-bg-hover` | `bg-oh-hover` | `#F3F4F7` | `#1F2430` | Interactive row / list item / ghost button hover |
| `--oh-bg-selected` | `bg-oh-selected` | `#FEF7E6` | `#261F12` | Selected table row, active nav item tint, chosen wizard card |
| `--oh-fg-primary` | `text-oh-primary` | `#141519` | `#F3F4F6` | Primary body copy, headings, key numbers (`>= 14.8:1` contrast) |
| `--oh-fg-secondary` | `text-oh-secondary` | `#4B505C` | `#A6AEBC` | Descriptions, table secondary cells, form labels (`>= 7.2:1`) |
| `--oh-fg-muted` | `text-oh-muted` | `#666C79` | `#828BA0` | Captions, placeholders, mono eyebrows, unit suffixes (`>= 4.7:1`) |
| `--oh-fg-disabled` | `text-oh-disabled` | `#9CA3AF` | `#4E5564` | Disabled controls only (always paired with `aria-disabled` / `disabled`) |
| `--oh-border-subtle` | `border-oh-subtle` | `#EAECEF` | `#1E222D` | Internal table row dividers, section hairlines |
| `--oh-border-default` | `border-oh-default` | `#DDE0E6` | `#262B38` | Card borders, input borders, shell structural grid lines |
| `--oh-border-strong` | `border-oh-strong` | `#C4C9D4` | `#363D4E` | Input hover border, active card outline, resizer handles |

### 2.2 Brand Accent Tokens (Single Accent: Amber)

> **Contrast Rule:** Never render white text (`#FFFFFF`) on `#F59E0B` amber fills (contrast is `2.15:1`, failing WCAG AA). Primary amber fills always use `--oh-accent-ink` (`#141008`, contrast `8.8:1`). Inline amber text on neutral surfaces uses `--oh-accent-text` (`#B45309` in Light `4.7:1`, `#FBBF24` in Dark `9.7:1`).

| Semantic Token | Tailwind Utility | Light Mode | Dark Mode | Usage |
| --- | --- | --- | --- | --- |
| `--oh-accent` | `bg-oh-accent` | `#F59E0B` | `#F59E0B` | Primary CTA fill, active tab bar (`2px`), active step node |
| `--oh-accent-hover` | `bg-oh-accent-hover` | `#D97706` | `#FBBF24` | Primary CTA hover fill |
| `--oh-accent-active` | `bg-oh-accent-active` | `#C56207` | `#D97706` | Primary CTA pressed fill (`>= 4.6:1` against `--oh-accent-ink`) |
| `--oh-accent-ink` | `text-oh-accent-ink` | `#141008` | `#141008` | Text & icons inside solid `--oh-accent` buttons/badges |
| `--oh-accent-text` | `text-oh-accent-text` | `#B45309` | `#FBBF24` | Links, highlighted mono metrics, active filter counts |
| `--oh-accent-subtle` | `bg-oh-accent-subtle` | `#FFF8EB` | `#271F10` | Accent badge fill, selected chip background, callout surface |
| `--oh-accent-border` | `border-oh-accent` | `#FCD34D` | `#6B4C12` | Selected card border, subtle accent badge border |
| `--oh-focus-ring` | `ring-oh-focus` | `#D97706` | `#FBBF24` | `2px` keyboard focus ring (`focus-visible` only, `2px` offset) |

### 2.3 Semantic Status & Telemetry Tokens

Every status badge or banner combines **icon + label + token** so color-blind users and monochrome printouts preserve full meaning.

| State | Foreground (`--oh-*-fg`) Light / Dark | Surface (`--oh-*-bg`) Light / Dark | Border (`--oh-*-border`) Light / Dark | Canonical Gateway Meaning |
| --- | --- | --- | --- | --- |
| **Success (`ok`)** | `#15803D` / `#4ADE80` | `#F0FDF4` / `#10261A` | `#BBF7D0` / `#1E462F` | Verified, Healthy, Settled, `2xx`, ZDR Verified |
| **Warning (`warn`)** | `#B45309` / `#FBBF24` | `#FFFBEB` / `#281F0F` | `#FDE68A` / `#5C4313` | Cooldown, Rate-Limited, Degraded, Expiring Soon, Estimated/Reserved |
| **Danger (`danger`)** | `#B91C1C` / `#F87171` | `#FEF2F2` / `#2A1315` | `#FECACA` / `#5C2125` | Auth Invalid, Circuit Open, Quota Exhausted, `4xx/5xx` Failure |
| **Info (`info`)** | `#1D4ED8` / `#60A5FA` | `#EFF6FF` / `#122038` | `#BFDBFE` / `#233E6B` | Running Async Job, Draining In-Flight, Policy Simulation |
| **Neutral (`neutral`)** | `#4B505C` / `#A6AEBC` | `#F1F3F6` / `#1D212C` | `#DDE0E6` / `#2E3444` | Disabled by Admin, Archived, Draft, Standard Protocol |
| **Unknown (`unknown`)** | `#666C79` / `#8A93A5` | `#F7F8FA` / `#151821` | `dashed #C4C9D4` / `dashed #363D4E` | Unverified, Insufficient Sample, Telemetry Stale |

### 2.4 Chart & Heatmap Palette Tokens

| Token | Light | Dark | Fixed Metric Assignment |
| --- | --- | --- | --- |
| `--oh-chart-1` | `#D97706` | `#F59E0B` | Total Requests / Throughput / Primary Selected Series |
| `--oh-chart-2` | `#16A34A` | `#34D399` | Successful Requests / Availability % |
| `--oh-chart-3` | `#2563EB` | `#60A5FA` | Latency (`P50` / `P95` / `TTFT`) / Cached Input Tokens |
| `--oh-chart-4` | `#7C3AED` | `#A78BFA` | Pool Capacity / Concurrent Streams / Secondary Model |
| `--oh-chart-5` | `#DC2626` | `#F87171` | Upstream Errors / Budget Rejections / `429` + `5xx` |
| `--oh-heat-0`..`4` | `#F1F3F6` → `#FEF3C7` → `#FDE68A` → `#F59E0B` → `#B45309` | `#161922` → `#241E12` → `#3B2E12` → `#6B4C12` → `#9A6B10` | Compare Heatmap Matrix quartile cells (always paired with printed numeric value + rank indicator; `--oh-heat-unknown` uses diagonal hatch / dashed border) |

---

## 3. Typography & Numeric Discipline

### 3.1 Self-Hosted Font Stack (Zero External CDN)

- **UI Sans (`--oh-font-sans`):** `'Inter Variable', 'Inter', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei UI', 'Noto Sans CJK SC', system-ui, -apple-system, sans-serif`
- **Technical Mono (`--oh-font-mono`):** `'JetBrains Mono Variable', 'JetBrains Mono', ui-monospace, 'SFMono-Regular', Menlo, Consolas, monospace`
- **Delivery:** Bundled locally as WOFF2 subsets in `frontend/public/fonts/` (`inter-latin-wght-normal.woff2`, `jetbrains-mono-latin-wght-normal.woff2`) with `font-display: swap`. Zero requests to `fonts.googleapis.com` or external CDNs, ensuring air-gapped / enterprise / mainland China deployments render identically.

### 3.2 Type Scale

| Token / Class | Font | Size / Line-Height | Weight / Tracking | Usage |
| --- | --- | --- | --- | --- |
| `oh-text-display` | Sans | `36px / 42px` (`2.25rem`) | `600` / `-0.025em` | Public Homepage & Pricing hero headlines only |
| `oh-text-title-lg` | Sans | `24px / 32px` (`1.5rem`) | `600` / `-0.02em` | Console `PageHeader` primary title |
| `oh-text-title-md` | Sans | `18px / 26px` (`1.125rem`) | `600` / `-0.015em` | Modal/Drawer title, major section heading |
| `oh-text-title-sm` | Sans | `15px / 22px` (`0.9375rem`) | `600` / `-0.01em` | Card header, wizard step title |
| `oh-text-body` | Sans | `14px / 20px` (`0.875rem`) | `400` (`500` emphasis) | Default UI text, form inputs, prose |
| `oh-text-body-sm` | Sans | `13px / 18px` (`0.8125rem`) | `400` (`500` controls) | Compact table cells, sidebar links, filter items |
| `oh-text-caption` | Sans | `12px / 16px` (`0.75rem`) | `400` | Helper descriptions, timestamps, secondary metadata (minimum prose size) |
| `oh-text-eyebrow` | Mono | `11px / 16px` (`0.6875rem`) | `600` / `+0.06em` uppercase | OpenCode-style sidebar section headers, table column headers, top nav labels |
| `oh-text-code` | Mono | `12.5px / 18px` | `400`–`500` | Model IDs (`openai/gpt-6.2-sol`), `req_...` IDs, key prefixes, SDK snippets |
| `oh-stat-lg` | Mono | `28px / 34px` (`tabular-nums`) | `600` / `-0.02em` | KPI `StatNumber` headline figures |
| `oh-stat-sm` | Mono | `13px / 18px` (`tabular-nums`) | `500` | Right-aligned table prices, latencies, token counts, percentages |

### 3.3 Mandatory Numeric & Financial Formatting Rules

Every numeric display (`StatNumber`, table cells, `PriceBreakdown`) enforces `font-family: var(--oh-font-mono)` and `font-variant-numeric: tabular-nums`:

| Value State | Rendered Output | Accessibility / Visual Rule |
| --- | --- | --- |
| **Confirmed Price** | `$2.00` + muted `/ 1M` | Currency symbol + exact decimal string from API; never JS float math (`0.1 + 0.2`) |
| **Tiered / Variant Price** | `$2.00/M` + dotted underline + `+2 more` | Hover/click popover expands cached read/write, batch, context-tier, or media rates |
| **Explicit Free (`0`)** | `Free` (`$0.00/M` sub-label) | Distinct from `0` usage and distinct from unconfigured price |
| **Zero Usage / Count** | `0` | Crisp `0` in `--oh-fg-primary` or `--oh-fg-secondary` |
| **Unknown / Null / No Sample** | `—` | Muted em-dash with `aria-label` / tooltip (`No sample in 24h window`); **never** render `0` or `$0.00` |
| **Unconfigured** | `Unconfigured` | Neutral pill or muted italic label with action link where editable |
| **Unsupported** | `×` + `sr-only` (`Unsupported`) | Muted `×` in capability matrix; distinct from `—` (`Unverified`) |
| **Huge Numbers** | `94.2B` (compact) or `1,050,000` (exact) | Tooltip always reveals full exact integer (`94,218,402,110 tokens`) |
| **Settlement Status** | `Estimated` · `Reserved` · `Settled` · `Pending Check` | Displayed adjacent to monetary totals whenever non-final |

---

## 4. Spacing, Layout Shells & Density

### 4.1 Spacing Scale & Structural Dimensions

- **4px Base Grid:** `2px` (hairline offset), `4px` (`gap-1`), `8px` (`gap-2`), `12px` (`gap-3`), `16px` (`gap-4`), `20px` (`gap-5`), `24px` (`gap-6`), `32px` (`gap-8`), `48px` (`gap-12`), `64px` (`gap-16`).
- **Public Site Shell:** `56px` sticky top bar (`1px` bottom border, subtle backdrop surface), centered `1200px` max-width container framed by OpenCode-style `1px` vertical structural rails on `>= 1280px` viewports. Prose reading width capped at `68ch`.
- **Console AppShell:**
  - **Top Bar (`56px` fixed):** BrandMark + `ScopeSwitcher` (`Personal` / `Workspace` / `Admin` mode badge) on left; global `⌘K` search trigger (`280px`–`360px`) in center; Active Async Tasks indicator, Docs link, Theme/Density/Locale menu, and User menu on right.
  - **Left Sidebar (`240px` expanded, `64px` icon-rail collapsed, drawer on `< 1024px`):** Grouped under uppercase `11px` mono eyebrows (`OVERVIEW`, `MODELS`, `ACCESS`, `REQUESTS`, `BILLING`). Active route uses `--oh-bg-selected` + `2px` left `--oh-accent` bar + `font-medium`.
  - **Main Content Area:** Fluid width (`max-w-[1600px]` for KPI/form pages; full width with contained horizontal scroll inside `Table` for wide operational views). Padding: `24px` (`>= 1024px`), `16px` (`768px–1023px`), `12px` (`< 768px`).

### 4.2 Density Modes (`Comfort` vs `Compact`)

Stored under `omnihive.ui.density.v1` (`data-density="comfort" | "compact"` on `<html>`):

| Element | Comfort (Default) | Compact (`[data-density="compact"]`) | Mobile (`< 768px`) Override |
| --- | --- | --- | --- |
| Table Row Height | `48px` (`py-3 px-4`) | `38px` (`py-1.5 px-3`) | Summary card stack or `44px` min tap target |
| Control Height (`md`) | `36px` | `32px` | `40px` (`16px` input text on iOS to prevent zoom) |
| Card Padding | `20px` (`p-5`) | `14px` (`p-3.5`) | `16px` (`p-4`) |
| Section Gap | `24px` | `16px` | `16px` |

---

## 5. Radii, Elevation & Motion

- **Border Radii:**
  - `--oh-radius-xs: 4px` — KBD keycaps, inline code, heatmap cells, micro status tags.
  - `--oh-radius-sm: 6px` — Small buttons, segmented items, checkboxes, capability chips.
  - `--oh-radius-md: 8px` — Standard buttons, inputs, selects, dropdown items, toast items.
  - `--oh-radius-lg: 12px` — Cards, data table containers, callout panels, drawers.
  - `--oh-radius-xl: 16px` — Modals, Command Palette (`⌘K`), Homepage code showcase frame.
  - `--oh-radius-pill: 9999px` — Filter pills, switch tracks, avatar rings.
- **Shadows (Restrained, Zero Glow):**
  - `--oh-shadow-none: none` — Default for console cards and tables (`1px` border carries structure).
  - `--oh-shadow-xs: 0 1px 2px rgba(10, 12, 16, 0.05)` — Raised active segmented button, light-mode primary cards.
  - `--oh-shadow-md: 0 8px 24px -4px rgba(10, 12, 16, 0.14), 0 2px 6px rgba(10, 12, 16, 0.06)` (dark: `0 12px 32px rgba(0, 0, 0, 0.55)`) — Popovers, select menus, tooltips, toasts.
  - `--oh-shadow-xl: 0 24px 48px -12px rgba(10, 12, 16, 0.28)` (dark: `0 24px 64px rgba(0, 0, 0, 0.72)`) — Modals, Command Palette, Drawers.
- **Motion:**
  - `--oh-ease-out: cubic-bezier(0.16, 1, 0.3, 1)`
  - Micro-interactions (hover, active, copy icon swap, switch thumb): `120ms var(--oh-ease-out)`.
  - Overlays (modal scale `0.98 → 1`, drawer translate, accordion): `180ms var(--oh-ease-out)`.
  - Skeleton pulse: gentle opacity `0.55 ↔ 1.0` over `1.6s` ease-in-out.
  - `@media (prefers-reduced-motion: reduce)`: disables transforms and shimmer animations; transitions collapse to `<= 1ms` opacity swap.

---

## 6. Core UI Component Library (`frontend/src/components/ui/`)

All primitives are built in Vue 3 `<script setup lang="ts">`, support full keyboard/ARIA semantics, honor Light/Dark + Comfort/Compact tokens, and contain zero hardcoded business logic:

| Primitive | Variants / Key Props | Accessibility & Behavioral Contract |
| --- | --- | --- |
| `Button` | `variant`: `primary` (amber), `secondary` (surface+border), `ghost`, `danger`, `outline-accent`; `size`: `xs`, `sm`, `md`, `lg`; `loading`, `disabled` | Prevents double-click when `loading=true` (`aria-busy="true"`), preserves button width during spinner swap, `2px` `--oh-focus-ring` |
| `IconButton` | Same variants; `size`: `xs (24)`, `sm (28)`, `md (36)`; **required** `ariaLabel` | Enforces accessible name via TypeScript prop type; optional tooltip integration |
| `Input` | `type` (`text`, `search`, `password`, `number`, `url`), `mono`, `prefix`/`suffix` slots, `error`, `hint`, `clearable` | Links label, hint, and error via `aria-describedby` and `aria-invalid`; supports secret reveal toggle when `revealable` |
| `Select` | Native-accessible or listbox select with `options`, `placeholder`, `error`, `disabled`, `size` | Full keyboard arrow/typeahead navigation, `aria-invalid` + error message binding |
| `Checkbox` | `modelValue`, `indeterminate`, `label`, `description`, `disabled` | Supports tri-state (`indeterminate`) for table header select-all |
| `Switch` | `modelValue`, `size` (`sm`, `md`), `label`, `description`, `disabled`, `loading` | `role="switch"`, `aria-checked`, Space/Enter toggle, OpenCode-style crisp track with amber active fill |
| `Badge` | `tone`: `neutral`, `accent`, `success`, `warning`, `danger`, `info`, `unknown`; `variant`: `subtle`, `outline`, `solid`; `mono`, `dot` | Never communicates status by dot alone; `unknown` uses dashed border |
| `Card` | `variant`: `default`, `subtle`, `interactive`, `selected`; `padding`: `none`, `sm`, `md`, `lg`; `header`/`footer` slots | `1px` crisp border; `interactive` adds hover border + keyboard focus support |
| `Table` | Typed `columns` (`key`, `label`, `align`, `mono`, `sortable`, `tooltip`, `width`), `rows`, `sortBy`, `sortOrder`, `loading`, `emptyTitle`, `error` | Renders `aria-sort` (`ascending`/`descending`/`none`), right-aligns `mono` columns, skeleton rows on initial load, subtle progress bar on background refresh, distinct Empty vs Zero-Filter vs Error states |
| `Tabs` | `items` (`value`, `label`, `count`, `badge`, `disabled`), `variant`: `underline` (OpenRouter `2px` amber bar) or `pill` | `role="tablist"`, roving `tabindex`, `ArrowLeft`/`ArrowRight`/`Home`/`End` keyboard switching |
| `SegmentedControl` | `options` (`value`, `label`, `icon`, `count`, `disabled`), `size`: `sm`, `md` | Crisp inset track (`--oh-bg-inset`) with elevated active segment (`List \| Table`, `Individuals \| Teams`) |
| `Modal` & `Drawer` | `open`, `title`, `description`, `size` (`sm`..`xl`), `closeOnBackdrop`, `DirtyGuard` support | `role="dialog"`, `aria-modal="true"`, focus trap (`Tab` / `Shift+Tab`), `Escape` close, restores focus to trigger element on close, locks body scroll |
| `Toast` | Imperative `useToast()` + `<ToastViewport />`; tones: `info`, `success`, `warning`, `danger` | `aria-live="polite"` (`assertive` for `danger`), pause timer on hover/focus, optional `requestId` copy chip, max 4 visible |
| `Tooltip` | `content`, `placement` (`top`, `bottom`, `left`, `right`), `delay` | Opens on both pointer hover and keyboard `focus-visible`; `Escape` dismisses immediately; `role="tooltip"` |
| `CopyButton` | `value` (string or async getter), `label`, `copiedLabel`, `variant`, `size` | In-place swap (`Copy` → check + `Copied` for `1800ms`, or warning + `Copy failed` on clipboard denial) with `aria-live="polite"`; zero toast spam |
| `Skeleton` | `variant`: `line`, `rect`, `circle`, `table-row`, `stat` | Matches real layout geometry to prevent CLS; respects `prefers-reduced-motion` |
| `EmptyState` | `title`, `description`, `variant` (`empty` vs `filtered`), `icon` (minimal honeycomb SVG default), `actions` slot | Distinguishes first-run empty dataset from zero filter matches (includes `Reset filters` action) |
| `ErrorState` | `title`, `message`, `code`, `requestId`, `retryable`, `variant` (`error` vs `permission` vs `stale`) | Displays copyable `request_id` in mono, retry CTA, or required role explanation for `403` |
| `PageHeader` | `eyebrow`, `title`, `description`, `breadcrumbs`, `badges` slot, `actions` slot | Standardized console/public page header (`56px+`), responsive action wrapping |
| `StatNumber` | `value`, `unit`, `prefix`, `currency`, `status` (`confirmed` \| `estimated` \| `unknown`), `delta`, `loading`, `tooltip` | Enforces `JetBrains Mono` + `tabular-nums`; renders `null`/`undefined` as `—`, never `0` |
| `CommandPalette` | `open`, `groups` (`id`, `label`, `items` with `shortcut`, `keywords`, `badge`), `placeholder`, `loading` | Global `⌘K` / `Ctrl+K` listener, instant query filter + async search hook, `ArrowUp`/`ArrowDown`/`Enter`/`Escape`, footer keycap legend |
| `Stepper` | `steps` (`id`, `title`, `description`, `status`: `complete` \| `current` \| `upcoming` \| `error` \| `warning`), `orientation` | Used by the 5-Step Upstream Connection Wizard and Recovery flow; supports clickable completed/draft steps and `aria-current="step"` |

---

## 7. Charts, Forms, Status Language & Global State Rules

### 7.1 Charts & Telemetry Visualization

- **Library:** Wrap `Chart.js` (`vue-chartjs`) in a theme-aware token adapter reading `--oh-chart-*`, `--oh-border-subtle`, and `--oh-font-mono` on mount and theme toggle.
- **Mandatory Context Header:** Every chart card displays: (1) metric title + unit, (2) active time window (`1h`, `24h`, `7d`, `30d`), (3) timezone badge (`UTC+08:00` or `UTC`), and (4) freshness / sample count (`Updated 12s ago · n = 14,820`).
- **Interaction:** Dashed `1px` vertical crosshair on hover/focus; mono tabular tooltip; clicking a time bucket or model bar drills down to Requests/Ledger pre-filtered to that slice. Partial probe failure renders an inline warning badge without blanking healthy series.

### 7.2 Forms & Secret Handling

- **Grouping:** Related fields sit inside `16px` (`space-y-4`) groups separated by `1px` `--oh-border-subtle` dividers with left/top section titles + descriptions.
- **Secret Lifecycle (`keep | replace | clear`):** Existing upstream API keys, OAuth tokens, and webhook signing secrets are never echoed back from the server. Edit forms display a `Configured (••••9f2a)` badge with explicit `Keep current` (default), `Replace secret`, and `Clear` actions.
- **Validation & Submission:** Client validation runs on blur and submit; server validation errors map to field `error` props and render a top summary banner with `request_id` if non-field-specific. Submit buttons lock during flight (`loading=true`) and require server confirmation before showing `Confirmed` state for billing, routing, credentials, or permissions.

### 7.3 Ten Mandatory Page & Async States (`PLAN_v1.0.md` §10.6)

1. **Initial Load:** Geometry-accurate `Skeleton` matching table columns or KPI cards; async tasks >2s show explicit stage copy (`Testing TLS & token scopes…`).
2. **Background Refresh:** Keep current rows, selection, and scroll offset intact; show a `2px` top progress line or `Refreshing…` indicator in the toolbar.
3. **True Empty State:** Explain what the resource does and offer the primary creation CTA (e.g., `Create Restricted API Key` or `Connect First Provider`).
4. **Zero Filter Match:** Show active filter chips + `Clear all filters` button; never show onboarding empty copy when data exists outside the filter.
5. **Partial Failure:** Render healthy panels normally; isolate failed panel with localized `ErrorState` + `Retry section`.
6. **Permission Denied (`403`):** Explain required scope/role (`Workspace Admin` or `Billing Viewer`) and purge any prior workspace cache before rendering.
7. **Stale / Offline Telemetry:** Show `Last synced HH:MM:SS` warning banner and disable destructive mutations that depend on live balance or active policy version.
8. **Version Conflict (`409`):** Present `DiffPreview` comparing server revision vs local draft (`Reload latest` or `Re-apply changes`).
9. **Confirmed Save / Publish:** Show resulting version ID, affected scope, and rollback/next-step link—not an ephemeral toast alone.
10. **Unknown / Timeout Outcome:** For non-idempotent or billable operations, show `Outcome Pending Verification` with a ledger/order check link rather than auto-retrying.

### 7.4 Responsive & Internationalization Rules

- **Verification Viewports:** `360px` (mobile), `768px` (tablet), `1280px` (standard laptop), `1920px` (widescreen ops).
- **Mobile Adaptation (`< 768px`):** Sidebar collapses into an off-canvas `Drawer`; wide tables expose a `Summary Card` view or pin the primary identifier + action column while horizontal scrolling stays strictly inside the table container; multi-pane tools (Playground, Wizard, Compare) switch to step or `SegmentedControl` tabs.
- **Bilingual (`zh-CN` / `en-US`) Resilience:** Zero hardcoded strings in shipping views; button/tab labels never use fixed pixel widths; CJK text never uses `italic` styling; timestamps and currencies always render explicit timezone and ISO currency symbols (`USD $`, `CNY ¥`).

---

## 8. Unified Information Architecture (Public · User Console · Admin Console)

### 8.1 Public Site Navigation (Default: Light Mode, Toggle Available)

Top Bar (`56px`): `BrandMark (OmniHive)` · `MODELS` (`/models`) · `PRICING` (`/pricing`) · `DOCS` (`/docs`) · `STATUS` (`/status`) · `CHANGELOG` (`/changelog`) — Right: `⌘K Search` · `Theme` · `Locale` · `Sign In` (`/login`) · `Open Console` (`/dashboard`, Amber CTA).

### 8.2 Authenticated Console Navigation (Default: Dark Mode)

`ScopeSwitcher` in the top bar switches cleanly between **User / Workspace Console** and **Platform Admin Console** (when `user.role === 'admin'`), eliminating the current mixed sidebar where personal and global admin pages collide.

| Top-Level Section (Mono Eyebrow) | User / Workspace Console | Platform Admin Console | Legacy Terms Retired (with Redirects) |
| --- | --- | --- | --- |
| `OVERVIEW` | **Overview** (`/dashboard`): Spend, requests, success rate, budget burn, quickstart | **Platform Overview** (`/admin/dashboard`): Global RPS, success %, capacity risk, expiring credentials, settlement backlog | — |
| `MODELS` | **Model Plaza** (`/models`), **Compare** (`/models/compare`), **Playground** (`/playground`), **API Docs** (`/docs`) | **Model Catalog** (`/admin/models`), **Capability & Price Mapping** (`/admin/channels/pricing`) | "Channel Pricing" → Model Mapping & Pricing |
| `ACCESS & RESOURCES` | **API Keys** (`/keys`), **Workspaces & BYOK** (`/workspaces`), **MCP Credentials** (`/mcp`) | **Upstream Connections** (`/admin/accounts`, `/admin/connections/new`), **Resource Pools** (`/admin/groups`), **Egress Proxies** (`/admin/proxies`, `/admin/proxy-groups`) | "Accounts" → Upstream Connections; "Groups" → Resource Pools |
| `POLICIES` | **Effective Policies** (`/policies` — read-only view of workspace model, region, ZDR & budget rules) | **Routing Policies & Simulator** (`/admin/routing`), **Risk & Rate Limits** (`/admin/risk-control`), **Content Audit** (`/admin/prompt-audit`) | Embedded `GroupsView` routing modal → Routing Policies |
| `REQUESTS & TASKS` | **Request Logs & Detail** (`/usage`, `/requests/:id`), **Task Center** (`/tasks`), **Service Health** (`/monitor`) | **Global Requests** (`/admin/usage`, `/admin/requests/:id`), **Live Operations** (`/admin/ops`), **Capacity Monitor** (`/admin/channels/monitor`) | Scattered batch/ops logs → Unified Request & Task views |
| `BILLING` | **Cost & Quota** (`/billing`), **Top Up** (`/payment`), **Plans & Subscriptions** (`/subscriptions`), **Orders** (`/orders`), **Redeem** (`/redeem`), **Referrals** (`/affiliates`) | **Billing & Settlement** (`/admin/orders/dashboard`), **Orders** (`/admin/orders`), **Plans** (`/admin/orders/plans`), **Entitlements** (`/admin/subscriptions`), **Credits & Promos** (`/admin/redeem`, `/admin/promo-codes`), **Referrals** (`/admin/affiliates`) | — |
| `SYSTEM` | **Account & Security** (`/profile` — Passkeys, TOTP, Sessions, Notifications) | **Users & Access** (`/admin/users`), **Audit Logs** (`/admin/audit-logs`), **Announcements** (`/admin/announcements`), **Extensions** (`/admin/plugins`), **Backup & Recovery** (`/admin/backup`), **System Settings** (`/admin/settings/:tab`) | Monolithic `SettingsView` → Tabbed domain settings |

---

## 9. Core Page Specifications (v1)

### 9.1 Homepage (`/` & `/home`)
- **Layout (`1200px` architectural grid, Light default):**
  - **Hero (2-Column Asymmetric above the fold):** Left column (`55%`) carries mono eyebrow `UNIFIED AI INFERENCE GATEWAY`, headline (`36px` tight tracking) stating multi-provider OpenAI / Anthropic / Gemini wire-compatible access with budget & routing governance, concise 2-line value summary, primary CTA `Explore Models` (Amber) + secondary CTA `Create API Key`. Right column (`45%`) embeds an interactive **SDK Tabbed Switcher** (`OpenAI SDK` · `Anthropic SDK` · `Gemini SDK` · `cURL`) bound to the deployment's live `baseURL` and real catalog slugs, with one-click `CopyButton` and highlight of the 2 changed lines (`baseURL` + `apiKey`).
  - **Section 2 — Live Catalog Preview:** Compact 6-row Vercel/OpenRouter table pulling real public models (`Model`, `Input $/1M`, `Output $/1M`, `Context`, `Capabilities`) + `View all models →` link. Zero hardcoded/fake TPS or customer claims.
  - **Section 3 — Architectural Routing Diagram:** Crisp static SVG honeycomb routing schematic (`App / SDK → Policy & Budget Gate → Multi-Provider Pool & Failover → Immutable Ledger`) with reduced-motion-safe subtle flow indicator.
  - **Section 4 — Three Governance Pillars (OpenCode 3-column hairline grid):** Protocol Wire-Compatibility, Verifiable Cost & Quota Attribution, Credential & Egress Isolation.
- **Mobile (`360px`):** Stacks Hero copy above scrollable SDK block; Catalog Preview switches to Fireworks-style 2-line model summary cards.

### 9.2 Model Plaza (`/models`, alias `/model-plaza`)
- **Synthesis:** OpenRouter left facet rail + Vercel governance/variant columns + Fireworks compact list mode.
- **Above-the-Fold Header:** Compact single row (`PageHeader` without tall marketing banner): Title `Models` + live count badge in mono, search input (`Filter by model, slug, or provider…` + `/` shortcut), quick toggle pills (`Pinned`, `My Accessible Only` when signed in, `ZDR`), sort `Select` (`Featured`, `Newest`, `Input Price ↑`, `Context ↓`, `Throughput ↓`), `Compare (0/3)` button, and `SegmentedControl` (`Table | List`).
- **Modality Tabs (`Tabs` underline):** `All` · `Text` · `Image` · `Audio` · `Video` · `Embeddings` · `Rerank` with mono counts.
- **Left Facet Rail (`240px` desktop, `Filter` Drawer on `< 1024px`):** Collapsible sections for `Input Modalities`, `Wire Protocol` (`OpenAI Chat/Responses`, `Anthropic Messages`, `Gemini Native`), `Supported Actions` (`Tool Calling`, `Structured Output`, `Reasoning`, `Prompt Caching`), `Context Length` slider/buckets, `Prompt Pricing`, `Provider`, `Data Policy` (`ZDR`, `No Training`), and `Access` (`Available to Current Key/Workspace`).
- **Table View Columns:**
  1. `Model` — Provider monochrome icon + display name + copyable mono canonical slug (`openai/gpt-6.2-sol`) + pin star + compare checkbox.
  2. `Access` (authenticated) — `Eligible`, `Requires Plan/Pool`, or `BYOK Available`.
  3. `Input` & `Output` — Right-aligned mono `$X.XX/M` with dotted underline + `+N more` popover for cache/tiered/media rates (`Unconfirmed` warning icon if price unverified).
  4. `Context` — Right-aligned mono (`1,050,000` or `200K`).
  5. `Latency` & `Throughput` — Observed window median (`2.4s`, `52 t/s`) with dotted header explaining window (`24h median`), or `—` when sample `< threshold`.
  6. `Governance` — `ZDR` (`✓` / `×` / `—`) & `Capabilities` icon strip (`Tools`, `Vision`, `Reasoning`, `Cache`).
- **List View & Mobile (`360px`):** Fireworks-style two-line dense rows: Line 1 = Icon + Name + Copyable Slug + Access Badge; Line 2 = `Text · Tools · 200K ctx · $2.00/M in · $10.00/M out`.
- **Sticky Compare Tray:** Appears at bottom when 1–3 models are checked, launching `/models/compare?models=a,b,c`.

### 9.3 Model Detail & Compare (`/models/:slug` & `/models/compare`)
- **Model Detail (`/models/:slug`):**
  - **Header:** Canonical slug + `CopyButton`, provider badge, availability status, `Open in Playground` (Amber CTA), `Add to Compare`, `API Snippet` modal/drawer.
  - **4 Structured Sections:** (1) **Pricing & Variants** (full breakdown of input, output, cached read/write, batch, image/audio units, price version & effective timestamp), (2) **Endpoints & Protocol Compatibility** (per-pool/provider capability matrix distinguishing native support vs gateway-bridged translation vs unsupported, plus ZDR/retention/region facts), (3) **Observed Runtime Telemetry** (`24h`/`7d` latency, TTFT, throughput, and error breakdown with sample size `n`), (4) ** Deployment-Bound Quickstart** (`OpenAI` / `Anthropic` / `Gemini` / `cURL` snippets).
- **Model Compare & Heatmap Matrix (`/models/compare`, up to 3–4 models):**
  - **Block A — Specification & Governance Diff:** Context, max output, native protocols, tool/structured/vision/cache support, ZDR, training policy (`Highlight differences` switch).
  - **Block B — Interactive Scenario Cost Estimator:** Inputs for `Requests/day`, `Input tokens/req`, `Output tokens/req`, and `Cache hit %` → renders side-by-side daily/monthly `StatNumber` estimates with explicit `Scenario Estimate — Not a Settlement Invoice` label.
  - **Block C — Telemetry & Benchmark Heatmap Matrix:** Rows represent verified gateway metrics (`TTFT P50`, `TTFT P95`, `Output Throughput t/s`, `24h Availability %`, `Effective Cost per 1K Calls`) and optional imported evaluation suites. Each cell uses `--oh-heat-0..4` background tint + high-contrast mono value + rank tag (`#1` badge on best cell in row) + inline `n=<count>` sub-label. Cells below minimum sample render `--oh-heat-unknown` (`— · Insufficient sample`)—never misleading green/red coloring. Header explicitly displays `Window: 24h · Source: Gateway Observed Telemetry · Updated HH:MM UTC`.

### 9.4 Playground (`/playground`)
- **Desktop 3-Pane Workbench (`calc(100vh - 56px)`):**
  - **Left Pane (`280px` collapsible):** Workspace/Key selector (only eligible keys), Model picker (filtered by key access), Protocol selector (`OpenAI Chat`, `OpenAI Responses`, `Anthropic Messages`, `Gemini`), System instructions, Parameters (`Temperature`, `Max Tokens`, `Reasoning Effort`, `Tools JSON`, `Structured Schema`).
  - **Center Pane (Fluid):** Conversation & structured message editor (`system` / `user` / `assistant` / `tool`), collapsible `Thinking / Reasoning` block, structured `Tool Call` arguments & mock/manual tool response injector, sticky bottom composer with `Run (⌘↵)` (Amber) and `Stop Stream (Esc)` (Danger outline while streaming).
  - **Right Pane (`340px` collapsible Inspector):** Tabs for `Telemetry & Cost` (`request_id` with `CopyButton`, routed pool/upstream summary if permitted, TTFT, total latency, prompt/cached/completion tokens, `Estimated` vs `Settled` cost), `Raw Request` (exact JSON/cURL sent), and `SSE Events` (timestamped stream frames).
- **Safety & Isolation Rules:** Switching model/session or starting a new run aborts any active `AbortController` so late SSE chunks never leak into the new session. Non-text async modalities show a clear banner linking directly to Task Center workflows rather than faking synchronous chat.
- **Mobile (`360px`):** Top `SegmentedControl` switches cleanly between `Params`, `Session`, and `Inspector`.

### 9.5 Upstream Connection 5-Step Wizard (`/admin/connections/new` & `/:id/edit`)
Replaces the 7,036-line `CreateAccountModal.vue` with a deep-linkable full-page wizard (`Stepper` on left `240px` rail on desktop, compact horizontal step bar on mobile) plus a lightweight right-hand `Drawer` on `/admin/accounts` for quick status/quota edits:
1. **Step 1 — Provider & Access Mode:** Searchable grid of provider families (`OpenAI`, `Anthropic`, `Gemini / Vertex`, `Bedrock`, `Azure`, `DeepSeek`, `Compatible OpenAI/Claude`, `OAuth / Session` where applicable) showing supported wire protocols and auth methods.
2. **Step 2 — Endpoint, Credentials & Egress:** Base URL, `ProviderSchema`-driven credential inputs (`SecretInput` enforcing `Keep / Replace / Clear` on edit, never echoing stored secrets), custom headers, TLS profile, and Egress Proxy / Pool selector. Allows `Save as Unverified Draft` at any point.
3. **Step 3 — Connection & Auth Verification:** Runs real server-side probe (`Network / Proxy Reachability` → `TLS Handshake` → `Credential Auth` → `Model List Discovery`), categorizing failures distinctly (`Proxy Unreachable`, `401 Invalid Secret`, `403 Missing Scope`, `Model Discovery Unsupported`) with `request_id`.
4. **Step 4 — Model Mapping & Capabilities:** OpenCode-style searchable toggle list + bulk mapping table (`Upstream Model ID → Public Catalog Slug`), capability overrides, and billing rate / cost basis verification.
5. **Step 5 — Pool Assignment, Quotas & Confirm:** Resource pool membership, priority/weight, RPM/TPM/concurrency/daily spend caps, cooldown rules (`UIR01`), and a structured `DiffPreview` summary. Primary CTA `Enable Connection` (disabled with clear warning if unverified unless operator explicitly selects `Save Disabled / Draft`).

### 9.6 Routing Policy & Simulator (`/admin/routing`)
- **Layout:** Top bar shows Scope (`Global` / `Workspace` / `Pool`), Policy Selector, active `Published v14` badge vs `Draft v15 (Unsaved changes)`, `Simulate`, and `Publish Version…`.
- **Split Workspace:**
  - **Left (`55%`) — Policy Rules:** Explicitly separates **Hard Constraints** (never violated: Allowed Regions, Require ZDR / No-Training, Max Input/Output Price ceiling, Protocol requirement, Excluded Providers) from **Soft Ranking Weights** (Priority tier, Weight %, Observed Latency/Throughput preference, Cache affinity, Fallback chain & retry status codes).
  - **Right (`45%`) — Dry-Run Simulator:** Operator inputs synthetic request attributes (`Model`, `Workspace/Key`, `Protocol`, `Estimated Tokens`, `Data Policy`, optional simulated upstream outage/429 toggles) and clicks `Run Simulation`. Renders a deterministic **Candidate Evaluation Waterfall**: every candidate connection shows `Selected #1`, `Fallback #2`, or `Filtered Out` with exact machine-readable reason (`Excluded: ZDR not verified`, `Skipped: Cooldown 18s remaining`, `Rank score 0.84`).
- **Publish & Rollback:** `Publish` opens a confirmation modal with version `DiffPreview`, blast-radius count (affected pools/models), and rollback one-click target to prior immutable revisions.

### 9.7 Request Detail & Attempt Waterfall (`/requests/:id` & `/admin/requests/:id`)
- **Header:** Mono `req_...` ID + `CopyButton`, HTTP status `Badge` (`200 OK` / `429 Rate Limited` / `502 Upstream Error`), canonical model slug, timestamp (`ISO + local TZ`), and total duration + settlement state badge.
- **4 Diagnostic Zones:**
  1. **KPI Strip (`4 × StatNumber`):** Total Latency, TTFT, Tokens (`Input / Cached / Reasoning / Output`), Cost (`$0.0142 · Settled` with price version link).
  2. **Attempt Waterfall Timeline:** Horizontal proportional timeline bar per gateway phase (`Auth & Quota Gate` → `Attempt #1: conn-openai-hk [429 in 180ms → Failover]` → `Attempt #2: conn-openai-us [200 Stream, TTFT 410ms, Total 2.1s]`). Admin view reveals upstream connection/proxy IDs; tenant view redacts internal operator connection names while preserving transparent failure category (`Platform Upstream Rate-Limited — Retried Automatically` vs `Your API Key Quota Exceeded`).
  3. **Routing & Error Evidence:** Exact failure classification (`Auth` / `Quota` / `Policy Block` / `Upstream 4xx/5xx` / `Client Cancelled Stream` / `Gateway Timeout`), matched policy version, and response headers.
  4. **Ledger & Price Proof:** Exact formula breakdown (`Input 4,200 × $2.00/1M + Cached 18,000 × $0.20/1M + Output 850 × $10.00/1M = $0.020500 USD`, `Price Version pv_2026_11_18`).
- **Guarded Replay Action:** `Replay in Playground` requires explicit confirmation stating that replay issues a new live inference call and incurs fresh usage charges.

### 9.8 Cost, Quota & Settlement Workbench (`/billing` & `/admin/billing`)
- **Unified Filter Bar (URL-synced):** Date range + timezone selector, Workspace/Project, API Key, Model, Provider/BYOK source, Currency, and Settlement Status (`All`, `Settled`, `Reserved`, `Estimated`, `Discrepancy`).
- **Effective Quota Waterfall (Nexus-inspired clarity):** Visualizes the active ceiling governing requests (`Organization Balance` → `Workspace Monthly Budget` → `Subscription Window Allowance` → `API Key Limit`), highlighting whichever bottleneck is tightest in amber/warning before requests fail. Clearly separates **Platform Balance Spend** from **Tenant BYOK Pass-Through Usage**.
- **Spend Chart + Drill-Down Ledger Table:** Stacked bar chart (by Model, Key, or Cost Source) directly above the immutable ledger/usage table sharing the exact same filter state. Clicking any row opens a detail drawer linking to `request_id`, `price_version`, and ledger entry ID.
- **Admin Settlement Queue & Adjustments:** Dedicated tab for `Pending Settlement`, `Missing Usage Proof`, `Amount Mismatch`, and `Manual Credit/Refund Adjustment` (append-only compensating ledger entry form citing original transaction ID—never mutating historical charge rows).

### 9.9 Task Center (`/tasks`)
- **Purpose:** Single operational home for asynchronous jobs: image/video/audio generation, batch inference, data exports, bulk imports, and credential drain/rotation jobs.
- **Summary Strip + Filterable Table:** Filters by `Type`, `Status` (`Queued`, `Running`, `Succeeded`, `Partial Failure`, `Failed`, `Cancelled`, `Expired`), `Model`, and `Created Window`.
- **Honest Progress Semantics:** When upstream returns exact item counts (`42 / 100 completed`), show a determinate `meter` bar + mono count; when upstream only reports lifecycle state, show stage stepper (`Submitted → Processing Upstream → Fetching Artifact`)—never a fake `0%–99%` timer bar.
- **Artifact & Sub-Item Drawer:** Shows `request_id`, cost settlement badge, explicit artifact expiration countdown (`Expires in 23h 14m` with `Download` / `Delete now`), and `Retry failed sub-items only` for partial batch failures.

### 9.10 Service Status Page (`/status` & `/monitor`)
- **Public `/status` (Light default, zero tenant/secret leakage):**
  - **Top Banner:** Overall state (`All Systems Operational`, `Partial Degradation`, or `Active Incident`) + `Last aggregated HH:MM:SS UTC (60s window, 30s delay)`.
  - **Platform Core Strip:** 90-day / 24-hour discrete bar strip for `OpenAI-Compatible Gateway`, `Anthropic-Compatible Gateway`, `Gemini-Compatible Gateway`, `Console & Auth`, and `Async Task Workers`.
  - **Model Family Health Table:** Aggregated public model availability %, median TTFT, stream completion %, and distinct failure modes (`First-Byte Timeout`, `Mid-Stream Drop`, `Upstream Rate Limit`). Models without recent traffic render neutral gray bars (`— · No recent sample`), never `100%` green by default.
  - **Incident Timeline:** Timestamped lifecycle entries (`Investigating` → `Identified` → `Monitoring` → `Resolved`) with affected models/protocols.
- **Authenticated `/monitor`:** Adds workspace-scoped pool health and deep links from any degraded model directly into pre-filtered `/usage?status=error&model=...`.

### 9.11 Pricing & Plans (`/pricing`)
- **Layout (OpenCode Go & Vercel inspired, Light default):**
  - **Header & Mode Switcher:** Headline `Transparent, verifiable inference pricing`, currency/unit notice (`USD · Exact decimal billing`), monochrome supported provider mark strip, and `SegmentedControl` (`Pay-As-You-Go` · `Subscriptions` · `BYOK & Enterprise`).
  - **Plan Cards (2–3 architectural columns):** Mono plan eyebrow (`PAY-AS-YOU-GO`, `PRO SUBSCRIPTION`, `TEAM / WORKSPACE`), large tabular `StatNumber` price (`$0` base + usage, or `$20 / month`), explicit entitlement bullets separating **Subscription Window Quota** from **Top-Up Credit Balance** and **BYOK Fee Policy**, plus clear CTA (`Create Free Key`, `Subscribe`, `Configure Workspace`).
  - **Full Rate Schedule & Interactive Calculator:** Searchable rate table linked to the live catalog projection (expanding cached prompt, reasoning output, batch discount, and image/audio per-unit pricing) alongside an interactive **Scenario Calculator** labeled `Estimate only — billed per actual settled token/unit under active price version`.

---

## 10. Acceptance Checklist for Design & Migration PRs

Every work package (`WP1`–`WP7`) consuming this system must pass before merge into `codex/refactor`:

- [ ] Zero off-brand accents (no cyan/purple/indigo theme overrides; Amber is the sole accent; dark ink `#141008` on solid amber buttons).
- [ ] All prices, IDs, token counts, latencies, and percentages use `--oh-font-mono` + `tabular-nums`; `null`/`undefined` renders `—`, never `0`.
- [ ] zero `Sub2API` strings in user-visible UI, titles, metadata, or onboarding snippets; storage keys follow `omnihive.<domain>.v1` with dual-read migration where required (`D08`).
- [ ] All 10 states (Skeleton, Refreshing, Empty, Zero-Filter, Partial Error, `403` Permission, Stale, `409` Conflict, Confirmed Save, Unknown Outcome) verified against real API bindings—zero mock data in shipped routes.
- [ ] Verified in Light & Dark themes and at `360px`, `768px`, `1280px`, and `1920px` viewports with full keyboard navigation (`Tab`, `Escape`, `Enter`, `⌘K`, focus return).
