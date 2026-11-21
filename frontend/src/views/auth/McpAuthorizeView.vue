<template>
  <AuthLayout>
    <section class="mcp-consent space-y-5">
      <header><p class="mb-2 font-mono text-xs tracking-widest opacity-60">OMNIHIVE / MCP OAUTH</p><h1 class="text-2xl font-bold tracking-tight">{{ phase === 'approved' ? (localOnly ? t('publicPortal.mcpAuthorize.titleApprovedLocal') : t('publicPortal.mcpAuthorize.titleApproved')) : phase === 'denied' ? t('publicPortal.mcpAuthorize.titleDenied') : t('publicPortal.mcpAuthorize.titleConsent') }}</h1><p class="mt-2 text-sm opacity-60">{{ t('publicPortal.mcpAuthorize.subtitle') }}</p></header>
      <p v-if="requestError" role="alert" class="mcp-warning rounded-lg border p-4 text-sm"><b class="block">{{ t('publicPortal.mcpAuthorize.invalidRequestTitle') }}</b><span class="mt-2 block">{{ requestError }}</span></p>
      <dl class="mcp-summary space-y-3 rounded-xl border p-4 text-sm">
        <div><dt>{{ t('publicPortal.mcpAuthorize.appName') }}</dt><dd class="font-semibold">{{ request.client_name || t('publicPortal.mcpAuthorize.notProvided') }}</dd></div>
        <div><dt>{{ t('publicPortal.mcpAuthorize.clientId') }}</dt><dd class="font-mono">{{ request.client_id || t('publicPortal.mcpAuthorize.notProvided') }}</dd></div>
        <div><dt>{{ t('publicPortal.mcpAuthorize.redirectUri') }}</dt><dd class="font-mono">{{ request.redirect_uri || t('publicPortal.mcpAuthorize.notProvided') }}</dd></div>
        <div><dt>{{ t('publicPortal.mcpAuthorize.resourceScope') }}</dt><dd class="font-mono">{{ request.resource || t('publicPortal.mcpAuthorize.notProvided') }}</dd></div>
      </dl>
      <template v-if="phase === 'approved' || phase === 'denied'">
        <div class="mcp-result rounded-xl p-4" role="status">
          <span class="mb-2 block text-2xl">{{ phase === 'approved' ? '✓' : '×' }}</span>
          <h2 class="font-semibold">{{ phase === 'approved' ? (localOnly ? t('publicPortal.mcpAuthorize.statusApprovedLocal') : t('publicPortal.mcpAuthorize.statusApproved')) : t('publicPortal.mcpAuthorize.statusDenied') }}</h2>
          <p v-if="result" class="mt-2 text-sm">{{ result.scope === 'admin' ? t('publicPortal.mcpAuthorize.scopeAdmin') : t('publicPortal.mcpAuthorize.scopeUser') }} · {{ result.permission === 'write' ? t('publicPortal.mcpAuthorize.permissionWrite') : t('publicPortal.mcpAuthorize.permissionRead') }}</p>
          <p v-if="notice" class="mt-2 text-sm">{{ notice }}</p>
        </div>
        <button type="button" class="btn btn-primary w-full" @click="router.push('/settings/mcp')">{{ t('publicPortal.mcpAuthorize.viewGrants') }}</button>
      </template>
      <template v-else-if="!requestError">
        <form v-if="phase === 'stepup'" class="space-y-4" @submit.prevent="completeStepUp">
          <h2 class="font-semibold">{{ t('publicPortal.mcpAuthorize.stepupTitle') }}</h2>
          <p class="mcp-warning rounded-lg border p-3 text-sm">{{ t('publicPortal.mcpAuthorize.stepupNotice') }}</p>
          <label class="block text-sm" for="consent-totp">{{ t('publicPortal.mcpAuthorize.totpLabel') }}<input id="consent-totp" ref="otpInput" v-model="totpCode" class="input mt-2 text-center font-mono text-xl tracking-[.4em]" inputmode="numeric" maxlength="6" autocomplete="one-time-code" :disabled="busy" /></label>
          <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
          <div class="flex flex-wrap gap-2"><button type="submit" class="btn btn-primary flex-1" :disabled="busy">{{ busy ? t('publicPortal.mcpAuthorize.submitting') : t('publicPortal.mcpAuthorize.verifyAndAuthorize') }}</button><button type="button" class="btn btn-secondary" :disabled="busy" @click="cancelStepUp">{{ t('publicPortal.mcpAuthorize.cancelVerification') }}</button></div>
        </form>
        <form v-else class="space-y-4" @submit.prevent="approve">
          <fieldset :disabled="busy" class="space-y-4">
            <label class="block text-sm" for="consent-scope">{{ t('publicPortal.mcpAuthorize.scopeLabel') }}<select id="consent-scope" v-model="scope" class="input mt-2"><option value="user">{{ t('publicPortal.mcpAuthorize.scopeOptionUser') }}</option><option value="admin" :disabled="!allowAdmin">{{ t('publicPortal.mcpAuthorize.scopeOptionAdmin') }}</option></select></label>
            <label class="block text-sm" for="consent-permission">{{ t('publicPortal.mcpAuthorize.permissionLabel') }}<select id="consent-permission" v-model="permission" class="input mt-2"><option value="read">{{ t('publicPortal.mcpAuthorize.permissionRead') }}</option><option value="write">{{ t('publicPortal.mcpAuthorize.permissionWrite') }}</option></select></label>
            <p class="text-sm opacity-60">{{ permission === 'write' ? t('publicPortal.mcpAuthorize.permissionHintWrite') : t('publicPortal.mcpAuthorize.permissionHintRead') }}</p>
            <p v-if="scope === 'admin'" class="mcp-warning rounded-lg border p-3 text-sm">{{ t('publicPortal.mcpAuthorize.adminScopeWarning') }}</p>
            <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
            <div class="flex flex-wrap gap-2"><button type="submit" class="btn btn-primary flex-1">{{ busy ? t('publicPortal.mcpAuthorize.authorizing') : t('publicPortal.mcpAuthorize.confirmAuthorize') }}</button><button type="button" class="btn btn-secondary flex-1" @click="deny">{{ t('publicPortal.mcpAuthorize.denyAuthorize') }}</button></div>
          </fieldset>
        </form>
      </template>
      <button v-if="requestError" type="button" class="btn btn-secondary w-full" @click="router.push('/settings/mcp')">{{ t('publicPortal.mcpAuthorize.backToMcpSettings') }}</button>
    </section>
  </AuthLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AuthLayout from '@/components/layout/AuthLayout.vue'
