<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores'
import {
  SCOPE_CHANGED_EVENT,
  getConsoleScopeStore,
  isDefaultSharedScope
} from '@/stores/consoleScope'
import { workspaceIDFromPath } from '@/api/workspaceScope'
import Skeleton from '@/components/ui/Skeleton.vue'
import Badge from '@/components/ui/Badge.vue'

withDefaults(
  defineProps<{
    align?: 'left' | 'right'
  }>(),
  {
    align: 'right'
  }
)

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const scopeStore = getConsoleScopeStore()
const authStore = useAuthStore()

const open = ref(false)
const query = ref('')
const activeIndex = ref(0)
const loadingMoreWorkspaces = ref(false)
const loadingProjectsFor = ref<string | null>(null)
const rootRef = ref<HTMLElement | null>(null)
const triggerRef = ref<HTMLButtonElement | null>(null)
const searchInputRef = ref<HTMLInputElement | null>(null)

const isZh = computed(() => String(locale?.value || 'zh').toLowerCase().startsWith('zh'))
const isSignedIn = computed(() => Boolean(authStore.isAuthenticated ?? authStore.user))
const isAdminRoute = computed(() => Boolean(route.path?.startsWith('/admin')))

const scopes = computed(() => scopeStore.scopes ?? [])
const scopesLoading = computed(() => Boolean(scopeStore.scopesLoading))
const scopesRefreshing = computed(() => Boolean(scopeStore.scopesRefreshing))
const scopesStale = computed(() => Boolean(scopeStore.scopesStale))
const scopesError = computed(() => scopeStore.scopesError ?? null)
const scopesNextCursor = computed(() => scopeStore.scopesNextCursor ?? null)
const activeScope = computed(
  () => scopeStore.activeScope ?? { mode: 'personal' as const, workspaceId: null, projectId: null }
)
const scopeValidationStatus = computed(() => scopeStore.scopeValidationStatus)

const urlWorkspaceId = computed(() => workspaceIDFromPath(route.path || '') ?? null)

const effectiveWorkspaceId = computed(
  () => urlWorkspaceId.value ?? activeScope.value.workspaceId
)

const currentWorkspace = computed(() => {
  if (!effectiveWorkspaceId.value) return null
  return scopes.value.find((ws) => ws.workspace_id === effectiveWorkspaceId.value) ?? null
})

const currentProject = computed(() => {
  if (!currentWorkspace.value || !activeScope.value.projectId) return null
  return (
    currentWorkspace.value.projects.find((p) => p.id === activeScope.value.projectId) ?? null
  )
})

const tierBadgeLabel = computed(() => {
  if (isAdminRoute.value) return 'ADMIN'
  if (effectiveWorkspaceId.value) {
    return activeScope.value.projectId ? 'PRJ' : 'WS'
  }
  return 'PER'
})

const primaryScopeLabel = computed(() => {
  if (effectiveWorkspaceId.value) {
    if (currentWorkspace.value) {
      return currentWorkspace.value.name
    }
    return `#${effectiveWorkspaceId.value}`
  }
  return t('scope.personal.label')
})

const secondaryProjectLabel = computed(() => {
  if (!effectiveWorkspaceId.value) return ''
  if (currentProject.value) return currentProject.value.name
  if (activeScope.value.projectId) return `#${activeScope.value.projectId}`
  if (currentWorkspace.value && currentWorkspace.value.projects.length === 0) {
    return isZh.value ? '无项目' : 'No projects'
  }
  return isZh.value ? '全部项目' : 'All projects'
})

interface FlatScopeOption {
  key: string
  kind: 'personal' | 'workspace' | 'project'
  workspaceId: string | null
  projectId: string | null
  label: string
  subLabel: string
  selected: boolean
  disabled?: boolean
  isArchived?: boolean
  isDefaultShared?: boolean
}

