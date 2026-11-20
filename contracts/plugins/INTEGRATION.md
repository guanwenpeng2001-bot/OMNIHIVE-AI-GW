# BE-20 integration

Apply migration 309 before serving the extension inventory. PostgreSQL projects the immutable archive digest and signed predecessor mapping through `omnihive_extension_inventory`. The append-only `omnihive_extension_migrations` mapping retains source/target digests and signer identities after replacement or uninstall. V1 and v2 OpenAI OAuth bindings share one active database scope. Distinct plugins can hold their own UI and task bindings.

## BE-23 and console

Register `PluginHandler.ListExtensions` as `GET A/extensions` in the authenticated operator route group. Successful data has an `items` array with plugin ID, SDK version, publisher ID, signed capabilities, state and SHA-256. A pre-archive v1 row can lack a digest or publisher. Existing administrative upload, enable/disable, configuration, diagnostic and UI endpoints use the same manager. The backend multipart upload accepts both package extensions. Add `.ohplugin` to the console picker and its validation, and display the v2 capabilities and publisher. The signed manifest selects UI Bridge v1. Keep the current authenticated v1 message sources for imported UI assets during the compatibility window.

Distribute local `trust-v2.json` to each node before enabling a v2 package. An absent or revoked key fails closed on that node. The public policy grants are ceiling permissions by publisher and plugin ID. Installation creates disabled binding records. Operator activation and signed manifest scope are both required.

## BE-26 provider and task actions

Use `PluginManager.AuthorizeExtension(ctx, id, capability)` after project/account/credential authorization and before dispatch. It checks the enabled persisted installation and binding, signed manifest, current publisher grant, runtime identity and deadline. A successful decision grants one host-selected operation. Implement typed action requests under the v2 SDK and retain the gateway's budget reservation, credential lookup, egress allowlist/proxy, redirected-destination validation, metering and idempotent settlement. The guest must never choose a wallet, charge, credential, network destination or unrestricted callback. Existing OpenAI OAuth authorization already continues through the host's HTTP implementation. Task execution payloads and catalog visibility belong to BE-26.

BE-24 should retain the original and rebuilt artifact inventories and verify a real provider cohort before moving it from native v1. BE-30 can retire the verifier and legacy UI sources after the inventory and rollback window drain.
