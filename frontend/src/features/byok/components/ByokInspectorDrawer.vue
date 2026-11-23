<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import CopyButton from '@/components/ui/CopyButton.vue'
import Drawer from '@/components/ui/Drawer.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import Input from '@/components/ui/Input.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import Tabs from '@/components/ui/Tabs.vue'
import { pushUiToast } from '@/components/ui/useToast'
import ConsoleAsyncBanner from '@/features/console/components/ConsoleAsyncBanner.vue'
import { classifyConsoleError, type ConsoleAsyncNotice } from '@/features/console/asyncState'
import {
  byokApi,
  type ByokCredential,
  type ByokEstimate,
  type ByokEvent,
  type ByokRevokeMode
} from '../api'
import {
  byokAuthTypeLabel,
  byokEventLabel,
  byokStateLabel,
  byokStateTone,
  formatByokTime,
  formatMoneyAmount
} from '../format'

const props = defineProps<{
  open: boolean
  scopeLevel: 'workspace' | 'project'
  workspaceId: string
  projectId: string
  actorUserId?: string
  credential: ByokCredential | null
}>()

const emit = defineEmits<{
  'update:open': [open: boolean]
  'credential-updated': [credential: ByokCredential]
  'credential-removed': [id: string]
}>()

const { locale } = useI18n()
const isZh = computed(() => String(locale?.value ?? '').startsWith('zh'))

const activeTab = ref<'estimate' | 'events' | 'lifecycle'>('estimate')
const localCredential = ref<ByokCredential | null>(null)
const refreshingCred = ref(false)
const actionNotice = ref<ConsoleAsyncNotice | null>(null)

// Estimate form & result
const estProjectId = ref('')
const estMemberId = ref('')
const estKeyId = ref('')
const estModelId = ref('')
const estInputTokens = ref('10000')
const estOutputTokens = ref('2000')
const estLoading = ref(false)
const estResult = ref<ByokEstimate | null>(null)

// Events state
const events = ref<ByokEvent[]>([])
const eventsCursor = ref<string | null>(null)
const eventsAsOf = ref('')
const eventsLoading = ref(false)
const eventsLoadingMore = ref(false)

// Lifecycle actions state
const replacementSecret = ref('')
const revokeMode = ref<ByokRevokeMode>('drain')
const lifecycleBusy = ref(false)

const drawerTabs = computed(() => [
  { value: 'estimate', label: isZh.value ? '成本与资金估算' : 'Cost Estimate' },
  { value: 'events', label: isZh.value ? '审计事件流' : 'Audit Events' },
  { value: 'lifecycle', label: isZh.value ? '轮换与吊销' : 'Rotate & Revoke' }
])

function seedEstimateDefaults(cred: ByokCredential): void {
  estProjectId.value = props.projectId || cred.scope.project_ids[0] || ''
  estMemberId.value = cred.scope.member_ids[0] || props.actorUserId || ''
  estKeyId.value = cred.scope.key_ids[0] || ''
  estModelId.value = cred.verified_model_ids[0] || cred.scope.model_ids[0] || ''
  estResult.value = null
}

async function syncLatestCredential(): Promise<void> {
  const targetId = localCredential.value?.id || props.credential?.id
  if (!targetId || !props.workspaceId) return
  refreshingCred.value = true
  actionNotice.value = null
  try {
    const fresh =
      props.scopeLevel === 'project' && props.projectId
        ? await byokApi.getProjectCredential(props.workspaceId, props.projectId, targetId)
        : await byokApi.getWorkspaceCredential(props.workspaceId, targetId)
    localCredential.value = fresh
    emit('credential-updated', fresh)
  } catch (err) {
    actionNotice.value = classifyConsoleError(err, {
      isZh: isZh.value,
      requiredGrantFallback: 'credentials.byok.manage'
    })
  } finally {
    refreshingCred.value = false
  }
}

