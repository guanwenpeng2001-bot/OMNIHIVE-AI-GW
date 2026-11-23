<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import CopyButton from '@/components/ui/CopyButton.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import PageHeader from '@/components/ui/PageHeader.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import Switch from '@/components/ui/Switch.vue'
import Tabs from '@/components/ui/Tabs.vue'
import { pushUiToast } from '@/components/ui/useToast'
import { useAuthStore } from '@/stores/auth'
import { getConsoleScopeStore } from '@/stores/consoleScope'
import { identityApi, type IdentityKey } from '@/api/identity'
import {
  ensureProjectAccess,
  ensureWorkspaceAccess,
  getAccessActionState,
  recheckProjectActionBeforeMutation,
  recheckWorkspaceActionBeforeMutation,
  type AccessProbeEntry
} from '@/features/console/scopeAccess'
import ConsoleAsyncBanner from '@/features/console/components/ConsoleAsyncBanner.vue'
import ScopeGateEmptyState from '@/features/console/components/ScopeGateEmptyState.vue'
import { classifyConsoleError, type ConsoleAsyncNotice } from '@/features/console/asyncState'
import {
  byokApi,
  type ByokClass,
  type ByokCreatePayload,
  type ByokCredential,
  type ByokPatchPayload,
  type ByokPolicyPayload
} from '@/features/byok/api'
import {
  byokAuthTypeLabel,
  byokStateLabel,
  byokStateTone,
  formatByokTime
} from '@/features/byok/format'
import ByokCredentialFormModal from '@/features/byok/components/ByokCredentialFormModal.vue'
import ByokInspectorDrawer from '@/features/byok/components/ByokInspectorDrawer.vue'
import ByokServiceAccountsPanel from '@/features/byok/components/ByokServiceAccountsPanel.vue'

type ByokScopeTab = 'workspace' | 'project'

const { t, locale } = useI18n()
const isZh = computed(() => String(locale?.value ?? '').startsWith('zh'))
const router = useRouter()
const consoleScopeStore = getConsoleScopeStore()
const authStore = useAuthStore()

const activeScope = computed(() => consoleScopeStore.activeScope)
const activeWorkspace = computed(() => consoleScopeStore.activeWorkspace)
const activeWorkspaceId = computed(() =>
  activeScope.value.mode === 'workspace' ? activeScope.value.workspaceId : null
)
const availableProjects = computed(() => consoleScopeStore.availableProjects)
const activeProject = computed(() => consoleScopeStore.activeProject)
const activeProjectId = computed(() =>
  activeScope.value.mode === 'workspace' ? activeScope.value.projectId : null
)
const scopeLoading = computed(() => consoleScopeStore.scopesLoading)
const lastSyncedAt = computed(() => consoleScopeStore.scopesAsOf)
const scopeError = computed(() => consoleScopeStore.scopesError)
const isScopeUnavailable = computed(
  () =>
    activeScope.value.mode === 'workspace' &&
    consoleScopeStore.scopeValidationStatus === 'unavailable'
)

const activeTab = ref<ByokScopeTab>('workspace')
const credentials = ref<ByokCredential[]>([])
const nextCursor = ref<string | null>(null)
const providerClasses = ref<ByokClass[]>([])
const projectKeys = ref<IdentityKey[]>([])

const loadingList = ref(false)
const loadingMore = ref(false)
const accessLoading = ref(false)
const pageNotice = ref<ConsoleAsyncNotice | null>(null)
const rowBusyId = ref<string | null>(null)
const workspaceAccessEntry = ref<AccessProbeEntry | null>(null)
const projectAccessEntry = ref<AccessProbeEntry | null>(null)
const canManageByok = ref(false)
const canReadKeys = ref(false)
let loadGeneration = 0

// Modal state (create & policy)
const formModalOpen = ref(false)
const formMode = ref<'create' | 'policy'>('create')
const editingCredential = ref<ByokCredential | null>(null)
const formSaving = ref(false)
const formErrorNotice = ref<ConsoleAsyncNotice | null>(null)

// Inspector drawer state
const inspectorOpen = ref(false)
const inspectedCredential = ref<ByokCredential | null>(null)

const actorUserId = computed(() => {
  const raw = authStore.user?.id
  return raw !== undefined && raw !== null ? String(raw) : ''
})

