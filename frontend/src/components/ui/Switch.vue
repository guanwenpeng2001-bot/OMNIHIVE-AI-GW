<script setup lang="ts">
import { computed } from 'vue'
import { cn, useUiId } from './utils'

export interface SwitchProps {
  modelValue?: boolean
  id?: string
  label?: string
  description?: string
  size?: 'sm' | 'md'
  disabled?: boolean
  loading?: boolean
}

const props = withDefaults(defineProps<SwitchProps>(), {
  modelValue: false,
  id: undefined,
  label: undefined,
  description: undefined,
  size: 'md',
  disabled: false,
  loading: false
})

const emit = defineEmits<{
  'update:modelValue': [checked: boolean]
}>()

const generatedId = useUiId('oh-switch')
const switchId = computed(() => props.id || generatedId)
const labelId = computed(() => `${switchId.value}-label`)
const descriptionId = computed(() => `${switchId.value}-desc`)
const isUnavailable = computed(() => props.disabled || props.loading)

function toggle(): void {
  if (isUnavailable.value) return
  emit('update:modelValue', !props.modelValue)
}
</script>

<template>
  <div class="inline-flex items-start gap-2.5">
    <button
      :id="switchId"
      type="button"
      role="switch"
      :aria-checked="modelValue ? 'true' : 'false'"
      :aria-labelledby="label || $slots.default ? labelId : undefined"
      :aria-describedby="description ? descriptionId : undefined"
      :aria-busy="loading ? 'true' : undefined"
      :disabled="isUnavailable"
      :class="
        cn(
          'oh-focus-ring relative inline-flex shrink-0 items-center rounded-oh-pill border transition-colors duration-150',
          size === 'sm' ? 'h-4 w-7' : 'h-5 w-9',
          modelValue
            ? 'border-transparent bg-oh-accent'
            : 'border-oh-strong bg-oh-inset hover:border-oh-accent',
          isUnavailable ? 'cursor-not-allowed opacity-55' : 'cursor-pointer'
        )
      "
      @click="toggle"
    >
      <span
        :class="
          cn(
            'pointer-events-none inline-block rounded-full transition-transform duration-150 shadow-oh-xs',
            size === 'sm' ? 'h-3 w-3' : 'h-3.5 w-3.5',
            modelValue
              ? size === 'sm'
                ? 'translate-x-3.5 bg-oh-accent-ink'
                : 'translate-x-4 bg-oh-accent-ink'
              : 'translate-x-0.5 bg-oh-primary'
          )
        "
        aria-hidden="true"
      />
    </button>

    <div
      v-if="label || description || $slots.default"
      class="flex min-w-0 flex-col select-none"
      :class="isUnavailable ? 'opacity-55' : 'cursor-pointer'"
      @click="toggle"
    >
      <span :id="labelId" class="text-sm font-medium leading-5 text-oh-primary">
        <slot>{{ label }}</slot>
      </span>
      <span v-if="description" :id="descriptionId" class="text-xs leading-4 text-oh-muted">
        {{ description }}
      </span>
    </div>
  </div>
</template>
