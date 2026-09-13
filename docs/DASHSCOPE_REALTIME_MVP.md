# DashScope realtime: bounded gateway proposal

Status: **design only; realtime is not enabled**. This is the section C fallback,
not a claim that a WebSocket adapter or billing integration has shipped.

## Why the current relay cannot be reused safely

`handler/qianwen_native_realtime.go` rejects before account admission and upgrade.
The native route already has API-key authentication. Removing that rejection is
insufficient:

* `handler/grok_audio.go` records one elapsed-minute charge after proxying a
  connection. DashScope's families have different units and per-response usage.
* `service/billing_service.go:CalculateAudioCost` supplies a default USD 0.05/min
  when a realtime price is absent. That default is not a DashScope price.
* `service/openai_gateway_usage.go:calculateOpenAIRecordUsageCost` dispatches
  `AudioUsage` to that calculator. Its existing native-audio special case only
  recognizes synchronous multimodal-generation and an explicit model allowlist.
* `service/upstream_job_service.go` anchors one terminal job charge with
  `charged_usage_log_id`. One conversation can have many completed or cancelled
  responses; a single job-level anchor would suppress later turns or rebill them
  as an aggregate. Request deduplication alone cannot recover missing receipts.

Long connections can be bounded inside the gateway; a separate service is not a
prerequisite. The release blockers are a durable turn ledger, model/region/unit
price resolution, and bounded disconnect handling. A transparent relay would
send chargeable output before those capabilities exist. This proposal therefore
retains the gate until the acceptance conditions below pass.

## Official protocol and pricing basis