const tabItems = computed(() => [
  {
    value: 'workspace',
    label: isZh.value ? '工作空间密钥 (Workspace BYOK)' : 'Workspace BYOK',
    badge: activeTab.value === 'workspace' ? String(credentials.value.length) : undefined
  },
  {
    value: 'project',
    label: isZh.value ? '项目授权与绑定 (Project BYOK)' : 'Project BYOK',
    badge: activeTab.value === 'project' ? String(credentials.value.length) : undefined
  }
])

const enabledProviderClasses = computed(() =>
  providerClasses.value.filter((item) => item.supported && item.enabled)
)

const hasByokManagePermission = computed(
  () => canManageByok.value && pageNotice.value?.kind !== 'permission'
)
const activeAccessEntry = computed(() =>
  activeTab.value === 'project' ? projectAccessEntry.value : workspaceAccessEntry.value
)
const scopeGateState = computed<'empty' | 'denied' | 'unavailable' | null>(() => {
  if (isScopeUnavailable.value) return 'unavailable'
  if (activeScope.value.mode === 'personal' || !activeWorkspaceId.value) {
    return 'empty'
  }
  if (activeTab.value === 'project' && !activeProjectId.value) {
    return 'empty'
  }
  const entry = activeAccessEntry.value
  if (!entry || entry.status === 'idle' || entry.status === 'loading') return null
  if (entry.status === 'unavailable') return 'unavailable'
  if (entry.status === 'denied' || !canManageByok.value) return 'denied'
  return null
})

const scopeErrorNotice = computed<ConsoleAsyncNotice | null>(() => {
  if (!scopeError.value) return null
  return classifyConsoleError(scopeError.value, {
    isZh: isZh.value,
    requiredGrantFallback: 'credentials.byok.manage'
  })
})

async function loadProjectKeysSilently(wsId: string, projId: string): Promise<void> {
  if (!wsId || !projId || !canReadKeys.value) {
    projectKeys.value = []
    return
  }
  try {
    const page = await identityApi.listProjectKeys(wsId, projId, { limit: 50 })
    projectKeys.value = page.items ?? []
  } catch {
    projectKeys.value = []
  }
}

async function refreshByokAccess(
  wsId: string,
  projId: string | null,
  force = false
): Promise<boolean> {
  if (!wsId || (activeTab.value === 'project' && !projId)) {
    canManageByok.value = false
    canReadKeys.value = false
    return false
  }
  accessLoading.value = true
  try {
    const entry =
      activeTab.value === 'project' && projId
        ? await ensureProjectAccess(wsId, projId, { force })
        : await ensureWorkspaceAccess(wsId, { force })
    if (activeTab.value === 'project') {
      projectAccessEntry.value = entry
    } else {
      workspaceAccessEntry.value = entry
    }
    if (entry.status !== 'ready') {
      canManageByok.value = false
      canReadKeys.value = false
      return false
    }
    canManageByok.value = getAccessActionState(entry, 'credentials.byok.manage') === 'allowed'
    canReadKeys.value = getAccessActionState(entry, 'keys.read') === 'allowed'
    return canManageByok.value
  } finally {
    accessLoading.value = false
  }
}

async function recheckByokMutation(wsId: string, projId: string | null): Promise<boolean> {
  const recheck =
    activeTab.value === 'project' && projId
      ? await recheckProjectActionBeforeMutation(wsId, projId, 'credentials.byok.manage')
      : await recheckWorkspaceActionBeforeMutation(wsId, 'credentials.byok.manage')
  if (activeTab.value === 'project') {
    projectAccessEntry.value = recheck.entry
  } else {
    workspaceAccessEntry.value = recheck.entry
  }
  if (recheck.state !== 'allowed') {
    canManageByok.value = false
    return false
  }
  canManageByok.value = true
  return true
}

