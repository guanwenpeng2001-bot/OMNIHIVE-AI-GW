# Current frontend audit

Source baseline: design-system worktree, 2026-11-19. This is a code audit, not a claim that every existing screen has passed visual or task review. `frontend/src/router/index.ts` is the current route authority. Paths below must retain compatible deep links when the information architecture changes.

## Complete route inventory

Page names resolve under `frontend/src/views/` unless otherwise stated. Proposed location is navigation ownership, not an authorization change.

| Route | Page / behavior | Purpose | Proposed IA |
| --- | --- | --- | --- |
| `/` | redirect `/home` | Entry | Public |
| `/home` | HomeView | Landing | Public / Home |
| `/setup` | setup/SetupWizardView | Installation | System onboarding |
| `/login` | auth/LoginView | Sign in | Identity |
| `/register` | auth/RegisterView | Register | Identity |
| `/email-verify` | auth/EmailVerifyView | Verify email | Identity |
| `/auth/callback`, alias `/auth/oauth/callback` | auth/OAuthCallbackView | OAuth return | Identity callback |
| `/auth/linuxdo/callback` | auth/LinuxDoCallbackView | OAuth return | Identity callback |
| `/auth/wechat/callback` | auth/WechatCallbackView | OAuth return | Identity callback |
| `/auth/wechat/payment/callback` | auth/WechatPaymentCallbackView | Payment identity return | Identity callback |
| `/auth/dingtalk/callback` | auth/DingTalkCallbackView | OAuth return | Identity callback |
| `/auth/dingtalk/email-completion` | auth/DingTalkEmailCompletionView | Complete identity | Identity |
| `/auth/oidc/callback` | auth/OidcCallbackView | OIDC return | Identity callback |
| `/oauth/mcp/authorize` | auth/McpAuthorizeView | Existing authorization | Identity; preserve existing contract, no new agent product |
| `/forgot-password` | auth/ForgotPasswordView | Recovery | Identity |
| `/reset-password` | auth/ResetPasswordView | Reset | Identity |
| `/key-usage` | KeyUsageView | Key-scoped usage lookup | Public / Usage lookup |
| `/legal/:documentId` | public/LegalDocumentView | Legal documents | Public / Legal |
| `/model-plaza` | ModelPlazaView | Models and compare | Public + console / Models |
| `/dashboard` | user/DashboardView | Personal metrics | Console / Overview |
| `/help` | HelpGuide | Integration guidance | Public / Docs |
| `/keys` | user/KeysView | API keys | Console / Access |
| `/batch-image` | user/BatchImageGuideView | Batch images | Console / Tasks |
| `/usage` | user/UsageView | Usage and requests | Console / Requests; costs link to Billing |
| `/redeem` | user/RedeemView | Redeem credit | Console / Billing |
| `/affiliate` | user/AffiliateView | Referrals | Console / Billing / Referrals |
| `/available-channels` | user/AvailableChannelsView | Available resource access | Console / Models / Access |
| `/profile` | user/ProfileView | Account and security | Console / Settings |
| `/settings/mcp` | user/McpSettingsView | Existing integration settings | Console / Settings; preserve existing scope |
| `/subscriptions` | user/SubscriptionsView | Entitlements | Console / Billing / Subscriptions |
| `/purchase` | user/PaymentView | Purchase | Console / Billing |
| `/orders` | user/UserOrdersView | Orders | Console / Billing / Orders |
| `/payment/qrcode` | user/PaymentQRCodeView | Checkout | Billing checkout |
| `/payment/result` | user/PaymentResultView via paymentRouteComponent | Checkout result | Billing checkout |
| `/payment/stripe` | user/StripePaymentView via paymentRouteComponent | Checkout | Billing checkout |
| `/payment/airwallex` | user/AirwallexPaymentView via paymentRouteComponent | Checkout | Billing checkout |
| `/payment/stripe-popup` | user/StripePopupView | Checkout popup | Billing checkout |
| `/custom/:id` | user/CustomPageView | Instance content | Console / configured navigation |
| `/workspaces` | user/WorkspacesView | Membership and scope | Console / Access / Workspaces |
| `/monitor` | user/ChannelStatusView | User resource health | Console / Requests / Health |
| `/admin` | redirect `/admin/dashboard` | Admin entry | Admin |
| `/admin/compliance` | null renderer, global mandatory dialog | Compliance gate | Admin / mandatory gate |
| `/admin/dashboard` | admin/DashboardView | Operational overview | Admin / Overview |
| `/admin/ops` | admin/ops/OpsDashboard | Request diagnostics | Admin / Requests / Operations |
| `/admin/audit-logs` | admin/AuditLogView | Audit events | Admin / Settings / Audit |
| `/admin/users` | admin/UsersView | Users and access | Admin / Settings / Users |
| `/admin/groups` | admin/GroupsView | Pools, access, pricing and routing | Split Admin / Resources, Policies, Billing |
| `/admin/channels` | redirect `/admin/channels/pricing` | Channel entry | Admin / Models |
| `/admin/channels/pricing` | admin/ChannelsView | Channel price configuration | Admin / Billing / Pricing |
| `/admin/channels/monitor` | admin/ChannelMonitorView | Channel health | Admin / Requests / Capacity |
| `/admin/subscriptions` | admin/SubscriptionsView | Subscription administration | Admin / Billing / Entitlements |
| `/admin/accounts` | admin/AccountsView | Upstream account lifecycle | Admin / Resources / Connections |
| `/admin/plugins` | admin/PluginsView | Extensions | Admin / Settings / Extensions |
| `/admin/announcements` | admin/AnnouncementsView | Notices | Admin / Settings / Announcements |
| `/admin/proxies` | admin/ProxiesView | Egress proxies | Admin / Resources / Egress |
| `/admin/proxy-groups` | admin/ProxyGroupsView | Egress pools | Admin / Resources / Egress pools |
| `/admin/redeem` | admin/RedeemView | Credit codes | Admin / Billing / Credit codes |
| `/admin/promo-codes` | admin/PromoCodesView | Promotions | Admin / Billing / Promotions |
| `/admin/settings` | admin/SettingsView | Global configuration | Admin / Settings; split security/payment/deployment |
| `/admin/backup` | admin/BackupView | Backup and restoration | Admin / Settings / Recovery |
| `/admin/risk-control` | admin/RiskControlView | Risk policy | Admin / Policies / Risk |
| `/admin/prompt-audit` | features/prompt-audit/PromptAuditView | Content audit | Admin / Policies / Content |
| `/admin/usage` | admin/UsageView | Global usage | Admin / Requests + Billing |
| `/admin/affiliates` | redirect `/admin/affiliates/invites` | Referral entry | Admin / Billing / Referrals |
| `/admin/affiliates/invites` | admin/affiliates/AdminAffiliateInvitesView | Invitations | Admin / Billing / Referrals |
| `/admin/affiliates/rebates` | admin/affiliates/AdminAffiliateRebatesView | Rebates | Admin / Billing / Referrals |
| `/admin/affiliates/transfers` | admin/affiliates/AdminAffiliateTransfersView | Transfers | Admin / Billing / Referrals |
| `/admin/orders/dashboard` | admin/orders/AdminPaymentDashboardView | Payment metrics | Admin / Billing / Payments |
| `/admin/orders` | admin/orders/AdminOrdersView | Order operations | Admin / Billing / Orders |
| `/admin/orders/plans` | admin/orders/AdminPaymentPlansView | Plans | Admin / Billing / Plans |
| `/:pathMatch(.*)*` | NotFoundView | Unknown route | Shared / Not found |

