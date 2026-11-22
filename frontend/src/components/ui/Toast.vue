<script setup lang="ts">
import CopyButton from './CopyButton.vue'
import { useUiToast, type UiToastItem } from './useToast'
import { cn, useUiI18n } from './utils'

export interface ToastProps {
  items?: readonly UiToastItem[]
  teleportDisabled?: boolean
}

withDefaults(defineProps<ToastProps>(), {
  items: undefined,
  teleportDisabled: false
})

const emit = defineEmits<{
  dismiss: [id: string]
}>()

const toastStore = useUiToast()
const { t } = useUiI18n()

function handleDismiss(id: string): void {
  toastStore.dismiss(id)
  emit('dismiss', id)
}
</script>

<template>
  <Teleport to="body" :disabled="teleportDisabled">
    <div
      role="region"
      :aria-label="t('ui.toast.notifications')"
      class="pointer-events-none fixed bottom-4 right-4 z-50 flex w-full max-w-sm flex-col gap-2 px-4 sm:px-0"
    >
      <div
        v-for="toast in items ?? toastStore.toasts"
        :key="toast.id"
        :role="toast.tone === 'danger' ? 'alert' : 'status'"
        :aria-live="toast.tone === 'danger' ? 'assertive' : 'polite'"
        :class="
          cn(
            'pointer-events-auto flex items-start gap-3 rounded-oh-lg border bg-oh-elevated p-3.5 text-oh-primary shadow-oh-md',
            toast.tone === 'success' && 'border-oh-status-ok-border',
            toast.tone === 'warning' && 'border-oh-status-warn-border',
            toast.tone === 'danger' && 'border-oh-status-danger-border',
            toast.tone === 'info' && 'border-oh-default'
          )
        "
        @mouseenter="toastStore.pause(toast.id)"
        @mouseleave="toastStore.resume(toast.id)"
        @focusin="toastStore.pause(toast.id)"
        @focusout="toastStore.resume(toast.id)"
      >
        <span
          :class="
            cn(
              'mt-1 h-2 w-2 shrink-0 rounded-full',
              toast.tone === 'success' && 'bg-oh-status-ok-fg',
              toast.tone === 'warning' && 'bg-oh-status-warn-fg',
              toast.tone === 'danger' && 'bg-oh-status-danger-fg',
              toast.tone === 'info' && 'bg-oh-accent'
            )
          "
          aria-hidden="true"
        />

        <div class="min-w-0 flex-1">
          <p class="text-xs font-semibold leading-4 text-oh-primary">
            {{ toast.title }}
          </p>
          <p v-if="toast.description" class="mt-1 text-xs leading-4 text-oh-secondary">
            {{ toast.description }}
          </p>

          <div v-if="toast.requestId || toast.actionLabel" class="mt-2 flex flex-wrap items-center gap-2">
            <span
              v-if="toast.requestId"
              class="inline-flex items-center gap-1 rounded-oh-xs border border-oh-default bg-oh-subtle px-1.5 py-0.5 font-mono text-[11px] text-oh-secondary"
            >
              <span>{{ toast.requestId }}</span>
              <CopyButton
                :value="toast.requestId"
                :aria-label="t('ui.toast.copyRequestId')"
                variant="inline"
                size="xs"
                icon-only
              />
            </span>

            <button
              v-if="toast.actionLabel"
              type="button"
              class="oh-focus-ring rounded-oh-xs text-xs font-semibold text-oh-accent-text hover:underline"
              @click="toast.onAction?.()"
            >
              {{ toast.actionLabel }}
            </button>
          </div>
        </div>

        <button
          type="button"
          :aria-label="t('ui.toast.dismiss')"
          class="oh-focus-ring -mr-1 -mt-1 inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-oh-xs text-oh-muted hover:bg-oh-hover hover:text-oh-primary"
          @click="handleDismiss(toast.id)"
        >
          <svg viewBox="0 0 14 14" class="h-3.5 w-3.5" fill="none" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
            <path stroke-linecap="round" d="M3.5 3.5l7 7M10.5 3.5l-7 7" />
          </svg>
        </button>
      </div>
    </div>
  </Teleport>
</template>
