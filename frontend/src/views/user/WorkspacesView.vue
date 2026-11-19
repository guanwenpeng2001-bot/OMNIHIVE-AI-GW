<template>
  <AppLayout>
    <div data-testid="workspaces-view" class="min-w-0 space-y-5">
      <header class="card p-5 sm:p-6">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <div class="flex flex-wrap items-center gap-2.5">
              <h1 class="text-2xl font-bold tracking-tight text-[var(--foreground)]">
                {{ t('workspaces.title') }}
              </h1>
              <span class="chip text-xs">
                <span class="inline-block h-1.5 w-1.5 rounded-full bg-[var(--oh-accent,#f59e0b)]" aria-hidden="true" />
                <span class="font-mono tabular-nums">{{ workspaces.length }}</span>
              </span>
              <span v-if="selected" class="pill pill-ghost font-mono text-[11px] uppercase">
                {{ selected.role }}
              </span>
              <span
                v-if="selected && String(selected.id) === activeScope.workspaceId"
                data-testid="active-workspace-badge"
                class="rounded border border-[var(--oh-accent-border,rgba(245,158,11,0.4))] bg-[var(--oh-accent-subtle,rgba(245,158,11,0.12))] px-2 py-0.5 font-mono text-[11px] font-semibold text-[var(--oh-ink,var(--foreground))]"
              >
                {{ t('workspaces.activeScopeBadge') }}
                <template v-if="activeScope.projectId"> · #{{ activeScope.projectId }}</template>
              </span>
            </div>
            <p class="mt-1.5 text-sm text-[var(--muted-foreground)]">
              {{ t('workspaces.description') }}
            </p>
          </div>
          <div v-if="selected" class="flex flex-wrap items-center gap-2">
            <button
              type="button"
              data-testid="activate-workspace-scope-btn"
              class="btn btn-secondary btn-sm font-mono text-xs"
              @click="activateScope(String(selected.id), projects[0]?.id ?? null)"
            >
              {{ t('workspaces.activateScope') }}
            </button>
          </div>
        </div>
      </header>

      <ConsoleAsyncBanner
        v-if="scopeConfirmedNotice"
        :notice="scopeConfirmedNotice"
        dismissible
        @dismiss="scopeConfirmedNotice = null"
      />

      <form v-if="auth.isAdmin" class="card flex flex-wrap items-end gap-3 p-5" @submit.prevent="createWorkspace">
        <label class="flex-1 space-y-2 text-sm font-medium text-[var(--foreground)]">
          <span class="flex items-center gap-2">
            <span class="inline-block h-1.5 w-1.5 rounded-full bg-[var(--oh-accent,#f59e0b)]" aria-hidden="true" />
            <span>{{ t('workspaces.name') }}</span>
          </span>
          <input v-model="workspaceName" required maxlength="100" class="input w-full" />
        </label>
        <button class="btn btn-primary" :disabled="busy || !workspaceName.trim()">{{ t('workspaces.create') }}</button>
      </form>

      <div class="card p-5">
        <label class="block space-y-2 text-sm font-medium text-[var(--foreground)]">
          <span class="flex items-center justify-between gap-2">
            <span class="flex items-center gap-2">
              <span class="inline-block h-1.5 w-1.5 rounded-full bg-[var(--oh-accent,#f59e0b)]" aria-hidden="true" />
              <span>{{ t('workspaces.select') }}</span>
            </span>
            <span v-if="selected" class="pill pill-ghost font-mono text-xs tabular-nums">
              #{{ selected.id }}
            </span>
          </span>
          <select v-model="selectedID" class="input w-full font-mono tabular-nums" :disabled="loading || busy" @change="onWorkspaceSelectionChange">
            <option :value="null" disabled>{{ t('workspaces.select') }}</option>
            <option v-for="workspace in workspaces" :key="workspace.id" :value="workspace.id">{{ workspace.name }}</option>
          </select>
        </label>
      </div>

      <p v-if="!loading && workspaces.length === 0" class="card p-8 text-center text-sm text-[var(--muted-foreground)]">
        {{ t('workspaces.empty') }}
      </p>

      <section v-if="selected" class="card space-y-4 p-5">
        <div class="flex flex-wrap items-center justify-between gap-2 border-b border-[var(--border)] pb-3">
          <div class="flex items-center gap-2">
            <span class="inline-block h-1.5 w-1.5 rounded-full bg-[var(--oh-accent,#f59e0b)]" aria-hidden="true" />
            <h2 class="text-lg font-semibold text-[var(--foreground)]">
              {{ selected.name }} · {{ t('workspaces.resources') }}
            </h2>
          </div>
          <span class="chip font-mono text-xs tabular-nums">
            {{ selected.role === 'admin' ? resources.length + 3 : 3 }}
          </span>
        </div>
        <div class="flex flex-wrap gap-2.5">
          <RouterLink :to="`/workspaces/${selected.id}/keys`" class="btn btn-secondary">{{ t('nav.apiKeys') }}</RouterLink>
          <RouterLink to="/playground" class="btn btn-secondary">Playground</RouterLink>
          <RouterLink to="/tasks" class="btn btn-secondary">Task Center</RouterLink>
          <template v-if="selected.role === 'admin'">
            <RouterLink v-for="resource in resources" :key="resource.path" :to="`/workspaces/${selected.id}/${resource.path}`" class="btn btn-secondary">{{ t(resource.label) }}</RouterLink>
          </template>
        </div>
      </section>

      <!-- BE-01 Projects & Scope Switcher Section (uses div/table so legacy form/li selectors stay untouched) -->
      <section v-if="selected" data-testid="workspace-projects-section" class="card space-y-4 p-5">
        <div class="flex flex-wrap items-start justify-between gap-3 border-b border-[var(--border)] pb-3">
          <div>
            <div class="flex items-center gap-2">
              <span class="inline-block h-1.5 w-1.5 rounded-full bg-[var(--oh-accent,#f59e0b)]" aria-hidden="true" />
              <h2 class="text-lg font-semibold text-[var(--foreground)]">{{ t('workspaces.projectsTitle') }}</h2>
              <span class="pill pill-ghost font-mono text-xs tabular-nums">{{ projects.length }}</span>
            </div>
            <p class="mt-1 text-xs text-[var(--muted-foreground)]">{{ t('workspaces.projectsDesc') }}</p>
          </div>
          <button
            type="button"
            data-testid="refresh-projects-btn"
            class="btn btn-secondary btn-sm"
            :disabled="projectsLoading"
            @click="loadProjects(selected.id, true)"
          >
            {{ t('common.refresh') }}
          </button>
        </div>

        <ConsoleAsyncBanner
          v-if="projectsNotice"
          :notice="projectsNotice"
          @retry="loadProjects(selected.id, true)"
        />

        <!-- Create Project Bar (admin only, div container instead of form) -->
        <div
          v-if="selected.role === 'admin'"
          class="flex flex-wrap items-center gap-2 rounded-[var(--oh-r-md,8px)] border border-[var(--border)] bg-[var(--surface-2)] p-3"
        >
          <input
            v-model="newProjectName"
            data-testid="new-project-name-input"
            type="text"
            maxlength="100"
            :placeholder="t('workspaces.newProjectPlaceholder')"
            class="input flex-1 text-sm"
            @keydown.enter.prevent="handleCreateProject"
          />
          <button
            type="button"
            data-testid="create-project-btn"
            class="btn btn-primary btn-sm"
            :disabled="projectActionBusy || !newProjectName.trim()"
            @click="handleCreateProject"
          >
            {{ t('workspaces.createProject') }}
          </button>
        </div>

        <div v-if="projects.length > 0" class="overflow-x-auto">
          <table class="w-full border-collapse text-left text-xs">
            <thead>
              <tr class="border-b border-[var(--border)] font-mono text-[11px] uppercase tracking-wider text-[var(--muted-foreground)]">
                <th class="py-2 pr-3">ID</th>
                <th class="px-3 py-2">Project</th>
                <th class="px-3 py-2">State</th>
                <th class="py-2 pl-3 text-right">Actions</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-[var(--border)]">
              <tr v-for="proj in projects" :key="proj.id" :data-testid="`project-row-${proj.id}`">
                <td class="py-2.5 pr-3 font-mono tabular-nums text-[var(--muted-foreground)]">#{{ proj.id }}</td>
                <td class="px-3 py-2.5">
                  <div v-if="renamingProjectId === proj.id" class="flex items-center gap-1.5">
                    <input
                      v-model="renameProjectDraft"
                      data-testid="rename-project-input"
                      type="text"
                      maxlength="100"
                      class="input h-7 px-2 text-xs"
                    />
                    <button
                      type="button"
                      data-testid="confirm-rename-project-btn"
                      class="btn btn-primary btn-sm px-2 py-1 text-xs"
                      :disabled="projectActionBusy || !renameProjectDraft.trim()"
                      @click="confirmRenameProject(proj)"
                    >
                      Save
                    </button>
                    <button
                      type="button"
                      class="btn btn-secondary btn-sm px-2 py-1 text-xs"
                      @click="renamingProjectId = null"
                    >
                      ×
                    </button>
                  </div>
                  <div v-else class="flex items-center gap-2">
                    <span class="font-medium text-[var(--foreground)]">{{ proj.name }}</span>
                    <span
                      v-if="
                        String(selected.id) === activeScope.workspaceId &&
                        proj.id === activeScope.projectId
                      "
                      class="rounded border border-[var(--oh-accent-border,rgba(245,158,11,0.4))] bg-[var(--oh-accent-subtle,rgba(245,158,11,0.12))] px-1.5 py-0.5 font-mono text-[10px] font-semibold text-[var(--foreground)]"
                    >
                      ACTIVE
                    </span>
                  </div>
                </td>
                <td class="px-3 py-2.5 font-mono text-[11px] uppercase text-[var(--muted-foreground)]">
                  {{ proj.state ?? 'active' }} · v{{ proj.version }}
                </td>
                <td class="py-2.5 pl-3 text-right">
                  <div class="inline-flex flex-wrap items-center justify-end gap-1.5">
                    <button
                      type="button"
                      :data-testid="`switch-project-${proj.id}`"
                      class="btn btn-secondary btn-sm px-2.5 py-1 font-mono text-xs"
                      @click="activateScope(String(selected.id), proj.id)"
                    >
                      {{ t('workspaces.activateScope') }}
                    </button>
                    <button
                      v-if="selected.role === 'admin' && (proj.state ?? 'active') === 'active'"
                      type="button"
                      :data-testid="`start-rename-project-${proj.id}`"
                      class="btn btn-secondary btn-sm px-2 py-1 text-xs"
                      @click="startRenameProject(proj)"
                    >
                      {{ t('workspaces.renameProject') }}
                    </button>
                    <button
                      v-if="selected.role === 'admin' && (proj.state ?? 'active') === 'active'"
                      type="button"
                      :data-testid="`archive-project-${proj.id}`"
                      class="btn btn-secondary btn-sm px-2 py-1 text-xs text-[var(--oh-danger,#dc2626)]"
                      :disabled="projectActionBusy"
                      @click="handleArchiveProject(proj)"
                    >
                      {{ t('workspaces.archiveProject') }}
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-else-if="!projectsLoading" class="text-xs text-[var(--muted-foreground)]">
          {{ t('workspaces.noProjects') }}
        </p>
      </section>

      <section v-if="selected?.role === 'admin'" class="card space-y-4 p-5" :aria-busy="loadingMembers">
        <div class="flex flex-wrap items-center justify-between gap-2 border-b border-[var(--border)] pb-3">
          <div class="flex items-center gap-2">
            <span class="inline-block h-1.5 w-1.5 rounded-full bg-[var(--oh-accent,#f59e0b)]" aria-hidden="true" />
            <h2 class="text-lg font-semibold text-[var(--foreground)]">{{ t('workspaces.members') }}</h2>
          </div>
          <span class="chip font-mono text-xs tabular-nums">{{ members.length }}</span>
        </div>
        <form class="flex flex-wrap items-end gap-3 rounded-[var(--oh-r-md,8px)] border border-[var(--border)] bg-[var(--surface-2)] p-4" @submit.prevent="saveMember">
          <label class="space-y-2 text-sm font-medium text-[var(--foreground)]"><span class="block">{{ t('workspaces.userID') }}</span><input v-model.number="userID" type="number" min="1" required class="input font-mono tabular-nums" /></label>
          <label class="space-y-2 text-sm font-medium text-[var(--foreground)]"><span class="block">{{ t('workspaces.role') }}</span><select v-model="memberRole" class="input"><option value="member">{{ t('workspaces.member') }}</option><option value="admin">{{ t('workspaces.admin') }}</option></select></label>
          <button class="btn btn-primary" :disabled="busy || !userID">{{ t('workspaces.save') }}</button>
        </form>
        <ul class="divide-y divide-[var(--border)]">
          <li v-for="member in members" :key="member.user_id" class="flex flex-wrap items-center justify-between gap-3 py-3">
            <span class="font-mono text-sm tabular-nums text-[var(--foreground)]">{{ t('workspaces.userID') }}: {{ member.user_id }} · {{ member.role === 'admin' ? t('workspaces.admin') : t('workspaces.member') }}</span>
            <button type="button" class="btn btn-secondary" :disabled="busy" @click="removeMember(member.user_id)">{{ t('workspaces.remove') }}</button>
          </li>
        </ul>
      </section>
      <section class="card space-y-5 p-5 sm:p-6" :aria-busy="policyLoading || policySaving" aria-labelledby="workspace-policy-title">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 id="workspace-policy-title" class="text-lg font-semibold text-[var(--foreground)]">OmniHive 工作空间路由与合规策略中心</h2>
            <p class="mt-2 text-sm text-[var(--muted-foreground)]">管理当前账户的路由偏好与合规配置；切换工作空间不会更改此账户级设置。</p>
          </div>
          <button type="button" class="btn btn-secondary" :disabled="policyLoading || policySaving" @click="loadRoutingPolicies">{{ policyLoading ? '正在读取…' : '重新读取' }}</button>
        </div>
        <p v-if="policyError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ policyError }}</p>
        <fieldset v-if="routingPolicy" :disabled="policyLoading || policySaving" class="space-y-5">
          <legend class="sr-only">路由与合规策略配置</legend>
          <div class="grid gap-4 sm:grid-cols-3">
            <label class="flex items-center gap-2 text-sm"><input v-model="routingPolicy.zdr_enabled" type="checkbox" class="h-4 w-4" />零数据留存 (ZDR)</label>
            <label class="flex items-center gap-2 text-sm"><input v-model="routingPolicy.allow_fallback" type="checkbox" class="h-4 w-4" />允许回退渠道</label>
            <label class="flex items-center gap-2 text-sm"><input v-model="routingPolicy.auto_prompt_cache" type="checkbox" class="h-4 w-4" />自动提示词缓存</label>
          </div>
          <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            <label class="space-y-2 text-sm"><span class="block">路由偏好</span><select v-model="routingPolicy.strategy_mode" class="input w-full"><option value="smart">智能均衡</option><option value="price">最低成本</option><option value="latency">最低延迟</option><option value="throughput">最高吞吐</option></select></label>
            <label class="space-y-2 text-sm"><span class="block">数据处理区域</span><select v-model="routingPolicy.region_preference" class="input w-full"><option value="global">全球自动</option><option value="us">美国 US</option><option value="eu">欧盟 EU</option><option value="ap">亚太 APAC</option></select></label>
            <label class="space-y-2 text-sm"><span class="block">月度预算告警阈值 (%)</span><input v-model.number="routingPolicy.alert_threshold_percent" type="number" min="1" max="100" step="1" required class="input w-full font-mono" /></label>
          </div>
          <button type="button" class="btn btn-primary" @click="saveRoutingPolicies">{{ policySaving ? '正在保存…' : '保存路由策略' }}</button>
        </fieldset>
        <p v-else-if="policyLoading" role="status" class="text-sm text-[var(--muted-foreground)]">正在读取路由策略…</p>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import { workspacesAPI, type Workspace, type WorkspaceMember } from '@/api/workspaces'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { normalizeApiError } from '@/utils/apiError'