const filteredOptions = computed<FlatScopeOption[]>(() => {
  const q = query.value.trim().toLowerCase()
  const result: FlatScopeOption[] = []

  const personalLabel = t('scope.personal.label')
  const personalSub = t('scope.personal.description')
  if (
    !q ||
    personalLabel.toLowerCase().includes(q) ||
    'personal'.includes(q)
  ) {
    result.push({
      key: 'personal',
      kind: 'personal',
      workspaceId: null,
      projectId: null,
      label: personalLabel,
      subLabel: personalSub,
      selected: activeScope.value.mode === 'personal' && !effectiveWorkspaceId.value
    })
  }

  for (const ws of scopes.value) {
    const wsMatches =
      !q ||
      ws.name.toLowerCase().includes(q) ||
      ws.workspace_id.toLowerCase().includes(q)

    const matchingProjects = q
      ? ws.projects.filter(
          (proj) =>
            wsMatches ||
            proj.name.toLowerCase().includes(q) ||
            proj.id.toLowerCase().includes(q)
        )
      : ws.projects

    if (!wsMatches && matchingProjects.length === 0) continue

    const wsArchived = ws.workspace_state === 'archived'
    const isWsSelected =
      effectiveWorkspaceId.value === ws.workspace_id && !activeScope.value.projectId
    result.push({
      key: `ws:${ws.workspace_id}`,
      kind: 'workspace',
      workspaceId: ws.workspace_id,
      projectId: null,
      label: ws.name,
      subLabel: `#${ws.workspace_id} · ${t('scope.workspace.overviewShort')}`,
      selected: isWsSelected,
      disabled: wsArchived,
      isArchived: wsArchived
    })

    for (const proj of matchingProjects) {
      const projArchived = wsArchived || proj.state === 'archived'
      const defaultShared = isDefaultSharedScope(ws, proj)
      const isProjSelected =
        effectiveWorkspaceId.value === ws.workspace_id &&
        activeScope.value.projectId === proj.id
      result.push({
        key: `proj:${ws.workspace_id}:${proj.id}`,
        kind: 'project',
        workspaceId: ws.workspace_id,
        projectId: proj.id,
        label: proj.name,
        subLabel: `${ws.name} · #${proj.id}`,
        selected: isProjSelected,
        disabled: projArchived,
        isArchived: projArchived,
        isDefaultShared: defaultShared
      })
    }
  }

  return result
})

function ensureScopesFetched(force = false): void {
  if (!isSignedIn.value || typeof scopeStore.fetchScopes !== 'function') return
  void scopeStore.fetchScopes({ force })
}

const SCOPE_QUERY_KEYS = ['workspace_id', 'workspaceId', 'project_id', 'projectId'] as const

function hasScopeQueryParams(rawQuery: Record<string, unknown> | undefined): boolean {
  if (!rawQuery) return false
  return SCOPE_QUERY_KEYS.some((key) => rawQuery[key] !== undefined)
}

function cleanScopeQuery(rawQuery: Record<string, unknown> | undefined): Record<string, string | null | (string | null)[]> {
  if (!rawQuery) return {}
  const next: Record<string, string | null | (string | null)[]> = {}
  for (const [key, value] of Object.entries(rawQuery)) {
    if ((SCOPE_QUERY_KEYS as readonly string[]).includes(key)) continue
    next[key] = value as string | null | (string | null)[]
  }
  return next
}

watch(
  () => [
    isSignedIn.value,
    route.path,
    route.query?.workspace_id,
    route.query?.workspaceId,
    route.query?.project_id,
    route.query?.projectId
  ],
  ([signedIn]) => {
    if (!signedIn) return
    scopeStore.syncFromLocation?.(
      route.path || '',
      (route.query ?? {}) as Record<string, unknown>
    )
    if (urlWorkspaceId.value) {
      ensureScopesFetched(false)
    }
  },
  { immediate: true }
)

watch(
  isSignedIn,
  (signedIn) => {
    if (signedIn) {
      ensureScopesFetched(false)
    }
  },
  { immediate: true }
)

watch(query, () => {
  activeIndex.value = 0
})

