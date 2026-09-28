<script setup lang="ts">
import { computed } from 'vue'
import { useAppStore } from '@/stores/app'

const appStore = useAppStore()
const siteICP = computed(() => appStore.cachedPublicSettings?.site_icp?.trim() || '')
</script>

<template>
  <footer v-if="siteICP" class="relative bg-gray-50 px-4 py-4 text-center text-xs text-gray-500 dark:bg-dark-950 dark:text-gray-400">
    <a href="https://beian.miit.gov.cn" target="_blank" rel="noopener" class="hover:underline">
      {{ siteICP }}
    </a>
  </footer>
</template>
