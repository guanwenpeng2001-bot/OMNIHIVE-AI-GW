<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { getEffectiveSettings, resetSettingOverrides, type EffectiveSettings, type SettingEffectiveValue } from '@/api/admin/effectiveSettings'
import { normalizeApiError } from '@/utils/apiError'

const { locale } = useI18n()
const isZh = computed(() => String(locale.value).startsWith('zh'))
const snapshot = ref<EffectiveSettings | null>(null)
const query = ref('')
const group = ref('')
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const pending = ref<{ keys: string[]; groups: string[]; label: string } | null>(null)
let generation = 0
let controller: AbortController | undefined

const groups = computed(() => [...new Set(snapshot.value?.items.map(item => item.group) ?? [])].sort())
const visible = computed(() => {
  const term = query.value.trim().toLowerCase()
  return (snapshot.value?.items ?? []).filter(item => (!group.value || item.group === group.value) &&
    (!term || [item.key, item.group, item.source, item.validation_state].some(value => value.toLowerCase().includes(term))))
})
const canReset = computed(() => Boolean(snapshot.value?.setting_version && snapshot.value.setting_version !== 'unversioned') && !loading.value && !saving.value && !error.value)

function valueText(value: unknown): string {
  if (value === null || value === undefined || value === '') return '—'
  return typeof value === 'object' ? JSON.stringify(value) : String(value)
}
function effectiveText(item: SettingEffectiveValue): string {
  return item.sensitive ? (item.configured ? (isZh.value ? '已配置（隐藏）' : 'Configured (hidden)') : (isZh.value ? '未配置' : 'Not configured')) : valueText(item.effective_value)
}
function sourceText(source: string): string {
  const labels: Record<string, string> = isZh.value
    ? { registry_default: '注册表默认值', environment: '环境变量', yaml: '部署配置', database_override: '数据库覆盖', migration_snapshot: '升级前生效值', deployment_config: '部署配置' }
    : { registry_default: 'Registry default', environment: 'Environment', yaml: 'Deployment config', database_override: 'Database override', migration_snapshot: 'Pre-upgrade value', deployment_config: 'Deployment config' }
  return labels[source] || source
}
function proposeReset(item: SettingEffectiveValue) {
  if (!canReset.value || !item.can_reset || item.override_state === 'inherit') return
  const wholeGroup = snapshot.value?.reset_groups.includes(item.group)
  pending.value = { keys: wholeGroup ? [] : [item.key], groups: wholeGroup ? [item.group] : [], label: wholeGroup ? item.group : item.key }
}
async function load() {
  controller?.abort()
  controller = new AbortController()
  const current = ++generation
  loading.value = true
  error.value = ''
  pending.value = null
  try {
    const result = await getEffectiveSettings(controller.signal)
    if (current === generation) snapshot.value = result
  } catch (err) {
    if (current === generation && !controller.signal.aborted) {
      snapshot.value = null
      error.value = normalizeApiError(err).message || (isZh.value ? '配置诊断暂不可用，请重试。' : 'Configuration diagnostics are unavailable. Retry.')
    }
  } finally {
    if (current === generation) loading.value = false
  }
}
async function confirmReset() {
  if (!canReset.value || !pending.value || !snapshot.value) return
  saving.value = true
  try {
    snapshot.value = await resetSettingOverrides({ expected_version: snapshot.value.setting_version, groups: pending.value.groups, keys: pending.value.keys })
    pending.value = null
  } catch (err) {
    const normalized = normalizeApiError(err)
    pending.value = null
    error.value = normalized.status === 409
      ? (isZh.value ? '配置已被其他操作修改。请刷新并重新检查后再重置。' : 'Configuration changed. Refresh and review before resetting again.')
      : (normalized.message || (isZh.value ? '重置失败，请刷新检查当前配置。' : 'Reset failed. Refresh to inspect the current configuration.'))
  } finally {
    saving.value = false
  }
}
onMounted(load)
onBeforeUnmount(() => { generation++; controller?.abort() })
</script>

