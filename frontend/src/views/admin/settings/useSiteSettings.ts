import { ref, computed } from "vue";
import { type SelectOption } from "@/components/common/Select.vue";
import {
  SITE_BILLING_MODES,
  SITE_BILLING_MODE_I18N_KEYS,
  billingModeToSettings,
  resolveSiteBillingMode,
  type SiteBillingMode,
} from "@/utils/siteBillingMode";
import { useSettingsServices } from "./useSettingsServices";
import type { SettingsForm } from "./settingsForm";

export function useSiteSettings(getForm: () => SettingsForm) {
  const { t } = useSettingsServices();
  const tablePageSizeOptionsInput = ref("10, 20, 50, 100");

  const tablePageSizeMin = 5;

  const tablePageSizeMax = 1000;

  const tablePageSizeDefault = 20;

  // Custom menu item management
  function addMenuItem() {
    getForm().custom_menu_items.push({
      id: "",
      label: "",
      icon_svg: "",
      url: "",
      visibility: "user",
      sort_order: getForm().custom_menu_items.length,
    });
  }

  function removeMenuItem(index: number) {
    getForm().custom_menu_items.splice(index, 1);
    // Re-index sort_order
    getForm().custom_menu_items.forEach((item, i) => {
      item.sort_order = i;
    });
  }

  function moveMenuItem(index: number, direction: -1 | 1) {
    const targetIndex = index + direction;
    if (targetIndex < 0 || targetIndex >= getForm().custom_menu_items.length)
      return;
    const items = getForm().custom_menu_items;
    const temp = items[index];
    items[index] = items[targetIndex];
    items[targetIndex] = temp;
    // Re-index sort_order
    items.forEach((item, i) => {
      item.sort_order = i;
    });
  }

  // Custom endpoint management
  function addEndpoint() {
    getForm().custom_endpoints.push({
      name: "",
      endpoint: "",
      description: "",
    });
  }

  function removeEndpoint(index: number) {
    getForm().custom_endpoints.splice(index, 1);
  }

  function formatTablePageSizeOptions(options: number[]): string {
    return options.join(", ");
  }

  function parseTablePageSizeOptionsInput(raw: string): number[] | null {
    const tokens = raw
      .split(",")
      .map((token) => token.trim())
      .filter((token) => token.length > 0);

    if (tokens.length === 0) {
      return null;
    }

    const parsed = tokens.map((token) => Number(token));
    if (parsed.some((value) => !Number.isInteger(value))) {
      return null;
    }

    const deduped = Array.from(new Set(parsed)).sort((a, b) => a - b);
    if (
      deduped.some(
        (value) => value < tablePageSizeMin || value > tablePageSizeMax,
      )
    ) {
      return null;
    }

    return deduped;
  }

  // 站点类型：由 subscription_enabled 与 payment_balance_disabled 两个开关派生的单选，
  // 保存时同时写回两者，避免出现「既无充值也无订阅」的组合。
  const siteBillingModeOptions = computed<SelectOption[]>(() =>
    SITE_BILLING_MODES.map((mode) => ({
      value: mode,
      label: t(
        `admin.settings.features.siteBillingMode.options.${SITE_BILLING_MODE_I18N_KEYS[mode]}`,
      ),
    })),
  );

  const siteBillingMode = computed<SiteBillingMode>({
    get: () => resolveSiteBillingMode(getForm()),
    set: (mode) => {
      Object.assign(getForm(), billingModeToSettings(mode));
    },
  });

  const siteBillingModeHint = computed(() =>
    t(
      `admin.settings.features.siteBillingMode.hints.${SITE_BILLING_MODE_I18N_KEYS[siteBillingMode.value]}`,
    ),
  );

  return {
    tablePageSizeOptionsInput,
    tablePageSizeMin,
    tablePageSizeMax,
    tablePageSizeDefault,
    addMenuItem,
    removeMenuItem,
    moveMenuItem,
    addEndpoint,
    removeEndpoint,
    formatTablePageSizeOptions,
    parseTablePageSizeOptionsInput,
    siteBillingModeOptions,
    siteBillingMode,
    siteBillingModeHint,
  };
}
