<template>
  <BaseDialog :show="show" :title="t('usage.exporting')" width="narrow" @close="$emit('cancel')">
    <div class="space-y-4">
      <p class="text-sm text-[var(--muted-foreground)]">{{ t('usage.exportingProgress') }}</p>
      <div class="rounded-xl border border-[var(--border)] bg-[var(--surface-2)] p-4">
        <div class="mb-3 flex flex-wrap items-end justify-between gap-2">
          <span class="text-xs text-[var(--muted-foreground)]">{{ t('usage.exportedCount', { current, total }) }}</span>
          <span class="font-mono text-2xl font-semibold tabular-nums text-[var(--foreground)]">{{ normalizedProgress }}<span class="text-sm">%</span></span>
        </div>
        <div
          role="progressbar"
          :aria-label="t('usage.exportingProgress')"
          :aria-valuenow="normalizedProgress"
          :aria-valuetext="t('usage.exportedCount', { current, total })"
          aria-valuemin="0"
          aria-valuemax="100"
          class="h-1.5 overflow-hidden rounded-full bg-[var(--border)]"
        >
          <div class="h-full rounded-full bg-[var(--brand)] transition-[width] motion-reduce:transition-none" :style="{ width: `${normalizedProgress}%` }"></div>
        </div>
      </div>
      <p v-if="estimatedTime" class="text-xs text-[var(--muted-foreground)]" aria-live="polite" aria-atomic="true">
        {{ t('usage.estimatedTime', { time: estimatedTime }) }}
      </p>
    </div>
    <template #footer>
      <button type="button" class="btn btn-secondary" @click="$emit('cancel')">{{ t('usage.cancelExport') }}</button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
const props = defineProps<{ show: boolean, progress: number, current: number, total: number, estimatedTime: string }>()
defineEmits(['cancel'])
const { t } = useI18n()
const normalizedProgress = computed(() => Math.min(100, Math.max(0, Math.round(Number.isFinite(props.progress) ? props.progress : 0))))
</script>
