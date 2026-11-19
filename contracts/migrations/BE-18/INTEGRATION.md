# BE-18 public status and incident publication

Apply 307 through the existing migration runner after BE-06 and BE-14. Apply the 307a concurrent source indexes in the same release. The first publisher tick builds recent data. Historical backfill then works backward in UTC day slices. Uncovered windows remain unknown with `ROLLUP_PENDING` while backfill advances. Existing retained observations determine what can be reconstructed for each date.

## Runtime wiring

- Register `handler.ProvideStatusHandler` in the application graph. Pass its `*control/status.Handler` to `statushttp.Register(v2, statusHandler, public, session)`. Apply `panelRateLimiter.PublicIP()` to the anonymous routes. Apply the existing v2 JWT session, authenticated rate limit and audit middleware to the publication routes. The handler resolves `ops.incidents.publish` through the BE-01 platform grant on each publication.
- Start `go statusHandler.RunCollector(ctx)` once in the normal application lifecycle and cancel its context before closing the database. The collector uses level-triggered PostgreSQL advisory transaction locks, so multiple replicas may run it. It honors the Ops monitoring switch. The anonymous reads remain available if the collector stops and age retained evidence into `stale`.
- Add `If-None-Match` and `If-Modified-Since` to the configured CORS request-header allowlist when wiring cross-origin clients that set them explicitly. The existing middleware exposes `ETag`. `Last-Modified` is a CORS-safelisted response header. The admin endpoints use the existing console session and CSRF policy.
- Recompose the aggregate OpenAPI from the fragments and regenerate the frontend v2 types. API fixtures are in `contracts/api/BE-18`. The stale component fixture omits the 90-item history array for readability. Public routes use the standard success envelope and 304 without a body. A new representation keeps the ETag stable until its selected data changes.

## Measurement and backfill

- One logical outcome comes from each eligible finalized journal request or turn, an independent terminal legacy receipt/usage record, or a correlated legacy Ops terminal failure. Receipt and usage joins require matching request ID, API key, funding user and account. Journal Ops attribution uses the key, key owner, exact correlation and execution time. The source never counts 2xx recovered upstream rows as request failures. Admission denials and client cancellations are reported independently and excluded from the rate. Unknown final outcomes suppress the rate.
- A journal dispatch permit counts in its dispatch minute, even when it precedes the request's finalization minute. The attempt total covers journaled requests and turns. The first-byte timeout count is the journal's explicit first-output abandonment reason. Upstream 429 requires provider attribution and upstream HTTP 429. Stream interruptions after first output have their own terminal category. The BE-08 routing trace is decision evidence and does not supply outcome counts.
- Version 307 defines the fixed inference endpoints. The public dimensions are the inference total and the OpenAI-compatible, Anthropic-compatible and Gemini-compatible families. Console/Auth and the async worker components have no publication-safe observations in this baseline and report `SOURCE_UNAVAILABLE`. Published incidents can represent confirmed impacts on those components. Historical success and latency are scoped to evidence for the allowlisted operations. A public model-family breakdown requires an approved canonical model mapping and adequate samples.
- Closed minute bins have a 30-second publication delay, with a 15-minute overlap for late evidence. The collector merges them into hour and UTC day bins. The four current windows have minute-aligned half-open bounds. Each component has exactly 90 chronological UTC dates including the partial current date as of the watermark. Daily history uses the retained verified sources, whose retention can be shorter than the publication horizon. A retained day with fewer than 20 outcomes reports `INSUFFICIENT_SAMPLES`. A success rate below 90% is an outage, 90% to below 99% is degraded, and 99% and above is operational. Current rates go unknown when the collector or component evidence is at least five minutes old. The last success remains available after expiry and is rounded to a minute.
- TTFT uses successful first-token observations in fixed latency bins. p50 and p95 are the upper bounds of their bins and are null when fewer than 20 TTFT samples exist or the percentile falls in the open-ended bin. Stream completion requires at least 20 known streaming outcomes. All rates are percentages.
- The public rollup contains only counts, timestamps and fixed component IDs. The incident publication DTO exposes approved publisher-authored plain text, public incident IDs, fixed components and lifecycle timestamps. Render the title and messages as text. The publication command table holds private actor and retry identity and is never joined into anonymous responses.

## Cutover checks

Compare the published aggregate against journal and Ops fixtures and verify the sparse and unobserved components on the status UI. A confirmed incident can raise visible severity independently of `observed_state`. Summary shows the newest 25 active incidents and the total active count. The incident list provides seek pagination and the detail endpoint returns the complete timeline.

This baseline contains no standalone public `/status` data source to remove. Select the new projection as the status page's source after publication checks and remove any frontend placeholders at that point. The authenticated `/monitor` channel views and process health probe serve existing scoped and machine consumers.

## BE-21 handoff

- Keep tenant-facing readers on the scoped Ops path. The public service exports fixed dimensions only, with a 90-date day horizon and 31 UTC dates at minute/hour grain. Operational retention jobs can prune source data per BE-21 policy after verifying the collector overlap and the backfill cutover. The compact last-success row preserves evidence after old days expire.
- Publish routes can use the existing audit middleware. Use the session actor and public incident ID as the action reference and leave message content out of privileged audit metadata. `status_incident_commands` stores actor-scoped idempotency hashes and references the exact published update for retries. Preserve that mapping for the promised retry period. Public incident timelines retain all published updates.
- If a future telemetry source replaces Ops or adds safe component coverage, verify terminal-outcome identity and source freshness before changing publication. Keep recovered attempts, capacity denials and tenant/private dimensions out of the request success denominator. Retain the grant, cache and validator behavior when moving to shared readers.

## Verification

Use a fresh PostgreSQL 16 `job_test` database and the existing disposable test-service guard.

```sh
go test -tags=integration -count=1 -v ./internal/control/status
go build ./...
go vet ./...
go test ./...
```
