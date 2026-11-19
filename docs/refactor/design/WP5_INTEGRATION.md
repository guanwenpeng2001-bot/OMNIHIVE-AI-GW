# WP5 routing integration

## Entry points

The existing `/admin/groups` and `/workspaces/:workspace_id/groups` routes open the resource pool list. The three tabs expose legacy groups, model access groups with a link to channel pricing, and the versioned routing workbench. Legacy group editing continues to use existing APIs for membership, billing, model rules, scheduling, fallback groups, and composite targets. BE-04 connection pools are a distinct resource. The v2 policy candidate editor uses BE-04 pool IDs.

The integration coordinator can register these additional routes using `GroupsView.vue` as the page component and the existing identity-scoped loader.

| Route | Page props | Access |
| --- | --- | --- |
| `/admin/pools` | `{ initialSection: 'pools' }` | Existing `AdminGroups` admin guard |
| `/admin/routing` | `{ initialSection: 'routing' }` | Admin console guard and BE-10 scoped authorization |
| `/workspaces/:workspace_id/routing` | `{ initialSection: 'routing' }` | Signed-in workspace route with BE-10 authorization for the selected scope. Preserve access for project-only grantees. |

Set navigation title keys to `routing.pool.title` and `routing.title`. Add the links to the shell navigation registry during integration. Retain established group paths and `/admin/channels/pricing`. The workspace pricing link follows the router's existing `/workspaces/:workspace_id/channels/pricing` path. The layout and route registry changes are coordinator-owned.

## Backend bindings

- BE-01 scope discovery supplies workspace and project IDs as strings. The key browser uses project key metadata. Entry by numeric ID also supports operators with routing access when key listing is unavailable.
- BE-10's exact scope filters select one lineage. Creation uses an idempotency key, draft updates send the quoted ETag, and publish or rollback sends both. Conflict handling fetches the current server head, then offers its version or reapplies edited fields. Lost acknowledgements require explicit readback before another write.
- Workspace and project ancestor definitions contribute known restrictions when their active definitions are available. An ancestor with a pending draft has an unknown active definition in the editor. Simulation evidence provides the server's constraint origins. Platform writes use the BE-10 deployment bootstrap.
- Simulation binds a saved revision, scenario, seed and frozen snapshot. Its proof expires at the server time. The view displays ordered candidates, up to three reasons per candidate, uncertainty and any affinity factor supplied by the trace. Missing evidence is shown as an em dash. Simulation and publication impact come from the server.
- A fresh simulation supplies the candidate count before saving ranking-only edits with matching constraints and scenario. For an unsaved definition with changed hard constraints or a new scenario, the count is unknown until a saved version can be simulated. The current API has no unsaved-draft estimator. Rollback impact is available in the committed response.
- Successful simulation depends on an authorized BE-08/10 snapshot producer serving the exact scenario. Expired or unavailable snapshots are handled as stale evidence. Refresh the graph using the producer described in the backend integration contract. [BE-10 integration](../../../contracts/migrations/BE-10/INTEGRATION.md)

## Async states and screenshots

| State | Surface |
| --- | --- |
| Initial load and background refresh | Scope and policy skeletons, then a refresh indicator with existing data retained |
| Empty and zero-filter match | Scoped draft creation and a separate version-filter state |
| Partial error and permission denied | Independent key, project, simulation, history and access errors |
| Stale evidence | Last synced policy or expired snapshot, with disabled dependent mutations |
| Conflict | HTTP 409 or 412 readback with server and local fields |
| Confirmed save | Persistent version confirmation and publication impact |
| Unknown outcome | Readback and review before another write |

The Playwright captures use the real app route and BE-10-shaped HTTP fixtures. The fixture disables the optional subscription badge. With the badge enabled, the shared WP1 header overflowed at 360 px. The WP5 workbench itself fit the viewport in both themes.

- [1440 px dark](screens/wp5/wp5-routing-dark-1440.png)
- [1440 px light](screens/wp5/wp5-routing-light-1440.png)
- [360 px dark](screens/wp5/wp5-routing-dark-360.png)
- [360 px light](screens/wp5/wp5-routing-light-360.png)
