<script setup lang="ts">
import { computed, getCurrentInstance, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import CopyButton from '@/components/ui/CopyButton.vue'
import SegmentedControl from '@/components/ui/SegmentedControl.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import Tabs from '@/components/ui/Tabs.vue'
import { classifyConsoleError, type ConsoleAsyncNotice } from '@/features/console/asyncState'
import { catalogApi, type CatalogModelItem } from '@/features/console/catalogApi'
import ConsoleAsyncBanner from '@/features/console/components/ConsoleAsyncBanner.vue'
import ConsoleScopeBar from '@/features/console/components/ConsoleScopeBar.vue'
import {
  EM_DASH,
  formatExactMoney,
  formatNullableInteger,
  formatNullableLatencyMs
} from '@/features/console/format'
import { identityApi, useConsoleScope, type IdentityKey } from '@/features/console/scope'
import { isUsableIdentityKey } from '@/features/console/keyPolicy'
import { playgroundApi } from '@/features/playground/api'
import { useAuthStore } from '@/stores/auth'
import PlaygroundCostConfirmModal from '@/features/playground/components/PlaygroundCostConfirmModal.vue'
import PlaygroundRunPane from '@/features/playground/components/PlaygroundRunPane.vue'
import {
  applyAuthoritativeQuoteToEstimate,
  buildNativePlaygroundInput,
  buildPlaygroundSubmitPayload,
  DEFAULT_MAX_OUTPUT_TOKENS,
  estimatePlaygroundRunCost,
  formatModelRatePerMillion,
  resolveCatalogQuoteModelId,
  isExecutableEstimate
} from '@/features/playground/costEstimate'
import { createPlaygroundRunController } from '@/features/playground/runMachine'
import { listRecoveries, type PlaygroundRecovery } from '@/features/playground/recovery'
import type {
  PlaygroundCostEstimate,
  PlaygroundMessageDraft,
  PlaygroundProtocol,
  PlaygroundReplay,
  PlaygroundSideConfig,
  PlaygroundSubmit
} from '@/features/playground/types'

interface KeyOption {
  id: string
  name: string
  prefix: string
  ownerId: string | null
  isOwned: boolean
  platform: string
  actions: string[]
  modelAccess: IdentityKey['model_access']
  excludedModels: string[]
}

const props = withDefaults(
  defineProps<{
    teleportDisabled?: boolean
  }>(),
  {
    teleportDisabled: false
  }
)

const { locale, t } = useI18n()
const isZh = computed(() => String(locale?.value || '').startsWith('zh'))
const instance = getCurrentInstance()

const scope = useConsoleScope()
const authStore = useAuthStore()

const mobilePane = ref<'params' | 'session' | 'inspector'>('session')
const compareMode = ref(false)
const inspectedSide = ref<'A' | 'B'>('A')
const inspectorTab = ref<'telemetry' | 'payload' | 'events'>('telemetry')
const inspectorOpen = ref(true)

const bootstrapLoading = ref(true)
const bootstrapRefreshing = ref(false)
const bootstrapNotice = ref<ConsoleAsyncNotice | null>(null)
const confirmedNotice = ref<ConsoleAsyncNotice | null>(null)
const quoteErrorNotice = ref<ConsoleAsyncNotice | null>(null)
const keysLoadError = ref(false)
let bootstrapGeneration = 0
let loadedScopeKey = ''
let hasBootstrapped = false

const availableKeys = ref<KeyOption[]>([])
const canActAsKeys = ref(false)
const canManageByok = ref(false)
const canRunPlayground = ref(false)
const canReadRaw = ref(false)
const canRetainContent = ref(false)
const canReplay = ref(false)
const projectAccessVersion = ref('')
const catalogModels = ref<CatalogModelItem[]>([])
const modelFilterQuery = ref('')

const systemPrompt = ref('')
const toolsJson = ref('')
const toolsJsonError = ref('')
const replayRequestId = ref('')

const messages = ref<PlaygroundMessageDraft[]>([
  { id: 'msg-1', role: 'user', content: '' }
])

const sideA = reactive<PlaygroundSideConfig>({
  label: 'Side A',
  apiKeyId: '',
  modelId: '',
  protocol: 'openai_chat',
  temperature: 0.7,
  maxOutputTokens: DEFAULT_MAX_OUTPUT_TOKENS,
  reasoningEffort: '',
  stream: true,
  includeRawEvents: false,
  retainTextPreview: false
})

const sideB = reactive<PlaygroundSideConfig>({
  label: 'Side B',
  apiKeyId: '',
  modelId: '',
  protocol: 'openai_chat',
  temperature: 0.7,
  maxOutputTokens: DEFAULT_MAX_OUTPUT_TOKENS,
  reasoningEffort: '',
  stream: true,
  includeRawEvents: false,
  retainTextPreview: false
})

function isKeyOwnedByCurrentUser(apiKeyId: string): boolean {
  const key = availableKeys.value.find((item) => item.id === apiKeyId)
  return key ? key.isOwned : false
}

function resolvePayerForKey(apiKeyId: string): string | undefined {
  const key = availableKeys.value.find((item) => item.id === apiKeyId)
  if (!key || key.isOwned || !key.ownerId) return undefined
  return key.ownerId
}

const controllerA = createPlaygroundRunController({
  isZh: () => isZh.value,
  isKeyOwned: isKeyOwnedByCurrentUser,
  principalId: () => String(authStore.user?.id ?? ''),
  slot: 'A'
})
const controllerB = createPlaygroundRunController({
  isZh: () => isZh.value,
  isKeyOwned: isKeyOwnedByCurrentUser,
  principalId: () => String(authStore.user?.id ?? ''),
  slot: 'B'
})

const recoveryItems = ref<PlaygroundRecovery[]>([])
async function recoverRun(handle: PlaygroundRecovery) {
  if (handle.workspaceId !== scope.activeWorkspaceId.value || handle.projectId !== scope.activeProjectId.value) return
  if (!controllerA.restoreRecovery(handle)) return
  await controllerA.recoverByIdempotencyKey(handle.workspaceId, handle.projectId)
  recoveryItems.value = listRecoveries(String(authStore.user?.id ?? ''), handle.workspaceId, handle.projectId)
}

const consentModalOpen = ref(false)
const pendingTargets = ref<Array<'A' | 'B'>>([])
const pendingPayloads = ref<{ A?: PlaygroundSubmit; B?: PlaygroundSubmit }>({})
const pendingReplayPayload = ref<PlaygroundReplay | null>(null)
const pendingEstimates = ref<PlaygroundCostEstimate[]>([])
const quoteLoading = ref(false)
const inlinePreviewEstimates = ref<PlaygroundCostEstimate[]>([])
const inlinePreviewLoading = ref(false)
let quoteGeneration = 0
let quotedAt = 0
let consentContext = ''
const currentContext = computed(() => JSON.stringify({ scope: scope.selection.value, access: projectAccessVersion.value, sideA, sideB, compare: compareMode.value, messages: messages.value, system: systemPrompt.value, tools: toolsJson.value, replay: replayRequestId.value }))
watch(currentContext, () => {
  if (consentContext && consentContext !== currentContext.value) {
    quoteGeneration++
    quotedAt = 0
    consentModalOpen.value = false
    pendingEstimates.value = []
    pendingPayloads.value = {}
    pendingReplayPayload.value = null
    quoteLoading.value = false
  }
}, { flush: 'sync' })

const protocolOptions = computed<Array<{ value: PlaygroundProtocol; label: string }>>(() =>
  isZh.value
    ? [
        { value: 'openai_chat', label: 'OpenAI 对话补全 (Chat)' },
        { value: 'openai_responses', label: 'OpenAI 原生响应 (Responses)' },
        { value: 'anthropic_messages', label: 'Anthropic 消息协议 (Messages)' },
        { value: 'gemini_generate', label: 'Gemini 内容生成 (GenerateContent)' }
      ]
    : [
        { value: 'openai_chat', label: 'OpenAI Chat Completions' },
        { value: 'openai_responses', label: 'OpenAI Responses' },
        { value: 'anthropic_messages', label: 'Anthropic Messages' },
        { value: 'gemini_generate', label: 'Gemini GenerateContent' }
      ]
)

watch(
  isZh,
  (zh) => {
    sideA.label = zh ? 'A 侧' : 'Side A'
    sideB.label = zh ? 'B 侧' : 'Side B'
  },
  { immediate: true }
)

const filteredModels = computed(() => {
  const q = modelFilterQuery.value.trim().toLowerCase()
  if (!q) return catalogModels.value
  return catalogModels.value.filter(
    (m) =>
      m.id.toLowerCase().includes(q) ||
      m.slug.toLowerCase().includes(q) ||
      m.displayName.toLowerCase().includes(q) ||
      m.provider.toLowerCase().includes(q)
  )
})

function modelsForKey(keyId: string): CatalogModelItem[] {
  const key = availableKeys.value.find(item => item.id === keyId)
  if (!key || !key.actions.includes('text.chat') || !key.modelAccess || key.modelAccess.mode === 'deny_all') return []
  return filteredModels.value.filter(model => {
    if (key.platform && key.platform !== 'composite' && model.provider !== key.platform) return false
    if (model.availabilityState === 'catalog_only' || model.routeState !== 'eligible' || model.accessState !== 'allowed') return false
    const native = model.slug.startsWith(`${model.provider}/`) ? model.slug.slice(model.provider.length + 1) : model.slug
    if (key.excludedModels.some(id => id === model.id || id === model.slug || (key.platform === model.provider && id === native))) return false
    if (key.modelAccess.mode === 'inherit') return true
    return key.modelAccess.mode === 'allow_list' && key.modelAccess.ids.some(id => id === model.id || id === model.slug || (key.platform === model.provider && id === native))
  })
}
const modelsA = computed(() => modelsForKey(sideA.apiKeyId))
const modelsB = computed(() => modelsForKey(sideB.apiKeyId))
watch([() => sideA.apiKeyId, () => sideB.apiKeyId, catalogModels], () => {
  if (!hasBootstrapped) return
  if (sideA.modelId && !isModelInList(sideA.modelId, modelsA.value)) sideA.modelId = ''
  if (sideB.modelId && !isModelInList(sideB.modelId, modelsB.value)) sideB.modelId = ''
}, { flush: 'sync' })

function isModelInList(modelId: string, list: CatalogModelItem[]): boolean {
  const target = modelId.trim()
  if (!target) return false
  return list.some((m) => m.slug === target || m.id === target)
}

function isCatalogOnlyOrUnrouted(modelId: string): boolean {
  const target = modelId.trim()
  if (!target) return false
  const matched = findCatalogModel(target)
  if (matched) {
    return matched.availabilityState === 'catalog_only'
  }
  return catalogModels.value.length > 0
}

const modelACatalogOnly = computed(() => isCatalogOnlyOrUnrouted(sideA.modelId))
const modelBCatalogOnly = computed(() => compareMode.value && isCatalogOnlyOrUnrouted(sideB.modelId))

function selectFirstRoutedModel(side: 'A' | 'B'): void {
  const firstRouted = modelsForKey(side === 'A' ? sideA.apiKeyId : sideB.apiKeyId)[0]
  if (!firstRouted) return
  const nextId = firstRouted.id
  if (side === 'A') {
    sideA.modelId = nextId
  } else {
    sideB.modelId = nextId
  }
}

const inspectedSession = computed(() =>
  inspectedSide.value === 'B' && compareMode.value ? controllerB.state : controllerA.state
)

const inspectedConfig = computed(() =>
  inspectedSide.value === 'B' && compareMode.value ? sideB : sideA
)

const hasPromptInput = computed(() =>
  messages.value.some((m) => m.content.trim().length > 0)
)

const anyStreaming = computed(
  () =>
    controllerA.state.status === 'submitting' ||
    controllerA.state.status === 'streaming' ||
    controllerB.state.status === 'submitting' ||
    controllerB.state.status === 'streaming'
)

const hasActiveProjectScope = computed(() =>
  Boolean(scope.activeWorkspaceId.value && scope.activeProjectId.value)
)

const validProjectKeyIds = computed(() => new Set(availableKeys.value.map((k) => k.id)))
const ownedKeys = computed(() => availableKeys.value.filter((k) => k.isOwned))
const actAsKeys = computed(() =>
  canActAsKeys.value ? availableKeys.value.filter((k) => !k.isOwned) : []
)

const hasValidKeyA = computed(
  () =>
    hasActiveProjectScope.value &&
    Boolean(sideA.apiKeyId) &&
    validProjectKeyIds.value.has(sideA.apiKeyId)
)

const hasValidKeyB = computed(
  () =>
    hasActiveProjectScope.value &&
    Boolean(sideB.apiKeyId) &&
    validProjectKeyIds.value.has(sideB.apiKeyId)
)

const canTriggerPrimaryRun = computed(() => {
  if (!canRunPlayground.value || bootstrapLoading.value || bootstrapRefreshing.value || !hasPromptInput.value || !hasValidKeyA.value) return false
  if (replayRequestId.value.trim() && !canReplay.value) return false
  if (compareMode.value) {
    return Boolean(sideA.modelId) && hasValidKeyB.value && Boolean(sideB.modelId)
  }
  return Boolean(sideA.modelId || replayRequestId.value.trim())
})

function navigateTo(path: string): void {
  const proxyRouter = (instance?.proxy as { $router?: { push?: (target: string) => unknown } } | null | undefined)?.$router
  const router = instance?.appContext.config.globalProperties.$router ?? proxyRouter
  if (router && typeof router.push === 'function') {
    void router.push(path)
  } else if (typeof window !== 'undefined') {
    window.location.assign(path)
  }
}

function resetTransientScopeState(): void {
  controllerA.reset()
  controllerB.reset()
  consentModalOpen.value = false
  quoteErrorNotice.value = null
  pendingTargets.value = []
  pendingPayloads.value = {}
  pendingReplayPayload.value = null
  pendingEstimates.value = []
  inlinePreviewEstimates.value = []
  availableKeys.value = []
  canActAsKeys.value = false
  canManageByok.value = false
  canRunPlayground.value = false
  canReadRaw.value = false
  canRetainContent.value = false
  canReplay.value = false
  quoteGeneration++
  quotedAt = 0
  consentContext = ''
  catalogModels.value = []
  sideA.apiKeyId = ''
  sideB.apiKeyId = ''
  keysLoadError.value = false
}

function isUsableKey(key: IdentityKey): boolean {
  return isUsableIdentityKey(key, 'text.chat')
}

async function fetchAllProjectKeys(wsId: string, projId: string) {
  const collected: Awaited<ReturnType<typeof identityApi.listProjectKeys>>['items'] = []
  let cursor: string | undefined
  for (let i = 0; i < 20; i++) {
    const page = await identityApi.listProjectKeys(
      wsId,
      projId,
      cursor ? { limit: 100, cursor } : { limit: 100 }
    )
    collected.push(...(page.items || []))
    if (!page.next_cursor) return collected.filter(isUsableKey)
    cursor = page.next_cursor
  }
  throw new Error('The key list is incomplete; narrow the project or retry.')
}

async function loadKeysAndCatalog(isRefresh = false): Promise<void> {
  const gen = ++bootstrapGeneration
  if (isRefresh) {
    bootstrapRefreshing.value = true
  } else {
    bootstrapLoading.value = true
  }
  bootstrapNotice.value = null
  keysLoadError.value = false
  const hadCached = availableKeys.value.length > 0 || catalogModels.value.length > 0

  try {
    await scope.refreshScopes(isRefresh)
    if (gen !== bootstrapGeneration) return
    const wsId = scope.activeWorkspaceId.value
    const projId = scope.activeProjectId.value
    const hasScope = Boolean(wsId && projId)
    loadedScopeKey = `${wsId}:${projId}`
    recoveryItems.value = listRecoveries(String(authStore.user?.id ?? ''), wsId, projId)

    const [keysResult, modelsResult, accessResult] = await Promise.allSettled([
      hasScope
        ? fetchAllProjectKeys(wsId, projId)
        : Promise.resolve([]),
      hasScope ? catalogApi.listScopedModels(wsId, projId, { limit: 100 }) : Promise.resolve([]),
      hasScope && typeof identityApi.getProjectAccess === 'function'
        ? identityApi.getProjectAccess(wsId, projId)
        : Promise.resolve(null)
    ])
    if (gen !== bootstrapGeneration) return

    const accessActions =
      accessResult.status === 'fulfilled' && Array.isArray(accessResult.value?.actions)
        ? accessResult.value.actions
        : []
    canActAsKeys.value = accessActions.includes('keys.act_as') || accessActions.includes('*')
    canManageByok.value = accessActions.includes('credentials.byok.manage') || accessActions.includes('*')
    canRunPlayground.value = accessActions.includes('playground.run') || accessActions.includes('*')
    canReadRaw.value = accessActions.includes('playground.raw') || accessActions.includes('*')
    canRetainContent.value = accessActions.includes('requests.content.read') || accessActions.includes('*')
    canReplay.value = accessActions.includes('requests.replay') || accessActions.includes('*')
    projectAccessVersion.value = accessResult.status === 'fulfilled' ? accessResult.value?.scope_version ?? '' : ''
    if (!canReadRaw.value) { sideA.includeRawEvents = false; sideB.includeRawEvents = false }
    if (!canRetainContent.value) { sideA.retainTextPreview = false; sideB.retainTextPreview = false }
    if (accessResult.status === 'rejected') bootstrapNotice.value = classifyConsoleError(accessResult.reason, { isZh: isZh.value, requiredGrantFallback: 'playground.run' })

    const rawUserId = authStore.user?.id
    const currentUserId =
      rawUserId !== undefined && rawUserId !== null && String(rawUserId).trim() !== ''
        ? String(rawUserId).trim()
        : accessResult.status === 'fulfilled' && accessResult.value?.actor_id
          ? String(accessResult.value.actor_id).trim()
          : ''

    if (keysResult.status === 'fulfilled') {
      const mappedKeys: KeyOption[] = keysResult.value.map((k) => {
        const rawOwner =
          (k as { owner_id?: string | number; user_id?: string | number }).owner_id ??
          (k as { user_id?: string | number }).user_id
        const ownerId =
          rawOwner !== undefined && rawOwner !== null && String(rawOwner).trim() !== ''
            ? String(rawOwner).trim()
            : null
        const isOwned = Boolean(ownerId && currentUserId && ownerId === currentUserId)
        return {
          id: String(k.id),
          name: k.name,
          prefix: k.prefix,
          ownerId,
          platform: k.platform,
          actions: k.actions,
          modelAccess: k.model_access,
          excludedModels: k.excluded_models ?? [],
          isOwned
        }
      })
      availableKeys.value = mappedKeys.filter((k) => k.isOwned || canActAsKeys.value)
    } else {
      availableKeys.value = []
      keysLoadError.value = true
      bootstrapNotice.value = classifyConsoleError(keysResult.reason, {
        hasCachedData: false,
        lastSyncedAt: scope.lastSyncedAt.value,
        requiredGrantFallback: 'keys.read',
        isZh: isZh.value
      })
    }

    if (!hasScope && scope.scopeError.value && !bootstrapNotice.value) {
      keysLoadError.value = true
      bootstrapNotice.value = classifyConsoleError(scope.scopeError.value, {
        hasCachedData: false,
        lastSyncedAt: scope.lastSyncedAt.value,
        requiredGrantFallback: 'playground.run',
        isZh: isZh.value
      })
    }

    if (modelsResult.status === 'fulfilled') {
      catalogModels.value = modelsResult.value
    } else {
      catalogModels.value = []
      canRunPlayground.value = false
      bootstrapNotice.value = classifyConsoleError(modelsResult.reason, { isZh: isZh.value, requiredGrantFallback: 'models.read' })
    }

    const validIds = new Set(availableKeys.value.map((k) => k.id))
    const preferredKeyId = ownedKeys.value[0]?.id ?? availableKeys.value[0]?.id ?? ''
    if (!validIds.has(sideA.apiKeyId)) {
      sideA.apiKeyId = preferredKeyId
    }
    if (!validIds.has(sideB.apiKeyId)) {
      sideB.apiKeyId = preferredKeyId
    }
    if (!sideA.modelId && modelsA.value.length > 0) sideA.modelId = modelsA.value[0].id
    if (!sideB.modelId && modelsB.value.length > 0) sideB.modelId = (modelsB.value[1] ?? modelsB.value[0]).id
  } catch (err) {
    if (gen !== bootstrapGeneration) return
    bootstrapNotice.value = classifyConsoleError(err, {
      hasCachedData: hadCached,
      lastSyncedAt: scope.lastSyncedAt.value,
      requiredGrantFallback: 'playground.run',
      isZh: isZh.value
    })
  } finally {
    if (gen === bootstrapGeneration) {
      hasBootstrapped = true
      bootstrapLoading.value = false
      bootstrapRefreshing.value = false
    }
  }
}

function handleScopeSelect(payload: { workspaceId: string; projectId: string | null }): void {
  resetTransientScopeState()
  scope.selectWorkspaceProject(payload.workspaceId, payload.projectId)
  loadedScopeKey = `${scope.activeWorkspaceId.value}:${scope.activeProjectId.value}`
  void loadKeysAndCatalog(true)
}

function addMessage(role: 'user' | 'assistant' | 'tool' = 'user'): void {
  messages.value.push({
    id: `msg-${Date.now()}-${messages.value.length}`,
    role,
    content: ''
  })
}

function removeMessage(index: number): void {
  if (messages.value.length <= 1) {
    messages.value[0].content = ''
    return
  }
  messages.value.splice(index, 1)
}

function injectToolResultMessage(payload: { toolCallId: string; name: string }): void {
  messages.value.push({
    id: `msg-tool-${Date.now()}`,
    role: 'tool',
    toolCallId: payload.toolCallId,
    content: JSON.stringify({ status: 'ok', tool: payload.name }, null, 2)
  })
}

function findCatalogModel(modelId: string): CatalogModelItem | null {
  const target = modelId.trim()
  return (
    catalogModels.value.find((m) => m.id === target || m.slug === target) ?? null
  )
}

function inferFundingSourceFromQuota(
  rules: Array<{ source: string }> | undefined
): string | null {
  if (!rules || rules.length === 0) return null
  if (rules.some((r) => r.source === 'subscription')) return 'subscription'
  if (rules.some((r) => r.source === 'wallet')) return 'wallet'
  if (rules.some((r) => r.source === 'gift')) return 'gift'
  if (rules.some((r) => r.source === 'budget')) return 'budget'
  return null
}

async function resolveServerQuoteForEstimate(
  base: PlaygroundCostEstimate,
  catalogModel: CatalogModelItem | null
): Promise<PlaygroundCostEstimate> {
  const wsId = scope.activeWorkspaceId.value
  const projId = scope.activeProjectId.value
  if (!wsId || !projId) {
    return {
      ...base,
      rateKnown: false,
      maxTotalCost: null,
      quoteState: 'requires_scope',
      unpricedReason: 'requires_scope'
    }
  }

  if (typeof playgroundApi.quoteProjectModel !== 'function') {
    return { ...base, rateKnown: false, maxTotalCost: null, quoteState: 'error', keyScoped: false }
  }

  const canonicalModelId = resolveCatalogQuoteModelId(base.modelId, catalogModel)
  if (!canonicalModelId) return { ...base, rateKnown: false, maxTotalCost: null, quoteState: 'unpriced', keyScoped: false, unpricedReason: 'unlisted_model' }

  const validKeyId = /^[1-9]\d*$/.test(base.apiKeyId.trim()) ? base.apiKeyId.trim() : undefined
  const scenario = {
    input_tokens: String(base.estimatedInputTokens),
    output_tokens: String(base.maxOutputTokens),
    requests: '1'
  }

  const quotaPromise =
    typeof playgroundApi.getEffectiveQuota === 'function'
      ? playgroundApi
          .getEffectiveQuota(wsId, projId, {
            api_key_id: validKeyId,
            model_id: canonicalModelId
          })
          .catch(() => null)
      : Promise.resolve(null)

  try {
    if (!validKeyId) throw new Error('An active project key is required for a quote.')
    const keyScoped = true
    const quoteResult = await playgroundApi.quoteProjectModel(wsId, projId, {
      model_id: canonicalModelId, api_key_id: validKeyId, protocol: base.protocol, action: 'text.chat', scenario
    })
    const quota = await quotaPromise
    return applyAuthoritativeQuoteToEstimate(base, quoteResult, {
      keyScoped,
      fundingSource: inferFundingSourceFromQuota(quota?.rules),
      budgetBlocked: Boolean(quota?.blocked)
    })
  } catch (err) {
    const status = (err as { status?: number; response?: { status?: number } })?.status ??
      (err as { response?: { status?: number } })?.response?.status
    if (status === 403 || (typeof status === 'number' && status >= 500) || status === 0) {
      quoteErrorNotice.value = classifyConsoleError(err, {
        requiredGrantFallback: 'playground.run',
        isZh: isZh.value
      })
      return {
        ...base,
        rateKnown: false,
        maxTotalCost: null,
        quoteState: 'error',
        unpricedReason: status === 403 ? 'forbidden' : 'quote_error'
      }
    }
    return { ...base, rateKnown: false, maxTotalCost: null, priceVersion: null, keyScoped: false,
      quoteState: 'error', unpricedReason: 'quote_error' }
  }
}

async function refreshAuthoritativeQuotes(
  initialEstimates: PlaygroundCostEstimate[],
  target: 'modal' | 'inline'
): Promise<void> {
  if (typeof playgroundApi.quoteProjectModel !== 'function' && scope.activeWorkspaceId.value && scope.activeProjectId.value) {
    pendingEstimates.value = initialEstimates.map(item => ({ ...item, rateKnown: false, maxTotalCost: null, quoteState: 'error' }))
    return
  }
  quoteErrorNotice.value = null
  const gen = ++quoteGeneration
  if (target === 'modal') {
    quoteLoading.value = true
  } else {
    inlinePreviewLoading.value = true
  }
  try {
    const enriched = await Promise.all(
      initialEstimates.map((item) =>
        resolveServerQuoteForEstimate(item, findCatalogModel(item.modelId))
      )
    )
    if (gen !== quoteGeneration) return
    if (target === 'modal') {
      pendingEstimates.value = enriched
      quotedAt = Date.now()
    } else {
      inlinePreviewEstimates.value = enriched
    }
  } finally {
    if (gen === quoteGeneration) {
      quoteLoading.value = false
      inlinePreviewLoading.value = false
    }
  }
}

function previewAuthoritativeQuotes(): void {
  if (consentModalOpen.value) return
  toolsJsonError.value = ''
  quoteErrorNotice.value = null
  try {
    const targets: Array<'A' | 'B'> = compareMode.value ? ['A', 'B'] : ['A']
    const estimates: PlaygroundCostEstimate[] = []
    for (const target of targets) {
      const cfg = target === 'A' ? sideA : sideB
      const nativeInput = buildNativePlaygroundInput(cfg.protocol, {
        systemPrompt: systemPrompt.value,
        messages: messages.value,
        temperature: cfg.temperature,
        maxOutputTokens: cfg.maxOutputTokens,
        reasoningEffort: cfg.reasoningEffort,
        toolsJson: toolsJson.value
      })
      const submitPayload = buildPlaygroundSubmitPayload(cfg, nativeInput)
      estimates.push(estimatePlaygroundRunCost(cfg, submitPayload, findCatalogModel(cfg.modelId)))
    }
    inlinePreviewEstimates.value = estimates
    void refreshAuthoritativeQuotes(estimates, 'inline')
  } catch (err) {
    toolsJsonError.value = err instanceof Error ? err.message : String(err)
  }
}

async function prepareConsentForTargets(targets: Array<'A' | 'B'>): Promise<void> {
  toolsJsonError.value = ''
  quoteErrorNotice.value = null
  confirmedNotice.value = null
  if (!canRunPlayground.value || !hasActiveProjectScope.value || availableKeys.value.length === 0) {
    return
  }
  for (const target of targets) {
    const cfg = target === 'A' ? sideA : sideB
    if (!cfg.apiKeyId || !validProjectKeyIds.value.has(cfg.apiKeyId)) {
      return
    }
  }

  try {
    const estimates: PlaygroundCostEstimate[] = []
    const payloads: { A?: PlaygroundSubmit; B?: PlaygroundSubmit } = {}
    pendingReplayPayload.value = null

    if (replayRequestId.value.trim() && targets.length === 1 && targets[0] === 'A') {
      if (!canReplay.value) return
      const fingerprint = currentContext.value
      const replay = await playgroundApi.getReplayContext(scope.activeWorkspaceId.value, scope.activeProjectId.value, replayRequestId.value.trim())
      if (fingerprint !== currentContext.value) return
      sideA.modelId = replay.model_id
      sideA.protocol = replay.protocol

      const nativeInput = buildNativePlaygroundInput(sideA.protocol, {
        systemPrompt: systemPrompt.value,
        messages: messages.value,
        temperature: sideA.temperature,
        maxOutputTokens: sideA.maxOutputTokens,
        reasoningEffort: sideA.reasoningEffort,
        toolsJson: toolsJson.value
      })
      const payerUserId = resolvePayerForKey(sideA.apiKeyId)
      const replayPayload: PlaygroundReplay = {
        api_key_id: sideA.apiKeyId,
        ...(payerUserId ? { payer_user_id: payerUserId } : {}),
        input: nativeInput,
        options: {
          stream: sideA.stream,
          include_raw_events: sideA.stream ? sideA.includeRawEvents : false,
          retain_text_preview: sideA.retainTextPreview
        },
        funding_acknowledged: true
      }
      const pseudoSubmit = buildPlaygroundSubmitPayload(sideA, nativeInput)
      estimates.push(
        estimatePlaygroundRunCost(sideA, pseudoSubmit, findCatalogModel(sideA.modelId))
      )
      pendingReplayPayload.value = replayPayload
      controllerA.markConsentPending(replayPayload)
    } else {
      for (const target of targets) {
        const cfg = target === 'A' ? sideA : sideB
        const nativeInput = buildNativePlaygroundInput(cfg.protocol, {
          systemPrompt: systemPrompt.value,
          messages: messages.value,
          temperature: cfg.temperature,
          maxOutputTokens: cfg.maxOutputTokens,
          reasoningEffort: cfg.reasoningEffort,
          toolsJson: toolsJson.value
        })
        const submitPayload = buildPlaygroundSubmitPayload(
          cfg,
          nativeInput,
          resolvePayerForKey(cfg.apiKeyId)
        )
        payloads[target] = submitPayload
        estimates.push(
          estimatePlaygroundRunCost(cfg, submitPayload, findCatalogModel(cfg.modelId))
        )
        if (target === 'A') controllerA.markConsentPending(submitPayload)
        if (target === 'B') controllerB.markConsentPending(submitPayload)
      }
    }

    consentContext = currentContext.value
    quotedAt = 0
    pendingTargets.value = targets
    pendingPayloads.value = payloads
    pendingEstimates.value = estimates
    consentModalOpen.value = true
    void refreshAuthoritativeQuotes(estimates, 'modal')
  } catch (err) {
    toolsJsonError.value = err instanceof Error ? err.message : String(err)
  }
}

function handleCancelConsent(): void {
  consentModalOpen.value = false
  controllerA.cancelConsent()
  controllerB.cancelConsent()
}

async function handleConfirmAndExecute(): Promise<void> {
  if (!canRunPlayground.value || quoteLoading.value || consentContext !== currentContext.value || !quotedAt || Date.now() - quotedAt > 60_000 || !pendingEstimates.value.length || !pendingEstimates.value.every(isExecutableEstimate)) return
  for (const [index, target] of pendingTargets.value.entries()) {
    const version = pendingEstimates.value[index]?.priceVersion
    if (pendingPayloads.value[target] && version) pendingPayloads.value[target]!.expected_price_version = version
  }
  if (pendingReplayPayload.value && pendingEstimates.value[0]?.priceVersion) pendingReplayPayload.value.expected_price_version = pendingEstimates.value[0].priceVersion
  consentModalOpen.value = false
  const wsId = scope.activeWorkspaceId.value
  const projId = scope.activeProjectId.value
  if (!wsId || !projId) {
    return
  }
  const targets = [...pendingTargets.value]
  if (pendingReplayPayload.value && replayRequestId.value.trim()) {
    if (!validProjectKeyIds.value.has(pendingReplayPayload.value.api_key_id)) {
      return
    }
  } else {
    for (const target of targets) {
      const payload = pendingPayloads.value[target]
      if (payload && !validProjectKeyIds.value.has(payload.api_key_id)) {
        return
      }
    }
  }

  confirmedNotice.value = {
    kind: 'confirmed',
    title: isZh.value
      ? '已确认费用预估，正在独立提交运行'
      : 'Cost Estimate Confirmed — Dispatching Funded Run(s)'
  }

  if (pendingReplayPayload.value && replayRequestId.value.trim()) {
    await controllerA.executeApprovedReplay(
      wsId,
      projId,
      replayRequestId.value.trim(),
      pendingReplayPayload.value,
      undefined,
      sideA.protocol
    )
    return
  }

  const tasks: Array<Promise<void>> = []
  if (targets.includes('A') && pendingPayloads.value.A) {
    tasks.push(controllerA.executeApprovedRun(wsId, projId, pendingPayloads.value.A))
  }
  if (targets.includes('B') && pendingPayloads.value.B) {
    tasks.push(controllerB.executeApprovedRun(wsId, projId, pendingPayloads.value.B))
  }
  await Promise.allSettled(tasks)
}

function handleKeydown(event: KeyboardEvent): void {
  if ((event.metaKey || event.ctrlKey) && event.key === 'Enter') {
    if (!consentModalOpen.value && canTriggerPrimaryRun.value) {
      event.preventDefault()
      prepareConsentForTargets(compareMode.value ? ['A', 'B'] : ['A'])
    }
  } else if (event.key === 'Escape' && !consentModalOpen.value && anyStreaming.value) {
    event.preventDefault()
    const wsId = scope.activeWorkspaceId.value || undefined
    const projId = scope.activeProjectId.value || undefined
    if (controllerA.state.status === 'streaming' || controllerA.state.status === 'submitting') {
      void controllerA.stop(wsId, projId)
    }
    if (controllerB.state.status === 'streaming' || controllerB.state.status === 'submitting') {
      void controllerB.stop(wsId, projId)
    }
  }
}

watch(compareMode, (enabled) => {
  if (!enabled) {
    controllerB.reset()
    inspectedSide.value = 'A'
  }
})

watch(
  () => `${scope.activeWorkspaceId.value}:${scope.activeProjectId.value}`,
  (nextKey) => {
    if (!hasBootstrapped || nextKey === loadedScopeKey) return
    loadedScopeKey = nextKey
    resetTransientScopeState()
    void loadKeysAndCatalog(true)
  }
)

function readRouteQuery(): Record<string, string | undefined> | undefined {
  const proxyRoute = (instance?.proxy as { $route?: { query?: Record<string, string | undefined> } } | null | undefined)?.$route
  return proxyRoute?.query ?? (instance?.appContext.config.globalProperties.$route?.query as Record<string, string | undefined> | undefined)
}

function applyRouteQueryOverrides(): void {
  const routeQuery = readRouteQuery()
  if (routeQuery?.model) {
    sideA.modelId = String(routeQuery.model)
  }
  if (routeQuery?.replay_request_id) {
    replayRequestId.value = String(routeQuery.replay_request_id)
  }
}

watch(
  () => {
    const routeQuery = readRouteQuery()
    return routeQuery ? `${routeQuery.model ?? ''}|${routeQuery.replay_request_id ?? ''}` : ''
  },
  () => {
    applyRouteQueryOverrides()
  }
)

onMounted(() => {
  applyRouteQueryOverrides()
  if (typeof window !== 'undefined') {
    window.addEventListener('keydown', handleKeydown)
  }
  void loadKeysAndCatalog(false)
})

onBeforeUnmount(() => {
  bootstrapGeneration++
  quoteGeneration++
  controllerA.reset()
  controllerB.reset()
  if (typeof window !== 'undefined') {
    window.removeEventListener('keydown', handleKeydown)
  }
})

const requestPayloadSnippet = computed(() => {
  const payload = inspectedSession.value.submittedPayload
  if (!payload) {
    return `// ${t('playgroundDiagnostics.rawRequestEmpty')}`
  }
  const sanitized: Record<string, unknown> = { ...payload }
  delete sanitized.funding_acknowledged
  return JSON.stringify(sanitized, null, 2)
})

function formatInspectorFrameData(raw: string): string {
  if (!raw) return EM_DASH
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

function formatInspectorRawEvent(rawEvent: unknown): string {
  try {
    return JSON.stringify(rawEvent, null, 2)
  } catch {
    return String(rawEvent)
  }
}

const linkedRequestPath = computed(() => {
  const reqId = inspectedSession.value.requestId
  if (!reqId) return ''
  const wsId = scope.activeWorkspaceId.value
  const projId = scope.activeProjectId.value
  const qs = wsId && projId ? `?workspace_id=${encodeURIComponent(wsId)}&project_id=${encodeURIComponent(projId)}` : ''
  return `/requests/${encodeURIComponent(reqId)}${qs}`
})

function formatSettlementState(state: string | null | undefined): string {
  if (!state) return EM_DASH
  if (!isZh.value) return state
  const map: Record<string, string> = {
    estimated: '预估',
    reserved: '已冻结',
    pending: '待结算',
    settled: '已结算',
    needs_review: '待核查',
    adjusted: '已调账',
    refunded: '已退回',
    canceled: '已取消',
    unknown: '未知'
  }
  return map[state] || state
}
</script>

<template>
  <AppLayout>
    <div data-testid="playground-workbench" class="w-full space-y-4">
      <section v-if="!hasActiveProjectScope && !bootstrapLoading" class="card space-y-3 p-4" data-testid="playground-project-guidance">
        <p>{{ isZh ? 'Playground 使用项目权限和项目资源。请选择已授权项目，或向工作空间管理员申请。个人 Key 可在个人空间独立创建和使用。' : 'Playground requires an authorized project. Select a project or ask its workspace administrator for access. Personal keys remain available in Personal.' }}</p>
        <button class="btn btn-primary" @click="navigateTo('/workspaces')">{{ isZh ? '选择项目与查看申请方式' : 'Select project and access guidance' }}</button>
        <button class="btn btn-secondary" @click="scope.selectPersonalScope(); navigateTo('/keys')">{{ isZh ? '创建个人 Key' : 'Create Personal Key' }}</button>
      </section>
      <section v-if="recoveryItems.length" class="card space-y-2 p-4" data-testid="playground-recovery-journal">
        <p>{{ isZh ? '以下运行尚未确认结算。请查询原运行，不要重复提交付费请求。' : 'These runs have no confirmed settlement. Recover the original run before submitting another paid request.' }}</p>
        <button v-for="handle in recoveryItems" :key="handle.idempotencyKey" class="btn btn-secondary" @click="recoverRun(handle)">{{ isZh ? '恢复原运行' : 'Recover original run' }} · {{ handle.runId || handle.idempotencyKey }}</button>
      </section>
      <ConsoleScopeBar
        :workspaces="scope.scopes.value"
        :projects="scope.availableProjects.value"
        :workspace-id="scope.activeWorkspaceId.value"
        :project-id="scope.activeProjectId.value"
        :loading="scope.loading.value || bootstrapLoading"
        :refreshing="scope.refreshing.value || bootstrapRefreshing"
        :last-synced-at="scope.lastSyncedAt.value"
        active-surface="playground"
        @select-scope="handleScopeSelect"
        @refresh="loadKeysAndCatalog(true)"
        @navigate="navigateTo"
      />

      <!-- Top Workbench Bar -->
      <header class="flex flex-wrap items-center justify-between gap-3 rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] px-4 py-3">
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <h1 class="text-lg font-semibold tracking-tight text-[var(--oh-ink,var(--foreground))]">
              {{ isZh ? '模型调试台' : 'Playground' }}
            </h1>
            <Badge tone="neutral" variant="outline" mono>{{ isZh ? '对话推理' : 'Chat Inference' }}</Badge>
          </div>
          <p class="mt-0.5 text-xs text-[var(--oh-ink-secondary,var(--muted-foreground))]">
            {{
              isZh
                ? '原生协议流式调试台 · 服务端权威报价与独立预算冻结 · 支持双模型左右对比与断线无损恢复'
                : 'Native protocol workbench · Authoritative server quotes & separate budget holds · Side-by-side compare & disconnect recovery'
            }}
          </p>
        </div>

        <div class="flex flex-wrap items-center gap-2">
          <SegmentedControl
            :model-value="compareMode ? 'compare' : 'single'"
            size="sm"
            mono
            :aria-label="isZh ? '调试台模式' : 'Playground Mode'"
            :options="[
              { value: 'single', label: isZh ? '单模型' : 'Single' },
              { value: 'compare', label: isZh ? '左右对比' : 'Compare A/B' }
            ]"
            @update:model-value="(v) => (compareMode = v === 'compare')"
          />
          <Button
            variant="ghost"
            size="sm"
            class="hidden lg:inline-flex"
            :aria-expanded="inspectorOpen"
            data-testid="toggle-inspector-drawer"
            @click="inspectorOpen = !inspectorOpen"
          >
            {{ inspectorOpen ? t('playgroundDiagnostics.collapseDrawer') : t('playgroundDiagnostics.expandDrawer') }}
          </Button>
          <Button variant="ghost" size="sm" @click="navigateTo('/tasks')">
            {{ isZh ? '图像/异步任务中心 →' : 'Async Task Center →' }}
          </Button>
        </div>
      </header>

      <ConsoleAsyncBanner
        v-if="bootstrapNotice"
        :notice="bootstrapNotice"
        :retry-label="isZh ? '重试加载' : 'Retry'"
        @retry="loadKeysAndCatalog(true)"
      />

      <ConsoleAsyncBanner
        v-if="confirmedNotice"
        :notice="confirmedNotice"
        dismissible
        @dismiss="confirmedNotice = null"
      />

      <!-- Mobile 360px Pane Switcher -->
      <div class="lg:hidden">
        <SegmentedControl
          v-model="mobilePane"
          size="sm"
          :aria-label="isZh ? '移动端面板切换' : 'Mobile Playground Pane'"
          :options="[
            { value: 'params', label: isZh ? '参数配置' : 'Params' },
            { value: 'session', label: isZh ? '会话与输出' : 'Session' },
            { value: 'inspector', label: isZh ? '遥测与帧' : 'Inspector' }
          ]"
        />
      </div>

      <!-- Initial Skeleton -->
      <div
        v-if="bootstrapLoading"
        data-testid="console-state-skeleton"
        class="grid gap-4 lg:grid-cols-[280px_minmax(0,1fr)_340px]"
      >
        <Skeleton variant="rect" height="420px" />
        <Skeleton variant="rect" height="420px" />
        <Skeleton variant="rect" height="420px" />
      </div>

      <!-- 3-Pane Desktop Workbench -->
      <div
        v-else
        :class="[
          'grid items-start gap-4',
          inspectorOpen ? 'lg:grid-cols-[280px_minmax(0,1fr)_340px]' : 'lg:grid-cols-[280px_minmax(0,1fr)]'
        ]"
      >
        <!-- LEFT PANE: Parameters & Credentials -->
        <aside
          data-testid="playground-left-pane"
          :class="[
            'space-y-4 rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] p-3.5',
            mobilePane !== 'params' ? 'hidden lg:block' : 'block'
          ]"
        >
          <div class="space-y-2">
            <div class="flex items-center justify-between">
              <span class="font-mono text-[11px] font-semibold uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
                {{ compareMode ? (isZh ? 'A 侧配置' : 'Side A Config') : (isZh ? '运行配置' : 'Run Config') }}
              </span>
            </div>

            <label v-if="availableKeys.length > 0" class="block space-y-1 text-xs">
              <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                {{ isZh ? '下游 API 密钥 (Key A)' : 'Downstream API Key (A)' }}
              </span>
              <select
                v-model="sideA.apiKeyId"
                data-testid="select-key-a"
                class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs tabular-nums text-[var(--oh-ink,var(--foreground))]"
              >
                <template v-if="actAsKeys.length > 0">
                  <optgroup v-if="ownedKeys.length > 0" :label="t('playgroundKeys.ownedGroupLabel')">
                    <option v-for="k in ownedKeys" :key="k.id" :value="k.id">
                      {{ k.name }} (#{{ k.id }}{{ k.prefix ? ` · ${k.prefix}` : '' }})
                    </option>
                  </optgroup>
                  <optgroup :label="t('playgroundKeys.actAsGroupLabel')">
                    <option v-for="k in actAsKeys" :key="k.id" :value="k.id">
                      {{ k.name }} (#{{ k.id }}{{ k.prefix ? ` · ${k.prefix}` : '' }})
                    </option>
                  </optgroup>
                </template>
                <template v-else>
                  <option v-for="k in ownedKeys" :key="k.id" :value="k.id">
                    {{ k.name }} (#{{ k.id }}{{ k.prefix ? ` · ${k.prefix}` : '' }})
                  </option>
                </template>
              </select>
            </label>
            <div
              v-else
              data-testid="playground-keys-empty-state"
              role="status"
              class="space-y-2 rounded-[var(--oh-r-sm,6px)] border border-dashed border-[var(--oh-border-strong,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] p-2.5 text-xs"
            >
              <p class="font-semibold text-[var(--oh-ink,var(--foreground))]">
                {{
                  keysLoadError
                    ? t('playgroundKeys.loadErrorTitle')
                    : !hasActiveProjectScope
                      ? t('playgroundKeys.scopeRequiredTitle')
                      : t('playgroundKeys.emptyTitle')
                }}
              </p>
              <p class="text-[11px] leading-relaxed text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                {{
                  keysLoadError
                    ? t('playgroundKeys.loadErrorDescription')
                    : !hasActiveProjectScope
                      ? t('playgroundKeys.scopeRequiredDescription')
                      : t('playgroundKeys.emptyDescription')
                }}
              </p>
              <div class="flex flex-wrap items-center gap-1.5 pt-0.5">
                <Button
                  v-if="hasActiveProjectScope && !keysLoadError"
                  data-testid="playground-create-project-key-btn"
                  variant="outline-accent"
                  size="xs"
                  @click="navigateTo('/keys')"
                >
                  {{ t('playgroundKeys.createProjectKeyAction') }}
                </Button>
                <Button
                  v-else-if="!hasActiveProjectScope && !keysLoadError"
                  data-testid="playground-select-scope-btn"
                  variant="secondary"
                  size="xs"
                  @click="navigateTo('/workspaces')"
                >
                  {{ t('playgroundKeys.selectScopeAction') }}
                </Button>
                <Button
                  v-if="keysLoadError"
                  data-testid="playground-retry-keys-btn"
                  variant="secondary"
                  size="xs"
                  @click="loadKeysAndCatalog(true)"
                >
                  {{ t('playgroundKeys.retryAction') }}
                </Button>
              </div>
            </div>

            <label class="block space-y-1 text-xs">
              <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                {{ isZh ? '筛选目录模型' : 'Filter Catalog Models' }}
              </span>
              <input
                v-model="modelFilterQuery"
                data-testid="model-filter-input"
                type="search"
                :placeholder="isZh ? '按 slug / 厂商筛选…' : 'Filter by slug or provider…'"
                class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2.5 font-mono text-xs text-[var(--oh-ink,var(--foreground))]"
              />
            </label>

            <p
              v-if="modelFilterQuery && filteredModels.length === 0 && catalogModels.length > 0"
              data-testid="console-state-zero-filter"
              class="rounded-[var(--oh-r-sm,6px)] border border-dashed border-[var(--oh-border,var(--border))] px-2.5 py-1.5 text-[11px] text-[var(--oh-ink-secondary,var(--muted-foreground))]"
            >
              {{ isZh ? '无匹配目录模型，可直接输入自定义模型 ID。' : 'Zero models match filter. Clear filter or type exact model ID.' }}
            </p>

            <label class="block space-y-1 text-xs">
              <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                {{ isZh ? '模型 ID (Model A)' : 'Model ID (A)' }}
              </span>
              <select
                v-if="modelsA.length > 0"
                v-model="sideA.modelId"
                data-testid="select-model-a"
                class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs text-[var(--oh-ink,var(--foreground))]"
              >
                <option
                  v-if="sideA.modelId && !isModelInList(sideA.modelId, modelsA)"
                  :value="sideA.modelId"
                >
                  {{ sideA.modelId }} ({{ isZh ? '平台目录模型 / 暂无可用路由' : 'Catalog only / No route' }})
                </option>
                <option v-for="m in modelsA" :key="m.id" :value="m.id">
                  {{ m.displayName && m.displayName !== m.slug ? `${m.displayName} (${m.slug})` : (m.slug || m.displayName) }}
                </option>
              </select>
              <input
                v-else
                v-model="sideA.modelId"
                data-testid="input-model-a"
                type="text"
                :placeholder="isZh ? '输入已路由模型 ID' : 'Enter routed model slug/ID'"
                class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2.5 font-mono text-xs text-[var(--oh-ink,var(--foreground))]"
              />
            </label>

            <div
              v-if="modelACatalogOnly"
              data-testid="playground-catalog-only-notice-A"
              class="space-y-1.5 rounded-[var(--oh-r-sm,6px)] border border-amber-500/40 bg-amber-500/10 p-2.5 text-[11px] text-[var(--oh-ink,var(--foreground))]"
            >
              <p class="font-medium">
                {{
                  isZh
                    ? `模型「${sideA.modelId}」为平台目录模型，当前项目作用域暂未配置可用路由（按需接入）。`
                    : `Model "${sideA.modelId}" is catalog-only and has no active route in this project scope.`
                }}
              </p>
              <div v-if="catalogModels.length > 0" class="flex flex-wrap gap-1.5">
                <Button
                  data-testid="switch-routed-model-A"
                  variant="secondary"
                  size="xs"
                  @click="selectFirstRoutedModel('A')"
                >
                  {{ isZh ? '切换为已就绪模型' : 'Switch to Routed Model' }}
                </Button>
              </div>
            </div>

            <label class="block space-y-1 text-xs">
              <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                {{ isZh ? '原生协议 (Protocol A)' : 'Wire Protocol (A)' }}
              </span>
              <select
                v-model="sideA.protocol"
                data-testid="select-protocol-a"
                class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs text-[var(--oh-ink,var(--foreground))]"
              >
                <option v-for="p in protocolOptions" :key="p.value" :value="p.value">
                  {{ p.label }}
                </option>
              </select>
            </label>

            <div class="grid grid-cols-2 gap-2">
              <label class="block space-y-1 text-xs">
                <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                  {{ isZh ? '温度' : 'Temp' }}
                </span>
                <input
                  v-model.number="sideA.temperature"
                  type="number"
                  min="0"
                  max="2"
                  step="0.1"
                  class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs tabular-nums text-[var(--oh-ink,var(--foreground))]"
                />
              </label>
              <label class="block space-y-1 text-xs">
                <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                  {{ isZh ? '最大输出' : 'Max Tokens' }}
                </span>
                <input
                  v-model.number="sideA.maxOutputTokens"
                  type="number"
                  min="1"
                  max="32768"
                  step="64"
                  class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs tabular-nums text-[var(--oh-ink,var(--foreground))]"
                />
              </label>
            </div>

            <label class="block space-y-1 text-xs">
              <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                {{ isZh ? '推理强度' : 'Reasoning Effort' }}
              </span>
              <select
                v-model="sideA.reasoningEffort"
                class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs text-[var(--oh-ink,var(--foreground))]"
              >
                <option value="">{{ isZh ? '默认' : 'Default' }}</option>
                <option value="low">{{ isZh ? '低 (Low)' : 'Low' }}</option>
                <option value="medium">{{ isZh ? '中 (Medium)' : 'Medium' }}</option>
                <option value="high">{{ isZh ? '高 (High)' : 'High' }}</option>
              </select>
            </label>

            <div class="space-y-1.5 pt-1 text-xs">
              <label class="flex items-center gap-2 text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                <input v-model="sideA.stream" type="checkbox" class="h-3.5 w-3.5" />
                <span>{{ isZh ? '流式输出 (SSE)' : 'SSE Streaming' }}</span>
              </label>
              <label class="flex items-center gap-2 text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                <input v-model="sideA.includeRawEvents" :disabled="!sideA.stream || !canReadRaw" type="checkbox" class="h-3.5 w-3.5" />
                <span>{{ isZh ? '包含上游原始帧' : 'Include raw upstream frames' }}</span>
              </label>
              <label class="flex items-center gap-2 text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                <input v-model="sideA.retainTextPreview" :disabled="!canRetainContent" type="checkbox" class="h-3.5 w-3.5" />
                <span>{{ isZh ? '保留 30 秒临时文本预览' : '30s Ephemeral Text Preview' }}</span>
              </label>
            </div>
          </div>

          <!-- Side B Config when Compare Mode is enabled -->
          <div
            v-if="compareMode"
            data-testid="side-b-config"
            class="space-y-2 border-t border-[var(--oh-border,var(--border))] pt-3.5"
          >
            <span class="font-mono text-[11px] font-semibold uppercase tracking-wider text-[var(--oh-accent-text,var(--brand-text,#d97706))]">
              {{ isZh ? 'B 侧配置（独立计费）' : 'Side B Config (Independent)' }}
            </span>

            <label v-if="availableKeys.length > 0" class="block space-y-1 text-xs">
              <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                {{ isZh ? '下游 API 密钥 (Key B)' : 'Downstream API Key (B)' }}
              </span>
              <select
                v-model="sideB.apiKeyId"
                data-testid="select-key-b"
                class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs tabular-nums text-[var(--oh-ink,var(--foreground))]"
              >
                <template v-if="actAsKeys.length > 0">
                  <optgroup v-if="ownedKeys.length > 0" :label="t('playgroundKeys.ownedGroupLabel')">
                    <option v-for="k in ownedKeys" :key="k.id" :value="k.id">
                      {{ k.name }} (#{{ k.id }}{{ k.prefix ? ` · ${k.prefix}` : '' }})
                    </option>
                  </optgroup>
                  <optgroup :label="t('playgroundKeys.actAsGroupLabel')">
                    <option v-for="k in actAsKeys" :key="k.id" :value="k.id">
                      {{ k.name }} (#{{ k.id }}{{ k.prefix ? ` · ${k.prefix}` : '' }})
                    </option>
                  </optgroup>
                </template>
                <template v-else>
                  <option v-for="k in ownedKeys" :key="k.id" :value="k.id">
                    {{ k.name }} (#{{ k.id }}{{ k.prefix ? ` · ${k.prefix}` : '' }})
                  </option>
                </template>
              </select>
            </label>
            <p
              v-else
              data-testid="playground-keys-empty-state-b"
              class="rounded-[var(--oh-r-sm,6px)] border border-dashed border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2.5 py-2 text-[11px] text-[var(--oh-ink-secondary,var(--muted-foreground))]"
            >
              {{ t('playgroundKeys.sideBUnavailable') }}
            </p>

            <label class="block space-y-1 text-xs">
              <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                {{ isZh ? '模型 ID (Model B)' : 'Model ID (B)' }}
              </span>
              <select
                v-if="modelsB.length > 0"
                v-model="sideB.modelId"
                data-testid="select-model-b"
                class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs text-[var(--oh-ink,var(--foreground))]"
              >
                <option
                  v-if="sideB.modelId && !isModelInList(sideB.modelId, modelsB)"
                  :value="sideB.modelId"
                >
                  {{ sideB.modelId }} ({{ isZh ? '平台目录模型 / 暂无可用路由' : 'Catalog only / No route' }})
                </option>
                <option v-for="m in modelsB" :key="m.id" :value="m.id">
                  {{ m.displayName && m.displayName !== m.slug ? `${m.displayName} (${m.slug})` : (m.slug || m.displayName) }}
                </option>
              </select>
              <input
                v-else
                v-model="sideB.modelId"
                data-testid="input-model-b"
                type="text"
                :placeholder="isZh ? '输入模型 B ID' : 'Enter Model B slug/ID'"
                class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2.5 font-mono text-xs text-[var(--oh-ink,var(--foreground))]"
              />
            </label>

            <div
              v-if="modelBCatalogOnly"
              data-testid="playground-catalog-only-notice-B"
              class="space-y-1.5 rounded-[var(--oh-r-sm,6px)] border border-amber-500/40 bg-amber-500/10 p-2.5 text-[11px] text-[var(--oh-ink,var(--foreground))]"
            >
              <p class="font-medium">
                {{
                  isZh
                    ? `模型「${sideB.modelId}」当前作用域暂未配置可用路由。`
                    : `Model "${sideB.modelId}" has no active route in this project scope.`
                }}
              </p>
              <Button
                v-if="catalogModels.length > 0"
                data-testid="switch-routed-model-B"
                variant="secondary"
                size="xs"
                @click="selectFirstRoutedModel('B')"
              >
                {{ isZh ? '切换为已就绪模型' : 'Switch to Routed Model' }}
              </Button>
            </div>

            <label class="block space-y-1 text-xs">
              <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                {{ isZh ? '原生协议 (Protocol B)' : 'Wire Protocol (B)' }}
              </span>
              <select
                v-model="sideB.protocol"
                data-testid="select-protocol-b"
                class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs text-[var(--oh-ink,var(--foreground))]"
              >
                <option v-for="p in protocolOptions" :key="p.value" :value="p.value">
                  {{ p.label }}
                </option>
              </select>
            </label>

            <div class="grid grid-cols-2 gap-2">
              <label class="block space-y-1 text-xs">
                <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                  {{ isZh ? '温度 (B)' : 'Temp (B)' }}
                </span>
                <input
                  v-model.number="sideB.temperature"
                  type="number"
                  min="0"
                  max="2"
                  step="0.1"
                  class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs tabular-nums text-[var(--oh-ink,var(--foreground))]"
                />
              </label>
              <label class="block space-y-1 text-xs">
                <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                  {{ isZh ? '最大输出 (B)' : 'Max Tokens (B)' }}
                </span>
                <input
                  v-model.number="sideB.maxOutputTokens"
                  type="number"
                  min="1"
                  max="32768"
                  step="64"
                  class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs tabular-nums text-[var(--oh-ink,var(--foreground))]"
                />
              </label>
            </div>
          </div>

          <!-- Tools JSON & Replay -->
          <div class="space-y-2 border-t border-[var(--oh-border,var(--border))] pt-3.5 text-xs">
            <label class="block space-y-1">
              <span class="font-mono text-[11px] uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
                {{ isZh ? '工具定义 JSON 数组（可选）' : 'Tools JSON Array (Optional)' }}
              </span>
              <textarea
                v-model="toolsJson"
                rows="3"
                placeholder='[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]'
                class="w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] p-2 font-mono text-xs text-[var(--oh-ink,var(--foreground))]"
              />
              <p v-if="toolsJsonError" role="alert" class="text-[11px] text-[var(--oh-danger,#dc2626)]">
                {{ toolsJsonError }}
              </p>
            </label>

            <label v-if="!compareMode" class="block space-y-1">
              <span class="font-mono text-[11px] uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
                {{ isZh ? '重放请求 ID（可选）' : 'Replay Request ID (Optional)' }}
              </span>
              <input
                v-model="replayRequestId"
                type="text"
                :placeholder="isZh ? '输入已有请求 UUID 进行重放' : 'Enter existing request UUID to replay'"
                class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs tabular-nums text-[var(--oh-ink,var(--foreground))]"
              />
            </label>
          </div>
        </aside>

        <!-- CENTER PANE: Prompt Composer & Output / Independent Compare -->
        <main
          data-testid="playground-center-pane"
          :class="[
            'min-w-0 space-y-4',
            mobilePane !== 'session' ? 'hidden lg:block' : 'block'
          ]"
        >
          <!-- System & Conversation Composer -->
          <section class="space-y-3 rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] p-3.5">
            <label class="block space-y-1 text-xs">
              <span class="font-mono text-[11px] font-semibold uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
                {{ isZh ? '系统提示词' : 'System Instructions' }}
              </span>
              <textarea
                v-model="systemPrompt"
                rows="2"
                :placeholder="isZh ? '输入系统指令（可选）…' : 'Optional system instructions…'"
                class="w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] p-2.5 text-sm text-[var(--oh-ink,var(--foreground))]"
              />
            </label>

            <div class="space-y-2.5">
              <div class="flex items-center justify-between">
                <span class="font-mono text-[11px] font-semibold uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
                  {{ isZh ? `对话消息 (${messages.length})` : `Messages (${messages.length})` }}
                </span>
                <div class="flex items-center gap-1.5">
                  <Button variant="ghost" size="xs" @click="addMessage('user')">
                    {{ isZh ? '+ 用户消息' : '+ User' }}
                  </Button>
                  <Button variant="ghost" size="xs" @click="addMessage('assistant')">
                    {{ isZh ? '+ 助手消息' : '+ Assistant' }}
                  </Button>
                </div>
              </div>

              <div
                v-for="(msg, index) in messages"
                :key="msg.id"
                class="space-y-1.5 rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] p-2.5"
              >
                <div class="flex items-center justify-between gap-2">
                  <div class="flex items-center gap-2">
                    <select
                      v-model="msg.role"
                      :aria-label="isZh ? `第 ${index + 1} 条消息角色` : `Message ${index + 1} role`"
                      class="h-6 rounded border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] px-1.5 font-mono text-[11px] uppercase text-[var(--oh-ink,var(--foreground))]"
                    >
                      <option value="user">{{ isZh ? '用户 (user)' : 'user' }}</option>
                      <option value="assistant">{{ isZh ? '助手 (assistant)' : 'assistant' }}</option>
                      <option value="tool">{{ isZh ? '工具 (tool)' : 'tool' }}</option>
                    </select>
                    <input
                      v-if="msg.role === 'tool'"
                      v-model="msg.toolCallId"
                      type="text"
                      :placeholder="isZh ? '工具调用 ID' : 'Tool Call ID'"
                      class="h-6 rounded border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] px-2 font-mono text-[11px] text-[var(--oh-ink,var(--foreground))]"
                    />
                  </div>
                  <button
                    type="button"
                    class="font-mono text-xs text-[var(--oh-ink-tertiary,var(--muted-foreground))] hover:text-[var(--oh-danger,#dc2626)]"
                    @click="removeMessage(index)"
                  >
                    {{ isZh ? '移除' : 'Clear' }}
                  </button>
                </div>
                <textarea
                  v-model="msg.content"
                  :data-testid="`message-input-${index}`"
                  rows="3"
                  :placeholder="isZh ? '输入测试提示词…' : 'Enter prompt content…'"
                  class="w-full rounded border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] p-2.5 text-sm text-[var(--oh-ink,var(--foreground))]"
                />
              </div>
            </div>

            <ConsoleAsyncBanner
              v-if="quoteErrorNotice"
              data-testid="playground-quote-error-banner"
              :notice="quoteErrorNotice"
              dismissible
              @dismiss="quoteErrorNotice = null"
            />

            <div
              v-if="inlinePreviewEstimates.length > 0"
              data-testid="playground-quote-preview"
              class="rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-3 py-2 text-xs"
            >
              <div class="flex flex-wrap items-center justify-between gap-2">
                <span class="font-semibold text-[var(--oh-ink,var(--foreground))]">
                  {{
                    inlinePreviewLoading
                      ? isZh
                        ? '正在获取服务端权威报价…'
                        : 'Fetching authoritative server quote…'
                      : isZh
                        ? '服务端权威报价预览'
                        : 'Authoritative Quote Preview'
                  }}
                </span>
                <button
                  type="button"
                  class="font-mono text-[11px] text-[var(--oh-ink-tertiary,var(--muted-foreground))] hover:text-[var(--oh-ink,var(--foreground))]"
                  @click="inlinePreviewEstimates = []"
                >
                  {{ isZh ? '收起' : 'Hide' }}
                </button>
              </div>
              <div class="mt-1.5 grid gap-2" :class="inlinePreviewEstimates.length > 1 ? 'sm:grid-cols-2' : 'grid-cols-1'">
                <div
                  v-for="(est, idx) in inlinePreviewEstimates"
                  :key="`${est.sideLabel}-${idx}`"
                  class="flex flex-wrap items-center justify-between gap-2 font-mono text-[11px] tabular-nums"
                >
                  <span class="truncate text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                    {{ est.sideLabel }} · {{ est.modelId || EM_DASH }}
                    ({{ formatModelRatePerMillion(est.inputRate, est.currency) || EM_DASH }} / {{ formatModelRatePerMillion(est.outputRate, est.currency) || EM_DASH }})
                  </span>
                  <span class="font-semibold text-[var(--oh-accent-text,var(--brand-text,#d97706))]">
                    {{
                      est.quoteState === 'requires_scope'
                        ? isZh
                          ? '需作用域'
                          : 'Scope Required'
                        : est.quoteState === 'error'
                          ? isZh
                            ? '报价受限/失败'
                            : 'Quote Failed'
                        : est.rateKnown && est.maxTotalCost !== null
                          ? formatExactMoney(est.maxTotalCost, est.currency)
                          : isZh
                            ? '未定价'
                            : 'Unpriced'
                    }}
                  </span>
                </div>
              </div>
            </div>

            <div class="flex flex-wrap items-center justify-between gap-2 border-t border-[var(--oh-border,var(--border))] pt-3">
              <span class="font-mono text-[11px] text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
                {{ isZh ? '运行前将核定服务端权威报价 (⌘+Enter)' : 'Authoritative quote confirmed before run (⌘+Enter)' }}
              </span>
              <div class="flex flex-wrap items-center gap-2">
                <Button
                  data-testid="playground-preview-quote-btn"
                  variant="secondary"
                  size="sm"
                  :loading="inlinePreviewLoading"
                  :disabled="!sideA.modelId && !replayRequestId"
                  @click="previewAuthoritativeQuotes"
                >
                  {{ isZh ? '预览报价' : 'Preview Quote' }}
                </Button>
                <Button
                  data-testid="playground-run-trigger"
                  variant="primary"
                  size="sm"
                  :disabled="!canTriggerPrimaryRun"
                  @click="prepareConsentForTargets(compareMode ? ['A', 'B'] : ['A'])"
                >
                  {{
                    compareMode
                      ? isZh
                        ? '预估费用并双侧独立运行'
                        : 'Estimate & Run Both (A + B)'
                      : replayRequestId
                        ? isZh
                          ? '预估并重放请求'
                          : 'Estimate & Replay'
                        : isZh
                          ? '预估费用并运行 (⌘↵)'
                          : 'Estimate & Run (⌘↵)'
                  }}
                </Button>
              </div>
            </div>
            <div
              v-if="!hasValidKeyA"
              data-testid="playground-run-key-required-hint"
              role="status"
              class="flex flex-wrap items-center justify-between gap-2 rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-3 py-2 text-[11px] text-[var(--oh-ink-secondary,var(--muted-foreground))]"
            >
              <span>{{ t('playgroundKeys.runBlockedHint') }}</span>
              <Button
                v-if="hasActiveProjectScope && !keysLoadError"
                variant="ghost"
                size="xs"
                @click="navigateTo('/keys')"
              >
                {{ t('playgroundKeys.createProjectKeyAction') }}
              </Button>
            </div>
          </section>

          <!-- Output Area: Single or Independent Side-by-Side Compare -->
          <div :class="compareMode ? 'grid gap-4 xl:grid-cols-2' : 'space-y-4'">
            <PlaygroundRunPane
              side-key="A"
              :config="sideA"
              :session="controllerA.state"
              :compare-mode="compareMode"
              :inspected="inspectedSide === 'A'"
              :is-zh="isZh"
              :is-admin="authStore.isAdmin"
              :can-manage-byok="canManageByok"
              @run="prepareConsentForTargets(['A'])"
              @stop="controllerA.stop(scope.activeWorkspaceId.value || undefined, scope.activeProjectId.value || undefined)"
              @recover="controllerA.recoverByIdempotencyKey(scope.activeWorkspaceId.value || undefined, scope.activeProjectId.value || undefined)"
              @query-status="controllerA.queryRunStatus(scope.activeWorkspaceId.value || undefined, scope.activeProjectId.value || undefined)"
              @inspect="inspectedSide = 'A'"
              @navigate="navigateTo"
              @inject-tool-response="injectToolResultMessage"
            />

            <PlaygroundRunPane
              v-if="compareMode"
              side-key="B"
              :config="sideB"
              :session="controllerB.state"
              compare-mode
              :inspected="inspectedSide === 'B'"
              :is-zh="isZh"
              :is-admin="authStore.isAdmin"
              :can-manage-byok="canManageByok"
              @run="prepareConsentForTargets(['B'])"
              @stop="controllerB.stop(scope.activeWorkspaceId.value || undefined, scope.activeProjectId.value || undefined)"
              @recover="controllerB.recoverByIdempotencyKey(scope.activeWorkspaceId.value || undefined, scope.activeProjectId.value || undefined)"
              @query-status="controllerB.queryRunStatus(scope.activeWorkspaceId.value || undefined, scope.activeProjectId.value || undefined)"
              @inspect="inspectedSide = 'B'"
              @navigate="navigateTo"
              @inject-tool-response="injectToolResultMessage"
            />
          </div>
        </main>

        <!-- RIGHT PANE: Telemetry, Raw Request & SSE Event Inspector -->
        <aside
          data-testid="playground-right-pane"
          :class="[
            'space-y-3 rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] p-3.5',
            mobilePane !== 'inspector' ? (inspectorOpen ? 'hidden lg:block' : 'hidden') : 'block'
          ]"
        >
          <div class="flex items-center justify-between gap-2">
            <span class="font-mono text-[11px] font-semibold uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
              {{ isZh ? '运行遥测检查器' : 'Inspector' }} {{ compareMode ? `(${inspectedConfig.label})` : '' }}
            </span>
            <SegmentedControl
              v-if="compareMode"
              v-model="inspectedSide"
              size="sm"
              mono
              :aria-label="isZh ? '选择检查侧' : 'Inspected Compare Side'"
              :options="[
                { value: 'A', label: 'A' },
                { value: 'B', label: 'B' }
              ]"
            />
          </div>

          <Tabs
            v-model="inspectorTab"
            :aria-label="isZh ? '检查器标签页' : 'Inspector Tabs'"
            :items="[
              { value: 'telemetry', label: isZh ? '遥测与结算' : 'Telemetry' },
              { value: 'payload', label: isZh ? '请求载荷' : 'Raw Payload' },
              { value: 'events', label: isZh ? '事件帧' : 'SSE Frames', count: inspectedSession.sseFrames.length }
            ]"
          />

          <div v-if="inspectorTab === 'telemetry'" class="space-y-2.5 pt-1 text-xs">
            <dl class="space-y-2">
              <div class="flex items-center justify-between gap-2">
                <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">{{ isZh ? '运行 ID' : 'Run ID' }}</dt>
                <dd class="flex items-center gap-1 font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                  <span class="max-w-[165px] truncate">{{ inspectedSession.runId || EM_DASH }}</span>
                  <CopyButton v-if="inspectedSession.runId" :value="inspectedSession.runId" size="xs" />
                </dd>
              </div>
              <div class="flex items-center justify-between gap-2">
                <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">{{ isZh ? '请求 ID' : 'Request ID' }}</dt>
                <dd class="flex items-center gap-1 font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                  <span class="max-w-[165px] truncate">{{ inspectedSession.requestId || EM_DASH }}</span>
                  <CopyButton v-if="inspectedSession.requestId" :value="inspectedSession.requestId" size="xs" />
                </dd>
              </div>
              <div class="flex items-center justify-between gap-2">
                <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">{{ isZh ? '恢复标识' : 'Recovery ID' }}</dt>
                <dd class="flex items-center gap-1 font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                  <span class="max-w-[165px] truncate">{{ inspectedSession.idempotencyKey || EM_DASH }}</span>
                  <CopyButton v-if="inspectedSession.idempotencyKey" :value="inspectedSession.idempotencyKey" size="xs" />
                </dd>
              </div>
              <div class="flex items-center justify-between gap-2 border-t border-[var(--oh-border,var(--border))] pt-2">
                <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">{{ isZh ? '结算状态' : 'Settlement State' }}</dt>
                <dd class="font-mono font-semibold tabular-nums text-[var(--oh-ink,var(--foreground))]">
                  {{ formatSettlementState(inspectedSession.settlementState) }}
                </dd>
              </div>
              <div class="flex items-center justify-between gap-2">
                <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">{{ isZh ? '价格版本' : 'Price Version' }}</dt>
                <dd class="font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                  {{ inspectedSession.priceVersion || EM_DASH }}
                </dd>
              </div>
              <div class="flex items-center justify-between gap-2">
                <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">{{ isZh ? '首字延迟' : 'TTFT' }}</dt>
                <dd class="font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                  {{ formatNullableLatencyMs(inspectedSession.ttftMs) }}
                </dd>
              </div>
              <div class="flex items-center justify-between gap-2">
                <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">{{ isZh ? '总耗时' : 'Total Elapsed' }}</dt>
                <dd class="font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                  {{ formatNullableLatencyMs(inspectedSession.elapsedMs) }}
                </dd>
              </div>
              <div class="flex items-center justify-between gap-2">
                <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">{{ isZh ? '输入 Token' : 'Input Tokens' }}</dt>
                <dd class="font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                  {{ inspectedSession.inputTokens !== null ? formatNullableInteger(inspectedSession.inputTokens) : EM_DASH }}
                </dd>
              </div>
              <div class="flex items-center justify-between gap-2">
                <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">{{ isZh ? '输出 Token' : 'Output Tokens' }}</dt>
                <dd class="font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                  {{ inspectedSession.outputTokens !== null ? formatNullableInteger(inspectedSession.outputTokens) : EM_DASH }}
                </dd>
              </div>
              <div class="flex items-center justify-between gap-2">
                <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">{{ isZh ? '最新事件序号' : 'Last Event Seq' }}</dt>
                <dd class="font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                  {{ inspectedSession.lastSeq > 0 ? inspectedSession.lastSeq : EM_DASH }}
                </dd>
              </div>
            </dl>

            <div
              v-if="linkedRequestPath"
              class="border-t border-[var(--oh-border,var(--border))] pt-2.5"
            >
              <a
                :href="linkedRequestPath"
                class="oh-focus-ring rounded font-mono text-xs text-[var(--oh-accent-text,var(--brand-text,#d97706))] underline"
                @click.prevent="navigateTo(linkedRequestPath)"
              >
                {{ isZh ? '查看关联请求与账单凭证 →' : 'Open Linked Request & Receipt →' }}
              </a>
            </div>
          </div>

          <div v-else-if="inspectorTab === 'payload'" class="space-y-2.5 pt-1">
            <div class="flex items-center justify-between">
              <span class="font-mono text-[11px] text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
                {{ isZh ? '标准请求载荷 (JSON)' : 'Request Payload (JSON)' }}
              </span>
              <CopyButton :value="requestPayloadSnippet" size="xs" />
            </div>
            <pre class="max-h-96 overflow-auto rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] p-2.5 font-mono text-[11px] leading-5 whitespace-pre-wrap break-all text-[var(--oh-ink,var(--foreground))]">{{ requestPayloadSnippet }}</pre>
          </div>

          <div v-else class="space-y-2 pt-1">
            <p
              v-if="inspectedSession.sseFrames.length === 0 && inspectedSession.rawEvents.length === 0"
              class="py-6 text-center font-mono text-xs text-[var(--oh-ink-tertiary,var(--muted-foreground))]"
            >
              {{ t('playgroundDiagnostics.sseFramesEmpty') }}
            </p>
            <template v-else>
              <ul v-if="inspectedSession.sseFrames.length > 0" class="max-h-96 space-y-1.5 overflow-auto">
                <li
                  v-for="(frame, idx) in inspectedSession.sseFrames"
                  :key="`${frame.id}-${idx}`"
                  class="rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] p-2 font-mono text-[11px] tabular-nums"
                >
                  <div class="flex items-center justify-between gap-2">
                    <span class="font-semibold text-[var(--oh-ink,var(--foreground))]">
                      #{{ frame.seq ?? EM_DASH }} · {{ frame.event }}
                    </span>
                    <span class="text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
                      +{{ frame.receivedAtMs }}ms
                    </span>
                  </div>
                  <p class="mt-0.5 truncate text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                    {{ frame.summary }}
                  </p>
                  <details v-if="frame.rawData" class="mt-1">
                    <summary class="cursor-pointer text-[10px] text-[var(--oh-ink-tertiary,var(--muted-foreground))] hover:text-[var(--oh-ink,var(--foreground))]">
                      {{ t('playgroundDiagnostics.framePayloadLabel') }}
                    </summary>
                    <pre class="mt-1 max-h-32 overflow-auto rounded bg-[var(--oh-surface,var(--card))] p-1.5 text-[10px] leading-4 whitespace-pre-wrap break-all text-[var(--oh-ink,var(--foreground))]">{{ formatInspectorFrameData(frame.rawData) }}</pre>
                  </details>
                </li>
              </ul>
              <div v-if="inspectedSession.rawEvents.length > 0" class="space-y-1.5 pt-1">
                <p class="font-mono text-[11px] text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
                  {{ t('playgroundDiagnostics.rawEventsLabel') }} ({{ inspectedSession.rawEvents.length }})
                </p>
                <ul class="max-h-56 space-y-1.5 overflow-auto">
                  <li
                    v-for="(rawEv, idx) in inspectedSession.rawEvents"
                    :key="idx"
                    class="rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] p-2"
                  >
                    <pre class="max-h-32 overflow-auto font-mono text-[10px] leading-4 whitespace-pre-wrap break-all text-[var(--oh-ink,var(--foreground))]">{{ formatInspectorRawEvent(rawEv) }}</pre>
                  </li>
                </ul>
              </div>
            </template>
          </div>
        </aside>
      </div>
    </div>

    <PlaygroundCostConfirmModal
      v-model:open="consentModalOpen"
      :estimates="pendingEstimates"
      :replay-request-id="replayRequestId || null"
      :quote-loading="quoteLoading"
      :is-zh="isZh"
      :teleport-disabled="props.teleportDisabled"
      @confirm="handleConfirmAndExecute"
      @cancel="handleCancelConsent"
    />
  </AppLayout>
</template>

<style scoped>
button:focus-visible,
a:focus-visible,
input:focus-visible,
select:focus-visible,
textarea:focus-visible {
  outline: 2px solid var(--oh-focus-ring);
  outline-offset: 2px;
}
</style>
