<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { resolveVisibleTierNavItems } from '@/router/navigationRegistry'

const { t, locale } = useI18n()
const app = useAppStore()
const auth = useAuthStore()
const router = useRouter()
const links = computed(() => [
  ...(router?.hasRoute?.('ProjectBilling') ? [{ path: '/billing', label: t('wp6.billing.title') }] : []),
  ...resolveVisibleTierNavItems('user', {
    isAuthenticated: auth.isAuthenticated,
    isAdmin: auth.isAdmin,
    isSimpleMode: auth.isSimpleMode,
    hasPendingAuthSession: false,
    backendModeEnabled: app.backendModeEnabled,
    publicSettings: app.cachedPublicSettings,
    publicSettingsLoaded: app.publicSettingsLoaded,
  }).filter(item => item.section === 'billing' && item.path !== '/billing').map(item => ({
    path: item.path,
    label: item.labelKey ? t(item.labelKey) : locale.value.startsWith('zh') ? item.fallbackLabelZh : item.fallbackLabelEn,
  })),
])
</script>

<template>
  <nav v-if="links.length" :aria-label="t('wp6.billing.navigation')" class="mb-4 flex w-full min-w-0 flex-wrap gap-1.5 border-b border-oh-subtle pb-2 text-sm">
    <RouterLink v-for="link in links" :key="link.path" :to="link.path" class="oh-focus-ring rounded-oh-sm px-2 py-1.5 font-medium text-oh-secondary hover:bg-oh-hover hover:text-oh-primary" exact-active-class="bg-oh-selected text-oh-accent-text">
      {{ link.label }}
    </RouterLink>
  </nav>
</template>
