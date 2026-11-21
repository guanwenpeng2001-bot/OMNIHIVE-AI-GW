# Tenant BYOK self-service

Apply expand migration 317 after 306. Existing pilot records default to the API-key class. Their member instance bindings, fees, grants, funding history and secret versions remain authoritative. The existing protected BYOK registration mounts the workspace and project endpoints in the byok-providers fragment. Compose the fragments and regenerate frontend types in the integration change.

## Review and rollout

1. Inspect `GET W/byok/provider-classes`. Each row describes a provider and authentication class from the connections registry. `supported` means a tenant credential adapter can attempt verification. `enabled` additionally requires a reviewed enrollment for the workspace. Custom endpoint profiles and modes lacking a verified discovery adapter stay unavailable.
2. Add an explicitly reviewed class with the transaction below. The existing provider row supplies the shared exact USD fee per BYOK attempt. An explicit class row overrides legacy API-key enrollment in both directions. An absent class row grants only the reviewed byok API-key lane.
3. Review pilot grant ownership and current member instances with the workspace owner. Apply an If-Match PATCH to opt the reviewed credential into current-writer fencing. Omitted member grants retain the previous membership instances across PATCH. Use explicit member IDs to authorize new instances. Import historical verify, rotate and revoke events using the adjacent idempotent SQL. Creation and policy history start when captured by byok-providers.
4. During a mixed-version rollout, drain byok processes before advertising self-service. Migration triggers fence old control writes and new paid or free permits for upgraded credentials, reviewed classes and dedicated service keys. Byok processes holding pre-cutover permits may complete. Preserve expanded storage for rollback. Disable class rows explicitly, drain processes and settle outstanding holds before rolling back. Removing a class override restores the older pilot enrollment rule.
5. byok's execution integration still applies. Build tenant candidates from the authorized scope using `Store.Eligible` and call `Gateway.Send` at the physical send boundary. Use the ephemeral credential fields and the committed permit inside the transport callback, whose context carries cancellation. Each paid attempt uses the wallet wallet reservation and frozen pricing price evidence. The aggregate inference adapter must supply this boundary before enabling production dispatch. The control routes are registered independently.

```sql
BEGIN;
INSERT INTO byok_pilot_providers(workspace_id,provider,enabled,service_fee_usd)
VALUES (:workspace_id,:provider,false,:approved_fee)
ON CONFLICT(workspace_id,provider) DO NOTHING;
INSERT INTO byok_provider_classes(workspace_id,provider,auth_type,enabled)
VALUES (:workspace_id,:provider,:auth_type,true)
ON CONFLICT(workspace_id,provider,auth_type) DO UPDATE SET enabled=excluded.enabled;
COMMIT;
```

## Credentials and funding

Submit native API keys, OAuth access tokens or Anthropic setup tokens through the existing write-only `secret` input. Reauthorization and rotation install a new verified version. User-supplied OAuth access tokens require replacement when the provider expires or revokes them. Service accounts accept a Google service-account JSON string. Verification exchanges the proposed signing key at Google's fixed token endpoint and checks each requested model using the publisher's token-count operation. Gemini uses the native default Vertex location and Anthropic uses the global endpoint. Bedrock accepts JSON containing `aws_region`, `aws_access_key_id`, `aws_secret_access_key` and optional `aws_session_token`. Its signed checks require model-specific authorization, entitlement, agreement and region availability. OpenAI-compatible classes use the default registered endpoint. A public catalog alone cannot prove authorization. Native verification evidence expires after 24 hours or an earlier declared JWT expiry. The credential status lists the granted models covered by current successful verification. [Google Gemini count tokens](https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/capabilities/get-token-count) · [Google Claude count tokens](https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/partner-models/claude/count-tokens)

The project route `PUT P/byok/credentials/service-accounts/{key_id}` designates an owned, membership-bound project key as a machine identity. The response gives the service account ID. Authorize this ID together with its key and project. A workspace administrator can grant the resulting machine identity across the selected credential's project list. Direct use always joins the machine's original project, key and sponsor membership. Retiring the designation permanently closes that key's machine grant. GET reads the designation and DELETE retires it. A new designation after retirement requires a new project key.

Use GET on the credential for current state, fingerprint, grants and version. PATCH accepts only the supplied grant and funding fields and requires a quoted revision. GET events returns metadata in ascending ID order. DELETE performs force revocation, using the same If-Match and idempotency semantics as POST revoke. Rotation and drain revocation let an already committed version finish. A force revoke, member exit, service-account retirement, class disablement or removal of the matching grant cancels participating transports on their next live check. The check runs every 250 ms and fails closed on database failure. Accepted upstream work retains its journal and settlement evidence.

Use the project estimate endpoint for the actual project, owner, key, model and token bounds. Render `byok_external_estimate` and `platform_service_fee` separately. Unknown external expense is null. The platform charges the service fee for the tenant lane. A fallback requires the credential's current policy, affirmative request consent, the approved candidate from the first attempt, the planner's current decision, a frozen wallet tariff and a live reservation. The sending identity, project and workspace are checked again at the permit boundary.

## Verification

Run `go build ./...`, `go vet ./...`, targeted unit tests and `go test -tags=integration -count=1 -v ./internal/control/byok` from backend with an isolated PostgreSQL database named `job_test` and Redis. The integration suite exercises the registered HTTP handler, principal and membership checks, encrypted vault, reservations and real policy planner. Supplier network responses come from a deterministic probe fixture.
