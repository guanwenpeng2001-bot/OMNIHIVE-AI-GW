<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ConfigAuthority, ConfigSource, ConfigState, EffectiveConfigItem } from '@/api/admin/config'
import type { SystemSettings } from '@/api/admin/settings'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import {
  Badge,
  Button,
  Card,
  EmptyState,
  ErrorState,
  Input,
  Modal,
  PageHeader,
  Select,
  Skeleton,
  pushUiToast,
  type UiSelectOption,
  type UiTone
} from '@/components/ui'
import {
  canResetConfigItem,
  formatBaselinePreviewToken,
  formatDiagnosticValueToken,
  isAtomicResetGroupItem,
  isNewUserSeededKey,
  isSeedProvenanceKey,
  resolveEffectiveSeedSource,
  useConfigDiagnostics,
  type DiagnosticsSyncSnapshot,
  type FormattedDiagnosticToken
} from './useConfigDiagnostics'

const props = withDefaults(
  defineProps<{
    embedded?: boolean
  }>(),
  {
    embedded: false
  }
)

const emit = defineEmits<{
  'sync-state-change': [snapshot: DiagnosticsSyncSnapshot]
  'settings-updated': [settings: SystemSettings]
}>()

const { t } = useI18n()

const {
  filters,
  items,
  itemsByKey,
  configFile,
  generatedAt,
  nextCursor,
  hasLoadedOnce,
  loading,
  refreshing,
  loadingMore,
  resetting,
  loadError,
  refreshError,
  resetError,
  pendingResetTarget,
  resetPreviewItems,
  resetRequiresRestart,
  hasActiveFilters,
  unhealthyItems,
  availableGroups,
  syncState,
  stepUp,
  loadFirstPage,
  loadMore,
  clearFiltersAndReload,
  openResetConfirm,
  closeResetConfirm,
  confirmReset
} = useConfigDiagnostics({
  onSyncChange: (snapshot) => emit('sync-state-change', snapshot),
  onSettingsUpdated: (settings) => emit('settings-updated', settings)
})

let searchDebounceTimer: ReturnType<typeof setTimeout> | null = null

function clearSearchTimer(): void {
  if (searchDebounceTimer !== null) {
    clearTimeout(searchDebounceTimer)
    searchDebounceTimer = null
  }
}

watch(
  () => [filters.group, filters.source, filters.state],
  () => {
    clearSearchTimer()
    void loadFirstPage(false)
  }
)

watch(
  () => filters.q,
  () => {
    clearSearchTimer()
    searchDebounceTimer = setTimeout(() => {
      searchDebounceTimer = null
      void loadFirstPage(false)
    }, 250)
  }
)

onMounted(() => {
  void loadFirstPage(false)
})

onBeforeUnmount(() => {
  clearSearchTimer()
})

function groupLabel(group: string): string {
  switch (group) {
    case 'system':
      return t('settingsDiagnostics.groups.system')
    case 'security':
      return t('settingsDiagnostics.groups.security')
    case 'onboarding':
      return t('settingsDiagnostics.groups.onboarding')
    case 'gateway':
      return t('settingsDiagnostics.groups.gateway')
    case 'email':
      return t('settingsDiagnostics.groups.email')
    case 'plaza':
      return t('settingsDiagnostics.groups.plaza')
    case 'ops':
      return t('settingsDiagnostics.groups.ops')
    case 'catalog':
      return t('settingsDiagnostics.groups.catalog')
    case 'logging':
      return t('settingsDiagnostics.groups.logging')
    case 'image_storage':
      return t('settingsDiagnostics.groups.image_storage')
    case 'backup':
      return t('settingsDiagnostics.groups.backup')
    case 'linuxdo_connect':
      return t('settingsDiagnostics.groups.linuxdo_connect')
    case 'dingtalk_connect':
      return t('settingsDiagnostics.groups.dingtalk_connect')
    case 'wechat_connect':
      return t('settingsDiagnostics.groups.wechat_connect')
    case 'oidc_connect':
      return t('settingsDiagnostics.groups.oidc_connect')
    case 'github_oauth':
      return t('settingsDiagnostics.groups.github_oauth')
    case 'google_oauth':
      return t('settingsDiagnostics.groups.google_oauth')
    default:
      return group
  }
}

