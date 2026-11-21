<script setup lang="ts">
import { computed, ref } from 'vue'
import { Badge, Button, Card, CopyButton, Input, Select, pushUiToast } from '@/components/ui'
import { classifyAsyncError } from '@/features/shared/asyncState'
import { normalizeApiError } from '@/utils/apiError'
import {
  generateCommandKey,
  requestGet,
  requestPost,
  requestPut,
  type CatalogAccountMode,
  type CatalogCompatibilityPublication,
  type CatalogDeprecationReason,
  type CatalogMaturity,
  type CatalogPublishPayload,
  type CatalogVerificationMatrixInput,
  type CatalogVerificationOperation,
  type CatalogVerificationTarget,
  type CatalogVerificationTargetDetail
} from './operationsApi'
import {
  computeCatalogEntryId,
  formatDateTimeDisplay,
  formatIfMatch,
  isValidAdapterVersion,
  isValidEntryId,
  isValidNonNegativeIntString,
  isValidPositiveIntString,
  isValidTargetId,
  validateVerificationInterval
} from './operationsHelpers'

const props = defineProps<{
  isZh: boolean
}>()

const ACCOUNT_MODES: CatalogAccountMode[] = [
  'api_key',
  'oauth',
  'setup-token',
  'service_account',
  'bedrock',
  'payg',
  'coding'
]

const ACCOUNT_MODE_LABELS: Record<CatalogAccountMode, { zh: string; en: string }> = {
  api_key: { zh: 'API 密钥 (api_key)', en: 'API Key (api_key)' },
  oauth: { zh: 'OAuth 授权 (oauth)', en: 'OAuth (oauth)' },
  'setup-token': { zh: '初始化令牌 (setup-token)', en: 'Setup Token (setup-token)' },
  service_account: { zh: '服务账号 (service_account)', en: 'Service Account (service_account)' },
  bedrock: { zh: '云托管凭据 (bedrock)', en: 'Bedrock IAM (bedrock)' },
  payg: { zh: '按量计费池 (payg)', en: 'Pay-As-You-Go (payg)' },
  coding: { zh: '编码专用通道 (coding)', en: 'Coding Channel (coding)' }
}

const accountModeOptions = computed(() =>
  ACCOUNT_MODES.map((mode) => ({
    value: mode,
    label: props.isZh ? ACCOUNT_MODE_LABELS[mode].zh : ACCOUNT_MODE_LABELS[mode].en
  }))
)

const rolloutWorkspace = ref('1')
const rolloutChannelPath = computed(() => {
  const workspace = rolloutWorkspace.value.trim()
  if (!isValidPositiveIntString(workspace)) return undefined
  return workspace === '1' ? '/admin/channels/pricing' : `/workspaces/${workspace}/channels/pricing`
})

const sessionTargets = ref<CatalogVerificationTarget[]>([])
const activeTargetDetail = ref<CatalogVerificationTargetDetail | null>(null)
const lastProbeOperation = ref<CatalogVerificationOperation | null>(null)

const permissionDenied = ref(false)
const workbenchError = ref('')
const workbenchSuccess = ref('')

function handleCatalogFailure(cause: unknown, fallbackZh: string, fallbackEn: string, onCurrentVersion?: (ver: string) => void) {
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
    workbenchError.value = ''
    return
  }
  permissionDenied.value = false
  if (issue.status === 412 && currentVer) {
    workbenchError.value = props.isZh
      ? `检测到版本冲突，已自动同步最新版本号 ${currentVer}，请确认后重新提交。`
      : `Precondition failed: synced latest version ${currentVer}. Confirm and submit again.`
    return
  }
  workbenchError.value = issue.message
}

// ==================== 1. Register Verification Target ====================
const targetProvider = ref('openai')
const targetModel = ref('')
const targetConnectionId = ref('')
const targetAdapterVersion = ref('ad_openai_chat_responses_v1')
const targetAction = ref('text.chat')
const targetAccountMode = ref<CatalogAccountMode>('api_key')
const targetProtocol = ref('openai_chat')
const targetIntervalInput = ref('0')
const registerValidationError = ref('')
const registeringTarget = ref(false)

function upsertSessionTarget(target: CatalogVerificationTarget) {
  const idx = sessionTargets.value.findIndex((item) => item.id === target.id)
  if (idx >= 0) {
    sessionTargets.value.splice(idx, 1, target)
  } else {
    sessionTargets.value = [target, ...sessionTargets.value]
  }
}

function syncFormsFromTarget(target: CatalogVerificationTarget, detail?: CatalogVerificationTargetDetail) {
  targetLookupId.value = target.id
  scheduleVersionInput.value = target.schedule_version
  scheduleIntervalInput.value = String(target.interval_seconds)
  probeProvider.value = target.provider
  probeModel.value = target.model
  probeEntryIdOverride.value = target.entry_id
  probeConnectionId.value = target.connection_id
  probeAdapterVersion.value = target.adapter_version
  probeAction.value = target.action
  probeAccountMode.value = target.account_mode
  probeProtocol.value = target.protocol

  if (detail) {
    publicationVersionInput.value = detail.publication?.publication_version ?? '0'
    const bestEvidence = detail.last_success?.evidence_id || detail.latest_attempt?.evidence_id || ''
    if (bestEvidence && !publishEvidenceId.value) {
      publishEvidenceId.value = bestEvidence
    }
  }
}

