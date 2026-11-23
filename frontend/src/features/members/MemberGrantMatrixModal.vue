<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { apiClient } from '@/api/client'
import { Badge, Button, Checkbox, Modal, Skeleton, pushUiToast } from '@/components/ui'
import { classifyAsyncError } from '@/features/shared/asyncState'
import { getControlAPIBaseURL } from '@/features/shared/controlClient'
import { membersV2API } from './api'
import {
  computeConflictingProjectIds,
  computeProjectGrantsDiff,
  hydrateProjectGrantsFromResponse,
  isProtectedSharedDefaultProject,
  normalizeProjectGrants,
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

export interface GrantMatrixMemberTarget {
  user_id: number | string
  role: 'admin' | 'member' | string
  role_source?: 'inherited' | 'delegated' | 'direct' | string | null
  username?: string
  email?: string
  grant_version?: string
}

interface MatrixProjectRow {
  id: string
  name: string
  isProtectedDefault: boolean
  isOffPage: boolean
}

const props = withDefaults(
  defineProps<{
    open: boolean
    workspaceId: string
    workspaceIsDefault?: boolean
    member: GrantMatrixMemberTarget | null
    projects: IdentityProject[]
    grantActionOptions: ReadonlyArray<{ action: string; label: string }>
  }>(),
  {
    workspaceIsDefault: false
  }
)

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'saved', payload: { memberId: string; version: string; projectGrants: ProjectGrantPair[] }): void
  (e: 'conflict', conflict: CasConflictState): void
}>()

const { t } = useI18n()

const grantVersion = ref('1')
const grantLoading = ref(false)
const grantLoadFailed = ref(false)
const grantBusy = ref(false)
const conflictState = ref<CasConflictState | null>(null)

const baselineGrants = ref<ProjectGrantPair[]>([])
const serverWinningGrants = ref<ProjectGrantPair[] | null>(null)
const draftByProject = ref<Record<string, string[]>>({})
const confirmClearAll = ref(false)

const grantableActionSet = computed(
  () => new Set(props.grantActionOptions.map((item) => item.action))
)

const memberRoleBadgeLabel = computed(() => {
  if (!props.member) return ''
  if (props.member.role === 'admin') {
    return props.member.role_source === 'inherited'
      ? t('membersAccess.matrix.roleSourceInherited')
      : t('membersAccess.matrix.roleSourceDelegated')
  }
  return t('membersAccess.matrix.roleSourceMember')
})

const allProjectRows = computed<MatrixProjectRow[]>(() => {
  const rows: MatrixProjectRow[] = []
  const seen = new Set<string>()

  for (const proj of props.projects) {
    if ((proj.state ?? 'active') === 'archived') continue
    seen.add(proj.id)
    rows.push({
      id: proj.id,
      name: proj.name,
      isProtectedDefault: isProtectedSharedDefaultProject(
        { is_default: props.workspaceIsDefault },
        proj
      ),
      isOffPage: false
    })
  }

  const referencedIds = new Set<string>([
    ...baselineGrants.value.map((g) => g.project_id),
    ...(serverWinningGrants.value ?? []).map((g) => g.project_id),
    ...Object.keys(draftByProject.value)
  ])

  for (const pid of Array.from(referencedIds).sort()) {
    if (!pid || seen.has(pid)) continue
    seen.add(pid)
    rows.push({
      id: pid,
      name: t('membersAccess.matrix.offPageProjectLabel', { id: pid }),
      isProtectedDefault: false,
      isOffPage: true
    })
  }

  return rows
})

const draftProjectGrants = computed<ProjectGrantPair[]>(() => {
  const pairs: ProjectGrantPair[] = []
  for (const row of allProjectRows.value) {
    if (row.isProtectedDefault) continue
    const actions = draftByProject.value[row.id] ?? []
    if (actions.length > 0) {
      pairs.push({
        project_id: row.id,
        actions: [...actions]
      })
    }
  }
  return normalizeProjectGrants(pairs)
})

const outOfScopeMemberActions = computed<string[]>(() => {
  const out = new Set<string>()
  for (const row of allProjectRows.value) {
    const actions = draftByProject.value[row.id] ?? []
    for (const act of actions) {
      if (!grantableActionSet.value.has(act)) {
        out.add(act)
      }
    }
  }
  return Array.from(out).sort()
})

