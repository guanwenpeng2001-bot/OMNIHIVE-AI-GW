<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

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
    <div>
      <span id="cursor-key-mode-label" class="input-label">{{ t('admin.accounts.cursor.keyMode') }}</span>
      <div class="mt-2 grid grid-cols-1 gap-3 sm:grid-cols-3" role="group" aria-labelledby="cursor-key-mode-label" data-testid="cursor-key-mode">
        <button
          v-for="option in (['auto', 'official', 'relay'] as const)"
          :key="option"
          type="button"
          :data-testid="`cursor-key-mode-${option}`"
          :disabled="editing"
          :aria-pressed="mode === option"
          :class="[
            'flex items-center gap-3 rounded-lg border-2 p-3 text-left transition-all disabled:cursor-not-allowed disabled:opacity-50',
            mode === option
              ? 'border-primary-500 bg-primary-50 dark:bg-primary-900/20'
              : 'border-gray-200 hover:border-gray-400 dark:border-dark-600 dark:hover:border-gray-600'
          ]"
          @click="mode = option"
        >
          <div :class="[
            'flex h-8 w-8 shrink-0 items-center justify-center rounded-lg',
            mode === option ? 'bg-primary-500 text-white' : 'bg-gray-100 text-gray-500 dark:bg-dark-600 dark:text-gray-400'
          ]">
            <Icon :name="option === 'auto' ? 'bolt' : option === 'official' ? 'key' : 'swap'" size="sm" />
          </div>
          <div>
            <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ t(`admin.accounts.cursor.${option}`) }}</span>
            <span class="text-xs text-gray-500 dark:text-gray-400">{{ t(`admin.accounts.cursor.${option}Desc`) }}</span>
          </div>
        </button>
      </div>
    </div>
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
