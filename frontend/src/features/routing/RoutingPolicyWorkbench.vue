<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Badge, Button, EmptyState, ErrorState, Input, Modal, PageHeader, Skeleton } from '@/components/ui'
import { isSilentRequestError } from '@/api/client'
import { normalizeApiError, type NormalizedApiError } from '@/utils/apiError'
import { routingAPI, type PolicyDraft, type PolicyScope, type RoutingWorkspace, type SimulationRequest } from './api'
import { listPublicModels } from '@/features/public/api'
import PolicyEditor from './PolicyEditor.vue'
import RoutingTracePanel from './RoutingTracePanel.vue'
import { canPublish, composeRestrictions, countRankingCandidates, isIdentity, listValues, policyForm, validScenario } from './policy'
import { useRoutingPolicy } from './useRoutingPolicy'
import './routing.css'

const props = defineProps<{ workspaceId?: string }>()
const { t, locale } = useI18n()
const isZh = computed(() => String(locale?.value ?? '').startsWith('zh'))

const workspaces = shallowRef<RoutingWorkspace[]>([])
const discoveryLoading = ref(true)
const discoveryIssue = shallowRef<NormalizedApiError | null>(null)
const workspace = ref(props.workspaceId ?? '')
const scopeType = ref<PolicyScope['type'] | 'platform'>('workspace')
const project = ref('')
const key = ref('')
const keys = shallowRef<Awaited<ReturnType<typeof routingAPI.keys>>['items']>([])
const keysCursor = ref<string | null>(null)
const keysLoading = ref(false)
const keysIssue = shallowRef<NormalizedApiError | null>(null)
const scope = computed<PolicyScope | null>(() => {
  if (!isIdentity(workspace.value) || scopeType.value === 'platform') return null
  if (scopeType.value === 'workspace') return { type: 'workspace', id: workspace.value }
  if (!isIdentity(project.value)) return null
  if (scopeType.value === 'project') return { type: 'project', id: project.value }
  if (!isIdentity(key.value)) return null
  return { type: 'api_key', id: key.value, project_id: project.value }
})
const scopeTypes = ['platform', 'workspace', 'project', 'api_key'] as const
const activeWorkspace = computed(() => workspaces.value.find(item => item.workspace_id === workspace.value))
const moreProjects = shallowRef<RoutingWorkspace['projects']>([])
const projectCursor = ref<string | null>(null)
const projectLoading = ref(false)
const projectIssue = shallowRef<NormalizedApiError | null>(null)
const projects = computed(() => [...(activeWorkspace.value?.projects ?? []), ...moreProjects.value])

const {
  head, form, baseline, dirty, opening, loading, synced, issue, blocked, refresh, save, mutation,
  mutationIssue, unconfirmed, verifyOutcome, conflict, resolveConflict, confirmation, validation,
  versions, versionsCursor, versionsLoading, versionsIssue, loadVersions,
  simulation, simulationLoading, simulationIssue, clearSimulation, simulate, publish, rollback,
} = useRoutingPolicy(workspace, scope)

const parentDefinitions = shallowRef<PolicyDraft[]>([])
const parentMissing = ref(false)
const parentBounds = computed(() => composeRestrictions(parentDefinitions.value))
const parentShowsRestrictions = computed(() => {
  const parent = parentBounds.value
  return parent.models !== null || parent.regions !== null || parent.rate !== null ||
    parent.retention || parent.training || parent.fundingDenied
})

const roots = new AbortController()
let tick: ReturnType<typeof setInterval> | undefined
const now = ref(Date.now())
const online = ref(typeof navigator === 'undefined' || navigator.onLine)
const scenarioProject = ref('')
const scenarioKey = ref('')
const model = ref('')
const protocol = ref('openai_chat')
const action = ref('text.chat')
const capabilities = ref('')
const inputTokenBound = ref('1024')
const outputTokenBound = ref('1024')
const seed = ref('1')
const snapshot = ref('')
const versionsFilter = ref('')
const changeNote = ref('')
const command = ref<{ kind: 'publish' | 'rollback'; version: string } | null>(null)
const scenarioInvalid = ref(false)
const availableModelSlugs = ref<string[]>([])

async function loadAvailableModels() {
  try {
    const page = await listPublicModels({ limit: 50, sort: 'name' })
    const slugs = (page.items ?? []).map(item => item.model).filter(Boolean)
    availableModelSlugs.value = slugs
    if (!model.value.trim() && slugs.length > 0) {
      model.value = slugs[0]
    }
  } catch {
    // Optional helper list; simulator continues to accept manual input.
  }
}

