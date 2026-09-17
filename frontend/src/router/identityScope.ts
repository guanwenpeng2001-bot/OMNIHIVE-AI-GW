import { defineComponent, h, ref, watch, type Component } from 'vue'
import { useAuthStore } from '@/stores/auth'
import type { Router } from 'vue-router'

/** Recover an already-open protected route when another tab revokes its identity. */
export function watchIdentityRouteRecovery(router: Router) {
  const auth = useAuthStore()
  return watch([() => auth.isAuthenticated, () => auth.isAdmin], () => {
    const route = router.currentRoute.value
    if (route.meta.requiresAuth === false) return
    if (!auth.isAuthenticated) {
      const redirect = route.fullPath.startsWith('/') && !route.fullPath.startsWith('//') &&
        !route.fullPath.includes('\\') ? route.fullPath : '/dashboard'
      void router.replace({ path: '/login', query: { redirect } })
    } else if (route.meta.requiresAdmin && !auth.isAdmin) {
      void router.replace('/dashboard')
    }
  })
}

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
