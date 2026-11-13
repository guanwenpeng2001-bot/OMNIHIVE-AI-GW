<script setup lang="ts">
import { computed } from 'vue'
import { useAppStore } from '@/stores/app'

const appStore = useAppStore()
const siteICP = computed(() => appStore.cachedPublicSettings?.site_icp?.trim() || '')
</script>

<template>
  <footer
    v-if="siteICP"
    class="relative border-t px-4 py-3.5 text-center font-mono text-[11px] transition-colors"
    style="background: var(--background); border-color: var(--border); color: var(--muted-foreground)"
  >
    <a
      href="https://beian.miit.gov.cn"
      target="_blank"
      rel="noopener"
      class="transition-colors hover:text-[var(--foreground)] hover:underline"
    >
      {{ siteICP }}
    </a>
  </footer>
</template>
