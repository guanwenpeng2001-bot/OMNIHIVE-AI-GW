<template>
  <BaseDialog
    :show="show"
    :title="t('keys.secretOnce.title')"
    width="normal"
    :close-on-escape="canClose"
    :close-on-click-outside="false"
    :show-close-button="canClose"
    @close="requestClose"
  >
    <div data-testid="key-secret-once-dialog" class="space-y-4">
      <div
        v-if="scopeLabel"
        data-testid="key-secret-scope-pin"
        class="inline-flex items-center gap-2 rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2.5 py-1 font-mono text-xs text-[var(--foreground)]"
      >
        <span>{{ scopeLabel }}</span>
      </div>
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

      <label class="flex cursor-pointer items-center gap-2 rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-3 py-2 text-xs text-[var(--foreground)]">
        <input
          v-model="savedConfirmed"
          type="checkbox"
          data-testid="confirm-secret-saved-checkbox"
          class="h-3.5 w-3.5 rounded border-[var(--border)] text-amber-600 focus:ring-amber-500"
        />
        <span>{{ confirmSavedLabel }}</span>
      </label>
    </div>

    <template #footer>
      <button
        type="button"
        data-testid="confirm-secret-close-btn"
        class="btn btn-primary"
        :disabled="!canClose"
        @click="requestClose"
      >
        {{ t('common.confirm') }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useClipboard } from '@/composables/useClipboard'

const props = defineProps<{
  show: boolean
  secret: string
  graceUntil: string | null
  scopeLabel?: string | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
}>()

const i18n = useI18n()
const { t } = i18n
const { copied, copyToClipboard } = useClipboard()
const hasCopied = ref(false)
const savedConfirmed = ref(false)
const canClose = computed(() => hasCopied.value || savedConfirmed.value)
const isZh = computed(() => {
  const loc = (i18n as { locale?: { value?: string } }).locale?.value
  return String(loc || '').toLowerCase().startsWith('zh')
})
const confirmSavedLabel = computed(() =>
  isZh.value ? '我已复制并妥善保存此一次性明文密钥' : 'I have copied and safely stored this one-time secret'
)

watch(
  () => props.show,
  (visible) => {
    if (visible) {
      copied.value = false
      hasCopied.value = false
      savedConfirmed.value = false
    }
  }
)

async function copySecret() {
  if (props.secret) {
    const ok = await copyToClipboard(props.secret)
    if (ok) {
      hasCopied.value = true
    }
  }
}

function requestClose() {
  if (!canClose.value) return
  emit('close')
}

function formatGraceUntil(value: string): string {
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString()
}
</script>
