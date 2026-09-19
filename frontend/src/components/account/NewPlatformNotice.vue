<template>
  <div v-if="platform === 'qianwen' || platform === 'commandcode' || platform === 'bedrock' || isChatOnlyPlatform(platform)" class="space-y-2 rounded-lg border border-primary-200 bg-primary-50 p-3 text-sm dark:border-primary-800 dark:bg-primary-900/20" data-testid="new-platform-notice">
    <p>{{ t(`admin.accounts.newPlatforms.${platform}Description`) }}</p>
    <p>{{ t('admin.accounts.newPlatforms.localUsage') }}</p>
    <p v-if="platform === 'commandcode'">{{ t('admin.accounts.newPlatforms.commandcodeNote') }}</p>
    <p v-if="platform === 'qianwen' && apiKey.trim().startsWith('sk-sp-')" class="font-medium text-amber-800 dark:text-amber-300" role="alert" data-testid="qianwen-token-plan-warning">{{ t('admin.accounts.newPlatforms.tokenPlanWarning') }}</p>
  </div>
</template>

<script setup lang="ts">
import { isChatOnlyPlatform } from '@/constants/platforms'
import { useI18n } from 'vue-i18n'
defineProps<{ platform: string; apiKey: string }>()
const { t } = useI18n()
</script>
