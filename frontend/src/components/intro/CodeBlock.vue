<template>
  <div class="doc-terminal overflow-hidden rounded-lg border">
    <div class="doc-terminal-bar flex items-center justify-between gap-3 border-b px-4 py-2">
      <div class="flex min-w-0 items-center gap-3">
        <span class="terminal-dots flex shrink-0 gap-1.5" aria-hidden="true"><i></i><i></i><i></i></span>
        <span class="truncate font-mono text-xs">{{ label }}</span>
      </div>
      <button type="button" class="terminal-copy shrink-0 rounded border px-3 py-1.5 font-mono text-xs" :aria-label="`${t('helpGuide.copy')} ${label}`" @click="copyCode">{{ t(status === 'copied' ? 'helpGuide.copied' : 'helpGuide.copy') }}</button>
    </div>
    <pre class="overflow-x-auto p-4 text-sm leading-7" tabindex="0" :aria-label="label"><code>{{ code }}</code></pre>
    <p v-if="status" role="status" class="text-secondary px-4 pb-3 text-sm">{{ t(`helpGuide.${status}`) }}</p>
  </div>
</template>
<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useClipboard } from '@/composables/useClipboard'
const props = defineProps<{ label: string; code: string }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const status = ref<'copied' | 'copyFailed' | ''>('')
watch(() => props.code, () => { status.value = '' })
async function copyCode() {
  try {
    status.value = await copyToClipboard(props.code, t('helpGuide.copied')) ? 'copied' : 'copyFailed'
  } catch {
    status.value = 'copyFailed'
  }
}
</script>

<style scoped>
.doc-terminal { background: var(--card); border-color: var(--border); color: var(--foreground); }
.doc-terminal-bar { background: var(--surface-2); border-color: var(--border); color: var(--muted-foreground); }
.terminal-dots i { width: 7px; height: 7px; border: 1px solid var(--muted-foreground); border-radius: 50%; opacity: .55; }
.terminal-dots i:first-child { background: var(--brand); border-color: var(--brand); opacity: 1; }
.terminal-copy { border-color: var(--border); background: var(--card); color: var(--foreground); }
.terminal-copy:hover { color: var(--brand-text); border-color: var(--brand); }
pre { font-family: var(--font-mono, ui-monospace, monospace); tab-size: 2; }
:is(button, pre):focus-visible { outline: 2px solid var(--brand); outline-offset: -2px; }
</style>
