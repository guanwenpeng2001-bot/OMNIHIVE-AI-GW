<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { authAPI } from '@/api'
import { Button, Card, Modal } from '@/components/ui'
import { useAuthStore } from '@/stores/auth'
import { useScopedRead } from '@/features/requests/useScopedRead'
import { classifyAsyncError, type AsyncViewState } from '@/features/shared/asyncState'
import OperationState from './OperationState.vue'

const { t } = useI18n()
const auth = useAuthStore()
const key = computed(() => auth.user ? JSON.stringify([auth.user.id, auth.sessionRevision]) : '')
const currentSession = useScopedRead(() => key.value, async () => {
  const { data } = await authAPI.getCurrentUser()
  if (String(data.id) !== String(auth.user?.id)) throw { status: 401 }
  return true
})
const status = computed<AsyncViewState>(() => {
  if (currentSession.loading.value) return currentSession.data.value ? 'refreshing' : 'skeleton'
  if (currentSession.error.value?.status === 403) return 'forbidden'
  if (currentSession.stale.value) return 'stale'
  if (currentSession.error.value) return 'partial-error'
  return currentSession.data.value ? 'ready' : 'empty'
})
const confirming = ref(false)
const revoking = ref(false)
const revokeState = ref<AsyncViewState>('ready')
let revision = 0
watch(key, () => {
  revision++
  confirming.value = false
  revoking.value = false
  revokeState.value = 'ready'
}, { flush: 'sync' })
onScopeDispose(() => { revision++ })

async function revokeAll() {
  if (!key.value || revoking.value) return
  const current = revision
  revoking.value = true
  revokeState.value = 'refreshing'
  try {
    await authAPI.revokeAllSessions()
    if (revision !== current) return
    revokeState.value = 'confirmed'
    confirming.value = false
    await auth.logout()
  } catch (cause) {
    if (revision !== current) return
    const issue = classifyAsyncError(cause)
    revokeState.value = issue.status >= 500 ? 'unknown' : issue.state === 'error' ? 'partial-error' : issue.state
    if (revokeState.value === 'unknown') {
      confirming.value = false
      void currentSession.refresh()
    }
  } finally {
    if (revision === current) revoking.value = false
  }
}
</script>

<template>
  <Card class="space-y-3" aria-labelledby="profile-sessions-title">
    <h2 id="profile-sessions-title" class="font-semibold">{{ t('profileOverview.sessions') }}</h2>
    <p class="text-sm text-oh-secondary">{{ t('profileSessions.description') }}</p>
    <OperationState :state="status" :message="status === 'partial-error' ? t('profileSessions.unverified') : undefined" />
    <ul v-if="currentSession.data.value" class="border-y border-oh-subtle py-2 text-sm">
      <li>{{ t('profileSessions.browser') }} · {{ currentSession.stale.value ? t('profileSessions.previous') : t('profileSessions.verified') }}</li>
    </ul>
    <div class="flex flex-wrap gap-2">
      <Button size="sm" variant="secondary" :disabled="currentSession.loading.value || revoking" @click="currentSession.refresh">{{ t('profileSessions.verify') }}</Button>
      <Button size="sm" variant="danger" :disabled="!currentSession.data.value || currentSession.stale.value || revoking" @click="confirming = true">{{ t('profileSessions.revoke') }}</Button>
    </div>
    <OperationState v-if="!confirming" :state="revokeState" />
    <Modal v-model:open="confirming" :title="t('profileSessions.revoke')" :description="t('profileSessions.confirmDescription')"
      :close-on-backdrop="!revoking" :close-on-escape="!revoking" :hide-close-button="revoking">
      <OperationState :state="revokeState" />
      <template #footer>
        <Button variant="secondary" :disabled="revoking" @click="confirming = false">{{ t('common.cancel') }}</Button>
        <Button variant="danger" :loading="revoking" @click="revokeAll">{{ t('profileSessions.confirm') }}</Button>
      </template>
    </Modal>
  </Card>
</template>
