<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { Badge, Button, Card, Input } from '@/components/ui'
import { connectionsApi } from '@/features/connections/api'
import type { ConnectionPool } from '@/features/connections/types'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{
  workspaceId?: string
}>()

const { t } = useI18n()
const router = useRouter()

const pools = ref<ConnectionPool[]>([])
const loading = ref(false)
const errorMsg = ref('')

const showCreateForm = ref(false)
const createName = ref('')
const createConcurrency = ref('50')
const creating = ref(false)

const editingPoolId = ref<string | null>(null)
const editName = ref('')
const editConcurrency = ref('50')
const savingEdit = ref(false)
const deletingId = ref<string | null>(null)
const pendingDeletePool = ref<ConnectionPool | null>(null)
let loadSeq = 0

const workspaceLabel = computed(() => {
  const ws = props.workspaceId?.trim()
  return ws ? `#${ws}` : t('routing.outboundPools.globalWorkspace')
})

async function loadPools(): Promise<void> {
  const seq = ++loadSeq
  const targetWorkspaceId = props.workspaceId
  loading.value = true
  errorMsg.value = ''
  try {
    const page = await connectionsApi.listPools({ limit: 100 }, targetWorkspaceId)
    if (seq !== loadSeq || props.workspaceId !== targetWorkspaceId) return
    pools.value = page.items ?? []
  } catch (err) {
    if (seq !== loadSeq || props.workspaceId !== targetWorkspaceId) return
    errorMsg.value = extractApiErrorMessage(err, t('routing.outboundPools.loadFailed'))
  } finally {
    if (seq === loadSeq) {
      loading.value = false
    }
  }
}

async function handleCreate(): Promise<void> {
  const name = createName.value.trim()
  const maxConcurrency = Number(createConcurrency.value)
  if (!name || !Number.isInteger(maxConcurrency) || maxConcurrency < 1 || maxConcurrency > 10000) {
    errorMsg.value = t('routing.outboundPools.validationError')
    return
  }
  creating.value = true
  errorMsg.value = ''
  try {
    await connectionsApi.createPool({ name, max_concurrency: maxConcurrency }, props.workspaceId)
    createName.value = ''
    createConcurrency.value = '50'
    showCreateForm.value = false
    await loadPools()
  } catch (err) {
    errorMsg.value = extractApiErrorMessage(err, t('routing.outboundPools.createFailed'))
  } finally {
    creating.value = false
  }
}

function startEdit(pool: ConnectionPool): void {
  editingPoolId.value = pool.id
  editName.value = pool.name
  editConcurrency.value = String(pool.max_concurrency)
  errorMsg.value = ''
}

async function saveEdit(poolId: string): Promise<void> {
  const name = editName.value.trim()
  const maxConcurrency = Number(editConcurrency.value)
  if (!name || !Number.isInteger(maxConcurrency) || maxConcurrency < 1 || maxConcurrency > 10000) {
    errorMsg.value = t('routing.outboundPools.validationError')
    return
  }
  savingEdit.value = true
  errorMsg.value = ''
  try {
    await connectionsApi.updatePool(poolId, { name, max_concurrency: maxConcurrency }, props.workspaceId)
    editingPoolId.value = null
    await loadPools()
  } catch (err) {
    errorMsg.value = extractApiErrorMessage(err, t('routing.outboundPools.updateFailed'))
  } finally {
    savingEdit.value = false
  }
}

function requestDelete(pool: ConnectionPool): void {
  pendingDeletePool.value = pool
  errorMsg.value = ''
}

function cancelDelete(): void {
  pendingDeletePool.value = null
}

async function confirmDelete(): Promise<void> {
  const pool = pendingDeletePool.value
  if (!pool) return
  deletingId.value = pool.id
  errorMsg.value = ''
  try {
    await connectionsApi.deletePool(pool.id, props.workspaceId)
    pendingDeletePool.value = null
    await loadPools()
  } catch (err) {
    errorMsg.value = extractApiErrorMessage(err, t('routing.outboundPools.deleteFailed'))
  } finally {
    deletingId.value = null
  }
}

watch(
  () => props.workspaceId,
  () => {
    pools.value = []
    editingPoolId.value = null
    pendingDeletePool.value = null
    errorMsg.value = ''
    void loadPools()
  }
)
onMounted(() => void loadPools())
</script>

