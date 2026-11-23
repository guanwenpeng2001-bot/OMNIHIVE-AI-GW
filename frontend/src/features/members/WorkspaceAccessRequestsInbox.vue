<script setup lang="ts">
import { useOverlayDialog } from '@/components/ui/utils'
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { apiClient } from '@/api/client'
import { classifyAsyncError } from '@/features/shared/asyncState'
import { getControlAPIBaseURL } from '@/features/shared/controlClient'
import {
  accessRequestsAPI,
  type ProjectAccessRequestItem
} from '@/features/onboarding/accessRequests'
import { membersV2API } from './api'
import {
  hydrateProjectGrantsFromResponse,
  isProtectedSharedDefaultProject,
  resolveCasMutationError,
  resolvePresetActions
} from './cas'
import type {
  CasConflictState,
  GrantPresetId,
  IdentityMemberGrants,
  IdentityProject,
  ProjectGrantPair
} from './types'

const props = withDefaults(
  defineProps<{
    workspaceId: string
    workspaceIsDefault?: boolean
    projects: IdentityProject[]
    grantActionOptions: ReadonlyArray<{ action: string; label: string }>
    autoLoad?: boolean
  }>(),
  {
    workspaceIsDefault: false,
    autoLoad: true
  }
)

const emit = defineEmits<{
  (e: 'decided', request: ProjectAccessRequestItem): void
}>()

const { t } = useI18n()

const loading = ref(false)
const loadingMore = ref(false)
const loadError = ref<string | null>(null)
const requests = ref<ProjectAccessRequestItem[]>([])
const nextCursor = ref<string | null>(null)

const activeRequest = ref<ProjectAccessRequestItem | null>(null)
const selectedProjectId = ref<string>('')
const selectedActions = ref<string[]>([])
const memberVersion = ref<string>('1')
const memberExistingGrants = ref<ProjectGrantPair[]>([])
const loadingMemberGrants = ref(false)
const decisionBusy = ref(false)
const decisionError = ref<string | null>(null)
const decisionConflict = ref<CasConflictState | null>(null)

const grantableActionSet = computed(
  () => new Set(props.grantActionOptions.map((item) => item.action))
)

const editableTeamProjects = computed<IdentityProject[]>(() =>
  props.projects.filter(
    (proj) =>
      (proj.state ?? 'active') !== 'archived' &&
      !isProtectedSharedDefaultProject({ is_default: props.workspaceIsDefault }, proj)
  )
)

const existingActionsForSelectedProject = computed<string[]>(() => {
  if (!selectedProjectId.value) return []
  const row = memberExistingGrants.value.find((r) => r.project_id === selectedProjectId.value)
  return row ? [...row.actions].sort() : []
})

const newlyAddedActionsForSelectedProject = computed<string[]>(() => {
  const existingSet = new Set(existingActionsForSelectedProject.value)
  return selectedActions.value.filter((act) => !existingSet.has(act)).sort()
})

function resolveRequesterLabel(req: ProjectAccessRequestItem): string {
  const uid = req.requester_user_id || req.requester_id || req.requester_member_id || ''
  if (req.requester_username && req.requester_email) {
    return `${req.requester_username} (${req.requester_email}) · #${uid}`
  }
  if (req.requester_email) return `${req.requester_email} · #${uid}`
  if (req.requester_username) return `${req.requester_username} · #${uid}`
  return uid ? `#${uid}` : '—'
}

function resolveRequestedProjectLabel(req: ProjectAccessRequestItem): string {
  const targetId = req.approved_project_id || req.requested_project_id
  if (!targetId) return t('membersAccess.inbox.unspecifiedProject')
  if (req.approved_project_name) return `${req.approved_project_name} (#${targetId})`
  if (req.requested_project_name) return `${req.requested_project_name} (#${targetId})`
  const match = props.projects.find((p) => p.id === targetId)
  return match ? `${match.name} (#${match.id})` : `#${targetId}`
}

async function fetchRequesterGrants(
  workspaceId: string,
  requesterId: string
): Promise<IdentityMemberGrants | null> {
  if (typeof membersV2API.getMemberGrants === 'function') {
    return membersV2API.getMemberGrants(workspaceId, requesterId)
  }
  const res = await apiClient.get<IdentityMemberGrants>(
    `/workspaces/${encodeURIComponent(workspaceId)}/members/${encodeURIComponent(requesterId)}/grants`,
    { baseURL: getControlAPIBaseURL() }
  )
  return res.data ?? null
}

