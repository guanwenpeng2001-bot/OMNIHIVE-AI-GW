<template>
  <AppLayout>
    <section class="mcp-workbench w-full space-y-5">
      <header class="flex flex-wrap items-start justify-between gap-3">
        <div><h1 class="text-2xl font-bold tracking-tight">{{ t('nav.mcpAuth') }}</h1><p class="mt-1 text-sm opacity-60">{{ t('mcpSettings.subtitle') }}</p></div>
        <span class="chip font-mono text-xs">
          <span class="pulse-dot pulse-ok" aria-hidden="true" />
          <span>{{ authStore.isAdmin ? t('mcpSettings.adminAccess') : t('mcpSettings.personalAccess') }}</span>
        </span>
      </header>
      <p v-if="notice" role="status" class="mcp-notice rounded-lg p-3 text-sm">{{ notice }}</p>
      <p v-if="error" role="alert" class="rounded-lg border border-red-400 p-3 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
      <div class="grid items-start gap-5 xl:grid-cols-2">
      <div class="space-y-5">
      <section class="card space-y-4 p-5">
        <div class="flex flex-wrap items-center justify-between gap-2"><h2 class="font-semibold">{{ t('mcpSettings.connect') }}</h2><span class="flex items-center gap-2 text-sm"><span class="pulse-dot pulse-ok" aria-hidden="true" />{{ t('mcpSettings.online') }}</span></div>
        <label for="mcp-endpoint" class="block text-sm opacity-60">{{ t('mcpSettings.endpoint') }}</label>
        <div class="flex flex-wrap gap-2">
          <input id="mcp-endpoint" :value="endpoint" readonly class="input min-w-0 flex-1 font-mono text-sm" />
          <button type="button" class="btn btn-secondary" @click="copyEndpoint">{{ copied ? t('mcpSettings.copied') : t('mcpSettings.copy') }}</button>
        </div>
        <ol class="space-y-2 text-sm opacity-70">
          <li>{{ t('mcpSettings.connectStep') }}</li><li>{{ t('mcpSettings.reviewStep') }}</li><li>{{ t('mcpSettings.manageStep') }}</li>
        </ol>
      </section>
      <form class="card space-y-4 p-5" novalidate @submit.prevent="requestGrant">
        <h2 class="font-semibold">{{ t('mcpSettings.request') }}</h2>
        <fieldset :disabled="busy || loading" class="space-y-4">
          <label class="block text-sm" for="mcp-name">{{ t('publicPortal.mcpAuthorize.appName') }}<input id="mcp-name" v-model="form.name" class="input mt-1" maxlength="120" required :placeholder="t('mcpSettings.nameExample')" /></label>
          <label class="block text-sm" for="mcp-redirect">{{ t('publicPortal.mcpAuthorize.redirectUri') }}<input id="mcp-redirect" v-model="form.redirect_uri" class="input mt-1 font-mono text-sm" type="url" required placeholder="https://client.example.com/callback" aria-describedby="mcp-redirect-help" /></label>
          <p id="mcp-redirect-help" class="text-sm opacity-60">{{ t('mcpSettings.redirectHint') }}</p>
          <div class="grid gap-4 sm:grid-cols-2">
            <label class="block text-sm" for="mcp-scope">{{ t('publicPortal.mcpAuthorize.scopeLabel') }}<select id="mcp-scope" v-model="form.scope" class="input mt-1"><option value="user">{{ t('publicPortal.mcpAuthorize.scopeUser') }}</option><option v-if="authStore.isAdmin" value="admin">{{ t('publicPortal.mcpAuthorize.scopeAdmin') }}</option></select></label>
            <label class="block text-sm" for="mcp-permission">{{ t('publicPortal.mcpAuthorize.permissionLabel') }}<select id="mcp-permission" v-model="form.permission" class="input mt-1"><option value="read">{{ t('publicPortal.mcpAuthorize.permissionRead') }}</option><option value="write">{{ t('publicPortal.mcpAuthorize.permissionWrite') }}</option></select></label>
          </div>
          <p v-if="needsStepUp" class="mcp-warning rounded-lg border p-3 text-sm">{{ t('publicPortal.mcpAuthorize.adminScopeWarning') }}</p>
          <div class="flex flex-wrap gap-2">
            <button type="submit" class="btn btn-primary">{{ busy ? t('publicPortal.mcpAuthorize.authorizing') : t('mcpSettings.create') }}</button>
            <button type="button" class="btn btn-secondary" @click="openConsent">{{ t('mcpSettings.openConsent') }}</button>
          </div>
        </fieldset>
      </form>
      </div>
      <section class="card space-y-4 p-5" :aria-busy="loading">
        <div class="flex flex-wrap items-center justify-between gap-3"><h2 class="font-semibold">{{ t('mcpSettings.authorized') }} <span class="ml-1 text-sm font-normal opacity-50">{{ grants.length }}</span></h2><button type="button" class="btn btn-secondary btn-sm" :disabled="loading || busy" @click="refreshGrants">{{ loading ? t('mcpSettings.refreshing') : t('mcpSettings.refresh') }}</button></div>
        <p v-if="loading" role="status" class="py-6 text-center text-sm opacity-60">{{ t('mcpSettings.loading') }}</p>
        <div v-else-if="!grants.length" class="space-y-4 rounded-lg border border-dashed border-[var(--border)] p-5 text-sm text-[var(--muted-foreground)]" role="status">
          <div aria-hidden="true" class="space-y-3"><div v-for="row in 4" :key="row" class="flex h-5 items-center gap-3"><span class="h-3 w-3 rounded-full border border-[var(--border)]" /><span class="h-1 rounded bg-[var(--border)]" :style="{ width: `${65 - row * 9}%` }" /><span class="ml-auto h-3 w-10 rounded-full border border-[var(--border)]" /></div></div>
          <p>{{ t('mcpSettings.empty') }}</p>
        </div>
        <article v-for="grant in grants" :key="grant.id" class="mcp-grant rounded-xl border p-4">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div class="min-w-0 space-y-1"><h3 class="break-words font-semibold">{{ grant.name }}</h3><p class="break-all font-mono text-xs opacity-60">{{ t('publicPortal.mcpAuthorize.clientId') }} · {{ grant.client_id }}</p></div>
            <span class="flex items-center gap-2 text-sm"><span class="pulse-dot" :class="grantStatus(grant) === 'active' ? 'pulse-ok' : 'pulse-idle'" aria-hidden="true" />{{ t('mcpSettings.' + grantStatus(grant)) }}</span>
          </div>
          <div class="my-3 flex flex-wrap gap-2 text-xs"><span class="mcp-chip">{{ grant.scope === 'admin' ? t('publicPortal.mcpAuthorize.scopeAdmin') : t('publicPortal.mcpAuthorize.scopeUser') }}</span><span class="mcp-chip">{{ grant.permission === 'write' ? t('publicPortal.mcpAuthorize.permissionWrite') : t('publicPortal.mcpAuthorize.permissionRead') }}</span><span v-if="grant.local_only" class="mcp-chip">{{ t('mcpSettings.localCache') }}</span><span v-if="grant.pending_revoke" class="mcp-chip">{{ t('mcpSettings.pendingRevoke') }}</span></div>
          <p class="break-all text-sm opacity-70">{{ t('publicPortal.mcpAuthorize.redirectUri') }} · {{ grant.redirect_uri }}</p>
          <dl class="mt-3 grid gap-2 text-xs opacity-60 sm:grid-cols-3">
            <div><dt>{{ t('mcpSettings.created') }}</dt><dd class="mt-1">{{ formatTime(grant.created_at) }}</dd></div>
            <div><dt>{{ t('mcpSettings.expires') }}</dt><dd class="mt-1">{{ formatTime(grant.expires_at) }}</dd></div>
            <div><dt>{{ t('mcpSettings.lastUsed') }}</dt><dd class="mt-1">{{ formatTime(grant.last_used_at, t('mcpSettings.neverUsed')) }}</dd></div>
          </dl>
          <div v-if="grant.status !== 'revoked' || grant.pending_revoke" class="mt-3 flex justify-end"><button type="button" class="btn btn-secondary btn-sm text-red-600 dark:text-red-400" :disabled="busy || loading" :aria-label="t('mcpSettings.revokeNamed', { name: grant.name })" @click="revokeTarget = grant">{{ grant.pending_revoke ? t('mcpSettings.retryRevoke') : t('mcpSettings.revoke') }}</button></div>
        </article>
      </section>
      </div>
      <BaseDialog :show="stepUpOpen" :title="t('publicPortal.mcpAuthorize.stepupTitle')" width="narrow" :close-on-escape="!busy" :show-close-button="!busy" @close="cancelStepUp">
        <form class="space-y-4" novalidate @submit.prevent="confirmStepUp">
          <p class="text-sm opacity-70">{{ t('mcpSettings.stepUpMessage', { name: form.name || t('mcpSettings.unnamed') }) }}</p>
          <label for="mcp-totp" class="block text-sm">{{ t('publicPortal.mcpAuthorize.totpLabel') }}<input id="mcp-totp" v-model="totpCode" class="input mt-2 text-center font-mono text-xl tracking-[.4em]" inputmode="numeric" autocomplete="one-time-code" maxlength="6" pattern="[0-9]{6}" :disabled="busy" /></label>
          <p v-if="stepError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ stepError }}</p>
          <div class="flex justify-end gap-2"><button type="button" class="btn btn-secondary" :disabled="busy" @click="cancelStepUp">{{ t('common.cancel') }}</button><button type="submit" class="btn btn-primary" :disabled="busy">{{ busy ? t('publicPortal.mcpAuthorize.submitting') : t('publicPortal.mcpAuthorize.verifyAndAuthorize') }}</button></div>
        </form>
      </BaseDialog>
      <BaseDialog :show="Boolean(revokeTarget)" :title="t('mcpSettings.revokeTitle')" width="narrow" :close-on-escape="!busy" :show-close-button="!busy" @close="revokeTarget = null">
        <p class="text-sm leading-relaxed">{{ t('mcpSettings.revokeMessage', { name: revokeTarget?.name }) }}</p>
        <p v-if="error" role="alert" class="mt-3 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
        <template #footer><button type="button" class="btn btn-secondary" :disabled="busy" @click="revokeTarget = null">{{ t('common.cancel') }}</button><button type="button" class="btn btn-primary" :disabled="busy" @click="revokeGrant">{{ busy ? t('mcpSettings.revoking') : t('mcpSettings.confirmRevoke') }}</button></template>
      </BaseDialog>
    </section>
  </AppLayout>
