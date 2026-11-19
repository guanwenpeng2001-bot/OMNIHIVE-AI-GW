<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import {
  Badge,
  Button,
  Card,
  Checkbox,
  EmptyState,
  ErrorState,
  Input,
  Modal,
  PageHeader,
  Select,
  Skeleton,
  Table,
  Tabs,
  Tag,
  pushUiToast
} from '@/components/ui'
import type { UiSelectOption, UiTabItem, UiTableColumn } from '@/components/ui'
import { workspacesAPI, type Workspace, type WorkspaceMember } from '@/api/workspaces'
import { classifyAsyncError, formatNullableMono } from '@/features/shared/asyncState'
import { membersV2API } from './api'
import { projectMemberPermissions, resolveCasMutationError } from './cas'
import type {
  CasConflictState,
  IdentityAccess,
  IdentityProject,
  IdentityWorkspace
} from './types'

const props = withDefaults(
  defineProps<{
    initialWorkspaceId?: string
  }>(),
  { initialWorkspaceId: '' }
)

const activeTab = ref('members')
const loading = ref(true)
const refreshing = ref(false)
const lastSyncedAt = ref<string | undefined>(undefined)
const stale = ref(false)

const loadError = ref<{ state: 'error' | 'forbidden' | 'unknown'; message: string; requestId?: string } | null>(null)
const partialErrors = ref<string[]>([])
const conflictState = ref<CasConflictState | null>(null)
const confirmedMessage = ref<string | null>(null)

const workspaces = ref<Workspace[]>([])
const v2Scopes = ref<IdentityWorkspace[]>([])
const selectedWorkspaceId = ref<string>(props.initialWorkspaceId || '')
const projects = ref<IdentityProject[]>([])
const projectsNextCursor = ref<string | null>(null)
const members = ref<WorkspaceMember[]>([])
const projectAccess = ref<IdentityAccess | null>(null)

const searchQuery = ref('')
const roleFilter = ref('all')

// Project create / edit CAS state
const showCreateProjectModal = ref(false)
const newProjectName = ref('')
const projectActionBusy = ref(false)

const editingProject = ref<IdentityProject | null>(null)
const editProjectName = ref('')

// Member grants CAS modal
const grantModalMember = ref<WorkspaceMember | null>(null)
const grantVersionInput = ref('1')
const grantSelectedActions = ref<string[]>(['text.chat', 'usage.read'])
const grantSelectedProjects = ref<string[]>([])
const grantBusy = ref(false)

// Add / update v1 workspace member
const newMemberUserId = ref('')
const newMemberRole = ref<'admin' | 'member'>('member')
const memberMutationBusy = ref(false)

const AVAILABLE_ACTIONS = [
  'text.chat',
  'text.embeddings',
  'image.generate',
  'usage.read',
  'tasks.read',
  'projects.read',
  'projects.write',
  'members.read',
  'members.grants'
]

const selectedWorkspace = computed(() =>
  workspaces.value.find((ws) => String(ws.id) === selectedWorkspaceId.value) ?? null
)

const roleProjection = computed(() =>
  projectMemberPermissions(projectAccess.value, selectedWorkspace.value?.role)
)

const workspaceOptions = computed<UiSelectOption[]>(() => {
  if (workspaces.value.length > 0) {
    return workspaces.value.map((ws) => ({
      value: String(ws.id),
      label: `${ws.name} (#${ws.id})`
    }))
  }
  return v2Scopes.value.map((ws) => ({
    value: ws.workspace_id,
    label: `${ws.name} (${ws.workspace_id})`
  }))
})

const roleFilterOptions: UiSelectOption[] = [
  { value: 'all', label: 'All Roles' },
  { value: 'admin', label: 'Admin' },
  { value: 'member', label: 'Member' }
]

const tabItems = computed<UiTabItem[]>(() => [
  { value: 'members', label: 'Workspace Members', count: members.value.length },
  { value: 'projects', label: 'Projects & CAS Versions', count: projects.value.length },
  { value: 'roles', label: 'Role Projections & Invitations' }
])

