<script setup lang="ts">
import { reactive, ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'
import { proxyGroupsAPI, type ProxyGroup, type ProxyGroupInput } from '@/api/admin/proxyGroups'
import { getAll } from '@/api/admin/proxies'
import type { Proxy } from '@/types/admin'
import { extractApiErrorMessage } from '@/utils/apiError'
const props = defineProps<{ group?: ProxyGroup }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const draft = reactive<ProxyGroupInput>({ name: props.group?.name ?? '', description: props.group?.description ?? '', region: props.group?.region ?? '', target_services: [...(props.group?.target_services ?? [])], status: props.group?.status ?? 'active', fallback_mode: props.group?.fallback_mode ?? 'none', fallback_proxy_id: props.group?.fallback_proxy_id ?? null, binding_ttl_seconds: props.group?.binding_ttl_seconds ?? 86400 })
const services = ref(draft.target_services.join(', '))
const proxies = ref<Proxy[]>([])
const busy = ref(false)
const error = ref('')
onMounted(async () => {
  try { proxies.value = await getAll() } catch { /* A retained fallback remains visible by ID below. */ }
})
async function save() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    const payload = { ...draft, name: draft.name.trim(), target_services: [...new Set(services.value.split(',').map(s => s.trim()).filter(Boolean))], fallback_proxy_id: draft.fallback_mode === 'proxy' ? draft.fallback_proxy_id : 0 }
    if (props.group) await proxyGroupsAPI.update(props.group.id, payload)
    else await proxyGroupsAPI.create(payload)
    emit('saved')
  } catch (err) { error.value = extractApiErrorMessage(err, t('admin.proxyGroups.saveFailed')) }
  finally { busy.value = false }
}
</script>

<template>
  <BaseDialog :show="true" :title="t(group ? 'admin.proxyGroups.edit' : 'admin.proxyGroups.create')" @close="!busy && emit('close')">
    <form id="proxy-group-editor" class="space-y-4 text-[var(--foreground)]" @submit.prevent="save">
      <label class="block"><span class="input-label">{{ t('admin.proxyGroups.name') }}</span><input v-model="draft.name" class="input" required maxlength="100" /></label>
      <label class="block"><span class="input-label">{{ t('admin.proxyGroups.descriptionLabel') }}</span><textarea v-model="draft.description" class="input" rows="2" /></label>
      <label class="block"><span class="input-label">{{ t('admin.proxyGroups.region') }}</span><input v-model="draft.region" class="input" /></label>
      <label class="block"><span class="input-label">{{ t('admin.proxyGroups.services') }}</span><input v-model="services" class="input" placeholder="grok, chatgpt, gemini, google" /></label>
      <p class="text-sm text-[var(--muted-foreground)]">{{ t('admin.proxyGroups.tagsHint') }}</p>
      <label class="block"><span class="input-label">{{ t('common.status') }}</span><select v-model="draft.status" class="input"><option value="active">{{ t('admin.proxyGroups.active') }}</option><option value="inactive">{{ t('admin.proxyGroups.inactive') }}</option></select></label>
      <label class="block"><span class="input-label">{{ t('admin.proxyGroups.fallback') }}</span><select v-model="draft.fallback_mode" class="input"><option v-for="mode in (['none', 'direct', 'proxy'] as const)" :key="mode" :value="mode">{{ t(`admin.proxyGroups.${mode}`) }}</option></select></label>
      <p v-if="draft.fallback_mode === 'direct'" class="text-sm text-[var(--warn-text)]">{{ t('admin.proxyGroups.directHint') }}</p>
      <div v-if="draft.fallback_mode === 'proxy'" class="space-y-2">
        <ProxySelector :model-value="draft.fallback_proxy_id ?? null" :proxies="proxies" @update:model-value="draft.fallback_proxy_id = $event" />
        <p v-if="draft.fallback_proxy_id" class="text-sm">{{ t('admin.proxyGroups.proxy') }} #{{ draft.fallback_proxy_id }}</p>
        <input class="sr-only" tabindex="-1" :value="draft.fallback_proxy_id || ''" required :aria-label="t('admin.proxyGroups.chooseNode')" />
      </div>
      <p class="text-sm text-[var(--muted-foreground)]">{{ t('admin.proxyGroups.inactiveHint') }}</p>
      <details class="rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-3"><summary class="cursor-pointer text-sm font-medium">{{ t('admin.proxyGroups.advanced') }}</summary><label class="mt-3 block"><span class="input-label">{{ t('admin.proxyGroups.ttl') }}</span><input v-model.number="draft.binding_ttl_seconds" class="input" type="number" min="1" step="1" required /></label></details>
      <p v-if="error" role="alert" class="text-sm text-[var(--bad-text)]">{{ error }}</p>
    </form>
    <template #footer><button class="btn btn-secondary" :disabled="busy" @click="emit('close')">{{ t('common.cancel') }}</button><button class="btn btn-primary" form="proxy-group-editor" type="submit" :disabled="busy || !draft.name.trim() || (draft.fallback_mode === 'proxy' && !draft.fallback_proxy_id)">{{ t('common.save') }}</button></template>
  </BaseDialog>
</template>