function beginNewDraft() {
  opening.value = true
  if (!form.value.name.trim()) {
    form.value.name = t('routing.defaultPolicyName')
  }
}
const scenarioKeyAnchor = computed(() => scope.value?.type === 'api_key' ? scope.value.id : scenarioKey.value)
const scenarioProjectAnchor = computed(() => scope.value?.type === 'project' || scope.value?.type === 'api_key' ? project.value : scenarioProject.value)
const expired = computed(() => Boolean(simulation.value && Date.parse(simulation.value.expires_at) <= now.value))
const canMutate = computed(() => !blocked.value && online.value)
const publishReady = computed(() => canMutate.value && canPublish(head.value, simulation.value, dirty.value, now.value))
const isStaleSimulationIssue = computed(() => {
  const currentIssue = simulationIssue.value
  if (!currentIssue) return false
  const reason = (currentIssue.reason ?? '').toUpperCase()
  return (
    currentIssue.status === 410 ||
    reason === 'SNAPSHOT_EXPIRED' ||
    reason === 'SIMULATION_STALE' ||
    (currentIssue.status === 409 && reason.includes('SIMULATION'))
  )
})

const workflowSteps = computed(() => {
  const hasSavedDraft = Boolean(head.value) && !dirty.value
  const hasValidSimulation = Boolean(simulation.value) && !expired.value && !isStaleSimulationIssue.value
  const hasActiveVersion = Boolean(head.value?.active_version)
  const hasRollbackTargets = versions.value.some((entry) => entry.id !== head.value?.active_version)

  return [
    {
      id: 'draft',
      index: '01',
      href: '#routing-editor',
      title: isZh.value ? '保存草稿' : 'Save Draft',
      detail: !head.value
        ? (isZh.value ? '待创建策略草稿' : 'Create initial draft')
        : dirty.value
          ? (isZh.value ? '有未保存修改' : 'Unsaved changes')
          : (isZh.value ? '草稿已同步' : 'Draft saved'),
      state: hasSavedDraft ? 'done' : 'current'
    },
    {
      id: 'simulate',
      index: '02',
      href: '#routing-simulator',
      title: isZh.value ? '运行候选集模拟' : 'Run Simulation',
      detail: isStaleSimulationIssue.value || expired.value
        ? (isZh.value ? '快照已过期，需重跑' : 'Snapshot expired, re-run')
        : hasValidSimulation
          ? (isZh.value ? '凭证有效，待发布' : 'Proof valid')
          : (isZh.value ? '验证候选路由轨迹' : 'Verify candidate trace'),
      state: hasValidSimulation ? 'done' : hasSavedDraft ? 'current' : 'pending'
    },
    {
      id: 'publish',
      index: '03',
      href: '#routing-simulator',
      title: isZh.value ? '验证通过后发布' : 'Publish Verified',
      detail: publishReady.value
        ? (isZh.value ? '可立即发布生效' : 'Ready to publish')
        : hasActiveVersion
          ? (isZh.value ? '已存在生效版本' : 'Active version live')
          : (isZh.value ? '完成模拟后解锁' : 'Unlocks after simulation'),
      state: hasActiveVersion && !head.value?.draft_version ? 'done' : publishReady.value ? 'current' : 'pending'
    },
    {
      id: 'rollback',
      index: '04',
      href: '#routing-versions',
      title: isZh.value ? '一键回滚' : 'One-Click Rollback',
      detail: hasRollbackTargets
        ? (isZh.value ? `${versions.value.length} 个历史版本可回滚` : `${versions.value.length} versions available`)
        : (isZh.value ? '发布后保留历史快照' : 'History kept after publish'),
      state: hasRollbackTargets ? 'done' : 'pending'
    }
  ] as const
})
const candidateCount = computed(() =>
  scope.value && !expired.value ? countRankingCandidates(baseline.value, form.value, scope.value, simulation.value?.trace ?? null) : null,
)
const history = computed(() => {
  const query = versionsFilter.value.toLowerCase().trim()
  return versions.value.filter(version => !query || `${version.id} ${version.change_note}`.toLowerCase().includes(query))
})
const differences = computed(() => {
  if (!conflict.value?.loaded) return []
  const server = policyForm(conflict.value.current?.definition)
  return (Object.keys(server) as (keyof typeof server)[])
    .filter(field => JSON.stringify(server[field]) !== JSON.stringify(form.value[field]))
    .map(field => ({ field, server: JSON.stringify(server[field]), local: JSON.stringify(form.value[field]) }))
})

function onlineChanged() {
  online.value = navigator.onLine
  if (online.value) void refresh()
}

async function discover() {
  discoveryLoading.value = true
  discoveryIssue.value = null
  try {
    const found: RoutingWorkspace[] = []
    let cursor: string | undefined
    do {
      const page = await routingAPI.workspaces(roots.signal, cursor)
      found.push(...page.items)
      cursor = page.next_cursor ?? undefined
    } while (cursor && !roots.signal.aborted)
    if (roots.signal.aborted) return
    workspaces.value = found
    workspace.value = found.some(item => item.workspace_id === props.workspaceId)
      ? props.workspaceId ?? ''
      : props.workspaceId ? '' : found[0]?.workspace_id ?? ''
  } catch (error) {
    if (!roots.signal.aborted && !isSilentRequestError(error)) {
      discoveryIssue.value = normalizeApiError(error)
      if (discoveryIssue.value.status === 403) {
        workspace.value = ''
        workspaces.value = []
      }
    }
  } finally {
    if (!roots.signal.aborted) discoveryLoading.value = false
  }
}

