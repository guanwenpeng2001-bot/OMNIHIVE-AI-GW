<script setup lang="ts">
import { computed, getCurrentInstance, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import { keysAPI } from '@/api'
import { classifyConsoleError, type ConsoleAsyncNotice } from '@/features/console/asyncState'
import { catalogApi, type CatalogModelItem } from '@/features/console/catalogApi'
import ConsoleAsyncBanner from '@/features/console/components/ConsoleAsyncBanner.vue'
import { EM_DASH, formatNullableInteger } from '@/features/console/format'
import { identityApi, useConsoleScope } from '@/features/console/scope'
import { tasksApi } from '@/features/tasks/api'
import TaskCreateModal from '@/features/tasks/components/TaskCreateModal.vue'
import TaskDetailDrawer, { type ArtifactPreviewEntry } from '@/features/tasks/components/TaskDetailDrawer.vue'
import TaskPaidRetryModal from '@/features/tasks/components/TaskPaidRetryModal.vue'
import {
  formatTaskKindLabel,
  formatTaskSettlementLabel,
  formatTaskStageLabel,
  formatTaskStateLabel,
  inferArtifactExtension,
  inferArtifactMediaCategory,
  resolveHonestTaskProgress,
  taskKindTone,
  taskStateTone
} from '@/features/tasks/progressRules'
import type {
  AsyncTaskKind,
  ImageTaskArtifact,
  ImageTaskCreateInput,
  ImageTaskDetail,
  ImageTaskItem,
  ImageTaskSummary
} from '@/features/tasks/types'

const props = withDefaults(
  defineProps<{
    teleportDisabled?: boolean
    nowMs?: number
  }>(),
  {
    teleportDisabled: false,
    nowMs: undefined
  }
)

const { locale } = useI18n()
const isZh = computed(() => String(locale?.value || '').startsWith('zh'))
const instance = getCurrentInstance()
const scope = useConsoleScope()

const loading = ref(true)
const refreshing = ref(false)
const tasks = ref<ImageTaskSummary[]>([])
const nextCursor = ref<string | null>(null)
const cursorStack = ref<string[]>([])
const activeCursor = ref<string | undefined>(undefined)
const asOf = ref('')

const pageNotice = ref<ConsoleAsyncNotice | null>(null)
const confirmedNotice = ref<ConsoleAsyncNotice | null>(null)
const canReadTasks = ref(true)
const canCreateTasks = ref(true)
const canReadKeys = ref(true)

const activeScopeIds = computed<{ wsId: string; projId: string } | null>(() => {
  const wsId = scope.activeWorkspaceId.value
  const projId = scope.activeProjectId.value
  return wsId && projId ? { wsId, projId } : null
})

const filters = reactive({
  kind: '',
  state: '',
  modelId: '',
  from: '',
  to: ''
})

const hasActiveFilters = computed(() =>
  Boolean(filters.kind || filters.state || filters.modelId.trim() || filters.from || filters.to)
)

const drawerOpen = ref(false)
const drawerLoading = ref(false)
const drawerNotice = ref<ConsoleAsyncNotice | null>(null)
const selectedDetail = ref<ImageTaskDetail | null>(null)
const selectedItems = ref<ImageTaskItem[]>([])
const actionBusyId = ref('')
const artifactPreviews = ref<Record<string, ArtifactPreviewEntry>>({})

const retryModalOpen = ref(false)
const retryPreselectedIds = ref<string[]>([])
const retrySubmitting = ref(false)

const createModalOpen = ref(false)
const createSubmitting = ref(false)
const createModelsLoading = ref(false)
const createActiveKind = ref<AsyncTaskKind>('image')
const createKeys = ref<Array<{ id: string; name: string; prefix?: string; scopeLabel?: string }>>([])
const createModels = ref<CatalogModelItem[]>([])

function revokeAllArtifactPreviews(): void {
  if (typeof URL !== 'undefined' && typeof URL.revokeObjectURL === 'function') {
    for (const previewEntry of Object.values(artifactPreviews.value)) {
      URL.revokeObjectURL(previewEntry.url)
    }
  }
  artifactPreviews.value = {}
}

watch(drawerOpen, (isOpen) => {
  if (!isOpen) revokeAllArtifactPreviews()
})

onBeforeUnmount(() => {
  revokeAllArtifactPreviews()
})

const summaryCounts = computed(() => {
  let active = 0
  let succeeded = 0
  let failedOrCanceled = 0
  let unknown = 0
  for (const t of tasks.value) {
    if (t.state === 'queued' || t.state === 'running') active++
    else if (t.state === 'succeeded') succeeded++
    else if (t.state === 'failed' || t.state === 'canceled') failedOrCanceled++
    else if (t.state === 'unknown') unknown++
  }
  return { total: tasks.value.length, active, succeeded, failedOrCanceled, unknown }
})

function navigateTo(path: string): void {
  const router = instance?.appContext.config.globalProperties.$router
  if (router && typeof router.push === 'function') {
    void router.push(path)
  } else if (typeof window !== 'undefined') {
    window.location.assign(path)
  }
}

async function refreshTaskAccess(wsId: string, projId: string): Promise<boolean> {
  if (typeof identityApi.getProjectAccess !== 'function') {
    canReadTasks.value = true
    canCreateTasks.value = true
    canReadKeys.value = true
    return true
  }
  try {
    const access = await identityApi.getProjectAccess(wsId, projId)
    const actions = Array.isArray(access?.actions) ? access.actions : []
    const allowAll = actions.includes('*')
    canReadTasks.value = allowAll || actions.includes('tasks.read')
    canCreateTasks.value = allowAll || actions.includes('tasks.create')
    canReadKeys.value = allowAll || actions.includes('keys.read')
    return canReadTasks.value
  } catch (err) {
    const status = (err as { status?: number; response?: { status?: number } })?.status ??
      (err as { response?: { status?: number } })?.response?.status
    if (status === 403) {
      canReadTasks.value = false
      canCreateTasks.value = false
      canReadKeys.value = false
      return false
    }
    canReadTasks.value = true
    canCreateTasks.value = true
    canReadKeys.value = true
    return true
  }
}

async function loadTasks(options?: { refresh?: boolean; cursor?: string }): Promise<void> {
  const isRefresh = Boolean(options?.refresh)
  if (isRefresh && tasks.value.length > 0) {
    refreshing.value = true
  } else {
    loading.value = true
  }
  pageNotice.value = null
  const hadCached = tasks.value.length > 0

  try {
    await scope.refreshScopes(false)
    const ids = activeScopeIds.value
    if (!ids) {
      tasks.value = []
      nextCursor.value = null
      canReadTasks.value = false
      canCreateTasks.value = false
      canReadKeys.value = false
      return
    }
    const { wsId, projId } = ids
    const canRead = await refreshTaskAccess(wsId, projId)
    if (!canRead) {
      tasks.value = []
      nextCursor.value = null
      pageNotice.value = classifyConsoleError(
        { status: 403, reason: 'INSUFFICIENT_GRANT' },
        { requiredGrantFallback: 'tasks.read', isZh: isZh.value }
      )
      return
    }
    const page = await tasksApi.listTasks(wsId, projId, {
      cursor: options?.cursor,
      limit: 25,
      kind: filters.kind ? [filters.kind] : undefined,
      state: filters.state ? [filters.state] : undefined,
      model_id: filters.modelId.trim() ? [filters.modelId.trim()] : undefined,
      from: filters.from ? new Date(filters.from).toISOString() : undefined,
      to: filters.to ? new Date(filters.to).toISOString() : undefined
    })
    tasks.value = page.items ?? []
    nextCursor.value = page.next_cursor ?? null
    asOf.value = page.as_of || new Date().toISOString()
  } catch (err) {
    const status = (err as { status?: number; response?: { status?: number } })?.status ??
      (err as { response?: { status?: number } })?.response?.status
    if (status === 403) {
      canReadTasks.value = false
      canCreateTasks.value = false
      tasks.value = []
    }
    pageNotice.value = classifyConsoleError(err, {
      hasCachedData: hadCached,
      lastSyncedAt: asOf.value,
      requiredGrantFallback: 'tasks.read',
      isZh: isZh.value
    })
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

function applyFilters(): void {
  cursorStack.value = []
  activeCursor.value = undefined
  void loadTasks({ refresh: false })
}

function clearFilters(): void {
  filters.kind = ''
  filters.state = ''
  filters.modelId = ''
  filters.from = ''
  filters.to = ''
  applyFilters()
}

function goNextPage(): void {
  if (!nextCursor.value) return
  cursorStack.value.push(activeCursor.value || '')
  activeCursor.value = nextCursor.value
  void loadTasks({ cursor: nextCursor.value })
}

function goPrevPage(): void {
  if (cursorStack.value.length === 0) return
  const prev = cursorStack.value.pop() || undefined
  activeCursor.value = prev
  void loadTasks({ cursor: prev })
}

async function openTaskDetail(taskId: string): Promise<void> {
  const ids = activeScopeIds.value
  if (!ids || !canReadTasks.value) return
  if (selectedDetail.value?.id !== taskId) {
    revokeAllArtifactPreviews()
  }
  drawerOpen.value = true
  drawerLoading.value = true
  drawerNotice.value = null
  const { wsId, projId } = ids

  try {
    const [detail, itemPage] = await Promise.all([
      tasksApi.getTaskDetail(wsId, projId, taskId),
      tasksApi.listTaskItems(wsId, projId, taskId, { limit: 100 }).catch(() => ({
        items: [],
        next_cursor: null,
        as_of: ''
      }))
    ])
    selectedDetail.value = detail
    selectedItems.value = itemPage.items ?? []
  } catch (err) {
    drawerNotice.value = classifyConsoleError(err, {
      hasCachedData: Boolean(selectedDetail.value && selectedDetail.value.id === taskId),
      requiredGrantFallback: 'tasks.read',
      isZh: isZh.value
    })
  } finally {
    drawerLoading.value = false
  }
}

async function handleCancelTask(taskId: string): Promise<void> {
  const ids = activeScopeIds.value
  if (!ids) return
  actionBusyId.value = `cancel:${taskId}`
  drawerNotice.value = null
  const { wsId, projId } = ids
  try {
    const res = await tasksApi.cancelTask(wsId, projId, taskId)
    confirmedNotice.value = {
      kind: 'confirmed',
      title: isZh.value
        ? `任务 ${res.task_id} 取消请求已受理 (${res.cancel_state})`
        : `Cancellation accepted for ${res.task_id} (${res.cancel_state})`
    }
    await Promise.all([openTaskDetail(taskId), loadTasks({ refresh: true, cursor: activeCursor.value })])
  } catch (err) {
    drawerNotice.value = classifyConsoleError(err, {
      requiredGrantFallback: 'tasks.cancel',
      isZh: isZh.value
    })
  } finally {
    actionBusyId.value = ''
  }
}

function handleOpenPaidRetry(payload: { taskId: string; preselectedItemIds: string[] }): void {
  retryPreselectedIds.value = payload.preselectedItemIds
  retryModalOpen.value = true
}

async function handleConfirmPaidRetry(payload: { itemIds: string[] }): Promise<void> {
  const ids = activeScopeIds.value
  if (!selectedDetail.value || !ids) return
  retrySubmitting.value = true
  drawerNotice.value = null
  const { wsId, projId } = ids
  try {
    const result = await tasksApi.retryTask(wsId, projId, selectedDetail.value.id, {
      item_ids: payload.itemIds.length > 0 ? payload.itemIds : undefined,
      funding_acknowledged: true
    })
    retryModalOpen.value = false
    confirmedNotice.value = {
      kind: 'confirmed',
      title: isZh.value
        ? `付费重试已创建新任务 ${result.task_id}（源任务 ${result.parent_task_id}）`
        : `Paid retry created new task ${result.task_id} (parent ${result.parent_task_id})`,
      requestId: result.new_request_id
    }
    await Promise.all([
      openTaskDetail(result.task_id),
      loadTasks({ refresh: true, cursor: activeCursor.value })
    ])
  } catch (err) {
    retryModalOpen.value = false
    drawerNotice.value = classifyConsoleError(err, {
      requiredGrantFallback: 'tasks.retry',
      isZh: isZh.value
    })
  } finally {
    retrySubmitting.value = false
  }
}

async function handleDownloadArtifact(artifact: ImageTaskArtifact): Promise<void> {
  const ids = activeScopeIds.value
  if (!selectedDetail.value || !ids) return
  actionBusyId.value = `download:${artifact.id}`
  drawerNotice.value = null
  const { wsId, projId } = ids
  const taskId = selectedDetail.value.id

  try {
    const ticket = await tasksApi.issueArtifactDownloadTicket(wsId, projId, taskId, artifact.id)
    const blob = await tasksApi.fetchArtifactContentBlob(wsId, projId, taskId, artifact.id, ticket)
    if (typeof window !== 'undefined' && typeof URL.createObjectURL === 'function') {
      const blobUrl = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      const ext = inferArtifactExtension(
        ticket.content_type || artifact.content_type,
        selectedDetail.value.kind
      )
      anchor.href = blobUrl
      anchor.download = `${taskId}-${artifact.id}.${ext}`
      document.body.appendChild(anchor)
      anchor.click()
      anchor.remove()
      setTimeout(() => URL.revokeObjectURL(blobUrl), 2000)
    }
    confirmedNotice.value = {
      kind: 'confirmed',
      title: isZh.value
        ? `产物 ${artifact.id} 已通过受控单次票据下载`
        : `Artifact ${artifact.id} downloaded via scope-bound ticket`
    }
  } catch (err) {
    drawerNotice.value = classifyConsoleError(err, {
      requiredGrantFallback: 'tasks.artifacts.read',
      isZh: isZh.value
    })
    await openTaskDetail(taskId)
  } finally {
    actionBusyId.value = ''
  }
}

async function handlePreviewArtifact(artifact: ImageTaskArtifact): Promise<void> {
  const ids = activeScopeIds.value
  if (!selectedDetail.value || !ids) return
  actionBusyId.value = `preview:${artifact.id}`
  drawerNotice.value = null
  const { wsId, projId } = ids
  const taskId = selectedDetail.value.id
  const taskKind = selectedDetail.value.kind

  try {
    const ticket = await tasksApi.issueArtifactDownloadTicket(wsId, projId, taskId, artifact.id)
    const blob = await tasksApi.fetchArtifactContentBlob(wsId, projId, taskId, artifact.id, ticket)
    if (typeof window !== 'undefined' && typeof URL.createObjectURL === 'function') {
      const existing = artifactPreviews.value[artifact.id]
      if (existing) URL.revokeObjectURL(existing.url)
      const resolvedContentType = ticket.content_type || artifact.content_type || blob.type || ''
      artifactPreviews.value = {
        ...artifactPreviews.value,
        [artifact.id]: {
          url: URL.createObjectURL(blob),
          contentType: resolvedContentType,
          mediaCategory: inferArtifactMediaCategory(resolvedContentType, taskKind)
        }
      }
    }
  } catch (err) {
    drawerNotice.value = classifyConsoleError(err, {
      requiredGrantFallback: 'tasks.artifacts.read',
      isZh: isZh.value
    })
    await openTaskDetail(taskId)
  } finally {
    actionBusyId.value = ''
  }
}

async function loadModelsForKind(kind: AsyncTaskKind): Promise<void> {
  const ids = activeScopeIds.value
  if (!ids) {
    createModels.value = []
    return
  }
  const { wsId, projId } = ids
  createModelsLoading.value = true
  try {
    const scopedModels = await catalogApi.listScopedModels(wsId, projId, {
      modality: [kind],
      limit: 50
    })
    if (createActiveKind.value === kind) {
      createModels.value = scopedModels
    }
  } catch {
    if (createActiveKind.value === kind) {
      createModels.value = []
    }
  } finally {
    if (createActiveKind.value === kind) {
      createModelsLoading.value = false
    }
  }
}

function handleCreateKindChange(kind: AsyncTaskKind): void {
  createActiveKind.value = kind
  void loadModelsForKind(kind)
}

async function handleRevokeArtifact(artifact: ImageTaskArtifact): Promise<void> {
  const ids = activeScopeIds.value
  if (!selectedDetail.value || !ids) return
  actionBusyId.value = `revoke:${artifact.id}`
  drawerNotice.value = null
  const { wsId, projId } = ids
  const taskId = selectedDetail.value.id

  try {
    await tasksApi.revokeArtifact(wsId, projId, taskId, artifact.id)
    confirmedNotice.value = {
      kind: 'confirmed',
      title: isZh.value
        ? `产物 ${artifact.id} 已撤销，关联下载票据立即失效`
        : `Artifact ${artifact.id} revoked; outstanding tickets invalidated`
    }
    await openTaskDetail(taskId)
  } catch (err) {
    drawerNotice.value = classifyConsoleError(err, {
      requiredGrantFallback: 'tasks.artifacts.revoke',
      isZh: isZh.value
    })
  } finally {
    actionBusyId.value = ''
  }
}

async function openCreateModal(): Promise<void> {
  const ids = activeScopeIds.value
  if (!ids || !canCreateTasks.value) return
  createModalOpen.value = true
  const { wsId, projId } = ids
  createModelsLoading.value = true
  const [projKeysRes, userKeysRes, mRes] = await Promise.allSettled([
    canReadKeys.value && typeof identityApi.listProjectKeys === 'function'
      ? identityApi.listProjectKeys(wsId, projId, { limit: 100 })
      : Promise.resolve({ items: [] }),
    canReadKeys.value ? keysAPI.list(1, 50, { status: 'active' }) : Promise.resolve({ items: [] }),
    catalogApi.listScopedModels(wsId, projId, { modality: [createActiveKind.value], limit: 50 })
  ])
  createModelsLoading.value = false

  const merged = new Map<string, { id: string; name: string; prefix?: string; scopeLabel?: string }>()
  if (projKeysRes.status === 'fulfilled') {
    for (const k of projKeysRes.value.items || []) {
      if (!k.state || k.state === 'active') {
        merged.set(String(k.id), {
          id: String(k.id),
          name: k.name,
          prefix: k.prefix,
          scopeLabel: isZh.value ? '项目密钥' : 'Project'
        })
      }
    }
  }
  if (userKeysRes.status === 'fulfilled') {
    for (const k of userKeysRes.value.items || []) {
      const id = String(k.id)
      if (!merged.has(id)) {
        merged.set(id, {
          id,
          name: k.name,
          prefix: k.key ? k.key.slice(0, 10) : undefined,
          scopeLabel: isZh.value ? '账户密钥' : 'Account'
        })
      }
    }
  }
  createKeys.value = Array.from(merged.values())

  if (mRes.status === 'fulfilled') {
    createModels.value = mRes.value
  }
}

function handleNavigateToKeysFromCreate(): void {
  createModalOpen.value = false
  navigateTo('/keys')
}

async function handleCreateTask(payload: ImageTaskCreateInput): Promise<void> {
  const ids = activeScopeIds.value
  if (!ids || !canCreateTasks.value) return
  createSubmitting.value = true
  const { wsId, projId } = ids
  try {
    const created = await tasksApi.createTask(wsId, projId, payload)
    createModalOpen.value = false
    const createdKindLabel = formatTaskKindLabel(created.kind || payload.kind, isZh.value)
    confirmedNotice.value = {
      kind: 'confirmed',
      title: isZh.value
        ? `${createdKindLabel}任务 ${created.id} 已提交准入`
        : `${createdKindLabel} task ${created.id} accepted`
    }
    revokeAllArtifactPreviews()
    selectedDetail.value = created
    selectedItems.value = []
    drawerOpen.value = true
    await loadTasks({ refresh: true })
  } catch (err) {
    createModalOpen.value = false
    pageNotice.value = classifyConsoleError(err, {
      requiredGrantFallback: 'tasks.create',
      isZh: isZh.value
    })
  } finally {
    createSubmitting.value = false
  }
}

onMounted(() => {
  void loadTasks({ refresh: false })
})
</script>

<template>
  <AppLayout>
    <div data-testid="task-center-view" class="w-full space-y-4">
      <!-- Header -->
      <header class="flex flex-wrap items-center justify-between gap-3 rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] px-4 py-3">
        <div>
          <div class="flex flex-wrap items-center gap-2">
            <h1 class="text-lg font-semibold tracking-tight text-[var(--oh-ink,var(--foreground))]">
              {{ isZh ? '任务中心' : 'Task Center' }}
            </h1>
          </div>
          <p class="mt-0.5 text-xs text-[var(--oh-ink-secondary,var(--muted-foreground))]">
            {{
              isZh
                ? '统一管理图像、音频与视频异步生成作业 · 真实生命周期进度 · 受控票据预览下载与付费重试'
                : 'Asynchronous image, audio & video executions · Honest lifecycle progress · Scope-bound artifact preview, download & paid retry'
            }}
          </p>
        </div>

        <div class="flex items-center gap-2">
          <Button
            v-if="canCreateTasks && canReadTasks && activeScopeIds"
            data-testid="open-create-task-btn"
            variant="primary"
            size="sm"
            @click="openCreateModal"
          >
            {{ isZh ? '+ 新建异步任务' : '+ New Async Task' }}
          </Button>
        </div>
      </header>

      <ConsoleAsyncBanner
        v-if="confirmedNotice"
        :notice="confirmedNotice"
        dismissible
        @dismiss="confirmedNotice = null"
      />

      <ConsoleAsyncBanner
        v-if="pageNotice"
        :notice="pageNotice"
        :retry-label="isZh ? '重试加载' : 'Retry'"
        @retry="loadTasks({ refresh: true, cursor: activeCursor })"
      />

      <!-- Summary Strip -->
      <section class="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <div class="rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] p-3.5">
          <p class="font-mono text-[11px] uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
            {{ isZh ? '当前页作业' : 'Page Tasks' }}
          </p>
          <p class="mt-1 font-mono text-xl font-bold tabular-nums text-[var(--oh-ink,var(--foreground))]">
            {{ loading ? EM_DASH : formatNullableInteger(summaryCounts.total) }}
          </p>
        </div>
        <div class="rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] p-3.5">
          <p class="font-mono text-[11px] uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
            {{ isZh ? '排队 / 执行中' : 'Queued / Running' }}
          </p>
          <p class="mt-1 font-mono text-xl font-bold tabular-nums text-[var(--oh-accent-text,var(--brand-text,#d97706))]">
            {{ loading ? EM_DASH : formatNullableInteger(summaryCounts.active) }}
          </p>
        </div>
        <div class="rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] p-3.5">
          <p class="font-mono text-[11px] uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
            {{ isZh ? '已完成' : 'Succeeded' }}
          </p>
          <p class="mt-1 font-mono text-xl font-bold tabular-nums text-[var(--oh-ink,var(--foreground))]">
            {{ loading ? EM_DASH : formatNullableInteger(summaryCounts.succeeded) }}
          </p>
        </div>
        <div class="rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] p-3.5">
          <p class="font-mono text-[11px] uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
            {{ isZh ? '失败 / 取消 / 未知' : 'Failed / Unknown' }}
          </p>
          <p class="mt-1 font-mono text-xl font-bold tabular-nums text-[var(--oh-ink,var(--foreground))]">
            {{ loading ? EM_DASH : formatNullableInteger(summaryCounts.failedOrCanceled + summaryCounts.unknown) }}
          </p>
        </div>
      </section>

      <!-- Filter Bar -->
      <section class="flex flex-wrap items-end justify-between gap-3 rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] p-3.5">
        <div class="flex flex-wrap items-end gap-2.5">
          <label class="space-y-1 text-xs">
            <span class="block font-mono text-[11px] uppercase text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
              {{ isZh ? '任务类型' : 'Kind' }}
            </span>
            <select
              v-model="filters.kind"
              data-testid="filter-task-kind"
              class="h-8 rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2.5 font-mono text-xs text-[var(--oh-ink,var(--foreground))]"
              @change="applyFilters"
            >
              <option value="">{{ isZh ? '全部类型' : 'All Kinds' }}</option>
              <option value="image">{{ isZh ? '图像 (Image)' : 'Image' }}</option>
              <option value="audio">{{ isZh ? '音频 (Audio)' : 'Audio' }}</option>
              <option value="video">{{ isZh ? '视频 (Video)' : 'Video' }}</option>
            </select>
          </label>

          <label class="space-y-1 text-xs">
            <span class="block font-mono text-[11px] uppercase text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
              {{ isZh ? '任务状态' : 'State' }}
            </span>
            <select
              v-model="filters.state"
              data-testid="filter-task-state"
              class="h-8 rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2.5 font-mono text-xs text-[var(--oh-ink,var(--foreground))]"
              @change="applyFilters"
            >
              <option value="">{{ isZh ? '全部状态' : 'All States' }}</option>
              <option value="queued">{{ isZh ? '排队中' : 'Queued' }}</option>
              <option value="running">{{ isZh ? '执行中' : 'Running' }}</option>
              <option value="succeeded">{{ isZh ? '已完成' : 'Succeeded' }}</option>
              <option value="failed">{{ isZh ? '执行失败' : 'Failed' }}</option>
              <option value="canceled">{{ isZh ? '已取消' : 'Canceled' }}</option>
              <option value="unknown">{{ isZh ? '状态未知' : 'Unknown' }}</option>
            </select>
          </label>

          <label class="space-y-1 text-xs">
            <span class="block font-mono text-[11px] uppercase text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
              {{ isZh ? '模型标识' : 'Model' }}
            </span>
            <input
              v-model="filters.modelId"
              data-testid="filter-task-model"
              type="search"
              :placeholder="isZh ? '按模型标识筛选…' : 'Filter by model…'"
              class="h-8 rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2.5 font-mono text-xs text-[var(--oh-ink,var(--foreground))]"
              @keydown.enter="applyFilters"
            />
          </label>

          <Button variant="secondary" size="sm" @click="applyFilters">
            {{ isZh ? '筛选' : 'Apply' }}
          </Button>
          <Button v-if="hasActiveFilters" variant="ghost" size="sm" @click="clearFilters">
            {{ isZh ? '重置条件' : 'Clear Filters' }}
          </Button>
        </div>

        <div class="flex items-center gap-2 font-mono text-xs tabular-nums text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
          <span v-if="asOf">{{ isZh ? '同步快照：' : 'Snapshot: ' }}{{ asOf.slice(0, 19).replace('T', ' ') }}Z</span>
        </div>
      </section>

      <!-- Initial Skeleton -->
      <div v-if="loading" data-testid="console-state-skeleton" class="space-y-2">
        <Skeleton height="52px" />
        <Skeleton height="52px" />
        <Skeleton height="52px" />
      </div>

      <!-- Missing Scope State -->
      <div
        v-else-if="!activeScopeIds"
        data-testid="console-state-no-scope"
      >
        <EmptyState
          :title="isZh ? '请先选择工作空间与项目' : 'Select Workspace and Project First'"
          :description="
            isZh
              ? '异步任务列表、创建及产物下载均按工作空间与项目作用域隔离，请先在上方作用域栏选择或前往工作空间页面配置项目。'
              : 'Asynchronous tasks and artifacts are scoped to a workspace and project. Choose a workspace and project above or manage scopes in Workspaces.'
          "
          :action-label="isZh ? '前往工作空间管理' : 'Manage Workspaces'"
          @action="navigateTo('/workspaces')"
        />
      </div>

      <!-- Zero-Filter State -->
      <div
        v-else-if="tasks.length === 0 && hasActiveFilters"
        data-testid="console-state-zero-filter"
        class="rounded-[var(--oh-r-md,8px)] border border-dashed border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] p-8 text-center"
      >
        <p class="text-sm font-medium text-[var(--oh-ink,var(--foreground))]">
          {{ isZh ? '当前筛选条件下没有匹配的异步任务' : 'Zero tasks match your current filters' }}
        </p>
        <p class="mt-1 text-xs text-[var(--oh-ink-secondary,var(--muted-foreground))]">
          {{ isZh ? '清除状态或模型筛选以查看全部作业。' : 'Clear state or model filters to view all scoped tasks.' }}
        </p>
        <div class="mt-4">
          <Button variant="secondary" size="sm" @click="clearFilters">
            {{ isZh ? '清除筛选条件' : 'Reset Filters' }}
          </Button>
        </div>
      </div>

      <!-- Empty State -->
      <div
        v-else-if="tasks.length === 0 && !pageNotice"
        data-testid="console-state-empty"
      >
        <EmptyState
          :title="isZh ? '暂无异步生成或批处理任务' : 'No Asynchronous Tasks Yet'"
          :description="
            isZh
              ? '提交图像、音频或视频生成任务后，可在此实时追踪真实生命周期、在线预览/下载私有归档产物或发起付费重试。'
              : 'Create an image, audio, or video generation task to track progress, preview and download private artifacts, or trigger paid retries.'
          "
        >
          <template #actions>
            <Button v-if="canCreateTasks && canReadTasks" variant="primary" size="sm" @click="openCreateModal">
              {{ isZh ? '创建首个异步任务' : 'Create First Async Task' }}
            </Button>
          </template>
        </EmptyState>
      </div>

      <!-- Task Table -->
      <div
        v-else-if="tasks.length > 0"
        class="overflow-x-auto rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))]"
      >
        <table class="w-full border-collapse text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] font-mono text-[11px] uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
              <th class="px-3.5 py-2.5">{{ isZh ? '任务 ID' : 'Task ID' }}</th>
              <th class="px-3.5 py-2.5">{{ isZh ? '类型' : 'Kind' }}</th>
              <th class="px-3.5 py-2.5">{{ isZh ? '模型' : 'Model' }}</th>
              <th class="px-3.5 py-2.5">{{ isZh ? '状态 / 阶段' : 'State / Stage' }}</th>
              <th class="px-3.5 py-2.5 text-right">{{ isZh ? '进度' : 'Progress' }}</th>
              <th class="px-3.5 py-2.5">{{ isZh ? '结算状态' : 'Settlement' }}</th>
              <th class="px-3.5 py-2.5 text-right">{{ isZh ? '创建时间' : 'Created' }}</th>
              <th class="px-3.5 py-2.5 text-right">{{ isZh ? '操作' : 'Actions' }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-[var(--oh-border,var(--border))]">
            <tr
              v-for="task in tasks"
              :key="task.id"
              :data-testid="`task-row-${task.id}`"
              class="cursor-pointer transition-colors hover:bg-[var(--oh-bg-subtle,var(--surface-2))]"
              @click="openTaskDetail(task.id)"
            >
              <td class="px-3.5 py-3 font-mono font-semibold tabular-nums text-[var(--oh-ink,var(--foreground))]">
                {{ task.id }}
              </td>
              <td class="px-3.5 py-3">
                <Badge :tone="taskKindTone(task.kind)" variant="subtle" mono>
                  {{ formatTaskKindLabel(task.kind, isZh) }}
                </Badge>
              </td>
              <td class="px-3.5 py-3 font-mono text-[var(--oh-ink,var(--foreground))]">
                {{ task.model_id }}
              </td>
              <td class="px-3.5 py-3">
                <div class="flex flex-wrap items-center gap-1.5">
                  <Badge :tone="taskStateTone(task.state)" variant="subtle" mono>
                    {{ formatTaskStateLabel(task.state, isZh) }}
                  </Badge>
                  <span class="font-mono text-[11px] text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                    {{ formatTaskStageLabel(task.stage, isZh) }}
                  </span>
                </div>
              </td>
              <td class="px-3.5 py-3 text-right font-mono tabular-nums">
                <div
                  v-if="resolveHonestTaskProgress(task).mode === 'determinate'"
                  class="inline-flex items-center gap-2"
                >
                  <div class="h-1.5 w-16 overflow-hidden rounded-full bg-[var(--oh-bg-inset,var(--border))]">
                    <div
                      class="h-full bg-[var(--oh-accent,#f59e0b)]"
                      :style="{ width: `${resolveHonestTaskProgress(task).percent}%` }"
                    />
                  </div>
                  <span class="font-semibold text-[var(--oh-ink,var(--foreground))]">
                    {{ resolveHonestTaskProgress(task).label }}
                  </span>
                </div>
                <span
                  v-else
                  :data-testid="`row-unreported-progress-${task.id}`"
                  class="text-[var(--oh-ink-tertiary,var(--muted-foreground))]"
                >
                  {{ EM_DASH }}
                </span>
              </td>
              <td class="px-3.5 py-3 font-mono tabular-nums text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                {{ formatTaskSettlementLabel(task.settlement_state, isZh) }}
              </td>
              <td class="px-3.5 py-3 text-right font-mono tabular-nums text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                {{ task.created_at ? task.created_at.slice(0, 19).replace('T', ' ') : EM_DASH }}
              </td>
              <td class="px-3.5 py-3 text-right" @click.stop>
                <Button
                  :data-testid="`inspect-task-${task.id}`"
                  variant="secondary"
                  size="xs"
                  @click="openTaskDetail(task.id)"
                >
                  {{ isZh ? '详情与产物' : 'Inspect' }}
                </Button>
              </td>
            </tr>
          </tbody>
        </table>

        <!-- Cursor Pagination -->
        <div class="flex items-center justify-between border-t border-[var(--oh-border,var(--border))] px-3.5 py-2.5">
          <span class="font-mono text-xs tabular-nums text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
            {{ tasks.length }} {{ isZh ? '条记录' : 'items' }}
          </span>
          <div class="flex items-center gap-2">
            <Button
              variant="secondary"
              size="xs"
              :disabled="cursorStack.length === 0 || loading"
              @click="goPrevPage"
            >
              ← {{ isZh ? '上一页' : 'Previous' }}
            </Button>
            <Button
              variant="secondary"
              size="xs"
              :disabled="!nextCursor || loading"
              @click="goNextPage"
            >
              {{ isZh ? '下一页' : 'Next' }} →
            </Button>
          </div>
        </div>
      </div>
    </div>

    <TaskDetailDrawer
      v-model:open="drawerOpen"
      :detail="selectedDetail"
      :items="selectedItems"
      :loading="drawerLoading"
      :action-busy-id="actionBusyId"
      :artifact-previews="artifactPreviews"
      :notice="drawerNotice"
      :is-zh="isZh"
      :teleport-disabled="props.teleportDisabled"
      :now-ms="props.nowMs"
      @refresh="selectedDetail && openTaskDetail(selectedDetail.id)"
      @cancel="handleCancelTask"
      @open-retry="handleOpenPaidRetry"
      @preview-artifact="handlePreviewArtifact"
      @download-artifact="handleDownloadArtifact"
      @revoke-artifact="handleRevokeArtifact"
      @select-task="openTaskDetail"
    />

    <TaskPaidRetryModal
      v-model:open="retryModalOpen"
      :task="selectedDetail"
      :items="selectedItems"
      :preselected-item-ids="retryPreselectedIds"
      :submitting="retrySubmitting"
      :is-zh="isZh"
      :teleport-disabled="props.teleportDisabled"
      @confirm="handleConfirmPaidRetry"
      @cancel="retryModalOpen = false"
    />

    <TaskCreateModal
      v-model:open="createModalOpen"
      :keys="createKeys"
      :models="createModels"
      :models-loading="createModelsLoading"
      :submitting="createSubmitting"
      :is-zh="isZh"
      :teleport-disabled="props.teleportDisabled"
      @submit="handleCreateTask"
      @kind-change="handleCreateKindChange"
      @navigate-keys="handleNavigateToKeysFromCreate"
    />
  </AppLayout>
</template>

<style scoped>
button:focus-visible,
input:focus-visible,
select:focus-visible,
textarea:focus-visible {
  outline: 2px solid var(--oh-focus-ring);
  outline-offset: 2px;
}
</style>
