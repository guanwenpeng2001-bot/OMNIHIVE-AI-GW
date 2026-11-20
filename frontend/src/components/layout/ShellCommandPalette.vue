<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import CommandPalette from '@/components/ui/CommandPalette.vue'
import type { UiCommandGroup, UiCommandItem } from '@/components/ui/types'
import { useAppStore, useAuthStore } from '@/stores'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import { useThemeDirection } from '@/composables/useThemeDirection'
import { resolveVisibleTierNavItems, type ShellNavItemSpec } from '@/router/navigationRegistry'
import { getModelPlaza } from '@/api/modelPlaza'
import { isSilentRequestError } from '@/api/client'

const router = useRouter()
const route = useRoute()
const { t, locale } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const adminSettingsStore = useAdminSettingsStore()
const {
  cmdkOpen,
  themeMode,
  densityMode,
  setThemeMode,
  setDensityMode,
  closeCommandPalette: closeThemeCmdk
} = useThemeDirection()

const isZh = computed(() => String(locale?.value || 'zh').toLowerCase().startsWith('zh'))

const liveModelNames = ref<Array<{ name: string; platform: string }>>([])
const modelsLoaded = ref(false)
const modelsLoading = ref(false)
let modelAbortController: AbortController | null = null

const isOpen = computed({
  get: () => Boolean(appStore.commandPaletteOpen || cmdkOpen.value),
  set: (next: boolean) => {
    appStore.commandPaletteOpen = next
    if (!next) {
      closeThemeCmdk()
    } else {
      cmdkOpen.value = true
    }
  }
})

function resolveItemTitle(item: ShellNavItemSpec): string {
  if (item.labelKey) {
    const translated = t(item.labelKey)
    if (translated && translated !== item.labelKey) {
      return translated
    }
  }
  return isZh.value ? item.fallbackLabelZh : item.fallbackLabelEn
}

const policyContext = computed(() => ({
  isAuthenticated: authStore.isAuthenticated,
  isAdmin: authStore.isAdmin,
  isSimpleMode: authStore.isSimpleMode,
  hasPendingAuthSession: authStore.hasPendingAuthSession,
  backendModeEnabled: appStore.backendModeEnabled,
  publicSettingsLoaded: appStore.publicSettingsLoaded,
  publicSettings: appStore.cachedPublicSettings,
  adminOpsMonitoringEnabled: adminSettingsStore.opsMonitoringEnabled,
  adminPaymentEnabled: adminSettingsStore.paymentEnabled
}))

function flattenNavItems(entries: ShellNavItemSpec[]): ShellNavItemSpec[] {
  return entries.flatMap((entry) =>
    entry.expandOnly && entry.children ? flattenNavItems(entry.children) : [entry]
  )
}

async function loadLiveModelsIfPermitted(): Promise<void> {
  if (modelsLoaded.value || modelsLoading.value) return
  if (appStore.cachedPublicSettings?.model_plaza_enabled === false) return
  if (
    appStore.cachedPublicSettings?.model_plaza_require_auth === true &&
    !authStore.isAuthenticated
  ) {
    return
  }

  modelAbortController?.abort()
  modelAbortController = new AbortController()
  modelsLoading.value = true
  try {
    const response = await getModelPlaza({ signal: modelAbortController.signal })
    const seen = new Set<string>()
    const collected: Array<{ name: string; platform: string }> = []
    for (const group of response?.groups ?? []) {
      for (const model of group.models ?? []) {
        if (!model.name || seen.has(model.name)) continue
        seen.add(model.name)
        collected.push({ name: model.name, platform: model.platform || group.platform || '' })
        if (collected.length >= 24) break
      }
      if (collected.length >= 24) break
    }
    liveModelNames.value = collected
    modelsLoaded.value = true
  } catch (err) {
    if (!isSilentRequestError(err)) {
      liveModelNames.value = []
    }
  } finally {
    modelsLoading.value = false
  }
}

watch(isOpen, (open) => {
  if (open) {
    void loadLiveModelsIfPermitted()
  } else {
    modelAbortController?.abort()
  }
})