async function loadSurface(reset = true): Promise<void> {
  if (consoleScopeStore.untrustedPending && consoleScopeStore.scopeValidationStatus === 'validating') {
    await consoleScopeStore.fetchScopes({ force: false })
  }
  const wsId = activeWorkspaceId.value
  const projId = activeProjectId.value
  if (!wsId || isScopeUnavailable.value || (activeTab.value === 'project' && !projId)) {
    canManageByok.value = false
    canReadKeys.value = false
    credentials.value = []
    nextCursor.value = null
    providerClasses.value = []
    projectKeys.value = []
    pageNotice.value = null
    return
  }
  if (reset) {
    const allowed = await refreshByokAccess(wsId, projId, false)
    if (!allowed) {
      credentials.value = []
      nextCursor.value = null
      providerClasses.value = []
      projectKeys.value = []
      loadingList.value = false
      loadingMore.value = false
      pageNotice.value = null
      return
    }
  }
  if (!canManageByok.value) {
    credentials.value = []
    nextCursor.value = null
    providerClasses.value = []
    projectKeys.value = []
    loadingList.value = false
    loadingMore.value = false
    return
  }
  if (!reset && (!nextCursor.value || loadingMore.value)) return

  const currentGen = ++loadGeneration
  if (reset) {
    loadingList.value = true
    pageNotice.value = null
  } else {
    loadingMore.value = true
  }

  try {
    if (activeTab.value === 'project' && projId) {
      const [credPage, clsResp] = await Promise.all([
        byokApi.listProjectCredentials(wsId, projId, {
          cursor: reset ? undefined : (nextCursor.value ?? undefined),
          limit: 25
        }),
        reset ? byokApi.listProjectClasses(wsId, projId) : Promise.resolve({ items: providerClasses.value })
      ])
      if (currentGen !== loadGeneration) return
      credentials.value = reset
        ? credPage.items ?? []
        : [...credentials.value, ...(credPage.items ?? [])]
      nextCursor.value = credPage.next_cursor ?? null
      if (reset) providerClasses.value = clsResp.items ?? []
    } else {
      const [credPage, clsResp] = await Promise.all([
        byokApi.listWorkspaceCredentials(wsId, {
          cursor: reset ? undefined : (nextCursor.value ?? undefined),
          limit: 25
        }),
        reset ? byokApi.listWorkspaceClasses(wsId) : Promise.resolve({ items: providerClasses.value })
      ])
      if (currentGen !== loadGeneration) return
      credentials.value = reset
        ? credPage.items ?? []
        : [...credentials.value, ...(credPage.items ?? [])]
      nextCursor.value = credPage.next_cursor ?? null
      if (reset) providerClasses.value = clsResp.items ?? []
    }

    if (reset && projId && canReadKeys.value) {
      void loadProjectKeysSilently(wsId, projId)
    }
  } catch (err) {
    if (currentGen !== loadGeneration) return
    pageNotice.value = classifyConsoleError(err, {
      hasCachedData: credentials.value.length > 0,
      lastSyncedAt: lastSyncedAt.value ?? undefined,
      isZh: isZh.value,
      requiredGrantFallback: 'credentials.byok.manage'
    })
  } finally {
    if (currentGen === loadGeneration) {
      loadingList.value = false
      loadingMore.value = false
    }
  }
}

function upsertLocalCredential(updated: ByokCredential): void {
  const idx = credentials.value.findIndex((item) => item.id === updated.id)
  if (idx >= 0) {
    credentials.value.splice(idx, 1, updated)
  } else {
    credentials.value = [updated, ...credentials.value]
  }
  if (editingCredential.value?.id === updated.id) {
    editingCredential.value = updated
  }
  if (inspectedCredential.value?.id === updated.id) {
    inspectedCredential.value = updated
  }
}

function removeLocalCredential(id: string): void {
  credentials.value = credentials.value.filter((item) => item.id !== id)
}

function openCreateModal(): void {
  if (!hasByokManagePermission.value) return
  formMode.value = 'create'
  editingCredential.value = null
  formErrorNotice.value = null
  formModalOpen.value = true
}

function openPolicyModal(cred: ByokCredential): void {
  formMode.value = 'policy'
  editingCredential.value = cred
  formErrorNotice.value = null
  formModalOpen.value = true
}

function openInspector(cred: ByokCredential): void {
  inspectedCredential.value = cred
  inspectorOpen.value = true
}