async function loadProjects() {
  if (projectLoading.value || !projectCursor.value) return
  const currentWorkspace = workspace.value
  projectLoading.value = true
  projectIssue.value = null
  try {
    const page = await routingAPI.projects(currentWorkspace, roots.signal, projectCursor.value)
    if (workspace.value !== currentWorkspace || roots.signal.aborted) return
    moreProjects.value = [...moreProjects.value, ...page.items]
    projectCursor.value = page.next_cursor
  } catch (error) {
    if (workspace.value === currentWorkspace && !isSilentRequestError(error)) projectIssue.value = normalizeApiError(error)
  } finally {
    if (workspace.value === currentWorkspace) projectLoading.value = false
  }
}

async function browseKeys() {
  if (keysLoading.value || !project.value) return
  const w = workspace.value
  const p = project.value
  keysLoading.value = true
  keysIssue.value = null
  try {
    const page = await routingAPI.keys(w, p, roots.signal, keysCursor.value ?? undefined)
    if (workspace.value !== w || project.value !== p || roots.signal.aborted) return
    keys.value = keysCursor.value ? [...keys.value, ...page.items] : page.items
    keysCursor.value = page.next_cursor
  } catch (error) {
    if (workspace.value === w && project.value === p && !isSilentRequestError(error)) {
      keysIssue.value = normalizeApiError(error)
      if (keysIssue.value.status === 403) {
        keys.value = []
        keysCursor.value = null
      }
    }
  } finally {
    if (workspace.value === w && project.value === p) keysLoading.value = false
  }
}

function runSimulation() {
  const current = head.value
  if (!current || !canMutate.value || dirty.value) return
  const version = current.draft_version || current.active_version
  if (!version) return
  const request: SimulationRequest = {
    version,
    compare_version: current.active_version && current.active_version !== version ? current.active_version : null,
    project_id: scenarioProjectAnchor.value,
    api_key_id: scenarioKeyAnchor.value.trim() || null,
    model_id: model.value.trim(),
    protocol: protocol.value.trim(),
    action: action.value.trim(),
    required_capabilities: listValues(capabilities.value),
    input_token_bound: inputTokenBound.value.trim(),
    output_token_bound: outputTokenBound.value.trim(),
    snapshot_id: snapshot.value.trim() || null,
    seed: seed.value.trim(),
  }
  scenarioInvalid.value = !validScenario(request)
  if (!scenarioInvalid.value) void simulate(request)
}

function rerunFreshSimulation() {
  if (snapshot.value.trim()) {
    snapshot.value = ''
  }
  runSimulation()
}

function openCommand(kind: 'publish' | 'rollback', version: string) {
  changeNote.value = ''
  command.value = { kind, version }
}

function changeWorkspace(value: string) {
  if (workspace.value === value) return
  project.value = ''
  key.value = ''
  workspace.value = value
}

async function confirmCommand() {
  const pending = command.value
  const note = changeNote.value.trim()
  if (!pending || !note || !canMutate.value) return
  if (pending.kind === 'publish') await publish(note)
  else await rollback(pending.version, note)
  command.value = null
}

watch(activeWorkspace, selected => {
  moreProjects.value = []
  projectCursor.value = selected?.projects_next_cursor ?? null
  projectLoading.value = false
  projectIssue.value = null
  scenarioProject.value = selected?.projects[0]?.id ?? ''
  scenarioKey.value = ''
})
watch(() => props.workspaceId, value => {
  changeWorkspace(value ?? '')
  void discover()
})
watch([workspace, project], () => {
  key.value = ''
  keys.value = []
  keysCursor.value = null
  keysLoading.value = false
  keysIssue.value = null
})
watch(scenarioProject, () => { scenarioKey.value = '' })
watch([scenarioProjectAnchor, scenarioKeyAnchor, model, protocol, action, capabilities, inputTokenBound, outputTokenBound, seed, snapshot], () => {
  scenarioInvalid.value = false
  clearSimulation()
})

watch(() => [workspace.value, scope.value?.type, scope.value?.id, scope.value?.project_id].join('/'), async (_, __, cleanup) => {
  parentDefinitions.value = []
  parentMissing.value = false
  command.value = null
  versionsFilter.value = ''
  const current = scope.value
  if (!current || current.type === 'workspace') return
  const controller = new AbortController()
  cleanup(() => controller.abort())
  const w = workspace.value
  const parentScopes: PolicyScope[] = [{ type: 'workspace', id: w }]
  if (current.type === 'api_key' && current.project_id) parentScopes.push({ type: 'project', id: current.project_id })
  const results = await Promise.allSettled(parentScopes.map(parent => routingAPI.list(w, parent, controller.signal)))
  if (controller.signal.aborted || issue.value?.status === 403) return
  for (const result of results) {
    if (result.status === 'rejected') {
      parentMissing.value = true
      continue
    }
    const next = result.value.items[0]
    if (!next?.active_version) continue
    if (next.draft_version) parentMissing.value = true
    else parentDefinitions.value = [...parentDefinitions.value, next.definition]
  }
})

