<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { adminAPI } from '@/api/admin'
import AppLayout from '@/components/layout/AppLayout.vue'
import {
  Badge,
  Button,
  Card,
  Checkbox,
  Input,
  PageHeader,
  SegmentedControl,
  Select,
  Stepper
} from '@/components/ui'
import { connectionsApi, pollConnectionOperation } from '@/features/connections/api'
import AsyncStateBanner from '@/features/connections/components/AsyncStateBanner.vue'
import {
  buildHeaderOverridesMap,
  getAuthModeLabel,
  getProviderDisplayMeta,
  isConnectionResource,
  isHeaderOverrideSupported,
  isOAuthWorkflowSupported,
  isVerificationFresh,
  mapApiErrorToAsyncContext,
  normalizeConnectionEndpointInput,
  validateConnectionEndpoint
} from '@/features/connections/rules'
import type {
  AsyncPageState,
  ConnectionEvent,
  ConnectionOperation,
  ConnectionPool,
  ConnectionSchema,
  CredentialOperationMode
} from '@/features/connections/types'
import {
  buildCredentialInputPayload,
  canProceedFromStep,
  createInitialWizardState,
  getWizardStepStatuses,
  hydrateWizardFromResource,
  invalidatePreview,
  invalidateVerification,
  WIZARD_STEP_ORDER,
  type WizardStepId
} from '@/features/connections/wizardState'
import { useAppStore } from '@/stores/app'
import type { Proxy as AccountProxy } from '@/types'

const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
const i18n = useI18n()
const isZh = computed(() => String(i18n.locale?.value || '').toLowerCase().startsWith('zh'))

const connectionIdParam = computed(() => {
  const raw = route.params.id
  return typeof raw === 'string' && raw.length > 0 ? raw : null
})

const state = reactive(createInitialWizardState(connectionIdParam.value ? 'edit' : 'create'))
const schemas = ref<ConnectionSchema[]>([])
const pools = ref<ConnectionPool[]>([])
const proxies = ref<AccountProxy[]>([])
const events = ref<ConnectionEvent[]>([])
const eventsLoading = ref(false)
const eventsExpanded = ref(true)
const schemaSearch = ref('')
const modelIdsText = ref('')
const oauthCode = ref('')
const newPoolName = ref('')
const creatingPool = ref(false)
const endpointNormalizationHint = ref('')
const stepAttempted = ref(false)
const activatedInSession = ref(false)

const pageAsyncState = ref<AsyncPageState | null>('initial_load')
const busy = ref(false)
const verifyOperation = ref<ConnectionOperation | null>(null)

const wizardPageTitle = computed(() => {
  if (state.mode === 'edit') {
    return isZh.value ? '编辑上游连接' : 'Edit Upstream Connection'
  }
  return isZh.value ? '新建上游连接' : 'New Upstream Connection'
})

const wizardBreadcrumbs = computed(() => [
  { label: isZh.value ? '管理员' : 'Admin', to: '/admin/accounts' },
  { label: isZh.value ? '上游连接' : 'Upstream Connections', to: '/admin/accounts' },
  { label: wizardPageTitle.value }
])

watch(
  [wizardPageTitle, () => appStore.siteName],
  ([title, siteName]) => {
    if (route.meta) {
      route.meta.titleKey = ''
      route.meta.title = title
    }
    if (typeof document !== 'undefined') {
      const brand = siteName?.trim() || 'OmniHive'
      document.title = `${title} - ${brand}`
    }
  },
  { immediate: true }
)

const stepLabels = computed<Record<WizardStepId, { title: string; description: string }>>(() =>
  isZh.value
    ? {
        provider: { title: '协议与端点', description: '选择上游协议、服务地址与请求头' },
        credentials: { title: '认证凭据', description: '配置密钥或接入 OAuth 授权' },
        verify: { title: '连通性验证', description: '执行网络、TLS、鉴权与模型探测' },
        preview: { title: '资源池与预览', description: '绑定出站资源池、模型与并发上限' },
        activate: { title: '确认并启用', description: '核对配置修订并正式启用连接' }
      }
    : {
        provider: { title: 'Provider & Endpoint', description: 'Select upstream protocol, URL & headers' },
        credentials: { title: 'Credentials', description: 'Configure secret or enroll OAuth' },
        verify: { title: 'Verify', description: 'Run live network, TLS, auth & model probe' },
        preview: { title: 'Pools & Preview', description: 'Assign pools, models & concurrency cap' },
        activate: { title: 'Confirm & Activate', description: 'Review revision and enable connection' }
      }
)

const stepperItems = computed(() => getWizardStepStatuses(state, stepLabels.value))

const filteredSchemas = computed(() => {
  const query = schemaSearch.value.trim().toLowerCase()
  if (!query) return schemas.value
  return schemas.value.filter((schema) => {
    const meta = getProviderDisplayMeta(schema.provider)
    return (
      schema.provider.toLowerCase().includes(query) ||
      meta.label.toLowerCase().includes(query) ||
      schema.auth_types.some((t) => t.toLowerCase().includes(query))
    )
  })
})

const selectedSchema = computed(() =>
  schemas.value.find(
    (s) => s.provider === state.provider && s.auth_types.includes(state.authType)
  ) ?? schemas.value.find((s) => s.provider === state.provider) ?? null
)

const authModeOptions = computed(() => {
  const schema = schemas.value.find((s) => s.provider === state.provider)
  if (!schema || schema.auth_types.length === 0) {
    return [{ value: 'api_key', label: 'API Key' }]
  }
  return schema.auth_types.map((authType) => ({
    value: authType,
    label: getAuthModeLabel(authType, isZh.value)
  }))
})

const credentialModeOptions = computed(() => {
  if (state.mode === 'edit') {
    return [
      { value: 'keep', label: isZh.value ? '保留现有凭据' : 'Keep Existing' },
      { value: 'set', label: isZh.value ? '替换密钥' : 'Replace Secret' },
      { value: 'clear', label: isZh.value ? '清空密钥' : 'Clear Secret' }
    ]
  }
  return [{ value: 'set', label: isZh.value ? '设置密钥' : 'Set Secret' }]
})

const proxyOptions = computed(() => {
  const base = [
    {
      value: '',
      label: isZh.value ? '默认直连（不使用代理）' : 'Direct Connection (No Proxy)'
    }
  ]
  const activeProxies = proxies.value.filter((p) => !p.status || p.status === 'active')
  for (const proxy of activeProxies) {
    const idStr = String(proxy.id)
    const addr = proxy.host && proxy.port ? `${proxy.protocol || 'http'}://${proxy.host}:${proxy.port}` : ''
    base.push({
      value: idStr,
      label: addr ? `#${idStr} · ${proxy.name} (${addr})` : `#${idStr} · ${proxy.name}`
    })
  }
  if (
    state.networkPolicyId &&
    !base.some((opt) => opt.value === state.networkPolicyId)
  ) {
    base.push({
      value: state.networkPolicyId,
      label: isZh.value ? `代理 #${state.networkPolicyId}` : `Proxy #${state.networkPolicyId}`
    })
  }
  return base
})

