<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import SettingsWorkbenchPage from '@/features/settings/SettingsWorkbenchPage.vue'
import { SETTINGS_SECTIONS, type SettingsSectionId } from '@/features/settings/sections'
import { useIsZhLocale } from '@/utils/activeLocale'

const props = defineProps<{
  section?: string
}>()

const route = useRoute()
const router = useRouter()
const isZh = useIsZhLocale('zh')

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
      </div>
    </template>

    <SettingsWorkbenchPage
      :initial-section="resolvedSection"
      @select-section="section => router.push({ name: 'AdminSettingsSection', params: { section } })"
    />
  </AppLayout>
</template>
