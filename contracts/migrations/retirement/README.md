# Compatibility retirement register

The deployment identity migration targets two stable releases, roughly 90 days after the first independent stable release. Confirm the cutover date and supported deployment inventory before starting that window. Keep each compatibility path until its owner reconciles client and deployment versions, verifies backup and restore where applicable, and rehearses the rollback. Source counts below describe in-repository entry points. Deployed caller counts are unknown until measured.

Record the rollout start, deployed versions and caller counts before retiring a compatibility path. Reconcile each domain and rehearse restore and rollback against a representative backup.

## Compatibility inventory

The initial 87 catalog entries cover nine domains. The repository supplies no complete deployed-version census, supported caller count, or published, elapsed exit window for any entry. All 87 deployed-version lists and remaining caller counts start as **unknown**. This change retires zero APIs, storage paths or identities (`actual_exit_count = 0`). Release smoke observations and a checkout SHA cannot establish fleet coverage. The [catalog](../../../backend/internal/legacy/retirement/catalog.tsv) lists the old surface, replacement, owner and exit gate for every observation ID. Related aliases sharing an owner and gate have one ID; its caller census must cover every alias.

| Initial disposition | Entries | Evidence |
| --- | ---: | --- |
| Hold, documented v1 read projections | 3 | `public-model-plaza`, `usage-detail`, `plugin-list` still have first-party API clients. No verified production census or elapsed publication window. |
| Hold, owner migration required | 74 | Wrappers, old schemas, credentials, plugins and runtime identities require the specific gate recorded against the ID. |
| Preserve | 9 | Native inference, user and plugin administration, historic key/request/receipt/payment identities, verifiers and relay compatibility. |
| Previously unregistered by runtime | 1 | Earlier MCP, onboarding, in-memory preferences/audit and rankings route family. Record old deployments and client fallbacks without claiming a retirement removal. |

The v1 model plaza has optional authenticated pricing and visibility. Migrate both public and scoped readers to catalog. The v1 usage detail has a numeric usage ID. Requests can read that historical identity as `legacy-usage-{id}` under the authorized project. The plugins v2 extension inventory has a different list shape; preserve v1 package management operations and confirm all required list fields before closing the old projection. The legacy Playground forwards a user action using an operator credential. Its change of authentication and funding requires a separate playground cutover. [Runtime route inventory](../../../docs/operations/runtime-modes.md) · [catalog catalog](../catalog/INTEGRATION.md) · [requests historical lookup](../requests/INTEGRATION.md) · [plugins inventory](../../plugins/INTEGRATION.md) · [Identity source review](../../../contracts/identity/IDENTITY_ALLOWLIST.md)

## Read a deployment

Build in `backend` with `go build -o /tmp/omnihive-retire ./cmd/omnihive-retire`. Supply approved read-only application credentials through `OMNIHIVE_REFACTOR_DSN` and optional independent journal credentials through `OMNIHIVE_RESTORE_JOURNAL_DSN`. The report covers the connected instance and the fleet census registered there. Each invocation takes fresh read-only repeatable-read snapshots. The migration application and independent restore snapshots and the retirement application snapshot may have different cutoffs. Quiesce changes before comparing release evidence. [migration measurements](../migration/INTEGRATION.md)

```sh
umask 077
/tmp/omnihive-retire scan > scan.json
/tmp/omnihive-retire report --format=markdown > retirement.md
/tmp/omnihive-retire plan --format=json > plan.json
```

`scan` and `report --format=json` return the same ordered contracts, migration status, and storage counts. `plan` orders the domain reviews and lists IDs by disposition. `--dry-run` is the default and the only accepted execution mode. Exit codes are 0 for a completed report, 2 for usage or missing configuration and 1 for read/output failure. A completed report can contain blocked entries. Use the JSON for evidence exchange. Reports from unchanged data remain identical until an observation becomes current or expires.

Storage probes count installations lacking a v2 manifest (including unknown manifests), verified plugin migration rows, live accounts without a clean workspace-matched connection, credential versions missing a matching owner/reference snapshot, pending/review receipts, and receipts without a journal link. Pre-journal historical receipts may legitimately lack a link. Migration adds counts for active recovery, retained resources and quarantines. An absent or inaccessible source, an incompatible older probe schema or active row filtering yields an unknown count. Storage rows, requests, source hits, idle leases and an empty database cannot measure distinct supported callers.

## Register deployment evidence