const supportsOAuth = computed(() => isOAuthWorkflowSupported(selectedSchema.value, state.authType))
const supportsHeaderOverrides = computed(() =>
  isHeaderOverrideSupported(state.provider, state.authType)
)
const stepValidation = computed(() =>
  canProceedFromStep(state, state.currentStep, selectedSchema.value, Date.now(), isZh.value)
)

const endpointInlineError = computed(() => {
  const endpointRequired = Boolean(
    selectedSchema.value?.fields?.some((field) => field.path === 'endpoint' && field.required)
  )
  if (!state.endpoint.trim() && !stepAttempted.value) return ''
  return validateConnectionEndpoint(state.endpoint, endpointRequired, isZh.value) || ''
})

const discoveredModels = computed(() => state.verification?.models ?? [])

async function loadConnectionEvents(connectionId?: string | null) {
  const targetId = connectionId ?? state.connectionId
  if (!targetId) {
    events.value = []
    return
  }
  eventsLoading.value = true
  try {
    const page = await connectionsApi.listEvents(targetId, { limit: 20 })
    events.value = page.items ?? []
  } catch {
    events.value = []
  } finally {
    eventsLoading.value = false
  }
}

async function loadInitialData() {
  pageAsyncState.value = 'initial_load'
  state.asyncError = null
  try {
    const [schemaPage, poolPage, proxyList] = await Promise.all([
      connectionsApi.listProviderSchemas({ limit: 100 }),
      connectionsApi.listPools({ limit: 100 }).catch(() => ({ items: [] })),
      adminAPI.proxies?.getAll ? adminAPI.proxies.getAll().catch(() => []) : Promise.resolve([])
    ])
    schemas.value = schemaPage.items
    pools.value = poolPage.items
    proxies.value = Array.isArray(proxyList) ? proxyList : []

    if (connectionIdParam.value) {
      const resource = await connectionsApi.getConnection(connectionIdParam.value)
      Object.assign(state, hydrateWizardFromResource(state, resource))
      if (state.networkPolicyId === 'default') {
        state.networkPolicyId = ''
      }
      modelIdsText.value = resource.model_ids.join(', ')
      void loadConnectionEvents(resource.id)
    } else if (schemas.value.length > 0 && !state.provider) {
      selectProviderSchema(schemas.value[0])
    }
    pageAsyncState.value = schemas.value.length === 0 && !connectionIdParam.value ? 'empty' : null
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(
      err,
      isZh.value ? '加载上游连接配置数据失败。' : 'Failed to load connection wizard data.',
      isZh.value
    )
    state.asyncError = mapped
    pageAsyncState.value = mapped.state
  }
}

onMounted(() => {
  void loadInitialData()
})

watch(connectionIdParam, () => {
  void loadInitialData()
})

function selectProviderSchema(schema: ConnectionSchema) {
  const prevMeta = state.provider ? getProviderDisplayMeta(state.provider) : null
  const prevAutoNameEn = prevMeta
    ? `${prevMeta.label} (${getAuthModeLabel(state.authType, false)})`
    : ''
  const prevAutoNameZh = prevMeta
    ? `${prevMeta.label} (${getAuthModeLabel(state.authType, true)})`
    : ''
  state.provider = schema.provider
  state.authType = schema.auth_types[0] ?? 'api_key'
  state.schemaVersion = schema.schema_version
  const meta = getProviderDisplayMeta(schema.provider)
  if (!state.endpoint || (prevMeta && state.endpoint === prevMeta.defaultEndpoint)) {
    state.endpoint = meta.defaultEndpoint
  }
  if (
    !state.name ||
    state.name === prevAutoNameEn ||
    state.name === prevAutoNameZh
  ) {
    state.name = `${meta.label} (${getAuthModeLabel(state.authType, isZh.value)})`
  }
  endpointNormalizationHint.value = ''
  Object.assign(state, invalidateVerification(state))
}

function handleEndpointChange(value: string | number) {
  state.endpoint = String(value)
  endpointNormalizationHint.value = ''
  Object.assign(state, invalidateVerification(state))
}

function normalizeStepOneInputs() {
  state.name = state.name.trim()
  if (state.networkPolicyId.trim().toLowerCase() === 'default') {
    state.networkPolicyId = ''
  } else {
    state.networkPolicyId = state.networkPolicyId.trim()
  }
  if (state.endpoint.trim()) {
    const { normalized, strippedSuffix } = normalizeConnectionEndpointInput(state.endpoint)
    if (normalized !== state.endpoint) {
      state.endpoint = normalized
    }
    if (strippedSuffix) {
      endpointNormalizationHint.value = isZh.value
        ? `已自动规范化为基础服务地址（已移除接口路径后缀 ${strippedSuffix}）。`
        : `Normalized to base endpoint URL (removed route suffix ${strippedSuffix}).`
    }
  } else {
    state.endpoint = ''
  }
}

function handleCredentialModeChange(mode: string | number) {
  state.credentialMode = mode as CredentialOperationMode
  Object.assign(state, invalidateVerification(state))
}

function addHeaderOverrideRow(presetKey = '', presetValue = '') {
  const existingIdx = presetKey
    ? state.headerOverrides.findIndex((r) => r.key.trim().toLowerCase() === presetKey.toLowerCase())
    : -1
  if (existingIdx >= 0) {
    state.headerOverrides[existingIdx].value = presetValue
  } else {
    state.headerOverrides.push({ key: presetKey, value: presetValue })
  }
  Object.assign(state, invalidateVerification(state))
}

function removeHeaderOverrideRow(index: number) {
  state.headerOverrides.splice(index, 1)
  Object.assign(state, invalidateVerification(state))
}

function applyCompatibleUaPreset() {
  addHeaderOverrideRow('User-Agent', 'curl/8.7.1')
}

function addDiscoveredModel(modelId: string) {
  const current = modelIdsText.value
    .split(/[,\n]/)
    .map((m) => m.trim())
    .filter(Boolean)
  if (!current.includes(modelId)) {
    modelIdsText.value = [...current, modelId].join(', ')
    Object.assign(state, invalidatePreview(state))
  }
}

