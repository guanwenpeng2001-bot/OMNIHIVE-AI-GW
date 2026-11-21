# T2 contract: onboarding and connection verification

Branch: `codex/v032-be-onboarding-probe`, based on `v0.3.1`.

## Connection checks

`POST /api/v2/workspaces/:workspace_id/connections/:id/verify` continues to return HTTP 202 with an operation containing the verification result. The verification snapshot and `last_verification.checks` use the same check format. Rotation verification uses it too.

```json
{"name":"network","state":"failed","reason":"URL_NOT_ALLOWED","detail":"Endpoint rejected by URL security policy. Use a valid HTTPS public endpoint; when the allowlist is enabled, configure SECURITY_URL_ALLOWLIST_UPSTREAM_HOSTS."}
```

`name`: `network`, `authorization`, or `discover_models`. `state`: `passed`, `failed`, `unknown`, or `unsupported`. `reason` remains a nullable string, including `null` on passed checks. New `detail` is an optional string (`omitempty`), omitted when empty. Treat detail as display text, never a machine discriminator. Diagnostics contain fixed safe text, never raw URLs, tokens, keys, transport errors, or upstream bodies.

| Reason | Meaning and states |
| --- | --- |
| `URL_NOT_ALLOWED` | Network failed: endpoint malformed, insecure HTTP disallowed, private address blocked, or hostname not allowlisted. Detail points to HTTPS/public policy and `SECURITY_URL_ALLOWLIST_UPSTREAM_HOSTS`. |
| `AUTH_REJECTED` | HTTP 401/403: network passed, authorization failed, discovery unknown. Also used for typed credential-build failures. |
| `UPSTREAM_RATE_LIMITED` | HTTP 429: network passed, authorization and discovery unknown. If only the anonymous authorization control was rate-limited, discovered models remain passed and authorization stays unknown. |
| `PROBE_FAILED` | Existing generic failure code: dial/TLS/timeout/proxy/redirect failure; non-success discovery HTTP status; invalid/oversized/unreadable catalog; failed request preparation. No false claim of unsupported discovery. |
| `EVIDENCE_UNAVAILABLE` | Existing fallback for unknown evidence without a more specific reason, including checks not attempted after another phase failed. |
| `CHECK_UNSUPPORTED` | Discovery unsupported only for typed `not_adapted`, `account_type`, `unsupported`, or `discovery_unconfirmed` builder errors. |
| `ACTIVE_MODEL_MISSING` | Existing rotation-only discovery failure when replacement credentials would remove an active model. |

UI guidance: show each check's state, reason and optional detail. A public unauthenticated model catalog still cannot establish credential validity. Activation eligibility still requires network and authorization to pass. Existing snapshots/prober implementations without new fields keep their previous fallback reason mapping.

## Workspace and project membership

All application user creation uses `userRepository.create`, including password registration, OAuth/SSO (GitHub/LinuxDo/OIDC/WeChat/DingTalk and email OAuth), and admin CreateUser. In the existing user transaction, insert membership in workspace **1**, where `is_default=true`:

- Global `admin`: membership role `admin`.
- Regular `user`: membership role `member`.
- Insert is idempotent with `ON CONFLICT(workspace_id,user_id) DO NOTHING`.
- No automatic `project_grants`, no personal workspace, and no new per-user project. Migration 290 already creates each workspace's Default project.
- Initial installer admin creation commits user, Default admin membership, and initial settings together.
- Membership insertion failure rolls back application user creation, including an outer invitation/signup transaction.

Migration **334_backfill_default_workspace_members.sql** follows the verified latest baseline migration 333. It backfills active, non-deleted `admin`/`user` accounts. It repairs existing admins' membership to `admin`, preserves existing regular users' explicit membership roles, and leaves grants untouched. Re-running causes no changes once complete, including no scope-version churn. There is deliberately no users INSERT trigger: outsider fixtures that use raw SQL after migrations still have no membership.

Regular membership grants exactly `projects.read` and `models.read` through the existing identity evaluator; the Default project becomes visible. Other projects still require explicit grants. Global administrators retain the existing administrator action set. Regular users cannot create a personal workspace: `WorkspaceService.Create` requires a global admin, and `WorkspacesView.vue` displays its create form only to admins. Existing owner-scoped v1 key APIs are retained.

## RBAC verification findings

Do **not** automatically grant project actions to ordinary members of the shared Default workspace/project:

| Surface | Verified scope in current implementation |
| --- | --- |
| `keys.read` | `modules/identity/*/store.go: Keys`: checks the grant, then lists every non-deleted key in the workspace/project. No `k.user_id=actor` predicate. Key creation does record the actor as owner, but does not make listing owner-scoped. |
| `requests.read` | `modules/requests/query.go`: base evidence query filters workspace/project/time, not the calling actor. Owner/funding joins establish evidence integrity, not viewer ownership. |
| `billing.read`, `billing.ledger.read` | `modules/billing/readmodel/store.go: filters`: workspace/project predicates; member/key are optional explicit filters, not automatic actor filters. Project-wide costs/ledger are visible with these grants. Quotas default to actor funding; another member requires the separate `billing.payers.read` grant in the handler. This does not make general billing reads private. |
| `tasks.*` | `modules/tasks/read.go`: task listing/detail/artifacts use workspace/project. Retry additionally checks owner, but task read is not owner-filtered. |
| `playground.*` | `modules/playground/store.go`: run lookup/cancel include `actor_id`; gateway key use checks ownership or explicit `keys.act_as`. These safeguards do not justify a broad shared-project default grant bundle. |
| `credentials.byok.manage` | `modules/byok/store.go` and `self_service.go`: visibility is workspace/project, not actor ownership. Never seed this grant on Default. |
| Administrative actions | No automatic `connections.*`, `pools.*`, `routing.*`, `billing.adjust`, `billing.reconcile.*`, `members.grants.write`, `ops.*`, `projects.edit`, or `projects.delete` for members. |

PostgreSQL regression tests assert the exact two-action member set, denial of sensitive actions, Default visibility, admin membership, no seeded project grants, migration idempotency, skipped inactive/deleted users, preserved explicit roles, and unchanged raw-SQL outsider behavior. Existing control identity/policies/billing/requests/connections integration suites also pass.
