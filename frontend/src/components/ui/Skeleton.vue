<script setup lang="ts">
import { cn } from './utils'

export interface SkeletonProps {
  variant?: 'line' | 'rect' | 'circle' | 'stat' | 'table-row'
  lines?: number
  columns?: number
  label?: string
}

withDefaults(defineProps<SkeletonProps>(), {
  variant: 'line',
  lines: 1,
  columns: 4,
  label: 'Loading content'
})
</script>

<template>
  <div role="status" :aria-label="label" aria-busy="true">
    <div v-if="variant === 'stat'" class="space-y-2">
      <div class="oh-skeleton-pulse h-3 w-24 rounded-oh-xs bg-oh-inset" />
      <div class="oh-skeleton-pulse h-7 w-32 rounded-oh-sm bg-oh-inset" />
      <div class="oh-skeleton-pulse h-3 w-20 rounded-oh-xs bg-oh-inset" />
    </div>

    <div v-else-if="variant === 'table-row'" class="flex items-center gap-4 py-3">
      <div
        v-for="colIndex in columns"
        :key="colIndex"
        :class="
          cn(
            'oh-skeleton-pulse h-4 rounded-oh-xs bg-oh-inset',
            colIndex === 1 ? 'w-2/5' : 'flex-1'
          )
        "
      />
    </div>

    <div
      v-else-if="variant === 'circle'"
      class="oh-skeleton-pulse h-9 w-9 rounded-full bg-oh-inset"
    />

    <div
      v-else-if="variant === 'rect'"
      class="oh-skeleton-pulse h-24 w-full rounded-oh-md bg-oh-inset"
    />

    <div v-else class="space-y-2">
      <div
        v-for="lineIndex in lines"
        :key="lineIndex"
        :class="
          cn(
            'oh-skeleton-pulse h-3.5 rounded-oh-xs bg-oh-inset',
            lineIndex === lines && lines > 1 ? 'w-3/4' : 'w-full'
          )
        "
      />
    </div>

    <span class="sr-only">{{ label }}</span>
  </div>
</template>
