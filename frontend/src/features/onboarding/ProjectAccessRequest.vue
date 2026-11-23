<script setup lang="ts">
import { useOverlayDialog } from '@/components/ui/utils'
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useConsoleScopeStore } from '@/stores/consoleScope'
import { classifyAsyncError } from '@/features/shared/asyncState'
import { createIdempotencyKey } from '@/features/shared/controlClient'
import { isProtectedSharedDefaultProject } from '@/features/members/cas'
import type { IdentityProject, IdentityWorkspace } from '@/features/members/types'
import {
  accessRequestsAPI,
  validateAccessRequestNote,
  type ProjectAccessRequestItem
} from './accessRequests'

const props = withDefaults(
  defineProps<{
    mode?: 'inline' | 'modal'
    open?: boolean
    initialWorkspaceId?: string
    initialProjectId?: string
    defaultNote?: string
    workspacesOverride?: IdentityWorkspace[]
    autoLoad?: boolean
  }>(),
  {
    mode: 'inline',
    open: true,
    initialWorkspaceId: '',
    initialProjectId: '',
    defaultNote: '',
    workspacesOverride: undefined,
    autoLoad: true
  }
)

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'submitted', request: ProjectAccessRequestItem): void
  (e: 'switched-project', payload: { workspaceId: string; projectId: string }): void
}>()

const { t } = useI18n()
let appStore: ReturnType<typeof useAppStore> | null = null
let consoleScopeStore: ReturnType<typeof useConsoleScopeStore> | null = null
try {
  appStore = useAppStore()
  consoleScopeStore = useConsoleScopeStore()
} catch {
  appStore = null
  consoleScopeStore = null
}

const selectedWorkspaceId = ref<string>(props.initialWorkspaceId || '')
const selectedProjectId = ref<string>(props.initialProjectId || '')
const note = ref<string>(props.defaultNote || '')
const draftIdempotencyKey = ref<string>(createIdempotencyKey())

const submitting = ref(false)
const submitError = ref<string | null>(null)
const loadingMine = ref(false)
const mineError = ref<string | null>(null)
const myRequests = ref<ProjectAccessRequestItem[]>([])
const loadingMoreWorkspaces = ref(false)
const loadingMoreProjects = ref(false)

const availableWorkspaces = computed<IdentityWorkspace[]>(() => {
  if (Array.isArray(props.workspacesOverride) && props.workspacesOverride.length > 0) {
    return props.workspacesOverride
  }
  return (consoleScopeStore?.scopes as IdentityWorkspace[] | undefined) ?? []
})

const selectedWorkspace = computed<IdentityWorkspace | null>(
  () =>
    availableWorkspaces.value.find((ws) => ws.workspace_id === selectedWorkspaceId.value) ??
    availableWorkspaces.value[0] ??
    null
)

const visibleProjects = computed<IdentityProject[]>(() => {
  const list = (selectedWorkspace.value?.projects as IdentityProject[] | undefined) ?? []
  return list.filter((proj) => !isProtectedSharedDefaultProject(selectedWorkspace.value, proj))
})

const hasProtectedDefaultInWorkspace = computed<boolean>(() => {
  const list = (selectedWorkspace.value?.projects as IdentityProject[] | undefined) ?? []
  return list.some((proj) => isProtectedSharedDefaultProject(selectedWorkspace.value, proj))
})

const canLoadMoreWorkspaces = computed<boolean>(() =>
  Boolean((appStore as { scopesNextCursor?: string | null } | null)?.scopesNextCursor)
)

const canLoadMoreProjects = computed<boolean>(() =>
  Boolean(selectedWorkspace.value?.projects_next_cursor)
)

const noteValidation = computed(() => validateAccessRequestNote(note.value))

const canSubmit = computed<boolean>(
  () =>
    Boolean(selectedWorkspace.value?.workspace_id) &&
    noteValidation.value.valid &&
    !submitting.value
)

const latestPendingRequest = computed<ProjectAccessRequestItem | null>(
  () => myRequests.value.find((item) => item.state === 'pending') ?? null
)

const latestApprovedRequest = computed<ProjectAccessRequestItem | null>(
  () => myRequests.value.find((item) => item.state === 'approved') ?? null
)

const latestDeclinedRequest = computed<ProjectAccessRequestItem | null>(
  () =>
    !latestPendingRequest.value && !latestApprovedRequest.value
      ? (myRequests.value.find((item) => item.state === 'declined') ?? null)
      : null
)