watch(() => issue.value?.status, status => {
  if (status === 403) {
    parentDefinitions.value = []
    parentMissing.value = false
  }
})

onMounted(() => {
  void discover()
  void loadAvailableModels()
  tick = setInterval(() => { now.value = Date.now() }, 1000)
  window.addEventListener('online', onlineChanged)
  window.addEventListener('offline', onlineChanged)
})
onUnmounted(() => {
  roots.abort()
  clearInterval(tick)
  window.removeEventListener('online', onlineChanged)
  window.removeEventListener('offline', onlineChanged)
})
</script>

<template>
  <div class="routing-workbench space-y-4">
    <PageHeader :title="t('routing.title')" :description="t('routing.description')">
      <template v-if="scope" #actions>
        <Button :loading="loading" :disabled="Boolean(mutation)" @click="refresh()">{{ t('routing.refresh') }}</Button>
      </template>
    </PageHeader>

    <div v-if="discoveryLoading" class="routing-panel space-y-3" :aria-label="t('routing.loadingScopes')" aria-busy="true">
      <Skeleton variant="line" width="26%" />
      <Skeleton variant="rect" height="36px" />
    </div>
    <ErrorState
      v-else-if="discoveryIssue"
      :title="t(discoveryIssue.status === 403 ? 'routing.permission' : 'routing.scopeError')"
      :variant="discoveryIssue.status === 403 ? 'permission' : 'error'"
      :message="discoveryIssue.status === 403 ? t('routing.readPermission') : discoveryIssue.message"
      :retry-label="t('routing.retry')"
      @retry="discover"
    />
    <EmptyState v-else-if="!workspaces.length" :title="t('routing.noWorkspaces')" :description="t('routing.noWorkspacesHint')" />
    <template v-else>
      <section class="routing-panel space-y-3" :aria-label="t('routing.scope')">
        <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <label class="routing-field">
            <span>{{ t('routing.scope') }}</span>
            <select v-model="scopeType" :disabled="Boolean(mutation) || Boolean(unconfirmed)" class="routing-control">
              <option v-for="kind in scopeTypes" :key="kind" :value="kind">{{ t('routing.scopeType.' + kind) }}</option>
            </select>
          </label>
          <label v-if="scopeType !== 'platform'" class="routing-field">
            <span>{{ t('routing.scopeType.workspace') }}</span>
            <select
              :value="workspace" :disabled="Boolean(props.workspaceId) || Boolean(mutation) || Boolean(unconfirmed)"
              class="routing-control" @change="changeWorkspace(($event.target as HTMLSelectElement).value)"
            >
              <option value="" disabled>{{ t('routing.selectWorkspace') }}</option>
              <option v-for="entry in workspaces" :key="entry.workspace_id" :value="entry.workspace_id">{{ entry.name }}</option>
            </select>
          </label>
          <label v-if="scopeType === 'project' || scopeType === 'api_key'" class="routing-field">
            <span>{{ t('routing.scopeType.project') }}</span>
            <select v-model="project" :disabled="Boolean(mutation) || Boolean(unconfirmed)" class="routing-control">
              <option value="" disabled>{{ t('routing.selectProject') }}</option>
              <option v-for="entry in projects" :key="entry.id" :value="entry.id">{{ entry.name }}</option>
            </select>
          </label>
          <Input
            v-if="scopeType === 'api_key'"
            v-model="key" :label="t('routing.keyId')" :hint="t('routing.keyIdHint')"
            :maxlength="19" mono :disabled="Boolean(mutation) || Boolean(unconfirmed)"
          />
        </div>
        <div class="flex flex-wrap items-center justify-between gap-2">
          <p v-if="scopeType !== 'platform'" class="routing-hint">{{ t('routing.inheritance') }}</p>
          <Button v-if="scopeType !== 'platform' && projectCursor" :loading="projectLoading" size="sm" @click="loadProjects">{{ t('routing.moreProjects') }}</Button>
        </div>
        <div v-if="scopeType === 'api_key' && project" class="space-y-2">
          <label v-if="keys.length" class="routing-field sm:max-w-xs">
            <span>{{ t('routing.selectKey') }}</span>
            <select v-model="key" class="routing-control" :disabled="Boolean(mutation) || Boolean(unconfirmed)">
              <option value="" disabled>{{ t('routing.selectKey') }}</option>
              <option v-for="entry in keys" :key="entry.id" :value="entry.id">{{ entry.name }} · {{ entry.prefix }}</option>
            </select>
          </label>
          <Button v-if="!keys.length || keysCursor" size="sm" :loading="keysLoading" :disabled="Boolean(mutation) || Boolean(unconfirmed)" @click="browseKeys">
            {{ t('routing.browseKeys') }}
          </Button>
          <p v-if="keysIssue" class="routing-hint" role="status">{{ t('routing.keysUnavailable') }}</p>
        </div>
        <ErrorState v-if="projectIssue" :title="t('routing.projectsError')" :retry-label="t('routing.retry')" compact @retry="loadProjects" />
      </section>

      <EmptyState v-if="scopeType === 'platform'" :title="t('routing.platformTitle')" :description="t('routing.platformHint')" />
      <EmptyState v-else-if="!scope" :title="t('routing.selectScope')" :description="t('routing.selectScopeHint')" />
      <div v-else-if="loading && !synced" class="grid gap-4 lg:grid-cols-2" :aria-label="t('routing.loadingPolicy')" aria-busy="true">
        <section v-for="index in 2" :key="index" class="routing-panel space-y-3">
          <Skeleton variant="line" width="42%" />
          <Skeleton variant="rect" height="36px" />
          <Skeleton variant="rect" height="96px" />
        </section>
      </div>
      <template v-else>
        <ErrorState
          v-if="issue || !online"
          :variant="issue?.status === 403 ? 'permission' : synced ? 'stale' : 'error'"
          :code="issue?.status || undefined"
          :title="t(issue?.status === 403 ? 'routing.permission' : synced ? 'routing.stale' : 'routing.loadError')"
          :message="issue?.status === 403 ? t('routing.readPermission') : issue?.message"
          :last-synced-at="synced || undefined"
          :retry-label="t('routing.retry')"
          @retry="refresh()"
        />

        <template v-if="(!issue || synced) && issue?.status !== 403">
          <div v-if="loading" class="flex items-center gap-2 border-t-2 border-oh-accent pt-2 text-xs text-oh-secondary" role="status">{{ t('routing.refreshing') }}</div>

          <section v-if="unconfirmed" class="routing-panel space-y-2" role="status">
            <h3 class="routing-heading">{{ t('routing.unknownOutcome') }}</h3>
            <p class="routing-hint">{{ t('routing.unknownOutcomeHint') }}</p>
            <Button size="sm" :loading="loading" @click="verifyOutcome">{{ t('routing.verifyOutcome') }}</Button>
          </section>
          <ErrorState
            v-if="mutationIssue" :title="t(mutationIssue.status === 403 ? 'routing.permission' : 'routing.writeError')"
            :message="mutationIssue.status === 403 ? t('routing.writePermission') : mutationIssue.message"
            :variant="mutationIssue.status === 403 ? 'permission' : 'error'" :retryable="false"
          />
          <section v-if="conflict" class="routing-panel space-y-3" role="alert">
            <h3 class="routing-heading">{{ t(conflict.status ? 'routing.conflict' : 'routing.latestVersion') }} <span v-if="conflict.status" class="ml-1 font-mono oh-tabular">{{ conflict.status }}</span></h3>
            <p class="routing-hint">{{ t(conflict.status ? 'routing.conflictHint' : 'routing.verificationHint') }}</p>
            <div v-if="conflict.loaded" class="space-y-3">
              <p class="routing-hint">{{ t('routing.serverRevision') }} <span class="font-mono oh-tabular">{{ conflict.current?.etag ?? '—' }}</span></p>
              <details v-if="differences.length">
                <summary class="oh-focus-ring cursor-pointer text-sm font-medium">{{ t('routing.draftDiff') }}</summary>
                <dl v-for="diff in differences" :key="diff.field" class="mt-2 grid min-w-0 gap-2 border-t border-oh-subtle pt-2 text-xs sm:grid-cols-2">
                  <div class="break-all"><dt>{{ t('routing.serverValue', { field: diff.field }) }}</dt><dd class="font-mono">{{ diff.server }}</dd></div>
                  <div class="break-all"><dt>{{ t('routing.localValue', { field: diff.field }) }}</dt><dd class="font-mono">{{ diff.local }}</dd></div>
                </dl>
              </details>
              <div class="flex flex-wrap gap-2">
                <Button size="sm" @click="resolveConflict(false)">{{ t('routing.reloadLatest') }}</Button>
                <Button v-if="dirty && conflict.status" size="sm" variant="outline-accent" @click="resolveConflict(true)">{{ t('routing.reapply') }}</Button>
              </div>
            </div>
            <Button v-else size="sm" :loading="loading" @click="refresh()">{{ t('routing.retry') }}</Button>
          </section>

          <section v-if="confirmation" class="routing-panel space-y-2" role="status">
            <Badge tone="success">{{ t(confirmation.publication ? 'routing.published' : 'routing.saved') }}</Badge>
            <p class="break-all font-mono text-xs oh-tabular">{{ confirmation.version }}</p>
            <div v-if="confirmation.publication" class="flex flex-wrap gap-x-4 gap-y-1 text-sm">
              <span>{{ t('routing.impactProjects') }} <b class="font-mono oh-tabular">{{ confirmation.publication.impact.projects ?? '—' }}</b></span>
              <span>{{ t('routing.impactKeys') }} <b class="font-mono oh-tabular">{{ confirmation.publication.impact.keys ?? '—' }}</b></span>
              <a href="#routing-versions" class="oh-focus-ring text-oh-accent-text underline">{{ t('routing.versions') }}</a>
            </div>
          </section>

          <EmptyState v-if="!opening" :title="t('routing.empty')" :description="t('routing.emptyHint')">
            <template #actions><Button variant="primary" :disabled="!canMutate" @click="beginNewDraft">{{ t('routing.create') }}</Button></template>
          </EmptyState>
          <template v-else>
            <section
              class="routing-panel space-y-2.5"
              :aria-label="isZh ? '策略模拟与发布步骤引导' : 'Policy simulation and publish workflow'"
            >
              <div class="flex flex-wrap items-center justify-between gap-2">
                <p class="font-mono text-[11px] font-semibold uppercase tracking-wider text-oh-muted">
                  {{ isZh ? '变更安全闭环 · WORKFLOW' : 'SAFE RELEASE WORKFLOW' }}
                </p>
                <span class="text-[11px] text-oh-muted">
                  {{ isZh ? '保存草稿 → 运行候选集模拟 → 验证通过后发布 → 一键回滚' : 'Save Draft → Run Simulation → Publish Verified → Rollback' }}
                </span>
              </div>
              <ol class="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
                <li v-for="step in workflowSteps" :key="step.id" class="min-w-0">
                  <a
                    :href="step.href"
                    class="oh-focus-ring flex h-full items-start gap-2.5 rounded-oh-sm border px-3 py-2.5 transition-colors"
                    :class="
                      step.state === 'done'
                        ? 'border-emerald-500/30 bg-emerald-500/5 hover:border-emerald-500/60'
                        : step.state === 'current'
                          ? 'border-oh-accent/60 bg-oh-accent-subtle/40 hover:border-oh-accent'
                          : 'border-oh-subtle bg-oh-subtle/30 hover:border-oh-default'
                    "
                  >
                    <span
                      class="mt-0.5 inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full font-mono text-[10px] font-semibold"
                      :class="
                        step.state === 'done'
                          ? 'bg-emerald-500/20 text-emerald-700 dark:text-emerald-300'
                          : step.state === 'current'
                            ? 'bg-[var(--oh-accent,#f59e0b)] text-slate-950'
                            : 'bg-oh-subtle text-oh-muted'
                      "
                    >
                      {{ step.state === 'done' ? '✓' : step.index }}
                    </span>
                    <span class="min-w-0">
                      <span class="block text-xs font-semibold text-oh-primary">{{ step.title }}</span>
                      <span class="mt-0.5 block text-[11px] text-oh-secondary">{{ step.detail }}</span>
                    </span>
                  </a>
                </li>
              </ol>
            </section>

            <section v-if="head" class="routing-panel grid gap-3 sm:grid-cols-3">
              <div v-for="item in [
                { title: t('routing.activeVersion'), value: head.active_version },
                { title: t('routing.draftVersion'), value: head.draft_version },
                { title: t('routing.revision'), value: head.etag }
              ]" :key="item.title">
                <p class="routing-hint">{{ item.title }}</p>
                <p class="mt-1 break-all font-mono text-xs oh-tabular" :aria-label="!item.value ? t('routing.unset') : undefined">{{ item.value || '—' }}</p>
              </div>
            </section>

            <nav class="flex flex-wrap gap-x-3 gap-y-2 text-xs text-oh-accent-text xl:hidden" :aria-label="t('routing.title')">
              <a href="#routing-editor" class="oh-focus-ring underline">{{ t('routing.definition') }}</a>
              <a href="#routing-simulator" class="oh-focus-ring underline">{{ t('routing.simulator') }}</a>
              <a v-if="head" href="#routing-versions" class="oh-focus-ring underline">{{ t('routing.versions') }}</a>
            </nav>
            <div class="grid min-w-0 items-start gap-4 xl:grid-cols-2">
              <div id="routing-editor" class="min-w-0 scroll-mt-20 space-y-4">
                <section v-if="parentShowsRestrictions || parentMissing" class="routing-panel space-y-2">
                  <h3 class="routing-heading">{{ t('routing.parentConstraints') }}</h3>
                  <p v-if="parentMissing" class="routing-hint">{{ t('routing.parentUnavailable') }}</p>
                  <div class="flex flex-wrap gap-2">
                    <Badge v-if="parentBounds.models !== null" tone="neutral">{{ t('routing.models') }} · {{ parentBounds.models.join(', ') || t('routing.mode.deny_all') }}</Badge>
                    <Badge v-if="parentBounds.regions !== null" tone="neutral">{{ t('routing.regions') }} · {{ parentBounds.regions.join(', ') || t('routing.mode.deny_all') }}</Badge>
                    <Badge v-if="parentBounds.retention" tone="neutral">{{ t('routing.strictZero') }}</Badge>
                    <Badge v-if="parentBounds.training" tone="neutral">{{ t('routing.prohibited') }}</Badge>
                    <Badge v-if="parentBounds.rate !== null" tone="neutral" mono>USD {{ parentBounds.rate }} / 1M</Badge>
                    <Badge v-if="parentBounds.fundingDenied" tone="neutral">{{ t('routing.fundingDenied') }}</Badge>
                  </div>
                </section>
                <PolicyEditor
                  v-model="form" :candidate-count="candidateCount" :disabled="Boolean(mutation) || Boolean(unconfirmed)"
                  :saving="mutation === 'save'" :save-disabled="!canMutate || (Boolean(head?.draft_version) && !dirty)"
                  :validation="validation" @save="save"
                />
              </div>

              <div class="min-w-0 space-y-4">
                <section id="routing-simulator" class="routing-panel scroll-mt-20 space-y-4">
                  <div>
                    <h3 class="routing-heading">{{ t('routing.simulator') }}</h3>
                    <p class="mt-1 routing-hint">{{ t('routing.simulatorHint') }}</p>
                  </div>
                  <form class="space-y-3" @submit.prevent="runSimulation">
                    <div class="grid gap-3 sm:grid-cols-2">
                      <label v-if="scope.type === 'workspace'" class="routing-field">
                        <span>{{ t('routing.scopeType.project') }}</span>
                        <select v-model="scenarioProject" class="routing-control" required>
                          <option value="" disabled>{{ t('routing.selectProject') }}</option>
                          <option v-for="entry in projects" :key="entry.id" :value="entry.id">{{ entry.name }}</option>
                        </select>
                      </label>
                      <Input v-if="scope.type !== 'api_key'" v-model="scenarioKey" :label="t('routing.optionalKey')" :maxlength="19" mono />
                      <div class="space-y-1">
                        <Input v-model="model" list="routing-simulator-models" :label="t('routing.modelId')" :placeholder="t('routing.modelPlaceholder')" mono required />
                        <datalist id="routing-simulator-models">
                          <option v-for="slug in availableModelSlugs" :key="slug" :value="slug" />
                        </datalist>
                      </div>
                      <label class="routing-field">
                        <span>{{ t('routing.protocol') }}</span>
                        <select v-model="protocol" class="routing-control">
                          <option v-for="value in ['openai_chat','openai_responses','anthropic_messages','gemini_generate','gemini_count_tokens','gemini_embedding','openai_embeddings','openai_images','openai_audio','openai_video']" :key="value" :value="value">{{ value }}</option>
                        </select>
                      </label>
                      <Input v-model="action" :label="t('routing.action')" mono required />
                      <Input v-model="seed" :label="t('routing.seed')" :hint="t('routing.seedHint')" :maxlength="20" mono required />
                      <Input v-model="inputTokenBound" :label="t('routing.inputBound')" :maxlength="19" mono required />
                      <Input v-model="outputTokenBound" :label="t('routing.outputBound')" :maxlength="19" mono required />
                    </div>
                    <details class="border-y border-oh-subtle py-2">
                      <summary class="oh-focus-ring cursor-pointer text-xs font-medium text-oh-secondary">{{ t('routing.evidenceOptions') }}</summary>
                      <div class="mt-3 grid gap-3 sm:grid-cols-2">
                        <Input v-model="capabilities" :label="t('routing.capabilities')" :hint="t('routing.commaList')" mono />
                        <Input v-model="snapshot" :label="t('routing.snapshotId')" mono />
                      </div>
                    </details>
                    <p v-if="simulationLoading" role="status" class="routing-hint">{{ t('routing.simulating') }}</p>
                    <p v-if="scenarioInvalid" role="alert" class="text-xs text-oh-status-danger-fg">{{ t('routing.invalidScenario') }}</p>
                    <p v-if="!head || dirty" class="routing-hint">{{ t('routing.saveBeforeSimulate') }}</p>
                    <div class="flex flex-wrap justify-between gap-2">
                      <Button type="submit" variant="outline-accent" :loading="simulationLoading" :disabled="!head || dirty || !canMutate">{{ t('routing.runSimulation') }}</Button>
                      <Button :disabled="!publishReady" @click="openCommand('publish', head?.draft_version ?? '')">{{ t('routing.publish') }}</Button>
                    </div>
                  </form>
                </section>
                <section
                  v-if="isStaleSimulationIssue || expired"
                  role="status"
                  class="routing-panel space-y-2.5 border-amber-500/40 bg-amber-500/5"
                >
                  <div class="flex flex-wrap items-center justify-between gap-2">
                    <div class="flex items-center gap-2">
                      <Badge tone="warning" size="xs" variant="subtle" mono>
                        {{
                          simulationIssue?.reason ||
                          (simulationIssue?.status === 410 ? 'SNAPSHOT_EXPIRED' : 'SIMULATION_STALE')
                        }}
                      </Badge>
                      <h4 class="text-xs font-semibold text-oh-primary">
                        {{
                          isZh
                            ? '实时模拟快照或发布凭证已过期'
                            : 'Live simulation snapshot or publish proof has expired'
                        }}
                      </h4>
                    </div>
                  </div>
                  <p class="text-xs leading-relaxed text-oh-secondary">
                    {{
                      isZh
                        ? '网关候选集快照或策略草稿状态已更新，当前发布凭证已自动失效。无需重写策略配置，点击下方按钮即可基于最新实时状态重新运行模拟并刷新发布凭证。'
                        : 'The gateway snapshot or draft state changed and the previous publish proof expired. Your draft edits are intact—re-run a live simulation below to refresh your publish proof.'
                    }}
                  </p>
                  <div class="flex flex-wrap items-center gap-2">
                    <Button
                      size="sm"
                      variant="primary"
                      :loading="simulationLoading"
                      :disabled="!head || dirty || !canMutate"
                      @click="rerunFreshSimulation"
                    >
                      {{
                        isZh
                          ? '重新运行实时模拟并刷新发布凭证'
                          : 'Re-run Live Simulation & Refresh Publish Proof'
                      }}
                    </Button>
                    <span v-if="dirty" class="text-[11px] text-oh-muted">
                      {{ isZh ? '检测到未保存草稿，请先点击左侧「保存」' : 'Save draft edits first before re-running simulation' }}
                    </span>
                  </div>
                </section>
                <ErrorState
                  v-else-if="simulationIssue"
                  :title="t(simulationIssue.status === 403 ? 'routing.permission' : 'routing.simulationError')"
                  :variant="simulationIssue.status === 403 ? 'permission' : 'error'"
                  :message="simulationIssue.status === 403 ? t('routing.simulatePermission') : simulationIssue.message"
                  :retryable="false"
                />
                <RoutingTracePanel v-if="simulation" :result="simulation" :expired="expired" />
              </div>
            </div>

            <section v-if="head" id="routing-versions" class="routing-panel scroll-mt-20 space-y-3">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <h3 class="routing-heading">{{ t('routing.versions') }}</h3>
                <Input v-if="versions.length" v-model="versionsFilter" :label="t('routing.filterVersions')" type="search" clearable />
              </div>
              <Skeleton v-if="versionsLoading && !versions.length" variant="line" width="72%" />
              <ErrorState
                v-if="versionsIssue" :title="t(versionsIssue.status === 403 ? 'routing.permission' : 'routing.historyError')"
                :message="versionsIssue.status === 403 ? t('routing.readPermission') : versionsIssue.message"
                :variant="versionsIssue.status === 403 ? 'permission' : 'error'" :retry-label="t('routing.retry')"
                compact @retry="loadVersions()"
              />
              <p v-else-if="!versionsLoading && !versions.length" class="routing-hint">{{ t('routing.noVersions') }}</p>
              <EmptyState v-else-if="versions.length && !history.length" variant="filtered" :title="t('routing.noMatches')">
                <template #actions><Button size="sm" @click="versionsFilter = ''">{{ t('routing.clearFilter') }}</Button></template>
              </EmptyState>
              <ul v-if="history.length" class="divide-y divide-oh-subtle">
                <li v-for="version in history" :key="version.id" class="flex flex-wrap items-start justify-between gap-3 py-3">
                  <div class="min-w-0 space-y-1">
                    <p class="break-all font-mono text-xs oh-tabular">{{ version.id }}</p>
                    <p class="break-words text-sm text-oh-secondary">{{ version.change_note }}</p>
                    <p class="routing-hint font-mono oh-tabular">{{ version.published_at }}</p>
                  </div>
                  <Badge v-if="version.id === head.active_version" tone="accent">{{ t('routing.active') }}</Badge>
                  <Button v-else size="sm" :disabled="!canMutate || dirty || !head.active_version" @click="openCommand('rollback', version.id)">{{ t('routing.rollback') }}</Button>
                </li>
              </ul>
              <Button v-if="versionsCursor" size="sm" :loading="versionsLoading" @click="loadVersions(true)">{{ t('routing.moreVersions') }}</Button>
            </section>
          </template>
        </template>
      </template>
    </template>

    <Modal
      :open="Boolean(command)" :title="t(command?.kind === 'rollback' ? 'routing.confirmRollback' : 'routing.confirmPublish')"
      :description="t(command?.kind === 'rollback' ? 'routing.rollbackHint' : 'routing.publishHint')"
      :close-on-escape="!mutation" :close-on-backdrop="!mutation" :hide-close-button="Boolean(mutation)"
      @update:open="open => { if (!open) command = null }"
    >
      <div class="space-y-3">
        <p class="break-all font-mono text-xs oh-tabular">{{ command?.version }}</p>
        <div v-if="command?.kind === 'publish' && simulation" class="flex flex-wrap gap-3 text-sm">
          <span>{{ t('routing.impactProjects') }} <b class="font-mono oh-tabular">{{ simulation.impact.projects ?? '—' }}</b></span>
          <span>{{ t('routing.impactKeys') }} <b class="font-mono oh-tabular">{{ simulation.impact.keys ?? '—' }}</b></span>
        </div>
        <Input v-model="changeNote" :label="t('routing.changeNote')" :maxlength="500" :disabled="Boolean(mutation)" required />
      </div>
      <template #footer>
        <Button :disabled="Boolean(mutation)" @click="command = null">{{ t('routing.cancel') }}</Button>
        <Button
          variant="primary" :loading="Boolean(mutation)"
          :disabled="!changeNote.trim() || !canMutate || (command?.kind === 'publish' && !publishReady)"
          @click="confirmCommand"
        >{{ t(command?.kind === 'rollback' ? 'routing.rollback' : 'routing.publish') }}</Button>
      </template>
    </Modal>
  </div>
</template>
