/** Keyboard behavior for the existing account radio-card buttons. */
export function navigateAccountChoices(event: KeyboardEvent) {
  if (!['ArrowRight', 'ArrowLeft', 'ArrowUp', 'ArrowDown', 'Home', 'End'].includes(event.key)) return
  const group = event.currentTarget as HTMLElement
  const choices = Array.from(group.querySelectorAll<HTMLButtonElement>(':scope > button[role="radio"]')).filter(button => !button.disabled)
  const current = (event.target as HTMLElement).closest<HTMLButtonElement>('button[role="radio"]')
  if (!current || !choices.length) return
  event.preventDefault()
  const direction = event.key === 'ArrowLeft' || event.key === 'ArrowUp' ? -1 : 1
  const index = event.key === 'Home' ? 0 : event.key === 'End' ? choices.length - 1 : (choices.indexOf(current) + direction + choices.length) % choices.length
  choices[index].click()
  choices[index].focus()
}
