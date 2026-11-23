<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { apiClient } from '@/api/client'
import { extractApiErrorCode, extractApiErrorStatus, normalizeApiError } from '@/utils/apiError'
import {
  formatRatePairDisplay,
  type AdminCatalogModelSummary
} from '@/api/admin/modelCatalog'

export type AdminBatchOperationKind =
  | 'enable_discovered'
  | 'draft_channel_pricing'
  | 'toggle_publication'

export interface AdminBatchSubmitPayload {
  operation: AdminBatchOperationKind
  modelIds: string[]
  models: string[]
  casRevision: string
  ifMatch: string
  targetAccountId: string
  targetChannelId: string
  targetGroupId: string
  targetPublicationState: 'published' | 'unpublished'
}

const props = withDefaults(
  defineProps<{
    open: boolean
    models: AdminCatalogModelSummary[]
    initialOperation?: AdminBatchOperationKind
    catalogVersion?: string
    defaultAccountId?: string
    defaultRulesRevision?: string
    isZh?: boolean
    submitHandler?: (payload: AdminBatchSubmitPayload) => Promise<void>
  }>(),
  {
    initialOperation: 'draft_channel_pricing',
    catalogVersion: '1',
    defaultAccountId: '1',
    defaultRulesRevision: '1',
    isZh: true
  }
)

const emit = defineEmits<{
  close: []
  submitted: [payload: AdminBatchSubmitPayload]
  'refresh-snapshot': []
}>()

const operation = ref<AdminBatchOperationKind>(props.initialOperation)
const targetAccountId = ref(props.defaultAccountId || '1')
const targetChannelId = ref('1')
const targetGroupId = ref('1')
const targetPublicationState = ref<'published' | 'unpublished'>('published')
const casRevision = ref(props.defaultRulesRevision || props.catalogVersion || '1')
const confirmedPreview = ref(false)
const submitting = ref(false)
const casConflict = ref(false)
const errorMessage = ref('')
const successMessage = ref('')

watch(
  () => [props.open, props.initialOperation, props.defaultRulesRevision, props.catalogVersion] as const,
  ([isOpen, initOp, rulesRev, catVer]) => {
    if (isOpen) {
      operation.value = initOp
      casRevision.value = rulesRev || catVer || '1'
      confirmedPreview.value = false
      casConflict.value = false
      errorMessage.value = ''
      successMessage.value = ''
    }
  },
  { immediate: true }
)

const diffRows = computed(() =>
  props.models.map((item) => {
    const refRate = item.billing.reference_price
      ? formatRatePairDisplay(
          item.billing.reference_price.input_rate,
          item.billing.reference_price.output_rate
        )
      : '—'
    const saleRate = item.billing.sale_price
      ? formatRatePairDisplay(
          item.billing.sale_price.input_rate,
          item.billing.sale_price.output_rate
        )
      : '—'

    if (operation.value === 'enable_discovered') {
      return {
        id: item.id,
        label: `${item.provider}/${item.model}`,
        before: props.isZh
          ? `2. 账号原生发现 (${item.discovered_accounts_count ?? 1} 连接) · 有效连接: ${item.accounts_count ?? 0}`
          : `2. Native Discovered (${item.discovered_accounts_count ?? 1}) · Effective: ${item.accounts_count ?? 0}`,
        after: props.isZh
          ? `3. 账号有效模型 -> 加入连接 #${targetAccountId.value} (${item.model})`
          : `3. Account Effective -> Add to Connection #${targetAccountId.value} (${item.model})`
      }
    }
    if (operation.value === 'draft_channel_pricing') {
      return {
        id: item.id,
        label: `${item.provider}/${item.model}`,
        before: props.isZh
          ? `2. 销售价: ${saleRate} (${item.billing.state}) · 1. 参考价（非结算价）: ${refRate}`
          : `2. Sale Rate: ${saleRate} (${item.billing.state}) · 1. Reference Price: ${refRate}`,
        after:
          refRate !== '—'
            ? props.isZh
              ? `渠道 #${targetChannelId.value} 销售价草稿: ${refRate} / 1M tokens（需保存后生效，不自动公开）`
              : `Channel #${targetChannelId.value} Sale Rate draft: ${refRate} / 1M tokens (requires save, no auto-publish)`
            : props.isZh
              ? '无可用参考价（保持未定价，需手动填写销售价）'
              : 'No Reference Price available (stays unpriced; enter Sale Rate manually)'
      }
    }
    return {
      id: item.id,
      label: `${item.provider}/${item.model}`,
      before: props.isZh
        ? `4. 分组对外可见: ${item.availability.publication}`
        : `4. Group Visible: ${item.availability.publication}`,
      after: props.isZh
        ? `分组 #${targetGroupId.value} / 渠道 #${targetChannelId.value} -> ${targetPublicationState.value === 'published' ? '分组对外可见 (published)' : '隐藏 (unpublished)'}`
        : `Group #${targetGroupId.value} / Channel #${targetChannelId.value} -> ${targetPublicationState.value}`
    }
  })
)

