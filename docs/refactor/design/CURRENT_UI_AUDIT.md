# Current Frontend Architecture & UI Audit

**Audit Baseline:** `codex/design-system` worktree (`301,262` total lines across `.vue` and `.ts` under `frontend/src/`), 2026-11-19.  
**Scope:** Exhaustive source audit of `frontend/src/router/index.ts`, `frontend/src/views/**`, `frontend/src/components/**`, `frontend/src/styles/**`, `frontend/src/style.css`, `frontend/tailwind.config.js`, `frontend/src/stores/**`, `frontend/src/api/**`, and `frontend/src/i18n/**`.

---

## 1. Complete Route Inventory & Target IA Mapping

All routes currently defined in `frontend/src/router/index.ts` must preserve working deep links (either as canonical paths or router redirects) when reorganized into the three-tier OmniHive Information Architecture (**Public Site**, **User Console**, **Admin Console**).

### 1.1 Public, Onboarding & Identity Routes

| Current Route | View Component | Guard / Meta | Current Purpose | Target IA Location & Deep-Link Contract |
| --- | --- | --- | --- | --- |
| `/` | Redirect → `/home` | Public | Root entry | **Public / Home** (`/` renders Homepage or keeps `/home` alias) |
| `/home` | `views/HomeView.vue` (2,658L) | `requiresAuth: false` | Marketing landing + prototype widgets | **Public / Home** (`/home`, alias `/`) |
| `/model-plaza` | `views/ModelPlazaView.vue` | `requiresAuth: false` | Public/console model directory | **Public & User Console / Models → Model Plaza** (`/models`, keep `/model-plaza` redirect) |
| `/key-usage` | `views/KeyUsageView.vue` (1,234L) | `requiresAuth: false` | Unauthenticated key quota lookup | **Public / Key Quota Inspector** (`/key-usage`, linked from Docs/Footer) |
| `/legal/:documentId` | `views/public/LegalDocumentView.vue` | `requiresAuth: false` | Terms, privacy & compliance docs | **Public / Legal** (`/legal/:documentId`) |
| `/setup` | `views/setup/SetupWizardView.vue` | `requiresAuth: false` + setup status guard | Initial instance DB/Redis/Admin wizard | **System Onboarding** (`/setup`, locked once `needs_setup === false`) |
| `/login` | `views/auth/LoginView.vue` | `requiresAuth: false` (guest redirect) | Password, passkey & OAuth sign-in | **Identity / Sign In** (`/login`) |
| `/register` | `views/auth/RegisterView.vue` (1,232L) | `requiresAuth: false` | Account registration + promo/invite | **Identity / Register** (`/register`) |
| `/email-verify` | `views/auth/EmailVerifyView.vue` | `requiresAuth: false` | Pending email verification gate | **Identity / Verify Email** (`/email-verify`) |
| `/forgot-password` | `views/auth/ForgotPasswordView.vue` | `requiresAuth: false` | Password reset request | **Identity / Account Recovery** (preserve exact path) |
| `/reset-password` | `views/auth/ResetPasswordView.vue` | `requiresAuth: false` | Password reset token completion | **Identity / Account Recovery** (preserve exact path) |
| `/auth/callback` (alias `/auth/oauth/callback`) | `views/auth/OAuthCallbackView.vue` | `requiresAuth: false` | Generic OAuth callback | **Identity / Callback** (preserve exact callback URI) |
| `/auth/linuxdo/callback` | `views/auth/LinuxDoCallbackView.vue` | `requiresAuth: false` | LinuxDo OAuth return | **Identity / Callback** (preserve exact callback URI) |
| `/auth/wechat/callback` | `views/auth/WechatCallbackView.vue` | `requiresAuth: false` | WeChat login callback | **Identity / Callback** (preserve exact callback URI) |
| `/auth/wechat/payment/callback` | `views/auth/WechatPaymentCallbackView.vue` | `requiresAuth: false` | WeChat payment openid callback | **Identity / Payment Callback** (preserve exact callback URI) |
| `/auth/dingtalk/callback` | `views/auth/DingTalkCallbackView.vue` | `requiresAuth: false` | DingTalk OAuth return | **Identity / Callback** (preserve exact callback URI) |
| `/auth/dingtalk/email-completion` | `views/auth/DingTalkEmailCompletionView.vue` | `requiresAuth: false` | Post-DingTalk email binding | **Identity / Callback** (preserve exact path) |
| `/auth/oidc/callback` | `views/auth/OidcCallbackView.vue` | `requiresAuth: false` | Enterprise OIDC return | **Identity / Callback** (preserve exact callback URI) |
| `/oauth/mcp/authorize` | `views/auth/McpAuthorizeView.vue` | `requiresAuth: false` | OAuth consent screen | **Identity / Consent** (`/oauth/mcp/authorize`) |
| `/:pathMatch(.*)*` | `views/NotFoundView.vue` | `requiresAuth: false` | 404 fallback | **Shared / Not Found** |

### 1.2 User Console & Payment Callback Routes

