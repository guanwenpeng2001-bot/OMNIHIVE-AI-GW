<template>
  <div
    :class="[
      'group relative rounded-lg border border-[var(--border)] transition-colors',
      enabled ? 'bg-[var(--card)]' : 'bg-[var(--surface-2)] opacity-50',
    ]"
    :title="!enabled ? t('admin.settings.payment.typeDisabled') + ' — ' + t('admin.settings.payment.enableTypesFirst') : undefined"
  >
    <div :class="[
      'flex flex-wrap items-center justify-between gap-4 px-4 py-3',
      !enabled && 'pointer-events-none',
    ]">
      <!-- Left: icon + name + key badge + type badges -->
      <div class="flex min-w-0 flex-wrap items-center gap-3">
        <div :class="[
          'rounded-md border border-[var(--border)] bg-[var(--surface-2)] p-1.5',
          provider.enabled && enabled ? 'text-[var(--foreground)]' : 'text-[var(--muted-foreground)]',
        ]">
          <Icon
            name="server"
            size="sm"
            :class="provider.enabled && enabled ? 'text-green-600 dark:text-green-400' : 'text-[var(--muted-foreground)]'"
          />
        </div>
        <span class="text-sm font-medium text-[var(--foreground)]">{{ provider.name }}</span>
        <span class="text-xs text-[var(--muted-foreground)]">{{ keyLabel }}</span>
        <span v-if="provider.payment_mode" class="text-xs text-[var(--muted-foreground)]">· {{ modeLabel }}</span>
        <span v-if="enabled && availableTypes.length" class="text-xs text-[var(--muted-foreground)]">|</span>
        <div v-if="enabled" class="flex items-center gap-1">
          <button
            v-for="pt in availableTypes"
            :key="pt.value"
            type="button"
            @click="emit('toggleType', pt.value)"
            :class="[
              'rounded px-2 py-0.5 text-xs font-medium transition-all',
              isSelected(pt.value)
                ? 'bg-primary-500 text-white'
                : 'bg-[var(--secondary)] text-[var(--muted-foreground)]',
            ]"
          >{{ pt.label }}</button>
        </div>
      </div>

      <!-- Right: toggles + actions -->
      <div class="flex flex-wrap items-center gap-4">
        <ToggleSwitch :label="t('common.enabled')" :checked="provider.enabled" @toggle="emit('toggleField', 'enabled')" />
        <ToggleSwitch :label="t('admin.settings.payment.refundEnabled')" :checked="provider.refund_enabled" @toggle="emit('toggleField', 'refund_enabled')" />
        <ToggleSwitch v-if="provider.refund_enabled" :label="t('admin.settings.payment.allowUserRefund')" :checked="provider.allow_user_refund" @toggle="emit('toggleField', 'allow_user_refund')" />
        <div class="flex items-center gap-2 border-l border-[var(--border)] pl-3">
          <button type="button" @click="emit('edit')" class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-[var(--muted-foreground)] transition-colors hover:bg-[var(--secondary)] hover:text-[var(--brand-text)]">
            <Icon name="edit" size="sm" />
            <span class="text-xs">{{ t('common.edit') }}</span>
          </button>
          <button type="button" @click="emit('delete')" class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-[var(--muted-foreground)] transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400">
            <Icon name="trash" size="sm" />
            <span class="text-xs">{{ t('common.delete') }}</span>
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import ToggleSwitch from './ToggleSwitch.vue'
import type { ProviderInstance } from '@/types/payment'
import type { TypeOption } from './providerConfig'
import { PAYMENT_MODE_QRCODE, PAYMENT_MODE_POPUP, PAYMENT_MODE_REDIRECT } from './providerConfig'

const PROVIDER_KEY_LABELS: Record<string, string> = {
  easypay: 'admin.settings.payment.providerEasypay',
  alipay: 'admin.settings.payment.providerAlipay',
  wxpay: 'admin.settings.payment.providerWxpay',
  stripe: 'admin.settings.payment.providerStripe',
  airwallex: 'admin.settings.payment.providerAirwallex',
}

const props = defineProps<{
  provider: ProviderInstance
  enabled: boolean
  availableTypes: TypeOption[]
}>()

const emit = defineEmits<{
  toggleField: [field: 'enabled' | 'refund_enabled' | 'allow_user_refund']
  toggleType: [type: string]
  edit: []
  delete: []
}>()

const { t } = useI18n()

const keyLabel = computed(() => t(PROVIDER_KEY_LABELS[props.provider.provider_key] || props.provider.provider_key))

const modeLabel = computed(() => {
  if (props.provider.payment_mode === PAYMENT_MODE_QRCODE) return t('admin.settings.payment.modeQRCode')
  if (props.provider.payment_mode === PAYMENT_MODE_POPUP) return t('admin.settings.payment.modePopup')
  if (props.provider.payment_mode === PAYMENT_MODE_REDIRECT) return t('admin.settings.payment.modeRedirect')
  return ''
})

function isSelected(type: string): boolean {
  return Array.isArray(props.provider.supported_types) && props.provider.supported_types.includes(type)
}
</script>
