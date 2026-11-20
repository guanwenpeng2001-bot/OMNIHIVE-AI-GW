<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { Badge, Button, Card, CopyButton, Input, Select, Switch, pushUiToast } from '@/components/ui'
import { useConsoleScope } from '@/features/console/scope'
import { classifyAsyncError, redactSensitiveText } from '@/features/shared/asyncState'
import { normalizeApiError } from '@/utils/apiError'
import {
  generateCommandKey,
  requestDelete,
  requestGet,
  requestPatch,
  requestPost,
  type TelemetryDestination,
  type TelemetryExportConfig,
  type TelemetryExportDetail
} from './operationsApi'
import {
  formatDateTimeDisplay,
  formatIfMatch,
  isValidPositiveIntString,
  isValidSampleRate,
  isValidUuid,
  validateTelemetryEndpoint
} from './operationsHelpers'

const props = defineProps<{
  isZh: boolean
}>()

const { scopes, activeWorkspaceId, refreshScopes, selectWorkspaceProject } = useConsoleScope()

const workspaceIdInput = ref('')

onMounted(() => {
  if (activeWorkspaceId.value && !workspaceIdInput.value) {
    workspaceIdInput.value = activeWorkspaceId.value
  }
  void refreshScopes()
})

watch(
  activeWorkspaceId,
  (nextId) => {
    if (nextId && nextId !== workspaceIdInput.value) {
      workspaceIdInput.value = nextId
    }
  },
  { immediate: true }
)

const workspaceSelectOptions = computed(() =>
  scopes.value.map((ws) => ({
    value: ws.workspace_id,
    label: `${ws.name} (#${ws.workspace_id})`
  }))
)

const effectiveWorkspaceId = computed(() => workspaceIdInput.value.trim())

function handleWorkspaceSelect(value: string | number) {
  const wsId = String(value).trim()
  workspaceIdInput.value = wsId
  const matched = scopes.value.find((item) => item.workspace_id === wsId)
  if (matched) {
    selectWorkspaceProject(wsId, matched.projects?.[0]?.id ?? null)
  }
}

// Session-scoped destinations & exports for active workspace
const sessionDestinations = ref<Array<TelemetryDestination & { hostHint?: string }>>([])
const sessionExports = ref<TelemetryExportConfig[]>([])
const activeExportDetail = ref<TelemetryExportDetail | null>(null)

const permissionDenied = ref(false)
const bannerError = ref('')
const bannerSuccess = ref('')

watch(effectiveWorkspaceId, () => {
  sessionDestinations.value = []
  sessionExports.value = []
  activeExportDetail.value = null
  permissionDenied.value = false
  bannerError.value = ''
  bannerSuccess.value = ''
})

function requireValidWorkspace(): string | null {
  const wsId = effectiveWorkspaceId.value
  if (!isValidPositiveIntString(wsId)) {
    bannerError.value = props.isZh
      ? '请先选择或输入有效的工作空间数字 ID。'
      : 'Select or enter a valid positive numeric workspace ID first.'
    return null
  }
  return wsId
}

function handleOperationFailure(cause: unknown, fallbackZh: string, fallbackEn: string, onCurrentVersion?: (ver: string) => void) {
  const normalized = normalizeApiError(cause)
  const currentVer =
    typeof normalized.metadata?.current_version === 'string'
      ? normalized.metadata.current_version
      : undefined
  if (currentVer && onCurrentVersion) {
    onCurrentVersion(currentVer)
  }
  const issue = classifyAsyncError(cause, props.isZh ? fallbackZh : fallbackEn)
  if (issue.status === 403 || issue.state === 'forbidden') {
    permissionDenied.value = true
    bannerError.value = ''
    return
  }
  permissionDenied.value = false
  if (issue.status === 412 && currentVer) {
    bannerError.value = props.isZh
      ? `配置版本发生变更，已自动同步最新版本号 ${currentVer}，请核对后重新提交。`
      : `Configuration version changed. Synced latest version ${currentVer}; review and retry.`
    return
  }
  bannerError.value = issue.message
}

// 1. Register HTTPS Collector
const collectorEndpoint = ref('')
const collectorValidationError = ref('')
const registeringDestination = ref(false)