async function ensureConnectionPersisted(): Promise<boolean> {
  state.asyncError = null
  normalizeStepOneInputs()
  const step1Check = canProceedFromStep(state, 'provider', selectedSchema.value, Date.now(), isZh.value)
  if (!step1Check.valid) {
    stepAttempted.value = true
    state.asyncError = {
      state: 'partial_failure',
      status: 422,
      message: step1Check.reason || (isZh.value ? '请先修正第 1 步配置项。' : 'Fix Step 1 fields first.')
    }
    pageAsyncState.value = 'partial_failure'
    return false
  }

  const headerMap = buildHeaderOverridesMap(state.headerOverrides)
  const hasHeaderOverrides = Object.keys(headerMap).length > 0
  const pendingCredentialMode = state.credentialMode
  const pendingCredentialPayload = buildCredentialInputPayload(state)
  if (!state.connectionId) {
    const created = await connectionsApi.createConnection({
      provider: state.provider,
      schema_version: '1',
      auth_type: state.authType,
      name: state.name.trim(),
      endpoint: state.endpoint.trim(),
      network_policy_id: state.networkPolicyId.trim(),
      ...(hasHeaderOverrides ? { header_overrides: headerMap } : {})
    })
    if (isConnectionResource(created)) {
      Object.assign(state, hydrateWizardFromResource(state, created))
    } else {
      state.version = created.version
    }
    if (state.connectionId && pendingCredentialMode !== 'keep') {
      const credResult = await connectionsApi.changeCredential(
        state.connectionId,
        state.version,
        pendingCredentialPayload
      )
      state.version = credResult.version
      state.credentialVersion = credResult.credential?.version ?? state.credentialVersion
      state.credentialState = credResult.credential?.state ?? state.credentialState
      state.credentialLastFour = credResult.credential?.last_four ?? null
      state.credentialSecret = ''
      state.credentialRefreshToken = ''
      state.credentialMode = 'keep'
    }
    if (state.connectionId) {
      void loadConnectionEvents(state.connectionId)
    }
    return true
  }

  const patched = await connectionsApi.patchConnection(state.connectionId, state.version, {
    name: state.name.trim(),
    endpoint: state.endpoint.trim(),
    network_policy_id: state.networkPolicyId.trim(),
    ...(supportsHeaderOverrides.value ? { header_overrides: headerMap } : {}),
    model_ids: state.modelIds,
    pool_ids: state.poolIds,
    limits: { concurrency: state.concurrency }
  })
  state.version = patched.version

  if (pendingCredentialMode !== 'keep') {
    const credResult = await connectionsApi.changeCredential(
      state.connectionId,
      state.version,
      pendingCredentialPayload
    )
    state.version = credResult.version
    state.credentialVersion = credResult.credential?.version ?? state.credentialVersion
    state.credentialState = credResult.credential?.state ?? state.credentialState
    state.credentialLastFour = credResult.credential?.last_four ?? null
    state.credentialSecret = ''
    state.credentialRefreshToken = ''
    state.credentialMode = 'keep'
  }
  void loadConnectionEvents(state.connectionId)
  return true
}

async function handleSaveDraft() {
  if (busy.value) return
  busy.value = true
  pageAsyncState.value = 'background_refresh'
  try {
    const ok = await ensureConnectionPersisted()
    if (ok) {
      pageAsyncState.value = 'confirmed'
    }
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(
      err,
      isZh.value ? '保存连接草稿失败。' : 'Failed to save draft.',
      isZh.value
    )
    state.asyncError = mapped
    pageAsyncState.value = mapped.state
  } finally {
    busy.value = false
  }
}

async function handleEnrollOAuth() {
  if (busy.value || !oauthCode.value.trim()) return
  busy.value = true
  pageAsyncState.value = 'background_refresh'
  try {
    const ok = await ensureConnectionPersisted()
    if (!ok || !state.connectionId) return
    const result = await connectionsApi.attachAuthorization(state.connectionId, state.version, {
      credential_draft: oauthCode.value.trim(),
      revision: state.version
    })
    state.version = result.version
    state.credentialVersion = result.credential?.version ?? state.credentialVersion
    state.credentialState = result.credential?.state ?? state.credentialState
    state.credentialLastFour = result.credential?.last_four ?? null
    oauthCode.value = ''
    pageAsyncState.value = 'confirmed'
    void loadConnectionEvents(state.connectionId)
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(
      err,
      isZh.value ? '绑定 OAuth 授权失败。' : 'OAuth enrollment failed.',
      isZh.value
    )
    state.asyncError = mapped
    pageAsyncState.value = mapped.state
  } finally {
    busy.value = false
  }
}

async function handleRunVerification() {
  if (busy.value) return
  busy.value = true
  pageAsyncState.value = 'background_refresh'
  state.asyncError = null
  try {
    const ok = await ensureConnectionPersisted()
    if (!ok || !state.connectionId) return
    const initialOp = await connectionsApi.verifyConnection(state.connectionId, state.version, {
      checks: ['network', 'authorization', 'discover_models'],
      credential_version: state.credentialVersion
    })
    verifyOperation.value = initialOp
    const finalOp = await pollConnectionOperation(initialOp, {
      onUpdate: (op) => {
        verifyOperation.value = op
      }
    })
    const refreshed = await connectionsApi.getConnection(state.connectionId)
    Object.assign(state, hydrateWizardFromResource(state, refreshed))
    state.verificationStale = !isVerificationFresh(state.verification)
    if (!modelIdsText.value.trim() && refreshed.last_verification?.models?.length) {
      modelIdsText.value = refreshed.last_verification.models.join(', ')
    }
    void loadConnectionEvents(state.connectionId)

    if (finalOp.state === 'failed') {
      state.asyncError = {
        state: 'partial_failure',
        status: 422,
        message:
          typeof finalOp.error?.message === 'string'
            ? finalOp.error.message
            : isZh.value
              ? '上游连通性或鉴权探测未通过，请查看下方检查项详情。'
              : 'Verification probe failed. Review check details below.',
        requestId: finalOp.operation_id
      }
      pageAsyncState.value = 'partial_failure'
    } else {
      pageAsyncState.value = 'confirmed'
    }
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(
      err,
      isZh.value ? '发起连通性验证失败。' : 'Verification request failed.',
      isZh.value
    )
    state.asyncError = mapped
    pageAsyncState.value = mapped.state
  } finally {
    busy.value = false
  }
}

async function handleRunPreview() {
  if (busy.value) return
  busy.value = true
  pageAsyncState.value = 'background_refresh'
  state.asyncError = null
  try {
    state.modelIds = modelIdsText.value
      .split(/[,\n]/)
      .map((item) => item.trim())
      .filter((item) => item.length > 0)
    const ok = await ensureConnectionPersisted()
    if (!ok || !state.connectionId) return
    const preview = await connectionsApi.previewConnection(state.connectionId, state.version, {
      model_ids: state.modelIds,
      pool_ids: state.poolIds,
      limits: { concurrency: state.concurrency }
    })
    state.preview = preview
    state.previewStale = false
    pageAsyncState.value = 'confirmed'
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(
      err,
      isZh.value ? '生成分配预览失败。' : 'Preview generation failed.',
      isZh.value
    )
    state.asyncError = mapped
    pageAsyncState.value = mapped.state
  } finally {
    busy.value = false
  }
}

async function handleCreateInlinePool() {
  if (!newPoolName.value.trim() || creatingPool.value) return
  creatingPool.value = true
  try {
    const pool = await connectionsApi.createPool({
      name: newPoolName.value.trim(),
      max_concurrency: Math.max(1, state.concurrency)
    })
    pools.value = [...pools.value, pool]
    if (!state.poolIds.includes(pool.id)) {
      state.poolIds = [...state.poolIds, pool.id]
      Object.assign(state, invalidatePreview(state))
    }
    newPoolName.value = ''
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(
      err,
      isZh.value ? '创建出站资源池失败。' : 'Failed to create pool.',
      isZh.value
    )
    state.asyncError = mapped
    pageAsyncState.value = mapped.state
  } finally {
    creatingPool.value = false
  }
}

