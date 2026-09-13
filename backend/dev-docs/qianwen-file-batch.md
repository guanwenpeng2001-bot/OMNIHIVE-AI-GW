# DashScope file batches

The `/compatible-mode/v1/files` and `/compatible-mode/v1/batches` endpoints use
`upstream_jobs` for ownership, submission claims and transactional settlement.
Migration 248 extended that table's family constraint; migration 252 adds durable
resource state. Deploy migration 252 together with this implementation.

## Production pricing

The Beijing adapter admits `qwen-long` and `qwen-long-latest`, with plain text
messages and no thinking, tools, images, external file references or caching.
Both have an official **Batch File** price of **CNY 0.25 input / CNY 1 output per
million tokens**. These are already discounted prices, not synchronous Batch
Chat prices. The diagnostic `batch-test-model` remains explicitly free.

Sources, checked 2026-09-14:

- [Supported Batch File models and protocol](https://help.aliyun.com/zh/model-studio/batch-interfaces-compatible-with-openai)
- [Explicit Qwen-Long Batch File prices](https://help.aliyun.com/zh/model-studio/qwen-long)
- [Successful and cancelled request billing](https://help.aliyun.com/zh/model-studio/batch-inference)

The current Beijing Batch File support list does not include `qwen-turbo`; its
historical Beijing discount is insufficient to admit new jobs. Singapore has a
different support list and prices and is not covered by this adapter.

An account needs an exact native model mapping and a verified Beijing endpoint.
`extra.qianwen_pricing_sync` must contain the operator's `usd_per_cny`,
`fx_source`, `fx_effective_at` (RFC3339), and `region=asia-pacific-china`.
No implicit exchange rate is used. At admission, the code's versioned tariff,
FX provenance, group/peak multiplier, subscription and key/account quota flags
are copied into `billing_snapshot`. The optional account extra record
`qianwen_file_batch_pricing` is an operator-visible catalog; it does not override
the validated tariff in code. Adding a model or changing the official tariff
requires a reviewed adapter/version update.

Example JSONL input (one homogeneous model and endpoint per file):

```json
{"custom_id":"item-1","method":"POST","url":"/v1/chat/completions","body":{"model":"qwen-long","messages":[{"role":"user","content":"Reply OK"}],"max_tokens":8}}
```

Upload using multipart `purpose=batch` and `file`, then submit:

```json
{"input_file_id":"<public-file-id>","endpoint":"/v1/chat/completions","completion_window":"24h"}
```

Inputs are bounded to 1 MiB / 100 unique custom IDs and results to 4 MiB. The
gateway stores normalized file hashes and custom-ID hashes, not request text.
Only allowlisted plain text parameters are forwarded. Unsupported dimensions,
unknown usage fields, nonzero cache/reasoning details, mismatched IDs/models,
duplicate results and inconsistent counts fail closed without settlement.

## Operations and ownership

| Request | Behavior |
| --- | --- |
| `POST /compatible-mode/v1/files` | Upload a validated input file; create a zero-cost audit anchor |
| `GET /compatible-mode/v1/files` | List owned input/output files; optional `purpose=batch` or `batch_output` |
| `GET /compatible-mode/v1/files/{id}` | Owned file metadata |
| `GET /compatible-mode/v1/files/{id}/content` | Integrity-checked remote content where DashScope permits download |
| `DELETE /compatible-mode/v1/files/{id}` | Remote deletion plus durable local tombstone; repeated deletion is successful |
| `POST /compatible-mode/v1/batches` | Create an account-pinned job using an owned input file |
| `GET /compatible-mode/v1/batches` | List owned jobs |
| `GET /compatible-mode/v1/batches/{id}` | Persisted status, refreshed by the existing polling worker |
| `POST /compatible-mode/v1/batches/{id}/cancel` | Request upstream cancellation once and continue polling final usage |

Lists use descending creation order, `limit=1..100` (default 20), public `after`
cursors and `has_more`. Output IDs are `output-<public-job-id>`; upstream IDs
are never returned. All operations require the same user, API key and group.
Lists, reads, cancellation and deletion remain usable without inference balance;
normal authentication and ownership checks still apply.

Input deletion is blocked while any associated batch is active. A parent row
lock serializes deletion against batch submission. Output deletion requires a
terminal job with a billing anchor so it cannot erase unsettled evidence.
Job records, fingerprints and billing anchors are retained after deletion.
There is no batch-record DELETE endpoint in this adapter.

## Settlement and recovery

Completed, cancelled and provider-expired jobs all validate successful output
rows before one task-level charge. The official completed count must match the
validated unique custom IDs. Failed/unstarted rows incur no charge; cancelled
jobs can therefore settle a validated subset. Zero completed rows receive a
zero-cost anchor. Public provider status is retained separately from internal
settlement status.

The row lock, usage-log insert, balance/subscription/quota effects and
`charged_usage_log_id` update share one transaction. Failed transactions roll
back together; repeated polling/settlement or process restart cannot charge an
anchored job again. Post-commit quota/cache invalidation retains the existing
gateway's behavior and crash window.

The submission claim precedes the network call. An ambiguous submission is never
automatically repeated. Replaying the same normalized input retains the first
account/job, including after a failure or deletion. Intentional new work must
change its custom IDs/input. Polling has a bounded 25-hour recovery window for a
24-hour batch; unresolved work after that point is not guessed or charged.

DashScope's documented content endpoint permits Batch output downloads. Input
download and remote deletion depend on upstream support; upstream failures are
reported rather than falsely claiming that remote content was deleted. Error
files, OSS input and arbitrary model families are not implemented here.

## Retired and diagnostic model policy

Qwen1.5 chat models were [retired on 2026-03-30](https://help.aliyun.com/zh/model-studio/rate-limit).
They are excluded from refreshed account catalogs, account model selection and
price imports, including aliases mapped to a retired native model.

`echo-model` has no verified official production price or callable Cursor model
in the inspected active account mappings. Do not create a public composite route
from a historical synthetic usage-log row alone. An operator must first supply
a live backing model and an explicit pricing policy.
