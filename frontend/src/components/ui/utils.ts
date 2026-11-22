import { nextTick, onBeforeUnmount, watch, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { i18n } from '@/i18n'
import { useIsZhLocale } from '@/utils/activeLocale'

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

function isElementHidden(element: HTMLElement, container: HTMLElement): boolean {
  if (
    element.hasAttribute('disabled') ||
    element.tabIndex < 0 ||
    element.getAttribute('aria-hidden') === 'true' ||
    element.closest('[hidden], [inert], [aria-hidden="true"]')
  ) {
    return true
  }

  let ancestor: HTMLElement | null = element
  while (ancestor) {
    if (ancestor.style?.display === 'none' || ancestor.style?.visibility === 'hidden') {
      return true
    }
    if (typeof window !== 'undefined' && typeof window.getComputedStyle === 'function') {
      const style = window.getComputedStyle(ancestor)
      if (style.display === 'none' || style.visibility === 'hidden') {
        return true
      }
    }
    if (ancestor === container) break
    ancestor = ancestor.parentElement
  }

  const containerHasLayout = container.offsetWidth > 0 || container.offsetHeight > 0
  if (containerHasLayout && element.offsetParent === null) {
    const position =
      typeof window !== 'undefined' && typeof window.getComputedStyle === 'function'
        ? window.getComputedStyle(element).position
        : element.style?.position
    if (position !== 'fixed') {
      return true
    }
  }

  return false
}

export function getFocusableElements(container: HTMLElement | null): HTMLElement[] {
  if (!container) return []
  return Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter(
    (element) => !isElementHidden(element, container)
  )
}

const UI_FALLBACK_MESSAGES: Record<'en' | 'zh', Record<string, string>> = {
  en: {
    'ui.table.emptyTitle': 'No records yet',
    'ui.table.filteredEmptyTitle': 'No matching results',
    'ui.table.filteredEmptyDescription': 'Adjust or clear active filters to restore rows.',
    'ui.table.refreshing': 'Refreshing table data',
    'ui.table.selectAllRows': 'Select all rows',
    'ui.table.selectRow': 'Select row {index}',
    'ui.empty.clearAllFilters': 'Clear all filters',
    'ui.modal.close': 'Close dialog',
    'ui.drawer.close': 'Close drawer',
    'ui.commandPalette.placeholder': 'Search models, routes, keys, or actions…',
    'ui.commandPalette.emptyText': 'No matching commands or models.',
    'ui.commandPalette.ariaLabel': 'Command palette',
    'ui.commandPalette.availableCommands': 'Available commands',
    'ui.commandPalette.searching': 'Searching catalog…',
    'ui.commandPalette.navigateHint': '↑↓ Navigate',
    'ui.commandPalette.selectHint': '↵ Select',
    'ui.commandPalette.closeHint': 'ESC Close',
    'ui.toast.notifications': 'Notifications',
    'ui.toast.copyRequestId': 'Copy request ID',
    'ui.toast.dismiss': 'Dismiss notification',
    'ui.input.clear': 'Clear input',
    'ui.input.hideSecret': 'Hide secret',
    'ui.input.revealSecret': 'Reveal secret',
    'ui.input.hide': 'Hide',
    'ui.input.show': 'Show',
    'ui.stat.free': 'Free',
    'ui.stat.noVerifiedData': 'No verified data',
    'ui.stat.settled': 'Settled',
    'ui.stat.estimated': 'Estimated',
    'ui.stat.reserved': 'Reserved',
    'ui.stat.pendingCheck': 'Pending Check'
  },
  zh: {
    'ui.table.emptyTitle': '暂无记录',
    'ui.table.filteredEmptyTitle': '无匹配结果',
    'ui.table.filteredEmptyDescription': '请调整或清空当前筛选条件以恢复列表。',
    'ui.table.refreshing': '正在刷新表格数据',
    'ui.table.selectAllRows': '全选所有行',
    'ui.table.selectRow': '选择第 {index} 行',
    'ui.empty.clearAllFilters': '清空所有筛选',
    'ui.modal.close': '关闭对话框',
    'ui.drawer.close': '关闭抽屉',
    'ui.commandPalette.placeholder': '搜索模型、路由、密钥或操作…',
    'ui.commandPalette.emptyText': '未找到匹配的命令或模型。',
    'ui.commandPalette.ariaLabel': '命令面板',
    'ui.commandPalette.availableCommands': '可用命令',
    'ui.commandPalette.searching': '正在搜索目录…',
    'ui.commandPalette.navigateHint': '↑↓ 导航',
    'ui.commandPalette.selectHint': '↵ 选择',
    'ui.commandPalette.closeHint': 'ESC 关闭',
    'ui.toast.notifications': '通知',
    'ui.toast.copyRequestId': '复制请求 ID',
    'ui.toast.dismiss': '关闭通知',
    'ui.input.clear': '清空输入',
    'ui.input.hideSecret': '隐藏密钥',
    'ui.input.revealSecret': '显示密钥',
    'ui.input.hide': '隐藏',
    'ui.input.show': '显示',
    'ui.stat.free': '免费',
    'ui.stat.noVerifiedData': '暂无已验证数据',
    'ui.stat.settled': '已结算',
    'ui.stat.estimated': '预估',
    'ui.stat.reserved': '预占',
    'ui.stat.pendingCheck': '待核对'
  }
}

export function useUiI18n(): {
  t: (key: string, params?: Record<string, string | number>) => string
} {
  let composer: { t?: (k: string, p?: Record<string, unknown>) => string; te?: (k: string) => boolean } | null = null
  try {
    composer = useI18n()
  } catch {
    composer = null
  }
  const isZh = useIsZhLocale('en')

  const t = (key: string, params?: Record<string, string | number>): string => {
    if (composer?.te && composer.t && composer.te(key)) {
      return composer.t(key, params)
    }
    const globalComposer = i18n?.global as
      | { t?: (k: string, p?: Record<string, unknown>) => string; te?: (k: string) => boolean }
      | undefined
    if (globalComposer?.te && globalComposer.t && globalComposer.te(key)) {
      return globalComposer.t(key, params)
    }
    const dict = isZh.value ? UI_FALLBACK_MESSAGES.zh : UI_FALLBACK_MESSAGES.en
    const template = dict[key] ?? UI_FALLBACK_MESSAGES.en[key] ?? key
    if (!params) return template
    return template.replace(/\{([a-zA-Z0-9_]+)\}/g, (_, token: string) =>
      params[token] !== undefined ? String(params[token]) : `{${token}}`
    )
  }

  return { t }
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
