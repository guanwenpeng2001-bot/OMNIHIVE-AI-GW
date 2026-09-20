<script setup lang="ts" generic="Preset extends { url: string; label: string }">
defineProps<{ presets: Preset[]; currentUrl?: string; testId?: string }>()
const emit = defineEmits<{ select: [preset: Preset] }>()
</script>

<template>
  <div class="flex min-w-0 flex-wrap gap-2">
    <button v-for="preset in presets" :key="preset.label + preset.url" type="button"
      :data-testid="testId" :aria-pressed="preset.url === currentUrl"
      :class="['max-w-full break-all rounded-lg px-3 py-2 text-left text-xs transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500',
        preset.url === currentUrl ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300' : 'bg-gray-100 text-gray-700 hover:bg-primary-50 dark:bg-dark-600 dark:text-gray-300 dark:hover:bg-primary-900/30']"
      @click="emit('select', preset)">
      {{ preset.label }} ({{ preset.url.replace(/^https?:\/\//i, '') }})
    </button>
  </div>
</template>
