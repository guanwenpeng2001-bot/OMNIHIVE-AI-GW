import { computed, onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import { isSilentRequestError } from '@/api/client'
import { normalizeApiError, type NormalizedApiError } from '@/utils/apiError'
import { routingAPI, type PolicyHead, type PolicyPublication, type PolicyScope, type PolicySimulation, type PolicyVersion, type SimulationRequest } from './api'
import { canPublish, mergePolicyForm, policyForm, serializePolicy, validatePolicy } from './policy'

type Mutation = 'save' | 'publish' | 'rollback'
type Conflict = { status: number; current: PolicyHead | null; loaded: boolean }
type Confirmation = { version: string; publication?: PolicyPublication }

export function useRoutingPolicy(workspace: Ref<string>, scope: Ref<PolicyScope | null>) {
  const head = shallowRef<PolicyHead | null>(null)
  const form = ref(policyForm())
  const baseline = computed(() => policyForm(head.value?.definition))
  const dirty = computed(() => JSON.stringify(form.value) !== JSON.stringify(baseline.value))
  const opening = ref(false)
  const loading = ref(false)
  const synced = ref('')
  const issue = shallowRef<NormalizedApiError | null>(null)
  const conflict = shallowRef<Conflict | null>(null)
  const confirmation = shallowRef<Confirmation | null>(null)
  const unconfirmed = ref<Mutation | null>(null)
  const mutation = ref<Mutation | null>(null)
  const mutationIssue = shallowRef<NormalizedApiError | null>(null)
  const validation = ref<string | null>(null)
  const versions = shallowRef<PolicyVersion[]>([])
  const versionsCursor = ref<string | null>(null)
  const versionsLoading = ref(false)
  const versionsIssue = shallowRef<NormalizedApiError | null>(null)
  const simulation = shallowRef<PolicySimulation | null>(null)
  const simulationLoading = ref(false)
  const simulationIssue = shallowRef<NormalizedApiError | null>(null)
  let session = new AbortController()
  let simulationRequest: AbortController | null = null
  let simulationGeneration = 0

  const blocked = computed(() =>
    loading.value || Boolean(issue.value || conflict.value || unconfirmed.value || mutation.value),
  )

  function clearSimulation() {
    simulationGeneration++
    simulationRequest?.abort()
    simulationLoading.value = false
    simulation.value = null
    simulationIssue.value = null
  }

  function setHead(next: PolicyHead | null) {
    if (head.value?.etag !== next?.etag || head.value?.id !== next?.id) clearSimulation()
    head.value = next
    form.value = policyForm(next?.definition)
    opening.value = Boolean(next)
  }

  function loseAccess() {
    head.value = null
    form.value = policyForm()
    opening.value = false
    confirmation.value = null
    conflict.value = null
    unconfirmed.value = null
    versions.value = []
    versionsCursor.value = null
    synced.value = ''
    clearSimulation()
  }

  async function refresh(adopt = false) {
    const target = scope.value
    if (!target || loading.value) return
    const signal = session.signal
    loading.value = true
    issue.value = null
    try {
      const page = await routingAPI.list(workspace.value, target, signal)
      if (signal.aborted) return
      const latest = page.items[0] ?? null
      synced.value = page.as_of
      if (conflict.value && !adopt) {
        conflict.value = { ...conflict.value, current: latest, loaded: true }
      } else if (!adopt && dirty.value && (latest?.etag !== head.value?.etag || latest?.id !== head.value?.id)) {
        conflict.value = { status: 412, current: latest, loaded: true }
      } else if (adopt || !dirty.value) {
        setHead(latest)
        conflict.value = null
      }
    } catch (error) {
      if (signal.aborted || isSilentRequestError(error)) return
      issue.value = normalizeApiError(error)
      if (issue.value.status === 403) loseAccess()
    } finally {
      if (!signal.aborted) loading.value = false
    }
  }

  async function loadVersions(append = false) {
    const selected = head.value
    if (!selected || versionsLoading.value || (append && !versionsCursor.value)) return
    const signal = session.signal
    versionsLoading.value = true
    versionsIssue.value = null
    try {
      const page = await routingAPI.versions(workspace.value, selected.id, signal, append ? versionsCursor.value ?? undefined : undefined)
      if (signal.aborted || selected.id !== head.value?.id) return
      versions.value = append ? [...versions.value, ...page.items] : page.items
      versionsCursor.value = page.next_cursor
    } catch (error) {
      if (signal.aborted || isSilentRequestError(error)) return
      versionsIssue.value = normalizeApiError(error)
      if (versionsIssue.value.status === 403) {
        versions.value = []
        versionsCursor.value = null
      }
    } finally {
      if (!signal.aborted) versionsLoading.value = false
    }
  }

  function resolveConflict(reapply: boolean) {
    if (!conflict.value?.loaded) return
    const next = conflict.value.current
    const merged = reapply ? mergePolicyForm(baseline.value, form.value, policyForm(next?.definition)) : null
    setHead(next)
    if (merged) {
      form.value = merged
      opening.value = true
    }
    conflict.value = null
    unconfirmed.value = null
    validation.value = null
    void loadVersions()
  }

  async function runMutation(
    kind: Mutation,
    perform: (signal: AbortSignal) => Promise<Confirmation>,
  ) {
    if (!scope.value || blocked.value) return
    const signal = session.signal
    mutation.value = kind
    mutationIssue.value = null
    confirmation.value = null
    try {
      const result = await perform(signal)
      if (signal.aborted) return
      confirmation.value = result
    } catch (error) {
      if (signal.aborted || isSilentRequestError(error)) return
      const failure = normalizeApiError(error)
      if (failure.reason === 'SIMULATION_STALE') {
        clearSimulation()
        simulationIssue.value = failure
      } else if (failure.status === 409 || failure.status === 412) {
        conflict.value = { status: failure.status, current: null, loaded: false }
        clearSimulation()
        await refresh()
      } else if (!failure.status || failure.status >= 500) {
        unconfirmed.value = kind
      } else {
        mutationIssue.value = failure
        if (failure.status === 403) clearSimulation()
      }
    } finally {
      if (!signal.aborted) mutation.value = null
    }
  }

  async function save() {
    if (!scope.value) return
    validation.value = validatePolicy(form.value)
    if (validation.value) return
    const payload = serializePolicy(form.value, scope.value)
    await runMutation('save', async signal => {
      const next = await routingAPI.save(workspace.value, payload, head.value, signal)
      if (!signal.aborted) {
        setHead(next)
        void loadVersions()
      }
      return { version: next.draft_version }
    })
  }

  async function simulate(request: SimulationRequest) {
    if (!head.value || blocked.value || dirty.value || simulationLoading.value) return
    const current = head.value
    clearSimulation()
    const controller = new AbortController()
    simulationRequest = controller
    const generation = simulationGeneration
    simulationLoading.value = true
    try {
      const result = await routingAPI.simulate(workspace.value, current.id, request, controller.signal)
      if (generation === simulationGeneration) simulation.value = result
    } catch (error) {
      if (generation !== simulationGeneration || isSilentRequestError(error)) return
      simulationIssue.value = normalizeApiError(error)
    } finally {
      if (generation === simulationGeneration) simulationLoading.value = false
    }
  }

  async function publish(note: string) {
    const current = head.value
    const proof = simulation.value
    if (!current || !proof || !canPublish(current, proof, dirty.value, Date.now())) return
    await runPublication('publish', signal => routingAPI.publish(workspace.value, current, proof, note, signal))
  }

  async function rollback(version: string, note: string) {
    const current = head.value
    if (!current?.active_version || current.active_version === version || dirty.value) return
    await runPublication('rollback', signal => routingAPI.rollback(workspace.value, current, version, note, signal))
  }

  async function runPublication(kind: 'publish' | 'rollback', perform: (signal: AbortSignal) => Promise<PolicyPublication>) {
    await runMutation(kind, async signal => {
      const publication = await perform(signal)
      if (!signal.aborted) {
        clearSimulation()
        await refresh(true)
        void loadVersions()
      }
      return { version: publication.active_version, publication }
    })
  }

  async function verifyOutcome() {
    if (!unconfirmed.value) return
    conflict.value = { status: 0, current: null, loaded: false }
    await refresh()
    await loadVersions()
  }

  watch(() => [workspace.value, scope.value?.type, scope.value?.id, scope.value?.project_id].join('/'), () => {
    session.abort()
    session = new AbortController()
    loseAccess()
    issue.value = null
    mutationIssue.value = null
    versionsIssue.value = null
    validation.value = null
    mutation.value = null
    loading.value = false
    versionsLoading.value = false
    void refresh()
  }, { immediate: true, flush: 'sync' })

  watch(() => head.value?.id, () => {
    if (head.value) void loadVersions()
  })

  onScopeDispose(() => {
    session.abort()
    simulationRequest?.abort()
  })

  return {
    head, form, baseline, dirty, loading, opening, synced, issue, blocked, refresh,
    mutation, mutationIssue, unconfirmed, verifyOutcome, conflict, resolveConflict, confirmation, validation, save,
    versions, versionsCursor, versionsLoading, versionsIssue, loadVersions,
    simulation, simulationLoading, simulationIssue, clearSimulation, simulate, publish, rollback,
  }
}
