# DashScope realtime relay and per-turn billing

The native authenticated `GET /api-ws/v1/realtime?model=...` route now supports
Qwen3-TTS-Flash-Realtime in explicit commit mode. Apply migration
`253_qianwen_realtime.sql` with the normal deployment process before serving
the new binary.

## Supported protocol

Supported models are `qwen3-tts-flash-realtime`,
`qwen3-tts-flash-realtime-2025-11-27`, and
`qwen3-tts-flash-realtime-2025-09-18`. Channel and account model mappings must
resolve to one of these models. The public query must itself name a supported
model. Omni, ASR, older token-priced TTS, custom-voice/instruct model families,
WebRTC, ephemeral tokens, audio input and server-commit mode remain unsupported.
Other realtime routes retain the structured `qianwen_realtime_unsupported`
response; invalid events in an accepted session receive a JSON error before
the connection closes or drains.

The selected account's `api_base_urls.dashscope` determines the region. Only
the official HTTPS hosts `dashscope.aliyuncs.com` (Beijing) and
`dashscope-intl.aliyuncs.com` (Singapore) are accepted. The gateway replaces the
base path with `/api-ws/v1/realtime`, uses WSS, sends the account credential,
preserves its configured proxy and optional `X-DashScope-WorkSpace`, and applies
the existing outbound host policy. Redirects and compression are disabled.
The upstream handshake and matching `session.created` must finish within 12
seconds, before the downstream upgrade. Failed upstream HTTP handshakes also
enter the existing account error/rate-limit policy.

After `session.created`, send:

```json
{"type":"session.update","session":{"mode":"commit","voice":"Cherry","response_format":"pcm","sample_rate":24000}}
```

Wait for `session.updated`, then send `input_text_buffer.append` with `text`
and `input_text_buffer.commit`. Consume the native `response.created`, audio
events and `response.done`. Only one response may generate at once. The next
turn can start after `response.done`. `input_text_buffer.clear` is allowed
before commit. End with `session.finish`; buffered uncommitted text must first
be committed or cleared. Each commit rechecks billing eligibility and runs
the existing content security audit before forwarding chargeable input.

## Bounds and cleanup

| Resource | Bound |
| --- | --- |
| User | One realtime session, also consuming an existing user slot |
| Account | Two realtime sessions, also respecting existing scheduler slots |
| All replicas sharing the database | Sixteen realtime sessions |
| Distributed session admission | PostgreSQL transaction advisory lock and 150-second expiring session rows |
| Redis slot configuration | At least three minutes; shorter configurations reject admission |
| Active lifetime | 120 seconds, independent of traffic, then at most five seconds draining |
| Idle timeout | 30 seconds, followed by bounded draining |
| Message / queue | 256 KiB per JSON text message; one queued frame per direction |
| Write deadline | Five seconds |
| Client events | 50 per second and 2,000 per session |
| Accepted text / turns | 2,000 Unicode characters across appends, including cleared text; 20 turns |

Admission never enters an SSE waiting loop. The database caps apply across
replicas and its lease outlives the absolute connection deadline, so renewal is
unnecessary. A single supervisor owns protocol state and writes, with two
bounded reader pumps that are canceled and joined on every exit. Disconnects
stop client input, retain the upstream reader briefly, and request finish only
when doing so cannot synthesize uncommitted text. There is no automatic
reconnect or input replay.

## Billing and receipt identity

The [official server-event schema](https://www.alibabacloud.com/help/en/model-studio/qwen-tts-realtime-server-events)
places Qwen3-TTS usage in `response.done.response.usage.characters`. The
[official price page](https://www.alibabacloud.com/help/en/model-studio/model-pricing)
lists $0.143353 per 10,000 input characters in Beijing and $0.13 in Singapore;
output is free. The immutable card version is
`dashscope-tts-characters-usd-2026-09-14`.

The gateway bills the provider's reported characters, never local text length,
PCM bytes or wall-clock time. For 25 Beijing characters, the unrounded cost is
$0.0003583825; the existing monetary quantization records $0.00035838 before
the user/group multiplier. A Singapore receipt of 25 characters costs
$0.000325. Subscription usage, balance, key quota/rate usage, account quota and
post-commit cache invalidation reuse the existing job billing path.

`audio_realtime_price_per_min` is **not used**: minutes cannot represent the
documented character unit. Leave group 13's field null. This adapter also does
not reuse the synchronous TTS tariff or Grok's default minute price.

Before committing input, a durable session row records `usage_pending` and the
turn count. Each received response has a separate `realtime_turn` job. Its
unique submission fingerprint is SHA-256 of the server-generated session UUID,
a separator and the provider response ID. The immutable billing snapshot holds
the session UUID, owner/key/group/account, resolved model, region and price.

The receipt is committed before charging. The existing job row lock then
protects a transaction containing the usage log, monetary/quota effects and
`charged_usage_log_id`. Replayed equal usage is a no-op; different response IDs
settle independently. Conflicting usage preserves the original `usage_json`,
retains the conflicting JSON in `result_ref`, marks `usage_conflict` and stops
generation. Neither a client event ID nor a client request ID can choose this
billing identity.

Billing transaction failures leave a durable pending receipt. The existing
timing-wheel scan retries settlement from that receipt after restart without
calling upstream or generating more audio. Known receipts remain eligible for
retry beyond their original job expiry. Unknown schemas, fields, negative or
fractional quantities, counts exceeding the admitted bound, and unknown
terminal status remain uncharged with raw usage JSON retained. Completed,
cancelled, failed and incomplete responses with explained usage can settle.

## Operational limits and verification

A disconnect/crash before receipt persistence cannot reconstruct provider usage.
The session retains a pending marker, and no estimate is charged. Expired
session leases release capacity even after a process crash. Inspect pending
sessions and uncharged turn jobs through read-only operational queries; there
is no verified provider receipt-retrieval endpoint. The current implementation
does not reserve balance escrow: it uses existing eligibility checks per commit
and bounds unresolved exposure by accepted text. These are limits relative to
the broader historical MVP proposal, not promises of recovery of lost usage.

Unit tests cover handshake/authentication rejection, configuration and message
bounds, distributed admission limits, two turns, concurrent receipt replay,
conflicts, unknown usage, transaction rollback, restart retry, disconnect
draining and pump termination. SQL-mock tests verify that an anchor-update
failure rolls back the monetary transaction. Real PostgreSQL and a deployed
provider round trip remain separate deployment verification requirements.

During this task, the only allowed live service, `:8080`, returned its older
`404 qianwen_realtime_unsupported` gate for authenticated TTS requests and 401
without an API key. No live usage or settlement row was produced, no instance
was started or restarted, and no group price or shared database row was changed.