async function handleCreateSubmit(payload: ByokCreatePayload): Promise<void> {
  const wsId = activeWorkspaceId.value
  const projId = activeProjectId.value
  if (!wsId || formSaving.value) return
  if (!(await recheckByokMutation(wsId, projId))) {
    formErrorNotice.value = classifyConsoleError(
      { status: activeAccessEntry.value?.error?.status || 403, message: activeAccessEntry.value?.error?.message || 'Permission denied' },
      { isZh: isZh.value, requiredGrantFallback: 'credentials.byok.manage' }
    )
    return
  }
  formSaving.value = true
  formErrorNotice.value = null
  try {
    const created =
      activeTab.value === 'project' && projId
        ? await byokApi.createProjectCredential(wsId, projId, payload)
        : await byokApi.createWorkspaceCredential(wsId, payload)
    upsertLocalCredential(created)
    formModalOpen.value = false
    pushUiToast({
      tone: 'success',
      title: isZh.value ? '自带密钥登记成功' : 'BYOK Credential Enrolled',
      description: `${created.provider} · ${created.fingerprint}`
    })
  } catch (err) {
    formErrorNotice.value = classifyConsoleError(err, {
      isZh: isZh.value,
      requiredGrantFallback: 'credentials.byok.manage'
    })
  } finally {
    formSaving.value = false
  }
}

async function handlePolicySubmit(payload: {
  id: string
  version: string
  policy: ByokPolicyPayload
}): Promise<void> {
  const wsId = activeWorkspaceId.value
  const projId = activeProjectId.value
  if (!wsId || formSaving.value) return
  if (!(await recheckByokMutation(wsId, projId))) {
    formErrorNotice.value = classifyConsoleError(
      { status: activeAccessEntry.value?.error?.status || 403, message: activeAccessEntry.value?.error?.message || 'Permission denied' },
      { isZh: isZh.value, requiredGrantFallback: 'credentials.byok.manage' }
    )
    return
  }
  formSaving.value = true
  formErrorNotice.value = null
  try {
    const updated =
      activeTab.value === 'project' && projId
        ? await byokApi.putProjectPolicy(wsId, projId, payload.id, payload.version, payload.policy)
        : await byokApi.putWorkspacePolicy(wsId, payload.id, payload.version, payload.policy)
    upsertLocalCredential(updated)
    formModalOpen.value = false
    pushUiToast({
      tone: 'success',
      title: isZh.value ? '凭据策略已更新' : 'Credential Policy Updated'
    })
  } catch (err) {
    formErrorNotice.value = classifyConsoleError(err, {
      isZh: isZh.value,
      requiredGrantFallback: 'credentials.byok.manage'
    })
  } finally {
    formSaving.value = false
  }
}

async function reloadEditingCredential(): Promise<void> {
  const target = editingCredential.value
  const wsId = activeWorkspaceId.value
  const projId = activeProjectId.value
  if (!target || !wsId) return
  formErrorNotice.value = null
  try {
    const fresh =
      activeTab.value === 'project' && projId
        ? await byokApi.getProjectCredential(wsId, projId, target.id)
        : await byokApi.getWorkspaceCredential(wsId, target.id)
    upsertLocalCredential(fresh)
  } catch (err) {
    formErrorNotice.value = classifyConsoleError(err, {
      isZh: isZh.value,
      requiredGrantFallback: 'credentials.byok.manage'
    })
  }
}

async function handleVerifyProbe(cred: ByokCredential): Promise<void> {
  const wsId = activeWorkspaceId.value
  const projId = activeProjectId.value
  if (!wsId || rowBusyId.value) return
  if (!(await recheckByokMutation(wsId, projId))) {
    return
  }
  rowBusyId.value = cred.id
  pageNotice.value = null
  try {
    const op =
      activeTab.value === 'project' && projId
        ? await byokApi.verifyProjectCredential(wsId, projId, cred.id, cred.version)
        : await byokApi.verifyWorkspaceCredential(wsId, cred.id, cred.version)
    const refreshed =
      activeTab.value === 'project' && projId
        ? await byokApi.getProjectCredential(wsId, projId, cred.id)
        : await byokApi.getWorkspaceCredential(wsId, cred.id)
    upsertLocalCredential(refreshed)
    pushUiToast({
      tone: refreshed.state === 'verified' ? 'success' : 'warning',
      title:
        refreshed.state === 'verified'
          ? (isZh.value ? '上游连通性验证通过' : 'Upstream Verification Passed')
          : (isZh.value ? `探测已完成 (${op.state})` : `Verification Completed (${op.state})`),
      description:
        refreshed.verified_model_ids.length > 0
          ? (isZh.value ? `已验证 ${refreshed.verified_model_ids.length} 个模型` : `${refreshed.verified_model_ids.length} verified model(s)`)
          : undefined
    })
  } catch (err) {
    pageNotice.value = classifyConsoleError(err, {
      isZh: isZh.value,
      requiredGrantFallback: 'credentials.byok.manage'
    })
  } finally {
    rowBusyId.value = null
  }
}

