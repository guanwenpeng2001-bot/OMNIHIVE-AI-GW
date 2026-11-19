# WP6 request diagnostics and billing

Register these entries with the WP1 router. Each view already mounts `AppLayout` and consumes the active workspace/project. Retain the existing authentication, payment, subscription and simple-mode guards on commerce routes.

| Path | Route name | Lazy component under `frontend/src/features` | Shell and guard |
| --- | --- | --- | --- |
| `/requests` | `ProjectRequests` | `requests/RequestListView.vue` | User, authenticated |
| `/requests/:id` | `ProjectRequest` | `requests/RequestDetailView.vue` | User, authenticated |
| `/admin/requests` | `AdminRequests` | `requests/RequestListView.vue` | Admin, authenticated administrator |
| `/admin/requests/:id` | `AdminRequest` | `requests/RequestDetailView.vue` | Admin, authenticated administrator |
| `/billing` | `ProjectBilling` | `billing/UserCostsView.vue` | User, authenticated |
| `/admin/billing` | `AdminBilling` | `billing/ReconciliationView.vue` | Admin, authenticated administrator |

Use `wp6.requests.title`, `wp6.requests.detail`, `wp6.billing.title` and `wp6.reconciliation.title` for route metadata. Register the four list/workbench entries in the WP1 navigation registry under Requests and Billing. Preserve `/usage` and `/admin/usage` for existing links. The shared billing navigation uses the `ProjectBilling` route name and the WP1 visibility rules for `/purchase`, `/subscriptions`, `/orders`, `/redeem` and `/affiliate`.

## Data and permissions

- WP6 uses the generated BE-01/14/16 types and the shared session client. Its v2 adapter serializes the exact query to accommodate endpoints with strict parameter lists. The WP1 scope loader also needs exact serialization for the BE-01 scopes endpoint. The WP6 project picker reads that endpoint directly and updates the WP1 scope selection.
- Deep links carry opaque `workspace_id` and `project_id` query values. The active project controls all reads and writes. Scope and session changes clear in-memory results and cancel outstanding requests. Request identity and scope are verified before rendering the detail.
- Request detail honors `requests.read`, `connections.read` and `billing.ledger.read`. It labels estimates and missing milestones with their BE-14 evidence semantics. A historical request may have an observation timestamp and no recorded admission or route trace.
- The cost workbench uses `billing.read` for activity, quota and charges, and `billing.ledger.read` for wallet entries. The quota view keeps each allowance and currency separate. Date fields describe an inclusive start day and exclusive end day in the chosen IANA timezone.
- CSV registration uses `billing.export` and sends the selected date window, timezone and currency. The BE-16 CSV input contains no model, key, funding-source or settlement filter. Downloads use the scoped operation result. A 410 offers a fresh registration and a lost response retains the original idempotency key for manual recovery.
- Reconciliation requests use the selected project within the workspace. Evidence attachment uses the case version and `billing.reconcile.write`. A posted adjustment cites an original USD ledger entry, requires a signed decimal amount, a reason and session step-up, and displays the returned new ledger entry. The backend remains the authority for grants and effects.

## Verification

The English and Chinese dictionaries share the `wp6` namespace. The four feature test files cover waterfall evidence and masking, half-open time and DST, cursor invalidation, exact quota and currencies, and CSV idempotency/expiry. The available 1440 CSS pixel dark request detail capture uses the WP1 shell and BE-14 fixtures. The 360/1440 light/dark matrix still needs remote capture. See [screens/wp6](screens/wp6).