</template>

<script lang="ts">
import { i18n } from '@/i18n'
import { apiClient } from '@/api/client'

export interface McpGrant {
  id: string
  client_id: string
  name: string
  redirect_uri: string
  scope: 'user' | 'admin'
  permission: 'read' | 'write'
  status: 'active' | 'revoked'
  created_at: string
  expires_at: string
  last_used_at: string | null
  local_only?: boolean
  pending_revoke?: boolean
}
export interface McpGrantRequest {
  client_id: string
  name: string
  redirect_uri: string
  scope: 'user' | 'admin'
  permission: 'read' | 'write'
  totp_code?: string
}
export const mcpStorageKey = (userId: number | string) => `omnihive.mcp.grants.v1:${userId}`
export function validMcpRedirect(value: string): boolean {
  if (!/^https?:\/\//i.test(value)) return false
  try {
    const parsed = new URL(value)
    return ['http:', 'https:'].includes(parsed.protocol) && Boolean(parsed.hostname) && !parsed.username && !parsed.password
  } catch { return false }
}
export function isMcpUnavailable(error: unknown): boolean {
  const failure = error as { status?: number; response?: { status?: number } } | null
  return [404, 502].includes(failure?.status ?? failure?.response?.status ?? 0)
}
export function isMcpGrant(value: unknown): value is McpGrant {
  if (!value || typeof value !== 'object') return false
  const grant = value as McpGrant
  return ['id', 'client_id', 'name', 'redirect_uri', 'created_at', 'expires_at'].every(key => typeof (value as Record<string, unknown>)[key] === 'string') &&
    ['user', 'admin'].includes(grant.scope) && ['read', 'write'].includes(grant.permission) && ['active', 'revoked'].includes(grant.status)
}
export function readMcpGrants(key: string): McpGrant[] {
  const raw = localStorage.getItem(key)
  if (!raw) return []
  const parsed: unknown = JSON.parse(raw)
  if (!Array.isArray(parsed) || !parsed.every(isMcpGrant)) throw new Error(i18n.global.t('mcpSettings.badCache'))
  return parsed
}
export function writeMcpGrants(key: string, grants: McpGrant[]): void {
  localStorage.setItem(key, JSON.stringify(grants))
}
export async function createMcpGrant(payload: McpGrantRequest): Promise<McpGrant> {
  try {
    const { data } = await apiClient.post<McpGrant>('/user/mcp/grants', payload)
    if (!isMcpGrant(data)) throw new Error(i18n.global.t('mcpSettings.badGrant'))
    return data
  } catch (failure) {
    if (!isMcpUnavailable(failure)) throw failure
  }
  const now = new Date()
  return {
    id: `local-${crypto.randomUUID()}`, client_id: payload.client_id, name: payload.name,
    redirect_uri: payload.redirect_uri, scope: payload.scope, permission: payload.permission,
    status: 'active', created_at: now.toISOString(), expires_at: new Date(now.getTime() + 30 * 86400000).toISOString(),
    last_used_at: null, local_only: true
  }
}
export function mcpErrorMessage(failure: unknown): string {
  const message = (failure as { message?: unknown })?.message
  return typeof message === 'string' ? message : i18n.global.t('publicPortal.mcpAuthorize.errGeneric')
}
</script>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useAuthStore } from '@/stores/auth'

