<script setup lang="ts">
import { computed } from 'vue'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import Modal from '@/components/ui/Modal.vue'
import {
  EM_DASH,
  formatExactMoney,
  formatNullableBytes,
  formatNullableInteger
} from '@/features/console/format'
import { formatModelRatePerMillion, MAX_PLAYGROUND_BODY_BYTES } from '../costEstimate'
import type { PlaygroundCostEstimate } from '../types'

const props = withDefaults(
  defineProps<{
    open: boolean
    estimates: PlaygroundCostEstimate[]
    replayRequestId?: string | null
    submitting?: boolean
    quoteLoading?: boolean
    isZh?: boolean
    teleportDisabled?: boolean
  }>(),
  {
    replayRequestId: null,
    submitting: false,
    quoteLoading: false,
    isZh: false,
    teleportDisabled: false
  }
)

const emit = defineEmits<{
  'update:open': [value: boolean]
  confirm: []
  cancel: []
}>()

const hasOversizedPayload = computed(() => props.estimates.some((item) => item.exceedsBodyLimit))

const combinedMaxTotal = computed(() => {
  if (props.estimates.length <= 1) return null
  const known = props.estimates.filter((e) => e.maxTotalCost !== null)
  if (known.length === 0) return null
  const sum = known.reduce((acc, item) => acc + Number(item.maxTotalCost || 0), 0)
  return formatExactMoney(sum.toFixed(8), known[0]?.currency || 'USD')
})

function quoteBadgeLabel(item: PlaygroundCostEstimate): string {
  if (props.quoteLoading || item.quoteState === 'loading') {
    return props.isZh ? '服务端报价核定中…' : 'Quoting…'
  }
  switch (item.quoteState) {
    case 'authoritative': {
      const isFrozen =
        item.quoteSource === 'frozen' ||
        item.quoteSource === 'group' ||
        item.quoteSource === 'channel' ||
        item.quoteSource === 'published'
      if (props.isZh) {
        return isFrozen ? '服务端冻结报价' : '服务端参考报价'
      }
      return isFrozen ? 'Frozen Quote' : 'Reference Quote'
    }
    case 'free':
      return props.isZh ? '免费额度 ($0)' : 'Free Tier ($0)'
    case 'requires_scope':
      return props.isZh ? '需作用域' : 'Scope Required'
    case 'unpriced':
      return props.isZh ? '未定价' : 'Unpriced'
    case 'fallback_catalog':
      return props.isZh ? '目录参考单价' : 'Catalog Rate'
    default:
      return item.rateKnown
        ? props.isZh
          ? '已定价'
          : 'Priced'
        : props.isZh
          ? '未定价'
          : 'Unpriced'
  }
}

function quoteBadgeTone(item: PlaygroundCostEstimate): 'success' | 'accent' | 'warning' | 'neutral' {
  if (item.quoteState === 'authoritative' || item.quoteState === 'free') return 'success'
  if (item.quoteState === 'requires_scope' || item.quoteState === 'unpriced') return 'warning'
  if (item.quoteState === 'fallback_catalog') return 'accent'
  return 'neutral'
}

function formatFundingSource(source: string | null | undefined): string {
  if (!source) return props.isZh ? '按密钥绑定账户结算' : 'Key-bound account'
  const mapZh: Record<string, string> = {
    wallet: '钱包余额',
    subscription: '订阅额度',
    byok: '自带密钥 (BYOK)',
    mixed: '混合资金池',
    gift: '赠金抵扣',
    budget: '项目预算池'
  }
  const mapEn: Record<string, string> = {
    wallet: 'Wallet Balance',
    subscription: 'Subscription Allowance',
    byok: 'BYOK Credential',
    mixed: 'Mixed Funding',
    gift: 'Promotional Credit',
    budget: 'Project Budget'
  }
  return (props.isZh ? mapZh[source] : mapEn[source]) || source
}

function unpricedExplanation(item: PlaygroundCostEstimate): string {
  if (item.quoteState === 'requires_scope' || item.unpricedReason === 'requires_scope') {
    return props.isZh
      ? '需作用域：请先在顶部选择工作空间与项目作用域，以获取服务端权威冻结报价。'
      : 'Scope required: Select an active workspace and project scope to obtain an authoritative server quote.'
  }
  if (item.unpricedReason === 'unlisted_model') {
    return props.isZh
      ? '未定价：当前模型标识未在所选项目的已授权目录中发布单价，网关将在准入时按实时策略核定。'
      : 'Unpriced: Model is not listed with a published rate in this project catalog; admission checks live policy.'
  }
  return props.isZh
    ? '未定价：当前作用域未公开该模型的固定价格卡，网关将在准入时按生效价格版本校验并冻结预算。'
    : 'Unpriced: No published price card in this scope; gateway validates and reserves budget at admission.'
}

