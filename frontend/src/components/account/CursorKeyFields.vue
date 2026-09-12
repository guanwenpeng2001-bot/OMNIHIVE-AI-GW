<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{ editing?: boolean }>()
const mode = defineModel<'official' | 'relay'>('mode', { required: true })
const apiKey = defineModel<string>('apiKey', { required: true })
const baseUrl = defineModel<string>('baseUrl', { required: true })
const clientVersion = defineModel<string>('clientVersion', { required: true })
const { t } = useI18n()
</script>

<template>
  <fieldset class="space-y-4 rounded-lg border border-gray-200 p-4 dark:border-dark-600">
    <legend class="px-2 text-sm font-medium">Cursor Key</legend>
    <label class="block">
      <span class="input-label">{{ t('admin.accounts.cursor.keyMode') }}</span>
      <select v-model="mode" class="input" :disabled="editing" data-testid="cursor-key-mode">
        <option value="official">{{ t('admin.accounts.cursor.official') }}</option>
        <option value="relay">{{ t('admin.accounts.cursor.relay') }}</option>
      </select>
    </label>
    <label class="block">
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
    <p class="input-hint">{{ t('admin.accounts.cursor.m1Hint') }}</p>
  </fieldset>
</template>