async function registerVerificationTarget() {
  if (registeringTarget.value) return
  const provider = targetProvider.value.trim()
  const model = targetModel.value.trim()
  const connectionId = targetConnectionId.value.trim()
  const adapterVersion = targetAdapterVersion.value.trim()
  const action = targetAction.value.trim()
  const protocol = targetProtocol.value.trim()

  if (!provider || provider.length > 64) {
    registerValidationError.value = props.isZh
      ? '请输入 1 至 64 个字符的提供商标识（如 openai）。'
      : 'Provider is required (1..64 characters).'
    return
  }
  if (!model || model.length > 256) {
    registerValidationError.value = props.isZh
      ? '请输入 1 至 256 个字符的模型名称。'
      : 'Model name is required (1..256 characters).'
    return
  }
  if (!isValidPositiveIntString(connectionId)) {
    registerValidationError.value = props.isZh
      ? '平台连接 ID 必须为正整数字符串。'
      : 'Platform connection ID must be a positive integer.'
    return
  }
  if (!isValidAdapterVersion(adapterVersion)) {
    registerValidationError.value = props.isZh
      ? '适配器版本格式无效（字母数字开头，可含 _ . -，最长 64 位）。'
      : 'Invalid adapter version format (alphanumeric start, up to 64 chars).'
    return
  }
  if (!action || !protocol) {
    registerValidationError.value = props.isZh
      ? '请填写网关能力动作与上游协议标识。'
      : 'Action and protocol are required.'
    return
  }
  const intervalCheck = validateVerificationInterval(targetIntervalInput.value, props.isZh)
  if (intervalCheck.error) {
    registerValidationError.value = intervalCheck.error
    return
  }

  registerValidationError.value = ''
  workbenchError.value = ''
  workbenchSuccess.value = ''
  permissionDenied.value = false
  registeringTarget.value = true
  try {
    const created = await requestPost<CatalogVerificationTarget>(
      '/admin/catalog/verification-targets',
      {
        provider,
        model,
        connection_id: connectionId,
        adapter_version: adapterVersion,
        action,
        account_mode: targetAccountMode.value,
        protocol,
        interval_seconds: intervalCheck.value
      },
      { 'Idempotency-Key': generateCommandKey() }
    )
    upsertSessionTarget(created)
    syncFormsFromTarget(created)
    workbenchSuccess.value = props.isZh
      ? `核验目标已注册（ID：${created.id}，目录条目：${created.entry_id}）。`
      : `Verification target registered (ID: ${created.id}, entry: ${created.entry_id}).`
    pushUiToast({
      tone: 'success',
      title: props.isZh ? '核验目标已注册' : 'Verification target registered',
      description: created.id
    })
  } catch (cause) {
    handleCatalogFailure(cause, '注册目录核验目标失败。', 'Failed to register catalog verification target.')
  } finally {
    registeringTarget.value = false
  }
}

// ==================== 2. Query Verification Target Detail ====================
const targetLookupId = ref('')
const loadingTargetDetail = ref(false)

async function loadTargetDetail(explicitId?: string) {
  if (loadingTargetDetail.value) return
  const id = (explicitId ?? targetLookupId.value).trim()
  if (!isValidTargetId(id)) {
    workbenchError.value = props.isZh
      ? '核验目标 ID 格式须为 vt_ 加标准 UUID。'
      : 'Target ID must match vt_<uuid>.'
    return
  }
  targetLookupId.value = id
  workbenchError.value = ''
  permissionDenied.value = false
  loadingTargetDetail.value = true
  try {
    const detail = await requestGet<CatalogVerificationTargetDetail>(
      `/admin/catalog/verification-targets/${encodeURIComponent(id)}`
    )
    activeTargetDetail.value = detail
    upsertSessionTarget(detail)
    syncFormsFromTarget(detail, detail)
  } catch (cause) {
    handleCatalogFailure(cause, '查询核验目标详情失败。', 'Failed to load verification target detail.')
  } finally {
    loadingTargetDetail.value = false
  }
}

// ==================== 3. Set Recurring Verification Schedule ====================
const scheduleIntervalInput = ref('0')
const scheduleVersionInput = ref('1')
const scheduleValidationError = ref('')
const savingSchedule = ref(false)

async function saveTargetSchedule() {
  if (savingSchedule.value) return
  const targetId = (activeTargetDetail.value?.id || targetLookupId.value).trim()
  if (!isValidTargetId(targetId)) {
    scheduleValidationError.value = props.isZh
      ? '请先查询或输入有效的核验目标 ID（vt_...）。'
      : 'Load or enter a valid verification target ID (vt_...) first.'
    return
  }
  if (!isValidNonNegativeIntString(scheduleVersionInput.value)) {
    scheduleValidationError.value = props.isZh
      ? '计划版本号必须为非负整数。'
      : 'Schedule version must be a non-negative integer.'
    return
  }
  const intervalCheck = validateVerificationInterval(scheduleIntervalInput.value, props.isZh)
  if (intervalCheck.error) {
    scheduleValidationError.value = intervalCheck.error
    return
  }

  scheduleValidationError.value = ''
  workbenchError.value = ''
  workbenchSuccess.value = ''
  permissionDenied.value = false
  savingSchedule.value = true
  try {
    const updated = await requestPut<CatalogVerificationTarget>(
      `/admin/catalog/verification-targets/${encodeURIComponent(targetId)}/schedule`,
      { interval_seconds: intervalCheck.value },
      { 'If-Match': formatIfMatch(scheduleVersionInput.value) }
    )
    upsertSessionTarget(updated)
    scheduleVersionInput.value = updated.schedule_version
    scheduleIntervalInput.value = String(updated.interval_seconds)
    if (activeTargetDetail.value && activeTargetDetail.value.id === updated.id) {
      activeTargetDetail.value = {
        ...activeTargetDetail.value,
        interval_seconds: updated.interval_seconds,
        schedule_version: updated.schedule_version,
        next_due_at: updated.next_due_at
      }
    }
    workbenchSuccess.value = props.isZh
      ? `核验计划已更新（版本 ${updated.schedule_version}，间隔 ${updated.interval_seconds === 0 ? '手动触发' : `${updated.interval_seconds} 秒`}）。`
      : `Verification schedule updated (version ${updated.schedule_version}, interval ${updated.interval_seconds === 0 ? 'manual' : `${updated.interval_seconds}s`}).`
  } catch (cause) {
    handleCatalogFailure(
      cause,
      '更新周期核验计划失败。',
      'Failed to update recurring verification schedule.',
      (ver) => {
        scheduleVersionInput.value = ver
      }
    )
  } finally {
    savingSchedule.value = false
  }
}