function resolveProjectDisplayName(req: ProjectAccessRequestItem): string {
  const targetId = req.approved_project_id || req.requested_project_id || ''
  if (req.approved_project_name) return req.approved_project_name
  if (req.requested_project_name) return req.requested_project_name
  const projects = (selectedWorkspace.value?.projects as IdentityProject[] | undefined) ?? []
  const match = projects.find((proj) => proj.id === targetId)
  if (match?.name) return `${match.name} (#${match.id})`
  return targetId ? `#${targetId}` : selectedWorkspace.value?.name || req.workspace_id
}

watch([selectedWorkspaceId, selectedProjectId, note], () => {
  draftIdempotencyKey.value = createIdempotencyKey()
  submitError.value = null
})

watch(
  () => props.initialWorkspaceId,
  (next) => {
    if (next && next !== selectedWorkspaceId.value) {
      selectedWorkspaceId.value = next
    }
  }
)

watch(
  availableWorkspaces,
  (list) => {
    if (!selectedWorkspaceId.value && list.length > 0) {
      const activeWsId = consoleScopeStore?.activeScope?.workspaceId
      const preferred =
        (activeWsId && list.find((ws) => ws.workspace_id === activeWsId)?.workspace_id) ||
        list[0]?.workspace_id ||
        ''
      if (preferred) {
        selectedWorkspaceId.value = preferred
      }
    }
  },
  { immediate: true }
)

watch(selectedWorkspaceId, (wsId) => {
  if (wsId && props.autoLoad) {
    void refreshMyRequests()
  }
})

async function ensureScopesLoaded(): Promise<void> {
  if (
    (!props.workspacesOverride || props.workspacesOverride.length === 0) &&
    availableWorkspaces.value.length === 0 &&
    typeof consoleScopeStore?.fetchScopes === 'function'
  ) {
    try {
      await consoleScopeStore.fetchScopes()
    } catch {
      // Fail closed; error state handled by store or empty list
    }
  }
}

async function handleLoadMoreWorkspaces(): Promise<void> {
  const cursor = (appStore as { scopesNextCursor?: string | null } | null)?.scopesNextCursor
  if (!cursor || typeof consoleScopeStore?.fetchScopes !== 'function') return
  loadingMoreWorkspaces.value = true
  try {
    await consoleScopeStore.fetchScopes({ cursor })
  } finally {
    loadingMoreWorkspaces.value = false
  }
}

async function handleLoadMoreProjects(): Promise<void> {
  const wsId = selectedWorkspace.value?.workspace_id
  if (
    !wsId ||
    typeof (appStore as { loadMoreWorkspaceProjects?: (id: string) => Promise<unknown> } | null)
      ?.loadMoreWorkspaceProjects !== 'function'
  ) {
    return
  }
  loadingMoreProjects.value = true
  try {
    await (
      appStore as { loadMoreWorkspaceProjects: (id: string) => Promise<unknown> }
    ).loadMoreWorkspaceProjects(wsId)
  } finally {
    loadingMoreProjects.value = false
  }
}

async function refreshMyRequests(): Promise<void> {
  const wsId = selectedWorkspace.value?.workspace_id || selectedWorkspaceId.value
  if (!wsId) return
  loadingMine.value = true
  mineError.value = null
  try {
    const page = await accessRequestsAPI.list(wsId, { view: 'mine', limit: 20 })
    myRequests.value = page.items
  } catch (error) {
    const classified = classifyAsyncError(error)
    mineError.value = classified.message
  } finally {
    loadingMine.value = false
  }
}

async function handleSubmitRequest(): Promise<void> {
  const wsId = selectedWorkspace.value?.workspace_id || selectedWorkspaceId.value
  if (!wsId || !canSubmit.value) return
  submitting.value = true
  submitError.value = null
  try {
    const created = await accessRequestsAPI.create(
      wsId,
      {
        note: noteValidation.value.trimmed,
        requested_project_id: selectedProjectId.value || undefined
      },
      draftIdempotencyKey.value
    )
    myRequests.value = [created, ...myRequests.value.filter((item) => item.id !== created.id)]
    note.value = ''
    selectedProjectId.value = ''
    draftIdempotencyKey.value = createIdempotencyKey()
    emit('submitted', created)
  } catch (error) {
    const classified = classifyAsyncError(error)
    submitError.value = classified.message
  } finally {
    submitting.value = false
  }
}

