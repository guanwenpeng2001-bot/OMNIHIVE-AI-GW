<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { useAuthStore } from '@/stores/auth'
import { useConsoleScope } from '@/features/console/scope'
import { catalogApi, type CatalogModelItem } from '@/features/console/catalogApi'
import { availabilityStateLabel, billingStateLabel, formatPriceSourceLabel, formatPublicTimestamp, publicTokenPrice } from '@/features/public/format'
import { normalizeApiError } from '@/utils/apiError'
import type { components } from '@/api/generated/openapi'

const { locale } = useI18n()
const isZh = computed(() => String(locale.value).startsWith('zh'))
const auth = useAuthStore()
const scope = useConsoleScope()
const { scopes, availableProjects, activeWorkspaceId, activeProjectId, scopeError } = scope
const models = ref<CatalogModelItem[]>([])
const modelId = ref('')
const diagnostics = ref<components['schemas']['AdminModelDiagnostics'] | null>(null)
const loading = ref(false)
const error = ref('')
let generation = 0
let controller: AbortController | undefined
const evidence = computed(() => (diagnostics.value?.field_evidence ?? []).flatMap(fact =>
  Object.entries(fact.provenance ?? {}).map(([field, provenance]) => ({ field, ...provenance }))))

function changeWorkspace(event: Event) {
  const id = (event.target as HTMLSelectElement).value
  if (id) scope.selectWorkspaceProject(id, null)
  else scope.selectPersonalScope()
}
function changeProject(event: Event) {
  scope.selectWorkspaceProject(activeWorkspaceId.value, (event.target as HTMLSelectElement).value || null)
}
function beginLoad() {
  controller?.abort()
  controller = new AbortController()
  const current = ++generation
  error.value = ''
  diagnostics.value = null
  return { current, signal: controller.signal }
}
function showError(err: unknown) {
  const normalized = normalizeApiError(err)
  error.value = normalized.status === 403
    ? (isZh.value ? '此项目需要 models.read 和 connections.read 授权才能查看模型运营链路。' : 'This project requires models.read and connections.read to inspect model operations.')
    : (normalized.message || (isZh.value ? '模型诊断暂不可用，请重试。' : 'Model diagnostics are unavailable. Retry.'))
}
async function loadModels() {
  const { current, signal } = beginLoad()
  models.value = []
  modelId.value = ''
  if (!activeWorkspaceId.value || !activeProjectId.value) { loading.value = false; return }
  loading.value = true
  const workspace = activeWorkspaceId.value
  const project = activeProjectId.value
  try {
    const result = await catalogApi.listScopedModels(workspace, project, { accessible: null }, signal)
    if (current === generation) models.value = result
  } catch (err) {
    if (current === generation && !signal.aborted) showError(err)
  } finally {
    if (current === generation) loading.value = false
  }
}
async function loadDiagnostics() {
  const { current, signal } = beginLoad()
  if (!activeWorkspaceId.value || !activeProjectId.value || !modelId.value) { loading.value = false; return }
  loading.value = true
  const workspace = activeWorkspaceId.value
  const project = activeProjectId.value
  const id = modelId.value
  try {
    const result = await catalogApi.getModelDiagnostics(workspace, project, id, signal)
    if (current === generation) diagnostics.value = result
  } catch (err) {
    if (current === generation && !signal.aborted) showError(err)
  } finally {
    if (current === generation) loading.value = false
  }
}
watch([activeWorkspaceId, activeProjectId], loadModels)
onMounted(async () => { await scope.refreshScopes(); if (!models.value.length) await loadModels() })
onBeforeUnmount(() => { generation++; controller?.abort() })
</script>

