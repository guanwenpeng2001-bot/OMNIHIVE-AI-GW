# Exact value migration handoff

The fixtures were captured from the finite billing paths at commit
`0bd0cd5cba7fd02e81ff86e87092db4dcb05d542`. Each case in
[v1-replay.json](v1-replay.json) contains a frozen v1 pricing snapshot, the raw
command, the result of `UsageBillingCommand.Normalize`, and the exact pricing
inputs for a read-only shadow calculation. The half-tie uses the ten input and
five output tokens from `usage_billing_quantize_test.go`. The quoted FX case
uses the v1 `QuantizeQuoteBeforeMultiplier` rule for a rate of 0.01/7 and a
multiplier of 5.

| Case | Frozen actual | V1 charged | Exact charge | Observation |
| --- | ---: | ---: | ---: | --- |
| half-tie | 0.000078125 | 0.00007813 | 0.00007813 | Wallet and key quota receive the same charge. The exact upper-bound reservation is 0.00007813. |
| float-tail | 0.30000000000000004 | 0.30000000 | 0.30000000 | Three units at 0.1 differ before rounding by 4e-17. |
| quantized-quote | 0.00714285 | 0.00714285 | 0.00714286 | V1 rounds the quote to 0.00142857 before applying 5. Single-boundary rounding adds one ledger quantum. |
| free | 0 | 0.00000000 | 0.00000000 | A zero unit rate creates an explicitly free price snapshot. Replay retains the supplied historical fingerprint. |

## Read-only comparison

BE-07 can replay frozen rates, typed usage and their multipliers with
`Rate.Cost`, `Exact.Add` and `Exact.Multiply`. `Quantity.InTier` implements
the existing open-lower, closed-upper interval. Pass a nil lower bound to
include zero in the first native Qianwen tier. Accumulate elapsed time across
seconds, minutes and hours as exact ratios. Count dimensions match by unit.
`Exact.Diagnostic` renders 18 decimal places, and `Exact.Ratio` supplies an
independent numerator and denominator for the frozen evidence. The ratio
retains all digits when a diagnostic rounds to zero.

Read v1 JSON numbers into `json.Number` and call `ParseV1Number` on their
original decimal text. `CompareHistorical(proposed, frozen, posted)` reports
the signed raw difference and the signed eight-place charge difference. A
versioned pricing change needs a separately approved effective date. Use the
fixture's v1 charge and original fingerprint for historical receipt recovery.

BE-11 should bind each exact effect to its actual destination. For a legacy
wallet, pass `Wallet20_8.Bind(posted.Amount())` to the SQL parameter and use
`Wallet20_8.Scanner(&amount)` for numeric text on read. `Usage20_10`,
`Payment20_2`, `Ledger28_8` and `RateNumeric` cover the other column shapes.
A `BoundsError` carries precision and scale and matches `ErrBounds`.
Composite `Money`, `Rate` and `Quantity` implement SQL Scanner/Valuer for
JSON/JSONB records, preserving currency, unit and denominator. Their JSON shapes
also round-trip through versioned Redis records. A missing amount stays nullable
in its containing record.

## Caller inventory

Paths below are relative to `backend/internal`. BE-07 owns pricing comparisons.
BE-11 owns the receipt/effect writer transition and coordination with the BE-19
financial writers.

| Owner | Existing caller or boundary | Migration action |
| --- | --- | --- |
| BE-07 | `service/pricing_service.go`, `service/model_pricing_facts.go`, `service/channel.go`, `service/group.go`, `service/model_pricing_resolver.go` | Read raw price and multiplier text, freeze currency, dimension and per-unit denominator with the effective card. Preserve explicit free, missing rates, tier endpoints and native first-tier behavior. |
| BE-07 | `service/billing_service.go` (`CalculateCostUnified`, token/cache breakdowns and per-request, search, audio, video and image costs) | Compute each rate × quantity as an exact ratio, sum components, apply frozen multipliers, then call `RoundCharge` once. Track price changes through shadow deltas. |
| BE-07 | `service/gateway_usage_billing.go`, `service/openai_gateway_usage.go` | Move channel/group precedence, time bands, media and usage dimensions to the shared pricing resolver. Pass frozen prices into receipt preparation. |
| BE-07 | `service/billing_pricing_snapshot.go` (`freezePricing`, `RecalculatePricingSnapshot`, `FreezeUsagePricing`), `service/qianwen_file_batch_pricing.go`, `service/qianwen_realtime_billing.go`, `service/openai_realtime_billing.go`, `service/{qianwen_async,qianwen_video,openai_video}_jobs.go` | Retain v1 early rounding, customer/addition and account multiplier order for replay. Preserve rational evidence for new unit/FX conversions. |
| BE-11 | `service/gateway_request_plan.go` (`estimateBudgetHold`), `service/api_key_budget_service.go`, `repository/api_key_budget_repo.go` | V1 quantizes holds with the charge rule. Ceil new positive bounds at eight places and use the recorded hold when releasing or capturing it. |
| BE-11 | `service/usage_billing.go` (`Normalize`, `buildUsageBillingFingerprint`, `QuantizeUsageBillingAmount`) | V1 derives a missing SHA-256 fingerprint over five original float64 amounts formatted with ten decimals and the optional payload hash. It then rounds each command field. Preserve persisted fingerprints verbatim and dispatch by receipt version. |
| BE-11 | `service/usage_settlement_service.go`, `service/usage_settlement_receipt.go`, `repository/usage_settlement_receipt_repo.go` | Decode stored v1 commands and snapshots through their v1 paths. Use exact validation for new commands. Compare identity, frozen evidence and charged amount before applying effects or recovering. |
| BE-11 | `repository/usage_billing_repo.go`, `repository/user_platform_quota_events.go`, `service/usage_log.go`, `repository/api_key_budget_repo.go` | Bind the same approved eight-place charge for wallet, key and other effects with the same basis. Bind independently frozen account costs and ten-place historical usage at the destination scale. Preserve dedup and transaction ordering. |
| BE-11 | `service/billing_cache_service.go`, `repository/billing_cache.go`, financial fields in `handler/dto` and `pkg/usagestats` | Version string-valued cache/DTO payloads, retaining v1 readers and database authority for hard budgets. |
| BE-19 handoff | `payment/currency.go`, `payment/fee.go`, `service/payment_order.go`, `service/{payment,affiliate,subscription}_service.go` | Bind legacy two-place payment columns after the owning currency/provider rule. Preserve original minor units, fee policy and refund basis. |

BE-02 adds value types and fixtures only. The existing billing, gateway, cache,
database and payment callers continue through their current entry points.
