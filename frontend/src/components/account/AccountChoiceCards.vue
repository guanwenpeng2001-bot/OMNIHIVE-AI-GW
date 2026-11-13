<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import { navigateAccountChoices } from './accountChoiceNavigation'
defineProps<{
  label: string
  modelValue?: string
  options: { value: string; label: string; description?: string; icon?: 'swap' | 'sparkles' | 'terminal' | 'chat' | 'creditCard' | 'bolt' | 'key' }[]
  columns?: 2 | 3
  locked?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
</script>

<template>
  <div>
    <p class="input-label">{{ label }}</p>
    <div role="radiogroup" :aria-label="label" class="mt-2 grid grid-cols-1 gap-2.5" :class="columns === 3 ? 'sm:grid-cols-3' : 'sm:grid-cols-2'" @keydown="navigateAccountChoices">
      <button v-for="(option, index) in options" :key="option.value" type="button" role="radio"
        :data-choice="option.value" :aria-checked="modelValue === option.value" :disabled="locked" :aria-disabled="locked || undefined"
        :tabindex="!locked && (modelValue === option.value || (!options.some(entry => entry.value === modelValue) && index === 0)) ? 0 : -1"
        class="flex min-w-0 items-center gap-3 rounded-lg border p-3 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)] disabled:cursor-default"
        :class="modelValue === option.value ? 'border-primary-500 bg-[var(--accent)]' : 'border-[var(--border)] bg-[var(--card)] hover:border-[var(--brand)] hover:bg-[var(--surface-2)]'"
        @click="emit('update:modelValue', option.value)">
        <span v-if="option.icon" aria-hidden="true" class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border transition-colors"
          :class="modelValue === option.value ? 'border-[var(--brand)]/40 bg-[var(--primary)] text-[var(--primary-foreground)]' : 'border-[var(--border)] bg-[var(--surface-2)] text-[var(--muted-foreground)]'">
          <Icon :name="option.icon" size="sm" />
        </span>
        <span class="min-w-0 break-words">
          <span class="block text-xs font-semibold text-[var(--foreground)]">{{ option.label }}</span>
          <span v-if="option.description" class="mt-0.5 block text-[11px] text-[var(--muted-foreground)]">{{ option.description }}</span>
        </span>
      </button>
    </div>
  </div>
</template>

<style scoped>
.border-primary-500 {
  border-color: var(--brand);
}
</style>
