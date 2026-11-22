import { defineComponent, h, onBeforeUnmount, onMounted, ref, watch, type Component } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useRoute, type Router } from 'vue-router'
import { SCOPE_CHANGED_EVENT, useAppStore } from '@/stores/app'

/** Recover an already-open protected route when another tab revokes its identity. */
export function watchIdentityRouteRecovery(router: Router, auth: ReturnType<typeof useAuthStore>) {
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
export function identityScoped(component: Component, admin: boolean, remountOnScope = true) {
  return defineComponent({
    name: 'IdentityScopedPage',
    setup() {
      const auth = useAuthStore()
      const route = useRoute()
      const revision = ref(0)
      const bumpRevision = () => {
        if (remountOnScope) revision.value++
      }
      watch(() => route?.params.workspace_id, (value) => {
        const workspaceId = typeof value === 'string' && /^[1-9]\d*$/.test(value) ? value : null
        if (workspaceId) {
          const app = useAppStore()
          if (app.activeScope.workspaceId !== workspaceId || app.activeScope.mode !== 'workspace') {
            app.selectWorkspaceScope(workspaceId, null)
          }
        }
        revision.value++
      }, { immediate: true, flush: 'sync' })
      // Token rotation alone is not a different principal. Never put a token into a DOM key.
      watch([() => auth.user?.id, () => auth.user?.role, () => auth.isAuthenticated, () => auth.sessionRevision], () => revision.value++, { flush: 'sync' })
      onMounted(() => {
        if (typeof window !== 'undefined') {
          window.addEventListener(SCOPE_CHANGED_EVENT, bumpRevision)
        }
      })
      onBeforeUnmount(() => {
        if (typeof window !== 'undefined') {
          window.removeEventListener(SCOPE_CHANGED_EVENT, bumpRevision)
        }
      })
      return () => auth.isAuthenticated && (!admin || auth.isAdmin)
        ? h(component, { key: revision.value }) : null
    }
  })
}

/** Share prefetch/navigation and retain one wrapper type; failed chunks can retry. */
export function lazyIdentityScoped(load: () => Promise<{ default: Component }>, admin: boolean, remountOnScope = true) {
  let pending: Promise<Component> | undefined
  return () => {
    if (!pending) {
      pending = load().then(module => identityScoped(module.default, admin, remountOnScope)).catch(error => {
        pending = undefined
        throw error
      })
    }
    return pending
  }
}
