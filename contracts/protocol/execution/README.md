# Execution identity and recovery

The Responses HTTP and WebSocket routes share the admission factory. Each native request, logical WS turn and accepted async image task has a server UUID. The gateway retains the provider's native response IDs on the wire. HTTP admission also exposes the UUID in `X-Execution-ID`.

A keyed Responses POST registers a SHA-256 hash of the key and a fingerprint of its JSON input. Object member order and whitespace are insignificant. Array order, string content and number spelling are significant. Keyed JSON rejects duplicate member names. The database scopes registration to workspace, project, actor, credential ID and route. Concurrent matching submissions return one admitted execution. On HTTP a repeat acknowledges that identity in `X-Execution-ID` with status 204, `Cache-Control: no-store` and `X-Idempotency-Replayed: true`. The gateway retains metadata only. A changed payload receives the native 409 error. WS frames and image workers use their turn and job identities.

The first attempt intent commits atomically with admission. The dispatch transaction sets its account, records unknown acceptance and claims its ordinal once. Later permits share the original deadline and budget. A lost admission acknowledgement can be read back using the server UUID. A lost dispatch acknowledgement stops sending and leaves the claimed permit available for reconciliation.

The crash fixture names the checkpoint where the subprocess stops. For the in-flight checkpoint the local provider receives the POST and withholds its reply. The parent kills the sender, advances the stored deadline, then queries the actual PostgreSQL attempt, receipt and release facts. The receipt-committed checkpoint covers the interval before client or worker acknowledgement. Explicit token-presence flags distinguish observed zero from unknown. A silent BE-03 timeout records its original reason, unknown acceptance and abandoned flag. A replacement on another account may generate its own receipt.

Run `contracts/migrations/execution/smoke.sh` with the dedicated PostgreSQL and Redis environments. The integration handoff describes the post-composition smoke and benchmark.