const formattedIfMatch = computed(() => {
  const raw = casRevision.value.trim() || '1'
  return raw.startsWith('"') && raw.endsWith('"') ? raw : `"${raw}"`
})

const canSubmit = computed(
  () =>
    props.models.length > 0 &&
    confirmedPreview.value &&
    casRevision.value.trim().length > 0 &&
    !submitting.value &&
    !casConflict.value
)

async function handleSubmit() {
  if (!canSubmit.value) return
  submitting.value = true
  errorMessage.value = ''
  successMessage.value = ''

  const payload: AdminBatchSubmitPayload = {
    operation: operation.value,
    modelIds: props.models.map((m) => m.id),
    models: props.models.map((m) => m.model),
    casRevision: casRevision.value.trim(),
    ifMatch: formattedIfMatch.value,
    targetAccountId: targetAccountId.value.trim() || '1',
    targetChannelId: targetChannelId.value.trim() || '1',
    targetGroupId: targetGroupId.value.trim() || '1',
    targetPublicationState: targetPublicationState.value
  }

  try {
    if (props.submitHandler) {
      await props.submitHandler(payload)
    } else if (operation.value === 'enable_discovered') {
      const mapping = Object.fromEntries(payload.models.map((m) => [m, m]))
      await apiClient.put(
        `/admin/accounts/${encodeURIComponent(payload.targetAccountId)}/models/allowlist`,
        {
          enabled: false,
          models: [],
          reason: '',
          expected_rules_revision: payload.casRevision,
          model_mapping: mapping
        },
        {
          headers: {
            'If-Match': payload.ifMatch
          }
        }
      )
    } else if (operation.value === 'toggle_publication') {
      const selected =
        payload.targetPublicationState === 'published'
          ? props.models.map((m) => ({ platform: m.provider, name: m.model }))
          : []
      await apiClient.put(
        `/admin/channels/${encodeURIComponent(payload.targetChannelId)}/groups/${encodeURIComponent(payload.targetGroupId)}/plaza-candidates`,
        {
          revision: payload.casRevision,
          models: selected
        },
        {
          headers: {
            'If-Match': payload.ifMatch
          }
        }
      )
    } else {
      await apiClient.post(
        `/admin/channels/${encodeURIComponent(payload.targetChannelId)}/pricing/draft`,
        {
          models: payload.models,
          expected_revision: payload.casRevision
        },
        {
          headers: {
            'If-Match': payload.ifMatch
          }
        }
      )
    }

    successMessage.value = props.isZh
      ? '批量变更已通过 CAS 校验并保存。'
      : 'Batch mutation verified with CAS and committed.'
    emit('submitted', payload)
  } catch (cause) {
    const status = extractApiErrorStatus(cause)
    const code = extractApiErrorCode(cause)
    const normalized = normalizeApiError(cause)
    if (
      status === 409 ||
      status === 412 ||
      status === 410 ||
      code === 'PLAZA_CONFIRMATION_CONFLICT' ||
      code === 'RULES_REVISION_CONFLICT' ||
      normalized.reason === 'SNAPSHOT_EXPIRED'
    ) {
      casConflict.value = true
      errorMessage.value = props.isZh
        ? '检测到并发版本冲突（409 / 412 CAS Conflict）：目标配置已被更新，当前草稿已保留。请点击「刷新快照重新预览」后再提交。'
        : 'Concurrent version conflict detected (409 / 412 CAS Conflict): target configuration changed. Draft preserved; click Refresh Snapshot & Re-preview.'
    } else {
      errorMessage.value =
        normalized.message ||
        (props.isZh ? '批量提交失败，请检查配置后重试。' : 'Batch submission failed.')
    }
  } finally {
    submitting.value = false
  }
}

function handleRefreshAfterConflict() {
  casConflict.value = false
  errorMessage.value = ''
  emit('refresh-snapshot')
}
</script>