<template>
  <Card padding="md" class="space-y-3" data-testid="v2-outbound-pools-panel">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div class="space-y-0.5">
        <div class="flex items-center gap-2">
          <h2 class="text-sm font-semibold text-oh-primary">
            {{ t('routing.outboundPools.title') }}
          </h2>
          <Badge tone="accent" size="xs" variant="subtle" mono>{{ pools.length }}</Badge>
        </div>
        <p class="text-xs text-oh-secondary">
          {{ t('routing.outboundPools.description') }}
        </p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <Button size="xs" variant="ghost" :loading="loading" @click="loadPools">
          {{ t('routing.outboundPools.refresh') }}
        </Button>
        <Button size="xs" variant="secondary" @click="router.push('/admin/connections/new')">
          {{ t('routing.outboundPools.openWizard') }}
        </Button>
        <Button size="xs" variant="primary" @click="showCreateForm = !showCreateForm">
          {{ showCreateForm ? t('routing.outboundPools.cancelForm') : t('routing.outboundPools.newPool') }}
        </Button>
      </div>
    </div>

    <p
      v-if="errorMsg"
      role="alert"
      class="rounded-oh-md border border-oh-status-danger-border bg-oh-status-danger-bg px-3 py-2 text-xs text-oh-status-danger-fg"
    >
      {{ errorMsg }}
    </p>

    <div
      v-if="pendingDeletePool"
      role="alertdialog"
      data-testid="outbound-pool-delete-confirm"
      class="flex flex-wrap items-center justify-between gap-3 rounded-oh-md border border-oh-status-danger-border bg-oh-status-danger-bg px-3 py-2.5 text-xs text-oh-status-danger-fg"
    >
      <div class="space-y-0.5">
        <p class="font-semibold">{{ t('routing.outboundPools.deleteConfirmTitle') }}</p>
        <p>
          {{
            t('routing.outboundPools.deleteConfirmMessage', {
              workspace: workspaceLabel,
              name: pendingDeletePool.name
            })
          }}
        </p>
      </div>
      <div class="flex items-center gap-2">
        <Button
          size="xs"
          variant="ghost"
          data-testid="cancel-delete-pool-btn"
          :disabled="deletingId === pendingDeletePool.id"
          @click="cancelDelete"
        >
          {{ t('routing.outboundPools.cancel') }}
        </Button>
        <Button
          size="xs"
          variant="danger"
          data-testid="confirm-delete-pool-btn"
          :loading="deletingId === pendingDeletePool.id"
          @click="confirmDelete"
        >
          {{ t('routing.outboundPools.confirmDelete') }}
        </Button>
      </div>
    </div>

    <form
      v-if="showCreateForm"
      class="grid gap-3 rounded-oh-md border border-oh-subtle bg-oh-subtle/50 p-3 sm:grid-cols-[1fr_11rem_auto] sm:items-end"
      @submit.prevent="handleCreate"
    >
      <Input
        v-model="createName"
        size="sm"
        :label="t('routing.outboundPools.nameLabel')"
        :placeholder="t('routing.outboundPools.namePlaceholder')"
        required
      />
      <Input
        v-model="createConcurrency"
        type="number"
        size="sm"
        mono
        :label="t('routing.outboundPools.concurrencyLabel')"
        placeholder="50"
        required
      />
      <Button type="submit" size="sm" variant="primary" :loading="creating">
        {{ t('routing.outboundPools.createSubmit') }}
      </Button>
    </form>

    <div
      v-if="!loading && pools.length === 0 && !showCreateForm"
      class="rounded-oh-md border border-dashed border-oh-default px-4 py-3 text-xs text-oh-muted"
    >
      {{ t('routing.outboundPools.empty') }}
    </div>

    <div v-else-if="pools.length > 0" class="divide-y divide-oh-subtle rounded-oh-md border border-oh-default">
      <div
        v-for="pool in pools"
        :key="pool.id"
        class="flex flex-wrap items-center justify-between gap-3 px-3.5 py-2.5 text-xs"
      >
        <template v-if="editingPoolId === pool.id">
          <div class="grid flex-1 gap-2 sm:grid-cols-[1fr_9rem]">
            <Input v-model="editName" size="sm" :placeholder="t('routing.outboundPools.nameLabel')" />
            <Input v-model="editConcurrency" type="number" size="sm" mono />
          </div>
          <div class="flex items-center gap-1.5">
            <Button size="xs" variant="primary" :loading="savingEdit" @click="saveEdit(pool.id)">
              {{ t('routing.outboundPools.save') }}
            </Button>
            <Button size="xs" variant="ghost" @click="editingPoolId = null">
              {{ t('routing.outboundPools.cancel') }}
            </Button>
          </div>
        </template>
        <template v-else>
          <div class="flex min-w-0 flex-wrap items-center gap-2.5">
            <span class="font-mono text-oh-muted">#{{ pool.id }}</span>
            <span class="font-medium text-oh-primary">{{ pool.name }}</span>
            <Badge tone="neutral" size="xs" variant="outline" mono>
              {{ t('routing.outboundPools.maxConcurrencyBadge', { count: pool.max_concurrency }) }}
            </Badge>
            <Badge tone="info" size="xs" variant="subtle" mono>
              {{ t('routing.outboundPools.connectionsBadge', { count: pool.connection_ids?.length ?? 0 }) }}
            </Badge>
          </div>
          <div class="flex items-center gap-1.5">
            <Button size="xs" variant="ghost" @click="startEdit(pool)">
              {{ t('routing.outboundPools.edit') }}
            </Button>
            <Button
              size="xs"
              variant="danger"
              :loading="deletingId === pool.id"
              @click="requestDelete(pool)"
            >
              {{ t('routing.outboundPools.delete') }}
            </Button>
          </div>
        </template>
      </div>
    </div>
  </Card>
</template>
