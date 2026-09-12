<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{ editing?: boolean }>()
const mode = defineModel<'official' | 'relay' | 'auto'>('mode', { required: true })
const autoCredentials = defineModel<Record<string, string>>('autoCredentials', { default: () => ({}) })
const apiKey = defineModel<string>('apiKey', { required: true })
const baseUrl = defineModel<string>('baseUrl', { required: true })
const clientVersion = defineModel<string>('clientVersion', { required: true })
const { t } = useI18n()
</script>

<template>
  <fieldset class="space-y-4">
    <label class="block">
      <span class="input-label">{{ t('admin.accounts.cursor.keyMode') }}</span>
      <select v-model="mode" class="input" :disabled="editing" data-testid="cursor-key-mode">
        <option value="auto">{{ t('admin.accounts.cursor.auto') }}</option>
        <option value="official">{{ t('admin.accounts.cursor.official') }}</option>
        <option value="relay">{{ t('admin.accounts.cursor.relay') }}</option>
      </select>
    </label>
    <div v-if="mode === 'auto'" class="space-y-3">
      <p class="input-hint">{{ t('admin.accounts.cursor.autoHint') }}</p>
      <label v-for="field in ['access_token', 'refresh_token', 'machine_id', 'mac_machine_id']" :key="field" class="block">
        <span class="input-label">{{ field }}</span>
        <input v-model="autoCredentials[field]" :type="field.endsWith('token') ? 'password' : 'text'" autocomplete="new-password" class="input" :data-testid="`cursor-${field}`" :placeholder="editing && field.endsWith('token') ? t('admin.accounts.cursor.keepKey') : field.endsWith('id') ? t('admin.accounts.cursor.machineFormat') : ''" />
      </label>
    </div>
    <label v-else class="block">
      <span class="input-label">API Key</span>
      <input v-model="apiKey" type="password" autocomplete="new-password" class="input" :required="!editing" :placeholder="editing ? t('admin.accounts.cursor.keepKey') : 'crsr_…'" data-testid="cursor-api-key" />
    </label>
    <label v-if="mode === 'relay'" class="block">
      <span class="input-label">Base URL</span>
      <input v-model="baseUrl" type="url" required class="input" placeholder="https://relay.example/v1" data-testid="cursor-base-url" />
      <span class="input-hint">{{ t('admin.accounts.cursor.relayHint') }}</span>
    </label>
    <label v-else class="block">
      <span class="input-label">{{ t('admin.accounts.cursor.clientVersion') }}</span>
      <input v-model="clientVersion" class="input" :placeholder="t('admin.accounts.cursor.globalDefault')" />
      <span class="input-hint">{{ t('admin.accounts.cursor.nativeHint') }}</span>
    </label>
    <p v-if="mode !== 'relay'" class="input-hint" data-testid="cursor-native-settings-hint">{{ t('admin.accounts.cursor.nativeSettingsHint') }}</p>
  </fieldset>
</template>
