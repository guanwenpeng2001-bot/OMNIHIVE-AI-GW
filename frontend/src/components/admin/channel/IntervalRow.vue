<template>
  <div class="flex flex-wrap items-start gap-2 rounded-lg border p-3 text-[var(--foreground)]"
       :class="isEmpty ? 'border-red-400 bg-[var(--surface-2)] dark:border-red-500' : 'border-[var(--border)] bg-[var(--card)]'">
    <!-- Token mode: context range + prices ($/MTok) -->
    <template v-if="mode === 'token'">
      <div class="pricing-interval-grid grid min-w-0 flex-1 gap-2">
        <div>
          <label class="text-xs text-[var(--muted-foreground)]">{{ t('admin.channels.form.minTokens') }}</label>
          <input :value="interval.min_tokens" @input="emitField('min_tokens', toInt(($event.target as HTMLInputElement).value))"
            type="number" min="0" class="input tabular-nums mt-0.5 text-xs" />
        </div>
        <div>
          <label class="text-xs text-[var(--muted-foreground)]">{{ t('admin.channels.form.maxTokens') }} <span class="text-[var(--muted-foreground)]">{{ t('admin.channels.form.inclusive') }}</span></label>
          <input :value="interval.max_tokens ?? ''" @input="emitField('max_tokens', toIntOrNull(($event.target as HTMLInputElement).value))"
            type="number" min="0" class="input tabular-nums mt-0.5 text-xs" :placeholder="'∞'" />
        </div>
        <div>
          <label class="text-xs text-[var(--muted-foreground)]">{{ t('admin.channels.form.inputPrice') }} <span v-if="isEmpty" class="text-red-500">*</span> <span class="text-[var(--muted-foreground)]">$/M</span></label>
          <input :value="interval.input_price" @input="emitField('input_price', ($event.target as HTMLInputElement).value)"
            type="number" step="any" min="0" class="input tabular-nums mt-0.5 text-xs" />
        </div>
        <div>
          <label class="text-xs text-[var(--muted-foreground)]">{{ t('admin.channels.form.outputPrice') }} <span v-if="isEmpty" class="text-red-500">*</span> <span class="text-[var(--muted-foreground)]">$/M</span></label>
          <input :value="interval.output_price" @input="emitField('output_price', ($event.target as HTMLInputElement).value)"
            type="number" step="any" min="0" class="input tabular-nums mt-0.5 text-xs" />
        </div>
        <div>
          <label class="text-xs text-[var(--muted-foreground)]">{{ t('admin.channels.form.cacheWrite5mPriceShort') }} <span class="text-[var(--muted-foreground)]">$/M</span></label>
          <input :value="interval.cache_write_price" @input="emitField('cache_write_price', ($event.target as HTMLInputElement).value)"
            type="number" step="any" min="0" class="input tabular-nums mt-0.5 text-xs" />
        </div>
        <div>
          <label class="text-xs text-[var(--muted-foreground)]">{{ t('admin.channels.form.cacheWrite1hPriceShort') }} <span class="text-[var(--muted-foreground)]">$/M</span></label>
          <input :value="interval.cache_write_1h_price" @input="emitField('cache_write_1h_price', ($event.target as HTMLInputElement).value)"
            type="number" step="any" min="0" class="input tabular-nums mt-0.5 text-xs" />
        </div>
        <div>
          <label class="text-xs text-[var(--muted-foreground)]">{{ t('admin.channels.form.cacheReadPriceShort') }} <span class="text-[var(--muted-foreground)]">$/M</span></label>
          <input :value="interval.cache_read_price" @input="emitField('cache_read_price', ($event.target as HTMLInputElement).value)"
            type="number" step="any" min="0" class="input tabular-nums mt-0.5 text-xs" />
        </div>
        <template v-if="enableMultipliers">
          <div>
            <label class="text-xs text-[var(--muted-foreground)]">{{ t('admin.channels.form.inputMultiplier') }}</label>
            <input :value="interval.input_multiplier" @input="emitField('input_multiplier', ($event.target as HTMLInputElement).value)"
              type="number" step="any" min="0.000001" class="input tabular-nums mt-0.5 text-xs" />
          </div>
          <div>
            <label class="text-xs text-[var(--muted-foreground)]">{{ t('admin.channels.form.outputMultiplier') }}</label>
            <input :value="interval.output_multiplier" @input="emitField('output_multiplier', ($event.target as HTMLInputElement).value)"
              type="number" step="any" min="0.000001" class="input tabular-nums mt-0.5 text-xs" />
          </div>
          <div>
            <label class="text-xs text-[var(--muted-foreground)]">{{ t('admin.channels.form.cacheWriteMultiplier') }}</label>
            <input :value="interval.cache_write_multiplier" @input="emitField('cache_write_multiplier', ($event.target as HTMLInputElement).value)"
              type="number" step="any" min="0.000001" class="input tabular-nums mt-0.5 text-xs" />
          </div>
          <div>
            <label class="text-xs text-[var(--muted-foreground)]">{{ t('admin.channels.form.cacheReadMultiplier') }}</label>
            <input :value="interval.cache_read_multiplier" @input="emitField('cache_read_multiplier', ($event.target as HTMLInputElement).value)"
              type="number" step="any" min="0.000001" class="input tabular-nums mt-0.5 text-xs" />
          </div>
        </template>
      </div>
    </template>

    <!-- Per-request / Image mode: tier label + context range + price -->
    <template v-else>
      <div class="min-w-0 basis-24 grow">
        <label class="text-xs text-[var(--muted-foreground)]">
          {{ mode === 'image' ? t('admin.channels.form.resolution') : t('admin.channels.form.tierLabel') }}
        </label>
        <input :value="interval.tier_label" @input="emitField('tier_label', ($event.target as HTMLInputElement).value)"
          type="text" class="input tabular-nums mt-0.5 text-xs" :placeholder="mode === 'image' ? '1K / 2K / 4K' : ''" />
      </div>
      <div class="min-w-0 basis-20 grow">
        <label class="text-xs text-[var(--muted-foreground)]">{{ t('admin.channels.form.minTokens') }}</label>
        <input :value="interval.min_tokens" @input="emitField('min_tokens', toInt(($event.target as HTMLInputElement).value))"
          type="number" min="0" class="input tabular-nums mt-0.5 text-xs" />
      </div>
      <div class="min-w-0 basis-20 grow">
        <label class="text-xs text-[var(--muted-foreground)]">{{ t('admin.channels.form.maxTokens') }} <span class="text-[var(--muted-foreground)]">{{ t('admin.channels.form.inclusive') }}</span></label>
        <input :value="interval.max_tokens ?? ''" @input="emitField('max_tokens', toIntOrNull(($event.target as HTMLInputElement).value))"
          type="number" min="0" class="input tabular-nums mt-0.5 text-xs" :placeholder="'∞'" />
      </div>
      <div class="min-w-0 basis-28 grow">
        <label class="text-xs text-[var(--muted-foreground)]">{{ t('admin.channels.form.perRequestPrice') }} <span v-if="isEmpty" class="text-red-500">*</span> <span class="text-[var(--muted-foreground)]">$</span></label>
        <input :value="interval.per_request_price" @input="emitField('per_request_price', ($event.target as HTMLInputElement).value)"
          type="number" step="any" min="0" class="input tabular-nums mt-0.5 text-xs" />
      </div>
    </template>

    <button type="button" @click="emit('remove')" class="mt-4 rounded p-0.5 text-[var(--muted-foreground)] hover:text-red-500">
      <Icon name="x" size="sm" />
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { IntervalFormEntry } from './types'
import type { BillingMode } from '@/api/admin/channels'

