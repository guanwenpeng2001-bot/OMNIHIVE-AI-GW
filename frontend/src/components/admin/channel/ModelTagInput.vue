<template>
  <div>
    <!-- Tags display -->
    <div class="model-tag-field flex min-h-[2.5rem] flex-wrap gap-1.5 rounded-lg border border-[var(--border)] bg-[var(--card)] p-2">
      <span
        v-for="(model, idx) in models"
        :key="idx"
        class="pill pill-ghost model-tag max-w-full gap-1 text-[var(--foreground)]"
      >
        {{ model }}
        <button
          type="button"
          @click="removeModel(idx)"
          class="ml-0.5 shrink-0 rounded p-0.5 text-[var(--muted-foreground)] hover:text-[var(--foreground)]"
        >
          <Icon name="x" size="xs" />
        </button>
      </span>
      <input
        ref="inputRef"
        v-model="inputValue"
        type="text"
        class="min-w-0 flex-1 basis-32 border-none bg-transparent text-sm text-[var(--foreground)] outline-none placeholder:text-[var(--muted-foreground)]"
        :placeholder="models.length === 0 ? placeholder : ''"
        @keydown.enter="handleEnter"
        @keydown.tab="handleTab"
        @keydown.backspace="handleBackspace"
        @paste="handlePaste"
        @blur="addModel"
      />
    </div>
    <p class="mt-1 text-xs text-[var(--muted-foreground)]">
      {{ t('admin.channels.form.modelInputHint', 'Press Enter to add, supports paste for batch import.') }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()

const props = defineProps<{
  models: string[]
  placeholder?: string
  platform?: string
}>()

const emit = defineEmits<{
  'update:models': [models: string[]]
}>()

const inputValue = ref('')
const inputRef = ref<HTMLInputElement>()

function addModel() {
  const val = inputValue.value.trim()
  if (!val) return
  if (!props.models.includes(val)) {
    emit('update:models', [...props.models, val])
  }
  inputValue.value = ''
}

function handleEnter(event: KeyboardEvent) {
  if (event.isComposing) return
  event.preventDefault()
  addModel()
}

function handleTab(event: KeyboardEvent) {
  if (event.isComposing || !inputValue.value.trim()) return
  event.preventDefault()
  addModel()
}

function removeModel(idx: number) {
  const newModels = [...props.models]
  newModels.splice(idx, 1)
  emit('update:models', newModels)
}

function handleBackspace(event: KeyboardEvent) {
  if (event.isComposing) return
  if (inputValue.value === '' && props.models.length > 0) {
    removeModel(props.models.length - 1)
  }
}

function handlePaste(e: ClipboardEvent) {
  e.preventDefault()
  const text = e.clipboardData?.getData('text') || ''
  const items = text.split(/[,\n;]+/).map(s => s.trim()).filter(Boolean)
  if (items.length === 0) return
  const unique = [...new Set([...props.models, ...items])]
  emit('update:models', unique)
  inputValue.value = ''
}
</script>

<style scoped>
.model-tag-field:focus-within {
  border-color: var(--brand);
  outline: 2px solid color-mix(in srgb, var(--brand) 22%, transparent);
  outline-offset: 2px;
}

.model-tag {
  white-space: normal;
  overflow-wrap: anywhere;
}

button:focus-visible {
  outline: 2px solid var(--brand);
  outline-offset: 2px;
}
</style>
