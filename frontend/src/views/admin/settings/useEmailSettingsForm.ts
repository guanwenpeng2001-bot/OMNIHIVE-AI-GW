import { reactive } from "vue";
import type { NotifyEmailEntry } from "@/types";
import type { SettingsForm } from "./settingsForm";

export function useEmailSettingsForm() {
  return reactive<
    Pick<
      SettingsForm,
      | "smtp_host"
      | "smtp_port"
      | "smtp_username"
      | "smtp_password"
      | "smtp_password_configured"
      | "smtp_from_email"
      | "smtp_from_name"
      | "smtp_use_tls"
      | "balance_low_notify_enabled"
      | "balance_low_notify_threshold"
      | "balance_low_notify_recharge_url"
      | "subscription_expiry_notify_enabled"
      | "account_quota_notify_enabled"
      | "account_quota_notify_emails"
    >
  >({
    smtp_host: "",

    smtp_port: 587,

    smtp_username: "",

    smtp_password: "",

    smtp_password_configured: false,

    smtp_from_email: "",

    smtp_from_name: "",

    smtp_use_tls: true,

    // 余额、订阅到期与账号限额通知
    balance_low_notify_enabled: false,

    balance_low_notify_threshold: 0,

    balance_low_notify_recharge_url: "",

    subscription_expiry_notify_enabled: true,

    account_quota_notify_enabled: false,

    account_quota_notify_emails: [] as NotifyEmailEntry[],
  });
}