<template>
  <AppLayout>
    <section class="space-y-5">
      <header class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 class="text-xl font-semibold">{{ isZh ? '配置生效值与来源' : 'Effective configuration and sources' }}</h1>
          <p class="mt-2 max-w-3xl text-sm text-[var(--muted-foreground)]">{{ isZh ? '查看实际生效值、默认值和来源。重置移除数据库覆盖，恢复环境变量、部署配置或注册表默认值；OAuth 凭据会按提供方整组重置。' : 'Inspect effective values, defaults, and sources. Reset removes database overrides and restores environment, deployment, or registry values. OAuth credentials reset as a complete provider group.' }}</p>
        </div>
        <div class="flex gap-2">
          <RouterLink class="btn btn-secondary" to="/admin/settings">{{ isZh ? '编辑设置' : 'Edit settings' }}</RouterLink>
          <button class="btn btn-primary" :disabled="loading || saving" @click="load">{{ loading ? (isZh ? '加载中…' : 'Loading…') : (isZh ? '刷新' : 'Refresh') }}</button>
        </div>
      </header>
      <p v-if="error" role="alert" class="rounded-lg border border-red-300 bg-red-50 p-3 text-sm text-red-800 dark:bg-red-950 dark:text-red-200">{{ error }}</p>
      <div v-if="pending" role="alertdialog" aria-modal="false" :aria-label="isZh ? '确认重置配置覆盖' : 'Confirm override reset'" class="card space-y-3 p-4">
        <p>{{ isZh ? '将移除以下配置覆盖并恢复继承：' : 'Remove these overrides and restore inherited values:' }} <strong>{{ pending.label }}</strong></p>
        <p v-if="pending.groups.length" class="text-sm">{{ isZh ? '该提供方的开关、客户端 ID、密钥和回调配置将一起重置，可能影响后续登录。' : 'This resets the provider switch, client ID, secret, and callback settings together and may affect subsequent sign-ins.' }}</p>
        <div class="flex gap-2"><button class="btn btn-primary" :disabled="!canReset" @click="confirmReset">{{ isZh ? '确认重置' : 'Confirm reset' }}</button><button class="btn btn-secondary" :disabled="saving" @click="pending = null">{{ isZh ? '取消' : 'Cancel' }}</button></div>
      </div>
      <div class="flex flex-wrap items-center gap-3">
        <input v-model="query" class="input min-w-64 flex-1" type="search" :placeholder="isZh ? '搜索配置键、来源或状态' : 'Search keys, sources, or states'" :aria-label="isZh ? '搜索配置' : 'Search configuration'" />
        <select v-model="group" class="input w-auto" :aria-label="isZh ? '配置组' : 'Configuration group'"><option value="">{{ isZh ? '全部配置组' : 'All groups' }}</option><option v-for="name in groups" :key="name" :value="name">{{ name }}</option></select>
        <span class="text-xs text-[var(--muted-foreground)]">{{ visible.length }} {{ isZh ? '项' : 'settings' }}</span>
      </div>
      <div class="card overflow-x-auto">
        <table class="w-full text-left text-sm">
          <thead class="border-b border-[var(--border)]"><tr><th class="p-3">{{ isZh ? '配置 / 状态' : 'Setting / state' }}</th><th class="p-3">{{ isZh ? '生效值 / 来源' : 'Effective value / source' }}</th><th class="p-3">{{ isZh ? '默认值 / 版本' : 'Default / version' }}</th><th class="p-3">{{ isZh ? '操作' : 'Action' }}</th></tr></thead>
          <tbody><tr v-for="item in visible" :key="item.key" class="border-b border-[var(--border)] align-top">
            <td class="p-3"><code class="break-all">{{ item.key }}</code><p class="mt-1 text-xs text-[var(--muted-foreground)]">{{ item.group }} · {{ item.validation_state }}</p><p v-if="item.blocked_by.length" class="mt-1 text-xs text-amber-700">{{ isZh ? '受部署开关限制：' : 'Blocked by deployment settings: ' }}{{ item.blocked_by.join(', ') }}</p><p v-if="item.restart_required" class="text-xs">{{ isZh ? '需重启后生效' : 'Restart required' }}</p></td>
            <td class="max-w-md p-3"><span class="break-all whitespace-pre-wrap">{{ effectiveText(item) }}</span><p class="mt-1 text-xs text-[var(--muted-foreground)]">{{ sourceText(item.source) }} · {{ item.override_state }}</p><p v-if="item.key_id" class="mt-1 text-xs">Key ID: {{ item.key_id }}</p></td>
            <td class="max-w-xs p-3"><span class="break-all">{{ item.sensitive ? '—' : valueText(item.default_value) }}</span><p class="mt-1 text-xs text-[var(--muted-foreground)]">{{ item.default_version }}</p></td>
            <td class="p-3"><button v-if="item.can_reset && item.override_state !== 'inherit'" class="btn btn-secondary btn-sm" :disabled="!canReset" @click="proposeReset(item)">{{ snapshot?.reset_groups.includes(item.group) ? (isZh ? '重置整组' : 'Reset group') : (isZh ? '恢复继承' : 'Restore inheritance') }}</button><span v-else class="text-xs text-[var(--muted-foreground)]">{{ isZh ? '继承或部署管理' : 'Inherited or deployment managed' }}</span></td>
          </tr></tbody>
        </table>
        <p v-if="!loading && !visible.length" class="p-6 text-center text-sm">{{ isZh ? '没有匹配的配置。' : 'No matching settings.' }}</p>
      </div>
      <p v-if="snapshot" class="break-all font-mono text-xs text-[var(--muted-foreground)]">{{ isZh ? '配置快照版本：' : 'Settings snapshot: ' }}{{ snapshot.setting_version }}</p>
    </section>
  </AppLayout>
</template>
