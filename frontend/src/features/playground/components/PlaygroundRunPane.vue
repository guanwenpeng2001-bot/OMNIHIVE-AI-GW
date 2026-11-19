<script setup lang="ts">
import { computed, ref } from 'vue'
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

const reasoningExpanded = ref(true)

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
              ? '按幂等键恢复'
              : 'Recover by Idempotency-Key'
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
            {{ isZh ? '幂等恢复句柄已保留' : 'Idempotent Run Handle Preserved' }}
          </p>
          <p class="mt-0.5 truncate font-mono text-[11px] tabular-nums text-[var(--oh-ink-secondary,var(--muted-foreground))]">
            Idempotency-Key: {{ session.idempotencyKey }}
          </p>
        </div>
        <Button
          :data-testid="`recover-btn-${sideKey}`"
          variant="outline-accent"
          size="xs"
          @click="emit('recover')"
        >
          {{ isZh ? '按幂等键恢复' : 'Recover Run' }}
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
              ? '上游完成状态未确认（unknown），请查询运行状态与关联收据。'
              : 'Upstream completion is uncertain (unknown). Query run status and linked settlement receipt.'
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
            Assistant Output
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
            Native JSON Output
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
          {{ isZh ? '工具调用事件 (Tool Calls)' : 'Tool Call Events' }} ({{ session.toolCalls.length }})
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
          [{{ diag.severity || 'diagnostic' }}] {{ diag.code || EM_DASH }}
          <span v-if="diag.field"> · field={{ diag.field }}</span>
          <span v-if="diag.reason"> · {{ diag.reason }}</span>
        </li>
      </ul>
    </div>

    <!-- Footer Usage & Settlement Strip -->
    <footer class="flex flex-wrap items-center justify-between gap-2 border-t border-[var(--oh-border,var(--border))] px-3.5 py-2 font-mono text-[11px] tabular-nums text-[var(--oh-ink-secondary,var(--muted-foreground))]">
      <div class="flex flex-wrap items-center gap-3">
        <span>in: {{ session.inputTokens !== null ? formatNullableInteger(session.inputTokens) : EM_DASH }}</span>
        <span>out: {{ session.outputTokens !== null ? formatNullableInteger(session.outputTokens) : EM_DASH }}</span>
        <span>TTFT: {{ formatNullableLatencyMs(session.ttftMs) }}</span>
        <span>Total: {{ formatNullableLatencyMs(session.elapsedMs) }}</span>
      </div>
      <div class="flex items-center gap-2">
        <span>settlement: {{ session.settlementState || EM_DASH }}</span>
        <span v-if="session.requestId" class="truncate max-w-[140px]">req:{{ session.requestId.slice(0, 8) }}</span>
      </div>
    </footer>
  </section>
</template>