async function handleActivate() {
  if (busy.value || !state.connectionId) return
  busy.value = true
  pageAsyncState.value = 'background_refresh'
  state.asyncError = null
  try {
    const result = await connectionsApi.activateConnection(state.connectionId, state.version, {
      verification_id: state.verification?.verification_id ?? '',
      preview_id: state.preview?.preview_id ?? '',
      expected_credential_version: state.credentialVersion
    })
    state.version = result.version
    state.confirmedVersion = result.version
    state.connectionState = 'active'
    activatedInSession.value = true
    pageAsyncState.value = 'confirmed'
    void loadConnectionEvents(state.connectionId)
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(
      err,
      isZh.value ? '启用上游连接失败。' : 'Activation failed.',
      isZh.value
    )
    state.asyncError = mapped
    pageAsyncState.value = mapped.state
  } finally {
    busy.value = false
  }
}

function goNext() {
  if (state.currentStep === 'provider') {
    normalizeStepOneInputs()
  }
  stepAttempted.value = true
  const check = canProceedFromStep(state, state.currentStep, selectedSchema.value, Date.now(), isZh.value)
  if (!check.valid) return
  const idx = WIZARD_STEP_ORDER.indexOf(state.currentStep)
  if (idx < WIZARD_STEP_ORDER.length - 1) {
    state.currentStep = WIZARD_STEP_ORDER[idx + 1]
    stepAttempted.value = false
    pageAsyncState.value = null
    state.asyncError = null
  }
}

function goBack() {
  const idx = WIZARD_STEP_ORDER.indexOf(state.currentStep)
  if (idx > 0) {
    state.currentStep = WIZARD_STEP_ORDER[idx - 1]
    stepAttempted.value = false
    pageAsyncState.value = null
    state.asyncError = null
  }
}

function handleSelectStep(targetStep: WizardStepId) {
  const currentIdx = WIZARD_STEP_ORDER.indexOf(state.currentStep)
  const targetIdx = WIZARD_STEP_ORDER.indexOf(targetStep)
  if (targetIdx <= currentIdx) {
    state.currentStep = targetStep
    stepAttempted.value = false
    pageAsyncState.value = null
    state.asyncError = null
    return
  }
  if (state.currentStep === 'provider') {
    normalizeStepOneInputs()
  }
  for (let i = currentIdx; i < targetIdx; i += 1) {
    const stepId = WIZARD_STEP_ORDER[i]
    const check = canProceedFromStep(state, stepId, selectedSchema.value, Date.now(), isZh.value)
    if (!check.valid) {
      state.currentStep = stepId
      stepAttempted.value = true
      state.asyncError = {
        state: 'partial_failure',
        status: 422,
        message: check.reason || (isZh.value ? '请先完成当前步骤必填项。' : 'Complete the current step before advancing.')
      }
      pageAsyncState.value = 'partial_failure'
      return
    }
  }
  state.currentStep = targetStep
  stepAttempted.value = false
  pageAsyncState.value = null
  state.asyncError = null
}

function togglePool(poolId: string) {
  if (state.poolIds.includes(poolId)) {
    state.poolIds = state.poolIds.filter((id) => id !== poolId)
  } else {
    state.poolIds = [...state.poolIds, poolId]
  }
  Object.assign(state, invalidatePreview(state))
}

function formatCredentialState(credState: string): string {
  if (!isZh.value) return credState
  if (credState === 'verified') return '已验证'
  if (credState === 'unverified') return '待验证'
  if (credState === 'revoked') return '已吊销'
  return '未设置'
}

function formatCheckName(name: string): string {
  if (name === 'network') return isZh.value ? '网络与 TLS 连通性探测' : 'Network & TLS Reachability'
  if (name === 'authorization') return isZh.value ? '认证凭据鉴权检查' : 'Credential Authorization'
  if (name === 'discover_models') return isZh.value ? '上游可用模型发现' : 'Upstream Model Discovery'
  return name
}

function formatCheckState(checkState: string): string {
  if (!isZh.value) return checkState
  if (checkState === 'passed') return '通过'
  if (checkState === 'failed') return '未通过'
  if (checkState === 'unsupported') return '不支持'
  return '待确认'
}

function formatBlockedReason(reason: string): string {
  const mapZh: Record<string, string> = {
    MODELS_REQUIRED: '请至少填写一个已通过探测的模型 ID',
    INCOMPATIBLE_PROVIDER: '所填模型不在本次探测发现的可用模型列表中，请核对模型名称',
    VERIFICATION_REQUIRED: '请先在上一歩完成连通性验证',
    CONNECTION_INACTIVE: '当前连接尚未处于启用状态'
  }
  const mapEn: Record<string, string> = {
    MODELS_REQUIRED: 'Specify at least one verified model ID',
    INCOMPATIBLE_PROVIDER: 'One or more model IDs were not returned by upstream discovery',
    VERIFICATION_REQUIRED: 'Complete connection verification first',
    CONNECTION_INACTIVE: 'Connection is currently inactive'
  }
  return (isZh.value ? mapZh[reason] : mapEn[reason]) || reason
}

function formatEventType(type: string): string {
  const labelsZh: Record<string, string> = {
    'connection.created': '创建连接草稿',
    'connection.updated': '更新连接配置',
    'connection.verified': '连通性验证通过',
    'connection.verification_failed': '连通性验证未通过',
    'connection.activated': '正式启用上游连接',
    'credential.updated': '更新认证凭据',
    'credential.cleared': '清空认证凭据',
    'rotation.validating': '轮换凭据：探测新密钥',
    'rotation.switching': '轮换凭据：切换生效',
    'rotation.draining': '轮换凭据：平滑排空',
    'rotation.revoked': '轮换凭据：旧密钥退役',
    'rotation.failed': '轮换凭据：探测未通过'
  }
  const labelsEn: Record<string, string> = {
    'connection.created': 'Draft created',
    'connection.updated': 'Configuration updated',
    'connection.verified': 'Verification passed',
    'connection.verification_failed': 'Verification failed',
    'connection.activated': 'Connection activated',
    'credential.updated': 'Credential updated',
    'credential.cleared': 'Credential cleared',
    'rotation.validating': 'Rotation: validating replacement',
    'rotation.switching': 'Rotation: switching secret',
    'rotation.draining': 'Rotation: draining sessions',
    'rotation.revoked': 'Rotation: previous secret retired',
    'rotation.failed': 'Rotation: validation failed'
  }
  return (isZh.value ? labelsZh[type] : labelsEn[type]) || type
}
</script>

