import { reactive } from "vue";
import { normalizeAccountSchedulingThresholdsMap } from "@/api/admin/settings";
import type { SettingsForm } from "./settingsForm";

export function useGatewaySettingsForm(
  defaultClaudeOAuthSystemPromptBlocks: string,
) {
  return reactive<
    Pick<
      SettingsForm,
      | "account_scheduling_thresholds"
      | "grok_default_text_model"
      | "grok_cross_client_model_map_enabled"
      | "grok_default_base_url_mode"
      | "min_claude_code_version"
      | "max_claude_code_version"
      | "allow_ungrouped_key_scheduling"
      | "openai_low_upstream_rate_priority_enabled"
      | "openai_oauth_scheduling_rate_multiplier"
      | "openai_advanced_scheduler_enabled"
      | "openai_advanced_scheduler_sticky_weighted_enabled"
      | "openai_advanced_scheduler_subscription_priority_enabled"
      | "openai_advanced_scheduler_lb_top_k"
      | "openai_advanced_scheduler_weight_priority"
      | "openai_advanced_scheduler_weight_load"
      | "openai_advanced_scheduler_weight_queue"
      | "openai_advanced_scheduler_weight_error_rate"
      | "openai_advanced_scheduler_weight_ttft"
      | "openai_advanced_scheduler_weight_reset"
      | "openai_advanced_scheduler_weight_quota_headroom"
      | "openai_advanced_scheduler_weight_upstream_cost"
      | "openai_advanced_scheduler_weight_previous_response"
      | "openai_advanced_scheduler_weight_session_sticky"
      | "openai_ttft_mode"
      | "enable_fingerprint_unification"
      | "enable_metadata_passthrough"
      | "enable_cch_signing"
      | "enable_claude_oauth_system_prompt_injection"
      | "claude_oauth_system_prompt"
      | "claude_oauth_system_prompt_blocks"
      | "enable_anthropic_cache_ttl_1h_injection"
      | "rewrite_message_cache_control"
      | "enable_client_dateline_normalization"
      | "cursor_client_version"
      | "antigravity_user_agent_version"
      | "openai_codex_user_agent"
      | "openai_codex_client_version"
      | "openai_codex_client_version_synced"
      | "openai_codex_version_auto_sync_enabled"
      | "min_codex_version"
      | "max_codex_version"
      | "codex_cli_only_blacklist"
      | "codex_cli_only_whitelist"
      | "codex_cli_only_allow_app_server_clients"
      | "codex_cli_only_engine_fingerprint_signals"
      | "allow_user_view_error_requests"
    >
  >({
    account_scheduling_thresholds: normalizeAccountSchedulingThresholdsMap(),

    grok_default_text_model: "grok-4.5",

    grok_cross_client_model_map_enabled: false,

    grok_default_base_url_mode: "cli",

    // Claude Code version check
    min_claude_code_version: "",

    max_claude_code_version: "",

    // 分组隔离
    allow_ungrouped_key_scheduling: false,

    openai_low_upstream_rate_priority_enabled: false,

    openai_oauth_scheduling_rate_multiplier: 1,

    openai_advanced_scheduler_enabled: false,

    openai_advanced_scheduler_sticky_weighted_enabled: false,

    openai_advanced_scheduler_subscription_priority_enabled: false,

    openai_advanced_scheduler_lb_top_k: "",

    openai_advanced_scheduler_weight_priority: "",

    openai_advanced_scheduler_weight_load: "",

    openai_advanced_scheduler_weight_queue: "",

    openai_advanced_scheduler_weight_error_rate: "",

    openai_advanced_scheduler_weight_ttft: "",

    openai_advanced_scheduler_weight_reset: "",

    openai_advanced_scheduler_weight_quota_headroom: "",

    openai_advanced_scheduler_weight_upstream_cost: "",

    openai_advanced_scheduler_weight_previous_response: "",

    openai_advanced_scheduler_weight_session_sticky: "",

    // Gateway forwarding behavior
    openai_ttft_mode: "semantic",

    enable_fingerprint_unification: true,

    enable_metadata_passthrough: false,

    enable_cch_signing: false,

    enable_claude_oauth_system_prompt_injection: true,

    claude_oauth_system_prompt: "",

    claude_oauth_system_prompt_blocks: defaultClaudeOAuthSystemPromptBlocks,

    enable_anthropic_cache_ttl_1h_injection: false,

    rewrite_message_cache_control: false,

    enable_client_dateline_normalization: true,

    cursor_client_version: "",

    antigravity_user_agent_version: "",

    openai_codex_user_agent: "",

    openai_codex_client_version: "",

    // 只读展示：自动同步任务写入的官方最新稳定版，不参与提交（提交载荷按字段显式构造）
    openai_codex_client_version_synced: "",

    openai_codex_version_auto_sync_enabled: true,

    // codex_cli_only 加固
    min_codex_version: "",

    max_codex_version: "",

    codex_cli_only_blacklist: "",

    codex_cli_only_whitelist: "",

    codex_cli_only_allow_app_server_clients: false,

    codex_cli_only_engine_fingerprint_signals: "",

    // Allow user view error requests
    allow_user_view_error_requests: false,
  });
}
