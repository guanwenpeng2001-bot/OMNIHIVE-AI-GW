<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-6">
      <!-- Loading State -->
      <div v-if="loading" class="flex items-center justify-center py-12">
        <LoadingSpinner size="md" color="primary" />
      </div>

      <!-- Settings Form -->
      <form v-else @submit.prevent="saveSettings" class="space-y-6" novalidate>
        <!-- Tab Navigation -->
        <div class="settings-tabs-shell">
          <nav
            class="settings-tabs-scroll"
            role="tablist"
            :aria-label="t('admin.settings.title')"
          >
            <div class="settings-tabs">
              <button
                v-for="tab in settingsTabs"
                :key="tab.key"
                :id="`settings-tab-${tab.key}`"
                type="button"
                role="tab"
                :aria-selected="activeTab === tab.key"
                aria-controls="settings-panel"
                :tabindex="activeTab === tab.key ? 0 : -1"
                :class="[
                  'settings-tab',
                  activeTab === tab.key && 'settings-tab-active',
                ]"
                @click="selectSettingsTab(tab.key)"
                @keydown="handleSettingsTabKeydown($event, tab.key)"
              >
                <span class="settings-tab-icon">
                  <Icon :name="tab.icon" size="sm" />
                </span>
                <span class="settings-tab-label">{{
                  t(`admin.settings.tabs.${tab.key}`)
                }}</span>
              </button>
            </div>
          </nav>
        </div>

        <div id="settings-panel" role="tabpanel" :aria-labelledby="`settings-tab-${activeTab}`" tabindex="0" class="space-y-6">
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

        <!-- Save Button -->
        <div v-show="activeTab !== 'backup'" class="flex justify-end">
          <button
            type="submit"
            :disabled="saving || loadFailed"
            class="btn btn-primary"
          >
            <LoadingSpinner v-if="saving" size="sm" color="white" />
            {{
              saving
                ? t("admin.settings.saving")
                : t("admin.settings.saveSettings")
            }}
          </button>
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
</script>

<style scoped>
.default-sub-group-select :deep(.select-trigger) {
  @apply h-[42px];
}

.default-sub-delete-btn {
  @apply h-[42px];
}

/* ============ 系统设置 Tab 导航 ============ */
.settings-tabs-shell {
  @apply sticky z-20 -mx-1 rounded-2xl border border-white/80 bg-white/90 p-1.5 backdrop-blur-xl;
  top: 4.75rem;
  box-shadow:
    0 12px 28px rgb(15 23 42 / 0.07),
    0 1px 0 rgb(255 255 255 / 0.9) inset;
}

.settings-tabs-scroll {
  @apply overflow-x-auto;
  -ms-overflow-style: none;
  scrollbar-width: none;
}

.settings-tabs-scroll::-webkit-scrollbar {
  display: none;
}

.settings-tabs {
  @apply flex min-w-max items-center gap-1;
}

.settings-tab {
  @apply relative isolate flex h-10 min-w-[6.75rem] shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-xl border border-transparent px-3 text-sm font-medium text-gray-600 outline-none transition-colors duration-200 ease-out dark:text-gray-300;
}

@media (min-width: 768px) {
  .settings-tabs {
    @apply min-w-full;
  }

  .settings-tab {
    @apply min-w-0 flex-1 basis-0 overflow-hidden px-2 text-[13px];
  }

  .settings-tab-icon {
    @apply h-6 w-6;
  }
}

.settings-tab::before {
  @apply absolute inset-0 -z-10 rounded-xl opacity-0 transition-opacity duration-200;
  content: "";
  background: linear-gradient(
    135deg,
    rgb(248 250 252 / 0.95),
    rgb(241 245 249 / 0.8)
  );
}

.settings-tab:hover::before,
.settings-tab:focus-visible::before {
  opacity: 1;
}

.settings-tab:focus-visible {
  @apply ring-2 ring-primary-500/40 ring-offset-2 ring-offset-white dark:ring-offset-dark-900;
}

.settings-tab-active {
  @apply border-primary-200/80 bg-white text-primary-700 shadow-sm dark:border-primary-400/30 dark:bg-dark-700/95 dark:text-primary-200;
  box-shadow:
    0 8px 18px rgb(15 23 42 / 0.08),
    0 1px 0 rgb(255 255 255 / 0.92) inset;
}

.settings-tab-active::before {
  opacity: 0;
}

.settings-tab-active::after {
  position: absolute;
  right: 0.75rem;
  bottom: 0.25rem;
  left: 0.75rem;
  height: 2px;
  border-radius: 9999px;
  content: "";
  background: linear-gradient(90deg, #14b8a6, #0ea5e9);
}

.settings-tab-icon {
  @apply flex h-7 w-7 shrink-0 items-center justify-center rounded-lg text-gray-500 transition-colors duration-200 dark:text-gray-400;
}

.settings-tab:hover .settings-tab-icon,
.settings-tab:focus-visible .settings-tab-icon {
  @apply text-gray-700 dark:text-gray-200;
}

.settings-tab-active .settings-tab-icon {
  @apply bg-primary-50 text-primary-600 dark:bg-primary-400/10 dark:text-primary-300;
}

.settings-tab-label {
  @apply min-w-0 overflow-hidden text-ellipsis whitespace-nowrap leading-none;
}
</style>
<style>
/* Dark-mode overrides for the settings tabs shell. Kept in an UNSCOPED block
   because Vue's scoped-CSS compiler was dropping the `:global(.dark) ...`
   rules in the production build, leaving inactive tabs unreadable on dark. */
.dark .settings-tabs-shell {
  border-color: rgb(51 65 85 / 0.65);
  background: rgb(15 23 42 / 0.86);
  box-shadow:
    0 16px 36px rgb(0 0 0 / 0.28),
    0 1px 0 rgb(255 255 255 / 0.06) inset;
}

.dark .settings-tab::before {
  background: linear-gradient(
    135deg,
    rgb(30 41 59 / 0.9),
    rgb(51 65 85 / 0.62)
  );
}

.dark .settings-tab-active {
  box-shadow:
    0 12px 26px rgb(0 0 0 / 0.22),
    0 1px 0 rgb(255 255 255 / 0.08) inset;
}
</style>
