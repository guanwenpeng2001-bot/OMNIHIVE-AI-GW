<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { modelAccess, type ModelAccessGroup } from '@/api/modelAccess'

const { t } = useI18n()
const groups = ref<ModelAccessGroup[]>([])
const editing = ref<ModelAccessGroup | null>(null)
const name = ref('')
const models = ref('')
const error = ref('')
const busy = ref(false)
async function load() { try { groups.value = await modelAccess.groups(true) } catch (cause) { error.value = cause instanceof Error ? cause.message : t('modelAccess.failed') } }
function edit(group: ModelAccessGroup | null) { editing.value = group; name.value = group?.name ?? ''; models.value = group?.models.join('\n') ?? '' }
async function save() {
  busy.value = true
  error.value = ''
  try { await modelAccess.saveGroup({ id: editing.value?.id ?? 0, revision: editing.value?.revision ?? 0, references: 0, name: name.value, models: [...new Set(models.value.split('\n').map(model => model.trim()).filter(Boolean))] }); edit(null); await load() }
  catch (cause) { error.value = cause instanceof Error ? cause.message : t('modelAccess.failed') }
  finally { busy.value = false }
}
async function remove(group: ModelAccessGroup) { busy.value = true; error.value = ''; try { await modelAccess.deleteGroup(group); await load() } catch (cause) { error.value = cause instanceof Error ? cause.message : t('modelAccess.failed') } finally { busy.value = false } }
</script>

<template>
  <details class="card mb-4 space-y-3 p-4" @toggle="($event.target as HTMLDetailsElement).open && load()">
    <summary class="cursor-pointer font-medium">{{ t('modelAccess.groups') }}</summary>
    <p class="input-hint">{{ t('modelAccess.explanation') }}</p>
    <div v-for="group in groups" :key="group.id" class="flex flex-wrap items-center gap-3 border-b border-gray-200 py-2 dark:border-dark-600"><span class="flex-1">{{ group.name }} · {{ t('modelAccess.references', { count: group.references }) }}</span><button type="button" class="btn btn-secondary" :disabled="busy" @click="edit(group)">{{ t('common.edit') }}</button><button type="button" class="btn btn-secondary" :disabled="busy || group.references > 0" @click="remove(group)">{{ t('common.delete') }}</button></div>
    <label class="block"><span class="input-label">{{ t('modelAccess.name') }}</span><input v-model="name" class="input" maxlength="100" :disabled="busy" /></label>
    <label class="block"><span class="input-label">{{ t('modelAccess.include') }}</span><textarea v-model="models" class="input" rows="3" :disabled="busy" /></label>
    <div class="flex gap-2"><button type="button" class="btn btn-primary" :disabled="busy || !name.trim()" @click="save">{{ t('common.save') }}</button><button v-if="editing" type="button" class="btn btn-secondary" @click="edit(null)">{{ t('common.cancel') }}</button></div>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
  </details>
</template>
