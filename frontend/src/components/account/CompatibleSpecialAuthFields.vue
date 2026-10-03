<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { changeSpecialCompatibleMode } from './compatibleSpecialAuth'

const { t } = useI18n()
const props = defineProps<{ platform: string; modelValue: Record<string, unknown> }>()
const emit = defineEmits<{ 'update:modelValue': [value: Record<string, unknown>]; 'reset-secret': [] }>()
const proxy = computed(() => props.modelValue.auth_mode === 'official_proxy')
function changeMode(mode: string) {
  emit('update:modelValue', changeSpecialCompatibleMode(props.modelValue, mode))
  emit('reset-secret')
}
function clearProxyKey() {
  emit('update:modelValue', { ...props.modelValue, api_key: '' })
  emit('reset-secret')
}
</script>

<template>
  <div class="space-y-4" data-testid="compatible-special-auth">
    <label v-if="platform === 'nous_research'" class="block">
      <span class="input-label">{{ t('admin.accounts.specialCompatibleAuth.authMode') }}</span>
      <select class="input" data-testid="special-auth-mode" :value="modelValue.auth_mode || ''" @change="changeMode(($event.target as HTMLSelectElement).value)">
        <option disabled value="">{{ t('admin.accounts.specialCompatibleAuth.selectAuthMode') }}</option>
        <option value="invocation_token">{{ t('admin.accounts.specialCompatibleAuth.invocationToken') }}</option>
        <option value="official_proxy">{{ t('admin.accounts.specialCompatibleAuth.officialProxy') }}</option>
      </select>
    </label>
    <p v-if="platform === 'github_copilot'" class="input-hint">{{ t('admin.accounts.specialCompatibleAuth.copilotHint') }}</p>
    <p v-else-if="proxy" class="input-hint">{{ t('admin.accounts.specialCompatibleAuth.proxyHint') }}</p>
    <p v-else class="input-hint">{{ t('admin.accounts.specialCompatibleAuth.invocationHint') }}</p>
    <button v-if="proxy" type="button" class="btn btn-secondary" data-testid="special-auth-clear-key" @click="clearProxyKey">{{ t('admin.accounts.specialCompatibleAuth.clearProxyKey') }}</button>
    <label class="block">
      <span class="input-label">{{ t(proxy ? 'admin.accounts.specialCompatibleAuth.proxyBaseUrl' : 'admin.accounts.specialCompatibleAuth.optionalBaseUrl') }}</span>
      <input class="input" data-testid="special-auth-base" :value="modelValue.base_url || ''" :required="proxy" :placeholder="platform === 'github_copilot' ? 'https://api.githubcopilot.com/v1' : proxy ? 'https://hermes.example.com/v1' : 'https://inference-api.nousresearch.com/v1'" @input="emit('update:modelValue', { ...modelValue, base_url: ($event.target as HTMLInputElement).value })" />
    </label>
    <p class="input-hint">{{ t('admin.accounts.specialCompatibleAuth.protocolHint') }}</p>
  </div>
</template>
