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
import { keysAPI } from '@/api'
import { classifyConsoleError, type ConsoleAsyncNotice } from '@/features/console/asyncState'
import { catalogV2Api, type CatalogModelItem } from '@/features/console/catalogApi'
import ConsoleAsyncBanner from '@/features/console/components/ConsoleAsyncBanner.vue'
import ConsoleScopeBar from '@/features/console/components/ConsoleScopeBar.vue'
import {
  EM_DASH,
  formatNullableInteger,
  formatNullableLatencyMs
} from '@/features/console/format'
import { identityV2Api, useConsoleScope } from '@/features/console/scope'
import PlaygroundCostConfirmModal from '@/features/playground/components/PlaygroundCostConfirmModal.vue'
import PlaygroundRunPane from '@/features/playground/components/PlaygroundRunPane.vue'
import {
  buildNativePlaygroundInput,
  buildPlaygroundSubmitPayload,
  DEFAULT_MAX_OUTPUT_TOKENS,
  estimatePlaygroundRunCost
} from '@/features/playground/costEstimate'
import { createPlaygroundRunController } from '@/features/playground/runMachine'
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
}

const props = withDefaults(
  defineProps<{
    teleportDisabled?: boolean
  }>(),
  {
    teleportDisabled: false
  }
)

const { locale } = useI18n()
const isZh = computed(() => String(locale.value || '').startsWith('zh'))
const instance = getCurrentInstance()

const scope = useConsoleScope()

const mobilePane = ref<'params' | 'session' | 'inspector'>('session')
const compareMode = ref(false)
const inspectedSide = ref<'A' | 'B'>('A')
const inspectorTab = ref<'telemetry' | 'payload' | 'events'>('telemetry')

const bootstrapLoading = ref(true)
const bootstrapRefreshing = ref(false)
const bootstrapNotice = ref<ConsoleAsyncNotice | null>(null)
const confirmedNotice = ref<ConsoleAsyncNotice | null>(null)

const availableKeys = ref<KeyOption[]>([])
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

const controllerA = createPlaygroundRunController({ isZh: () => isZh.value })
const controllerB = createPlaygroundRunController({ isZh: () => isZh.value })

const consentModalOpen = ref(false)
const pendingTargets = ref<Array<'A' | 'B'>>([])
const pendingPayloads = ref<{ A?: PlaygroundSubmit; B?: PlaygroundSubmit }>({})
const pendingReplayPayload = ref<PlaygroundReplay | null>(null)
const pendingEstimates = ref<PlaygroundCostEstimate[]>([])

const protocolOptions: Array<{ value: PlaygroundProtocol; label: string }> = [
  { value: 'openai_chat', label: 'OpenAI Chat (/v1/chat/completions)' },
  { value: 'openai_responses', label: 'OpenAI Responses (/v1/responses)' },
  { value: 'anthropic_messages', label: 'Anthropic Messages (/v1/messages)' },
  { value: 'gemini_generate', label: 'Gemini GenerateContent' }
]

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

function navigateTo(path: string): void {
  const router = instance?.appContext.config.globalProperties.$router
  if (router && typeof router.push === 'function') {
    void router.push(path)
  } else if (typeof window !== 'undefined') {
    window.location.assign(path)
  }
}

