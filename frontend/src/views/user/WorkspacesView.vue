<template>
  <AppLayout>
    <div class="space-y-6">
      <header class="card p-5 sm:p-6">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <div class="flex flex-wrap items-center gap-2.5">
              <h1 class="text-2xl font-bold tracking-tight text-[var(--foreground)]">
                {{ t('workspaces.title') }}
              </h1>
              <span class="chip text-xs">
                <span class="pulse-dot pulse-ok" aria-hidden="true" />
                <span class="font-mono tabular-nums">{{ workspaces.length }}</span>
              </span>
              <span v-if="selected" class="pill pill-ghost font-mono text-[11px] uppercase">
                {{ selected.role }}
              </span>
            </div>
            <p class="mt-1.5 text-sm text-[var(--muted-foreground)]">
              {{ t('workspaces.description') }}
            </p>
          </div>
        </div>
      </header>
      <form v-if="auth.isAdmin" class="card flex flex-wrap items-end gap-3 p-5" @submit.prevent="createWorkspace">
        <label class="flex-1 space-y-2 text-sm font-medium text-[var(--foreground)]">
          <span class="flex items-center gap-2">
            <span class="pulse-dot pulse-ok" aria-hidden="true" />
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
              <span class="pulse-dot pulse-ok" aria-hidden="true" />
              <span>{{ t('workspaces.select') }}</span>
            </span>
            <span v-if="selected" class="pill pill-ghost font-mono text-xs tabular-nums">
              #{{ selected.id }}
            </span>
          </span>
          <select v-model="selectedID" class="input w-full font-mono" :disabled="loading || busy" @change="loadMembers">
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
            <span class="pulse-dot pulse-ok" aria-hidden="true" />
            <h2 class="text-lg font-semibold text-[var(--foreground)]">
              {{ selected.name }} · {{ t('workspaces.resources') }}
            </h2>
          </div>
          <span class="chip font-mono text-xs tabular-nums">
            {{ selected.role === 'admin' ? resources.length + 1 : 1 }}
          </span>
        </div>
        <div class="flex flex-wrap gap-3">
          <RouterLink :to="`/workspaces/${selected.id}/keys`" class="btn btn-secondary">{{ t('nav.apiKeys') }}</RouterLink>
          <template v-if="selected.role === 'admin'">
            <RouterLink v-for="resource in resources" :key="resource.path" :to="`/workspaces/${selected.id}/${resource.path}`" class="btn btn-secondary">{{ t(resource.label) }}</RouterLink>
          </template>
        </div>
      </section>
      <section v-if="selected?.role === 'admin'" class="card space-y-4 p-5" :aria-busy="loadingMembers">
        <div class="flex flex-wrap items-center justify-between gap-2 border-b border-[var(--border)] pb-3">
          <div class="flex items-center gap-2">
            <span class="pulse-dot pulse-ok" aria-hidden="true" />
            <h2 class="text-lg font-semibold text-[var(--foreground)]">{{ t('workspaces.members') }}</h2>
          </div>
          <span class="chip font-mono text-xs tabular-nums">{{ members.length }}</span>
        </div>
        <form class="flex flex-wrap items-end gap-3 rounded-xl border border-[var(--border)] bg-[var(--surface-2)] p-4" @submit.prevent="saveMember">
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

const { t } = useI18n()
const auth = useAuthStore()
const app = useAppStore()
const workspaces = ref<Workspace[]>([])
const members = ref<WorkspaceMember[]>([])
const selectedID = ref<number | null>(null)
const selected = computed(() => workspaces.value.find(workspace => workspace.id === selectedID.value))
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

async function refresh() {
  workspaces.value = await workspacesAPI.list()
  if (!workspaces.value.some(workspace => workspace.id === selectedID.value)) selectedID.value = workspaces.value[0]?.id ?? null
  await loadMembers()
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
