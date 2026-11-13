<template>
  <span :class="['oh-status-badge', badgeToneClass]">
    <span
      :class="[
        'pulse-dot',
        pulseToneClass,
        variantClass
      ]"
      aria-hidden="true"
    ></span>
    <span class="oh-status-label">
      {{ label }}
    </span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  status: string
  label: string
}>()

const variantClass = computed(() => {
  switch (props.status) {
    case 'active':
    case 'success':
      return 'bg-green-500'
    case 'disabled':
    case 'inactive':
    case 'warning':
      return 'bg-yellow-500'
    case 'error':
    case 'danger':
      return 'bg-red-500'
    default:
      return 'bg-gray-400'
  }
})

const pulseToneClass = computed(() => {
  switch (props.status) {
    case 'active':
    case 'success':
      return 'pulse-ok'
    case 'disabled':
    case 'inactive':
    case 'warning':
      return 'pulse-warn'
    case 'error':
    case 'danger':
      return 'pulse-bad'
    default:
      return 'pulse-neutral'
  }
})

const badgeToneClass = computed(() => {
  switch (props.status) {
    case 'active':
    case 'success':
      return 'oh-status-ok'
    case 'disabled':
    case 'inactive':
    case 'warning':
      return 'oh-status-warn'
    case 'error':
    case 'danger':
      return 'oh-status-bad'
    default:
      return 'oh-status-neutral'
  }
})
</script>

<style scoped>
.oh-status-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 2.5px 9px;
  border-radius: 9999px;
  font-size: 12px;
  font-weight: 500;
  line-height: 1.35;
  border: 1px solid transparent;
}

.oh-status-ok {
  color: var(--ok-text, #15803d);
  background: color-mix(in srgb, var(--ok, #16a34a) 12%, transparent);
  border-color: color-mix(in srgb, var(--ok, #16a34a) 22%, transparent);
}

.oh-status-warn {
  color: var(--warn-text, #92400e);
  background: color-mix(in srgb, var(--warn, #d97706) 12%, transparent);
  border-color: color-mix(in srgb, var(--warn, #d97706) 22%, transparent);
}

.oh-status-bad {
  color: var(--bad-text, #b91c1c);
  background: color-mix(in srgb, var(--bad, #dc2626) 12%, transparent);
  border-color: color-mix(in srgb, var(--bad, #dc2626) 22%, transparent);
}

.oh-status-neutral {
  color: var(--muted-foreground, #52525b);
  background: var(--surface-2, #f4f4f5);
  border-color: var(--border, #e4e4e7);
}

.dark .oh-status-ok {
  color: var(--ok-text, #4ade80);
}

.dark .oh-status-warn {
  color: var(--warn-text, #fbbf24);
}

.dark .oh-status-bad {
  color: var(--bad-text, #f87171);
}

.dark .oh-status-neutral {
  color: var(--muted-foreground, #a1a1aa);
  background: var(--surface-2, #1d1d21);
  border-color: var(--border, #27272a);
}

.pulse-dot {
  width: 7.5px;
  height: 7.5px;
  border-radius: 9999px;
  display: inline-block;
  flex: none;
}

.pulse-ok {
  background: var(--ok, #16a34a);
  color: var(--ok, #16a34a);
  animation: oh-badge-breath 2s cubic-bezier(0.4, 0, 0.6, 1) infinite;
}

.pulse-warn {
  background: var(--warn, #d97706);
  color: var(--warn, #d97706);
  animation: oh-badge-breath 1.7s cubic-bezier(0.4, 0, 0.6, 1) infinite;
}

.pulse-bad {
  background: var(--bad, #dc2626);
  color: var(--bad, #dc2626);
  animation: oh-badge-breath 1.2s cubic-bezier(0.4, 0, 0.6, 1) infinite;
}

.pulse-neutral {
  background: var(--muted-foreground, #9ca3af);
}

@keyframes oh-badge-breath {
  0% {
    box-shadow: 0 0 0 0 color-mix(in srgb, currentColor 70%, transparent);
    opacity: 0.9;
  }
  60% {
    box-shadow: 0 0 0 6px color-mix(in srgb, currentColor 0%, transparent);
    opacity: 1;
  }
  100% {
    box-shadow: 0 0 0 0 color-mix(in srgb, currentColor 0%, transparent);
    opacity: 0.9;
  }
}
</style>
