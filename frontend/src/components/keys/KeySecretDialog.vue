<template>
  <BaseDialog :show="show" :title="t('keys.secretOnce.title')" width="normal" @close="emit('close')">
    <div class="space-y-4">
      <div
        class="flex items-start gap-3 rounded-lg border border-green-200 bg-green-50 p-4 dark:border-green-800 dark:bg-green-900/20"
      >
        <p class="text-sm font-medium text-green-800 dark:text-green-200">
          {{ t('keys.secretOnce.warning') }}
        </p>
      </div>

      <div class="flex items-center gap-2">
        <code class="code flex-1 break-all text-xs">{{ secret }}</code>
        <button type="button" class="btn btn-secondary shrink-0" @click="copySecret">
          {{ copied ? t('keys.copied') : t('keys.copyToClipboard') }}
        </button>
      </div>

      <p v-if="graceUntil" class="text-xs text-gray-500 dark:text-gray-400">
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
