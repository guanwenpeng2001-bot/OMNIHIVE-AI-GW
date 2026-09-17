// Shared by dialogs and their teleported controls.
let nextDialogId = 0
const dialogs: Array<{ id: string; element: HTMLElement; zIndex: number }> = []
const previousInert = new Map<HTMLElement, boolean>()
let observer: MutationObserver | null = null

export function createDialogId(): string {
  return `modal-title-${++nextDialogId}`
}

export function topDialogId(): string | undefined {
  return [...dialogs].sort((first, second) => first.zIndex - second.zIndex).at(-1)?.id
}

function syncBackground(): void {
  const activeId = topDialogId()
  const active = dialogs.find(dialog => dialog.id === activeId)
  for (const child of Array.from(document.body.children)) {
    if (!(child instanceof HTMLElement)) continue
    if (!previousInert.has(child)) previousInert.set(child, child.inert)
    const ownedPortal = child.dataset.dialogOwner === activeId
    child.inert = active ? !(child.contains(active.element) || ownedPortal) : previousInert.get(child) ?? false
  }
  document.body.classList.toggle('modal-open', dialogs.length > 0)
}

export function registerDialog(id: string, element: HTMLElement, zIndex: number): void {
  dialogs.push({ id, element, zIndex })
  if (!observer) {
    observer = new MutationObserver(syncBackground)
    observer.observe(document.body, { childList: true })
  }
  syncBackground()
}

export function unregisterDialog(id: string): void {
  const index = dialogs.findIndex(dialog => dialog.id === id)
  if (index < 0) return
  dialogs.splice(index, 1)
  syncBackground()
  if (!dialogs.length) {
    observer?.disconnect()
    observer = null
    for (const [element, inert] of previousInert) element.inert = inert
    previousInert.clear()
  }
}
