<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
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

const i18n = useI18n()
const isZh = computed(() => String(i18n.locale?.value || '').toLowerCase().startsWith('zh'))

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
const grantSelectedActions = ref<string[]>(['text.chat', 'usage.read'])
const grantSelectedProjects = ref<string[]>([])
const grantLoading = ref(false)
const grantBusy = ref(false)
const grantModalConflict = ref<CasConflictState | null>(null)

// Add / update workspace member
const newMemberUserId = ref('')
const newMemberRole = ref<'admin' | 'member'>('member')
const memberMutationBusy = ref(false)

const ACTION_CATALOG: ReadonlyArray<{ action: string; labelZh: string; labelEn: string }> = [
  { action: 'text.chat', labelZh: '对话生成', labelEn: 'Chat Generation' },
  { action: 'text.embeddings', labelZh: '向量嵌入', labelEn: 'Embeddings' },
  { action: 'image.generate', labelZh: '图像生成', labelEn: 'Image Generation' },
  { action: 'usage.read', labelZh: '用量读取', labelEn: 'Read Usage' },
  { action: 'tasks.read', labelZh: '异步任务读取', labelEn: 'Read Tasks' },
  { action: 'projects.read', labelZh: '项目读取', labelEn: 'Read Projects' },
  { action: 'projects.edit', labelZh: '项目编辑', labelEn: 'Edit Projects' },
  { action: 'models.read', labelZh: '模型目录读取', labelEn: 'Read Models' },
  { action: 'keys.read', labelZh: 'API Key 查看', labelEn: 'Read API Keys' },
  { action: 'keys.create', labelZh: 'API Key 创建', labelEn: 'Create API Keys' },
  { action: 'connections.read', labelZh: '上游连接查看', labelEn: 'Read Connections' },
  { action: 'connections.edit', labelZh: '上游连接编辑', labelEn: 'Edit Connections' },
  { action: 'routing.read', labelZh: '路由策略查看', labelEn: 'Read Routing' },
  { action: 'requests.read', labelZh: '请求日志查看', labelEn: 'Read Requests' },
  { action: 'billing.read', labelZh: '账单与预算查看', labelEn: 'Read Billing' },
  { action: 'playground.run', labelZh: '调试台执行', labelEn: 'Run Playground' }
]

const grantActionOptions = computed(() => {
  const known = new Set(ACTION_CATALOG.map((item) => item.action))
  const extras = grantSelectedActions.value
    .filter((act) => !known.has(act))
    .map((act) => ({ action: act, labelZh: act, labelEn: act }))
  return [...ACTION_CATALOG, ...extras]
})

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
  { value: 'all', label: isZh.value ? '全部角色' : 'All Roles' },
  { value: 'admin', label: isZh.value ? '管理员' : 'Admin' },
  { value: 'member', label: isZh.value ? '普通成员' : 'Member' }
])

