<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Button, CopyButton } from '@/components/ui'
import type { NormalizedApiError } from '@/utils/apiError'
import type { CsvState } from '../useCsvExport'

defineProps<{ state: CsvState; operationId: string | null; error: NormalizedApiError | null }>()
const emit = defineEmits<{ check: []; retry: []; renew: []; download: [] }>()
const { t } = useI18n()
</script>

<template>
  <div
    v-if="state !== 'idle'"
    class="flex min-w-0 flex-wrap items-center justify-between gap-2 rounded-oh-md border p-3 text-sm"
    :class="state === 'expired' || state === 'conflict' || state === 'unknown' || state === 'failed' ? 'border-oh-status-warn-border bg-oh-status-warn-bg/55' : 'border-oh-default bg-oh-surface'"
    :role="state === 'failed' || state === 'conflict' ? 'alert' : 'status'"
  >
    <div class="min-w-0">
      <p class="font-medium text-oh-primary">{{ t(`costLedger.export.${state}`) }}</p>
      <p v-if="state === 'unknown'" class="mt-0.5 text-xs text-oh-secondary">{{ t('costLedger.export.verifyOutcome') }}</p>
      <p v-if="error?.status === 403" class="mt-0.5 text-xs text-oh-secondary">{{ t('costLedger.state.requiredGrant', { grant: 'billing.export' }) }}</p>
      <div v-if="operationId" class="mt-1 flex flex-wrap items-center gap-1.5 text-xs text-oh-muted">
        <code class="break-all">{{ operationId }}</code>
        <CopyButton :value="operationId" :label="t('costLedger.copy.label')" :copied-label="t('costLedger.copy.copied')" :failed-label="t('costLedger.copy.failed')" size="xs" variant="inline" />
      </div>
    </div>
    <div class="flex flex-wrap items-center gap-2">
      <Button v-if="state === 'ready' || state === 'downloaded'" size="sm" variant="primary" @click="emit('download')">{{ t('costLedger.export.download') }}</Button>
      <Button v-if="(state === 'unknown' || state === 'pending') && operationId" size="sm" variant="secondary" @click="emit('check')">{{ t('costLedger.export.check') }}</Button>
      <Button v-if="state === 'expired' || state === 'conflict'" size="sm" variant="secondary" @click="emit('renew')">{{ t('costLedger.export.renew') }}</Button>
      <Button v-if="(state === 'unknown' || state === 'failed') && !operationId" size="sm" variant="secondary" @click="emit('retry')">{{ t('costLedger.export.retry') }}</Button>
    </div>
  </div>
</template>
