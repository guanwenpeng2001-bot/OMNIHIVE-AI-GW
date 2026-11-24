<template>
  <AppLayout>
    <div class="w-full space-y-5">
      <!-- Loading State -->
      <div v-if="loading" class="flex items-center justify-center py-12">
        <LoadingSpinner size="md" color="primary" />
      </div>

      <!-- Settings Form -->
      <form v-else @submit.prevent="saveSettings" class="space-y-5" novalidate>
        <!-- 顶部设置工作台头栏 -->
        <div class="settings-toolbar card sticky top-14 z-30 px-3 py-3 sm:px-4">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <span class="pill pill-ghost font-mono text-xs tabular-nums">
                  [{{ activeTabOrder }}/09]
                </span>
                <h1 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
                  {{ activeDomainMeta.title }}
                </h1>
              </div>
            </div>

            <div class="contents sm:flex sm:flex-wrap sm:items-center sm:gap-2">
              <RouterLink to="/admin/settings/diagnostics" class="btn btn-secondary btn-sm">
                {{ t('settingsDiagnostics.title') }}
              </RouterLink>
              <!-- 设置项实时快速检索框 -->
              <div
                class="order-last flex w-full items-center gap-2 rounded-lg border px-3 py-1.5 text-sm transition-colors focus-within:border-[var(--ring)] sm:order-none sm:w-auto"
                style="border-color: var(--border); background: var(--card)"
              >
                <Icon name="search" size="sm" class="text-gray-400" />
                <input
                  v-model.trim="searchQuery"
                  type="search"
                  class="min-w-0 w-full bg-transparent text-xs text-[var(--foreground)] outline-none placeholder:text-[var(--muted-foreground)] sm:w-52"
                  :placeholder="t('adminPlatform.settings.search')"
                  :aria-label="t('adminPlatform.settings.searchLabel')"
                  @keydown.enter.prevent="applyFirstMatchedTab"
                />
                <button
                  v-if="searchQuery"
                  type="button"
                  class="text-xs text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
                  :title="t('adminPlatform.settings.clear')"
                  @click="searchQuery = ''"
                >
                  ✕
                </button>
              </div>

              <!-- 顶部快速保存按钮 -->
              <span
                v-if="activeTab !== 'backup'"
                class="rounded-full border border-[var(--border)] px-2 py-1 text-[11px]"
                :class="hasLocalChanges ? 'bg-[var(--accent)] text-[var(--brand-text)]' : 'text-[var(--muted-foreground)]'"
                role="status"
              >
                {{ loadFailed ? t('adminPlatform.settings.failed') : saving ? t('admin.settings.saving') : hasLocalChanges ? t('adminPlatform.settings.unsaved') : t('adminPlatform.settings.synced') }}
              </span>
              <button
                v-show="activeTab !== 'backup'"
                type="submit"
                :disabled="saving || loadFailed"
                class="btn btn-primary btn-sm"
              >
                <LoadingSpinner v-if="saving" size="sm" color="white" />
                {{
                  saving
                    ? t("admin.settings.saving")
                    : t("admin.settings.saveSettings")
                }}
              </button>
            </div>
          </div>

          <!-- 搜索结果快捷切换建议条 -->
          <div
            v-if="searchQuery"
            class="mt-3 flex flex-wrap items-center gap-2 border-t pt-3 text-xs"
            style="border-color: var(--border)"
          >
            <span class="text-gray-500 dark:text-gray-400">
              {{ t('adminPlatform.settings.matches', { count: matchedTabs.length }) }}
            </span>
            <template v-if="matchedTabs.length > 0">
              <span
                v-for="item in matchedTabs"
                :key="`suggest-${item.key}`"
                role="button"
                tabindex="0"
                class="chip cursor-pointer transition-colors hover:border-amber-500/60"
                :style="
                  activeTab === item.key
                    ? 'border-color: var(--brand); background: color-mix(in srgb, var(--brand) 14%, var(--card))'
                    : ''
                "
                @click="selectSettingsTab(item.key)"
                @keydown.enter.prevent="selectSettingsTab(item.key)"
              >
                <span class="font-mono text-[10px] opacity-60">{{ item.order }}</span>
                <span>{{ item.meta.title }}</span>
                <span class="opacity-50">· {{ item.meta.badge }}</span>
              </span>
            </template>
            <span v-else class="text-amber-600 dark:text-amber-400">
              {{ t('adminPlatform.settings.noMatches', { query: searchQuery }) }}
            </span>
          </div>
        </div>

        <!-- 主体双栏工作台布局：左二级配置域导航 + 右配置面板 -->
        <div class="grid items-start gap-4 md:grid-cols-[200px_minmax(0,1fr)]">
          <!-- 左栏：二级配置域导航 -->
          <aside class="settings-domain-rail card min-w-0 p-2.5">
            <div class="nav-group-title hidden px-2.5 pb-1.5 pt-1 md:block">
              {{ t('adminPlatform.settings.domains') }}
            </div>
            <nav
              class="flex gap-1 overflow-x-auto md:block md:space-y-1 md:overflow-visible"
              role="tablist"
              :aria-label="t('admin.settings.title')"
            >
              <button
                v-for="(tab, index) in settingsTabs"
                :key="tab.key"
                :id="`settings-tab-${tab.key}`"
                type="button"
                role="tab"
                :aria-selected="activeTab === tab.key"
                aria-controls="settings-panel"
                :tabindex="activeTab === tab.key ? 0 : -1"
                :class="[
                  'tree-item shrink-0 text-left md:w-full',
                  activeTab === tab.key && 'active',
                  searchQuery && !isTabMatched(tab.key) && 'opacity-40',
                  searchQuery && isTabMatched(tab.key) && activeTab !== tab.key && 'settings-tab-hit',
                ]"
                @click="selectSettingsTab(tab.key)"
                @keydown="handleSettingsTabKeydown($event, tab.key)"
              >
                <span class="settings-tab-icon flex h-6 w-6 shrink-0 items-center justify-center rounded-md">
                  <Icon :name="tab.icon" size="sm" />
                </span>
                <span class="settings-tab-label flex-1 whitespace-normal">{{
                  t(`admin.settings.tabs.${tab.key}`)
                }}</span>
                <span class="ml-auto font-mono text-[10px] tabular-nums opacity-45">
                  {{ formatTabOrder(index) }}
                </span>
              </button>
            </nav>

            <div
              class="mt-3 hidden border-t px-2.5 pt-2.5 text-[11px] leading-relaxed text-gray-500 dark:text-gray-400 md:block"
              style="border-color: var(--border)"
            >
              <div class="flex items-center justify-between">
                <span>{{ t('adminPlatform.settings.switch') }}</span>
                <span class="kbd">↑↓</span>
              </div>
              <div class="mt-1 flex items-center justify-between">
                <span>{{ t('adminPlatform.settings.commands') }}</span>
                <span class="kbd">⌘K</span>
              </div>
            </div>
          </aside>

          <!-- 右栏：配置面板容器 -->
          <div
            id="settings-panel"
            role="tabpanel"
            :aria-labelledby="`settings-tab-${activeTab}`"
            tabindex="0"
            class="min-w-0 space-y-4"
            @input="hasLocalChanges = true"
            @change="hasLocalChanges = true"
          >
            <!-- Tab: Security — Admin API Key -->
            <AdminApiKeySettings />
            <!-- /Tab: Security — Admin API Key -->

            <!-- Tab: Gateway -->
            <GatewayRuntimeSettings />
            <!-- /Tab: Gateway -->

            <!-- Tab: Security — Registration, Turnstile, LinuxDo -->
            <SecuritySettings />
            <!-- /Tab: Security — Registration, Turnstile, LinuxDo, OIDC -->

            <!-- Tab: Users -->
            <UserDefaultsSettings />
            <!-- /Tab: Users -->

            <!-- Tab: Gateway — Claude Code, Scheduling -->
            <GatewaySettings />
            <!-- /Tab: Gateway — Claude Code, Scheduling -->

            <!-- Tab: General -->
            <GeneralSettings />
            <!-- /Tab: General -->

            <!-- Tab: Login Agreement -->
            <LoginAgreementSettings />
            <!-- /Tab: Login Agreement -->

            <!-- Tab: Features (功能开关) -->
            <FeatureSettings /><!-- /Tab: Features -->

            <!-- Tab: Email -->
            <!-- Tab: Payment -->
            <PaymentSettings />

            <EmailSettings />
            <!-- /Tab: Email -->

            <!-- Tab: Backup -->
            <BackupSettingsSection />

          </div>
        </div>
      </form>

      <!-- Provider dialogs placed outside the settings form to prevent form submission bubbling -->
      <PaymentProviderDialog
        ref="providerDialogRef"
        :show="showProviderDialog"
        :saving="providerSaving"
        :editing="editingProvider"
        :all-key-options="providerKeyOptions"
        :enabled-key-options="enabledProviderKeyOptions"
        :all-payment-types="allPaymentTypes"
        :redirect-label="t('admin.settings.payment.easypayRedirect')"
        @close="showProviderDialog = false"
        @save="handleSaveProvider"
      />
      <ConfirmDialog
        :show="showDeleteProviderDialog"
        :title="t('admin.settings.payment.deleteProvider')"
        :message="t('admin.settings.payment.deleteProviderConfirm')"
        :confirm-text="t('common.delete')"
        danger
        @confirm="handleDeleteProvider"
        @cancel="showDeleteProviderDialog = false"
      />
      <ConfirmDialog
        :show="affiliateConfirmDialog.show"
        :title="affiliateConfirmDialog.title"
        :message="affiliateConfirmDialog.message"
        :confirm-text="affiliateConfirmDialog.confirmText"
        danger
        @confirm="handleAffiliateConfirm"
        @cancel="cancelAffiliateConfirm"
      />
      <!-- 关闭 step-up 开关等敏感保存操作触发的 TOTP 二次验证 -->
      <TotpStepUpDialog :controller="settingsStepUp" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { RouterLink } from 'vue-router'
