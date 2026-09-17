<template>
  <section :id="id" class="scroll-mt-28 space-y-5 border-t pt-10" :aria-labelledby="`${id}-title`">
    <h2 :id="`${id}-title`" class="text-2xl font-semibold tracking-tight">{{ title }}</h2>
    <slot />
  </section>
</template>
<script setup lang="ts">
defineProps<{ id: string; title: string }>()
</script>