Migration [319](../../../backend/migrations/319_retirement_observations.sql) adds a table for reviewed attestations and leaves existing data readable. Running the CLI before 319 yields `register_available=false`; the catalog and available storage counts still report. The operator records one row per catalog ID and census cutoff using an inventory writer, separate from the read-only CLI. A review scope must include **all supported deployments** for that ID, including offline clients, old binaries, restore media, plugins, external SDKs and mixed-version workers as applicable. Use an opaque evidence UUID referring to a restricted artifact that identifies the covered instances and collection method. Keep raw payloads, DSNs, tickets, fingerprints and credential material out of the register.

For example, supply a real catalog ID, cutoff, observation UUID and release label as `psql` variables for an initial partial observation. Other fields remain unknown until verified.

```sql
INSERT INTO public.retirement_observations
    (contract_id, observed_at, evidence_id, deployed_versions)
VALUES (:'contract_id', :'observed_at'::timestamptz, :'evidence_id'::uuid,
        ARRAY[:'release_version']);
```

For a complete census use a new cutoff and record the following fields in the same insert. The latest `observed_at` for each ID controls the report. A later partial or expired observation supersedes the earlier zero. The composite primary key detects a duplicate cutoff; after a retry confirm the stored row matches the submitted evidence. A corrected observation gets a new cutoff.

| Evidence | Fields and meaning |
| --- | --- |
| Measured deployment | `deployed_versions` includes every supported release observed in scope. `remaining_callers` counts distinct supported callers still depending on the old contract during the complete window. An unmeasured count stays NULL. `complete_census` attests full coverage. |
| Time and publication | `coverage_from`, `observed_at` and `valid_until` bound the measurement and its validity. `notice_id` identifies the published announcement. `window_start` and `window_end` are its actual release-window instants. The owner must verify the announced stable releases and any longer retention, TTL or rollback requirements. |
| Reconciliation and rollback | `minimum_binary` and `minimum_schema` record the rehearsed floor. `reconciliation_id` identifies the per-domain reconciliation including caller migration and reader/lease/receipt drain. `recovery_drill_id` identifies the verified backup, representative restore and rollback using the retained release and physical resources. |

Only the three catalog entries marked `projection` can enter `review`. This requires a complete, current, zero-caller census, coverage spanning the full published window and its end, the recorded floor and both drill/reconciliation references. `review` requests owner verification of the referenced artifacts, specific exit gate and release evidence. Removal requires a separate approved change. A future, stale, unknown or partial census produces `remaining_supported_callers=null`; the raw observed count remains available in `observed_callers`. Re-register or invalidate evidence when deployed versions change.

## Review sequence and rollback

| Order | Domain | Verify before narrowing support; rollback action |
| ---: | --- | --- |
| 1 | Control projections | Compare methods, shapes, scopes and IDs with the replacement for all supported consoles. Restore routing to the previous serving binary above the recorded floor. |
| 2 | Identity | Resolve default-project and ownership quarantine. Restore previous readers with the same key digest, original RP and expanded schema. |
| 3 | Credentials | Rehearse decrypt/verify with the original persistent key, inventory native refreshers and in-flight credential leases. Drain v2 admissions and restore verified version-bound reads. |
| 4 | Catalog | Compare public/scoped visibility, price and action evidence. Republish the previous compatible snapshot with its original provenance. |
| 5 | Execution | Reconcile tuple behavior, active streams, task generations, policy epochs and outstanding IDs. Fence the writer before resuming a compatible gateway or worker. |
| 6 | Finance | Reconcile postings, holds, balances, historical invoices and pending callbacks/receipts per scope and currency. Recover using original effect IDs under a single writer. |
| 7 | Plugins | Verify stockpiled signed artifacts, mappings, grants and bindings on every host. Restore the original verified package and trust policy with the recorded mapping. |
| 8 | Operations | Verify audit custody, outboxes, telemetry purge and the independent restore journal. Preserve recorded generations and resolve outstanding operations before restarting workers. |
| 9 | Runtime identities | Inventory every deployed alias, retained client and physical binding. Roll back using the same volume, keys and negotiated/derived protocol identities. |

Coordinate dependent cutovers with the owner. Each P2 schema waits for its producer and each data contract waits for a recoverable backup, representative restore and a zero census of dependent readers, leases and receipts. Keep the additive schema during binary rollback. Preserve financial history, fingerprints, attribution and immutable migrations. See the [migration contract](../migration/INTEGRATION.md).

## Isolated verification

Set `RETIREMENT_INTEGRATION_TEST_DSN` to a disposable loopback PostgreSQL `job_test` URI. The fixture creates a uniquely named database and uses a read-only connection for the CLI. It checks pre-319 coverage, double application, deterministic scan/report/plan, a new census superseding an old zero, protected survivors, historical invoice/receipt lookup and storage fallbacks. The schema and domain gates are reviewed separately from production release evidence.

