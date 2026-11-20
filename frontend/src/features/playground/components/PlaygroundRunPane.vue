<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import CopyButton from '@/components/ui/CopyButton.vue'
import ConsoleAsyncBanner from '@/features/console/components/ConsoleAsyncBanner.vue'
import {
  EM_DASH,
  formatNullableInteger,
  formatNullableLatencyMs
} from '@/features/console/format'
import type { PlaygroundSessionSnapshot, PlaygroundSideConfig, PlaygroundToolCallView } from '../types'

const props = withDefaults(
  defineProps<{
    sideKey: 'A' | 'B'
    config: PlaygroundSideConfig
    session: PlaygroundSessionSnapshot
    compareMode?: boolean
    inspected?: boolean
    isZh?: boolean
  }>(),
  {
    compareMode: false,
    inspected: false,
    isZh: false
  }
)

const emit = defineEmits<{
  run: []
  stop: []
  recover: []
  'query-status': []
  inspect: []
  'inject-tool-response': [payload: { toolCallId: string; name: string }]
}>()

const { t } = useI18n()
const reasoningExpanded = ref(true)
const diagnosticsOpen = ref(false)
const diagnosticsTab = ref<'request' | 'events'>('request')

const formattedSubmittedPayload = computed(() => {
  if (!props.session.submittedPayload) return ''
  return JSON.stringify(props.session.submittedPayload, null, 2)
})

const totalEventFramesCount = computed(
  () => props.session.sseFrames.length + (props.session.rawEvents?.length ?? 0)
)

const formattedSseFramesText = computed(() =>
  JSON.stringify(
    {
      sseFrames: props.session.sseFrames,
      rawEvents: props.session.rawEvents ?? []
    },
    null,
    2
  )
)