const filteredMembers = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  return members.value.filter((member) => {
    if (roleFilter.value !== 'all' && member.role !== roleFilter.value) return false
    if (query && !String(member.user_id).toLowerCase().includes(query) && !member.role.toLowerCase().includes(query)) {
      return false
    }
    return true
  })
})

const filteredProjects = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  if (!query) return projects.value
  return projects.value.filter(
    (proj) => proj.name.toLowerCase().includes(query) || proj.id.toLowerCase().includes(query)
  )
})

const hasActiveFilters = computed(() => Boolean(searchQuery.value.trim()) || roleFilter.value !== 'all')

const memberColumns: UiTableColumn[] = [
  { key: 'user_id', label: 'Member ID', mono: true, sortable: true },
  { key: 'role', label: 'Workspace Role' },
  { key: 'actions', label: 'Actions' }
]

const projectColumns: UiTableColumn[] = [
  { key: 'name', label: 'Project Name', sortable: true },
  { key: 'id', label: 'Project ID', mono: true },
  { key: 'version', label: 'CAS Version', mono: true },
  { key: 'actions', label: 'Actions', align: 'right' }
]

function clearFilters(): void {
  searchQuery.value = ''
  roleFilter.value = 'all'
}

async function loadWorkspaceDetails(workspaceId: string): Promise<void> {
  if (!workspaceId) return
  partialErrors.value = []
  const numericId = Number(workspaceId)

  const [projectsRes, membersRes] = await Promise.allSettled([
    membersV2API.listProjects(workspaceId, { limit: 50 }),
    Number.isFinite(numericId) && numericId > 0 ? workspacesAPI.members(numericId) : Promise.resolve([])
  ])

  if (projectsRes.status === 'fulfilled') {
    projects.value = projectsRes.value.items
    projectsNextCursor.value = projectsRes.value.next_cursor
    if (projects.value.length > 0) {
      try {
        projectAccess.value = await membersV2API.getProjectAccess(workspaceId, projects.value[0].id)
      } catch {
        projectAccess.value = null
      }
    } else {
      projectAccess.value = null
    }
  } else {
    const err = classifyAsyncError(projectsRes.reason, 'Failed to load v2 projects')
    if (err.state === 'forbidden') {
      loadError.value = { state: 'forbidden', message: err.message, requestId: err.requestId }
      return
    }
    partialErrors.value.push(`Projects: ${err.message}`)
  }

  if (membersRes.status === 'fulfilled') {
    members.value = membersRes.value
  } else {
    const err = classifyAsyncError(membersRes.reason, 'Failed to load workspace members')
    if (err.state === 'forbidden') {
      partialErrors.value.push(`Members (403): ${err.message}`)
    } else {
      partialErrors.value.push(`Members: ${err.message}`)
    }
  }
}

