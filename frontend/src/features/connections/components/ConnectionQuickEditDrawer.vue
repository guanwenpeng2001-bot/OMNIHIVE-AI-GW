<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Badge, Button, Drawer, Input, Select } from '@/components/ui'
import { connectionsV2Api, pollConnectionOperation } from '../api'
import {
  canPerformConnectionAction,
  getAuthModeLabel,
  getProviderDisplayMeta,
  isConnectionResource,
  mapApiErrorToAsyncContext
} from '../rules'
import type {
  AsyncErrorContext,
  AsyncPageState,
  ConnectionOperation,
  ConnectionPool,
  ConnectionResource
} from '../types'
import AsyncStateBanner from './AsyncStateBanner.vue'

export interface ConnectionQuickEditDrawerProps {
  open: boolean
  connectionId: string | null
  workspaceId?: string
}

const props = defineProps<ConnectionQuickEditDrawerProps>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  saved: [connectionId: string, version: string]
  openWizard: [connectionId: string]
}>()

const loading = ref(false)
const saving = ref(false)
const rotating = ref(false)
const connection = ref<ConnectionResource | null>(null)
const pools = ref<ConnectionPool[]>([])
const asyncState = ref<AsyncPageState | null>(null)
const asyncError = ref<AsyncErrorContext | null>(null)
const confirmedVersion = ref<string | null>(null)

const nameInput = ref('')
const endpointInput = ref('')
const concurrencyInput = ref('4')
const selectedPoolId = ref('')

const rotationSecret = ref('')
const rotationOperation = ref<ConnectionOperation | null>(null)

const canUpdate = computed(() => canPerformConnectionAction(connection.value, 'update'))
const canRotate = computed(() => canPerformConnectionAction(connection.value, 'rotate_credential'))
const providerMeta = computed(() =>
  connection.value ? getProviderDisplayMeta(connection.value.provider) : null
)
const poolOptions = computed(() => [
  { value: '', label: 'Unassigned' },
  ...pools.value.map((pool) => ({ value: pool.id, label: `${pool.name} (${pool.id})` }))
])

async function loadConnection() {
  if (!props.connectionId) return
  loading.value = true
  asyncState.value = 'initial_load'
  asyncError.value = null
  confirmedVersion.value = null
  rotationOperation.value = null
  try {
    const [resource, poolPage] = await Promise.all([
      connectionsV2Api.getConnection(props.connectionId, props.workspaceId),
      connectionsV2Api.listPools({ limit: 100 }, props.workspaceId).catch(() => ({ items: [] }))
    ])
    connection.value = resource
    pools.value = poolPage.items
    nameInput.value = resource.name
    endpointInput.value = resource.endpoint
    concurrencyInput.value = String(resource.limits?.concurrency ?? 4)
    selectedPoolId.value = resource.pool_ids[0] ?? ''
    asyncState.value = null
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(err, 'Failed to load connection details.')
    asyncState.value = mapped.state
    asyncError.value = mapped
  } finally {
    loading.value = false
  }
}

watch(
  () => [props.open, props.connectionId] as const,
  ([isOpen, id]) => {
    if (isOpen && id) {
      void loadConnection()
    }
  },
  { immediate: true }
)

async function handleSaveQuickEdit() {
  if (!connection.value || saving.value) return
  saving.value = true
  asyncState.value = 'background_refresh'
  asyncError.value = null
  try {
    const parsedConcurrency = Math.max(1, Math.trunc(Number(concurrencyInput.value) || 4))
    const result = await connectionsV2Api.patchConnection(
      connection.value.id,
      connection.value.version,
      {
        name: nameInput.value.trim(),
        endpoint: endpointInput.value.trim(),
        pool_ids: selectedPoolId.value ? [selectedPoolId.value] : [],
        limits: {
          ...connection.value.limits,
          concurrency: parsedConcurrency
        }
      },
      props.workspaceId
    )
    if (!isConnectionResource(result)) {
      confirmedVersion.value = result.version
      asyncState.value = 'confirmed'
      emit('saved', connection.value.id, result.version)
      await loadConnection()
    } else {
      connection.value = result
      confirmedVersion.value = result.version
      asyncState.value = 'confirmed'
      emit('saved', result.id, result.version)
    }
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(err, 'Quick edit failed.')
    asyncState.value = mapped.state
    asyncError.value = mapped
  } finally {
    saving.value = false
  }
}

async function handleRotateCredential() {
  if (!connection.value || rotating.value || !rotationSecret.value.trim()) return
  rotating.value = true
  asyncState.value = 'background_refresh'
  asyncError.value = null
  try {
    const initialOp = await connectionsV2Api.rotateCredential(
      connection.value.id,
      connection.value.version,
      {
        expected_credential_version: connection.value.credential?.version ?? '1',
        replacement: {
          type: connection.value.auth_type,
          secret: rotationSecret.value.trim()
        }
      },
      props.workspaceId
    )
    rotationOperation.value = initialOp
    const finalOp = await pollConnectionOperation(initialOp, {
      onUpdate: (op) => {
        rotationOperation.value = op
      }
    })
    rotationSecret.value = ''
    if (finalOp.state === 'failed') {
      asyncState.value = 'partial_failure'
      asyncError.value = {
        state: 'partial_failure',
        status: 500,
        message: typeof finalOp.error?.message === 'string' ? finalOp.error.message : 'Credential rotation failed.',
        requestId: finalOp.operation_id
      }
    } else {
      asyncState.value = 'confirmed'
      confirmedVersion.value = connection.value.version
      await loadConnection()
    }
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(err, 'Credential rotation request failed.')
    asyncState.value = mapped.state
    asyncError.value = mapped
  } finally {
    rotating.value = false
  }
}

