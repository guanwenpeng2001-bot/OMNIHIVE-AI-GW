# Gateway lifecycle replay

The three JSON fixtures drive the real Responses, Messages and WebSocket handlers against the local Grok adapter stub. Each failure entry specifies the decision at the failed account. The first 429 follows an actual transport send. A revoked credential fails before dispatch. The lost-ack case makes the transport return an unexpected EOF. The heartbeat, semantic-output and terminal scenarios inject the corresponding downstream state immediately before the same 429. The deadline fixture expires the original admitted context during the send. For a Grok capacity 429, Responses receives provider same-account retry metadata. The Messages and WS bridge return a switch decision. Separate writer tests exercise actual SSE and WS frames, failed writes, and the once-only terminal guard.

`attempt_number` counts actual generation sends in the logical request or turn. A pre-dispatch credential failure can have attempt number zero. `remaining` comes from the shared transport budget and includes provider-level repairs. `excluded` is a sorted snapshot. `not_sent` for a known rejection indicates the provider declined generation; the transport record distinguishes a rejection from a dial failure. `accepted` indicates a transport response or an observed WS response event. An ambiguous send or a WS write without a provider acknowledgement has `unknown` acceptance.

For BE-06, install `attempt.WithRecorder` on the admitted request context. The callback receives the decision with the current plan in context. The recorder runs synchronously outside the budget lock. `AttemptBudget.Response()` returns the response lifecycle for the current logical turn. An older handle keeps its own final state after rollover. The request plan, transport dispatch, receipt and reservation remain with their existing owners.

For BE-12, HTTP error encoding still uses the native gateway encoders. The writer recognizes SSE comment and ping forms, protects semantic output, and observes terminal delivery. The WS native, HTTP bridge and passthrough frame writers report the result of the native write. Known WS field rejections, missing previous responses and provider rate limits reconcile a pending send before a same-turn retry. Further ingress codecs can use the same lifecycle and replay decision.

## Legacy WS first-output expectation

At base `555a92ab7`, `TestOpenAIResponsesWebSocket_FirstOutputTimeoutWithoutDownstreamReusesClientForOneFailover` requires a second account to receive `response.create` after the first received the request and returned no acknowledgement. The fixture has no idempotency key or provider reconciliation. The protected controller reports `acceptance_unknown` and the client receives the existing retryable WS close. An explicit upstream rejection before any output still permits failover. The legacy expectation requires a contract decision before integration.

## Microbenchmarks

Go 1.27.4, linux/amd64, AMD Ryzen 5 7500F, one Go CPU. These are medians of three one-second runs. The SSE comparison writes a 95-byte text delta to a Gin recorder with body storage disabled.

| Operation | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| One failover decision, one excluded account | 96.59 | 8 | 1 |
| Heartbeat, semantic output and successful terminal transitions | 6.786 | 0 | 0 |
| Native SSE writer | 4.365 | 0 | 0 |
| SSE writer with lifecycle tracking | 223.3 | 96 | 1 |

From `backend/` run

```sh
go test -tags=unit ./internal/gateway/... ./internal/handler -run '^$' -bench 'Benchmark(AttemptDecision|ResponseLifecycle|AttemptSSEWrite)$' -benchmem -benchtime=1s -count=3 -cpu=1
```