Generated workspace routes are additional, real routes. They reuse the named pages but remove the global admin requirement and apply workspace membership/role checks:

| Route | Reused page | Purpose / proposed IA |
| --- | --- | --- |
| `/workspaces/:workspace_id/accounts` | admin/AccountsView | Workspace / Resources / Connections |
| `/workspaces/:workspace_id/groups` | admin/GroupsView | Workspace / Resources and Policies |
| `/workspaces/:workspace_id/proxies` | admin/ProxiesView | Workspace / Resources / Egress |
| `/workspaces/:workspace_id/proxy-groups` | admin/ProxyGroupsView | Workspace / Resources / Egress pools |
| `/workspaces/:workspace_id/channels/pricing` | admin/ChannelsView | Workspace / Billing / Pricing |
| `/workspaces/:workspace_id/channels/monitor` | admin/ChannelMonitorView | Workspace / Requests / Capacity |
| `/workspaces/:workspace_id/keys` | user/KeysView | Workspace / Access / Keys |
| `/workspaces/:workspace_id/monitor` | user/ChannelStatusView | Workspace / Requests / Health |

## Inconsistency and maintainability findings

- `style.css` defines five competing directions; `useThemeDirection.ts` exposes teal/control/slate/indigo/mono plus drawer and command state. Some directions replace amber with cyan, purple or green. The foundation introduces one `--oh-*` semantic palette, independent of those legacy choices. Migration owner must retire the old picker and aliases after all consuming pages move.
- Existing `components/common` already contains Input, Select, DataTable, EmptyState, Skeleton, StatusBadge, BaseDialog, OmniHiveDrawer and CommandPalette. These remain compatibility components, not a second recommended system. New pages import `components/ui`; migrate callers before deleting common implementations. Reuse tested dialog infrastructure rather than losing its modal stack behavior.
- Hard-coded gray/dark utility combinations, scoped page CSS, glass/glow shadows and global component classes compete for surface meaning. New primitives are `oh-*` scoped to prevent accidental overrides.
- CreateAccountModal is 7,036 lines; EditAccountModal 5,579; BulkEditAccountModal 2,273; GroupsView 5,418 at audit baseline. The problem is mixed provider, credential, price, routing and lifecycle responsibilities, not just file size.
- Price/model display logic belongs in shared domain projections, not in primitive components. The UI must not calculate settlement from JavaScript floating point.
- Locale directories already split English/Chinese domains and have completeness tests. Components accept labels through props/slots; consumers supply translated copy. A dev gallery may use English fixture copy, but fixtures must never enter shipping pages.

