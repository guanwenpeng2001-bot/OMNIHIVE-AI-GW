<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
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

const { t, locale } = useI18n()
const isZh = computed(() => String(locale?.value || 'zh').toLowerCase().startsWith('zh'))

const activeSection = ref<SettingsSectionId>(props.initialSection)
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
      sec.label.toLowerCase().includes(query) ||
      sec.labelZh.toLowerCase().includes(query) ||
      sec.description.toLowerCase().includes(query) ||
      sec.descriptionZh.toLowerCase().includes(query) ||
      sec.fields.some((f) => String(f).toLowerCase().includes(query))
  )
})

const tabItems = computed<UiTabItem[]>(() =>
  visibleSections.value.map((sec) => ({
    value: sec.id,
    label: isZh.value ? sec.labelZh : sec.label,
    badge: dirtySections.value.includes(sec.id) ? (isZh.value ? '未保存' : 'Unsaved') : undefined
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
    const err = classifyAsyncError(error, isZh.value ? '加载系统设置失败' : 'Failed to load system settings')
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
  fieldErrors.value = validateSettingsSection(activeSection.value, draft.value)
  if (Object.keys(fieldErrors.value).length > 0) {
    pushUiToast({
      tone: 'warning',
      title: isZh.value ? '校验未通过' : 'Validation failed',
      description: isZh.value ? '请先修正标红字段后再保存。' : 'Fix highlighted fields before saving.'
    })
    return
  }

  const patch = buildSectionPatch(activeSection.value, baseline.value, draft.value)
  if (Object.keys(patch).length === 0) {
    confirmedMessage.value = isZh.value ? '当前分组没有待保存的更改。' : 'No changes to save in this section.'
    return
  }

  saving.value = true
  saveError.value = null
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
    const activeLabel = isZh.value ? activeMeta.value.labelZh : activeMeta.value.label
    confirmedMessage.value = isZh.value ? `已保存「${activeLabel}」配置。` : `${activeLabel} saved.`
    pushUiToast({
      tone: 'success',
      title: isZh.value ? '设置已保存' : 'Settings saved',
      description: activeLabel
    })
  } catch (error) {
    const err = classifyAsyncError(error, isZh.value ? '保存设置失败' : 'Failed to save settings')
    if (err.state === 'conflict') {
      mutationConflict.value = { message: err.message, requestId: err.requestId }
    } else if (err.state === 'unknown') {
      mutationUnknown.value = { message: err.message, requestId: err.requestId }
    } else {
      saveError.value = { message: err.message, requestId: err.requestId }
      pushUiToast({ tone: 'danger', title: isZh.value ? '保存失败' : 'Save failed', description: err.message })
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
    pushUiToast({ tone: 'success', title: isZh.value ? 'SMTP 连接测试成功' : 'SMTP connection OK', description: res.message })
  } catch (error) {
    const err = classifyAsyncError(error, isZh.value ? 'SMTP 测试失败' : 'SMTP test failed')
    pushUiToast({ tone: 'danger', title: isZh.value ? 'SMTP 测试失败' : 'SMTP test failed', description: err.message })
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
    ...baseline.value,
    ...updated,
    smtp_password: '',
    turnstile_secret_key: ''
  }
  baseline.value = structuredClone(nextBaseline)
  let nextDraft = structuredClone(draft.value)
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
      :eyebrow="isZh ? '系统配置' : 'SYSTEM CONFIGURATION'"
      :title="isZh ? '系统设置' : 'System Settings'"
      :description="isZh ? '按功能分区管理平台配置，支持分区域未保存提醒、字段校验与独立保存。' : 'Sectioned platform configuration with per-section dirty tracking, validation, and scoped saves.'"
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
            {{ isZh ? `${dirtySections.length} 项未保存` : `${dirtySections.length} dirty` }}
          </Badge>
          <Badge
            v-else-if="hasLoadedSettings"
            tone="success"
            variant="subtle"
            data-testid="workbench-sync-badge"
          >
            {{ isZh ? '已同步' : 'Synced' }}
          </Badge>
        </template>
      </template>

      <template #actions>
        <div v-if="activeSection !== 'diagnostics'" class="w-full sm:w-56">
          <Input v-model="sectionFilter" type="search" :placeholder="isZh ? '搜索配置分组或字段…' : 'Filter sections or keys…'" size="sm" clearable />
        </div>
        <Button
          size="sm"
          variant="secondary"
          :loading="activeSection === 'diagnostics' ? diagnosticsSync.refreshing : refreshing"
          @click="handleHeaderRefresh"
        >
          {{ isZh ? '刷新' : 'Refresh' }}
        </Button>
      </template>

      <template #tabs>
        <Tabs v-model="activeSection" :items="tabItems" />
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
      :title="loadError.state === 'forbidden' ? (isZh ? '需要管理员权限 (403)' : 'Admin Permission Required (403)') : (isZh ? '加载系统设置失败' : 'Failed to load settings')"
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
        :title="isZh ? '配置版本冲突 (409)' : '409 Settings Conflict'"
        :message="mutationConflict.message"
        :request-id="mutationConflict.requestId"
        :retry-label="isZh ? '重新加载最新配置' : 'Reload Latest Settings'"
        @retry="loadSettings(true)"
      />

      <ErrorState
        v-if="mutationUnknown"
        variant="stale"
        :title="isZh ? '保存状态待确认' : 'Unknown Save Outcome'"
        :message="mutationUnknown.message"
        :request-id="mutationUnknown.requestId"
        :retry-label="isZh ? '刷新确认最新配置' : 'Verify Latest Settings'"
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
            <h2 class="text-base font-semibold text-oh-primary">{{ isZh ? activeMeta.labelZh : activeMeta.label }}</h2>
            <p class="mt-0.5 text-xs text-oh-secondary">{{ isZh ? activeMeta.descriptionZh : activeMeta.description }}</p>
          </div>
          <div class="flex items-center gap-2">
            <Button size="sm" variant="ghost" :disabled="!activeSectionDirty || saving" @click="handleResetSection">
              {{ isZh ? '重置本组' : 'Reset Section' }}
            </Button>
            <Button size="sm" variant="primary" :loading="saving" :disabled="!activeSectionDirty" @click="handleSaveSection">
              {{ isZh ? '保存本组' : 'Save Section' }}
            </Button>
          </div>
        </div>

        <div v-if="activeSection === 'general'" class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Input v-model="(draft.site_name as string)" :label="isZh ? '站点名称' : 'Site Name'" />
          <Input v-model="(draft.site_subtitle as string)" :label="isZh ? '站点副标题' : 'Site Subtitle'" />
          <Input
            v-model="(draft.frontend_url as string)"
            :label="isZh ? '前端访问地址' : 'Frontend URL'"
            :error="fieldErrors.frontend_url"
            mono
          />
          <div class="space-y-3 pt-1">
            <Switch
              :model-value="Boolean(draft.registration_enabled)"
              :label="isZh ? '开放公开注册' : 'Public Registration Enabled'"
              @update:model-value="(v) => (draft.registration_enabled = v)"
            />
            <Switch
              :model-value="Boolean(draft.invitation_code_enabled)"
              :label="isZh ? '注册需填写邀请码' : 'Require Invitation Code'"
              @update:model-value="(v) => (draft.invitation_code_enabled = v)"
            />
          </div>
        </div>

        <div v-else-if="activeSection === 'security'" class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Switch
            :model-value="Boolean(draft.totp_enabled)"
            :label="isZh ? '启用 TOTP 两步验证' : 'TOTP 2FA Enabled'"
            @update:model-value="(v) => (draft.totp_enabled = v)"
          />
          <Switch
            :model-value="Boolean(draft.passkey_enabled)"
            :label="isZh ? '启用 Passkey 通行密钥' : 'Passkey Authentication Enabled'"
            @update:model-value="(v) => (draft.passkey_enabled = v)"
          />
          <Switch
            :model-value="Boolean(draft.session_binding_enabled)"
            :label="isZh ? '会话 IP/UA 绑定' : 'Session IP/UA Binding'"
            @update:model-value="(v) => (draft.session_binding_enabled = v)"
          />
          <Switch
            :model-value="Boolean(draft.step_up_enabled)"
            :label="isZh ? '敏感操作二次验证' : 'Step-Up 2FA for Sensitive Actions'"
            @update:model-value="(v) => (draft.step_up_enabled = v)"
          />
          <Input v-model="(draft.passkey_rp_id as string)" :label="isZh ? 'Passkey RP ID' : 'Passkey RP ID'" mono />
          <Input
            v-model="(draft.audit_log_retention_days as number)"
            type="number"
            :label="isZh ? '审计日志保留天数' : 'Audit Log Retention (days)'"
            :error="fieldErrors.audit_log_retention_days"
            mono
          />
          <Switch
            :model-value="Boolean(draft.turnstile_enabled)"
            :label="isZh ? '启用 Cloudflare Turnstile' : 'Cloudflare Turnstile Enabled'"
            @update:model-value="(v) => (draft.turnstile_enabled = v)"
          />
          <Input v-model="(draft.turnstile_site_key as string)" :label="isZh ? 'Turnstile Site Key' : 'Turnstile Site Key'" mono />
          <Input
            v-model="(draft.turnstile_secret_key as string)"
            type="password"
            :label="isZh ? 'Turnstile Secret Key' : 'Turnstile Secret Key'"
            :placeholder="isZh ? '已配置（留空则保持不变）' : 'Configured (leave blank to keep existing)'"
            mono
          />
        </div>

        <div v-else-if="activeSection === 'users'" class="grid grid-cols-1 gap-4 md:grid-cols-3">
          <Input
            v-model="(draft.default_balance as number)"
            type="number"
            :label="isZh ? '默认初始余额 (USD)' : 'Default Balance (USD)'"
            :error="fieldErrors.default_balance"
            mono
          />
          <Input
            v-model="(draft.default_concurrency as number)"
            type="number"
            :label="isZh ? '默认并发上限' : 'Default Concurrency'"
            :error="fieldErrors.default_concurrency"
            mono
          />
          <Input
            v-model="(draft.default_user_rpm_limit as number)"
            type="number"
            :label="isZh ? '默认用户 RPM 上限' : 'Default User RPM Limit'"
            mono
          />
          <Input
            v-model="(draft.affiliate_rebate_rate as number)"
            type="number"
            :label="isZh ? '联盟返利比例 (0–1)' : 'Affiliate Rebate Rate (0–1)'"
            :error="fieldErrors.affiliate_rebate_rate"
            mono
          />
          <Input
            v-model="(draft.affiliate_rebate_freeze_hours as number)"
            type="number"
            :label="isZh ? '返利冻结小时数' : 'Rebate Freeze Hours'"
            mono
          />
          <Input
            v-model="(draft.affiliate_rebate_duration_days as number)"
            type="number"
            :label="isZh ? '返利有效天数' : 'Rebate Duration (days)'"
            mono
          />
        </div>

        <div v-else-if="activeSection === 'gateway'" class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Input
            v-model="(draft.api_base_url as string)"
            :label="isZh ? '网关 API 基础地址' : 'Gateway API Base URL'"
            :error="fieldErrors.api_base_url"
            mono
          />
          <Input v-model="(draft.doc_url as string)" :label="isZh ? '文档链接' : 'Documentation URL'" mono />
          <Input v-model="(draft.contact_info as string)" :label="isZh ? '运营联系方式' : 'Operator Contact Info'" />
          <div class="pt-5">
            <Switch
              :model-value="Boolean(draft.hide_ccs_import_button)"
              :label="isZh ? '隐藏 CCS 导入按钮' : 'Hide CCS Import Button'"
              @update:model-value="(v) => (draft.hide_ccs_import_button = v)"
            />
          </div>
        </div>

        <div v-else-if="activeSection === 'payment'" class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Switch
            :model-value="Boolean(draft.payment_enabled)"
            :label="isZh ? '启用在线支付' : 'Enable Online Payments'"
            @update:model-value="(v) => (draft.payment_enabled = v)"
          />
          <Switch
            :model-value="Boolean(draft.promo_code_enabled)"
            :label="isZh ? '启用优惠码' : 'Enable Promo Codes'"
            @update:model-value="(v) => (draft.promo_code_enabled = v)"
          />
        </div>

        <div v-else-if="activeSection === 'email'" class="space-y-4">
          <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
            <Switch
              :model-value="Boolean(draft.email_verify_enabled)"
              :label="isZh ? '要求邮箱验证' : 'Require Email Verification'"
              @update:model-value="(v) => (draft.email_verify_enabled = v)"
            />
            <Switch
              :model-value="Boolean(draft.password_reset_enabled)"
              :label="isZh ? '允许邮箱找回密码' : 'Allow Password Reset'"
              @update:model-value="(v) => (draft.password_reset_enabled = v)"
            />
            <Input v-model="(draft.smtp_host as string)" :label="isZh ? 'SMTP 主机' : 'SMTP Host'" mono />
            <Input
              v-model="(draft.smtp_port as number)"
              type="number"
              :label="isZh ? 'SMTP 端口' : 'SMTP Port'"
              :error="fieldErrors.smtp_port"
              mono
            />
            <Input v-model="(draft.smtp_username as string)" :label="isZh ? 'SMTP 用户名' : 'SMTP Username'" mono />
            <Input
              v-model="(draft.smtp_password as string)"
              type="password"
              :label="isZh ? 'SMTP 密码' : 'SMTP Password'"
              :placeholder="isZh ? '已配置（留空则保持不变）' : 'Configured (leave blank to keep existing)'"
              mono
            />
            <Input v-model="(draft.smtp_from_email as string)" :label="isZh ? '发件人邮箱' : 'From Email'" mono />
            <Input v-model="(draft.smtp_from_name as string)" :label="isZh ? '发件人名称' : 'From Name'" />
            <Switch
              :model-value="Boolean(draft.smtp_use_tls)"
              :label="isZh ? '启用 TLS 加密' : 'Use TLS'"
              @update:model-value="(v) => (draft.smtp_use_tls = v)"
            />
          </div>
          <div>
            <Button size="sm" variant="secondary" :loading="testingSmtp" @click="handleTestSmtp">
              {{ isZh ? '测试 SMTP 连接' : 'Test SMTP Connection' }}
            </Button>
          </div>
        </div>

        <div v-else-if="activeSection === 'features'" class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Switch
            :model-value="Boolean(draft.registration_email_domain_quota_enabled)"
            :label="isZh ? '启用注册邮箱域名配额限制' : 'Email Domain Quota Enforcement'"
            @update:model-value="(v) => (draft.registration_email_domain_quota_enabled = v)"
          />
          <Switch
            :model-value="Boolean(draft.user_custom_key_enabled)"
            :label="isZh ? '允许用户自定义 API 密钥' : 'Allow User Custom API Keys'"
            @update:model-value="(v) => (draft.user_custom_key_enabled = v)"
          />
        </div>

        <div v-else-if="activeSection === 'agreement'" class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Switch
            :model-value="Boolean(draft.login_agreement_enabled)"
            :label="isZh ? '登录需同意服务协议' : 'Require Login Agreement'"
            @update:model-value="(v) => (draft.login_agreement_enabled = v)"
          />
          <Select
            v-model="(draft.login_agreement_mode as string)"
            :label="isZh ? '协议展示方式' : 'Agreement Presentation Mode'"
            :options="[
              { value: 'checkbox', label: isZh ? '勾选框确认' : 'Checkbox' },
              { value: 'modal', label: isZh ? '弹窗确认' : 'Modal' }
            ]"
          />
        </div>
      </Card>
    </template>
  </div>
</template>