<template>
  <div
    v-if="open"
    data-testid="admin-model-batch-preview-modal"
    role="dialog"
    aria-modal="true"
    :aria-label="isZh ? '批量变更预览与 CAS 确认' : 'Batch Preview & CAS Confirmation'"
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-xs"
    @click.self="emit('close')"
  >
    <div
      class="flex max-h-[90vh] w-full max-w-3xl flex-col overflow-hidden rounded-2xl border border-[var(--border)] bg-[var(--card)] text-[var(--foreground)] shadow-2xl"
    >
      <header
        class="flex items-center justify-between border-b border-[var(--border)] px-5 py-4"
      >
        <div>
          <h2 class="text-base font-bold">
            {{ isZh ? '批量变更预览与 CAS 确认' : 'Batch Preview & CAS Confirmation' }}
          </h2>
          <p class="mt-0.5 text-xs text-[var(--muted-foreground)]">
            {{
              isZh
                ? '所有批量写操作必须先核对变更前后差异（Preview），再携带版本令牌（If-Match / rules_revision）显式提交。'
                : 'Every bulk mutation requires an explicit Before -> After diff preview and CAS token (If-Match / rules_revision).'
            }}
          </p>
        </div>
        <button
          type="button"
          data-testid="batch-modal-close"
          class="btn btn-secondary btn-sm text-xs"
          @click="emit('close')"
        >
          {{ isZh ? '关闭' : 'Close' }}
        </button>
      </header>

      <div class="flex-1 overflow-y-auto p-5 space-y-4 text-xs">
        <!-- 409 / 412 CAS Conflict Recovery Banner -->
        <div
          v-if="casConflict"
          role="alert"
          data-testid="batch-cas-conflict-banner"
          class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-amber-500/50 bg-amber-500/15 p-3.5 text-amber-800 dark:text-amber-200"
        >
          <div class="space-y-1">
            <p class="font-semibold">
              {{
                isZh
                  ? '检测到并发版本冲突（409 / 412 CAS Conflict）'
                  : 'Concurrent Version Conflict Detected (409 / 412 CAS Conflict)'
              }}
            </p>
            <p>{{ errorMessage }}</p>
          </div>
          <button
            type="button"
            data-testid="batch-cas-refresh-button"
            class="btn btn-secondary btn-sm text-xs font-semibold"
            @click="handleRefreshAfterConflict"
          >
            {{ isZh ? '刷新快照重新预览' : 'Refresh Snapshot & Re-preview' }}
          </button>
        </div>

        <div
          v-else-if="errorMessage"
          role="alert"
          data-testid="batch-error-banner"
          class="rounded-xl border border-red-500/40 bg-red-500/10 p-3 text-red-700 dark:text-red-300"
        >
          {{ errorMessage }}
        </div>

        <div
          v-if="successMessage"
          role="status"
          data-testid="batch-success-banner"
          class="rounded-xl border border-emerald-500/40 bg-emerald-500/10 p-3 text-emerald-700 dark:text-emerald-300"
        >
          {{ successMessage }}
        </div>

        <!-- Operation & Target Selectors -->
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <label class="space-y-1">
            <span class="font-semibold">{{ isZh ? '批量操作类型' : 'Batch Operation' }}</span>
            <select
              v-model="operation"
              data-testid="batch-operation-select"
              class="input w-full text-xs"
            >
              <option value="enable_discovered">
                {{
                  isZh
                    ? '加入账号有效模型（2. 账号原生发现 -> 3. 账号有效模型）'
                    : 'Add to Account Effective (2. Native Discovered -> 3. Account Effective)'
                }}
              </option>
              <option value="draft_channel_pricing">
                {{
                  isZh
                    ? '以参考价起草渠道销售价（1. 参考价（非结算价） -> 2. 销售价草稿）'
                    : 'Draft Channel Sale Rate from Reference Price (1. Reference Price -> 2. Sale Rate Draft)'
                }}
              </option>
              <option value="toggle_publication">
                {{
                  isZh
                    ? '批量更新分组对外可见（3. 账号有效模型 -> 4. 分组对外可见）'
                    : 'Bulk Update Group Visibility (3. Account Effective -> 4. Group Visible)'
                }}
              </option>
            </select>
          </label>

          <label class="space-y-1">
            <span class="font-semibold">
              {{ isZh ? 'CAS 版本令牌 (If-Match / rules_revision)' : 'CAS Revision Token (If-Match / rules_revision)' }}
            </span>
            <input
              v-model="casRevision"
              type="text"
              data-testid="batch-cas-revision-input"
              class="input w-full font-mono text-xs"
            />
          </label>
        </div>

        <div v-if="operation === 'enable_discovered'" class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <label class="space-y-1">
            <span class="font-semibold">{{ isZh ? '目标上游连接 ID' : 'Target Connection ID' }}</span>
            <input
              v-model="targetAccountId"
              type="text"
              data-testid="batch-target-account-input"
              class="input w-full font-mono text-xs"
            />
          </label>
        </div>

        <div v-else class="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <label class="space-y-1">
            <span class="font-semibold">{{ isZh ? '目标渠道 ID' : 'Target Channel ID' }}</span>
            <input
              v-model="targetChannelId"
              type="text"
              data-testid="batch-target-channel-input"
              class="input w-full font-mono text-xs"
            />
          </label>
          <label v-if="operation === 'toggle_publication'" class="space-y-1">
            <span class="font-semibold">{{ isZh ? '目标路由分组 ID' : 'Target Group ID' }}</span>
            <input
              v-model="targetGroupId"
              type="text"
              data-testid="batch-target-group-input"
              class="input w-full font-mono text-xs"
            />
          </label>
          <label v-if="operation === 'toggle_publication'" class="space-y-1">
            <span class="font-semibold">{{ isZh ? '目标可见状态' : 'Target Visibility State' }}</span>
            <select
              v-model="targetPublicationState"
              data-testid="batch-target-publication-select"
              class="input w-full text-xs"
            >
              <option value="published">{{ isZh ? '分组对外可见 (published)' : 'Published' }}</option>
              <option value="unpublished">{{ isZh ? '隐藏 (unpublished)' : 'Unpublished' }}</option>
            </select>
          </label>
        </div>

        <!-- Before -> After Diff Table -->
        <div class="overflow-x-auto rounded-xl border border-[var(--border)]">
          <table
            data-testid="batch-preview-diff-table"
            class="w-full text-left text-xs tabular-nums"
          >
            <thead class="border-b border-[var(--border)] bg-[var(--surface-2)] text-[var(--muted-foreground)]">
              <tr>
                <th class="px-3 py-2 font-semibold">{{ isZh ? '规范模型' : 'Canonical Model' }}</th>
                <th class="px-3 py-2 font-semibold">{{ isZh ? '变更前状态 (Before)' : 'Before State' }}</th>
                <th class="px-3 py-2 font-semibold">{{ isZh ? '变更后预览 (After)' : 'After Preview' }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-[var(--border)]">
              <tr v-for="row in diffRows" :key="row.id" data-testid="batch-diff-row">
                <td class="px-3 py-2.5 font-mono font-semibold">{{ row.label }}</td>
                <td class="px-3 py-2.5 text-[var(--muted-foreground)]">{{ row.before }}</td>
                <td class="px-3 py-2.5 font-medium text-emerald-700 dark:text-emerald-300">
                  {{ row.after }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- Explicit Confirmation Gate -->
        <label
          class="flex cursor-pointer items-center gap-2 rounded-lg border border-[var(--border)] bg-[var(--surface-2)]/60 px-3.5 py-2.5"
        >
          <input
            v-model="confirmedPreview"
            type="checkbox"
            data-testid="batch-preview-confirm-checkbox"
          />
          <span class="font-medium">
            {{
              isZh
                ? `我已核对以上 ${diffRows.length} 项变更前后差异，并确认携带 If-Match: ${formattedIfMatch} 提交`
                : `I have reviewed the ${diffRows.length} Before -> After diff rows and confirm submitting with If-Match: ${formattedIfMatch}`
            }}
          </span>
        </label>
      </div>

      <footer
        class="flex items-center justify-end gap-3 border-t border-[var(--border)] bg-[var(--surface-2)]/40 px-5 py-3.5"
      >
        <button
          type="button"
          class="btn btn-secondary btn-sm text-xs"
          @click="emit('close')"
        >
          {{ isZh ? '取消' : 'Cancel' }}
        </button>
        <button
          type="button"
          data-testid="batch-preview-submit"
          class="btn btn-primary btn-sm text-xs"
          :disabled="!canSubmit"
          @click="handleSubmit"
        >
          {{
            submitting
              ? isZh
                ? '正在提交 CAS…'
                : 'Submitting CAS…'
              : isZh
                ? '确认并携带 CAS 提交'
                : 'Confirm & Submit with CAS'
          }}
        </button>
      </footer>
    </div>
  </div>
</template>
