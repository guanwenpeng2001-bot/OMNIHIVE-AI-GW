<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import { keysAPI } from '@/api'
import { classifyConsoleError, type ConsoleAsyncNotice } from '@/features/console/asyncState'
import { catalogV2Api, type CatalogModelItem } from '@/features/console/catalogApi'
import ConsoleAsyncBanner from '@/features/console/components/ConsoleAsyncBanner.vue'
import ConsoleScopeBar from '@/features/console/components/ConsoleScopeBar.vue'
import { EM_DASH, formatNullableInteger } from '@/features/console/format'
import { useConsoleScope } from '@/features/console/scope'
import { tasksV2Api } from '@/features/tasks/api'
import TaskCreateModal from '@/features/tasks/components/TaskCreateModal.vue'
import TaskDetailDrawer from '@/features/tasks/components/TaskDetailDrawer.vue'
import TaskPaidRetryModal from '@/features/tasks/components/TaskPaidRetryModal.vue'
import { resolveHonestTaskProgress, taskStateTone } from '@/features/tasks/progressRules'
import type {
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
const isZh = computed(() => String(locale.value || '').startsWith('zh'))
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

const filters = reactive({
  state: '',
  modelId: '',
  from: '',
  to: ''
})

const hasActiveFilters = computed(() =>
  Boolean(filters.state || filters.modelId.trim() || filters.from || filters.to)
)

const drawerOpen = ref(false)
const drawerLoading = ref(false)
const drawerNotice = ref<ConsoleAsyncNotice | null>(null)
const selectedDetail = ref<ImageTaskDetail | null>(null)
const selectedItems = ref<ImageTaskItem[]>([])
const actionBusyId = ref('')

const retryModalOpen = ref(false)
const retryPreselectedIds = ref<string[]>([])
const retrySubmitting = ref(false)

const createModalOpen = ref(false)
const createSubmitting = ref(false)
const createKeys = ref<Array<{ id: string; name: string }>>([])
const createModels = ref<CatalogModelItem[]>([])

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
    const wsId = scope.activeWorkspaceId.value || '1'
    const projId = scope.activeProjectId.value || '1'
    const page = await tasksV2Api.listTasks(wsId, projId, {
      cursor: options?.cursor,
      limit: 25,
      state: filters.state ? [filters.state] : undefined,
      model_id: filters.modelId.trim() ? [filters.modelId.trim()] : undefined,
      from: filters.from ? new Date(filters.from).toISOString() : undefined,
      to: filters.to ? new Date(filters.to).toISOString() : undefined
    })
    tasks.value = page.items ?? []
    nextCursor.value = page.next_cursor ?? null
    asOf.value = page.as_of || new Date().toISOString()
  } catch (err) {
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
  drawerOpen.value = true
  drawerLoading.value = true
  drawerNotice.value = null
  const wsId = scope.activeWorkspaceId.value || '1'
  const projId = scope.activeProjectId.value || '1'

  try {
    const [detail, itemPage] = await Promise.all([
      tasksV2Api.getTaskDetail(wsId, projId, taskId),
      tasksV2Api.listTaskItems(wsId, projId, taskId, { limit: 100 }).catch(() => ({
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
  actionBusyId.value = `cancel:${taskId}`
  drawerNotice.value = null
  const wsId = scope.activeWorkspaceId.value || '1'
  const projId = scope.activeProjectId.value || '1'
  try {
    const res = await tasksV2Api.cancelTask(wsId, projId, taskId)
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
  if (!selectedDetail.value) return
  retrySubmitting.value = true
  drawerNotice.value = null
  const wsId = scope.activeWorkspaceId.value || '1'
  const projId = scope.activeProjectId.value || '1'
  try {
    const result = await tasksV2Api.retryTask(wsId, projId, selectedDetail.value.id, {
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
  if (!selectedDetail.value) return
  actionBusyId.value = `download:${artifact.id}`
  drawerNotice.value = null
  const wsId = scope.activeWorkspaceId.value || '1'
  const projId = scope.activeProjectId.value || '1'
  const taskId = selectedDetail.value.id

  try {
    const ticket = await tasksV2Api.issueArtifactDownloadTicket(wsId, projId, taskId, artifact.id)
    const blob = await tasksV2Api.fetchArtifactContentBlob(wsId, projId, taskId, artifact.id, ticket)
    if (typeof window !== 'undefined' && typeof URL.createObjectURL === 'function') {
      const blobUrl = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      const ext = (ticket.content_type || artifact.content_type || 'image/png').split('/')[1] || 'bin'
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

async function handleRevokeArtifact(artifact: ImageTaskArtifact): Promise<void> {
  if (!selectedDetail.value) return
  actionBusyId.value = `revoke:${artifact.id}`
  drawerNotice.value = null
  const wsId = scope.activeWorkspaceId.value || '1'
  const projId = scope.activeProjectId.value || '1'
  const taskId = selectedDetail.value.id

  try {
    await tasksV2Api.revokeArtifact(wsId, projId, taskId, artifact.id)
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
  createModalOpen.value = true
  const wsId = scope.activeWorkspaceId.value || '1'
  const projId = scope.activeProjectId.value || '1'
  const [kRes, mRes] = await Promise.allSettled([
    keysAPI.list(1, 50, { status: 'active' }),
    catalogV2Api.listScopedModels(wsId, projId, { modality: ['image'], limit: 50 })
  ])
  if (kRes.status === 'fulfilled') {
    createKeys.value = (kRes.value.items || []).map((k) => ({ id: String(k.id), name: k.name }))
  }
  if (mRes.status === 'fulfilled') {
    createModels.value = mRes.value
  }
}

async function handleCreateTask(payload: ImageTaskCreateInput): Promise<void> {
  createSubmitting.value = true
  const wsId = scope.activeWorkspaceId.value || '1'
  const projId = scope.activeProjectId.value || '1'
  try {
    const created = await tasksV2Api.createTask(wsId, projId, payload)
    createModalOpen.value = false
    confirmedNotice.value = {
      kind: 'confirmed',
      title: isZh.value
        ? `图像任务 ${created.id} 已提交准入`
        : `Image task ${created.id} accepted`
    }
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

function handleScopeSelect(payload: { workspaceId: string; projectId: string | null }): void {
  drawerOpen.value = false
  selectedDetail.value = null
  cursorStack.value = []
  activeCursor.value = undefined
  scope.selectWorkspaceProject(payload.workspaceId, payload.projectId)
  void loadTasks({ refresh: false })
}

onMounted(() => {
  void loadTasks({ refresh: false })
})
</script>

<template>
  <AppLayout>
    <div data-testid="task-center-view" class="w-full space-y-4">
      <ConsoleScopeBar
        :workspaces="scope.scopes.value"
        :projects="scope.availableProjects.value"
        :workspace-id="scope.activeWorkspaceId.value"
        :project-id="scope.activeProjectId.value"
        :loading="scope.loading.value || loading"
        :refreshing="scope.refreshing.value || refreshing"
        :last-synced-at="asOf || scope.lastSyncedAt.value"
        active-surface="tasks"
        @select-scope="handleScopeSelect"
        @refresh="loadTasks({ refresh: true, cursor: activeCursor })"
        @navigate="navigateTo"
      />

      <!-- Header -->
      <header class="flex flex-wrap items-center justify-between gap-3 rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] px-4 py-3">
        <div>
          <div class="flex flex-wrap items-center gap-2">
            <h1 class="text-lg font-semibold tracking-tight text-[var(--oh-ink,var(--foreground))]">
              {{ isZh ? '任务中心' : 'Task Center' }}
            </h1>
            <Badge tone="accent" variant="subtle" mono>BE-15 · v2</Badge>
          </div>
          <p class="mt-0.5 text-xs text-[var(--oh-ink-secondary,var(--muted-foreground))]">
            {{
              isZh
                ? '统一管理异步图像与批处理作业 · 真实生命周期进度 · 受控票据产物下载与付费重试'
                : 'Asynchronous image & batch executions · Honest lifecycle progress · Scope-bound artifact tickets & paid retry'
            }}
          </p>
        </div>

        <div class="flex items-center gap-2">
          <Button
            data-testid="open-create-task-btn"
            variant="primary"
            size="sm"
            @click="openCreateModal"
          >
            {{ isZh ? '+ 新建图像任务' : '+ New Image Task' }}
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
          <p class="mt-1 font-mono text-xl font-bold tabular-nums text-[var(--oh-accent-ink,var(--brand-text,#d97706))]">
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
            <span class="block font-mono text-[11px] uppercase text-[var(--oh-ink-tertiary,var(--muted-foreground))]">State</span>
            <select
              v-model="filters.state"
              data-testid="filter-task-state"
              class="h-8 rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2.5 font-mono text-xs text-[var(--oh-ink,var(--foreground))]"
              @change="applyFilters"
            >
              <option value="">{{ isZh ? '全部状态' : 'All States' }}</option>
              <option value="queued">queued</option>
              <option value="running">running</option>
              <option value="succeeded">succeeded</option>
              <option value="failed">failed</option>
              <option value="canceled">canceled</option>
              <option value="unknown">unknown</option>
            </select>
          </label>

          <label class="space-y-1 text-xs">
            <span class="block font-mono text-[11px] uppercase text-[var(--oh-ink-tertiary,var(--muted-foreground))]">Model</span>
            <input
              v-model="filters.modelId"
              data-testid="filter-task-model"
              type="search"
              :placeholder="isZh ? '按 model_id 筛选…' : 'Filter model_id…'"
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
          <span v-if="asOf">as_of: {{ asOf.slice(0, 19).replace('T', ' ') }}Z</span>
        </div>
      </section>

      <!-- Initial Skeleton -->
      <div v-if="loading" data-testid="console-state-skeleton" class="space-y-2">
        <Skeleton height="52px" />
        <Skeleton height="52px" />
        <Skeleton height="52px" />
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
              ? '提交图像生成或批量推理后，可在此实时追踪真实生命周期、下载私有归档产物或发起付费重试。'
              : 'Create an image generation task or submit via the OpenAI-compatible image gateway to track progress, download private artifacts, or trigger paid retries.'
          "
        >
          <template #actions>
            <Button variant="primary" size="sm" @click="openCreateModal">
              {{ isZh ? '创建首个图像任务' : 'Create First Image Task' }}
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
              <th class="px-3.5 py-2.5">Task ID</th>
              <th class="px-3.5 py-2.5">Model</th>
              <th class="px-3.5 py-2.5">State / Stage</th>
              <th class="px-3.5 py-2.5 text-right">Progress</th>
              <th class="px-3.5 py-2.5">Settlement</th>
              <th class="px-3.5 py-2.5 text-right">Created</th>
              <th class="px-3.5 py-2.5 text-right">Actions</th>
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
              <td class="px-3.5 py-3 font-mono text-[var(--oh-ink,var(--foreground))]">
                {{ task.model_id }}
              </td>
              <td class="px-3.5 py-3">
                <div class="flex flex-wrap items-center gap-1.5">
                  <Badge :tone="taskStateTone(task.state)" variant="subtle" mono>
                    {{ task.state }}
                  </Badge>
                  <span class="font-mono text-[11px] text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                    {{ task.stage || EM_DASH }}
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
                {{ task.settlement_state || EM_DASH }}
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
      :notice="drawerNotice"
      :is-zh="isZh"
      :teleport-disabled="props.teleportDisabled"
      :now-ms="props.nowMs"
      @refresh="selectedDetail && openTaskDetail(selectedDetail.id)"
      @cancel="handleCancelTask"
      @open-retry="handleOpenPaidRetry"
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
      :submitting="createSubmitting"
      :is-zh="isZh"
      :teleport-disabled="props.teleportDisabled"
      @submit="handleCreateTask"
    />
  </AppLayout>
</template>
