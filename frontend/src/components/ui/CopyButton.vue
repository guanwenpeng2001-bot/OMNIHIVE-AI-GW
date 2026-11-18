<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import type { UiSize } from './types'
import { cn } from './utils'

export interface CopyButtonProps {
  value: string | (() => string | Promise<string>)
  label?: string
  copiedLabel?: string
  failedLabel?: string
  ariaLabel?: string
  variant?: 'ghost' | 'secondary' | 'inline'
  size?: Exclude<UiSize, 'lg'>
  disabled?: boolean
  resetDelayMs?: number
  iconOnly?: boolean
}

const props = withDefaults(defineProps<CopyButtonProps>(), {
  label: 'Copy',
  copiedLabel: 'Copied',
  failedLabel: 'Copy failed',
  ariaLabel: 'Copy to clipboard',
  variant: 'ghost',
  size: 'sm',
  disabled: false,
  resetDelayMs: 1800,
  iconOnly: false
})

const emit = defineEmits<{
  copied: [value: string]
  error: [reason: unknown]
}>()

const status = ref<'idle' | 'copied' | 'failed'>('idle')
const inFlight = ref(false)
let resetTimer: ReturnType<typeof setTimeout> | null = null

const currentText = computed(() => {
  if (status.value === 'copied') return props.copiedLabel
  if (status.value === 'failed') return props.failedLabel
  return props.label
})

function scheduleReset(): void {
  if (resetTimer !== null) {
    clearTimeout(resetTimer)
  }
  resetTimer = setTimeout(() => {
    status.value = 'idle'
    resetTimer = null
  }, props.resetDelayMs)
}

async function writeClipboard(text: string): Promise<void> {
  if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text)
    return
  }
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.setAttribute('readonly', 'true')
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)
  textarea.select()
  const succeeded = document.execCommand('copy')
  document.body.removeChild(textarea)
  if (!succeeded) {
    throw new Error('Clipboard copy command rejected')
  }
}

async function handleCopy(): Promise<void> {
  if (props.disabled || inFlight.value) return
  inFlight.value = true
  try {
    const resolved = typeof props.value === 'function' ? await props.value() : props.value
    await writeClipboard(resolved)
    status.value = 'copied'
    emit('copied', resolved)
  } catch (reason) {
    status.value = 'failed'
    emit('error', reason)
  } finally {
    inFlight.value = false
    scheduleReset()
  }
}

onBeforeUnmount(() => {
  if (resetTimer !== null) {
    clearTimeout(resetTimer)
  }
})
</script>

<template>
  <button
    type="button"
    :disabled="disabled || inFlight"
    :aria-label="iconOnly ? currentText || ariaLabel : ariaLabel"
    :title="currentText"
    :class="
      cn(
        'oh-focus-ring inline-flex shrink-0 items-center justify-center gap-1.5 font-medium transition-colors duration-150',
        size === 'xs' && 'h-6 px-1.5 text-[11px] rounded-oh-xs',
        size === 'sm' && 'h-7 px-2 text-xs rounded-oh-sm',
        size === 'md' && 'h-8 px-2.5 text-xs rounded-oh-md',
        variant === 'secondary' &&
          'border border-oh-default bg-oh-surface text-oh-secondary hover:border-oh-strong hover:bg-oh-hover hover:text-oh-primary',
        variant === 'ghost' &&
          'border border-transparent bg-transparent text-oh-secondary hover:bg-oh-hover hover:text-oh-primary',
        variant === 'inline' &&
          'h-auto px-1 py-0.5 font-mono text-xs text-oh-secondary hover:text-oh-primary rounded-oh-xs',
        status === 'copied' && 'text-oh-status-ok-fg',
        status === 'failed' && 'text-oh-status-danger-fg',
        (disabled || inFlight) && 'cursor-not-allowed opacity-55'
      )
    "
    @click.stop="handleCopy"
  >
    <svg
      v-if="status === 'copied'"
      viewBox="0 0 16 16"
      class="h-3.5 w-3.5 shrink-0"
      fill="none"
      stroke="currentColor"
      stroke-width="1.8"
      aria-hidden="true"
    >
      <path stroke-linecap="round" stroke-linejoin="round" d="M3 8.5l3.25 3.25L13 4.75" />
    </svg>
    <svg
      v-else-if="status === 'failed'"
      viewBox="0 0 16 16"
      class="h-3.5 w-3.5 shrink-0"
      fill="none"
      stroke="currentColor"
      stroke-width="1.8"
      aria-hidden="true"
    >
      <path stroke-linecap="round" stroke-linejoin="round" d="M8 5v3.5M8 11h.01M2.75 13h10.5L8 3.25 2.75 13z" />
    </svg>
    <svg
      v-else
      viewBox="0 0 16 16"
      class="h-3.5 w-3.5 shrink-0"
      fill="none"
      stroke="currentColor"
      stroke-width="1.6"
      aria-hidden="true"
    >
      <rect x="5.5" y="5.5" width="7.5" height="7.5" rx="1.5" />
      <path stroke-linecap="round" d="M3.5 10.5H3A1.5 1.5 0 0 1 1.5 9V3A1.5 1.5 0 0 1 3 1.5h6A1.5 1.5 0 0 1 10.5 3v.5" />
    </svg>

    <span v-if="!iconOnly">
      <slot :status="status">{{ currentText }}</slot>
    </span>
    <span class="sr-only" aria-live="polite">
      {{ status === 'idle' ? '' : currentText }}
    </span>
  </button>
</template>
