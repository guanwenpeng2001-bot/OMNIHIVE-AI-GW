<template>
  <aside class="self-start lg:sticky lg:top-24">
    <button type="button" class="btn btn-secondary mb-3 w-full justify-between lg:hidden" :aria-expanded="expanded" aria-controls="guide-contents" @click="expanded = !expanded">
      {{ t('helpGuide.contents') }} <span aria-hidden="true">{{ expanded ? '−' : '+' }}</span>
    </button>
    <nav id="guide-contents" :aria-label="t('helpGuide.contents')" class="doc-rail p-3 lg:block lg:max-h-[calc(100vh-8rem)] lg:overflow-y-auto" :class="{ hidden: !expanded }">
      <p class="text-secondary mb-3 hidden text-xs font-semibold uppercase tracking-widest lg:block">{{ t('helpGuide.contents') }}</p>
      <details v-for="(group, index) in helpGroups" :key="group.id" :open="group.sections.some(id => id === active)" class="mb-2">
        <summary class="cursor-pointer px-2 py-3 text-sm font-semibold"><span class="mr-2 font-mono text-xs text-[var(--muted-foreground)]" aria-hidden="true">[{{ String(index + 1).padStart(2, '0') }}]</span><h3 class="inline">{{ t(`helpGuide.groups.${group.id}`) }}</h3></summary>
        <a v-for="section in sections.filter(item => group.sections.some(id => id === item.id))" :key="section.id" :href="`#${section.id}`" :aria-current="active === section.id ? 'location' : undefined" class="doc-rail-link flex gap-2 px-3 py-2.5 text-sm transition-colors" @click="expanded = false"><span aria-hidden="true" class="shrink-0 whitespace-nowrap font-mono">{{ active === section.id ? '[-]' : '[ ]' }}</span>{{ section.title }}</a>
      </details>
    </nav>
  </aside>
</template>
<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { helpGroups } from './helpSections'
defineProps<{ sections: { id: string; title: string }[]; active: string }>()
const { t } = useI18n()
const expanded = ref(false)
</script>
<style scoped>
.doc-rail { border-left: 1px solid var(--border); background: var(--background); color: var(--foreground); }
summary { list-style: none; }
summary::-webkit-details-marker { display: none; }
.doc-rail-link { margin-left: -13px; padding-left: 20px; border-left: 2px solid transparent; color: var(--muted-foreground); }
.doc-rail-link:hover { background: var(--surface-2); color: var(--foreground); }
.doc-rail-link[aria-current] { border-left-color: var(--brand); color: var(--brand-text); background: color-mix(in srgb, var(--brand) 8%, var(--background)); font-weight: 600; }
:is(a, summary):focus-visible { outline: 2px solid var(--brand); outline-offset: 2px; }
</style>
