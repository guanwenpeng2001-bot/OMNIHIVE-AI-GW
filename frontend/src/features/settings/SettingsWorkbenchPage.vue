<script setup lang="ts">
import { computed, toRaw, onScopeDispose, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { isStepUpCancelled, useStepUp } from '@/composables/useStepUp'
import { adminAPI } from '@/api'
import type { SystemSettings } from '@/api/admin/settings'
import {
  Badge,
  Button,
  Card,
  ErrorState,
  Input,
  PageHeader,
  Select,
  Skeleton,
  Switch,
  Tabs,
  pushUiToast
} from '@/components/ui'
import type { UiTabItem } from '@/components/ui'
import { classifyAsyncError } from '@/features/shared/asyncState'
import ConfigDiagnosticsView from '@/views/admin/settings/ConfigDiagnosticsView.vue'
import type { DiagnosticsSyncSnapshot } from '@/views/admin/settings/useConfigDiagnostics'
import {
  SETTINGS_SECTIONS,
  buildSectionPatch,
  getDirtySections,
  isSectionDirty,
  resetSectionDraft,
  validateSettingsSection,
  type SettingsDraft,
  type SettingsSectionId
} from './sections'

const props = withDefaults(
  defineProps<{
    initialSection?: SettingsSectionId
  }>(),
  { initialSection: 'general' }
)

const { t } = useI18n()

const emit = defineEmits<{ 'select-section': [section: string] }>()
const activeSection = computed(() => props.initialSection)
const stepUp = useStepUp()
onScopeDispose(stepUp.onCancel)
const sectionFilter = ref('')

const hasLoadedSettings = ref(false)
const loading = ref(props.initialSection !== 'diagnostics')
const refreshing = ref(false)
const saving = ref(false)
const testingSmtp = ref(false)
const lastSyncedAt = ref<string | undefined>(undefined)

const baseline = ref<SettingsDraft>({})
const draft = ref<SettingsDraft>({})
const fieldErrors = ref<Record<string, string>>({})

const loadError = ref<{ state: 'error' | 'forbidden' | 'unknown'; message: string; requestId?: string } | null>(null)
const refreshError = ref<{ message: string; requestId?: string } | null>(null)
const saveError = ref<{ message: string; requestId?: string } | null>(null)
const mutationConflict = ref<{ message: string; requestId?: string } | null>(null)
const mutationUnknown = ref<{ message: string; requestId?: string } | null>(null)
const confirmedMessage = ref<string | null>(null)

const diagnosticsRef = ref<{ reload: () => Promise<void> } | null>(null)
const diagnosticsSync = ref<DiagnosticsSyncSnapshot>({
  syncState: 'loading',
  refreshing: false,
  unhealthyCount: 0,
  generatedAt: ''
})

const dirtySections = computed(() => getDirtySections(baseline.value, draft.value))
const activeSectionDirty = computed(() => isSectionDirty(activeSection.value, baseline.value, draft.value))
const activeMeta = computed(
  () => SETTINGS_SECTIONS.find((s) => s.id === activeSection.value) ?? SETTINGS_SECTIONS[0]
)

const visibleSections = computed(() => {
  const query = sectionFilter.value.trim().toLowerCase()
  if (!query) return SETTINGS_SECTIONS
  return SETTINGS_SECTIONS.filter(
    (sec) =>
      t(`adminPlatform.workbench.sections.${sec.id}.title`).toLowerCase().includes(query) ||
      t(`adminPlatform.workbench.sections.${sec.id}.description`).toLowerCase().includes(query) ||
      sec.fields.some((f) => String(f).toLowerCase().includes(query))
  )
})

const tabItems = computed<UiTabItem[]>(() =>
  visibleSections.value.map((sec) => ({
    value: sec.id,
    label: t(`adminPlatform.workbench.sections.${sec.id}.title`),
    badge: dirtySections.value.includes(sec.id) ? t('adminPlatform.workbench.unsaved') : undefined
  }))
)

function hydrateSettings(data: SystemSettings): void {
  const sanitized: SettingsDraft = {
    ...data,
    smtp_password: '',
    turnstile_secret_key: ''
  }
  baseline.value = structuredClone(sanitized)
  draft.value = structuredClone(sanitized)
  fieldErrors.value = {}
}

async function loadSettings(isRefresh = false): Promise<void> {
  if (isRefresh && hasLoadedSettings.value) {
    refreshing.value = true
  } else {
    loading.value = true
  }
  loadError.value = null
  refreshError.value = null
  saveError.value = null
  mutationConflict.value = null
  mutationUnknown.value = null

  try {
    const data = await adminAPI.settings.getSettings()
    hydrateSettings(data)
    hasLoadedSettings.value = true
    lastSyncedAt.value = new Date().toLocaleTimeString()
  } catch (error) {
    const err = classifyAsyncError(error, t('adminPlatform.workbench.failedToLoadSystemSettings'))
    if (hasLoadedSettings.value) {
      refreshError.value = {
        message: err.message,
        requestId: err.requestId
      }
    } else {
      loadError.value = {
        state: err.state === 'forbidden' ? 'forbidden' : err.state === 'unknown' ? 'unknown' : 'error',
        message: err.message,
        requestId: err.requestId
      }
    }
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

function handleResetSection(): void {
  draft.value = resetSectionDraft(activeSection.value, baseline.value, draft.value)
  fieldErrors.value = {}
  saveError.value = null
  mutationConflict.value = null
  mutationUnknown.value = null
}

async function handleSaveSection(): Promise<void> {
  const section = activeSection.value
  const activeLabel = t(`adminPlatform.workbench.sections.${activeMeta.value.id}.title`)
  fieldErrors.value = validateSettingsSection(section, draft.value, baseline.value)
  if (Object.keys(fieldErrors.value).length > 0) {
    pushUiToast({
      tone: 'warning',
      title: t('adminPlatform.workbench.validationFailed'),
      description: t('adminPlatform.workbench.fixHighlightedFieldsBeforeSaving')
    })
    return
  }

  const patch = buildSectionPatch(activeSection.value, baseline.value, draft.value)
  if (Object.keys(patch).length === 0) {
    confirmedMessage.value = t('adminPlatform.workbench.noChangesToSaveInThisSection')
    return
  }

  saving.value = true
  saveError.value = null
  mutationConflict.value = null
  mutationUnknown.value = null
  confirmedMessage.value = null

  try {
    const updated = await stepUp.run(() => adminAPI.settings.updateSettings(patch))
    const nextBaseline: SettingsDraft = {
      ...toRaw(baseline.value),
      ...updated,
      smtp_password: '',
      turnstile_secret_key: ''
    }
    baseline.value = structuredClone(nextBaseline)
    draft.value = resetSectionDraft(section, nextBaseline, draft.value)
    lastSyncedAt.value = new Date().toLocaleTimeString()
    confirmedMessage.value = t('adminPlatform.workbench.saved', { section: activeLabel })
    pushUiToast({
      tone: 'success',
      title: t('adminPlatform.workbench.settingsSaved'),
      description: activeLabel
    })
  } catch (error) {
    if (isStepUpCancelled(error)) return
    const err = classifyAsyncError(error, t('adminPlatform.workbench.failedToSaveSettings'))
    if (err.state === 'conflict') {
      mutationConflict.value = { message: err.message, requestId: err.requestId }
    } else if (err.state === 'unknown') {
      mutationUnknown.value = { message: err.message, requestId: err.requestId }
    } else {
      saveError.value = { message: err.message, requestId: err.requestId }
      pushUiToast({ tone: 'danger', title: t('adminPlatform.workbench.saveFailed'), description: err.message })
    }
  } finally {
    saving.value = false
  }
}

async function handleTestSmtp(): Promise<void> {
  testingSmtp.value = true
  try {
    const res = await adminAPI.settings.testSmtpConnection({
      smtp_host: String(draft.value.smtp_host ?? ''),
      smtp_port: Number(draft.value.smtp_port ?? 587),
      smtp_username: String(draft.value.smtp_username ?? ''),
      smtp_password: String(draft.value.smtp_password ?? ''),
      smtp_use_tls: Boolean(draft.value.smtp_use_tls)
    })
    pushUiToast({ tone: 'success', title: t('adminPlatform.workbench.smtpConnectionOK'), description: res.message })
  } catch (error) {
    const err = classifyAsyncError(error, t('adminPlatform.workbench.smtpTestFailed'))
    pushUiToast({ tone: 'danger', title: t('adminPlatform.workbench.smtpTestFailed'), description: err.message })
  } finally {
    testingSmtp.value = false
  }
}

function handleDiagnosticsSyncState(snapshot: DiagnosticsSyncSnapshot): void {
  diagnosticsSync.value = snapshot
}

function handleDiagnosticsSettingsUpdated(updated: SystemSettings): void {
  if (!hasLoadedSettings.value) return
  const previouslyDirty = new Set<SettingsSectionId>(dirtySections.value)
  const nextBaseline: SettingsDraft = {
    ...toRaw(baseline.value),
    ...updated,
    smtp_password: '',
    turnstile_secret_key: ''
  }
  baseline.value = structuredClone(nextBaseline)
  let nextDraft = structuredClone(toRaw(draft.value))
  for (const sec of SETTINGS_SECTIONS) {
    if (!previouslyDirty.has(sec.id)) {
      nextDraft = resetSectionDraft(sec.id, nextBaseline, nextDraft)
    }
  }
  draft.value = nextDraft
  lastSyncedAt.value = new Date().toLocaleTimeString()
}

function handleHeaderRefresh(): void {
  if (activeSection.value === 'diagnostics') {
    void diagnosticsRef.value?.reload()
    return
  }
  void loadSettings(true)
}

watch(activeSection, (nextSection) => {
  if (nextSection !== 'diagnostics' && !hasLoadedSettings.value && !loading.value) {
    void loadSettings(false)
  }
})

onMounted(() => {
  if (activeSection.value !== 'diagnostics') {
    void loadSettings(false)
  }
})
</script>

<template>
  <div class="space-y-5">
    <PageHeader
      :eyebrow="t('adminPlatform.workbench.systemCONFIGURATION')"
      :title="t('adminPlatform.workbench.systemSettings')"
      :description="t('adminPlatform.workbench.sectionedPlatformConfigurationWithPerSectionDirty')"
    >
      <template #badges>
        <template v-if="activeSection === 'diagnostics'">
          <Badge
            v-if="diagnosticsSync.syncState === 'loading' || diagnosticsSync.syncState === 'refreshing'"
            tone="neutral"
            variant="subtle"
            data-testid="workbench-sync-badge"
          >
            {{ t('settingsDiagnostics.syncBadges.loading') }}
          </Badge>
          <Badge
            v-else-if="diagnosticsSync.syncState === 'error'"
            tone="danger"
            variant="subtle"
            data-testid="workbench-sync-badge"
          >
            {{ t('settingsDiagnostics.syncBadges.notSynced') }}
          </Badge>
          <Badge
            v-else-if="diagnosticsSync.syncState === 'stale'"
            tone="warning"
            variant="subtle"
            data-testid="workbench-sync-badge"
          >
            {{ t('settingsDiagnostics.syncBadges.stale') }}
          </Badge>
          <Badge
            v-else
            tone="success"
            variant="subtle"
            data-testid="workbench-sync-badge"
          >
            {{ t('settingsDiagnostics.syncBadges.synced') }}
          </Badge>
          <Badge
            v-if="diagnosticsSync.unhealthyCount > 0"
            tone="danger"
            variant="subtle"
            mono
            data-testid="workbench-diagnostics-issues-badge"
          >
            {{ t('settingsDiagnostics.syncBadges.issuesCount', { count: diagnosticsSync.unhealthyCount }) }}
          </Badge>
        </template>
        <template v-else>
          <Badge
            v-if="loading && !hasLoadedSettings"
            tone="neutral"
            variant="subtle"
            data-testid="workbench-sync-badge"
          >
            {{ t('settingsDiagnostics.workbench.loadingBadge') }}
          </Badge>
          <Badge
            v-else-if="loadError"
            tone="danger"
            variant="subtle"
            data-testid="workbench-sync-badge"
          >
            {{ t('settingsDiagnostics.workbench.notSyncedBadge') }}
          </Badge>
          <Badge
            v-else-if="refreshError || mutationConflict || mutationUnknown || saveError"
            tone="warning"
            variant="subtle"
            data-testid="workbench-sync-badge"
          >
            {{ t('settingsDiagnostics.workbench.syncFailedBadge') }}
          </Badge>
          <Badge
            v-else-if="dirtySections.length > 0"
            tone="warning"
            variant="subtle"
            mono
            data-testid="workbench-sync-badge"
          >
            {{ t('adminPlatform.workbench.dirty', { count: dirtySections.length }) }}
          </Badge>
          <Badge
            v-else-if="hasLoadedSettings"
            tone="success"
            variant="subtle"
            data-testid="workbench-sync-badge"
          >
            {{ t('adminPlatform.workbench.synced') }}
          </Badge>
        </template>
      </template>

      <template #actions>
        <div v-if="activeSection !== 'diagnostics'" class="w-full sm:w-56">
          <Input v-model="sectionFilter" type="search" :placeholder="t('adminPlatform.workbench.filterSectionsOrKeys')" size="sm" clearable />
        </div>
        <Button
          size="sm"
          variant="secondary"
          :loading="activeSection === 'diagnostics' ? diagnosticsSync.refreshing : refreshing"
          @click="handleHeaderRefresh"
        >
          {{ t('adminPlatform.workbench.refresh') }}
        </Button>
      </template>

      <template #tabs>
        <Tabs :model-value="activeSection" :items="tabItems" @update:model-value="emit('select-section', $event)" />
      </template>
    </PageHeader>

    <ConfigDiagnosticsView
      v-if="activeSection === 'diagnostics'"
      ref="diagnosticsRef"
      embedded
      @sync-state-change="handleDiagnosticsSyncState"
      @settings-updated="handleDiagnosticsSettingsUpdated"
    />

    <div v-else-if="loading" class="space-y-3">
      <Skeleton height="3rem" />
      <Skeleton height="16rem" />
    </div>

    <ErrorState
      v-else-if="loadError"
      :variant="loadError.state === 'forbidden' ? 'permission' : 'error'"
      :title="loadError.state === 'forbidden' ? (t('adminPlatform.workbench.adminPermissionRequired403')) : (t('adminPlatform.workbench.failedToLoadSettings'))"
      :message="loadError.message"
      :request-id="loadError.requestId"
      @retry="loadSettings(false)"
    />

    <template v-else>
      <ErrorState
        v-if="refreshError"
        variant="stale"
        compact
        :title="t('settingsDiagnostics.workbench.refreshFailedTitle')"
        :message="refreshError.message"
        :request-id="refreshError.requestId"
        :last-synced-at="lastSyncedAt"
        @retry="loadSettings(true)"
      />

      <ErrorState
        v-if="saveError"
        variant="error"
        compact
        :title="t('settingsDiagnostics.workbench.saveFailedTitle')"
        :message="saveError.message"
        :request-id="saveError.requestId"
        :retryable="false"
      />

      <ErrorState
        v-if="mutationConflict"
        variant="stale"
        :title="t('adminPlatform.workbench.409SettingsConflict')"
        :message="mutationConflict.message"
        :request-id="mutationConflict.requestId"
        :retry-label="t('adminPlatform.workbench.reloadLatestSettings')"
        @retry="loadSettings(true)"
      />

      <ErrorState
        v-if="mutationUnknown"
        variant="stale"
        :title="t('adminPlatform.workbench.unknownSaveOutcome')"
        :message="mutationUnknown.message"
        :request-id="mutationUnknown.requestId"
        :retry-label="t('adminPlatform.workbench.verifyLatestSettings')"
        @retry="loadSettings(true)"
      />

      <div
        v-if="confirmedMessage"
        role="status"
        class="rounded-oh-md border border-oh-status-ok-border bg-oh-status-ok-bg/60 px-4 py-2.5 text-xs text-oh-primary flex items-center justify-between"
      >
        <span>{{ confirmedMessage }}</span>
        <span v-if="lastSyncedAt" class="font-mono text-oh-muted tabular-nums">{{ lastSyncedAt }}</span>
      </div>

      <Card padding="md">
        <div class="mb-5 flex flex-wrap items-start justify-between gap-3 border-b border-oh-subtle pb-4">
          <div>
            <h2 class="text-base font-semibold text-oh-primary">{{ t(`adminPlatform.workbench.sections.${activeMeta.id}.title`) }}</h2>
            <p class="mt-0.5 text-xs text-oh-secondary">{{ t(`adminPlatform.workbench.sections.${activeMeta.id}.description`) }}</p>
          </div>
          <div class="flex items-center gap-2">
            <Button size="sm" variant="ghost" :disabled="!activeSectionDirty || saving" @click="handleResetSection">
              {{ t('adminPlatform.workbench.resetSection') }}
            </Button>
            <Button size="sm" variant="primary" :loading="saving" :disabled="!activeSectionDirty" @click="handleSaveSection">
              {{ t('adminPlatform.workbench.saveSection') }}
            </Button>
          </div>
        </div>

        <div v-if="activeSection === 'general'" class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Input v-model="(draft.site_name as string)" :label="t('adminPlatform.workbench.siteName')" />
          <Input v-model="(draft.site_subtitle as string)" :label="t('adminPlatform.workbench.siteSubtitle')" />
          <Input
            v-model="(draft.frontend_url as string)"
            :label="t('adminPlatform.workbench.frontendURL')"
            :error="fieldErrors.frontend_url && t(fieldErrors.frontend_url)"
            mono
          />
          <div class="space-y-3 pt-1">
            <Switch
              :model-value="Boolean(draft.registration_enabled)"
              :label="t('adminPlatform.workbench.publicRegistrationEnabled')"
              @update:model-value="(v) => (draft.registration_enabled = v)"
            />
            <Switch
              :model-value="Boolean(draft.invitation_code_enabled)"
              :label="t('adminPlatform.workbench.requireInvitationCode')"
              @update:model-value="(v) => (draft.invitation_code_enabled = v)"
            />
          </div>
        </div>

        <div v-else-if="activeSection === 'security'" class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Switch
            :model-value="Boolean(draft.totp_enabled)"
            :label="t('adminPlatform.workbench.totp2FAEnabled')"
            @update:model-value="(v) => (draft.totp_enabled = v)"
          />
          <Switch
            :model-value="Boolean(draft.passkey_enabled)"
            :label="t('adminPlatform.workbench.passkeyAuthenticationEnabled')"
            @update:model-value="(v) => (draft.passkey_enabled = v)"
          />
          <Switch
            :model-value="Boolean(draft.session_binding_enabled)"
            :label="t('adminPlatform.workbench.sessionIPUABinding')"
            @update:model-value="(v) => (draft.session_binding_enabled = v)"
          />
          <Switch
            id="settings-step-up"
            :model-value="Boolean(draft.step_up_enabled)"
            :label="t('adminPlatform.workbench.stepUp2FAForSensitiveActions')"
            @update:model-value="(v) => (draft.step_up_enabled = v)"
          />
          <Input :model-value="draft.passkey_rp_id as string" readonly label="Passkey RP ID" :hint="t('adminPlatform.settings.rpHint')" mono />
          <Input
            v-model.number="(draft.audit_log_retention_days as number)"
            id="settings-audit_log_retention_days"
            type="number"
            :label="t('adminPlatform.workbench.auditLogRetentionDays')"
            :error="fieldErrors.audit_log_retention_days && t(fieldErrors.audit_log_retention_days)"
            mono
          />
          <Switch
            :model-value="Boolean(draft.turnstile_enabled)"
            :label="t('adminPlatform.workbench.cloudflareTurnstileEnabled')"
            @update:model-value="(v) => (draft.turnstile_enabled = v)"
          />
          <Input v-model="(draft.turnstile_site_key as string)" :label="t('adminPlatform.workbench.turnstileSiteKey')" mono />
          <Input
            v-model="(draft.turnstile_secret_key as string)"
            type="password"
            :label="t('adminPlatform.workbench.turnstileSecretKey')"
            :placeholder="t('adminPlatform.workbench.configuredLeaveBlankToKeepExisting')"
            mono
          />
        </div>

        <div v-else-if="activeSection === 'users'" class="grid grid-cols-1 gap-4 md:grid-cols-3">
          <Input
            v-model.number="(draft.default_balance as number)"
            id="settings-default_balance"
            type="number"
            :label="t('adminPlatform.workbench.defaultBalanceUSD')"
            :error="fieldErrors.default_balance && t(fieldErrors.default_balance)"
            mono
          />
          <Input
            v-model.number="(draft.default_concurrency as number)"
            id="settings-default_concurrency"
            type="number"
            :label="t('adminPlatform.workbench.defaultConcurrency')"
            :error="fieldErrors.default_concurrency && t(fieldErrors.default_concurrency)"
            mono
          />
          <Input
            v-model.number="(draft.default_user_rpm_limit as number)"
            id="settings-default_user_rpm_limit"
            :error="fieldErrors.default_user_rpm_limit && t(fieldErrors.default_user_rpm_limit)"
            type="number"
            :label="t('adminPlatform.workbench.defaultUserRPMLimit')"
            mono
          />
          <Input
            v-model.number="(draft.affiliate_rebate_rate as number)"
            id="settings-affiliate_rebate_rate"
            type="number"
            :label="t('adminPlatform.workbench.affiliateRebateRate0100')"
            :error="fieldErrors.affiliate_rebate_rate && t(fieldErrors.affiliate_rebate_rate)"
            mono
          />
          <Input
            v-model.number="(draft.affiliate_rebate_freeze_hours as number)"
            id="settings-affiliate_rebate_freeze_hours"
            :error="fieldErrors.affiliate_rebate_freeze_hours && t(fieldErrors.affiliate_rebate_freeze_hours)"
            type="number"
            :label="t('adminPlatform.workbench.rebateFreezeHours')"
            mono
          />
          <Input
            v-model.number="(draft.affiliate_rebate_duration_days as number)"
            id="settings-affiliate_rebate_duration_days"
            :error="fieldErrors.affiliate_rebate_duration_days && t(fieldErrors.affiliate_rebate_duration_days)"
            type="number"
            :label="t('adminPlatform.workbench.rebateDurationDays')"
            mono
          />
        </div>

        <div v-else-if="activeSection === 'gateway'" class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Input
            v-model="(draft.api_base_url as string)"
            :label="t('adminPlatform.workbench.gatewayAPIBaseURL')"
            :error="fieldErrors.api_base_url && t(fieldErrors.api_base_url)"
            mono
          />
          <Input v-model="(draft.doc_url as string)" :label="t('adminPlatform.workbench.documentationURL')" mono />
          <Input v-model="(draft.contact_info as string)" :label="t('adminPlatform.workbench.operatorContactInfo')" />
          <div class="pt-5">
            <Switch
              :model-value="Boolean(draft.hide_ccs_import_button)"
              :label="t('adminPlatform.workbench.hideCCSImportButton')"
              @update:model-value="(v) => (draft.hide_ccs_import_button = v)"
            />
          </div>
        </div>

        <div v-else-if="activeSection === 'payment'" class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Switch
            :model-value="Boolean(draft.payment_enabled)"
            :label="t('adminPlatform.workbench.enableOnlinePayments')"
            @update:model-value="(v) => (draft.payment_enabled = v)"
          />
          <Switch
            :model-value="Boolean(draft.promo_code_enabled)"
            :label="t('adminPlatform.workbench.enablePromoCodes')"
            @update:model-value="(v) => (draft.promo_code_enabled = v)"
          />
        </div>

        <div v-else-if="activeSection === 'email'" class="space-y-4">
          <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
            <Switch
              :model-value="Boolean(draft.email_verify_enabled)"
              :label="t('adminPlatform.workbench.requireEmailVerification')"
              @update:model-value="(v) => (draft.email_verify_enabled = v)"
            />
            <Switch
              :model-value="Boolean(draft.password_reset_enabled)"
              :label="t('adminPlatform.workbench.allowPasswordReset')"
              @update:model-value="(v) => (draft.password_reset_enabled = v)"
            />
            <Input v-model="(draft.smtp_host as string)" :label="t('adminPlatform.workbench.smtpHost')" mono />
            <Input
              v-model.number="(draft.smtp_port as number)"
              id="settings-smtp_port"
              type="number"
              :label="t('adminPlatform.workbench.smtpPort')"
              :error="fieldErrors.smtp_port && t(fieldErrors.smtp_port)"
              mono
            />
            <Input v-model="(draft.smtp_username as string)" :label="t('adminPlatform.workbench.smtpUsername')" mono />
            <Input
              v-model="(draft.smtp_password as string)"
              type="password"
              :label="t('adminPlatform.workbench.smtpPassword')"
              :placeholder="t('adminPlatform.workbench.configuredLeaveBlankToKeepExisting')"
              mono
            />
            <Input v-model="(draft.smtp_from_email as string)" :label="t('adminPlatform.workbench.fromEmail')" mono />
            <Input v-model="(draft.smtp_from_name as string)" :label="t('adminPlatform.workbench.fromName')" />
            <Switch
              :model-value="Boolean(draft.smtp_use_tls)"
              :label="t('adminPlatform.workbench.useTLS')"
              @update:model-value="(v) => (draft.smtp_use_tls = v)"
            />
          </div>
          <div>
            <Button size="sm" variant="secondary" :loading="testingSmtp" @click="handleTestSmtp">
              {{ t('adminPlatform.workbench.testSMTPConnection') }}
            </Button>
          </div>
        </div>

        <div v-else-if="activeSection === 'features'" class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Switch
            :model-value="Boolean(draft.registration_email_domain_quota_enabled)"
            :label="t('adminPlatform.workbench.emailDomainQuotaEnforcement')"
            @update:model-value="(v) => (draft.registration_email_domain_quota_enabled = v)"
          />
          <Switch
            :model-value="Boolean(draft.user_custom_key_enabled)"
            :label="t('adminPlatform.workbench.allowUserCustomAPIKeys')"
            @update:model-value="(v) => (draft.user_custom_key_enabled = v)"
          />
        </div>

        <div v-else-if="activeSection === 'agreement'" class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Switch
            :model-value="Boolean(draft.login_agreement_enabled)"
            :label="t('adminPlatform.workbench.requireLoginAgreement')"
            @update:model-value="(v) => (draft.login_agreement_enabled = v)"
          />
          <Select
            v-model="(draft.login_agreement_mode as string)"
            :label="t('adminPlatform.workbench.agreementPresentationMode')"
            :options="[
              { value: 'checkbox', label: t('adminPlatform.workbench.checkbox') },
              { value: 'modal', label: t('adminPlatform.workbench.modal') }
            ]"
          />
        </div>
      </Card>
    </template>
  </div>
  <TotpStepUpDialog :controller="stepUp" />
</template>
