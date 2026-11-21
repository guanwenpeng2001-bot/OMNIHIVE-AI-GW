<script setup lang="ts">
import { computed, ref, toRef } from 'vue'
import IconButton from './IconButton.vue'
import { cn, useOverlayDialog, useUiId } from './utils'

export interface DrawerProps {
  open: boolean
  title?: string
  description?: string
  side?: 'right' | 'left'
  size?: 'sm' | 'md' | 'lg'
  closeOnBackdrop?: boolean
  closeOnEscape?: boolean
  teleportDisabled?: boolean
}

const props = withDefaults(defineProps<DrawerProps>(), {
  title: undefined,
  description: undefined,
  side: 'right',
  size: 'md',
  closeOnBackdrop: true,
  closeOnEscape: true,
  teleportDisabled: false
})

const emit = defineEmits<{
  'update:open': [open: boolean]
  close: []
}>()

const panelRef = ref<HTMLElement | null>(null)
const drawerId = useUiId('oh-drawer')
const titleId = computed(() => `${drawerId}-title`)
const descriptionId = computed(() => `${drawerId}-desc`)

function requestClose(): void {
  emit('update:open', false)
  emit('close')
}

useOverlayDialog({
  open: toRef(props, 'open'),
  panelRef,
  closeOnEscape: toRef(props, 'closeOnEscape'),
  onClose: requestClose
})

const widthClasses = computed(() => {
  switch (props.size) {
    case 'sm':
      return 'max-w-sm'
    case 'lg':
      return 'max-w-2xl'
    default:
      return 'max-w-lg'
  }
})
</script>

<template>
  <Teleport to="body" :disabled="teleportDisabled">
    <div v-if="open" class="fixed inset-0 z-50 flex">
      <div
        class="fixed inset-0 bg-[var(--oh-backdrop)]"
        aria-hidden="true"
        @click="closeOnBackdrop && requestClose()"
      />

      <div
        ref="panelRef"
        role="dialog"
        aria-modal="true"
        :aria-labelledby="title || $slots.header ? titleId : undefined"
        :aria-describedby="description ? descriptionId : undefined"
        tabindex="-1"
        :class="
          cn(
            'relative z-10 flex h-full w-full flex-col bg-oh-elevated text-oh-primary shadow-oh-xl focus:outline-none',
            widthClasses,
            side === 'right' ? 'ml-auto border-l border-oh-default' : 'mr-auto border-r border-oh-default'
          )
        "
      >
        <header class="flex items-start justify-between gap-4 border-b border-oh-subtle px-5 py-4">
          <div class="min-w-0 flex-1">
            <slot name="header">
              <h2 v-if="title" :id="titleId" class="text-base font-semibold text-oh-primary">
                {{ title }}
              </h2>
              <p v-if="description" :id="descriptionId" class="mt-1 text-xs text-oh-secondary">
                {{ description }}
              </p>
            </slot>
          </div>
          <IconButton aria-label="Close drawer" ariaLabel="Close drawer" size="sm" @click="requestClose">
            <svg viewBox="0 0 16 16" class="h-4 w-4" fill="none" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
              <path stroke-linecap="round" d="M4 4l8 8M12 4l-8 8" />
            </svg>
          </IconButton>
        </header>

        <div class="flex-1 overflow-y-auto px-5 py-4">
          <slot />
        </div>

        <footer
          v-if="$slots.footer"
          class="flex flex-wrap items-center justify-end gap-2 border-t border-oh-subtle bg-oh-subtle/50 px-5 py-3.5"
        >
          <slot name="footer" />
        </footer>
      </div>
    </div>
  </Teleport>
</template>
