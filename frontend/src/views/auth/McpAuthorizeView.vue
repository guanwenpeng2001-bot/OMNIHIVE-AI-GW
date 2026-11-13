<template>
  <AuthLayout>
    <section class="mcp-consent space-y-5">
      <header><p class="mb-2 font-mono text-xs tracking-widest opacity-60">OMNIHIVE / MCP OAUTH</p><h1 class="text-2xl font-bold tracking-tight">{{ phase === 'approved' ? (localOnly ? '授权请求已缓存' : '授权完成') : phase === 'denied' ? '已拒绝授权' : '授权应用访问' }}</h1><p class="mt-2 text-sm opacity-60">核对应用、回调地址与所需权限后决定。</p></header>
      <p v-if="requestError" role="alert" class="mcp-warning rounded-lg border p-4 text-sm"><b class="block">无效的授权请求</b><span class="mt-2 block">{{ requestError }}</span></p>
      <dl class="mcp-summary space-y-3 rounded-xl border p-4 text-sm">
        <div><dt>应用名称</dt><dd class="font-semibold">{{ request.client_name || '未提供' }}</dd></div>
        <div><dt>客户端 ID</dt><dd class="font-mono">{{ request.client_id || '未提供' }}</dd></div>
        <div><dt>回调地址</dt><dd class="font-mono">{{ request.redirect_uri || '未提供' }}</dd></div>
        <div><dt>范围资源</dt><dd class="font-mono">{{ request.resource || '未提供' }}</dd></div>
      </dl>
      <template v-if="phase === 'approved' || phase === 'denied'">
        <div class="mcp-result rounded-xl p-4" role="status">
          <span class="mb-2 block text-2xl">{{ phase === 'approved' ? '✓' : '×' }}</span>
          <h2 class="font-semibold">{{ phase === 'approved' ? (localOnly ? '等待授权服务确认' : '授权记录已保存') : '此请求未获得任何权限' }}</h2>
          <p v-if="result" class="mt-2 text-sm">{{ result.scope === 'admin' ? '平台管理' : '个人账户' }} · {{ result.permission === 'write' ? '允许修改' : '只读' }}</p>
          <p v-if="notice" class="mt-2 text-sm">{{ notice }}</p>
        </div>
        <button type="button" class="btn btn-primary w-full" @click="router.push('/settings/mcp')">查看授权记录 →</button>
      </template>
      <template v-else-if="!requestError">
        <form v-if="phase === 'stepup'" class="space-y-4" @submit.prevent="completeStepUp">
          <h2 class="font-semibold">平台管理权限确认</h2>
          <p class="mcp-warning rounded-lg border p-3 text-sm">允许修改平台管理数据需要二次确认，请输入身份验证器中的 6 位验证码。</p>
          <label class="block text-sm" for="consent-totp">6 位验证码<input id="consent-totp" ref="otpInput" v-model="totpCode" class="input mt-2 text-center font-mono text-xl tracking-[.4em]" inputmode="numeric" maxlength="6" autocomplete="one-time-code" :disabled="busy" /></label>
          <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
          <div class="flex flex-wrap gap-2"><button type="submit" class="btn btn-primary flex-1" :disabled="busy">{{ busy ? '正在提交…' : '验证并授权' }}</button><button type="button" class="btn btn-secondary" :disabled="busy" @click="cancelStepUp">取消验证</button></div>
        </form>
        <form v-else class="space-y-4" @submit.prevent="approve">
          <fieldset :disabled="busy" class="space-y-4">
            <label class="block text-sm" for="consent-scope">访问范围<select id="consent-scope" v-model="scope" class="input mt-2"><option value="user">个人账户</option><option value="admin" :disabled="!allowAdmin">平台管理（需两步验证）</option></select></label>
            <label class="block text-sm" for="consent-permission">权限<select id="consent-permission" v-model="permission" class="input mt-2"><option value="read">只读</option><option value="write">允许修改</option></select></label>
            <p class="text-sm opacity-60">{{ permission === 'write' ? '允许应用进行所选范围内的修改操作。' : '应用仅可读取所选范围的数据。' }}</p>
            <p v-if="scope === 'admin'" class="mcp-warning rounded-lg border p-3 text-sm">平台管理范围可能影响所有用户，允许修改时需要额外两步确认。</p>
            <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
            <div class="flex flex-wrap gap-2"><button type="submit" class="btn btn-primary flex-1">{{ busy ? '正在授权…' : '确认授权' }}</button><button type="button" class="btn btn-secondary flex-1" @click="deny">拒绝授权</button></div>
          </fieldset>
        </form>
      </template>
      <button v-if="requestError" type="button" class="btn btn-secondary w-full" @click="router.push('/settings/mcp')">返回 MCP 授权管理</button>
    </section>
  </AuthLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AuthLayout from '@/components/layout/AuthLayout.vue'
