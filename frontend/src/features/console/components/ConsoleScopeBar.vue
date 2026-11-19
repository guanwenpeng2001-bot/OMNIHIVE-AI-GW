<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import type { IdentityProjectSummary, IdentityWorkspace } from '../scope'

const props = withDefaults(
  defineProps<{
    workspaces: IdentityWorkspace[]
    projects: IdentityProjectSummary[]
    workspaceId: string
    projectId: string
    loading?: boolean
    refreshing?: boolean
    lastSyncedAt?: string
    activeSurface?: 'dashboard' | 'keys' | 'playground' | 'tasks' | 'workspaces'
  }>(),
  {
    loading: false,
    refreshing: false,
    lastSyncedAt: '',
    activeSurface: undefined
  }
)

const emit = defineEmits<{
  'select-scope': [payload: { workspaceId: string; projectId: string | null }]
  refresh: []
  navigate: [path: string]
}>()

const { locale } = useI18n()
const isZh = computed(() => String(locale.value || '').startsWith('zh'))

const navLinks = computed(() => [
  { id: 'dashboard', path: '/dashboard', label: isZh.value ? '概览' : 'Overview' },
  { id: 'keys', path: '/keys', label: 'API Keys' },
  { id: 'playground', path: '/playground', label: 'Playground' },
  { id: 'tasks', path: '/tasks', label: isZh.value ? '任务中心' : 'Task Center' },
  { id: 'workspaces', path: '/workspaces', label: 'Workspaces' }
])

function onWorkspaceChange(event: Event): void {
  const nextWs = (event.target as HTMLSelectElement).value
  const ws = props.workspaces.find((item) => item.workspace_id === nextWs)
  const firstProject = ws?.projects?.[0]?.id ?? null
  emit('select-scope', { workspaceId: nextWs, projectId: firstProject })
}

function onProjectChange(event: Event): void {
  const nextProj = (event.target as HTMLSelectElement).value || null
  emit('select-scope', { workspaceId: props.workspaceId, projectId: nextProj })
}
</script>

<template>
  <div
    data-testid="console-scope-bar"
    class="flex flex-wrap items-center justify-between gap-2.5 rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] px-3 py-2"
  >
    <div class="flex flex-wrap items-center gap-2">
      <span class="font-mono text-[11px] font-semibold uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
        SCOPE
      </span>

      <template v-if="workspaces.length > 0">
        <label class="sr-only" for="oh-console-ws-select">Workspace</label>
        <select
          id="oh-console-ws-select"
          data-testid="scope-workspace-select"
          :value="workspaceId"
          :disabled="loading"
          class="h-7 rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs tabular-nums text-[var(--oh-ink,var(--foreground))] focus-visible:outline focus-visible:outline-2 focus-visible:outline-[var(--oh-accent,#f59e0b)]"
          @change="onWorkspaceChange"
        >
          <option v-for="ws in workspaces" :key="ws.workspace_id" :value="ws.workspace_id">
            {{ ws.name || ws.workspace_name }} (#{{ ws.workspace_id }})
          </option>
        </select>

        <span class="font-mono text-xs text-[var(--oh-ink-tertiary,var(--muted-foreground))]" aria-hidden="true">/</span>

        <label class="sr-only" for="oh-console-proj-select">Project</label>
        <select
          id="oh-console-proj-select"
          data-testid="scope-project-select"
          :value="projectId"
          :disabled="loading || projects.length === 0"
          class="h-7 rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs tabular-nums text-[var(--oh-ink,var(--foreground))] focus-visible:outline focus-visible:outline-2 focus-visible:outline-[var(--oh-accent,#f59e0b)]"
          @change="onProjectChange"
        >
          <option v-if="projects.length === 0" value="">
            {{ isZh ? '无可见项目 (—)' : 'No visible project (—)' }}
          </option>
          <option v-for="proj in projects" :key="proj.id" :value="proj.id">
            {{ proj.name }} (#{{ proj.id }})
          </option>
        </select>
      </template>

      <Badge v-else tone="neutral" variant="outline" mono>
        {{ loading ? (isZh ? '同步作用域…' : 'Syncing scope…') : (isZh ? '默认个人作用域' : 'Default Personal Scope') }}
      </Badge>

      <Badge v-if="refreshing" data-testid="console-state-refreshing" tone="accent" variant="subtle" mono dot>
        {{ isZh ? '刷新中' : 'Refreshing' }}
      </Badge>
    </div>

    <nav class="flex flex-wrap items-center gap-1" :aria-label="isZh ? '开发者控制台导航' : 'Developer Console Navigation'">
      <button
        v-for="item in navLinks"
        :key="item.id"
        type="button"
        :aria-current="activeSurface === item.id ? 'page' : undefined"
        :class="[
          'rounded-[var(--oh-r-sm,6px)] px-2.5 py-1 font-mono text-xs transition-colors',
          activeSurface === item.id
            ? 'border border-[var(--oh-accent-border,rgba(245,158,11,0.4))] bg-[var(--oh-accent-subtle,rgba(245,158,11,0.12))] font-semibold text-[var(--oh-ink,var(--foreground))]'
            : 'border border-transparent text-[var(--oh-ink-secondary,var(--muted-foreground))] hover:border-[var(--oh-border,var(--border))] hover:text-[var(--oh-ink,var(--foreground))]'
        ]"
        @click="emit('navigate', item.path)"
      >
        {{ item.label }}
      </button>
      <Button variant="ghost" size="xs" :loading="refreshing" @click="emit('refresh')">
        {{ isZh ? '刷新' : 'Sync' }}
      </Button>
    </nav>
  </div>
</template>
