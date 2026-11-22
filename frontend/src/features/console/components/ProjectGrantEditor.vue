<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { identityApi, type IdentityMemberGrants } from '../scope'

const props = defineProps<{ workspaceId: string; memberId: string; projects: Array<{ id: string; name: string }>; inheritedAdmin?: boolean }>()
const { locale } = useI18n()
const zh = computed(() => String(locale.value).startsWith('zh'))
const grants = ref<IdentityMemberGrants | null>(null)
const projectId = ref('')
const actions = ref<string[]>([])
const busy = ref(false)
const error = ref('')
const saved = ref(false)
let generation = 0
const context = computed(() => `${props.workspaceId}:${props.memberId}`)
onBeforeUnmount(() => { generation++ })
function selectProject() {
  actions.value = [...(grants.value?.project_grants.find(item => item.project_id === projectId.value)?.actions ?? [])]
  saved.value = false
}
async function load() {
  const gen = ++generation
  grants.value = null
  busy.value = true
  error.value = ''
  try {
    const result = await identityApi.getMemberGrants(props.workspaceId, props.memberId)
    if (gen !== generation) return
    if (!Array.isArray(result.project_grants) || !Array.isArray(result.delegable_actions)) throw new Error(zh.value ? '授权合同不可用，请升级后端后重试。' : 'The paired grant contract is unavailable. Upgrade the backend and retry.')
    grants.value = result
    selectProject()
  } catch (err) {
    if (gen === generation) error.value = (err as { message?: string }).message || (zh.value ? '无法读取授权，请重试。' : 'Unable to read grants. Retry.')
  } finally { if (gen === generation) busy.value = false }
}
async function save() {
  if (!grants.value || !projectId.value || busy.value) return
  const gen = generation
  const selectedProject = projectId.value
  const body = [...actions.value]
  busy.value = true
  error.value = ''
  try {
    await identityApi.updateProjectGrants(props.workspaceId, props.memberId, selectedProject, body, grants.value.version)
    if (gen !== generation) return
    await load()
    saved.value = !error.value
  } catch (err) {
    if (gen !== generation) return
    const status = (err as { status?: number; response?: { status?: number } }).status ?? (err as { response?: { status?: number } }).response?.status
    if (status === 412 || status === 409) {
      await load()
      error.value = zh.value ? '授权已被其他管理员修改，已重新读取。请检查后再保存。' : 'Grants changed concurrently and were reloaded. Review before saving again.'
    } else error.value = (err as { message?: string }).message || (zh.value ? '保存失败。' : 'Save failed.')
  } finally { busy.value = false }
}
watch(context, () => { projectId.value = ''; actions.value = []; saved.value = false; void load() }, { immediate: true })
</script>

<template>
  <section class="space-y-3 rounded border p-4" data-testid="project-grant-editor" :aria-busy="busy">
    <h3 class="font-semibold">{{ zh ? '逐项目授权' : 'Project permissions' }} · #{{ memberId }}</h3>
    <p class="text-sm">{{ zh ? '只保存所选项目的直接授权，其他项目保持不变。' : 'Save direct grants for the selected project. Other projects are unchanged.' }}</p>
    <p v-if="inheritedAdmin" class="text-sm">{{ zh ? '此成员作为工作空间管理员继承非平台权限；此处的直接授权不会撤销角色继承权限。' : 'Workspace administrators inherit non-platform actions. Direct grants here do not remove inherited role permissions.' }}</p>
    <p v-if="error" role="alert">{{ error }}</p>
    <p v-if="saved" role="status">{{ zh ? '授权已保存并重新读取。' : 'Grants saved and reloaded.' }}</p>
    <label class="block text-sm">{{ zh ? '项目' : 'Project' }}
      <select v-model="projectId" :disabled="busy || !grants" class="input w-full" @change="selectProject">
        <option value="">{{ zh ? '明确选择项目' : 'Select a project' }}</option>
        <option v-for="project in projects" :key="project.id" :value="project.id">{{ project.name }} (#{{ project.id }})</option>
      </select>
    </label>
    <fieldset v-if="grants && projectId" :disabled="busy" class="grid gap-2 sm:grid-cols-2">
      <label v-for="action in grants.delegable_actions" :key="action" class="flex gap-2 text-sm"><input v-model="actions" type="checkbox" :value="action" />{{ action }}</label>
    </fieldset>
    <button class="btn btn-primary" :disabled="busy || !grants || !projectId" @click="save">{{ zh ? '保存此项目授权' : 'Save project grants' }}</button>
    <button class="btn btn-secondary" :disabled="busy" @click="load">{{ zh ? '重新读取' : 'Reload' }}</button>
  </section>
</template>