function extractSafeHostHint(endpoint: string): string {
  try {
    const url = new URL(endpoint.trim())
    return `${url.protocol}//${url.host}${url.pathname}`
  } catch {
    return ''
  }
}

async function registerCollector() {
  const wsId = requireValidWorkspace()
  if (!wsId || registeringDestination.value) return
  const validationError = validateTelemetryEndpoint(collectorEndpoint.value, props.isZh)
  if (validationError) {
    collectorValidationError.value = validationError
    return
  }
  collectorValidationError.value = ''
  bannerError.value = ''
  bannerSuccess.value = ''
  permissionDenied.value = false
  registeringDestination.value = true

  const endpointValue = collectorEndpoint.value.trim()
  const hostHint = extractSafeHostHint(endpointValue)
  try {
    const created = await requestPost<TelemetryDestination>(
      `/workspaces/${encodeURIComponent(wsId)}/observability/destinations`,
      { endpoint: endpointValue },
      { 'Idempotency-Key': generateCommandKey() }
    )
    const existingIndex = sessionDestinations.value.findIndex((item) => item.id === created.id)
    const nextEntry = { ...created, hostHint }
    if (existingIndex >= 0) {
      sessionDestinations.value.splice(existingIndex, 1, nextEntry)
    } else {
      sessionDestinations.value = [nextEntry, ...sessionDestinations.value]
    }
    if (!exportDestinationId.value && created.state === 'registered') {
      exportDestinationId.value = created.id
    }
    collectorEndpoint.value = ''
    bannerSuccess.value = props.isZh
      ? `HTTPS 采集端已注册（ID：${created.id}，版本 ${created.version}）。`
      : `HTTPS collector registered (ID: ${created.id}, version ${created.version}).`
    pushUiToast({
      tone: 'success',
      title: props.isZh ? '采集端注册成功' : 'Collector registered',
      description: created.id
    })
  } catch (cause) {
    handleOperationFailure(cause, '注册 HTTPS 采集端失败。', 'Failed to register HTTPS collector.')
  } finally {
    registeringDestination.value = false
  }
}

// 2. Revoke Collector
const manualRevokeId = ref('')
const manualRevokeVersion = ref('1')
const revokingDestinationId = ref('')

async function revokeCollector(id: string, version: string) {
  const wsId = requireValidWorkspace()
  if (!wsId || revokingDestinationId.value) return
  const cleanId = id.trim()
  if (!isValidUuid(cleanId)) {
    bannerError.value = props.isZh ? '请输入合法的采集端 UUID。' : 'Enter a valid collector UUID.'
    return
  }
  if (!isValidPositiveIntString(version)) {
    bannerError.value = props.isZh ? '采集端版本号必须为正整数。' : 'Collector version must be a positive integer.'
    return
  }
  bannerError.value = ''
  bannerSuccess.value = ''
  permissionDenied.value = false
  revokingDestinationId.value = cleanId
  try {
    const revoked = await requestDelete<TelemetryDestination>(
      `/workspaces/${encodeURIComponent(wsId)}/observability/destinations/${encodeURIComponent(cleanId)}`,
      {
        'Idempotency-Key': generateCommandKey(),
        'If-Match': formatIfMatch(version)
      }
    )
    const existingIndex = sessionDestinations.value.findIndex((item) => item.id === revoked.id)
    if (existingIndex >= 0) {
      const prevHint = sessionDestinations.value[existingIndex].hostHint
      sessionDestinations.value.splice(existingIndex, 1, { ...revoked, hostHint: prevHint })
    } else {
      sessionDestinations.value = [revoked, ...sessionDestinations.value]
    }
    if (exportDestinationId.value === revoked.id) {
      exportDestinationId.value = ''
    }
    bannerSuccess.value = props.isZh
      ? `采集端 ${revoked.id} 已撤销并清理待发送样本队列。`
      : `Collector ${revoked.id} revoked and queued samples purged.`
  } catch (cause) {
    handleOperationFailure(
      cause,
      '撤销采集端失败。',
      'Failed to revoke collector.',
      (ver) => {
        manualRevokeVersion.value = ver
        sessionDestinations.value = sessionDestinations.value.map((item) =>
          item.id === cleanId ? { ...item, version: ver } : item
        )
      }
    )
  } finally {
    revokingDestinationId.value = ''
  }
}

