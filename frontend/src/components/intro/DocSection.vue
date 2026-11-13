<template>
  <section :id="id" class="doc-section scroll-mt-28 space-y-5 border-t pt-10" :aria-labelledby="`${id}-title`">
    <h2 :id="`${id}-title`" class="flex items-baseline gap-3 text-2xl font-semibold tracking-tight"><span aria-hidden="true" class="font-mono text-base text-[var(--brand-text)]">#</span>{{ title }}</h2>
    <slot />
  </section>
</template>
<script setup lang="ts">
defineProps<{ id: string; title: string }>()
</script>
<style scoped>
.doc-section { border-color: var(--border); color: var(--foreground); }
.doc-section :deep(p) { max-width: 80ch; }
.doc-section :deep(table) { border-collapse: collapse; background: var(--card); }
.doc-section :deep(:is(th, td, tr)) { border-color: var(--border); }
</style>
