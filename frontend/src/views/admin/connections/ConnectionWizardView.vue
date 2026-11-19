<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import {
  Badge,
  Button,
  Card,
  Checkbox,
  Input,
  PageHeader,
  SegmentedControl,
  Select,
  Stepper
} from '@/components/ui'
import { connectionsV2Api, pollConnectionOperation } from '@/features/connections/api'
import AsyncStateBanner from '@/features/connections/components/AsyncStateBanner.vue'
import {
  getAuthModeLabel,
  getProviderDisplayMeta,
  isConnectionResource,
  isOAuthWorkflowSupported,
  isVerificationFresh,
  mapApiErrorToAsyncContext
} from '@/features/connections/rules'
import type {
  AsyncPageState,
  ConnectionOperation,
  ConnectionPool,
  ConnectionSchema,
  CredentialOperationMode
} from '@/features/connections/types'
import {
  buildCredentialInputPayload,
  canProceedFromStep,
  createInitialWizardState,
  getWizardStepStatuses,
  hydrateWizardFromResource,
  invalidatePreview,
  invalidateVerification,
  WIZARD_STEP_ORDER,
  type WizardStepId
} from '@/features/connections/wizardState'

const route = useRoute()
const router = useRouter()

const connectionIdParam = computed(() => {
  const raw = route.params.id
  return typeof raw === 'string' && raw.length > 0 ? raw : null
})

const state = reactive(createInitialWizardState(connectionIdParam.value ? 'edit' : 'create'))
const schemas = ref<ConnectionSchema[]>([])
const pools = ref<ConnectionPool[]>([])
const schemaSearch = ref('')
const modelIdsText = ref('')
const oauthCode = ref('')
const newPoolName = ref('')
const creatingPool = ref(false)

const pageAsyncState = ref<AsyncPageState | null>('initial_load')
const busy = ref(false)
const verifyOperation = ref<ConnectionOperation | null>(null)

const stepLabels: Record<WizardStepId, { title: string; description: string }> = {
  provider: { title: 'Provider & Schema', description: 'Select upstream protocol & endpoint' },
  credentials: { title: 'Credentials', description: 'Keep, set, clear, or enroll OAuth' },
  verify: { title: 'Verify', description: 'Run live probe & check discovery' },
  preview: { title: 'Preview', description: 'Assign pools, models & caps' },
  activate: { title: 'Activate', description: 'Confirm version & enable' }
}

const stepperItems = computed(() => getWizardStepStatuses(state, stepLabels))

const filteredSchemas = computed(() => {
  const query = schemaSearch.value.trim().toLowerCase()
  if (!query) return schemas.value
  return schemas.value.filter((schema) => {
    const meta = getProviderDisplayMeta(schema.provider)
    return (
      schema.provider.toLowerCase().includes(query) ||
      meta.label.toLowerCase().includes(query) ||
      schema.auth_types.some((t) => t.toLowerCase().includes(query))
    )
  })
})

const selectedSchema = computed(() =>
  schemas.value.find(
    (s) => s.provider === state.provider && s.auth_types.includes(state.authType)
  ) ?? schemas.value.find((s) => s.provider === state.provider) ?? null
)

const authModeOptions = computed(() => {
  const schema = schemas.value.find((s) => s.provider === state.provider)
  if (!schema || schema.auth_types.length === 0) {
    return [{ value: 'api_key', label: 'API Key' }]
  }
  return schema.auth_types.map((authType) => ({
    value: authType,
    label: getAuthModeLabel(authType)
  }))
})

const credentialModeOptions = computed(() => {
  if (state.mode === 'edit') {
    return [
      { value: 'keep', label: 'Keep Existing' },
      { value: 'set', label: 'Replace Secret' },
      { value: 'clear', label: 'Clear Secret' }
    ]
  }
  return [{ value: 'set', label: 'Set Secret' }]
})

const supportsOAuth = computed(() => isOAuthWorkflowSupported(selectedSchema.value, state.authType))
const stepValidation = computed(() => canProceedFromStep(state, state.currentStep, selectedSchema.value))

