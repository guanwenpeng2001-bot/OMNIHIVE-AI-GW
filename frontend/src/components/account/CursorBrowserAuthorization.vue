<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { apiClient } from '@/api/client'

const props = defineProps<{ accountId?: number; account?: Record<string, unknown>; clientVersion: string }>()
const emit = defineEmits<{ success: [accountId: number]; busy: [value: boolean] }>()
const { t } = useI18n()
const state = ref('idle')
const authorizationUrl = ref('')
const copied = ref(false)
let sessionId = ''
let deadline = 0
let timer: ReturnType<typeof setTimeout> | undefined
let controller: AbortController | undefined
let disposed = false
type Authorization = { id: string; url?: string; status: string; expires_at: string; account_id?: number }

function finish(status: string) {
  state.value = status
  emit('busy', false)
  if (timer) clearTimeout(timer)
}
async function poll() {
  if (disposed || state.value !== 'waiting') return
  if (Date.now() >= deadline) { finish('timeout'); await cancelRemote(); return }
  try {
    const { data } = await apiClient.post<Authorization>(`/admin/accounts/cursor/auth/${sessionId}`, {}, { signal: controller?.signal })
    if (disposed || state.value !== 'waiting') return
    if (data.status === 'waiting') timer = setTimeout(poll, 4000)
    else {
      finish(data.status)
      if (data.status === 'success' && data.account_id) emit('success', data.account_id)
    }
  } catch {
    if (!disposed && state.value === 'waiting') finish('failed')
  }
}
async function start() {
  if (state.value === 'waiting') return
  state.value = 'waiting'
  authorizationUrl.value = ''
  copied.value = false
  emit('busy', true)
  controller = new AbortController()
  try {
    const { data } = await apiClient.post<Authorization>('/admin/accounts/cursor/auth', {
      account_id: props.accountId, account: props.account, client_version: props.clientVersion
    }, { signal: controller.signal })
    sessionId = data.id
    if (disposed || state.value !== 'waiting') { await cancelRemote(); return }
    authorizationUrl.value = data.url || ''
    deadline = new Date(data.expires_at).getTime()
    timer = setTimeout(poll, 4000)
  } catch { if (!disposed && state.value === 'waiting') finish('failed') }
}
async function cancelRemote() {
  if (sessionId) await apiClient.delete(`/admin/accounts/cursor/auth/${sessionId}`).catch(() => {})
}
async function cancel() {
  finish('cancelled')
  controller?.abort()
  await cancelRemote()
}
async function copyUrl() {
  try { await navigator.clipboard.writeText(authorizationUrl.value); copied.value = true } catch { copied.value = false }
}
onBeforeUnmount(() => { disposed = true; if (state.value === 'waiting') void cancel() })
</script>

<template>
  <div class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-600">
    <p class="input-hint">{{ t('admin.accounts.cursor.browserHint') }}</p>
    <button type="button" class="btn btn-secondary" :disabled="state === 'waiting' || (!accountId && !account?.name)" @click="start">{{ t('admin.accounts.cursor.browserAuthorize') }}</button>
    <div v-if="authorizationUrl" class="space-y-2">
      <a :href="authorizationUrl" target="_blank" rel="noopener noreferrer" class="block break-all text-sm text-primary-600">{{ authorizationUrl }}</a>
      <button type="button" class="btn btn-secondary" @click="copyUrl">{{ t(copied ? 'admin.accounts.cursor.copied' : 'admin.accounts.cursor.copyUrl') }}</button>
    </div>
    <p v-if="state !== 'idle'" role="status" aria-live="polite">{{ t(`admin.accounts.cursor.authStatus.${state}`) }}</p>
    <button v-if="state === 'waiting'" type="button" class="btn btn-secondary" @click="cancel">{{ t('common.cancel') }}</button>
  </div>
</template>