function sourceLabel(source: ConfigSource | string): string {
  switch (source) {
    case 'database':
      return t('settingsDiagnostics.sources.database')
    case 'environment':
      return t('settingsDiagnostics.sources.environment')
    case 'yaml':
      return t('settingsDiagnostics.sources.yaml')
    case 'default':
      return t('settingsDiagnostics.sources.default')
    case 'derived':
      return t('settingsDiagnostics.sources.derived')
    case 'artifact':
      return t('settingsDiagnostics.sources.artifact')
    default:
      return source
  }
}

function sourceTone(source: ConfigSource | string): UiTone {
  switch (source) {
    case 'database':
      return 'accent'
    case 'environment':
      return 'info'
    case 'yaml':
      return 'warning'
    case 'derived':
      return 'info'
    case 'artifact':
      return 'success'
    default:
      return 'neutral'
  }
}

function authorityLabel(authority: ConfigAuthority | string): string {
  switch (authority) {
    case 'deployment':
      return t('settingsDiagnostics.authorities.deployment')
    case 'database_override':
      return t('settingsDiagnostics.authorities.database_override')
    case 'database_only':
      return t('settingsDiagnostics.authorities.database_only')
    case 'versioned_artifact':
      return t('settingsDiagnostics.authorities.versioned_artifact')
    default:
      return authority
  }
}

function stateLabel(state: ConfigState | string): string {
  switch (state) {
    case 'effective':
      return t('settingsDiagnostics.states.effective')
    case 'invalid':
      return t('settingsDiagnostics.states.invalid')
    case 'unavailable':
      return t('settingsDiagnostics.states.unavailable')
    case 'blocked_by_hard_gate':
      return t('settingsDiagnostics.states.blocked_by_hard_gate')
    default:
      return state
  }
}

function stateTone(item: EffectiveConfigItem): UiTone {
  if (item.state === 'invalid' || item.state === 'unavailable') {
    return 'danger'
  }
  if (item.state === 'blocked_by_hard_gate') {
    return item.error_code === 'TURNSTILE_REQUIRED_NOT_CONFIGURED' ? 'danger' : 'warning'
  }
  return 'success'
}

function seedSourceLabel(seedSource: string): string {
  switch (seedSource) {
    case 'install_seed':
      return t('settingsDiagnostics.seedSources.install_seed')
    case 'environment':
      return t('settingsDiagnostics.seedSources.environment')
    case 'yaml':
      return t('settingsDiagnostics.seedSources.yaml')
    case 'default':
      return t('settingsDiagnostics.seedSources.default')
    case 'admin':
      return t('settingsDiagnostics.seedSources.admin')
    case 'unknown':
      return t('settingsDiagnostics.seedSources.unknown')
    default:
      return seedSource
  }
}

function nonResettableReason(item: EffectiveConfigItem): string {
  if (item.source === 'derived') {
    return t('settingsDiagnostics.reset.notResettableDerived')
  }
  switch (item.authority) {
    case 'deployment':
      return t('settingsDiagnostics.reset.notResettableDeployment')
    case 'database_only':
      return t('settingsDiagnostics.reset.notResettableDbOnly')
    case 'versioned_artifact':
      return t('settingsDiagnostics.reset.notResettableArtifact')
    default:
      return t('settingsDiagnostics.reset.notResettableDeployment')
  }
}

function secretStatusText(configured: boolean | null | undefined): string {
  if (configured === true) {
    return t('settingsDiagnostics.values.secretConfigured')
  }
  if (configured === false) {
    return t('settingsDiagnostics.values.secretNotConfigured')
  }
  return t('settingsDiagnostics.values.secretUnknown')
}

function secretStatusTone(configured: boolean | null | undefined): UiTone {
  if (configured === true) return 'info'
  if (configured === false) return 'neutral'
  return 'unknown'
}

function renderBaselineTokenText(token: FormattedDiagnosticToken): string {
  if (token.kind === 'secret') {
    return t('settingsDiagnostics.reset.secretBaselineRedacted')
  }
  if (token.kind === 'empty-string') {
    return t('settingsDiagnostics.values.emptyString')
  }
  if (token.kind === 'null') {
    return t('settingsDiagnostics.reset.baselineUnavailable')
  }
  return token.text ?? '—'
}