| Current Route | View Component | Guard / Meta | Current Purpose | Target IA Location (`PLAN_v1.0.md` §10.2) |
| --- | --- | --- | --- | --- |
| `/dashboard` | `views/user/DashboardView.vue` | `requiresAuth: true` | User spend, request KPIs, quickstart | **User Console / 1. Overview** (`/dashboard`) |
| `/keys` | `views/user/KeysView.vue` (2,814L) | `requiresAuth: true` | API key CRUD, quotas, IP/model scopes | **User Console / 3. Access & Resources → Access Keys** (`/keys`) |
| `/workspaces` | `views/user/WorkspacesView.vue` | `requiresAuth: true` | Workspace membership & switching | **User Console / 3. Access & Resources → Workspaces** (`/workspaces` + top ScopeSwitcher) |
| `/available-channels` | `views/user/AvailableChannelsView.vue` | `requiresAuth: true` | User-visible groups/pools & rates | **User Console / 4. Policies & Governance → Accessible Pools & Rates** (`/policies`, redirect `/available-channels`) |
| `/usage` | `views/user/UsageView.vue` | `requiresAuth: true` | Request logs, token/cost table, errors | **User Console / 5. Requests & Tasks → Request Logs** (`/requests` + `/billing/usage`, preserve `/usage`) |
| `/monitor` | `views/user/ChannelStatusView.vue` | `requiresAuth: true` | Upstream/model availability probes | **User Console / 5. Requests & Tasks → Service Health** (`/status` / `/monitor`) |
| `/batch-image` (alias `/docs/batch-image`) | `views/user/BatchImageGuideView.vue` (2,922L) | `requiresAuth: true` | Async image guide + task runner | **User Console / 5. Requests & Tasks → Task Center** (`/tasks`) + **2. Models → API Docs** (`/docs/batch-image`) |
| `/help` | `views/HelpGuide.vue` | `requiresAuth: true` | Integration quickstart & client docs | **Public & User Console / 2. Models & Invocation → API Docs** (`/docs`, preserve `/help`) |
| `/purchase` | `views/user/PaymentView.vue` (1,209L) | `requiresAuth: true`, `requiresPayment: true` | Top-up balance & buy plans | **User Console / 6. Billing & Entitlements → Top Up & Plans** (`/billing/topup`, preserve `/purchase`) |
| `/subscriptions` | `views/user/SubscriptionsView.vue` | `requiresAuth: true`, `requiresSubscription: true`, blocked in SimpleMode | Active user subscriptions & quotas | **User Console / 6. Billing & Entitlements → Subscriptions** (`/subscriptions`) |
| `/orders` | `views/user/UserOrdersView.vue` | `requiresAuth: true`, `requiresPayment: true` | Payment order history & status | **User Console / 6. Billing & Entitlements → Orders** (`/orders`) |
| `/redeem` | `views/user/RedeemView.vue` | `requiresAuth: true`, blocked in SimpleMode | Credit/subscription code redemption | **User Console / 6. Billing & Entitlements → Redeem** (`/redeem`) |
| `/affiliate` | `views/user/AffiliateView.vue` | `requiresAuth: true` | Referral links & commission ledger | **User Console / 6. Billing & Entitlements → Referrals** (`/affiliate`) |
| `/payment/qrcode` | `views/user/PaymentQRCodeView.vue` | `requiresAuth: true`, `requiresPayment: true` | QR scan checkout step | **Checkout Flow** (`/payment/qrcode` — preserve exact path) |
| `/payment/result` | `views/user/PaymentResultView.vue` | `paymentRouteComponent(..., 'result')` | Return landing from payment gateway | **Checkout Callback** (`/payment/result` — preserve exact path) |
| `/payment/stripe` | `views/user/StripePaymentView.vue` | `paymentRouteComponent(..., 'stripe')` | Hosted/inline Stripe checkout | **Checkout Callback** (`/payment/stripe` — preserve exact path) |
| `/payment/stripe-popup` | `views/user/StripePopupView.vue` | `requiresAuth: false` | Popup Stripe completion | **Checkout Callback** (`/payment/stripe-popup` — preserve exact path) |
| `/payment/airwallex` | `views/user/AirwallexPaymentView.vue` | `paymentRouteComponent(..., 'airwallex')` | Airwallex checkout return | **Checkout Callback** (`/payment/airwallex` — preserve exact path) |
| `/profile` | `views/user/ProfileView.vue` | `requiresAuth: true` | Password, MFA/passkey, OAuth bindings | **User Console / 7. Settings → Security & Profile** (`/settings/profile`, preserve `/profile`) |
| `/settings/mcp` | `views/user/McpSettingsView.vue` | `requiresAuth: true` | Authorized OAuth/MCP tokens | **User Console / 7. Settings → Authorized Apps** (`/settings/mcp`) |
| `/custom/:id` | `views/user/CustomPageView.vue` | `requiresAuth: true` | Operator-defined custom menu iframe/md | **User Console / Custom Extension Page** (`/custom/:id`) |

### 1.3 Admin Console Routes