async function loadAll(isRefresh = false): Promise<void> {
  if (isRefresh) {
    refreshing.value = true
  } else {
    loading.value = true
  }
  loadError.value = null
  partialErrors.value = []
  stale.value = false

  try {
    const [wsRes, scopesRes] = await Promise.allSettled([
      workspacesAPI.list(),
      membersV2API.listScopes({ limit: 50 })
    ])

    if (wsRes.status === 'fulfilled') {
      workspaces.value = wsRes.value
    } else {
      const err = classifyAsyncError(wsRes.reason, 'Failed to load workspaces')
      if (err.state === 'forbidden' || err.state === 'unknown') {
        loadError.value = { state: err.state, message: err.message, requestId: err.requestId }
        return
      }
      partialErrors.value.push(`Workspaces: ${err.message}`)
    }

    if (scopesRes.status === 'fulfilled') {
      v2Scopes.value = scopesRes.value.items
    } else {
      const err = classifyAsyncError(scopesRes.reason, 'Failed to load v2 scopes')
      partialErrors.value.push(`Scopes v2: ${err.message}`)
    }

    if (!selectedWorkspaceId.value) {
      const firstWs = workspaces.value[0]?.id ? String(workspaces.value[0].id) : v2Scopes.value[0]?.workspace_id
      if (firstWs) selectedWorkspaceId.value = firstWs
    }

    if (selectedWorkspaceId.value) {
      await loadWorkspaceDetails(selectedWorkspaceId.value)
    }
    lastSyncedAt.value = new Date().toLocaleTimeString()
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

async function handleCreateProject(): Promise<void> {
  if (!selectedWorkspaceId.value || !newProjectName.value.trim()) return
  projectActionBusy.value = true
  conflictState.value = null
  confirmedMessage.value = null
  try {
    const created = await membersV2API.createProject(selectedWorkspaceId.value, newProjectName.value)
    projects.value = [created, ...projects.value]
    newProjectName.value = ''
    showCreateProjectModal.value = false
    confirmedMessage.value = `Project "${created.name}" created (v${created.version}).`
    pushUiToast({ tone: 'success', title: 'Project created', description: created.name })
  } catch (error) {
    const err = classifyAsyncError(error, 'Failed to create project')
    pushUiToast({ tone: err.state === 'unknown' ? 'warning' : 'danger', title: 'Create failed', description: err.message })
  } finally {
    projectActionBusy.value = false
  }
}

function openEditProject(project: IdentityProject): void {
  editingProject.value = project
  editProjectName.value = project.name
  conflictState.value = null
}

async function handleSaveProject(): Promise<void> {
  if (!editingProject.value || !selectedWorkspaceId.value || !editProjectName.value.trim()) return
  projectActionBusy.value = true
  conflictState.value = null
  confirmedMessage.value = null
  const current = editingProject.value
  try {
    const updated = await membersV2API.updateProject(
      selectedWorkspaceId.value,
      current.id,
      editProjectName.value,
      current.version
    )
    projects.value = projects.value.map((p) => (p.id === updated.id ? updated : p))
    editingProject.value = null
    confirmedMessage.value = `Project "${updated.name}" saved at CAS version ${updated.version}.`
    pushUiToast({ tone: 'success', title: 'Project updated', description: `Version ${updated.version}` })
  } catch (error) {
    const resolved = resolveCasMutationError(
      error,
      'project',
      current.id,
      current.version
    )
    if (resolved.kind === 'conflict') {
      conflictState.value = resolved.conflict
      stale.value = true
    } else {
      pushUiToast({ tone: 'danger', title: 'Update failed', description: resolved.message })
    }
  } finally {
    projectActionBusy.value = false
  }
}

async function handleDeleteProject(project: IdentityProject): Promise<void> {
  if (!selectedWorkspaceId.value) return
  projectActionBusy.value = true
  conflictState.value = null
  try {
    await membersV2API.deleteProject(selectedWorkspaceId.value, project.id, project.version)
    projects.value = projects.value.filter((p) => p.id !== project.id)
    confirmedMessage.value = `Project "${project.name}" deleted.`
    pushUiToast({ tone: 'success', title: 'Project deleted', description: project.name })
  } catch (error) {
    const resolved = resolveCasMutationError(error, 'project', project.id, project.version)
    if (resolved.kind === 'conflict') {
      conflictState.value = resolved.conflict
      stale.value = true
    } else {
      pushUiToast({ tone: 'danger', title: 'Delete failed', description: resolved.message })
    }
  } finally {
    projectActionBusy.value = false
  }
}

function openGrantModal(member: WorkspaceMember): void {
  grantModalMember.value = member
  grantSelectedProjects.value = projects.value.map((p) => p.id)
  conflictState.value = null
}

function toggleGrantAction(action: string, checked: boolean): void {
  if (checked) {
    if (!grantSelectedActions.value.includes(action)) {
      grantSelectedActions.value = [...grantSelectedActions.value, action]
    }
  } else {
    grantSelectedActions.value = grantSelectedActions.value.filter((a) => a !== action)
  }
}

function toggleGrantProject(projectId: string, checked: boolean): void {
  if (checked) {
    if (!grantSelectedProjects.value.includes(projectId)) {
      grantSelectedProjects.value = [...grantSelectedProjects.value, projectId]
    }
  } else {
    grantSelectedProjects.value = grantSelectedProjects.value.filter((id) => id !== projectId)
  }
}

async function handleSaveGrants(): Promise<void> {
  if (!grantModalMember.value || !selectedWorkspaceId.value) return
  grantBusy.value = true
  conflictState.value = null
  confirmedMessage.value = null
  const memberId = String(grantModalMember.value.user_id)
  try {
    const res = await membersV2API.setMemberGrants(
      selectedWorkspaceId.value,
      memberId,
      grantVersionInput.value,
      {
        actions: grantSelectedActions.value,
        project_ids: grantSelectedProjects.value
      }
    )
    grantVersionInput.value = res.version
    grantModalMember.value = null
    confirmedMessage.value = `Grants updated for member #${memberId} (version ${res.version}).`
    pushUiToast({ tone: 'success', title: 'Grants updated', description: `Version ${res.version}` })
  } catch (error) {
    const resolved = resolveCasMutationError(error, 'grants', memberId, grantVersionInput.value)
    if (resolved.kind === 'conflict') {
      conflictState.value = resolved.conflict
      stale.value = true
    } else {
      pushUiToast({ tone: 'danger', title: 'Grant update failed', description: resolved.message })
    }
  } finally {
    grantBusy.value = false
  }
}

async function handleAddMember(): Promise<void> {
  const wsId = Number(selectedWorkspaceId.value)
  const userId = Number(newMemberUserId.value)
  if (!Number.isFinite(wsId) || !Number.isFinite(userId) || userId <= 0) return
  memberMutationBusy.value = true
  try {
    await workspacesAPI.setMember(wsId, userId, newMemberRole.value)
    newMemberUserId.value = ''
    await loadWorkspaceDetails(selectedWorkspaceId.value)
    confirmedMessage.value = `Member #${userId} added as ${newMemberRole.value}.`
    pushUiToast({ tone: 'success', title: 'Member updated', description: `User #${userId}` })
  } catch (error) {
    const err = classifyAsyncError(error, 'Failed to update member')
    pushUiToast({ tone: 'danger', title: 'Member update failed', description: err.message })
  } finally {
    memberMutationBusy.value = false
  }
}

async function handleRemoveMember(userId: number): Promise<void> {
  const wsId = Number(selectedWorkspaceId.value)
  if (!Number.isFinite(wsId)) return
  memberMutationBusy.value = true
  try {
    await workspacesAPI.removeMember(wsId, userId)
    members.value = members.value.filter((m) => m.user_id !== userId)
    confirmedMessage.value = `Member #${userId} removed from workspace.`
    pushUiToast({ tone: 'success', title: 'Member removed', description: `User #${userId}` })
  } catch (error) {
    const err = classifyAsyncError(error, 'Failed to remove member')
    pushUiToast({ tone: 'danger', title: 'Remove failed', description: err.message })
  } finally {
    memberMutationBusy.value = false
  }
}

watch(selectedWorkspaceId, async (next, prev) => {
  if (next && next !== prev && !loading.value) {
    refreshing.value = true
    await loadWorkspaceDetails(next)
    refreshing.value = false
  }
})

onMounted(() => {
  loadAll(false)
})
</script>

<template>
  <div class="space-y-5">
    <PageHeader
      eyebrow="IDENTITY & ACCESS · BE-01 V2"
      title="Members & Access"
      description="Manage workspace members, project boundaries, and CAS-fenced capability grants."
    >
      <template #badges>
        <Badge v-if="selectedWorkspace" tone="accent" variant="subtle">
          {{ selectedWorkspace.role.toUpperCase() }}
        </Badge>
        <Badge v-if="roleProjection.scopeVersion" tone="neutral" variant="outline" mono>
          scope v{{ roleProjection.scopeVersion }}
        </Badge>
      </template>

      <template #actions>
        <div class="w-56">
          <Select
            v-model="selectedWorkspaceId"
            :options="workspaceOptions"
            placeholder="Select workspace"
            size="sm"
          />
        </div>
        <Button size="sm" variant="secondary" :loading="refreshing" @click="loadAll(true)">
          Refresh
        </Button>
        <Button
          v-if="roleProjection.canManageProjects"
          size="sm"
          variant="primary"
          @click="showCreateProjectModal = true"
        >
          New Project
        </Button>
      </template>

      <template #tabs>
        <Tabs v-model="activeTab" :items="tabItems" />
      </template>
    </PageHeader>

    <div v-if="loading" class="space-y-3">
      <Skeleton height="3.5rem" />
      <Skeleton height="12rem" />
    </div>

    <ErrorState
      v-else-if="loadError"
      :variant="loadError.state === 'forbidden' ? 'permission' : 'error'"
      :title="loadError.state === 'forbidden' ? 'Access Denied (403)' : 'Failed to load access scope'"
      :message="loadError.message"
      :request-id="loadError.requestId"
      @retry="loadAll(false)"
    />

    <template v-else>
      <ErrorState
        v-if="conflictState"
        variant="stale"
        title="409 Version Conflict (CAS)"
        :message="`${conflictState.message} (expected If-Match: ${conflictState.expectedVersion})`"
        :request-id="conflictState.requestId"
        retry-label="Reload Latest State"
        @retry="loadAll(true)"
      />

      <ErrorState
        v-else-if="stale"
        variant="stale"
        title="Data may be stale"
        message="A concurrent update was detected. Refresh to synchronize versions."
        :last-synced-at="lastSyncedAt"
        @retry="loadAll(true)"
      />

      <div
        v-if="partialErrors.length > 0"
        role="status"
        class="rounded-oh-md border border-oh-status-warn-border bg-oh-status-warn-bg/60 px-4 py-3 text-xs text-oh-primary"
      >
        <p class="font-semibold">Partial load warning</p>
        <ul class="mt-1 list-disc pl-4 space-y-0.5">
          <li v-for="err in partialErrors" :key="err">{{ err }}</li>
        </ul>
      </div>

      <div
        v-if="confirmedMessage"
        role="status"
        class="rounded-oh-md border border-oh-status-ok-border bg-oh-status-ok-bg/60 px-4 py-2.5 text-xs text-oh-primary flex items-center justify-between gap-2"
      >
        <span>{{ confirmedMessage }}</span>
        <button type="button" class="text-oh-muted hover:text-oh-primary" @click="confirmedMessage = null">✕</button>
      </div>

      <Card v-if="activeTab === 'members'" padding="md">
        <div class="mb-4 flex flex-wrap items-end justify-between gap-3">
          <div class="flex flex-wrap items-center gap-2">
            <div class="w-56">
              <Input v-model="searchQuery" type="search" placeholder="Filter by member ID or role…" size="sm" clearable />
            </div>
            <div class="w-40">
              <Select v-model="roleFilter" :options="roleFilterOptions" size="sm" />
            </div>
          </div>
          <form
            v-if="roleProjection.canInviteMembers"
            class="flex flex-wrap items-center gap-2"
            @submit.prevent="handleAddMember"
          >
            <div class="w-36">
              <Input v-model="newMemberUserId" type="number" placeholder="User ID" size="sm" mono />
            </div>
            <div class="w-32">
              <Select
                v-model="newMemberRole"
                :options="[
                  { value: 'member', label: 'Member' },
                  { value: 'admin', label: 'Admin' }
                ]"
                size="sm"
              />
            </div>
            <Button type="submit" size="sm" variant="primary" :loading="memberMutationBusy" :disabled="!newMemberUserId">
              Add / Update
            </Button>
          </form>
        </div>

        <Table
          :columns="memberColumns"
          :rows="filteredMembers as unknown as Array<Record<string, unknown>>"
          row-key="user_id"
          :refreshing="refreshing"
          :filtered="hasActiveFilters"
          empty-title="No workspace members"
          empty-description="Add an existing user ID or invite a collaborator to this workspace."
          filtered-empty-title="No members match the current filter"
          @clear-filters="clearFilters"
        >
          <template #cell-user_id="{ row }">
            <span class="font-mono text-xs tabular-nums">#{{ row.user_id }}</span>
          </template>
          <template #cell-role="{ row }">
            <Tag :tone="row.role === 'admin' ? 'accent' : 'neutral'">{{ String(row.role).toUpperCase() }}</Tag>
          </template>
          <template #cell-actions="{ row }">
            <div class="flex flex-wrap items-center gap-2">
              <Button
                size="xs"
                variant="secondary"
                :disabled="!roleProjection.canManageGrants"
                @click="openGrantModal(row as unknown as WorkspaceMember)"
              >
                Grants (CAS)
              </Button>
              <Button
                v-if="roleProjection.canInviteMembers"
                size="xs"
                variant="ghost"
                :disabled="memberMutationBusy"
                @click="handleRemoveMember(Number(row.user_id))"
              >
                Remove
              </Button>
            </div>
          </template>
        </Table>
      </Card>

      <Card v-else-if="activeTab === 'projects'" padding="md">
        <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
          <div class="w-64">
            <Input v-model="searchQuery" type="search" placeholder="Filter projects…" size="sm" clearable />
          </div>
          <p v-if="projectsNextCursor" class="font-mono text-xs text-oh-muted tabular-nums">
            cursor: {{ projectsNextCursor }}
          </p>
        </div>

        <Table
          :columns="projectColumns"
          :rows="filteredProjects as unknown as Array<Record<string, unknown>>"
          row-key="id"
          :refreshing="refreshing"
          :filtered="Boolean(searchQuery.trim())"
          empty-title="No projects in workspace"
          empty-description="Create a project to isolate API keys and member grants."
          @clear-filters="clearFilters"
        >
          <template #cell-version="{ row }">
            <Badge tone="neutral" variant="outline" mono>v{{ formatNullableMono(row.version as string) }}</Badge>
          </template>
          <template #cell-actions="{ row }">
            <div class="flex items-center justify-end gap-2">
              <Button
                size="xs"
                variant="secondary"
                :disabled="!roleProjection.canManageProjects"
                @click="openEditProject(row as unknown as IdentityProject)"
              >
                Rename (If-Match)
              </Button>
              <Button
                size="xs"
                variant="danger"
                :disabled="!roleProjection.canManageProjects || projectActionBusy"
                @click="handleDeleteProject(row as unknown as IdentityProject)"
              >
                Delete
              </Button>
            </div>
          </template>
        </Table>
      </Card>

      <div v-else class="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card padding="md">
          <h2 class="text-sm font-semibold text-oh-primary">Effective Role Projection</h2>
          <p class="mt-1 text-xs text-oh-secondary">
            Evaluated from BE-01 <code class="font-mono">/projects/{id}/access</code> and workspace role.
          </p>
          <dl class="mt-4 space-y-2 text-xs">
            <div class="flex items-center justify-between border-b border-oh-subtle py-1.5">
              <dt class="text-oh-muted">Actor ID</dt>
              <dd class="font-mono tabular-nums">{{ formatNullableMono(projectAccess?.actor_id) }}</dd>
            </div>
            <div class="flex items-center justify-between border-b border-oh-subtle py-1.5">
              <dt class="text-oh-muted">Scope Version</dt>
              <dd class="font-mono tabular-nums">{{ formatNullableMono(projectAccess?.scope_version) }}</dd>
            </div>
            <div class="flex items-center justify-between border-b border-oh-subtle py-1.5">
              <dt class="text-oh-muted">Manage Grants</dt>
              <dd><Tag :tone="roleProjection.canManageGrants ? 'success' : 'neutral'">{{ roleProjection.canManageGrants ? 'Allowed' : 'Read-only' }}</Tag></dd>
            </div>
            <div class="flex items-center justify-between py-1.5">
              <dt class="text-oh-muted">Manage Projects</dt>
              <dd><Tag :tone="roleProjection.canManageProjects ? 'success' : 'neutral'">{{ roleProjection.canManageProjects ? 'Allowed' : 'Read-only' }}</Tag></dd>
            </div>
          </dl>
          <div v-if="roleProjection.actions.length > 0" class="mt-3 flex flex-wrap gap-1.5">
            <Badge v-for="action in roleProjection.actions" :key="action" tone="neutral" variant="subtle" mono>
              {{ action }}
            </Badge>
          </div>
        </Card>

        <Card padding="md">
          <h2 class="text-sm font-semibold text-oh-primary">Invitations</h2>
          <p class="mt-1 text-xs text-oh-secondary">
            BE-01 v2 currently exposes workspace scopes, projects, access projections, and member grants. Email invitation tokens remain on the legacy user-onboarding path until v2 invitations land.
          </p>
          <EmptyState
            class="mt-4"
            compact
            title="No pending v2 invitations"
            description="Use Workspace Members to bind existing user IDs, or create users from Admin Users."
          />
        </Card>
      </div>
    </template>

    <Modal :open="showCreateProjectModal" title="Create Project" @close="showCreateProjectModal = false">
      <form class="space-y-4" @submit.prevent="handleCreateProject">
        <Input v-model="newProjectName" label="Project Name" placeholder="e.g. production-gateway" required />
        <div class="flex justify-end gap-2">
          <Button variant="secondary" @click="showCreateProjectModal = false">Cancel</Button>
          <Button type="submit" variant="primary" :loading="projectActionBusy">Create Project</Button>
        </div>
      </form>
    </Modal>

    <Modal :open="Boolean(editingProject)" title="Rename Project (If-Match CAS)" @close="editingProject = null">
      <form v-if="editingProject" class="space-y-4" @submit.prevent="handleSaveProject">
        <p class="font-mono text-xs text-oh-muted">If-Match: {{ editingProject.version }}</p>
        <Input v-model="editProjectName" label="Project Name" required />
        <div class="flex justify-end gap-2">
          <Button variant="secondary" @click="editingProject = null">Cancel</Button>
          <Button type="submit" variant="primary" :loading="projectActionBusy">Save Changes</Button>
        </div>
      </form>
    </Modal>

    <Modal :open="Boolean(grantModalMember)" title="Edit Member Grants (If-Match CAS)" @close="grantModalMember = null">
      <form v-if="grantModalMember" class="space-y-4" @submit.prevent="handleSaveGrants">
        <Input
          v-model="grantVersionInput"
          label="Expected Grant Version (If-Match)"
          mono
          required
        />
        <div class="space-y-2">
          <p class="text-xs font-semibold text-oh-secondary">Capability Actions</p>
          <div class="grid grid-cols-2 gap-2">
            <Checkbox
              v-for="action in AVAILABLE_ACTIONS"
              :key="action"
              :model-value="grantSelectedActions.includes(action)"
              :label="action"
              @update:model-value="(v) => toggleGrantAction(action, Boolean(v))"
            />
          </div>
        </div>
        <div v-if="projects.length > 0" class="space-y-2">
          <p class="text-xs font-semibold text-oh-secondary">Scoped Projects</p>
          <div class="grid grid-cols-2 gap-2">
            <Checkbox
              v-for="proj in projects"
              :key="proj.id"
              :model-value="grantSelectedProjects.includes(proj.id)"
              :label="proj.name"
              @update:model-value="(v) => toggleGrantProject(proj.id, Boolean(v))"
            />
          </div>
        </div>
        <div class="flex justify-end gap-2">
          <Button variant="secondary" @click="grantModalMember = null">Cancel</Button>
          <Button type="submit" variant="primary" :loading="grantBusy">Save Grants</Button>
        </div>
      </form>
    </Modal>
  </div>
</template>
