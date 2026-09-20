export function focusAccountCredentialField(formId: string, field: string) {
  const container = document.getElementById(formId)?.querySelector<HTMLElement>(`[data-account-field="${field}"]`)
  if (!container) return
  const input = container.matches('input, select, textarea, button') ? container : container.querySelector<HTMLElement>('input, select, textarea, button')
  if (!input) return
  input.setAttribute('aria-invalid', 'true')
  input.addEventListener('input', () => input.removeAttribute('aria-invalid'), { once: true })
  input.focus()
  input.scrollIntoView?.({ block: 'nearest' })
}