function renderCurrentPreviewText(item: EffectiveConfigItem): string {
  const token = formatDiagnosticValueToken(item)
  if (token.kind === 'secret') {
    return secretStatusText(token.configured)
  }
  if (token.kind === 'empty-string') {
    return t('settingsDiagnostics.values.emptyString')
  }
  if (token.kind === 'unavailable') {
    return t('settingsDiagnostics.values.unavailableValue')
  }
  if (token.kind === 'null') {
    return 'null'
  }
  return token.text ?? '—'
}

function gateExplanation(item: EffectiveConfigItem): string {
  if (item.error_code === 'TURNSTILE_REQUIRED_NOT_CONFIGURED') {
    return t('settingsDiagnostics.gates.turnstileRequiredIncomplete')
  }
  if (item.state === 'invalid') {
    return t('settingsDiagnostics.gates.invalidHint', {
      code: item.error_code || 'CONFIG_INVALID'
    })
  }
  if (item.state === 'unavailable') {
    return t('settingsDiagnostics.gates.unavailableHint', {
      code: item.error_code || 'CONFIG_UNAVAILABLE'
    })
  }
  if (item.key === 'passkey_enabled' || item.key === 'passkey.effective_enabled') {
    return t('settingsDiagnostics.gates.passkeyGateHint')
  }
  if (item.key === 'ops_monitoring_enabled' || item.key === 'ops.effective_enabled') {
    return t('settingsDiagnostics.gates.opsGateHint')
  }
  if (item.state === 'blocked_by_hard_gate') {
    return t('settingsDiagnostics.gates.blockedByKey', {
      blockedBy: item.blocked_by || '—'
    })
  }
  return t('settingsDiagnostics.gates.healthyHint')
}

const groupOptions = computed<UiSelectOption[]>(() => [
  { value: '', label: t('settingsDiagnostics.filters.allGroups') },
  ...availableGroups.value.map((group) => ({
    value: group,
    label: groupLabel(group)
  }))
])

const sourceOptions = computed<UiSelectOption[]>(() => [
  { value: '', label: t('settingsDiagnostics.filters.allSources') },
  { value: 'database', label: sourceLabel('database') },
  { value: 'environment', label: sourceLabel('environment') },
  { value: 'yaml', label: sourceLabel('yaml') },
  { value: 'default', label: sourceLabel('default') },
  { value: 'derived', label: sourceLabel('derived') },
  { value: 'artifact', label: sourceLabel('artifact') }
])

const stateOptions = computed<UiSelectOption[]>(() => [
  { value: '', label: t('settingsDiagnostics.filters.allStates') },
  { value: 'effective', label: stateLabel('effective') },
  { value: 'blocked_by_hard_gate', label: stateLabel('blocked_by_hard_gate') },
  { value: 'invalid', label: stateLabel('invalid') },
  { value: 'unavailable', label: stateLabel('unavailable') }
])

const resetModalDescription = computed(() => {
  const target = pendingResetTarget.value
  if (!target) return ''
  if (isAtomicResetGroupItem(target)) {
    return t('settingsDiagnostics.reset.modalDescriptionGroup', {
      key: target.key,
      group: groupLabel(target.group)
    })
  }
  return t('settingsDiagnostics.reset.modalDescriptionSingle', {
    key: target.key
  })
})

const resetErrorDisplayMessage = computed(() => {
  const err = resetError.value
  if (!err) return ''
  if (err.stepUpBlockedReason === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN') {
    return t('stepUp.adminApiKeyForbidden')
  }
  if (err.stepUpBlockedReason) {
    return t('stepUp.notEnabled')
  }
  return err.message
})

async function handleConfirmReset(): Promise<void> {
  const result = await confirmReset()
  if (!result) return
  pushUiToast({
    tone: 'success',
    title: t('settingsDiagnostics.reset.successToastTitle'),
    description: t('settingsDiagnostics.reset.successToastBody', {
      count: result.items.length
    })
  })
}

defineExpose({
  reload: () => loadFirstPage(true),
  syncState
})
</script>

