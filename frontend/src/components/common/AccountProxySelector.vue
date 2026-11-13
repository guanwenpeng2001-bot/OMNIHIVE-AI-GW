<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Proxy } from '@/types/admin'
import ProxySelector from './ProxySelector.vue'
import ProxyGroupSelector from './ProxyGroupSelector.vue'
const props = defineProps<{ proxyId: number | null; proxyGroupId: number | null; proxies: Proxy[]; groupName?: string; disabled?: boolean }>()
const emit = defineEmits<{ 'update:proxyId': [value: number | null]; 'update:proxyGroupId': [value: number | null]; valid: [value: boolean] }>()
const { t } = useI18n()
const mode = ref<'direct' | 'single' | 'group'>('direct')
watch(() => [props.proxyId, props.proxyGroupId], () => {
  if (props.proxyGroupId) mode.value = 'group'
  else if (props.proxyId) mode.value = 'single'
}, { immediate: true })
const valid = computed(() => mode.value === 'direct' || (mode.value === 'single' ? !!props.proxyId : !!props.proxyGroupId))
watch(valid, value => emit('valid', value), { immediate: true })
function changeMode(event: Event) {
  mode.value = (event.target as HTMLSelectElement).value as typeof mode.value
  emit('update:proxyId', null)
  emit('update:proxyGroupId', null)
}
function selectNode(value: number | null) {
  if (!value) mode.value = 'direct'
  emit('update:proxyId', value)
}
</script>

<template>
  <fieldset :disabled="disabled" class="min-w-0 space-y-2 rounded-lg border border-[var(--border)] bg-[var(--card)] p-3">
    <legend class="input-label px-1">{{ t('admin.proxyGroups.egress') }}</legend>
    <select class="input" :value="mode" :aria-label="t('admin.proxyGroups.egress')" @change="changeMode">
      <option value="direct">{{ t('admin.proxyGroups.noProxy') }}</option>
      <option value="single">{{ t('admin.proxyGroups.single') }}</option>
      <option value="group">{{ t('admin.proxyGroups.group') }}</option>
    </select>
    <p v-if="mode === 'direct'" class="text-xs text-[var(--muted-foreground)]">{{ t('admin.proxyGroups.defaultEgress') }}</p>
    <template v-if="mode === 'single'">
      <ProxySelector :model-value="proxyId" :proxies="proxies" :disabled="disabled" @update:model-value="selectNode" />
      <input class="sr-only" tabindex="-1" :value="proxyId || ''" required :aria-label="t('admin.proxyGroups.chooseNode')" />
    </template>
    <template v-if="mode === 'group'">
      <ProxyGroupSelector :model-value="proxyGroupId" :selected-name="groupName" :disabled="disabled" @update:model-value="emit('update:proxyGroupId', $event)" />
      <p class="text-xs text-[var(--muted-foreground)]">{{ t('admin.proxyGroups.groupHint') }}</p>
    </template>
  </fieldset>
</template>
