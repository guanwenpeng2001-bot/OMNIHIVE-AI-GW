# Connections integration

## Server composition

Add `internal/control/connections` as `connectionshttp` and `internal/modules/connections` as `connections`. Include `connectionshttp.ProviderSet` next to `identityhttp.ProviderSet` in the server Wire injector. It reuses the existing PostgreSQL database, `identity.AccessEvaluator`, persistent `service.SecretEncryptor`, configuration and `*service.AccountTestService`. Register the operation reader in the existing constructor.

```go
func provideIdentityOperations(store *connections.Store) identity.Operations {
    return identity.Operations{Readers: []identity.OperationReader{store}}
}
```

Retain readers added by other packages. Pass `*connectionshttp.Handler` alongside the identity handler through the router provider, `SetupRouter` and `registerRoutes`. Use these calls adjacent to the identity registration with the same JWT, panel rate limit and audit middleware.

```go
connectionshttp.Register(r.Group("/api/v2"), connectionsHandler,
    gin.HandlerFunc(jwtAuth), panelRateLimiter.Global(), gin.HandlerFunc(auditLog))
connectionshttp.RegisterAuthorization(r.Group("/api/v1"), connectionsHandler,
    gin.HandlerFunc(jwtAuth), panelRateLimiter.Global(), gin.HandlerFunc(auditLog))
```

Add these route keys to the existing `auditBodyOmittedRoutes` in `backend/internal/server/middleware/audit_log.go` before mounting the handlers. Audits still record actor, operation, success or failure and request ID. Omitting the complete body also covers malformed or unrecognized credential fields.

```go
"PUT /api/v2/workspaces/:workspace_id/connections/:id/credential": {},
"POST /api/v2/workspaces/:workspace_id/connections/:id/rotations": {},
"PUT /api/v1/workspaces/:workspace_id/connections/:id/authorization": {},
```

From `backend`, run `go generate ./cmd/server` and `go run ./api/openapi/compose`. From the repository root, run `npm exec --yes --package=openapi-typescript@7.10.1 -- openapi-typescript backend/api/v2/openapi.yaml -o frontend/src/api/generated/v2.d.ts`. Build both ordinary and embedded servers and run the production-route smoke below.

## Database and rollout

1. Apply migration 293 with the normal migration runner after identity migration 290. It snapshots existing encrypted references without changing IDs, workspace ownership or serving credentials. The insert/update trigger captures subsequent v1 refreshes, rotations and revocations in the same transaction. Newly created v2 connections receive a disabled legacy account with matching ID. Native eligibility changes at confirmation.
2. Use the application's ordinary database configuration and explicitly configured persistent TOTP encryption key. Run `go run ./internal/legacy/connections/cmd/backfill` from `backend` with the deployed configuration. Each locked account is its own transaction. New inline references are installed only after encrypting and decrypting the owner-, account-, reference- and version-bound payload and comparing both JSON documents. Existing references are verified with their original identity. Failed conversions preserve the old account and quarantine a stable reason. Repeat the command during mixed-version operation. Previously mapped accounts changed by legacy configuration writers re-enter review.
3. Let every native scheduler consume the conversion outbox through the last converted account and retire any pre-conversion inline cache entries before enabling v2 rotation for migrated accounts. Inspect coverage and quarantine before opening the inventory. Changes to the legacy row re-enable review. After repairing a key configuration issue, remove only the corresponding reviewed quarantine IDs and rerun. Keep serving established v1 accounts until their configurations can be represented safely. The adapter quarantines complex model aliases, proxy groups and unsupported legacy transport options.

```sql
SELECT count(*) FILTER (WHERE c.account_id IS NOT NULL AND NOT c.legacy_dirty) AS mapped,
       count(*) FILTER (WHERE q.account_id IS NOT NULL) AS quarantined,
       count(*) FILTER (WHERE (c.account_id IS NULL OR c.legacy_dirty) AND q.account_id IS NULL) AS pending
FROM accounts a
LEFT JOIN provider_connections c ON c.account_id=a.id AND c.workspace_id=a.workspace_id
LEFT JOIN connection_migration_quarantine q ON q.account_id=a.id
WHERE a.deleted_at IS NULL;

SELECT reason,count(*) FROM connection_migration_quarantine GROUP BY reason ORDER BY reason;
```

4. Preserve the expanded tables, trigger and encryption key for binary rollback. Stop v2 enrollment first. Existing gateway instances continue to read account IDs and credential references. retirement owns removal of legacy secret writes after legacy writers and leases drain. Audit history and old encrypted versions remain available for retention policy. Dispatch and verification resolve only the current unrevoked version.

## Wizard contract

Use workspace `provider-schemas` to select a provider, auth type and schema version. Forms use the fixed component names in the schema. An unspecified network policy uses direct egress. A nonempty `network_policy_id` identifies an active, unexpired proxy owned by the same workspace. The v2 wizard currently supports individual proxy nodes. The native discovery adapter selects the protocol-specific upstream URL and headers. Checks use HTTPS, the configured host policy, resolved-IP enforcement and a no-redirect HTTP client. An anonymous control distinguishes protected catalogs from public listings. A public catalog without separate authorization evidence yields unknown authorization. Unsupported provider/auth discovery combinations remain ineligible for v2 activation. Existing native gateway processing and v1 tests continue to support their established combinations.

