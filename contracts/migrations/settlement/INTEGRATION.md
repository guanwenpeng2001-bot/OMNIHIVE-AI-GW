# Wallet accounting and settlement evidence

Apply migration 300 through the existing runner after settlement migration 295. The migration takes write-conflicting locks on `usage_settlement_receipts` and `users` in settlement order for the opening snapshot and trigger installation. Size the migration window using a representative user count and verify the snapshot before enabling consumers.

The existing receipt transaction remains the settlement authority. Its dedup key and historical fingerprint are reused. USD wallet openings and changes are captured from PostgreSQL numeric values in the wallet transaction. Each event produces equal and opposite postings. Sum the wallet side by funding account and currency to compare against `users.balance`. The wallet records the actual movement, including reservation refunds, while the reader exposes the amount on the settled command. These amounts can differ when a legacy balance cannot cover the charge.

Migration 300 records one outbox row per receipt transition to settled. The reader associates wallet events for the same funding account and PostgreSQL transaction. This relies on the current repository settling one receipt per transaction. Existing receipt history remains readable. Historical settlements before migration 300 have no wallet event IDs. The outbox is an integration surface for future consumers and currently accumulates rows.

The scoped reader consumes settlement journal views. An expired dispatched attempt without a receipt remains unconfirmed, with a null charge. A recorded zero token count remains distinguishable from missing observations. Pricing version is the version frozen at admission. Receipt rows and fingerprints retain their existing format.

## Pending integrations

- pricing must supply its final exact calculator and price snapshot to the gateway assemblers. This branch reads the existing frozen receipt for compatibility and does not switch paid pricing.
- The existing legacy wallet and quota mutations still use the repository command types. Cutover to an exact financial command, including the existing batch-image and quota callers, needs their owners' migration and mixed-binary parity checks.
- The reader covers journaled attempts and wallet postings. Budget rule projection, subscription postings, the public billing reader wiring and outbox delivery remain integration work.
- Use isolated test databases for fixtures that reset user sequences. Reusing user IDs against retained funding history correctly conflicts with the original account mapping.

## Verification

Point `UPSTREAM_JOB_TEST_DSN` at a disposable PostgreSQL database named `job_test` and run from `backend/`:

```sh
go test -tags=integration -count=1 -run '^TestBillingWalletPostgres$' ./internal/modules/billing/settlement
go test -tags=unit -count=1 \
  -run 'Test(UsageSettlementReceiptPostgres|KeyBudgetReservationPostgres|Settlement)' \
  ./internal/repository ./internal/service
```

Set `AFFINITY_REDIS_TEST_ADDR` to a disposable Redis instance to also run the settlement smoke script from the repository root.