import type { RoutingPolicyConfig } from '@/api/mcp'
import { classifyConsoleError, type ConsoleAsyncNotice } from '@/features/console/asyncState'
import ConsoleAsyncBanner from '@/features/console/components/ConsoleAsyncBanner.vue'
import {
  identityV2Api,
  persistConsoleScope,
  readPersistedConsoleScope,
  type ConsoleScopeSelection,
  type IdentityProject
} from '@/features/console/scope'

const { t } = useI18n()
const auth = useAuthStore()
const app = useAppStore()
const workspaces = ref<Workspace[]>([])
const members = ref<WorkspaceMember[]>([])
const selectedID = ref<number | null>(null)
const selected = computed(() => workspaces.value.find(workspace => workspace.id === selectedID.value))

const activeScope = ref<ConsoleScopeSelection>(readPersistedConsoleScope())
const scopeConfirmedNotice = ref<ConsoleAsyncNotice | null>(null)
const projects = ref<IdentityProject[]>([])
const projectsLoading = ref(false)
const projectActionBusy = ref(false)
const projectsNotice = ref<ConsoleAsyncNotice | null>(null)
const newProjectName = ref('')
const renamingProjectId = ref<string | null>(null)
const renameProjectDraft = ref('')
let projectRequestSeq = 0
const workspaceName = ref('')
const userID = ref<number | null>(null)
const memberRole = ref<WorkspaceMember['role']>('member')
const busy = ref(false)
const loading = ref(true)
const loadingMembers = ref(false)
let memberRequest = 0
const routingPolicy = ref<RoutingPolicyConfig | null>(null)
const policyLoading = ref(false)
const policySaving = ref(false)
const policyError = ref('')
let policyRequest = 0