async function loadKeysAndCatalog(isRefresh = false): Promise<void> {
  if (isRefresh) {
    bootstrapRefreshing.value = true
  } else {
    bootstrapLoading.value = true
  }
  bootstrapNotice.value = null
  const hadCached = availableKeys.value.length > 0 || catalogModels.value.length > 0

  try {
    await scope.refreshScopes(isRefresh)
    const wsId = scope.activeWorkspaceId.value
    const projId = scope.activeProjectId.value

    const [keysResult, modelsResult] = await Promise.allSettled([
      wsId && projId
        ? identityV2Api.listProjectKeys(wsId, projId, { limit: 100 }).then((page) =>
            (page.items || [])
              .filter((k) => !k.state || k.state === 'active')
              .map((k) => ({ id: String(k.id), name: k.name, prefix: k.prefix }))
          )
        : Promise.reject(new Error('no_v2_scope')),
      catalogV2Api.listScopedModels(wsId, projId, { limit: 100 })
    ])

    if (keysResult.status === 'fulfilled' && keysResult.value.length > 0) {
      availableKeys.value = keysResult.value
    } else {
      const v1Keys = await keysAPI.list(1, 100, { status: 'active' })
      availableKeys.value = (v1Keys.items || []).map((k) => ({
        id: String(k.id),
        name: k.name,
        prefix: k.key ? k.key.slice(0, 10) : `key_${k.id}`
      }))
    }

    if (modelsResult.status === 'fulfilled') {
      catalogModels.value = modelsResult.value
    } else {
      const pubModels = await catalogV2Api.listPublicModels({ limit: 100 }).catch((err) => {
        throw err
      })
      catalogModels.value = pubModels
    }

    if (!sideA.apiKeyId && availableKeys.value.length > 0) {
      sideA.apiKeyId = availableKeys.value[0].id
    }
    if (!sideB.apiKeyId && availableKeys.value.length > 0) {
      sideB.apiKeyId = availableKeys.value[0].id
    }
    if (!sideA.modelId && catalogModels.value.length > 0) {
      sideA.modelId = catalogModels.value[0].slug || catalogModels.value[0].id
    }
    if (!sideB.modelId && catalogModels.value.length > 0) {
      const second = catalogModels.value[1] ?? catalogModels.value[0]
      sideB.modelId = second.slug || second.id
    }
  } catch (err) {
    bootstrapNotice.value = classifyConsoleError(err, {
      hasCachedData: hadCached,
      lastSyncedAt: scope.lastSyncedAt.value,
      requiredGrantFallback: 'playground.run',
      isZh: isZh.value
    })
  } finally {
    bootstrapLoading.value = false
    bootstrapRefreshing.value = false
  }
}

