<script setup lang="ts">
import { computed } from 'vue'
import Badge from '@/components/ui/Badge.vue'
import type { OriginBadgeVM } from '@/features/catalog/viewModel'

const props = withDefaults(
  defineProps<{
    badges?: OriginBadgeVM[]
    badge?: OriginBadgeVM | null
    showAll?: boolean
    size?: 'xs' | 'sm'
  }>(),
  {
    badges: () => [],
    badge: null,
    showAll: false,
    size: 'xs'
  }
)

const visibleBadges = computed<OriginBadgeVM[]>(() => {
  if (props.showAll && props.badges.length > 0) {
    return props.badges
  }
  if (props.badge) {
    return [props.badge]
  }
  if (props.badges.length > 0) {
    return [props.badges[0]]
  }
  return []
})
</script>

<template>
  <template v-for="entry in visibleBadges" :key="entry.kind">
    <Badge
      :data-testid="`origin-badge-${entry.kind}`"
      :tone="entry.tone"
      :size="size"
      variant="outline"
    >
      {{ entry.label }}
    </Badge>
  </template>
</template>
