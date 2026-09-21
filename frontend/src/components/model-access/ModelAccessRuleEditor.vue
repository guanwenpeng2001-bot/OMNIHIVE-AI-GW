<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import { modelAccess, type ModelAccessGroup, type ModelAccessImpact, type ModelAccessRule } from '@/api/modelAccess'

const props = defineProps<{ path: string }>()
const { t } = useI18n()
const groups = ref<ModelAccessGroup[]>([])
const rule = ref<ModelAccessRule>({ access_group_id: null, models: null, exclude: [], revision: 0 })
const includeText = ref('')
const excludeText = ref('')
const limited = ref(false)
const busy = ref(false)
const loaded = ref(false)
const error = ref('')
const saved = ref(false)
const impact = ref<ModelAccessImpact | null>(null)
const options = computed(() => [{ value: '', label: t('modelAccess.inherit') }, ...groups.value.map(group => ({ value: String(group.id), label: group.name }))])
const selected = computed({ get: () => rule.value.access_group_id == null ? '' : String(rule.value.access_group_id), set: (value: string | number | boolean | null | undefined) => { rule.value.access_group_id = value ? Number(value) : null } })
const lines = (text: string) => [...new Set(text.split('\n').map(model => model.trim()).filter(Boolean))]
function payload(): ModelAccessRule { return { ...rule.value, models: limited.value ? lines(includeText.value) : null, exclude: lines(excludeText.value) } }
watch(() => props.path, async (path, _previous, cleanup) => {
  const controller = new AbortController()
  cleanup(() => controller.abort())
  loaded.value = false
  error.value = ''
  impact.value = null
  try {
    const [available, current] = await Promise.all([modelAccess.groups(path.startsWith('/admin')), modelAccess.rule(path, controller.signal)])
    if (controller.signal.aborted) return
    groups.value = available
    rule.value = current
    limited.value = current.models !== null
    includeText.value = current.models?.join('\n') ?? ''
    excludeText.value = current.exclude?.join('\n') ?? ''
    loaded.value = true
  } catch (cause) { if (!controller.signal.aborted) error.value = cause instanceof Error ? cause.message : t('modelAccess.failed') }
}, { immediate: true })
watch([limited, includeText, excludeText, selected], () => { impact.value = null; saved.value = false })
async function perform(save: boolean) {
  busy.value = true
  error.value = ''
  const path = props.path
  const proposed = payload()
  try {
    if (save) { const current = await modelAccess.saveRule(path, proposed); if (path === props.path) { rule.value = current; saved.value = true } }
    if (path !== props.path) return
    const preview = await modelAccess.preview(path, proposed)
    if (path === props.path) impact.value = preview
  } catch (cause) { if (path === props.path) error.value = cause instanceof Error ? cause.message : t('modelAccess.failed') }
  finally { busy.value = false }
}
</script>

<template>
  <section class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-600">
    <h3 class="input-label">{{ t('modelAccess.title') }}</h3>
    <p class="input-hint">{{ t('modelAccess.explanation') }}</p>
    <template v-if="loaded">
      <Select v-model="selected" :options="options" :disabled="busy" :placeholder="t('modelAccess.inherit')" />
      <label class="flex items-center gap-2 text-sm"><input v-model="limited" type="checkbox" :disabled="busy" />{{ t('modelAccess.limit') }}</label>
      <label v-if="limited" class="block"><span class="input-label">{{ t('modelAccess.include') }}</span><textarea v-model="includeText" class="input" rows="3" :disabled="busy" /></label>
      <label class="block"><span class="input-label">{{ t('modelAccess.exclude') }}</span><textarea v-model="excludeText" class="input" rows="2" :disabled="busy" /></label>
      <div class="flex gap-2"><button type="button" class="btn btn-secondary" :disabled="busy" @click="perform(false)">{{ t('modelAccess.preview') }}</button><button type="button" class="btn btn-primary" :disabled="busy" @click="perform(true)">{{ t('modelAccess.save') }}</button></div>
      <p v-if="saved" role="status" class="input-hint">{{ t('modelAccess.saved') }}</p>
      <div v-if="impact" class="input-hint" role="status"><p>{{ t('modelAccess.impact', { count: impact.models.length }) }}</p><p v-if="!impact.fully_enumerable">{{ t('modelAccess.partial') }}</p><p class="max-h-32 overflow-auto break-all">{{ impact.models.join(', ') || t('modelAccess.empty') }}</p></div>
    </template>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
  </section>
</template>
