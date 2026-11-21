import { nextTick, onBeforeUnmount, watch, type Ref } from 'vue'

let idSequence = 0

export function useUiId(prefix = 'oh'): string {
  idSequence += 1
  return `${prefix}-${idSequence}`
}

export type UiClassValue = string | false | null | undefined | UiClassValue[]

export function cn(...parts: UiClassValue[]): string {
  const flattened: string[] = []
  const visit = (items: UiClassValue[]): void => {
    for (const item of items) {
      if (!item) continue
      if (Array.isArray(item)) visit(item)
      else flattened.push(item)
    }
  }
  visit(parts)
  return flattened.join(' ')
}

const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])'
].join(',')

const activeOverlayStack: symbol[] = []
let previousBodyOverflow: string | null = null

export function getFocusableElements(container: HTMLElement | null): HTMLElement[] {
  if (!container) return []
  return Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter(
    (element) => !element.hasAttribute('disabled') && element.getAttribute('aria-hidden') !== 'true'
  )
}

export interface UseOverlayDialogOptions {
  open: Ref<boolean>
  panelRef: Ref<HTMLElement | null>
  initialFocusRef?: Ref<HTMLElement | null>
  closeOnEscape?: Ref<boolean> | boolean
  onClose: () => void
}

export function useOverlayDialog(options: UseOverlayDialogOptions): void {
  const overlayToken = Symbol('oh-overlay')
  let triggerElement: HTMLElement | null = null

  const shouldCloseOnEscape = (): boolean => {
    if (typeof options.closeOnEscape === 'boolean') return options.closeOnEscape
    if (options.closeOnEscape) return options.closeOnEscape.value
    return true
  }

  const isTopOverlay = (): boolean =>
    activeOverlayStack[activeOverlayStack.length - 1] === overlayToken

  const handleKeydown = (event: KeyboardEvent): void => {
    if (!options.open.value || !isTopOverlay()) return

    if (event.key === 'Escape' && shouldCloseOnEscape()) {
      event.preventDefault()
      event.stopPropagation()
      options.onClose()
      return
    }

    if (event.key === 'Tab') {
      const panel = options.panelRef.value
      if (!panel) return
      const focusables = getFocusableElements(panel)
      if (focusables.length === 0) {
        event.preventDefault()
        panel.focus()
        return
      }
      const first = focusables[0]
      const last = focusables[focusables.length - 1]
      const active = document.activeElement as HTMLElement | null

      if (event.shiftKey) {
        if (!active || active === first || !panel.contains(active)) {
          event.preventDefault()
          last.focus()
        }
      } else if (!active || active === last || !panel.contains(active)) {
        event.preventDefault()
        first.focus()
      }
    }
  }

  const activate = async (): Promise<void> => {
    if (typeof document === 'undefined') return
    if (!activeOverlayStack.includes(overlayToken)) {
      if (activeOverlayStack.length === 0) {
        previousBodyOverflow = document.body.style.overflow
        document.body.style.overflow = 'hidden'
      }
      activeOverlayStack.push(overlayToken)
    }
    triggerElement = document.activeElement instanceof HTMLElement ? document.activeElement : null
    document.addEventListener('keydown', handleKeydown, true)

    await nextTick()
    const explicitTarget = options.initialFocusRef?.value
    if (explicitTarget) {
      explicitTarget.focus()
      return
    }
    const focusables = getFocusableElements(options.panelRef.value)
    if (focusables.length > 0) {
      focusables[0].focus()
    } else {
      options.panelRef.value?.focus()
    }
  }

  const deactivate = (): void => {
    if (typeof document === 'undefined') return
    document.removeEventListener('keydown', handleKeydown, true)
    const stackIndex = activeOverlayStack.indexOf(overlayToken)
    if (stackIndex !== -1) {
      activeOverlayStack.splice(stackIndex, 1)
    }
    if (activeOverlayStack.length === 0 && previousBodyOverflow !== null) {
      document.body.style.overflow = previousBodyOverflow
      previousBodyOverflow = null
    }
    const targetToRestore = triggerElement
    triggerElement = null
    if (targetToRestore && typeof targetToRestore.focus === 'function' && targetToRestore.isConnected) {
      nextTick(() => {
        targetToRestore.focus()
      })
    }
  }

  watch(
    options.open,
    (isOpen) => {
      if (isOpen) {
        void activate()
      } else {
        deactivate()
      }
    },
    { immediate: true }
  )

  onBeforeUnmount(() => {
    deactivate()
  })
}