const groups = computed<UiCommandGroup[]>(() => {
  const result: UiCommandGroup[] = []

  if (authStore.isAuthenticated) {
    const userItems = flattenNavItems(resolveVisibleTierNavItems('user', policyContext.value))
    if (userItems.length > 0) {
      result.push({
        id: 'nav-user',
        label: isZh.value ? '开发者控制台' : 'DEVELOPER CONSOLE',
        items: userItems.map(
          (entry): UiCommandItem => ({
            id: `cmd-${entry.id}`,
            label: resolveItemTitle(entry),
            description: entry.path,
            keywords: entry.keywords,
            onSelect: () => {
              void router.push(entry.path)
            }
          })
        )
      })
    }

    if (authStore.isAdmin) {
      const adminItems = flattenNavItems(resolveVisibleTierNavItems('admin', policyContext.value))
      if (adminItems.length > 0) {
        result.push({
          id: 'nav-admin',
          label: isZh.value ? '管理控制台' : 'ADMIN CONSOLE',
          items: adminItems.map(
            (entry): UiCommandItem => ({
              id: `cmd-${entry.id}`,
              label: resolveItemTitle(entry),
              description: entry.path,
              badge: 'ADMIN',
              keywords: entry.keywords,
              onSelect: () => {
                void router.push(entry.path)
              }
            })
          )
        })
      }
    }

    if ((appStore.scopes?.length ?? 0) > 0) {
      const scopeItems: UiCommandItem[] = [
        {
          id: 'scope-personal',
          label: isZh.value ? '切换至个人空间 (Personal)' : 'Switch to Personal Scope',
          badge: appStore.activeScope.mode === 'personal' ? 'ACTIVE' : undefined,
          keywords: ['scope', 'personal', 'workspace', '个人空间'],
          onSelect: () => appStore.selectPersonalScope()
        },
        ...appStore.scopes.map(
          (ws): UiCommandItem => ({
            id: `scope-ws-${ws.workspace_id}`,
            label: `${ws.name}`,
            description: `#${ws.workspace_id}`,
            badge:
              appStore.activeScope.workspaceId === ws.workspace_id ? 'ACTIVE' : 'WORKSPACE',
            keywords: ['workspace', 'scope', ws.name, ws.workspace_id],
            onSelect: () => appStore.selectWorkspaceScope(ws.workspace_id)
          })
        )
      ]
      result.push({
        id: 'scopes',
        label: isZh.value ? '工作空间切换' : 'SCOPES',
        items: scopeItems
      })
    }
  }

  const publicItems = flattenNavItems(resolveVisibleTierNavItems('public', policyContext.value))
  if (publicItems.length > 0) {
    result.push({
      id: 'nav-public',
      label: isZh.value ? '公开站点' : 'PUBLIC',
      items: publicItems.map(
        (entry): UiCommandItem => ({
          id: `cmd-${entry.id}`,
          label: resolveItemTitle(entry),
          description: entry.path,
          keywords: entry.keywords,
          onSelect: () => {
            void router.push(entry.path)
          }
        })
      )
    })
  }

  if (liveModelNames.value.length > 0) {
    result.push({
      id: 'live-models',
      label: isZh.value ? '实时目录模型' : 'CATALOG MODELS',
      items: liveModelNames.value.map(
        (model): UiCommandItem => ({
          id: `mdl-${model.name}`,
          label: model.name,
          badge: model.platform ? model.platform.toUpperCase() : 'MODEL',
          keywords: ['model', model.name, model.platform],
          onSelect: () => {
            void router.push({ path: '/model-plaza', query: { q: model.name } })
          }
        })
      )
    })
  }

  const actionItems: UiCommandItem[] = [
    {
      id: 'theme-light',
      label: isZh.value ? '外观：切换为浅色模式 (Light)' : 'Theme: Switch to Light Mode',
      badge: themeMode.value === 'light' ? 'ACTIVE' : undefined,
      keywords: ['theme', 'light', 'mode', '浅色', '亮色'],
      onSelect: () => setThemeMode('light')
    },
    {
      id: 'theme-dark',
      label: isZh.value ? '外观：切换为深色模式 (Dark)' : 'Theme: Switch to Dark Mode',
      badge: themeMode.value === 'dark' ? 'ACTIVE' : undefined,
      keywords: ['theme', 'dark', 'mode', '深色', '暗色'],
      onSelect: () => setThemeMode('dark')
    },
    {
      id: 'theme-system',
      label: isZh.value ? '外观：跟随系统主题 (System)' : 'Theme: Follow System Preference',
      badge: themeMode.value === 'system' ? 'ACTIVE' : undefined,
      keywords: ['theme', 'system', 'auto', '跟随系统'],
      onSelect: () => setThemeMode('system')
    },
    {
      id: 'density-comfort',
      label: isZh.value ? '密度：标准间距 (Comfort)' : 'Density: Comfort Spacing',
      badge: densityMode.value === 'comfort' ? 'ACTIVE' : undefined,
      keywords: ['density', 'comfort', 'spacing', '标准密度'],
      onSelect: () => setDensityMode('comfort')
    },
    {
      id: 'density-compact',
      label: isZh.value ? '密度：高密度紧凑视图 (Compact)' : 'Density: Compact Spacing',
      badge: densityMode.value === 'compact' ? 'ACTIVE' : undefined,
      keywords: ['density', 'compact', 'dense', '紧凑密度'],
      onSelect: () => setDensityMode('compact')
    }
  ]

  if (authStore.isAuthenticated && authStore.isAdmin) {
    const onAdmin = route.path.startsWith('/admin')
    actionItems.unshift({
      id: 'switch-console-tier',
      label: onAdmin
        ? isZh.value
          ? '切换至开发者控制台 (User Console)'
          : 'Switch to Developer Console'
        : isZh.value
          ? '切换至管理控制台 (Admin Console)'
          : 'Switch to Admin Console',
      keywords: ['switch', 'admin', 'user', 'console', '切换控制台'],
      onSelect: () => {
        void router.push(onAdmin ? '/dashboard' : '/admin/dashboard')
      }
    })
  }

  result.push({
    id: 'preferences',
    label: isZh.value ? '外观与偏好' : 'PREFERENCES',
    items: actionItems
  })

  return result
})

function handleOpenCustomEvent(): void {
  isOpen.value = true
}

onMounted(() => {
  if (typeof window !== 'undefined') {
    window.addEventListener('omnihive:open-cmdk', handleOpenCustomEvent)
  }
})

onBeforeUnmount(() => {
  modelAbortController?.abort()
  if (typeof window !== 'undefined') {
    window.removeEventListener('omnihive:open-cmdk', handleOpenCustomEvent)
  }
})
</script>

<template>
  <CommandPalette
    v-model:open="isOpen"
    :groups="groups"
    :loading="modelsLoading"
    :placeholder="
      isZh
        ? '搜索页面、实时模型、工作空间或外观命令…'
        : 'Search routes, live models, workspaces, or preferences…'
    "
    :empty-text="
      isZh ? '没有匹配的页面、模型或操作。' : 'No matching routes, models, or commands.'
    "
    register-shortcut
  />
</template>