function handleClose(): void {
  emit('update:open', false)
  emit('cancel')
}
</script>

<template>
  <Modal
    :open="open"
    size="lg"
    :teleport-disabled="teleportDisabled"
    :title="
      replayRequestId
        ? isZh
          ? '确认重放请求服务端报价'
          : 'Confirm Request Replay Quote'
        : isZh
          ? '运行前服务端报价确认'
          : 'Confirm Authoritative Run Quote'
    "
    :description="
      isZh
        ? '每次调试台调用均独立核定单价、冻结预算，并按实际用量回执结算。'
        : 'Each Playground invocation independently verifies pricing, reserves budget, and settles from actual usage receipts.'
    "
    @update:open="(val) => !val && handleClose()"
    @close="handleClose"
  >
    <div data-testid="playground-cost-confirm-modal" class="space-y-4">
      <div
        v-if="replayRequestId"
        class="rounded-[var(--oh-r-md,8px)] border border-[var(--oh-accent-border,rgba(245,158,11,0.4))] bg-[var(--oh-accent-subtle,rgba(245,158,11,0.1))] px-3 py-2 text-xs text-[var(--oh-ink,var(--foreground))]"
      >
        <span class="font-semibold">{{ isZh ? '重放源请求 ID：' : 'Source Request ID: ' }}</span>
        <code class="font-mono tabular-nums">{{ replayRequestId }}</code>
      </div>

      <div class="grid gap-3" :class="estimates.length > 1 ? 'sm:grid-cols-2' : 'grid-cols-1'">
        <div
          v-for="(item, idx) in estimates"
          :key="`${item.sideLabel}-${idx}`"
          :data-testid="`cost-estimate-card-${idx}`"
          class="space-y-2.5 rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] p-3.5"
        >
          <div class="flex flex-wrap items-center justify-between gap-2">
            <Badge tone="accent" variant="subtle" mono>{{ item.sideLabel }}</Badge>
            <div class="flex flex-wrap items-center gap-1.5">
              <Badge :tone="quoteBadgeTone(item)" variant="subtle" mono>
                {{ quoteBadgeLabel(item) }}
              </Badge>
              <Badge tone="neutral" variant="outline" mono>{{ item.protocol }}</Badge>
            </div>
          </div>

          <div>
            <p class="text-[11px] uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
              {{ isZh ? '模型 / 密钥' : 'Model / Key' }}
            </p>
            <p class="mt-0.5 truncate font-mono text-xs font-semibold tabular-nums text-[var(--oh-ink,var(--foreground))]">
              {{ item.modelId || EM_DASH }} · {{ isZh ? '密钥' : 'Key' }} #{{ item.apiKeyId || EM_DASH }}
            </p>
          </div>

          <dl class="space-y-1.5 border-t border-[var(--oh-border,var(--border))] pt-2 text-xs">
            <div class="flex items-center justify-between gap-2">
              <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                {{ isZh ? '输入 / 输出单价' : 'Input / Output Rate' }}
              </dt>
              <dd class="font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                {{ formatModelRatePerMillion(item.inputRate, item.currency) || EM_DASH }}
                <span class="text-[var(--oh-ink-tertiary,var(--muted-foreground))]">·</span>
                {{ formatModelRatePerMillion(item.outputRate, item.currency) || EM_DASH }}
              </dd>
            </div>
            <div class="flex items-center justify-between gap-2">
              <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                {{ isZh ? '资金来源' : 'Funding Source' }}
              </dt>
              <dd class="font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                {{ formatFundingSource(item.fundingSource) }}
              </dd>
            </div>
            <div v-if="item.priceVersion" class="flex items-center justify-between gap-2">
              <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                {{ isZh ? '生效价格版本' : 'Price Version' }}
              </dt>
              <dd class="max-w-[180px] truncate font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                {{ item.priceVersion }}
              </dd>
            </div>
            <div class="flex items-center justify-between gap-2">
              <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                {{ isZh ? '预估输入 Token' : 'Est. Input Tokens' }}
              </dt>
              <dd class="font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                ~{{ formatNullableInteger(item.estimatedInputTokens) }}
              </dd>
            </div>
            <div class="flex items-center justify-between gap-2">
              <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                {{ isZh ? '最大输出上限' : 'Enforced Output Bound' }}
              </dt>
              <dd class="font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                {{ formatNullableInteger(item.maxOutputTokens) }}
              </dd>
            </div>
            <div class="flex items-center justify-between gap-2">
              <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                {{ isZh ? '请求载荷大小' : 'Payload Size' }}
              </dt>
              <dd
                class="font-mono tabular-nums"
                :class="
                  item.exceedsBodyLimit
                    ? 'font-semibold text-[var(--oh-danger,#dc2626)]'
                    : 'text-[var(--oh-ink,var(--foreground))]'
                "
              >
                {{ formatNullableBytes(item.payloadBytes) }} / {{ formatNullableBytes(MAX_PLAYGROUND_BODY_BYTES) }}
              </dd>
            </div>
            <div class="flex items-center justify-between gap-2 border-t border-[var(--oh-border,var(--border))] pt-1.5">
              <dt class="font-medium text-[var(--oh-ink,var(--foreground))]">
                {{ isZh ? '预估最大费用上限' : 'Authoritative Max Estimate' }}
              </dt>
              <dd
                data-testid="estimate-max-cost"
                class="font-mono text-sm font-bold tabular-nums text-[var(--oh-accent-text,var(--brand-text,#d97706))]"
              >
                {{
                  item.rateKnown && item.maxTotalCost !== null
                    ? formatExactMoney(item.maxTotalCost, item.currency)
                    : isZh
                      ? '未定价'
                      : 'Unpriced'
                }}
              </dd>
            </div>
          </dl>

          <p
            v-if="item.budgetBlocked"
            role="status"
            class="rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-warning-border,rgba(245,158,11,0.4))] bg-[var(--oh-warning-subtle,rgba(245,158,11,0.1))] px-2.5 py-1.5 text-[11px] leading-4 text-[var(--oh-ink,var(--foreground))]"
          >
            {{
              isZh
                ? '提示：当前作用域有效配额或预算规则已处于受限状态，提交后可能被预算门禁拦截。'
                : 'Notice: An effective budget or quota rule in this scope is currently blocked.'
            }}
          </p>

          <p
            v-if="!item.rateKnown"
            class="text-[11px] leading-4 text-[var(--oh-ink-tertiary,var(--muted-foreground))]"
          >
            {{ unpricedExplanation(item) }}
          </p>
        </div>
      </div>

      <div
        v-if="combinedMaxTotal"
        class="flex items-center justify-between rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] px-3.5 py-2 text-xs"
      >
        <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
          {{ isZh ? '双侧独立运行合计预估上限（各自独立冻结与取消）' : 'Combined Independent Compare Bound (separate holds & streams)' }}
        </span>
        <span class="font-mono font-bold tabular-nums text-[var(--oh-ink,var(--foreground))]">
          {{ combinedMaxTotal }}
        </span>
      </div>

      <p
        v-if="hasOversizedPayload"
        role="alert"
        class="rounded-[var(--oh-r-md,8px)] border border-[var(--oh-danger-border,rgba(239,68,68,0.4))] bg-[var(--oh-danger-subtle,rgba(239,68,68,0.1))] px-3 py-2 text-xs text-[var(--oh-danger,#dc2626)]"
      >
        {{
          isZh
            ? '请求体超过 256 KiB 上限，请精简消息或工具定义后再提交。'
            : 'Request body exceeds the 256 KiB limit. Reduce message or tool size.'
        }}
      </p>
    </div>

    <template #footer>
      <div class="flex items-center justify-end gap-2">
        <Button variant="secondary" size="sm" :disabled="submitting" @click="handleClose">
          {{ isZh ? '取消' : 'Cancel' }}
        </Button>
        <Button
          data-testid="confirm-playground-funding"
          variant="primary"
          size="sm"
          :loading="submitting"
          :disabled="hasOversizedPayload || submitting"
          @click="emit('confirm')"
        >
          {{ isZh ? '确认预估并付费执行' : 'Acknowledge Cost & Run' }}
        </Button>
      </div>
    </template>
  </Modal>
</template>
