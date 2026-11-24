<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Skeleton } from '@/components/ui'
import type { AsyncViewState } from '@/features/shared/asyncState'

defineProps<{ state: AsyncViewState; message?: string }>()

const { t } = useI18n()
const labels: Record<AsyncViewState, string> = {
  skeleton: 'adminPlatform.operationState.skeleton',
  refreshing: 'adminPlatform.operationState.refreshing',
  ready: '',
  empty: 'adminPlatform.operationState.empty',
  'zero-filter': 'adminPlatform.operationState.zeroFilter',
  'partial-error': 'adminPlatform.operationState.partialError',
  forbidden: 'adminPlatform.operationState.forbidden',
  stale: 'adminPlatform.operationState.stale',
  conflict: 'adminPlatform.operationState.conflict',
  confirmed: 'adminPlatform.operationState.confirmed',
  unknown: 'adminPlatform.operationState.unknown',
}
</script>

<template>
  <div v-if="state !== 'ready' || message" role="status" class="space-y-2 text-sm text-oh-secondary">
    <Skeleton v-if="state === 'skeleton'" class="h-8 w-full" />
    <p>{{ message || t(labels[state]) }}</p>
  </div>
</template>
