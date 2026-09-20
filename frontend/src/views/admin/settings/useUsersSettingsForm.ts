import { reactive } from "vue";
import { normalizePlatformQuotasMap } from "@/api/admin/settings";
import type { DefaultPlatformQuotasMap } from "@/api/admin/settings";
import type { SettingsForm } from "./settingsForm";

export function useUsersSettingsForm() {
  return reactive<
    Pick<
      SettingsForm,
      | "default_balance"
      | "default_platform_quotas"
      | "default_concurrency"
      | "default_subscriptions"
      | "force_email_on_third_party_signup"
      | "default_user_rpm_limit"
    >
  >({
    default_balance: 0,

    default_platform_quotas:
      normalizePlatformQuotasMap() as DefaultPlatformQuotasMap,

    default_concurrency: 1,

    default_subscriptions: [],

    force_email_on_third_party_signup: false,

    default_user_rpm_limit: 0,
  });
}