import { useAuthStore } from '@/stores/auth'
import { mcpAPI, type MCPGrantDTO } from '@/api/mcp'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const endpoint = `${window.location.origin}/mcp`
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
  return typeof message === 'string' ? message : t('publicPortal.mcpAuthorize.errGeneric')
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
  if (!request.value.client_id || !request.value.redirect_uri || !request.value.resource) return t('publicPortal.mcpAuthorize.errMissingParams')
  if (!validMcpRedirect(request.value.redirect_uri)) return t('publicPortal.mcpAuthorize.errInvalidRedirect')
  if (request.value.resource !== endpoint) return t('publicPortal.mcpAuthorize.errResourceMismatch', { endpoint })
  return ''
})
function reset() { sequence++; phase.value = 'consent'; error.value = ''; notice.value = ''; totpCode.value = ''; result.value = null; busy.value = false }
watch(() => route.fullPath, () => {
  reset()
  const clientId = queryString('client_id').trim()
  request.value = {
    client_id: clientId,
    client_name: queryString('client_name').trim() || clientId,
    redirect_uri: queryString('redirect_uri').trim(),
    resource: queryString('resource').trim()
  }
  scope.value = queryString('scope') === 'admin' && allowAdmin.value ? 'admin' : 'user'
  permission.value = queryString('permission') === 'write' ? 'write' : 'read'
}, { immediate: true })
async function approve() {
  if (busy.value || requestError.value || phase.value !== 'consent') return
  if (scope.value === 'admin' && !allowAdmin.value) { error.value = t('publicPortal.mcpAuthorize.errAdminRequired'); return }
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
  if (!/^[0-9]{6}$/.test(totpCode.value)) { error.value = t('publicPortal.mcpAuthorize.errInvalidTotp'); return }
  await saveAuthorization(totpCode.value)
}
async function saveAuthorization(code?: string) {
  if (busy.value || requestError.value) return
  if (!authStore.user?.id) { error.value = t('publicPortal.mcpAuthorize.errLoginRequired'); return }
  if (scope.value === 'admin' && !allowAdmin.value) { error.value = t('publicPortal.mcpAuthorize.errAdminRequired'); return }
  const key = storageKey()
  const requestSequence = ++sequence
  busy.value = true
  error.value = ''
  const payload = { client_id: request.value.client_id, name: request.value.client_name || request.value.client_id, redirect_uri: request.value.redirect_uri, scope: scope.value, permission: permission.value, ...(code ? { totp_code: code } : {}) }
  try {
    const decision = await mcpAPI.authorizeGrant({ ...payload, resource: request.value.resource, decision: 'approve' })
    if (decision.decision !== 'approved') throw new Error(t('publicPortal.mcpAuthorize.errApproveUnconfirmed'))
    const grant = decision.grant
    if (disposed || requestSequence !== sequence || key !== storageKey()) return
    try {
      cacheGrants(key, [grant], true)
    } catch {
      if (grant.id.startsWith('local-')) throw new Error(t('publicPortal.mcpAuthorize.errLocalSaveFailed'))
      notice.value = t('publicPortal.mcpAuthorize.noticeSavedRemoteNoCache')
    }
    if (grant.id.startsWith('local-')) notice.value = t('publicPortal.mcpAuthorize.noticeSavedLocalOnly')
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
    if (decision.decision !== 'denied') throw new Error(t('publicPortal.mcpAuthorize.errDenyUnconfirmed'))
  } catch {
    if (disposed || requestSequence !== sequence || key !== storageKey()) return
    notice.value = t('publicPortal.mcpAuthorize.noticeDenySyncFailed')
  }
  try {
    const { grants } = await mcpAPI.listGrants()
    if (disposed || requestSequence !== sequence || key !== storageKey()) return
    cacheGrants(key, grants)
  } catch {
    if (disposed || requestSequence !== sequence || key !== storageKey()) return
    notice.value = t('publicPortal.mcpAuthorize.noticeDenyListSyncFailed')
  }
  if (disposed || requestSequence !== sequence || key !== storageKey()) return
  try {
    localStorage.setItem(`${key}:last-decision`, JSON.stringify({ client_id: request.value.client_id, decision: 'denied', created_at: new Date().toISOString() }))
  } catch {
    notice.value = t('publicPortal.mcpAuthorize.noticeDenyLocalSaveFailed')
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