async function handleCheckRotationStatus() {
  if (!rotationOperation.value) return
  try {
    const op = await connectionsV2Api.getOperation(rotationOperation.value.status_url)
    rotationOperation.value = op
    if (op.state === 'succeeded') {
      asyncState.value = 'confirmed'
      await loadConnection()
    }
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(err, 'Unable to fetch operation status.')
    asyncState.value = mapped.state
    asyncError.value = mapped
  }
}
</script>

<template>
  <Drawer
    :open="open"
    size="md"
    title="Quick Edit Connection"
    subtitle="Fenced versioned updates and credential rotation"
    @update:open="emit('update:open', $event)"
  >
    <div class="space-y-5">
      <AsyncStateBanner
        v-if="asyncState"
        :state="asyncState"
        :error="asyncError"
        :confirmed-version="confirmedVersion"
        confirmed-label="Connection updated"
        @retry="loadConnection"
        @reload-latest="loadConnection"
        @refresh-status="handleCheckRotationStatus"
      />

      <template v-if="connection && asyncState !== 'initial_load'">
        <div class="flex flex-wrap items-center justify-between gap-2 rounded-oh-md border border-oh-default bg-oh-subtle/50 px-3 py-2.5">
          <div class="space-y-0.5">
            <div class="flex items-center gap-2">
              <span class="text-sm font-semibold text-oh-fg">{{ providerMeta?.label }}</span>
              <Badge tone="neutral" variant="outline">{{ getAuthModeLabel(connection.auth_type) }}</Badge>
            </div>
            <div class="font-mono text-xs tabular-nums text-oh-muted">
              id: {{ connection.id }} · v{{ connection.version }}
            </div>
          </div>
          <Button
            size="xs"
            variant="ghost"
            @click="emit('openWizard', connection.id)"
          >
            Full 5-Step Wizard
          </Button>
        </div>

        <div class="space-y-3">
          <Input
            v-model="nameInput"
            label="Connection Name"
            :disabled="!canUpdate || saving"
          />
          <Input
            v-model="endpointInput"
            label="Upstream Endpoint"
            mono
            :disabled="!canUpdate || saving"
          />
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Input
              v-model="concurrencyInput"
              label="Concurrency Cap"
              type="number"
              mono
              :disabled="!canUpdate || saving"
            />
            <Select
              v-model="selectedPoolId"
              label="Primary Pool"
              :options="poolOptions"
              :disabled="!canUpdate || saving"
            />
          </div>
          <div class="flex justify-end pt-1">
            <Button
              variant="primary"
              size="sm"
              :loading="saving"
              :disabled="!canUpdate"
              @click="handleSaveQuickEdit"
            >
              Save Quick Edit
            </Button>
          </div>
        </div>

        <div class="border-t border-oh-default pt-4 space-y-3">
          <div class="flex items-center justify-between">
            <div>
              <h3 class="text-sm font-semibold text-oh-fg">Fenced Credential Rotation</h3>
              <p class="text-xs text-oh-muted">
                Stored secret is never echoed. Current suffix:
                <span class="font-mono tabular-nums">
                  {{ connection.credential?.last_four ? `••••${connection.credential.last_four}` : '—' }}
                </span>
              </p>
            </div>
            <Badge
              :tone="connection.credential?.state === 'verified' ? 'success' : 'warning'"
              variant="subtle"
            >
              {{ connection.credential?.state || 'unset' }}
            </Badge>
          </div>

          <Input
            v-model="rotationSecret"
            type="password"
            label="New Secret"
            placeholder="Enter replacement API key or token"
            autocomplete="new-password"
            :disabled="!canRotate || rotating"
          />

          <div
            v-if="rotationOperation"
            class="rounded-oh-md border border-oh-default bg-oh-surface px-3 py-2 text-xs space-y-1"
          >
            <div class="flex items-center justify-between">
              <span class="font-medium text-oh-fg">Rotation Operation</span>
              <Badge tone="accent" variant="subtle">{{ rotationOperation.state }}</Badge>
            </div>
            <div class="font-mono tabular-nums text-oh-muted">
              op: {{ rotationOperation.operation_id }}
            </div>
          </div>

          <div class="flex justify-end">
            <Button
              variant="secondary"
              size="sm"
              :loading="rotating"
              :disabled="!canRotate || !rotationSecret.trim()"
              @click="handleRotateCredential"
            >
              Rotate Credential
            </Button>
          </div>
        </div>
      </template>
    </div>
  </Drawer>
</template>
