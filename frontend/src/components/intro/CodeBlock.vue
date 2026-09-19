<template>
  <div class="overflow-hidden rounded-xl border bg-gray-50 dark:bg-dark-900">
    <div class="flex items-center justify-between gap-3 border-b px-4 py-2">
      <span class="text-secondary text-xs font-medium">{{ label }}</span>
      <button type="button" class="btn btn-secondary btn-sm" :aria-label="`${t('helpGuide.copy')} ${label}`" @click="copyCode">{{ t('helpGuide.copy') }}</button>
    </div>
    <pre class="overflow-x-auto p-4 text-sm leading-7" tabindex="0" :aria-label="label"><code>{{ code }}</code></pre>
    <p v-if="status" role="status" class="text-secondary px-4 pb-3 text-sm">{{ t(`helpGuide.${status}`) }}</p>
  </div>
</template>
<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useClipboard } from '@/composables/useClipboard'
const props = defineProps<{ label: string; code: string }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const status = ref<'copied' | 'copyFailed' | ''>('')
async function copyCode() {
  try {
    status.value = await copyToClipboard(props.code, t('helpGuide.copied')) ? 'copied' : 'copyFailed'
  } catch {
    status.value = 'copyFailed'
  }
}
</script>