async function togglePopover(): Promise<void> {
  if (open.value) {
    closePopover(true)
    return
  }
  open.value = true
  query.value = ''
  activeIndex.value = Math.max(
    0,
    filteredOptions.value.findIndex((item) => item.selected)
  )
  ensureScopesFetched(false)
  await nextTick()
  searchInputRef.value?.focus()
}

function closePopover(restoreFocus = false): void {
  if (!open.value) return
  open.value = false
  if (restoreFocus) {
    nextTick(() => triggerRef.value?.focus())
  }
}

async function applyOption(option: FlatScopeOption): Promise<void> {
  if (option.disabled) return
  const currentPath = route.path || ''
  const fromUrlWs = workspaceIDFromPath(currentPath)
  const rawQuery = (route.query ?? {}) as Record<string, unknown>
  const hasQueryScope = hasScopeQueryParams(rawQuery)

  if (option.kind === 'personal') {
    scopeStore.selectPersonalScope()
    closePopover(true)
    if (fromUrlWs || hasQueryScope) {
      const stripped = fromUrlWs
        ? currentPath.replace(/^\/workspaces\/[1-9]\d*/, '') || '/dashboard'
        : currentPath
      await router.push({ path: stripped, query: cleanScopeQuery(rawQuery) })
    }
    return
  }

  if (option.workspaceId) {
    scopeStore.selectWorkspaceScope(option.workspaceId, option.projectId)
    closePopover(true)
    if (fromUrlWs && fromUrlWs !== option.workspaceId) {
      const suffix = currentPath.replace(/^\/workspaces\/[1-9]\d*/, '')
      await router.push({
        path: `/workspaces/${option.workspaceId}${suffix}`,
        query: cleanScopeQuery(rawQuery)
      })
    } else if (hasQueryScope) {
      await router.replace({ path: currentPath, query: cleanScopeQuery(rawQuery) })
    }
  }
}

function handleKeydown(event: KeyboardEvent): void {
  const total = filteredOptions.value.length
  if (event.key === 'Escape') {
    event.preventDefault()
    closePopover(true)
    return
  }
  if (event.key === 'ArrowDown') {
    event.preventDefault()
    if (total > 0) {
      activeIndex.value = (activeIndex.value + 1) % total
    }
  } else if (event.key === 'ArrowUp') {
    event.preventDefault()
    if (total > 0) {
      activeIndex.value = (activeIndex.value - 1 + total) % total
    }
  } else if (event.key === 'Enter') {
    const target = filteredOptions.value[activeIndex.value]
    if (target && !target.disabled) {
      event.preventDefault()
      void applyOption(target)
    }
  }
}

async function handleLoadMoreWorkspaces(): Promise<void> {
  if (!scopesNextCursor.value || loadingMoreWorkspaces.value) return
  loadingMoreWorkspaces.value = true
  try {
    await scopeStore.fetchScopes({ cursor: scopesNextCursor.value })
  } finally {
    loadingMoreWorkspaces.value = false
  }
}

async function handleLoadMoreProjects(workspaceId: string): Promise<void> {
  if (loadingProjectsFor.value) return
  loadingProjectsFor.value = workspaceId
  try {
    await scopeStore.loadMoreWorkspaceProjects(workspaceId)
  } finally {
    loadingProjectsFor.value = null
  }
}

function handleRetry(): void {
  ensureScopesFetched(true)
}

function handleDocumentPointerDown(event: MouseEvent): void {
  if (!open.value) return
  if (rootRef.value && !rootRef.value.contains(event.target as Node)) {
    closePopover(false)
  }
}

function handleExternalScopeChanged(): void {
  scopeStore.syncFromStorage()
}

onMounted(() => {
  document.addEventListener('mousedown', handleDocumentPointerDown)
  if (typeof window !== 'undefined') {
    window.addEventListener(SCOPE_CHANGED_EVENT, handleExternalScopeChanged)
  }
})

onBeforeUnmount(() => {
  document.removeEventListener('mousedown', handleDocumentPointerDown)
  if (typeof window !== 'undefined') {
    window.removeEventListener(SCOPE_CHANGED_EVENT, handleExternalScopeChanged)
  }
})
</script>

