<template>
  <label class="block space-y-1" data-testid="tpm-limit-field">
    <span class="input-label">{{ t('admin.accounts.tpmLimit') }}</span>
    <input :value="modelValue" type="number" min="0" step="1" :max="Number.MAX_SAFE_INTEGER"
      required :disabled="disabled" class="input" @input="update" />
    <span class="input-hint block">{{ t('admin.accounts.tpmLimitHint') }}</span>
    <span v-if="modelValue === 0" class="text-sm text-gray-500">{{ t('admin.accounts.tpmUnlimited') }}</span>
  </label>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
defineProps<{ modelValue: number; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: number] }>()
function update(event: Event) {
  const input = event.target as HTMLInputElement
  const value = input.value === '' ? NaN : Number(input.value)
  input.setCustomValidity(Number.isSafeInteger(value) && value >= 0 ? '' : t('admin.accounts.tpmLimitInvalid'))
  emit('update:modelValue', value)
}
</script>
