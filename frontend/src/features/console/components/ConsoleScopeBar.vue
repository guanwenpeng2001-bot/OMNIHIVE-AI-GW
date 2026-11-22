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

const { t } = useI18n()

const activeWorkspace = computed(() =>
  props.workspaces.find((item) => item.workspace_id === props.workspaceId) ?? null
)

const activeProject = computed(() =>
  props.projects.find((item) => item.id === props.projectId) ?? null
)
</script>

<template>
  <div
    data-testid="console-scope-bar"
    class="flex min-w-0 flex-wrap items-center justify-between gap-2.5 rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] px-3 py-2"
  >
    <div class="flex min-w-0 max-w-full flex-wrap items-center gap-2">
      <span class="font-mono text-[11px] font-semibold uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
        {{ t('consoleScopeBar.scopeLabel') }}
      </span>

      <template v-if="activeWorkspace">
        <Badge data-testid="console-active-workspace" tone="neutral" variant="outline" mono>
          {{ activeWorkspace.name || activeWorkspace.workspace_name }} (#{{ activeWorkspace.workspace_id }})
        </Badge>

        <span class="font-mono text-xs text-[var(--oh-ink-tertiary,var(--muted-foreground))]" aria-hidden="true">/</span>

        <Badge data-testid="console-active-project" tone="neutral" variant="outline" mono>
          {{ activeProject ? `${activeProject.name} (#${activeProject.id})` : t('consoleScopeBar.noVisibleProject') }}
        </Badge>
      </template>

      <Badge v-else tone="neutral" variant="outline" mono>
        {{ loading ? t('consoleScopeBar.syncingScope') : t('consoleScopeBar.defaultPersonalScope') }}
      </Badge>

      <Badge v-if="refreshing" data-testid="console-state-refreshing" tone="accent" variant="subtle" mono dot>
        {{ t('consoleScopeBar.refreshing') }}
      </Badge>
    </div>

    <div class="flex items-center gap-2">
      <Button variant="ghost" size="xs" @click="emit('navigate', '/workspaces')">{{ t('nav.workspaces') }}</Button>
      <span v-if="lastSyncedAt" class="hidden font-mono text-[11px] text-[var(--oh-ink-tertiary,var(--muted-foreground))] sm:inline">
        {{ lastSyncedAt }}
      </span>
      <Button variant="ghost" size="xs" :loading="refreshing" @click="emit('refresh')">
        {{ t('consoleScopeBar.sync') }}
      </Button>
    </div>
  </div>
</template>
