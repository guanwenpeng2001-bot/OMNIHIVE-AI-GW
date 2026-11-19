<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Badge, Button, Checkbox, Input, Modal, Select } from '@/components/ui'
import { connectionsV2Api, pollConnectionOperation } from '../api'
import { mapApiErrorToAsyncContext } from '../rules'
import { computeBulkDiffSummary, toggleBulkSelection } from '../wizardState'
import type {
  AsyncErrorContext,
  AsyncPageState,
  ConnectionAssignments,
  ConnectionOperation,
  ConnectionPool,
  ConnectionPreview
} from '../types'
import AsyncStateBanner from './AsyncStateBanner.vue'

export interface ConnectionBulkDiffModalProps {
  open: boolean
  connectionIds: string[]
  workspaceId?: string
}

const props = defineProps<ConnectionBulkDiffModalProps>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  applied: []
}>()

const targetConcurrency = ref('')
const targetPoolId = ref('')
const targetModelIds = ref('')
const pools = ref<ConnectionPool[]>([])

const previewing = ref(false)
const applying = ref(false)
const previewResult = ref<ConnectionPreview | null>(null)
const selectedIds = ref<string[]>([])
const applyOperation = ref<ConnectionOperation | null>(null)
const asyncState = ref<AsyncPageState | null>(null)
const asyncError = ref<AsyncErrorContext | null>(null)

const poolOptions = computed(() => [
  { value: '', label: 'Keep current pools' },
  ...pools.value.map((pool) => ({ value: pool.id, label: `${pool.name} (${pool.id})` }))
])

const summary = computed(() =>
  computeBulkDiffSummary(previewResult.value?.items ?? [])
)

watch(
  () => props.open,
  async (isOpen) => {
    if (!isOpen) return
    previewResult.value = null
    applyOperation.value = null
    asyncState.value = null
    asyncError.value = null
    selectedIds.value = []
    try {
      const poolPage = await connectionsV2Api.listPools({ limit: 100 }, props.workspaceId)
      pools.value = poolPage.items
    } catch {
      pools.value = []
    }
  }
)

function buildPatchPayload(): ConnectionAssignments {
  const patch: ConnectionAssignments = {}
  if (targetPoolId.value) {
    patch.pool_ids = [targetPoolId.value]
  }
  if (targetModelIds.value.trim()) {
    patch.model_ids = targetModelIds.value
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s.length > 0)
  }
  const concurrencyNum = Number(targetConcurrency.value)
  if (targetConcurrency.value.trim() && Number.isFinite(concurrencyNum) && concurrencyNum > 0) {
    patch.limits = { concurrency: Math.trunc(concurrencyNum) }
  }
  return patch
}

async function runBulkPreview() {
  if (props.connectionIds.length === 0 || previewing.value) return
  previewing.value = true
  asyncState.value = 'background_refresh'
  asyncError.value = null
  applyOperation.value = null
  try {
    const preview = await connectionsV2Api.bulkPreview(
      {
        ids: props.connectionIds,
        patch: buildPatchPayload()
      },
      props.workspaceId
    )
    previewResult.value = preview
    selectedIds.value = computeBulkDiffSummary(preview.items).selectableIds
    asyncState.value = preview.items.length === 0 ? 'empty' : null
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(err, 'Bulk preview failed.')
    asyncState.value = mapped.state
    asyncError.value = mapped
  } finally {
    previewing.value = false
  }
}

async function runBulkApply() {
  if (!previewResult.value || selectedIds.value.length === 0 || applying.value) return
  applying.value = true
  asyncState.value = 'background_refresh'
  asyncError.value = null
  try {
    const initialOp = await connectionsV2Api.bulkApply(
      {
        preview_id: previewResult.value.preview_id,
        selected_ids: selectedIds.value
      },
      props.workspaceId
    )
    applyOperation.value = initialOp
    const finalOp = await pollConnectionOperation(initialOp, {
      onUpdate: (op) => {
        applyOperation.value = op
      }
    })
    if (finalOp.state === 'failed') {
      asyncState.value = 'partial_failure'
      asyncError.value = {
        state: 'partial_failure',
        status: 207,
        message: typeof finalOp.error?.message === 'string' ? finalOp.error.message : 'Some connections failed to update.',
        requestId: finalOp.operation_id
      }
    } else {
      asyncState.value = 'confirmed'
      emit('applied')
    }
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(err, 'Bulk apply failed.')
    asyncState.value = mapped.state
    asyncError.value = mapped
  } finally {
    applying.value = false
  }
}

