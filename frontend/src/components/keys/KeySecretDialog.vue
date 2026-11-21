<template>
  <BaseDialog :show="show" :title="t('keys.secretOnce.title')" width="normal" @close="emit('close')">
    <div data-testid="key-secret-once-dialog" class="space-y-4">
      <div
        class="flex items-start gap-3 rounded-[var(--oh-r-md,8px)] border border-[var(--oh-accent-border,rgba(245,158,11,0.4))] bg-[var(--oh-accent-subtle,rgba(245,158,11,0.08))] p-3.5"
      >
        <p class="text-sm font-medium text-[var(--foreground)]">
          {{ t('keys.secretOnce.warning') }}
        </p>
      </div>

      <div class="flex flex-col items-stretch gap-2 sm:flex-row sm:items-center">
        <code
          data-testid="key-secret-value"
          class="min-w-0 flex-1 break-all rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-inset,#121316)] p-3.5 font-mono text-xs leading-6 tabular-nums text-[#f4f4f5]"
        >{{ secret }}</code>
        <button
          type="button"
          data-testid="copy-key-secret-btn"
          class="btn btn-secondary shrink-0 font-mono text-xs"
          @click="copySecret"
        >
          {{ copied ? t('keys.copied') : t('keys.copyToClipboard') }}
        </button>
      </div>

      <p v-if="copied" role="status" class="font-mono text-xs text-oh-accent-text">
        ✓ {{ t('keys.copied') }}
      </p>

      <p v-if="graceUntil" class="font-mono text-xs tabular-nums text-[var(--oh-ink-secondary,var(--muted-foreground))]">
        {{ t('keys.secretOnce.graceUntil', { time: formatGraceUntil(graceUntil) }) }}
      </p>
    </div>

    <template #footer>
      <button type="button" class="btn btn-primary" @click="emit('close')">
        {{ t('common.confirm') }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useClipboard } from '@/composables/useClipboard'

const props = defineProps<{
  show: boolean
  secret: string
  graceUntil: string | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
}>()

const { t } = useI18n()
const { copied, copyToClipboard } = useClipboard()

watch(
  () => props.show,
  (visible) => {
    if (visible) {
      copied.value = false
    }
  }
)

function copySecret() {
  if (props.secret) {
    void copyToClipboard(props.secret)
  }
}

function formatGraceUntil(value: string): string {
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString()
}
</script>