// 3. Create Export Pipeline
const exportDestinationId = ref('')
const exportSampleRate = ref('0.1')
const exportRetentionDays = ref('7')
const createExportValidationError = ref('')
const creatingExport = ref(false)

const registeredDestinationOptions = computed(() =>
  sessionDestinations.value
    .filter((item) => item.state === 'registered')
    .map((item) => ({
      value: item.id,
      label: item.hostHint ? `${item.id.slice(0, 8)}… · ${item.hostHint}` : item.id
    }))
)

async function createExportPipeline() {
  const wsId = requireValidWorkspace()
  if (!wsId || creatingExport.value) return
  const destId = exportDestinationId.value.trim()
  const sampleRate = exportSampleRate.value.trim()
  const retentionRaw = exportRetentionDays.value.trim()

  if (!isValidUuid(destId)) {
    createExportValidationError.value = props.isZh
      ? '请选择或输入合法的采集端 UUID。'
      : 'Select or enter a valid collector destination UUID.'
    return
  }
  if (!isValidSampleRate(sampleRate)) {
    createExportValidationError.value = props.isZh
      ? '采样率需为 0 到 1 之间的小数字符串（最多 6 位小数，如 0.05 或 1）。'
      : 'Sample rate must be a decimal between 0 and 1 (up to 6 decimal places, e.g. 0.05 or 1).'
    return
  }
  if (!isValidPositiveIntString(retentionRaw) || Number(retentionRaw) < 1 || Number(retentionRaw) > 30) {
    createExportValidationError.value = props.isZh
      ? '导出队列保留天数需为 1 至 30 之间的整数。'
      : 'Export retention days must be an integer between 1 and 30.'
    return
  }
  createExportValidationError.value = ''
  bannerError.value = ''
  bannerSuccess.value = ''
  permissionDenied.value = false
  creatingExport.value = true

  try {
    const created = await requestPost<TelemetryExportConfig>(
      `/workspaces/${encodeURIComponent(wsId)}/observability/exports`,
      {
        destination_id: destId,
        data_classes: ['timings'],
        sample_rate: sampleRate,
        retention_days: Number(retentionRaw)
      },
      { 'Idempotency-Key': generateCommandKey() }
    )
    upsertSessionExport(created)
    exportLookupId.value = created.id
    patchVersion.value = created.version
    patchSampleRate.value = sampleRate
    patchRetentionDays.value = retentionRaw
    patchEnabled.value = created.state !== 'disabled'
    bannerSuccess.value = props.isZh
      ? `导出流水线已创建（ID：${created.id}，状态：${formatExportState(created.state)}）。`
      : `Telemetry export pipeline created (ID: ${created.id}, state: ${created.state}).`
  } catch (cause) {
    handleOperationFailure(cause, '创建遥测导出流水线失败。', 'Failed to create telemetry export pipeline.')
  } finally {
    creatingExport.value = false
  }
}

function upsertSessionExport(config: TelemetryExportConfig) {
  const idx = sessionExports.value.findIndex((item) => item.id === config.id)
  if (idx >= 0) {
    sessionExports.value.splice(idx, 1, config)
  } else {
    sessionExports.value = [config, ...sessionExports.value]
  }
}

// 4. Query Export Detail & Queue Depth
const exportLookupId = ref('')
const loadingExportDetail = ref(false)

async function loadExportDetail(explicitId?: string) {
  const wsId = requireValidWorkspace()
  if (!wsId || loadingExportDetail.value) return
  const targetId = (explicitId ?? exportLookupId.value).trim()
  if (!isValidUuid(targetId)) {
    bannerError.value = props.isZh
      ? '请输入合法的导出流水线 UUID。'
      : 'Enter a valid telemetry export UUID.'
    return
  }
  exportLookupId.value = targetId
  bannerError.value = ''
  permissionDenied.value = false
  loadingExportDetail.value = true
  try {
    const detail = await requestGet<TelemetryExportDetail>(
      `/workspaces/${encodeURIComponent(wsId)}/observability/exports/${encodeURIComponent(targetId)}`
    )
    activeExportDetail.value = detail
    upsertSessionExport({
      id: detail.id,
      state: detail.state,
      version: detail.version,
      destination_id: detail.destination_id
    })
    patchVersion.value = detail.version
    patchSampleRate.value = detail.sample_rate
    patchRetentionDays.value = String(detail.retention_days)
    patchEnabled.value = detail.state !== 'disabled'
  } catch (cause) {
    handleOperationFailure(cause, '查询遥测导出详情失败。', 'Failed to load telemetry export detail.')
  } finally {
    loadingExportDetail.value = false
  }
}