async function loadRoutingPolicies() {
  if (policySaving.value) return
  const request = ++policyRequest
  policyLoading.value = true
  policyError.value = ''
  try {
    const { mcpAPI } = await import('@/api/mcp')
    if (request !== policyRequest) return
    const config = await mcpAPI.getRoutingPolicies()
    if (request === policyRequest) routingPolicy.value = config
  } catch (error) {
    if (request === policyRequest) policyError.value = normalizeApiError(error).message || '路由策略暂时无法读取，请重试。'
  } finally {
    if (request === policyRequest) policyLoading.value = false
  }
}

async function saveRoutingPolicies() {
  if (!routingPolicy.value || policySaving.value || policyLoading.value) return
  const config = routingPolicy.value
  if (!Number.isInteger(config.alert_threshold_percent) || config.alert_threshold_percent < 1 || config.alert_threshold_percent > 100) {
    policyError.value = '月度预算告警阈值须为 1 至 100 的整数。'
    return
  }
  const request = ++policyRequest
  policySaving.value = true
  policyError.value = ''
  try {
    const { mcpAPI } = await import('@/api/mcp')
    if (request !== policyRequest) return
    const saved = await mcpAPI.updateRoutingPolicies({
      zdr_enabled: config.zdr_enabled,
      allow_fallback: config.allow_fallback,
      auto_prompt_cache: config.auto_prompt_cache,
      strategy_mode: config.strategy_mode,
      region_preference: config.region_preference,
      alert_threshold_percent: config.alert_threshold_percent,
    })
    if (request !== policyRequest) return
    routingPolicy.value = saved
    app.showSuccess('路由与合规策略已保存。')
  } catch (error) {
    if (request !== policyRequest) return
    policyError.value = normalizeApiError(error).message || '保存失败，请稍后重试。'
    app.showError(policyError.value)
  } finally {
    if (request === policyRequest) policySaving.value = false
  }
}

