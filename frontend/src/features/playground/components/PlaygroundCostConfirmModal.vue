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
import { MAX_PLAYGROUND_BODY_BYTES } from '../costEstimate'
import type { PlaygroundCostEstimate } from '../types'

const props = withDefaults(
  defineProps<{
    open: boolean
    estimates: PlaygroundCostEstimate[]
    replayRequestId?: string | null
    submitting?: boolean
    isZh?: boolean
    teleportDisabled?: boolean
  }>(),
  {
    replayRequestId: null,
    submitting: false,
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
          ? '确认重放请求计费预估'
          : 'Confirm Paid Request Replay Estimate'
        : isZh
          ? '运行前费用预估确认'
          : 'Confirm Playground Cost Estimate'
    "
    :description="
      isZh
        ? '每次 Playground 调用均独立冻结预算并按实际结算回执扣费（funding_acknowledged: true）。'
        : 'Each Playground invocation reserves budget independently and settles from verified usage receipts (funding_acknowledged: true).'
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
            <Badge tone="neutral" variant="outline" mono>{{ item.protocol }}</Badge>
          </div>

          <div>
            <p class="text-[11px] uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
              Model / Key
            </p>
            <p class="mt-0.5 truncate font-mono text-xs font-semibold tabular-nums text-[var(--oh-ink,var(--foreground))]">
              {{ item.modelId || EM_DASH }} · Key #{{ item.apiKeyId || EM_DASH }}
            </p>
          </div>

          <dl class="space-y-1.5 border-t border-[var(--oh-border,var(--border))] pt-2 text-xs">
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
                {{ isZh ? '输出上限 (Max Output)' : 'Enforced Output Bound' }}
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
                {{ isZh ? '预估最大冻结额度' : 'Est. Upper-Bound Hold' }}
              </dt>
              <dd
                data-testid="estimate-max-cost"
                class="font-mono text-sm font-bold tabular-nums text-[var(--oh-accent-text,var(--brand-text,#d97706))]"
              >
                {{
                  item.rateKnown && item.maxTotalCost !== null
                    ? formatExactMoney(item.maxTotalCost, item.currency)
                    : EM_DASH
                }}
              </dd>
            </div>
          </dl>

          <p
            v-if="!item.rateKnown"
            class="text-[11px] leading-4 text-[var(--oh-ink-tertiary,var(--muted-foreground))]"
          >
            {{
              isZh
                ? '目录未公开该模型静态单价 (—)，网关将在准入时按生效价格版本校验并冻结预算。'
                : 'Catalog rate not published in scope (—); gateway reserves and settles under the active price version.'
            }}
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
            ? '外层请求超过 BE-13 规定的 256 KiB 上限，请精简消息或工具定义后再提交。'
            : 'Request body exceeds the BE-13 256 KiB outer payload limit. Reduce message or tool size.'
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
