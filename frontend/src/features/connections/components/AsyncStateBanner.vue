<script setup lang="ts">
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

withDefaults(defineProps<AsyncStateBannerProps>(), {
  error: null,
  emptyTitle: 'No items available',
  emptyDescription: undefined,
  emptyActionLabel: undefined,
  confirmedLabel: 'Changes saved',
  confirmedVersion: null
})

const emit = defineEmits<{
  retry: []
  action: []
  clearFilters: []
  refreshStatus: []
  reloadLatest: []
}>()
</script>

<template>
  <div v-if="state === 'initial_load'" class="space-y-3" role="status" aria-label="Loading">
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
      Refreshing live state…
    </span>
    <span v-if="error?.requestId" class="font-mono tabular-nums text-[11px]">
      req: {{ error.requestId }}
    </span>
  </div>

  <EmptyState
    v-else-if="state === 'empty'"
    variant="empty"
    :title="emptyTitle"
    :description="emptyDescription"
    :action-label="emptyActionLabel"
    @action="emit('action')"
  />

  <EmptyState
    v-else-if="state === 'zero_filter'"
    variant="filtered"
    :title="emptyTitle"
    :description="emptyDescription"
    @clear-filters="emit('clearFilters')"
  />

  <ErrorState
    v-else-if="state === 'permission_denied'"
    variant="permission"
    title="Permission denied (403)"
    :message="error?.message || 'Your role cannot perform this connection action.'"
    :code="403"
    :required-role="error?.requiredRole || 'Workspace Admin'"
    :request-id="error?.requestId"
    :retryable="false"
  />

  <ErrorState
    v-else-if="state === 'version_conflict'"
    variant="error"
    title="Version conflict (409)"
    :message="error?.message || 'Another operator modified this connection. Reload the latest version before saving.'"
    :code="409"
    :request-id="error?.requestId"
    retry-label="Reload Latest Version"
    @retry="emit('reloadLatest')"
  />

  <ErrorState
    v-else-if="state === 'stale'"
    variant="stale"
    title="Verification or preview is stale"
    :message="error?.message || 'Configuration changed after the last check. Re-run verification to confirm.'"
    :code="error?.status"
    :request-id="error?.requestId"
    retry-label="Re-run Check"
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
          <Badge tone="warning" variant="subtle">Outcome Unknown</Badge>
          <span v-if="error?.requestId" class="font-mono text-xs tabular-nums text-oh-muted">
            req: {{ error.requestId }}
          </span>
        </div>
        <p class="text-sm font-medium text-oh-fg">
          {{ error?.message || 'The request timed out before confirmation. Check operation status before retrying.' }}
        </p>
      </div>
      <Button size="sm" variant="secondary" @click="emit('refreshStatus')">
        Check Operation Status
      </Button>
    </div>
  </div>

  <ErrorState
    v-else-if="state === 'partial_failure'"
    variant="error"
    title="Operation partially failed or rejected"
    :message="error?.message || 'One or more checks failed. Review details and retry.'"
    :code="error?.status"
    :request-id="error?.requestId"
    retry-label="Retry"
    @retry="emit('retry')"
  />

  <div
    v-else-if="state === 'confirmed'"
    class="flex flex-wrap items-center justify-between gap-2 rounded-oh-md border border-oh-default bg-oh-surface px-3.5 py-2.5"
    role="status"
  >
    <div class="flex items-center gap-2">
      <Badge tone="success" variant="subtle">Confirmed</Badge>
      <span class="text-sm text-oh-fg">{{ confirmedLabel }}</span>
    </div>
    <span v-if="confirmedVersion" class="font-mono text-xs tabular-nums text-oh-muted">
      version {{ confirmedVersion }}
    </span>
  </div>
</template>
