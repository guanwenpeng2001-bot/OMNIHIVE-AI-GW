<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AccountChoiceCards from './AccountChoiceCards.vue'
import { resolveMember, type SupplierFamily, type SupplierSelection, type SupplierAxis } from '@/constants/supplierFamilies'
import { compatibleProvider, compatibleMode } from '@/constants/compatibleProviders'
const props = defineProps<{ supplier: SupplierFamily; modelValue: SupplierSelection; locked?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: SupplierSelection] }>()
const { t } = useI18n()
const member = computed(() => resolveMember(props.supplier, props.modelValue))
const host = computed(() => {
  const provider = compatibleProvider(member.value?.platform ?? '')
  const mode = provider && compatibleMode(provider)
  return mode?.endpoints.find(endpoint => endpoint.protocol === mode.default_protocol)?.base_url
})
function options(axis: SupplierAxis) {
  return [...new Set(props.supplier.members.map(entry => entry.selection[axis]!))].map(value => {
    return { value, label: t(`admin.accounts.supplier.choices.${value}`) }
  })
}
</script>

<template>
  <div class="space-y-4 rounded-lg border border-[var(--border)] bg-[var(--card)] p-3" data-testid="supplier-variants">
    <AccountChoiceCards v-for="axis in supplier.axes" :key="axis" :data-testid="`supplier-${axis}`"
      :label="t(`admin.accounts.supplier.${axis}`)" :options="options(axis)" :model-value="modelValue[axis]" :locked="locked"
      @update:model-value="emit('update:modelValue', { ...modelValue, [axis]: $event })" />
    <p v-if="locked" class="input-hint" data-testid="supplier-locked">{{ t('admin.accounts.supplier.locked') }}</p>
    <p v-else-if="!member" role="status" class="input-hint">{{ t('admin.accounts.supplier.chooseVariant') }}</p>
    <p v-if="host" class="input-hint break-all rounded-md border border-[var(--border)] bg-[var(--surface-2)] px-2 py-1 font-mono text-[11px]" data-testid="supplier-endpoint">{{ host }}</p>
  </div>
</template>
