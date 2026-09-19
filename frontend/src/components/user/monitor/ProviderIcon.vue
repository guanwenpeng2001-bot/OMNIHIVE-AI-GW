<template>
  <PlatformIcon
    v-if="knownPlatform"
    :platform="knownPlatform"
    :style="{ width: `${size}px`, height: `${size}px` }"
  />
  <span
    v-else
    aria-hidden="true"
    class="inline-flex items-center justify-center font-bold text-gray-500"
    :style="{ width: `${size}px`, height: `${size}px`, fontSize: `${Math.round(size * 0.5)}px` }"
  >
    {{ fallbackText }}
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { GROUP_PLATFORM_OPTIONS } from '@/constants/platforms'
import type { Provider } from '@/api/admin/channelMonitor'

const props = withDefaults(defineProps<{
  provider: Provider | string
  size?: number
}>(), {
  size: 20,
})

const knownPlatform = computed(() =>
  GROUP_PLATFORM_OPTIONS.find(option => option.value === props.provider)?.value
)
const fallbackText = computed(() => (props.provider || '?').charAt(0).toUpperCase())
</script>
