import { ref } from 'vue'

/**
 * Tracks mounted PublicShell and Console AppLayout instances so App.vue can
 * avoid duplicate ICP footers on public pages and offset the fixed left sidebar
 * on console pages.
 */
export const activePublicShellCount = ref(0)
export const activeConsoleShellCount = ref(0)
