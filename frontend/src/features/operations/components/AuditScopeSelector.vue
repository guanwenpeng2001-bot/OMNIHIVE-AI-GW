<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue'
import { Button, Select } from '@/components/ui'
import { projectScope, scopeValue, type ProjectScope } from '@/features/requests/api'
import { useScopedRead } from '@/features/requests/useScopedRead'
import { useAuthStore } from '@/stores/auth'
import { requestGet, type ScopedProjectPage, type ScopedWorkspacePage, type ScopedWorkspaceSummary } from './operationsApi'

const props = defineProps<{
  scope: ProjectScope | null
  isZh: boolean
}>()

const emit = defineEmits<{
  change: [scope: ProjectScope]
}>()

const auth = useAuthStore()
const workspaces = ref<ScopedWorkspaceSummary[]>([])
const cursor = ref<string | null>(null)
const actorKey = computed(() => JSON.stringify([auth.user?.id ?? '', auth.sessionRevision]))
let projectRequest: AbortController | null = null

watch(
  actorKey,
  () => {
    projectRequest?.abort()
    workspaces.value = []
    cursor.value = null
  },
  { flush: 'sync' }
)

onScopeDispose(() => projectRequest?.abort())

const directory = useScopedRead(
  () => actorKey.value,
  async (signal) => {
    const params: Record<string, unknown> = { limit: 50 }
    if (cursor.value) params.cursor = cursor.value
    const page = await requestGet<ScopedWorkspacePage>('/me/scopes', params, undefined, signal)
    if (!signal.aborted) {
      workspaces.value = cursor.value ? [...workspaces.value, ...page.items] : page.items
    }
    return page
  }
)

const options = computed(() => {
  if (directory.error.value?.status === 403) return []
  const values = workspaces.value.flatMap((workspace) =>
    workspace.projects.map((project) => ({
      value: `${workspace.workspace_id}/${project.id}`,
      label: `${workspace.name} / ${project.name}`
    }))
  )
  const current = scopeValue(props.scope)
  if (current && !values.some((option) => option.value === current)) {
    values.unshift({
      value: current,
      label: props.isZh ? `当前项目 (${current})` : `Current Scope (${current})`
    })
  }
  return values
})

function handleSelect(value: string | number) {
  const [workspace, project] = String(value).split('/')
  const target = projectScope(workspace, project)
  if (target) emit('change', target)
}

async function loadMoreWorkspaces() {
  cursor.value = directory.data.value?.next_cursor ?? null
  await directory.refresh()
}

const loadingProjects = ref(false)
const projectRequestFailed = ref(false)
const moreProjectWorkspace = computed(() => workspaces.value.find((w) => w.projects_next_cursor))

async function loadMoreProjects() {
  const workspace = moreProjectWorkspace.value
  const next = workspace?.projects_next_cursor
  if (!workspace || !next || loadingProjects.value) return
  loadingProjects.value = true
  projectRequestFailed.value = false
  const request = new AbortController()
  projectRequest = request
  try {
    const page = await requestGet<ScopedProjectPage>(
      `/workspaces/${encodeURIComponent(workspace.workspace_id)}/projects`,
      { limit: 50, cursor: next },
      undefined,
      request.signal
    )
    if (request.signal.aborted) return
    workspaces.value = workspaces.value.map((current) =>
      current.workspace_id === workspace.workspace_id
        ? {
            ...current,
            projects: [...current.projects, ...page.items.map(({ id, name }) => ({ id, name }))],
            projects_next_cursor: page.next_cursor
          }
        : current
    )
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
      :label="isZh ? '所属项目' : 'Project Scope'"
      :placeholder="isZh ? '选择工作空间与项目' : 'Select workspace & project'"
      :options="options"
      :disabled="directory.loading.value && options.length === 0"
      @update:model-value="handleSelect"
    />
    <div
      v-if="directory.error.value || directory.data.value?.next_cursor || moreProjectWorkspace"
      class="mt-1 flex flex-wrap gap-1"
    >
      <Button
        v-if="directory.error.value"
        size="xs"
        variant="ghost"
        @click="directory.refresh"
      >
        {{ isZh ? '重试加载列表' : 'Retry loading scopes' }}
      </Button>
      <Button
        v-else-if="directory.data.value?.next_cursor"
        size="xs"
        variant="ghost"
        :disabled="directory.loading.value"
        @click="loadMoreWorkspaces"
      >
        {{ isZh ? '加载更多工作空间' : 'More workspaces' }}
      </Button>
      <Button
        v-if="moreProjectWorkspace"
        size="xs"
        variant="ghost"
        :loading="loadingProjects"
        @click="loadMoreProjects"
      >
        {{ isZh ? `加载更多项目 (${moreProjectWorkspace.name})` : `More projects (${moreProjectWorkspace.name})` }}
      </Button>
    </div>
    <p v-if="projectRequestFailed" role="status" class="mt-1 text-xs text-oh-status-danger-fg">
      {{ isZh ? '加载项目列表失败，请稍后重试。' : 'Failed to load projects. Please try again.' }}
    </p>
  </div>
</template>