async function loadInitialData() {
  pageAsyncState.value = 'initial_load'
  state.asyncError = null
  try {
    const [schemaPage, poolPage] = await Promise.all([
      connectionsV2Api.listProviderSchemas({ limit: 100 }),
      connectionsV2Api.listPools({ limit: 100 }).catch(() => ({ items: [] }))
    ])
    schemas.value = schemaPage.items
    pools.value = poolPage.items

    if (connectionIdParam.value) {
      const resource = await connectionsV2Api.getConnection(connectionIdParam.value)
      Object.assign(state, hydrateWizardFromResource(state, resource))
      modelIdsText.value = resource.model_ids.join(', ')
    } else if (schemas.value.length > 0 && !state.provider) {
      selectProviderSchema(schemas.value[0])
    }
    pageAsyncState.value = schemas.value.length === 0 && !connectionIdParam.value ? 'empty' : null
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(err, 'Failed to load connection wizard data.')
    state.asyncError = mapped
    pageAsyncState.value = mapped.state
  }
}

onMounted(() => {
  void loadInitialData()
})

watch(connectionIdParam, () => {
  void loadInitialData()
})

function selectProviderSchema(schema: ConnectionSchema) {
  state.provider = schema.provider
  state.authType = schema.auth_types[0] ?? 'api_key'
  state.schemaVersion = schema.schema_version
  const meta = getProviderDisplayMeta(schema.provider)
  if (!state.endpoint && meta.defaultEndpoint) {
    state.endpoint = meta.defaultEndpoint
  }
  if (!state.name) {
    state.name = `${meta.label} (${getAuthModeLabel(state.authType)})`
  }
  Object.assign(state, invalidateVerification(state))
}

function handleEndpointChange(value: string | number) {
  state.endpoint = String(value)
  Object.assign(state, invalidateVerification(state))
}

function handleCredentialModeChange(mode: string | number) {
  state.credentialMode = mode as CredentialOperationMode
  Object.assign(state, invalidateVerification(state))
}

async function ensureConnectionPersisted(): Promise<boolean> {
  state.asyncError = null
  if (!state.connectionId) {
    const created = await connectionsV2Api.createConnection({
      provider: state.provider,
      schema_version: '1',
      auth_type: state.authType,
      name: state.name.trim(),
      endpoint: state.endpoint.trim(),
      network_policy_id: state.networkPolicyId.trim() || 'default'
    })
    if (isConnectionResource(created)) {
      Object.assign(state, hydrateWizardFromResource(state, created))
    } else {
      state.version = created.version
    }
    if (state.connectionId && state.credentialMode !== 'keep') {
      const credResult = await connectionsV2Api.changeCredential(
        state.connectionId,
        state.version,
        buildCredentialInputPayload(state)
      )
      state.version = credResult.version
      state.credentialVersion = credResult.credential?.version ?? state.credentialVersion
      state.credentialState = credResult.credential?.state ?? state.credentialState
      state.credentialLastFour = credResult.credential?.last_four ?? null
      state.credentialSecret = ''
      state.credentialRefreshToken = ''
      state.credentialMode = 'keep'
    }
    return true
  }

  const patched = await connectionsV2Api.patchConnection(state.connectionId, state.version, {
    name: state.name.trim(),
    endpoint: state.endpoint.trim(),
    network_policy_id: state.networkPolicyId.trim() || 'default',
    model_ids: state.modelIds,
    pool_ids: state.poolIds,
    limits: { concurrency: state.concurrency }
  })
  state.version = patched.version

  if (state.credentialMode !== 'keep') {
    const credResult = await connectionsV2Api.changeCredential(
      state.connectionId,
      state.version,
      buildCredentialInputPayload(state)
    )
    state.version = credResult.version
    state.credentialVersion = credResult.credential?.version ?? state.credentialVersion
    state.credentialState = credResult.credential?.state ?? state.credentialState
    state.credentialLastFour = credResult.credential?.last_four ?? null
    state.credentialSecret = ''
    state.credentialRefreshToken = ''
    state.credentialMode = 'keep'
  }
  return true
}

