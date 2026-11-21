# Typed provider and task actions

`GET /api/v2/admin/extensions` returns `authorized_actions` for each installation. The labels require an active reviewed grant for at least one account in its rollout, an exact artifact match, a running WASI instance and current publisher trust on this host. The signed declarations remain in `capabilities`. An empty list represents zero effective typed actions.

| Signed capability | Standard entry point | Guest request kind |
| --- | --- | --- |
| `provider.responses.v1` | HTTP Responses inference with a durable execution and budget admission | `PROVIDER_RESPONSES` |
| `task.image.generate.v1` | Image generation from an admitted native image task | `TASK_IMAGE_GENERATE` |

The first revision supports OpenAI OAuth and API-key accounts with project-owned credential references. The scoped catalog includes `verified_extension_actions` on matching, fresh endpoint capabilities for accessible offers when endpoint metadata is authorized. These labels represent recorded verification of the account/model tuple. Per-call availability also requires the live node's trust, runtime, caller credential and budget checks. The public catalog keeps its registry projection.

Sign each desired capability with its exact platform and account type in a v2 manifest. Include the same versioned ID in the publisher's trust grant and the runtime's capability handshake. Signers may combine the two typed actions in one package. Existing plugins `provider` and `task` grants apply only to their original operations.

The v2 WASI frame carries `operation="dispatch"` and `action.kind`. On acceptance, return a typed plan echoing that kind and choosing `HOST_DEFAULT` or `IDENTITY`. The latter asks the host to use an uncompressed response if it has no existing encoding preference. Authorization and dispatch run under one child deadline of at most one second. Two optional planners may run at once. Each receives a fresh, bounded WASI instance. The host discards invalid, rejected or late plans and forwards the independently admitted native request once. The plugins global provider gate still enforces its existing policy when enabled.

The standard gateway selects the credential, proxy and destination. The host performs the generation attempt, applies audit public-address and redirect policy to the selected action, reads provider usage and settles under the existing execution identity. All task input, objects, result locators and downloads stay with tasks. The structured `extension_action` audit event records numeric actor, scope, installation and account IDs plus the action and outcome. The guest sees neither that event nor the request content.
