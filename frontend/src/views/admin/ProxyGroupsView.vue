<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import Pagination from '@/components/common/Pagination.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ProxyGroupEditModal from '@/components/proxyGroup/ProxyGroupEditModal.vue'
import ProxyGroupMembersModal from '@/components/proxyGroup/ProxyGroupMembersModal.vue'
import ProxyGroupBindingsDrawer from '@/components/proxyGroup/ProxyGroupBindingsDrawer.vue'
import EgressPoolsPanel from '@/features/egress/EgressPoolsPanel.vue'
import { ErrorState } from '@/components/ui'
import { proxyGroupsAPI, type ProxyGroup } from '@/api/admin/proxyGroups'
import { extractApiErrorMessage } from '@/utils/apiError'
const { t } = useI18n()
const search = ref('')
const status = ref('')
const service = ref('')
const groups = ref<ProxyGroup[]>([])
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const loading = ref(false)
const error = ref('')
const editor = ref<{ group?: ProxyGroup } | null>(null)
const memberGroup = ref<number | null>(null)
const bindingGroup = ref<number | null>(null)
const deleting = ref<ProxyGroup | null>(null)
const deletingBusy = ref(false)
let request = 0
async function load() {
  const current = ++request
  loading.value = true
  error.value = ''
  try {
    const result = await proxyGroupsAPI.list(page.value, pageSize.value, { search: search.value, status: status.value, target_service: service.value })
    if (current !== request) return
    groups.value = result.items
    total.value = result.total
  } catch (err) { if (current === request) error.value = extractApiErrorMessage(err, t('admin.proxyGroups.loadFailed')) }
  finally { if (current === request) loading.value = false }
}
function filter() { if (page.value !== 1) page.value = 1; else void load() }
watch([status, service, pageSize], filter)
watch(page, load)
onMounted(load)
async function remove() {
  if (!deleting.value || deletingBusy.value) return
  deletingBusy.value = true
  try {
    await proxyGroupsAPI.remove(deleting.value.id)
    deleting.value = null
    if (groups.value.length === 1 && page.value > 1) page.value--
    else await load()
  } catch (err) { deleting.value = null; error.value = extractApiErrorMessage(err, t('admin.proxyGroups.deleteFailed')) }
  finally { deletingBusy.value = false }
}
function saved() { editor.value = null; memberGroup.value = null; void load() }
</script>

