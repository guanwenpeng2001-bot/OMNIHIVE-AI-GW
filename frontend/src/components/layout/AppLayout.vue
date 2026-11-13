<template>
  <div class="app-shell min-h-screen" style="background-color: var(--background); color: var(--foreground)">
    <!-- Subtle OmniHive Ambient Glow & Honeycomb Watermark -->
    <div class="pointer-events-none fixed inset-0 overflow-hidden z-0">
      <div
        class="absolute inset-0 opacity-40 dark:opacity-25"
        style="
          background-image: radial-gradient(
            720px circle at 78% -10%,
            color-mix(in srgb, var(--brand, #f59e0b) 8%, transparent),
            transparent 70%
          );
        "
      ></div>
    </div>

    <AppHeader />

    <AppSidebar />

    <!-- Main Content Area -->
    <div
      class="relative z-10 min-h-[calc(100vh-3.5rem)] min-w-0 transition-[margin] duration-300 motion-reduce:transition-none"
      :class="[sidebarCollapsed ? 'lg:ml-[72px]' : 'lg:ml-[248px]']"
    >
      <!-- Admin Operations Pulse Strip (常驻蜂巢运行脉搏条) -->
      <div
        v-if="isAdminRoute"
        class="border-b px-4 py-1.5 md:px-6 lg:px-8 transition-colors"
        style="
          border-color: var(--border);
          background: linear-gradient(
            90deg,
            color-mix(in srgb, var(--brand, #f59e0b) 5%, var(--card, #ffffff)),
            var(--card, #ffffff) 65%
          );
        "
      >
        <div class="flex w-full flex-wrap items-center justify-between gap-2 text-xs">
          <div class="flex flex-wrap items-center gap-2.5 min-w-0">
            <span class="pulse-dot" :class="pulseStatusClass"></span>
            <span class="font-semibold tracking-tight" style="color: var(--foreground)">
              蜂巢运行脉搏
            </span>
            <!-- Live Telemetry Strip (OpenRouter / OpenCode NOC style) -->
            <div
              v-if="pulseMetrics"
              class="hidden md:flex flex-wrap items-center gap-1.5 font-mono text-[11px] tabular-nums"
            >
              <span class="pill pill-ghost" title="今日累计调度请求">
                <span class="opacity-55">REQ</span>
                <strong>{{ formatCompact(pulseMetrics.today_requests) }}</strong>
              </span>
              <span class="pill pill-ghost" title="实时吞吐率">
                <span class="opacity-55">RATE</span>
                <strong>{{ Math.round(pulseMetrics.rpm || 0) }} RPM</strong>
                <span class="opacity-40">·</span>
                <span>{{ formatCompact(pulseMetrics.tpm || 0) }} TPM</span>
              </span>
              <span v-if="pulseMetrics.p95_latency_ms != null" class="pill pill-ghost" title="P95 响应延迟">
                <span class="opacity-55">P95</span>
                <strong :style="{ color: pulseMetrics.p95_latency_ms > 2500 ? 'var(--warn-text)' : 'var(--ok-text)' }">
                  {{ Math.round(pulseMetrics.p95_latency_ms) }}ms
                </strong>
              </span>
              <span v-if="pulseMetrics.error_rate_percent != null" class="pill pill-ghost" title="窗口错误率">
                <span class="opacity-55">ERR</span>
                <strong :style="{ color: pulseMetrics.error_rate_percent > 2 ? 'var(--bad-text)' : 'var(--foreground)' }">
                  {{ pulseMetrics.error_rate_percent.toFixed(2) }}%
                </strong>
              </span>
              <span v-if="(pulseMetrics.total_accounts ?? 0) > 0" class="pill pill-ghost" title="可调度上游账号池">
                <span class="opacity-55">POOL</span>
                <strong style="color: var(--brand-text)">
                  {{ pulseMetrics.active_accounts ?? 0 }}/{{ pulseMetrics.total_accounts }}
                </strong>
              </span>
            </div>
            <span v-else class="hidden sm:inline font-mono text-[11px] opacity-60 truncate">
              ENGINE ONLINE · REALTIME DISPATCH
            </span>
          </div>

          <div class="flex flex-wrap items-center gap-1.5">
            <template v-if="!pulseCollapsed">
              <button
                v-for="item in pulseLinks"
                :key="item.path"
                type="button"
                @click="navigatePulse(item.path)"
                class="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-[11.5px] font-medium transition-all hover:border-[var(--brand)] cursor-pointer"
                :style="
                  currentPath.startsWith(item.path)
                    ? 'border-color: var(--brand, #f59e0b); background: color-mix(in srgb, var(--brand, #f59e0b) 12%, var(--card)); color: var(--brand-text, #92610a); font-weight: 600;'
                    : 'border-color: var(--border); background: var(--card); color: var(--foreground);'
                "
              >
                <span>{{ item.label }}</span>
                <span class="opacity-50">→</span>
              </button>
            </template>

            <button
              type="button"
              @click="pulseCollapsed = !pulseCollapsed"
              class="inline-flex items-center gap-1 rounded-md px-2 py-1 text-[11px] opacity-65 transition-opacity hover:opacity-100 cursor-pointer"
              :aria-expanded="!pulseCollapsed ? 'true' : 'false'"
              :title="pulseCollapsed ? '展开运行脉搏快捷入口' : '收起运行脉搏快捷入口'"
            >
              <span>{{ pulseCollapsed ? '展开快捷台 ▾' : '收起 ▴' }}</span>
            </button>
          </div>
        </div>
      </div>

      <!-- Main Content -->
      <main class="w-full p-4 pb-14 md:p-6 md:pb-16 lg:p-7 lg:pb-16">
        <div class="w-full">
          <div v-if="route.params?.workspace_id" class="card mb-4 flex items-center gap-3 p-3 text-sm">
            <RouterLink to="/workspaces" class="text-primary-600 dark:text-primary-400">{{ t('workspaces.title') }}</RouterLink>
            <span>#{{ route.params.workspace_id }}</span>
          </div>
          <slot />
        </div>
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import '@/styles/onboarding.css'
import { computed, getCurrentInstance, onMounted, onUnmounted, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useOnboardingTour } from '@/composables/useOnboardingTour'
import { useOnboardingStore } from '@/stores/onboarding'
import type { AdminPulseDTO } from '@/api/mcp'
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
const instance = getCurrentInstance()

const pulseCollapsed = ref(false)
const pulseMetrics = ref<AdminPulseDTO | null>(null)
let pulseTimer: ReturnType<typeof setInterval> | null = null
const currentPath = computed(() => route.path || route.fullPath || '')
const isAdminRoute = computed(() => currentPath.value.startsWith('/admin'))
const pulseStatusClass = computed(() => {
  if (!pulseMetrics.value) return 'pulse-ok'
  if (pulseMetrics.value.status === 'degraded' || (pulseMetrics.value.error_rate_percent ?? 0) > 5) return 'pulse-warn'
  return 'pulse-ok'
})

function formatCompact(num: number): string {
  if (!Number.isFinite(num)) return '0'
  if (num >= 1_000_000) return `${(num / 1_000_000).toFixed(1)}M`
  if (num >= 1_000) return `${(num / 1_000).toFixed(1)}K`
  return String(Math.round(num))
}

async function refreshPulse() {
  if (!isAdminRoute.value || !isAdmin.value) return
  try {
    const { mcpAPI } = await import('@/api/mcp')
    const data = await mcpAPI.getAdminPulse()
    if (data) pulseMetrics.value = data
  } catch {
    // Isolated component unit tests may mock vue-i18n without full apiClient setup.
  }
}

const pulseLinks = [
  { label: '运维实时监控', path: '/admin/ops' },
  { label: '渠道监控', path: '/admin/channels/monitor' },
  { label: '上游账号池', path: '/admin/accounts' },
  { label: '用量记录', path: '/admin/usage' },
  { label: '系统设置', path: '/admin/settings' }
]

function navigatePulse(path: string) {
  const router = instance?.appContext.config.globalProperties.$router
  if (router && typeof router.push === 'function') {
    router.push(path)
  }
}

watch(() => route.fullPath, cancelTour, { flush: 'sync' })
watch(sidebarCollapsed, cancelTour, { flush: 'sync' })
watch(isAdminRoute, (active) => {
  if (active) void refreshPulse()
}, { immediate: true })

onMounted(() => {
  onboardingStore.setReplayCallback(replayTour)
  if (isAdminRoute.value) void refreshPulse()
  pulseTimer = setInterval(() => {
    if (isAdminRoute.value && typeof document !== 'undefined' && document.visibilityState === 'visible') {
      void refreshPulse()
    }
  }, 30000)
})

onUnmounted(() => {
  onboardingStore.setReplayCallback(null)
  if (pulseTimer) {
    clearInterval(pulseTimer)
    pulseTimer = null
  }
})

defineExpose({ replayTour })
</script>
