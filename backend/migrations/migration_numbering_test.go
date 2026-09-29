package migrations

import (
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

// migrationFileNamePattern 是迁移文件必须匹配的命名格式：
// 三位数字前缀 + 可选单个小写字母后缀 + 语义名 + .sql。
// 例：001_init.sql、108a_widen_auth_identity_migration_report_type.sql。
var migrationFileNamePattern = regexp.MustCompile(`^([0-9]{3})[a-z]?_[a-z0-9_]+\.sql$`)

// historicalDuplicateMigrationPrefixes 冻结 runner 已经在真实库按文件名顺序应用过的同号前缀文件集合。
//
// 迁移记录表以完整文件名为主键，runner 按完整文件名字典序执行，所以：
//   - 历史同号文件不允许改名（改名会让已应用记录与 checksum 全部失配，等于重建迁移）；
//   - 新增迁移必须使用从未出现过的三位前缀号，编号由集成者统一分配；
//   - 不得往历史同号前缀里追加新文件（那会让执行顺序依赖字典序而不是编号语义）。
//
// 本表只登记历史例外；任何新增的同号前缀，或对既有同号前缀追加文件，
// 都会让 TestMigrationFilesUseUniqueNumericPrefixes 失败。
var historicalDuplicateMigrationPrefixes = map[string][]string{
	"006": {
		"006_add_users_allowed_groups_compat.sql",
		"006_fix_invalid_subscription_expires_at.sql",
		"006b_guard_users_allowed_groups.sql",
	},
	"028": {
		"028_add_account_notes.sql",
		"028_add_usage_logs_user_agent.sql",
		"028_group_image_pricing.sql",
	},
	"029": {
		"029_add_group_claude_code_restriction.sql",
		"029_usage_log_image_fields.sql",
	},
	"033": {
		"033_add_promo_codes.sql",
		"033_ops_monitoring_vnext.sql",
	},
	"034": {
		"034_ops_upstream_error_events.sql",
		"034_usage_dashboard_aggregation_tables.sql",
	},
	"036": {
		"036_ops_error_logs_add_is_count_tokens.sql",
		"036_scheduler_outbox.sql",
	},
	"037": {
		"037_add_account_rate_multiplier.sql",
		"037_ops_alert_silences.sql",
	},
	"042": {
		"042_add_usage_cleanup_tasks.sql",
		"042b_add_ops_system_metrics_switch_count.sql",
	},
	"043": {
		"043_add_usage_cleanup_cancel_audit.sql",
		"043b_add_group_invalid_request_fallback.sql",
	},
	"044": {
		"044_add_user_totp.sql",
		"044b_add_group_mcp_xml_inject.sql",
	},
	"045": {
		"045_add_accounts_extra_index.sql",
		"045_add_announcements.sql",
		"045_add_api_key_quota.sql",
	},
	"046": {
		"046_add_sora_accounts.sql",
		"046_add_usage_log_reasoning_effort.sql",
		"046b_add_group_supported_model_scopes.sql",
	},
	"047": {
		"047_add_sora_pricing_and_media_type.sql",
		"047_add_user_group_rate_multipliers.sql",
	},
	"052": {
		"052_add_group_sort_order.sql",
		"052_migrate_upstream_to_apikey.sql",
	},
	"053": {
		"053_add_security_secrets.sql",
		"053_add_skip_monitoring_to_error_passthrough.sql",
	},
	"054": {
		"054_drop_legacy_cache_columns.sql",
		"054_ops_system_logs.sql",
	},
	"060": {
		"060_add_gemini31_flash_image_to_model_mapping.sql",
		"060_add_usage_log_openai_ws_mode.sql",
	},
	"070": {
		"070_add_scheduled_test_auto_recover.sql",
		"070_add_usage_log_service_tier.sql",
	},
	"071": {
		"071_add_gemini25_flash_image_to_model_mapping.sql",
		"071_add_usage_billing_dedup.sql",
	},
	"075": {
		"075_add_usage_log_upstream_model.sql",
		"075_map_haiku45_to_sonnet46.sql",
	},
	"081": {
		"081_add_group_account_filter.sql",
		"081_create_channels.sql",
	},
	"095": {
		"095_channel_features.sql",
		"095_subscription_plans.sql",
	},
	"101": {
		"101_add_account_stats_pricing.sql",
		"101_add_balance_notify_fields.sql",
		"101_add_channel_features_config.sql",
		"101_add_payment_mode.sql",
	},
	"102": {
		"102_add_balance_notify_threshold_type.sql",
		"102_add_out_trade_no_to_payment_orders.sql",
	},
	"108": {
		"108_auth_identity_foundation_core.sql",
		"108a_widen_auth_identity_migration_report_type.sql",
	},
	"120": {
		"120_enforce_payment_orders_out_trade_no_unique_notx.sql",
		"120a_align_payment_orders_out_trade_no_index_name.sql",
	},
	"125": {
		"125_add_channel_monitors.sql",
		"125_add_group_rpm_limit.sql",
	},
	"126": {
		"126_add_channel_monitor_aggregation.sql",
		"126_add_user_rpm_limit.sql",
	},
	"127": {
		"127_add_user_group_rpm_override.sql",
		"127_drop_channel_monitor_deleted_at.sql",
	},
	"134": {
		"134_affiliate_ledger_audit_snapshots.sql",
		"134_image_generation_group_controls.sql",
	},
	"135": {
		"135_allow_email_oauth_provider_types.sql",
		"135_content_moderation.sql",
	},
	"136": {
		"136_add_dingtalk_provider_type.sql",
		"136_remove_ops_retry_replay.sql",
		"136_usage_log_image_size_metadata.sql",
	},
	"145": {
		"145_deleted_api_key_audit.sql",
		"145_ops_metrics_ttft_sample_count.sql",
	},
	"151": {
		"151_account_autopause_expiry_index_notx.sql",
		"151_channel_monitor_jitter.sql",
	},
	"154": {
		"154_account_spark_shadow.sql",
		"154_add_ops_system_logs_api_key_id.sql",
		"154a_account_spark_shadow_indexes_notx.sql",
	},
	"158": {
		"158_add_group_peak_rate_multiplier.sql",
		"158_enable_grok_media_generation_groups.sql",
	},
	"160": {
		"160_add_user_frozen_balance.sql",
		"160_batch_image_provider_refs.sql",
	},
	"172": {
		"172_composite_model_routes.sql",
		"172_video_per_second_billing_metadata.sql",
	},
	"174": {
		"174_add_usage_log_long_context_billing.sql",
		"174_add_usage_logs_api_key_latest_ip_index_notx.sql",
		"174_group_web_search_price_per_call.sql",
	},
	"175": {
		"175_add_ops_system_logs_host.sql",
		"175_default_openai_long_context_billing.sql",
		"175a_add_ops_system_logs_host_index_notx.sql",
	},
	"181": {
		"181_group_duplicate_operation_id.sql",
		"181_prompt_audit.sql",
	},
	"186": {
		"186_alipay_mobile_precreate_deep_link.sql",
		"186_group_auth_cache_image_generation.sql",
	},
	"194": {
		"194_add_usage_log_upstream_response_model.sql",
		"194_channel_monitor_v2.sql",
	},
	"195": {
		"195_add_usage_log_upstream_model_mismatch_index_notx.sql",
		"195_channel_monitor_mode.sql",
	},
	"225": {
		"225_backfill_codex_fingerprint_seed.sql",
		"225_channel_model_time_pricing.sql",
	},
	"226": {
		"226_add_usage_log_effective_model_indexes_notx.sql",
		"226_channel_monitor_quota_mode.sql",
	},
	"231": {
		"231_add_usage_log_native_compaction_v2.sql",
		"231_add_usage_log_requested_reasoning_effort.sql",
		"231_user_restrict_public_groups.sql",
	},
	"232": {
		"232_add_usage_log_upstream_request_id.sql",
		"232_channel_cache_write_1h_pricing.sql",
		"232_group_force_openai_fast.sql",
		"232_group_reasoning_effort_over_limit.sql",
	},
	"233": {
		"233_add_usage_log_upstream_request_id_index_notx.sql",
		"233_group_free_openai_fast.sql",
	},
	"234": {
		"234_channel_max_reasoning_effort_multiplier.sql",
		"234_group_codex_models_manifest_config.sql",
	},
	"238": {
		"238_opencode_go_platform.sql",
		"238_purge_unlimited_user_platform_quotas.sql",
	},
}

// collectDuplicateMigrationPrefixes 返回同号前缀到文件名的映射（每个前缀至少两个文件）并按文件名排序。
// 同时校验每个文件符合 NNN_语义名.sql 命名格式。
func collectDuplicateMigrationPrefixes(fsys fs.FS) (map[string][]string, error) {
	files, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, err
	}

	byPrefix := make(map[string][]string)
	for _, name := range files {
		match := migrationFileNamePattern.FindStringSubmatch(name)
		if match == nil {
			return nil, fmt.Errorf("迁移文件 %q 必须匹配 NNN_语义名.sql（可带单个小写字母后缀）", name)
		}
		byPrefix[match[1]] = append(byPrefix[match[1]], name)
	}

	duplicates := make(map[string][]string)
	for prefix, names := range byPrefix {
		if len(names) < 2 {
			continue
		}
		sorted := append([]string(nil), names...)
		sort.Strings(sorted)
		duplicates[prefix] = sorted
	}
	return duplicates, nil
}