async function handleSaveDraft() {
  if (busy.value) return
  busy.value = true
  pageAsyncState.value = 'background_refresh'
  try {
    await ensureConnectionPersisted()
    pageAsyncState.value = 'confirmed'
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(err, 'Failed to save draft.')
    state.asyncError = mapped
    pageAsyncState.value = mapped.state
  } finally {
    busy.value = false
  }
}

async function handleEnrollOAuth() {
  if (busy.value || !oauthCode.value.trim()) return
  busy.value = true
  pageAsyncState.value = 'background_refresh'
  try {
    await ensureConnectionPersisted()
    if (!state.connectionId) return
    const result = await connectionsV2Api.attachAuthorization(state.connectionId, state.version, {
      credential_draft: oauthCode.value.trim(),
      revision: state.version
    })
    state.version = result.version
    state.credentialVersion = result.credential?.version ?? state.credentialVersion
    state.credentialState = result.credential?.state ?? state.credentialState
    state.credentialLastFour = result.credential?.last_four ?? null
    oauthCode.value = ''
    pageAsyncState.value = 'confirmed'
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(err, 'OAuth enrollment failed.')
    state.asyncError = mapped
    pageAsyncState.value = mapped.state
  } finally {
    busy.value = false
  }
}

async function handleRunVerification() {
  if (busy.value) return
  busy.value = true
  pageAsyncState.value = 'background_refresh'
  state.asyncError = null
  try {
    await ensureConnectionPersisted()
    if (!state.connectionId) return
    const initialOp = await connectionsV2Api.verifyConnection(state.connectionId, state.version, {
      checks: ['network', 'authorization', 'discover_models'],
      credential_version: state.credentialVersion
    })
    verifyOperation.value = initialOp
    const finalOp = await pollConnectionOperation(initialOp, {
      onUpdate: (op) => {
        verifyOperation.value = op
      }
    })
    const refreshed = await connectionsV2Api.getConnection(state.connectionId)
    Object.assign(state, hydrateWizardFromResource(state, refreshed))
    state.verificationStale = !isVerificationFresh(state.verification)

    if (finalOp.state === 'failed') {
      state.asyncError = {
        state: 'partial_failure',
        status: 422,
        message: typeof finalOp.error?.message === 'string' ? finalOp.error.message : 'Verification probe failed.',
        requestId: finalOp.operation_id
      }
      pageAsyncState.value = 'partial_failure'
    } else {
      pageAsyncState.value = 'confirmed'
    }
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(err, 'Verification request failed.')
    state.asyncError = mapped
    pageAsyncState.value = mapped.state
  } finally {
    busy.value = false
  }
}

async function handleRunPreview() {
  if (busy.value) return
  busy.value = true
  pageAsyncState.value = 'background_refresh'
  state.asyncError = null
  try {
    state.modelIds = modelIdsText.value
      .split(/[,\n]/)
      .map((item) => item.trim())
      .filter((item) => item.length > 0)
    await ensureConnectionPersisted()
    if (!state.connectionId) return
    const preview = await connectionsV2Api.previewConnection(state.connectionId, state.version, {
      model_ids: state.modelIds,
      pool_ids: state.poolIds,
      limits: { concurrency: state.concurrency }
    })
    state.preview = preview
    state.previewStale = false
    pageAsyncState.value = 'confirmed'
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(err, 'Preview generation failed.')
    state.asyncError = mapped
    pageAsyncState.value = mapped.state
  } finally {
    busy.value = false
  }
}

async function handleCreateInlinePool() {
  if (!newPoolName.value.trim() || creatingPool.value) return
  creatingPool.value = true
  try {
    const pool = await connectionsV2Api.createPool({
      name: newPoolName.value.trim(),
      max_concurrency: Math.max(1, state.concurrency)
    })
    pools.value = [...pools.value, pool]
    if (!state.poolIds.includes(pool.id)) {
      state.poolIds = [...state.poolIds, pool.id]
      Object.assign(state, invalidatePreview(state))
    }
    newPoolName.value = ''
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(err, 'Failed to create pool.')
    state.asyncError = mapped
    pageAsyncState.value = mapped.state
  } finally {
    creatingPool.value = false
  }
}

