<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { i18n as globalI18n } from '@/i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import SettingsWorkbenchPage from '@/features/settings/SettingsWorkbenchPage.vue'
import { SETTINGS_SECTIONS, type SettingsSectionId } from '@/features/settings/sections'

const props = defineProps<{
  section?: string
}>()

const route = useRoute()

let i18nLocale: { value?: string } | string | undefined
try {
  const composer = useI18n()
  i18nLocale = composer?.locale
} catch {
  i18nLocale = undefined
}

const isZh = computed(() => {
  const raw =
    (typeof i18nLocale === 'string' ? i18nLocale : i18nLocale?.value) ||
    globalI18n?.global?.locale?.value ||
    'zh'
  return String(raw).toLowerCase().startsWith('zh')
})

const validSectionIds = new Set<string>(SETTINGS_SECTIONS.map((item) => item.id))

const resolvedSection = computed<SettingsSectionId>(() => {
  const raw = String(props.section ?? route.params?.section ?? 'general')
  return (validSectionIds.has(raw) ? raw : 'general') as SettingsSectionId
})
</script>

<template>
  <AppLayout tier="admin" contained>
    <template #subbar>
      <div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div class="inline-flex shrink-0 items-center gap-1 rounded-oh-md border border-oh-default bg-oh-surface p-0.5 text-xs">
          <RouterLink
            to="/admin/settings"
            class="oh-focus-ring rounded-oh-sm px-2.5 py-1 font-medium text-oh-secondary transition-colors hover:bg-oh-hover hover:text-oh-primary"
          >
            {{ isZh ? '全量系统设置' : 'All Classic Settings' }}
          </RouterLink>
          <span
            class="rounded-oh-sm bg-oh-selected px-2.5 py-1 font-semibold text-oh-accent-text"
            aria-current="page"
          >
            {{ isZh ? '分区配置工作台' : 'Sectioned Workbench' }}
          </span>
        </div>

        <nav
          :aria-label="isZh ? '系统设置分区分段导航' : 'Settings section quick links'"
          class="flex flex-wrap items-center gap-1 text-xs"
        >
          <RouterLink
            v-for="item in SETTINGS_SECTIONS"
            :key="item.id"
            :to="`/admin/settings/${item.id}`"
            :aria-current="resolvedSection === item.id ? 'page' : undefined"
            :class="[
              'oh-focus-ring rounded-oh-sm border px-2 py-0.5 font-mono text-[11px] transition-colors',
              resolvedSection === item.id
                ? 'border-oh-accent bg-oh-selected font-semibold text-oh-accent-text'
                : 'border-oh-default bg-oh-surface text-oh-secondary hover:border-oh-strong hover:text-oh-primary'
            ]"
          >
            {{ isZh ? item.labelZh : item.label }}
          </RouterLink>
        </nav>
      </div>
    </template>

    <SettingsWorkbenchPage :key="resolvedSection" :initial-section="resolvedSection" />
  </AppLayout>
</template>
