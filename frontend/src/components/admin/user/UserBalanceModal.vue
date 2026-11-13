<template>
  <BaseDialog :show="show" :title="activeOperation === 'add' ? t('admin.users.deposit') : t('admin.users.withdraw')" width="narrow" @close="$emit('close')">
    <form v-if="user" id="balance-form" @submit.prevent="handleBalanceSubmit" class="space-y-5">
      <div class="flex items-center gap-3 rounded-xl border border-[var(--border)] bg-[color-mix(in_srgb,var(--surface-2)_40%,transparent)] p-4">
        <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border border-[var(--border)] bg-[var(--card)]"><span class="text-lg font-medium text-[var(--foreground)]">{{ user.email.charAt(0).toUpperCase() }}</span></div>
        <div class="min-w-0 flex-1"><p class="break-all font-medium text-[var(--foreground)]">{{ user.email }}</p><p class="mt-1 text-xs text-[var(--muted-foreground)]">{{ t('admin.users.currentBalance') }}: <span class="font-mono tabular-nums">${{ formatBalance(user.balance) }}</span></p></div>
      </div>
      <div class="segmented" role="group" :aria-label="t('admin.users.columns.balance')">
        <button type="button" :class="{ on: activeOperation === 'add' }" :aria-pressed="activeOperation === 'add'" :disabled="submitting" @click="activeOperation = 'add'">{{ t('admin.users.deposit') }}</button>
        <button type="button" :class="{ on: activeOperation === 'subtract' }" :aria-pressed="activeOperation === 'subtract'" :disabled="submitting" @click="activeOperation = 'subtract'">{{ t('admin.users.withdraw') }}</button>
      </div>
      <div class="space-y-4 rounded-xl border border-[var(--border)] bg-[color-mix(in_srgb,var(--surface-2)_40%,transparent)] p-4">
      <div>
        <label class="input-label">{{ activeOperation === 'add' ? t('admin.users.depositAmount') : t('admin.users.withdrawAmount') }}</label>
        <div class="relative flex flex-wrap gap-2">
          <div class="relative min-w-0 flex-1"><div class="absolute left-3 top-1/2 -translate-y-1/2 font-medium text-[var(--muted-foreground)]">$</div><input v-model.number="form.amount" type="number" step="any" min="0" required class="input pl-8 font-mono tabular-nums" /></div>
          <button v-if="activeOperation === 'subtract'" type="button" @click="fillAllBalance" class="btn btn-secondary whitespace-nowrap">{{ t('admin.users.withdrawAll') }}</button>
        </div>
      </div>
      <div><label class="input-label">{{ t('admin.users.notes') }}</label><textarea v-model="form.notes" rows="3" class="input"></textarea></div>
      </div>
      <div v-if="form.amount > 0" class="rounded-xl border border-[var(--border)] bg-[color-mix(in_srgb,var(--surface-2)_40%,transparent)] p-4" aria-live="polite"><div class="flex items-center justify-between gap-3 text-sm"><span class="text-[var(--muted-foreground)]">{{ t('admin.users.newBalance') }}:</span><span class="font-mono font-semibold tabular-nums text-[var(--foreground)]">${{ formatBalance(calculateNewBalance()) }}</span></div></div>
    </form>
    <template #footer>
      <div class="flex flex-wrap justify-end gap-2">
        <button @click="$emit('close')" class="btn btn-secondary">{{ t('common.cancel') }}</button>
        <button type="submit" form="balance-form" :disabled="submitting || !form.amount" class="btn btn-primary">{{ submitting ? t('common.saving') : t('common.confirm') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { extractApiErrorMessage } from '@/utils/apiError'

import { reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { AdminUser } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'

const props = defineProps<{ show: boolean, user: AdminUser | null, operation: 'add' | 'subtract' }>()
const emit = defineEmits(['close', 'success']); const { t } = useI18n(); const appStore = useAppStore()

const submitting = ref(false); const form = reactive({ amount: 0, notes: '' })
const activeOperation = ref(props.operation)
watch(() => props.operation, (operation) => { activeOperation.value = operation })
watch(() => props.show, (v) => { if(v) { form.amount = 0; form.notes = ''; activeOperation.value = props.operation } })

// 格式化余额：显示完整精度，去除尾部多余的0
const formatBalance = (value: number) => {
  if (value === 0) return '0.00'
  // 最多保留8位小数，去除尾部的0
  const formatted = value.toFixed(8).replace(/\.?0+$/, '')
  // 确保至少有2位小数
  const parts = formatted.split('.')
  if (parts.length === 1) return formatted + '.00'
  if (parts[1].length === 1) return formatted + '0'
  return formatted
}

// 填入全部余额
const fillAllBalance = () => {
  if (props.user) {
    form.amount = props.user.balance
  }
}

const calculateNewBalance = () => {
  if (!props.user) return 0
  const result = activeOperation.value === 'add' ? props.user.balance + form.amount : props.user.balance - form.amount
  // 避免浮点数精度问题导致的 -0.00 显示
  return Math.abs(result) < 1e-10 ? 0 : result
}
const handleBalanceSubmit = async () => {
  if (!props.user) return
  if (!form.amount || form.amount <= 0) {
    appStore.showError(t('admin.users.amountRequired'))
    return
  }
  // 退款时验证金额不超过实际余额
  if (activeOperation.value === 'subtract' && form.amount > props.user.balance) {
    appStore.showError(t('admin.users.insufficientBalance'))
    return
  }
  submitting.value = true
  try {
    await adminAPI.users.updateBalance(props.user.id, form.amount, activeOperation.value, form.notes)
    appStore.showSuccess(t('common.success')); emit('success'); emit('close')
  } catch (e: any) {
    console.error('Failed to update balance:', e)
    appStore.showError(extractApiErrorMessage(e, '') || t('common.error'))
  } finally { submitting.value = false }
}
</script>
