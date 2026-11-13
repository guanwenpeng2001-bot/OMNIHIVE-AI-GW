<template>
  <div class="mt-5 grid grid-cols-2 gap-2">
    <div
      class="min-w-0 rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-3"
    >
      <div
        class="flex items-center gap-1.5 text-xs font-medium text-[var(--muted-foreground)]"
      >
        <Icon :name="primaryIcon" size="xs" />
        <span>{{ primaryLabel }}</span>
      </div>
      <div class="mt-1.5 text-lg font-bold font-mono tabular-nums text-gray-900 dark:text-gray-100">
        {{ primaryValue }}<span class="text-xs font-normal text-gray-400 ml-0.5">{{ primaryUnit }}</span>
      </div>
    </div>
    <div
      class="min-w-0 rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-3"
    >
      <div
        class="flex items-center gap-1.5 text-xs font-medium text-[var(--muted-foreground)]"
      >
        <Icon :name="secondaryIcon" size="xs" />
        <span>{{ secondaryLabel }}</span>
      </div>
      <div class="mt-1.5 text-lg font-bold font-mono tabular-nums text-gray-900 dark:text-gray-100">
        {{ secondaryValue }}<span class="text-xs font-normal text-gray-400 ml-0.5">{{ secondaryUnit }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'

defineProps<{
  primaryLabel: string
  primaryValue: string
  primaryUnit: string
  primaryIcon: 'bolt' | 'globe' | 'clock' | 'link'
  secondaryLabel: string
  secondaryValue: string
  secondaryUnit: string
  secondaryIcon: 'bolt' | 'globe' | 'clock' | 'link'
}>()
</script>
