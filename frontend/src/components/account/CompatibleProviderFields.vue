<script setup lang="ts">
import { computed } from 'vue'
import AccountChoiceCards from './AccountChoiceCards.vue'
import CompatiblePlanFields from './CompatiblePlanFields.vue'
import CompatibleSpecialAuthFields from './CompatibleSpecialAuthFields.vue'
import { isSpecialCompatibleProvider } from './compatibleSpecialAuth'
import { useI18n } from 'vue-i18n'
import { compatibleMode, compatibleProtocolLabel, compatibleAuthLabel, type CompatibleProvider } from '@/constants/compatibleProviders'
const props = defineProps<{ provider: CompatibleProvider; modelValue: Record<string, unknown> }>()
const emit = defineEmits<{ 'update:modelValue': [value: Record<string, unknown>]; 'reset-secret': [] }>()
const { t, locale } = useI18n()
const mode = computed(() => compatibleMode(props.provider, props.modelValue.account_mode))
const protocol = computed(() => String(props.modelValue.api_protocol || 'adaptive'))
const bases = computed(() => (props.modelValue.api_base_urls || {}) as Record<string, string>)
const endpoints = computed(() => mode.value?.endpoints.filter(endpoint => protocol.value === 'adaptive' || endpoint.protocol === protocol.value) ?? [])
const protocolIcons = { adaptive: 'swap', chat_completions: 'chat', anthropic: 'sparkles', responses: 'terminal' } as const
const protocolOptions = computed(() => ['adaptive', ...(mode.value?.endpoints.map(endpoint => endpoint.protocol) ?? [])].map(value => ({
  value,
  label: t(`admin.accounts.cnProviders.apiProtocol.${value === 'chat_completions' ? 'chatCompletions' : value}`),
  description: t(`admin.accounts.supplier.protocolDescriptions.${value}`),
  icon: protocolIcons[value as keyof typeof protocolIcons]
})))
function modeLabel(entry: { mode: string; label?: string; label_zh?: string }) {
  return (locale?.value.startsWith('zh') ? entry.label_zh || entry.label : entry.label) || t(`admin.accounts.cnProviders.accountMode.${entry.mode}`)
}
function update(key: string, value: unknown) { emit('update:modelValue', { ...props.modelValue, [key]: value }) }
function changeMode(value: string) {
  const next = compatibleMode(props.provider, value)
  const selected = next?.endpoints.some(endpoint => endpoint.protocol === protocol.value) ? protocol.value : 'adaptive'
  emit('update:modelValue', { ...props.modelValue, account_mode: value, api_protocol: selected })
}
function updateBase(protocol: string, value: string) {
  const next = { ...bases.value }
  // An explicit empty override clears the stored URL through partial map merges.
  // Untouched preset placeholders never call this handler and remain absent.
  next[protocol] = value.trim()
  update('api_base_urls', next)
}
</script>

<template>
  <div class="space-y-4 rounded-lg border border-[var(--border)] bg-[var(--card)] p-3 [&_input]:font-mono" data-testid="compatible-provider-fields">
    <CompatiblePlanFields v-if="provider.plans?.length && provider.modes.length === 1" :provider="provider" :model-value="modelValue" @update:model-value="emit('update:modelValue', $event)" />
    <CompatibleSpecialAuthFields v-if="isSpecialCompatibleProvider(provider.id)" :platform="provider.id" :model-value="modelValue" @update:model-value="emit('update:modelValue', $event)" @reset-secret="emit('reset-secret')" />
    <template v-else>
      <AccountChoiceCards v-if="provider.modes.length > 1" data-testid="compatible-mode"
        :label="t('admin.accounts.supplier.planType')" :model-value="mode?.mode"
        :options="provider.modes.map(entry => ({ value: entry.mode, label: modeLabel(entry), icon: entry.mode === 'payg' ? 'creditCard' : 'bolt', description: t(`admin.accounts.supplier.planDescriptions.${entry.mode === 'payg' ? 'payg' : 'subscription'}`) }))"
        @update:model-value="changeMode" />
      <p v-if="provider.modes.length > 1" class="input-hint">{{ t('admin.accounts.supplier.planKeyHint') }}</p>
      <AccountChoiceCards data-testid="compatible-protocol" :label="t('admin.accounts.cnProviders.apiProtocol.title')" :model-value="protocol"
        :options="protocolOptions" :columns="3"
        @update:model-value="update('api_protocol', $event)" />
      <p class="input-hint">{{ t('admin.accounts.compatible.protocolHint') }}</p>
      <label class="block">
        <span class="input-label">{{ t('admin.accounts.baseUrl') }}</span>
        <input class="input" data-testid="compatible-base" :value="modelValue.base_url || ''" :required="provider.base_source === 'account'" :placeholder="provider.base_source === 'account' ? 'https://resource.example.com' : mode?.endpoints.find(endpoint => endpoint.protocol === (protocol === 'adaptive' ? mode?.default_protocol : protocol))?.base_url" @input="update('base_url', ($event.target as HTMLInputElement).value)" />
      </label>
      <div v-for="endpoint in endpoints" :key="endpoint.protocol" class="space-y-1 border-t border-[var(--border)] pt-3">
        <label class="block">
          <span class="input-label">{{ compatibleProtocolLabel(endpoint.protocol) }}</span>
          <input class="input" :data-testid="`compatible-base-${endpoint.protocol}`" :value="bases[endpoint.protocol] || ''" :placeholder="String(modelValue.base_url || endpoint.base_url || endpoint.path || '')" @input="updateBase(endpoint.protocol, ($event.target as HTMLInputElement).value)" />
        </label>
        <p class="input-hint break-all">{{ compatibleAuthLabel(endpoint.auth) }}<span v-if="endpoint.path"> · {{ endpoint.path }}</span></p>
      </div>
    </template>
    <p v-if="mode?.discovery === 'none'" class="input-hint">{{ t('admin.accounts.compatible.manualModels') }}</p>
    <p v-if="provider.base_source === 'account' && mode?.endpoints.some(endpoint => endpoint.auth === 'api_key_header')" class="input-hint" data-testid="compatible-deployment-hint">{{ t('admin.accounts.compatible.deploymentHint') }}</p>
  </div>
</template>