import { computed, ref, watch } from "vue";
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import AppLayout from "@/components/layout/AppLayout.vue";
import Icon from "@/components/icons/Icon.vue";
import ConfirmDialog from "@/components/common/ConfirmDialog.vue";
import PaymentProviderDialog from "@/components/payment/PaymentProviderDialog.vue";
import TotpStepUpDialog from "@/components/auth/TotpStepUpDialog.vue";
import AdminApiKeySettings from "./settings/AdminApiKeySettings.vue";
import GatewayRuntimeSettings from "./settings/GatewayRuntimeSettings.vue";
import SecuritySettings from "./settings/SecuritySettings.vue";
import UserDefaultsSettings from "./settings/UserDefaultsSettings.vue";
import GatewaySettings from "./settings/GatewaySettings.vue";
import GeneralSettings from "./settings/GeneralSettings.vue";
import LoginAgreementSettings from "./settings/LoginAgreementSettings.vue";
import FeatureSettings from "./settings/FeatureSettings.vue";
import PaymentSettings from "./settings/PaymentSettings.vue";
import EmailSettings from "./settings/EmailSettings.vue";
import BackupSettingsSection from "./settings/BackupSettingsSection.vue";
import { useSettingsPage } from "./settings/useSettingsPage";
import { provideSettingsContext } from "./settings/settingsContext";