<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-6 px-4 py-6 sm:px-6">
      <PageHeader
        :title="wizardPageTitle"
        :subtitle="
          isZh
            ? '按协议引导完成端点配置、密钥托管、连通性探测、资源池绑定与上线启用'
            : '5-step guided upstream provisioning, live verification, pool binding, and activation'
        "
        :breadcrumbs="wizardBreadcrumbs"
      >
        <template #actions>
          <Button variant="ghost" size="sm" @click="router.push('/admin/accounts')">
            {{ isZh ? '返回上游连接' : 'Back to Connections' }}
          </Button>
          <Button variant="secondary" size="sm" :loading="busy" @click="handleSaveDraft">
            {{ isZh ? '保存草稿' : 'Save Draft' }}
          </Button>
        </template>
      </PageHeader>

      <AsyncStateBanner
        v-if="pageAsyncState"
        :state="pageAsyncState"
        :error="state.asyncError"
        :confirmed-version="state.confirmedVersion ?? state.version"
        :confirmed-label="isZh ? '连接状态已同步' : 'Connection state synchronized'"
        :empty-title="isZh ? '暂无可用的提供商协议模板' : 'No provider schemas available'"
        :empty-description="
          isZh ? '当前工作空间尚未返回可用的上游协议定义。' : 'No upstream provider schemas are currently available.'
        "
        @retry="loadInitialData"
        @reload-latest="loadInitialData"
      />

      <div class="lg:hidden">
        <Stepper
          :steps="stepperItems"
          :current-step-id="state.currentStep"
          orientation="horizontal"
          @update:current-step-id="handleSelectStep($event as WizardStepId)"
        />
      </div>

      <div class="grid grid-cols-1 gap-6 lg:grid-cols-[240px_1fr]">
        <aside class="hidden lg:block space-y-4">
          <Card padding="md">
            <Stepper
              :steps="stepperItems"
              :current-step-id="state.currentStep"
              orientation="vertical"
              @update:current-step-id="handleSelectStep($event as WizardStepId)"
            />
          </Card>
        </aside>

        <main class="space-y-4">
          <Card v-if="state.currentStep === 'provider'" padding="lg">
            <div class="space-y-5">
              <div class="flex flex-wrap items-center justify-between gap-3">
                <div>
                  <h2 class="text-base font-semibold text-oh-fg">
                    {{ isZh ? '1. 选择协议与服务地址' : '1. Provider & Endpoint' }}
                  </h2>
                  <p class="text-xs text-oh-muted">
                    {{
                      isZh
                        ? '选择上游提供商协议，配置基础服务地址、出站代理与可选请求头覆写。'
                        : 'Choose an upstream provider protocol, base endpoint URL, egress proxy, and optional headers.'
                    }}
                  </p>
                </div>
                <div class="w-full sm:w-64">
                  <Input
                    v-model="schemaSearch"
                    :placeholder="isZh ? '搜索提供商或认证方式…' : 'Search provider or auth…'"
                    size="sm"
                  />
                </div>
              </div>

              <div class="grid grid-cols-1 gap-2.5 sm:grid-cols-2 lg:grid-cols-3">
                <button
                  v-for="schema in filteredSchemas"
                  :key="schema.provider"
                  type="button"
                  class="rounded-oh-md border p-3 text-left transition-colors"
                  :class="
                    state.provider === schema.provider
                      ? 'border-oh-accent bg-oh-accent/10'
                      : 'border-oh-default bg-oh-surface hover:border-oh-strong'
                  "
                  @click="selectProviderSchema(schema)"
                >
                  <div class="flex items-center justify-between gap-2">
                    <span class="text-sm font-semibold text-oh-fg">
                      {{ getProviderDisplayMeta(schema.provider).label }}
                    </span>
                    <Badge tone="neutral" variant="outline">
                      {{ getAuthModeLabel(schema.auth_types[0] ?? 'api_key', isZh) }}
                    </Badge>
                  </div>
                  <p class="mt-1 text-xs text-oh-muted">
                    {{ getProviderDisplayMeta(schema.provider).protocolSummary }}
                  </p>
                </button>
              </div>

              <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <Input
                  v-model="state.name"
                  :label="isZh ? '连接名称' : 'Connection Name'"
                  :placeholder="isZh ? '例如：OpenAI 主力连接' : 'e.g. OpenAI Primary Connection'"
                  @blur="state.name = state.name.trim()"
                />
                <Select
                  v-model="state.authType"
                  :label="isZh ? '认证方式' : 'Authentication Mode'"
                  :options="authModeOptions"
                />
                <div class="space-y-1">
                  <Input
                    :model-value="state.endpoint"
                    :label="isZh ? '上游服务地址 (Endpoint URL)' : 'Upstream Endpoint URL'"
                    mono
                    :placeholder="
                      isZh
                        ? '例如 https://api.openai.com 或 http://127.0.0.1:3425'
                        : 'e.g. https://api.openai.com or http://127.0.0.1:3425'
                    "
                    @update:model-value="handleEndpointChange"
                    @blur="normalizeStepOneInputs"
                  />
                  <p v-if="endpointInlineError" class="text-xs text-oh-danger">
                    {{ endpointInlineError }}
                  </p>
                  <p v-else-if="endpointNormalizationHint" class="text-xs text-oh-accent">
                    {{ endpointNormalizationHint }}
                  </p>
                  <p v-else class="text-[11px] text-oh-muted">
                    {{
                      isZh
                        ? '填写 http:// 或 https:// 基础地址（无需拼接 /v1/chat/completions），留空则使用官方默认地址。'
                        : 'Enter http:// or https:// base URL (without /v1/chat/completions), or leave blank for official default.'
                    }}
                  </p>
                </div>
                <Select
                  v-model="state.networkPolicyId"
                  :label="isZh ? '出站代理策略（可选）' : 'Egress Proxy Policy (Optional)'"
                  :options="proxyOptions"
                />
              </div>

              <div class="rounded-oh-md border border-oh-default bg-oh-subtle/30 p-4 space-y-3">
                <div class="flex flex-wrap items-start justify-between gap-2">
                  <div>
                    <h3 class="text-sm font-semibold text-oh-fg">
                      {{ isZh ? '自定义出站请求头 (header_overrides，可选)' : 'Outbound Custom Headers (header_overrides, optional)' }}
                    </h3>
                    <p class="text-xs text-oh-muted">
                      {{
                        supportsHeaderOverrides
                          ? isZh
                            ? '转发请求至上游前按名称覆盖对应 HTTP Header；适合自建网关、WAF 准入校验或兼容特定客户端标识。'
                            : 'Override outbound HTTP headers sent to this upstream; useful for WAF gates or custom client identifiers.'
                          : isZh
                            ? '当前所选提供商或认证方式暂不支持自定义请求头覆写（切换至 OpenAI / Anthropic / Kimi / DeepSeek / Zhipu 等 API Key 模式后可用）。'
                            : 'Custom header overrides are available on API Key modes for OpenAI, Anthropic, Kimi, DeepSeek, Zhipu, Grok, etc.'
                      }}
                    </p>
                  </div>
                  <div v-if="supportsHeaderOverrides" class="flex flex-wrap items-center gap-2">
                    <Button
                      size="xs"
                      variant="secondary"
                      type="button"
                      @click="applyCompatibleUaPreset"
                    >
                      {{ isZh ? '标准兼容客户端 UA (curl/8.7.1)' : 'Compatible Client UA (curl/8.7.1)' }}
                    </Button>
                    <Button
                      size="xs"
                      variant="ghost"
                      type="button"
                      @click="addHeaderOverrideRow()"
                    >
                      {{ isZh ? '+ 添加请求头' : '+ Add Header' }}
                    </Button>
                  </div>
                </div>

                <div v-if="state.headerOverrides.length > 0" class="space-y-2">
                  <div
                    v-for="(row, idx) in state.headerOverrides"
                    :key="idx"
                    class="grid grid-cols-1 gap-2 sm:grid-cols-[1fr_1.3fr_auto] items-center"
                  >
                    <Input
                      v-model="row.key"
                      size="sm"
                      mono
                      :disabled="!supportsHeaderOverrides"
                      :placeholder="isZh ? 'Header 名称，如 User-Agent 或 X-Custom-Gate' : 'Header name, e.g. User-Agent'"
                      @update:model-value="Object.assign(state, invalidateVerification(state))"
                    />
                    <Input
                      v-model="row.value"
                      size="sm"
                      mono
                      :disabled="!supportsHeaderOverrides"
                      :placeholder="isZh ? 'Header 值，如 curl/8.7.1' : 'Header value, e.g. curl/8.7.1'"
                      @update:model-value="Object.assign(state, invalidateVerification(state))"
                    />
                    <Button
                      size="xs"
                      variant="ghost"
                      type="button"
                      @click="removeHeaderOverrideRow(idx)"
                    >
                      {{ isZh ? '移除' : 'Remove' }}
                    </Button>
                  </div>
                </div>
              </div>
            </div>
          </Card>

          <Card v-else-if="state.currentStep === 'credentials'" padding="lg">
            <div class="space-y-5">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <h2 class="text-base font-semibold text-oh-fg">
                    {{ isZh ? '2. 认证凭据与授权' : '2. Credentials & Authorization' }}
                  </h2>
                  <p class="text-xs text-oh-muted">
                    {{
                      isZh
                        ? '支持显式保留、替换或清空凭据；已加密存储的密钥绝不回显明文。'
                        : 'Explicit keep, replace, or clear semantics. Encrypted secrets are never echoed.'
                    }}
                  </p>
                </div>
                <Badge
                  :tone="state.credentialState === 'verified' ? 'success' : 'warning'"
                  variant="subtle"
                >
                  {{ formatCredentialState(state.credentialState) }}
                  <span v-if="state.credentialLastFour" class="ml-1 font-mono">
                    ••••{{ state.credentialLastFour }}
                  </span>
                </Badge>
              </div>

              <SegmentedControl
                :model-value="state.credentialMode"
                :options="credentialModeOptions"
                full-width
                @update:model-value="handleCredentialModeChange"
              />

              <div v-if="state.credentialMode === 'set'" class="space-y-3">
                <Input
                  v-model="state.credentialSecret"
                  type="password"
                  :label="
                    isZh
                      ? `${getAuthModeLabel(state.authType, true)} 密钥`
                      : `${getAuthModeLabel(state.authType, false)} Secret`
                  "
                  :placeholder="isZh ? '粘贴上游 API Key 或访问令牌' : 'Paste upstream API key or token'"
                  autocomplete="new-password"
                />
                <Input
                  v-if="supportsOAuth"
                  v-model="state.credentialRefreshToken"
                  type="password"
                  :label="isZh ? '刷新令牌 Refresh Token（可选）' : 'Refresh Token (optional)'"
                  :placeholder="isZh ? '粘贴 OAuth Refresh Token（可选）' : 'OAuth refresh token'"
                  autocomplete="new-password"
                />
              </div>

              <div
                v-if="supportsOAuth"
                class="rounded-oh-md border border-oh-default bg-oh-subtle/40 p-4 space-y-3"
              >
                <div class="flex items-center justify-between">
                  <span class="text-sm font-medium text-oh-fg">
                    {{ isZh ? 'OAuth 授权草稿绑定' : 'OAuth Authorization Enrollment' }}
                  </span>
                  <Badge tone="accent" variant="subtle">OAuth 2.0</Badge>
                </div>
                <div class="flex flex-col gap-2 sm:flex-row">
                  <Input
                    v-model="oauthCode"
                    :placeholder="isZh ? '粘贴授权会话凭据引用码' : 'Paste authorization draft reference'"
                    class="flex-1"
                  />
                  <Button
                    variant="secondary"
                    size="sm"
                    :loading="busy"
                    :disabled="!oauthCode.trim()"
                    @click="handleEnrollOAuth"
                  >
                    {{ isZh ? '绑定授权凭据' : 'Attach Authorization' }}
                  </Button>
                </div>
              </div>
            </div>
          </Card>

          <Card v-else-if="state.currentStep === 'verify'" padding="lg">
            <div class="space-y-5">
              <div class="flex flex-wrap items-center justify-between gap-3">
                <div>
                  <h2 class="text-base font-semibold text-oh-fg">
                    {{ isZh ? '3. 连通性与凭据验证' : '3. Connection & Auth Verification' }}
                  </h2>
                  <p class="text-xs text-oh-muted">
                    {{
                      isZh
                        ? '由服务端真实发起网络连通性、TLS 握手、凭据鉴权与上游可用模型探测。'
                        : 'Server-side network reachability, TLS handshake, credential auth, and model discovery probe.'
                    }}
                  </p>
                </div>
                <Button
                  variant="primary"
                  size="sm"
                  :loading="busy"
                  @click="handleRunVerification"
                >
                  {{
                    state.verification
                      ? isZh
                        ? '重新执行验证探测'
                        : 'Re-run Verification'
                      : isZh
                        ? '运行验证探针'
                        : 'Run Verification Probe'
                  }}
                </Button>
              </div>

              <div
                v-if="state.verificationStale"
                class="rounded-oh-md border border-oh-warning bg-oh-surface px-3 py-2 text-xs text-oh-fg"
              >
                {{
                  isZh
                    ? '由于服务地址、请求头或凭据已变更，上次探测结果已过期，请重新执行验证后再进入下一步。'
                    : 'Verification is stale because endpoint, header, or credential settings changed. Re-run verification before previewing.'
                }}
              </div>

              <div
                v-if="verifyOperation"
                class="flex items-center justify-between rounded-oh-md border border-oh-default bg-oh-subtle/40 px-3 py-2 text-xs"
              >
                <span class="text-oh-muted">
                  {{ isZh ? '探测任务状态' : 'Verification Task Status' }}
                </span>
                <Badge tone="accent" variant="subtle">{{ verifyOperation.state }}</Badge>
              </div>

              <div v-if="state.verification" class="divide-y divide-oh-default rounded-oh-md border border-oh-default">
                <div
                  v-for="check in state.verification.checks"
                  :key="check.name"
                  class="flex flex-wrap items-center justify-between gap-2 px-3.5 py-2.5 text-sm"
                >
                  <div>
                    <div class="font-medium text-oh-fg">{{ formatCheckName(check.name) }}</div>
                    <div v-if="check.reason" class="text-xs text-oh-danger">{{ check.reason }}</div>
                  </div>
                  <div class="flex items-center gap-3">
                    <Badge
                      :tone="check.state === 'passed' ? 'success' : check.state === 'failed' ? 'danger' : 'neutral'"
                      variant="subtle"
                    >
                      {{ formatCheckState(check.state) }}
                    </Badge>
                  </div>
                </div>
              </div>

              <div
                v-if="discoveredModels.length > 0"
                class="rounded-oh-md border border-oh-default bg-oh-subtle/30 p-3.5 space-y-2"
              >
                <div class="text-xs font-medium text-oh-fg">
                  {{
                    isZh
                      ? `已探测发现 ${discoveredModels.length} 个上游模型：`
                      : `Discovered ${discoveredModels.length} upstream models:`
                  }}
                </div>
                <div class="flex flex-wrap gap-1.5 max-h-32 overflow-y-auto">
                  <Badge
                    v-for="m in discoveredModels"
                    :key="m"
                    tone="neutral"
                    variant="outline"
                    mono
                  >
                    {{ m }}
                  </Badge>
                </div>
              </div>
            </div>
          </Card>

          <Card v-else-if="state.currentStep === 'preview'" padding="lg">
            <div class="space-y-5">
              <div class="flex flex-wrap items-center justify-between gap-3">
                <div>
                  <h2 class="text-base font-semibold text-oh-fg">
                    {{ isZh ? '4. 资源池分配与变更预览' : '4. Pool Assignment & Preview' }}
                  </h2>
                  <p class="text-xs text-oh-muted">
                    {{
                      isZh
                        ? '选择暴露的模型 ID、绑定出站资源池与并发上限，并生成生效预览。'
                        : 'Select exposed model IDs, assign resource pools and concurrency cap, then generate a preview.'
                    }}
                  </p>
                </div>
                <Button
                  variant="secondary"
                  size="sm"
                  :loading="busy"
                  @click="handleRunPreview"
                >
                  {{ isZh ? '生成变更预览' : 'Generate Preview' }}
                </Button>
              </div>

              <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <Input
                  v-model="modelIdsText"
                  :label="isZh ? '允许调度的模型 ID（逗号分隔）' : 'Exposed Model IDs (comma-separated)'"
                  mono
                  placeholder="gpt-4o, gpt-4.1-mini"
                  @update:model-value="Object.assign(state, invalidatePreview(state))"
                />
                <Input
                  :model-value="String(state.concurrency)"
                  :label="isZh ? '单连接并发上限' : 'Concurrency Cap'"
                  type="number"
                  mono
                  @update:model-value="
                    state.concurrency = Math.max(1, Number($event) || 1);
                    Object.assign(state, invalidatePreview(state))
                  "
                />
              </div>

              <div v-if="discoveredModels.length > 0" class="space-y-1.5">
                <div class="text-xs text-oh-muted">
                  {{ isZh ? '点击快速加入已探测模型：' : 'Click to add discovered models:' }}
                </div>
                <div class="flex flex-wrap gap-1.5 max-h-28 overflow-y-auto">
                  <button
                    v-for="m in discoveredModels"
                    :key="m"
                    type="button"
                    class="rounded-oh-sm border border-oh-default bg-oh-surface px-2 py-0.5 font-mono text-xs text-oh-fg hover:border-oh-accent"
                    @click="addDiscoveredModel(m)"
                  >
                    + {{ m }}
                  </button>
                </div>
              </div>

              <div class="space-y-2">
                <div class="flex flex-wrap items-center justify-between gap-2">
                  <span class="text-xs font-medium text-oh-fg">
                    {{ isZh ? '出站资源池（可选）' : 'Egress Resource Pools (Optional)' }}
                  </span>
                  <div class="flex items-center gap-2">
                    <Input
                      v-model="newPoolName"
                      size="sm"
                      :placeholder="isZh ? '输入新资源池名称' : 'New pool name'"
                    />
                    <Button
                      size="xs"
                      variant="ghost"
                      :loading="creatingPool"
                      :disabled="!newPoolName.trim()"
                      @click="handleCreateInlinePool"
                    >
                      {{ isZh ? '+ 新建资源池' : '+ Create Pool' }}
                    </Button>
                  </div>
                </div>
                <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
                  <div
                    v-for="pool in pools"
                    :key="pool.id"
                    class="flex items-center justify-between rounded-oh-md border border-oh-default px-3 py-2 text-xs"
                  >
                    <Checkbox
                      :model-value="state.poolIds.includes(pool.id)"
                      :label="pool.name"
                      @update:model-value="togglePool(pool.id)"
                    />
                    <span class="font-mono tabular-nums text-oh-muted">#{{ pool.id }}</span>
                  </div>
                </div>
              </div>

              <div
                v-if="state.preview"
                class="rounded-oh-md border border-oh-default bg-oh-subtle/40 p-3 space-y-2 text-xs"
              >
                <div class="flex items-center justify-between">
                  <span class="font-medium text-oh-fg">
                    {{ isZh ? '变更预览已就绪' : 'Preview Ready' }}
                  </span>
                  <span class="font-mono tabular-nums text-oh-muted">
                    {{ isZh ? '有效期至' : 'Expires' }} {{ new Date(state.preview.expires_at).toLocaleTimeString() }}
                  </span>
                </div>
                <div
                  v-if="state.preview.blocked.length > 0"
                  class="rounded-oh-sm border border-oh-status-danger-border bg-oh-surface px-2.5 py-2 text-oh-danger space-y-1"
                >
                  <div v-for="reason in state.preview.blocked" :key="reason">
                    {{ formatBlockedReason(reason) }}
                  </div>
                </div>
                <div v-for="item in state.preview.items" :key="item.id" class="flex items-center justify-between">
                  <span class="font-mono tabular-nums">
                    #{{ item.id }} · {{ isZh ? `配置修订 #${item.version ?? item.base_version}` : `Revision #${item.version ?? item.base_version}` }}
                  </span>
                  <Badge
                    :tone="item.state === 'applicable' ? 'success' : 'danger'"
                    variant="subtle"
                  >
                    {{
                      item.state === 'applicable'
                        ? isZh
                          ? '可启用'
                          : 'Applicable'
                        : formatBlockedReason(item.reason || item.state)
                    }}
                  </Badge>
                </div>
              </div>
            </div>
          </Card>

          <Card v-else-if="state.currentStep === 'activate'" padding="lg">
            <div class="space-y-5">
              <div>
                <h2 class="text-base font-semibold text-oh-fg">
                  {{ isZh ? '5. 核对修订并启用连接' : '5. Confirm Revision & Activate' }}
                </h2>
                <p class="text-xs text-oh-muted">
                  {{
                    isZh
                      ? '确认已通过探测验证的配置修订，正式启用此上游连接参与网关流量调度。'
                      : 'Review the verified configuration revision and enable this connection for traffic scheduling.'
                  }}
                </p>
              </div>

              <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 rounded-oh-md border border-oh-default bg-oh-subtle/40 p-4 text-xs">
                <div>
                  <span class="text-oh-muted">{{ isZh ? '提供商与认证方式' : 'Provider & Auth' }}</span>
                  <div class="mt-0.5 font-medium text-oh-fg">
                    {{ getProviderDisplayMeta(state.provider).label }} · {{ getAuthModeLabel(state.authType, isZh) }}
                  </div>
                </div>
                <div>
                  <span class="text-oh-muted">{{ isZh ? '配置修订' : 'Revision' }}</span>
                  <div class="mt-0.5 font-mono tabular-nums text-oh-fg">#{{ state.version }}</div>
                </div>
                <div>
                  <span class="text-oh-muted">{{ isZh ? '上游服务地址' : 'Endpoint' }}</span>
                  <div class="mt-0.5 font-mono text-oh-fg truncate">
                    {{ state.endpoint || (isZh ? '官方默认地址' : 'Official default') }}
                  </div>
                </div>
                <div>
                  <span class="text-oh-muted">{{ isZh ? '资源池与并发上限' : 'Pools & Concurrency' }}</span>
                  <div class="mt-0.5 font-mono tabular-nums text-oh-fg">
                    {{
                      isZh
                        ? `${state.poolIds.length} 个资源池 · ${state.concurrency} 并发`
                        : `${state.poolIds.length} pools · ${state.concurrency} concurrent`
                    }}
                  </div>
                </div>
              </div>

              <div class="flex flex-wrap items-center justify-end gap-2">
                <Button
                  variant="secondary"
                  size="sm"
                  :loading="busy"
                  @click="handleSaveDraft"
                >
                  {{ isZh ? '仅保存为草稿' : 'Save as Draft' }}
                </Button>
                <Button
                  variant="primary"
                  size="sm"
                  :loading="busy"
                  :disabled="Boolean(state.confirmedVersion)"
                  @click="handleActivate"
                >
                  {{
                    state.confirmedVersion
                      ? isZh
                        ? `已启用（配置修订 #${state.confirmedVersion}）`
                        : `Activated (Revision #${state.confirmedVersion})`
                      : isZh
                        ? '正式启用连接'
                        : 'Activate Connection'
                  }}
                </Button>
              </div>

              <div
                v-if="state.confirmedVersion || activatedInSession || state.connectionState === 'active'"
                class="rounded-oh-lg border border-oh-accent bg-oh-accent/5 p-4 space-y-3"
              >
                <div class="flex flex-wrap items-center justify-between gap-2">
                  <div>
                    <h3 class="text-sm font-semibold text-oh-fg">
                      {{ isZh ? '下一步发布指引' : 'Next Steps to Publish Models' }}
                    </h3>
                    <p class="text-xs text-oh-muted">
                      {{
                        isZh
                          ? '上游连接启用后不会自动向终端用户公开模型。请按顺序完成以下三步，使模型在模型广场与客户端调用中可见：'
                          : 'Activated upstream connections are not exposed to end users until grouped and priced. Complete these three steps to publish models:'
                      }}
                    </p>
                  </div>
                  <Badge tone="success" variant="subtle">
                    {{ isZh ? '连接已就绪' : 'Connection Active' }}
                  </Badge>
                </div>

                <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
                  <button
                    type="button"
                    class="rounded-oh-md border border-oh-default bg-oh-surface p-3 text-left transition-colors hover:border-oh-accent"
                    @click="router.push('/admin/groups')"
                  >
                    <div class="text-xs font-semibold text-oh-fg">
                      {{ isZh ? '1. 绑定分组' : '1. Bind Group' }}
                    </div>
                    <p class="mt-1 text-[11px] text-oh-muted">
                      {{
                        isZh
                          ? '将该上游账号加入目标调度分组，并授权给 API Key 或订阅套餐。'
                          : 'Add this upstream account into a routing group accessible to keys or plans.'
                      }}
                    </p>
                  </button>
                  <button
                    type="button"
                    class="rounded-oh-md border border-oh-default bg-oh-surface p-3 text-left transition-colors hover:border-oh-accent"
                    @click="router.push('/admin/channels/pricing')"
                  >
                    <div class="text-xs font-semibold text-oh-fg">
                      {{ isZh ? '2. 配置渠道与定价' : '2. Configure Channel & Pricing' }}
                    </div>
                    <p class="mt-1 text-[11px] text-oh-muted">
                      {{
                        isZh
                          ? '在渠道目录中关联分组、设置模型映射与计费倍率并发布模型目录。'
                          : 'Map models, configure token/request pricing, and publish catalog entries.'
                      }}
                    </p>
                  </button>
                  <button
                    type="button"
                    class="rounded-oh-md border border-oh-default bg-oh-surface p-3 text-left transition-colors hover:border-oh-accent"
                    @click="router.push('/model-plaza')"
                  >
                    <div class="text-xs font-semibold text-oh-fg">
                      {{ isZh ? '3. 模型广场可见确认' : '3. Verify in Model Plaza' }}
                    </div>
                    <p class="mt-1 text-[11px] text-oh-muted">
                      {{
                        isZh
                          ? '前往模型广场确认新发布的模型卡片、协议标签与定价展示无误。'
                          : 'Confirm published model cards, protocol badges, and pricing in Model Plaza.'
                      }}
                    </p>
                  </button>
                </div>
              </div>
            </div>
          </Card>

          <div class="flex items-center justify-between pt-2">
            <Button
              variant="ghost"
              size="sm"
              :disabled="state.currentStep === 'provider'"
              @click="goBack"
            >
              {{ isZh ? '上一步' : 'Previous' }}
            </Button>
            <div class="flex items-center gap-3">
              <span v-if="!stepValidation.valid" class="text-xs text-oh-warning">
                {{ stepValidation.reason }}
              </span>
              <Button
                v-if="state.currentStep !== 'activate'"
                variant="primary"
                size="sm"
                :disabled="!stepValidation.valid"
                @click="goNext"
              >
                {{ isZh ? '下一步' : 'Continue' }}
              </Button>
            </div>
          </div>

          <Card v-if="state.connectionId" padding="md">
            <div class="space-y-3">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <button
                  type="button"
                  class="flex items-center gap-2 text-left text-sm font-semibold text-oh-fg"
                  @click="eventsExpanded = !eventsExpanded"
                >
                  <span>
                    {{ isZh ? '连接变更与探测事件' : 'Connection Change & Probe Events' }}
                  </span>
                  <Badge tone="neutral" variant="subtle" mono>
                    {{ events.length }}
                  </Badge>
                </button>
                <div class="flex items-center gap-2">
                  <Button
                    size="xs"
                    variant="ghost"
                    :loading="eventsLoading"
                    @click="loadConnectionEvents(state.connectionId)"
                  >
                    {{ isZh ? '刷新事件' : 'Refresh Events' }}
                  </Button>
                  <Button
                    size="xs"
                    variant="ghost"
                    @click="eventsExpanded = !eventsExpanded"
                  >
                    {{ eventsExpanded ? (isZh ? '收起' : 'Collapse') : (isZh ? '展开' : 'Expand') }}
                  </Button>
                </div>
              </div>

              <div v-if="eventsExpanded">
                <p v-if="events.length === 0 && !eventsLoading" class="text-xs text-oh-muted">
                  {{ isZh ? '暂无连接变更或探测记录。' : 'No connection events recorded yet.' }}
                </p>
                <div
                  v-else
                  class="max-h-52 overflow-y-auto divide-y divide-oh-default rounded-oh-md border border-oh-default text-xs"
                >
                  <div
                    v-for="ev in events"
                    :key="ev.id"
                    class="flex flex-wrap items-center justify-between gap-2 px-3 py-2"
                  >
                    <div class="flex items-center gap-2.5 min-w-0">
                      <span class="font-medium text-oh-fg">{{ formatEventType(ev.type) }}</span>
                      <span v-if="ev.actor_id" class="text-oh-muted">
                        {{ isZh ? `操作人 #${ev.actor_id}` : `Actor #${ev.actor_id}` }}
                      </span>
                    </div>
                    <div class="flex items-center gap-2 text-oh-muted font-mono tabular-nums">
                      <span>{{ isZh ? `修订 #${ev.version}` : `Rev #${ev.version}` }}</span>
                      <span>·</span>
                      <span>{{ new Date(ev.at).toLocaleString() }}</span>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </Card>
        </main>
      </div>
    </div>
  </AppLayout>
</template>
