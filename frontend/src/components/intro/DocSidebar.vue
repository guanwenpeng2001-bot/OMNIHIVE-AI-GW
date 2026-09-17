<template>
  <aside class="self-start lg:sticky lg:top-24">
    <button type="button" class="btn btn-secondary mb-3 w-full justify-between lg:hidden" :aria-expanded="expanded" aria-controls="guide-contents" @click="expanded = !expanded">
      {{ t('helpGuide.contents') }} <span aria-hidden="true">{{ expanded ? '−' : '+' }}</span>
    </button>
    <nav id="guide-contents" :aria-label="t('helpGuide.contents')" class="card p-4 lg:block" :class="{ hidden: !expanded }">
      <p class="text-secondary mb-3 hidden text-xs font-semibold uppercase tracking-widest lg:block">{{ t('helpGuide.contents') }}</p>
      <a v-for="section in sections" :key="section.id" :href="`#${section.id}`" :aria-current="active === section.id ? 'location' : undefined" class="block rounded-lg px-3 py-2.5 text-sm transition-colors" :class="active === section.id ? 'bg-primary-50 font-semibold text-action dark:text-primary-300 dark:bg-primary-900/30' : 'text-secondary hover:bg-gray-100 dark:hover:bg-dark-700'" @click="expanded = false">{{ section.title }}</a>
    </nav>
  </aside>
</template>
<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
defineProps<{ sections: { id: string; title: string }[]; active: string }>()
const { t } = useI18n()
const expanded = ref(false)
</script>