| Current Route | View Component | Guard / Meta | Current Purpose | Target IA Location (`PLAN_v1.0.md` §10.2) |
| --- | --- | --- | --- | --- |
| `/admin` | Redirect → `/admin/dashboard` | `requiresAdmin: true` | Admin root | **Admin Console / 1. Overview** |
| `/admin/compliance` | Null render (global `AdminComplianceDialog`) | `requiresAuth: true`, `requiresAdmin: true` | Mandatory operator compliance gate | **Admin Mandatory Gate** (`/admin/compliance` — preserve guard behavior) |
| `/admin/dashboard` | `views/admin/DashboardView.vue` | `requiresAdmin: true` | Traffic, throughput, top models/users | **Admin Console / 1. Overview** (`/admin/dashboard`) |
| `/admin/accounts` | `views/admin/AccountsView.vue` (2,852L) | `requiresAdmin: true` | Upstream provider accounts & credentials | **Admin Console / 3. Resources → Upstream Connections & Credentials** (`/admin/connections`, preserve `/admin/accounts`) |
| `/admin/groups` | `views/admin/GroupsView.vue` (5,418L) | `requiresAdmin: true` | Resource pools, model whitelist, routing & pricing | **Split across:** **3. Resources → Resource Pools** (`/admin/pools`), **4. Policies → Routing Policies** (`/admin/routing`), **6. Billing → Pool Pricing** (preserve `/admin/groups` redirect) |
| `/admin/channels` | Redirect → `/admin/channels/pricing` | `requiresAdmin: true` | Channel section entry | **Admin Console / 2. Models & Catalog** |
| `/admin/channels/pricing` | `views/admin/ChannelsView.vue` (1,866L) | `requiresAdmin: true` | Model mapping & channel pricing | **Admin Console / 2. Models → Catalog & Mapping** + **6. Billing → Model Pricing** (preserve path) |
| `/admin/channels/monitor` | `views/admin/ChannelMonitorView.vue` | `requiresAdmin: true` | Synthetic upstream probe schedules | **Admin Console / 5. Requests & Health → Capacity & Probes** (`/admin/channels/monitor`) |
| `/admin/proxies` | `views/admin/ProxiesView.vue` (2,204L) | `requiresAdmin: true` | Egress proxy inventory & latency checks | **Admin Console / 3. Resources → Egress Proxies** (`/admin/proxies`) |
| `/admin/proxy-groups` | `views/admin/ProxyGroupsView.vue` | `requiresAdmin: true` | Egress IP pools & account bindings | **Admin Console / 3. Resources → Egress Pools** (`/admin/proxy-groups`) |
| `/admin/risk-control` | `views/admin/RiskControlView.vue` (2,372L) | `requiresAdmin: true`, `requiresRiskControl: true` | Anomaly rules, rate/IP blocks | **Admin Console / 4. Policies & Governance → Risk & Rate Policies** (`/admin/risk-control`) |
| `/admin/prompt-audit` | `features/prompt-audit/PromptAuditView.vue` | `requiresAdmin: true`, `requiresRiskControl: true` | Content inspection policies & hits | **Admin Console / 4. Policies & Governance → Content Policy** (`/admin/prompt-audit`) |
| `/admin/ops` | `views/admin/ops/OpsDashboard.vue` | `requiresAdmin: true` | Live telemetry, error logs, concurrency | **Admin Console / 5. Requests & Diagnostics → Operations & Attempts** (`/admin/ops`) |
| `/admin/usage` | `views/admin/UsageView.vue` | `requiresAdmin: true` | Cross-tenant usage & cost records | **Admin Console / 5. Requests & Diagnostics → Request Explorer** + **6. Billing → Settlement Ledger** (`/admin/usage`) |
| `/admin/subscriptions` | `views/admin/SubscriptionsView.vue` (1,677L) | `requiresAdmin: true`, blocked in SimpleMode | Tenant subscription grants & bulk ops | **Admin Console / 6. Billing & Settlement → Subscriptions** (`/admin/subscriptions`) |
| `/admin/orders/dashboard` | `views/admin/orders/AdminPaymentDashboardView.vue` | `requiresAdmin: true`, `requiresPayment: true` | Revenue & payment provider funnel | **Admin Console / 6. Billing & Settlement → Payment Overview** (`/admin/orders/dashboard`) |
| `/admin/orders` | `views/admin/orders/AdminOrdersView.vue` | `requiresAdmin: true`, `requiresPayment: true` | Payment orders, refunds, reconciliation | **Admin Console / 6. Billing & Settlement → Orders & Adjustments** (`/admin/orders`) |
| `/admin/orders/plans` | `views/admin/orders/AdminPaymentPlansView.vue` | `requiresAdmin: true`, `requiresPayment: true` | Plan catalog authoring | **Admin Console / 6. Billing & Settlement → Plans** (`/admin/orders/plans`) |
| `/admin/redeem` | `views/admin/RedeemView.vue` (1,236L) | `requiresAdmin: true`, blocked in SimpleMode | Credit/voucher batch generation | **Admin Console / 6. Billing & Settlement → Credit Vouchers** (`/admin/redeem`) |
| `/admin/promo-codes` | `views/admin/PromoCodesView.vue` | `requiresAdmin: true` | Discount/bonus code rules | **Admin Console / 6. Billing & Settlement → Promotions** (`/admin/promo-codes`) |
| `/admin/affiliates` | Redirect → `/admin/affiliates/invites` | `requiresAdmin: true` | Affiliate admin root | **Admin Console / 6. Billing & Settlement → Referrals** |
| `/admin/affiliates/invites` | `views/admin/affiliates/AdminAffiliateInvitesView.vue` | `requiresAdmin: true` | Invite attribution records | **Admin Console / 6. Billing & Settlement → Referrals (Invites)** |
| `/admin/affiliates/rebates` | `views/admin/affiliates/AdminAffiliateRebatesView.vue` | `requiresAdmin: true` | Rebate calculation ledger | **Admin Console / 6. Billing & Settlement → Referrals (Rebates)** |
| `/admin/affiliates/transfers` | `views/admin/affiliates/AdminAffiliateTransfersView.vue` | `requiresAdmin: true` | Commission payout transfers | **Admin Console / 6. Billing & Settlement → Referrals (Transfers)** |
| `/admin/users` | `views/admin/UsersView.vue` (2,096L) | `requiresAdmin: true` | User directory, balances, attributes, groups | **Admin Console / 7. Settings & System → Users & Roles** (`/admin/users`) |
| `/admin/audit-logs` | `views/admin/AuditLogView.vue` | `requiresAdmin: true` | Operator/security audit trail | **Admin Console / 7. Settings & System → Audit Trail** (`/admin/audit-logs`) |
| `/admin/announcements` | `views/admin/AnnouncementsView.vue` | `requiresAdmin: true` | In-app/banner notices | **Admin Console / 7. Settings & System → Announcements** (`/admin/announcements`) |
| `/admin/plugins` | `views/admin/PluginsView.vue` | `requiresAdmin: true` | Runtime extensions & signatures | **Admin Console / 7. Settings & System → Extensions** (`/admin/plugins`) |
| `/admin/backup` | `views/admin/BackupView.vue` | `requiresAdmin: true` | DB backup, archive & restore | **Admin Console / 7. Settings & System → Backup & Recovery** (`/admin/backup` — full-page flow) |
| `/admin/settings` | `views/admin/SettingsView.vue` | `requiresAdmin: true` | Monolithic settings tabs (general, auth, mail, payment, scheduling) | **Admin Console / 7. Settings & System** (split sub-routes for General, Identity/Auth, Payment Gateways, Deployment & Updates) |

### 1.4 Generated Workspace-Scoped Routes & New Missing Routes

`router/index.ts` dynamically clones 8 resource routes under `/workspaces/:workspace_id/...` (`accounts`, `groups`, `proxies`, `proxy-groups`, `channels/pricing`, `channels/monitor`, `keys`, `monitor`), clearing `requiresAdmin` and checking `workspacesAPI.list()` membership (`role === 'admin'` except `/keys`). Under the new IA, workspace context moves into the persistent top `ScopeSwitcher`, while these 8 deep links remain valid and synchronize the active workspace scope.

