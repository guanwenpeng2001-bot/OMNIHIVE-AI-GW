<template>
  <Teleport to="body">
    <Transition name="modal" :duration="{ enter: 250, leave: 200 }">
      <div
        v-if="show"
        class="modal-overlay oh-modal-overlay"
        :style="zIndexStyle"
        :aria-labelledby="dialogId"
        :data-dialog-id="dialogId"
        role="dialog"
        aria-modal="true"
        @click.self="handleClose"
      >
        <!-- Modal panel -->
        <div ref="dialogRef" :class="['modal-content oh-modal-content', widthClasses]" @click.stop>
          <!-- Header -->
          <div class="modal-header oh-modal-header">
            <h3 ref="titleRef" tabindex="-1" :id="dialogId" class="modal-title oh-modal-title">
              {{ title }}
            </h3>
            <button
              v-if="showCloseButton"
              type="button"
              @click="emit('close')"
              class="oh-modal-close -mr-2 rounded-lg p-2 opacity-60 transition-colors hover:bg-[var(--secondary)] hover:opacity-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--brand)]"
              :aria-label="t('common.close')"
            >
              <Icon name="x" size="md" />
            </button>
          </div>

          <!-- Body -->
          <div ref="modalBodyRef" class="modal-body oh-modal-body">
            <slot></slot>
          </div>

          <!-- Footer -->
          <div v-if="$slots.footer" class="modal-footer oh-modal-footer">
            <slot name="footer"></slot>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, watch, onMounted, onUnmounted, ref, nextTick } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { useI18n } from 'vue-i18n'
import { createDialogId, registerDialog, unregisterDialog, topDialogId } from './dialogStack'

const { t } = useI18n()

// 生成唯一ID以避免多个对话框时ID冲突
const dialogId = createDialogId()
const titleRef = ref<HTMLElement | null>(null)

// 焦点管理
const dialogRef = ref<HTMLElement | null>(null)
const modalBodyRef = ref<HTMLElement | null>(null)
let previousActiveElement: HTMLElement | null = null

type DialogWidth = 'narrow' | 'normal' | 'wide' | 'extra-wide' | 'full'

interface Props {
  show: boolean
  title: string
  width?: DialogWidth
  closeOnEscape?: boolean
  closeOnClickOutside?: boolean
  showCloseButton?: boolean
  zIndex?: number
}

interface Emits {
  (e: 'close'): void
}

const props = withDefaults(defineProps<Props>(), {
  width: 'normal',
  closeOnEscape: true,
  closeOnClickOutside: false,
  showCloseButton: true,
  zIndex: 50
})

const emit = defineEmits<Emits>()

// Custom z-index style (overrides the default z-50 from CSS)
const zIndexStyle = computed(() => {
  return props.zIndex !== 50 ? { zIndex: props.zIndex } : undefined
})

const widthClasses = computed(() => {
  // Width guidance: narrow=confirm/short prompts, normal=standard forms,
  // wide=multi-section forms or rich content, extra-wide=analytics/tables,
  // full=full-screen or very dense layouts.
  const widths: Record<DialogWidth, string> = {
    narrow: 'max-w-md',
    normal: 'max-w-lg',
    wide: 'w-full sm:max-w-2xl md:max-w-3xl lg:max-w-4xl',
    'extra-wide': 'w-full sm:max-w-3xl md:max-w-4xl lg:max-w-5xl xl:max-w-6xl',
    full: 'w-full sm:max-w-4xl md:max-w-5xl lg:max-w-6xl xl:max-w-7xl'
  }
  return widths[props.width]
})

const handleClose = () => {
  if (props.closeOnClickOutside) {
    emit('close')
  }
}

