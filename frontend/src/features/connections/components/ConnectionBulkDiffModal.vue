<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Badge, Button, Checkbox, Input, Modal, Select } from '@/components/ui'
import { connectionsApi, pollConnectionOperation } from '../api'
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

const i18n = useI18n()
const isZh = computed(() => String(i18n.locale?.value || '').toLowerCase().startsWith('zh'))

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
  { value: '', label: isZh.value ? '保持当前资源池不变' : 'Keep current pools' },
  ...pools.value.map((pool) => ({ value: pool.id, label: `${pool.name} (#${pool.id})` }))
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
      const poolPage = await connectionsApi.listPools({ limit: 100 }, props.workspaceId)
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
    const preview = await connectionsApi.bulkPreview(
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
    const mapped = mapApiErrorToAsyncContext(
      err,
      isZh.value ? '生成批量变更预览失败。' : 'Bulk preview failed.',
      isZh.value
    )
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
    const initialOp = await connectionsApi.bulkApply(
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
        message:
          typeof finalOp.error?.message === 'string'
            ? finalOp.error.message
            : isZh.value
              ? '部分连接更新未成功，请检查预览状态后重试。'
              : 'Some connections failed to update.',
        requestId: finalOp.operation_id
      }
    } else {
      asyncState.value = 'confirmed'
      emit('applied')
    }
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(
      err,
      isZh.value ? '应用批量变更失败。' : 'Bulk apply failed.',
      isZh.value
    )
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

function formatItemState(state: string): string {
  if (!isZh.value) return state
  if (state === 'applicable') return '可应用'
  if (state === 'applied') return '已应用'
  if (state === 'conflict') return '修订冲突'
  if (state === 'blocked') return '受阻断'
  return state
}
</script>

<template>
  <Modal
    :open="open"
    size="lg"
    :title="isZh ? '批量变更预览与应用' : 'Bulk Connection Preview & Apply'"
    :subtitle="
      isZh
        ? '在批量生效前逐条检查配置修订差异与连通性前置条件'
        : 'Preview per-connection revision diffs before applying updates'
    "
    @update:open="emit('update:open', $event)"
  >
    <div class="space-y-4">
      <AsyncStateBanner
        v-if="asyncState"
        :state="asyncState"
        :error="asyncError"
        :confirmed-label="isZh ? '批量更新已生效' : 'Bulk update applied'"
        :empty-title="isZh ? '预览中没有可变更的连接' : 'No matching connections in preview'"
        @retry="runBulkPreview"
      />

      <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <Select
          v-model="targetPoolId"
          :label="isZh ? '分配出站资源池' : 'Assign Pool'"
          :options="poolOptions"
        />
        <Input
          v-model="targetModelIds"
          :label="isZh ? '暴露模型 ID（逗号分隔）' : 'Model IDs (comma-separated)'"
          mono
          :placeholder="isZh ? '留空保持不变' : 'Unchanged'"
        />
        <Input
          v-model="targetConcurrency"
          :label="isZh ? '并发上限' : 'Concurrency Cap'"
          type="number"
          mono
          :placeholder="isZh ? '留空保持不变' : 'Unchanged'"
        />
      </div>

      <div class="flex flex-wrap items-center justify-between gap-2 rounded-oh-md border border-oh-default bg-oh-subtle/40 px-3 py-2 text-xs">
        <span class="text-oh-muted">
          {{ isZh ? '已选上游连接：' : 'Selected connections: ' }}
          <strong class="font-mono tabular-nums text-oh-fg">{{ connectionIds.length }}</strong>
        </span>
        <Button
          size="sm"
          variant="secondary"
          :loading="previewing"
          :disabled="connectionIds.length === 0"
          @click="runBulkPreview"
        >
          {{ isZh ? '生成逐项差异预览' : 'Generate Diff Preview' }}
        </Button>
      </div>

      <div v-if="previewResult" class="space-y-3">
        <div class="flex flex-wrap items-center gap-2 text-xs">
          <Badge tone="success" variant="subtle">
            {{ isZh ? '可应用' : 'Applicable' }}: {{ summary.applicableCount }}
          </Badge>
          <Badge tone="warning" variant="subtle">
            {{ isZh ? '修订冲突' : 'Conflict' }}: {{ summary.conflictCount }}
          </Badge>
          <Badge tone="danger" variant="subtle">
            {{ isZh ? '受阻断' : 'Blocked' }}: {{ summary.blockedCount }}
          </Badge>
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
                  #{{ item.id }}
                  <span class="text-oh-muted">
                    {{ isZh ? `修订 #${item.version ?? item.base_version}` : `Rev #${item.version ?? item.base_version}` }}
                  </span>
                </div>
                <div v-if="item.reason" class="text-oh-muted truncate">
                  {{ item.reason }}
                </div>
              </div>
            </div>
            <Badge :tone="itemBadgeTone(item.state)" variant="subtle">
              {{ formatItemState(item.state) }}
            </Badge>
          </div>
        </div>
      </div>

      <div
        v-if="applyOperation"
        class="rounded-oh-md border border-oh-default bg-oh-surface px-3 py-2 text-xs flex items-center justify-between"
      >
        <span class="text-oh-fg">{{ isZh ? '批量应用状态' : 'Bulk Apply Status' }}</span>
        <Badge tone="accent" variant="subtle">{{ applyOperation.state }}</Badge>
      </div>
    </div>

    <template #footer>
      <div class="flex items-center justify-end gap-2">
        <Button variant="ghost" size="sm" @click="emit('update:open', false)">
          {{ isZh ? '关闭' : 'Close' }}
        </Button>
        <Button
          variant="primary"
          size="sm"
          :loading="applying"
          :disabled="!previewResult || selectedIds.length === 0"
          @click="runBulkApply"
        >
          {{ isZh ? `确认应用 (${selectedIds.length})` : `Apply (${selectedIds.length})` }}
        </Button>
      </div>
    </template>
  </Modal>
</template>
