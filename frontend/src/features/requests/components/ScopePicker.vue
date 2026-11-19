<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Select } from '@/components/ui'
import { requestsApi, projectScope, scopeValue, type ProjectScope, type Schema } from '../api'
import { useScopedRead } from '../useScopedRead'
import { useAuthStore } from '@/stores/auth'

const props = defineProps<{ scope: ProjectScope | null }>()
const emit = defineEmits<{ change: [scope: ProjectScope] }>()
const { t } = useI18n()
const auth = useAuthStore()
const workspaces = ref<Schema['IdentityWorkspace'][]>([])
const cursor = ref<string | null>(null)
const actorKey = computed(() => JSON.stringify([auth.user?.id ?? '', auth.sessionRevision]))
let projectRequest: AbortController | null = null
watch(actorKey, () => {
  projectRequest?.abort()
  workspaces.value = []
  cursor.value = null
}, { flush: 'sync' })
onScopeDispose(() => projectRequest?.abort())
const directory = useScopedRead(() => actorKey.value, async signal => {
  const page = await requestsApi.scopes(cursor.value, signal)
  if (!signal.aborted) workspaces.value = cursor.value ? [...workspaces.value, ...page.items] : page.items
  return page
})

const options = computed(() => {
  if (directory.error.value?.status === 403) return []
  const values = workspaces.value.flatMap(workspace => workspace.projects.map(project => ({
    value: `${workspace.workspace_id}/${project.id}`, label: `${workspace.name} / ${project.name}`,
  })))
  const current = scopeValue(props.scope)
  if (current && !values.some(option => option.value === current)) {
    values.unshift({ value: current, label: t('wp6.scope.currentIds', { scope: current }) })
  }
  return values
})

function select(value: string | number) {
  const [workspace, project] = String(value).split('/')
  const target = projectScope(workspace, project)
  if (target) emit('change', target)
}

async function more() {
  cursor.value = directory.data.value?.next_cursor ?? null
  await directory.refresh()
}

const loadingProjects = ref(false)
const projectRequestFailed = ref(false)
const moreProjectWorkspace = computed(() => workspaces.value.find(w => w.projects_next_cursor))
async function moreProjects() {
  const workspace = moreProjectWorkspace.value
  const next = workspace?.projects_next_cursor
  if (!workspace || !next || loadingProjects.value) return
  loadingProjects.value = true
  projectRequestFailed.value = false
  const request = new AbortController()
  projectRequest = request
  try {
    const page = await requestsApi.projects(workspace.workspace_id, next, request.signal)
    if (request.signal.aborted) return
    workspaces.value = workspaces.value.map(current => current.workspace_id === workspace.workspace_id ? {
      ...current,
      projects: [...current.projects, ...page.items.map(({ id, name }) => ({ id, name }))],
      projects_next_cursor: page.next_cursor,
    } : current)
  } catch {
    if (!request.signal.aborted) projectRequestFailed.value = true
  } finally {
    loadingProjects.value = false
  }
}
</script>

<template>
  <div class="w-full min-w-0 sm:w-64">
    <Select
      :model-value="scopeValue(scope)"
      :label="t('wp6.scope.project')"
      :placeholder="t('wp6.scope.select')"
      :options="options"
      :disabled="directory.loading.value && options.length === 0"
      @update:model-value="select"
    />
    <div v-if="directory.error.value || directory.data.value?.next_cursor || moreProjectWorkspace" class="mt-1 flex flex-wrap gap-1">
      <Button
        v-if="directory.error.value"
        size="xs"
        variant="ghost"
        @click="directory.refresh"
      >
        {{ t('wp6.state.retrySection') }}
      </Button>
      <Button
        v-else-if="directory.data.value?.next_cursor"
        size="xs"
        variant="ghost"
        :disabled="directory.loading.value"
        @click="more"
      >
        {{ t('wp6.scope.moreWorkspaces') }}
      </Button>
      <Button v-if="moreProjectWorkspace" size="xs" variant="ghost" :loading="loadingProjects" @click="moreProjects">
        {{ t('wp6.scope.moreProjects', { name: moreProjectWorkspace.name }) }}
      </Button>
    </div>
    <p v-if="projectRequestFailed" role="status" class="mt-1 text-xs text-oh-status-danger-fg">
      {{ t('wp6.state.loadFailed') }}
    </p>
  </div>
</template>