async function loadRequests(reset = true): Promise<void> {
  if (!props.workspaceId) return
  if (reset) {
    loading.value = true
    loadError.value = null
  } else {
    loadingMore.value = true
  }
  try {
    const page = await accessRequestsAPI.list(props.workspaceId, {
      view: 'review',
      cursor: reset ? undefined : (nextCursor.value ?? undefined),
      limit: 50
    })
    if (reset) {
      requests.value = page.items
    } else {
      const seen = new Set(requests.value.map((r) => r.id))
      requests.value = [...requests.value, ...page.items.filter((r) => !seen.has(r.id))]
    }
    nextCursor.value = page.next_cursor
  } catch (error) {
    const classified = classifyAsyncError(error)
    if (classified.status !== 403) {
      loadError.value = classified.message
    }
  } finally {
    loading.value = false
    loadingMore.value = false
  }
}

async function loadRequesterMemberState(req: ProjectAccessRequestItem): Promise<void> {
  const requesterId = req.requester_user_id || req.requester_id || req.requester_member_id || ''
  if (!props.workspaceId || !requesterId) return
  loadingMemberGrants.value = true
  try {
    const grants = await fetchRequesterGrants(props.workspaceId, requesterId)
    if (grants) {
      memberVersion.value = String(grants.version || '1')
      memberExistingGrants.value = hydrateProjectGrantsFromResponse(grants)
    }
  } catch {
    // Keep default memberVersion if endpoint fails; submit will re-verify
  } finally {
    loadingMemberGrants.value = false
  }
}

function openApproveModal(req: ProjectAccessRequestItem): void {
  activeRequest.value = req
  decisionError.value = null
  decisionConflict.value = null
  const preferredProject =
    (req.requested_project_id &&
      editableTeamProjects.value.find((p) => p.id === req.requested_project_id)?.id) ||
    editableTeamProjects.value[0]?.id ||
    ''
  selectedProjectId.value = preferredProject
  selectedActions.value = resolvePresetActions('caller', grantableActionSet.value)
  memberVersion.value = '1'
  memberExistingGrants.value = []
  void loadRequesterMemberState(req)
}

function applyPreset(preset: GrantPresetId): void {
  selectedActions.value = resolvePresetActions(preset, grantableActionSet.value)
}

function toggleAction(action: string, checked: boolean): void {
  if (checked) {
    if (!selectedActions.value.includes(action)) {
      selectedActions.value = [...selectedActions.value, action].sort()
    }
  } else {
    selectedActions.value = selectedActions.value.filter((a) => a !== action)
  }
}

async function handleConfirmApprove(): Promise<void> {
  const req = activeRequest.value
  if (!req || !props.workspaceId || !selectedProjectId.value || selectedActions.value.length === 0) {
    return
  }
  decisionBusy.value = true
  decisionError.value = null
  decisionConflict.value = null
  try {
    const updated = await accessRequestsAPI.decide(props.workspaceId, req.id, req.version, {
      decision: 'approve',
      project_id: selectedProjectId.value,
      add_actions: selectedActions.value,
      member_version: memberVersion.value
    })
    requests.value = requests.value.map((item) => (item.id === updated.id ? updated : item))
    activeRequest.value = null
    emit('decided', updated)
  } catch (error) {
    const resolved = resolveCasMutationError(error, 'access_request', req.id, req.version)
    if (resolved.kind === 'conflict') {
      decisionConflict.value = resolved.conflict
      await Promise.all([loadRequests(true), loadRequesterMemberState(req)])
      const latestReq = requests.value.find((item) => item.id === req.id)
      if (latestReq) {
        activeRequest.value = latestReq
      }
    } else {
      decisionError.value = resolved.message
    }
  } finally {
    decisionBusy.value = false
  }
}

async function handleDecline(req: ProjectAccessRequestItem): Promise<void> {
  if (!props.workspaceId || decisionBusy.value) return
  decisionBusy.value = true
  decisionError.value = null
  try {
    const updated = await accessRequestsAPI.decide(props.workspaceId, req.id, req.version, {
      decision: 'decline'
    })
    requests.value = requests.value.map((item) => (item.id === updated.id ? updated : item))
    if (activeRequest.value?.id === req.id) {
      activeRequest.value = null
    }
    emit('decided', updated)
  } catch (error) {
    const classified = classifyAsyncError(error)
    loadError.value = classified.message
  } finally {
    decisionBusy.value = false
  }
}