**Net-new routes required by `PLAN_v1.0.md` §10.2–10.8:**
- Public: `/models` (canonical for `/model-plaza`), `/models/:slug` (Model Detail), `/models/compare` (Compare & Heatmap), `/pricing` (Plans & Rate Calculator), `/docs` (Public/Console API Docs), `/status` (Public Service Health).
- User Console: `/playground` (Interactive API Debugger), `/requests/:id` (Request Waterfall & Cost Evidence), `/tasks` (Unified Async Task Center), `/billing` (Effective Quota & Ledger).
- Admin Console: `/admin/connections/new` & `/admin/connections/:id/edit` (5-step Upstream Wizard replacing the 7,036-line modal), `/admin/routing` (Routing Policy & Simulator), `/admin/requests/:id` (Admin Attempt Diagnostics).
- Dev-only: `/__design` (Design System v1 Component & State Gallery, registered only when `import.meta.env.DEV || import.meta.env.MODE === 'test'`).

---

## 2. Visual, Token & Engineering Inconsistencies

1. **Five Competing Theme Directions Violate Charter Brand Identity:**
   `frontend/src/style.css` (1,698 lines) and `composables/useThemeDirection.ts` ship five unrelated palettes (`teal` "Hive Aura", `control` cyan, `slate` indigo-purple, `indigo` violet, `mono` neon-mint) plus a floating `ThemeStyleDock.vue` widget and `body.spec-mode` dashed debug outlines. Four of the five palettes replace OmniHive's Amber accent with cyan, violet, or neon green, directly violating `CHARTER.md` (*"Amber = the single accent"*).
2. **Dark-Mode Boot Race Between `index.html` and `main.ts`:**
   `frontend/index.html` checks `localStorage.getItem('omnihive.mode') || localStorage.getItem('theme')`, whereas `frontend/src/main.ts` (`initThemeClass`) reads **only** `localStorage.getItem('theme')`. Toggling dark mode via `useThemeDirection` (`omnihive.mode`) causes `index.html` to add `.dark` before paint and `main.ts` to immediately strip `.dark` during Vue bootstrap, producing a visible flash and broken theme persistence.
3. **Unbundled Fonts (`Inter` & `JetBrains Mono` Missing):**
   Both `tailwind.config.js` and `style.css` declare `'Inter'` and `'JetBrains Mono'`, yet `frontend/public/` and `package.json` contain **zero** font files or `@fontsource` packages. Every machine without those developer fonts locally installed falls back to `Microsoft YaHei` / `Segoe UI` / `Consolas`, destroying typographic hierarchy and tabular numeric alignment.
4. **Three Competing Styling Layers & Decorative Bloat:**
   - Layer A: Legacy Tailwind `@apply` classes in `style.css` hardcoded to `bg-white dark:bg-dark-800 border-gray-200 dark:border-dark-700 rounded-2xl shadow-2xl` and `shadow-glow` / `backdrop-blur`.
   - Layer B: Prototype CSS variables (`--background`, `--card`, `--brand`) mixed with cartoon keyframe animations (`@keyframes beeBob`, `petalSway`, `nectarGlow`, `floaty`).
   - Layer C: Ad-hoc inline Tailwind grays (`text-gray-500 dark:text-gray-400`, `bg-slate-900`) inside individual `.vue` views.
5. **Unsafe Raw HTML Drawer (`OmniHiveDrawer.vue`):**
   `useThemeDirection.ts` stores global `drawerState.html` and `footerHtml` strings rendered via raw markup rather than typed Vue slots, mixing theme state, overlay orchestration, and string templating in a single composable.
6. **Mixed Localization Discipline:**
   While `frontend/src/i18n/locales/{en,zh}/` has a domain-split key structure enforced by `localeKeyCompleteness.spec.ts`, newer shell additions (`AppSidebar.vue` `"快速筛选页面…"`, `"没有匹配的页面"`, `router/index.ts` `'备份与归档'`, `'MCP 授权'`) hardcode Simplified Chinese directly in templates and route titles.

---

## 3. Duplicated Components & Logic (`PLAN_v1.0.md` §11.6 + UI Primitives)

### 3.1 Primitive Component Duplication Matrix