The [TTS WebSocket reference](https://www.alibabacloud.com/help/en/model-studio/interactive-process-of-qwen-tts-realtime-synthesis)
specifies bearer authentication during the handshake, a `model` query, optional
`X-DashScope-WorkSpace`, and JSON events. TTS starts with `session.created`, accepts
`session.update` and text-buffer events, and ends with `session.finish` /
`session.finished`. Its English page currently repeats the international host
under Beijing; verify the selected regional endpoint against the console before
enabling a price card. Never infer region from the model name.

The [TTS server-event reference](https://www.alibabacloud.com/help/en/model-studio/qwen-tts-realtime-server-events)
places Qwen3-TTS character usage in `response.done.response.usage.characters`.
Legacy Qwen-TTS uses token details instead. `response.audio.done` is not a billing
receipt. Keep the provider response ID, session ID, status and complete usage
object; do not reconstruct charges from text length or received PCM bytes.

The [Omni realtime guide](https://www.alibabacloud.com/help/en/model-studio/realtime)
describes modality-token billing and different audio token conversion rules by
model generation. Wall-clock session time includes silence and cannot represent
these quantities.

Published [official USD prices](https://www.alibabacloud.com/help/en/model-studio/model-pricing),
checked September 14, 2026 (China time):

| Model / region | Input | Output |
| --- | --- | --- |
| qwen3-tts-flash-realtime / Beijing | $0.143353 per 10,000 characters | Free |
| qwen3-tts-flash-realtime / Singapore | $0.13 per 10,000 characters | Free |
| qwen3-omni-flash-realtime / Beijing | Per million tokens: text $0.315, audio $2.709, image $0.559 | Text-only input: text $1.19; multimodal input: text $2.179; audio $10.766 (text accompanying audio not separately billed) |

For example, 25 reported TTS characters cost $0.0003583825 at the listed Beijing
rate, before the gateway multiplier. These are reference prices, not installed
cards or observed charges. A blanket `audio_realtime_price_per_min` cannot encode
them. Leave it null for DashScope; do not set zero or reuse the Grok default.

First implementation should allow only pinned Qwen3-TTS-Flash-Realtime models
with region-specific character cards. Existing synchronous TTS group pricing
must not be overwritten: it may have a different rate. Omni requires separate
input/output modality prices and output-mode conditions. ASR needs its own
verified duration schema/card. All other models remain explicitly unavailable
until their schemas and regional prices are verified.

## Minimum implementation contract

These are proposed limits, not active configuration or official provider limits.

1. Accept only authenticated `GET /api-ws/v1/realtime?model=...` upgrades. Apply
   group/platform, mapped-model, moderation and billing-eligibility checks before
   dialing. Do not issue upstream API keys or implement ephemeral-token/WebRTC
   endpoints as a shortcut. Resolve account ownership through the scheduler.
2. Reserve a user slot and account slot for the whole connection. Start with one
   concurrent session per key/user, two per account, and sixteen per process,
   also respecting stricter existing limits. Use distributed expiring leases
   with renewal and fencing; process caps alone do not protect multiple replicas.
   Reject excess admission with 429 before upgrade, without an SSE wait ping.
3. Construct the WSS URL from the validated configured provider host, replace
   `/compatible-mode/v1` with `/api-ws/v1/realtime`, and set only the resolved
   model. Preserve region and approved proxy/SSRF policy. Use the account bearer
   credential, never the downstream key. Disable redirects and compression.
   Bound dial and first `session.created` to 12 seconds. Upgrade the client only
   after both succeed. Release every lease on failed dial or accept.
4. Two supervised pumps relay JSON text frames. Bound each frame to 256 KiB,
   buffered data to 1 MiB per direction, writes to 5 seconds, idle time to 30
   seconds and total session lifetime to 120 seconds. A lifetime timer remains
   independent of traffic. Cap client events at 50/second and 2,000 per session;
   cap TTS accepted characters at 2,000 and generated turns at 20. Backpressure
   closes a session instead of allocating unbounded queues or goroutines.
5. Start TTS in explicit commit mode with one outstanding response. Track
   CREATED -> CONFIGURED -> BUFFERING -> GENERATING -> DRAINING -> CLOSED.
   Reject attempts to change the bound model or bypass the supported mode and
   content limits. Allow buffer clear before commit. After commit, do not claim
   cancellation refunds synthesis: close/drain and settle any reported usage.
   A future Omni adapter forwards `response.cancel` and buffer-clear events but
   keeps cancelled responses eligible for usage-based settlement.
6. On client disappearance stop accepting input, retain the upstream reader for
   at most 5 seconds, request graceful finish/cancel as appropriate to the family,
   and persist final receipts even when the client cannot receive them. A context
   detached from the request must still have this deadline. On expiry close both
   sockets, join both pumps and release leases exactly once. Do not reconnect or
   replay audio/text automatically; that can cause a second generation.

## Turn settlement and idempotency

Before forwarding chargeable input, persist a session owned by user, key, group
and account with a server-generated UUID, model, region, immutable price version
and reserved spending allowance. A reservation is not a final usage charge.
Block commits that would exceed the remaining allowance. This first TTS scope
has a bounded character quantity; an Omni implementation needs a separately
validated maximum-output budget.

Persist each trusted upstream terminal receipt before acknowledging its
completion downstream. Use a unique key `(gateway_session_id, response_id)` and
store a canonical usage hash, quantities, status and `charged_usage_log_id`.
Never key settlement by client `event_id`, client request ID or session alone.
The same ID and same usage is a no-op; conflicting usage for one ID is quarantined
and stops further generation. Different responses in one session charge
independently. Reconnection creates a new session; it is not an upstream resume.

Settle from that durable row using the existing transactional billing repository
and deduplication machinery, with a server-derived money-event ID. The charge,
dedup marker and receipt anchor must commit atomically; failures remain pending
for a durable retry worker. An in-memory map or an asynchronous callback attached
to Gin does not provide this guarantee.

Validate integral nonnegative quantities, model/schema agreement, totals and
finite positive prices. Unknown units, absent receipts and conflicting totals
are unresolved, never zero-price successes. Preserve raw usage for investigation
without retaining audio in the ledger. For transport loss before final usage,
mark the turn `usage_pending`, release only the unconsumed reservation according
to an explicit bounded reconciliation policy, and alert on outstanding liability.
Do not charge an estimate. Because no provider response-retrieval mechanism was
verified here, any crash-loss reconciliation must be proved before rollout;
reserve enough to bound operator exposure and define a maximum pending interval.

## Required release evidence

Use tests with mocked sockets and transaction failures for malformed/oversized
events, unauthorized models, bad origins, dial failure, slow readers, all timeout
paths, interruption before/after a receipt, and concurrent lease exhaustion.
Verify every termination joins pumps and releases all slots.

Inject duplicate receipts, conflicting receipts, two independent turns, and
crashes before/after each transaction boundary. Assert one charge per accepted
receipt and replay of pending work after restart. Missing usage must stay pending.

Finally run through the deployed gateway: authenticated 101, session-created,
configuration acknowledgement, actual audio, response.done with official usage,
session-finished, one durable charge and no duplicate charge on receipt replay.
Record path, account, region, elapsed time, usage, price version and cost. Local
tests cannot replace this evidence. The task forbids restarting :8080, so any new
binary requires a separate authorized deployment before that final verification.
