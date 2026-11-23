<script setup lang="ts">
import Badge from '@/components/ui/Badge.vue'
import type { AvailabilityBadgeVM } from '@/features/catalog/viewModel'

withDefaults(
  defineProps<{
    badge: AvailabilityBadgeVM | null
    size?: 'xs' | 'sm' | 'md'
    testIdPrefix?: string
  }>(),
  {
    size: 'xs',
    testIdPrefix: 'availability-badge'
  }
)
</script>

<template>
  <Badge
    v-if="badge"
    :data-testid="`${testIdPrefix}-${badge.state}`"
    :tone="badge.tone"
    :size="size"
    variant="subtle"
    dot
  >
    {{ badge.label }}
  </Badge>
</template>
