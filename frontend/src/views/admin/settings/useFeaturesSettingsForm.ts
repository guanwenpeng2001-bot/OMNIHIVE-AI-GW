import { reactive } from "vue";
import type { SettingsForm } from "./settingsForm";

export function useFeaturesSettingsForm() {
  return reactive<
    Pick<
      SettingsForm,
      | "affiliate_rebate_rate"
      | "affiliate_rebate_freeze_hours"
      | "affiliate_rebate_duration_days"
      | "affiliate_rebate_per_invitee_cap"
      | "affiliate_admin_recharge_enabled"
      | "risk_control_enabled"
      | "cyber_session_block_enabled"
      | "cyber_session_block_ttl_seconds"
      | "channel_monitor_enabled"
      | "channel_monitor_mode"
      | "channel_monitor_default_interval_seconds"
      | "channel_monitor_hide_throughput"
      | "channel_monitor_show_quota"
      | "channel_monitor_hide_user_ranking"
      | "available_channels_enabled"
      | "model_plaza_enabled"
      | "model_plaza_require_auth"
      | "model_plaza_description"
      | "plugin_management_enabled"
      | "affiliate_enabled"
    >
  >({
    affiliate_rebate_rate: 20,

    affiliate_rebate_freeze_hours: 0,

    affiliate_rebate_duration_days: 0,

    affiliate_rebate_per_invitee_cap: 0,

    affiliate_admin_recharge_enabled: false,

    risk_control_enabled: false,

    cyber_session_block_enabled: false,

    cyber_session_block_ttl_seconds: 3600,

    // Channel Monitor feature switch
    channel_monitor_enabled: true,

    channel_monitor_mode: "v1" as "v1" | "v2",

    channel_monitor_default_interval_seconds: 60,

    channel_monitor_hide_throughput: false,

    channel_monitor_show_quota: false,

    channel_monitor_hide_user_ranking: false,

    // Available Channels feature switch
    available_channels_enabled: false,

    // Model Plaza feature switches + description
    model_plaza_enabled: false,

    model_plaza_require_auth: false,

    model_plaza_description: "",

    // Plugin management menu visibility; plugin runtime is unaffected.
    plugin_management_enabled: false,

    // Affiliate (邀请返利) feature switch
    affiliate_enabled: false,
  });
}