<template>
  <div class="space-y-4" data-testid="config-diagnostics-view">
    <PageHeader
      v-if="!props.embedded"
      :eyebrow="t('settingsDiagnostics.eyebrow')"
      :title="t('settingsDiagnostics.title')"
      :description="t('settingsDiagnostics.description')"
    >
      <template #badges>
        <Badge
          v-if="syncState === 'loading' || syncState === 'refreshing'"
          tone="neutral"
          variant="subtle"
          data-testid="diagnostics-sync-badge"
        >
          {{ t('settingsDiagnostics.syncBadges.loading') }}
        </Badge>
        <Badge
          v-else-if="syncState === 'error'"
          tone="danger"
          variant="subtle"
          data-testid="diagnostics-sync-badge"
        >
          {{ t('settingsDiagnostics.syncBadges.notSynced') }}
        </Badge>
        <Badge
          v-else-if="syncState === 'stale'"
          tone="warning"
          variant="subtle"
          data-testid="diagnostics-sync-badge"
        >
          {{ t('settingsDiagnostics.syncBadges.stale') }}
        </Badge>
        <Badge
          v-else
          tone="success"
          variant="subtle"
          data-testid="diagnostics-sync-badge"
        >
          {{ t('settingsDiagnostics.syncBadges.synced') }}
        </Badge>

        <Badge
          v-if="hasLoadedOnce && unhealthyItems.length > 0"
          tone="danger"
          variant="subtle"
          mono
          data-testid="diagnostics-issues-badge"
        >
          {{ t('settingsDiagnostics.syncBadges.issuesCount', { count: unhealthyItems.length }) }}
        </Badge>
      </template>

      <template #actions>
        <Button
          size="sm"
          variant="secondary"
          :loading="refreshing || loading"
          data-testid="diagnostics-refresh-btn"
          @click="loadFirstPage(true)"
        >
          {{ t('settingsDiagnostics.actions.refresh') }}
        </Button>
      </template>
    </PageHeader>

    <!-- Deployment metadata & hard-vs-soft gate guide -->
    <Card padding="sm">
      <div class="flex flex-col gap-3">
        <div class="flex flex-wrap items-center justify-between gap-2 border-b border-oh-subtle pb-2.5 text-xs">
          <div class="flex flex-wrap items-center gap-2">
            <span class="font-medium text-oh-secondary">
              {{ t('settingsDiagnostics.meta.configFileLabel') }}:
            </span>
            <code
              class="rounded-oh-xs border border-oh-default bg-oh-subtle px-1.5 py-0.5 font-mono text-[11px] text-oh-primary"
              data-testid="diagnostics-config-file"
            >
              {{ configFile || t('settingsDiagnostics.meta.configFileUnset') }}
            </code>
          </div>
          <div v-if="generatedAt" class="flex items-center gap-1.5 text-oh-muted">
            <span>{{ t('settingsDiagnostics.meta.generatedAtLabel') }}:</span>
            <span class="font-mono tabular-nums" data-testid="diagnostics-generated-at">
              {{ generatedAt }}
            </span>
          </div>
        </div>

        <div class="rounded-oh-md border border-oh-default bg-oh-subtle/50 px-3 py-2 text-xs leading-5 text-oh-secondary">
          <span class="font-semibold text-oh-primary">
            {{ t('settingsDiagnostics.gates.guideTitle') }}:
          </span>
          {{ t('settingsDiagnostics.gates.guideBody') }}
        </div>

        <!-- Filter controls -->
        <div class="grid grid-cols-1 gap-3 pt-1 sm:grid-cols-2 lg:grid-cols-4">
          <Input
            :model-value="filters.q"
            type="search"
            size="sm"
            clearable
            :label="t('settingsDiagnostics.filters.searchAriaLabel')"
            :placeholder="t('settingsDiagnostics.filters.searchPlaceholder')"
            data-testid="diagnostics-search-input"
            @update:model-value="(val) => (filters.q = String(val ?? ''))"
          />
          <Select
            :model-value="filters.group"
            size="sm"
            :label="t('settingsDiagnostics.filters.groupLabel')"
            :options="groupOptions"
            data-testid="diagnostics-group-select"
            @update:model-value="(val) => (filters.group = String(val ?? ''))"
          />
          <Select
            :model-value="filters.source"
            size="sm"
            :label="t('settingsDiagnostics.filters.sourceLabel')"
            :options="sourceOptions"
            data-testid="diagnostics-source-select"
            @update:model-value="(val) => (filters.source = (val ? String(val) : '') as ConfigSource | '')"
          />
          <Select
            :model-value="filters.state"
            size="sm"
            :label="t('settingsDiagnostics.filters.stateLabel')"
            :options="stateOptions"
            data-testid="diagnostics-state-select"
            @update:model-value="(val) => (filters.state = (val ? String(val) : '') as ConfigState | '')"
          />
        </div>

        <div v-if="hasActiveFilters" class="flex justify-end">
          <Button
            size="xs"
            variant="ghost"
            data-testid="diagnostics-clear-filters"
            @click="clearFiltersAndReload"
          >
            {{ t('settingsDiagnostics.actions.clearFilters') }}
          </Button>
        </div>
      </div>
    </Card>

    <!-- Initial loading skeleton -->
    <div v-if="!hasLoadedOnce && loading" class="space-y-3" data-testid="diagnostics-loading">
      <Skeleton height="3rem" />
      <Skeleton height="14rem" />
    </div>

    <!-- Initial load failure — never pretends synced or substitutes defaults -->
    <ErrorState
      v-else-if="!hasLoadedOnce && loadError"
      :variant="loadError.state === 'forbidden' ? 'permission' : 'error'"
      :title="
        loadError.state === 'forbidden'
          ? t('settingsDiagnostics.alerts.forbiddenTitle')
          : t('settingsDiagnostics.alerts.loadFailedTitle')
      "
      :message="loadError.message"
      :request-id="loadError.requestId"
      :retry-label="t('settingsDiagnostics.actions.retry')"
      data-testid="diagnostics-load-error"
      @retry="loadFirstPage(false)"
    />

    <template v-else>
      <!-- Refresh / pagination failure banner (stale warning, never claims synced) -->
      <ErrorState
        v-if="refreshError"
        variant="stale"
        compact
        :title="t('settingsDiagnostics.alerts.staleTitle')"
        :message="refreshError.message"
        :request-id="refreshError.requestId"
        :last-synced-at="generatedAt || undefined"
        :retry-label="t('settingsDiagnostics.actions.retry')"
        data-testid="diagnostics-stale-error"
        @retry="loadFirstPage(true)"
      />

      <!-- Unhealthy configuration / storage banner -->
      <div
        v-if="unhealthyItems.length > 0"
        role="alert"
        data-testid="diagnostics-unhealthy-banner"
        class="rounded-oh-lg border border-oh-status-danger-border bg-oh-status-danger-bg/60 p-4 text-xs"
      >
        <div class="font-semibold text-oh-primary">
          {{ t('settingsDiagnostics.alerts.unhealthyBannerTitle', { count: unhealthyItems.length }) }}
        </div>
        <p class="mt-1 text-oh-secondary">
          {{ t('settingsDiagnostics.alerts.unhealthyBannerBody') }}
        </p>
      </div>

      <!-- Empty / zero-filter state -->
      <EmptyState
        v-if="items.length === 0"
        :variant="hasActiveFilters ? 'filtered' : 'empty'"
        :title="
          hasActiveFilters
            ? t('settingsDiagnostics.alerts.filteredEmptyTitle')
            : t('settingsDiagnostics.alerts.emptyTitle')
        "
        :description="
          hasActiveFilters
            ? t('settingsDiagnostics.alerts.filteredEmptyDescription')
            : undefined
        "
        :clear-filter-label="t('settingsDiagnostics.actions.clearFilters')"
        data-testid="diagnostics-empty-state"
        @clear-filters="clearFiltersAndReload"
      />

      <!-- Diagnostics table -->
      <Card v-else padding="none" class="overflow-hidden">
        <div class="overflow-x-auto">
          <table
            class="min-w-full divide-y divide-oh-subtle text-left text-xs"
            data-testid="config-diagnostics-table"
          >
            <thead class="bg-oh-subtle/70 text-[11px] font-semibold uppercase tracking-wide text-oh-secondary">
              <tr>
                <th scope="col" class="px-4 py-3">{{ t('settingsDiagnostics.columns.setting') }}</th>
                <th scope="col" class="px-4 py-3">{{ t('settingsDiagnostics.columns.effectiveValue') }}</th>
                <th scope="col" class="px-4 py-3">{{ t('settingsDiagnostics.columns.sourceAndChain') }}</th>
                <th scope="col" class="px-4 py-3">{{ t('settingsDiagnostics.columns.stateAndGate') }}</th>
                <th scope="col" class="px-4 py-3 text-right">{{ t('settingsDiagnostics.columns.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-oh-subtle bg-oh-surface">
              <tr
                v-for="item in items"
                :key="item.key"
                :data-testid="`diag-row-${item.key}`"
                :class="[
                  'align-top transition-colors hover:bg-oh-hover/40',
                  (item.state === 'invalid' || item.state === 'unavailable') && 'bg-oh-status-danger-bg/20'
                ]"
              >
                <!-- Column 1: Key, group, authority, restart & seed metadata -->
                <td class="px-4 py-3.5">
                  <div class="font-mono text-xs font-semibold text-oh-primary break-all">
                    {{ item.key }}
                  </div>
                  <div class="mt-1.5 flex flex-wrap items-center gap-1.5">
                    <Badge tone="neutral" variant="outline" size="xs">
                      {{ groupLabel(item.group) }}
                    </Badge>
                    <Badge tone="neutral" variant="subtle" size="xs">
                      {{ authorityLabel(item.authority) }}
                    </Badge>
                    <Badge
                      v-if="item.restart_required"
                      tone="warning"
                      variant="subtle"
                      size="xs"
                    >
                      {{ t('settingsDiagnostics.values.restartRequired') }}
                    </Badge>
                    <Badge
                      v-if="isNewUserSeededKey(item.key) && resolveEffectiveSeedSource(item, itemsByKey)"
                      tone="info"
                      variant="subtle"
                      size="xs"
                      :data-testid="`seed-badge-${item.key}`"
                    >
                      {{
                        t('settingsDiagnostics.seed.badge', {
                          source: seedSourceLabel(resolveEffectiveSeedSource(item, itemsByKey))
                        })
                      }}
                    </Badge>
                  </div>

                  <p
                    v-if="isNewUserSeededKey(item.key)"
                    class="mt-1.5 max-w-md text-[11px] leading-4 text-oh-secondary"
                  >
                    {{ t('settingsDiagnostics.seed.newUsersOnlyHint') }}
                  </p>
                  <p
                    v-else-if="isSeedProvenanceKey(item.key)"
                    class="mt-1.5 max-w-md text-[11px] leading-4 text-oh-secondary"
                  >
                    {{ t('settingsDiagnostics.seed.provenanceRowHint') }}
                  </p>
                  <p
                    v-else-if="item.key === 'model_plaza_enabled'"
                    class="mt-1.5 max-w-md text-[11px] leading-4 text-oh-secondary"
                  >
                    {{ t('settingsDiagnostics.seed.plazaSeedHint') }}
                  </p>
                  <p
                    v-if="item.key === 'model_catalog_refresh_enabled'"
                    class="mt-1.5 max-w-md text-[11px] leading-4 text-oh-secondary"
                  >
                    {{ t('settingsDiagnostics.upgradeNotes.catalogRefreshOptIn') }}
                  </p>
                  <p
                    v-else-if="item.key === 'log.format'"
                    class="mt-1.5 max-w-md text-[11px] leading-4 text-oh-secondary"
                  >
                    {{ t('settingsDiagnostics.upgradeNotes.logFormatJson') }}
                  </p>
                </td>

                <!-- Column 2: Redacted-safe Effective Value -->
                <td class="px-4 py-3.5" :data-testid="`diag-value-${item.key}`">
                  <template
                    v-for="token in [formatDiagnosticValueToken(item)]"
                    :key="`${item.key}-${token.kind}`"
                  >
                    <Badge
                      v-if="token.kind === 'secret'"
                      :tone="secretStatusTone(token.configured)"
                      variant="subtle"
                    >
                      {{ secretStatusText(token.configured) }}
                    </Badge>
                    <Badge
                      v-else-if="token.kind === 'seed-provenance'"
                      tone="info"
                      variant="outline"
                    >
                      {{
                        t('settingsDiagnostics.values.seedProvenanceValue', {
                          source: seedSourceLabel(token.seedSource || '')
                        })
                      }}
                    </Badge>
                    <Badge
                      v-else-if="token.kind === 'unavailable'"
                      tone="danger"
                      variant="subtle"
                    >
                      {{ t('settingsDiagnostics.values.unavailableValue') }}
                    </Badge>
                    <span
                      v-else-if="token.kind === 'empty-string'"
                      class="font-mono text-xs text-oh-muted"
                    >
                      {{ t('settingsDiagnostics.values.emptyString') }}
                    </span>
                    <Badge
                      v-else-if="token.kind === 'bool'"
                      :tone="token.text === 'true' ? 'success' : 'neutral'"
                      variant="subtle"
                      mono
                    >
                      {{ token.text }}
                    </Badge>
                    <span
                      v-else-if="token.kind === 'null'"
                      class="font-mono text-xs text-oh-muted"
                    >
                      null
                    </span>
                    <code
                      v-else
                      class="block max-w-xs break-all font-mono text-xs text-oh-primary"
                    >
                      {{ token.text }}
                    </code>
                  </template>
                </td>

                <!-- Column 3: Active Source & Fallback Chain -->
                <td class="px-4 py-3.5" :data-testid="`diag-source-${item.key}`">
                  <div class="flex items-center gap-1.5">
                    <Badge :tone="sourceTone(item.source)" variant="subtle">
                      {{ sourceLabel(item.source) }}
                    </Badge>
                  </div>
                  <div
                    v-if="item.fallback_chain.length > 0"
                    class="mt-1.5 flex flex-wrap items-center gap-1 text-[11px] text-oh-muted"
                  >
                    <template v-for="(step, idx) in item.fallback_chain" :key="`${item.key}-${step}-${idx}`">
                      <span v-if="idx > 0" aria-hidden="true">→</span>
                      <span
                        :class="[
                          'font-mono',
                          step === item.source ? 'font-semibold text-oh-primary underline decoration-dotted' : ''
                        ]"
                      >
                        {{ step }}
                      </span>
                    </template>
                  </div>
                  <p
                    v-if="item.authority === 'database_override' && item.source === 'database'"
                    class="mt-1.5 max-w-xs text-[11px] leading-4 text-oh-secondary"
                  >
                    {{ t('settingsDiagnostics.values.overrideFrozenHint') }}
                  </p>
                </td>

                <!-- Column 4: State & Hard/Soft Gate Explanation -->
                <td class="px-4 py-3.5" :data-testid="`diag-state-${item.key}`">
                  <div class="flex flex-wrap items-center gap-1.5">
                    <Badge :tone="stateTone(item)" variant="subtle">
                      {{ stateLabel(item.state) }}
                    </Badge>
                    <code
                      v-if="item.blocked_by"
                      class="rounded-oh-xs border border-oh-default bg-oh-subtle px-1.5 py-0.5 font-mono text-[11px] text-oh-primary"
                    >
                      {{ item.blocked_by }}
                    </code>
                  </div>
                  <p class="mt-1.5 max-w-sm text-[11px] leading-4 text-oh-secondary">
                    {{ gateExplanation(item) }}
                  </p>
                </td>

                <!-- Column 5: Reset Action -->
                <td class="px-4 py-3.5 text-right">
                  <Button
                    v-if="canResetConfigItem(item)"
                    size="xs"
                    variant="secondary"
                    :data-testid="`reset-btn-${item.key}`"
                    @click="openResetConfirm(item)"
                  >
                    {{ t('settingsDiagnostics.actions.resetToBaseline') }}
                  </Button>
                  <span v-else class="text-[11px] text-oh-muted">
                    {{ nonResettableReason(item) }}
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- Pagination footer -->
        <div class="flex flex-wrap items-center justify-between gap-2 border-t border-oh-subtle bg-oh-subtle/40 px-4 py-2.5 text-xs text-oh-secondary">
          <span>{{ t('settingsDiagnostics.meta.showingCount', { count: items.length }) }}</span>
          <div v-if="nextCursor" class="flex items-center gap-2">
            <span class="text-[11px] text-oh-muted">{{ t('settingsDiagnostics.meta.moreAvailable') }}</span>
            <Button
              size="xs"
              variant="secondary"
              :loading="loadingMore"
              data-testid="diagnostics-load-more"
              @click="loadMore"
            >
              {{
                loadingMore
                  ? t('settingsDiagnostics.actions.loadingMore')
                  : t('settingsDiagnostics.actions.loadMore')
              }}
            </Button>
          </div>
        </div>
      </Card>
    </template>

    <!-- Reset-to-baseline confirmation modal (secrets never displayed) -->
    <Modal
      :open="Boolean(pendingResetTarget)"
      :title="t('settingsDiagnostics.reset.modalTitle')"
      :description="resetModalDescription"
      size="lg"
      teleport-disabled
      @close="closeResetConfirm"
      @update:open="(val) => !val && closeResetConfirm()"
    >
      <div v-if="pendingResetTarget" class="space-y-3 text-xs" data-testid="reset-confirm-modal">
        <div v-if="isAtomicResetGroupItem(pendingResetTarget)">
          <Badge tone="warning" variant="subtle">
            {{
              t('settingsDiagnostics.reset.groupScopeBadge', {
                group: groupLabel(pendingResetTarget.group)
              })
            }}
          </Badge>
        </div>

        <div class="font-medium text-oh-primary">
          {{ t('settingsDiagnostics.reset.previewHeading') }}
        </div>

        <div class="overflow-hidden rounded-oh-md border border-oh-default">
          <table class="min-w-full divide-y divide-oh-subtle text-left text-xs">
            <thead class="bg-oh-subtle/70 text-[11px] font-semibold text-oh-secondary">
              <tr>
                <th scope="col" class="px-3 py-2">{{ t('settingsDiagnostics.columns.setting') }}</th>
                <th scope="col" class="px-3 py-2">
                  {{
                    t('settingsDiagnostics.reset.currentLabel', {
                      source: sourceLabel(pendingResetTarget.source)
                    })
                  }}
                </th>
                <th scope="col" class="px-3 py-2">{{ t('settingsDiagnostics.reset.baselineLabel') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-oh-subtle bg-oh-surface">
              <tr
                v-for="previewItem in resetPreviewItems"
                :key="previewItem.key"
                :data-testid="`reset-preview-${previewItem.key}`"
              >
                <td class="px-3 py-2 font-mono font-medium text-oh-primary break-all">
                  {{ previewItem.key }}
                </td>
                <td class="px-3 py-2 font-mono text-oh-secondary break-all">
                  {{ renderCurrentPreviewText(previewItem) }}
                </td>
                <td class="px-3 py-2 font-mono text-oh-primary break-all">
                  {{ renderBaselineTokenText(formatBaselinePreviewToken(previewItem)) }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div
          v-if="resetRequiresRestart"
          class="rounded-oh-md border border-oh-status-warn-border bg-oh-status-warn-bg/50 px-3 py-2 text-oh-secondary"
        >
          {{ t('settingsDiagnostics.reset.restartNotice') }}
        </div>

        <p class="text-[11px] text-oh-muted">
          {{ t('settingsDiagnostics.reset.stepUpNotice') }}
        </p>

        <ErrorState
          v-if="resetError"
          :variant="resetError.state === 'unknown' ? 'stale' : 'error'"
          compact
          :title="
            resetError.state === 'unknown'
              ? t('settingsDiagnostics.reset.unknownOutcomeTitle')
              : t('settingsDiagnostics.reset.failedTitle')
          "
          :message="resetErrorDisplayMessage"
          :request-id="resetError.requestId"
          :retryable="false"
          data-testid="reset-confirm-error"
        />
      </div>

      <template #footer>
        <Button
          size="sm"
          variant="ghost"
          :disabled="resetting"
          data-testid="cancel-reset-btn"
          @click="closeResetConfirm"
        >
          {{ t('settingsDiagnostics.actions.cancel') }}
        </Button>
        <Button
          size="sm"
          variant="danger"
          :loading="resetting"
          data-testid="confirm-reset-btn"
          @click="handleConfirmReset"
        >
          {{ t('settingsDiagnostics.actions.confirmReset') }}
        </Button>
      </template>
    </Modal>

    <TotpStepUpDialog v-if="stepUp.visible.value" :controller="stepUp" />
  </div>
</template>
