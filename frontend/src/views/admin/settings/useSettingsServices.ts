import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useClipboard } from "@/composables/useClipboard";
import { useAppStore } from "@/stores";

export function useSettingsServices() {
  const { t, locale } = useI18n();

  const appStore = useAppStore();

  const isZhLocale = computed(() => locale.value.startsWith("zh"));

  function localText(zh: string, en: string): string {
    return isZhLocale.value ? zh : en;
  }

  const { copyToClipboard } = useClipboard();

  return { t, locale, appStore, isZhLocale, localText, copyToClipboard };
}