async function handleQuickPatch(cred: ByokCredential, patch: ByokPatchPayload): Promise<void> {
  const wsId = activeWorkspaceId.value
  const projId = activeProjectId.value
  if (!wsId || rowBusyId.value) return
  if (!(await recheckByokMutation(wsId, projId))) {
    return
  }
  rowBusyId.value = cred.id
  pageNotice.value = null
  try {
    const updated =
      activeTab.value === 'project' && projId
        ? await byokApi.patchProjectCredential(wsId, projId, cred.id, cred.version, patch)
        : await byokApi.patchWorkspaceCredential(wsId, cred.id, cred.version, patch)
    upsertLocalCredential(updated)
    pushUiToast({
      tone: 'success',
      title: isZh.value ? '调度开关已即时生效' : 'Routing Policy Updated'
    })
  } catch (err) {
    pageNotice.value = classifyConsoleError(err, {
      isZh: isZh.value,
      requiredGrantFallback: 'credentials.byok.manage'
    })
  } finally {
    rowBusyId.value = null
  }
}

function handleSwitchPersonal(): void {
  consoleScopeStore.selectPersonalScope()
}

function handleSelectProject(payload: { workspaceId: string; projectId: string }): void {
  consoleScopeStore.selectWorkspaceScope(payload.workspaceId, payload.projectId)
}

async function handleFullRefresh(): Promise<void> {
  await consoleScopeStore.fetchScopes({ force: true })
  if (activeWorkspaceId.value) {
    await refreshByokAccess(activeWorkspaceId.value, activeProjectId.value, true)
  }
  await loadSurface(true)
}

watch([activeWorkspaceId, activeProjectId, activeTab, () => consoleScopeStore.scopeValidationStatus], () => {
  credentials.value = []
  nextCursor.value = null
  providerClasses.value = []
  projectKeys.value = []
  pageNotice.value = null
  void loadSurface(true)
})

onMounted(async () => {
  if (!consoleScopeStore.scopesLoaded) {
    await consoleScopeStore.fetchScopes({ force: false })
  }
  if (activeWorkspaceId.value) {
    await loadSurface(true)
  }
})
</script>

