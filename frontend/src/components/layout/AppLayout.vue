<template>
  <!-- Deduplicate accidental nested AppLayout wrappers -->
  <slot v-if="isNestedShell" />

  <!-- Tier 1: Public Architectural Shell -->
  <PublicShell
    v-else-if="resolvedTier === 'public'"
    :padded="!resolvedFullBleed"
  >
    <template v-if="$slots.subbar" #subbar>
      <slot name="subbar" />
    </template>
    <slot />
    <template v-if="$slots.footer" #footer>
      <slot name="footer" />
    </template>
  </PublicShell>

  <!-- Tier 2 & 3: User Console & Admin Console Shell -->
  <div
    v-else
    :data-shell-tier="resolvedTier"
    class="app-shell min-h-screen bg-oh-canvas text-oh-primary"
  >
    <AppHeader />

    <AppSidebar />

    <div
      class="relative z-10 flex min-h-[calc(100vh-3.5rem)] min-w-0 flex-col transition-[margin] duration-150 motion-reduce:transition-none"
      :class="[sidebarCollapsed ? 'lg:ml-[64px]' : 'lg:ml-[240px]']"
    >
      <!-- Admin Telemetry Strip (1px structural border, tabular mono, zero fake zeros) -->
      <div
        v-if="resolvedTier === 'admin'"
        data-testid="admin-telemetry-strip"
        class="border-b border-oh-default bg-oh-subtle px-3 py-1.5 sm:px-4 lg:px-6"
      >
        <div class="flex w-full flex-wrap items-center justify-between gap-2 text-xs">
          <div class="flex min-w-0 flex-wrap items-center gap-2.5">
            <span
              :class="[
                'inline-block h-2 w-2 shrink-0 rounded-full',
                pulseDegraded ? 'bg-[var(--oh-warn-fg)]' : 'bg-[var(--oh-ok-fg)]'
              ]"
            />
            <span class="oh-eyebrow text-oh-primary">GATEWAY TELEMETRY</span>

            <div
              v-if="pulseMetrics"
              class="hidden flex-wrap items-center gap-3 md:flex"
            >
              <span class="oh-tabular text-[11.5px] text-oh-secondary">
                <span class="text-oh-muted">REQ</span>
                {{ ' ' }}
                <strong class="text-oh-primary">{{ formatCompact(pulseMetrics.today_requests) }}</strong>
              </span>
              <span class="oh-tabular text-[11.5px] text-oh-secondary">
                <span class="text-oh-muted">RPM</span>
                {{ ' ' }}
                <strong class="text-oh-primary">
                  {{ pulseMetrics.rpm != null ? Math.round(pulseMetrics.rpm) : '—' }}
                </strong>
                <span class="mx-1 text-oh-muted">·</span>
                <span class="text-oh-muted">TPM</span>
                {{ ' ' }}
                <span>{{ formatCompact(pulseMetrics.tpm) }}</span>
              </span>
              <span class="oh-tabular text-[11.5px] text-oh-secondary">
                <span class="text-oh-muted">P95</span>
                {{ ' ' }}
                <strong
                  :class="[
                    pulseMetrics.p95_latency_ms != null && pulseMetrics.p95_latency_ms > 2500
                      ? 'text-[var(--oh-warn-fg)]'
                      : 'text-oh-primary'
                  ]"
                >
                  {{
                    pulseMetrics.p95_latency_ms != null
                      ? `${Math.round(pulseMetrics.p95_latency_ms)}ms`
                      : '—'
                  }}
                </strong>
              </span>
              <span class="oh-tabular text-[11.5px] text-oh-secondary">
                <span class="text-oh-muted">ERR</span>
                {{ ' ' }}
                <strong
                  :class="[
                    pulseMetrics.error_rate_percent != null && pulseMetrics.error_rate_percent > 2
                      ? 'text-[var(--oh-danger-fg)]'
                      : 'text-oh-primary'
                  ]"
                >
                  {{
                    pulseMetrics.error_rate_percent != null
                      ? `${pulseMetrics.error_rate_percent.toFixed(2)}%`
                      : '—'
                  }}
                </strong>
              </span>
            </div>
          </div>

          <div class="flex flex-wrap items-center gap-1.5">
            <template v-if="!pulseCollapsed">
              <button
                v-for="item in pulseLinks"
                :key="item.path"
                type="button"
                :class="[
                  'oh-focus-ring inline-flex items-center gap-1 rounded-oh-sm border px-2 py-0.5 font-mono text-[11px] transition-colors',
                  currentPath.startsWith(item.path)
                    ? 'border-oh-accent bg-oh-selected font-semibold text-oh-accent-text'
                    : 'border-oh-default bg-oh-surface text-oh-secondary hover:border-oh-strong hover:text-oh-primary'
                ]"
                @click="navigatePulse(item.path)"
              >
                <span>{{ isZh ? item.labelZh : item.labelEn }}</span>
              </button>
            </template>

            <button
              type="button"
              :aria-expanded="!pulseCollapsed"
              class="oh-focus-ring rounded-oh-sm px-1.5 py-0.5 font-mono text-[11px] text-oh-muted hover:text-oh-primary"
              @click="pulseCollapsed = !pulseCollapsed"
            >
              {{ pulseCollapsed ? '▾' : '▴' }}
            </button>
          </div>
        </div>
      </div>

      <!-- Optional Sub-header / Banner Slot -->
      <div
        v-if="$slots.subbar"
        class="border-b border-oh-default bg-oh-subtle px-4 py-2.5 sm:px-6 lg:px-7"
      >
        <slot name="subbar" />
      </div>

      <!-- Main Console Canvas -->
      <main
        :class="[
          'flex w-full flex-1 flex-col',
          resolvedFullBleed ? 'p-0' : 'p-4 pb-12 sm:p-5 sm:pb-14 lg:p-6 lg:pb-16'
        ]"
      >
        <div
          :class="[
            'w-full flex-1',
            resolvedContained ? 'mx-auto max-w-[1600px]' : ''
          ]"
        >
          <div
            v-if="route.params?.workspace_id"
            class="mb-4 flex items-center gap-2 rounded-oh-md border border-oh-default bg-oh-surface px-3 py-2 text-xs text-oh-secondary"
          >
            <RouterLink to="/workspaces" class="font-medium text-oh-accent-text hover:underline">
              {{ t('workspaces.title') }}
            </RouterLink>
            <span class="text-oh-muted">/</span>
            <span class="font-mono text-oh-primary">#{{ route.params.workspace_id }}</span>
          </div>

          <div v-if="$slots.header" class="mb-5">
            <slot name="header" />
          </div>

          <div
            v-if="$slots.aside"
            class="grid grid-cols-1 gap-6 xl:grid-cols-[minmax(0,1fr)_320px]"
          >
            <div class="min-w-0">
              <slot />
            </div>
            <aside class="min-w-0">
              <slot name="aside" />
            </aside>
          </div>
          <slot v-else />
        </div>
      </main>

      <footer
        v-if="$slots.footer"
        class="border-t border-oh-default bg-oh-subtle px-4 py-3 text-xs text-oh-muted sm:px-6 lg:px-7"
      >
        <slot name="footer" />
      </footer>
    </div>
  </div>
