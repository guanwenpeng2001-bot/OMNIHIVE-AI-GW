<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{
  total: number | null
  active: number
  visible: number
  standard: number
  subscription: number
  showBilling: boolean
}>()
const { t } = useI18n()
</script>

<template>
  <dl class="grid gap-2 sm:grid-cols-3" :aria-label="t('routing.pool.aria')">
    <div
      v-for="stat in [
        { label: t('routing.pool.total'), value: total },
        { label: t('routing.pool.active'), value: total == null ? null : active + ' / ' + visible },
        ...(showBilling ? [{ label: t('routing.pool.billing'), value: total == null ? null : standard + ' / ' + subscription }] : []),
      ]"
      :key="stat.label"
      class="flex min-w-0 items-center justify-between gap-3 rounded-oh-lg border border-oh-default bg-oh-surface px-4 py-3 sm:block"
    >
      <dt class="text-xs text-oh-secondary">{{ stat.label }}</dt>
      <dd class="font-mono text-lg font-semibold oh-tabular" :aria-label="stat.value == null ? t('routing.unknown') : undefined">{{ stat.value ?? '—' }}</dd>
    </div>
  </dl>
</template>
