# BE-25 verified audio and video tasks

Migration 314 expands BE-15 with endpoint approvals, typed media attributes and scoped views. The existing task handler constructor installs the two adapters. Keep the BE-15 JWT, CSRF, rate limit, audit redaction, persistent cipher, recovery sweep and ticket delivery bindings. The synchronous audio adapter writes to the private task artifact bucket. Native video jobs use the existing upstream job repository, polling lifecycle and pinned DashScope delivery account. The native poller must be running.

## Enabling a verified endpoint

The approval table starts empty. The owner of the endpoint verification process publishes an immutable row in `task_media_verifications` for a specific workspace, project, billing group, account, native model and implemented adapter. Supply references to the model capability test, successful action invocation and funding/settlement evidence, the verification and expiry times, and the native USD base rate with its unit and denominator. A video approval with tested cancellation may also carry `cancel_evidence`. For a price change or reverification, insert a new row. Revocation sets `revoked_at`. Changing the account credential, provider identity, proxy binding, workspace or active status invalidates its approvals. Keep the table writable only by the trusted verification process. A session cannot supply approvals or operator-selected accounts.

| Family | Model | Action and adapter | Accepted input and output | Base rate | Cancellation |
| --- | --- | --- | --- | --- | --- |
| Audio | qwen3-tts-flash and its 2025-09-18 and 2025-11-27 snapshots | audio.tts, qianwen.audio.tts | 1–600 Unicode characters, Cherry, PCM WAV up to 32 MiB and 120 s | USD per 1,000,000 provider-reported characters | Unavailable |
| Video | wan2.6-t2v | video.generate, qianwen.video.generate | Five seconds, 1280×720, one MP4 up to 256 MiB | USD per provider-reported second at 720p | Queued upstream jobs with current cancellation evidence |

Use the catalog's public model ID in the Task API. The normal gateway performs project key and funding admission, moderation, quota checks and account selection. A send requires the selected API-key account, exact native model mapping, current scoped approval and native rate. The generation path takes the BE-15 live lease and BE-06 one-use permit. Request Detail receives the original request UUID and the verified price version. Operators should test through the target group's real account selection before enabling a cohort.

Synchronous speech uses the native per-character resolver. Realtime sessions have their own turn-level funding and retention contract. The native video worker owns its provider task and charge anchor. A new v2 video uses the BE-06 execution UUID as its upstream public job ID. Its native charging transaction persists the BE-11 receipt, links the attempt, emits the settlement outbox event and commits the original usage-log anchor together. Equal commands return the stored response. An explicit fresh paid command creates an independent execution, even for equal prompt text. Separate historical v1 callers keep body-fingerprint replay. A crash or ambiguous acknowledgement leaves a queryable unknown state. Provider status, cancellation confirmation, usage receipts and output retention update independently. A cancel command commits its idempotency record before the one upstream invocation. A lost cancel acknowledgement leaves the request recorded while polling continues.

## Existing data and rollout

Migration 314 captures scoped Qianwen Wan 2.6 T2V/I2V video jobs and pinned Qwen3 TTS realtime turn receipts. It verifies the persisted key, owner, workspace and project mappings. Each native source retains its original status, result, funding identity and expiration. Realtime turn rows contain usage and terminal status. Downloadable audio requires a retained private WAV from a newly submitted speech task. A historical video can expose a retained result with null size. A charge is displayed only with the matching receipt or original usage-log anchor, owner, key, account and request identity. Historical native jobs may have no BE-06 request.

Run the SQL check below after the existing identity migration completes. Resolve ambiguous ownership through BE-01, then call `task_media_import(id)` on the eligible source. The function and insert trigger preserve the original mapping across replay.

```sql
SELECT u.id,u.family,u.status
FROM upstream_jobs u
WHERE u.platform='qianwen'
  AND ((u.family='video' AND u.billing_snapshot->>'model' IN ('wan2.6-t2v','wan2.6-i2v'))
    OR (u.family='realtime_turn' AND u.billing_snapshot->>'model' IN
        ('qwen3-tts-flash-realtime','qwen3-tts-flash-realtime-2025-09-18','qwen3-tts-flash-realtime-2025-11-27')))
  AND NOT EXISTS (SELECT 1 FROM task_media_attributes m WHERE m.provider_job_id=u.public_request_id);
```

Apply 314 before rolling out these task readers. Route Task Center traffic to upgraded readers and complete the native worker rollout before publishing approvals. Older BE-15 readers label shared journal rows as images. Roll back paid admission by revoking approvals. Keep upgraded readers and native polling available for retained results and accepted jobs. Contract work belongs to BE-30 after reader drain and financial reconciliation.

Compose the BE-15 and BE-25 fragments and regenerate the frontend types in the integration branch. The existing Task Center registration owns all public paths. Configure verification records only after measuring a deployment's actual model, price, account and result behavior. No verification records ship with this change.

## Verification

Run `contracts/migrations/media/smoke.sh` from a checkout against dedicated PostgreSQL and Redis instances. It runs the BE-15 idempotency, Redis legacy and ticket regressions, and the media admission, native unit pricing, ambiguity, result expiry, cancellation and legacy job checks. Provider traffic uses in-process fixtures. The native video verification record from September 2026 is described in `backend/dev-docs/qianwen-video-generation.md`. The distinct realtime turn contract is described in `docs/DASHSCOPE_REALTIME_RELAY.md`.