const { t, locale } = useI18n()
const router = useRouter()
const authStore = useAuthStore()
const endpoint = `${window.location.origin}/mcp`
const storageKey = computed(() => mcpStorageKey(authStore.user?.id ?? 'guest'))
const form = reactive({ name: 'Claude Desktop', redirect_uri: 'https://client.example.com/callback', scope: 'user' as 'user' | 'admin', permission: 'read' as 'read' | 'write' })
const grants = ref<McpGrant[]>([])
const loading = ref(false)
const busy = ref(false)
const notice = ref('')
const error = ref('')
const copied = ref(false)
const stepUpOpen = ref(false)
const totpCode = ref('')
const stepError = ref('')
const revokeTarget = ref<McpGrant | null>(null)
const needsStepUp = computed(() => form.scope === 'admin' && form.permission === 'write')
let sequence = 0
let disposed = false
watch(needsStepUp, required => { if (required) { totpCode.value = ''; stepError.value = ''; stepUpOpen.value = true } })
function cancelStepUp() { if (!busy.value) { stepUpOpen.value = false; totpCode.value = ''; stepError.value = '' } }
function validateForm(): boolean {
  error.value = ''
  if (!form.name.trim()) error.value = t('mcpSettings.missingName')
  else if (!validMcpRedirect(form.redirect_uri.trim())) error.value = t('publicPortal.mcpAuthorize.errInvalidRedirect')
  return !error.value
}
async function copyEndpoint() {
  try { await navigator.clipboard.writeText(endpoint); copied.value = true; notice.value = t('mcpSettings.endpointCopied') }
  catch { error.value = t('mcpSettings.copyFailed') }
}
async function refreshGrants() {
  if (busy.value) return
  const request = ++sequence
  const key = storageKey.value
  loading.value = true
  error.value = ''
  notice.value = ''
  try {
    const { data } = await apiClient.get<{ grants: McpGrant[] }>('/user/mcp/grants')
    if (request !== sequence || disposed || key !== storageKey.value) return
    const rows = Array.isArray(data) ? data : data.grants
    if (!Array.isArray(rows) || !rows.every(isMcpGrant)) throw new Error(t('mcpSettings.badList'))
    let local: McpGrant[] = []
    try { local = readMcpGrants(key).filter(grant => grant.local_only) } catch { notice.value = t('mcpSettings.cacheUnavailable') }
    grants.value = [...rows, ...local.filter(grant => !rows.some(remote => remote.id === grant.id))]
    try { writeMcpGrants(key, grants.value) } catch { notice.value = t('mcpSettings.cacheWriteFailed') }
  } catch (failure) {
    if (request !== sequence || disposed || key !== storageKey.value) return
    if (isMcpUnavailable(failure)) {
      try { grants.value = readMcpGrants(key) }
      catch (storageFailure) { error.value = mcpErrorMessage(storageFailure) }
    } else error.value = mcpErrorMessage(failure)
  } finally { if (request === sequence) loading.value = false }
}
async function requestGrant() {
  if (busy.value || loading.value || !validateForm()) return
  if (needsStepUp.value) { totpCode.value = ''; stepError.value = ''; stepUpOpen.value = true; return }
  await saveGrant()
}
async function confirmStepUp() {
  if (!/^[0-9]{6}$/.test(totpCode.value)) { stepError.value = t('publicPortal.mcpAuthorize.errInvalidTotp'); return }
  if (!validateForm()) { stepError.value = error.value; return }
  await saveGrant(totpCode.value)
}
async function saveGrant(code?: string) {
  if (busy.value) return
  if (!authStore.user?.id) { error.value = t('publicPortal.mcpAuthorize.errLoginRequired'); return }
  if (form.scope === 'admin' && !authStore.isAdmin) { error.value = t('publicPortal.mcpAuthorize.errAdminRequired'); return }
  busy.value = true
  error.value = ''
  notice.value = ''
  const key = storageKey.value
  const request = ++sequence
  const payload: McpGrantRequest = { client_id: `oh-client-${crypto.randomUUID()}`, name: form.name.trim(), redirect_uri: form.redirect_uri.trim(), scope: form.scope, permission: form.permission, ...(code ? { totp_code: code } : {}) }
  try {
    const grant = await createMcpGrant(payload)
    if (disposed || request !== sequence || key !== storageKey.value) return
    let existing = grants.value
    try { existing = readMcpGrants(key) } catch { if (grant.local_only) throw new Error(t('mcpSettings.readFailed')) }
    const merged = [grant, ...existing.filter(item => item.id !== grant.id), ...grants.value.filter(item => item.id !== grant.id && !existing.some(cached => cached.id === item.id))]
    try { writeMcpGrants(key, merged) }
    catch { if (grant.local_only) throw new Error(t('publicPortal.mcpAuthorize.errLocalSaveFailed')); notice.value = t('publicPortal.mcpAuthorize.noticeSavedRemoteNoCache') }
    grants.value = merged
    notice.value ||= grant.local_only ? t('mcpSettings.savedLocal') : t('mcpSettings.saved')
    stepUpOpen.value = false
    totpCode.value = ''
  } catch (failure) { error.value = mcpErrorMessage(failure); stepError.value = error.value }
  finally { busy.value = false }
}
function openConsent() {
  if (!validateForm()) return
  void router.push({ path: '/oauth/mcp/authorize', query: {
    client_id: `oh-client-${crypto.randomUUID()}`,
    client_name: form.name.trim(), redirect_uri: form.redirect_uri.trim(),
    resource: endpoint, scope: form.scope, permission: form.permission
  } })
}
async function revokeGrant() {
  if (busy.value || !revokeTarget.value) return
  const target = revokeTarget.value
  const key = storageKey.value
  busy.value = true
  error.value = ''
  notice.value = ''
  ++sequence
  try {
    let pending = false
    if (!target.local_only) {
      try { await apiClient.delete(`/user/mcp/grants/${encodeURIComponent(target.id)}`) }
      catch (failure) { if (!isMcpUnavailable(failure)) throw failure; pending = true }
    }
    if (disposed || key !== storageKey.value) return
    let current = grants.value
    try { current = readMcpGrants(key) } catch { if (target.local_only || pending) throw new Error(t('mcpSettings.readRevokeFailed')) }
    const updated = grants.value.map(grant => grant.id === target.id ? { ...grant, status: 'revoked' as const, pending_revoke: pending } : grant)
    const merged = [...updated, ...current.filter(grant => !updated.some(item => item.id === grant.id))]
    try { writeMcpGrants(key, merged) }
    catch { if (target.local_only || pending) throw new Error(t('mcpSettings.revokeFailed')); notice.value = t('mcpSettings.revokedRemote') }
    grants.value = merged
    notice.value ||= pending ? t('mcpSettings.retryLater') : t('mcpSettings.revokedNotice')
    revokeTarget.value = null
  } catch (failure) { error.value = mcpErrorMessage(failure) }
  finally { busy.value = false }
}
function grantStatus(grant: McpGrant) { return grant.pending_revoke ? 'pending' : grant.status === 'revoked' ? 'revoked' : Date.parse(grant.expires_at) <= Date.now() ? 'expired' : 'active' }
function formatTime(value: string | null, fallback = '—') { const date = value ? new Date(value) : null; return date && Number.isFinite(date.getTime()) ? date.toLocaleString(locale.value, { hour12: false }) : fallback }
watch(storageKey, () => { sequence++; grants.value = []; revokeTarget.value = null; stepUpOpen.value = false; totpCode.value = ''; void refreshGrants() })
onMounted(refreshGrants)
onUnmounted(() => { disposed = true; sequence++ })
</script>

