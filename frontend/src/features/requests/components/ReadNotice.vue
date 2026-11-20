<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ErrorState, Skeleton } from '@/components/ui'
import type { NormalizedApiError } from '@/utils/apiError'
import { timestamp } from '../presentation'

const props = defineProps<{
  loading: boolean
  error: NormalizedApiError | null
  hasData: boolean
  offline?: boolean
  lastSynced?: string | null
  grant?: string
  rows?: number
}>()
const emit = defineEmits<{ retry: [] }>()
const { t, locale } = useI18n()
const denied = computed(() => props.error?.status === 403)
const stale = computed(() => props.hasData && (props.offline || props.error?.status === 0))
const title = computed(() => {
  if (denied.value) return t('costLedger.state.denied')
  if (stale.value) return t('costLedger.state.stale')
  if (props.error?.status === 404) return t('costLedger.state.missing')
  if (props.error?.status === 410) return t('costLedger.state.snapshotExpired')
  return t(props.hasData ? 'costLedger.state.partialFailure' : 'costLedger.state.loadFailed')
})
const message = computed(() => {
  if (denied.value && props.grant) return t('costLedger.state.requiredGrant', { grant: props.grant })
  if (stale.value && props.lastSynced) return t('costLedger.state.lastSynced', {
    at: timestamp(props.lastSynced, locale.value),
  })
  if (props.error?.status === 410) return t('costLedger.state.restartPage')
  return props.error?.message || undefined
})
const requestId = computed(() => {
  const value = props.error?.metadata.request_id
  return typeof value === 'string' ? value : undefined
})
</script>

<template>
  <ErrorState
    v-if="error || stale"
    :title="title"
    :message="message"
    :variant="denied ? 'permission' : stale ? 'stale' : 'error'"
    :code="error?.reason"
    :request-id="requestId"
    :retry-label="t('costLedger.state.retrySection')"
    :retryable="!denied && !offline"
    compact
    @retry="emit('retry')"
  />
  <div
    v-else-if="loading && hasData"
    role="status"
    class="flex items-center gap-2 text-xs text-oh-muted"
  >
    <span class="h-0.5 w-9 bg-oh-accent oh-skeleton-pulse" aria-hidden="true" />
    {{ t('costLedger.state.refreshing') }}
  </div>
  <div v-else-if="loading" class="rounded-oh-lg border border-oh-default bg-oh-surface">
    <Skeleton
      variant="table-row"
      :lines="rows ?? 4"
      :columns="4"
      :label="t('costLedger.state.loading')"
    />
  </div>
</template>
