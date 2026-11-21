<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useAppStore } from '@/stores'
import { useIsZhLocale } from '@/utils/activeLocale'
import AppLayout from '@/components/layout/AppLayout.vue'
import MembersAccessPage from '@/features/members/MembersAccessPage.vue'

const route = useRoute()
const appStore = useAppStore()
const isZh = useIsZhLocale('zh')

const initialWorkspaceId = computed(() => {
  const fromQuery = typeof route.query?.workspace_id === 'string' ? route.query.workspace_id : ''
  return fromQuery || appStore.activeScope?.workspaceId || ''
})
</script>

<template>
  <AppLayout tier="admin" contained>
    <template #subbar>
      <nav
        :aria-label="isZh ? '身份与成员管理视图切换' : 'Identity & member management views'"
        class="flex flex-wrap items-center justify-between gap-2 text-xs"
      >
        <div class="inline-flex items-center gap-1 rounded-oh-md border border-oh-default bg-oh-surface p-0.5">
          <RouterLink
            to="/admin/users"
            class="oh-focus-ring rounded-oh-sm px-2.5 py-1 font-medium text-oh-secondary transition-colors hover:bg-oh-hover hover:text-oh-primary"
          >
            {{ isZh ? '平台用户管理' : 'Platform Users' }}
          </RouterLink>
          <RouterLink
            to="/admin/members"
            aria-current="page"
            class="oh-focus-ring rounded-oh-sm bg-oh-selected px-2.5 py-1 font-semibold text-oh-accent-text"
          >
            {{ isZh ? '工作空间成员与授权' : 'Workspace Members & Access' }}
          </RouterLink>
        </div>

        <span class="hidden font-mono text-[11px] text-oh-muted sm:inline">
          {{ isZh ? '支持项目边界隔离与 CAS 并发保护' : 'Project isolation & CAS optimistic concurrency' }}
        </span>
      </nav>
    </template>

    <MembersAccessPage :initial-workspace-id="initialWorkspaceId" />
  </AppLayout>
</template>
