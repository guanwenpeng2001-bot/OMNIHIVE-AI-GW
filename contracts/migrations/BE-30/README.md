# BE-30 identity compatibility inventory

The B0 identity migration targets two stable releases, roughly 90 days after the first independent stable release. Confirm the cutover date and supported deployment inventory before starting that window. Keep each compatibility path until its owner reconciles client and deployment versions, verifies backup and restore where applicable, and rehearses the rollback. Source counts below describe in-repository entry points. Deployed caller counts are unknown until measured.

## Protocol retirement candidates

| Legacy name | Owner | Current contract and caller count | Retirement gate |
| --- | --- | --- | --- |
| `sub2api-admin` | FE-WP1, BE-30 | One first-party console connector, `subscribeQPS`, and one server route, `/api/v1/admin/ops/ws/qps`. The console offers `omnihive-admin` first and `sub2api-admin` second for old servers. The current server negotiates the preferred offered name and strips the ticket before the upgrade. | Publish support for the new name and maintain dual acceptance for the B0 window. Count successful upgrades by selected protocol, without collecting tickets. Retire the fallback after the old servers and all supported legacy-only clients drain. |
| `X-Sub2API-Usage-Source` | BE-12, BE-30 | Two Cursor non-streaming writer paths share one emitter and return the same value in both this header and `X-OmniHive-Usage-Source`. Zero in-repository production readers. Downstream SDKs, proxies, and monitoring configuration may read the legacy header. Streaming keeps the SSE marker. | Readers prefer the OmniHive header and fall back to the legacy header. Keep dual emission for the B0 window and verify external client upgrades before removal. Server emission telemetry alone cannot establish which header a caller reads. |
| `/v1/sub2api/billing`, `/sub2api/billing` and `sub2api.key_billing` | BE-12, BE-30 | Two inbound route aliases and one outgoing relay probe still use this path and discriminator. Absent a relay upgrade, the probe needs to keep reading the legacy response. | Publish a versioned replacement, dual-read discriminators, and inventory supported relay versions before retiring either route. Retain interoperability beyond the B0 window while supported relay callers remain. |
| `X-Sub2API-Grok-Client-Tool-Cache`, `python__sub2api`, Google `ErrorInfo.domain` values `sub2api` and `sub2api.securityaudit` | BE-12, BE-21, BE-30 | Client cache opt-in, replayed Codex tool names and machine-readable error identities. Older clients and saved transcripts may depend on these names. | Publish a versioned mapping. Accept both opt-in names and both tool aliases through the B0 window. Switch error domains only under a negotiated client contract. Keep legacy replay support for retained transcripts. |
| Grok OAuth referrer and User-Agent | BE-12 | One xAI authorization builder sends the old referrer and two client paths send the old User-Agent. Vendor-side usage is unknown. | Verify both names against xAI before any producer change, then retire the old value after the B0 window and vendor compatibility sign-off. |
| Account export `sub2api-data` / `sub2api-bundle` | FE-WP4, BE-22, BE-30 | Shared by the exporter, importer, and safe-import documentation. Existing archives keep their historical discriminators. | Read both formats while writing an explicitly versioned replacement. Drain old importers during the B0 window. Keep archive imports until recovery coverage permits retirement. |
| Legacy compliance acknowledgment phrase | FE-WP1, BE-21 | An exact legacy phrase is accepted as an alias during acknowledgment. Stored legal and settings defaults can also contain the historical name. | Publish current copy, preserve acknowledgment records and authored clauses, and retain legacy-input compatibility for the B0 window and until older bundles drain. |

## Persisted, cryptographic and deployment identities

