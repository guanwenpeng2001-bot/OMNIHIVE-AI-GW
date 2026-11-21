<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { apiClient } from '@/api/client'
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
import { getControlAPIBaseURL } from '@/features/shared/controlClient'
import { membersV2API } from './api'
import { projectMemberPermissions, resolveCasMutationError } from './cas'
import type {
  CasConflictState,
  IdentityAccess,
  IdentityMemberGrants,
  IdentityMemberItem,
  IdentityProject,
  IdentityWorkspace
} from './types'

interface MemberDisplayRow {
  user_id: number | string
  role: 'admin' | 'member' | string
  username?: string
  email?: string
  status?: string
  grant_version?: string
}

const props = withDefaults(
  defineProps<{
    initialWorkspaceId?: string
  }>(),
  { initialWorkspaceId: '' }
)

const { t } = useI18n()

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
const members = ref<MemberDisplayRow[]>([])
const projectAccess = ref<IdentityAccess | null>(null)

const searchQuery = ref('')
const roleFilter = ref('all')

// Project create / edit state
const showCreateProjectModal = ref(false)
const newProjectName = ref('')
const projectActionBusy = ref(false)

const editingProject = ref<IdentityProject | null>(null)
const editProjectName = ref('')

// Member grants modal
const grantModalMember = ref<MemberDisplayRow | null>(null)
const grantVersionInput = ref('1')
const grantSelectedActions = ref<string[]>([])
const grantSelectedProjects = ref<string[]>([])
const grantLoading = ref(false)
const grantLoadFailed = ref(false)
const grantBusy = ref(false)
const grantModalConflict = ref<CasConflictState | null>(null)

// Add / update workspace member
const newMemberUserId = ref('')
const newMemberRole = ref<'admin' | 'member'>('member')
const memberMutationBusy = ref(false)
const platformUsers = ref<Array<{ id: number; email: string; username?: string }>>([])
const platformUsersTotal = ref<number | null>(null)
const platformUsersLoading = ref(false)
const platformUsersError = ref<string | null>(null)
const platformUserSearchQuery = ref('')
const platformUserDropdownOpen = ref(false)
const selectedPlatformUser = ref<{ id: number; email: string; username?: string } | null>(null)
let platformUsersRequestSeq = 0
let platformUsersSearchTimer: ReturnType<typeof setTimeout> | null = null
let platformUserBlurTimer: ReturnType<typeof setTimeout> | null = null

function formatPlatformUserLabel(user: { id: number; email: string; username?: string }): string {
  return `#${user.id} · ${user.username ? `${user.username} (${user.email})` : user.email}`
}

async function loadPlatformUsersDirectory(search = ''): Promise<void> {
  const reqSeq = ++platformUsersRequestSeq
  const trimmed = search.trim()
  platformUsersLoading.value = true
  platformUsersError.value = null
  try {
    const res = await apiClient.get<{
      items?: Array<{ id: number; email: string; username?: string }>
      total?: number
    }>(
      '/admin/users',
      {
        params: {
          page: 1,
          page_size: 20,
          ...(trimmed ? { search: trimmed } : {})
        }
      }
    )
    if (reqSeq !== platformUsersRequestSeq) return
    const list = Array.isArray(res.data?.items) ? res.data.items : []
    platformUsers.value = list
    platformUsersTotal.value = typeof res.data?.total === 'number' ? res.data.total : list.length
  } catch (error) {
    if (reqSeq !== platformUsersRequestSeq) return
    const err = classifyAsyncError(error, t('membersAccess.errors.searchUsersFailed'))
    platformUsersError.value = err.message
    platformUsers.value = []
    platformUsersTotal.value = null
  } finally {
    if (reqSeq === platformUsersRequestSeq) {
      platformUsersLoading.value = false
    }
  }
}

function handlePlatformUserSearchInput(value: string | number): void {
  const query = String(value ?? '')
  platformUserSearchQuery.value = query
  newMemberUserId.value = ''
  selectedPlatformUser.value = null
  platformUsersRequestSeq++
  platformUsersError.value = null
  platformUserDropdownOpen.value = true
  if (platformUsersSearchTimer) {
    clearTimeout(platformUsersSearchTimer)
  }
  platformUsersSearchTimer = setTimeout(() => {
    platformUsersSearchTimer = null
    void loadPlatformUsersDirectory(query)
  }, 250)
}

function handleMemberUserIdInput(value: string | number): void {
  const raw = String(value ?? '')
  newMemberUserId.value = raw
  if (selectedPlatformUser.value && String(selectedPlatformUser.value.id) !== raw.trim()) {
    selectedPlatformUser.value = null
  }
}

function handlePlatformUserSearchFocus(): void {
  if (platformUserBlurTimer) {
    clearTimeout(platformUserBlurTimer)
    platformUserBlurTimer = null
  }
  platformUserDropdownOpen.value = true
}

function handlePlatformUserSearchBlur(): void {
  platformUserBlurTimer = setTimeout(() => {
    platformUserDropdownOpen.value = false
    platformUserBlurTimer = null
  }, 150)
}

function selectPlatformUser(user: { id: number; email: string; username?: string }): void {
  newMemberUserId.value = String(user.id)
  selectedPlatformUser.value = user
  platformUserSearchQuery.value = formatPlatformUserLabel(user)
  platformUserDropdownOpen.value = false
}

const canSubmitAddMember = computed(() => {
  const trimmed = newMemberUserId.value.trim()
  if (!trimmed) return false
  const parsed = Number(trimmed)
  return Number.isInteger(parsed) && parsed > 0
})

const NON_GRANTABLE_SERVER_ACTIONS = new Set([
  'projects.create',
  'members.grants.write',
  'ops.export.configure',
  'ops.restore.preview',
  'ops.restore.execute',
  'ops.incidents.publish',
  'catalog.verify',
  'catalog.publish',
  'extensions.read'
])

