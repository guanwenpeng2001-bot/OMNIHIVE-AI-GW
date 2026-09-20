import { reactive } from "vue";
import type { SettingsForm } from "./settingsForm";

export function usePaymentSettingsForm() {
  return reactive<
    Pick<
      SettingsForm,
      | "payment_enabled"
      | "payment_min_amount"
      | "payment_max_amount"
      | "payment_daily_limit"
      | "payment_max_pending_orders"
      | "payment_order_timeout_minutes"
      | "payment_balance_disabled"
      | "payment_balance_recharge_multiplier"
      | "payment_subscription_usd_to_cny_rate"
      | "payment_recharge_fee_rate"
      | "payment_enabled_types"
      | "payment_help_image_url"
      | "payment_help_text"
      | "payment_product_name_prefix"
      | "payment_product_name_suffix"
      | "payment_load_balance_strategy"
      | "payment_cancel_rate_limit_enabled"
      | "payment_cancel_rate_limit_max"
      | "payment_cancel_rate_limit_window"
      | "payment_cancel_rate_limit_unit"
      | "payment_cancel_rate_limit_window_mode"
      | "payment_alipay_force_qrcode"
      | "payment_alipay_mobile_precreate_deep_link"
    >
  >({
    payment_enabled: false,

    payment_min_amount: 1,

    payment_max_amount: 10000,

    payment_daily_limit: 50000,

    payment_max_pending_orders: 3,

    payment_order_timeout_minutes: 30,

    payment_balance_disabled: false,

    payment_balance_recharge_multiplier: 1,

    payment_subscription_usd_to_cny_rate: 0,

    payment_recharge_fee_rate: 0,

    payment_enabled_types: [],

    payment_help_image_url: "",

    payment_help_text: "",

    payment_product_name_prefix: "",

    payment_product_name_suffix: "",

    payment_load_balance_strategy: "round-robin",

    payment_cancel_rate_limit_enabled: false,

    payment_cancel_rate_limit_max: 10,

    payment_cancel_rate_limit_window: 1,

    payment_cancel_rate_limit_unit: "day",

    payment_cancel_rate_limit_window_mode: "rolling",

    payment_alipay_force_qrcode: false,

    payment_alipay_mobile_precreate_deep_link: false,
  });
}