<style scoped>
.mcp-workbench { color: var(--foreground); font-variant-numeric: tabular-nums; }
.mcp-workbench .card { background: var(--card); border: 1px solid var(--border); }
.mcp-workbench .input { color: var(--foreground); background: var(--card); border-color: var(--border); }
.segmented { display: inline-flex; gap: 3px; padding: 3px; border: 1px solid var(--border); border-radius: 9px; background: var(--background); }
.segmented button { padding: 6px 12px; border-radius: 6px; font-size: 14px; color: var(--muted-foreground); }
.segmented button.on { background: var(--card); color: var(--foreground); box-shadow: 0 1px 3px #00000015; font-weight: 600; }
.mcp-warning { background: color-mix(in srgb, #f59e0b 12%, var(--card)); border-color: #f59e0b80; }
.mcp-notice { background: color-mix(in srgb, var(--brand, #f59e0b) 10%, var(--card)); }
.mcp-grant { border-color: var(--border); }
.mcp-chip { padding: 3px 8px; border-radius: 6px; background: var(--surface-2); border: 1px solid var(--border); }
.pulse-dot { display: inline-block; flex: none; width: 7px; height: 7px; border-radius: 50%; }
.pulse-ok { background: #16a34a; animation: mcp-breath 2s infinite; }
.pulse-idle { background: #a1a1aa; }
@keyframes mcp-breath { 50% { box-shadow: 0 0 0 5px #16a34a1f; } }
button:focus-visible { outline: 2px solid var(--brand, #f59e0b); outline-offset: 3px; }
@media (prefers-reduced-motion: reduce) { .pulse-ok { animation: none; } }
</style>
