<template>
  <section class="space-y-3 rounded-lg border border-[var(--border)] bg-[var(--card)] p-3 text-[var(--foreground)]" data-testid="group-pricing-card">
    <template v-if="isReadOnlyGroupPricing(entry)">
      <div class="rounded-md bg-[var(--surface-2)] p-3">
        <div class="flex items-center justify-between gap-2">
          <span class="text-sm font-medium">{{ entry.platform }} · {{ entry.models.join(', ') }}</span>
          <div v-if="confirmingDelete" class="flex items-center gap-2">
            <button
              type="button"
              class="btn btn-danger"
              data-testid="confirm-remove-pricing-card"
              @click="confirmRemove"
            >
              {{ t('common.confirm') }}
            </button>
            <button
              type="button"
              class="btn btn-secondary"
              data-testid="cancel-remove-pricing-card"
              @click="confirmingDelete = false"
            >
              {{ t('common.cancel') }}
            </button>
          </div>
          <button
            v-else
            type="button"
            class="btn btn-secondary"
            data-testid="remove-pricing-card"
            @click="confirmingDelete = true"
          >
            {{ t('common.delete') }}
          </button>
        </div>
        <p
          v-if="confirmingDelete"
          data-testid="remove-pricing-card-confirm-hint"
          class="mt-2 rounded border border-amber-300 bg-amber-50 px-2.5 py-1.5 text-xs text-amber-800 dark:border-amber-700/60 dark:bg-amber-900/20 dark:text-amber-300"
        >
          {{ pricingText('pricingSummary.confirmDelete') }}
        </p>
        <p class="mt-2 text-xs text-[var(--muted-foreground)]">{{ pricingText('pricingSummary.readOnly') }}</p>
        <p class="mt-1 text-xs">{{ summary }}</p>
        <details class="mt-2 text-xs">
          <summary>{{ pricingText('pricingSummary.details') }}</summary>
          <pre class="mt-2 max-h-64 overflow-auto whitespace-pre-wrap break-all rounded-md border border-[var(--border)] bg-[var(--card)] p-3 font-mono">{{ JSON.stringify(entry.original, null, 2) }}</pre>
        </details>
      </div>
    </template>
    <template v-else>
      <select v-if="platform === 'composite' && !entry.original" :value="entry.platform" class="input" aria-label="Pricing provider" @change="emit('update', { ...entry, platform: ($event.target as HTMLSelectElement).value })">
        <option value="" disabled>{{ pricingText('pricingSummary.provider') }}</option>
        <option v-for="option in CONCRETE_PLATFORM_OPTIONS" :key="option.value" :value="option.value">{{ getPlatformLabel(option.value, t) }}</option>
      </select>
      <p v-else class="pill pill-ghost">{{ entry.platform || platform }}</p>
      <PricingEntryCard :entry="entry" :platform="entry.platform || platform" :disable-default-pricing="platform === 'composite'" hide-token-intervals @update="emit('update', { ...entry, ...$event })" @remove="emit('remove')" />
    </template>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { computed, ref, watch } from 'vue'
import PricingEntryCard from '@/components/admin/channel/PricingEntryCard.vue'
import { CONCRETE_PLATFORM_OPTIONS, getPlatformLabel } from '@/constants/platforms'
import { isReadOnlyGroupPricing, type GroupPricingEntry } from './groupsModelPricing'
const props = defineProps<{ entry: GroupPricingEntry, platform: string }>()
const emit = defineEmits<{ update: [entry: GroupPricingEntry], remove: [] }>()
const { t } = useI18n()
const confirmingDelete = ref(false)
watch(() => props.entry, () => {
  confirmingDelete.value = false
})
function confirmRemove() {
  confirmingDelete.value = false
  emit('remove')
}
const { t: pricingText } = useI18n({
  useScope: 'local',
  messages: {
    en: { pricingSummary: {
      readOnly: 'This card contains advanced pricing. Its prices and conditions are preserved on save; delete the card to remove it.',
      confirmDelete: 'Deleting this advanced pricing card cannot be undone after saving. Confirm deletion?',
      details: 'View complete pricing (read only)', provider: 'Select pricing provider',
      intervals: 'Context tiers', thinking: 'Thinking', cache: 'Explicit cache', batch: 'Batch', source: 'Source',
    } },
    zh: { pricingSummary: {
      readOnly: '此卡包含高级定价，保存时原样保留价格和条件；若需移除，请删除该卡。',
      confirmDelete: '删除此高级价卡后，保存时将移除全部分段与高级条件且无法自动还原，确认删除吗？',
      details: '查看完整价卡（只读）', provider: '选择价卡供应商',
      intervals: '上下文档位', thinking: '思考模式', cache: '显式缓存', batch: 'Batch', source: '来源',
    } },
  },
})
const summary = computed(() => {
  const original = props.entry.original
  if (!original) return ''
  return [
    original.native_pricing_source && `${pricingText('pricingSummary.source')}: ${original.native_pricing_source}`,
    original.intervals?.length && `${pricingText('pricingSummary.intervals')}: ${original.intervals.length}`,
    original.thinking_pricing && pricingText('pricingSummary.thinking'),
    original.explicit_cache_pricing && pricingText('pricingSummary.cache'),
    original.batch_chat_pricing && pricingText('pricingSummary.batch'),
  ].filter(Boolean).join(' · ')
})
</script>