// ==================== 4. Publish Model Compatibility Status ====================
const publishMaturity = ref<CatalogMaturity>('experimental')
const publicationVersionInput = ref('0')
const publishEvidenceId = ref('')
const publishDeprecationReason = ref<CatalogDeprecationReason>('superseded')
const publishSunsetAt = ref('')
const publishRollbackVersion = ref('0')
const publishValidationError = ref('')
const publishingCompatibility = ref(false)

const maturityOptions = computed(() => [
  {
    value: 'experimental',
    label: props.isZh ? '实验性可用 (experimental)' : 'Experimental (experimental)'
  },
  {
    value: 'stable',
    label: props.isZh ? '稳定已验证 (stable)' : 'Stable Verified (stable)'
  },
  {
    value: 'deprecated',
    label: props.isZh ? '发布弃用通知 (deprecated)' : 'Deprecated Notice (deprecated)'
  },
  {
    value: 'closed',
    label: props.isZh ? '正式下线关闭 (closed)' : 'Closed / Retired (closed)'
  },
  {
    value: 'candidate',
    label: props.isZh ? '回退至候选或指定版本 (candidate)' : 'Roll Back to Candidate / Revision (candidate)'
  }
])

const deprecationReasonOptions = computed(() => [
  {
    value: 'superseded',
    label: props.isZh ? '已被新版本替代 (superseded)' : 'Superseded by newer model (superseded)'
  },
  {
    value: 'provider_retirement',
    label: props.isZh ? '上游服务商退市 (provider_retirement)' : 'Provider retirement (provider_retirement)'
  },
  {
    value: 'adapter_retirement',
    label: props.isZh ? '网关适配器下线 (adapter_retirement)' : 'Adapter retirement (adapter_retirement)'
  }
])

function buildPublishPayload(): CatalogPublishPayload | null {
  const mode = publishMaturity.value
  if (mode === 'experimental') {
    const ev = publishEvidenceId.value.trim()
    return ev ? { maturity: 'experimental', evidence_id: ev } : { maturity: 'experimental' }
  }
  if (mode === 'stable') {
    const ev = publishEvidenceId.value.trim()
    if (!ev) {
      publishValidationError.value = props.isZh
        ? '发布稳定版（stable）必须关联新鲜的真实探测证据 ID（ve_...）。'
        : 'Publishing stable compatibility requires a fresh verification evidence ID (ve_...).'
      return null
    }
    return { maturity: 'stable', evidence_id: ev }
  }
  if (mode === 'deprecated') {
    if (!publishSunsetAt.value) {
      publishValidationError.value = props.isZh
        ? '发布弃用通知必须指定未来的正式停服时间（sunset_at）。'
        : 'Deprecation notice requires a future sunset timestamp.'
      return null
    }
    const sunsetDate = new Date(publishSunsetAt.value)
    if (!Number.isFinite(+sunsetDate) || sunsetDate.getTime() <= Date.now()) {
      publishValidationError.value = props.isZh
        ? '停服下线时间必须晚于当前时间。'
        : 'Sunset time must be in the future.'
      return null
    }
    return {
      maturity: 'deprecated',
      deprecation_reason: publishDeprecationReason.value,
      sunset_at: sunsetDate.toISOString()
    }
  }
  if (mode === 'closed') {
    return { maturity: 'closed' }
  }
  const rollbackVer = publishRollbackVersion.value.trim()
  if (!isValidNonNegativeIntString(rollbackVer)) {
    publishValidationError.value = props.isZh
      ? '回退目标版本号须为非负整数（0 表示回退至初始候选状态）。'
      : 'Rollback version must be a non-negative integer (0 resets to candidate).'
    return null
  }
  return { rollback_version: rollbackVer }
}

async function publishCompatibility() {
  if (publishingCompatibility.value) return
  const targetId = (activeTargetDetail.value?.id || targetLookupId.value).trim()
  if (!isValidTargetId(targetId)) {
    publishValidationError.value = props.isZh
      ? '请先选择或输入有效的核验目标 ID（vt_...）。'
      : 'Select or enter a valid verification target ID (vt_...) first.'
    return
  }
  if (!isValidNonNegativeIntString(publicationVersionInput.value)) {
    publishValidationError.value = props.isZh
      ? '当前发布版本号须为非负整数（首次发布填 0）。'
      : 'Current publication version must be a non-negative integer (0 for initial publish).'
    return
  }
  const payload = buildPublishPayload()
  if (!payload) return

  publishValidationError.value = ''
  workbenchError.value = ''
  workbenchSuccess.value = ''
  permissionDenied.value = false
  publishingCompatibility.value = true
  try {
    const published = await requestPost<CatalogCompatibilityPublication>(
      `/admin/catalog/verification-targets/${encodeURIComponent(targetId)}/publications`,
      payload,
      {
        'Idempotency-Key': generateCommandKey(),
        'If-Match': formatIfMatch(publicationVersionInput.value)
      }
    )
    publicationVersionInput.value = published.publication_version
    if (activeTargetDetail.value && activeTargetDetail.value.id === targetId) {
      activeTargetDetail.value = {
        ...activeTargetDetail.value,
        publication: published
      }
    }
    workbenchSuccess.value = props.isZh
      ? `模型兼容性状态已发布：${formatMaturity(published.maturity)}（发布版本 ${published.publication_version}）。`
      : `Compatibility published: ${published.maturity} (publication version ${published.publication_version}).`
  } catch (cause) {
    handleCatalogFailure(
      cause,
      '发布模型兼容性状态失败。',
      'Failed to publish model compatibility status.',
      (ver) => {
        publicationVersionInput.value = ver
      }
    )
  } finally {
    publishingCompatibility.value = false
  }
}