## Protocol retirement candidates

| Legacy name | Owner | Current contract and caller count | Retirement gate |
| --- | --- | --- | --- |
| `sub2api-admin` | Console, retirement | One first-party console connector, `subscribeQPS`, and one server route, `/api/v1/admin/ops/ws/qps`. The console offers `omnihive-admin` first and `sub2api-admin` second for old servers. The current server negotiates the preferred offered name and strips the ticket before the upgrade. | Publish support for the new name and maintain dual acceptance for the B0 window. Count successful upgrades by selected protocol, without collecting tickets. Retire the fallback after the old servers and all supported legacy-only clients drain. |
| `X-Sub2API-Usage-Source` | protocol, retirement | Two Cursor non-streaming writer paths share one emitter and return the same value in both this header and `X-OmniHive-Usage-Source`. Zero in-repository production readers. Downstream SDKs, proxies, and monitoring configuration may read the legacy header. Streaming keeps the SSE marker. | Readers prefer the OmniHive header and fall back to the legacy header. Keep dual emission for the B0 window and verify external client upgrades before removal. Server emission telemetry alone cannot establish which header a caller reads. |
| `/v1/sub2api/billing`, `/sub2api/billing` and `sub2api.key_billing` | protocol, retirement | Two inbound route aliases and one outgoing relay probe still use this path and discriminator. Absent a relay upgrade, the probe needs to keep reading the legacy response. | Publish a versioned replacement, dual-read discriminators, and inventory supported relay versions before retiring either route. Retain interoperability beyond the B0 window while supported relay callers remain. |
| `X-Sub2API-Grok-Client-Tool-Cache`, `python__sub2api`, Google `ErrorInfo.domain` values `sub2api` and `sub2api.securityaudit` | protocol, audit, retirement | Client cache opt-in, replayed Codex tool names and machine-readable error identities. Older clients and saved transcripts may depend on these names. | Publish a versioned mapping. Accept both opt-in names and both tool aliases through the B0 window. Switch error domains only under a negotiated client contract. Keep legacy replay support for retained transcripts. |
| Grok OAuth referrer and User-Agent | protocol | One xAI authorization builder sends the old referrer and two client paths send the old User-Agent. Vendor-side usage is unknown. | Verify both names against xAI before any producer change, then retire the old value after the B0 window and vendor compatibility sign-off. |
| Account export `sub2api-data` / `sub2api-bundle` | Connections console, runtime, retirement | Shared by the exporter, importer, and safe-import documentation. Existing archives keep their historical discriminators. | Read both formats while writing an explicitly versioned replacement. Drain old importers during the B0 window. Keep archive imports until recovery coverage permits retirement. |
| Legacy compliance acknowledgment phrase | Console, audit | An exact legacy phrase is accepted as an alias during acknowledgment. Stored legal and settings defaults can also contain the historical name. | Publish current copy, preserve acknowledgment records and authored clauses, and retain legacy-input compatibility for the B0 window and until older bundles drain. |

## Persisted, cryptographic and deployment identities