</template>

<script setup lang="ts">
import '@/styles/onboarding.css'
import {
  computed,
  getCurrentInstance,
  inject,
  onMounted,
  onUnmounted,
  provide,
  ref,
  watch,
  type InjectionKey
} from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { StorageKeys } from '@/api/tokenRefresh'
import { useOnboardingTour } from '@/composables/useOnboardingTour'
import { useOnboardingStore } from '@/stores/onboarding'
import type { AdminPulseDTO } from '@/api/mcp'
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'
import PublicShell from './PublicShell.vue'

const SHELL_MOUNTED_KEY: InjectionKey<boolean> = Symbol('omnihive-shell-mounted')

export interface AppLayoutProps {
  tier?: 'auto' | 'public' | 'user' | 'admin'
  contained?: boolean
  fullBleed?: boolean
}

const props = withDefaults(defineProps<AppLayoutProps>(), {
  tier: 'auto',
  contained: false,
  fullBleed: false
})

const isNestedShell = inject(SHELL_MOUNTED_KEY, false)
if (!isNestedShell) {
  provide(SHELL_MOUNTED_KEY, true)
}

const appStore = useAppStore()
const { t, locale } = useI18n()
const authStore = useAuthStore()
const onboardingStore = useOnboardingStore()
const route = useRoute()
const instance = getCurrentInstance()