function itemBadgeTone(state: string): 'success' | 'warning' | 'danger' | 'neutral' {
  if (state === 'applicable' || state === 'applied') return 'success'
  if (state === 'conflict') return 'warning'
  if (state === 'blocked') return 'danger'
  return 'neutral'
}
</script>

<template>
  <Modal
    :open="open"
    size="lg"
    title="Bulk Connection Preview & Apply"
    subtitle="Preview per-item version diffs before applying fenced updates"
    @update:open="emit('update:open', $event)"
  >
    <div class="space-y-4">
      <AsyncStateBanner
        v-if="asyncState"
        :state="asyncState"
        :error="asyncError"
        confirmed-label="Bulk update applied"
        empty-title="No matching connections in preview"
        @retry="runBulkPreview"
      />

      <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <Select
          v-model="targetPoolId"
          label="Assign Pool"
          :options="poolOptions"
        />
        <Input
          v-model="targetModelIds"
          label="Model IDs (comma-separated)"
          mono
          placeholder="Unchanged"
        />
        <Input
          v-model="targetConcurrency"
          label="Concurrency Cap"
          type="number"
          mono
          placeholder="Unchanged"
        />
      </div>

      <div class="flex flex-wrap items-center justify-between gap-2 rounded-oh-md border border-oh-default bg-oh-subtle/40 px-3 py-2 text-xs">
        <span class="text-oh-muted">
          Selected connections: <strong class="font-mono tabular-nums text-oh-fg">{{ connectionIds.length }}</strong>
        </span>
        <Button
          size="sm"
          variant="secondary"
          :loading="previewing"
          :disabled="connectionIds.length === 0"
          @click="runBulkPreview"
        >
          Generate Per-Item Diff Preview
        </Button>
      </div>

      <div v-if="previewResult" class="space-y-3">
        <div class="flex flex-wrap items-center gap-2 text-xs">
          <Badge tone="success" variant="subtle">
            Applicable: {{ summary.applicableCount }}
          </Badge>
          <Badge tone="warning" variant="subtle">
            Conflict: {{ summary.conflictCount }}
          </Badge>
          <Badge tone="danger" variant="subtle">
            Blocked: {{ summary.blockedCount }}
          </Badge>
          <span class="ml-auto font-mono tabular-nums text-oh-muted">
            preview: {{ previewResult.preview_id }}
          </span>
        </div>

        <div class="max-h-64 overflow-y-auto divide-y divide-oh-default rounded-oh-md border border-oh-default">
          <div
            v-for="item in previewResult.items"
            :key="item.id"
            class="flex items-center justify-between gap-3 px-3 py-2 text-xs"
          >
            <div class="flex items-center gap-2.5 min-w-0">
              <Checkbox
                :model-value="selectedIds.includes(item.id)"
                :disabled="item.state !== 'applicable'"
                @update:model-value="selectedIds = toggleBulkSelection(selectedIds, item.id)"
              />
              <div class="min-w-0">
                <div class="font-mono tabular-nums text-oh-fg truncate">
                  {{ item.id }} <span class="text-oh-muted">v{{ item.version ?? item.base_version }}</span>
                </div>
                <div v-if="item.reason" class="text-oh-muted truncate">
                  {{ item.reason }}
                </div>
              </div>
            </div>
            <Badge :tone="itemBadgeTone(item.state)" variant="subtle">
              {{ item.state }}
            </Badge>
          </div>
        </div>
      </div>

      <div
        v-if="applyOperation"
        class="rounded-oh-md border border-oh-default bg-oh-surface px-3 py-2 text-xs flex items-center justify-between"
      >
        <span class="font-mono tabular-nums text-oh-fg">Operation {{ applyOperation.operation_id }}</span>
        <Badge tone="accent" variant="subtle">{{ applyOperation.state }}</Badge>
      </div>
    </div>

    <template #footer>
      <div class="flex items-center justify-end gap-2">
        <Button variant="ghost" size="sm" @click="emit('update:open', false)">
          Close
        </Button>
        <Button
          variant="primary"
          size="sm"
          :loading="applying"
          :disabled="!previewResult || selectedIds.length === 0"
          @click="runBulkApply"
        >
          Apply ({{ selectedIds.length }})
        </Button>
      </div>
    </template>
  </Modal>
</template>