watch(() => auth.user?.id, () => {
  policyRequest++
  routingPolicy.value = null
  policySaving.value = false
  void loadRoutingPolicies()
})

onBeforeUnmount(() => { policyRequest++ })

const resources = [
  { path: 'accounts', label: 'nav.accounts' },
  { path: 'groups', label: 'nav.groups' },
  { path: 'proxies', label: 'nav.proxies' },
  { path: 'channels/pricing', label: 'nav.channels' },
  { path: 'channels/monitor', label: 'nav.channelMonitor' },
  { path: 'monitor', label: 'nav.channelStatus' },
]

async function loadMembers() {
  const request = ++memberRequest
  members.value = []
  if (selected.value?.role !== 'admin') return
  loadingMembers.value = true
  try {
    const result = await workspacesAPI.members(selected.value.id)
    if (request === memberRequest) members.value = result
  } catch (error) {
    if (request === memberRequest) app.showError(normalizeApiError(error).message)
  } finally {
    if (request === memberRequest) loadingMembers.value = false
  }
}

async function loadProjects(workspaceID: number | null | undefined, explicit = false) {
  const seq = ++projectRequestSeq
  projectsNotice.value = null
  if (!workspaceID) {
    projects.value = []
    return
  }
  if (!explicit && !(auth as { user?: { id?: number } }).user?.id) {
    return
  }
  const hadCached = projects.value.length > 0
  projectsLoading.value = true
  try {
    const page = await identityV2Api.listProjects(String(workspaceID), { limit: 100 })
    if (seq === projectRequestSeq) {
      projects.value = page.items ?? []
    }
  } catch (err) {
    if (seq === projectRequestSeq) {
      projectsNotice.value = classifyConsoleError(err, {
        hasCachedData: hadCached,
        requiredGrantFallback: 'projects.read'
      })
    }
  } finally {
    if (seq === projectRequestSeq) {
      projectsLoading.value = false
    }
  }
}