const handleEscape = (event: KeyboardEvent) => {
  if (!props.show || topDialogId() !== dialogId || event.defaultPrevented) return
  // Select consumes the first Escape even when focus remains on its trigger.
  if (event.key === 'Escape' && document.querySelector(`[data-dialog-owner="${dialogId}"][data-select-open="true"]`)) return
  if (event.key === 'Escape' && props.closeOnEscape) {
    event.preventDefault()
    event.stopImmediatePropagation()
    emit('close')
  }
  if (event.key === 'Tab' && dialogRef.value) {
    const focusable = Array.from(dialogRef.value.querySelectorAll<HTMLElement>(
      'button:not(:disabled), [href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])'
    )).filter(element => {
      if (element.tabIndex < 0 || element.closest('[hidden], [inert]')) return false
      let ancestor: HTMLElement | null = element
      while (ancestor && ancestor !== dialogRef.value) {
        const style = getComputedStyle(ancestor)
        if (style.display === 'none' || style.visibility === 'hidden') return false
        ancestor = ancestor.parentElement
      }
      return true
    })
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    if (!first) {
      event.preventDefault()
      if (topDialogId() === dialogId) titleRef.value?.focus()
    } else if (event.shiftKey && (document.activeElement === first || document.activeElement === titleRef.value)) {
      event.preventDefault()
      last.focus()
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault()
      first.focus()
    }
  }
}

// Prevent body scroll when modal is open and manage focus
watch(
  () => props.show,
  async (isOpen, _wasOpen, onCleanup) => {
    let invalidated = false
    onCleanup(() => { invalidated = true })
    if (isOpen) {
      // 保存当前焦点元素
      previousActiveElement = document.activeElement as HTMLElement

      // 等待DOM更新后设置焦点到对话框
      await nextTick()
      if (invalidated || !props.show) return
      if (modalBodyRef.value) {
        modalBodyRef.value.scrollTop = 0
      }
      if (dialogRef.value) registerDialog(dialogId, dialogRef.value, props.zIndex)
      if (topDialogId() === dialogId) titleRef.value?.focus()
    } else {
      const wasTopDialog = topDialogId() === dialogId
      unregisterDialog(dialogId)
      // Only the closing top dialog may restore focus.
      if (wasTopDialog && previousActiveElement?.isConnected) {
        previousActiveElement.focus()
      }
      previousActiveElement = null
    }
  },
  { immediate: true }
)

onMounted(() => {
  document.addEventListener('keydown', handleEscape)
})

onUnmounted(() => {
  document.removeEventListener('keydown', handleEscape)
  // 确保组件卸载时移除滚动锁定
  const wasTopDialog = topDialogId() === dialogId
  unregisterDialog(dialogId)
  if (wasTopDialog && previousActiveElement?.isConnected) previousActiveElement.focus()
})
</script>

<style scoped>
.oh-modal-overlay {
  background: rgba(10, 10, 14, 0.48);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
}

.oh-modal-content {
  border-radius: var(--radius-lg, 16px);
  border: 1px solid var(--border, #e9e9ee);
  background: var(--card, #ffffff);
  color: var(--card-foreground, #131316);
  box-shadow: 0 24px 64px -12px rgba(0, 0, 0, 0.28),
    0 0 0 1px rgba(255, 255, 255, 0.06) inset;
}

.dark .oh-modal-content {
  border-color: var(--border, #232329);
  background: var(--card, #131316);
  color: var(--card-foreground, #ececf0);
}

.oh-modal-header {
  border-bottom: 1px solid var(--border, #e9e9ee);
}

.dark .oh-modal-header {
  border-bottom-color: var(--border, #232329);
}

.oh-modal-title {
  letter-spacing: -0.01em;
}

.oh-modal-footer {
  border-top: 1px solid var(--border, #e9e9ee);
  background: color-mix(in srgb, var(--surface-2, #f4f4f5) 45%, var(--card, #ffffff));
  border-bottom-left-radius: var(--radius-lg, 16px);
  border-bottom-right-radius: var(--radius-lg, 16px);
}

.dark .oh-modal-footer {
  border-top-color: var(--border, #232329);
  background: color-mix(in srgb, var(--surface-2, #17171b) 65%, var(--card, #131316));
}
</style>