| UI Need | Current Competing Implementations | Defect | Target Canonical Primitive (`components/ui/`) |
| --- | --- | --- | --- |
| Buttons | `.btn`, `.btn-primary`, `.btn-secondary`, `.btn-ghost`, `.btn-danger` in `style.css` + raw `<button class="px-4 py-2 ...">` across 60+ views | Inconsistent heights (`34px`–`44px`), missing double-submit lock, white text on amber failing AA contrast (`2.15:1`) | `Button.vue`, `IconButton.vue` (dark `#14110B` ink on `#F59E0B` amber = `9.6:1` contrast, `loading` lock) |
| Text / Secret Inputs | `common/Input.vue`, `common/SearchInput.vue`, `common/TextArea.vue`, `.input` CSS class, raw `<input>` | Missing unified `id`/`aria-describedby`/`aria-invalid` wiring and secret keep/replace affordances | `Input.vue` (prefix/suffix slots, mono option, error/hint aria binding) |
| Selects | `common/Select.vue`, `GroupSelector.vue`, `ProxySelector.vue`, native `<select class="input">` | Keyboard navigation and disabled/empty states vary per screen | `Select.vue` (keyboard accessible, empty/loading/disabled states) |
| Switches & Checkboxes | `common/Toggle.vue`, `payment/ToggleSwitch.vue`, raw `<input type="checkbox">` | Two separate switch components with different sizes, colors, and `role="switch"` semantics | `Switch.vue`, `Checkbox.vue` (indeterminate support, `role="switch"`) |
| Badges & Tags | `common/StatusBadge.vue`, `payment/OrderStatusBadge.vue`, `GroupBadge.vue`, `PlatformTypeBadge.vue`, `.badge-*`, `.pill`, `.chip` | Status dots rely on color alone or conflicting hues | `Badge.vue` (semantic status variants with icon/dot + border + accessible label) |
| Cards & Metrics | `common/StatCard.vue`, `.card`, `.card-glass`, ad-hoc `<div class="rounded-2xl border ...">` | Inconsistent padding (`16px`–`28px`), excessive `rounded-2xl` radii, non-tabular numbers | `Card.vue`, `StatNumber.vue` (`tabular-nums`, explicit `0` vs `Unknown —` vs `Free`) |
| Tables | `common/DataTable.vue`, `payment/OrderTable.vue`, `user/UserErrorRequestsTable.vue`, `PlazaModelPricingTable.vue`, raw `<table class="table">` | Sort headers lack `aria-sort`; empty vs filtered-empty vs error states duplicated per view | `Table.vue` (sortable headers, `aria-sort`, Comfort/Compact density, skeleton/empty/error slots) |
| Tabs & Segmented | `.oh-tab-strip` CSS, `.segmented` CSS, ad-hoc button bars in `SettingsView` / `GroupsView` | Missing `role="tablist"` / ArrowLeft-ArrowRight roving focus | `Tabs.vue`, `SegmentedControl.vue` (WAI-ARIA tablist + keyboard navigation) |
| Modals & Drawers | `common/BaseDialog.vue`, `common/ConfirmDialog.vue`, `OmniHiveDrawer.vue`, `.modal-overlay`, `.dialog-overlay` | Five overlay implementations; inconsistent Esc/focus-return behavior | `Modal.vue`, `Drawer.vue` (focus trap, Esc close, trigger focus restoration, scroll lock) |
| Toasts & Copy | `common/Toast.vue` (`appStore.toasts`), ad-hoc `navigator.clipboard.writeText` + global toast | Copy triggers global toast far from cursor instead of in-place confirmation | `Toast.vue`, `CopyButton.vue` (in-place `Copied` / `Failed` feedback + `aria-live`) |
| Empty / Error / Skeleton | `common/EmptyState.vue`, `UsageStatsState.vue`, `common/Skeleton.vue`, `LoadingSpinner.vue` | Spinner-only loading hides layout; permission-denied and error request IDs lack standard panel | `Skeleton.vue`, `EmptyState.vue`, `ErrorState.vue` (supports `error`, `permission`, `stale` modes + `requestId`) |
| Command Palette & Wizard | `common/CommandPalette.vue` (coupled to `useThemeDirection`), ad-hoc step bars in `SetupWizardView` / `CreateAccountModal` | Palette tied to legacy theme composable; wizard steppers rewritten per modal | `CommandPalette.vue` (`⌘K`/`Ctrl+K` shell), `Stepper.vue` (linear/non-linear status steps) |

### 3.2 Domain Duplication Audit (`PLAN_v1.0.md` §11.6 `UIR01`–`UIR07`)

| ID | Verified Code Locations | Duplicated Logic / Defect | Required Migration Contract |
| --- | --- | --- | --- |
| **UIR01** (P1) | `components/account/CreateAccountModal.vue` (7,036L) & `EditAccountModal.vue` (5,579L) | Both independently define `parsePoolModeRetryStatusCodes`, `toggleErrorCode`, `buildTempUnschedRules`, `buildMixedChannelDetails`, plus near-identical pool/quota/model mapping sections | Extract pure domain serializers with snapshot payload tests first; compose shared `ConnectionForm`, `CredentialForm` (`keep \| replace \| clear` secret state), `CapabilityPicker`, `PoolAssignment`, `QuotaPolicy` |
| **UIR02** (P1) | `views/admin/GroupsView.vue` (5,418L) | Create and Edit flows duplicate price multiplier, model whitelist, and routing rule assembly | Extract shared form model with explicit `serializeGroupCreate` (`null` for cleared price) vs `serializeGroupPatch` (`-1` sentinel for cleared price) |
| **UIR03** (P1) | `AccountsView.vue` (`useTableLoader`) vs `GroupsView.vue`, `UsageView.vue`, `UsersView.vue` | Only Accounts uses `useTableLoader`; Groups and Usage hand-roll `AbortController`, page cursors, stale-response guards, and loading flags | Standardize on one workspace/session-scoped query controller per page; preserve domain `unknown` states for async capacity/usage enrichment |
| **UIR04** (P0→P1) | `router/index.ts` guards + `AppSidebar.vue` + `App.vue` (`FeatureFlags`) | Payment, subscription, risk-control, simple-mode, and backend-mode visibility rules are re-evaluated separately in router guards and sidebar computed lists | Consolidate into a single `RoutePolicy` evaluator returning `{ state: 'enabled' \| 'disabled' \| 'unknown', reason }`; never treat transient public-settings fetch failure as `disabled` |
| **UIR05** (P1) | `api/client.ts`, `utils/apiError.ts`, and 20+ view `catch` blocks | Views repeatedly check `axios.isCancel` / `CanceledError` / `AUTH_SESSION_CHANGED` before deciding whether to toast | Centralize silent-cancel and stale-session suppression in API error classification; preserve blob/export error decoding |
| **UIR06** (P0→P1) | `composables/useThemeDirection.ts`, `main.ts`, `index.html`, `AppHeader.vue`, `AppSidebar.vue`, `stores/app.ts` | Theme composable owns drawer + `⌘K` state; `main.ts` and `index.html` disagree on storage key; `app.ts` and `title.ts` fall back to `'Sub2API'` | Restrict theme module strictly to `light \| dark \| system` preference; move shell overlays to shell store; centralize brand identity in `BrandConfig` defaulting to `'OmniHive'` |
| **UIR07** (P1) | `ModelPlazaView.vue`, `components/modelPlaza/*`, `ChannelsView.vue`, `PlatformCostCell.vue` | Model categories inferred via client-side name regexes; input/output/cache price unit conversion and group multipliers formatted in multiple components | Introduce shared `ModelDisplay` and `PricePresentation` projections backed by catalog metadata and exact decimal strings; never compute billing totals in JS floats |

---

## 4. Giant Components Decomposition Map

