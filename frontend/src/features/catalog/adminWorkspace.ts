import type { useAppStore } from '@/stores/app'

type WorkspaceDirectory = Pick<ReturnType<typeof useAppStore>,
  'activeScope' | 'scopes' | 'scopesLoaded' | 'scopesNextCursor' | 'fetchScopes'>

export function catalogWorkspace(store: WorkspaceDirectory): string {
  return store.activeScope.workspaceId || store.scopes.find(scope => scope.is_default)?.workspace_id || ''
}

export async function resolveCatalogWorkspace(store: WorkspaceDirectory): Promise<string> {
  if (!catalogWorkspace(store) && !store.scopesLoaded) await store.fetchScopes()
  while (!catalogWorkspace(store) && store.scopesNextCursor) {
    const cursor = store.scopesNextCursor
    const page = await store.fetchScopes({ cursor })
    if (!page || store.scopesNextCursor === cursor) break
  }
  return catalogWorkspace(store)
}