const diffPreview = computed(() =>
  computeProjectGrantsDiff(baselineGrants.value, draftProjectGrants.value)
)

const conflictingProjectIds = computed<Set<string>>(() => {
  if (!conflictState.value || !serverWinningGrants.value) return new Set<string>()
  return computeConflictingProjectIds(
    baselineGrants.value,
    serverWinningGrants.value,
    draftProjectGrants.value
  )
})

const requiresEmptyClearConfirmation = computed<boolean>(
  () => draftProjectGrants.value.length === 0 && baselineGrants.value.length > 0
)

const canSubmitSave = computed<boolean>(() => {
  if (grantLoading.value || grantLoadFailed.value || grantBusy.value) return false
  if (requiresEmptyClearConfirmation.value && !confirmClearAll.value) return false
  return true
})

function resolveProjectName(projectId: string): string {
  const found = allProjectRows.value.find((r) => r.id === projectId)
  return found ? `${found.name} (#${found.id})` : `#${projectId}`
}

function serverActionsForProject(projectId: string): string[] {
  const source = serverWinningGrants.value ?? baselineGrants.value
  return source.find((g) => g.project_id === projectId)?.actions ?? []
}

function populateDraftMap(pairs: ReadonlyArray<ProjectGrantPair>): void {
  const next: Record<string, string[]> = {}
  for (const row of pairs) {
    const projObj = props.projects.find((p) => p.id === row.project_id)
    if (
      projObj &&
      isProtectedSharedDefaultProject({ is_default: props.workspaceIsDefault }, projObj)
    ) {
      continue
    }
    next[row.project_id] = [...row.actions].sort()
  }
  draftByProject.value = next
}

async function fetchMemberGrantsFromServer(
  workspaceId: string,
  memberId: string
): Promise<IdentityMemberGrants | null> {
  try {
    const res = await apiClient.get<IdentityMemberGrants>(
      `/workspaces/${encodeURIComponent(workspaceId)}/members/${encodeURIComponent(memberId)}/grants`,
      { baseURL: getControlAPIBaseURL() }
    )
    if (res?.data && (res.data.version || Array.isArray(res.data.project_grants) || Array.isArray(res.data.project_ids))) {
      return res.data
    }
  } catch (err) {
    if (typeof membersV2API.getMemberGrants !== 'function') {
      throw err
    }
  }
  if (typeof membersV2API.getMemberGrants === 'function') {
    return membersV2API.getMemberGrants(workspaceId, memberId)
  }
  return null
}

async function loadMemberGrants(preserveDraft = false): Promise<boolean> {
  if (!props.workspaceId || !props.member) return false
  const memberId = String(props.member.user_id)
  grantLoading.value = true
  grantLoadFailed.value = false
  try {
    const grants = await fetchMemberGrantsFromServer(props.workspaceId, memberId)
    if (grants) {
      grantVersion.value = String(grants.version || '1')
      const hydrated = hydrateProjectGrantsFromResponse(grants)
      if (preserveDraft) {
        serverWinningGrants.value = hydrated
      } else {
        baselineGrants.value = hydrated
        serverWinningGrants.value = null
        populateDraftMap(hydrated)
        conflictState.value = null
        confirmClearAll.value = false
      }
      return true
    }
  } catch (error) {
    grantLoadFailed.value = true
    const err = classifyAsyncError(error, t('membersAccess.errors.loadGrantsFailed'))
    pushUiToast({
      tone: 'warning',
      title: t('membersAccess.errors.loadGrantsTitle'),
      description: err.message
    })
  } finally {
    grantLoading.value = false
  }
  return false
}

function isActionChecked(projectId: string, action: string): boolean {
  return (draftByProject.value[projectId] ?? []).includes(action)
}

function toggleProjectAction(projectId: string, action: string, checked: boolean): void {
  const current = draftByProject.value[projectId] ?? []
  const next = checked
    ? Array.from(new Set([...current, action])).sort()
    : current.filter((a) => a !== action)
  draftByProject.value = {
    ...draftByProject.value,
    [projectId]: next
  }
}