function onWorkspaceSelectionChange() {
  projects.value = []
  renamingProjectId.value = null
  void loadMembers()
  void loadProjects(selectedID.value, false)
}

function activateScope(workspaceId: string, projectId: string | null) {
  const next: ConsoleScopeSelection = {
    mode: 'workspace',
    workspaceId,
    projectId
  }
  persistConsoleScope(next)
  activeScope.value = next
  scopeConfirmedNotice.value = {
    kind: 'confirmed',
    title: projectId
      ? `Active scope switched to Workspace #${workspaceId} / Project #${projectId}`
      : `Active scope switched to Workspace #${workspaceId}`
  }
}

async function handleCreateProject() {
  if (!selected.value || !newProjectName.value.trim() || projectActionBusy.value) return
  projectActionBusy.value = true
  projectsNotice.value = null
  try {
    const created = await identityV2Api.createProject(String(selected.value.id), {
      name: newProjectName.value.trim()
    })
    newProjectName.value = ''
    projects.value = [created, ...projects.value.filter((p) => p.id !== created.id)]
    scopeConfirmedNotice.value = {
      kind: 'confirmed',
      title: `Created project "${created.name}" (#${created.id})`
    }
  } catch (err) {
    projectsNotice.value = classifyConsoleError(err, {
      requiredGrantFallback: 'projects.write'
    })
  } finally {
    projectActionBusy.value = false
  }
}