| Namespace or persisted value | Owner | Migration strategy and window |
| --- | --- | --- |
| `database.dbname=sub2api` | runtime, retirement | Wizard, installer auto-setup and shipped Compose samples use `omnihive` on new installs. `DATABASE_DBNAME` overrides the config file in the usual order. An existing config, install lock, or installer bypass with the name omitted keeps the legacy default. Pin the actual name before relocating configuration or changing Compose templates. Keep explicit legacy DB names valid indefinitely. Retire implicit fallback only after all supported installs pin the name and restore verification passes. |
| `sub2api:prompt_guard:config:invalidate`, `sub2api:prompt_audit:payload:`, `SUB2API_PROMPT_AUDIT_PRIORITY_END` | audit, retirement | Add versioned payload decoding and dual reads before switching writers. Keep invalidations visible on both channels during a mixed-version rollout. Drain old workers and finish reading or expiring old payloads before removing the old namespace after the B0 window. |
| `sub2api/live-attestation/v1` | protocol, retirement | Version the domain. Verify both signatures before enabling new signing, then permit new signatures only for capable verifiers. Preserve old verification through the B0 window and until the final v1 attestation expires. |
| `sub2api:codex-install-id:v2:`, `sub2api:codex-session-id:v2:`, `sub2api:codex-thread-id:v2:`, `sub2api:codex-account-identity:`, `sub2api:codex-call-id:v1:`, `sub2api:codex-compact-probe:v1:` | protocol, retirement | Version and compare both derivations during migration. Keep one pinned version for existing accounts, affinities and transcripts. Dual-read persisted mappings. Sign or derive new identities under the new domain only after compatible readers are deployed. Keep old versions through the B0 window and the longest retained replay or affinity lifetime. |
| `sub2api:grok-safety-id:v1:` | protocol, retirement | Add a versioned derivation, compare outputs and pin existing sessions to their original version. Coordinate using one stable upstream ID per session. Drain legacy sessions after the B0 window and confirm upstream acceptance before retirement. |
| `sub2api.reasoning-continuation` | protocol, retirement | Version the continuation envelope, dual-read and verify before minting new tokens. Keep the legacy decoder through the B0 window and until retained transcripts have migrated or aged out. |
| `urn:sub2api:identity-ticket` | identity, retirement | Accept both provider keys under the same single-use store and scope checks before issuing new tickets. Wait through the B0 window and maximum ticket TTL after old binaries drain. |
| `sub2api/payment/encryption` and historical `sub2api-payment-resume` signatures | payments, retirement | New version reads both encryption versions and verifies both signature versions, then explicitly reseals stored secrets and issues new signed resume data. Preserve old decryption and verification through migration, the B0 window and the last supported rollback. |
| `sub2api:duplicate_operation_id`, dashboard Redis `sub2api:`, PostgreSQL `sub2api.usage_retention` | status, runtime, retirement | Version and read both old/new metadata or cache keys without splitting uniqueness, deduplication, or coordination. Preserve the SQL GUC for existing triggers until a new migration and dual-version deploy pass. Contract after the B0 window, outstanding operations, and supported rollback all clear. Existing migration files stay immutable. |
| Aliyun credential validation marker and upstream request fixtures | connections, protocol | Migrate a serialized marker under a versioned parser if it is still needed. Treat caller-supplied legacy model inputs, path prefixes, role data and RP origins as valid data. External relying-party identities need explicit user-side migration and recovery before any retirement. |
| Browser locale, login acknowledgment, refresh lock, IP geolocation, preview IndexedDB and duplicate/bulk operation keys | Console | Use the versioned adapter and preserve mixed-tab refresh coordination. Start writing new state after dual-read testing. Remove old reads after the B0 window, old tabs and pending operations drain. |
| Legacy service paths, binary/archive names, app volume/network identities, and deployment environment aliases | Deployment identity, release, retirement | Preserve identity or explicitly bind the old physical resource for existing deployments. See [deployment identity migration](../../../docs/operations/identity-migration.md) for fallback precedence. Cut over after verifying asset provenance, inventory, rollback and the B0 window. External explicitly named volumes remain supported. |
| Legacy test-only flags and fixture resource IDs | Deployment identity, release | Keep CI/harness aliases paired while the test callers migrate. Test prefix and database values represent isolated or explicit legacy fixtures. Remove flags after migration of supported CI and local harness callers. |

### Frontend storage keys

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

## Plugin handoff

| v1 surface | Identity |
| --- | --- |
| Built-in package and trusted signer | `local.sub2api.openai-transport`, `sub2api-openai-transport-v1` |
| Manifest and frontend compatibility fields | `requires.sub2api`, `recommended_sub2api_version`, `tested_sub2api_versions`, `current_sub2api_version`, `required_sub2api_version` |
| Host and UI session | `sub2api:plugin-ui:v1:`, `sub2api-plugin-ui`, `sub2api-plugin-host`, `sub2api.plugin.ready` |
| SDK and package | `sub2api.plugin.v1` protobuf/gRPC, v1 handshake/magic, and `.s2plugin` |
| Stored plugin state | `sub2api_plugin_installations`, `sub2api_plugin_bindings` and their SQL constraints and indexes |

The plugin manager verifies the original archive, manifest bytes, signer, and capabilities before importing. Map compatible host ranges and plugin IDs into the v2 schema. Rebuild and re-sign with an authorized publisher and record old/new digests, signer IDs, and capability grants. Ship trusted key metadata through the existing trusted release chain or explicit operator verification. Test the v2 host and bridge against both package inventories. Keep v1 verification and rollback mappings through the B0 window, until old packages and runtime callers drain. Preserve v1 signing material and immutable migrations under their original identities.

The v1 host accepts the new `local.omnihive.openai-transport` and `omnihive-openai-transport-v1` pair against the pinned public key and continues to accept the corresponding legacy pair. Both pairs preserve exact signed bytes and package identity. New manifests can declare `requires.omnihive` and its associated version fields. The host prefers them when present and continues evaluating legacy v1 manifests with their existing ranges. The compatibility response carries both field names for old and new consoles. Publish an OmniHive-only package after old hosts have drained. Legacy-capable manifests keep issuing legacy UI tokens during mixed-host deployments. The new host reads both token prefixes.