1. Create a named draft with `POST connections`. `GET connections/{id}` returns its version.
2. Set, keep or clear the credential explicitly. Set on a draft disables dispatch while it awaits verification. Clear revokes the current local secret and disables dispatch. Rotation is the live replacement path. Credential fingerprints identify a stored encrypted version. The suffix is nullable for historical records and for non-API-key credentials.
3. For OAuth, start the schema-selected v1 workspace authorization workflow. Its existing exchange returns an opaque credential draft, revision and expiry, bound to the actor, workspace and HttpOnly browser proof. Substitute the connection ID into the schema's `apply` URL and PUT `{"credential_draft":"…","revision":"…"}` from the same browser. That URL shares the original cookie path and rechecks identity connection-edit and credential-write permissions. Ticket consumption, plaintext removal from the pending ticket, encryption and installation are atomic. For API keys or manually entered service-account JSON, submit the write-only secret directly to the credential route.
4. Verify with network, authorization and optional model discovery. Read the returned operation through the workspace operation URL. The bounded probe runs inline and can already be terminal on the 202 response. A process failure before commit rolls back the receipt and connection transition. Replaying a committed request returns the same operation. Review the discovered models, select models, pools and limits, then POST a preview. Include an empty list to clear an assignment. Limits and model choices are rechecked per connection. Verification expires after 24 hours and previews after five minutes.
5. Activate the confirmed preview and verification with the credential version and the quoted connection version. Changed credentials, proxy revision, connection revision, grant version, verification or expired previews require fresh evidence. Name and draft edits go through PATCH. Endpoint and proxy changes require clearing the previous credential first. An active connection with an empty model selection has native scheduling disabled.

All POST commands and the v1 authorization bridge require `Idempotency-Key`. Use `If-Match: "<connection version>"` on mutations addressing an existing connection. Each bulk item carries its base version and verification. Applications may select a subset. The operation result reports each applied, blocked or stale item. Updates to disjoint rows can succeed in a mixed batch. Operation progress for a successful local rotation records validating, switching, draining and revoked. Existing requests holding the old material finish independently. Provider-side revocation is outside this operation.

## Tests and production smoke

Use a dedicated, migrated `job_test` PostgreSQL database and isolated Redis with `UPSTREAM_JOB_TEST_DSN` and `AFFINITY_REDIS_TEST_ADDR`. Run storage tests sequentially. Several existing repository tests use database-wide notifications.

```sh
go test -p 1 -tags=integration -count=1 ./internal/legacy/connections/migrate ./internal/control/connections
CREDENTIAL_SECRET_TEST_DSN="$UPSTREAM_JOB_TEST_DSN" go test -count=1 -run '^TestCredentialSecret' ./internal/repository
go test -tags=unit ./internal/modules/connections ./internal/modules/credentials
go test -tags=unit -run 'Test(AccountTestService|ConnectionProbe|CredentialSecret)' ./internal/service ./internal/handler/admin
go test -p 1 ./internal/service/... ./internal/handler/... ./internal/repository/...
```

The smoke script starts an ephemeral OpenAI-compatible HTTPS fixture bound to loopback and generates disposable upstream credentials in memory. Before starting the isolated server, create a fixture certificate and key.

```sh
fixture_dir=$(mktemp -d)
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj '/CN=localhost' \
  -addext 'subjectAltName=IP:127.0.0.1,DNS:localhost' \
  -keyout "$fixture_dir/fixture.key" -out "$fixture_dir/fixture.pem"
export SMOKE_TLS_CERT="$fixture_dir/fixture.pem" SMOKE_TLS_KEY="$fixture_dir/fixture.key"
export SSL_CERT_FILE="$SMOKE_TLS_CERT"
```

For the isolated smoke server, enable the upstream URL allowlist with `127.0.0.1` in `upstream_hosts`, enable `allow_private_hosts`, and set a persistent TOTP encryption key. Use the standard-mode admin test database, Redis and bootstrap setup from identity. With that running server's `SMOKE_BASE_URL`, `SMOKE_ADMIN_EMAIL` and `SMOKE_ADMIN_PASSWORD`, run `contracts/migrations/connections/smoke.sh` from the repository root. The script exercises session validation, replay, pool creation, set, verify, operation lookup, preview, activate, rotate, revoke and lifecycle history. Inspect the three credential-bearing audit route entries and confirm their request bodies were omitted.

## Dependent packages

- catalog should ingest the scoped eligible connection verification snapshot and its observed and expiry times. Provider action descriptors are static gateway bounds. A public catalog must project only independently publishable evidence.
- routing should join active `connection_pool_members`, `connection_pools` and clean active `provider_connections` on workspace and account ID, and enforce pool concurrency alongside native account eligibility. Draft `pool_ids` describe the next confirmation. Billing/routing groups retain their existing independent semantics.
- byok can consume `credentials.Resolve` after checking its own tenant, project, key and member grants. Resolve again after any dispatch delay with the confirmed credential version. BYOK binding, funding consent, external cost estimates and provider-side revocation remain separate responsibilities.
- The frontend uses `contracts/api/connections/`, the generated OpenAPI operations and the schema registry. Keep authorization references and plaintext out of query keys, draft autosave and logs. Show unsupported or unknown checks explicitly and preserve the last successful quota observation when the source provides one.