async function handleActivate() {
  if (busy.value || !state.connectionId) return
  busy.value = true
  pageAsyncState.value = 'background_refresh'
  state.asyncError = null
  try {
    const result = await connectionsV2Api.activateConnection(state.connectionId, state.version, {
      verification_id: state.verification?.verification_id ?? '',
      preview_id: state.preview?.preview_id ?? '',
      expected_credential_version: state.credentialVersion
    })
    state.version = result.version
    state.confirmedVersion = result.version
    state.connectionState = 'active'
    pageAsyncState.value = 'confirmed'
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(err, 'Activation failed.')
    state.asyncError = mapped
    pageAsyncState.value = mapped.state
  } finally {
    busy.value = false
  }
}

function goNext() {
  if (!stepValidation.value.valid) return
  const idx = WIZARD_STEP_ORDER.indexOf(state.currentStep)
  if (idx < WIZARD_STEP_ORDER.length - 1) {
    state.currentStep = WIZARD_STEP_ORDER[idx + 1]
    pageAsyncState.value = null
    state.asyncError = null
  }
}

function goBack() {
  const idx = WIZARD_STEP_ORDER.indexOf(state.currentStep)
  if (idx > 0) {
    state.currentStep = WIZARD_STEP_ORDER[idx - 1]
    pageAsyncState.value = null
    state.asyncError = null
  }
}

function togglePool(poolId: string) {
  if (state.poolIds.includes(poolId)) {
    state.poolIds = state.poolIds.filter((id) => id !== poolId)
  } else {
    state.poolIds = [...state.poolIds, poolId]
  }
  Object.assign(state, invalidatePreview(state))
}
</script>

