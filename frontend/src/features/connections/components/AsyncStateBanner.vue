<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Badge, Button, EmptyState, ErrorState, Skeleton } from '@/components/ui'
import type { AsyncErrorContext, AsyncPageState } from '../types'

export interface AsyncStateBannerProps {
  state: AsyncPageState
  error?: AsyncErrorContext | null
  emptyTitle?: string
  emptyDescription?: string
  emptyActionLabel?: string
  confirmedLabel?: string
  confirmedVersion?: string | null
}

const props = withDefaults(defineProps<AsyncStateBannerProps>(), {
  error: null,
  emptyTitle: undefined,
  emptyDescription: undefined,
  emptyActionLabel: undefined,
  confirmedLabel: undefined,
  confirmedVersion: null
})

const emit = defineEmits<{
  retry: []
  action: []
  clearFilters: []
  refreshStatus: []
  reloadLatest: []
}>()

const { locale } = useI18n()
const isZh = computed(() => String(locale?.value || '').toLowerCase().startsWith('zh'))

const resolvedEmptyTitle = computed(
  () => props.emptyTitle || (isZh.value ? '暂无可用记录' : 'No items available')
)
const resolvedConfirmedLabel = computed(
  () => props.confirmedLabel || (isZh.value ? '配置已保存' : 'Changes saved')
)
</script>

<template>
  <div
    v-if="state === 'initial_load'"
    class="space-y-3"
    role="status"
    :aria-label="isZh ? '加载中' : 'Loading'"
  >
    <Skeleton height="56px" />
    <Skeleton height="120px" />
    <Skeleton height="56px" />
  </div>

  <div
    v-else-if="state === 'background_refresh'"
    class="flex items-center justify-between rounded-oh-md border border-oh-default bg-oh-surface px-3 py-2 text-xs text-oh-muted"
    role="status"
  >
    <span class="flex items-center gap-2">
      <span class="h-2 w-2 rounded-full bg-oh-accent animate-pulse" aria-hidden="true"></span>
      {{ isZh ? '正在同步最新状态…' : 'Refreshing live state…' }}
    </span>
    <span v-if="error?.requestId" class="font-mono tabular-nums text-[11px]">
      {{ isZh ? '请求 ID' : 'Req' }}: {{ error.requestId }}
    </span>
  </div>

  <EmptyState
    v-else-if="state === 'empty'"
    variant="empty"
    :title="resolvedEmptyTitle"
    :description="emptyDescription"
    :action-label="emptyActionLabel"
    @action="emit('action')"
  />

  <EmptyState
    v-else-if="state === 'zero_filter'"
    variant="filtered"
    :title="resolvedEmptyTitle"
    :description="emptyDescription"
    @clear-filters="emit('clearFilters')"
  />

  <ErrorState
    v-else-if="state === 'permission_denied'"
    variant="permission"
    :title="isZh ? '权限不足 (403)' : 'Permission denied (403)'"
    :message="
      error?.message ||
      (isZh ? '当前角色无权执行此连接操作。' : 'Your role cannot perform this connection action.')
    "
    :code="403"
    :required-role="error?.requiredRole || (isZh ? '工作空间管理员' : 'Workspace Admin')"
    :request-id="error?.requestId"
    :retryable="false"
  />

  <ErrorState
    v-else-if="state === 'version_conflict'"
    variant="error"
    :title="isZh ? '配置修订冲突 (409)' : 'Revision conflict (409)'"
    :message="
      error?.message ||
      (isZh
        ? '其他管理员已更新此连接配置，请先重新加载最新修订后再保存。'
        : 'Another operator modified this connection. Reload the latest revision before saving.')
    "
    :code="409"
    :request-id="error?.requestId"
    :retry-label="isZh ? '重新加载最新修订' : 'Reload Latest Revision'"
    @retry="emit('reloadLatest')"
  />

  <ErrorState
    v-else-if="state === 'stale'"
    variant="stale"
    :title="isZh ? '验证或预览结果已过期' : 'Verification or preview is stale'"
    :message="
      error?.message ||
      (isZh
        ? '上次探测后连接配置已发生变更，请重新执行验证以确认连通性。'
        : 'Configuration changed after the last check. Re-run verification to confirm.')
    "
    :code="error?.status"
    :request-id="error?.requestId"
    :retry-label="isZh ? '重新执行检查' : 'Re-run Check'"
    @retry="emit('retry')"
  />

  <div
    v-else-if="state === 'unknown_outcome'"
    class="rounded-oh-lg border border-oh-warning bg-oh-surface p-4"
    role="alert"
  >
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div class="space-y-1">
        <div class="flex items-center gap-2">
          <Badge tone="warning" variant="subtle">
            {{ isZh ? '结果待确认' : 'Outcome Unknown' }}
          </Badge>
          <span v-if="error?.requestId" class="font-mono text-xs tabular-nums text-oh-muted">
            {{ isZh ? '请求 ID' : 'Req' }}: {{ error.requestId }}
          </span>
        </div>
        <p class="text-sm font-medium text-oh-fg">
          {{
            error?.message ||
            (isZh
              ? '请求在收到确认前超时，请先查询操作状态，避免重复提交。'
              : 'The request timed out before confirmation. Check operation status before retrying.')
          }}
        </p>
      </div>
      <Button size="sm" variant="secondary" @click="emit('refreshStatus')">
        {{ isZh ? '查询操作状态' : 'Check Operation Status' }}
      </Button>
    </div>
  </div>

  <div v-else-if="state === 'partial_failure'" class="space-y-2">
    <ErrorState
      variant="error"
      :title="
        error?.fields?.length
          ? isZh
            ? '配置字段校验未通过'
            : 'Field validation failed'
          : isZh
            ? '操作未通过或被拒绝'
            : 'Operation failed or rejected'
      "
      :message="
        error?.message ||
        (isZh ? '一项或多项检查未通过，请查看详情后重试。' : 'One or more checks failed. Review details and retry.')
      "
      :code="error?.status || undefined"
      :request-id="error?.requestId"
      :retry-label="isZh ? '重试' : 'Retry'"
      @retry="emit('retry')"
    />
    <ul
      v-if="error?.fields && error.fields.length > 0"
      class="space-y-1.5 rounded-oh-md border border-oh-status-danger-border bg-oh-surface px-3.5 py-2.5 text-xs"
    >
      <li
        v-for="field in error.fields"
        :key="field.path"
        class="flex flex-wrap items-baseline gap-1.5 text-oh-fg"
      >
        <span class="font-semibold">{{ isZh ? field.labelZh : field.labelEn }}:</span>
        <span class="text-oh-secondary">{{ isZh ? field.hintZh : field.hintEn }}</span>
      </li>
    </ul>
  </div>

  <div
    v-else-if="state === 'confirmed'"
    class="flex flex-wrap items-center justify-between gap-2 rounded-oh-md border border-oh-default bg-oh-surface px-3.5 py-2.5"
    role="status"
  >
    <div class="flex items-center gap-2">
      <Badge tone="success" variant="subtle">{{ isZh ? '已确认' : 'Confirmed' }}</Badge>
      <span class="text-sm text-oh-fg">{{ resolvedConfirmedLabel }}</span>
    </div>
    <span v-if="confirmedVersion" class="font-mono text-xs tabular-nums text-oh-muted">
      {{ isZh ? `配置修订 #${confirmedVersion}` : `Revision #${confirmedVersion}` }}
    </span>
  </div>
</template>
