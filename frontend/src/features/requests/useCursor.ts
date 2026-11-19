import { computed, ref, watch } from 'vue'

export function useCursor(filters: () => string) {
  const history = ref<(string | null)[]>([])
  const cursor = ref<string | null>(null)
  function reset() {
    history.value = []
    cursor.value = null
  }
  function next(value: string | null | undefined) {
    if (!value || value === cursor.value) return
    history.value.push(cursor.value)
    cursor.value = value
  }
  function previous() {
    if (history.value.length !== 0) cursor.value = history.value.pop() ?? null
  }
  watch(filters, reset, { flush: 'sync' })
  return { cursor, hasPrevious: computed(() => history.value.length !== 0), next, previous, reset }
}
