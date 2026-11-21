<template>
  <ChannelProbeStatusView v-if="isV1" class="channel-status-surface" />
  <ChannelTelemetryStatusView v-else class="channel-status-surface" />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { isChannelProbeMode } from '@/utils/featureFlags'
import ChannelProbeStatusView from './ChannelProbeStatusView.vue'
import ChannelTelemetryStatusView from './ChannelTelemetryStatusView.vue'

const isV1 = computed(() => isChannelProbeMode())
</script>

<style scoped>
.channel-status-surface { color: var(--foreground); font-variant-numeric: tabular-nums; }
</style>