<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="space-y-3">
          <div class="flex flex-wrap items-center justify-between gap-3"><h1 class="text-2xl font-semibold tracking-tight text-[var(--foreground)]">{{ t('admin.proxyGroups.title') }}</h1><button class="btn btn-primary" @click="editor = {}">{{ t('admin.proxyGroups.create') }}</button></div>
          <form class="flex flex-wrap items-center gap-2 rounded-lg border border-[var(--border)] bg-[var(--card)] p-3" @submit.prevent="filter">
            <input v-model="search" class="input w-full sm:w-64" :placeholder="t('admin.proxyGroups.search')" :aria-label="t('admin.proxyGroups.search')" type="search" @input="filter" />
            <select v-model="status" class="input w-full sm:w-40" :aria-label="t('common.status')"><option value="">{{ t('admin.proxyGroups.allStatus') }}</option><option value="active">{{ t('admin.proxyGroups.active') }}</option><option value="inactive">{{ t('admin.proxyGroups.inactive') }}</option></select>
            <input v-model="service" class="input w-full sm:w-48" list="proxy-group-services" :placeholder="t('admin.proxyGroups.allServices')" :aria-label="t('admin.proxyGroups.services')" />
            <datalist id="proxy-group-services"><option v-for="name in ['grok', 'chatgpt', 'gemini', 'google']" :key="name" :value="name" /></datalist>
            <button class="btn btn-secondary" type="button" :disabled="loading" @click="load">{{ t('common.refresh') }}</button>
          </form>
          <p class="text-sm text-[var(--muted-foreground)]">{{ t('admin.proxyGroups.countsHint') }} {{ t('admin.proxyGroups.tagsHint') }}</p>
          <p v-if="error" role="alert" class="text-sm text-[var(--bad-text)]">{{ error }}</p>
          <EgressPoolsPanel />
        </div>
      </template>
      <template #table>
        <div class="table-wrapper bg-[var(--card)] text-[var(--foreground)]" :aria-busy="loading">
          <p v-if="loading" role="status" class="p-8 text-center text-sm text-[var(--muted-foreground)]">{{ t('common.loading') }}</p>
          <div v-else-if="error && !groups.length" class="p-6">
            <ErrorState
              :title="t('admin.proxyGroups.loadFailed')"
              :message="error"
              :retry-label="t('common.tryAgain')"
              @retry="load"
            />
          </div>
          <p v-else-if="!groups.length && !error" class="p-8 text-center text-sm text-[var(--muted-foreground)]">{{ t('admin.proxyGroups.empty') }}</p>
          <div v-else class="divide-y divide-[var(--border)]">
            <div class="hidden grid-cols-[minmax(0,2fr)_1fr_1fr_1fr_minmax(0,2fr)] gap-4 bg-[var(--surface-2)] px-4 py-3 text-sm text-[var(--muted-foreground)] lg:grid"><span>{{ t('admin.proxyGroups.name') }} / {{ t('admin.proxyGroups.services') }}</span><span>{{ t('admin.proxyGroups.counts') }}</span><span>{{ t('admin.proxyGroups.accounts') }}</span><span>{{ t('admin.proxyGroups.fallback') }}</span><span>{{ t('common.actions') }}</span></div>
            <article v-for="group in groups" :key="group.id" class="grid gap-3 px-4 py-3 transition-colors hover:bg-[var(--surface-2)] lg:grid-cols-[minmax(0,2fr)_1fr_1fr_1fr_minmax(0,2fr)] lg:items-center lg:gap-4">
              <div class="min-w-0 space-y-2"><div class="flex flex-wrap items-center gap-2"><h2 class="break-words text-sm font-semibold">{{ group.name }}</h2><span class="pill" :class="group.status === 'active' ? 'text-[var(--ok-text)]' : 'pill-ghost text-[var(--muted-foreground)]'"><span class="pulse-dot" :class="group.status === 'active' ? 'pulse-ok' : 'bg-[var(--muted-foreground)]'" aria-hidden="true" />{{ t(`admin.proxyGroups.${group.status}`) }}</span></div><p v-if="group.region" class="text-xs text-[var(--muted-foreground)]">{{ group.region }}</p><div class="flex flex-wrap gap-1"><span v-for="tag in group.target_services" :key="tag" class="pill pill-ghost text-[var(--muted-foreground)]">{{ tag }}</span></div></div>
              <p class="font-mono text-sm tabular-nums" :class="group.schedulable_member_count === 0 && 'text-[var(--bad-text)]'"><span class="mr-2 font-sans text-xs text-[var(--muted-foreground)] lg:hidden">{{ t('admin.proxyGroups.counts') }}</span>{{ group.schedulable_member_count }} / {{ group.member_count }}</p>
              <p class="font-mono text-sm tabular-nums"><span class="mr-2 font-sans text-xs text-[var(--muted-foreground)] lg:hidden">{{ t('admin.proxyGroups.accounts') }}</span>{{ group.bound_account_count }}</p>
              <p class="text-xs text-[var(--muted-foreground)]">{{ t(`admin.proxyGroups.${group.fallback_mode}`) }}</p>
              <div class="flex flex-wrap gap-1.5"><button class="btn btn-secondary btn-sm" @click="editor = { group }">{{ t('common.edit') }}</button><button class="btn btn-secondary btn-sm" @click="memberGroup = group.id">{{ t('admin.proxyGroups.members') }}</button><button class="btn btn-secondary btn-sm" @click="bindingGroup = group.id">{{ t('admin.proxyGroups.bindings') }}</button><span :title="group.bound_account_count ? t('admin.proxyGroups.inUse') : ''"><button class="btn btn-secondary btn-sm text-[var(--bad-text)]" :disabled="group.bound_account_count > 0 || deletingBusy" @click="deleting = group">{{ t('common.delete') }}</button></span></div>
            </article>
          </div>
        </div>
      </template>
      <template #pagination><Pagination v-if="total > 0" v-model:page="page" v-model:page-size="pageSize" :total="total" /></template>
    </TablePageLayout>
    <ProxyGroupEditModal v-if="editor" :group="editor.group" @close="editor = null" @saved="saved" />
    <ProxyGroupMembersModal v-if="memberGroup" :group-id="memberGroup" @close="memberGroup = null" @saved="saved" />
    <ProxyGroupBindingsDrawer v-if="bindingGroup" :group-id="bindingGroup" @close="bindingGroup = null" />
    <ConfirmDialog :show="!!deleting" :title="t('admin.proxyGroups.delete')" :message="t('admin.proxyGroups.deleteConfirm')" danger @cancel="!deletingBusy && (deleting = null)" @confirm="remove" />
  </AppLayout>
</template>