function applyRowPreset(projectId: string, preset: GrantPresetId): void {
  const current = draftByProject.value[projectId] ?? []
  const outOfScope = current.filter((act) => !grantableActionSet.value.has(act))
  const presetActions = resolvePresetActions(preset, grantableActionSet.value)
  draftByProject.value = {
    ...draftByProject.value,
    [projectId]: Array.from(new Set([...outOfScope, ...presetActions])).sort()
  }
}

function clearProjectRow(projectId: string): void {
  const current = draftByProject.value[projectId] ?? []
  const outOfScope = current.filter((act) => !grantableActionSet.value.has(act))
  draftByProject.value = {
    ...draftByProject.value,
    [projectId]: outOfScope
  }
}

function clearAllProjectRows(): void {
  const next: Record<string, string[]> = {}
  for (const [pid, actions] of Object.entries(draftByProject.value)) {
    const outOfScope = actions.filter((act) => !grantableActionSet.value.has(act))
    if (outOfScope.length > 0) {
      next[pid] = outOfScope
    }
  }
  draftByProject.value = next
  confirmClearAll.value = true
}

function adoptServerRow(projectId: string): void {
  const serverActions = serverActionsForProject(projectId)
  draftByProject.value = {
    ...draftByProject.value,
    [projectId]: [...serverActions]
  }
}

function adoptAllServerGrants(): void {
  if (serverWinningGrants.value) {
    baselineGrants.value = serverWinningGrants.value
    populateDraftMap(serverWinningGrants.value)
    serverWinningGrants.value = null
    conflictState.value = null
  } else {
    void loadMemberGrants(false)
  }
}

async function handleSaveGrants(): Promise<void> {
  if (!props.member || !props.workspaceId || !canSubmitSave.value) return
  grantBusy.value = true
  const memberId = String(props.member.user_id)
  const payloadGrants = draftProjectGrants.value
  try {
    const res = await membersV2API.setMemberGrants(
      props.workspaceId,
      memberId,
      grantVersion.value,
      {
        project_grants: payloadGrants
      }
    )
    const newVersion = String(res?.version || grantVersion.value)
    grantVersion.value = newVersion
    baselineGrants.value = payloadGrants
    serverWinningGrants.value = null
    conflictState.value = null
    pushUiToast({
      tone: 'success',
      title: t('membersAccess.feedback.grantsUpdatedTitle'),
      description: t('membersAccess.feedback.grantRevisionDesc', { version: newVersion })
    })
    emit('saved', {
      memberId,
      version: newVersion,
      projectGrants: payloadGrants
    })
    emit('close')
  } catch (error) {
    const resolved = resolveCasMutationError(error, 'grants', memberId, grantVersion.value)
    if (resolved.kind === 'conflict') {
      conflictState.value = resolved.conflict
      if (resolved.conflict.serverVersion) {
        grantVersion.value = resolved.conflict.serverVersion
      }
      await loadMemberGrants(true)
      emit('conflict', resolved.conflict)
    } else {
      pushUiToast({
        tone: 'danger',
        title: t('membersAccess.errors.saveGrantsFailed'),
        description: resolved.message
      })
    }
  } finally {
    grantBusy.value = false
  }
}

async function handleReviewCompleteAndRetry(): Promise<void> {
  if (serverWinningGrants.value) {
    baselineGrants.value = serverWinningGrants.value
    serverWinningGrants.value = null
  }
  conflictState.value = null
  await handleSaveGrants()
}

watch(
  () => [props.open, props.member?.user_id, props.workspaceId] as const,
  ([isOpen, memberId, wsId]) => {
    if (isOpen && memberId !== undefined && wsId) {
      grantVersion.value = props.member?.grant_version || '1'
      baselineGrants.value = []
      serverWinningGrants.value = null
      draftByProject.value = {}
      conflictState.value = null
      confirmClearAll.value = false
      void loadMemberGrants(false)
    }
  },
  { immediate: true }
)
</script>

