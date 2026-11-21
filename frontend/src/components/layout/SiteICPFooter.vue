<script setup lang="ts">
import { computed } from 'vue'
import { useAppStore } from '@/stores/app'

withDefaults(
  defineProps<{
    inline?: boolean
  }>(),
  {
    inline: false
  }
)

const appStore = useAppStore()
const siteICP = computed(() => appStore.cachedPublicSettings?.site_icp?.trim() || '')
</script>

<template>
  <a
    v-if="inline && siteICP"
    href="https://beian.miit.gov.cn"
    target="_blank"
    rel="noopener"
    class="font-mono text-[11px] text-oh-muted transition-colors hover:text-oh-primary hover:underline"
  >
    {{ siteICP }}
  </a>
  <footer
    v-else-if="!inline && siteICP"
    class="relative border-t border-oh-default bg-oh-canvas px-4 py-3.5 text-center font-mono text-[11px] text-oh-muted transition-colors"
  >
    <a
      href="https://beian.miit.gov.cn"
      target="_blank"
      rel="noopener"
      class="transition-colors hover:text-oh-primary hover:underline"
    >
      {{ siteICP }}
    </a>
  </footer>
</template>
