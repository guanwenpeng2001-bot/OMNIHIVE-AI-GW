import { ref, computed } from "vue";
import type { SettingsForm } from "./settingsForm";

export function useCaptchaSettings(form: SettingsForm) {
  // 人机验证 UI 状态：单卡片「总开关 + 服务商单选」，落库仍是三个独立
  // enabled 键（与上游一致），由下面的映射保证同一时间至多一家启用。
  type CaptchaProviderSelection = "turnstile" | "tencent" | "aliyun";

  const captchaProviderSelection = ref<CaptchaProviderSelection>("turnstile");

  function applyCaptchaSelection(
    provider: CaptchaProviderSelection | null,
  ): void {
    form.turnstile_enabled = provider === "turnstile";
    form.tencent_captcha_enabled = provider === "tencent";
    form.aliyun_captcha_enabled = provider === "aliyun";
  }

  const captchaMasterEnabled = computed({
    get: () =>
      form.turnstile_enabled ||
      form.tencent_captcha_enabled ||
      form.aliyun_captcha_enabled,
    set: (enabled: boolean) =>
      applyCaptchaSelection(enabled ? captchaProviderSelection.value : null),
  });

  function selectCaptchaProvider(provider: CaptchaProviderSelection): void {
    captchaProviderSelection.value = provider;
    applyCaptchaSelection(provider);
  }

  // 天御中国站与国际站是两套独立账号体系，控制台与文档入口不通用，
  // 按当前选择的站点给出对应链接，避免管理员在错误的控制台里找不到 CaptchaAppId。
  const tencentCaptchaLinks = computed(() =>
    form.tencent_captcha_region === "intl"
      ? {
          console: "https://console.tencentcloud.com/captcha/graphical",
          cloudKeys: "https://console.tencentcloud.com/cam/capi",
          webDocs: "https://www.tencentcloud.com/document/product/1159/49680",
        }
      : {
          console: "https://console.cloud.tencent.com/captcha",
          cloudKeys: "https://console.cloud.tencent.com/cam/capi",
          webDocs: "https://cloud.tencent.com/document/product/1110/36841",
        },
  );

  function syncCaptchaProviderSelection(): void {
    if (form.tencent_captcha_enabled) {
      captchaProviderSelection.value = "tencent";
    } else if (form.aliyun_captcha_enabled) {
      captchaProviderSelection.value = "aliyun";
    } else if (form.turnstile_enabled) {
      captchaProviderSelection.value = "turnstile";
    }
  }

  return {
    captchaProviderSelection,
    applyCaptchaSelection,
    captchaMasterEnabled,
    selectCaptchaProvider,
    tencentCaptchaLinks,
    syncCaptchaProviderSelection,
  };
}
