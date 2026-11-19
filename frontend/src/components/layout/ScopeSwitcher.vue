<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore } from '@/stores'
import { workspaceIDFromPath } from '@/api/workspaceScope'
import Skeleton from '@/components/ui/Skeleton.vue'
import Badge from '@/components/ui/Badge.vue'

const { locale } = useI18n()
const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
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

const scopes = computed(() => appStore.scopes ?? [])
const scopesLoading = computed(() => Boolean(appStore.scopesLoading))
const scopesRefreshing = computed(() => Boolean(appStore.scopesRefreshing))
const scopesStale = computed(() => Boolean(appStore.scopesStale))
const scopesError = computed(() => appStore.scopesError ?? null)
const scopesNextCursor = computed(() => appStore.scopesNextCursor ?? null)
const activeScope = computed(
  () => appStore.activeScope ?? { mode: 'personal' as const, workspaceId: null, projectId: null }
)

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
  if (effectiveWorkspaceId.value) return 'WORKSPACE'
  return 'PERSONAL'
})

const primaryScopeLabel = computed(() => {
  if (effectiveWorkspaceId.value) {
    if (currentWorkspace.value) {
      return currentWorkspace.value.name
    }
    return `#${effectiveWorkspaceId.value}`
  }
  return isZh.value ? '个人空间' : 'Personal'
})

const secondaryProjectLabel = computed(() => {
  if (!effectiveWorkspaceId.value) return ''
  if (currentProject.value) return currentProject.value.name
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
}

const filteredOptions = computed<FlatScopeOption[]>(() => {
  const q = query.value.trim().toLowerCase()
  const result: FlatScopeOption[] = []

  const personalLabel = isZh.value ? '个人空间' : 'Personal'
  const personalSub = isZh.value ? '默认个人工作台' : 'Default personal scope'
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
      selected: !effectiveWorkspaceId.value
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

    const isWsSelected =
      effectiveWorkspaceId.value === ws.workspace_id && !activeScope.value.projectId
    result.push({
      key: `ws:${ws.workspace_id}`,
      kind: 'workspace',
      workspaceId: ws.workspace_id,
      projectId: null,
      label: ws.name,
      subLabel: `#${ws.workspace_id}`,
      selected: isWsSelected
    })

    for (const proj of matchingProjects) {
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
        selected: isProjSelected
      })
    }
  }

  return result
})

function ensureScopesFetched(force = false): void {
  if (!isSignedIn.value || typeof appStore.fetchScopes !== 'function') return
  void appStore.fetchScopes({ force })
}

watch(
  () => urlWorkspaceId.value,
  (wsId) => {
    if (wsId && wsId !== activeScope.value.workspaceId && typeof appStore.selectWorkspaceScope === 'function') {
      appStore.selectWorkspaceScope(wsId, null)
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
  const currentPath = route.path || ''
  const fromUrlWs = workspaceIDFromPath(currentPath)

  if (option.kind === 'personal') {
    appStore.selectPersonalScope?.()
    closePopover(true)
    if (fromUrlWs) {
      const stripped = currentPath.replace(/^\/workspaces\/[1-9]\d*/, '') || '/dashboard'
      await router.push(stripped)
    }
    return
  }

  if (option.workspaceId) {
    appStore.selectWorkspaceScope?.(option.workspaceId, option.projectId)
    closePopover(true)
    if (fromUrlWs && fromUrlWs !== option.workspaceId) {
      const suffix = currentPath.replace(/^\/workspaces\/[1-9]\d*/, '')
      await router.push(`/workspaces/${option.workspaceId}${suffix}`)
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
    if (target) {
      event.preventDefault()
      void applyOption(target)
    }
  }
}

async function handleLoadMoreWorkspaces(): Promise<void> {
  if (!scopesNextCursor.value || loadingMoreWorkspaces.value) return
  loadingMoreWorkspaces.value = true
  try {
    await appStore.fetchScopes?.({ cursor: scopesNextCursor.value })
  } finally {
    loadingMoreWorkspaces.value = false
  }
}

async function handleLoadMoreProjects(workspaceId: string): Promise<void> {
  if (loadingProjectsFor.value) return
  loadingProjectsFor.value = workspaceId
  try {
    await appStore.loadMoreWorkspaceProjects?.(workspaceId)
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

onMounted(() => {
  document.addEventListener('mousedown', handleDocumentPointerDown)
})

onBeforeUnmount(() => {
  document.removeEventListener('mousedown', handleDocumentPointerDown)
})
</script>

<template>
  <div v-if="isSignedIn" ref="rootRef" class="relative shrink-0">
    <div
      v-if="scopesLoading && !appStore.scopesLoaded"
      class="inline-flex h-8 items-center gap-2 rounded-oh-md border border-oh-subtle bg-oh-subtle px-2.5"
      aria-busy="true"
      :aria-label="isZh ? '正在加载工作空间作用域' : 'Loading workspace scopes'"
    >
      <span class="font-mono text-[10px] font-semibold uppercase tracking-wider text-oh-muted">
        {{ tierBadgeLabel }}
      </span>
      <Skeleton width="88px" height="14px" rounded="sm" />
    </div>

    <button
      v-else
      ref="triggerRef"
      type="button"
      data-testid="scope-switcher-trigger"
      :aria-expanded="open"
      aria-haspopup="dialog"
      :aria-label="isZh ? '切换工作空间与项目作用域' : 'Switch workspace and project scope'"
      class="oh-focus-ring inline-flex h-8 max-w-[240px] items-center gap-1.5 rounded-oh-md border border-oh-default bg-oh-surface px-2.5 text-left text-[13px] transition-colors hover:border-oh-strong hover:bg-oh-hover sm:max-w-[300px]"
      @click="togglePopover"
      @keydown.esc="closePopover(true)"
    >
      <span
        :class="[
          'shrink-0 rounded-oh-xs border px-1.5 py-0.5 font-mono text-[10px] font-semibold uppercase tracking-wider',
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
      class="absolute left-0 top-full z-50 mt-1.5 w-[min(340px,calc(100vw-24px))] overflow-hidden rounded-oh-lg border border-oh-default bg-oh-elevated text-oh-primary shadow-oh-md"
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
            :class="[
              'flex w-full items-center justify-between gap-2 rounded-oh-sm px-2.5 py-1.5 text-left text-[13px] transition-colors',
              item.kind === 'project' ? 'pl-6' : '',
              idx === activeIndex
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

      <div class="flex items-center justify-between border-t border-oh-subtle bg-oh-subtle px-3 py-2 text-xs">
        <router-link
          to="/workspaces"
          class="font-medium text-oh-secondary hover:text-oh-primary"
          @click="closePopover(false)"
        >
          {{ isZh ? '管理工作空间 →' : 'Manage Workspaces →' }}
        </router-link>
        <span class="font-mono text-[10px] text-oh-muted">GET /api/v2/me/scopes</span>
      </div>
    </div>
  </div>
</template>
