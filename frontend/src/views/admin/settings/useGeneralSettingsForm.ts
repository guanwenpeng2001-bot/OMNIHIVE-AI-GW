import { reactive } from "vue";
import type { SettingsForm } from "./settingsForm";

export function useGeneralSettingsForm(tablePageSizeDefault: number) {
  return reactive<
    Pick<
      SettingsForm,
      | "registration_email_suffix_whitelist"
      | "site_name"
      | "site_logo"
      | "site_subtitle"
      | "api_base_url"
      | "contact_info"
      | "doc_url"
      | "home_content"
      | "compact_home_enabled"
      | "backend_mode_enabled"
      | "hide_ccs_import_button"
      | "table_default_page_size"
      | "table_page_size_options"
      | "custom_menu_items"
      | "custom_endpoints"
      | "enable_model_fallback"
      | "fallback_model_anthropic"
      | "fallback_model_openai"
      | "fallback_model_gemini"
      | "fallback_model_antigravity"
      | "enable_identity_patch"
      | "identity_patch_prompt"
      | "ops_monitoring_enabled"
      | "ops_realtime_monitoring_enabled"
      | "ops_query_mode_default"
      | "ops_metrics_interval_seconds"
      | "subscription_enabled"
    >
  >({
    registration_email_suffix_whitelist: [],

    site_name: "Sub2API",

    site_logo: "",

    site_subtitle: "Subscription to API Conversion Platform",

    api_base_url: "",

    contact_info: "",

    doc_url: "",

    home_content: "",

    compact_home_enabled: false,

    backend_mode_enabled: false,

    hide_ccs_import_button: false,

    table_default_page_size: tablePageSizeDefault,

    table_page_size_options: [10, 20, 50, 100],

    custom_menu_items: [] as Array<{
      id: string;
      label: string;
      icon_svg: string;
      url: string;
      visibility: "user" | "admin";
      sort_order: number;
      hide_open_button?: boolean;
    }>,

    custom_endpoints: [] as Array<{
      name: string;
      endpoint: string;
      description: string;
    }>,

    // Model fallback
    enable_model_fallback: false,

    fallback_model_anthropic: "claude-3-5-sonnet-20241022",

    fallback_model_openai: "gpt-4o",

    fallback_model_gemini: "gemini-2.5-pro",

    fallback_model_antigravity: "gemini-2.5-pro",

    // Identity patch (Claude -> Gemini)
    enable_identity_patch: true,

    identity_patch_prompt: "",

    // Ops monitoring (vNext)
    ops_monitoring_enabled: true,

    ops_realtime_monitoring_enabled: true,

    ops_query_mode_default: "auto",

    ops_metrics_interval_seconds: 60,

    // Subscription feature switch (user sidebar "My Subscriptions" entry)
    subscription_enabled: true,
  });
}