const ACTION_CATALOG: ReadonlyArray<{ action: string; labelKey: string }> = [
  { action: 'text.chat', labelKey: 'membersAccess.actions.textChat' },
  { action: 'text.embeddings', labelKey: 'membersAccess.actions.textEmbeddings' },
  { action: 'image.generate', labelKey: 'membersAccess.actions.imageGenerate' },
  { action: 'usage.read', labelKey: 'membersAccess.actions.usageRead' },
  { action: 'tasks.read', labelKey: 'membersAccess.actions.tasksRead' },
  { action: 'tasks.create', labelKey: 'membersAccess.actions.tasksCreate' },
  { action: 'tasks.cancel', labelKey: 'membersAccess.actions.tasksCancel' },
  { action: 'tasks.retry', labelKey: 'membersAccess.actions.tasksRetry' },
  { action: 'tasks.artifacts.read', labelKey: 'membersAccess.actions.tasksArtifactsRead' },
  { action: 'tasks.artifacts.revoke', labelKey: 'membersAccess.actions.tasksArtifactsRevoke' },
  { action: 'projects.read', labelKey: 'membersAccess.actions.projectsRead' },
  { action: 'projects.edit', labelKey: 'membersAccess.actions.projectsEdit' },
  { action: 'projects.delete', labelKey: 'membersAccess.actions.projectsDelete' },
  { action: 'models.read', labelKey: 'membersAccess.actions.modelsRead' },
  { action: 'keys.read', labelKey: 'membersAccess.actions.keysRead' },
  { action: 'keys.create', labelKey: 'membersAccess.actions.keysCreate' },
  { action: 'keys.act_as', labelKey: 'membersAccess.actions.keysActAs' },
  { action: 'credentials.byok.manage', labelKey: 'membersAccess.actions.credentialsByokManage' },
  { action: 'credentials.read_metadata', labelKey: 'membersAccess.actions.credentialsReadMetadata' },
  { action: 'credentials.rotate', labelKey: 'membersAccess.actions.credentialsRotate' },
  { action: 'credentials.revoke', labelKey: 'membersAccess.actions.credentialsRevoke' },
  { action: 'credentials.write', labelKey: 'membersAccess.actions.credentialsWrite' },
  { action: 'connections.read', labelKey: 'membersAccess.actions.connectionsRead' },
  { action: 'connections.edit', labelKey: 'membersAccess.actions.connectionsEdit' },
  { action: 'connections.verify', labelKey: 'membersAccess.actions.connectionsVerify' },
  { action: 'connections.activate', labelKey: 'membersAccess.actions.connectionsActivate' },
  { action: 'pools.read', labelKey: 'membersAccess.actions.poolsRead' },
  { action: 'pools.edit', labelKey: 'membersAccess.actions.poolsEdit' },
  { action: 'pools.assign', labelKey: 'membersAccess.actions.poolsAssign' },
  { action: 'routing.read', labelKey: 'membersAccess.actions.routingRead' },
  { action: 'routing.edit', labelKey: 'membersAccess.actions.routingEdit' },
  { action: 'routing.simulate', labelKey: 'membersAccess.actions.routingSimulate' },
  { action: 'routing.publish', labelKey: 'membersAccess.actions.routingPublish' },
  { action: 'requests.read', labelKey: 'membersAccess.actions.requestsRead' },
  { action: 'requests.content.read', labelKey: 'membersAccess.actions.requestsContentRead' },
  { action: 'requests.export', labelKey: 'membersAccess.actions.requestsExport' },
  { action: 'requests.replay', labelKey: 'membersAccess.actions.requestsReplay' },
  { action: 'billing.read', labelKey: 'membersAccess.actions.billingRead' },
  { action: 'billing.ledger.read', labelKey: 'membersAccess.actions.billingLedgerRead' },
  { action: 'billing.payers.read', labelKey: 'membersAccess.actions.billingPayersRead' },
  { action: 'billing.budgets.write', labelKey: 'membersAccess.actions.billingBudgetsWrite' },
  { action: 'billing.adjust', labelKey: 'membersAccess.actions.billingAdjust' },
  { action: 'billing.export', labelKey: 'membersAccess.actions.billingExport' },
  { action: 'billing.reconcile.read', labelKey: 'membersAccess.actions.billingReconcileRead' },
  { action: 'billing.reconcile.write', labelKey: 'membersAccess.actions.billingReconcileWrite' },
  { action: 'playground.read', labelKey: 'membersAccess.actions.playgroundRead' },
  { action: 'playground.run', labelKey: 'membersAccess.actions.playgroundRun' },
  { action: 'playground.cancel', labelKey: 'membersAccess.actions.playgroundCancel' },
  { action: 'playground.raw', labelKey: 'membersAccess.actions.playgroundRaw' },
  { action: 'audit.read', labelKey: 'membersAccess.actions.auditRead' },
  { action: 'audit.export', labelKey: 'membersAccess.actions.auditExport' },
  { action: 'audit.retention.write', labelKey: 'membersAccess.actions.auditRetentionWrite' },
  { action: 'operations.read', labelKey: 'membersAccess.actions.operationsRead' }
]

const ACTION_LABEL_KEY_BY_NAME = new Map(ACTION_CATALOG.map((item) => [item.action, item.labelKey]))

const grantActionOptions = computed(() => {
  const serverActions = projectAccess.value?.actions
  if (!Array.isArray(serverActions)) {
    return []
  }
  if (serverActions.includes('*')) {
    return ACTION_CATALOG.map((item) => ({ action: item.action, label: t(item.labelKey) }))
  }
  const allowedGrantable = serverActions.filter((act) => !NON_GRANTABLE_SERVER_ACTIONS.has(act))
  const allowedSet = new Set(allowedGrantable)
  const catalogOptions = ACTION_CATALOG.filter((item) => allowedSet.has(item.action)).map((item) => ({
    action: item.action,
    label: t(item.labelKey)
  }))
  const extraOptions = allowedGrantable
    .filter((act) => !ACTION_LABEL_KEY_BY_NAME.has(act))
    .sort()
    .map((act) => ({ action: act, label: act }))
  return [
    ...catalogOptions,
    ...extraOptions
  ]
})

