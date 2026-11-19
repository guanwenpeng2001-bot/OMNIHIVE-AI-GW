<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
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

const activeSection = ref<SettingsSectionId>(props.initialSection)
const sectionFilter = ref('')

const loading = ref(true)
const refreshing = ref(false)
const saving = ref(false)
const testingSmtp = ref(false)
const lastSyncedAt = ref<string | undefined>(undefined)

const baseline = ref<SettingsDraft>({})
const draft = ref<SettingsDraft>({})
const fieldErrors = ref<Record<string, string>>({})

const loadError = ref<{ state: 'error' | 'forbidden' | 'unknown'; message: string; requestId?: string } | null>(null)
const mutationConflict = ref<{ message: string; requestId?: string } | null>(null)
const mutationUnknown = ref<{ message: string; requestId?: string } | null>(null)
const confirmedMessage = ref<string | null>(null)

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
      sec.label.toLowerCase().includes(query) ||
      sec.description.toLowerCase().includes(query) ||
      sec.fields.some((f) => String(f).toLowerCase().includes(query))
  )
})

const tabItems = computed<UiTabItem[]>(() =>
  visibleSections.value.map((sec) => ({
    value: sec.id,
    label: sec.label,
    badge: dirtySections.value.includes(sec.id) ? 'Unsaved' : undefined
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
  if (isRefresh) {
    refreshing.value = true
  } else {
    loading.value = true
  }
  loadError.value = null
  mutationConflict.value = null
  mutationUnknown.value = null

  try {
    const data = await adminAPI.settings.getSettings()
    hydrateSettings(data)
    lastSyncedAt.value = new Date().toLocaleTimeString()
  } catch (error) {
    const err = classifyAsyncError(error, 'Failed to load system settings')
    loadError.value = {
      state: err.state === 'forbidden' ? 'forbidden' : err.state === 'unknown' ? 'unknown' : 'error',
      message: err.message,
      requestId: err.requestId
    }
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

function handleResetSection(): void {
  draft.value = resetSectionDraft(activeSection.value, baseline.value, draft.value)
  fieldErrors.value = {}
  mutationConflict.value = null
  mutationUnknown.value = null
}

async function handleSaveSection(): Promise<void> {
  fieldErrors.value = validateSettingsSection(activeSection.value, draft.value)
  if (Object.keys(fieldErrors.value).length > 0) {
    pushUiToast({ tone: 'warning', title: 'Validation failed', message: 'Fix highlighted fields before saving.' })
    return
  }

  const patch = buildSectionPatch(activeSection.value, baseline.value, draft.value)
  if (Object.keys(patch).length === 0) {
    confirmedMessage.value = 'No changes to save in this section.'
    return
  }

  saving.value = true
  mutationConflict.value = null
  mutationUnknown.value = null
  confirmedMessage.value = null

  try {
    const updated = await adminAPI.settings.updateSettings(patch)
    const nextBaseline: SettingsDraft = {
      ...baseline.value,
      ...updated,
      smtp_password: '',
      turnstile_secret_key: ''
    }
    baseline.value = structuredClone(nextBaseline)
    draft.value = resetSectionDraft(activeSection.value, nextBaseline, draft.value)
    lastSyncedAt.value = new Date().toLocaleTimeString()
    confirmedMessage.value = `${activeMeta.value.label} saved.`
    pushUiToast({ tone: 'success', title: 'Settings saved', message: activeMeta.value.label })
  } catch (error) {
    const err = classifyAsyncError(error, 'Failed to save settings')
    if (err.state === 'conflict') {
      mutationConflict.value = { message: err.message, requestId: err.requestId }
    } else if (err.state === 'unknown') {
      mutationUnknown.value = { message: err.message, requestId: err.requestId }
    } else {
      pushUiToast({ tone: 'danger', title: 'Save failed', message: err.message })
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
    pushUiToast({ tone: 'success', title: 'SMTP connection OK', message: res.message })
  } catch (error) {
    const err = classifyAsyncError(error, 'SMTP test failed')
    pushUiToast({ tone: 'danger', title: 'SMTP test failed', message: err.message })
  } finally {
    testingSmtp.value = false
  }
}

onMounted(() => {
  loadSettings(false)
})
</script>

<template>
  <div class="space-y-5">
    <PageHeader
      eyebrow="SYSTEM CONFIGURATION"
      title="System Settings"
      description="Sectioned platform configuration with per-section dirty tracking, validation, and scoped saves."
    >
      <template #badges>
        <Badge v-if="dirtySections.length > 0" tone="warning" variant="subtle" mono>
          {{ dirtySections.length }} dirty
        </Badge>
        <Badge v-else tone="success" variant="subtle">Synced</Badge>
      </template>

      <template #actions>
        <div class="w-56">
          <Input v-model="sectionFilter" type="search" placeholder="Filter sections or keys…" size="sm" clearable />
        </div>
        <Button size="sm" variant="secondary" :loading="refreshing" @click="loadSettings(true)">
          Refresh
        </Button>
      </template>

      <template #tabs>
        <Tabs v-model="activeSection" :items="tabItems" />
      </template>
    </PageHeader>

    <div v-if="loading" class="space-y-3">
      <Skeleton height="3rem" />
      <Skeleton height="16rem" />
    </div>

    <ErrorState
      v-else-if="loadError"
      :variant="loadError.state === 'forbidden' ? 'permission' : 'error'"
      :title="loadError.state === 'forbidden' ? 'Admin Permission Required (403)' : 'Failed to load settings'"
      :message="loadError.message"
      :request-id="loadError.requestId"
      @retry="loadSettings(false)"
    />

    <template v-else>
      <ErrorState
        v-if="mutationConflict"
        variant="stale"
        title="409 Settings Conflict"
        :message="mutationConflict.message"
        :request-id="mutationConflict.requestId"
        retry-label="Reload Latest Settings"
        @retry="loadSettings(true)"
      />

      <ErrorState
        v-if="mutationUnknown"
        variant="stale"
        title="Unknown Save Outcome"
        :message="mutationUnknown.message"
        :request-id="mutationUnknown.requestId"
        retry-label="Verify Latest Settings"
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
            <h2 class="text-base font-semibold text-oh-primary">{{ activeMeta.label }}</h2>
            <p class="mt-0.5 text-xs text-oh-secondary">{{ activeMeta.description }}</p>
          </div>
          <div class="flex items-center gap-2">
            <Button size="sm" variant="ghost" :disabled="!activeSectionDirty || saving" @click="handleResetSection">
              Reset Section
            </Button>
            <Button size="sm" variant="primary" :loading="saving" :disabled="!activeSectionDirty" @click="handleSaveSection">
              Save Section
            </Button>
          </div>
        </div>

        <div v-if="activeSection === 'general'" class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Input v-model="(draft.site_name as string)" label="Site Name" />
          <Input v-model="(draft.site_subtitle as string)" label="Site Subtitle" />
          <Input
            v-model="(draft.frontend_url as string)"
            label="Frontend URL"
            :error="fieldErrors.frontend_url"
            mono
          />
          <div class="space-y-3 pt-1">
            <Switch
              :model-value="Boolean(draft.registration_enabled)"
              label="Public Registration Enabled"
              @update:model-value="(v) => (draft.registration_enabled = v)"
            />
            <Switch
              :model-value="Boolean(draft.invitation_code_enabled)"
              label="Require Invitation Code"
              @update:model-value="(v) => (draft.invitation_code_enabled = v)"
            />
          </div>
        </div>

        <div v-else-if="activeSection === 'security'" class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Switch
            :model-value="Boolean(draft.totp_enabled)"
            label="TOTP 2FA Enabled"
            @update:model-value="(v) => (draft.totp_enabled = v)"
          />
          <Switch
            :model-value="Boolean(draft.passkey_enabled)"
            label="Passkey Authentication Enabled"
            @update:model-value="(v) => (draft.passkey_enabled = v)"
          />
          <Switch
            :model-value="Boolean(draft.session_binding_enabled)"
            label="Session IP/UA Binding"
            @update:model-value="(v) => (draft.session_binding_enabled = v)"
          />
          <Switch
            :model-value="Boolean(draft.step_up_enabled)"
            label="Step-Up 2FA for Sensitive Actions"
            @update:model-value="(v) => (draft.step_up_enabled = v)"
          />
          <Input v-model="(draft.passkey_rp_id as string)" label="Passkey RP ID" mono />
          <Input
            v-model="(draft.audit_log_retention_days as number)"
            type="number"
            label="Audit Log Retention (days)"
            :error="fieldErrors.audit_log_retention_days"
            mono
          />
          <Switch
            :model-value="Boolean(draft.turnstile_enabled)"
            label="Cloudflare Turnstile Enabled"
            @update:model-value="(v) => (draft.turnstile_enabled = v)"
          />
          <Input v-model="(draft.turnstile_site_key as string)" label="Turnstile Site Key" mono />
          <Input
            v-model="(draft.turnstile_secret_key as string)"
            type="password"
            label="Turnstile Secret Key"
            placeholder="Configured (leave blank to keep existing)"
            mono
          />
        </div>

        <div v-else-if="activeSection === 'users'" class="grid grid-cols-1 gap-4 md:grid-cols-3">
          <Input
            v-model="(draft.default_balance as number)"
            type="number"
            label="Default Balance (USD)"
            :error="fieldErrors.default_balance"
            mono
          />
          <Input
            v-model="(draft.default_concurrency as number)"
            type="number"
            label="Default Concurrency"
            :error="fieldErrors.default_concurrency"
            mono
          />
          <Input
            v-model="(draft.default_user_rpm_limit as number)"
            type="number"
            label="Default User RPM Limit"
            mono
          />
          <Input
            v-model="(draft.affiliate_rebate_rate as number)"
            type="number"
            label="Affiliate Rebate Rate (0–1)"
            :error="fieldErrors.affiliate_rebate_rate"
            mono
          />
          <Input
            v-model="(draft.affiliate_rebate_freeze_hours as number)"
            type="number"
            label="Rebate Freeze Hours"
            mono
          />
          <Input
            v-model="(draft.affiliate_rebate_duration_days as number)"
            type="number"
            label="Rebate Duration (days)"
            mono
          />
        </div>

        <div v-else-if="activeSection === 'gateway'" class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Input
            v-model="(draft.api_base_url as string)"
            label="Gateway API Base URL"
            :error="fieldErrors.api_base_url"
            mono
          />
          <Input v-model="(draft.doc_url as string)" label="Documentation URL" mono />
          <Input v-model="(draft.contact_info as string)" label="Operator Contact Info" />
          <div class="pt-5">
            <Switch
              :model-value="Boolean(draft.hide_ccs_import_button)"
              label="Hide CCS Import Button"
              @update:model-value="(v) => (draft.hide_ccs_import_button = v)"
            />
          </div>
        </div>

        <div v-else-if="activeSection === 'payment'" class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Switch
            :model-value="Boolean(draft.payment_enabled)"
            label="Enable Online Payments"
            @update:model-value="(v) => (draft.payment_enabled = v)"
          />
          <Switch
            :model-value="Boolean(draft.promo_code_enabled)"
            label="Enable Promo Codes"
            @update:model-value="(v) => (draft.promo_code_enabled = v)"
          />
        </div>

        <div v-else-if="activeSection === 'email'" class="space-y-4">
          <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
            <Switch
              :model-value="Boolean(draft.email_verify_enabled)"
              label="Require Email Verification"
              @update:model-value="(v) => (draft.email_verify_enabled = v)"
            />
            <Switch
              :model-value="Boolean(draft.password_reset_enabled)"
              label="Allow Password Reset"
              @update:model-value="(v) => (draft.password_reset_enabled = v)"
            />
            <Input v-model="(draft.smtp_host as string)" label="SMTP Host" mono />
            <Input
              v-model="(draft.smtp_port as number)"
              type="number"
              label="SMTP Port"
              :error="fieldErrors.smtp_port"
              mono
            />
            <Input v-model="(draft.smtp_username as string)" label="SMTP Username" mono />
            <Input
              v-model="(draft.smtp_password as string)"
              type="password"
              label="SMTP Password"
              placeholder="Configured (leave blank to keep existing)"
              mono
            />
            <Input v-model="(draft.smtp_from_email as string)" label="From Email" mono />
            <Input v-model="(draft.smtp_from_name as string)" label="From Name" />
            <Switch
              :model-value="Boolean(draft.smtp_use_tls)"
              label="Use TLS"
              @update:model-value="(v) => (draft.smtp_use_tls = v)"
            />
          </div>
          <div>
            <Button size="sm" variant="secondary" :loading="testingSmtp" @click="handleTestSmtp">
              Test SMTP Connection
            </Button>
          </div>
        </div>

        <div v-else-if="activeSection === 'features'" class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Switch
            :model-value="Boolean(draft.registration_email_domain_quota_enabled)"
            label="Email Domain Quota Enforcement"
            @update:model-value="(v) => (draft.registration_email_domain_quota_enabled = v)"
          />
          <Switch
            :model-value="Boolean(draft.user_custom_key_enabled)"
            label="Allow User Custom API Keys"
            @update:model-value="(v) => (draft.user_custom_key_enabled = v)"
          />
        </div>

        <div v-else-if="activeSection === 'agreement'" class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Switch
            :model-value="Boolean(draft.login_agreement_enabled)"
            label="Require Login Agreement"
            @update:model-value="(v) => (draft.login_agreement_enabled = v)"
          />
          <Select
            v-model="(draft.login_agreement_mode as string)"
            label="Agreement Presentation Mode"
            :options="[
              { value: 'checkbox', label: 'Checkbox' },
              { value: 'modal', label: 'Modal' }
            ]"
          />
        </div>
      </Card>
    </template>
  </div>
</template>
