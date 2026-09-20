import { inject, provide, type InjectionKey } from "vue";
import type { useSettingsPage } from "./useSettingsPage";

type SettingsContext = ReturnType<typeof useSettingsPage>;
const settingsContextKey: InjectionKey<SettingsContext> = Symbol("settings");

export function provideSettingsContext(context: SettingsContext) {
  provide(settingsContextKey, context);
}

export function useSettingsContext() {
  const context = inject(settingsContextKey);
  if (!context) throw new Error("Settings sections require SettingsView");
  return context;
}
