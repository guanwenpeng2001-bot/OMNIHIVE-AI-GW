<template>
  <div class="flex items-baseline justify-between gap-4 py-0.5">
    <span class="text-[var(--muted-foreground)]">{{ label }}</span>
    <span class="text-right font-mono tabular-nums text-[var(--foreground)]">{{ display }}</span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { formatScaled } from '@/utils/pricing'

const props = withDefaults(
  defineProps<{
    label: string
    value: number | null
    unit: string
    scale: number
  }>(),
  { value: null }
)

const display = computed(() =>
  props.value == null ? '-' : `${formatScaled(props.value, props.scale)} ${props.unit}`
)
</script>
