<script setup lang="ts">
defineProps<{
  colorClass: string
  tooltip?: string
  current: string | number
  max: string | number
  suffix?: string
}>()
</script>

<template>
  <span
    :class="[
      'inline-flex min-w-max items-center gap-1 rounded-md border border-current/15 px-1.5 py-0.5 text-[10px] font-medium leading-tight tabular-nums',
      colorClass
    ]"
    :title="tooltip"
  >
    <slot />
    <span class="font-mono">{{ current }}</span>
    <span class="text-gray-400 dark:text-gray-500">/</span>
    <span class="font-mono">{{ max }}</span>
    <span v-if="suffix" class="text-[9px] opacity-60">{{ suffix }}</span>
  </span>
</template>