| Lines | File | Mixed Responsibilities Today | Target Decomposition |
| ---: | --- | --- | --- |
| **7,036** | `components/account/CreateAccountModal.vue` | 20+ provider schemas, OAuth popup orchestration, proxy selection, model discovery, pricing overrides, pool retry rules, temp-unschedule rules, live probe | Replace modal with full-page 5-step wizard (`/admin/connections/new`) under `features/connections/`: `ProviderStep`, `EndpointCredentialStep`, `ModelCapabilityStep`, `PoolQuotaStep`, `VerifyConfirmStep` |
| **5,579** | `components/account/EditAccountModal.vue` | Full copy of CreateAccountModal plus secret retention/partial update branches | Share the exact 5-step domain field groups on `/admin/connections/:id/edit` + lightweight `Drawer` for quick status/weight/note edits |
| **5,418** | `views/admin/GroupsView.vue` | Resource pool CRUD, capacity gauges, model access rules, per-model price overrides, routing affinity/fallback, member bindings | Split into `features/pools/` (`/admin/pools`), `features/routing/` (`/admin/routing`), and `features/billing/` pool pricing |
| **2,922** | `views/user/BatchImageGuideView.vue` | Static documentation, SDK code examples, interactive batch uploader, polling task table, artifact zip export | Split static protocol docs into `/docs` and async job execution/history into `/tasks` (`features/tasks/`) |
| **2,852** | `views/admin/AccountsView.vue` | Filter toolbar, 18-column custom table, bulk actions, inline probe triggers, OAuth re-auth, quota cells | Compose with `ui/PageHeader`, `ui/Table`, column preset manager, and isolated row action menu |
| **2,814** | `views/user/KeysView.vue` | Key table, create/edit quota modal, model/IP ACL editors, client snippet modal | Split `KeyTable`, `KeyScopeDrawer`, and `KeyQuickstartModal` under `features/keys/` |
| **2,658** | `views/HomeView.vue` | Marketing hero, animated SVG bees/flowers, mock interactive console tabs, pricing preview | Rebuild clean `<600` line `HomeView` with live catalog projection, deploy-bound SDK tabs, and architectural honeycomb diagram |
| **2,372** | `views/admin/RiskControlView.vue` | Rule builder, IP/user blocklists, hit logs, threshold settings | Split tab subviews under `features/policies/risk/` |
| **2,273** | `components/account/BulkEditAccountModal.vue` | Multi-account heterogeneous patch form without per-account diff preview | Replace with `BulkChangeWizard` featuring compatibility check + `DiffPreview` step |
| **2,204** | `views/admin/ProxiesView.vue` | Proxy CRUD, batch import, geo lookup, health check runner | Split under `features/egress/` |
| **2,096** | `views/admin/UsersView.vue` | User table, balance adjustment, group assignment, custom attributes | Split under `features/members/` with dedicated non-destructive balance adjustment drawer |
| **1,972** | `components/keys/UseKeyModal.vue` | Hardcoded third-party tool tutorials with 24 `Sub2API` references | Replace with concise protocol-standard (`OpenAI` / `Anthropic` / `Gemini` / `cURL`) quickstart panel |
| **1,866** | `views/admin/ChannelsView.vue` | Channel pricing table, batch sync, preset import, mapping editor | Split model catalog mapping vs pricing sheet |
| **1,806** | `components/account/AccountUsageCell.vue` | Provider-specific quota windows, upstream billing probes, sparkbars inside a single table cell | Extract `QuotaWindowPresenter` + compact `StatNumber`/meter cell + detail popover |

---

## 5. Leftover `Sub2API` Branding Inventory (`CHARTER.md` Identity Audit)

`rg -i "sub2api" frontend/` identifies **53 non-test files** and **14 test files**. Per `CHARTER.md`, these must **not** be blind-replaced; they fall into four distinct handling tiers:

### Tier A — User-Visible Fallbacks & Regex Band-Aids (Replace with `OmniHive` via `BrandConfig`)
- `frontend/index.html:7` — `<title>OmniHive · Sub2API - AI API Gateway</title>`
- `frontend/public/logo.svg:2` — `<title id="title">OmniHive · Sub2API</title>`
- `frontend/src/stores/app.ts:29,297` — `siteName = ref<string>('Sub2API')` default & fallback
- `frontend/src/router/title.ts:11` — `normalizedSiteName` fallback `'Sub2API'` (and update `router/__tests__/title.spec.ts`)
- `frontend/src/main.ts:48` — `if (appStore.siteName && appStore.siteName !== 'Sub2API')`
- `frontend/src/stores/adminCompliance.ts:7–8` & `components/admin/AdminComplianceDialog.vue` — Compliance acknowledgement phrase hardcoding `Sub2API` (coordinate with backend phrase validator)
- `frontend/src/views/HomeView.vue:1259,1263`, `views/setup/SetupWizardView.vue:17,18`, `views/public/LegalDocumentView.vue:73` — Runtime `.replace(/sub2api|omniscription/gi, 'OmniHive')` band-aids compensating for uncleaned locale strings.

### Tier B — User-Facing Copy, SDK Snippets & Developer Docs (Clean Rewrite in EN + ZH)
- `components/keys/UseKeyModal.vue` (24 hits: `$SUB2API_API_KEY`, config profile names), `components/intro/helpExamples.ts` (15 hits), `components/intro/quickstart.ts` (9 hits).
- `i18n/locales/{en,zh}/dashboard.ts` (10 hits each), `helpGuide.ts` (9 hits each), `admin/accounts.ts` (4–5 hits), `admin/settings.ts` (4 hits), `admin/overview.ts` (3 hits).
- View placeholders/examples: `SetupWizardView.vue:144,560` (`dbname: 'sub2api'` — preserve existing DB name on upgrade, align fresh install default with backend), `LoginView.vue`, `RegisterView.vue`, `EmailVerifyView.vue`, `KeyUsageView.vue`, `BatchImageGuideView.vue`, `PlazaNavBar.vue`, `ProxyAdBanner.vue`, `PaymentSettings.vue`, `AccountsView.vue`, `GroupsView.vue`.
- Internal markdown docs: `router/README.md`, `stores/README.md`, `components/layout/README.md`, `views/auth/*.md`.