// TestMigrationFilesUseUniqueNumericPrefixes 断言迁移目录里每个前缀号最多只被一组冻结的历史例外占用。
// 新增迁移必须选一个从未出现过的三位前缀号（集成者统一分配），历史同号文件保持原样不改名。
func TestMigrationFilesUseUniqueNumericPrefixes(t *testing.T) {
	observed, err := collectDuplicateMigrationPrefixes(FS)
	require.NoError(t, err)
	require.NotEmpty(t, observed, "嵌入的迁移目录不应为空")

	expected := make(map[string][]string, len(historicalDuplicateMigrationPrefixes))
	for prefix, names := range historicalDuplicateMigrationPrefixes {
		require.Greaterf(t, len(names), 1, "前缀 %s 不是历史同号，不应出现在例外表里", prefix)
		sorted := append([]string(nil), names...)
		sort.Strings(sorted)
		expected[prefix] = sorted
	}

	require.Equal(t, expected, observed,
		"同号前缀只允许历史例外：新迁移必须使用未占用的三位前缀号，且不得往历史同号前缀追加文件；"+
			"确需登记新例外必须先说明 runner 已按该文件集在生产库应用过")
}

// TestCollectDuplicateMigrationPrefixesDetectsNewCollisions 用合成文件集验证守卫本身能拦住新碰号。
func TestCollectDuplicateMigrationPrefixesDetectsNewCollisions(t *testing.T) {
	t.Run("唯一前缀不报同号", func(t *testing.T) {
		duplicates, err := collectDuplicateMigrationPrefixes(fstest.MapFS{
			"001_init.sql":          &fstest.MapFile{},
			"002_add_users.sql":     &fstest.MapFile{},
			"273_session_index.sql": &fstest.MapFile{},
		})
		require.NoError(t, err)
		require.Empty(t, duplicates)
	})

	t.Run("新碰号被识别", func(t *testing.T) {
		duplicates, err := collectDuplicateMigrationPrefixes(fstest.MapFS{
			"273_session_index.sql":      &fstest.MapFile{},
			"273_another_new_change.sql": &fstest.MapFile{},
		})
		require.NoError(t, err)
		require.Equal(t, map[string][]string{
			"273": {"273_another_new_change.sql", "273_session_index.sql"},
		}, duplicates)
	})

	t.Run("往历史同号前缀追加文件同样被识别", func(t *testing.T) {
		duplicates, err := collectDuplicateMigrationPrefixes(fstest.MapFS{
			"174_add_usage_log_long_context_billing.sql":          &fstest.MapFile{},
			"174_add_usage_logs_api_key_latest_ip_index_notx.sql": &fstest.MapFile{},
			"174_group_web_search_price_per_call.sql":             &fstest.MapFile{},
			"174_brand_new_change.sql":                            &fstest.MapFile{},
		})
		require.NoError(t, err)
		require.Len(t, duplicates["174"], 4)
	})

	t.Run("不合规文件名被拒绝", func(t *testing.T) {
		_, err := collectDuplicateMigrationPrefixes(fstest.MapFS{
			"bad-name.sql": &fstest.MapFile{},
		})
		require.Error(t, err)
	})
}

// TestLatestMigrationNumberIsUnique 断言最高编号只被一个文件占用，避免新迁移复用“最新号”而依赖字典序判断先后。
func TestLatestMigrationNumberIsUnique(t *testing.T) {
	files, err := fs.Glob(FS, "*.sql")
	require.NoError(t, err)

	highest := ""
	for _, name := range files {
		match := migrationFileNamePattern.FindStringSubmatch(name)
		require.NotNilf(t, match, "迁移文件 %q 命名不合规", name)
		if match[1] > highest {
			highest = match[1]
		}
	}
	require.NotEmpty(t, highest)

	count := 0
	for _, name := range files {
		if match := migrationFileNamePattern.FindStringSubmatch(name); match != nil && match[1] == highest {
			count++
		}
	}
	require.Equal(t, 1, count, "最高迁移号 %s 必须唯一，新迁移请使用更大的未占用编号", highest)
}
