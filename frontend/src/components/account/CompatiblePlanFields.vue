<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AccountChoiceCards from './AccountChoiceCards.vue'
import type { CompatibleProvider } from '@/constants/compatibleProviders'

const props = defineProps<{ provider: CompatibleProvider; modelValue: Record<string, unknown> }>()
const emit = defineEmits<{ 'update:modelValue': [value: Record<string, unknown>] }>()
const { t, locale } = useI18n()
const options = computed(() => (props.provider.plans ?? []).map(plan => ({
  value: plan.id,
  label: locale?.value?.startsWith('zh') ? plan.label_zh : plan.label,
  icon: plan.query === 'payg' ? 'creditCard' as const : 'bolt' as const
})))
const selected = computed(() => options.value.find(plan => plan.value === props.modelValue.billing_plan)?.value)
function select(value: string | null) {
  emit('update:modelValue', { ...props.modelValue, billing_plan: value })
}
</script>

<template>
  <div class="space-y-2">
    <AccountChoiceCards data-testid="compatible-plan" :label="t('admin.accounts.supplier.planType')"
      :model-value="selected" :options="options" :columns="options.length > 4 ? 3 : 2"
      @update:model-value="select" />
    <button v-if="modelValue.billing_plan != null && modelValue.billing_plan !== ''" type="button"
      data-testid="compatible-plan-clear" class="text-xs text-primary-600 underline underline-offset-2 dark:text-primary-400"
      @click="select(null)">{{ t('admin.accounts.compatiblePlans.clear') }}</button>
    <p class="input-hint">{{ t('admin.accounts.compatiblePlans.hint') }}</p>
    <p v-if="provider.id === 'byteplus'" class="input-hint">{{ t('admin.accounts.compatiblePlans.byteplusHint') }}</p>
  </div>
</template>