### Tier C — Wire Contracts, Headers, Plugins & Cache Keys (Versioned Compatibility Required)
- `types/admin.ts:454` — `object: 'sub2api.key_billing'` response discriminator (coordinate dual-accept with backend API owner).
- `api/admin/plugins.ts` (6 hits) & `views/admin/PluginsView.vue` (4 hits) — Plugin manifest/signature & repository keys (must read legacy manifests during migration window per `CHARTER.md`).
- `components/account/{CreateAccountModal,EditAccountModal,UpstreamRequestIdHeaderField,UpstreamBillingRateCell}.vue` & `components/admin/TLSFingerprintProfilesModal.vue` & `ImportDataModal.vue` — Default header names (`x-sub2api-...`) and backup import format compatibility (accept both legacy `sub2api` and `omnihive` export envelopes).
- `utils/ipGeoLookup.ts:24` — `CACHE_STORAGE_KEY = 'sub2api:ip-geo-cache:v1'` (migrate to `omnihive.net.ip_geo_cache.v1` and purge old key).

### Tier D — Inherited License & Attribution
- Repository `LICENSE` / `NOTICE` files (outside `frontend/src`): retain intact per `CHARTER.md`.

---

## 6. Browser Storage, Session & Lock Keys Audit (`PLAN_v1.0.md` D08)

Currently **45+ browser storage and Web Lock keys** lack a consistent `omnihive.<domain>.v1` namespace. Blindly renaming Category 1 keys would log out active users mid-request or cause split-brain refresh races across open tabs.

### Category 1 — Multi-Tab Auth, Refresh Lock & Session Protocol (P0 Coordinated Migration)
| Key / Identifier | Storage / API | Current Owners | Migration Requirement |
| --- | --- | --- | --- |
| `auth_token` | `localStorage` | `stores/auth.ts`, `api/client.ts`, `api/tokenRefresh.ts`, `api/admin/*` | Read `omnihive.auth.token.v1` ?? legacy `auth_token`; write both during transition or migrate to HttpOnly cookie session (`PLAN_v1.0.md` §11.4); clear both on logout |
| `refresh_token` | `localStorage` | `stores/auth.ts`, `api/client.ts`, `api/tokenRefresh.ts` | Dual-read / atomic clear with `auth_token` |
| `auth_user` | `localStorage` | `stores/auth.ts`, `api/client.ts`, `api/tokenRefresh.ts`, `api/admin/{groups,channelMonitor}.ts` | Replace direct `localStorage.getItem('auth_user')` in admin API modules with auth store selector; namespace to `omnihive.auth.user.v1` |
| `token_expires_at` | `localStorage` | `stores/auth.ts`, `api/tokenRefresh.ts` | Migrate alongside token pair (`omnihive.auth.expires_at.v1`) |
| `auth_session_revision` | `localStorage` | `api/tokenRefresh.ts`, `stores/auth.ts` | Cross-tab `storage` listener must watch both `omnihive.auth.session_rev.v1` and legacy key during rollout |
| `pending_auth_session` | `localStorage` | `stores/auth.ts` | Namespace to `omnihive.auth.pending_session.v1` with dual-read |
| `sub2api-auth-token-refresh` | `navigator.locks` | `api/tokenRefresh.ts` (`TOKEN_REFRESH_LOCK_NAME`) | Coordinate lock name transition so old/new bundles cannot refresh the same single-use refresh token concurrently |
| `auth_expired` | `sessionStorage` | `api/client.ts`, `views/auth/LoginView.vue` | Namespace to `omnihive.auth.expired_notice.v1` |
| `ops_monitoring_enabled_cached` | `localStorage` | `api/client.ts`, `views/admin/ops/*` | Scope by workspace/user under `omnihive.ops.enabled.v1` |

### Category 2 — Onboarding, OAuth Handshake & Payment Recovery (P1 Dual-Read Migration)
| Keys | Storage | Current Owners | Target Namespace |
| --- | --- | --- | --- |
| `auth_remembered_email`, `register_data`, `email_oauth_pending_provider`, `oauth_aff_code`, `affiliate_referral_code` | `localStorage` / `sessionStorage` | `views/auth/*` | `omnihive.auth.<name>.v1` (never persist raw passwords; clear immediately after handshake) |
| `payment.recovery.current`, `payment_result_resume` | `sessionStorage` / `localStorage` | `components/payment/*`, `views/user/Payment*` | `omnihive.payment.recovery.v1` (preserve in-flight checkout return across redirect) |
| `duplicateOperationStorageKey(id)` | `sessionStorage` | `api/admin/{accounts,groups,channelMonitor}.ts` | `omnihive.idempotency.<resource>.v1:<id>` |
| `chunk_reload_attempted` | `sessionStorage` | `router/index.ts` | `omnihive.router.chunk_reload.v1` |

### Category 3 — Theme, Locale & Global UI Preferences (Unify Immediately in Shell Package)
| Current Competing Keys | Defect | Target Unified Key |
| --- | --- | --- |
| `theme`, `omnihive.mode`, `omnihive.style`, `ui_mode` | Four keys controlling theme/mode; `index.html` and `main.ts` read different keys; `omnihive.style` activates off-brand palettes | `omnihive.theme.mode.v1` (`'light' \| 'dark' \| 'system'`, migrating legacy `omnihive.mode` / `theme` on first read) and `omnihive.ui.density.v1` (`'comfort' \| 'compact'`) |
| `sub2api_locale`, `lang` | Legacy locale storage keys | `omnihive.i18n.locale.v1` (with read-fallback to legacy key so existing user language preference is preserved) |
| `sub2api_login_agreement_consent`, `omnihive.dashboard.onboardingHidden` | Mixed legacy tour/onboarding flags | `omnihive.onboarding.<tourId>.v1` |
| `omnihive:model-favorites`, `sub2api:ip-geo-cache:v1` | Colon delimiter + `sub2api:` prefix | `omnihive.models.favorites.v1`, `omnihive.net.ip_geo_cache.v1` |