const grantableActionSet = computed(() => new Set(grantActionOptions.value.map((item) => item.action)))

const outOfScopeMemberActions = computed(() =>
  grantSelectedActions.value.filter((act) => !grantableActionSet.value.has(act))
)

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
    label: `${ws.name} (#${ws.workspace_id})`
  }))
})

const roleFilterOptions = computed<UiSelectOption[]>(() => [
  { value: 'all', label: t('membersAccess.roles.all') },
  { value: 'admin', label: t('membersAccess.roles.admin') },
  { value: 'member', label: t('membersAccess.roles.member') }
])

const tabItems = computed<UiTabItem[]>(() => [
  {
    value: 'members',
    label: t('membersAccess.tabs.members'),
    count: members.value.length
  },
  {
    value: 'projects',
    label: t('membersAccess.tabs.projects'),
    count: projects.value.length
  },
  {
    value: 'roles',
    label: t('membersAccess.tabs.roles')
  }
])

const filteredMembers = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  return members.value.filter((member) => {
    if (roleFilter.value !== 'all' && member.role !== roleFilter.value) return false
    if (
      query &&
      !String(member.user_id).toLowerCase().includes(query) &&
      !member.role.toLowerCase().includes(query) &&
      !(member.username || '').toLowerCase().includes(query) &&
      !(member.email || '').toLowerCase().includes(query)
    ) {
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

const memberColumns = computed<UiTableColumn[]>(() => [
  { key: 'user_id', label: t('membersAccess.columns.memberIdentity'), sortable: true },
  { key: 'role', label: t('membersAccess.columns.workspaceRole') },
  { key: 'grant_version', label: t('membersAccess.columns.grantRevision'), mono: true },
  { key: 'actions', label: t('membersAccess.columns.actions'), align: 'right' }
])

const projectColumns = computed<UiTableColumn[]>(() => [
  { key: 'name', label: t('membersAccess.columns.projectName'), sortable: true },
  { key: 'id', label: t('membersAccess.columns.projectId'), mono: true },
  { key: 'version', label: t('membersAccess.columns.projectRevision'), mono: true },
  { key: 'actions', label: t('membersAccess.columns.actions'), align: 'right' }
])

function formatRoleLabel(role: unknown): string {
  const r = String(role || '').toLowerCase()
  if (r === 'admin') return t('membersAccess.roles.admin')
  if (r === 'member') return t('membersAccess.roles.memberShort')
  return String(role || '').toUpperCase()
}

function clearFilters(): void {
  searchQuery.value = ''
  roleFilter.value = 'all'
}

async function fetchWorkspaceMembersWithFallback(workspaceId: string): Promise<MemberDisplayRow[]> {
  try {
    const { data } = await apiClient.get<{ items?: IdentityMemberItem[] } | IdentityMemberItem[]>(
      `/workspaces/${workspaceId}/members`,
      {
        baseURL: getControlAPIBaseURL(),
        params: { limit: 100 }
      }
    )
    const rawItems = Array.isArray(data) ? data : Array.isArray(data?.items) ? data.items : null
    if (rawItems) {
      return rawItems.map((item) => ({
        user_id: item.user_id || item.id,
        role: item.role,
        username: item.username,
        email: item.email,
        status: item.status,
        grant_version: item.version
      }))
    }
  } catch {
    // Fall back to /api/v1/workspaces/:id/members when actor lacks members.grants.write or v2 unavailable
  }
  const numericId = Number(workspaceId)
  if (!Number.isFinite(numericId) || numericId <= 0) return []
  const legacyMembers: WorkspaceMember[] = await workspacesAPI.members(numericId)
  return legacyMembers.map((m) => ({
    user_id: m.user_id,
    role: m.role
  }))
}

async function fetchMemberGrants(workspaceId: string, memberId: string): Promise<IdentityMemberGrants | null> {
  const { data } = await apiClient.get<IdentityMemberGrants>(
    `/workspaces/${workspaceId}/members/${memberId}/grants`,
    { baseURL: getControlAPIBaseURL() }
  )
  return data ?? null
}

async function loadWorkspaceDetails(workspaceId: string): Promise<void> {
  if (!workspaceId) return
  partialErrors.value = []

  const [projectsRes, membersRes] = await Promise.allSettled([
    membersV2API.listProjects(workspaceId, { limit: 50 }),
    fetchWorkspaceMembersWithFallback(workspaceId)
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
    const err = classifyAsyncError(
      projectsRes.reason,
      t('membersAccess.errors.loadProjectsFailed')
    )
    if (err.state === 'forbidden') {
      loadError.value = { state: 'forbidden', message: err.message, requestId: err.requestId }
      return
    }
    partialErrors.value.push(`${t('membersAccess.errors.projectsPrefix')}: ${err.message}`)
  }

  if (membersRes.status === 'fulfilled') {
    members.value = membersRes.value
  } else {
    const err = classifyAsyncError(
      membersRes.reason,
      t('membersAccess.errors.loadMembersFailed')
    )
    if (err.state === 'forbidden') {
      partialErrors.value.push(`${t('membersAccess.errors.membersForbiddenPrefix')}: ${err.message}`)
    } else {
      partialErrors.value.push(`${t('membersAccess.errors.membersPrefix')}: ${err.message}`)
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
      const err = classifyAsyncError(
        wsRes.reason,
        t('membersAccess.errors.loadWorkspacesFailed')
      )
      if (err.state === 'forbidden' || err.state === 'unknown') {
        loadError.value = { state: err.state, message: err.message, requestId: err.requestId }
        return
      }
      partialErrors.value.push(`${t('membersAccess.errors.workspacesPrefix')}: ${err.message}`)
    }

    if (scopesRes.status === 'fulfilled') {
      v2Scopes.value = scopesRes.value.items
    } else {
      const err = classifyAsyncError(
        scopesRes.reason,
        t('membersAccess.errors.loadScopesFailed')
      )
      partialErrors.value.push(`${t('membersAccess.errors.scopesPrefix')}: ${err.message}`)
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
    confirmedMessage.value = t('membersAccess.feedback.projectCreatedMessage', {
      name: created.name,
      version: created.version
    })
    pushUiToast({
      tone: 'success',
      title: t('membersAccess.feedback.projectCreatedTitle'),
      description: created.name
    })
  } catch (error) {
    const err = classifyAsyncError(error, t('membersAccess.errors.createProjectFailed'))
    pushUiToast({
      tone: err.state === 'unknown' ? 'warning' : 'danger',
      title: t('membersAccess.errors.createProjectFailed'),
      description: err.message
    })
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
    confirmedMessage.value = t('membersAccess.feedback.projectUpdatedMessage', {
      name: updated.name,
      version: updated.version
    })
    pushUiToast({
      tone: 'success',
      title: t('membersAccess.feedback.projectUpdatedTitle'),
      description: t('membersAccess.feedback.projectRevisionDesc', { version: updated.version })
    })
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
      pushUiToast({
        tone: 'danger',
        title: t('membersAccess.errors.updateProjectFailed'),
        description: resolved.message
      })
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
    confirmedMessage.value = t('membersAccess.feedback.projectDeletedMessage', { name: project.name })
    pushUiToast({
      tone: 'success',
      title: t('membersAccess.feedback.projectDeletedTitle'),
      description: project.name
    })
  } catch (error) {
    const resolved = resolveCasMutationError(error, 'project', project.id, project.version)
    if (resolved.kind === 'conflict') {
      conflictState.value = resolved.conflict
      stale.value = true
    } else {
      pushUiToast({
        tone: 'danger',
        title: t('membersAccess.errors.deleteProjectFailed'),
        description: resolved.message
      })
    }
  } finally {
    projectActionBusy.value = false
  }
}

async function loadMemberGrantsIntoModal(memberId: string): Promise<boolean> {
  if (!selectedWorkspaceId.value) return false
  grantLoading.value = true
  grantLoadFailed.value = false
  try {
    const grants = await fetchMemberGrants(selectedWorkspaceId.value, memberId)
    if (grants) {
      grantVersionInput.value = String(grants.version || '1')
      grantSelectedProjects.value = Array.isArray(grants.project_ids) ? [...grants.project_ids] : []
      grantSelectedActions.value = Array.isArray(grants.actions) ? [...grants.actions] : []
      members.value = members.value.map((m) =>
        String(m.user_id) === memberId ? { ...m, grant_version: String(grants.version) } : m
      )
      grantModalConflict.value = null
      return true
    }
  } catch (error) {
    grantLoadFailed.value = true
    const err = classifyAsyncError(
      error,
      t('membersAccess.errors.loadGrantsFailed')
    )
    pushUiToast({
      tone: 'warning',
      title: t('membersAccess.errors.loadGrantsTitle'),
      description: err.message
    })
  } finally {
    grantLoading.value = false
  }
  return false
}

function openGrantModal(member: MemberDisplayRow): void {
  grantModalMember.value = member
  grantVersionInput.value = member.grant_version || '1'
  grantSelectedProjects.value = projects.value.map((p) => p.id)
  grantSelectedActions.value = []
  grantLoadFailed.value = false
  conflictState.value = null
  grantModalConflict.value = null
  void loadMemberGrantsIntoModal(String(member.user_id))
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
  grantModalConflict.value = null
  confirmedMessage.value = null
  const memberId = String(grantModalMember.value.user_id)
  try {
    const res = await membersV2API.setMemberGrants(
      selectedWorkspaceId.value,
      memberId,
      grantVersionInput.value,
      {
        actions: grantSelectedActions.value.filter((act) => grantableActionSet.value.has(act)),
        project_ids: grantSelectedProjects.value
      }
    )
    grantVersionInput.value = res.version
    members.value = members.value.map((m) =>
      String(m.user_id) === memberId ? { ...m, grant_version: res.version } : m
    )
    grantModalMember.value = null
    confirmedMessage.value = t('membersAccess.feedback.grantsUpdatedMessage', {
      id: memberId,
      version: res.version
    })
    pushUiToast({
      tone: 'success',
      title: t('membersAccess.feedback.grantsUpdatedTitle'),
      description: t('membersAccess.feedback.grantRevisionDesc', { version: res.version })
    })
  } catch (error) {
    const resolved = resolveCasMutationError(error, 'grants', memberId, grantVersionInput.value)
    if (resolved.kind === 'conflict') {
      conflictState.value = resolved.conflict
      grantModalConflict.value = resolved.conflict
      stale.value = true
    } else {
      pushUiToast({
        tone: 'danger',
        title: t('membersAccess.errors.saveGrantsFailed'),
        description: resolved.message
      })
    }
  } finally {
    grantBusy.value = false
  }
}

async function handleReloadLatestAndRetryGrants(): Promise<void> {
  if (!grantModalMember.value || !selectedWorkspaceId.value || grantBusy.value) return
  const memberId = String(grantModalMember.value.user_id)
  grantLoading.value = true
  try {
    const latest = await fetchMemberGrants(selectedWorkspaceId.value, memberId)
    if (latest?.version) {
      grantVersionInput.value = String(latest.version)
      grantModalConflict.value = null
      conflictState.value = null
      grantLoading.value = false
      await handleSaveGrants()
      return
    }
  } catch (error) {
    const err = classifyAsyncError(
      error,
      t('membersAccess.errors.reloadRevisionFailed')
    )
    pushUiToast({
      tone: 'danger',
      title: t('membersAccess.errors.reloadRevisionTitle'),
      description: err.message
    })
  } finally {
    grantLoading.value = false
  }
}

async function handleConflictReload(): Promise<void> {
  if (conflictState.value?.resourceType === 'grants' && selectedWorkspaceId.value) {
    const targetMemberId = conflictState.value.resourceId
    await loadMemberGrantsIntoModal(targetMemberId)
    conflictState.value = null
    stale.value = false
    return
  }
  conflictState.value = null
  await loadAll(true)
}

async function handleAddMember(): Promise<void> {
  const wsId = Number(selectedWorkspaceId.value)
  const userId = Number(newMemberUserId.value)
  if (!Number.isFinite(wsId) || !Number.isFinite(userId) || userId <= 0) return
  memberMutationBusy.value = true
  try {
    await workspacesAPI.setMember(wsId, userId, newMemberRole.value)
    newMemberUserId.value = ''
    selectedPlatformUser.value = null
    platformUserSearchQuery.value = ''
    await loadWorkspaceDetails(selectedWorkspaceId.value)
    confirmedMessage.value = t('membersAccess.feedback.memberUpdatedMessage', {
      id: userId,
      role: formatRoleLabel(newMemberRole.value)
    })
    pushUiToast({
      tone: 'success',
      title: t('membersAccess.feedback.memberUpdatedTitle'),
      description: `#${userId}`
    })
  } catch (error) {
    const err = classifyAsyncError(error, t('membersAccess.errors.updateMemberFailed'))
    pushUiToast({
      tone: 'danger',
      title: t('membersAccess.errors.updateMemberFailed'),
      description: err.message
    })
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
    confirmedMessage.value = t('membersAccess.feedback.memberRemovedMessage', { id: userId })
    pushUiToast({
      tone: 'success',
      title: t('membersAccess.feedback.memberRemovedTitle'),
      description: `#${userId}`
    })
  } catch (error) {
    const err = classifyAsyncError(error, t('membersAccess.errors.removeMemberFailed'))
    pushUiToast({
      tone: 'danger',
      title: t('membersAccess.errors.removeMemberFailed'),
      description: err.message
    })
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
  void loadPlatformUsersDirectory()
})

onUnmounted(() => {
  if (platformUsersSearchTimer) {
    clearTimeout(platformUsersSearchTimer)
    platformUsersSearchTimer = null
  }
  if (platformUserBlurTimer) {
    clearTimeout(platformUserBlurTimer)
    platformUserBlurTimer = null
  }
})
</script>

<template>
  <div class="space-y-5">
    <PageHeader
      :eyebrow="t('membersAccess.eyebrow')"
      :title="t('membersAccess.title')"
      :description="t('membersAccess.description')"
    >
      <template #badges>
        <Badge v-if="selectedWorkspace" tone="accent" variant="subtle">
          {{ formatRoleLabel(selectedWorkspace.role) }}
        </Badge>
        <Badge v-if="roleProjection.scopeVersion" tone="neutral" variant="outline" mono>
          {{ t('membersAccess.scopeBadge', { version: roleProjection.scopeVersion }) }}
        </Badge>
      </template>

      <template #actions>
        <div class="w-56">
          <Select
            v-model="selectedWorkspaceId"
            :options="workspaceOptions"
            :placeholder="t('membersAccess.selectWorkspace')"
            size="sm"
          />
        </div>
        <Button size="sm" variant="secondary" :loading="refreshing" @click="loadAll(true)">
          {{ t('membersAccess.refresh') }}
        </Button>
        <Button
          v-if="roleProjection.canManageProjects"
          size="sm"
          variant="primary"
          @click="showCreateProjectModal = true"
        >
          {{ t('membersAccess.newProject') }}
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
      :title="
        loadError.state === 'forbidden'
          ? t('membersAccess.forbiddenTitle')
          : t('membersAccess.loadFailedTitle')
      "
      :message="loadError.message"
      :request-id="loadError.requestId"
      :retry-label="t('membersAccess.retry')"
      @retry="loadAll(false)"
    />

    <template v-else>
      <ErrorState
        v-if="conflictState"
        variant="stale"
        :title="t('membersAccess.conflictTitle')"
        :message="
          conflictState.serverVersion
            ? t('membersAccess.conflictMessageWithServer', {
                message: conflictState.message,
                expected: conflictState.expectedVersion,
                server: conflictState.serverVersion
              })
            : t('membersAccess.conflictMessage', {
                message: conflictState.message,
                expected: conflictState.expectedVersion
              })
        "
        :request-id="conflictState.requestId"
        :retry-label="t('membersAccess.reloadLatestRevision')"
        @retry="handleConflictReload"
      />

      <ErrorState
        v-else-if="stale"
        variant="stale"
        :title="t('membersAccess.staleTitle')"
        :message="t('membersAccess.staleMessage')"
        :last-synced-at="lastSyncedAt"
        :retry-label="t('membersAccess.syncNow')"
        @retry="loadAll(true)"
      />

      <div
        v-if="partialErrors.length > 0"
        role="status"
        class="rounded-oh-md border border-oh-status-warn-border bg-oh-status-warn-bg/60 px-4 py-3 text-xs text-oh-primary"
      >
        <p class="font-semibold">{{ t('membersAccess.partialLoadNotice') }}</p>
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
            <div class="w-64">
              <Input
                v-model="searchQuery"
                type="search"
                :placeholder="t('membersAccess.filters.membersPlaceholder')"
                size="sm"
                clearable
              />
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
            <div class="relative w-64">
              <Input
                :model-value="platformUserSearchQuery"
                type="search"
                role="combobox"
                :aria-expanded="platformUserDropdownOpen"
                aria-controls="platform-user-search-listbox"
                data-testid="platform-user-search-input"
                :placeholder="t('membersAccess.userPicker.searchPlaceholder')"
                size="sm"
                clearable
                @update:model-value="handlePlatformUserSearchInput"
                @focus="handlePlatformUserSearchFocus"
                @blur="handlePlatformUserSearchBlur"
              />
              <div
                v-if="platformUserDropdownOpen"
                id="platform-user-search-listbox"
                role="listbox"
                data-testid="platform-user-results"
                class="absolute left-0 right-0 z-30 mt-1 max-h-56 overflow-auto rounded-oh-md border border-oh-default bg-oh-surface shadow-lg"
              >
                <p
                  v-if="platformUsersLoading"
                  data-testid="platform-user-search-loading"
                  class="px-3 py-2 text-xs text-oh-muted"
                >
                  {{ t('membersAccess.userPicker.searching') }}
                </p>
                <div
                  v-else-if="platformUsersError"
                  data-testid="platform-user-search-error"
                  role="alert"
                  class="flex items-center justify-between gap-2 px-3 py-2 text-xs text-oh-status-bad-fg"
                >
                  <span class="truncate">
                    {{ t('membersAccess.userPicker.searchFailed', { message: platformUsersError }) }}
                  </span>
                  <button
                    type="button"
                    class="shrink-0 font-medium underline"
                    @mousedown.prevent="loadPlatformUsersDirectory(platformUserSearchQuery)"
                  >
                    {{ t('membersAccess.userPicker.retrySearch') }}
                  </button>
                </div>
                <p
                  v-else-if="platformUsers.length === 0"
                  data-testid="platform-user-search-empty"
                  class="px-3 py-2 text-xs text-oh-muted"
                >
                  {{ t('membersAccess.userPicker.noMatches') }}
                </p>
                <template v-else>
                  <button
                    v-for="user in platformUsers"
                    :key="user.id"
                    type="button"
                    role="option"
                    :aria-selected="newMemberUserId === String(user.id)"
                    :data-testid="`platform-user-option-${user.id}`"
                    class="flex w-full items-center justify-between gap-2 px-3 py-1.5 text-left text-xs text-oh-primary hover:bg-oh-hover"
                    @mousedown.prevent="selectPlatformUser(user)"
                  >
                    <span class="truncate">{{ formatPlatformUserLabel(user) }}</span>
                  </button>
                  <p
                    v-if="platformUsersTotal !== null && platformUsersTotal > platformUsers.length"
                    data-testid="platform-user-truncated-hint"
                    class="border-t border-oh-subtle px-3 py-1.5 text-[11px] text-oh-muted"
                  >
                    {{
                      t('membersAccess.userPicker.truncatedHint', {
                        shown: platformUsers.length,
                        total: platformUsersTotal
                      })
                    }}
                  </p>
                </template>
              </div>
            </div>
            <div class="w-36">
              <Input
                :model-value="newMemberUserId"
                type="number"
                data-testid="new-member-user-id-input"
                :placeholder="t('membersAccess.userPicker.userIdPlaceholder')"
                size="sm"
                mono
                @update:model-value="handleMemberUserIdInput"
              />
            </div>
            <div class="w-32">
              <Select
                v-model="newMemberRole"
                :options="[
                  { value: 'member', label: t('membersAccess.roles.member') },
                  { value: 'admin', label: t('membersAccess.roles.admin') }
                ]"
                size="sm"
              />
            </div>
            <Button
              type="submit"
              size="sm"
              variant="primary"
              data-testid="add-member-submit-btn"
              :loading="memberMutationBusy"
              :disabled="!canSubmitAddMember"
            >
              {{ t('membersAccess.userPicker.addOrUpdate') }}
            </Button>
          </form>
        </div>
        <p
          v-if="selectedPlatformUser"
          data-testid="selected-platform-user-summary"
          class="mb-3 rounded-oh-md border border-oh-subtle bg-oh-subtle/50 px-3 py-1.5 font-mono text-xs text-oh-primary"
        >
          {{
            t('membersAccess.userPicker.selectedTarget', {
              id: selectedPlatformUser.id,
              email: selectedPlatformUser.email
            })
          }}
        </p>

        <div
          v-if="filteredMembers.length === 0 && !hasActiveFilters"
          class="rounded-oh-md border border-oh-subtle bg-oh-subtle/50 px-3.5 py-2.5 text-xs text-oh-secondary"
        >
          {{ t('membersAccess.membersTable.superAdminHint') }}
        </div>

        <Table
          :columns="memberColumns"
          :rows="filteredMembers as unknown as Array<Record<string, unknown>>"
          row-key="user_id"
          :refreshing="refreshing"
          :filtered="hasActiveFilters"
          :empty-title="t('membersAccess.membersTable.emptyTitle')"
          :empty-description="t('membersAccess.membersTable.emptyDescription')"
          :filtered-empty-title="t('membersAccess.membersTable.filteredEmptyTitle')"
          @clear-filters="clearFilters"
        >
          <template #cell-user_id="{ row }">
            <div class="space-y-0.5">
              <div class="flex items-center gap-1.5">
                <span class="font-mono text-xs font-medium tabular-nums text-oh-primary">#{{ row.user_id }}</span>
                <span v-if="row.username" class="text-xs font-medium text-oh-primary">{{ row.username }}</span>
              </div>
              <div v-if="row.email" class="text-[11px] text-oh-muted">{{ row.email }}</div>
            </div>
          </template>
          <template #cell-role="{ row }">
            <Tag :tone="row.role === 'admin' ? 'accent' : 'neutral'">
              {{ formatRoleLabel(row.role) }}
            </Tag>
          </template>
          <template #cell-grant_version="{ row }">
            <Badge v-if="row.grant_version" tone="neutral" variant="outline" mono>
              #{{ row.grant_version }}
            </Badge>
            <span v-else class="text-xs text-oh-muted">—</span>
          </template>
          <template #cell-actions="{ row }">
            <div class="flex flex-wrap items-center justify-end gap-2">
              <Button
                size="xs"
                variant="secondary"
                :disabled="!roleProjection.canManageGrants"
                @click="openGrantModal(row as unknown as MemberDisplayRow)"
              >
                {{ t('membersAccess.membersTable.editGrants') }}
              </Button>
              <Button
                v-if="roleProjection.canInviteMembers"
                size="xs"
                variant="ghost"
                :disabled="memberMutationBusy"
                @click="handleRemoveMember(Number(row.user_id))"
              >
                {{ t('membersAccess.membersTable.remove') }}
              </Button>
            </div>
          </template>
        </Table>
      </Card>

      <Card v-else-if="activeTab === 'projects'" padding="md">
        <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
          <div class="w-64">
            <Input
              v-model="searchQuery"
              type="search"
              :placeholder="t('membersAccess.filters.projectsPlaceholder')"
              size="sm"
              clearable
            />
          </div>
        </div>

        <Table
          :columns="projectColumns"
          :rows="filteredProjects as unknown as Array<Record<string, unknown>>"
          row-key="id"
          :refreshing="refreshing"
          :filtered="Boolean(searchQuery.trim())"
          :empty-title="t('membersAccess.projectsTable.emptyTitle')"
          :empty-description="t('membersAccess.projectsTable.emptyDescription')"
          @clear-filters="clearFilters"
        >
          <template #cell-version="{ row }">
            <Badge tone="neutral" variant="outline" mono>#{{ formatNullableMono(row.version as string) }}</Badge>
          </template>
          <template #cell-actions="{ row }">
            <div class="flex items-center justify-end gap-2">
              <Button
                size="xs"
                variant="secondary"
                :disabled="!roleProjection.canManageProjects"
                @click="openEditProject(row as unknown as IdentityProject)"
              >
                {{ t('membersAccess.projectsTable.rename') }}
              </Button>
              <Button
                size="xs"
                variant="danger"
                :disabled="!roleProjection.canManageProjects || projectActionBusy"
                @click="handleDeleteProject(row as unknown as IdentityProject)"
              >
                {{ t('membersAccess.projectsTable.delete') }}
              </Button>
            </div>
          </template>
        </Table>
      </Card>

      <div v-else class="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card padding="md">
          <h2 class="text-sm font-semibold text-oh-primary">
            {{ t('membersAccess.effectiveAccess.title') }}
          </h2>
          <p class="mt-1 text-xs text-oh-secondary">
            {{ t('membersAccess.effectiveAccess.description') }}
          </p>
          <dl class="mt-4 space-y-2 text-xs">
            <div class="flex items-center justify-between border-b border-oh-subtle py-1.5">
              <dt class="text-oh-muted">{{ t('membersAccess.effectiveAccess.actorId') }}</dt>
              <dd class="font-mono tabular-nums">{{ formatNullableMono(projectAccess?.actor_id) }}</dd>
            </div>
            <div class="flex items-center justify-between border-b border-oh-subtle py-1.5">
              <dt class="text-oh-muted">{{ t('membersAccess.effectiveAccess.scopeRevision') }}</dt>
              <dd class="font-mono tabular-nums">#{{ formatNullableMono(projectAccess?.scope_version) }}</dd>
            </div>
            <div class="flex items-center justify-between border-b border-oh-subtle py-1.5">
              <dt class="text-oh-muted">{{ t('membersAccess.effectiveAccess.manageGrants') }}</dt>
              <dd>
                <Tag :tone="roleProjection.canManageGrants ? 'success' : 'neutral'">
                  {{ roleProjection.canManageGrants ? t('membersAccess.effectiveAccess.allowed') : t('membersAccess.effectiveAccess.readOnly') }}
                </Tag>
              </dd>
            </div>
            <div class="flex items-center justify-between py-1.5">
              <dt class="text-oh-muted">{{ t('membersAccess.effectiveAccess.manageProjects') }}</dt>
              <dd>
                <Tag :tone="roleProjection.canManageProjects ? 'success' : 'neutral'">
                  {{ roleProjection.canManageProjects ? t('membersAccess.effectiveAccess.allowed') : t('membersAccess.effectiveAccess.readOnly') }}
                </Tag>
              </dd>
            </div>
          </dl>
          <div v-if="roleProjection.actions.length > 0" class="mt-3 flex flex-wrap gap-1.5">
            <Badge v-for="action in roleProjection.actions" :key="action" tone="neutral" variant="subtle" mono>
              {{ action }}
            </Badge>
          </div>
        </Card>

        <Card padding="md">
          <h2 class="text-sm font-semibold text-oh-primary">
            {{ t('membersAccess.onboarding.title') }}
          </h2>
          <p class="mt-1 text-xs text-oh-secondary">
            {{ t('membersAccess.onboarding.description') }}
          </p>
          <EmptyState
            class="mt-4"
            compact
            :title="t('membersAccess.onboarding.emptyTitle')"
            :description="t('membersAccess.onboarding.emptyDescription')"
          />
        </Card>
      </div>
    </template>

    <Modal
      :open="showCreateProjectModal"
      :title="t('membersAccess.modals.createProjectTitle')"
      @close="showCreateProjectModal = false"
    >
      <form class="space-y-4" @submit.prevent="handleCreateProject">
        <Input
          v-model="newProjectName"
          :label="t('membersAccess.modals.projectNameLabel')"
          :placeholder="t('membersAccess.modals.projectNamePlaceholder')"
          required
        />
        <div class="flex justify-end gap-2">
          <Button variant="secondary" @click="showCreateProjectModal = false">
            {{ t('membersAccess.modals.cancel') }}
          </Button>
          <Button type="submit" variant="primary" :loading="projectActionBusy">
            {{ t('membersAccess.modals.createProjectSubmit') }}
          </Button>
        </div>
      </form>
    </Modal>

    <Modal
      :open="Boolean(editingProject)"
      :title="t('membersAccess.modals.renameProjectTitle')"
      @close="editingProject = null"
    >
      <form v-if="editingProject" class="space-y-4" @submit.prevent="handleSaveProject">
        <p class="font-mono text-xs text-oh-muted">
          {{ t('membersAccess.modals.currentProjectRevision', { version: editingProject.version }) }}
        </p>
        <Input v-model="editProjectName" :label="t('membersAccess.modals.projectNameLabel')" required />
        <div class="flex justify-end gap-2">
          <Button variant="secondary" @click="editingProject = null">
            {{ t('membersAccess.modals.cancel') }}
          </Button>
          <Button type="submit" variant="primary" :loading="projectActionBusy">
            {{ t('membersAccess.modals.saveChanges') }}
          </Button>
        </div>
      </form>
    </Modal>

    <Modal
      :open="Boolean(grantModalMember)"
      :title="
        grantModalMember
          ? t('membersAccess.modals.editGrantsTitleWithId', { id: grantModalMember.user_id })
          : t('membersAccess.modals.editGrantsTitle')
      "
      @close="grantModalMember = null"
    >
      <form v-if="grantModalMember" class="space-y-4" @submit.prevent="handleSaveGrants">
        <div class="flex items-center justify-between rounded-oh-md border border-oh-default bg-oh-subtle/40 px-3 py-2 text-xs">
          <span class="text-oh-secondary">
            {{ t('membersAccess.modals.currentGrantRevision') }}:
            <strong class="font-mono tabular-nums text-oh-primary">#{{ grantVersionInput }}</strong>
          </span>
          <Button
            size="xs"
            variant="ghost"
            type="button"
            :loading="grantLoading"
            @click="loadMemberGrantsIntoModal(String(grantModalMember.user_id))"
          >
            {{ t('membersAccess.modals.reloadCurrentGrants') }}
          </Button>
        </div>

        <div
          v-if="grantModalConflict"
          class="rounded-oh-md border border-oh-status-warn-border bg-oh-status-warn-bg/60 p-3 text-xs space-y-2"
          role="alert"
        >
          <div class="font-semibold text-oh-primary">
            {{ t('membersAccess.modals.grantConflictTitle') }}
          </div>
          <p class="text-oh-secondary">
            {{
              grantModalConflict.serverVersion
                ? t('membersAccess.modals.grantConflictMessageWithServer', {
                    version: grantModalConflict.serverVersion
                  })
                : t('membersAccess.modals.grantConflictMessage')
            }}
          </p>
          <div class="flex flex-wrap items-center gap-2">
            <Button
              size="xs"
              variant="secondary"
              type="button"
              :loading="grantLoading"
              @click="loadMemberGrantsIntoModal(String(grantModalMember.user_id))"
            >
              {{ t('membersAccess.modals.loadServerGrants') }}
            </Button>
            <Button
              size="xs"
              variant="primary"
              type="button"
              :loading="grantBusy || grantLoading"
              @click="handleReloadLatestAndRetryGrants"
            >
              {{ t('membersAccess.modals.syncAndRetryGrants') }}
            </Button>
          </div>
        </div>

        <div v-if="grantLoading" class="space-y-2">
          <Skeleton height="2.5rem" />
          <Skeleton height="5rem" />
        </div>

        <div class="space-y-2">
          <p class="text-xs font-semibold text-oh-secondary">
            {{ t('membersAccess.modals.capabilityActions') }}
          </p>
          <p class="text-[11px] text-oh-muted">
            {{ t('membersAccess.modals.grantScopeHint') }}
          </p>
          <p
            v-if="grantActionOptions.length === 0"
            data-testid="grant-actions-unavailable"
            class="rounded-oh-md border border-oh-subtle bg-oh-subtle/50 px-3 py-2 text-xs text-oh-muted"
          >
            {{ t('membersAccess.modals.noGrantableActions') }}
          </p>
          <div
            v-else
            data-testid="grant-actions-grid"
            class="grid grid-cols-1 gap-2 sm:grid-cols-2 max-h-52 overflow-y-auto pr-1"
          >
            <Checkbox
              v-for="item in grantActionOptions"
              :key="item.action"
              :model-value="grantSelectedActions.includes(item.action)"
              :label="`${item.label} (${item.action})`"
              @update:model-value="(v) => toggleGrantAction(item.action, Boolean(v))"
            />
          </div>
          <div
            v-if="outOfScopeMemberActions.length > 0"
            data-testid="grant-actions-out-of-scope"
            class="rounded-oh-md border border-oh-subtle bg-oh-subtle/40 px-3 py-2 text-[11px] text-oh-muted space-y-1"
          >
            <p>{{ t('membersAccess.modals.outOfScopeActionsHint') }}</p>
            <div class="flex flex-wrap gap-1.5">
              <Badge
                v-for="act in outOfScopeMemberActions"
                :key="act"
                tone="neutral"
                variant="outline"
                mono
              >
                {{ act }}
              </Badge>
            </div>
          </div>
        </div>
        <div v-if="projects.length > 0" class="space-y-2">
          <p class="text-xs font-semibold text-oh-secondary">
            {{ t('membersAccess.modals.scopedProjects') }}
          </p>
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
          <Button variant="secondary" type="button" @click="grantModalMember = null">
            {{ t('membersAccess.modals.cancel') }}
          </Button>
          <Button type="submit" variant="primary" :loading="grantBusy" :disabled="grantLoading || grantLoadFailed || grantActionOptions.length === 0">
            {{ t('membersAccess.modals.saveGrants') }}
          </Button>
        </div>
      </form>
    </Modal>
  </div>
</template>