<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-6 px-4 py-6 sm:px-6">
      <PageHeader
        :title="state.mode === 'edit' ? 'Edit Upstream Connection' : 'New Upstream Connection'"
        subtitle="5-step schema-driven provisioning, verification, and fenced activation"
        :breadcrumbs="[
          { label: 'Admin', to: '/admin/accounts' },
          { label: 'Upstream Connections', to: '/admin/accounts' },
          { label: state.mode === 'edit' ? `Edit ${state.connectionId}` : 'New Connection' }
        ]"
      >
        <template #actions>
          <Button variant="ghost" size="sm" @click="router.push('/admin/accounts')">
            Back to Connections
          </Button>
          <Button variant="secondary" size="sm" :loading="busy" @click="handleSaveDraft">
            Save Draft
          </Button>
        </template>
      </PageHeader>

      <AsyncStateBanner
        v-if="pageAsyncState"
        :state="pageAsyncState"
        :error="state.asyncError"
        :confirmed-version="state.confirmedVersion ?? state.version"
        confirmed-label="Connection state synchronized"
        empty-title="No provider schemas returned"
        empty-description="Verify BE-04 provider-schemas endpoint availability."
        @retry="loadInitialData"
        @reload-latest="loadInitialData"
      />

      <div class="lg:hidden">
        <Stepper
          :steps="stepperItems"
          :current-step-id="state.currentStep"
          orientation="horizontal"
          @update:current-step-id="state.currentStep = $event as WizardStepId"
        />
      </div>

      <div class="grid grid-cols-1 gap-6 lg:grid-cols-[240px_1fr]">
        <aside class="hidden lg:block">
          <Card padding="md">
            <Stepper
              :steps="stepperItems"
              :current-step-id="state.currentStep"
              orientation="vertical"
              @update:current-step-id="state.currentStep = $event as WizardStepId"
            />
          </Card>
        </aside>

        <main class="space-y-4">
          <Card v-if="state.currentStep === 'provider'" padding="lg">
            <div class="space-y-5">
              <div class="flex flex-wrap items-center justify-between gap-3">
                <div>
                  <h2 class="text-base font-semibold text-oh-fg">1. Provider & Schema</h2>
                  <p class="text-xs text-oh-muted">Choose an upstream provider schema and base endpoint.</p>
                </div>
                <div class="w-full sm:w-64">
                  <Input
                    v-model="schemaSearch"
                    placeholder="Search provider or auth…"
                    size="sm"
                  />
                </div>
              </div>

              <div class="grid grid-cols-1 gap-2.5 sm:grid-cols-2 lg:grid-cols-3">
                <button
                  v-for="schema in filteredSchemas"
                  :key="schema.provider"
                  type="button"
                  class="rounded-oh-md border p-3 text-left transition-colors"
                  :class="
                    state.provider === schema.provider
                      ? 'border-oh-accent bg-oh-accent/10'
                      : 'border-oh-default bg-oh-surface hover:border-oh-strong'
                  "
                  @click="selectProviderSchema(schema)"
                >
                  <div class="flex items-center justify-between gap-2">
                    <span class="text-sm font-semibold text-oh-fg">
                      {{ getProviderDisplayMeta(schema.provider).label }}
                    </span>
                    <Badge tone="neutral" variant="outline">
                      {{ getAuthModeLabel(schema.auth_types[0] ?? 'api_key') }}
                    </Badge>
                  </div>
                  <p class="mt-1 text-xs text-oh-muted">
                    {{ getProviderDisplayMeta(schema.provider).protocolSummary }}
                  </p>
                </button>
              </div>

              <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <Input
                  v-model="state.name"
                  label="Connection Name"
                  placeholder="e.g. OpenAI Primary Pool"
                />
                <Select
                  v-model="state.authType"
                  label="Authentication Mode"
                  :options="authModeOptions"
                />
                <Input
                  :model-value="state.endpoint"
                  label="Upstream Endpoint URL"
                  mono
                  placeholder="https://api.openai.com"
                  @update:model-value="handleEndpointChange"
                />
                <Input
                  v-model="state.networkPolicyId"
                  label="Egress Network Policy ID"
                  mono
                  placeholder="default"
                />
              </div>
            </div>
          </Card>

          <Card v-else-if="state.currentStep === 'credentials'" padding="lg">
            <div class="space-y-5">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <h2 class="text-base font-semibold text-oh-fg">2. Credentials & Authorization</h2>
                  <p class="text-xs text-oh-muted">
                    Explicit Keep / Set / Clear semantics. Stored secrets are never echoed.
                  </p>
                </div>
                <Badge
                  :tone="state.credentialState === 'verified' ? 'success' : 'warning'"
                  variant="subtle"
                >
                  {{ state.credentialState }}
                  <span v-if="state.credentialLastFour" class="ml-1 font-mono">
                    ••••{{ state.credentialLastFour }}
                  </span>
                </Badge>
              </div>

              <SegmentedControl
                :model-value="state.credentialMode"
                :options="credentialModeOptions"
                full-width
                @update:model-value="handleCredentialModeChange"
              />

              <div v-if="state.credentialMode === 'set'" class="space-y-3">
                <Input
                  v-model="state.credentialSecret"
                  type="password"
                  :label="`${getAuthModeLabel(state.authType)} Secret`"
                  placeholder="Paste API key or token"
                  autocomplete="new-password"
                />
                <Input
                  v-if="supportsOAuth"
                  v-model="state.credentialRefreshToken"
                  type="password"
                  label="Refresh Token (optional)"
                  placeholder="OAuth refresh token"
                  autocomplete="new-password"
                />
              </div>

              <div
                v-if="supportsOAuth"
                class="rounded-oh-md border border-oh-default bg-oh-subtle/40 p-4 space-y-3"
              >
                <div class="flex items-center justify-between">
                  <span class="text-sm font-medium text-oh-fg">OAuth Authorization Enrollment</span>
                  <Badge tone="accent" variant="subtle">OAuth 2.0</Badge>
                </div>
                <div class="flex flex-col gap-2 sm:flex-row">
                  <Input
                    v-model="oauthCode"
                    placeholder="Paste authorization code"
                    class="flex-1"
                  />
                  <Button
                    variant="secondary"
                    size="sm"
                    :loading="busy"
                    :disabled="!oauthCode.trim()"
                    @click="handleEnrollOAuth"
                  >
                    Attach Authorization
                  </Button>
                </div>
              </div>
            </div>
          </Card>

          <Card v-else-if="state.currentStep === 'verify'" padding="lg">
            <div class="space-y-5">
              <div class="flex flex-wrap items-center justify-between gap-3">
                <div>
                  <h2 class="text-base font-semibold text-oh-fg">3. Connection & Auth Verification</h2>
                  <p class="text-xs text-oh-muted">
                    Server-side reachability, TLS, credential auth, and model discovery probe.
                  </p>
                </div>
                <Button
                  variant="primary"
                  size="sm"
                  :loading="busy"
                  @click="handleRunVerification"
                >
                  {{ state.verification ? 'Re-run Verification' : 'Run Verification Probe' }}
                </Button>
              </div>

              <div
                v-if="state.verificationStale"
                class="rounded-oh-md border border-oh-warning bg-oh-surface px-3 py-2 text-xs text-oh-fg"
              >
                Verification is stale because endpoint or credential settings changed. Re-run verification before previewing.
              </div>

              <div
                v-if="verifyOperation"
                class="flex items-center justify-between rounded-oh-md border border-oh-default bg-oh-subtle/40 px-3 py-2 text-xs"
              >
                <span class="font-mono tabular-nums text-oh-muted">op: {{ verifyOperation.operation_id }}</span>
                <Badge tone="accent" variant="subtle">{{ verifyOperation.state }}</Badge>
              </div>

              <div v-if="state.verification" class="divide-y divide-oh-default rounded-oh-md border border-oh-default">
                <div
                  v-for="check in state.verification.checks"
                  :key="check.name"
                  class="flex flex-wrap items-center justify-between gap-2 px-3.5 py-2.5 text-sm"
                >
                  <div>
                    <div class="font-medium text-oh-fg">{{ check.name }}</div>
                    <div v-if="check.reason" class="text-xs text-oh-danger">{{ check.reason }}</div>
                  </div>
                  <div class="flex items-center gap-3">
                    <Badge
                      :tone="check.state === 'passed' ? 'success' : check.state === 'failed' ? 'danger' : 'neutral'"
                      variant="subtle"
                    >
                      {{ check.state }}
                    </Badge>
                  </div>
                </div>
              </div>
            </div>
          </Card>

          <Card v-else-if="state.currentStep === 'preview'" padding="lg">
            <div class="space-y-5">
              <div class="flex flex-wrap items-center justify-between gap-3">
                <div>
                  <h2 class="text-base font-semibold text-oh-fg">4. Pool Assignment & Preview</h2>
                  <p class="text-xs text-oh-muted">
                    Assign resource pools, model IDs, and concurrency caps, then generate a preview.
                  </p>
                </div>
                <Button
                  variant="secondary"
                  size="sm"
                  :loading="busy"
                  @click="handleRunPreview"
                >
                  Generate Preview
                </Button>
              </div>

              <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <Input
                  v-model="modelIdsText"
                  label="Exposed Model IDs (comma-separated)"
                  mono
                  placeholder="gpt-4o, gpt-4.1-mini"
                />
                <Input
                  :model-value="String(state.concurrency)"
                  label="Concurrency Cap"
                  type="number"
                  mono
                  @update:model-value="state.concurrency = Math.max(1, Number($event) || 1)"
                />
              </div>

              <div class="space-y-2">
                <div class="flex items-center justify-between">
                  <span class="text-xs font-medium text-oh-fg">Resource Pools</span>
                  <div class="flex items-center gap-2">
                    <Input
                      v-model="newPoolName"
                      size="sm"
                      placeholder="New pool name"
                    />
                    <Button
                      size="xs"
                      variant="ghost"
                      :loading="creatingPool"
                      :disabled="!newPoolName.trim()"
                      @click="handleCreateInlinePool"
                    >
                      + Create Pool
                    </Button>
                  </div>
                </div>
                <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
                  <div
                    v-for="pool in pools"
                    :key="pool.id"
                    class="flex items-center justify-between rounded-oh-md border border-oh-default px-3 py-2 text-xs"
                  >
                    <Checkbox
                      :model-value="state.poolIds.includes(pool.id)"
                      :label="pool.name"
                      @update:model-value="togglePool(pool.id)"
                    />
                    <span class="font-mono tabular-nums text-oh-muted">{{ pool.id }}</span>
                  </div>
                </div>
              </div>

              <div
                v-if="state.preview"
                class="rounded-oh-md border border-oh-default bg-oh-subtle/40 p-3 space-y-2 text-xs"
              >
                <div class="flex items-center justify-between">
                  <span class="font-medium text-oh-fg">Preview Ready</span>
                  <span class="font-mono tabular-nums text-oh-muted">
                    id: {{ state.preview.preview_id }} · expires {{ state.preview.expires_at }}
                  </span>
                </div>
                <div v-for="item in state.preview.items" :key="item.id" class="flex items-center justify-between">
                  <span class="font-mono tabular-nums">{{ item.id }} v{{ item.version ?? item.base_version }}</span>
                  <Badge tone="success" variant="subtle">{{ item.state }}</Badge>
                </div>
              </div>
            </div>
          </Card>

          <Card v-else-if="state.currentStep === 'activate'" padding="lg">
            <div class="space-y-5">
              <div>
                <h2 class="text-base font-semibold text-oh-fg">5. Confirm Version & Activate</h2>
                <p class="text-xs text-oh-muted">
                  Review the verified configuration and activate with fenced version confirmation.
                </p>
              </div>

              <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 rounded-oh-md border border-oh-default bg-oh-subtle/40 p-4 text-xs">
                <div>
                  <span class="text-oh-muted">Provider & Auth</span>
                  <div class="mt-0.5 font-medium text-oh-fg">
                    {{ getProviderDisplayMeta(state.provider).label }} · {{ getAuthModeLabel(state.authType) }}
                  </div>
                </div>
                <div>
                  <span class="text-oh-muted">Version</span>
                  <div class="mt-0.5 font-mono tabular-nums text-oh-fg">v{{ state.version }}</div>
                </div>
                <div>
                  <span class="text-oh-muted">Endpoint</span>
                  <div class="mt-0.5 font-mono text-oh-fg truncate">{{ state.endpoint || '—' }}</div>
                </div>
                <div>
                  <span class="text-oh-muted">Pools & Concurrency</span>
                  <div class="mt-0.5 font-mono tabular-nums text-oh-fg">
                    {{ state.poolIds.length }} pools · {{ state.concurrency }} concurrent
                  </div>
                </div>
              </div>

              <div class="flex flex-wrap items-center justify-end gap-2">
                <Button
                  variant="secondary"
                  size="sm"
                  :loading="busy"
                  @click="handleSaveDraft"
                >
                  Save Disabled / Draft
                </Button>
                <Button
                  variant="primary"
                  size="sm"
                  :loading="busy"
                  :disabled="Boolean(state.confirmedVersion)"
                  @click="handleActivate"
                >
                  {{ state.confirmedVersion ? `Activated (v${state.confirmedVersion})` : 'Enable Connection' }}
                </Button>
              </div>
            </div>
          </Card>

          <div class="flex items-center justify-between pt-2">
            <Button
              variant="ghost"
              size="sm"
              :disabled="state.currentStep === 'provider'"
              @click="goBack"
            >
              Previous
            </Button>
            <div class="flex items-center gap-3">
              <span v-if="!stepValidation.valid" class="text-xs text-oh-warning">
                {{ stepValidation.reason }}
              </span>
              <Button
                v-if="state.currentStep !== 'activate'"
                variant="primary"
                size="sm"
                :disabled="!stepValidation.valid"
                @click="goNext"
              >
                Continue
              </Button>
            </div>
          </div>
        </main>
      </div>
    </div>
  </AppLayout>
</template>
