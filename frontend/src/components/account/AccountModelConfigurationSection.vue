<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ModelWhitelistSelector from './ModelWhitelistSelector.vue'
import type { Account } from '@/types'
import type {
  AccountModelSelectionMode,
  ModelMappingEntry,
  ModelRestrictionError
} from '@/composables/useModelWhitelist'

defineOptions({ inheritAttrs: false })
const props = withDefaults(defineProps<{
  modelValue: string[]
  mappings: ModelMappingEntry[]
  publicTargets?: string[]
  mode?: AccountModelSelectionMode
  reason?: string
  presets?: (ModelMappingEntry & { label?: string })[]
}>(), { presets: () => [] })
const emit = defineEmits<{
  'update:modelValue': [models: string[]]
  'update:mappings': [mappings: ModelMappingEntry[]]
  'update:publicTargets': [targets: string[]]
  'update:mode': [mode: AccountModelSelectionMode]
  'update:reason': [reason: string]
  'selection-saved': [account: Account]
  'catalog-hydrated': [selection: { publicTargets: string[] }]
}>()
const { t } = useI18n()
const selector = ref<InstanceType<typeof ModelWhitelistSelector>>()
function revealValidation(errors: ModelRestrictionError[]) {
  selector.value?.revealValidation(errors)
}
defineExpose({ revealValidation })
</script>

<template>
  <section class="min-w-0 space-y-4 rounded-xl border border-[var(--border)] bg-[var(--surface-2)]/30 p-4 sm:p-5" data-testid="account-model-configuration">
    <div class="space-y-1">
      <h3 class="text-sm font-semibold text-[var(--foreground)]">{{ t('admin.accounts.editSections.models') }}</h3>
      <p data-testid="account-model-funnel-steps" class="text-xs text-[var(--muted-foreground)]">
        1. {{ t('admin.accounts.modelSync.platformCandidates') }} -> 2. {{ t('admin.accounts.modelSync.nativeDiscovered') }} -> 3. {{ t('admin.accounts.modelSync.accountEffective') }} -> 4. {{ t('admin.accounts.modelSync.groupPublic') }}
      </p>
    </div>
    <ModelWhitelistSelector
      ref="selector"
      v-bind="$attrs"
      :model-value="modelValue"
      :model-mappings="mappings"
      :public-targets="props.publicTargets"
      :mode="props.mode"
      :reason="props.reason"
      :presets="props.presets"
      @update:model-value="emit('update:modelValue', $event)"
      @update:model-mappings="emit('update:mappings', $event)"
      @update:public-targets="emit('update:publicTargets', $event)"
      @update:mode="emit('update:mode', $event)"
      @update:reason="emit('update:reason', $event)"
      @selection-saved="emit('selection-saved', $event)"
      @catalog-hydrated="emit('catalog-hydrated', $event)"
      @validation-failed="revealValidation"
    />
    <slot name="model-rules" />
  </section>
</template>