watch(
  () => props.workspaceId,
  (next) => {
    if (next && props.autoLoad) {
      void loadRequests(true)
    }
  }
)

onMounted(() => {
  if (props.workspaceId && props.autoLoad) {
    void loadRequests(true)
  }
})
const panelRef = ref<HTMLElement | null>(null)
useOverlayDialog({
  open: computed(() => Boolean(activeRequest.value)),
  panelRef,
  onClose: () => { activeRequest.value = null }
})
</script>

<template>
  <section
    data-testid="workspace-access-requests-inbox"
    class="rounded-xl border border-[var(--border)] bg-[var(--card)] p-4 space-y-4"
  >
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div class="space-y-1">
        <div class="flex items-center gap-2">
          <h3 class="text-sm font-semibold text-[var(--foreground)]">
            {{ t('membersAccess.inbox.title') }}
          </h3>
          <span
            data-testid="inbox-pending-count"
            class="rounded-full bg-amber-500/15 px-2 py-0.5 font-mono text-[11px] font-semibold tabular-nums text-amber-600 dark:text-amber-400"
          >
            {{ requests.filter((r) => r.state === 'pending').length }}
          </span>
        </div>
        <p class="text-xs text-[var(--muted-foreground)]">
          {{ t('membersAccess.inbox.subtitle') }}
        </p>
      </div>
      <button
        type="button"
        data-testid="refresh-inbox-requests-btn"
        class="btn btn-secondary btn-sm text-xs"
        :disabled="loading"
        @click="loadRequests(true)"
      >
        {{ t('membersAccess.inbox.refresh') }}
      </button>
    </div>

    <p v-if="loadError" data-testid="inbox-load-error" role="alert" class="text-xs text-red-600 dark:text-red-400">
      {{ loadError }}
    </p>

    <div v-if="requests.length === 0 && !loading" data-testid="inbox-empty-state" class="py-4 text-center text-xs text-[var(--muted-foreground)]">
      <p class="font-medium text-[var(--foreground)]">{{ t('membersAccess.inbox.emptyTitle') }}</p>
      <p class="mt-0.5">{{ t('membersAccess.inbox.emptyDescription') }}</p>
    </div>

    <div v-else class="overflow-x-auto">
      <table class="w-full text-left text-xs">
        <thead>
          <tr class="border-b border-[var(--border)] font-mono text-[11px] uppercase text-[var(--muted-foreground)]">
            <th class="pb-2 pr-3">{{ t('membersAccess.inbox.requesterColumn') }}</th>
            <th class="pb-2 pr-3">{{ t('membersAccess.inbox.targetColumn') }}</th>
            <th class="pb-2 pr-3">{{ t('membersAccess.inbox.stateColumn') }}</th>
            <th class="pb-2 text-right">{{ t('membersAccess.inbox.actionsColumn') }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-[var(--border)]">
          <tr
            v-for="req in requests"
            :key="req.id"
            :data-testid="`access-request-row-${req.id}`"
          >
            <td class="py-2.5 pr-3 font-mono text-xs text-[var(--foreground)]">
              {{ resolveRequesterLabel(req) }}
            </td>
            <td class="py-2.5 pr-3">
              <div class="font-medium text-[var(--foreground)]">
                {{ resolveRequestedProjectLabel(req) }}
              </div>
              <p v-if="req.note" class="mt-0.5 text-[11px] text-[var(--muted-foreground)]">
                {{ req.note }}
              </p>
            </td>
            <td class="py-2.5 pr-3">
              <span
                :data-testid="`access-request-state-${req.id}`"
                class="inline-flex rounded px-2 py-0.5 font-mono text-[11px] font-medium"
                :class="
                  req.state === 'pending'
                    ? 'bg-amber-500/15 text-amber-600 dark:text-amber-400'
                    : req.state === 'approved'
                      ? 'bg-emerald-500/15 text-emerald-600 dark:text-emerald-400'
                      : 'bg-zinc-500/15 text-[var(--muted-foreground)]'
                "
              >
                {{
                  req.state === 'pending'
                    ? t('membersAccess.inbox.statePending')
                    : req.state === 'approved'
                      ? t('membersAccess.inbox.stateApproved')
                      : t('membersAccess.inbox.stateDeclined')
                }}
              </span>
            </td>
            <td class="py-2.5 text-right">
              <div v-if="req.state === 'pending'" class="inline-flex items-center gap-1.5">
                <button
                  type="button"
                  :data-testid="`open-approve-request-${req.id}`"
                  class="btn btn-primary btn-sm px-2.5 py-1 text-xs"
                  :disabled="decisionBusy"
                  @click="openApproveModal(req)"
                >
                  {{ t('membersAccess.inbox.approveBtn') }}
                </button>
                <button
                  type="button"
                  :data-testid="`decline-request-${req.id}`"
                  class="btn btn-secondary btn-sm px-2.5 py-1 text-xs"
                  :disabled="decisionBusy"
                  @click="handleDecline(req)"
                >
                  {{ t('membersAccess.inbox.declineBtn') }}
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="nextCursor" class="flex justify-center pt-1">
      <button
        type="button"
        data-testid="load-more-inbox-requests-btn"
        class="btn btn-secondary btn-sm text-xs"
        :disabled="loadingMore"
        @click="loadRequests(false)"
      >
        {{ t('membersAccess.inbox.loadMore') }}
      </button>
    </div>

    <!-- Approve Request Dialog -->
    <div
      v-if="activeRequest"
      data-testid="approve-access-request-modal"
      @click.self="activeRequest = null"
      role="dialog"
      aria-modal="true"
      :aria-label="t('membersAccess.inbox.approveModalTitle')"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
    >
      <div ref="panelRef" tabindex="-1" class="max-h-[calc(100dvh-2rem)] overflow-y-auto w-full max-w-xl rounded-xl border border-[var(--border)] bg-[var(--card)] p-5 shadow-xl space-y-4">
        <div class="flex items-start justify-between gap-3">
          <div>
            <h4 class="text-base font-semibold text-[var(--foreground)]">
              {{ t('membersAccess.inbox.approveModalTitle') }}
            </h4>
            <p class="mt-0.5 font-mono text-xs text-[var(--muted-foreground)]">
              {{ resolveRequesterLabel(activeRequest) }} · rev #{{ activeRequest.version }} · member rev #{{ memberVersion }}
            </p>
          </div>
          <button
            type="button"
            class="btn btn-ghost btn-sm text-xs"
            @click="activeRequest = null"
          >
            {{ t('common.cancel') }}
          </button>
        </div>

        <div
          v-if="decisionConflict"
          data-testid="approve-request-conflict-banner"
          role="alert"
          class="rounded-lg border border-amber-500/40 bg-amber-500/10 p-3 text-xs space-y-1"
        >
          <p class="font-semibold text-[var(--foreground)]">
            {{ t('rbac.cas.conflict412') }}
          </p>
        </div>

        <p v-if="decisionError" role="alert" class="text-xs text-red-600 dark:text-red-400">
          {{ decisionError }}
        </p>

        <div class="space-y-1.5">
          <label for="approve-target-project-select" class="block text-xs font-semibold text-[var(--foreground)]">
            {{ t('membersAccess.inbox.selectTargetProject') }}
          </label>
          <select
            id="approve-target-project-select"
            v-model="selectedProjectId"
            data-testid="approve-target-project-select"
            class="input w-full text-xs"
          >
            <option
              v-for="proj in editableTeamProjects"
              :key="proj.id"
              :value="proj.id"
            >
              {{ proj.name }} (#{{ proj.id }})
            </option>
          </select>
          <p v-if="workspaceIsDefault" class="text-[11px] text-[var(--muted-foreground)]">
            {{ t('membersAccess.matrix.sharedDefaultLocked') }}
          </p>
        </div>

        <div class="space-y-2">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <span class="text-xs font-semibold text-[var(--foreground)]">
              {{ t('membersAccess.inbox.selectPresetOrActions') }}
            </span>
            <div class="flex flex-wrap items-center gap-1">
              <button
                type="button"
                data-testid="approve-preset-observer"
                class="btn btn-secondary btn-sm px-2 py-0.5 text-[11px]"
                @click="applyPreset('observer')"
              >
                {{ t('membersAccess.matrix.presetObserver') }}
              </button>
              <button
                type="button"
                data-testid="approve-preset-caller"
                class="btn btn-secondary btn-sm px-2 py-0.5 text-[11px]"
                @click="applyPreset('caller')"
              >
                {{ t('membersAccess.matrix.presetCaller') }}
              </button>
              <button
                type="button"
                data-testid="approve-preset-issuer"
                class="btn btn-secondary btn-sm px-2 py-0.5 text-[11px]"
                @click="applyPreset('issuer')"
              >
                {{ t('membersAccess.matrix.presetIssuer') }}
              </button>
              <button
                type="button"
                data-testid="approve-preset-maintainer"
                class="btn btn-secondary btn-sm px-2 py-0.5 text-[11px]"
                @click="applyPreset('maintainer')"
              >
                {{ t('membersAccess.matrix.presetMaintainer') }}
              </button>
            </div>
          </div>

          <div
            data-testid="approve-actions-checkboxes"
            class="grid max-h-44 grid-cols-1 gap-1.5 overflow-y-auto rounded-lg border border-[var(--border)] p-2.5 sm:grid-cols-2"
          >
            <label
              v-for="item in grantActionOptions"
              :key="item.action"
              class="flex items-center gap-2 text-xs text-[var(--foreground)]"
            >
              <input
                type="checkbox"
                :checked="selectedActions.includes(item.action)"
                :data-testid="`approve-action-checkbox-${item.action}`"
                @change="toggleAction(item.action, ($event.target as HTMLInputElement).checked)"
              />
              <span>{{ item.label }} ({{ item.action }})</span>
            </label>
          </div>
        </div>

        <!-- Exact Additive Diff Preview -->
        <div
          data-testid="approve-additive-diff-preview"
          class="rounded-lg border border-[var(--border)] bg-[var(--surface-2,rgba(0,0,0,0.03))] p-3 text-xs space-y-1.5"
        >
          <div class="font-semibold text-[var(--foreground)]">
            {{ t('membersAccess.inbox.additiveDiffTitle') }}
          </div>
          <div class="flex flex-wrap items-center gap-1.5">
            <span class="text-[var(--muted-foreground)]">
              {{ t('membersAccess.inbox.existingActionsOnProject') }}
            </span>
            <template v-if="existingActionsForSelectedProject.length > 0">
              <span
                v-for="act in existingActionsForSelectedProject"
                :key="`exist-${act}`"
                class="rounded border border-[var(--border)] px-1.5 py-0.5 font-mono text-[11px]"
              >
                {{ act }}
              </span>
            </template>
            <span v-else class="font-mono text-[11px] text-[var(--muted-foreground)]">
              {{ t('membersAccess.inbox.noExistingActions') }}
            </span>
          </div>
          <div class="flex flex-wrap items-center gap-1.5">
            <span class="text-[var(--muted-foreground)]">
              {{ t('membersAccess.inbox.addingActionsOnProject') }}
            </span>
            <template v-if="newlyAddedActionsForSelectedProject.length > 0">
              <span
                v-for="act in newlyAddedActionsForSelectedProject"
                :key="`add-${act}`"
                class="rounded bg-emerald-500/15 px-1.5 py-0.5 font-mono text-[11px] font-semibold text-emerald-700 dark:text-emerald-300"
              >
                + {{ act }}
              </span>
            </template>
            <span v-else class="font-mono text-[11px] text-[var(--muted-foreground)]">
              {{
                selectedActions.length > 0
                  ? selectedActions.map((a) => `+ ${a}`).join(', ')
                  : t('membersAccess.inbox.noNewActionsSelected')
              }}
            </span>
          </div>
        </div>

        <div class="flex flex-wrap items-center justify-end gap-2">
          <button
            type="button"
            class="btn btn-secondary btn-sm text-xs"
            @click="activeRequest = null"
          >
            {{ t('common.cancel') }}
          </button>
          <button
            type="button"
            data-testid="confirm-approve-request-btn"
            class="btn btn-primary btn-sm text-xs"
            :disabled="
              decisionBusy ||
              loadingMemberGrants ||
              !selectedProjectId ||
              selectedActions.length === 0
            "
            @click="handleConfirmApprove"
          >
            {{ t('membersAccess.inbox.confirmApprove') }}
          </button>
        </div>
      </div>
    </div>
  </section>
</template>