### Category 4 — Unnamespaced Table & Filter Preferences (26 Keys)
`table-page-size`, `account-auto-refresh`, `account-hidden-columns`, `account-hidden-columns-version`, `account-table-sort`, `account_scheduling_threshold`, `group-hidden-columns`, `group-column-settings-version`, `api-key-hidden-columns`, `api-key-column-settings-version`, `admin-users-table-sort`, `admin-users-usage-sort`, `subscription-hidden-columns`, `subscription-user-column-mode`, `usage-hidden-columns`, `usage-hidden-columns-version`, `usage-error-hidden-columns`, `user-column-settings-version`, `user-filter-values`, `user-hidden-columns`, `user-usage-error-hidden-columns`, `user-usage-hidden-columns`, `user-visible-filters`, `antigravity_project_id`, `grok_client_tool_cache_enabled`, `header_override_enabled`, `header_overrides`, `protocol_rules`, `src_host`, `src_url`.
- **Target Contract:** Consolidate table presentation preferences into a typed `useTablePreferences(tableId)` helper storing JSON under `omnihive.table.<tableId>.v1`, scoped by user/workspace where columns depend on role, and never persisting upstream secrets or prompt content.

---

## 7. Recommended Parallel Page-Migration Work Packages

After `codex/design-system` lands `frontend/src/components/ui/**` and `DESIGN_SYSTEM.md`, page migration splits cleanly into **7 non-overlapping work packages** (WP1 lands first or alongside WP2–WP7 via clean route registration):

| Package | Scope & Deliverables | Exclusive File Ownership | Parallelism |
| --- | --- | --- | --- |
| **WP1: App Shell, Brand & Session Namespace** | OpenCode-style Console Shell (`AppLayout`, `AppHeader`, `AppSidebar`), `ScopeSwitcher`, `RoutePolicy` (`UIR04`), `BrandConfig` (`UIR06`), `index.html` + `main.ts` theme/brand fix, D08 storage/session adapter, Auth & Setup pages | `index.html`, `public/logo.svg`, `src/main.ts`, `src/App.vue`, `src/components/layout/**`, `src/composables/useThemeDirection.ts`, `src/stores/{app,auth,adminCompliance}.ts`, `src/api/{client,tokenRefresh}.ts`, `src/router/**`, `src/views/{auth,setup}/**` | Gatekeeper for global shell; auth/storage adapter coordinated with backend |
| **WP2: Public Site, Model Plaza, Detail/Compare & Docs** | Homepage (`/home`), Model Plaza (`/models`), Model Detail (`/models/:slug`), Compare & Heatmap (`/models/compare`), Public Pricing (`/pricing`), Public Status (`/status`), API Docs (`/docs`), `ModelDisplay` + `PricePresentation` (`UIR07`) | `src/views/{HomeView,ModelPlazaView,HelpGuide,KeyUsageView}.vue`, `src/views/public/**`, `src/components/{modelPlaza,intro}/**`, `src/features/models/**` | Runs in parallel with WP3–WP7 |
| **WP3: Developer Console — Overview, Keys, Playground & Tasks** | User Overview (`/dashboard`), Access Keys (`/keys`, `UseKeyModal` clean rewrite), Playground (`/playground`), Unified Task Center (`/tasks`), Workspaces (`/workspaces`) | `src/views/user/{DashboardView,KeysView,WorkspacesView,BatchImageGuideView,AvailableChannelsView}.vue`, `src/components/keys/**`, `src/features/{overview,playground,keys,tasks}/**` | Runs in parallel with WP2, WP4–WP7 |
| **WP4: Upstream Connections & Egress Wizard** | 5-Step Upstream Connection Wizard (`/admin/connections/new`, `/:id/edit`), Quick-Edit Drawer, Bulk Change Diff Wizard, Accounts list, Egress Proxies & Pools, `UIR01` pure rule extraction | `src/views/admin/{AccountsView,ProxiesView,ProxyGroupsView}.vue`, `src/components/{account,admin/account,proxyGroup}/**`, `src/features/{connections,credentials,egress}/**` | Runs in parallel with WP2–WP3, WP5–WP7 |
| **WP5: Resource Pools, Model Mapping & Routing Simulator** | Split `GroupsView` (5,418L) into Resource Pools (`/admin/pools`), Routing Policy + Simulator (`/admin/routing`), Model Catalog Mapping (`/admin/channels/*`), Risk & Content Policy, `UIR02` serializers | `src/views/admin/{GroupsView,ChannelsView,ChannelMonitorView,RiskControlView}.vue`, `src/components/model-access/**`, `src/features/{pools,routing,prompt-audit}/**` | Runs in parallel with WP2–WP4, WP6–WP7 |
| **WP6: Request Diagnostics, Cost Ledger & Billing** | Request Waterfall Detail (`/requests/:id`, `/admin/requests/:id`), User & Admin Usage/Ledger, Subscriptions, Payment Top-Up, Orders, Redeem, Promo, Affiliates | `src/views/user/{UsageView,Payment*,SubscriptionsView,UserOrdersView,RedeemView,AffiliateView,Stripe*,Airwallex*}.vue`, `src/views/admin/{UsageView,SubscriptionsView,RedeemView,PromoCodesView}.vue`, `src/views/admin/{orders,affiliates}/**`, `src/components/{payment,usage}/**`, `src/features/{requests,billing}/**` | Runs in parallel with WP2–WP5, WP7 |
| **WP7: Operations, Members, Audit, Backup & System Settings** | Ops Dashboard (`/admin/ops`), Channel Status (`/monitor`), Users & Roles (`/admin/users`), Audit Logs, Full-Page Backup & Recovery (`/admin/backup`), Plugins, Announcements, modular System Settings, User Profile | `src/views/admin/ops/**`, `src/views/admin/{UsersView,AuditLogView,BackupView,PluginsView,AnnouncementsView,SettingsView}.vue`, `src/views/user/{ProfileView,McpSettingsView,ChannelStatusView,CustomPageView}.vue`, `src/components/{user,admin}/**` (excluding `admin/account`), `src/features/{ops,members,audit,system}/**` | Runs in parallel with WP2–WP6 |
