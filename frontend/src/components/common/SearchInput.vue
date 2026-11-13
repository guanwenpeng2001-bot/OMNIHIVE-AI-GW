<template>
  <div class="relative w-full">
    <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3">
      <Icon name="search" size="md" class="text-[var(--muted-foreground)]" />
    </div>
    <input
      v-model="searchValue"
      type="text"
      class="input pl-10"
      :placeholder="placeholder"
      :aria-label="placeholder"
    />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(defineProps<{
  modelValue: string
  placeholder?: string
  debounceMs?: number
}>(), {
  placeholder: 'Search...',
  debounceMs: 300
})

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'search', value: string): void
}>()

const debouncedEmitSearch = useDebounceFn((value: string) => {
  emit('search', value)
}, props.debounceMs)

const searchValue = computed({
  get: () => props.modelValue,
  set: (value: string) => {
    emit('update:modelValue', value)
    debouncedEmitSearch(value)
  }
})
</script>

<style scoped>
.input { background: var(--card); color: var(--foreground); border: 1px solid var(--border); border-radius: 8px; transition: border-color .15s, box-shadow .15s; }
.input::placeholder { color: var(--muted-foreground); }
.input:focus { outline: none; border-color: var(--brand); box-shadow: 0 0 0 3px color-mix(in srgb, var(--brand) 18%, transparent); }
@media (prefers-reduced-motion: reduce) { .input { transition: none; } }
</style>