| Namespace or persisted value | Owner | Migration strategy and window |
| --- | --- | --- |
| `database.dbname=sub2api` | BE-22, BE-30 | Wizard, installer auto-setup and shipped Compose samples use `omnihive` on new installs. `DATABASE_DBNAME` overrides the config file in the usual order. An existing config, install lock, or installer bypass with the name omitted keeps the legacy default. Pin the actual name before relocating configuration or changing Compose templates. Keep explicit legacy DB names valid indefinitely. Retire implicit fallback only after all supported installs pin the name and restore verification passes. |
| `sub2api:prompt_guard:config:invalidate`, `sub2api:prompt_audit:payload:`, `SUB2API_PROMPT_AUDIT_PRIORITY_END` | BE-21, BE-30 | Add versioned payload decoding and dual reads before switching writers. Keep invalidations visible on both channels during a mixed-version rollout. Drain old workers and finish reading or expiring old payloads before removing the old namespace after the B0 window. |
| `sub2api/live-attestation/v1` | BE-12, BE-30 | Version the domain. Verify both signatures before enabling new signing, then permit new signatures only for capable verifiers. Preserve old verification through the B0 window and until the final v1 attestation expires. |
| `sub2api:codex-install-id:v2:`, `sub2api:codex-session-id:v2:`, `sub2api:codex-thread-id:v2:`, `sub2api:codex-account-identity:`, `sub2api:codex-call-id:v1:`, `sub2api:codex-compact-probe:v1:` | BE-12, BE-30 | Version and compare both derivations during migration. Keep one pinned version for existing accounts, affinities and transcripts. Dual-read persisted mappings. Sign or derive new identities under the new domain only after compatible readers are deployed. Keep old versions through the B0 window and the longest retained replay or affinity lifetime. |
| `sub2api:grok-safety-id:v1:` | BE-12, BE-30 | Add a versioned derivation, compare outputs and pin existing sessions to their original version. Coordinate using one stable upstream ID per session. Drain legacy sessions after the B0 window and confirm upstream acceptance before retirement. |
| `sub2api.reasoning-continuation` | BE-12, BE-30 | Version the continuation envelope, dual-read and verify before minting new tokens. Keep the legacy decoder through the B0 window and until retained transcripts have migrated or aged out. |
| `urn:sub2api:identity-ticket` | BE-01, BE-30 | Accept both provider keys under the same single-use store and scope checks before issuing new tickets. Wait through the B0 window and maximum ticket TTL after old binaries drain. |
| `sub2api/payment/encryption` and historical `sub2api-payment-resume` signatures | BE-19, BE-30 | New version reads both encryption versions and verifies both signature versions, then explicitly reseals stored secrets and issues new signed resume data. Preserve old decryption and verification through migration, the B0 window and the last supported rollback. |
| `sub2api:duplicate_operation_id`, dashboard Redis `sub2api:`, PostgreSQL `sub2api.usage_retention` | BE-18, BE-22, BE-30 | Version and read both old/new metadata or cache keys without splitting uniqueness, deduplication, or coordination. Preserve the SQL GUC for existing triggers until a new migration and dual-version deploy pass. Contract after the B0 window, outstanding operations, and supported rollback all clear. Existing migration files stay immutable. |
| Aliyun credential validation marker and upstream request fixtures | BE-04, BE-12 | Migrate a serialized marker under a versioned parser if it is still needed. Treat caller-supplied legacy model inputs, path prefixes, role data and RP origins as valid data. External relying-party identities need explicit user-side migration and recovery before any retirement. |
| Browser locale, login acknowledgment, refresh lock, IP geolocation, preview IndexedDB and duplicate/bulk operation keys | FE-WP1 (D08) | Use the D08 versioned adapter and preserve mixed-tab refresh coordination. Start writing new state after dual-read testing. Remove old reads after the B0 window, old tabs and pending operations drain. |
| Legacy service paths, binary/archive names, app volume/network identities, and deployment environment aliases | B0, BE-24, BE-30 | Preserve identity or explicitly bind the old physical resource for existing deployments. See [deployment identity migration](../../../docs/refactor/IDENTITY_MIGRATION.md) for fallback precedence. Cut over after verifying asset provenance, inventory, rollback and the B0 window. External explicitly named volumes remain supported. |
| Legacy test-only flags and fixture resource IDs | B0, BE-24 | Keep CI/harness aliases paired while the test callers migrate. Test prefix and database values represent isolated or explicit legacy fixtures. Remove flags after migration of supported CI and local harness callers. |

### Frontend D08 keys

| Owner area | Key |
| --- | --- |
| IP geolocation | `sub2api:ip-geo-cache:v1` |
| Batch image preview | IndexedDB `sub2api-batch-image-preview-cache` |
| Admin duplicate account | `sub2api:admin:account-duplicate:<account ID>` |
| Admin duplicate group | `sub2api:admin:group-duplicate:<admin ID>:<group ID>` |
| Admin duplicate channel monitor | `sub2api:admin:channel-monitor-duplicate:<admin ID>:<monitor ID>` |
| Subscription bulk operation | `sub2api:admin:subscription-bulk:<admin ID>:<serialized request>` |
| Locale | `sub2api_locale` |
| Login/register acknowledgment | `sub2api_login_agreement_consent` |
| Auth refresh lock | `sub2api-auth-token-refresh` |

## BE-20 plugin handoff

| v1 surface | Identity |
| --- | --- |
| Built-in package and trusted signer | `local.sub2api.openai-transport`, `sub2api-openai-transport-v1` |
| Manifest and frontend compatibility fields | `requires.sub2api`, `recommended_sub2api_version`, `tested_sub2api_versions`, `current_sub2api_version`, `required_sub2api_version` |
| Host and UI session | `sub2api:plugin-ui:v1:`, `sub2api-plugin-ui`, `sub2api-plugin-host`, `sub2api.plugin.ready` |
| SDK and package | `sub2api.plugin.v1` protobuf/gRPC, v1 handshake/magic, and `.s2plugin` |
| Stored plugin state | `sub2api_plugin_installations`, `sub2api_plugin_bindings` and their SQL constraints and indexes |

BE-20 verifies the original archive, manifest bytes, signer, and capabilities before importing. Map compatible host ranges and plugin IDs into the v2 schema. Rebuild and re-sign with an authorized publisher and record old/new digests, signer IDs, and capability grants. Ship trusted key metadata through the existing trusted release chain or explicit operator verification. Test the v2 host and bridge against both package inventories. Keep v1 verification and rollback mappings through the B0 window, until old packages and runtime callers drain. Preserve v1 signing material and immutable migrations under their original identities.

The v1 host accepts the new `local.omnihive.openai-transport` and `omnihive-openai-transport-v1` pair against the pinned public key and continues to accept the corresponding legacy pair. Both pairs preserve exact signed bytes and package identity. New manifests can declare `requires.omnihive` and its associated version fields. The host prefers them when present and continues evaluating legacy v1 manifests with their existing ranges. The compatibility response carries both field names for old and new consoles. Publish an OmniHive-only package after old hosts have drained. Legacy-capable manifests keep issuing legacy UI tokens during mixed-host deployments. The new host reads both token prefixes.
