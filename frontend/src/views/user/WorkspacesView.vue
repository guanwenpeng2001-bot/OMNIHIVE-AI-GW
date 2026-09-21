<template>
  <AppLayout>
    <div class="space-y-6">
      <header>
        <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('workspaces.title') }}</h1>
        <p class="mt-2 text-gray-500 dark:text-gray-400">{{ t('workspaces.description') }}</p>
      </header>
      <form v-if="auth.isAdmin" class="card flex flex-wrap items-end gap-3 p-4" @submit.prevent="createWorkspace">
        <label class="flex-1 space-y-2">
          <span>{{ t('workspaces.name') }}</span>
          <input v-model="workspaceName" required maxlength="100" class="input w-full" />
        </label>
        <button class="btn btn-primary" :disabled="busy || !workspaceName.trim()">{{ t('workspaces.create') }}</button>
      </form>
      <label class="block space-y-2">
        <span>{{ t('workspaces.select') }}</span>
        <select v-model="selectedID" class="input w-full" :disabled="loading || busy" @change="loadMembers">
          <option :value="null" disabled>{{ t('workspaces.select') }}</option>
          <option v-for="workspace in workspaces" :key="workspace.id" :value="workspace.id">{{ workspace.name }}</option>
        </select>
      </label>
      <p v-if="!loading && workspaces.length === 0">{{ t('workspaces.empty') }}</p>
      <section v-if="selected" class="card space-y-4 p-4">
        <h2 class="text-lg font-semibold">{{ selected.name }} · {{ t('workspaces.resources') }}</h2>
        <div class="flex flex-wrap gap-3">
          <RouterLink :to="`/workspaces/${selected.id}/keys`" class="btn btn-secondary">{{ t('nav.apiKeys') }}</RouterLink>
          <template v-if="selected.role === 'admin'">
            <RouterLink v-for="resource in resources" :key="resource.path" :to="`/workspaces/${selected.id}/${resource.path}`" class="btn btn-secondary">{{ t(resource.label) }}</RouterLink>
          </template>
        </div>
      </section>
      <section v-if="selected?.role === 'admin'" class="card space-y-4 p-4" :aria-busy="loadingMembers">
        <h2 class="text-lg font-semibold">{{ t('workspaces.members') }}</h2>
        <form class="flex flex-wrap items-end gap-3" @submit.prevent="saveMember">
          <label class="space-y-2"><span class="block">{{ t('workspaces.userID') }}</span><input v-model.number="userID" type="number" min="1" required class="input" /></label>
          <label class="space-y-2"><span class="block">{{ t('workspaces.role') }}</span><select v-model="memberRole" class="input"><option value="member">{{ t('workspaces.member') }}</option><option value="admin">{{ t('workspaces.admin') }}</option></select></label>
          <button class="btn btn-primary" :disabled="busy || !userID">{{ t('workspaces.save') }}</button>
        </form>
        <ul class="divide-y divide-gray-200 dark:divide-gray-700">
          <li v-for="member in members" :key="member.user_id" class="flex flex-wrap items-center justify-between gap-3 py-3">
            <span>{{ t('workspaces.userID') }}: {{ member.user_id }} · {{ member.role === 'admin' ? t('workspaces.admin') : t('workspaces.member') }}</span>
            <button type="button" class="btn btn-secondary" :disabled="busy" @click="removeMember(member.user_id)">{{ t('workspaces.remove') }}</button>
          </li>
        </ul>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import { workspacesAPI, type Workspace, type WorkspaceMember } from '@/api/workspaces'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { normalizeApiError } from '@/utils/apiError'

const { t } = useI18n()
const auth = useAuthStore()
const app = useAppStore()
const workspaces = ref<Workspace[]>([])
const members = ref<WorkspaceMember[]>([])
const selectedID = ref<number | null>(null)
const selected = computed(() => workspaces.value.find(workspace => workspace.id === selectedID.value))
const workspaceName = ref('')
const userID = ref<number | null>(null)
const memberRole = ref<WorkspaceMember['role']>('member')
const busy = ref(false)
const loading = ref(true)
const loadingMembers = ref(false)
let memberRequest = 0
const resources = [
  { path: 'accounts', label: 'nav.accounts' },
  { path: 'groups', label: 'nav.groups' },
  { path: 'proxies', label: 'nav.proxies' },
  { path: 'channels/pricing', label: 'nav.channels' },
  { path: 'channels/monitor', label: 'nav.channelMonitor' },
  { path: 'monitor', label: 'nav.channelStatus' },
]

async function loadMembers() {
  const request = ++memberRequest
  members.value = []
  if (selected.value?.role !== 'admin') return
  loadingMembers.value = true
  try {
    const result = await workspacesAPI.members(selected.value.id)
    if (request === memberRequest) members.value = result
  } catch (error) {
    if (request === memberRequest) app.showError(normalizeApiError(error).message)
  } finally {
    if (request === memberRequest) loadingMembers.value = false
  }
}

async function refresh() {
  workspaces.value = await workspacesAPI.list()
  if (!workspaces.value.some(workspace => workspace.id === selectedID.value)) selectedID.value = workspaces.value[0]?.id ?? null
  await loadMembers()
}

async function createWorkspace() {
  busy.value = true
  try {
    const workspace = await workspacesAPI.create(workspaceName.value.trim())
    selectedID.value = workspace.id
    workspaceName.value = ''
    await refresh()
  } catch (error) { app.showError(normalizeApiError(error).message) }
  finally { busy.value = false }
}

async function saveMember() {
  if (!selected.value || !userID.value) return
  busy.value = true
  try {
    await workspacesAPI.setMember(selected.value.id, userID.value, memberRole.value)
    app.showSuccess(t('workspaces.saved'))
    await refresh()
  } catch (error) { app.showError(normalizeApiError(error).message) }
  finally { busy.value = false }
}

async function removeMember(memberID: number) {
  if (!selected.value) return
  busy.value = true
  try { await workspacesAPI.removeMember(selected.value.id, memberID); await refresh() }
  catch (error) { app.showError(normalizeApiError(error).message) }
  finally { busy.value = false }
}

onMounted(async () => {
  try { await refresh() }
  catch (error) { app.showError(normalizeApiError(error).message) }
  finally { loading.value = false }
})
</script>
