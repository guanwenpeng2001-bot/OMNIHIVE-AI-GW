<template>
  <div v-show="activeTab === 'users'" class="space-y-6">
    <!-- Default Settings -->
    <div class="card border-[var(--border)] bg-[var(--card)] shadow-none">
      <div class="border-b border-[var(--border)] px-4 py-4 sm:px-6">
        <h2 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
          {{ t("admin.settings.defaults.title") }}
        </h2>
        <p class="mt-1 text-sm text-[var(--muted-foreground)]">
          {{ t("admin.settings.defaults.description") }}
        </p>
      </div>
      <div class="space-y-6 p-4 sm:p-6">
        <div class="grid grid-cols-1 gap-6 md:grid-cols-2">
          <div>
            <label
              class="mb-2 block text-sm font-medium text-[var(--foreground)]"
            >
              {{ t("admin.settings.defaults.defaultBalance") }}
            </label>
            <input
              v-model.number="form.default_balance"
              type="number"
              step="0.01"
              min="0"
              class="input"
              placeholder="0.00"
            />
            <p class="mt-1.5 text-xs text-[var(--muted-foreground)]">
              {{ t("admin.settings.defaults.defaultBalanceHint") }}
            </p>
          </div>
          <div>
            <label
              class="mb-2 block text-sm font-medium text-[var(--foreground)]"
            >
              {{ t("admin.settings.defaults.defaultConcurrency") }}
            </label>
            <input
              v-model.number="form.default_concurrency"
              type="number"
              min="1"
              class="input"
              placeholder="1"
            />
            <p class="mt-1.5 text-xs text-[var(--muted-foreground)]">
              {{ t("admin.settings.defaults.defaultConcurrencyHint") }}
            </p>
          </div>
          <div>
            <label
              class="mb-2 block text-sm font-medium text-[var(--foreground)]"
            >
              {{ t("admin.settings.defaults.defaultUserRpmLimit") }}
            </label>
            <input
              v-model.number="form.default_user_rpm_limit"
              type="number"
              min="0"
              step="1"
              class="input"
              placeholder="0"
            />
            <p class="mt-1.5 text-xs text-[var(--muted-foreground)]">
              {{ t("admin.settings.defaults.defaultUserRpmLimitHint") }}
            </p>
          </div>
        </div>

        <div class="border-t border-[var(--border)] pt-4">
          <div class="mb-3 flex items-center justify-between">
            <div>
              <label class="font-medium text-[var(--foreground)]">
                {{ t("admin.settings.defaults.defaultSubscriptions") }}
              </label>
              <p class="text-sm text-[var(--muted-foreground)]">
                {{ t("admin.settings.defaults.defaultSubscriptionsHint") }}
              </p>
            </div>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              @click="addDefaultSubscription"
              :disabled="subscriptionGroups.length === 0"
            >
              {{ t("admin.settings.defaults.addDefaultSubscription") }}
            </button>
          </div>

          <div
            v-if="form.default_subscriptions.length === 0"
            class="rounded border border-dashed border-[var(--border)] px-4 py-3 text-sm text-[var(--muted-foreground)]"
          >
            {{ t("admin.settings.defaults.defaultSubscriptionsEmpty") }}
          </div>

          <div v-else class="space-y-3">
            <div
              v-for="(item, index) in form.default_subscriptions"
              :key="`default-sub-${index}`"
              class="grid grid-cols-1 gap-3 rounded border border-[var(--border)] p-3 md:grid-cols-[1fr_160px_auto]"
            >
              <div>
                <label
                  class="mb-1 block text-xs font-medium text-[var(--muted-foreground)]"
                >
                  {{ t("admin.settings.defaults.subscriptionGroup") }}
                </label>
                <Select
                  v-model="item.group_id"
                  class="default-sub-group-select"
                  :options="defaultSubscriptionGroupOptions"
                  :placeholder="t('admin.settings.defaults.subscriptionGroup')"
                >
                  <template #selected="{ option }">
                    <GroupBadge
                      v-if="option"
                      :name="
                        (option as unknown as DefaultSubscriptionGroupOption)
                          .label
                      "
                      :platform="
                        (option as unknown as DefaultSubscriptionGroupOption)
                          .platform
                      "
                      :subscription-type="
                        (option as unknown as DefaultSubscriptionGroupOption)
                          .subscriptionType
                      "
                      :rate-multiplier="
                        (option as unknown as DefaultSubscriptionGroupOption)
                          .rate
                      "
                    />
                    <span v-else class="text-[var(--muted-foreground)]">
                      {{ t("admin.settings.defaults.subscriptionGroup") }}
                    </span>
                  </template>
                  <template #option="{ option, selected }">
                    <GroupOptionItem
                      :name="
                        (option as unknown as DefaultSubscriptionGroupOption)
                          .label
                      "
                      :platform="
                        (option as unknown as DefaultSubscriptionGroupOption)
                          .platform
                      "
                      :subscription-type="
                        (option as unknown as DefaultSubscriptionGroupOption)
                          .subscriptionType
                      "
                      :rate-multiplier="
                        (option as unknown as DefaultSubscriptionGroupOption)
                          .rate
                      "
                      :description="
                        (option as unknown as DefaultSubscriptionGroupOption)
                          .description
                      "
                      :selected="selected"
                    />
                  </template>
                </Select>
              </div>
              <div>
                <label
                  class="mb-1 block text-xs font-medium text-[var(--muted-foreground)]"
                >
                  {{ t("admin.settings.defaults.subscriptionValidityDays") }}
                </label>
                <input
                  v-model.number="item.validity_days"
                  type="number"
                  min="1"
                  max="36500"
                  class="input h-[42px]"
                />
              </div>
              <div class="flex items-end">
                <button
                  type="button"
                  class="btn btn-secondary default-sub-delete-btn w-full text-[var(--bad-text)] hover:text-[var(--bad-text)]"
                  @click="removeDefaultSubscription(index)"
                >
                  {{ t("common.delete") }}
                </button>
              </div>
            </div>
          </div>
        </div>

        <!-- ★ 新增：系统全局默认平台限额矩阵 -->
        <div class="border-t border-[var(--border)] pt-4">
          <div class="mb-3">
            <label class="font-medium text-[var(--foreground)]">
              {{ t("admin.settings.defaults.defaultPlatformQuotas") }}
            </label>
            <p class="mt-1 text-sm text-[var(--muted-foreground)]">
              {{ t("admin.settings.defaults.defaultPlatformQuotasHint") }}
            </p>
            <p class="mt-0.5 text-xs text-[var(--warn-text)]">
              {{ t("admin.settings.defaults.platformQuotaNotice") }}
            </p>
          </div>
          <div class="overflow-x-auto">
            <table class="min-w-full text-sm">
              <thead>
                <tr class="text-left text-xs text-[var(--muted-foreground)]">
                  <th class="pb-2 pr-4 font-medium">
                    {{ t("admin.settings.platformQuota.platform") }}
                  </th>
                  <th class="pb-2 pr-4 font-medium">
                    {{ t("admin.settings.platformQuota.daily") }}
                  </th>
                  <th class="pb-2 pr-4 font-medium">
                    {{ t("admin.settings.platformQuota.weekly") }}
                  </th>
                  <th class="pb-2 font-medium">
                    {{ t("admin.settings.platformQuota.monthly") }}
                  </th>
                </tr>
              </thead>
              <tbody class="space-y-2">
                <tr v-for="p in PLATFORMS" :key="p" class="align-top">
                  <td class="pr-4 py-1">
                    <span
                      class="font-mono text-xs text-[var(--foreground)]"
                      >{{ p }}</span
                    >
                  </td>
                  <td class="pr-4 py-1">
                    <input
                      v-model.number="form.default_platform_quotas[p]!.daily"
                      type="number"
                      step="0.01"
                      min="0"
                      class="input h-8 w-28 text-sm"
                      :placeholder="
                        t('admin.settings.platformQuota.placeholder')
                      "
                    />
                  </td>
                  <td class="pr-4 py-1">
                    <input
                      v-model.number="form.default_platform_quotas[p]!.weekly"
                      type="number"
                      step="0.01"
                      min="0"
                      class="input h-8 w-28 text-sm"
                      :placeholder="
                        t('admin.settings.platformQuota.placeholder')
                      "
                    />
                  </td>
                  <td class="py-1">
                    <input
                      v-model.number="form.default_platform_quotas[p]!.monthly"
                      type="number"
                      step="0.01"
                      min="0"
                      class="input h-8 w-28 text-sm"
                      :placeholder="
                        t('admin.settings.platformQuota.placeholder')
                      "
                    />
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
        <!-- /全局平台限额矩阵 -->
      </div>
    </div>

    <div class="card border-[var(--border)] bg-[var(--card)] shadow-none">
      <div class="border-b border-[var(--border)] px-4 py-4 sm:px-6">
        <h2 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
          {{ t("admin.settings.authSourceDefaults.title") }}
        </h2>
        <p class="mt-1 text-sm text-[var(--muted-foreground)]">
          {{ t("admin.settings.authSourceDefaults.description") }}
        </p>
      </div>
      <div class="space-y-6 p-4 sm:p-6">
        <div
          class="flex items-center justify-between rounded border border-[var(--border)] px-4 py-3"
        >
          <div>
            <label class="font-medium text-[var(--foreground)]">
              {{ t("admin.settings.authSourceDefaults.requireEmailLabel") }}
            </label>
            <p class="text-sm text-[var(--muted-foreground)]">
              {{ t("admin.settings.authSourceDefaults.requireEmailHint") }}
            </p>
          </div>
          <Toggle v-model="form.force_email_on_third_party_signup" />
        </div>

        <div class="space-y-4">
          <div
            v-for="authSource in authSourceDefaultsMeta"
            :key="authSource.source"
            class="rounded-xl border border-[var(--border)] p-4"
          >
            <div class="flex items-center justify-between gap-4">
              <div>
                <div class="font-medium text-[var(--foreground)]">
                  {{ authSource.title }}
                </div>
                <p class="mt-1 text-sm text-[var(--muted-foreground)]">
                  {{ authSource.description }}
                </p>
              </div>
              <Toggle
                v-model="authSourceDefaults[authSource.source].grant_on_signup"
                :data-testid="`auth-source-${authSource.source}-enabled`"
              />
            </div>

            <div
              v-if="authSourceDefaults[authSource.source].grant_on_signup"
              :data-testid="`auth-source-${authSource.source}-panel`"
              class="mt-4 space-y-4 border-t border-[var(--border)] pt-4"
            >
              <p class="text-sm text-[var(--muted-foreground)]">
                {{ t("admin.settings.authSourceDefaults.enabledHint") }}
              </p>

              <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
                <div>
                  <label
                    class="mb-2 block text-sm font-medium text-[var(--foreground)]"
                  >
                    {{ t("admin.settings.defaults.defaultBalance") }}
                  </label>
                  <input
                    v-model.number="
                      authSourceDefaults[authSource.source].balance
                    "
                    type="number"
                    step="0.01"
                    min="0"
                    class="input"
                    placeholder="0.00"
                  />
                </div>
                <div>
                  <label
                    class="mb-2 block text-sm font-medium text-[var(--foreground)]"
                  >
                    {{ t("admin.settings.defaults.defaultConcurrency") }}
                  </label>
                  <input
                    v-model.number="
                      authSourceDefaults[authSource.source].concurrency
                    "
                    type="number"
                    min="1"
                    class="input"
                    placeholder="5"
                  />
                </div>
              </div>

              <div
                class="flex items-center justify-between rounded border border-[var(--border)] px-4 py-3"
              >
                <div>
                  <label class="font-medium text-[var(--foreground)]">
                    {{
                      t(
                        "admin.settings.authSourceDefaults.grantOnFirstBindLabel",
                      )
                    }}
                  </label>
                  <p class="mt-0.5 text-xs text-[var(--muted-foreground)]">
                    {{
                      t(
                        "admin.settings.authSourceDefaults.grantOnFirstBindHint",
                      )
                    }}
                  </p>
                </div>
                <Toggle
                  v-model="
                    authSourceDefaults[authSource.source].grant_on_first_bind
                  "
                />
              </div>

              <div class="mb-3 flex items-center justify-between">
                <div>
                  <label class="font-medium text-[var(--foreground)]">
                    {{
                      t(
                        "admin.settings.authSourceDefaults.defaultSubscriptionsLabel",
                      )
                    }}
                  </label>
                  <p class="text-sm text-[var(--muted-foreground)]">
                    {{
                      t(
                        "admin.settings.authSourceDefaults.defaultSubscriptionsHint",
                      )
                    }}
                  </p>
                </div>
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  @click="addAuthSourceDefaultSubscription(authSource.source)"
                  :disabled="subscriptionGroups.length === 0"
                >
                  {{ t("admin.settings.defaults.addDefaultSubscription") }}
                </button>
              </div>

              <div
                v-if="
                  authSourceDefaults[authSource.source].subscriptions.length ===
                  0
                "
                class="rounded border border-dashed border-[var(--border)] px-4 py-3 text-sm text-[var(--muted-foreground)]"
              >
                {{
                  t("admin.settings.authSourceDefaults.noSourceSubscriptions")
                }}
              </div>

              <div v-else class="space-y-3">
                <div
                  v-for="(item, index) in authSourceDefaults[authSource.source]
                    .subscriptions"
                  :key="`${authSource.source}-sub-${index}`"
                  class="grid grid-cols-1 gap-3 rounded border border-[var(--border)] p-3 md:grid-cols-[1fr_160px_auto]"
                >
                  <div>
                    <label
                      class="mb-1 block text-xs font-medium text-[var(--muted-foreground)]"
                    >
                      {{ t("admin.settings.defaults.subscriptionGroup") }}
                    </label>
                    <Select
                      v-model="item.group_id"
                      class="default-sub-group-select"
                      :options="defaultSubscriptionGroupOptions"
                      :placeholder="
                        t('admin.settings.defaults.subscriptionGroup')
                      "
                    >
                      <template #selected="{ option }">
                        <GroupBadge
                          v-if="option"
                          :name="
                            (
                              option as unknown as DefaultSubscriptionGroupOption
                            ).label
                          "
                          :platform="
                            (
                              option as unknown as DefaultSubscriptionGroupOption
                            ).platform
                          "
                          :subscription-type="
                            (
                              option as unknown as DefaultSubscriptionGroupOption
                            ).subscriptionType
                          "
                          :rate-multiplier="
                            (
                              option as unknown as DefaultSubscriptionGroupOption
                            ).rate
                          "
                        />
                        <span v-else class="text-[var(--muted-foreground)]">
                          {{ t("admin.settings.defaults.subscriptionGroup") }}
                        </span>
                      </template>
                      <template #option="{ option, selected }">
                        <GroupOptionItem
                          :name="
                            (
                              option as unknown as DefaultSubscriptionGroupOption
                            ).label
                          "
                          :platform="
                            (
                              option as unknown as DefaultSubscriptionGroupOption
                            ).platform
                          "
                          :subscription-type="
                            (
                              option as unknown as DefaultSubscriptionGroupOption
                            ).subscriptionType
                          "
                          :rate-multiplier="
                            (
                              option as unknown as DefaultSubscriptionGroupOption
                            ).rate
                          "
                          :description="
                            (
                              option as unknown as DefaultSubscriptionGroupOption
                            ).description
                          "
                          :selected="selected"
                        />
                      </template>
                    </Select>
                  </div>
                  <div>
                    <label
                      class="mb-1 block text-xs font-medium text-[var(--muted-foreground)]"
                    >
                      {{
                        t("admin.settings.defaults.subscriptionValidityDays")
                      }}
                    </label>
                    <input
                      v-model.number="item.validity_days"
                      type="number"
                      min="1"
                      max="36500"
                      class="input h-[42px]"
                    />
                  </div>
                  <div class="flex items-end">
                    <button
                      type="button"
                      class="btn btn-secondary w-full text-[var(--bad-text)] hover:text-[var(--bad-text)]"
                      @click="
                        removeAuthSourceDefaultSubscription(
                          authSource.source,
                          index,
                        )
                      "
                    >
                      {{ t("common.delete") }}
                    </button>
                  </div>
                </div>
              </div>

              <!-- ★ 新增：auth source 平台限额覆盖区块 -->
              <div class="border-t border-[var(--border)] pt-4">
                <div class="mb-3">
                  <label class="font-medium text-[var(--foreground)]">
                    {{
                      t(
                        "admin.settings.authSourceDefaults.platformQuotasOverride",
                      )
                    }}
                  </label>
                  <p class="mt-0.5 text-xs text-[var(--muted-foreground)]">
                    {{
                      t(
                        "admin.settings.authSourceDefaults.platformQuotasOverrideHint",
                      )
                    }}
                  </p>
                </div>
                <div class="overflow-x-auto">
                  <table class="min-w-full text-sm">
                    <thead>
                      <tr
                        class="text-left text-xs text-[var(--muted-foreground)]"
                      >
                        <th class="pb-2 pr-4 font-medium">
                          {{ t("admin.settings.platformQuota.platform") }}
                        </th>
                        <th class="pb-2 pr-4 font-medium">
                          {{ t("admin.settings.platformQuota.daily") }}
                        </th>
                        <th class="pb-2 pr-4 font-medium">
                          {{ t("admin.settings.platformQuota.weekly") }}
                        </th>
                        <th class="pb-2 font-medium">
                          {{ t("admin.settings.platformQuota.monthly") }}
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      <tr
                        v-for="p in PLATFORMS"
                        :key="`${authSource.source}-pq-${p}`"
                        class="align-top"
                      >
                        <td class="pr-4 py-1">
                          <span
                            class="font-mono text-xs text-[var(--foreground)]"
                            >{{ p }}</span
                          >
                        </td>
                        <td class="pr-4 py-1">
                          <input
                            v-model.number="
                              authSourceDefaults[authSource.source]
                                .platform_quotas[p]!.daily
                            "
                            type="number"
                            step="0.01"
                            min="0"
                            class="input h-8 w-28 text-sm"
                            :placeholder="
                              t('admin.settings.platformQuota.placeholder')
                            "
                          />
                        </td>
                        <td class="pr-4 py-1">
                          <input
                            v-model.number="
                              authSourceDefaults[authSource.source]
                                .platform_quotas[p]!.weekly
                            "
                            type="number"
                            step="0.01"
                            min="0"
                            class="input h-8 w-28 text-sm"
                            :placeholder="
                              t('admin.settings.platformQuota.placeholder')
                            "
                          />
                        </td>
                        <td class="py-1">
                          <input
                            v-model.number="
                              authSourceDefaults[authSource.source]
                                .platform_quotas[p]!.monthly
                            "
                            type="number"
                            step="0.01"
                            min="0"
                            class="input h-8 w-28 text-sm"
                            :placeholder="
                              t('admin.settings.platformQuota.placeholder')
                            "
                          />
                        </td>
                      </tr>
                    </tbody>
                  </table>
                </div>
              </div>
              <!-- /auth source 平台限额覆盖区块 -->
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { PLATFORMS } from "@/api/admin/settings";
import Select from "@/components/common/Select.vue";
import GroupBadge from "@/components/common/GroupBadge.vue";
import GroupOptionItem from "@/components/common/GroupOptionItem.vue";
import Toggle from "@/components/common/Toggle.vue";
import type { DefaultSubscriptionGroupOption } from "./useUserDefaults";
import { useSettingsContext } from "./settingsContext";
const {
  t,
  activeTab,
  subscriptionGroups,
  authSourceDefaults,
  authSourceDefaultsMeta,
  defaultSubscriptionGroupOptions,
  addDefaultSubscription,
  removeDefaultSubscription,
  addAuthSourceDefaultSubscription,
  removeAuthSourceDefaultSubscription,
  form,
} = useSettingsContext();
</script>
<style scoped>
.default-sub-group-select :deep(.select-trigger),
.default-sub-delete-btn {
  @apply h-[42px];
}
</style>