const isZh = computed(() => String(locale?.value || 'zh').toLowerCase().startsWith('zh'))
const sidebarCollapsed = computed(() => Boolean(appStore.sidebarCollapsed))
const isAdmin = computed(() => authStore.user?.role === 'admin')
const currentPath = computed(() => route.path || route.fullPath || '')

const resolvedTier = computed<'public' | 'user' | 'admin'>(() => {
  if (props.tier !== 'auto') return props.tier
  if (route.meta?.shellTier === 'public') return 'public'
  if (route.meta?.shellTier === 'admin' || currentPath.value.startsWith('/admin')) {
    return 'admin'
  }
  return 'user'
})

const resolvedContained = computed(() => props.contained || Boolean(route.meta?.contained))
const resolvedFullBleed = computed(() => props.fullBleed || Boolean(route.meta?.fullBleed))

const { replayTour, cancelTour } = useOnboardingTour({
  storageKey: isAdmin.value
    ? StorageKeys.ONBOARDING_ADMIN_GUIDE
    : StorageKeys.ONBOARDING_USER_GUIDE,
  autoStart: true
})

const pulseCollapsed = ref(false)
const pulseMetrics = ref<AdminPulseDTO | null>(null)
let pulseTimer: ReturnType<typeof setInterval> | null = null

const pulseDegraded = computed(() => {
  if (!pulseMetrics.value) return false
  return (
    pulseMetrics.value.status === 'degraded' ||
    (pulseMetrics.value.error_rate_percent ?? 0) > 5
  )
})

function formatCompact(num: number | null | undefined): string {
  if (num === null || num === undefined || !Number.isFinite(num)) return '—'
  if (num >= 1_000_000_000) return `${(num / 1_000_000_000).toFixed(1)}B`
  if (num >= 1_000_000) return `${(num / 1_000_000).toFixed(1)}M`
  if (num >= 1_000) return `${(num / 1_000).toFixed(1)}K`
  return String(Math.round(num))
}

async function refreshPulse(): Promise<void> {
  if (isNestedShell || resolvedTier.value !== 'admin' || !isAdmin.value) return
  try {
    const { mcpAPI } = await import('@/api/mcp')
    const data = await mcpAPI.getAdminPulse()
    if (data) pulseMetrics.value = data
  } catch {
    // Isolated unit tests may not mount full HTTP runtime.
  }
}

const pulseLinks = [
  { labelZh: '实时监控', labelEn: 'Ops', path: '/admin/ops' },
  { labelZh: '健康探测', labelEn: 'Probes', path: '/admin/channels/monitor' },
  { labelZh: '上游连接', labelEn: 'Connections', path: '/admin/accounts' },
  { labelZh: '全局账本', labelEn: 'Ledger', path: '/admin/usage' },
  { labelZh: '系统设置', labelEn: 'Settings', path: '/admin/settings' }
]

function navigatePulse(path: string): void {
  const globalRouter = instance?.appContext.config.globalProperties.$router
  if (globalRouter && typeof globalRouter.push === 'function') {
    void globalRouter.push(path)
  }
}

watch(() => route.fullPath, cancelTour, { flush: 'sync' })
watch(sidebarCollapsed, cancelTour, { flush: 'sync' })
watch(
  () => resolvedTier.value,
  (tier) => {
    if (tier === 'admin') void refreshPulse()
  },
  { immediate: true }
)

onMounted(() => {
  if (isNestedShell) return
  onboardingStore.setReplayCallback(replayTour)
  if (resolvedTier.value === 'admin') void refreshPulse()
  pulseTimer = setInterval(() => {
    if (
      resolvedTier.value === 'admin' &&
      typeof document !== 'undefined' &&
      document.visibilityState === 'visible'
    ) {
      void refreshPulse()
    }
  }, 30000)
})

onUnmounted(() => {
  if (isNestedShell) return
  onboardingStore.setReplayCallback(null)
  if (pulseTimer) {
    clearInterval(pulseTimer)
    pulseTimer = null
  }
})

defineExpose({ replayTour })
</script>