<template>
  <AppLayout>
    <section class="space-y-5">
      <header class="flex flex-wrap items-start justify-between gap-3">
        <div><h1 class="text-xl font-semibold">{{ isZh ? '模型目录与运营链路' : 'Model catalog and operations' }}</h1><p class="mt-2 text-sm text-[var(--muted-foreground)]">{{ isZh ? '在有权项目内查看同一模型的事实来源、发现账号、分组报价与上架状态。' : 'Inspect model facts, discovered accounts, group tariffs, and publication within an authorized project.' }}</p></div>
        <RouterLink v-if="auth.isAdmin" class="btn btn-secondary" to="/admin/channels/pricing">{{ isZh ? '编辑渠道与分组价格' : 'Edit channel and group pricing' }}</RouterLink>
      </header>
      <div class="card grid gap-3 p-4 md:grid-cols-3">
        <label class="space-y-1 text-sm"><span>{{ isZh ? '工作空间' : 'Workspace' }}</span><select :value="activeWorkspaceId" class="input w-full" @change="changeWorkspace"><option value="">{{ isZh ? '选择工作空间' : 'Select workspace' }}</option><option v-for="workspace in scopes" :key="workspace.workspace_id" :value="workspace.workspace_id">{{ workspace.name }} (#{{ workspace.workspace_id }})</option></select></label>
        <label class="space-y-1 text-sm"><span>{{ isZh ? '项目' : 'Project' }}</span><select :value="activeProjectId" class="input w-full" :disabled="!activeWorkspaceId" @change="changeProject"><option value="">{{ isZh ? '选择项目' : 'Select project' }}</option><option v-for="project in availableProjects" :key="project.id" :value="project.id">{{ project.name }} (#{{ project.id }})</option></select></label>
        <label class="space-y-1 text-sm"><span>{{ isZh ? '模型（按 provider 区分）' : 'Model (provider qualified)' }}</span><select v-model="modelId" class="input w-full" :disabled="loading || !models.length" @change="loadDiagnostics"><option value="">{{ isZh ? '选择模型' : 'Select model' }}</option><option v-for="model in models" :key="model.id" :value="model.id">{{ model.provider }}/{{ model.displayName }}</option></select></label>
      </div>
      <p v-if="scopeError" role="alert" class="text-sm text-red-600">{{ isZh ? '作用域信息暂不可用，请到工作空间页刷新。' : 'Scope information is unavailable. Refresh it on the Workspaces page.' }}</p>
      <div v-if="error" role="alert" class="card flex flex-wrap items-center justify-between gap-3 p-4"><p class="text-sm text-red-600">{{ error }}</p><button class="btn btn-secondary" :disabled="loading" @click="modelId ? loadDiagnostics() : loadModels()">{{ isZh ? '重试' : 'Retry' }}</button></div>
      <p v-if="!activeProjectId" class="card p-5 text-sm">{{ isZh ? '模型运营诊断需要明确的项目范围。请选择有权项目；个人空间不会自动切换到 Default。' : 'Model diagnostics require a specific project. Select an authorized project; personal space does not switch to Default automatically.' }} <RouterLink class="underline" to="/workspaces">{{ isZh ? '管理工作空间授权' : 'Manage workspace access' }}</RouterLink></p>
      <p v-if="loading" role="status" class="p-4 text-sm">{{ isZh ? '加载模型运营链路…' : 'Loading model operations…' }}</p>
      <template v-if="diagnostics">
        <section class="card space-y-3 p-5">
          <h2 class="text-lg font-semibold">{{ diagnostics.model.provider }}/{{ diagnostics.model.display_name }}</h2>
          <p class="break-all font-mono text-xs">{{ diagnostics.model.id }}</p>
          <dl class="grid gap-4 text-sm sm:grid-cols-3"><div><dt class="text-[var(--muted-foreground)]">{{ isZh ? '来源' : 'Origins' }}</dt><dd>{{ diagnostics.model.origins?.join(', ') || (isZh ? '未知' : 'Unknown') }}</dd></div><div><dt class="text-[var(--muted-foreground)]">{{ isZh ? '上架状态' : 'Publication' }}</dt><dd>{{ diagnostics.model.publication_state || (isZh ? '未知' : 'Unknown') }}</dd></div><div><dt class="text-[var(--muted-foreground)]">{{ isZh ? '当前项目可用性' : 'Project availability' }}</dt><dd>{{ availabilityStateLabel(diagnostics.model.availability_state || 'unknown', isZh) }}</dd></div></dl>
          <p class="text-xs text-[var(--muted-foreground)]">{{ isZh ? '当前生效配置快照' : 'Current effective configuration snapshot' }} · {{ diagnostics.catalog_version }} · {{ formatPublicTimestamp(diagnostics.evaluated_at) }}</p>
        </section>
        <section class="card overflow-x-auto p-5">
          <h2 class="mb-3 font-semibold">{{ isZh ? '发现账号 → 绑定分组 → 销售价' : 'Discovered accounts → bound groups → sale pricing' }}</h2>
          <table class="w-full text-left text-sm"><thead><tr class="border-b border-[var(--border)]"><th class="p-2">{{ isZh ? '分组 / 路由' : 'Group / routing' }}</th><th class="p-2">{{ isZh ? '账号发现与连接' : 'Account discovery and connection' }}</th><th class="p-2">{{ isZh ? '销售价格 / 来源' : 'Sale tariff / source' }}</th></tr></thead><tbody><tr v-for="offer in diagnostics.offers" :key="offer.group_id" class="border-b border-[var(--border)] align-top"><td class="p-2"><p>#{{ offer.group_id }}</p><p class="mt-1 text-xs">{{ offer.eligible ? (isZh ? '路由候选' : 'Route eligible') : (isZh ? '不可路由' : 'Not routable') }} · {{ offer.access ? (isZh ? '项目可见' : 'Project visible') : (isZh ? '无访问权' : 'Access denied') }}</p><p v-if="offer.discovered_only" class="text-xs">{{ isZh ? '仅发现，未启用' : 'Discovered, not enabled' }}</p></td><td class="p-2"><p v-for="account in offer.accounts" :key="account.account_id" class="mb-2 text-xs">#{{ account.account_id }} · {{ account.discovery_state }} / {{ account.connection_state }}<br />{{ formatPublicTimestamp(account.observed_at) }}</p><span v-if="!offer.accounts?.length">{{ isZh ? '无账号证据' : 'No account evidence' }}</span></td><td class="p-2"><p>{{ billingStateLabel(offer.sale_price.billing_state || 'missing', isZh) }}</p><p class="mt-1 font-mono text-xs">{{ isZh ? '输入 / 输出每百万 token：' : 'Input / output per million tokens: ' }}{{ publicTokenPrice(offer.sale_price.input_rate, 'adaptive', isZh) }} / {{ publicTokenPrice(offer.sale_price.output_rate, 'adaptive', isZh) }}</p><p class="text-xs">{{ formatPriceSourceLabel(offer.sale_price.source, isZh) }}</p><p v-if="offer.sale_price.billing_reason" class="text-xs">{{ offer.sale_price.billing_reason }}</p><p v-if="offer.sale_price.additional_rates?.length || offer.sale_price.time_bands?.length" class="text-xs">{{ isZh ? '包含阶梯、缓存或分时费率，请在具体 Key 上获取完整报价。' : 'Includes tiered, cached, or time-dependent rates. Obtain a complete quote for a specific Key.' }}</p></td></tr></tbody></table>
          <p v-if="!diagnostics.offers.length" class="mt-3 text-sm">{{ isZh ? '当前项目没有绑定此模型的运营渠道；目录资料不代表承诺接入。' : 'This project has no bound operator offer for this model. Catalog information does not promise integration.' }}</p>
        </section>
        <section class="card overflow-x-auto p-5"><h2 class="mb-3 font-semibold">{{ isZh ? '字段级事实来源' : 'Field provenance' }}</h2><table class="w-full text-left text-xs"><thead><tr><th class="p-2">{{ isZh ? '字段' : 'Field' }}</th><th class="p-2">{{ isZh ? '来源 / 版本' : 'Source / revision' }}</th><th class="p-2">{{ isZh ? '审阅 / 选中原因' : 'Approval / selection reason' }}</th></tr></thead><tbody><tr v-for="(item, index) in evidence" :key="`${item.field}-${index}`" class="border-t border-[var(--border)]"><td class="p-2 font-mono">{{ item.field_path || item.field }}</td><td class="break-all p-2">{{ item.source_kind }}<br />{{ item.source_revision }}</td><td class="p-2">{{ item.approval }}<br />{{ item.selected_reason }}</td></tr></tbody></table><p v-if="!evidence.length" class="text-sm">{{ isZh ? '没有可展示的字段证据，保留未知状态。' : 'No field evidence is available; values remain unknown.' }}</p></section>
      </template>
    </section>
  </AppLayout>
</template>
