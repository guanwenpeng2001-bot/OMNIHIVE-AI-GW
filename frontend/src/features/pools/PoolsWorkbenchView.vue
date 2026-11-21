<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import { PageHeader } from '@/components/ui'
import { workspaceIDFromPath } from '@/api/workspaceScope'
import { useAppStore } from '@/stores'
import OutboundPoolsPanel from './OutboundPoolsPanel.vue'

const { t } = useI18n()
const route = useRoute()
const appStore = useAppStore()

const resolvedWorkspaceId = computed<string | undefined>(() => {
  const fromPath = workspaceIDFromPath(route.path || '')
  if (fromPath) return fromPath
  const fromQuery = typeof route.query?.workspace_id === 'string' ? route.query.workspace_id : ''
  if (fromQuery) return fromQuery
  return appStore.activeScope?.workspaceId || undefined
})
</script>

<template>
  <AppLayout tier="admin" contained>
    <div class="space-y-4" data-testid="pools-workbench-view">
      <PageHeader
        :title="t('routing.outboundPools.pageTitle')"
        :description="t('routing.outboundPools.pageDescription')"
      />
      <OutboundPoolsPanel :workspace-id="resolvedWorkspaceId" />
    </div>
  </AppLayout>
</template>