function startRenameProject(proj: IdentityProject) {
  renamingProjectId.value = proj.id
  renameProjectDraft.value = proj.name
}

async function confirmRenameProject(proj: IdentityProject) {
  if (!selected.value || !renameProjectDraft.value.trim() || projectActionBusy.value) return
  projectActionBusy.value = true
  projectsNotice.value = null
  try {
    const updated = await identityV2Api.renameProject(
      String(selected.value.id),
      proj.id,
      { name: renameProjectDraft.value.trim() },
      proj.version
    )
    projects.value = projects.value.map((item) => (item.id === updated.id ? updated : item))
    renamingProjectId.value = null
    scopeConfirmedNotice.value = {
      kind: 'confirmed',
      title: `Renamed project #${updated.id} to "${updated.name}" (v${updated.version})`
    }
  } catch (err) {
    projectsNotice.value = classifyConsoleError(err, {
      requiredGrantFallback: 'projects.write'
    })
  } finally {
    projectActionBusy.value = false
  }
}

async function handleArchiveProject(proj: IdentityProject) {
  if (!selected.value || projectActionBusy.value) return
  projectActionBusy.value = true
  projectsNotice.value = null
  try {
    const archived = await identityV2Api.archiveProject(
      String(selected.value.id),
      proj.id,
      proj.version
    )
    projects.value = projects.value.map((item) => (item.id === archived.id ? archived : item))
    scopeConfirmedNotice.value = {
      kind: 'confirmed',
      title: `Archived project "${archived.name}" (#${archived.id})`
    }
  } catch (err) {
    projectsNotice.value = classifyConsoleError(err, {
      requiredGrantFallback: 'projects.write'
    })
  } finally {
    projectActionBusy.value = false
  }
}

async function refresh() {
  workspaces.value = await workspacesAPI.list()
  if (!workspaces.value.some(workspace => workspace.id === selectedID.value)) selectedID.value = workspaces.value[0]?.id ?? null
  await loadMembers()
  await loadProjects(selectedID.value, false)
}

async function createWorkspace() {
  busy.value = true
  try {
    const workspace = await workspacesAPI.create(workspaceName.value.trim())
    selectedID.value = workspace.id
    workspaceName.value = ''
    await refresh()
  } catch (error) { app.showError(normalizeApiError(error).message) }
  finally { busy.value = false }
}

async function saveMember() {
  if (!selected.value || !userID.value) return
  busy.value = true
  try {
    await workspacesAPI.setMember(selected.value.id, userID.value, memberRole.value)
    app.showSuccess(t('workspaces.saved'))
    await refresh()
  } catch (error) { app.showError(normalizeApiError(error).message) }
  finally { busy.value = false }
}

async function removeMember(memberID: number) {
  if (!selected.value) return
  busy.value = true
  try { await workspacesAPI.removeMember(selected.value.id, memberID); await refresh() }
  catch (error) { app.showError(normalizeApiError(error).message) }
  finally { busy.value = false }
}

onMounted(async () => {
  void loadRoutingPolicies()
  try { await refresh() }
  catch (error) { app.showError(normalizeApiError(error).message) }
  finally { loading.value = false }
})
</script>
