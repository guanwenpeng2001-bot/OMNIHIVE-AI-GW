import { defineComponent, h, ref, watch, type Component } from 'vue'
import { useAuthStore } from '@/stores/auth'

/** Destroy page-local filters, data and dialogs when the authenticated principal changes. */
export function identityScoped(component: Component, admin: boolean) {
  return defineComponent({
    name: 'IdentityScopedPage',
    setup() {
      const auth = useAuthStore()
      const revision = ref(0)
      // Token rotation alone is not a different principal. Never put a token into a DOM key.
      watch([() => auth.user?.id, () => auth.user?.role, () => auth.isAuthenticated, () => auth.sessionRevision], () => revision.value++, { flush: 'sync' })
      return () => auth.isAuthenticated && (!admin || auth.isAdmin)
        ? h(component, { key: revision.value }) : null
    }
  })
}