<template>
  <AppLayout>
    <div data-testid="byok-view" class="min-w-0 space-y-5">
      <PageHeader
        :eyebrow="isZh ? '自带密钥托管 · SCOPED BYOK' : 'SCOPED BYOK CREDENTIALS'"
        :title="isZh ? '自带上游密钥 (BYOK)' : 'Bring Your Own Key (BYOK)'"
        :description="
          isZh
            ? '在工作空间或项目维度托管自有供应商 API Key 与服务账号凭据。支持实时探测验证、并发安全策略绑定、外部成本试算与全链路审计。'
            : 'Enroll and govern workspace or project-scoped upstream credentials with verification probes, concurrent-safe policies, cost estimates, and lifecycle audit trails.'
        "
      >
        <template #badges>
          <Badge v-if="activeWorkspace" tone="neutral" variant="subtle" mono>
            {{ activeWorkspace.name }}
          </Badge>
          <Badge v-else tone="neutral" variant="subtle" mono>
            {{ t('scope.personal.label') }}
          </Badge>
          <Badge v-if="activeTab === 'project' && activeProject" tone="accent" variant="subtle" mono>
            {{ activeProject.name }}
          </Badge>
        </template>

        <template #actions>
          <Button variant="secondary" size="sm" :loading="loadingList" @click="loadSurface(true)">
            {{ isZh ? '刷新列表' : 'Refresh' }}
          </Button>
          <Button
            variant="primary"
            size="sm"
            data-testid="byok-enroll-btn"
            :disabled="!hasByokManagePermission || !activeWorkspaceId || (activeTab === 'project' && !activeProjectId)"
            @click="openCreateModal"
          >
            {{
              activeTab === 'project'
                ? (isZh ? '新建项目绑定密钥' : 'Enroll Project Key')
                : (isZh ? '登记工作空间密钥' : 'Enroll Workspace Key')
            }}
          </Button>
        </template>

        <template #tabs>
          <Tabs
            :model-value="activeTab"
            :items="tabItems"
            :aria-label="isZh ? '自带密钥作用域切换' : 'BYOK scope level tabs'"
            @update:model-value="activeTab = $event as ByokScopeTab"
          />
        </template>
      </PageHeader>

      <ConsoleAsyncBanner
        v-if="scopeErrorNotice"
        :notice="scopeErrorNotice"
        :retry-label="isZh ? '重试加载作用域' : 'Retry Scope'"
        @retry="handleFullRefresh"
      />

      <ConsoleAsyncBanner
        v-if="pageNotice"
        :notice="pageNotice"
        :retry-label="
          pageNotice.kind === 'conflict'
            ? (isZh ? '重新拉取最新版本' : 'Reload Latest State')
            : (isZh ? '重新加载' : 'Retry')
        "
        dismissible
        @retry="loadSurface(true)"
        @dismiss="pageNotice = null"
      />

      <div
        v-if="enabledProviderClasses.length > 0"
        data-testid="byok-provider-classes"
        class="flex flex-wrap items-center gap-2 rounded-oh-md border border-oh-default bg-oh-surface px-3.5 py-2.5 text-xs"
      >
        <span class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">
          {{ isZh ? '已开通上游类型' : 'Eligible Classes' }}
        </span>
        <span
          v-for="cls in enabledProviderClasses"
          :key="`${cls.provider}-${cls.auth_type}`"
          class="inline-flex items-center gap-1 rounded-oh-xs border border-oh-subtle bg-oh-subtle px-2 py-0.5 font-mono text-[11px] text-oh-secondary"
        >
          <span class="font-medium text-oh-primary">{{ cls.provider }}</span>
          <span class="text-oh-muted">· {{ cls.auth_type }}</span>
        </span>
      </div>

      <Skeleton
        v-if="scopeLoading || loadingList || accessLoading"
        :lines="6"
        :label="isZh ? '正在读取自带密钥配置' : 'Loading BYOK credentials'"
      />

      <ScopeGateEmptyState
        v-else-if="scopeGateState !== null"
        :state="scopeGateState"
        surface="byok"
        :title="
          scopeGateState === 'empty'
            ? (!activeWorkspaceId ? t('rbac.byok.workspaceRequired') : t('rbac.byok.projectRequired'))
            : undefined
        "
        :workspace-id="activeWorkspaceId"
        :workspace-name="activeWorkspace?.name ?? null"
        :project-id="activeProjectId"
        :project-name="activeProject?.name ?? null"
        required-grant="credentials.byok.manage"
        :description="activeAccessEntry?.error?.message ?? undefined"
        :http-status="activeAccessEntry?.error?.status ?? null"
        :request-id="activeAccessEntry?.error?.requestId ?? null"
        :project-options="consoleScopeStore.availableProjectOptions"
        :show-retry="scopeGateState === 'unavailable'"
        @switch-personal="handleSwitchPersonal"
        @select-project="handleSelectProject"
        @request-access="router.push('/workspaces?requestAccess=1')"
        @retry="handleFullRefresh"
      />

      <template v-else>
        <EmptyState
          v-if="credentials.length === 0 && !pageNotice"
          :title="
            activeTab === 'project'
              ? (isZh ? '当前项目暂未绑定专属自带密钥' : 'No Project BYOK Credentials Bound')
              : (isZh ? '当前工作空间暂未登记自带密钥' : 'No Workspace BYOK Credentials Enrolled')
          "
          :description="
            activeTab === 'project'
              ? (isZh
                  ? '登记项目专属密钥后，项目内指定成员或服务账号在使用受权 API Key 请求目标模型时将优先使用您的上游密钥。'
                  : 'Enroll a project-bound credential so authorized members, service accounts, and API keys route directly through your upstream key.')
              : (isZh
                  ? '在工作空间统一托管 OpenAI、Anthropic、Gemini、DeepSeek 等自有凭据，并按需跨项目精细分配模型与预算策略。'
                  : 'Enroll your own OpenAI, Anthropic, Gemini, or compatible keys at the workspace level and scope them across projects and keys.')
          "
          :action-label="
            hasByokManagePermission
              ? (activeTab === 'project'
                  ? (isZh ? '登记首个项目密钥' : 'Enroll First Project Key')
                  : (isZh ? '登记首个工作空间密钥' : 'Enroll First Workspace Key'))
              : undefined
          "
          @action="openCreateModal"
        />

        <div v-else-if="credentials.length > 0" class="space-y-3">
          <article
            v-for="cred in credentials"
            :key="cred.id"
            data-testid="byok-credential-card"
            class="rounded-oh-lg border border-oh-default bg-oh-surface p-4 transition-colors hover:border-oh-strong sm:p-5"
          >
            <div class="flex flex-wrap items-start justify-between gap-3">
              <div class="min-w-0 space-y-1.5">
                <div class="flex flex-wrap items-center gap-2">
                  <span class="font-mono text-sm font-semibold text-oh-primary">
                    {{ cred.provider }}
                  </span>
                  <Badge tone="neutral" size="xs" variant="outline" mono>
                    {{ byokAuthTypeLabel(cred.auth_type, isZh) }}
                  </Badge>
                  <Badge :tone="byokStateTone(cred.state)" size="xs" variant="subtle" dot>
                    {{ byokStateLabel(cred.state, isZh) }}
                  </Badge>
                  <Badge tone="neutral" size="xs" variant="subtle" mono>
                    {{ isZh ? `优先级 ${cred.priority}` : `Prio ${cred.priority}` }}
                  </Badge>
                </div>

                <div class="flex flex-wrap items-center gap-2 font-mono text-xs text-oh-muted">
                  <span>#{{ cred.id }}</span>
                  <span>·</span>
                  <span class="truncate text-oh-secondary">{{ cred.fingerprint }}</span>
                  <CopyButton
                    :value="cred.fingerprint"
                    size="xs"
                    :aria-label="isZh ? '复制密钥指纹' : 'Copy fingerprint'"
                  />
                  <template v-if="cred.verified_until">
                    <span>·</span>
                    <span>
                      {{ isZh ? `有效验证至 ${formatByokTime(cred.verified_until)}` : `Verified until ${formatByokTime(cred.verified_until)}` }}
                    </span>
                  </template>
                </div>
              </div>

              <div class="flex flex-wrap items-center gap-2">
                <Button
                  variant="secondary"
                  size="xs"
                  data-testid="byok-verify-btn"
                  :disabled="!hasByokManagePermission || cred.state === 'revoked' || rowBusyId !== null"
                  :loading="rowBusyId === cred.id"
                  @click="handleVerifyProbe(cred)"
                >
                  {{ isZh ? '探测验证' : 'Verify Probe' }}
                </Button>
                <Button
                  variant="secondary"
                  size="xs"
                  data-testid="byok-policy-btn"
                  :disabled="!hasByokManagePermission || cred.state === 'revoked' || rowBusyId !== null"
                  @click="openPolicyModal(cred)"
                >
                  {{ isZh ? '编辑策略' : 'Edit Policy' }}
                </Button>
                <Button
                  variant="ghost"
                  size="xs"
                  data-testid="byok-inspect-btn"
                  @click="openInspector(cred)"
                >
                  {{ isZh ? '估算 · 审计 · 轮换' : 'Estimate & Audit' }}
                </Button>
              </div>
            </div>

            <div class="mt-3.5 grid gap-3 border-t border-oh-subtle pt-3.5 text-xs sm:grid-cols-3">
              <div>
                <span class="block font-mono text-[10px] uppercase tracking-wider text-oh-muted">
                  {{ isZh ? '允许路由模型' : 'Allowed Models' }}
                </span>
                <div class="mt-1 flex flex-wrap gap-1">
                  <span
                    v-for="m in cred.scope.model_ids.slice(0, 5)"
                    :key="m"
                    :class="[
                      'rounded-oh-xs border px-1.5 py-0.5 font-mono text-[11px]',
                      cred.verified_model_ids.includes(m)
                        ? 'border-emerald-500/30 bg-emerald-500/10 text-oh-primary'
                        : 'border-oh-subtle bg-oh-subtle text-oh-secondary'
                    ]"
                  >
                    {{ m }}
                  </span>
                  <span
                    v-if="cred.scope.model_ids.length > 5"
                    class="rounded-oh-xs bg-oh-subtle px-1.5 py-0.5 font-mono text-[11px] text-oh-muted"
                  >
                    +{{ cred.scope.model_ids.length - 5 }}
                  </span>
                </div>
              </div>

              <div>
                <span class="block font-mono text-[10px] uppercase tracking-wider text-oh-muted">
                  {{ isZh ? '授权作用域' : 'Authorized Scope' }}
                </span>
                <p class="mt-1 font-mono text-[11px] text-oh-secondary">
                  {{ isZh ? '项目' : 'Projects' }}: {{ cred.scope.project_ids.map((id) => `#${id}`).join(', ') || '—' }}
                  · Keys: {{ cred.scope.key_ids.map((id) => `#${id}`).join(', ') || '—' }}
                </p>
                <p class="mt-0.5 font-mono text-[11px] text-oh-muted">
                  {{ isZh ? '主体' : 'Principals' }}:
                  <template v-if="cred.scope.member_ids.length">
                    {{ isZh ? '成员' : 'Members' }} {{ cred.scope.member_ids.map((id) => `#${id}`).join(', ') }}
                  </template>
                  <template v-if="cred.scope.service_account_ids.length">
                    · SA {{ cred.scope.service_account_ids.map((id) => `#${id}`).join(', ') }}
                  </template>
                </p>
              </div>

              <div class="flex flex-wrap items-center gap-4 sm:justify-end">
                <Switch
                  :model-value="cred.allow_platform_fallback"
                  size="sm"
                  :disabled="!hasByokManagePermission || cred.state === 'revoked' || rowBusyId === cred.id"
                  :label="isZh ? '平台池回退' : 'Platform Fallback'"
                  @update:model-value="handleQuickPatch(cred, { allow_platform_fallback: $event })"
                />
                <Switch
                  :model-value="cred.include_external_in_budget"
                  size="sm"
                  :disabled="!hasByokManagePermission || cred.state === 'revoked' || rowBusyId === cred.id"
                  :label="isZh ? '计入预算' : 'In Budget'"
                  @update:model-value="handleQuickPatch(cred, { include_external_in_budget: $event })"
                />
              </div>
            </div>
          </article>

          <div v-if="nextCursor" class="flex justify-center pt-2">
            <Button variant="secondary" size="sm" :loading="loadingMore" @click="loadSurface(false)">
              {{ isZh ? '加载更多密钥' : 'Load more credentials' }}
            </Button>
          </div>
        </div>

        <ByokServiceAccountsPanel
          v-if="activeTab === 'project' && activeProjectId && hasByokManagePermission"
          :workspace-id="activeWorkspaceId ?? ''"
          :project-id="activeProjectId"
          :project-keys="projectKeys"
        />
      </template>

      <ByokCredentialFormModal
        :open="formModalOpen"
        :mode="formMode"
        :scope-level="activeTab"
        :workspace-id="activeWorkspaceId ?? ''"
        :project-id="activeProjectId ?? ''"
        :actor-user-id="actorUserId"
        :provider-classes="providerClasses"
        :available-projects="availableProjects"
        :project-keys="projectKeys"
        :credential="editingCredential"
        :saving="formSaving"
        :error-notice="formErrorNotice"
        @update:open="formModalOpen = $event"
        @submit-create="handleCreateSubmit"
        @submit-policy="handlePolicySubmit"
        @reload-conflict="reloadEditingCredential"
      />

      <ByokInspectorDrawer
        :open="inspectorOpen"
        :scope-level="activeTab"
        :workspace-id="activeWorkspaceId ?? ''"
        :project-id="activeProjectId ?? ''"
        :actor-user-id="actorUserId"
        :credential="inspectedCredential"
        @update:open="inspectorOpen = $event"
        @credential-updated="upsertLocalCredential"
        @credential-removed="removeLocalCredential"
      />
    </div>
  </AppLayout>
</template>
