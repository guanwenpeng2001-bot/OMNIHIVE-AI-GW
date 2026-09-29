<template>
  <div class="min-h-screen bg-gray-50 dark:bg-dark-950">
    <!-- Background Decoration -->
    <div class="pointer-events-none fixed inset-0 bg-mesh-gradient"></div>

    <!-- Sidebar -->
    <AppSidebar />

    <!-- Main Content Area -->
    <div
      class="relative min-h-screen transition-all duration-300"
      :class="[sidebarCollapsed ? 'lg:ml-[72px]' : 'lg:ml-64']"
    >
      <!-- Header -->
      <AppHeader />

      <!-- Main Content -->
      <main class="p-4 md:p-6 lg:p-8">
        <div v-if="route.params?.workspace_id" class="card mb-4 flex items-center gap-3 p-3 text-sm">
          <RouterLink to="/workspaces" class="text-primary-600 dark:text-primary-400">{{ t('workspaces.title') }}</RouterLink>
          <span>#{{ route.params.workspace_id }}</span>
        </div>
        <slot />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import '@/styles/onboarding.css'
import { computed, onMounted, onUnmounted, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useOnboardingTour } from '@/composables/useOnboardingTour'
import { useOnboardingStore } from '@/stores/onboarding'
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'

const appStore = useAppStore()
const { t } = useI18n()
const authStore = useAuthStore()
const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const isAdmin = computed(() => authStore.user?.role === 'admin')

const { replayTour, cancelTour } = useOnboardingTour({
  storageKey: isAdmin.value ? 'admin_guide' : 'user_guide',
  autoStart: true
})

const onboardingStore = useOnboardingStore()
const route = useRoute()

watch(() => route.fullPath, cancelTour, { flush: 'sync' })
watch(sidebarCollapsed, cancelTour, { flush: 'sync' })

onMounted(() => {
  onboardingStore.setReplayCallback(replayTour)
})

onUnmounted(() => {
  onboardingStore.setReplayCallback(null)
})

defineExpose({ replayTour })
</script>