const settings = useSettingsPage();
provideSettingsContext(settings);
const {
  t,
  activeTab,
  settingsTabs,
  selectSettingsTab,
  handleSettingsTabKeydown,
  allPaymentTypes,
  providerSaving,
  showProviderDialog,
  showDeleteProviderDialog,
  editingProvider,
  providerDialogRef,
  providerKeyOptions,
  enabledProviderKeyOptions,
  handleSaveProvider,
  handleDeleteProvider,
  affiliateConfirmDialog,
  handleAffiliateConfirm,
  cancelAffiliateConfirm,
  settingsStepUp,
  loading,
  loadFailed,
  saving,
  saveSettings,
} = settings;

const hasLocalChanges = ref(false);
watch(settings.form, () => {
  if (!loading.value && !saving.value) hasLocalChanges.value = true;
}, { deep: true, flush: "sync" });
watch(() => settings.appStore.toasts?.filter(
  (toast) => toast.type === "success" && toast.message === t("admin.settings.settingsSaved")
).map((toast) => toast.id).join(","), (current, previous) => {
  if (saving.value && current && current !== previous) hasLocalChanges.value = false;
}, { flush: "sync" });
watch(loading, (value) => {
  if (!value) hasLocalChanges.value = false;
});

type SettingsTabKey = (typeof settingsTabs)[number]["key"];

interface DomainMeta {
  title: string;
  badge: string;
  summary: string;
  keywords: string;
}

function domainMeta(key: SettingsTabKey): DomainMeta {
  const prefix = `adminPlatform.settings.domainsMeta.${key}`
  return { title: t(`${prefix}.title`), badge: t(`${prefix}.badge`), summary: t(`${prefix}.summary`), keywords: t(`${prefix}.keywords`) };
}

const searchQuery = ref("");

const formatTabOrder = (index: number): string => String(index + 1).padStart(2, "0");

const activeTabOrder = computed(() => {
  const idx = settingsTabs.findIndex((item) => item.key === activeTab.value);
  return formatTabOrder(idx >= 0 ? idx : 0);
});

const activeDomainMeta = computed<DomainMeta>(() => {
  return domainMeta(activeTab.value);
});

function isTabMatched(key: SettingsTabKey): boolean {
  const q = searchQuery.value.trim().toLowerCase();
  if (!q) return true;
  const meta = domainMeta(key);
  const i18nLabel = String(t(`admin.settings.tabs.${key}`)).toLowerCase();
  const haystack = `${meta?.title ?? ""} ${meta?.badge ?? ""} ${meta?.summary ?? ""} ${meta?.keywords ?? ""} ${i18nLabel} ${key}`.toLowerCase();
  return haystack.includes(q);
}

const matchedTabs = computed(() =>
  settingsTabs
    .map((tab, index) => ({
      key: tab.key,
      order: formatTabOrder(index),
      meta: domainMeta(tab.key),
    }))
    .filter((item) => isTabMatched(item.key))
);

function applyFirstMatchedTab(): void {
  const first = matchedTabs.value[0];
  if (first) {
    selectSettingsTab(first.key);
  }
}
</script>

<style scoped>
.settings-toolbar {
  background: var(--card);
  border-color: var(--border);
  box-shadow: 0 4px 12px color-mix(in srgb, var(--foreground) 4%, transparent);
}

.default-sub-group-select :deep(.select-trigger) {
  @apply h-[42px];
}

.default-sub-delete-btn {
  @apply h-[42px];
}

/* ============ 系统设置 Tab 导航 ============ */
.settings-tab-icon {
  color: var(--muted-foreground);
  transition: color 0.15s var(--ease);
}

.tree-item.active .settings-tab-icon {
  color: var(--brand-text);
}

.settings-tab-hit {
  border: 1px dashed color-mix(in srgb, var(--brand) 55%, var(--border));
}
</style>
