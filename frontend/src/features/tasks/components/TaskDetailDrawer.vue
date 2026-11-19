<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import CopyButton from '@/components/ui/CopyButton.vue'
import Drawer from '@/components/ui/Drawer.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import Stepper from '@/components/ui/Stepper.vue'
import ConsoleAsyncBanner from '@/features/console/components/ConsoleAsyncBanner.vue'
import type { ConsoleAsyncNotice } from '@/features/console/asyncState'
import {
  EM_DASH,
  formatChargeObject,
  formatNullableBytes,
  formatNullableInteger
} from '@/features/console/format'
import {
  buildTaskLifecycleSteps,
  classifyTaskArtifact,
  resolveHonestTaskProgress,
  taskStateTone
} from '../progressRules'
import type { ImageTaskArtifact, ImageTaskDetail, ImageTaskItem } from '../types'

const props = withDefaults(
  defineProps<{
    open: boolean
    detail: ImageTaskDetail | null
    items: ImageTaskItem[]
    loading?: boolean
    actionBusyId?: string
    notice?: ConsoleAsyncNotice | null
    isZh?: boolean
    teleportDisabled?: boolean
    nowMs?: number
  }>(),
  {
    loading: false,
    actionBusyId: '',
    notice: null,
    isZh: false,
    teleportDisabled: false,
    nowMs: undefined
  }
)

const emit = defineEmits<{
  'update:open': [value: boolean]
  close: []
  refresh: []
  cancel: [taskId: string]
  'open-retry': [payload: { taskId: string; preselectedItemIds: string[] }]
  'download-artifact': [artifact: ImageTaskArtifact]
  'revoke-artifact': [artifact: ImageTaskArtifact]
  'select-task': [taskId: string]
}>()

const liveClockMs = ref(Date.now())
let clockInterval: ReturnType<typeof setInterval> | null = null

onMounted(() => {
  if (props.nowMs === undefined) {
    clockInterval = setInterval(() => {
      liveClockMs.value = Date.now()
    }, 15_000)
  }
})

onBeforeUnmount(() => {
  if (clockInterval) clearInterval(clockInterval)
})

const effectiveNowMs = computed(() => props.nowMs ?? liveClockMs.value)

const honestProgress = computed(() =>
  props.detail ? resolveHonestTaskProgress(props.detail) : null
)

const lifecycleSteps = computed(() =>
  props.detail ? buildTaskLifecycleSteps(props.detail, props.isZh) : []
)

const currentStepId = computed(() => {
  const current = lifecycleSteps.value.find((s) => s.status === 'current' || s.status === 'error' || s.status === 'warning')
  return current?.id || lifecycleSteps.value[lifecycleSteps.value.length - 1]?.id || 'admitted'
})

const canCancel = computed(() =>
  Boolean(props.detail?.allowed_actions?.includes('tasks.cancel'))
)

const canRetry = computed(() =>
  Boolean(props.detail?.allowed_actions?.includes('tasks.retry'))
)

const cancelDisabledReason = computed(
  () => props.detail?.action_reasons?.['tasks.cancel'] || ''
)

const retryDisabledReason = computed(
  () => props.detail?.action_reasons?.['tasks.retry'] || ''
)

const retryableItemIds = computed(() =>
  props.items.filter((item) => item.retryable).map((item) => item.id)
)

function handleClose(): void {
  emit('update:open', false)
  emit('close')
}
</script>

