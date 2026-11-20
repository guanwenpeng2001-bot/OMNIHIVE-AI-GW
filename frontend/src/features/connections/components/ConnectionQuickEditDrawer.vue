<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Badge, Button, Drawer, Input, Select } from '@/components/ui'
import { connectionsApi, pollConnectionOperation } from '../api'
import {
  canPerformConnectionAction,
  getAuthModeLabel,
  getProviderDisplayMeta,
  isConnectionResource,
  mapApiErrorToAsyncContext,
  normalizeConnectionEndpointInput,
  validateConnectionEndpoint
} from '../rules'
import type {
  AsyncErrorContext,
  AsyncPageState,
  ConnectionEvent,
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

const i18n = useI18n()
const isZh = computed(() => String(i18n.locale?.value || '').toLowerCase().startsWith('zh'))

const loading = ref(false)
const saving = ref(false)
const rotating = ref(false)
const connection = ref<ConnectionResource | null>(null)
const pools = ref<ConnectionPool[]>([])
const events = ref<ConnectionEvent[]>([])
const eventsLoading = ref(false)
const asyncState = ref<AsyncPageState | null>(null)
const asyncError = ref<AsyncErrorContext | null>(null)
const confirmedVersion = ref<string | null>(null)

const nameInput = ref('')
const endpointInput = ref('')
const concurrencyInput = ref('4')
const selectedPoolId = ref('')

const rotationSecret = ref('')
const rotationOperation = ref<ConnectionOperation | null>(null)

const canUpdate = computed(
  () =>
    canPerformConnectionAction(connection.value, 'connections.edit') ||
    canPerformConnectionAction(connection.value, 'update')
)
const canRotate = computed(
  () =>
    canPerformConnectionAction(connection.value, 'credentials.rotate') ||
    canPerformConnectionAction(connection.value, 'rotate_credential')
)
const providerMeta = computed(() =>
  connection.value ? getProviderDisplayMeta(connection.value.provider, isZh.value) : null
)
const poolOptions = computed(() => [
  { value: '', label: isZh.value ? '暂不绑定资源池' : 'Unassigned' },
  ...pools.value.map((pool) => ({ value: pool.id, label: `${pool.name} (#${pool.id})` }))
])

function formatCredentialState(state?: string): string {
  const key = state || 'unset'
  if (!isZh.value) return key
  if (key === 'verified') return '已验证'
  if (key === 'unverified') return '待验证'
  if (key === 'revoked') return '已吊销'
  return '未设置'
}

function formatOperationState(state?: string): string {
  if (!state) return ''
  if (!isZh.value) return state
  if (state === 'succeeded') return '已完成'
  if (state === 'failed') return '失败'
  if (state === 'running' || state === 'pending') return '执行中'
  return state
}

function formatEventType(type: string): string {
  const labelsZh: Record<string, string> = {
    'connection.created': '创建连接草稿',
    'connection.updated': '更新连接配置',
    'connection.verified': '连通性验证通过',
    'connection.verification_failed': '连通性验证未通过',
    'connection.activated': '启用上游连接',
    'credential.updated': '更新认证凭据',
    'credential.cleared': '清空认证凭据',
    'rotation.validating': '轮换凭据：正在探测新密钥',
    'rotation.switching': '轮换凭据：切换生效中',
    'rotation.draining': '轮换凭据：平滑排空中',
    'rotation.revoked': '轮换凭据：旧密钥已退役',
    'rotation.failed': '轮换凭据：探测失败已回滚'
  }
  const labelsEn: Record<string, string> = {
    'connection.created': 'Draft created',
    'connection.updated': 'Configuration updated',
    'connection.verified': 'Verification passed',
    'connection.verification_failed': 'Verification failed',
    'connection.activated': 'Connection activated',
    'credential.updated': 'Credential updated',
    'credential.cleared': 'Credential cleared',
    'rotation.validating': 'Rotation: validating replacement',
    'rotation.switching': 'Rotation: switching secret',
    'rotation.draining': 'Rotation: draining sessions',
    'rotation.revoked': 'Rotation: previous secret retired',
    'rotation.failed': 'Rotation: validation failed'
  }
  return (isZh.value ? labelsZh[type] : labelsEn[type]) || type
}

async function loadConnectionEvents(id: string) {
  eventsLoading.value = true
  try {
    const page = await connectionsApi.listEvents(id, { limit: 10 }, props.workspaceId)
    events.value = page.items ?? []
  } catch {
    events.value = []
  } finally {
    eventsLoading.value = false
  }
}

async function loadConnection() {
  if (!props.connectionId) return
  loading.value = true
  asyncState.value = 'initial_load'
  asyncError.value = null
  confirmedVersion.value = null
  rotationOperation.value = null
  try {
    const [resource, poolPage] = await Promise.all([
      connectionsApi.getConnection(props.connectionId, props.workspaceId),
      connectionsApi.listPools({ limit: 100 }, props.workspaceId).catch(() => ({ items: [] }))
    ])
    connection.value = resource
    pools.value = poolPage.items
    nameInput.value = resource.name
    endpointInput.value = resource.endpoint
    concurrencyInput.value = String(resource.limits?.concurrency ?? 4)
    selectedPoolId.value = resource.pool_ids[0] ?? ''
    asyncState.value = null
    void loadConnectionEvents(resource.id)
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(
      err,
      isZh.value ? '加载上游连接详情失败。' : 'Failed to load connection details.',
      isZh.value
    )
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
  const normalizedEndpoint = normalizeConnectionEndpointInput(endpointInput.value)
  endpointInput.value = normalizedEndpoint.normalized
  const endpointErr = validateConnectionEndpoint(endpointInput.value, false, isZh.value)
  if (endpointErr) {
    asyncState.value = 'partial_failure'
    asyncError.value = {
      state: 'partial_failure',
      status: 422,
      message: endpointErr
    }
    return
  }
  saving.value = true
  asyncState.value = 'background_refresh'
  asyncError.value = null
  try {
    const parsedConcurrency = Math.max(1, Math.trunc(Number(concurrencyInput.value) || 4))
    const result = await connectionsApi.patchConnection(
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
      void loadConnectionEvents(result.id)
    }
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(
      err,
      isZh.value ? '保存快捷修改失败。' : 'Quick edit failed.',
      isZh.value
    )
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
    const initialOp = await connectionsApi.rotateCredential(
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
        message:
          typeof finalOp.error?.message === 'string'
            ? finalOp.error.message
            : isZh.value
              ? '凭据轮换未通过验证，原凭据保持不变。'
              : 'Credential rotation failed.',
        requestId: finalOp.operation_id
      }
    } else {
      asyncState.value = 'confirmed'
      confirmedVersion.value = connection.value.version
      await loadConnection()
    }
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(
      err,
      isZh.value ? '发起凭据轮换失败。' : 'Credential rotation request failed.',
      isZh.value
    )
    asyncState.value = mapped.state
    asyncError.value = mapped
  } finally {
    rotating.value = false
  }
}

async function handleCheckRotationStatus() {
  if (!rotationOperation.value) return
  try {
    const op = await connectionsApi.getOperation(rotationOperation.value.status_url)
    rotationOperation.value = op
    if (op.state === 'succeeded') {
      asyncState.value = 'confirmed'
      await loadConnection()
    }
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(
      err,
      isZh.value ? '获取操作状态失败。' : 'Unable to fetch operation status.',
      isZh.value
    )
    asyncState.value = mapped.state
    asyncError.value = mapped
  }
}
</script>

<template>
  <Drawer
    :open="open"
    size="md"
    :title="isZh ? '快捷编辑上游连接' : 'Quick Edit Connection'"
    :subtitle="isZh ? '快速调整端点、资源池并发与平滑轮换凭据' : 'Update endpoint, pool assignment, and rotate credentials'"
    @update:open="emit('update:open', $event)"
  >
    <div class="space-y-5">
      <AsyncStateBanner
        v-if="asyncState"
        :state="asyncState"
        :error="asyncError"
        :confirmed-version="confirmedVersion"
        :confirmed-label="isZh ? '连接配置已更新' : 'Connection updated'"
        @retry="loadConnection"
        @reload-latest="loadConnection"
        @refresh-status="handleCheckRotationStatus"
      />

      <template v-if="connection && asyncState !== 'initial_load'">
        <div class="flex flex-wrap items-center justify-between gap-2 rounded-oh-md border border-oh-default bg-oh-subtle/50 px-3 py-2.5">
          <div class="space-y-0.5">
            <div class="flex items-center gap-2">
              <span class="text-sm font-semibold text-oh-fg">{{ providerMeta?.label }}</span>
              <Badge tone="neutral" variant="outline">{{ getAuthModeLabel(connection.auth_type, isZh) }}</Badge>
            </div>
            <div class="font-mono text-xs tabular-nums text-oh-muted">
              #{{ connection.id }} · {{ isZh ? `配置修订 #${connection.version}` : `Revision #${connection.version}` }}
            </div>
          </div>
          <Button
            size="xs"
            variant="ghost"
            @click="emit('openWizard', connection.id)"
          >
            {{ isZh ? '打开完整配置向导' : 'Open Full Wizard' }}
          </Button>
        </div>

        <div class="space-y-3">
          <Input
            v-model="nameInput"
            :label="isZh ? '连接名称' : 'Connection Name'"
            :disabled="!canUpdate || saving"
          />
          <Input
            v-model="endpointInput"
            :label="isZh ? '上游服务地址' : 'Upstream Endpoint'"
            mono
            :placeholder="isZh ? '留空使用官方默认地址' : 'Leave blank for default endpoint'"
            :disabled="!canUpdate || saving"
          />
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Input
              v-model="concurrencyInput"
              :label="isZh ? '并发上限' : 'Concurrency Cap'"
              type="number"
              mono
              :disabled="!canUpdate || saving"
            />
            <Select
              v-model="selectedPoolId"
              :label="isZh ? '所属出站资源池' : 'Primary Pool'"
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
              {{ isZh ? '保存修改' : 'Save Changes' }}
            </Button>
          </div>
        </div>

        <div class="border-t border-oh-default pt-4 space-y-3">
          <div class="flex items-center justify-between">
            <div>
              <h3 class="text-sm font-semibold text-oh-fg">
                {{ isZh ? '平滑轮换认证凭据' : 'Credential Rotation' }}
              </h3>
              <p class="text-xs text-oh-muted">
                {{ isZh ? '已保存密钥不会回显。当前密钥尾号：' : 'Stored secret is never echoed. Current suffix: ' }}
                <span class="font-mono tabular-nums">
                  {{ connection.credential?.last_four ? `••••${connection.credential.last_four}` : '—' }}
                </span>
              </p>
            </div>
            <Badge
              :tone="connection.credential?.state === 'verified' ? 'success' : 'warning'"
              variant="subtle"
            >
              {{ formatCredentialState(connection.credential?.state) }}
            </Badge>
          </div>

          <Input
            v-model="rotationSecret"
            type="password"
            :label="isZh ? '新密钥凭据' : 'New Secret'"
            :placeholder="isZh ? '粘贴用于替换的 API Key 或访问令牌' : 'Enter replacement API key or token'"
            autocomplete="new-password"
            :disabled="!canRotate || rotating"
          />

          <div
            v-if="rotationOperation"
            class="rounded-oh-md border border-oh-default bg-oh-surface px-3 py-2 text-xs space-y-1"
          >
            <div class="flex items-center justify-between">
              <span class="font-medium text-oh-fg">
                {{ isZh ? '凭据轮换状态' : 'Rotation Status' }}
              </span>
              <Badge tone="accent" variant="subtle">
                {{ formatOperationState(rotationOperation.state) }}
              </Badge>
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
              {{ isZh ? '验证并轮换凭据' : 'Rotate Credential' }}
            </Button>
          </div>
        </div>

        <div class="border-t border-oh-default pt-4 space-y-2.5">
          <div class="flex items-center justify-between">
            <h3 class="text-sm font-semibold text-oh-fg">
              {{ isZh ? '连接变更与探测事件' : 'Connection Events' }}
            </h3>
            <Button
              size="xs"
              variant="ghost"
              :loading="eventsLoading"
              @click="loadConnectionEvents(connection.id)"
            >
              {{ isZh ? '刷新' : 'Refresh' }}
            </Button>
          </div>
          <p v-if="events.length === 0 && !eventsLoading" class="text-xs text-oh-muted">
            {{ isZh ? '暂无变更或探测事件记录。' : 'No connection events recorded yet.' }}
          </p>
          <div
            v-else
            class="max-h-44 overflow-y-auto divide-y divide-oh-default rounded-oh-md border border-oh-default text-xs"
          >
            <div
              v-for="ev in events"
              :key="ev.id"
              class="flex items-center justify-between gap-2 px-3 py-2"
            >
              <div class="min-w-0">
                <div class="font-medium text-oh-fg truncate">{{ formatEventType(ev.type) }}</div>
                <div class="text-[11px] text-oh-muted">
                  {{ new Date(ev.at).toLocaleString() }}
                </div>
              </div>
              <Badge tone="neutral" variant="outline" mono>
                #{{ ev.version }}
              </Badge>
            </div>
          </div>
        </div>
      </template>
    </div>
  </Drawer>
</template>