// ==================== 5. Enqueue Immediate Platform Catalog Probe ====================
const probeProvider = ref('openai')
const probeModel = ref('')
const probeEntryIdOverride = ref('')
const probeConnectionId = ref('')
const probeAdapterVersion = ref('ad_openai_chat_responses_v1')
const probeAction = ref('text.chat')
const probeAccountMode = ref<CatalogAccountMode>('api_key')
const probeProtocol = ref('openai_chat')
const probeValidationError = ref('')
const enqueuingProbe = ref(false)
const refreshingProbeStatus = ref(false)

const derivedProbeEntryId = computed(() =>
  computeCatalogEntryId(probeProvider.value, probeModel.value)
)

const effectiveProbeEntryId = computed(() => {
  const manual = probeEntryIdOverride.value.trim()
  return manual || derivedProbeEntryId.value
})

async function enqueueCatalogProbe() {
  if (enqueuingProbe.value) return
  const entryId = effectiveProbeEntryId.value
  const adapterVersion = probeAdapterVersion.value.trim()
  const action = probeAction.value.trim()
  const protocol = probeProtocol.value.trim()

  if (!isValidEntryId(entryId)) {
    probeValidationError.value = props.isZh
      ? '请填写提供商与模型名称自动派生目录条目 ID，或直接输入合法的 m_ 条目 ID。'
      : 'Provide provider and model to derive entry ID, or enter a valid m_<32-hex> entry ID.'
    return
  }
  if (!isValidAdapterVersion(adapterVersion)) {
    probeValidationError.value = props.isZh
      ? '适配器版本格式不合法。'
      : 'Invalid adapter version format.'
    return
  }
  if (!action) {
    probeValidationError.value = props.isZh
      ? '请填写探测能力动作（如 text.chat）。'
      : 'Probe action is required (e.g. text.chat).'
    return
  }

  probeValidationError.value = ''
  workbenchError.value = ''
  workbenchSuccess.value = ''
  permissionDenied.value = false
  enqueuingProbe.value = true

  const matrixBody: CatalogVerificationMatrixInput = {
    entry_id: entryId,
    adapter_version: adapterVersion,
    action,
    account_mode: probeAccountMode.value,
    ...(protocol ? { protocol } : {})
  }

  try {
    const operation = await requestPost<CatalogVerificationOperation>(
      '/admin/catalog/verifications',
      matrixBody,
      { 'Idempotency-Key': generateCommandKey() }
    )
    lastProbeOperation.value = operation
    if (operation.result?.evidence_id && !publishEvidenceId.value) {
      publishEvidenceId.value = operation.result.evidence_id
    }
    workbenchSuccess.value = props.isZh
      ? `平台目录探测已入队（异步操作 ID：${operation.operation_id}，当前状态：${formatOperationState(operation.state)}）。`
      : `Catalog verification probe queued (Operation ID: ${operation.operation_id}, state: ${operation.state}).`
  } catch (cause) {
    handleCatalogFailure(cause, '触发平台目录探测入队失败。', 'Failed to enqueue platform catalog verification.')
  } finally {
    enqueuingProbe.value = false
  }
}

async function refreshProbeOperation() {
  if (!lastProbeOperation.value?.status_url || refreshingProbeStatus.value) return
  refreshingProbeStatus.value = true
  workbenchError.value = ''
  try {
    const updated = await requestGet<CatalogVerificationOperation>(lastProbeOperation.value.status_url)
    lastProbeOperation.value = updated
    if (updated.result?.evidence_id) {
      publishEvidenceId.value = updated.result.evidence_id
    }
  } catch (cause) {
    handleCatalogFailure(cause, '刷新异步探测状态失败。', 'Failed to refresh verification operation status.')
  } finally {
    refreshingProbeStatus.value = false
  }
}

function formatMaturity(maturity: string): string {
  switch (maturity) {
    case 'candidate':
      return props.isZh ? '候选待发布 (candidate)' : 'Candidate'
    case 'experimental':
      return props.isZh ? '实验性 (experimental)' : 'Experimental'
    case 'stable':
      return props.isZh ? '稳定版 (stable)' : 'Stable'
    case 'deprecated':
      return props.isZh ? '已弃用 (deprecated)' : 'Deprecated'
    case 'closed':
      return props.isZh ? '已关闭 (closed)' : 'Closed'
    default:
      return maturity
  }
}

function maturityTone(maturity: string): 'success' | 'info' | 'warning' | 'danger' | 'neutral' {
  switch (maturity) {
    case 'stable':
      return 'success'
    case 'experimental':
      return 'info'
    case 'deprecated':
      return 'warning'
    case 'closed':
      return 'danger'
    default:
      return 'neutral'
  }
}

function formatOperationState(state: string): string {
  switch (state) {
    case 'queued':
      return props.isZh ? '排队中' : 'Queued'
    case 'running':
      return props.isZh ? '探测执行中' : 'Running'
    case 'succeeded':
      return props.isZh ? '探测完成' : 'Succeeded'
    case 'failed':
      return props.isZh ? '探测失败' : 'Failed'
    default:
      return props.isZh ? '未知状态' : 'Unknown'
  }
}
</script>

