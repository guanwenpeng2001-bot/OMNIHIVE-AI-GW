<template>
  <div class="card oh-quick-card overflow-hidden">
    <div class="oh-quick-header flex items-center justify-between border-b px-5 py-3.5">
      <h2 class="text-base font-semibold">{{ t('dashboard.quickActions') }}</h2>
      <span class="font-mono text-[11px] opacity-50">SHORTCUTS</span>
    </div>
    <div class="px-5">
      <button v-if="canCreateKey" type="button" data-testid="quick-action-create-key" @click="router.push('/keys')" class="oh-action-row flex w-full items-center gap-3 py-4 text-left">
        <Icon name="key" size="md" class="shrink-0 opacity-60" aria-hidden="true" />
        <div class="min-w-0 flex-1">
          <div class="flex items-center justify-between gap-2">
            <p class="text-sm font-bold">{{ t('dashboard.createApiKey') }}</p>
          </div>
          <p class="mt-1 text-xs opacity-60">{{ t('dashboard.generateNewKey') }}</p>
        </div>
        <kbd class="kbd shrink-0">G K</kbd>
        <span class="text-xl opacity-40" aria-hidden="true">›</span>
      </button>

      <button v-if="canViewUsage" type="button" data-testid="quick-action-view-usage" @click="router.push('/usage')" class="oh-action-row flex w-full items-center gap-3 py-4 text-left">
        <Icon name="chart" size="md" class="shrink-0 opacity-60" aria-hidden="true" />
        <div class="min-w-0 flex-1">
          <div class="flex items-center justify-between gap-2">
            <p class="text-sm font-bold">{{ t('dashboard.viewUsage') }}</p>
          </div>
          <p class="mt-1 text-xs opacity-60">{{ t('dashboard.checkDetailedLogs') }}</p>
        </div>
        <kbd class="kbd shrink-0">G U</kbd>
        <span class="text-xl opacity-40" aria-hidden="true">›</span>
      </button>

      <button v-if="canUseBatchImage" type="button" @click="router.push('/batch-image')" class="oh-action-row flex w-full items-center gap-3 py-4 text-left">
        <Icon name="sparkles" size="md" class="shrink-0 opacity-60" aria-hidden="true" />
        <div class="min-w-0 flex-1">
          <div class="flex items-center justify-between gap-2">
            <p class="text-sm font-bold">{{ t('dashboard.batchImageAgent') }}</p>
          </div>
          <p class="mt-1 text-xs opacity-60">{{ t('dashboard.batchImageAgentDesc') }}</p>
        </div>
        <kbd class="kbd shrink-0">G I</kbd>
        <span class="text-xl opacity-40" aria-hidden="true">›</span>
      </button>

      <button type="button" @click="router.push('/redeem')" class="oh-action-row flex w-full items-center gap-3 py-4 text-left">
        <Icon name="gift" size="md" class="shrink-0 opacity-60" aria-hidden="true" />
        <div class="min-w-0 flex-1">
          <div class="flex items-center justify-between gap-2">
            <p class="text-sm font-bold">{{ t('dashboard.redeemCode') }}</p>
          </div>
          <p class="mt-1 text-xs opacity-60">{{ t('dashboard.addBalanceWithCode') }}</p>
        </div>
        <kbd class="kbd shrink-0">G R</kbd>
        <span class="text-xl opacity-40" aria-hidden="true">›</span>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useBatchImageAccess } from '@/composables/useBatchImageAccess'

withDefaults(
  defineProps<{
    canCreateKey?: boolean
    canViewUsage?: boolean
  }>(),
  {
    canCreateKey: true,
    canViewUsage: true,
  }
)

const router = useRouter()
const { t } = useI18n()
const { canUseBatchImage, refreshBatchImageAccess } = useBatchImageAccess()

onMounted(() => {
  void refreshBatchImageAccess()
})
</script>

<style scoped>
.oh-quick-card { color: var(--foreground); background: var(--card); border: 1px solid var(--border); }
.oh-quick-header, .oh-action-row + .oh-action-row { border-color: var(--border); }
.oh-action-row + .oh-action-row { border-top-width: 1px; }
.oh-action-row { transition: background-color .15s; }
.oh-action-row:hover { background: var(--surface-2); }
.oh-action-row:focus-visible { outline: 2px solid var(--brand); outline-offset: 2px; }
.kbd { font-family: var(--font-mono, ui-monospace, monospace); font-size: 11px; white-space: nowrap; padding: 2px 5px; border: 1px solid var(--border); border-radius: 4px; color: var(--muted-foreground); background: transparent; }
@media (prefers-reduced-motion: reduce) { .oh-action-row { transition: none; } }
</style>