const { t } = useI18n()

const props = defineProps<{
  interval: IntervalFormEntry
  mode: BillingMode
  enableMultipliers?: boolean
}>()

const emit = defineEmits<{
  update: [interval: IntervalFormEntry]
  remove: []
}>()

// 检测所有价格字段是否都为空
const isEmpty = computed(() => {
  const iv = props.interval
  return (iv.input_price == null || iv.input_price === '') &&
    (iv.output_price == null || iv.output_price === '') &&
    (iv.cache_write_price == null || iv.cache_write_price === '') &&
    (iv.cache_write_1h_price == null || iv.cache_write_1h_price === '') &&
    (iv.cache_read_price == null || iv.cache_read_price === '') &&
    (iv.input_multiplier == null || iv.input_multiplier === '') &&
    (iv.output_multiplier == null || iv.output_multiplier === '') &&
    (iv.cache_write_multiplier == null || iv.cache_write_multiplier === '') &&
    (iv.cache_read_multiplier == null || iv.cache_read_multiplier === '') &&
    (iv.per_request_price == null || iv.per_request_price === '')
})

function emitField(field: keyof IntervalFormEntry, value: string | number | null) {
  emit('update', { ...props.interval, [field]: value === '' ? null : value })
}

function toInt(val: string): number {
  const tokenCount = Math.trunc(Number(val))
  return Number.isFinite(tokenCount) ? tokenCount : 0
}

function toIntOrNull(val: string): number | null {
  if (val === '') return null
  const tokenCount = Math.trunc(Number(val))
  return Number.isFinite(tokenCount) ? tokenCount : null
}
</script>

<style scoped>
:is(input, select, textarea).input {
  min-height: 40px;
  min-width: 0;
  border-radius: 8px;
  border-color: var(--field-border, var(--input));
  background: var(--card);
  color: var(--foreground);
  box-shadow: none;
  transition: border-color 150ms ease, background-color 150ms ease;
}

:is(input, select, textarea).input::placeholder { color: var(--muted-foreground); }
:is(input, select, textarea).input:disabled { background: var(--surface-2); }
:is(input, select, textarea).input:focus {
  border-color: var(--field-border, var(--brand));
  outline: 2px solid color-mix(in srgb, var(--field-border, var(--brand)) 22%, transparent);
  outline-offset: 2px;
  box-shadow: none;
}

.input-error { --field-border: var(--bad); }
label.input-label { color: var(--foreground); }
p.input-hint { color: var(--muted-foreground); }

div.card {
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--card);
  color: var(--foreground);
  box-shadow: none;
}

button:focus-visible, a:focus-visible, summary:focus-visible {
  outline: 2px solid var(--brand);
  outline-offset: 3px;
}

input[type='checkbox'] { accent-color: var(--brand); }


.pricing-interval-grid {
  grid-template-columns: repeat(auto-fit, minmax(7.5rem, 1fr));
}
@media (prefers-reduced-motion: reduce) {
  :is(button, a).btn, :is(input, select, textarea).input, .fade-enter-active, .fade-leave-active,
  .agreement-fade-enter-active, .agreement-fade-leave-active,
  .agreement-fade-enter-active > div, .agreement-fade-leave-active > div {
    transition: none;
  }
}
</style>