// 5. Patch / Enable-Disable Export Pipeline
const patchVersion = ref('1')
const patchSampleRate = ref('0.1')
const patchRetentionDays = ref('7')
const patchEnabled = ref(true)
const patchValidationError = ref('')
const updatingExport = ref(false)

async function updateExportPipeline() {
  const wsId = requireValidWorkspace()
  if (!wsId || updatingExport.value) return
  const targetId = (activeExportDetail.value?.id || exportLookupId.value).trim()
  if (!isValidUuid(targetId)) {
    patchValidationError.value = props.isZh
      ? '请先查询或输入目标导出流水线 UUID。'
      : 'Load or enter a valid target export UUID first.'
    return
  }
  if (!isValidPositiveIntString(patchVersion.value)) {
    patchValidationError.value = props.isZh
      ? '流水线当前版本号必须为正整数。'
      : 'Current export version must be a positive integer.'
    return
  }
  const sampleRate = patchSampleRate.value.trim()
  if (!isValidSampleRate(sampleRate)) {
    patchValidationError.value = props.isZh
      ? '采样率格式无效（应为 0 至 1 之间的小数，最多 6 位）。'
      : 'Invalid sample rate (must be 0 to 1 with up to 6 decimals).'
    return
  }
  const retentionRaw = patchRetentionDays.value.trim()
  if (!isValidPositiveIntString(retentionRaw) || Number(retentionRaw) < 1 || Number(retentionRaw) > 30) {
    patchValidationError.value = props.isZh
      ? '保留天数需为 1 至 30 之间的整数。'
      : 'Retention days must be an integer between 1 and 30.'
    return
  }
  patchValidationError.value = ''
  bannerError.value = ''
  bannerSuccess.value = ''
  permissionDenied.value = false
  updatingExport.value = true

  try {
    const updated = await requestPatch<TelemetryExportConfig>(
      `/workspaces/${encodeURIComponent(wsId)}/observability/exports/${encodeURIComponent(targetId)}`,
      {
        sample_rate: sampleRate,
        retention_days: Number(retentionRaw),
        enabled: patchEnabled.value
      },
      {
        'Idempotency-Key': generateCommandKey(),
        'If-Match': formatIfMatch(patchVersion.value)
      }
    )
    upsertSessionExport(updated)
    patchVersion.value = updated.version
    if (activeExportDetail.value && activeExportDetail.value.id === updated.id) {
      activeExportDetail.value = {
        ...activeExportDetail.value,
        state: updated.state,
        version: updated.version,
        destination_id: updated.destination_id,
        sample_rate: sampleRate,
        retention_days: Number(retentionRaw)
      }
    }
    bannerSuccess.value = props.isZh
      ? `导出流水线已更新至版本 ${updated.version}（状态：${formatExportState(updated.state)}）。`
      : `Export pipeline updated to version ${updated.version} (state: ${updated.state}).`
  } catch (cause) {
    handleOperationFailure(
      cause,
      '更新遥测导出流水线失败。',
      'Failed to update telemetry export pipeline.',
      (ver) => {
        patchVersion.value = ver
      }
    )
  } finally {
    updatingExport.value = false
  }
}

function formatDestinationState(state: string): string {
  if (state === 'registered') return props.isZh ? '已注册' : 'Registered'
  if (state === 'revoked') return props.isZh ? '已撤销' : 'Revoked'
  return state
}

function formatExportState(state: string): string {
  switch (state) {
    case 'validating':
      return props.isZh ? '连通校验中' : 'Validating'
    case 'active':
      return props.isZh ? '运行中' : 'Active'
    case 'failed':
      return props.isZh ? '投递异常' : 'Failed'
    case 'disabled':
      return props.isZh ? '已停用' : 'Disabled'
    default:
      return state
  }
}

