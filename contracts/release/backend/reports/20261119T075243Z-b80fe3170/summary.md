# BE-24 candidate verification

Candidate `b80fe3170de114beab2cfa19c74ec264d171ffcb`. All services ran on loopback in an isolated network namespace.

| Check | Status |
| --- | --- |
| OPENAPI | pass |
| TYPESCRIPT | pass |
| JOURNEY | fail |
| GROUPED_PLAYGROUND | fail |
| SURFACES_HTTP | pass |
| PROTOCOL_HTTP | fail |
| PUBLIC_NATIVE | pass |
| SDK_HTTP | pass |
| CALLBACK_HTTP | pass |
| ACCESS | pass |
| LOAD | pass |
| LIVE | pass |
| MATRIX | pass |
| REGISTRY | pass |
| STATUS_MODULE | pass |
| FINANCE | pass |
| FINANCE_MODULES | fail |
| SIGNED_CALLBACK | pass |
| ACCESS_MODULES | pass |
| CONTROL_MODULES | pass |
| MIGRATION | pass |
| RESTORE | pass |
| CUTOVER_FIXTURE | pass |
| PAIRED_2H | pending |
| CUTOVER | pending |
| WIRE_PARITY | pending |

Cohort decision: HOLD. Measurement details and field assertions are in [results.json](results.json). Individual candidate binding decisions are in [matrix.json](matrix.json).

The first FINANCE_MODULES command selected the integration tag and matched zero tests. On the same product revision, both PostgreSQL suites ran with their required unit tag in a fresh namespace. All 40 tests and subtests passed. [Corrected finance evidence](finance-recheck.json)

The full declaration inventory covers 346 account/mode/protocol profiles and 29 actions. Its decisions include the source registry evidence. [Registry](registry.json)

Playground failed before dispatch, and the text-only fixture received an embeddings POST. All 300 short-load requests succeeded. The paired two-hour and final cutover gates remain open. [Case results and owners](../../../../../docs/runbooks/backend-v2/GA_CHECKLIST.md)
