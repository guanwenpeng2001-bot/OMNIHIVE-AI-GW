<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { compatibleMode, compatibleProtocolLabel, compatibleAuthLabel, type CompatibleProvider } from '@/constants/compatibleProviders'
const props = defineProps<{ provider: CompatibleProvider; modelValue: Record<string, unknown> }>()
const emit = defineEmits<{ 'update:modelValue': [value: Record<string, unknown>] }>()
const { t } = useI18n()
const mode = computed(() => compatibleMode(props.provider, props.modelValue.account_mode))
const protocol = computed(() => String(props.modelValue.api_protocol || 'adaptive'))
const bases = computed(() => (props.modelValue.api_base_urls || {}) as Record<string, string>)
const endpoints = computed(() => mode.value?.endpoints.filter(endpoint => protocol.value === 'adaptive' || endpoint.protocol === protocol.value) ?? [])
function update(key: string, value: unknown) { emit('update:modelValue', { ...props.modelValue, [key]: value }) }
function changeMode(value: string) {
  const next = compatibleMode(props.provider, value)
  const selected = next?.endpoints.some(endpoint => endpoint.protocol === protocol.value) ? protocol.value : 'adaptive'
  emit('update:modelValue', { ...props.modelValue, account_mode: value, api_protocol: selected })
}
function updateBase(protocol: string, value: string) {
  const next = { ...bases.value }
  if (value.trim()) next[protocol] = value.trim()
  else delete next[protocol]
  update('api_base_urls', next)
}
</script>

<template>
  <div class="space-y-4" data-testid="compatible-provider-fields">
    <label class="block">
      <span class="input-label">{{ t('admin.accounts.cnProviders.accountMode.title') }}</span>
      <select class="input" data-testid="compatible-mode" :value="mode?.mode ?? modelValue.account_mode" @change="changeMode(($event.target as HTMLSelectElement).value)">
        <option v-for="entry in provider.modes" :key="entry.mode" :value="entry.mode">{{ t(`admin.accounts.cnProviders.accountMode.${entry.mode}`) }}</option>
      </select>
    </label>
    <label class="block">
      <span class="input-label">{{ t('admin.accounts.cnProviders.apiProtocol.title') }}</span>
      <select class="input" data-testid="compatible-protocol" :value="protocol" @change="update('api_protocol', ($event.target as HTMLSelectElement).value)">
        <option value="adaptive">{{ t('admin.accounts.cnProviders.apiProtocol.adaptive') }}</option>
        <option v-for="endpoint in mode?.endpoints" :key="endpoint.protocol" :value="endpoint.protocol">{{ compatibleProtocolLabel(endpoint.protocol) }}</option>
      </select>
    </label>
    <p class="input-hint">{{ t('admin.accounts.compatible.protocolHint') }}</p>
    <label class="block">
      <span class="input-label">{{ t('admin.accounts.baseUrl') }}</span>
      <input class="input" data-testid="compatible-base" :value="modelValue.base_url || ''" :required="provider.base_source === 'account'" :placeholder="provider.base_source === 'account' ? 'https://resource.example.com' : mode?.endpoints.find(endpoint => endpoint.protocol === (protocol === 'adaptive' ? mode?.default_protocol : protocol))?.base_url" @input="update('base_url', ($event.target as HTMLInputElement).value)" />
    </label>
    <div v-for="endpoint in endpoints" :key="endpoint.protocol" class="space-y-1">
      <label class="block">
        <span class="input-label">{{ compatibleProtocolLabel(endpoint.protocol) }}</span>
        <input class="input" :data-testid="`compatible-base-${endpoint.protocol}`" :value="bases[endpoint.protocol] || ''" :placeholder="String(modelValue.base_url || endpoint.base_url || endpoint.path || '')" @input="updateBase(endpoint.protocol, ($event.target as HTMLInputElement).value)" />
      </label>
      <p class="input-hint break-all">{{ compatibleAuthLabel(endpoint.auth) }}<span v-if="endpoint.path"> · {{ endpoint.path }}</span></p>
    </div>
    <p v-if="mode?.discovery === 'none'" class="input-hint">{{ t('admin.accounts.compatible.manualModels') }}</p>
    <p v-if="provider.base_source === 'account' && mode?.endpoints.some(endpoint => endpoint.auth === 'api_key_header')" class="input-hint" data-testid="compatible-deployment-hint">{{ t('admin.accounts.compatible.deploymentHint') }}</p>
  </div>
</template>
