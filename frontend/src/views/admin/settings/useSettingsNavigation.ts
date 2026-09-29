import { ref } from "vue";

export function useSettingsNavigation() {
  type SettingsTab =
    | "general"
    | "agreement"
    | "features"
    | "security"
    | "users"
    | "gateway"
    | "payment"
    | "email"
    | "backup";

  const activeTab = ref<SettingsTab>("general");

  const settingsTabs = [
    { key: "general" as SettingsTab, icon: "home" as const },
    { key: "agreement" as SettingsTab, icon: "document" as const },
    { key: "features" as SettingsTab, icon: "bolt" as const },
    { key: "security" as SettingsTab, icon: "shield" as const },
    { key: "users" as SettingsTab, icon: "user" as const },
    { key: "gateway" as SettingsTab, icon: "server" as const },
    { key: "payment" as SettingsTab, icon: "creditCard" as const },
    { key: "email" as SettingsTab, icon: "mail" as const },
    { key: "backup" as SettingsTab, icon: "database" as const },
  ];

  const settingsTabKeyboardActions = {
    ArrowLeft: -1,
    ArrowUp: -1,
    ArrowRight: 1,
    ArrowDown: 1,
    Home: "first",
    End: "last",
  } as const;

  function selectSettingsTab(tab: SettingsTab): void {
    activeTab.value = tab;
  }

  function focusSettingsTab(tab: SettingsTab): void {
    window.requestAnimationFrame(() => {
      document.getElementById(`settings-tab-${tab}`)?.focus();
    });
  }

  function handleSettingsTabKeydown(
    event: KeyboardEvent,
    tab: SettingsTab,
  ): void {
    const action =
      settingsTabKeyboardActions[
        event.key as keyof typeof settingsTabKeyboardActions
      ];
    if (action === undefined) {
      return;
    }

    event.preventDefault();
    const currentIndex = settingsTabs.findIndex((item) => item.key === tab);
    let nextIndex = currentIndex < 0 ? 0 : currentIndex;

    if (action === "first") {
      nextIndex = 0;
    } else if (action === "last") {
      nextIndex = settingsTabs.length - 1;
    } else {
      nextIndex =
        (nextIndex + action + settingsTabs.length) % settingsTabs.length;
    }

    const nextTab = settingsTabs[nextIndex]?.key;
    if (!nextTab) {
      return;
    }

    selectSettingsTab(nextTab);
    focusSettingsTab(nextTab);
  }

  return {
    activeTab,
    settingsTabs,
    settingsTabKeyboardActions,
    selectSettingsTab,
    focusSettingsTab,
    handleSettingsTabKeydown,
  };
}
