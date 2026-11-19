# WP3 Integration Guide — Developer Console (Overview, Keys, Playground, Task Center, Workspaces)

## 1. Router Registration Snippet (`frontend/src/router/index.ts`)

Per file ownership rules, `fe-wp3` does not modify `frontend/src/router/**` or `frontend/src/components/layout/**`. When composing `codex/fe-wp3-console` with WP1's shell on `codex/refactor`, register the new BE-13 Playground and BE-15 Task Center user console routes in `frontend/src/router/index.ts`:

```ts
{
  path: '/playground',
  name: 'Playground',
  component: () => import('@/views/user/PlaygroundView.vue'),
  meta: {
    requiresAuth: true,
    requiresAdmin: false,
    shellTier: 'user',
    navGroup: 'MODELS',
    title: 'Playground',
    titleKey: 'playgroundConsole.title',
    descriptionKey: 'playgroundConsole.description'
  }
},
{
  path: '/tasks',
  name: 'TaskCenter',
  component: () => import('@/views/user/TaskCenterView.vue'),
  meta: {
    requiresAuth: true,
    requiresAdmin: false,
    shellTier: 'user',
    navGroup: 'REQUESTS & TASKS',
    title: 'Task Center',
    titleKey: 'taskCenter.title',
    descriptionKey: 'taskCenter.description'
  }
}
```

Existing user console routes remain in place and now point to the upgraded WP3 views:
- `/dashboard` → `frontend/src/views/user/DashboardView.vue` (Overview: usage, spend, BE-14 observed success rate, recent requests, explicit empty/stale/conflict states, typed `<Drawer>` replacing legacy raw-HTML `useThemeDirection` drawer).
- `/keys` (and `/workspaces/:workspaceId/keys`) → `frontend/src/views/user/KeysView.vue` (one-time secret display + copy status, zero plaintext caching after dialog dismissal/unmount, edit, disable, rotate, period budget, D08 dual-read column storage `omnihive.keys.hidden_columns.v1`).
- `/workspaces` → `frontend/src/views/user/WorkspacesView.vue` (BE-01 projects list/create/rename/archive with `If-Match`, active workspace/project scope switching via `omnihive.console.scope.v1`, and workspace membership delegation).

## 2. Shared Scope Storage & Event Contract (with WP1 ScopeSwitcher)

`frontend/src/features/console/scope.ts` coordinates workspace and project selection with WP1's `ScopeSwitcher` without editing `stores/` or `components/layout/`:

- **Storage keys:**
  - `omnihive.console.scope.v1` (`{ mode: 'personal' | 'workspace', workspaceId: string | null, projectId: string | null }`)
  - `omnihive.workspace_id.v1` (string numeric ID, shared with WP4 `resolveActiveWorkspaceId`)
  - `omnihive.project_id.v1` (string numeric ID)
- **DOM CustomEvent:** `window.dispatchEvent(new CustomEvent('omnihive:scope-changed', { detail: selection }))`

## 3. Known Gaps & Backend Rollout Dependencies

1. **Legacy `/batch-image` guide (`BatchImageGuideView.vue`):** Retained at `/batch-image` for legacy CSV/JSONL batch upload templates (`PREVIEW_CACHE_DB_NAME` migrated to `omnihive.batch_image_preview_cache.v1`); new asynchronous image tracking, scope-bound 1-minute download tickets (`X-Artifact-Ticket`), revocation, and paid sub-item retries live in `/tasks` (`TaskCenterView.vue` bound to BE-15).
2. **Audio / Video async task creation (BE-25):** Per `BE-15.yaml` (`kind: const 'image'`) and `contracts/migrations/BE-15/INTEGRATION.md`, BE-15 currently admits `kind: "image"` only; audio/video task types await BE-25 adapter and funding verification.
3. **Playground catalog unit-rate availability:** When a model in `/api/v2/workspaces/{ws}/projects/{proj}/models` omits static `pricing.rates` (or before BE-05 catalog pricing composition is populated), `PlaygroundCostConfirmModal` renders `—` (never `$0.00`) and informs the operator that BE-13 gateway admission enforces the active price version and output token bound.
4. **Key management v1/v2 bridge:** Full key table filters, IP allow/deny lists, model access rules, and period USD budgets continue to bind `/api/v1/keys` (`keysAPI`), while BE-13 Playground also reads BE-01 `/api/v2/workspaces/{ws}/projects/{proj}/keys` with automatic fallback to `/api/v1/keys` so keys work across personal and workspace-project scopes during migration.