async function loadEvents(reset = true): Promise<void> {
  const cred = localCredential.value
  if (!cred || !props.workspaceId) return
  if (!reset && (!eventsCursor.value || eventsLoadingMore.value)) return
  if (reset) {
    eventsLoading.value = true
  } else {
    eventsLoadingMore.value = true
  }
  try {
    const page =
      props.scopeLevel === 'project' && props.projectId
        ? await byokApi.listProjectEvents(props.workspaceId, props.projectId, cred.id, {
            cursor: reset ? undefined : (eventsCursor.value ?? undefined),
            limit: 25
          })
        : await byokApi.listWorkspaceEvents(props.workspaceId, cred.id, {
            cursor: reset ? undefined : (eventsCursor.value ?? undefined),
            limit: 25
          })
    events.value = reset ? page.items ?? [] : [...events.value, ...(page.items ?? [])]
    eventsCursor.value = page.next_cursor ?? null
    eventsAsOf.value = page.as_of || ''
  } catch (err) {
    actionNotice.value = classifyConsoleError(err, {
      isZh: isZh.value,
      requiredGrantFallback: 'credentials.byok.manage'
    })
  } finally {
    eventsLoading.value = false
    eventsLoadingMore.value = false
  }
}

async function runEstimate(): Promise<void> {
  const cred = localCredential.value
  if (!cred || estLoading.value) return
  actionNotice.value = null
  estLoading.value = true
  try {
    if (props.scopeLevel === 'project' && props.projectId) {
      estResult.value = await byokApi.estimateProjectCredential(
        props.workspaceId,
        props.projectId,
        cred.id,
        {
          member_id: estMemberId.value.trim(),
          key_id: estKeyId.value.trim(),
          model_id: estModelId.value.trim(),
          input_token_bound: String(estInputTokens.value).trim(),
          output_token_bound: String(estOutputTokens.value).trim()
        }
      )
    } else {
      estResult.value = await byokApi.estimateWorkspaceCredential(props.workspaceId, cred.id, {
        project_id: estProjectId.value.trim(),
        member_id: estMemberId.value.trim(),
        key_id: estKeyId.value.trim(),
        model_id: estModelId.value.trim(),
        input_token_bound: String(estInputTokens.value).trim(),
        output_token_bound: String(estOutputTokens.value).trim()
      })
    }
  } catch (err) {
    actionNotice.value = classifyConsoleError(err, {
      isZh: isZh.value,
      requiredGrantFallback: 'credentials.byok.manage'
    })
  } finally {
    estLoading.value = false
  }
}

async function handleRotate(): Promise<void> {
  const cred = localCredential.value
  if (!cred || !replacementSecret.value.trim() || lifecycleBusy.value) return
  lifecycleBusy.value = true
  actionNotice.value = null
  try {
    const op =
      props.scopeLevel === 'project' && props.projectId
        ? await byokApi.rotateProjectCredential(
            props.workspaceId,
            props.projectId,
            cred.id,
            cred.version,
            replacementSecret.value.trim()
          )
        : await byokApi.rotateWorkspaceCredential(
            props.workspaceId,
            cred.id,
            cred.version,
            replacementSecret.value.trim()
          )
    replacementSecret.value = ''
    pushUiToast({
      tone: op.state === 'failed' ? 'warning' : 'success',
      title: isZh.value ? '密钥轮换请求已提交' : 'Credential Rotation Submitted',
      description: isZh.value ? `执行状态：${op.state}` : `Operation state: ${op.state}`
    })
    await syncLatestCredential()
    void loadEvents(true)
  } catch (err) {
    actionNotice.value = classifyConsoleError(err, {
      isZh: isZh.value,
      requiredGrantFallback: 'credentials.byok.manage'
    })
  } finally {
    lifecycleBusy.value = false
  }
}

async function handleRevoke(): Promise<void> {
  const cred = localCredential.value
  if (!cred || lifecycleBusy.value) return
  lifecycleBusy.value = true
  actionNotice.value = null
  try {
    const op =
      props.scopeLevel === 'project' && props.projectId
        ? await byokApi.revokeProjectCredential(
            props.workspaceId,
            props.projectId,
            cred.id,
            cred.version,
            revokeMode.value
          )
        : await byokApi.revokeWorkspaceCredential(
            props.workspaceId,
            cred.id,
            cred.version,
            revokeMode.value
          )
    pushUiToast({
      tone: 'info',
      title: isZh.value ? '密钥已执行吊销' : 'Credential Revocation Accepted',
      description: isZh.value ? `模式：${revokeMode.value} · 状态：${op.state}` : `Mode: ${revokeMode.value} · State: ${op.state}`
    })
    await syncLatestCredential()
    void loadEvents(true)
  } catch (err) {
    actionNotice.value = classifyConsoleError(err, {
      isZh: isZh.value,
      requiredGrantFallback: 'credentials.byok.manage'
    })
  } finally {
    lifecycleBusy.value = false
  }
}

