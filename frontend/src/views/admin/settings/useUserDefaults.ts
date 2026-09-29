import { ref, reactive, computed } from "vue";
import { adminAPI } from "@/api";
import {
  buildAuthSourceDefaultsState,
  SCHEDULING_THRESHOLD_PLATFORMS,
} from "@/api/admin/settings";
import type {
  AuthSourceDefaultsState,
  AuthSourceType,
  DefaultSubscriptionSetting,
} from "@/api/admin/settings";
import type { AdminGroup } from "@/types";
import { useSettingsServices } from "./useSettingsServices";
import type { SettingsForm } from "./settingsForm";

export interface DefaultSubscriptionGroupOption {
  value: number;
  label: string;
  description: string | null;
  platform: AdminGroup["platform"];
  subscriptionType: AdminGroup["subscription_type"];
  rate: number;
  [key: string]: unknown;
}

export function useUserDefaults(form: SettingsForm) {
  const { t, localText } = useSettingsServices();
  const subscriptionGroups = ref<AdminGroup[]>([]);

  const schedulingThresholdPlatforms = SCHEDULING_THRESHOLD_PLATFORMS;

  const authSourceDefaults = reactive<AuthSourceDefaultsState>(
    buildAuthSourceDefaultsState({}),
  );

  const authSourceDefaultsMeta = computed(() => [
    {
      source: "email" as AuthSourceType,
      title: t("admin.settings.authSourceDefaults.sources.email.title"),
      description: t(
        "admin.settings.authSourceDefaults.sources.email.description",
      ),
    },
    {
      source: "linuxdo" as AuthSourceType,
      title: t("admin.settings.authSourceDefaults.sources.linuxdo.title"),
      description: t(
        "admin.settings.authSourceDefaults.sources.linuxdo.description",
      ),
    },
    {
      source: "oidc" as AuthSourceType,
      title: t("admin.settings.authSourceDefaults.sources.oidc.title"),
      description: t(
        "admin.settings.authSourceDefaults.sources.oidc.description",
      ),
    },
    {
      source: "wechat" as AuthSourceType,
      title: t("admin.settings.authSourceDefaults.sources.wechat.title"),
      description: t(
        "admin.settings.authSourceDefaults.sources.wechat.description",
      ),
    },
    {
      source: "github" as AuthSourceType,
      title: "GitHub",
      description: localText(
        "通过 GitHub 已验证邮箱首次注册或首次绑定时应用。",
        "Applied on first signup or first bind through a verified GitHub email.",
      ),
    },
    {
      source: "google" as AuthSourceType,
      title: "Google",
      description: localText(
        "通过 Google 已验证邮箱首次注册或首次绑定时应用。",
        "Applied on first signup or first bind through a verified Google email.",
      ),
    },
    {
      source: "dingtalk" as AuthSourceType,
      title: t("auth.dingtalkProviderName"),
      description: localText(
        "通过钉钉首次注册或首次绑定时应用。",
        "Applied on first signup or first bind through DingTalk.",
      ),
    },
  ]);

  const defaultSubscriptionGroupOptions = computed<
    DefaultSubscriptionGroupOption[]
  >(() =>
    subscriptionGroups.value.map((group) => ({
      value: group.id,
      label: group.name,
      description: group.description,
      platform: group.platform,
      subscriptionType: group.subscription_type,
      rate: group.rate_multiplier,
    })),
  );

  async function loadSubscriptionGroups() {
    try {
      const groups = await adminAPI.groups.getAll();
      subscriptionGroups.value = groups.filter(
        (group) =>
          group.subscription_type === "subscription" &&
          group.status === "active",
      );
    } catch (_error: unknown) {
      subscriptionGroups.value = [];
    }
  }

  function findNextAvailableSubscriptionGroup(
    existingGroupIDs: number[],
  ): AdminGroup | undefined {
    const existing = new Set(existingGroupIDs);
    return subscriptionGroups.value.find((group) => !existing.has(group.id));
  }

  function addDefaultSubscription() {
    if (subscriptionGroups.value.length === 0) return;
    const candidate = findNextAvailableSubscriptionGroup(
      form.default_subscriptions.map((item) => item.group_id),
    );
    if (!candidate) return;
    form.default_subscriptions.push({
      group_id: candidate.id,
      validity_days: 30,
    });
  }

  function removeDefaultSubscription(index: number) {
    form.default_subscriptions.splice(index, 1);
  }

  function addAuthSourceDefaultSubscription(source: AuthSourceType) {
    if (subscriptionGroups.value.length === 0) return;
    const candidate = findNextAvailableSubscriptionGroup(
      authSourceDefaults[source].subscriptions.map((item) => item.group_id),
    );
    if (!candidate) return;
    authSourceDefaults[source].subscriptions.push({
      group_id: candidate.id,
      validity_days: 30,
    });
  }

  function removeAuthSourceDefaultSubscription(
    source: AuthSourceType,
    index: number,
  ) {
    authSourceDefaults[source].subscriptions.splice(index, 1);
  }

  function findDuplicateDefaultSubscription(
    subscriptions: DefaultSubscriptionSetting[],
  ): DefaultSubscriptionSetting | undefined {
    const seenGroupIDs = new Set<number>();

    return subscriptions.find((item) => {
      if (seenGroupIDs.has(item.group_id)) {
        return true;
      }
      seenGroupIDs.add(item.group_id);
      return false;
    });
  }

  return {
    subscriptionGroups,
    schedulingThresholdPlatforms,
    authSourceDefaults,
    authSourceDefaultsMeta,
    defaultSubscriptionGroupOptions,
    loadSubscriptionGroups,
    findNextAvailableSubscriptionGroup,
    addDefaultSubscription,
    removeDefaultSubscription,
    addAuthSourceDefaultSubscription,
    removeAuthSourceDefaultSubscription,
    findDuplicateDefaultSubscription,
  };
}
