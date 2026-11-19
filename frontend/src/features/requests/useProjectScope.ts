import { computed, onMounted, onScopeDispose, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { projectScope, requestsApi, scopeQuery, scopeValue, type ProjectScope } from './api'
import { useScopedRead } from './useScopedRead'

export function firstAvailableScope(
  workspaces: ReadonlyArray<{ workspace_id: string; projects: ReadonlyArray<{ id: string }> }>
): { workspaceId: string; projectId: string } | null {
  const workspace = workspaces.find((item) => item.projects.length > 0)
  const project = workspace?.projects[0]
  return workspace && project ? { workspaceId: workspace.workspace_id, projectId: project.id } : null
}

export function useProjectScope() {
  const app = useAppStore()
  const auth = useAuthStore()
  const route = useRoute()
  const router = useRouter()
  const admin = computed(() => route.path.startsWith('/admin/'))
  const scope = computed(() => projectScope(app.activeScope.workspaceId, app.activeScope.projectId))
  const identityKey = computed(() => JSON.stringify([auth.user?.id ?? '', auth.sessionRevision, app.scopeRevision, scopeValue(scope.value)]))
  const scopeKey = computed(() => scope.value ? identityKey.value : '')

  function choose(next: ProjectScope) {
    app.selectWorkspaceScope(next.workspaceId, next.projectId)
  }

  watch(
    () => [app.scopesLoaded, app.scopes, scopeValue(scope.value)] as const,
    ([loaded, workspaces, current]) => {
      if (!loaded || current) return
      const next = firstAvailableScope(workspaces)
      if (next) {
        app.selectWorkspaceScope(next.workspaceId, next.projectId)
      }
    },
    { immediate: true }
  )

  watch(
    () => [route.query.workspace_id, route.query.project_id],
    ([workspace, project]) => {
      const linked = projectScope(workspace, project)
      if (linked && scopeValue(linked) !== scopeValue(scope.value)) choose(linked)
    },
    { immediate: true },
  )

  watch(scopeKey, () => {
    if (!route.query.workspace_id && !route.query.project_id) return
    const linked = projectScope(route.query.workspace_id, route.query.project_id)
    if (scopeValue(linked) === scopeValue(scope.value)) return
    const query = { ...route.query }
    delete query.workspace_id
    delete query.project_id
    if (scope.value) Object.assign(query, scopeQuery(scope.value))
    void router.replace({ query })
  })

  const access = useScopedRead(() => scopeKey.value, async signal => {
    const target = scope.value!
    const result = await requestsApi.access(target, signal)
    if (result.workspace_id !== target.workspaceId || result.project_id !== target.projectId) {
      throw { status: 404, reason: 'NOT_FOUND' }
    }
    return result
  })

  const now = ref(Date.now())
  let clock: ReturnType<typeof setInterval> | undefined
  function refreshExpired() {
    now.value = Date.now()
    const expires = Date.parse(access.data.value?.expires_at ?? '')
    if (Number.isFinite(expires) && expires <= now.value && !access.loading.value && !access.error.value) {
      void access.refresh()
    }
  }
  onMounted(() => {
    document.addEventListener('visibilitychange', refreshExpired)
    clock = setInterval(refreshExpired, 1000)
  })
  onScopeDispose(() => {
    if (clock !== undefined) clearInterval(clock)
    if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', refreshExpired)
  })

  function can(action: string): boolean {
    const result = access.data.value
    return Boolean(result && !access.error.value && Date.parse(result.expires_at) > now.value &&
      result.actions.includes(action))
  }

  return { scope, scopeKey, identityKey, admin, access, choose, can }
}