## Plan §11.6 consolidation map

| ID | Current seam | Migration contract |
| --- | --- | --- |
| UIR01 | Create/Edit parsePoolModeRetryStatusCodes, toggleErrorCode, buildTempUnschedRules, buildMixedChannelDetails | Extract pure rules with payload tests; share field groups; retain secret keep/replace/clear |
| UIR02 | GroupsView create/update price/routing serialization | Share normalization; explicit create vs patch (`null` vs `-1` clearing) |
| UIR03 | Accounts useTableLoader vs Groups/Usage local paging/cancellation | One authoritative scoped query controller per migrated page; retain unknown capacity |
| UIR04 | FeatureFlags plus sidebar/router conditionals | Shared route policy with enabled/disabled/unknown + reason; backend remains authoritative |
| UIR05 | API client/error normalization plus repeated cancellation checks | One client; cancelled/old-session responses stay silent; binary failures remain supported |
| UIR06 | Theme owns overlays; Header/Sidebar/store own branding | Theme preference only; shell overlay state; single BrandConfig |
| UIR07 | Regex model category inference; duplicate price unit/rate calculation | ModelDisplay/PricePresentation projections with evidence and price version |

## Brand and browser-state debt (D08)

`stores/app.ts` initializes and falls back to `Sub2API`; router title fallback, setup/auth/public/usage views and help copy contain further legacy strings. `router/index.ts`, README files and tests also mention the old identity. Distinguish visible copy from migration fixtures and required attribution; do not blind-replace.

`stores/auth.ts` reads/writes `auth_token`, `auth_user`, `refresh_token`, `token_expires_at`, `pending_auth_session`. `api/client.ts` directly uses those keys plus `auth_expired` and `ops_monitoring_enabled_cached`. This is a multi-tab refresh/session protocol, not a visual rename. The identity owner must provide versioned read-old/write-new handling and synchronized refresh-lock transition, test old/new tabs and signout, then remove compatibility on a stated date. New non-sensitive preferences use `omnihive.<domain>.v1`; secrets must not be persisted by the design library.

## Migration packages, ordered

1. **Shell + identity**: layout/Header/Sidebar, theme composables, route/nav registry, BrandConfig, auth page presentation. Coordinate session owner; no auth behavior rewrites in visual patches.
2. **Model discovery**: ModelPlazaView + model components + pricing/model display projections; detail/compare and public documentation links.
3. **Developer console**: user Dashboard/Keys/Usage, request detail and Playground feature folders; one scoped query owner per page.
4. **Connections**: AccountsView, account components and new provider/credential feature folders; replace giant modal with five-step deep-link flow.
5. **Pools and policies**: GroupsView, channels, routing feature; coordinate serializer contracts before splitting.
6. **Billing**: user payment/subscription/order/redeem + admin equivalents; preserve provider callbacks and exact amounts.
7. **Operations/settings**: Ops, audit, backup, proxy, plugin, announcements, identity settings; preserve all listed deep links.

Packages 2–3 and 4–6 can run in parallel after shared shell and primitive APIs settle, with separate file ownership. Each package needs 360/768/1280/1920 screenshots, light/dark, keyboard completion and live API state evidence before acceptance.
