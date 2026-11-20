# Identity allowlist

This is a snapshot of matching lines in `backend`, `frontend`, `deploy`, `docs`, and `.github` from the identity sweep based on `12b753875`. Counts refer to matching lines, including when one line contains multiple matches. File and line references describe this worktree and can move as other packages merge. The line entries below retain every classified location. The allowlist itself is excluded so its file references only count once.

The root [NOTICE](../../NOTICE) credits the inherited source alongside the existing [LICENSE](../../LICENSE). These root files are outside the scoped counts.

| Classification | Matching lines |
| --- | ---: |
| License / attribution | 2 |
| Persisted compatibility | 495 |
| Historical docs | 126 |
| Defect | 14 |
| **Total** | **637** |

## License / attribution

| File | Lines | Reason |
| --- | --- | --- |
| `backend/internal/payment/provider/easypay_notify_security_test.go` | 4 | Original source or license reference |
| `backend/internal/pkg/cursor/nal.go` | 11 | Original source or license reference |

## Persisted compatibility

| File | Lines | Reason |
| --- | --- | --- |
| `backend/cmd/server/main.go` | 198 | [runtime environment fallbacks](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/config/config.go` | 1837 | [PostgreSQL settings and explicit legacy fixtures](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/config/config.go` | 2410 | [Redis and SQL namespaces](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/config/config_test.go` | 33–37, 39, 1295 | [PostgreSQL settings and explicit legacy fixtures](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/config/config_test.go` | 1075–1076 | [Redis and SQL namespaces](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/config/webauthn_test.go` | 23–24, 45, 56–57, 68–69 | [explicit caller path, origin or RP fixture](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/domain/platform_capabilities_test.go` | 13 | [test-only hooks and CI fallbacks](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/handler/admin/account_data.go` | 23–24 | [account archives](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/handler/admin/plugin_handler_test.go` | 127, 135 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/internal/handler/admin/subscription_bulk_live_extended_test.go` | 110, 113, 117, 180 | [test-only hooks and CI fallbacks](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/handler/admin/subscription_bulk_live_test.go` | 56, 371, 375 | [test-only hooks and CI fallbacks](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/handler/auth_dingtalk_oauth_test.go` | 108, 120 | [pending identity tickets](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/handler/auth_email_oauth_test.go` | 64, 123, 175, 228 | [pending identity tickets](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/handler/auth_linuxdo_oauth_test.go` | 414, 499, 564, 637, 701, 830, 896, 985 | [pending identity tickets](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/handler/auth_oidc_oauth_test.go` | 329, 396, 446, 512, 553, 613, 703, 997, 1062, 1111 | [pending identity tickets](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/handler/auth_wechat_oauth_test.go` | 147, 208, 286, 365, 574, 673, 764, 844, 904, 928, 1159, 1376 | [pending identity tickets](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/handler/gateway_generation_idempotency.go` | 134 | [error, tool and header contracts](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/handler/gateway_key_billing.go` | 34, 89 | [relay billing](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/handler/gateway_key_billing_test.go` | 58, 85 | [relay billing](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/handler/openrouter_reasoning_replay_test.go` | 359, 439 | [signature domains and stable IDs](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/handler/passkey_handler_test.go` | 121–122 | [explicit caller path, origin or RP fixture](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/handler/security_audit_errors.go` | 119 | [error, tool and header contracts](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/handler/security_audit_errors_test.go` | 131 | [error, tool and header contracts](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/payment/wire.go` | 39 | [payment encryption and signature domains](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/pkg/claude/cli_version.go` | 27 | [runtime environment fallbacks](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/pkg/runtimeenv/identity.go` | 16 | [deployment paths and downgrade support](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/pkg/xai/oauth.go` | 590 | [xAI OAuth](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/pkg/xai/oauth_test.go` | 91 | [xAI OAuth](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/repository/backup_pg_dumper_test.go` | 31, 33 | [PostgreSQL settings and explicit legacy fixtures](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/repository/dashboard_cache.go` | 21 | [Redis and SQL namespaces](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/repository/external_test_resources_test.go` | 30, 32 | [PostgreSQL settings and explicit legacy fixtures](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/repository/grok_oauth_client.go` | 69, 101 | [xAI OAuth](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/repository/integration_fixtures_test.go` | 74 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/internal/repository/integration_harness_test.go` | 188 | [test-only hooks and CI fallbacks](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/repository/plugin_repo.go` | 77, 100, 102–103, 127, 133, 135, 152, 171, 174, 191, 210, 243, 268, 273, 288, 316 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/internal/repository/plugin_repo_integration_test.go` | 21 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/internal/repository/usage_dashboard_coverage.go` | 220 | [Redis and SQL namespaces](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/repository/usage_dashboard_writer_lock_test.go` | 227 | [Redis and SQL namespaces](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/securityaudit/prompt_snapshot.go` | 24 | [prompt audit](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/securityaudit/prompt_types.go` | 12–13 | [prompt audit](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/server/middleware/admin_auth.go` | 18 | [admin WebSocket](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/server/middleware/admin_auth_test.go` | 119, 135–136 | [admin WebSocket](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/server/middleware/api_key_auth.go` | 35, 238, 285 | [relay billing](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/server/middleware/api_key_auth_test.go` | 1310, 1340, 1545 | [relay billing](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/server/middleware/gateway_protocol.go` | 25 | [relay billing](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/server/middleware/jwt_auth_test.go` | 295 | [admin WebSocket](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/server/routes/gateway.go` | 194, 354–355 | [relay billing](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/server/routes/gateway_key_billing_test.go` | 116, 121, 129, 139, 151, 158 | [relay billing](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/server/routes/gateway_protocol_matrix_test.go` | 220 | [relay billing](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/service/admin_compliance.go` | 142 | [existing compliance copy or saved display settings](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/service/aliyun_captcha_service.go` | 77 | [serialized marker or replay parser fixture](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/antigravity_gateway_compat_test.go` | 422 | [error, tool and header contracts](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/service/auth_identity_tickets.go` | 15 | [pending identity tickets](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/channel_monitor_service.go` | 93 | [duplicate operation metadata](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/cursor_billing_probe_test.go` | 23 | [relay billing](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/service/cursor_gateway_compat.go` | 423 | [Cursor usage source](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/service/cursor_gateway_m3_test.go` | 190 | [Cursor usage source](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/service/cursor_gateway_service_test.go` | 222 | [Cursor usage source](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/service/gateway_service.go` | 912–914 | [runtime environment fallbacks](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/grok_media.go` | 928 | [upstream compatibility rationale](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/service/grok_media_content_test.go` | 223 | [upstream compatibility rationale](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/service/openai_codex_account_identity.go` | 100 | [signature domains and stable IDs](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/openai_codex_fingerprint.go` | 239, 247, 257 | [signature domains and stable IDs](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/openai_codex_tool_names.go` | 15 | [error, tool and header contracts](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/service/openai_codex_tool_names_test.go` | 69, 105, 108, 111, 117–118, 139, 157, 162, 167, 212, 214, 216, 221, 226, 228, 231, 243 | [error, tool and header contracts](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/service/openai_codex_transform.go` | 138, 140 | [serialized marker or replay parser fixture](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/openai_codex_transform.go` | 121, 139, 141 | [signature domains and stable IDs](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/openai_compact_probe.go` | 163, 165 | [signature domains and stable IDs](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/openai_compact_stream_bridge_test.go` | 337 | [upstream compatibility rationale](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/service/openai_compat_model_test.go` | 684, 730, 784, 889, 1056, 1445, 2331 | [serialized marker or replay parser fixture](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/openai_endpoint_url_test.go` | 20 | [relay billing](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/service/openai_gateway_chat_completions.go` | 46 | [upstream compatibility rationale](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/service/openai_gateway_grok.go` | 654 | [signature domains and stable IDs](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/openai_gateway_grok_cache.go` | 19 | [error, tool and header contracts](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/service/openai_gateway_grok_test.go` | 98 | [signature domains and stable IDs](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/openai_gateway_service_test.go` | 3181 | [serialized marker or replay parser fixture](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/openai_images.go` | 51 | [runtime environment fallbacks](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/openai_images_model_test.go` | 22, 62 | [runtime environment fallbacks](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/openai_live_attestation.go` | 29 | [signature domains and stable IDs](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/openai_messages_todo_guard.go` | 11–12 | [serialized marker or replay parser fixture](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/openai_nonstreaming_terminal_failure_failover_test.go` | 16 | [upstream compatibility rationale](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/service/openai_oauth_passthrough_test.go` | 230 | [serialized marker or replay parser fixture](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/openrouter_google_continuation.go` | 19 | [signature domains and stable IDs](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/openrouter_google_continuation_test.go` | 92 | [signature domains and stable IDs](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/passkey_test.go` | 78–79 | [explicit caller path, origin or RP fixture](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/payment_resume_service_test.go` | 853 | [payment encryption and signature domains](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/plugin_compatibility.go` | 22–24 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/internal/service/plugin_compatibility_test.go` | 20, 26, 44 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/internal/service/plugin_manager.go` | 33, 842 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/internal/service/plugin_manifest.go` | 51–53, 63, 95–97 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/internal/service/plugin_package.go` | 27, 29 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/internal/service/plugin_package_test.go` | 181–183 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/internal/service/plugin_runtime_integration_test.go` | 27, 29 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/internal/service/plugin_security_regression_test.go` | 86, 88 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/internal/service/update_service.go` | 35 | [deployment paths and downgrade support](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/update_service_test.go` | 267, 310, 324, 326, 357, 371, 377 | [deployment paths and downgrade support](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/service/upstream_billing_probe.go` | 46–47, 240, 653, 804, 994–995, 998, 1022–1023, 1029, 1163–1164 | [relay billing](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/service/upstream_billing_probe_multiplatform_test.go` | 15, 74, 111, 122, 142, 296, 358 | [relay billing](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/service/upstream_billing_probe_test.go` | 180, 295, 333, 372, 517, 563, 588, 640, 669, 677, 685, 775 | [relay billing](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/service/upstream_models_discovery_test.go` | 290 | [relay billing](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `backend/internal/setup/setup_test.go` | 281, 283, 290 | [PostgreSQL settings and explicit legacy fixtures](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/testutil/testprocess/client.go` | 20 | [test-only hooks and CI fallbacks](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/internal/testutil/testresources/external.go` | 22 | [test-only hooks and CI fallbacks](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/migrations/229_plugins.sql` | 5, 25, 27, 31, 33, 41–42, 45–46, 48–49, 52–53 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/migrations/230_plugin_artifacts.sql` | 3 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/migrations/258_usage_dashboard_coverage.sql` | 43 | [Redis and SQL namespaces](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/migrations/278_usage_lineage_convergence.sql` | 30 | [Redis and SQL namespaces](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/migrations/279_usage_history_origin.sql` | 34 | [Redis and SQL namespaces](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/migrations/plugins_migration_test.go` | 15–16, 18–19, 32 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/migrations/usage_coverage_upgrade_integration_test.go` | 83, 150, 157 | [Redis and SQL namespaces](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `backend/pkg/pluginapi/README.md` | 53–55 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/pkg/pluginapi/docs/package-format.md` | 24–26 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/pkg/pluginapi/docs/ui-bridge.md` | 21, 32, 43 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/pkg/pluginapi/v1/manifest.schema.json` | 3, 19, 24–26 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/pkg/pluginapi/v1/plugin.pb.go` | 1124, 1165, 1177, 1179, 1191, 1195, 1205, 1208–1209, 1212–1215, 1217–1218, 1234–1252, 1255–1274 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/pkg/pluginapi/v1/plugin.proto` | 3 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/pkg/pluginapi/v1/plugin_grpc.pb.go` | 22–27, 273 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `backend/pkg/pluginapi/v1/runtime.go` | 24–25 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `deploy/apple-container.sh` | 6, 9, 22, 206, 221, 451, 455, 483, 585, 598–599, 907, 1001–1002 | [deployment paths and downgrade support](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `deploy/docker-compose.dev.yml` | 75–78, 83 | [deployment paths and downgrade support](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `deploy/docker-compose.local.yml` | 181 | [deployment paths and downgrade support](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `deploy/docker-compose.standalone.yml` | 133 | [deployment paths and downgrade support](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `deploy/docker-compose.yml` | 67, 181 | [deployment paths and downgrade support](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `deploy/docker-entrypoint.sh` | 18, 27–28 | [deployment paths and downgrade support](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `deploy/install.sh` | 40–42, 614, 626–627, 657, 700, 720 | [deployment paths and downgrade support](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `deploy/tests/apple-container-test.sh` | 108, 111–112 | [deployment paths and downgrade support](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `deploy/tests/docker-compose-gateway-env-test.sh` | 7 | [deployment paths and downgrade support](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `deploy/tests/fixtures/bin/container` | 40–41, 58–59 | [deployment paths and downgrade support](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `deploy/tests/install-pricing-resources-test.sh` | 41–43, 46, 49, 51 | [deployment paths and downgrade support](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `docs/PLUGIN_DEVELOPMENT.md` | 91, 194 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `docs/gateway-platform-protocol-matrix.md` | 396, 415 | [relay billing](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `docs/operations/identity-migration.md` | 3, 9, 19, 27, 29, 33 | [deployment paths and downgrade support](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `docs/safe-account-import.md` | 15 | [account archives](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `frontend/src/api/__tests__/admin.channelMonitor.duplicate.spec.ts` | 82, 85 | [browser storage or cross-tab coordination](../../contracts/migrations/BE-30/README.md#frontend-d08-keys) |
| `frontend/src/api/__tests__/admin.groups.duplicate.spec.ts` | 82, 85 | [browser storage or cross-tab coordination](../../contracts/migrations/BE-30/README.md#frontend-d08-keys) |
| `frontend/src/api/__tests__/ops-ws-ticket.spec.ts` | 39, 45 | [admin WebSocket](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `frontend/src/api/__tests__/passkey.spec.ts` | 83, 131 | [explicit caller path, origin or RP fixture](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `frontend/src/api/__tests__/url.spec.ts` | 12 | [explicit caller path, origin or RP fixture](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `frontend/src/api/admin/accounts.ts` | 190 | [browser storage or cross-tab coordination](../../contracts/migrations/BE-30/README.md#frontend-d08-keys) |
| `frontend/src/api/admin/channelMonitor.ts` | 270 | [browser storage or cross-tab coordination](../../contracts/migrations/BE-30/README.md#frontend-d08-keys) |
| `frontend/src/api/admin/groups.ts` | 167 | [browser storage or cross-tab coordination](../../contracts/migrations/BE-30/README.md#frontend-d08-keys) |
| `frontend/src/api/admin/ops.ts` | 460, 506 | [admin WebSocket](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `frontend/src/api/admin/plugins.ts` | 13–15, 41–43 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `frontend/src/api/tokenRefresh.ts` | 9 | [browser storage or cross-tab coordination](../../contracts/migrations/BE-30/README.md#frontend-d08-keys) |
| `frontend/src/components/account/UpstreamBillingRateCell.vue` | 109 | [relay billing](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `frontend/src/components/account/__tests__/UpstreamBillingRateCell.spec.ts` | 46 | [relay billing](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `frontend/src/components/admin/AdminComplianceDialog.vue` | 145 | [existing compliance copy or saved display settings](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `frontend/src/components/admin/account/ImportDataModal.vue` | 227 | [account archives](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `frontend/src/components/admin/subscription/__tests__/bulkSubscriptionOperation.spec.ts` | 33 | [browser storage or cross-tab coordination](../../contracts/migrations/BE-30/README.md#frontend-d08-keys) |
| `frontend/src/components/admin/subscription/bulkSubscriptionOperation.ts` | 53 | [browser storage or cross-tab coordination](../../contracts/migrations/BE-30/README.md#frontend-d08-keys) |
| `frontend/src/constants/__tests__/platforms.spec.ts` | 25 | [test-only hooks and CI fallbacks](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |
| `frontend/src/i18n/__tests__/localeLifecycle.spec.ts` | 34, 62 | [browser storage or cross-tab coordination](../../contracts/migrations/BE-30/README.md#frontend-d08-keys) |
| `frontend/src/i18n/index.ts` | 8 | [browser storage or cross-tab coordination](../../contracts/migrations/BE-30/README.md#frontend-d08-keys) |
| `frontend/src/types/admin.ts` | 454 | [relay billing](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `frontend/src/utils/__tests__/ipGeoLookup.spec.ts` | 220, 236, 256, 264 | [browser storage or cross-tab coordination](../../contracts/migrations/BE-30/README.md#frontend-d08-keys) |
| `frontend/src/utils/ipGeoLookup.ts` | 24 | [browser storage or cross-tab coordination](../../contracts/migrations/BE-30/README.md#frontend-d08-keys) |
| `frontend/src/views/HomeView.vue` | 1263 | [existing compliance copy or saved display settings](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `frontend/src/views/admin/PluginsView.vue` | 66, 570, 581, 592 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `frontend/src/views/admin/__tests__/PluginsView.spec.ts` | 75, 105–107, 156 | [v1 contract, signature, storage or test](../../contracts/migrations/BE-30/README.md#be-20-plugin-handoff) |
| `frontend/src/views/auth/LoginView.vue` | 222 | [browser storage or cross-tab coordination](../../contracts/migrations/BE-30/README.md#frontend-d08-keys) |
| `frontend/src/views/auth/RegisterView.vue` | 423 | [browser storage or cross-tab coordination](../../contracts/migrations/BE-30/README.md#frontend-d08-keys) |
| `frontend/src/views/public/LegalDocumentView.vue` | 73, 129, 254 | [existing compliance copy or saved display settings](../../contracts/migrations/BE-30/README.md#protocol-retirement-candidates) |
| `frontend/src/views/user/BatchImageGuideView.vue` | 811 | [browser storage or cross-tab coordination](../../contracts/migrations/BE-30/README.md#frontend-d08-keys) |
| `frontend/src/views/user/__tests__/KeysView.spec.ts` | 635 | [explicit caller path, origin or RP fixture](../../contracts/migrations/BE-30/README.md#persisted-cryptographic-and-deployment-identities) |

## Historical docs

| File | Lines | Reason |
| --- | --- | --- |
| `backend/internal/handler/admin/ops_runtime_logging_handler_test.go` | 91 | Test-only example, retained configuration fixture or regression note |
| `backend/internal/handler/admin/redeem_handler.go` | 181 | Historical source comment or compatibility rationale |
| `backend/internal/handler/openai_gateway_compact_log_test.go` | 90 | Test-only example, retained configuration fixture or regression note |
| `backend/internal/integration/e2e_user_flow_test.go` | 252 | Test-only example, retained configuration fixture or regression note |
| `backend/internal/pkg/claude/cli_version.go` | 17 | Historical source comment or compatibility rationale |
| `backend/internal/pkg/logger/logger_test.go` | 20, 46, 121, 164 | Test-only example, retained configuration fixture or regression note |
| `backend/internal/pkg/logger/stdlog_bridge_test.go` | 66, 128 | Test-only example, retained configuration fixture or regression note |
| `backend/internal/pkg/openai/request.go` | 47 | Historical source comment or compatibility rationale |
| `backend/internal/pkg/openai_compat/upstream_capability.go` | 3 | Historical source comment or compatibility rationale |
| `backend/internal/repository/BILLING_LOCK_ORDER.md` | 67 | Archived comparison or verification note |
| `backend/internal/repository/account_repo_test.go` | 21 | Test-only example, retained configuration fixture or regression note |
| `backend/internal/repository/external_test_resources_test.go` | 47 | Test-only example, retained configuration fixture or regression note |
| `backend/internal/repository/integration_harness_test.go` | 102 | Test-only example, retained configuration fixture or regression note |
| `backend/internal/repository/setting_repo_integration_test.go` | 125, 139 | Test-only example, retained configuration fixture or regression note |
| `backend/internal/server/middleware/request_access_logger_test.go` | 48 | Test-only example, retained configuration fixture or regression note |
| `backend/internal/service/composite_platform.go` | 151 | Historical source comment or compatibility rationale |
| `backend/internal/service/crs_sync_service.go` | 286, 457, 581, 745, 892, 1030 | Historical source comment or compatibility rationale |
| `backend/internal/service/domain_constants.go` | 566 | Historical source comment or compatibility rationale |
| `backend/internal/service/gemini_oauth_service.go` | 22 | Historical source comment or compatibility rationale |
| `backend/internal/service/identity_service.go` | 42 | Historical source comment or compatibility rationale |
| `backend/internal/service/identity_service_user_agent_validation_test.go` | 69 | Test-only example, retained configuration fixture or regression note |
| `backend/internal/service/image_output_accounting_test.go` | 32–35, 55 | Test-only example, retained configuration fixture or regression note |
| `backend/internal/service/openai_compact_stream_bridge_test.go` | 212 | Test-only example, retained configuration fixture or regression note |
| `backend/internal/service/openai_first_output_timeout_test.go` | 297 | Test-only example, retained configuration fixture or regression note |
| `backend/internal/service/openai_gateway_count_tokens_test.go` | 330, 340 | Test-only example, retained configuration fixture or regression note |
| `backend/internal/service/openai_gateway_grok_cache.go` | 99, 215, 224 | Historical source comment or compatibility rationale |
| `backend/internal/service/openai_gateway_passthrough.go` | 1831, 2372 | Historical source comment or compatibility rationale |
| `backend/internal/service/openai_gateway_response_handling.go` | 1657 | Historical source comment or compatibility rationale |
| `backend/internal/service/openai_oauth_passthrough_test.go` | 369 | Test-only example, retained configuration fixture or regression note |
| `backend/internal/service/ops_log_runtime_test.go` | 132, 203, 314, 371, 537 | Test-only example, retained configuration fixture or regression note |
| `backend/internal/service/pricing_service.go` | 723 | Historical source comment or compatibility rationale |
| `backend/internal/service/ratelimit_service.go` | 2398 | Historical source comment or compatibility rationale |
| `backend/internal/service/upstream_request_id_test.go` | 13–14 | Test-only example, retained configuration fixture or regression note |
| `backend/migrations/001_init.sql` | 1 | Immutable applied SQL migration or its comments |
| `backend/migrations/002_account_type_migration.sql` | 1 | Immutable applied SQL migration or its comments |
| `backend/migrations/003_subscription.sql` | 1 | Immutable applied SQL migration or its comments |
| `backend/migrations/038_ops_errors_resolution_retry_results_and_standardize_classification.sql` | 74, 77 | Immutable applied SQL migration or its comments |
| `backend/migrations/052_migrate_upstream_to_apikey.sql` | 3 | Immutable applied SQL migration or its comments |
| `backend/migrations/202_channel_monitor_v2_full_table_permissions.sql` | 3 | Immutable applied SQL migration or its comments |
| `backend/migrations/229_plugins.sql` | 3 | Immutable applied SQL migration or its comments |
| `backend/migrations/cursor_platform_integration_test.go` | 34 | Test-only example, retained configuration fixture or regression note |
| `backend/migrations/new_platforms_integration_test.go` | 45 | Test-only example, retained configuration fixture or regression note |
| `docs/account-model-access.md` | 7, 116 | Archived worktree, integration or retirement reference |
| `docs/model-registry-contract.md` | 7, 237 | Archived worktree, integration or retirement reference |
| `frontend/src/api/__tests__/settings.authSourceDefaults.spec.ts` | 102, 165 | Test-only example, retained configuration fixture or regression note |
| `frontend/src/components/account/EditAccountModal.vue` | 5444 | Historical source comment or compatibility rationale |
| `frontend/src/components/auth/__tests__/WechatOAuthSection.spec.ts` | 65 | Test-only example, retained configuration fixture or regression note |
| `frontend/src/components/user/profile/__tests__/ProfileIdentityBindingsSection.spec.ts` | 251 | Test-only example, retained configuration fixture or regression note |
| `frontend/src/components/user/profile/__tests__/totp-timer-cleanup.spec.ts` | 62 | Test-only example, retained configuration fixture or regression note |
| `frontend/src/router/__tests__/feature-access.spec.ts` | 25 | Test-only example, retained configuration fixture or regression note |
| `frontend/src/router/__tests__/wechat-route.spec.ts` | 11 | Test-only example, retained configuration fixture or regression note |
| `frontend/src/utils/__tests__/ccswitchImport.antigravityUrl.spec.ts` | 9, 14, 18 | Test-only example, retained configuration fixture or regression note |
| `frontend/src/utils/__tests__/ccswitchImport.spec.ts` | 17, 149 | Test-only example, retained configuration fixture or regression note |
| `frontend/src/views/__tests__/HelpGuide.spec.ts` | 223 | Test-only example, retained configuration fixture or regression note |
| `frontend/src/views/__tests__/KeyUsageView.spec.ts` | 128 | Test-only example, retained configuration fixture or regression note |
| `frontend/src/views/admin/ops/utils/opsFormatters.ts` | 4 | Archived comparison or verification note |

## Defect

FE-WP1 owns the logo, shell bootstrap and router, and setup wizard. The wizard controls the initial database name and placeholder. D08 storage keys retain their existing identifiers until the adapter migration.

FE-WP4 owns the account export filename and account page copy. The proxy surfaces link to the OmniHive deployment guide. Account import continues accepting the historical discriminators during archive migration.

| File | Lines | Reason |
| --- | --- | --- |
| `frontend/public/logo.svg` | 2 | FE-WP1 shell copy |
| `frontend/src/components/account/CreateAccountModal.vue` | 1459, 1757 | FE-WP4 account labels and export filename |
| `frontend/src/components/account/EditAccountModal.vue` | 498, 947 | FE-WP4 account labels and export filename |
| `frontend/src/main.ts` | 48 | FE-WP1 shell copy |
| `frontend/src/router/index.ts` | 5 | FE-WP1 shell copy |
| `frontend/src/views/admin/AccountsView.vue` | 339, 395, 2369 | FE-WP4 account labels and export filename |
| `frontend/src/views/setup/SetupWizardView.vue` | 17–18, 144, 560 | FE-WP1 setup/auth copy and fixtures |

## Machine-readable gate entries

The table above is the review inventory. `scripts/independence-check.sh` uses
the following path/token expressions for the protected product-source scan.
Documentation, tests, migrations and generated descriptors are outside that
scan. A line passes only when both its path and content match an entry.

Format: `path-regex <TAB> line-regex <TAB> reason`. Matching is case-insensitive
for the line expression. Add an entry only for attribution or a persisted or
negotiated compatibility value.

```
^backend/internal/modules/runtime/mode\.go$	"compatible-mode", "sub2api", "files"	BE-22 gateway-mode dispatch for the retained /sub2api/billing relay alias
^backend/internal/legacy/retirement/catalog\.tsv$	sub2api	BE-30 read-only inventory of persisted or negotiated legacy identities
^backend/internal/legacy/retirement/probes\.go$	public\.sub2api_plugin_installations	BE-30 read-only count of installed legacy plugin artifacts
^backend/internal/pkg/cursor/nal\.go$	Wei-Shaw/sub2api	upstream protocol provenance
^backend/internal/payment/provider/easypay_notify_security_test\.go$	Wei-Shaw/sub2api	test provenance
^backend/\.golangci\.yml$	Wei-Shaw/sub2api	legacy module path attribution
^backend/	SUB2API_[A-Z0-9_]+	legacy environment aliases
^backend/internal/payment/wire\.go$	sub2api/payment/encryption	persisted payment derivation key
^backend/internal/pkg/runtimeenv/identity\.go$	sub2api	legacy environment and directory compatibility
^backend/internal/config/	SUB2API_	legacy configuration aliases
^backend/internal/setup/	SUB2API_	legacy setup aliases
^backend/internal/server/	urn:sub2api:identity-ticket	persisted identity ticket namespace
^backend/internal/service/auth_identity_tickets\.go$	urn:sub2api:identity-ticket	persisted identity ticket namespace
^backend/internal/service/domain_constants\.go$	sub2api	persisted domain/cache namespace
^backend/internal/service/plugin_	(?:sub2api|Sub2API)	plugin manifest and compatibility keys
^backend/internal/service/plugin_manager\.go$	(?:sub2api|Sub2API)	plugin compatibility keys
^backend/internal/service/update_service\.go$	sub2api	legacy release compatibility
^backend/internal/repository/plugin_repo\.go$	sub2api_plugin_	persisted plugin table names
^backend/internal/repository/.*\.go$	sub2api\.(?:usage_retention|key_billing|ip-geo-cache)|sub2api:(?:ip-geo-cache|admin:)	persisted database and cache keys
^backend/internal/handler/.*\.go$	/v1/sub2api/billing	legacy billing endpoint compatibility
^backend/internal/server/.*\.go$	/v1/sub2api/billing	legacy billing endpoint compatibility
^backend/internal/securityaudit/	sub2api	persisted audit namespaces
^backend/internal/repository/BILLING_LOCK_ORDER\.md$	sub2api	persisted lock-order documentation
^backend/internal/pkg/claude/	sub2api	legacy client configuration
^backend/internal/pkg/cursor/	(?:sub2api|Sub2API)	legacy client configuration
^backend/internal/pkg/ip/	sub2api	persisted cache key
^backend/internal/pkg/logger/	sub2api	legacy log file compatibility
^backend/internal/pkg/openai/	sub2api	persisted client configuration
^backend/internal/pkg/openai_compat/	sub2api	persisted client configuration
^backend/internal/pkg/xai/	sub2api	legacy upstream identity compatibility
^backend/internal/platform/	sub2api	persisted platform identity
^backend/pkg/pluginapi/	(?:Sub2API|sub2api)	plugin protocol compatibility
^backend/internal/handler/.*\.go$	X-Sub2API-	negotiated legacy upstream headers
^backend/internal/handler/.*\.go$	sub2api-claude-code-todo-guard	legacy upstream tool namespace
^backend/internal/service/.*\.go$	X-Sub2API-	negotiated legacy upstream headers
^backend/internal/service/.*\.go$	sub2api-grok-oauth	legacy upstream user agent
^backend/internal/service/.*\.go$	(?:Sub2API|sub2api)	persisted routing, plugin and provider compatibility values
^backend/internal/handler/.*\.go$	(?:Sub2API|sub2api)	project key and protocol compatibility values
^backend/internal/server/.*\.go$	(?:Sub2API|sub2api)	persisted gateway protocol compatibility values
^backend/internal/repository/.*\.go$	(?:Sub2API|sub2api)	persisted storage compatibility values
^backend/internal/config/.*\.go$	(?:Sub2API|sub2api)	legacy configuration compatibility values
^backend/internal/testutil/	sub2api	test resource compatibility
^deploy/	(?i)sub2api	deployment compatibility
^\.github/	SUB2API_	CI compatibility aliases
^Makefile$	sub2api	legacy test environment documentation
^backend/scripts/	sub2api	legacy test harness compatibility
^frontend/src/api/admin/	sub2api:admin:	persisted browser storage and dedupe keys
^frontend/src/api/admin/	sub2api-admin	negotiated admin websocket protocol
^frontend/src/api/admin/plugins\.ts$	sub2api	plugin manifest compatibility keys
^frontend/src/components/admin/subscription/	sub2api:admin:	persisted browser storage key
^frontend/src/components/admin/account/ImportDataModal\.vue$	sub2api-(?:data|bundle)	legacy import format
^frontend/src/components/account/	sub2api	legacy account help and probe text
^frontend/src/components/intro/	(?:Sub2API|sub2api|SUB2API_)	legacy shell examples
^frontend/src/components/keys/UseKeyModal\.vue$	SUB2API_	legacy client configuration examples
^frontend/src/components/keys/UseKeyModal\.vue$	(?:Sub2API|sub2api)	legacy client configuration examples
^frontend/src/components/modelPlaza/PlazaNavBar\.vue$	sub2api	legacy site-name scrub
^frontend/src/components/admin/AdminComplianceDialog\.vue$	Sub2API	legacy compliance document scrub
^frontend/src/components/admin/TLSFingerprintProfilesModal\.vue$	tls\.sub2api\.org	legacy fingerprint reference link
^frontend/src/components/common/ProxyAdBanner\.vue$	sub2api\.io	legacy proxy reference link
^frontend/src/components/account/UpstreamBillingRateCell\.vue$	sub2api	legacy upstream capability text
^frontend/src/components/layout/README\.md$	Sub2API	historical component documentation
^frontend/src/i18n/	(?:Sub2API|sub2api)	translated legacy product labels
^frontend/src/api/tokenRefresh\.ts$	sub2api	persisted browser refresh lock name
^frontend/index\.html$	Sub2API	existing document title
^frontend/public/logo\.svg$	Sub2API	existing asset title
^frontend/src/api/index\.ts$	Sub2API	historical file header
^frontend/src/api/admin/groups\.ts$	Sub2API	historical comment
^frontend/src/api/admin/ops\.ts$	sub2api-admin	negotiated admin websocket protocol
^frontend/src/main\.ts$	Sub2API	default site-name compatibility
^frontend/src/router/	(?:Sub2API|sub2api)	historical router documentation and fallback title
^frontend/src/stores/	(?:Sub2API|sub2api)	legacy site-name and compliance fallbacks
^frontend/src/types/admin\.ts$	sub2api\.key_billing	persisted billing object name
^frontend/src/utils/ipGeoLookup\.ts$	sub2api:ip-geo-cache	persisted cache key
^frontend/src/views/HomeView\.vue$	(?:Sub2API|sub2api)	legacy site-name scrub and default copy
^frontend/src/views/KeyUsageView\.vue$	sub2api	legacy site-name scrub
^frontend/src/views/admin/AccountsView\.vue$	(?:Sub2API|sub2api)	legacy export name and copy scrub
^frontend/src/views/admin/GroupsView\.vue$	sub2api	legacy copy scrub
^frontend/src/views/admin/PluginsView\.vue$	(?:Sub2API|sub2api)	plugin protocol compatibility
^frontend/src/views/admin/ops/utils/opsFormatters\.ts$	docs/sub2api	historical design reference
^frontend/src/views/admin/settings/PaymentSettings\.vue$	(?:Sub2API|sub2api)	legacy payment defaults
^frontend/src/views/admin/settings/useGeneralSettingsForm\.ts$	Sub2API	legacy site-name default
^frontend/src/views/auth/EmailVerifyView\.vue$	Sub2API	legacy site-name fallback
^frontend/src/views/auth/LoginView\.vue$	sub2api	persisted agreement storage key
^frontend/src/views/auth/RegisterView\.vue$	sub2api	persisted agreement storage key
^frontend/src/views/auth/.*\.md$	Sub2API	historical authentication documentation
^frontend/src/views/public/LegalDocumentView\.vue$	sub2api	legacy document scrub
^frontend/src/views/setup/SetupWizardView\.vue$	(?:Sub2API|sub2api)	legacy setup defaults
^frontend/src/views/user/BatchImageGuideView\.vue$	sub2api	persisted preview cache and request identifiers
```