<template>
  <Modal
    :open="open && Boolean(member)"
    :title="
      member
        ? t('membersAccess.modals.editGrantsTitleWithId', { id: member.user_id })
        : t('membersAccess.modals.editGrantsTitle')
    "
    @close="emit('close')"
  >
    <form v-if="member" data-testid="member-grant-matrix-form" class="space-y-4" @submit.prevent="handleSaveGrants">
      <!-- Member Role Source & Revision Strip -->
      <div class="flex flex-wrap items-center justify-between gap-2 rounded-oh-md border border-oh-default bg-oh-subtle/40 px-3 py-2 text-xs">
        <div class="flex flex-wrap items-center gap-2">
          <span class="text-oh-secondary">
            {{ t('membersAccess.modals.currentGrantRevision') }}:
            <strong data-testid="grant-matrix-version" class="font-mono tabular-nums text-oh-primary">#{{ grantVersion }}</strong>
          </span>
          <span
            data-testid="grant-matrix-role-source-badge"
            class="rounded border border-oh-default bg-oh-surface px-2 py-0.5 font-mono text-[11px] font-medium text-oh-primary"
          >
            {{ memberRoleBadgeLabel }}
          </span>
        </div>
        <div class="flex items-center gap-1.5">
          <Button
            size="xs"
            variant="ghost"
            type="button"
            data-testid="clear-all-project-grants-btn"
            :disabled="grantLoading || grantBusy"
            @click="clearAllProjectRows"
          >
            {{ t('membersAccess.matrix.clearAllProjects') }}
          </Button>
          <Button
            size="xs"
            variant="ghost"
            type="button"
            :loading="grantLoading"
            @click="loadMemberGrants(false)"
          >
            {{ t('membersAccess.modals.reloadCurrentGrants') }}
          </Button>
        </div>
      </div>

      <p class="text-[11px] text-oh-muted">
        {{ t('membersAccess.matrix.baselineNote') }}
      </p>

      <!-- 412 CAS Conflict Banner (Preserves draft & highlights conflicting rows) -->
      <div
        v-if="conflictState"
        data-testid="grant-conflict-412-banner"
        class="rounded-oh-md border border-oh-status-warn-border bg-oh-status-warn-bg/60 p-3 text-xs space-y-2"
        role="alert"
      >
        <div class="font-semibold text-oh-primary">
          {{ t('rbac.cas.conflict412') }}
        </div>
        <p class="text-oh-secondary">
          {{
            conflictState.serverVersion
              ? t('membersAccess.modals.grantConflictMessageWithServer', {
                  version: conflictState.serverVersion
                })
              : t('membersAccess.modals.grantConflictMessage')
          }}
        </p>
        <div class="flex flex-wrap items-center gap-2">
          <Button
            size="xs"
            variant="secondary"
            type="button"
            data-testid="adopt-server-grants-btn"
            :loading="grantLoading"
            @click="adoptAllServerGrants"
          >
            {{ t('membersAccess.modals.loadServerGrants') }}
          </Button>
          <Button
            size="xs"
            variant="primary"
            type="button"
            data-testid="retry-draft-grants-btn"
            :loading="grantBusy || grantLoading"
            @click="handleReviewCompleteAndRetry"
          >
            {{ t('membersAccess.matrix.retryWithDraft', { version: grantVersion }) }}
          </Button>
        </div>
      </div>

      <div v-if="grantLoading" class="space-y-2">
        <Skeleton height="2.5rem" />
        <Skeleton height="5rem" />
      </div>

      <p
        v-if="grantActionOptions.length === 0"
        data-testid="grant-actions-unavailable"
        class="rounded-oh-md border border-oh-subtle bg-oh-subtle/50 px-3 py-2 text-xs text-oh-muted"
      >
        {{ t('membersAccess.modals.noGrantableActions') }}
      </p>

      <!-- Per-Project Grant Matrix (1 row per project) -->
      <div
        v-else
        data-testid="grant-actions-grid"
        class="max-h-80 space-y-3 overflow-y-auto pr-1"
      >
        <div
          v-for="row in allProjectRows"
          :key="row.id"
          :data-testid="`grant-matrix-row-${row.id}`"
          class="rounded-oh-md border p-3 space-y-2.5"
          :class="
            conflictingProjectIds.has(row.id)
              ? 'border-amber-500 bg-amber-500/5'
              : row.isProtectedDefault
                ? 'border-oh-subtle bg-oh-subtle/30 opacity-80'
                : 'border-oh-default bg-oh-surface'
          "
        >
          <div class="flex flex-wrap items-center justify-between gap-2">
            <div class="flex flex-wrap items-center gap-2">
              <span class="font-semibold text-xs text-oh-primary">
                {{ row.name }}
              </span>
              <span class="font-mono text-[11px] text-oh-muted">#{{ row.id }}</span>
              <span
                v-if="row.isProtectedDefault"
                data-testid="grant-row-locked-default"
                class="rounded border border-oh-default bg-oh-subtle px-2 py-0.5 font-mono text-[10px] text-oh-secondary"
              >
                {{ t('membersAccess.matrix.sharedDefaultLocked') }}
              </span>
              <span
                v-if="conflictingProjectIds.has(row.id)"
                :data-testid="`grant-row-conflict-${row.id}`"
                class="rounded bg-amber-500/20 px-2 py-0.5 font-mono text-[10px] font-semibold text-amber-700 dark:text-amber-300"
              >
                {{ t('membersAccess.matrix.conflictRowBadge') }}
              </span>
            </div>

            <!-- Per-Row Least-Privilege Presets -->
            <div v-if="!row.isProtectedDefault" class="flex flex-wrap items-center gap-1">
              <button
                type="button"
                :data-testid="`preset-observer-${row.id}`"
                class="rounded border border-oh-default bg-oh-subtle/50 px-2 py-0.5 text-[11px] text-oh-secondary hover:text-oh-primary"
                @click="applyRowPreset(row.id, 'observer')"
              >
                {{ t('membersAccess.matrix.presetObserver') }}
              </button>
              <button
                type="button"
                :data-testid="`preset-caller-${row.id}`"
                class="rounded border border-oh-default bg-oh-subtle/50 px-2 py-0.5 text-[11px] text-oh-secondary hover:text-oh-primary"
                @click="applyRowPreset(row.id, 'caller')"
              >
                {{ t('membersAccess.matrix.presetCaller') }}
              </button>
              <button
                type="button"
                :data-testid="`preset-issuer-${row.id}`"
                class="rounded border border-oh-default bg-oh-subtle/50 px-2 py-0.5 text-[11px] text-oh-secondary hover:text-oh-primary"
                @click="applyRowPreset(row.id, 'issuer')"
              >
                {{ t('membersAccess.matrix.presetIssuer') }}
              </button>
              <button
                type="button"
                :data-testid="`preset-maintainer-${row.id}`"
                class="rounded border border-oh-default bg-oh-subtle/50 px-2 py-0.5 text-[11px] text-oh-secondary hover:text-oh-primary"
                @click="applyRowPreset(row.id, 'maintainer')"
              >
                {{ t('membersAccess.matrix.presetMaintainer') }}
              </button>
              <button
                type="button"
                :data-testid="`clear-project-row-${row.id}`"
                class="rounded border border-oh-default bg-oh-subtle/50 px-2 py-0.5 text-[11px] text-oh-muted hover:text-oh-primary"
                @click="clearProjectRow(row.id)"
              >
                {{ t('membersAccess.matrix.clearProjectRow') }}
              </button>
            </div>
          </div>

          <!-- Concurrent conflict comparison for this row -->
          <div
            v-if="conflictingProjectIds.has(row.id)"
            class="rounded border border-amber-500/40 bg-amber-500/10 p-2 text-[11px] space-y-1"
          >
            <div class="flex flex-wrap items-center justify-between gap-2">
              <span>
                <strong>{{ t('membersAccess.matrix.serverCurrentLabel') }}</strong>
                <span class="font-mono">
                  {{ serverActionsForProject(row.id).join(', ') || '∅' }}
                </span>
              </span>
              <button
                type="button"
                :data-testid="`adopt-server-row-${row.id}`"
                class="font-semibold text-amber-700 underline dark:text-amber-300"
                @click="adoptServerRow(row.id)"
              >
                {{ t('membersAccess.matrix.adoptServerRow') }}
              </button>
            </div>
            <div>
              <strong>{{ t('membersAccess.matrix.yourDraftLabel') }}</strong>
              <span class="font-mono">
                {{ (draftByProject[row.id] ?? []).join(', ') || '∅' }}
              </span>
            </div>
          </div>

          <div
            v-if="!row.isProtectedDefault"
            class="grid grid-cols-1 gap-1.5 sm:grid-cols-2"
          >
            <Checkbox
              v-for="item in grantActionOptions"
              :key="`${row.id}-${item.action}`"
              :model-value="isActionChecked(row.id, item.action)"
              :label="`${item.label} (${item.action})`"
              @update:model-value="(v) => toggleProjectAction(row.id, item.action, Boolean(v))"
            />
          </div>
        </div>
      </div>

      <div
        v-if="outOfScopeMemberActions.length > 0"
        data-testid="grant-actions-out-of-scope"
        class="rounded-oh-md border border-oh-subtle bg-oh-subtle/40 px-3 py-2 text-[11px] text-oh-muted space-y-1"
      >
        <p>{{ t('membersAccess.modals.outOfScopeActionsHint') }}</p>
        <div class="flex flex-wrap gap-1.5">
          <Badge
            v-for="act in outOfScopeMemberActions"
            :key="act"
            tone="neutral"
            variant="outline"
            mono
          >
            {{ act }}
          </Badge>
        </div>
      </div>

      <!-- Explicit confirmation when clearing all active grants -->
      <div
        v-if="requiresEmptyClearConfirmation"
        data-testid="confirm-clear-all-grants-box"
        class="rounded-oh-md border border-amber-500/40 bg-amber-500/10 p-3 text-xs"
      >
        <label class="flex items-center gap-2 font-medium text-oh-primary">
          <input
            v-model="confirmClearAll"
            type="checkbox"
            data-testid="confirm-clear-all-grants-checkbox"
          />
          <span>{{ t('membersAccess.matrix.confirmClearAll') }}</span>
        </label>
      </div>

      <!-- Exact Diff Preview Before Save -->
      <div
        data-testid="grant-matrix-diff-preview"
        class="rounded-oh-md border border-oh-default bg-oh-subtle/30 p-3 text-xs space-y-1.5"
      >
        <div class="font-semibold text-oh-primary">
          {{ t('membersAccess.matrix.diffPreviewTitle') }}
        </div>
        <p v-if="diffPreview.changedCount === 0" class="text-oh-muted">
          {{ t('membersAccess.matrix.diffNoChanges') }}
        </p>
        <div v-else class="space-y-1">
          <div
            v-for="rowDiff in diffPreview.rows.filter((r) => !r.unchanged)"
            :key="`diff-${rowDiff.projectId}`"
            :data-testid="`grant-diff-row-${rowDiff.projectId}`"
            class="flex flex-wrap items-center gap-1.5 font-mono text-[11px]"
          >
            <span class="font-semibold text-oh-primary">
              {{ resolveProjectName(rowDiff.projectId) }}:
            </span>
            <span
              v-for="added in rowDiff.added"
              :key="`add-${rowDiff.projectId}-${added}`"
              class="rounded bg-emerald-500/15 px-1.5 py-0.5 text-emerald-700 dark:text-emerald-300"
            >
              + {{ added }}
            </span>
            <span
              v-for="removed in rowDiff.removed"
              :key="`rem-${rowDiff.projectId}-${removed}`"
              class="rounded bg-red-500/15 px-1.5 py-0.5 text-red-700 dark:text-red-300"
            >
              - {{ removed }}
            </span>
          </div>
          <p v-if="diffPreview.unchangedCount > 0" class="text-[11px] text-oh-muted">
            {{ t('membersAccess.matrix.diffUnchangedCount', { count: diffPreview.unchangedCount }) }}
          </p>
        </div>
      </div>

      <div class="flex justify-end gap-2 pt-2">
        <Button
          variant="ghost"
          type="button"
          @click="emit('close')"
        >
          {{ t('membersAccess.modals.cancel') }}
        </Button>
        <Button
          variant="primary"
          type="submit"
          data-testid="save-member-grants-btn"
          :loading="grantBusy"
          :disabled="!canSubmitSave"
        >
          {{ t('membersAccess.modals.saveGrants') }}
        </Button>
      </div>
    </form>
  </Modal>
</template>