function formatFramePayload(raw: string): string {
  if (!raw) return EM_DASH
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

function formatRawEvent(event: unknown): string {
  try {
    return JSON.stringify(event, null, 2)
  } catch {
    return String(event)
  }
}

const isStreaming = computed(
  () => props.session.status === 'submitting' || props.session.status === 'streaming'
)

const statusBadgeTone = computed(() => {
  switch (props.session.status) {
    case 'streaming':
    case 'submitting':
      return 'accent'
    case 'succeeded':
      return 'success'
    case 'failed':
      return 'danger'
    case 'cancel_requested':
    case 'canceled':
      return 'warning'
    case 'unknown':
    case 'disconnected_recoverable':
      return 'unknown'
    default:
      return 'neutral'
  }
})

const statusLabel = computed(() => {
  const s = props.session.status
  if (props.isZh) {
    const map: Record<string, string> = {
      idle: '待命',
      awaiting_consent: '待确认费用',
      submitting: '准入中…',
      streaming: '流式生成中',
      cancel_requested: '取消请求已提交',
      succeeded: '已完成',
      failed: '执行失败',
      canceled: '已取消',
      unknown: '状态未知',
      disconnected_recoverable: '连接中断·可恢复'
    }
    return map[s] || s
  }
  return s.replace(/_/g, ' ')
})

function formatSettlementLabel(state: string | null | undefined): string {
  if (!state) return EM_DASH
  if (state === 'pending' && props.session.status === 'succeeded') {
    return props.isZh ? '异步确认中' : 'confirming'
  }
  if (!props.isZh) return state
  const map: Record<string, string> = {
    estimated: '预估',
    reserved: '已冻结',
    pending: '待结算',
    settled: '已结算',
    needs_review: '待核查',
    adjusted: '已调账',
    refunded: '已退回',
    canceled: '已取消',
    unknown: '未知'
  }
  return map[state] || state
}

function triggerToolResponse(tc: PlaygroundToolCallView): void {
  emit('inject-tool-response', {
    toolCallId: tc.toolCallId || `call_${tc.index}`,
    name: tc.name || 'tool'
  })
}
</script>

<template>
  <section
    :data-testid="`playground-run-pane-${sideKey}`"
    :class="[
      'flex min-w-0 flex-col rounded-[var(--oh-r-md,8px)] border bg-[var(--oh-surface,var(--card))] transition-colors',
      compareMode && inspected
        ? 'border-[var(--oh-accent,#f59e0b)]'
        : 'border-[var(--oh-border,var(--border))]'
    ]"
  >
    <header class="flex flex-wrap items-center justify-between gap-2 border-b border-[var(--oh-border,var(--border))] px-3.5 py-2.5">
      <div class="flex min-w-0 flex-wrap items-center gap-2">
        <Badge v-if="compareMode" tone="accent" variant="subtle" mono>
          {{ config.label }}
        </Badge>
        <span class="truncate font-mono text-xs font-semibold tabular-nums text-[var(--oh-ink,var(--foreground))]">
          {{ config.modelId || (isZh ? '未选择模型' : 'No model selected') }}
        </span>
        <Badge tone="neutral" variant="outline" mono>{{ config.protocol }}</Badge>
        <Badge :tone="statusBadgeTone" variant="subtle" mono :dot="isStreaming">
          {{ statusLabel }}
        </Badge>
      </div>

      <div class="flex flex-wrap items-center gap-1.5">
        <Button
          variant="ghost"
          size="xs"
          :aria-expanded="diagnosticsOpen"
          :data-testid="`toggle-diagnostics-${sideKey}`"
          @click="diagnosticsOpen = !diagnosticsOpen"
        >
          {{ t('playgroundDiagnostics.togglePane') }}
          <span
            v-if="totalEventFramesCount > 0"
            class="ml-1 rounded bg-[var(--oh-bg-subtle,var(--surface-2))] px-1 font-mono text-[10px] tabular-nums"
          >
            {{ totalEventFramesCount }}
          </span>
        </Button>
        <Button
          v-if="compareMode"
          variant="ghost"
          size="xs"
          :data-testid="`inspect-side-${sideKey}`"
          @click="emit('inspect')"
        >
          {{ inspected ? (isZh ? '检查中' : 'Inspecting') : (isZh ? '检查指标' : 'Inspect') }}
        </Button>
        <Button
          v-if="compareMode && !isStreaming"
          variant="secondary"
          size="xs"
          :data-testid="`run-single-side-${sideKey}`"
          :disabled="!config.modelId || !config.apiKeyId"
          @click="emit('run')"
        >
          {{ isZh ? `仅运行 ${sideKey}` : `Run ${sideKey}` }}
        </Button>
        <Button
          v-if="isStreaming || session.status === 'cancel_requested'"
          variant="danger"
          size="xs"
          :data-testid="`stop-run-${sideKey}`"
          :loading="session.status === 'cancel_requested'"
          @click="emit('stop')"
        >
          {{ isZh ? '停止 (Esc)' : 'Stop (Esc)' }}
        </Button>
      </div>
    </header>

    <div class="flex-1 space-y-3 p-3.5">
      <ConsoleAsyncBanner
        v-if="session.errorNotice"
        :notice="session.errorNotice"
        :retry-label="
          session.status === 'disconnected_recoverable'
            ? isZh
              ? '恢复原运行状态'
              : 'Recover Original Run'
            : isZh
              ? '查询运行状态'
              : 'Query Run Status'
        "
        @retry="
          session.status === 'disconnected_recoverable' ? emit('recover') : emit('query-status')
        "
      />

      <div
        v-if="session.status === 'disconnected_recoverable' && session.idempotencyKey"
        :data-testid="`disconnect-recovery-${sideKey}`"
        class="flex flex-wrap items-center justify-between gap-2 rounded-[var(--oh-r-md,8px)] border border-dashed border-[var(--oh-accent-border,rgba(245,158,11,0.45))] bg-[var(--oh-accent-subtle,rgba(245,158,11,0.08))] px-3 py-2 text-xs"
      >
        <div class="min-w-0">
          <p class="font-medium text-[var(--oh-ink,var(--foreground))]">
            {{ isZh ? '运行恢复句柄已保留' : 'Run Recovery Handle Preserved' }}
          </p>
          <p class="mt-0.5 truncate font-mono text-[11px] tabular-nums text-[var(--oh-ink-secondary,var(--muted-foreground))]">
            {{ isZh ? '恢复标识：' : 'Recovery ID: ' }}{{ session.idempotencyKey }}
          </p>
        </div>
        <Button
          :data-testid="`recover-btn-${sideKey}`"
          variant="outline-accent"
          size="xs"
          @click="emit('recover')"
        >
          {{ isZh ? '恢复运行状态' : 'Recover Run' }}
        </Button>
      </div>

      <div
        v-if="session.status === 'unknown'"
        :data-testid="`unknown-outcome-${sideKey}`"
        class="flex flex-wrap items-center justify-between gap-2 rounded-[var(--oh-r-md,8px)] border border-dashed border-[var(--oh-border-strong,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-3 py-2 text-xs"
      >
        <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
          {{
            isZh
              ? '上游完成状态暂未确认，请查询运行状态与关联结算收据。'
              : 'Upstream completion is uncertain. Query run status and linked settlement receipt.'
          }}
        </span>
        <Button
          :data-testid="`query-status-btn-${sideKey}`"
          variant="secondary"
          size="xs"
          @click="emit('query-status')"
        >
          {{ isZh ? '刷新状态' : 'Query Status' }}
        </Button>
      </div>

      <div
        v-if="session.status === 'succeeded' && session.settlementState === 'pending'"
        :data-testid="`settlement-pending-${sideKey}`"
        class="flex flex-wrap items-center justify-between gap-2 rounded-[var(--oh-r-md,8px)] border border-oh-status-info-border bg-oh-status-info-bg px-3 py-2 text-xs"
      >
        <div class="min-w-0">
          <p class="font-medium text-[var(--oh-ink,var(--foreground))]">
            {{
              isZh
                ? '推理已完成 · 账本异步确认中'
                : 'Completed · Ledger confirming asynchronously'
            }}
          </p>
          <p class="mt-0.5 text-[11px] text-[var(--oh-ink-secondary,var(--muted-foreground))]">
            {{
              isZh
                ? '本次推理结果已成功返回，最终扣费与结算流水正在后台异步入账。'
                : 'The inference response succeeded. Final ledger settlement is confirming in the background.'
            }}
          </p>
        </div>
        <Button
          :data-testid="`refresh-settlement-btn-${sideKey}`"
          variant="secondary"
          size="xs"
          @click="emit('query-status')"
        >
          {{ isZh ? '刷新结算状态' : 'Refresh Settlement' }}
        </Button>
      </div>

      <!-- Reasoning / Thinking Trace -->
      <div
        v-if="session.reasoningText"
        :data-testid="`reasoning-block-${sideKey}`"
        class="rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))]"
      >
        <button
          type="button"
          class="flex w-full items-center justify-between px-3 py-2 text-left font-mono text-xs text-[var(--oh-ink-secondary,var(--muted-foreground))]"
          :aria-expanded="reasoningExpanded"
          @click="reasoningExpanded = !reasoningExpanded"
        >
          <span>{{ isZh ? '推理过程 (Reasoning)' : 'Reasoning Trace' }}</span>
          <span>{{ reasoningExpanded ? '▾' : '▸' }}</span>
        </button>
        <pre
          v-show="reasoningExpanded"
          class="max-h-48 overflow-auto border-t border-[var(--oh-border,var(--border))] px-3 py-2.5 font-mono text-xs leading-5 whitespace-pre-wrap break-words text-[var(--oh-ink-secondary,var(--muted-foreground))]"
        >{{ session.reasoningText }}</pre>
      </div>

      <!-- Assistant Output -->
      <div
        v-if="session.outputText"
        :data-testid="`output-text-${sideKey}`"
        class="rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] p-3"
      >
        <div class="mb-1.5 flex items-center justify-between">
          <span class="font-mono text-[11px] uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
            {{ isZh ? '助手输出' : 'Assistant Output' }}
          </span>
          <CopyButton :value="session.outputText" size="xs" />
        </div>
        <div class="whitespace-pre-wrap break-words text-sm leading-6 text-[var(--oh-ink,var(--foreground))]">
          {{ session.outputText }}
        </div>
      </div>

      <!-- Synchronous Non-Stream Output -->
      <div
        v-else-if="session.syncOutput"
        class="rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] p-3"
      >
        <div class="mb-1.5 flex items-center justify-between">
          <span class="font-mono text-[11px] uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
            {{ isZh ? '同步响应 JSON' : 'Sync JSON Output' }}
          </span>
          <CopyButton :value="JSON.stringify(session.syncOutput, null, 2)" size="xs" />
        </div>
        <pre class="max-h-64 overflow-auto font-mono text-xs leading-5 whitespace-pre-wrap text-[var(--oh-ink,var(--foreground))]">{{ JSON.stringify(session.syncOutput, null, 2) }}</pre>
      </div>

      <!-- Empty Session Output -->
      <div
        v-else-if="session.status === 'idle' && session.toolCalls.length === 0"
        :data-testid="`empty-run-${sideKey}`"
        class="flex min-h-[132px] flex-col items-center justify-center rounded-[var(--oh-r-md,8px)] border border-dashed border-[var(--oh-border,var(--border))] px-4 py-6 text-center"
      >
        <p class="font-mono text-xs text-[var(--oh-ink-secondary,var(--muted-foreground))]">
          {{
            isZh
              ? '选择密钥、模型与协议后提交运行（每次调用先确认费用预估）'
              : 'Select an API key, model, and protocol to run (cost estimate confirmed before dispatch)'
          }}
        </p>
      </div>

      <!-- Tool Call Events -->
      <div v-if="session.toolCalls.length > 0" :data-testid="`tool-calls-${sideKey}`" class="space-y-2">
        <p class="font-mono text-[11px] font-semibold uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
          {{ isZh ? '工具调用事件' : 'Tool Call Events' }} ({{ session.toolCalls.length }})
        </p>
        <div
          v-for="tc in session.toolCalls"
          :key="tc.key"
          class="rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] p-3"
        >
          <div class="flex flex-wrap items-center justify-between gap-2">
            <div class="flex items-center gap-2 font-mono text-xs tabular-nums">
              <Badge tone="accent" variant="outline" mono>#{{ tc.index }}</Badge>
              <span class="font-semibold text-[var(--oh-ink,var(--foreground))]">{{ tc.name || 'function' }}</span>
              <span class="text-[var(--oh-ink-tertiary,var(--muted-foreground))]">{{ tc.toolCallId || EM_DASH }}</span>
            </div>
            <Button variant="ghost" size="xs" @click="triggerToolResponse(tc)">
              {{ isZh ? '+ 注入工具响应' : '+ Inject Tool Result' }}
            </Button>
          </div>
          <pre class="mt-2 max-h-40 overflow-auto font-mono text-xs leading-5 whitespace-pre-wrap break-all text-[var(--oh-ink,var(--foreground))]">{{ tc.parsedArguments ? JSON.stringify(tc.parsedArguments, null, 2) : tc.rawArguments || EM_DASH }}</pre>
        </div>
      </div>

      <!-- Diagnostics -->
      <ul v-if="session.diagnostics.length > 0" class="space-y-1">
        <li
          v-for="diag in session.diagnostics"
          :key="diag.seq"
          class="rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-warning-border,rgba(245,158,11,0.35))] bg-[var(--oh-warning-subtle,rgba(245,158,11,0.08))] px-2.5 py-1.5 font-mono text-xs text-[var(--oh-ink,var(--foreground))]"
        >
          [{{ diag.severity || (isZh ? '诊断' : 'diagnostic') }}] {{ diag.code || EM_DASH }}
          <span v-if="diag.field"> · {{ isZh ? '字段：' : 'field=' }}{{ diag.field }}</span>
          <span v-if="diag.reason"> · {{ diag.reason }}</span>
        </li>
      </ul>

      <!-- Collapsible Raw Request JSON & SSE Event Frames Inspector -->
      <section
        v-if="diagnosticsOpen"
        :data-testid="`playground-diagnostics-inspector-${sideKey}`"
        class="rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] p-3 space-y-2.5"
      >
        <div class="flex flex-wrap items-center justify-between gap-2">
          <div
            role="tablist"
            :aria-label="t('playgroundDiagnostics.tabsAriaLabel')"
            class="inline-flex rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] p-0.5"
          >
            <button
              type="button"
              role="tab"
              :aria-selected="diagnosticsTab === 'request'"
              :data-testid="`diagnostics-tab-request-${sideKey}`"
              :class="[
                'rounded-[4px] px-2.5 py-1 font-mono text-[11px] transition-colors',
                diagnosticsTab === 'request'
                  ? 'bg-[var(--oh-accent-subtle,rgba(245,158,11,0.14))] font-semibold text-[var(--oh-ink,var(--foreground))]'
                  : 'text-[var(--oh-ink-secondary,var(--muted-foreground))] hover:text-[var(--oh-ink,var(--foreground))]'
              ]"
              @click="diagnosticsTab = 'request'"
            >
              {{ t('playgroundDiagnostics.tabRawRequest') }}
            </button>
            <button
              type="button"
              role="tab"
              :aria-selected="diagnosticsTab === 'events'"
              :data-testid="`diagnostics-tab-sse-${sideKey}`"
              :class="[
                'rounded-[4px] px-2.5 py-1 font-mono text-[11px] transition-colors',
                diagnosticsTab === 'events'
                  ? 'bg-[var(--oh-accent-subtle,rgba(245,158,11,0.14))] font-semibold text-[var(--oh-ink,var(--foreground))]'
                  : 'text-[var(--oh-ink-secondary,var(--muted-foreground))] hover:text-[var(--oh-ink,var(--foreground))]'
              ]"
              @click="diagnosticsTab = 'events'"
            >
              {{ t('playgroundDiagnostics.tabSseFrames') }} ({{ totalEventFramesCount }})
            </button>
          </div>

          <CopyButton
            v-if="diagnosticsTab === 'request' && formattedSubmittedPayload"
            :value="formattedSubmittedPayload"
            :label="t('playgroundDiagnostics.copyPayload')"
            size="xs"
          />
          <CopyButton
            v-else-if="diagnosticsTab === 'events' && totalEventFramesCount > 0"
            :value="formattedSseFramesText"
            :label="t('playgroundDiagnostics.copyFrames')"
            size="xs"
          />
        </div>

        <div v-if="diagnosticsTab === 'request'" class="space-y-1.5">
          <p class="font-mono text-[11px] text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
            {{ t('playgroundDiagnostics.rawRequestLabel') }}
          </p>
          <pre
            v-if="formattedSubmittedPayload"
            :data-testid="`diagnostics-submitted-payload-${sideKey}`"
            class="max-h-64 overflow-auto rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] p-2.5 font-mono text-[11px] leading-5 whitespace-pre-wrap break-all text-[var(--oh-ink,var(--foreground))]"
          >{{ formattedSubmittedPayload }}</pre>
          <p
            v-else
            :data-testid="`diagnostics-submitted-payload-empty-${sideKey}`"
            class="py-3 text-center font-mono text-xs text-[var(--oh-ink-tertiary,var(--muted-foreground))]"
          >
            {{ t('playgroundDiagnostics.rawRequestEmpty') }}
          </p>
        </div>

        <div v-else class="space-y-2">
          <p
            v-if="totalEventFramesCount === 0"
            :data-testid="`diagnostics-sse-empty-${sideKey}`"
            class="py-3 text-center font-mono text-xs text-[var(--oh-ink-tertiary,var(--muted-foreground))]"
          >
            {{ t('playgroundDiagnostics.sseFramesEmpty') }}
          </p>
          <template v-else>
            <div v-if="session.sseFrames.length > 0" class="space-y-1.5">
              <p class="font-mono text-[11px] text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
                {{ t('playgroundDiagnostics.sseFramesLabel') }} ({{ session.sseFrames.length }})
              </p>
              <ul
                :data-testid="`diagnostics-sse-frames-${sideKey}`"
                class="max-h-64 space-y-1.5 overflow-auto"
              >
                <li
                  v-for="(frame, idx) in session.sseFrames"
                  :key="`${frame.id}-${idx}`"
                  class="rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] p-2 font-mono text-[11px] tabular-nums"
                >
                  <div class="flex flex-wrap items-center justify-between gap-2">
                    <div class="flex items-center gap-1.5">
                      <Badge tone="neutral" variant="outline" mono>{{ frame.event }}</Badge>
                      <span class="font-semibold text-[var(--oh-ink,var(--foreground))]">
                        #{{ frame.seq ?? EM_DASH }}
                      </span>
                    </div>
                    <span class="text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
                      +{{ frame.receivedAtMs }}ms
                    </span>
                  </div>
                  <p v-if="frame.summary" class="mt-1 text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                    {{ frame.summary }}
                  </p>
                  <pre
                    v-if="frame.rawData"
                    class="mt-1.5 max-h-32 overflow-auto rounded bg-[var(--oh-bg-subtle,var(--surface-2))] p-2 text-[10px] leading-4 whitespace-pre-wrap break-all text-[var(--oh-ink,var(--foreground))]"
                  >{{ formatFramePayload(frame.rawData) }}</pre>
                </li>
              </ul>
            </div>

            <div v-if="(session.rawEvents?.length ?? 0) > 0" class="space-y-1.5">
              <p class="font-mono text-[11px] text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
                {{ t('playgroundDiagnostics.rawEventsLabel') }} ({{ session.rawEvents.length }})
              </p>
              <ul
                :data-testid="`diagnostics-raw-events-${sideKey}`"
                class="max-h-56 space-y-1.5 overflow-auto"
              >
                <li
                  v-for="(rawEvent, idx) in session.rawEvents"
                  :key="idx"
                  class="rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] p-2"
                >
                  <pre class="max-h-32 overflow-auto font-mono text-[10px] leading-4 whitespace-pre-wrap break-all text-[var(--oh-ink,var(--foreground))]">{{ formatRawEvent(rawEvent) }}</pre>
                </li>
              </ul>
            </div>
          </template>
        </div>
      </section>
    </div>

    <!-- Footer Usage & Settlement Strip -->
    <footer class="flex flex-wrap items-center justify-between gap-2 border-t border-[var(--oh-border,var(--border))] px-3.5 py-2 font-mono text-[11px] tabular-nums text-[var(--oh-ink-secondary,var(--muted-foreground))]">
      <div class="flex flex-wrap items-center gap-3">
        <span>{{ isZh ? '输入' : 'in' }}: {{ session.inputTokens !== null ? formatNullableInteger(session.inputTokens) : EM_DASH }}</span>
        <span>{{ isZh ? '输出' : 'out' }}: {{ session.outputTokens !== null ? formatNullableInteger(session.outputTokens) : EM_DASH }}</span>
        <span>{{ isZh ? '首字' : 'TTFT' }}: {{ formatNullableLatencyMs(session.ttftMs) }}</span>
        <span>{{ isZh ? '总计' : 'Total' }}: {{ formatNullableLatencyMs(session.elapsedMs) }}</span>
      </div>
      <div class="flex items-center gap-2">
        <span>{{ isZh ? '结算' : 'settlement' }}: {{ formatSettlementLabel(session.settlementState) }}</span>
        <span v-if="session.requestId" class="truncate max-w-[140px]">{{ isZh ? '请求' : 'req' }}:{{ session.requestId.slice(0, 8) }}</span>
      </div>
    </footer>
  </section>
</template>
