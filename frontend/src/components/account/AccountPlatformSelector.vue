<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { getPlatformLabel, platformFamily, type PlatformFamily } from '@/constants/platforms'

import { supplierPlatformOptions, supplierOf } from '@/constants/supplierFamilies'

const props = defineProps<{ modelValue: string }>()
const emit = defineEmits<{ select: [platform: string] }>()
const { t } = useI18n()
const search = ref('')
const family = ref<'all' | PlatformFamily>('all')
const options = computed(supplierPlatformOptions)
const selected = computed(() => supplierOf(props.modelValue)?.members[0].platform ?? props.modelValue)
// 族名走 i18n；分类按真实协议族（见 platformFamily），因此那一栏叫
// “OpenAI 兼容 / 其它”而不是把所有成员都称作 OpenAI。
const FAMILY_OPTIONS: ReadonlyArray<'all' | PlatformFamily> = ['all', 'anthropic', 'google', 'openaiCompatible']
const families = computed(() => FAMILY_OPTIONS.map(value => ({
  value,
  label: value === 'all' ? t('common.all') : t(`admin.accounts.platformFamilies.${value}`)
})))
const visible = computed(() => options.value.filter(option => {
  const belongs = family.value === 'all' || platformFamily(option.value) === family.value
  return belongs && `${option.label} ${option.search} ${option.value} ${getPlatformLabel(option.value, t)}`.toLowerCase().includes(search.value.trim().toLowerCase())
}))
function navigate(event: KeyboardEvent, index: number) {
  const offset = ['ArrowRight', 'ArrowDown'].includes(event.key) ? 1 : ['ArrowLeft', 'ArrowUp'].includes(event.key) ? -1 : 0
  if (!offset && !['Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const position = event.key === 'Home' ? 0 : event.key === 'End' ? visible.value.length - 1 : (index + offset + visible.value.length) % visible.value.length
  const next = visible.value[position]
  if (!next) return
  emit('select', next.value)
  const button = event.currentTarget as HTMLElement
  button.parentElement?.querySelector<HTMLButtonElement>(`[data-testid="${next.value}-platform"]`)?.focus()
}
</script>

<template>
  <div class="min-w-0 space-y-3">
    <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
      <input v-model="search" type="search" class="input" :aria-label="t('common.search')" :placeholder="t('common.search')" />
      <Select v-model="family" :options="families" :aria-label="t('admin.accounts.platform')" />
    </div>
    <div role="radiogroup" :aria-label="t('admin.accounts.platform')" class="flex max-h-80 flex-wrap gap-1.5 overflow-y-auto rounded-lg bg-gray-100 p-1 border border-[var(--border)] !bg-[var(--surface-2)] !p-2" data-tour="account-form-platform">
      <button v-for="(option, index) in visible" :key="option.value" type="button" role="radio"
        :aria-checked="selected === option.value" :data-testid="`${option.value}-platform`"
        :tabindex="selected === option.value || (!visible.some(item => item.value === selected) && index === 0) ? 0 : -1"
        :class="[
          'flex items-center gap-2 rounded-lg border px-2.5 py-1.5 text-xs font-medium transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)]',
          selected === option.value
            ? 'bg-white text-primary-600 shadow-sm border-[var(--brand)] !bg-[var(--card)] !text-[var(--foreground)] font-semibold ring-1 ring-[var(--brand)]/25'
            : 'border-transparent bg-[var(--card)]/70 text-[var(--muted-foreground)] hover:border-[var(--border)] hover:bg-[var(--card)] hover:text-[var(--foreground)]'
        ]"
        @click="emit('select', option.value)" @keydown="navigate($event, index)">
        <span class="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-md border border-[var(--border)] bg-[var(--card)]">
          <PlatformIcon :platform="option.value === 'vertex' ? 'gemini' : option.value" size="sm" />
        </span>{{ option.supplier ? t(`admin.accounts.supplier.names.${option.supplier.id}`) : getPlatformLabel(option.value, t) }}
      </button>
    </div>
    <p v-if="!visible.length" role="status" class="input-hint">{{ t('common.noData') }}</p>
  </div>
</template>