<template>
  <Drawer
    :open="open"
    size="lg"
    :teleport-disabled="teleportDisabled"
    :title="detail ? `${isZh ? '任务详情' : 'Task Detail'} · ${detail.id}` : isZh ? '任务详情' : 'Task Detail'"
    :description="
      detail
        ? `${detail.model_id} · source=${detail.source} · v${detail.version}`
        : undefined
    "
    @update:open="(val) => !val && handleClose()"
    @close="handleClose"
  >
    <div data-testid="task-detail-drawer" class="space-y-4">
      <ConsoleAsyncBanner
        v-if="notice"
        :notice="notice"
        :retry-label="isZh ? '刷新详情' : 'Refresh'"
        @retry="emit('refresh')"
      />

      <div v-if="loading && !detail" data-testid="drawer-skeleton" class="space-y-3">
        <Skeleton height="84px" />
        <Skeleton height="120px" />
        <Skeleton height="160px" />
      </div>

      <template v-else-if="detail">
        <!-- Status & Actions Header -->
        <div class="flex flex-wrap items-center justify-between gap-2 rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] p-3">
          <div class="flex flex-wrap items-center gap-2">
            <Badge :tone="taskStateTone(detail.state)" variant="subtle" mono>
              {{ detail.state }}
            </Badge>
            <Badge tone="neutral" variant="outline" mono>
              stage: {{ detail.stage || EM_DASH }}
            </Badge>
            <Badge tone="neutral" variant="outline" mono>
              settlement: {{ detail.settlement_state || EM_DASH }}
            </Badge>
          </div>

          <div class="flex flex-wrap items-center gap-1.5">
            <Button variant="ghost" size="xs" :loading="loading" @click="emit('refresh')">
              {{ isZh ? '刷新' : 'Refresh' }}
            </Button>
            <Button
              v-if="canCancel"
              data-testid="task-cancel-btn"
              variant="danger"
              size="xs"
              :loading="actionBusyId === `cancel:${detail.id}`"
              @click="emit('cancel', detail.id)"
            >
              {{ isZh ? '取消任务' : 'Cancel Task' }}
            </Button>
            <Button
              v-if="canRetry"
              data-testid="task-paid-retry-btn"
              variant="primary"
              size="xs"
              @click="emit('open-retry', { taskId: detail.id, preselectedItemIds: retryableItemIds })"
            >
              {{ isZh ? '付费重试' : 'Paid Retry' }}
            </Button>
          </div>
        </div>

        <!-- Disabled Action Explanations -->
        <div
          v-if="(!canCancel && cancelDisabledReason) || (!canRetry && retryDisabledReason)"
          data-testid="task-action-reasons"
          class="flex flex-wrap gap-2 font-mono text-[11px] text-[var(--oh-ink-tertiary,var(--muted-foreground))]"
        >
          <span
            v-if="!canCancel && cancelDisabledReason"
            class="rounded border border-[var(--oh-border,var(--border))] px-2 py-0.5"
          >
            cancel: {{ cancelDisabledReason }}
          </span>
          <span
            v-if="!canRetry && retryDisabledReason"
            class="rounded border border-[var(--oh-border,var(--border))] px-2 py-0.5"
          >
            retry: {{ retryDisabledReason }}
          </span>
        </div>

        <!-- Honest Progress Section: Never Fake Unreported Progress -->
        <section
          data-testid="task-progress-section"
          class="space-y-3 rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] p-3.5"
        >
          <div class="flex items-center justify-between gap-2">
            <span class="font-mono text-[11px] font-semibold uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
              {{ isZh ? '执行进度与阶段' : 'Execution Progress & Lifecycle' }}
            </span>
            <span
              data-testid="task-progress-value"
              class="font-mono text-xs font-bold tabular-nums text-[var(--oh-ink,var(--foreground))]"
            >
              {{ honestProgress?.label || EM_DASH }}
              <span class="font-normal text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
                ({{ detail.progress_source || 'unreported' }})
              </span>
            </span>
          </div>

          <div
            v-if="honestProgress?.mode === 'determinate' && honestProgress.percent !== null"
            data-testid="determinate-progress-bar"
            class="h-2 overflow-hidden rounded-full bg-[var(--oh-bg-inset,var(--border))]"
          >
            <div
              class="h-full bg-[var(--oh-accent,#f59e0b)] transition-[width]"
              :style="{ width: `${honestProgress.percent}%` }"
            />
          </div>
          <p
            v-else
            data-testid="unreported-progress-note"
            class="text-xs text-[var(--oh-ink-secondary,var(--muted-foreground))]"
          >
            {{
              isZh
                ? '上游未报告百分比进度 (—)，仅展示真实生命周期阶段与子项计数，不伪造进度条。'
                : 'Upstream did not report numeric percentage (—). Showing verified lifecycle stages and exact sub-item counts.'
            }}
          </p>

          <Stepper
            :steps="lifecycleSteps"
            :current-step-id="currentStepId"
            :allow-step-click="false"
            aria-label="Task Lifecycle"
          />

          <!-- Sub-item Rollup Counts -->
          <div class="grid grid-cols-5 gap-2 border-t border-[var(--oh-border,var(--border))] pt-2.5 text-center font-mono text-xs tabular-nums">
            <div>
              <p class="text-[10px] uppercase text-[var(--oh-ink-tertiary,var(--muted-foreground))]">Total</p>
              <p class="font-semibold text-[var(--oh-ink,var(--foreground))]">{{ formatNullableInteger(detail.items.total) }}</p>
            </div>
            <div>
              <p class="text-[10px] uppercase text-[var(--oh-ink-tertiary,var(--muted-foreground))]">Succeeded</p>
              <p class="font-semibold text-[var(--oh-ink,var(--foreground))]">{{ formatNullableInteger(detail.items.succeeded) }}</p>
            </div>
            <div>
              <p class="text-[10px] uppercase text-[var(--oh-ink-tertiary,var(--muted-foreground))]">Failed</p>
              <p class="font-semibold text-[var(--oh-danger,#dc2626)]">{{ formatNullableInteger(detail.items.failed) }}</p>
            </div>
            <div>
              <p class="text-[10px] uppercase text-[var(--oh-ink-tertiary,var(--muted-foreground))]">Pending</p>
              <p class="font-semibold text-[var(--oh-ink,var(--foreground))]">{{ formatNullableInteger(detail.items.pending) }}</p>
            </div>
            <div>
              <p class="text-[10px] uppercase text-[var(--oh-ink-tertiary,var(--muted-foreground))]">Canceled</p>
              <p class="font-semibold text-[var(--oh-ink,var(--foreground))]">{{ formatNullableInteger(detail.items.canceled) }}</p>
            </div>
          </div>
        </section>

        <!-- Attribution & Billing Metadata -->
        <section class="space-y-2 rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] p-3.5 text-xs">
          <span class="font-mono text-[11px] font-semibold uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
            {{ isZh ? '计费与关联凭证' : 'Attribution & Settlement' }}
          </span>
          <dl class="grid gap-2 sm:grid-cols-2">
            <div class="flex items-center justify-between gap-2">
              <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Estimate</dt>
              <dd class="font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                {{ formatChargeObject(detail.estimate) }}
              </dd>
            </div>
            <div class="flex items-center justify-between gap-2">
              <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Actual Charge</dt>
              <dd class="font-mono font-semibold tabular-nums text-[var(--oh-accent-text,var(--brand-text,#d97706))]">
                {{ formatChargeObject(detail.actual_charge) }}
              </dd>
            </div>
            <div class="flex items-center justify-between gap-2">
              <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Request ID</dt>
              <dd class="flex items-center gap-1 font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                <span class="max-w-[130px] truncate">{{ detail.request_id || EM_DASH }}</span>
                <CopyButton v-if="detail.request_id" :value="detail.request_id" size="xs" />
              </dd>
            </div>
            <div class="flex items-center justify-between gap-2">
              <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Settlement ID</dt>
              <dd class="flex items-center gap-1 font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">
                <span class="max-w-[130px] truncate">{{ detail.settlement_id || EM_DASH }}</span>
                <CopyButton v-if="detail.settlement_id" :value="detail.settlement_id" size="xs" />
              </dd>
            </div>
            <div v-if="detail.parent_task_id" class="flex items-center justify-between gap-2 sm:col-span-2">
              <dt class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Parent Task</dt>
              <dd>
                <button
                  type="button"
                  class="oh-focus-ring rounded font-mono text-xs tabular-nums text-[var(--oh-accent-text,var(--brand-text,#d97706))] underline"
                  @click="emit('select-task', detail.parent_task_id!)"
                >
                  {{ detail.parent_task_id }}
                </button>
              </dd>
            </div>
          </dl>
        </section>

        <!-- Artifacts Section: Explicit Available / Expired / Revoked -->
        <section
          data-testid="task-artifacts-section"
          class="space-y-2.5 rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] p-3.5"
        >
          <div class="flex items-center justify-between">
            <span class="font-mono text-[11px] font-semibold uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
              {{ isZh ? '任务产物 (Artifacts)' : 'Task Artifacts' }} ({{ detail.artifacts.length }})
            </span>
          </div>

          <p
            v-if="detail.artifacts.length === 0"
            class="py-3 text-xs text-[var(--oh-ink-secondary,var(--muted-foreground))]"
          >
            {{
              detail.artifact_state === 'expired'
                ? isZh
                  ? '产物保留期已过（已过期）。'
                  : 'Artifacts have passed the retention window (Expired).'
                : detail.artifact_state === 'revoked'
                  ? isZh
                    ? '产物已被撤销。'
                    : 'Artifacts were explicitly revoked.'
                  : isZh
                    ? '当前尚无已归档产物 (—)'
                    : 'No archived artifacts available yet (—)'
            }}
          </p>

          <ul v-else class="space-y-2">
            <li
              v-for="artifact in detail.artifacts"
              :key="artifact.id"
              :data-testid="`artifact-row-${artifact.id}`"
              class="rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] p-3"
            >
              <div class="flex flex-wrap items-center justify-between gap-2">
                <div class="min-w-0 space-y-1">
                  <div class="flex flex-wrap items-center gap-2 font-mono text-xs tabular-nums">
                    <span class="font-semibold text-[var(--oh-ink,var(--foreground))]">{{ artifact.id }}</span>
                    <Badge
                      :data-testid="`artifact-badge-${artifact.id}`"
                      :tone="classifyTaskArtifact(artifact, detail.allowed_actions, effectiveNowMs).tone"
                      variant="subtle"
                      mono
                    >
                      {{
                        isZh
                          ? classifyTaskArtifact(artifact, detail.allowed_actions, effectiveNowMs).badgeTextZh
                          : classifyTaskArtifact(artifact, detail.allowed_actions, effectiveNowMs).badgeTextEn
                      }}
                    </Badge>
                  </div>
                  <p class="font-mono text-[11px] tabular-nums text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                    {{ artifact.content_type || EM_DASH }} · {{ formatNullableBytes(artifact.size_bytes) }} ·
                    {{
                      isZh
                        ? classifyTaskArtifact(artifact, detail.allowed_actions, effectiveNowMs).countdown.labelZh
                        : classifyTaskArtifact(artifact, detail.allowed_actions, effectiveNowMs).countdown.labelEn
                    }}
                  </p>
                </div>

                <div class="flex items-center gap-1.5">
                  <Button
                    :data-testid="`download-artifact-${artifact.id}`"
                    variant="secondary"
                    size="xs"
                    :loading="actionBusyId === `download:${artifact.id}`"
                    :disabled="!classifyTaskArtifact(artifact, detail.allowed_actions, effectiveNowMs).canDownload"
                    @click="emit('download-artifact', artifact)"
                  >
                    {{ isZh ? '票据下载' : 'Download' }}
                  </Button>
                  <Button
                    v-if="classifyTaskArtifact(artifact, detail.allowed_actions, effectiveNowMs).canRevoke"
                    :data-testid="`revoke-artifact-${artifact.id}`"
                    variant="danger"
                    size="xs"
                    :loading="actionBusyId === `revoke:${artifact.id}`"
                    @click="emit('revoke-artifact', artifact)"
                  >
                    {{ isZh ? '撤销产物' : 'Revoke' }}
                  </Button>
                </div>
              </div>

              <p
                v-if="classifyTaskArtifact(artifact, detail.allowed_actions, effectiveNowMs).effectiveState === 'expired'"
                :data-testid="`artifact-expired-reason-${artifact.id}`"
                class="mt-2 text-[11px] text-[var(--oh-ink-secondary,var(--muted-foreground))]"
              >
                {{
                  isZh
                    ? '该产物已超过 24 小时保留窗口，下载票据不再签发。'
                    : 'Artifact retention window has elapsed; download tickets can no longer be issued.'
                }}
              </p>
              <p
                v-else-if="classifyTaskArtifact(artifact, detail.allowed_actions, effectiveNowMs).effectiveState === 'revoked'"
                :data-testid="`artifact-revoked-reason-${artifact.id}`"
                class="mt-2 text-[11px] text-[var(--oh-danger,#dc2626)]"
              >
                {{
                  isZh
                    ? '该产物已被显式撤销，所有已签发下载票据立即失效。'
                    : 'Artifact has been revoked; all outstanding download tickets are permanently disabled.'
                }}
              </p>
            </li>
          </ul>
        </section>

        <!-- Sub-Items List -->
        <section v-if="items.length > 0" class="space-y-2 rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] p-3.5">
          <span class="font-mono text-[11px] font-semibold uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
            {{ isZh ? '子任务明细 (Items)' : 'Sub-Items' }} ({{ items.length }})
          </span>
          <ul class="divide-y divide-[var(--oh-border,var(--border))]">
            <li
              v-for="item in items"
              :key="item.id"
              class="flex flex-wrap items-center justify-between gap-2 py-2 text-xs"
            >
              <div class="flex flex-wrap items-center gap-2 font-mono tabular-nums">
                <span class="font-semibold text-[var(--oh-ink,var(--foreground))]">#{{ item.id }}</span>
                <Badge :tone="taskStateTone(item.state)" variant="subtle" mono>{{ item.state }}</Badge>
                <span class="text-[var(--oh-ink-tertiary,var(--muted-foreground))]">{{ item.acceptance }}</span>
                <span v-if="item.reason" class="text-[var(--oh-danger,#dc2626)]">{{ item.reason }}</span>
              </div>
              <Badge :tone="item.retryable ? 'accent' : 'neutral'" variant="outline" mono>
                {{ item.retryable ? (isZh ? '支持付费重试' : 'retryable') : (isZh ? '不可重试' : 'non-retryable') }}
              </Badge>
            </li>
          </ul>
        </section>
      </template>
    </div>
  </Drawer>
</template>
