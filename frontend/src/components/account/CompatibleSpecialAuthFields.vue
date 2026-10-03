<script setup lang="ts">
import { computed } from 'vue'
import { changeSpecialCompatibleMode } from './compatibleSpecialAuth'

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
      <span class="input-label">Authentication mode</span>
      <select class="input" data-testid="special-auth-mode" :value="modelValue.auth_mode || ''" @change="changeMode(($event.target as HTMLSelectElement).value)">
        <option disabled value="">Select authentication mode</option>
        <option value="invocation_token">Invocation token</option>
        <option value="official_proxy">Official proxy</option>
      </select>
    </label>
    <p v-if="platform === 'github_copilot'" class="input-hint">Use a GitHub fine-grained PAT in the API key field. The gateway identifies itself as OmniHive; upstream admission and entitlement are determined by GitHub.</p>
    <p v-else-if="proxy" class="input-hint">Enter the official proxy prefix, including /v1 if configured. The API key field is an optional proxy frontdoor bearer. Leave it empty for a proxy without frontdoor authentication.</p>
    <p v-else class="input-hint">Use an invocation JWT in the API key field. It must contain inference:invoke and a future expiry. Replace it when expired; automatic refresh is unsupported.</p>
    <button v-if="proxy" type="button" class="btn btn-secondary" data-testid="special-auth-clear-key" @click="clearProxyKey">Clear proxy frontdoor key</button>
    <label class="block">
      <span class="input-label">{{ proxy ? 'Proxy base URL (required)' : 'Base URL (optional)' }}</span>
      <input class="input" data-testid="special-auth-base" :value="modelValue.base_url || ''" :required="proxy" :placeholder="platform === 'github_copilot' ? 'https://api.githubcopilot.com/v1' : proxy ? 'http://127.0.0.1:8645/v1' : 'https://inference-api.nousresearch.com/v1'" @input="emit('update:modelValue', { ...modelValue, base_url: ($event.target as HTMLInputElement).value })" />
    </label>
    <p class="input-hint">Chat Completions upstream · Messages, Responses and Gemini through gateway bridges. Routing mode is fixed; it does not describe a vendor plan or billing method.</p>
  </div>
</template>