async function handleDelete(): Promise<void> {
  const cred = localCredential.value
  if (!cred || lifecycleBusy.value) return
  const confirmed =
    typeof window === 'undefined' ||
    window.confirm(
      isZh.value
        ? `确认彻底移除凭据 #${cred.id} (${cred.provider})？该操作不可恢复。`
        : `Permanently delete credential #${cred.id} (${cred.provider})? This cannot be undone.`
    )
  if (!confirmed) return
  lifecycleBusy.value = true
  actionNotice.value = null
  try {
    if (props.scopeLevel === 'project' && props.projectId) {
      await byokApi.deleteProjectCredential(props.workspaceId, props.projectId, cred.id, cred.version)
    } else {
      await byokApi.deleteWorkspaceCredential(props.workspaceId, cred.id, cred.version)
    }
    pushUiToast({
      tone: 'success',
      title: isZh.value ? '自带上游凭据已移除' : 'BYOK Credential Deleted'
    })
    emit('credential-removed', cred.id)
    emit('update:open', false)
  } catch (err) {
    actionNotice.value = classifyConsoleError(err, {
      isZh: isZh.value,
      requiredGrantFallback: 'credentials.byok.manage'
    })
  } finally {
    lifecycleBusy.value = false
  }
}

watch(
  () => [props.open, props.credential?.id],
  ([isOpen]) => {
    if (isOpen && props.credential) {
      localCredential.value = props.credential
      actionNotice.value = null
      activeTab.value = 'estimate'
      seedEstimateDefaults(props.credential)
      void syncLatestCredential()
      void loadEvents(true)
    }
  },
  { immediate: true }
)
</script>