<template>
  <div v-if="isSignedIn" ref="rootRef" class="relative min-w-0 shrink sm:shrink-0">
    <div
      v-if="scopesLoading && !scopeStore.scopesLoaded"
      class="inline-flex h-8 max-w-full min-w-0 items-center gap-2 rounded-oh-md border border-oh-subtle bg-oh-subtle px-2 sm:px-2.5"
      aria-busy="true"
      :aria-label="isZh ? '正在加载工作空间作用域' : 'Loading workspace scopes'"
    >
      <span class="hidden font-mono text-[10px] font-semibold uppercase tracking-wider text-oh-muted sm:inline">
        {{ tierBadgeLabel }}
      </span>
      <Skeleton width="64px" height="14px" rounded="sm" />
    </div>

    <button
      v-else
      ref="triggerRef"
      type="button"
      data-testid="scope-switcher-trigger"
      :aria-expanded="open"
      aria-haspopup="dialog"
      :aria-label="isZh ? '切换工作空间与项目作用域' : 'Switch workspace and project scope'"
      class="oh-focus-ring inline-flex h-8 max-w-[168px] min-w-0 items-center gap-1 rounded-oh-md border border-oh-default bg-oh-surface px-1.5 text-left text-xs transition-colors hover:border-oh-strong hover:bg-oh-hover sm:max-w-[232px] sm:gap-1.5 sm:px-2.5"
      @click="togglePopover"
      @keydown.esc="closePopover(true)"
    >
      <span
        :class="[
          'hidden shrink-0 rounded-oh-xs border px-1.5 py-0.5 font-mono text-[10px] font-semibold uppercase tracking-wider sm:inline-flex',
          isAdminRoute
            ? 'border-oh-accent bg-oh-accent-subtle text-oh-accent-text'
            : 'border-oh-subtle bg-oh-subtle text-oh-muted'
        ]"
      >
        {{ tierBadgeLabel }}
      </span>

      <span class="min-w-0 truncate font-medium text-oh-primary">
        {{ primaryScopeLabel }}
      </span>

      <template v-if="secondaryProjectLabel">
        <span class="hidden text-oh-muted sm:inline" aria-hidden="true">/</span>
        <span class="hidden max-w-[104px] truncate text-xs text-oh-secondary sm:inline">
          {{ secondaryProjectLabel }}
        </span>
      </template>

      <span
        v-if="scopesStale"
        class="h-1.5 w-1.5 shrink-0 rounded-full bg-[var(--oh-warn-fg)]"
        :title="isZh ? '作用域列表使用缓存，点击刷新' : 'Showing cached scopes'"
      />
      <span
        v-else-if="scopeValidationStatus === 'unavailable'"
        data-testid="scope-switcher-unavailable-dot"
        class="h-1.5 w-1.5 shrink-0 rounded-full bg-[var(--oh-danger-fg)]"
        :title="t('rbac.probe.scopeUnavailable')"
      />

      <svg
        viewBox="0 0 16 16"
        class="ml-0.5 h-3.5 w-3.5 shrink-0 text-oh-muted"
        fill="none"
        stroke="currentColor"
        stroke-width="1.6"
        aria-hidden="true"
      >
        <path stroke-linecap="round" stroke-linejoin="round" d="M5 6.25 8 3.5l3 2.75M5 9.75 8 12.5l3-2.75" />
      </svg>
    </button>

    <div
      v-if="open"
      role="dialog"
      data-testid="scope-switcher-popover"
      :aria-label="isZh ? '选择工作空间与项目' : 'Select workspace and project'"
      :class="[
        'max-sm:fixed max-sm:left-3 max-sm:right-3 max-sm:top-14 max-sm:w-auto sm:absolute sm:top-full z-50 mt-1.5 sm:w-[min(340px,calc(100vw-24px))] overflow-hidden rounded-oh-lg border border-oh-default bg-oh-elevated text-oh-primary shadow-oh-md',
        align === 'left' ? 'sm:left-0' : 'sm:right-0'
      ]"
      @keydown="handleKeydown"
    >
      <div class="flex items-center gap-2 border-b border-oh-subtle px-3 py-2">
        <svg
          viewBox="0 0 16 16"
          class="h-3.5 w-3.5 shrink-0 text-oh-muted"
          fill="none"
          stroke="currentColor"
          stroke-width="1.6"
          aria-hidden="true"
        >
          <circle cx="7" cy="7" r="4.5" />
          <path stroke-linecap="round" d="m10.5 10.5 3 3" />
        </svg>
        <input
          ref="searchInputRef"
          v-model="query"
          type="search"
          data-testid="scope-switcher-search"
          :placeholder="isZh ? '筛选工作空间或项目…' : 'Filter workspaces or projects…'"
          :aria-label="isZh ? '筛选工作空间或项目' : 'Filter workspaces or projects'"
          class="w-full bg-transparent text-[13px] text-oh-primary placeholder:text-oh-muted focus:outline-none"
        />
        <span
          v-if="scopesRefreshing"
          class="h-3.5 w-3.5 shrink-0 animate-spin rounded-full border-[1.5px] border-oh-default border-t-[var(--oh-accent)]"
          :aria-label="isZh ? '正在刷新' : 'Refreshing'"
        />
        <Badge v-else-if="scopesStale" tone="warning" variant="outline" size="sm">
          {{ isZh ? '缓存' : 'Stale' }}
        </Badge>
      </div>

      <div
        v-if="scopesError"
        role="alert"
        data-testid="scope-switcher-error"
        class="border-b border-oh-subtle bg-[var(--oh-warn-bg)] px-3 py-2.5 text-xs text-[var(--oh-warn-fg)]"
      >
        <div class="flex items-start justify-between gap-2">
          <div class="min-w-0 space-y-0.5">
            <p class="font-medium">
              {{
                scopesError.status === 403
                  ? isZh
                    ? '无权读取团队工作空间列表'
                    : 'Workspace directory access restricted (403)'
                  : scopesError.message
              }}
            </p>
            <p v-if="scopesError.requestId" class="oh-tabular text-[11px] opacity-85">
              req: {{ scopesError.requestId }}
            </p>
          </div>
          <button
            v-if="scopesError.status !== 403"
            type="button"
            data-testid="scope-switcher-retry"
            class="oh-focus-ring shrink-0 rounded-oh-xs border border-[var(--oh-warn-border)] bg-oh-surface px-2 py-0.5 font-medium text-oh-primary hover:bg-oh-hover"
            @click="handleRetry"
          >
            {{ isZh ? '重试' : 'Retry' }}
          </button>
        </div>
      </div>

      <div class="max-h-72 overflow-y-auto p-1.5" role="listbox">
        <template v-if="filteredOptions.length > 0">
          <button
            v-for="(item, idx) in filteredOptions"
            :key="item.key"
            type="button"
            role="option"
            :data-testid="`scope-option-${item.key}`"
            :aria-selected="item.selected"
            :aria-disabled="item.disabled ? 'true' : undefined"
            :disabled="item.disabled"
            :class="[
              'flex w-full items-center justify-between gap-2 rounded-oh-sm px-2.5 py-1.5 text-left text-[13px] transition-colors',
              item.kind === 'project' ? 'pl-6' : '',
              item.disabled
                ? 'cursor-not-allowed opacity-55'
                : idx === activeIndex
                ? 'bg-oh-selected text-oh-primary'
                : 'text-oh-primary hover:bg-oh-hover'
            ]"
            @mouseenter="activeIndex = idx"
            @click="applyOption(item)"
          >
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-1.5">
                <span
                  class="shrink-0 font-mono text-[10px] uppercase tracking-wider text-oh-muted"
                >
                  {{
                    item.kind === 'personal'
                      ? 'PER'
                      : item.kind === 'workspace'
                        ? 'WS'
                        : 'PRJ'
                  }}
                </span>
                <span class="truncate font-medium">{{ item.label }}</span>
                <Badge
                  v-if="item.isArchived"
                  tone="warning"
                  variant="outline"
                  size="sm"
                >
                  {{ isZh ? '已归档' : 'Archived' }}
                </Badge>
                <Badge
                  v-else-if="item.isDefaultShared"
                  data-testid="scope-option-default-shared"
                  tone="neutral"
                  variant="outline"
                  size="sm"
                >
                  {{ t('scope.project.defaultSharedShort') }}
                </Badge>
              </div>
              <p class="oh-tabular truncate text-[11px] text-oh-muted">
                {{ item.subLabel }}
              </p>
            </div>

            <span
              v-if="item.selected"
              class="shrink-0 font-mono text-xs font-semibold text-oh-accent-text"
              aria-hidden="true"
            >
              ✓
            </span>
          </button>
        </template>

        <div
          v-else
          data-testid="scope-switcher-zero-filter"
          class="px-3 py-6 text-center text-xs text-oh-muted"
        >
          <p>{{ isZh ? '未找到匹配的工作空间或项目' : 'No matching workspaces or projects' }}</p>
          <button
            v-if="query"
            type="button"
            class="mt-2 font-medium text-oh-accent-text hover:underline"
            @click="query = ''"
          >
            {{ isZh ? '清空筛选条件' : 'Clear filter' }}
          </button>
        </div>

        <div
          v-if="!query && scopes.length === 0 && !scopesLoading && !scopesError"
          data-testid="scope-switcher-empty"
          class="mt-1 rounded-oh-sm border border-dashed border-oh-subtle bg-oh-subtle/60 px-3 py-2.5 text-xs text-oh-muted"
        >
          {{ isZh ? '当前账号暂无已分配的团队工作空间。' : 'No team workspaces assigned yet.' }}
        </div>

        <div
          v-for="ws in scopes.filter((w) => w.projects_next_cursor)"
          :key="`more-proj-${ws.workspace_id}`"
          class="px-2.5 py-1"
        >
          <button
            type="button"
            class="oh-focus-ring text-xs font-medium text-oh-accent-text hover:underline"
            :disabled="loadingProjectsFor === ws.workspace_id"
            @click="handleLoadMoreProjects(ws.workspace_id)"
          >
            {{
              loadingProjectsFor === ws.workspace_id
                ? isZh
                  ? '正在加载更多项目…'
                  : 'Loading projects…'
                : isZh
                  ? `加载 ${ws.name} 的更多项目`
                  : `Load more projects in ${ws.name}`
            }}
          </button>
        </div>

        <div v-if="scopesNextCursor" class="px-2.5 py-1.5">
          <button
            type="button"
            data-testid="scope-switcher-load-more"
            class="oh-focus-ring w-full rounded-oh-sm border border-oh-default bg-oh-surface py-1 text-center text-xs font-medium text-oh-secondary hover:bg-oh-hover"
            :disabled="loadingMoreWorkspaces"
            @click="handleLoadMoreWorkspaces"
          >
            {{
              loadingMoreWorkspaces
                ? isZh
                  ? '加载中…'
                  : 'Loading…'
                : isZh
                  ? '加载更多工作空间'
                  : 'Load more workspaces'
            }}
          </button>
        </div>
      </div>

      <div class="flex flex-wrap items-center justify-between gap-2 border-t border-oh-subtle bg-oh-subtle px-3 py-2 text-xs">
        <router-link
          to="/workspaces?requestAccess=1"
          data-testid="scope-switcher-request-access"
          class="font-medium text-oh-accent-text hover:underline"
          @click="closePopover(false)"
        >
          {{ t('scopeGate.requestAccess') }}
        </router-link>
        <router-link
          to="/workspaces"
          class="font-medium text-oh-secondary hover:text-oh-primary"
          @click="closePopover(false)"
        >
          {{ isZh ? '管理工作空间 →' : 'Manage Workspaces →' }}
        </router-link>
      </div>
    </div>
  </div>
</template>