const tabItems = computed<UiTabItem[]>(() => [
  {
    value: 'members',
    label: isZh.value ? '工作空间成员' : 'Workspace Members',
    count: members.value.length
  },
  {
    value: 'projects',
    label: isZh.value ? '项目边界' : 'Projects',
    count: projects.value.length
  },
  {
    value: 'roles',
    label: isZh.value ? '权限视图与邀请说明' : 'Effective Access & Invitations'
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
  { key: 'user_id', label: isZh.value ? '成员身份' : 'Member', sortable: true },
  { key: 'role', label: isZh.value ? '工作空间角色' : 'Workspace Role' },
  { key: 'grant_version', label: isZh.value ? '权限修订' : 'Grant Revision', mono: true },
  { key: 'actions', label: isZh.value ? '操作' : 'Actions', align: 'right' }
])

const projectColumns = computed<UiTableColumn[]>(() => [
  { key: 'name', label: isZh.value ? '项目名称' : 'Project Name', sortable: true },
  { key: 'id', label: isZh.value ? '项目 ID' : 'Project ID', mono: true },
  { key: 'version', label: isZh.value ? '项目修订' : 'Revision', mono: true },
  { key: 'actions', label: isZh.value ? '操作' : 'Actions', align: 'right' }
])

function formatRoleLabel(role: unknown): string {
  const r = String(role || '').toLowerCase()
  if (r === 'admin') return isZh.value ? '管理员' : 'ADMIN'
  if (r === 'member') return isZh.value ? '成员' : 'MEMBER'
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
      isZh.value ? '加载项目列表失败' : 'Failed to load projects'
    )
    if (err.state === 'forbidden') {
      loadError.value = { state: 'forbidden', message: err.message, requestId: err.requestId }
      return
    }
    partialErrors.value.push(`${isZh.value ? '项目列表' : 'Projects'}: ${err.message}`)
  }

  if (membersRes.status === 'fulfilled') {
    members.value = membersRes.value
  } else {
    const err = classifyAsyncError(
      membersRes.reason,
      isZh.value ? '加载工作空间成员失败' : 'Failed to load workspace members'
    )
    if (err.state === 'forbidden') {
      partialErrors.value.push(`${isZh.value ? '成员列表 (403)' : 'Members (403)'}: ${err.message}`)
    } else {
      partialErrors.value.push(`${isZh.value ? '成员列表' : 'Members'}: ${err.message}`)
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
        isZh.value ? '加载工作空间失败' : 'Failed to load workspaces'
      )
      if (err.state === 'forbidden' || err.state === 'unknown') {
        loadError.value = { state: err.state, message: err.message, requestId: err.requestId }
        return
      }
      partialErrors.value.push(`${isZh.value ? '工作空间' : 'Workspaces'}: ${err.message}`)
    }

    if (scopesRes.status === 'fulfilled') {
      v2Scopes.value = scopesRes.value.items
    } else {
      const err = classifyAsyncError(
        scopesRes.reason,
        isZh.value ? '加载权限作用域失败' : 'Failed to load workspace scopes'
      )
      partialErrors.value.push(`${isZh.value ? '作用域' : 'Scopes'}: ${err.message}`)
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
    confirmedMessage.value = isZh.value
      ? `已创建项目「${created.name}」（项目修订 #${created.version}）。`
      : `Project "${created.name}" created (Revision #${created.version}).`
    pushUiToast({
      tone: 'success',
      title: isZh.value ? '项目已创建' : 'Project created',
      description: created.name
    })
  } catch (error) {
    const err = classifyAsyncError(error, isZh.value ? '创建项目失败' : 'Failed to create project')
    pushUiToast({
      tone: err.state === 'unknown' ? 'warning' : 'danger',
      title: isZh.value ? '创建项目失败' : 'Create failed',
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
    confirmedMessage.value = isZh.value
      ? `项目「${updated.name}」已保存（项目修订 #${updated.version}）。`
      : `Project "${updated.name}" saved (Revision #${updated.version}).`
    pushUiToast({
      tone: 'success',
      title: isZh.value ? '项目已更新' : 'Project updated',
      description: isZh.value ? `项目修订 #${updated.version}` : `Revision #${updated.version}`
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
        title: isZh.value ? '更新项目失败' : 'Update failed',
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
    confirmedMessage.value = isZh.value
      ? `项目「${project.name}」已归档移除。`
      : `Project "${project.name}" deleted.`
    pushUiToast({
      tone: 'success',
      title: isZh.value ? '项目已删除' : 'Project deleted',
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
        title: isZh.value ? '删除项目失败' : 'Delete failed',
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
    const err = classifyAsyncError(
      error,
      isZh.value ? '读取成员当前授权失败' : 'Failed to load current member grants'
    )
    pushUiToast({
      tone: 'warning',
      title: isZh.value ? '未能读取最新授权' : 'Could not fetch current grants',
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
  grantSelectedActions.value = ['text.chat', 'usage.read']
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
        actions: grantSelectedActions.value,
        project_ids: grantSelectedProjects.value
      }
    )
    grantVersionInput.value = res.version
    members.value = members.value.map((m) =>
      String(m.user_id) === memberId ? { ...m, grant_version: res.version } : m
    )
    grantModalMember.value = null
    confirmedMessage.value = isZh.value
      ? `成员 #${memberId} 的项目授权已更新（权限修订 #${res.version}）。`
      : `Grants updated for member #${memberId} (Revision #${res.version}).`
    pushUiToast({
      tone: 'success',
      title: isZh.value ? '成员授权已更新' : 'Grants updated',
      description: isZh.value ? `权限修订 #${res.version}` : `Revision #${res.version}`
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
        title: isZh.value ? '保存授权失败' : 'Grant update failed',
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
      isZh.value ? '获取最新权限修订失败' : 'Failed to reload latest grant revision'
    )
    pushUiToast({
      tone: 'danger',
      title: isZh.value ? '刷新修订失败' : 'Reload failed',
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
    await loadWorkspaceDetails(selectedWorkspaceId.value)
    confirmedMessage.value = isZh.value
      ? `成员 #${userId} 已更新为${formatRoleLabel(newMemberRole.value)}。`
      : `Member #${userId} updated as ${newMemberRole.value}.`
    pushUiToast({
      tone: 'success',
      title: isZh.value ? '成员角色已更新' : 'Member updated',
      description: `#${userId}`
    })
  } catch (error) {
    const err = classifyAsyncError(error, isZh.value ? '更新成员失败' : 'Failed to update member')
    pushUiToast({
      tone: 'danger',
      title: isZh.value ? '更新成员失败' : 'Member update failed',
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
    confirmedMessage.value = isZh.value
      ? `已将成员 #${userId} 移出当前工作空间。`
      : `Member #${userId} removed from workspace.`
    pushUiToast({
      tone: 'success',
      title: isZh.value ? '成员已移除' : 'Member removed',
      description: `#${userId}`
    })
  } catch (error) {
    const err = classifyAsyncError(error, isZh.value ? '移除成员失败' : 'Failed to remove member')
    pushUiToast({
      tone: 'danger',
      title: isZh.value ? '移除成员失败' : 'Remove failed',
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
})
</script>

<template>
  <div class="space-y-5">
    <PageHeader
      :eyebrow="isZh ? '身份与访问控制' : 'IDENTITY & ACCESS'"
      :title="isZh ? '成员与访问权限' : 'Members & Access'"
      :description="
        isZh
          ? '管理工作空间成员角色、项目隔离边界与细粒度能力授权。'
          : 'Manage workspace member roles, project isolation boundaries, and fine-grained capability grants.'
      "
    >
      <template #badges>
        <Badge v-if="selectedWorkspace" tone="accent" variant="subtle">
          {{ formatRoleLabel(selectedWorkspace.role) }}
        </Badge>
        <Badge v-if="roleProjection.scopeVersion" tone="neutral" variant="outline" mono>
          {{ isZh ? `作用域修订 #${roleProjection.scopeVersion}` : `Scope #${roleProjection.scopeVersion}` }}
        </Badge>
      </template>

      <template #actions>
        <div class="w-56">
          <Select
            v-model="selectedWorkspaceId"
            :options="workspaceOptions"
            :placeholder="isZh ? '选择工作空间' : 'Select workspace'"
            size="sm"
          />
        </div>
        <Button size="sm" variant="secondary" :loading="refreshing" @click="loadAll(true)">
          {{ isZh ? '刷新' : 'Refresh' }}
        </Button>
        <Button
          v-if="roleProjection.canManageProjects"
          size="sm"
          variant="primary"
          @click="showCreateProjectModal = true"
        >
          {{ isZh ? '新建项目' : 'New Project' }}
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
          ? isZh
            ? '权限不足 (403)'
            : 'Access Denied (403)'
          : isZh
            ? '加载访问权限失败'
            : 'Failed to load access scope'
      "
      :message="loadError.message"
      :request-id="loadError.requestId"
      :retry-label="isZh ? '重试' : 'Retry'"
      @retry="loadAll(false)"
    />

    <template v-else>
      <ErrorState
        v-if="conflictState"
        variant="stale"
        :title="isZh ? '权限修订冲突 (409)' : 'Revision Conflict (409)'"
        :message="
          isZh
            ? `${conflictState.message}（提交修订 #${conflictState.expectedVersion}${conflictState.serverVersion ? `，服务端最新修订 #${conflictState.serverVersion}` : ''}）`
            : `${conflictState.message} (expected revision #${conflictState.expectedVersion}${conflictState.serverVersion ? `, latest #${conflictState.serverVersion}` : ''})`
        "
        :request-id="conflictState.requestId"
        :retry-label="isZh ? '重新加载最新修订' : 'Reload Latest Revision'"
        @retry="handleConflictReload"
      />

      <ErrorState
        v-else-if="stale"
        variant="stale"
        :title="isZh ? '当前数据可能已过期' : 'Data may be stale'"
        :message="
          isZh
            ? '检测到并发修改，请刷新以同步最新权限与项目修订。'
            : 'A concurrent update was detected. Refresh to synchronize revisions.'
        "
        :last-synced-at="lastSyncedAt"
        :retry-label="isZh ? '立即同步' : 'Sync Now'"
        @retry="loadAll(true)"
      />

      <div
        v-if="partialErrors.length > 0"
        role="status"
        class="rounded-oh-md border border-oh-status-warn-border bg-oh-status-warn-bg/60 px-4 py-3 text-xs text-oh-primary"
      >
        <p class="font-semibold">{{ isZh ? '部分数据加载提示' : 'Partial load notice' }}</p>
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
                :placeholder="isZh ? '按成员 ID、用户名、邮箱或角色筛选…' : 'Filter by ID, username, email, or role…'"
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
            <div class="w-36">
              <Input
                v-model="newMemberUserId"
                type="number"
                :placeholder="isZh ? '用户 ID' : 'User ID'"
                size="sm"
                mono
              />
            </div>
            <div class="w-32">
              <Select
                v-model="newMemberRole"
                :options="[
                  { value: 'member', label: isZh ? '普通成员' : 'Member' },
                  { value: 'admin', label: isZh ? '管理员' : 'Admin' }
                ]"
                size="sm"
              />
            </div>
            <Button type="submit" size="sm" variant="primary" :loading="memberMutationBusy" :disabled="!newMemberUserId">
              {{ isZh ? '添加 / 更新角色' : 'Add / Update' }}
            </Button>
          </form>
        </div>

        <Table
          :columns="memberColumns"
          :rows="filteredMembers as unknown as Array<Record<string, unknown>>"
          row-key="user_id"
          :refreshing="refreshing"
          :filtered="hasActiveFilters"
          :empty-title="isZh ? '暂无工作空间成员' : 'No workspace members'"
          :empty-description="
            isZh ? '输入已有用户 ID 即可将其加入当前工作空间并分配角色。' : 'Add an existing user ID to grant workspace membership.'
          "
          :filtered-empty-title="isZh ? '没有符合筛选条件的成员' : 'No members match the current filter'"
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
                {{ isZh ? '项目授权' : 'Edit Grants' }}
              </Button>
              <Button
                v-if="roleProjection.canInviteMembers"
                size="xs"
                variant="ghost"
                :disabled="memberMutationBusy"
                @click="handleRemoveMember(Number(row.user_id))"
              >
                {{ isZh ? '移除' : 'Remove' }}
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
              :placeholder="isZh ? '筛选项目名称或 ID…' : 'Filter projects…'"
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
          :empty-title="isZh ? '当前工作空间暂无项目' : 'No projects in workspace'"
          :empty-description="
            isZh ? '新建项目以隔离 API Key、成员权限与预算统计。' : 'Create a project to isolate API keys and member grants.'
          "
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
                {{ isZh ? '重命名' : 'Rename' }}
              </Button>
              <Button
                size="xs"
                variant="danger"
                :disabled="!roleProjection.canManageProjects || projectActionBusy"
                @click="handleDeleteProject(row as unknown as IdentityProject)"
              >
                {{ isZh ? '删除' : 'Delete' }}
              </Button>
            </div>
          </template>
        </Table>
      </Card>

      <div v-else class="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card padding="md">
          <h2 class="text-sm font-semibold text-oh-primary">
            {{ isZh ? '当前生效权限视图' : 'Effective Access Summary' }}
          </h2>
          <p class="mt-1 text-xs text-oh-secondary">
            {{
              isZh
                ? '综合当前工作空间角色与项目授权计算的实际可执行能力。'
                : 'Evaluated from your workspace role and project-level capability grants.'
            }}
          </p>
          <dl class="mt-4 space-y-2 text-xs">
            <div class="flex items-center justify-between border-b border-oh-subtle py-1.5">
              <dt class="text-oh-muted">{{ isZh ? '当前操作者 ID' : 'Actor ID' }}</dt>
              <dd class="font-mono tabular-nums">{{ formatNullableMono(projectAccess?.actor_id) }}</dd>
            </div>
            <div class="flex items-center justify-between border-b border-oh-subtle py-1.5">
              <dt class="text-oh-muted">{{ isZh ? '作用域修订' : 'Scope Revision' }}</dt>
              <dd class="font-mono tabular-nums">#{{ formatNullableMono(projectAccess?.scope_version) }}</dd>
            </div>
            <div class="flex items-center justify-between border-b border-oh-subtle py-1.5">
              <dt class="text-oh-muted">{{ isZh ? '成员授权管理' : 'Manage Grants' }}</dt>
              <dd>
                <Tag :tone="roleProjection.canManageGrants ? 'success' : 'neutral'">
                  {{ roleProjection.canManageGrants ? (isZh ? '允许' : 'Allowed') : (isZh ? '只读' : 'Read-only') }}
                </Tag>
              </dd>
            </div>
            <div class="flex items-center justify-between py-1.5">
              <dt class="text-oh-muted">{{ isZh ? '项目边界管理' : 'Manage Projects' }}</dt>
              <dd>
                <Tag :tone="roleProjection.canManageProjects ? 'success' : 'neutral'">
                  {{ roleProjection.canManageProjects ? (isZh ? '允许' : 'Allowed') : (isZh ? '只读' : 'Read-only') }}
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
            {{ isZh ? '成员加入与协作说明' : 'Member Onboarding' }}
          </h2>
          <p class="mt-1 text-xs text-oh-secondary">
            {{
              isZh
                ? '工作空间成员通过平台已有用户账号绑定接入，项目级权限授权与成员身份自动联动。'
                : 'Workspace membership binds existing platform users; project capability grants attach to workspace members.'
            }}
          </p>
          <EmptyState
            class="mt-4"
            compact
            :title="isZh ? '暂无待处理的邀请' : 'No pending invitations'"
            :description="
              isZh
                ? '可在“工作空间成员”标签页输入用户 ID 绑定现有账号，或先前往系统用户管理创建新用户。'
                : 'Bind existing user IDs in Workspace Members, or create accounts from Admin Users first.'
            "
          />
        </Card>
      </div>
    </template>

    <Modal
      :open="showCreateProjectModal"
      :title="isZh ? '新建项目' : 'Create Project'"
      @close="showCreateProjectModal = false"
    >
      <form class="space-y-4" @submit.prevent="handleCreateProject">
        <Input
          v-model="newProjectName"
          :label="isZh ? '项目名称' : 'Project Name'"
          :placeholder="isZh ? '例如：生产环境网关' : 'e.g. production-gateway'"
          required
        />
        <div class="flex justify-end gap-2">
          <Button variant="secondary" @click="showCreateProjectModal = false">
            {{ isZh ? '取消' : 'Cancel' }}
          </Button>
          <Button type="submit" variant="primary" :loading="projectActionBusy">
            {{ isZh ? '创建项目' : 'Create Project' }}
          </Button>
        </div>
      </form>
    </Modal>

    <Modal
      :open="Boolean(editingProject)"
      :title="isZh ? '重命名项目' : 'Rename Project'"
      @close="editingProject = null"
    >
      <form v-if="editingProject" class="space-y-4" @submit.prevent="handleSaveProject">
        <p class="font-mono text-xs text-oh-muted">
          {{ isZh ? `当前项目修订：#${editingProject.version}` : `Current Revision: #${editingProject.version}` }}
        </p>
        <Input v-model="editProjectName" :label="isZh ? '项目名称' : 'Project Name'" required />
        <div class="flex justify-end gap-2">
          <Button variant="secondary" @click="editingProject = null">
            {{ isZh ? '取消' : 'Cancel' }}
          </Button>
          <Button type="submit" variant="primary" :loading="projectActionBusy">
            {{ isZh ? '保存修改' : 'Save Changes' }}
          </Button>
        </div>
      </form>
    </Modal>

    <Modal
      :open="Boolean(grantModalMember)"
      :title="
        grantModalMember
          ? isZh
            ? `配置成员项目授权（#${grantModalMember.user_id}）`
            : `Edit Member Grants (#${grantModalMember.user_id})`
          : isZh
            ? '配置成员项目授权'
            : 'Edit Member Grants'
      "
      @close="grantModalMember = null"
    >
      <form v-if="grantModalMember" class="space-y-4" @submit.prevent="handleSaveGrants">
        <div class="flex items-center justify-between rounded-oh-md border border-oh-default bg-oh-subtle/40 px-3 py-2 text-xs">
          <span class="text-oh-secondary">
            {{ isZh ? '当前权限修订' : 'Current Grant Revision' }}:
            <strong class="font-mono tabular-nums text-oh-primary">#{{ grantVersionInput }}</strong>
          </span>
          <Button
            size="xs"
            variant="ghost"
            type="button"
            :loading="grantLoading"
            @click="loadMemberGrantsIntoModal(String(grantModalMember.user_id))"
          >
            {{ isZh ? '重新读取当前授权' : 'Reload Current Grants' }}
          </Button>
        </div>

        <div
          v-if="grantModalConflict"
          class="rounded-oh-md border border-oh-status-warn-border bg-oh-status-warn-bg/60 p-3 text-xs space-y-2"
          role="alert"
        >
          <div class="font-semibold text-oh-primary">
            {{ isZh ? '检测到权限修订冲突 (409)' : 'Grant Revision Conflict (409)' }}
          </div>
          <p class="text-oh-secondary">
            {{
              isZh
                ? `其他管理员已更新该成员权限${grantModalConflict.serverVersion ? `（最新修订 #${grantModalConflict.serverVersion}）` : ''}。可重新加载服务端配置，或同步最新修订号立即重试提交当前勾选。`
                : `Another operator updated this member's grants${grantModalConflict.serverVersion ? ` (latest revision #${grantModalConflict.serverVersion})` : ''}. Reload server grants or sync the latest revision and retry.`
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
              {{ isZh ? '加载服务端最新权限' : 'Load Server Grants' }}
            </Button>
            <Button
              size="xs"
              variant="primary"
              type="button"
              :loading="grantBusy || grantLoading"
              @click="handleReloadLatestAndRetryGrants"
            >
              {{ isZh ? '同步最新修订并重试保存' : 'Sync Revision & Retry Save' }}
            </Button>
          </div>
        </div>

        <div v-if="grantLoading" class="space-y-2">
          <Skeleton height="2.5rem" />
          <Skeleton height="5rem" />
        </div>

        <div class="space-y-2">
          <p class="text-xs font-semibold text-oh-secondary">
            {{ isZh ? '允许执行的能力项 (Actions)' : 'Capability Actions' }}
          </p>
          <div class="grid grid-cols-1 gap-2 sm:grid-cols-2 max-h-52 overflow-y-auto pr-1">
            <Checkbox
              v-for="item in grantActionOptions"
              :key="item.action"
              :model-value="grantSelectedActions.includes(item.action)"
              :label="`${isZh ? item.labelZh : item.labelEn} (${item.action})`"
              @update:model-value="(v) => toggleGrantAction(item.action, Boolean(v))"
            />
          </div>
        </div>
        <div v-if="projects.length > 0" class="space-y-2">
          <p class="text-xs font-semibold text-oh-secondary">
            {{ isZh ? '生效项目范围 (Projects)' : 'Scoped Projects' }}
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
            {{ isZh ? '取消' : 'Cancel' }}
          </Button>
          <Button type="submit" variant="primary" :loading="grantBusy" :disabled="grantLoading">
            {{ isZh ? '保存授权' : 'Save Grants' }}
          </Button>
        </div>
      </form>
    </Modal>
  </div>
</template>