function exportStateTone(state: string): 'info' | 'success' | 'danger' | 'neutral' {
  switch (state) {
    case 'active':
      return 'success'
    case 'validating':
      return 'info'
    case 'failed':
      return 'danger'
    default:
      return 'neutral'
  }
}
</script>

<template>
  <Card
    data-testid="workspace-telemetry-panel"
    :title="isZh ? '工作空间遥测导出配置' : 'Workspace Telemetry Export'"
    :subtitle="isZh ? '注册公网 HTTPS 遥测采集端，配置时序指标采样率、保留窗口并监控投递队列深度。' : 'Register HTTPS collectors, configure timing export sampling and retention windows, and inspect delivery queue depth.'"
  >
    <div class="min-w-0 space-y-4">
      <div class="flex min-w-0 flex-wrap items-end gap-2 border-b border-oh-subtle pb-3">
        <div v-if="workspaceSelectOptions.length > 0" class="w-full min-w-0 sm:w-48">
          <Select
            :model-value="effectiveWorkspaceId"
            size="sm"
            :label="isZh ? '工作空间' : 'Workspace'"
            :options="workspaceSelectOptions"
            @update:model-value="handleWorkspaceSelect"
          />
        </div>
        <div class="w-full min-w-0 sm:w-36">
          <Input
            id="telemetry-workspace-id"
            v-model="workspaceIdInput"
            size="sm"
            mono
            :label="isZh ? '工作空间 ID' : 'Workspace ID'"
            :placeholder="isZh ? '例如：1' : 'e.g. 1'"
          />
        </div>
      </div>
      <div
        v-if="permissionDenied"
        role="alert"
        data-testid="telemetry-permission-denied"
        class="rounded-oh-md border border-oh-status-warn-border bg-oh-status-warn-bg px-3 py-2 text-xs text-oh-status-warn-fg"
      >
        {{
          isZh
            ? '当前账号无权配置工作空间遥测导出，请联系工作空间管理员授予 ops.export.configure 权限。'
            : 'Permission denied: configuring workspace telemetry exports requires the ops.export.configure grant.'
        }}
      </div>

      <p
        v-if="bannerError"
        role="alert"
        data-testid="telemetry-banner-error"
        class="rounded-oh-md border border-oh-status-danger-border bg-oh-status-danger-bg px-3 py-2 text-xs text-oh-status-danger-fg"
      >
        {{ bannerError }}
      </p>

      <p
        v-if="bannerSuccess"
        role="status"
        data-testid="telemetry-banner-success"
        class="rounded-oh-md border border-oh-status-ok-border bg-oh-status-ok-bg px-3 py-2 text-xs text-oh-status-ok-fg"
      >
        {{ bannerSuccess }}
      </p>

      <div class="grid gap-4 xl:grid-cols-2">
        <!-- Left Column: HTTPS Destinations -->
        <div class="min-w-0 space-y-3 rounded-oh-md border border-oh-subtle bg-oh-subtle/40 p-3.5">
          <div class="space-y-0.5">
            <h4 class="text-sm font-semibold text-oh-primary">
              {{ isZh ? '1. HTTPS 遥测采集端管理' : '1. HTTPS Collector Destinations' }}
            </h4>
            <p class="text-xs text-oh-muted">
              {{
                isZh
                  ? '仅允许无内嵌凭据、无查询参数（?）与无锚点（#）的公网 HTTPS 地址。'
                  : 'Public HTTPS URLs only, without embedded credentials, query parameters, or fragments.'
              }}
            </p>
          </div>

          <form class="flex flex-col gap-2 sm:flex-row sm:items-start" @submit.prevent="registerCollector">
            <div class="min-w-0 flex-1">
              <Input
                id="telemetry-collector-endpoint"
                v-model="collectorEndpoint"
                type="url"
                mono
                :label="isZh ? '采集端 HTTPS 地址' : 'Collector HTTPS Endpoint'"
                placeholder="https://telemetry.example.com/ingest"
                :error="collectorValidationError || undefined"
              />
            </div>
            <div class="sm:pt-6">
              <Button
                type="submit"
                size="sm"
                data-testid="telemetry-register-destination"
                :loading="registeringDestination"
              >
                {{ isZh ? '注册采集端' : 'Register Collector' }}
              </Button>
            </div>
          </form>

          <div v-if="sessionDestinations.length > 0" class="space-y-2">
            <p class="text-xs font-medium text-oh-secondary">
              {{ isZh ? '当前会话已登记采集端' : 'Session Collectors' }}
            </p>
            <ul class="space-y-1.5">
              <li
                v-for="dest in sessionDestinations"
                :key="dest.id"
                data-testid="telemetry-destination-item"
                class="flex flex-wrap items-center justify-between gap-2 rounded-oh-sm border border-oh-default bg-oh-surface px-3 py-2 text-xs"
              >
                <div class="min-w-0 space-y-0.5">
                  <div class="flex flex-wrap items-center gap-1.5">
                    <span class="font-mono text-oh-primary">{{ dest.id }}</span>
                    <CopyButton :value="dest.id" size="xs" />
                    <Badge :tone="dest.state === 'registered' ? 'success' : 'neutral'" size="xs">
                      {{ formatDestinationState(dest.state) }}
                    </Badge>
                    <span class="font-mono text-oh-muted">
                      {{ isZh ? `版本 ${dest.version}` : `rev ${dest.version}` }}
                    </span>
                  </div>
                  <p v-if="dest.hostHint" class="truncate font-mono text-[11px] text-oh-muted">
                    {{ dest.hostHint }}
                  </p>
                </div>
                <div class="flex items-center gap-1.5">
                  <Button
                    v-if="dest.state === 'registered'"
                    size="xs"
                    variant="ghost"
                    @click="exportDestinationId = dest.id"
                  >
                    {{ isZh ? '用于导出' : 'Use' }}
                  </Button>
                  <Button
                    v-if="dest.state === 'registered'"
                    size="xs"
                    variant="danger"
                    data-testid="telemetry-revoke-destination"
                    :loading="revokingDestinationId === dest.id"
                    @click="revokeCollector(dest.id, dest.version)"
                  >
                    {{ isZh ? '撤销' : 'Revoke' }}
                  </Button>
                </div>
              </li>
            </ul>
          </div>

          <details class="text-xs text-oh-secondary">
            <summary class="cursor-pointer select-none font-medium hover:text-oh-primary">
              {{ isZh ? '按采集端 ID 手动撤销已登记采集端' : 'Revoke existing collector by ID' }}
            </summary>
            <div class="mt-2 grid gap-2 sm:grid-cols-[1fr_7rem_auto] sm:items-end">
              <Input
                id="telemetry-manual-revoke-id"
                v-model="manualRevokeId"
                size="sm"
                mono
                :label="isZh ? '采集端 UUID' : 'Destination UUID'"
                placeholder="00000000-0000-4000-8000-000000000000"
              />
              <Input
                id="telemetry-manual-revoke-version"
                v-model="manualRevokeVersion"
                size="sm"
                mono
                :label="isZh ? '当前版本' : 'Version'"
                placeholder="1"
              />
              <Button
                size="sm"
                variant="danger"
                :loading="Boolean(revokingDestinationId) && revokingDestinationId === manualRevokeId.trim()"
                @click="revokeCollector(manualRevokeId, manualRevokeVersion)"
              >
                {{ isZh ? '执行撤销' : 'Revoke' }}
              </Button>
            </div>
          </details>
        </div>

        <!-- Right Column: Create Export Pipeline -->
        <div class="min-w-0 space-y-3 rounded-oh-md border border-oh-subtle bg-oh-subtle/40 p-3.5">
          <div class="space-y-0.5">
            <h4 class="text-sm font-semibold text-oh-primary">
              {{ isZh ? '2. 创建遥测导出流水线' : '2. Create Telemetry Export Pipeline' }}
            </h4>
            <p class="text-xs text-oh-muted">
              {{
                isZh
                  ? '固定导出时序延迟类别（timings），创建后自动进入采集端连通性验证队列。'
                  : 'Exports timing observations (timings) and queues collector reachability validation.'
              }}
            </p>
          </div>

          <form class="space-y-2.5" @submit.prevent="createExportPipeline">
            <Select
              v-if="registeredDestinationOptions.length > 0"
              :model-value="exportDestinationId"
              size="sm"
              mono
              :label="isZh ? '快速选择已注册采集端' : 'Select Registered Collector'"
              :placeholder="isZh ? '选择当前会话采集端' : 'Choose session collector'"
              :options="registeredDestinationOptions"
              @update:model-value="exportDestinationId = String($event)"
            />
            <Input
              id="telemetry-export-destination-id"
              v-model="exportDestinationId"
              size="sm"
              mono
              :label="isZh ? '目标采集端 ID (UUID)' : 'Target Destination UUID'"
              placeholder="00000000-0000-4000-8000-000000000000"
            />
            <div class="grid gap-2.5 sm:grid-cols-2">
              <Input
                id="telemetry-export-sample-rate"
                v-model="exportSampleRate"
                size="sm"
                mono
                :label="isZh ? '采样比例 (0 ~ 1)' : 'Sample Rate (0 ~ 1)'"
                placeholder="0.1"
              />
              <Input
                id="telemetry-export-retention-days"
                v-model="exportRetentionDays"
                type="number"
                size="sm"
                mono
                :label="isZh ? '队列保留天数 (1 ~ 30)' : 'Retention Days (1 ~ 30)'"
                placeholder="7"
              />
            </div>
            <div class="flex flex-wrap items-center justify-between gap-2 pt-1">
              <span class="text-xs text-oh-muted">
                {{ isZh ? '数据类别：请求时序指标 (timings)' : 'Data class: timings' }}
              </span>
              <Button
                type="submit"
                size="sm"
                data-testid="telemetry-create-export"
                :loading="creatingExport"
              >
                {{ isZh ? '创建导出流水线' : 'Create Pipeline' }}
              </Button>
            </div>
            <p v-if="createExportValidationError" role="alert" class="text-xs text-oh-status-danger-fg">
              {{ createExportValidationError }}
            </p>
          </form>

          <div v-if="sessionExports.length > 0" class="space-y-1.5 pt-1">
            <p class="text-xs font-medium text-oh-secondary">
              {{ isZh ? '已登记流水线快捷切换' : 'Session Export Pipelines' }}
            </p>
            <div class="flex flex-wrap gap-1.5">
              <Button
                v-for="exp in sessionExports"
                :key="exp.id"
                size="xs"
                variant="secondary"
                @click="loadExportDetail(exp.id)"
              >
                <span class="font-mono">{{ exp.id.slice(0, 8) }}…</span>
                <span class="ml-1">({{ formatExportState(exp.state) }})</span>
              </Button>
            </div>
          </div>
        </div>
      </div>

      <!-- Bottom Section: Export Inspection, Queue Depth & Live Reconfiguration -->
      <div class="space-y-3 rounded-oh-md border border-oh-subtle bg-oh-subtle/40 p-3.5">
        <div class="flex flex-wrap items-end justify-between gap-3">
          <div class="space-y-0.5">
            <h4 class="text-sm font-semibold text-oh-primary">
              {{ isZh ? '3. 导出状态、投递队列深度与启停调优' : '3. Pipeline Status, Queue Depth & Reconfiguration' }}
            </h4>
            <p class="text-xs text-oh-muted">
              {{
                isZh
                  ? '按导出流水线 ID 查询投递状态与积压队列深度，支持在线调整采样率、保留天数或启停流水线。'
                  : 'Inspect delivery status and queue depth by export ID, or update sampling, retention, and enabled state.'
              }}
            </p>
          </div>
          <form class="flex flex-wrap items-end gap-2" @submit.prevent="loadExportDetail()">
            <div class="w-64 sm:w-72">
              <Input
                id="telemetry-export-lookup-id"
                v-model="exportLookupId"
                size="sm"
                mono
                :label="isZh ? '导出流水线 ID' : 'Export Pipeline UUID'"
                placeholder="00000000-0000-4000-8000-000000000000"
              />
            </div>
            <Button
              type="submit"
              size="sm"
              variant="secondary"
              data-testid="telemetry-query-export"
              :loading="loadingExportDetail"
            >
              {{ isZh ? '查询详情与队列' : 'Inspect Pipeline' }}
            </Button>
          </form>
        </div>

        <div
          v-if="activeExportDetail"
          data-testid="telemetry-export-detail"
          class="grid gap-3 rounded-oh-md border border-oh-default bg-oh-surface p-3 text-xs sm:grid-cols-2 lg:grid-cols-4"
        >
          <div class="space-y-1">
            <span class="text-oh-muted">{{ isZh ? '运行状态 / 版本' : 'State / Version' }}</span>
            <div class="flex items-center gap-1.5">
              <Badge :tone="exportStateTone(activeExportDetail.state)" size="xs" dot>
                {{ formatExportState(activeExportDetail.state) }}
              </Badge>
              <span class="font-mono text-oh-secondary">
                {{ isZh ? `版本 ${activeExportDetail.version}` : `rev ${activeExportDetail.version}` }}
              </span>
            </div>
          </div>
          <div class="space-y-1">
            <span class="text-oh-muted">{{ isZh ? '待投递队列深度' : 'Queue Depth' }}</span>
            <p class="font-mono text-sm font-semibold tabular-nums text-oh-primary">
              {{ activeExportDetail.queue_depth }}
            </p>
          </div>
          <div class="space-y-1">
            <span class="text-oh-muted">{{ isZh ? '采样比例 / 保留天数' : 'Sample Rate / Retention' }}</span>
            <p class="font-mono text-oh-primary">
              {{ activeExportDetail.sample_rate }} ·
              {{ isZh ? `${activeExportDetail.retention_days} 天` : `${activeExportDetail.retention_days} days` }}
            </p>
          </div>
          <div class="space-y-1">
            <span class="text-oh-muted">{{ isZh ? '最近投递成功时间' : 'Last Delivery Success' }}</span>
            <p class="font-mono tabular-nums text-oh-primary">
              {{ formatDateTimeDisplay(activeExportDetail.last_success_at) }}
            </p>
          </div>
          <div class="space-y-1 sm:col-span-2">
            <span class="text-oh-muted">{{ isZh ? '绑定采集端 ID' : 'Bound Destination ID' }}</span>
            <div class="flex items-center gap-1.5 font-mono text-oh-secondary">
              <span class="truncate">{{ activeExportDetail.destination_id }}</span>
              <CopyButton :value="activeExportDetail.destination_id" size="xs" />
            </div>
          </div>
          <div class="space-y-1 sm:col-span-2">
            <span class="text-oh-muted">{{ isZh ? '最近错误原因' : 'Last Error Reason' }}</span>
            <p
              :class="[
                'font-mono',
                activeExportDetail.last_error_reason ? 'text-oh-status-danger-fg' : 'text-oh-secondary'
              ]"
            >
              {{ redactSensitiveText(activeExportDetail.last_error_reason) }}
            </p>
          </div>
        </div>

        <form
          class="grid gap-3 border-t border-oh-subtle pt-3 sm:grid-cols-2 lg:grid-cols-5"
          @submit.prevent="updateExportPipeline"
        >
          <Input
            id="telemetry-patch-sample-rate"
            v-model="patchSampleRate"
            size="sm"
            mono
            :label="isZh ? '新采样比例' : 'New Sample Rate'"
            placeholder="0.05"
          />
          <Input
            id="telemetry-patch-retention-days"
            v-model="patchRetentionDays"
            type="number"
            size="sm"
            mono
            :label="isZh ? '新保留天数 (1~30)' : 'New Retention (1~30)'"
            placeholder="7"
          />
          <Input
            id="telemetry-patch-version"
            v-model="patchVersion"
            size="sm"
            mono
            :label="isZh ? '并发控制版本' : 'Expected Version'"
            placeholder="1"
          />
          <div class="flex items-center pt-5">
            <Switch
              id="telemetry-patch-enabled"
              v-model="patchEnabled"
              size="sm"
              :label="patchEnabled ? (isZh ? '启用导出投递' : 'Pipeline Enabled') : (isZh ? '停用导出投递' : 'Pipeline Disabled')"
            />
          </div>
          <div class="flex items-end justify-end">
            <Button
              type="submit"
              size="sm"
              data-testid="telemetry-patch-export"
              :loading="updatingExport"
            >
              {{ isZh ? '更新流水线配置' : 'Apply Changes' }}
            </Button>
          </div>
          <p
            v-if="patchValidationError"
            role="alert"
            class="text-xs text-oh-status-danger-fg sm:col-span-2 lg:col-span-5"
          >
            {{ patchValidationError }}
          </p>
        </form>
      </div>
    </div>
  </Card>
</template>