<template>
  <Card
    data-testid="catalog-verification-workbench"
    :title="isZh ? '模型目录兼容性核验与发布工作台' : 'Model Catalog Verification & Publication Workbench'"
    :subtitle="isZh ? '注册平台模型核验目标、管理周期探测计划、立即触发真实探测并按证据推进兼容性生命周期发布。' : 'Register verification targets, manage recurring probe schedules, trigger immediate catalog probes, and publish evidence-backed compatibility states.'"
  >
    <div class="space-y-4">
      <p class="text-sm">
        {{ isZh
          ? '核验目标只接受平台工作区（#1）的活动连接。提供商、账号模式和协议须与连接一致。OpenAI Chat 经 Responses 桥接时，使用 ad_openai_chat_responses_v1，成功探测会自动运行兼容性 fixture。其他适配器请填写实际版本。'
          : 'Use an active platform workspace (#1) connection with the matching provider, account mode and protocol. For OpenAI Chat over the Responses bridge, use ad_openai_chat_responses_v1 to run the compatibility fixture on a successful probe. For other adapters enter their actual version.' }}
      </p>
      <p class="text-sm">
        {{ isZh
          ? 'experimental 已可用于上线。要在项目目录和 Playground 使用模型，请在目标工作区将已验证账号加入项目分组，将活动渠道关联该分组，保存后重新编辑渠道，确认并保存该模型。公开广场读取平台工作区的公开分组。'
          : 'Experimental is sufficient for rollout. In the target workspace, add the verified account to a project group, associate an active channel with the group, then save and reopen the channel to confirm the model. The public plaza reads public groups in the platform workspace.' }}
      </p>
      <div class="flex flex-wrap items-end gap-3">
        <Input id="catalog-rollout-workspace" v-model="rolloutWorkspace" size="sm"
          :label="isZh ? '上线目标工作区 ID' : 'Rollout workspace ID'" />
        <a v-if="rolloutChannelPath" :href="rolloutChannelPath" data-testid="catalog-channel-setup" class="underline">{{ isZh ? '管理渠道与广场展示' : 'Manage channels and plaza visibility' }}</a>
      </div>
      <div
        v-if="permissionDenied"
        role="alert"
        data-testid="catalog-permission-denied"
        class="rounded-oh-md border border-oh-status-warn-border bg-oh-status-warn-bg px-3 py-2 text-xs text-oh-status-warn-fg"
      >
        {{
          isZh
            ? '当前账号缺少平台目录核验或发布权限（需要 catalog.verify 或 catalog.publish 授权）。'
            : 'Permission denied: requires catalog.verify or catalog.publish administrative grant.'
        }}
      </div>

      <p
        v-if="workbenchError"
        role="alert"
        data-testid="catalog-workbench-error"
        class="rounded-oh-md border border-oh-status-danger-border bg-oh-status-danger-bg px-3 py-2 text-xs text-oh-status-danger-fg"
      >
        {{ workbenchError }}
      </p>

      <p
        v-if="workbenchSuccess"
        role="status"
        data-testid="catalog-workbench-success"
        class="rounded-oh-md border border-oh-status-ok-border bg-oh-status-ok-bg px-3 py-2 text-xs text-oh-status-ok-fg"
      >
        {{ workbenchSuccess }}
      </p>

      <div class="grid gap-4 xl:grid-cols-2">
        <!-- 1. Register Verification Target -->
        <div class="space-y-3 rounded-oh-md border border-oh-subtle bg-oh-subtle/40 p-3.5">
          <div class="space-y-0.5">
            <h4 class="text-sm font-semibold text-oh-primary">
              {{ isZh ? '1. 注册目录核验目标' : '1. Register Verification Target' }}
            </h4>
            <p class="text-xs text-oh-muted">
              {{
                isZh
                  ? '绑定已审核平台连接与模型协议矩阵，注册后初始处于候选（candidate）状态。'
                  : 'Bind a reviewed platform connection to a model/protocol matrix; starts as a candidate.'
              }}
            </p>
          </div>

          <form class="grid gap-2.5 sm:grid-cols-2" @submit.prevent="registerVerificationTarget">
            <Input
              id="catalog-target-provider"
              v-model="targetProvider"
              size="sm"
              mono
              :label="isZh ? '提供商 (provider)' : 'Provider'"
              placeholder="openai"
            />
            <Input
              id="catalog-target-model"
              v-model="targetModel"
              size="sm"
              mono
              :label="isZh ? '模型标识 (model)' : 'Model'"
              placeholder="gpt-4o-mini"
            />
            <Input
              id="catalog-target-connection-id"
              v-model="targetConnectionId"
              size="sm"
              mono
              :label="isZh ? '平台连接 ID' : 'Connection ID'"
              placeholder="101"
            />
            <Input
              id="catalog-target-adapter-version"
              v-model="targetAdapterVersion"
              size="sm"
              mono
              :label="isZh ? '适配器版本' : 'Adapter Version'"
              placeholder="ad_openai_chat_responses_v1"
            />
            <Input
              id="catalog-target-action"
              v-model="targetAction"
              size="sm"
              mono
              :label="isZh ? '能力动作 (action)' : 'Gateway Action'"
              placeholder="text.chat"
            />
            <Input
              id="catalog-target-protocol"
              v-model="targetProtocol"
              size="sm"
              mono
              :label="isZh ? '协议标识 (protocol)' : 'Protocol'"
              placeholder="openai_chat"
            />
            <Select
              v-model="targetAccountMode"
              size="sm"
              :label="isZh ? '账号凭据模式' : 'Account Mode'"
              :options="accountModeOptions"
            />
            <Input
              id="catalog-target-interval"
              v-model="targetIntervalInput"
              type="number"
              size="sm"
              mono
              :label="isZh ? '周期秒数 (0=手动)' : 'Interval Seconds (0=manual)'"
              placeholder="0"
            />
            <div class="flex items-center justify-end pt-1 sm:col-span-2">
              <Button
                type="submit"
                size="sm"
                data-testid="catalog-register-target"
                :loading="registeringTarget"
              >
                {{ isZh ? '注册核验目标' : 'Register Target' }}
              </Button>
            </div>
            <p v-if="registerValidationError" role="alert" class="text-xs text-oh-status-danger-fg sm:col-span-2">
              {{ registerValidationError }}
            </p>
          </form>
        </div>

        <!-- 5. Immediate Catalog Probe Enqueue -->
        <div class="space-y-3 rounded-oh-md border border-oh-subtle bg-oh-subtle/40 p-3.5">
          <div class="space-y-0.5">
            <h4 class="text-sm font-semibold text-oh-primary">
              {{ isZh ? '2. 立即触发平台目录探测入队' : '2. Enqueue Immediate Catalog Probe' }}
            </h4>
            <p class="text-xs text-oh-muted">
              {{
                isZh
                  ? '按模型矩阵异步入队真实上游探测任务，返回操作 ID 供跟踪观测证据。'
                  : 'Enqueue an asynchronous live probe for a model compatibility matrix and track its operation ID.'
              }}
            </p>
          </div>

          <form class="grid gap-2.5 sm:grid-cols-2" @submit.prevent="enqueueCatalogProbe">
            <Input
              id="catalog-probe-provider"
              v-model="probeProvider"
              size="sm"
              mono
              :label="isZh ? '提供商' : 'Provider'"
              placeholder="openai"
              @update:model-value="probeEntryIdOverride = ''"
            />
            <Input
              id="catalog-probe-model"
              v-model="probeModel"
              size="sm"
              mono
              :label="isZh ? '模型名称' : 'Model'"
              placeholder="gpt-4o-mini"
              @update:model-value="probeEntryIdOverride = ''"
            />
            <Input
              id="catalog-probe-entry-id"
              :model-value="effectiveProbeEntryId"
              size="sm"
              mono
              :label="isZh ? '目录条目 ID (自动派生或手动输入)' : 'Catalog Entry ID (derived or manual)'"
              placeholder="m_00000000000000000000000000000000"
              @update:model-value="probeEntryIdOverride = $event"
            />
            <Input
              id="catalog-probe-connection-id"
              v-model="probeConnectionId"
              size="sm"
              mono
              :label="isZh ? '关联连接 ID（选填备查）' : 'Connection ID (reference)'"
              placeholder="101"
            />
            <Input
              id="catalog-probe-adapter-version"
              v-model="probeAdapterVersion"
              size="sm"
              mono
              :label="isZh ? '适配器版本' : 'Adapter Version'"
              placeholder="ad_openai_chat_responses_v1"
            />
            <Input
              id="catalog-probe-action"
              v-model="probeAction"
              size="sm"
              mono
              :label="isZh ? '探测能力动作' : 'Action'"
              placeholder="text.chat"
            />
            <Select
              v-model="probeAccountMode"
              size="sm"
              :label="isZh ? '账号凭据模式' : 'Account Mode'"
              :options="accountModeOptions"
            />
            <Input
              id="catalog-probe-protocol"
              v-model="probeProtocol"
              size="sm"
              mono
              :label="isZh ? '协议标识（选填）' : 'Protocol (optional)'"
              placeholder="openai_chat"
            />
            <div class="flex items-center justify-end pt-1 sm:col-span-2">
              <Button
                type="submit"
                size="sm"
                data-testid="catalog-enqueue-probe"
                :loading="enqueuingProbe"
              >
                {{ isZh ? '立即触发探测入队' : 'Enqueue Probe' }}
              </Button>
            </div>
            <p v-if="probeValidationError" role="alert" class="text-xs text-oh-status-danger-fg sm:col-span-2">
              {{ probeValidationError }}
            </p>
          </form>

          <div
            v-if="lastProbeOperation"
            data-testid="catalog-probe-operation"
            class="space-y-1.5 rounded-oh-sm border border-oh-default bg-oh-surface p-2.5 text-xs"
          >
            <div class="flex flex-wrap items-center justify-between gap-2">
              <div class="flex flex-wrap items-center gap-1.5">
                <span class="text-oh-muted">{{ isZh ? '操作 ID：' : 'Operation:' }}</span>
                <span class="font-mono text-oh-primary">{{ lastProbeOperation.operation_id }}</span>
                <CopyButton :value="lastProbeOperation.operation_id" size="xs" />
                <Badge
                  :tone="lastProbeOperation.state === 'succeeded' ? 'success' : lastProbeOperation.state === 'failed' ? 'danger' : 'info'"
                  size="xs"
                  dot
                >
                  {{ formatOperationState(lastProbeOperation.state) }}
                </Badge>
              </div>
              <Button
                size="xs"
                variant="ghost"
                :loading="refreshingProbeStatus"
                @click="refreshProbeOperation"
              >
                {{ isZh ? '刷新状态' : 'Refresh Status' }}
              </Button>
            </div>
            <p v-if="lastProbeOperation.result" class="font-mono text-[11px] text-oh-secondary">
              {{ isZh ? '生成证据：' : 'Evidence:' }}
              {{ lastProbeOperation.result.evidence_id }} · {{ lastProbeOperation.result.state }} · {{ lastProbeOperation.result.freshness }}
            </p>
          </div>
        </div>
      </div>

      <!-- 2, 3, 4: Target Detail Inspection, Schedule Management & Compatibility Publication -->
      <div class="space-y-4 rounded-oh-md border border-oh-subtle bg-oh-subtle/40 p-3.5">
        <div class="flex flex-wrap items-end justify-between gap-3">
          <div class="space-y-0.5">
            <h4 class="text-sm font-semibold text-oh-primary">
              {{ isZh ? '3. 核验目标观测详情、周期计划与兼容性发布' : '3. Target Inspection, Schedule & Compatibility Publication' }}
            </h4>
            <p class="text-xs text-oh-muted">
              {{
                isZh
                  ? '读取核验目标最近探测记录与已发布状态，支持乐观并发锁更新核验周期或发布生命周期状态。'
                  : 'Inspect recent observations and active publication, update recurring intervals, or publish lifecycle transitions.'
              }}
            </p>
          </div>
          <form class="flex flex-wrap items-end gap-2" @submit.prevent="loadTargetDetail()">
            <div class="w-64 sm:w-80">
              <Input
                id="catalog-target-lookup-id"
                v-model="targetLookupId"
                size="sm"
                mono
                :label="isZh ? '核验目标 ID (vt_...)' : 'Verification Target ID (vt_...)'"
                placeholder="vt_00000000-0000-0000-0000-000000000000"
              />
            </div>
            <Button
              type="submit"
              size="sm"
              variant="secondary"
              data-testid="catalog-query-target"
              :loading="loadingTargetDetail"
            >
              {{ isZh ? '查询目标详情' : 'Load Target' }}
            </Button>
          </form>
        </div>

        <div v-if="sessionTargets.length > 0" class="flex flex-wrap items-center gap-1.5">
          <span class="text-xs text-oh-muted">{{ isZh ? '会话目标：' : 'Session targets:' }}</span>
          <Button
            v-for="item in sessionTargets"
            :key="item.id"
            size="xs"
            variant="secondary"
            @click="loadTargetDetail(item.id)"
          >
            <span class="font-mono">{{ item.provider }}/{{ item.model }}</span>
          </Button>
        </div>

        <div
          v-if="activeTargetDetail"
          data-testid="catalog-target-detail"
          class="space-y-3 rounded-oh-md border border-oh-default bg-oh-surface p-3 text-xs"
        >
          <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <div class="space-y-0.5">
              <span class="text-oh-muted">{{ isZh ? '模型与协议矩阵' : 'Model & Matrix' }}</span>
              <p class="font-mono font-semibold text-oh-primary">
                {{ activeTargetDetail.provider }} / {{ activeTargetDetail.model }}
              </p>
              <p class="font-mono text-[11px] text-oh-secondary">
                {{ activeTargetDetail.action }} · {{ activeTargetDetail.protocol }} · {{ activeTargetDetail.account_mode }}
              </p>
            </div>
            <div class="space-y-0.5">
              <span class="text-oh-muted">{{ isZh ? '条目 ID / 连接 ID' : 'Entry ID / Connection' }}</span>
              <div class="flex items-center gap-1 font-mono text-oh-primary">
                <span class="truncate">{{ activeTargetDetail.entry_id }}</span>
                <CopyButton :value="activeTargetDetail.entry_id" size="xs" />
              </div>
              <p class="font-mono text-[11px] text-oh-secondary">
                {{ isZh ? `连接 #${activeTargetDetail.connection_id} · 适配器 ${activeTargetDetail.adapter_version}` : `Conn #${activeTargetDetail.connection_id} · Adapter ${activeTargetDetail.adapter_version}` }}
              </p>
            </div>
            <div class="space-y-0.5">
              <span class="text-oh-muted">{{ isZh ? '核验计划版本 / 下次执行' : 'Schedule Version / Next Due' }}</span>
              <p class="font-mono text-oh-primary">
                {{ isZh ? `版本 ${activeTargetDetail.schedule_version}` : `rev ${activeTargetDetail.schedule_version}` }} ·
                {{ activeTargetDetail.interval_seconds === 0 ? (isZh ? '手动触发' : 'Manual') : `${activeTargetDetail.interval_seconds}s` }}
              </p>
              <p class="font-mono text-[11px] tabular-nums text-oh-secondary">
                {{ formatDateTimeDisplay(activeTargetDetail.next_due_at) }}
              </p>
            </div>
            <div class="space-y-0.5">
              <span class="text-oh-muted">{{ isZh ? '当前对外发布状态' : 'Current Publication' }}</span>
              <div v-if="activeTargetDetail.publication" class="space-y-0.5">
                <div class="flex items-center gap-1.5">
                  <Badge :tone="maturityTone(activeTargetDetail.publication.maturity)" size="xs" dot>
                    {{ formatMaturity(activeTargetDetail.publication.maturity) }}
                  </Badge>
                  <span class="font-mono text-oh-secondary">
                    {{ isZh ? `版本 ${activeTargetDetail.publication.publication_version}` : `rev ${activeTargetDetail.publication.publication_version}` }}
                  </span>
                </div>
                <p v-if="activeTargetDetail.publication.deprecation" class="text-[11px] text-oh-status-warn-fg">
                  {{ isZh ? '停服时间：' : 'Sunset: ' }}{{ formatDateTimeDisplay(activeTargetDetail.publication.deprecation.sunset_at) }}
                </p>
              </div>
              <p v-else class="text-oh-muted">
                {{ isZh ? '尚未发布（候选状态，发布版本 0）' : 'Unpublished (candidate, version 0)' }}
              </p>
            </div>
          </div>

          <div class="grid gap-3 border-t border-oh-subtle pt-2.5 sm:grid-cols-2">
            <div class="space-y-1">
              <span class="font-medium text-oh-secondary">{{ isZh ? '最近一次探测尝试 (latest_attempt)' : 'Latest Probe Attempt' }}</span>
              <div v-if="activeTargetDetail.latest_attempt" class="space-y-0.5 font-mono text-[11px]">
                <p>
                  {{ activeTargetDetail.latest_attempt.evidence_id }} ·
                  {{ activeTargetDetail.latest_attempt.state }} ({{ activeTargetDetail.latest_attempt.freshness }})
                </p>
                <p class="text-oh-muted">
                  {{ formatDateTimeDisplay(activeTargetDetail.latest_attempt.observed_at) }}
                  <span v-if="activeTargetDetail.latest_attempt.reason"> · {{ activeTargetDetail.latest_attempt.reason }}</span>
                </p>
              </div>
              <p v-else class="text-oh-muted">{{ isZh ? '暂无探测记录' : 'No attempts recorded' }}</p>
            </div>

            <div class="space-y-1">
              <span class="font-medium text-oh-secondary">{{ isZh ? '最近一次成功证据 (last_success)' : 'Last Successful Evidence' }}</span>
              <div v-if="activeTargetDetail.last_success" class="space-y-0.5 font-mono text-[11px]">
                <div class="flex flex-wrap items-center gap-1.5">
                  <span>{{ activeTargetDetail.last_success.evidence_id }}</span>
                  <Button
                    size="xs"
                    variant="ghost"
                    @click="publishEvidenceId = activeTargetDetail.last_success.evidence_id"
                  >
                    {{ isZh ? '填入发布表单' : 'Use Evidence' }}
                  </Button>
                </div>
                <p class="text-oh-muted">
                  {{ activeTargetDetail.last_success.source }} · {{ activeTargetDetail.last_success.freshness }} ·
                  {{ formatDateTimeDisplay(activeTargetDetail.last_success.observed_at) }}
                </p>
              </div>
              <p v-else class="text-oh-muted">{{ isZh ? '暂无成功探测证据' : 'No successful evidence yet' }}</p>
            </div>
          </div>
        </div>

        <div class="grid gap-4 border-t border-oh-subtle pt-3 xl:grid-cols-2">
          <!-- 3. Schedule Form -->
          <form class="space-y-2.5" @submit.prevent="saveTargetSchedule">
            <h5 class="text-xs font-semibold text-oh-primary">
              {{ isZh ? '设置周期核验计划' : 'Update Recurring Verification Schedule' }}
            </h5>
            <div class="grid gap-2.5 sm:grid-cols-[1fr_7.5rem_auto] sm:items-end">
              <Input
                id="catalog-schedule-interval"
                v-model="scheduleIntervalInput"
                type="number"
                size="sm"
                mono
                :label="isZh ? '核验间隔秒数 (0 或 300~604800)' : 'Interval Seconds (0 or 300~604800)'"
                placeholder="3600"
              />
              <Input
                id="catalog-schedule-version"
                v-model="scheduleVersionInput"
                size="sm"
                mono
                :label="isZh ? '计划版本号' : 'Schedule Ver'"
                placeholder="1"
              />
              <Button
                type="submit"
                size="sm"
                data-testid="catalog-save-schedule"
                :loading="savingSchedule"
              >
                {{ isZh ? '保存核验计划' : 'Save Schedule' }}
              </Button>
            </div>
            <p v-if="scheduleValidationError" role="alert" class="text-xs text-oh-status-danger-fg">
              {{ scheduleValidationError }}
            </p>
          </form>

          <!-- 4. Publication Form -->
          <form class="space-y-2.5" @submit.prevent="publishCompatibility">
            <h5 class="text-xs font-semibold text-oh-primary">
              {{ isZh ? '发布模型兼容性生命周期状态' : 'Publish Compatibility Lifecycle State' }}
            </h5>
            <div class="grid gap-2.5 sm:grid-cols-2">
              <Select
                v-model="publishMaturity"
                size="sm"
                :label="isZh ? '目标生命周期状态' : 'Target Lifecycle Maturity'"
                :options="maturityOptions"
              />
              <Input
                id="catalog-publish-version"
                v-model="publicationVersionInput"
                size="sm"
                mono
                :label="isZh ? '当前发布版本 (首次为 0)' : 'Current Pub Version (0 if new)'"
                placeholder="0"
              />
              <Input
                v-if="publishMaturity === 'experimental' || publishMaturity === 'stable'"
                id="catalog-publish-evidence-id"
                v-model="publishEvidenceId"
                size="sm"
                mono
                class="sm:col-span-2"
                :label="publishMaturity === 'stable' ? (isZh ? '核验证据 ID (必填，ve_...)' : 'Evidence ID (required, ve_...)') : (isZh ? '核验证据 ID (选填，ve_...)' : 'Evidence ID (optional, ve_...)')"
                placeholder="ve_00000000-0000-0000-0000-000000000000"
              />
              <template v-if="publishMaturity === 'deprecated'">
                <Select
                  v-model="publishDeprecationReason"
                  size="sm"
                  :label="isZh ? '弃用原因' : 'Deprecation Reason'"
                  :options="deprecationReasonOptions"
                />
                <label class="grid min-w-0 gap-1 text-xs font-medium text-oh-secondary">
                  <span>{{ isZh ? '计划停服时间 (sunset_at)' : 'Sunset Timestamp' }}</span>
                  <input v-model="publishSunsetAt" type="datetime-local" class="input oh-focus-ring h-8 w-full min-w-0 text-xs" />
                </label>
              </template>
              <Input
                v-if="publishMaturity === 'candidate'"
                id="catalog-publish-rollback-version"
                v-model="publishRollbackVersion"
                size="sm"
                mono
                class="sm:col-span-2"
                :label="isZh ? '回退目标版本号 (0 表示回退至初始候选状态)' : 'Rollback Target Version (0 resets to candidate)'"
                placeholder="0"
              />
            </div>
            <div class="flex justify-end">
              <Button
                type="submit"
                size="sm"
                data-testid="catalog-publish-compatibility"
                :loading="publishingCompatibility"
              >
                {{ isZh ? '发布兼容性状态' : 'Publish Compatibility' }}
              </Button>
            </div>
            <p v-if="publishValidationError" role="alert" class="text-xs text-oh-status-danger-fg">
              {{ publishValidationError }}
            </p>
          </form>
        </div>
      </div>
    </div>
  </Card>
</template>