async function handleSwitchToApprovedProject(req: ProjectAccessRequestItem): Promise<void> {
  const targetProjectId = req.approved_project_id || req.requested_project_id || ''
  if (!req.workspace_id || !targetProjectId) return
  consoleScopeStore?.selectWorkspaceScope?.(req.workspace_id, targetProjectId)
  emit('switched-project', {
    workspaceId: req.workspace_id,
    projectId: targetProjectId
  })
  if (
    (!props.workspacesOverride || props.workspacesOverride.length === 0) &&
    typeof consoleScopeStore?.fetchScopes === 'function'
  ) {
    try {
      await consoleScopeStore.fetchScopes({ force: true })
      consoleScopeStore?.selectWorkspaceScope?.(req.workspace_id, targetProjectId)
    } catch {
      // Keep selected scope
    }
  }
}

onMounted(async () => {
  if (!props.autoLoad) return
  await ensureScopesLoaded()
  if (selectedWorkspace.value?.workspace_id || selectedWorkspaceId.value) {
    await refreshMyRequests()
  }
})
const panelRef = ref<HTMLElement | null>(null)
useOverlayDialog({
  open: computed(() => props.mode === 'modal' && props.open),
  panelRef,
  onClose: () => emit('close')
})
</script>

<template>
  <div
    v-if="mode === 'inline' || open"
    data-testid="project-access-request"
    @click.self="mode === 'modal' && emit('close')"
    :class="
      mode === 'modal'
        ? 'fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4'
        : 'w-full'
    "
  >
    <div
      :class="
        mode === 'modal'
          ? 'max-h-[calc(100dvh-2rem)] w-full max-w-lg overflow-y-auto rounded-xl border border-[var(--border)] bg-[var(--card)] p-5 shadow-xl space-y-4'
          : 'rounded-xl border border-[var(--border)] bg-[var(--card)] p-4 space-y-4'
      "
      ref="panelRef"
      :tabindex="mode === 'modal' ? -1 : undefined"
      :role="mode === 'modal' ? 'dialog' : undefined"
      :aria-modal="mode === 'modal' ? 'true' : undefined"
      :aria-label="t('onboarding.request.modalTitle')"
    >
      <div class="flex items-start justify-between gap-3">
        <div class="space-y-1">
          <h3 class="text-base font-semibold text-[var(--foreground)]">
            {{ t('onboarding.request.modalTitle') }}
          </h3>
          <p class="text-xs text-[var(--muted-foreground)]">
            {{ t('onboarding.request.subtitle') }}
          </p>
        </div>
        <div class="flex items-center gap-2">
          <button
            type="button"
            data-testid="refresh-access-requests-btn"
            class="btn btn-secondary btn-sm text-xs"
            :disabled="loadingMine"
            @click="refreshMyRequests"
          >
            {{ t('onboarding.request.refreshBtn') }}
          </button>
          <button
            v-if="mode === 'modal'"
            type="button"
            data-testid="close-access-request-modal-btn"
            class="btn btn-ghost btn-sm text-xs"
            @click="emit('close')"
          >
            {{ t('common.cancel') }}
          </button>
        </div>
      </div>

      <!-- Approved request banner with explicit Switch CTA -->
      <div
        v-if="latestApprovedRequest"
        data-testid="access-request-approved-banner"
        role="status"
        class="rounded-lg border border-emerald-500/40 bg-emerald-500/10 p-3 text-xs space-y-2"
      >
        <p class="font-medium text-[var(--foreground)]">
          {{
            t('onboarding.request.approved', {
              project: resolveProjectDisplayName(latestApprovedRequest)
            })
          }}
        </p>
        <button
          v-if="latestApprovedRequest.approved_project_id || latestApprovedRequest.requested_project_id"
          type="button"
          data-testid="switch-to-approved-project-btn"
          class="btn btn-primary btn-sm text-xs"
          @click="handleSwitchToApprovedProject(latestApprovedRequest)"
        >
          {{ t('onboarding.request.switchNowBtn') }}
        </button>
      </div>

      <!-- Pending request banner -->
      <div
        v-if="latestPendingRequest"
        data-testid="access-request-pending-banner"
        role="status"
        class="rounded-lg border border-amber-500/40 bg-amber-500/10 p-3 text-xs space-y-1"
      >
        <p class="font-medium text-[var(--foreground)]">
          {{ t('onboarding.request.pending') }}
        </p>
        <p v-if="latestPendingRequest.note" class="text-[var(--muted-foreground)]">
          {{ latestPendingRequest.note }}
        </p>
      </div>

      <!-- Declined request banner -->
      <div
        v-if="latestDeclinedRequest"
        data-testid="access-request-declined-banner"
        role="status"
        class="rounded-lg border border-red-500/40 bg-red-500/10 p-3 text-xs"
      >
        <p class="font-medium text-[var(--foreground)]">
          {{ t('onboarding.request.declined') }}
        </p>
      </div>

      <form class="space-y-3" @submit.prevent="handleSubmitRequest">
        <div class="grid gap-3 sm:grid-cols-2">
          <div class="space-y-1">
            <label for="access-req-workspace-select" class="block text-xs font-medium text-[var(--foreground)]">
              {{ t('onboarding.request.workspaceLabel') }}
            </label>
            <select
              id="access-req-workspace-select"
              v-model="selectedWorkspaceId"
              data-testid="access-request-workspace-select"
              class="input w-full text-xs"
            >
              <option
                v-for="ws in availableWorkspaces"
                :key="ws.workspace_id"
                :value="ws.workspace_id"
              >
                {{ ws.name }} (#{{ ws.workspace_id }})
              </option>
            </select>
            <button
              v-if="canLoadMoreWorkspaces"
              type="button"
              data-testid="access-request-load-more-workspaces"
              class="text-[11px] font-medium text-[var(--brand,#d97706)] hover:underline"
              :disabled="loadingMoreWorkspaces"
              @click="handleLoadMoreWorkspaces"
            >
              {{ t('onboarding.request.loadMoreWorkspaces') }}
            </button>
          </div>

          <div class="space-y-1">
            <label for="access-req-project-select" class="block text-xs font-medium text-[var(--foreground)]">
              {{ t('onboarding.request.projectLabel') }}
            </label>
            <select
              id="access-req-project-select"
              v-model="selectedProjectId"
              data-testid="access-request-project-select"
              class="input w-full text-xs"
            >
              <option value="">
                {{ t('onboarding.request.projectPlaceholder') }}
              </option>
              <option
                v-for="proj in visibleProjects"
                :key="proj.id"
                :value="proj.id"
              >
                {{ proj.name }} (#{{ proj.id }})
              </option>
            </select>
            <p
              v-if="hasProtectedDefaultInWorkspace"
              data-testid="access-request-default-project-hint"
              class="text-[11px] text-[var(--muted-foreground)]"
            >
              {{ t('onboarding.request.defaultProjectHint') }}
            </p>
            <button
              v-if="canLoadMoreProjects"
              type="button"
              data-testid="access-request-load-more-projects"
              class="text-[11px] font-medium text-[var(--brand,#d97706)] hover:underline"
              :disabled="loadingMoreProjects"
              @click="handleLoadMoreProjects"
            >
              {{ t('onboarding.request.loadMoreProjects') }}
            </button>
          </div>
        </div>

        <div class="space-y-1">
          <div class="flex items-center justify-between">
            <label for="access-req-note-input" class="block text-xs font-medium text-[var(--foreground)]">
              {{ t('onboarding.request.noteLabel') }}
            </label>
            <span
              data-testid="access-request-note-count"
              class="font-mono text-[11px] tabular-nums text-[var(--muted-foreground)]"
            >
              {{ noteValidation.length }} / 500
            </span>
          </div>
          <textarea
            id="access-req-note-input"
            v-model="note"
            rows="3"
            maxlength="550"
            data-testid="access-request-note-input"
            class="input w-full text-xs"
            :placeholder="t('onboarding.request.notePlaceholder')"
          />
        </div>

        <p
          v-if="submitError || mineError"
          data-testid="access-request-error"
          role="alert"
          class="text-xs text-red-600 dark:text-red-400"
        >
          {{ submitError || mineError }}
        </p>

        <div class="flex flex-wrap items-center justify-end gap-2">
          <button
            type="submit"
            data-testid="submit-access-request-btn"
            class="btn btn-primary btn-sm text-xs"
            :disabled="!canSubmit"
          >
            {{
              submitting
                ? t('onboarding.request.submittingBtn')
                : t('onboarding.request.submitBtn')
            }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>
