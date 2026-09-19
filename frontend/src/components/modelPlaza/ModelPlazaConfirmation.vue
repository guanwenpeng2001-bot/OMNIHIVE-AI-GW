<template>
  <section class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-600" :aria-busy="loading || saving">
    <h3 class="font-semibold">{{ t('modelPlaza.confirmation.title') }} — {{ groupName || groupId }}</h3>
    <p class="text-sm text-gray-500">{{ t('modelPlaza.confirmation.description') }}</p>
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <p v-if="error" role="alert">{{ error }}</p>
    <p v-if="view && !options.length">{{ t('modelPlaza.confirmation.empty') }}</p>
    <label v-for="model in options" :key="identity(model)" class="flex items-start gap-2 break-all text-sm">
      <input v-model="selected" type="checkbox" :value="identity(model)" :disabled="loading || saving || stale" />
      <span>{{ model.platform }} / {{ model.name }}
        <small v-if="saved(model)?.confirmed_at" class="block text-gray-500">{{ t('modelPlaza.confirmation.confirmed', { actor: saved(model)?.confirmed_by, time: saved(model)?.confirmed_at }) }}</small>
        <small v-else-if="saved(model)" class="block text-gray-500">{{ t('modelPlaza.confirmation.legacy') }}</small>
      </span>
    </label>
    <div class="flex flex-wrap gap-2">
      <button type="button" class="btn btn-secondary" :disabled="loading || saving" @click="load">{{ t('common.refresh') }}</button>
      <button type="button" class="btn btn-primary" :disabled="!view || loading || saving || stale" @click="save">{{ t('modelPlaza.confirmation.save') }}</button>
    </div>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { confirmPlazaModels, getPlazaCandidates, type PlazaCandidateIdentity, type PlazaCandidateView } from '@/api/admin/channels'
import { useAuthStore } from '@/stores/auth'
onBeforeUnmount(() => { generation++ })
const props = defineProps<{ channelId: number; groupId: number; groupName?: string }>()
const { t } = useI18n()
const auth = useAuthStore()
const view = ref<PlazaCandidateView | null>(null)
const loading = ref(false)
const saving = ref(false)
const stale = ref(false)
const error = ref('')
const selected = ref<string[]>([])
let generation = 0
const identity = (model: PlazaCandidateIdentity) => JSON.stringify([model.platform, model.name])
const options = computed(() => {
  const unique = new Map<string, PlazaCandidateIdentity>()
  for (const model of [...(view.value?.candidates ?? []), ...(view.value?.selected ?? [])]) unique.set(identity(model), model)
  return [...unique.values()]
})
const saved = (model: PlazaCandidateIdentity) => view.value?.selected.find(entry => identity(entry) === identity(model))
async function load() {
  const current = ++generation
  loading.value = true
  stale.value = true
  error.value = ''
  try {
    const result = await getPlazaCandidates(props.channelId, props.groupId)
    if (current !== generation) return
    view.value = result
    selected.value = result.selected.map(identity)
    stale.value = false
  } catch {
    if (current === generation) error.value = t('modelPlaza.confirmation.loadError')
  } finally { if (current === generation) loading.value = false }
}
async function save() {
  if (!view.value || stale.value || saving.value) return
  const current = generation
  saving.value = true
  error.value = ''
  try {
    await confirmPlazaModels(props.channelId, props.groupId, view.value.revision, options.value.filter(model => selected.value.includes(identity(model))))
    if (current === generation) { saving.value = false; await load() }
  } catch (failure) {
    if (current !== generation) return
    const status = (failure as { response?: { status?: number } }).response?.status
    stale.value = status === 409
    error.value = t(stale.value ? 'modelPlaza.confirmation.conflict' : 'modelPlaza.confirmation.saveError')
  } finally { if (current === generation) saving.value = false }
}
watch(() => [props.channelId, props.groupId, auth.user?.id, auth.user?.role, auth.sessionRevision], () => {
  generation++
  view.value = null
  selected.value = []
  saving.value = false
  void load()
}, { immediate: true })
</script>
