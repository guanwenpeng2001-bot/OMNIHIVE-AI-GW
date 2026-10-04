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
    <div role="radiogroup" :aria-label="label" class="mt-2 grid grid-cols-1 gap-3" :class="columns === 3 ? 'sm:grid-cols-3' : 'sm:grid-cols-2'" @keydown="navigateAccountChoices">
      <button v-for="(option, index) in options" :key="option.value" type="button" role="radio"
        :data-choice="option.value" :aria-checked="modelValue === option.value" :disabled="locked" :aria-disabled="locked || undefined"
        :tabindex="!locked && (modelValue === option.value || (!options.some(entry => entry.value === modelValue) && index === 0)) ? 0 : -1"
        class="flex min-w-0 items-center gap-3 rounded-lg border-2 p-3 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-default"
        :class="modelValue === option.value ? 'border-primary-500 bg-primary-50 dark:bg-primary-900/20' : 'border-gray-200 hover:border-gray-400 dark:border-dark-600 dark:hover:border-gray-600'"
        @click="emit('update:modelValue', option.value)">
        <span v-if="option.icon" aria-hidden="true" class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg"
          :class="modelValue === option.value ? 'bg-primary-500 text-white' : 'bg-gray-100 text-gray-500 dark:bg-dark-600 dark:text-gray-400'">
          <Icon :name="option.icon" size="sm" />
        </span>
        <span class="min-w-0 break-words">
          <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ option.label }}</span>
          <span v-if="option.description" class="text-xs text-gray-500 dark:text-gray-400">{{ option.description }}</span>
        </span>
      </button>
    </div>
  </div>
</template>