function handleScopeSelect(payload: { workspaceId: string; projectId: string | null }): void {
  controllerA.reset()
  controllerB.reset()
  scope.selectWorkspaceProject(payload.workspaceId, payload.projectId)
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

function prepareConsentForTargets(targets: Array<'A' | 'B'>): void {
  toolsJsonError.value = ''
  confirmedNotice.value = null

  try {
    const estimates: PlaygroundCostEstimate[] = []
    const payloads: { A?: PlaygroundSubmit; B?: PlaygroundSubmit } = {}
    pendingReplayPayload.value = null

    if (replayRequestId.value.trim() && targets.length === 1 && targets[0] === 'A') {
      const nativeInput = buildNativePlaygroundInput(sideA.protocol, {
        systemPrompt: systemPrompt.value,
        messages: messages.value,
        temperature: sideA.temperature,
        maxOutputTokens: sideA.maxOutputTokens,
        reasoningEffort: sideA.reasoningEffort,
        toolsJson: toolsJson.value
      })
      const replayPayload: PlaygroundReplay = {
        api_key_id: sideA.apiKeyId,
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
        const submitPayload = buildPlaygroundSubmitPayload(cfg, nativeInput)
        payloads[target] = submitPayload
        estimates.push(
          estimatePlaygroundRunCost(cfg, submitPayload, findCatalogModel(cfg.modelId))
        )
        if (target === 'A') controllerA.markConsentPending(submitPayload)
        if (target === 'B') controllerB.markConsentPending(submitPayload)
      }
    }

    pendingTargets.value = targets
    pendingPayloads.value = payloads
    pendingEstimates.value = estimates
    consentModalOpen.value = true
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
  consentModalOpen.value = false
  const wsId = scope.activeWorkspaceId.value || '1'
  const projId = scope.activeProjectId.value || '1'
  const targets = [...pendingTargets.value]

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
      pendingReplayPayload.value
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
    if (!consentModalOpen.value && hasPromptInput.value && sideA.apiKeyId) {
      event.preventDefault()
      prepareConsentForTargets(compareMode.value ? ['A', 'B'] : ['A'])
    }
  } else if (event.key === 'Escape' && !consentModalOpen.value && anyStreaming.value) {
    event.preventDefault()
    const wsId = scope.activeWorkspaceId.value || '1'
    const projId = scope.activeProjectId.value || '1'
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

onMounted(() => {
  const routeQuery = instance?.appContext.config.globalProperties.$route?.query as
    | Record<string, string | undefined>
    | undefined
  if (routeQuery?.model) {
    sideA.modelId = String(routeQuery.model)
  }
  if (routeQuery?.replay_request_id) {
    replayRequestId.value = String(routeQuery.replay_request_id)
  }
  if (typeof window !== 'undefined') {
    window.addEventListener('keydown', handleKeydown)
  }
  void loadKeysAndCatalog(false)
})

onBeforeUnmount(() => {
  controllerA.reset()
  controllerB.reset()
  if (typeof window !== 'undefined') {
    window.removeEventListener('keydown', handleKeydown)
  }
})

const curlSnippet = computed(() => {
  const wsId = scope.activeWorkspaceId.value || '1'
  const projId = scope.activeProjectId.value || '1'
  const payload = inspectedSession.value.submittedPayload
  const body = payload ? JSON.stringify(payload, null, 2) : '{}'
  return [
    `curl -N -X POST "${typeof window !== 'undefined' ? window.location.origin : ''}/api/v2/workspaces/${wsId}/projects/${projId}/playground/runs" \\`,
    `  -H "Content-Type: application/json" \\`,
    `  -H "Accept: text/event-stream" \\`,
    `  -H "Idempotency-Key: ${inspectedSession.value.idempotencyKey || '<uuid>'}" \\`,
    `  -d '${body}'`
  ].join('\n')
})
</script>

<template>
  <AppLayout>
    <div data-testid="playground-workbench" class="w-full space-y-4">
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
              Playground
            </h1>
            <Badge tone="accent" variant="subtle" mono>BE-13 · v2</Badge>
            <Badge tone="neutral" variant="outline" mono>text.chat</Badge>
          </div>
          <p class="mt-0.5 text-xs text-[var(--oh-ink-secondary,var(--muted-foreground))]">
            {{
              isZh
                ? '原生协议流式调试台 · 每次调用独立预算冻结 · 支持双模型左右独立对比与断线幂等恢复'
                : 'Native protocol workbench · Separate budget holds · Independent side-by-side compare & idempotent disconnect recovery'
            }}
          </p>
        </div>

        <div class="flex flex-wrap items-center gap-2">
          <SegmentedControl
            :model-value="compareMode ? 'compare' : 'single'"
            size="sm"
            mono
            aria-label="Playground Mode"
            :options="[
              { value: 'single', label: isZh ? '单模型' : 'Single' },
              { value: 'compare', label: isZh ? '左右对比' : 'Compare A/B' }
            ]"
            @update:model-value="(v) => (compareMode = v === 'compare')"
          />
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
          aria-label="Mobile Playground Pane"
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
        <Skeleton height="420px" />
        <Skeleton height="420px" />
        <Skeleton height="420px" />
      </div>

      <!-- 3-Pane Desktop Workbench -->
      <div
        v-else
        class="grid items-start gap-4 lg:grid-cols-[280px_minmax(0,1fr)_340px]"
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
                {{ compareMode ? 'Side A Config' : 'Run Config' }}
              </span>
              <button
                v-if="availableKeys.length === 0"
                type="button"
                class="oh-focus-ring rounded font-mono text-[11px] text-[var(--oh-accent-text,var(--brand-text,#d97706))] underline"
                @click="navigateTo('/keys')"
              >
                {{ isZh ? '去创建密钥' : '+ Create Key' }}
              </button>
            </div>

            <label class="block space-y-1 text-xs">
              <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                {{ isZh ? '下游 API 密钥 (Key A)' : 'Downstream API Key (A)' }}
              </span>
              <select
                v-if="availableKeys.length > 0"
                v-model="sideA.apiKeyId"
                data-testid="select-key-a"
                class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs tabular-nums text-[var(--oh-ink,var(--foreground))]"
              >
                <option v-for="k in availableKeys" :key="k.id" :value="k.id">
                  {{ k.name }} (#{{ k.id }} · {{ k.prefix }})
                </option>
              </select>
              <input
                v-else
                v-model="sideA.apiKeyId"
                data-testid="input-key-a"
                type="text"
                :placeholder="isZh ? '输入数字密钥 ID (如 12)' : 'Numeric Key ID (e.g. 12)'"
                class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2.5 font-mono text-xs tabular-nums text-[var(--oh-ink,var(--foreground))]"
              />
            </label>

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
                v-if="filteredModels.length > 0"
                v-model="sideA.modelId"
                data-testid="select-model-a"
                class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs text-[var(--oh-ink,var(--foreground))]"
              >
                <option v-for="m in filteredModels" :key="m.id" :value="m.slug || m.id">
                  {{ m.displayName }} ({{ m.slug }})
                </option>
              </select>
              <input
                v-model="sideA.modelId"
                data-testid="input-model-a"
                type="text"
                :placeholder="isZh ? '输入已路由模型 ID' : 'Enter routed model slug/ID'"
                class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2.5 font-mono text-xs text-[var(--oh-ink,var(--foreground))]"
              />
            </label>

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
                <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Temp</span>
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
                <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Max Tokens</span>
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
                {{ isZh ? '推理强度 (Reasoning)' : 'Reasoning Effort' }}
              </span>
              <select
                v-model="sideA.reasoningEffort"
                class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs text-[var(--oh-ink,var(--foreground))]"
              >
                <option value="">Default</option>
                <option value="low">Low</option>
                <option value="medium">Medium</option>
                <option value="high">High</option>
              </select>
            </label>

            <div class="space-y-1.5 pt-1 text-xs">
              <label class="flex items-center gap-2 text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                <input v-model="sideA.stream" type="checkbox" class="h-3.5 w-3.5" />
                <span>SSE Stream</span>
              </label>
              <label class="flex items-center gap-2 text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                <input v-model="sideA.includeRawEvents" :disabled="!sideA.stream" type="checkbox" class="h-3.5 w-3.5" />
                <span>Raw frames (playground.raw)</span>
              </label>
              <label class="flex items-center gap-2 text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                <input v-model="sideA.retainTextPreview" type="checkbox" class="h-3.5 w-3.5" />
                <span>30s Ephemeral Text Preview</span>
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
              Side B Config (Independent)
            </span>

            <label class="block space-y-1 text-xs">
              <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                {{ isZh ? '下游 API 密钥 (Key B)' : 'Downstream API Key (B)' }}
              </span>
              <select
                v-if="availableKeys.length > 0"
                v-model="sideB.apiKeyId"
                data-testid="select-key-b"
                class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs tabular-nums text-[var(--oh-ink,var(--foreground))]"
              >
                <option v-for="k in availableKeys" :key="k.id" :value="k.id">
                  {{ k.name }} (#{{ k.id }} · {{ k.prefix }})
                </option>
              </select>
              <input
                v-else
                v-model="sideB.apiKeyId"
                data-testid="input-key-b"
                type="text"
                :placeholder="isZh ? '输入数字密钥 ID' : 'Numeric Key ID'"
                class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2.5 font-mono text-xs tabular-nums text-[var(--oh-ink,var(--foreground))]"
              />
            </label>

            <label class="block space-y-1 text-xs">
              <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                {{ isZh ? '模型 ID (Model B)' : 'Model ID (B)' }}
              </span>
              <select
                v-if="filteredModels.length > 0"
                v-model="sideB.modelId"
                data-testid="select-model-b"
                class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs text-[var(--oh-ink,var(--foreground))]"
              >
                <option v-for="m in filteredModels" :key="m.id" :value="m.slug || m.id">
                  {{ m.displayName }} ({{ m.slug }})
                </option>
              </select>
              <input
                v-model="sideB.modelId"
                data-testid="input-model-b"
                type="text"
                :placeholder="isZh ? '输入模型 B ID' : 'Enter Model B slug/ID'"
                class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2.5 font-mono text-xs text-[var(--oh-ink,var(--foreground))]"
              />
            </label>

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
                <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Temp (B)</span>
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
                <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Max Tokens (B)</span>
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
                Tools JSON Array (Optional)
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
                Replay Request ID (Optional)
              </span>
              <input
                v-model="replayRequestId"
                type="text"
                :placeholder="isZh ? '输入已有 request UUID 重放' : 'Request UUID for requests.replay'"
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
                System Instructions
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
                  Messages ({{ messages.length }})
                </span>
                <div class="flex items-center gap-1.5">
                  <Button variant="ghost" size="xs" @click="addMessage('user')">+ User</Button>
                  <Button variant="ghost" size="xs" @click="addMessage('assistant')">+ Assistant</Button>
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
                      :aria-label="`Message ${index + 1} role`"
                      class="h-6 rounded border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] px-1.5 font-mono text-[11px] uppercase text-[var(--oh-ink,var(--foreground))]"
                    >
                      <option value="user">user</option>
                      <option value="assistant">assistant</option>
                      <option value="tool">tool</option>
                    </select>
                    <input
                      v-if="msg.role === 'tool'"
                      v-model="msg.toolCallId"
                      type="text"
                      placeholder="tool_call_id"
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

            <div class="flex flex-wrap items-center justify-between gap-2 border-t border-[var(--oh-border,var(--border))] pt-3">
              <span class="font-mono text-[11px] text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
                {{ isZh ? '运行前必须确认费用预估 (⌘+Enter)' : 'Cost estimate confirmation required before run (⌘+Enter)' }}
              </span>
              <div class="flex flex-wrap items-center gap-2">
                <Button
                  data-testid="playground-run-trigger"
                  variant="primary"
                  size="sm"
                  :disabled="
                    !hasPromptInput ||
                    !sideA.apiKeyId ||
                    (!sideA.modelId && !replayRequestId) ||
                    (compareMode && (!sideB.apiKeyId || !sideB.modelId))
                  "
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
              @run="prepareConsentForTargets(['A'])"
              @stop="controllerA.stop(scope.activeWorkspaceId.value || '1', scope.activeProjectId.value || '1')"
              @recover="controllerA.recoverByIdempotencyKey(scope.activeWorkspaceId.value || '1', scope.activeProjectId.value || '1')"
              @query-status="controllerA.queryRunStatus(scope.activeWorkspaceId.value || '1', scope.activeProjectId.value || '1')"
              @inspect="inspectedSide = 'A'"
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
              @run="prepareConsentForTargets(['B'])"
              @stop="controllerB.stop(scope.activeWorkspaceId.value || '1', scope.activeProjectId.value || '1')"
              @recover="controllerB.recoverByIdempotencyKey(scope.activeWorkspaceId.value || '1', scope.activeProjectId.value || '1')"
              @query-status="controllerB.queryRunStatus(scope.activeWorkspaceId.value || '1', scope.activeProjectId.value || '1')"
              @inspect="inspectedSide = 'B'"
              @inject-tool-response="injectToolResultMessage"
            />
          </div>
        </main>

        <!-- RIGHT PANE: Telemetry, Raw Request & SSE Event Inspector -->
        <aside
          data-testid="playground-right-pane"
          :class="[
            'space-y-3 rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] p-3.5',
            mobilePane !== 'inspector' ? 'hidden lg:block' : 'block'
          ]"
        >
          <div class="flex items-center justify-between gap-2">
            <span class="font-mono text-[11px] font-semibold uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
              Inspector {{ compareMode ? `(${inspectedConfig.label})` : '' }}
            </span>
            <SegmentedControl
              v-if="compareMode"
              v-model="inspectedSide"
              size="sm"
              mono
              aria-label="Inspected Compare Side"
              :options="[
                { value: 'A', label: 'A' },
                { value: 'B', label: 'B' }
              ]"
            />
          </div>

          <Tabs
            v-model="inspectorTab"
            aria-label="Inspector Tabs"
            :items="[
              { value: 'telemetry', label: isZh ? '遥测与结算' : 'Telemetry' },
              { value: 'payload', label: isZh ? '请求载荷' : 'Raw Payload' },
              { value: 'events', label: 'SSE', count: inspectedSession.sseFrames.length }
            ]"
          />

          <div v-if="inspectorTab === 'telemetry'" class="space-y-2.5 pt-1 text-xs">
            <dl class="space-y-2">
              <div class="flex items-center justify-between gap-2">
                <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Run ID</dt>
                <dd class="flex items-center gap-1 font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                  <span class="max-w-[165px] truncate">{{ inspectedSession.runId || EM_DASH }}</span>
                  <CopyButton v-if="inspectedSession.runId" :value="inspectedSession.runId" size="xs" />
                </dd>
              </div>
              <div class="flex items-center justify-between gap-2">
                <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Request ID</dt>
                <dd class="flex items-center gap-1 font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                  <span class="max-w-[165px] truncate">{{ inspectedSession.requestId || EM_DASH }}</span>
                  <CopyButton v-if="inspectedSession.requestId" :value="inspectedSession.requestId" size="xs" />
                </dd>
              </div>
              <div class="flex items-center justify-between gap-2">
                <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Idempotency-Key</dt>
                <dd class="flex items-center gap-1 font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                  <span class="max-w-[165px] truncate">{{ inspectedSession.idempotencyKey || EM_DASH }}</span>
                  <CopyButton v-if="inspectedSession.idempotencyKey" :value="inspectedSession.idempotencyKey" size="xs" />
                </dd>
              </div>
              <div class="flex items-center justify-between gap-2 border-t border-[var(--oh-border,var(--border))] pt-2">
                <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Settlement State</dt>
                <dd class="font-mono font-semibold tabular-nums text-[var(--oh-ink,var(--foreground))]">
                  {{ inspectedSession.settlementState || EM_DASH }}
                </dd>
              </div>
              <div class="flex items-center justify-between gap-2">
                <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Price Version</dt>
                <dd class="font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                  {{ inspectedSession.priceVersion || EM_DASH }}
                </dd>
              </div>
              <div class="flex items-center justify-between gap-2">
                <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">TTFT</dt>
                <dd class="font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                  {{ formatNullableLatencyMs(inspectedSession.ttftMs) }}
                </dd>
              </div>
              <div class="flex items-center justify-between gap-2">
                <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Total Elapsed</dt>
                <dd class="font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                  {{ formatNullableLatencyMs(inspectedSession.elapsedMs) }}
                </dd>
              </div>
              <div class="flex items-center justify-between gap-2">
                <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Input Tokens</dt>
                <dd class="font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                  {{ inspectedSession.inputTokens !== null ? formatNullableInteger(inspectedSession.inputTokens) : EM_DASH }}
                </dd>
              </div>
              <div class="flex items-center justify-between gap-2">
                <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Output Tokens</dt>
                <dd class="font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                  {{ inspectedSession.outputTokens !== null ? formatNullableInteger(inspectedSession.outputTokens) : EM_DASH }}
                </dd>
              </div>
              <div class="flex items-center justify-between gap-2">
                <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Last SSE Seq</dt>
                <dd class="font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                  {{ inspectedSession.lastSeq > 0 ? inspectedSession.lastSeq : EM_DASH }}
                </dd>
              </div>
            </dl>

            <div
              v-if="inspectedSession.detailUrl"
              class="border-t border-[var(--oh-border,var(--border))] pt-2.5"
            >
              <a
                :href="inspectedSession.detailUrl"
                class="oh-focus-ring rounded font-mono text-xs text-[var(--oh-accent-text,var(--brand-text,#d97706))] underline"
              >
                {{ isZh ? '查看关联请求与账单凭证 →' : 'Open Linked Request & Receipt →' }}
              </a>
            </div>
          </div>

          <div v-else-if="inspectorTab === 'payload'" class="space-y-2.5 pt-1">
            <div class="flex items-center justify-between">
              <span class="font-mono text-[11px] text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
                cURL / JSON
              </span>
              <CopyButton :value="curlSnippet" size="xs" />
            </div>
            <pre class="max-h-96 overflow-auto rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] p-2.5 font-mono text-[11px] leading-5 whitespace-pre-wrap break-all text-[var(--oh-ink,var(--foreground))]">{{ curlSnippet }}</pre>
          </div>

          <div v-else class="space-y-2 pt-1">
            <p
              v-if="inspectedSession.sseFrames.length === 0"
              class="py-6 text-center font-mono text-xs text-[var(--oh-ink-tertiary,var(--muted-foreground))]"
            >
              {{ isZh ? '暂无 SSE 事件帧 (—)' : 'No SSE frames recorded yet (—)' }}
            </p>
            <ul v-else class="max-h-96 space-y-1.5 overflow-auto">
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
              </li>
            </ul>
          </div>
        </aside>
      </div>
    </div>

    <PlaygroundCostConfirmModal
      v-model:open="consentModalOpen"
      :estimates="pendingEstimates"
      :replay-request-id="replayRequestId || null"
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