import { useAuthStore } from '@/stores/auth'
import { mcpAPI, type MCPGrantDTO } from '@/api/mcp'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const endpoint = `${window.location.origin}/mcp`
const scenario = ref('')
const hasClientId = computed(() => Object.prototype.hasOwnProperty.call(route.query, 'client_id'))
const request = ref({ client_id: '', client_name: '', redirect_uri: '', resource: '' })
const scope = ref<'user' | 'admin'>('user')
const permission = ref<'read' | 'write'>('read')
const phase = ref<'consent' | 'stepup' | 'approved' | 'denied'>('consent')
const busy = ref(false)
const error = ref('')
const notice = ref('')
const totpCode = ref('')
const otpInput = ref<HTMLInputElement | null>(null)
const result = ref<MCPGrantDTO | null>(null)
const localOnly = computed(() => result.value?.id.startsWith('local-') ?? false)
let sequence = 0
let disposed = false
const queryString = (key: string) => typeof route.query[key] === 'string' ? route.query[key] as string : ''
const allowAdmin = computed(() => Boolean(authStore.isAdmin))
const storageKey = () => `omnihive.mcp.grants.v1:${authStore.user?.id ?? 'guest'}`
const mcpErrorMessage = (failure: unknown) => {
  const message = (failure as { message?: unknown })?.message
  return typeof message === 'string' ? message : '操作失败，请稍后重试。'
}
const validMcpRedirect = (value: string) => {
  try {
    const parsed = new URL(value)
    return /^https?:\/\//i.test(value) && ['http:', 'https:'].includes(parsed.protocol) && Boolean(parsed.hostname) && !parsed.username && !parsed.password
  } catch { return false }
}
const cacheGrants = (key: string, grants: MCPGrantDTO[], merge = false) => {
  const cached: unknown = merge ? JSON.parse(localStorage.getItem(key) || '[]') : []
  const existing = Array.isArray(cached) ? cached.filter((item): item is MCPGrantDTO => Boolean(item && typeof item === 'object' && typeof item.id === 'string')) : []
  localStorage.setItem(key, JSON.stringify([
    ...grants.map(grant => ({ ...grant, ...(grant.id.startsWith('local-') ? { local_only: true } : {}) })),
    ...existing.filter(item => !grants.some(grant => grant.id === item.id))
  ]))
}
const requestError = computed(() => {
  if (scenario.value === 'missing' || !request.value.client_id || !request.value.redirect_uri || !request.value.resource) return '缺少必要参数：client_id、redirect_uri 和 resource 均为必填。请从客户端重新发起请求。'
  if (scenario.value === 'unsafe' || !validMcpRedirect(request.value.redirect_uri)) return '回调地址无效。仅允许完整的 http:// 或 https:// 地址，且不能包含用户名或密码。'
  if (request.value.resource !== endpoint) return `范围资源不属于本站 MCP 服务。预期地址：${endpoint}`
  return ''
})
function reset() { sequence++; phase.value = 'consent'; error.value = ''; notice.value = ''; totpCode.value = ''; result.value = null; busy.value = false }
watch(() => route.fullPath, () => {
  reset()
  scenario.value = queryString('scenario') || 'valid'
  request.value = hasClientId.value
    ? { client_id: queryString('client_id').trim(), client_name: queryString('client_name').trim() || queryString('client_id').trim(), redirect_uri: queryString('redirect_uri').trim(), resource: queryString('resource').trim() }
    : { client_id: 'claude-desktop-mcp', client_name: 'Claude Desktop MCP', redirect_uri: 'http://127.0.0.1:43110/oauth/callback', resource: endpoint }
  if (scenario.value === 'missing') request.value.client_id = ''
  if (scenario.value === 'unsafe') request.value.redirect_uri = 'javascript:alert(1)'
  scope.value = queryString('scope') === 'admin' && allowAdmin.value ? 'admin' : 'user'
  permission.value = queryString('permission') === 'write' ? 'write' : 'read'
}, { immediate: true })
async function approve() {
  if (busy.value || requestError.value || phase.value !== 'consent') return
  if (scope.value === 'admin' && !allowAdmin.value) { error.value = '平台管理范围需要管理员身份。'; return }
  error.value = ''
  if (scope.value === 'admin' && permission.value === 'write') {
    phase.value = 'stepup'
    totpCode.value = ''
    await nextTick()
    otpInput.value?.focus()
    return
  }
  await saveAuthorization()
}
function cancelStepUp() { if (!busy.value) { phase.value = 'consent'; totpCode.value = ''; error.value = '' } }
async function completeStepUp() {
  if (phase.value !== 'stepup' || busy.value) return
  if (!/^[0-9]{6}$/.test(totpCode.value)) { error.value = '请输入 6 位数字验证码。'; return }
  await saveAuthorization(totpCode.value)
}
async function saveAuthorization(code?: string) {
  if (busy.value || requestError.value) return
  if (!authStore.user?.id) { error.value = '请先登录后再授权。'; return }
  if (scope.value === 'admin' && !allowAdmin.value) { error.value = '平台管理范围需要管理员身份。'; return }
  const key = storageKey()
  const requestSequence = ++sequence
  busy.value = true
  error.value = ''
  const payload = { client_id: request.value.client_id, name: request.value.client_name || request.value.client_id, redirect_uri: request.value.redirect_uri, scope: scope.value, permission: permission.value, ...(code ? { totp_code: code } : {}) }
  try {
    const decision = await mcpAPI.authorizeGrant({ ...payload, resource: request.value.resource, decision: 'approve' })
    if (decision.decision !== 'approved') throw new Error('授权服务未确认授权结果。')
    const grant = decision.grant
    if (disposed || requestSequence !== sequence || key !== storageKey()) return
    try {
      cacheGrants(key, [grant], true)
    } catch {
      if (grant.id.startsWith('local-')) throw new Error('本地授权保存失败，请检查浏览器存储权限后重试。')
      notice.value = '服务端已保存授权记录；本地缓存不可用，可前往授权管理刷新查看。'
    }
    if (grant.id.startsWith('local-')) notice.value = '授权服务暂不可用，请求已保存在本机。请在服务恢复后重新确认授权。'
    result.value = grant
    phase.value = 'approved'
    totpCode.value = ''
  } catch (failure) { if (!disposed && requestSequence === sequence && key === storageKey()) error.value = mcpErrorMessage(failure) }
  finally { if (requestSequence === sequence) busy.value = false }
}
async function deny() {
  if (busy.value || requestError.value || phase.value !== 'consent') return
  const key = storageKey()
  const requestSequence = ++sequence
  busy.value = true
  error.value = ''
  totpCode.value = ''
  try {
    const decision = await mcpAPI.authorizeGrant({
      client_id: request.value.client_id,
      name: request.value.client_name || request.value.client_id,
      redirect_uri: request.value.redirect_uri,
      resource: request.value.resource,
      scope: scope.value,
      permission: permission.value,
      decision: 'deny'
    })
    if (disposed || requestSequence !== sequence || key !== storageKey()) return
    if (decision.decision !== 'denied') throw new Error('授权服务未确认拒绝结果。')
  } catch {
    if (disposed || requestSequence !== sequence || key !== storageKey()) return
    notice.value = '已取消本次授权，拒绝记录暂时无法同步。'
  }
  try {
    const { grants } = await mcpAPI.listGrants()
    if (disposed || requestSequence !== sequence || key !== storageKey()) return
    cacheGrants(key, grants)
  } catch {
    if (disposed || requestSequence !== sequence || key !== storageKey()) return
    notice.value = '已拒绝本次请求，授权列表暂时无法同步。'
  }
  if (disposed || requestSequence !== sequence || key !== storageKey()) return
  try {
    localStorage.setItem(`${key}:last-decision`, JSON.stringify({ client_id: request.value.client_id, decision: 'denied', created_at: new Date().toISOString() }))
  } catch {
    notice.value = '已拒绝本次请求，本地记录暂时无法保存。'
  }
  phase.value = 'denied'
  busy.value = false
}
watch(() => authStore.user?.id, () => { reset(); scope.value = 'user'; permission.value = 'read' })
onUnmounted(() => { disposed = true; sequence++ })
</script>

<style scoped>
.mcp-consent { color: var(--foreground); }
.segmented { display: flex; flex-wrap: wrap; gap: 3px; padding: 3px; border: 1px solid var(--border); border-radius: 9px; background: var(--background); }
.segmented button { flex: 1; padding: 7px; border-radius: 6px; font-size: 12px; white-space: nowrap; color: var(--muted-foreground); }
.segmented button.on { color: var(--foreground); background: var(--card); font-weight: 600; box-shadow: 0 1px 3px #00000015; }
.mcp-summary { border-color: var(--border); background: var(--background); }
.mcp-summary > div { display: grid; grid-template-columns: 80px minmax(0, 1fr); gap: 12px; }
.mcp-summary dt { color: var(--muted-foreground); }
.mcp-summary dd { overflow-wrap: anywhere; text-align: right; }
.mcp-warning, .mcp-result { background: var(--background); border: 1px solid var(--border); }
details { border-color: var(--border); }
button:focus-visible, summary:focus-visible { outline: 2px solid currentColor; outline-offset: 3px; }
</style>
