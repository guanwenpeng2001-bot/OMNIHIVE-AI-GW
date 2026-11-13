<script setup lang="ts" generic="Preset extends { url: string; label: string }">
defineProps<{ presets: Preset[]; currentUrl?: string; testId?: string }>()
const emit = defineEmits<{ select: [preset: Preset] }>()
</script>

<template>
  <div class="flex min-w-0 flex-wrap gap-2">
    <button v-for="preset in presets" :key="preset.label + preset.url" type="button"
      :data-testid="testId" :aria-pressed="preset.url === currentUrl"
      :class="['max-w-full break-all rounded-md border px-2.5 py-1.5 text-left font-mono text-[11px] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)]',
        preset.url === currentUrl ? 'border-[var(--brand)] bg-[var(--accent)] text-[var(--brand-text)]' : 'border-[var(--border)] bg-[var(--card)] text-[var(--muted-foreground)] hover:border-[var(--brand)] hover:text-[var(--foreground)]']"
      @click="emit('select', preset)">
      {{ preset.label }} ({{ preset.url.replace(/^https?:\/\//i, '') }})
    </button>
  </div>
</template>