<template>
  <Drawer
    :open="open"
    size="lg"
    :title="
      localCredential
        ? `${localCredential.provider} · #${localCredential.id}`
        : (isZh ? '凭据详情与审计' : 'Credential Inspector')
    "
    :description="
      localCredential
        ? `${byokAuthTypeLabel(localCredential.auth_type, isZh)} · ${localCredential.fingerprint}`
        : undefined
    "
    @update:open="emit('update:open', $event)"
  >
    <div v-if="localCredential" class="space-y-4 text-sm">
      <div class="flex flex-wrap items-center justify-between gap-2 rounded-oh-md border border-oh-default bg-oh-subtle/50 p-3">
        <div class="flex flex-wrap items-center gap-2">
          <Badge :tone="byokStateTone(localCredential.state)" variant="subtle" dot>
            {{ byokStateLabel(localCredential.state, isZh) }}
          </Badge>
          <span class="font-mono text-xs text-oh-secondary">
            {{ isZh ? '指纹' : 'Fingerprint' }}: {{ localCredential.fingerprint }}
          </span>
          <CopyButton :value="localCredential.fingerprint" size="xs" :aria-label="isZh ? '复制密钥指纹' : 'Copy fingerprint'" />
        </div>
        <Button variant="ghost" size="xs" :loading="refreshingCred" @click="syncLatestCredential">
          {{ isZh ? '同步最新状态' : 'Refresh State' }}
        </Button>
      </div>

      <ConsoleAsyncBanner
        v-if="actionNotice"
        :notice="actionNotice"
        :retry-label="isZh ? '同步最新状态' : 'Reload Latest'"
        dismissible
        @retry="syncLatestCredential"
        @dismiss="actionNotice = null"
      />

      <Tabs
        :model-value="activeTab"
        :items="drawerTabs"
        :aria-label="isZh ? '凭据管理视图' : 'Credential inspector tabs'"
        @update:model-value="activeTab = $event as 'estimate' | 'events' | 'lifecycle'"
      />

      <section v-if="activeTab === 'estimate'" class="space-y-4">
        <p class="text-xs text-oh-secondary">
          {{
            isZh
              ? '输入调用上下文与上下行 Token 上限，实时试算该 BYOK 凭据的外部参考费用与平台服务费。'
              : 'Simulate external token spend and platform service fees for a given caller principal and token bound.'
          }}
        </p>

        <form class="grid gap-3 sm:grid-cols-2" @submit.prevent="runEstimate">
          <Input
            v-if="scopeLevel === 'workspace'"
            v-model="estProjectId"
            mono
            size="sm"
            required
            :label="isZh ? '项目 ID (Project ID)' : 'Project ID'"
          />
          <Input
            v-model="estMemberId"
            mono
            size="sm"
            required
            :label="isZh ? '调用成员 ID (Member ID)' : 'Caller Member ID'"
          />
          <Input
            v-model="estKeyId"
            mono
            size="sm"
            required
            :label="isZh ? '绑定 API Key ID' : 'API Key ID'"
          />
          <Input
            v-model="estModelId"
            mono
            size="sm"
            required
            :label="isZh ? '目标模型 ID' : 'Target Model ID'"
          />
          <Input
            v-model="estInputTokens"
            type="number"
            mono
            size="sm"
            required
            :label="isZh ? '输入 Token 上限' : 'Input Token Bound'"
          />
          <Input
            v-model="estOutputTokens"
            type="number"
            mono
            size="sm"
            required
            :label="isZh ? '输出 Token 上限' : 'Output Token Bound'"
          />
          <div class="sm:col-span-2">
            <Button type="submit" size="sm" :loading="estLoading">
              {{ isZh ? '运行资金与成本试算' : 'Calculate Estimate' }}
            </Button>
          </div>
        </form>

        <div
          v-if="estResult"
          data-testid="byok-estimate-result"
          class="space-y-2.5 rounded-oh-md border border-oh-default bg-oh-surface p-4"
        >
          <div class="flex flex-wrap items-center justify-between gap-2 border-b border-oh-subtle pb-2">
            <span class="font-mono text-xs font-semibold uppercase text-oh-secondary">
              {{ isZh ? '试算结果' : 'Funding Estimate' }}
            </span>
            <Badge :tone="estResult.external_state === 'estimated' ? 'success' : 'unknown'" size="xs" variant="subtle">
              {{
                estResult.external_state === 'estimated'
                  ? (isZh ? '已命中费率' : 'Estimated')
                  : (isZh ? '外部费率未知' : 'External Rate Unknown')
              }}
            </Badge>
          </div>
          <dl class="grid grid-cols-2 gap-3 font-mono text-xs tabular-nums">
            <div>
              <dt class="text-[11px] text-oh-muted">{{ isZh ? '资金来源' : 'Funding Source' }}</dt>
              <dd class="mt-0.5 font-semibold uppercase text-oh-primary">{{ estResult.funding_source }}</dd>
            </div>
            <div>
              <dt class="text-[11px] text-oh-muted">{{ isZh ? '平台调度服务费' : 'Platform Service Fee' }}</dt>
              <dd class="mt-0.5 font-semibold text-oh-primary">{{ formatMoneyAmount(estResult.platform_service_fee) }}</dd>
            </div>
            <div>
              <dt class="text-[11px] text-oh-muted">{{ isZh ? '上游外部估算消耗' : 'External BYOK Spend' }}</dt>
              <dd class="mt-0.5 font-semibold text-oh-primary">{{ formatMoneyAmount(estResult.byok_external_estimate) }}</dd>
            </div>
            <div>
              <dt class="text-[11px] text-oh-muted">{{ isZh ? '计入项目预算控制' : 'Included in Budget' }}</dt>
              <dd class="mt-0.5 text-oh-secondary">
                {{ estResult.include_external_in_budget ? (isZh ? '是' : 'Yes') : (isZh ? '否' : 'No') }}
              </dd>
            </div>
          </dl>
          <p v-if="estResult.price_version" class="font-mono text-[11px] text-oh-muted">
            {{ isZh ? `费率快照版本：${estResult.price_version}` : `Price revision: ${estResult.price_version}` }}
          </p>
        </div>
      </section>

      <section v-else-if="activeTab === 'events'" class="space-y-3">
        <div class="flex items-center justify-between">
          <span class="text-xs text-oh-secondary">
            {{ isZh ? '记录凭据创建、探测、轮换、策略变更与吊销全生命周期审计轨迹' : 'Chronological lifecycle and policy audit trail' }}
          </span>
          <Button variant="ghost" size="xs" :loading="eventsLoading" @click="loadEvents(true)">
            {{ isZh ? '刷新日志' : 'Refresh' }}
          </Button>
        </div>

        <Skeleton v-if="eventsLoading" :lines="4" />
        <EmptyState
          v-else-if="events.length === 0"
          compact
          :title="isZh ? '暂无生命周期审计事件' : 'No audit events recorded yet'"
        />
        <div v-else class="divide-y divide-oh-subtle rounded-oh-md border border-oh-default bg-oh-surface">
          <div
            v-for="ev in events"
            :key="ev.id"
            class="flex flex-wrap items-center justify-between gap-2 px-3.5 py-2.5 text-xs"
          >
            <div class="space-y-0.5">
              <div class="flex items-center gap-2">
                <Badge
                  :tone="ev.type.includes('failed') || ev.type.includes('revoke') ? 'warning' : 'info'"
                  size="xs"
                  variant="subtle"
                  mono
                >
                  {{ ev.type }}
                </Badge>
                <span class="font-medium text-oh-primary">{{ byokEventLabel(ev.type, isZh) }}</span>
              </div>
              <p class="font-mono text-[11px] text-oh-muted">
                {{ isZh ? '操作主体' : 'Actor' }}: {{ ev.actor_id ? `#${ev.actor_id}` : 'system' }}
              </p>
            </div>
            <span class="font-mono text-[11px] tabular-nums text-oh-muted">
              {{ formatByokTime(ev.at) }}
            </span>
          </div>
        </div>

        <div v-if="eventsCursor" class="flex justify-center pt-1">
          <Button variant="secondary" size="xs" :loading="eventsLoadingMore" @click="loadEvents(false)">
            {{ isZh ? '加载更早事件' : 'Load older events' }}
          </Button>
        </div>
      </section>

      <section v-else class="space-y-5">
        <div class="space-y-3 rounded-oh-md border border-oh-default bg-oh-surface p-4">
          <h3 class="text-xs font-semibold uppercase tracking-wider text-oh-primary">
            {{ isZh ? '平滑轮换上游密钥 (Rotate Secret)' : 'Rotate Upstream Secret' }}
          </h3>
          <p class="text-xs text-oh-secondary">
            {{
              isZh
                ? '提交新密钥后网关会自动执行连通性验证并切换版本，旧密钥明文立即销毁。'
                : 'Validates and switches to the replacement upstream secret without changing credential bindings.'
            }}
          </p>
          <Input
            v-model="replacementSecret"
            type="password"
            revealable
            mono
            size="sm"
            autocomplete="off"
            :placeholder="isZh ? '输入新的上游 API Key 或凭据' : 'Enter replacement upstream secret'"
          />
          <Button
            size="sm"
            :disabled="!replacementSecret.trim() || localCredential.state === 'revoked'"
            :loading="lifecycleBusy"
            @click="handleRotate"
          >
            {{ isZh ? '执行密钥轮换' : 'Rotate Secret' }}
          </Button>
        </div>

        <div class="space-y-3 rounded-oh-md border border-oh-default bg-oh-surface p-4">
          <h3 class="text-xs font-semibold uppercase tracking-wider text-oh-primary">
            {{ isZh ? '吊销凭据 (Revoke Credential)' : 'Revoke Credential' }}
          </h3>
          <div class="flex flex-wrap items-center gap-2">
            <label class="text-xs text-oh-secondary" for="byok-revoke-mode">
              {{ isZh ? '吊销模式：' : 'Revocation mode:' }}
            </label>
            <select
              id="byok-revoke-mode"
              v-model="revokeMode"
              class="oh-focus-ring h-8 rounded-oh-sm border border-oh-default bg-oh-surface px-2.5 font-mono text-xs text-oh-primary"
            >
              <option value="drain">{{ isZh ? 'drain · 平滑排空在途请求后停用' : 'drain · Graceful in-flight drain' }}</option>
              <option value="force">{{ isZh ? 'force · 立即强制阻断全部调度' : 'force · Immediate cut-off' }}</option>
            </select>
            <Button
              variant="secondary"
              size="sm"
              :disabled="localCredential.state === 'revoked'"
              :loading="lifecycleBusy"
              @click="handleRevoke"
            >
              {{ isZh ? '吊销该密钥' : 'Revoke' }}
            </Button>
          </div>
        </div>

        <div class="space-y-2.5 rounded-oh-md border border-oh-status-danger-border bg-oh-status-danger-bg p-4">
          <h3 class="text-xs font-semibold uppercase tracking-wider text-oh-primary">
            {{ isZh ? '永久移除凭据记录' : 'Delete Credential Record' }}
          </h3>
          <p class="text-xs text-oh-secondary">
            {{
              isZh
                ? '强制吊销并从当前工作空间或项目列表中移除此自带上游凭据配置。'
                : 'Force-revokes and removes this BYOK credential from active routing.'
            }}
          </p>
          <Button variant="danger" size="sm" :loading="lifecycleBusy" @click="handleDelete">
            {{ isZh ? '彻底删除凭据' : 'Delete Credential' }}
          </Button>
        </div>
      </section>
    </div>
  </Drawer>
</template>
