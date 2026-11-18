# Account compatibility

The connection ID is the native account ID. The adapter creates inactive accounts, checks scoped proxy identity, publishes confirmed configuration and enqueues native scheduler invalidation in the same transaction. Provider pools have independent membership. Existing account-group relationships continue through the native billing and access paths.

The v1 account wizard, authorization handlers, token refreshers, credential-secret handler and scheduler remain native readers/writers. BE-04 uses the same AES-GCM envelopes and current reference row. Migration 293 captures every credential version, including native writes. The browser authorization bridge uses the existing pending-ticket decoder and expiry/binding checks inside the credential installation transaction. The migration command and rollback sequence are in `contracts/migrations/BE-04/INTEGRATION.md`.
