# WP4 Integration Guide — Upstream Connection Wizard & Egress Governance

## 1. Router Registration Snippet

After WP1 merges, add these routes inside the `/admin` children block in `frontend/src/router/index.ts` (under `meta: { requiresAuth: true, requiresAdmin: true }`):

```ts
{
  path: 'connections/new',
  name: 'AdminConnectionNew',
  component: () => import('@/views/admin/connections/ConnectionWizardView.vue'),
  meta: {
    title: 'New Upstream Connection',
    titleKey: 'admin.connections.newTitle'
  }
},
{
  path: 'connections/:id/edit',
  name: 'AdminConnectionEdit',
  component: () => import('@/views/admin/connections/ConnectionWizardView.vue'),
  meta: {
    title: 'Edit Upstream Connection',
    titleKey: 'admin.connections.editTitle'
  }
}
```

Existing routes (`/admin/accounts`, `/admin/proxies`, `/admin/proxy-groups`) remain unchanged and continue to work with both the new v2 flows (Wizard link, Quick-Edit Drawer, Bulk Diff Modal, BE-04 Pools panel) and legacy modal fallbacks.

## 2. Legacy Gap List (BE-04 v2 vs. Legacy Modals)

The legacy `CreateAccountModal.vue`, `EditAccountModal.vue`, and `BulkEditAccountModal.vue` remain reachable from `/admin/accounts` for capabilities not yet modeled in BE-04 v2 `ConnectionCreate` / `ConnectionPatch` / `ConnectionSchema`:

- **Platform-specific OAuth browser/device/cookie enrollment helpers**: Claude setup-token, Antigravity cookie/OAuth, Cursor browser authorization receipt, Grok SSO/RT batch import, and Qwen/iFlow legacy token exchange remain in legacy modals (BE-04 v2 provides generic `PUT /v1/workspaces/{workspace}/connections/{connection}/authorization` with `code`).
- **Legacy model mapping & passthrough billing toggles**: OpenAI long-context billing, compact model mapping, OpenAI Responses WS mode (`http_bridge`), `codex_cli_only`, and Antigravity identity/alias catalog presets.
- **Temp unschedulable keyword rules & error passthrough rules**: `temp_unschedulable_rules` and `custom_error_codes` are extracted as pure UIR01 rules in `features/connections/rules.ts`, but BE-04 v2 `ConnectionLimits` currently exposes `concurrency` and `daily_cost_limit` rather than per-keyword cooldown rules.
- **Mixed-channel conflict dialog (`CheckMixedChannelResponse`)**: `buildMixedChannelDetails` is extracted into `features/connections/rules.ts` and shared with legacy modals; BE-04 v2 handles conflicts via HTTP `409` and bulk preview `conflict` items.
- **TLS fingerprint profile binding & Spark shadow account creation**: Remain on the legacy `/admin/accounts` action menu.
