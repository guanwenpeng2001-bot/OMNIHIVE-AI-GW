<template>
  <AppLayout>
    <p role="note" class="mb-4 rounded-xl bg-[var(--surface-2)] px-4 py-3 text-sm text-[var(--brand-text)]">{{ t('channelTelemetry.cursorProbeUnsupported') }}</p>
    <div class="w-full min-w-0 space-y-6 pb-8 text-[var(--foreground)] tabular-nums">
      <header
        class="page-header mb-0 rounded-xl bg-[var(--card)] p-5 shadow-none border border-[var(--border)] sm:p-6"
      >
        <h1 class="page-title flex items-center gap-2 text-xl font-semibold text-[var(--foreground)]">
          <span class="inline-flex h-8 w-8 items-center justify-center rounded-xl bg-[var(--surface-2)] text-[var(--brand-text)]">
            <Icon name="chart" size="sm" />
          </span>
          {{ t('admin.channelMonitor.title') }}
        </h1>
        <p class="page-description mt-1.5 text-xs text-[var(--muted-foreground)]">
          {{
            isV1Mode
              ? t('channelTelemetry.admin.descriptionProbe')
              : t('channelTelemetry.admin.descriptionTelemetry')
          }}
        </p>
        <div v-if="!workspaceScoped" class="mt-4 border-t border-[var(--border)] pt-4">
          <div
            class="tabs segmented inline-flex w-full flex-wrap sm:w-auto"
            role="tablist"
            :aria-label="t('channelTelemetry.admin.tabAria')"
          >
            <button
              type="button"
              role="tab"
              class="tab seg-btn flex-1 sm:flex-none"
              :class="adminMonitorTab === 'telemetry' ? 'tab-active seg-active' : ''"
              :aria-selected="adminMonitorTab === 'telemetry'"
              @click="adminMonitorTab = 'telemetry'"
            >
              {{ t('channelTelemetry.admin.tabTelemetry') }}
            </button>
            <button
              type="button"
              role="tab"
              class="tab seg-btn flex-1 sm:flex-none"
              :class="adminMonitorTab === 'probes' ? 'tab-active seg-active' : ''"
              :aria-selected="adminMonitorTab === 'probes'"
              @click="adminMonitorTab = 'probes'"
            >
              {{ isV1Mode ? t('channelTelemetry.admin.tabProbeActive') : t('channelTelemetry.admin.tabProbeHistory') }}
            </button>
          </div>
        </div>
      </header>

      <MonitorSettingsPanel v-if="adminMonitorTab === 'telemetry'" />

      <TablePageLayout v-else>
      <template #filters>
        <MonitorFiltersBar
          v-model:search="searchQuery"
          v-model:provider="providerFilter"
          v-model:enabled="enabledFilter"
          :loading="loading"
          @reload="reload"
          @create="openCreateDialog"
          @manage-templates="showTemplateManager = true"
          @search-input="handleSearch"
        />
      </template>

      <template #table>
        <DataTable :columns="columns" :data="monitors" :loading="loading">
          <template #cell-name="{ row, value }">
            <div class="flex items-center gap-1.5">
              <span class="font-medium text-[var(--foreground)]">{{ value }}</span>
              <HelpTooltip v-if="row.api_key_decrypt_failed" :content="t('admin.channelMonitor.apiKeyDecryptFailed')">
                <Icon name="exclamationTriangle" size="sm" class="text-red-500" />
              </HelpTooltip>
            </div>
          </template>

          <template #cell-provider="{ row }">
            <span class="pill pill-ghost monitor-provider-badge font-medium" :class="providerBadgeClass(row.provider)">
              {{ providerLabel(row.provider) }}
            </span>
            <!-- 三种检测模式并列展示，quota 系配额数据源与纯探活一眼可分 -->
            <span class="pill ml-1 font-medium" :class="checkModeBadgeClass(row.check_mode)">
              {{ checkModeLabel(row.check_mode) }}
            </span>
          </template>

          <template #cell-primary_model="{ row }">
            <MonitorPrimaryModelCell :row="row" />
          </template>

          <template #cell-availability_7d="{ row }">
            <span class="text-sm text-[var(--foreground)]">{{ formatAvailability(row) }}</span>
          </template>

          <template #cell-latency="{ row }">
            <span class="text-sm text-[var(--foreground)]">{{ formatLatency(row.primary_latency_ms) }}</span>
          </template>

          <template #cell-enabled="{ row }">
            <span v-if="String(row.provider) === 'cursor'" class="text-xs text-[var(--muted-foreground)]">{{ t('channelTelemetry.cursorLegacyReadOnly') }}</span>
            <Toggle v-else :modelValue="row.enabled" @update:modelValue="toggleEnabled(row)" />
          </template>

          <template #cell-actions="{ row }">
            <MonitorActionsCell
              v-if="String(row.provider) !== 'cursor'"
              :row="row"
              :running="runningId === row.id"
              :duplicating="duplicatingIds.has(row.id)"
              @run="handleRunNow"
              @duplicate="handleDuplicate"
              @edit="openEditDialog"
              @delete="handleDelete"
            />
          </template>

          <template #empty>
            <EmptyState
              :title="t('admin.channelMonitor.noMonitorsYet')"
              :description="t('admin.channelMonitor.createFirstMonitor')"
              :action-text="t('admin.channelMonitor.createButton')"
              @action="openCreateDialog"
            />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="onPageChange"
          @update:pageSize="onPageSizeChange"
        />
      </template>
      </TablePageLayout>
    </div>

    <MonitorFormDialog
      :show="showDialog"
      :monitor="editing"
      @close="closeDialog"
      @saved="reload"
    />

    <MonitorTemplateManagerDialog
      :show="showTemplateManager"
      @close="showTemplateManager = false"
      @updated="reload"
    />

    <MonitorRunResultDialog
      :show="showRunResult"
      :results="runResults"
      @close="showRunResult = false"
    />

    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('common.delete')"
      :message="deleteConfirmMessage"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="confirmDelete"
      @cancel="showDeleteDialog = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { adminAPI } from '@/api/admin'
import { workspaceIDFromPath } from '@/api/workspaceScope'
import type {
  ChannelMonitor,
  CheckResult,
  ListParams,
  Provider,
} from '@/api/admin/channelMonitor'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import Icon from '@/components/icons/Icon.vue'
import Toggle from '@/components/common/Toggle.vue'
import MonitorFiltersBar from '@/components/admin/monitor/MonitorFiltersBar.vue'
import MonitorFormDialog from '@/components/admin/monitor/MonitorFormDialog.vue'
import MonitorTemplateManagerDialog from '@/components/admin/monitor/MonitorTemplateManagerDialog.vue'
import MonitorRunResultDialog from '@/components/admin/monitor/MonitorRunResultDialog.vue'
import MonitorPrimaryModelCell from '@/components/admin/monitor/MonitorPrimaryModelCell.vue'
import MonitorActionsCell from '@/components/admin/monitor/MonitorActionsCell.vue'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'
import MonitorSettingsPanel from '@/features/channel-telemetry/MonitorSettingsPanel.vue'
import { isChannelProbeMode } from '@/utils/featureFlags'

const { t } = useI18n()
const appStore = useAppStore()
const isV1Mode = computed(() => isChannelProbeMode())
const workspaceScoped = !!workspaceIDFromPath(window.location.pathname)
const adminMonitorTab = ref<'telemetry' | 'probes'>(workspaceScoped || isChannelProbeMode() ? 'probes' : 'telemetry')
const {
  providerLabel,
  providerBadgeClass,
  checkModeLabel,
  checkModeBadgeClass,
  formatLatency,
  formatAvailability,
} = useChannelMonitorFormat()

const monitors = ref<ChannelMonitor[]>([])
const loading = ref(false)
const runningId = ref<number | null>(null)
const searchQuery = ref('')
const providerFilter = ref<Provider | ''>('')
const enabledFilter = ref<'' | 'true' | 'false'>('')
const pagination = reactive({ page: 1, page_size: getPersistedPageSize(), total: 0 })

const showDialog = ref(false)
const showTemplateManager = ref(false)
const editing = ref<ChannelMonitor | null>(null)
const showDeleteDialog = ref(false)
const deleting = ref<ChannelMonitor | null>(null)
const showRunResult = ref(false)
const runResults = ref<CheckResult[]>([])
const duplicatingIds = reactive(new Set<number>())

let abortController: AbortController | null = null
let searchTimeout: ReturnType<typeof setTimeout> | null = null

const columns = computed<Column[]>(() => [
  { key: 'name', label: t('admin.channelMonitor.columns.name'), sortable: false },
  { key: 'provider', label: t('admin.channelMonitor.columns.provider'), sortable: false },
  { key: 'primary_model', label: t('admin.channelMonitor.columns.primaryModel'), sortable: false },
  { key: 'availability_7d', label: t('admin.channelMonitor.columns.availability7d'), sortable: false },
  { key: 'latency', label: t('admin.channelMonitor.columns.latency'), sortable: false },
  { key: 'enabled', label: t('admin.channelMonitor.columns.enabled'), sortable: false },
  { key: 'actions', label: t('admin.channelMonitor.columns.actions'), sortable: false },
])

const deleteConfirmMessage = computed(() => {
  const name = deleting.value?.name || ''
  return t('admin.channelMonitor.deleteConfirm', { name })
})

async function reload() {
  if (abortController) abortController.abort()
  const ctrl = new AbortController()
  abortController = ctrl
  loading.value = true
  try {
    const params: ListParams = {
      page: pagination.page,
      page_size: pagination.page_size,
    }
    if (providerFilter.value) params.provider = providerFilter.value
    if (enabledFilter.value === 'true') params.enabled = true
    if (enabledFilter.value === 'false') params.enabled = false
    if (searchQuery.value.trim()) params.search = searchQuery.value.trim()

    const res = await adminAPI.channelMonitor.list(params, { signal: ctrl.signal })
    if (ctrl.signal.aborted || abortController !== ctrl) return
    monitors.value = res.items || []
    pagination.total = res.total
  } catch (err: unknown) {
    const e = err as { name?: string; code?: string }
    if (e?.name === 'AbortError' || e?.code === 'ERR_CANCELED') return
    appStore.showError(extractApiErrorMessage(err, t('admin.channelMonitor.loadError')))
  } finally {
    if (abortController === ctrl) {
      loading.value = false
      abortController = null
    }
  }
}

function handleSearch() {
  if (searchTimeout) clearTimeout(searchTimeout)
  searchTimeout = setTimeout(() => {
    pagination.page = 1
    reload()
  }, 300)
}

function onPageChange(page: number) {
  pagination.page = page
  reload()
}

function onPageSizeChange(size: number) {
  pagination.page_size = size
  pagination.page = 1
  reload()
}

function openCreateDialog() {
  editing.value = null
  showDialog.value = true
}

function openEditDialog(row: ChannelMonitor) {
  editing.value = row
  showDialog.value = true
}

function closeDialog() {
  showDialog.value = false
  editing.value = null
}

async function toggleEnabled(row: ChannelMonitor) {
  const next = !row.enabled
  try {
    await adminAPI.channelMonitor.update(row.id, { enabled: next })
    row.enabled = next
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error'), { encryption_key_required: t('common.encryptionKeyRequired') }))
  }
}

async function handleRunNow(row: ChannelMonitor) {
  if (!isV1Mode.value) {
    appStore.showError(t('admin.channelMonitor.runFailed'))
    return
  }
  if (runningId.value != null) return
  runningId.value = row.id
  try {
    const res = await adminAPI.channelMonitor.runNow(row.id)
    runResults.value = res.results || []
    showRunResult.value = true
    appStore.showSuccess(t('admin.channelMonitor.runSuccess'))
    // Refresh row to get latest status from backend
    void reload()
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.channelMonitor.runFailed')))
  } finally {
    runningId.value = null
  }
}

async function handleDuplicate(row: ChannelMonitor) {
  if (row.api_key_decrypt_failed) {
    appStore.showError(t('admin.channelMonitor.duplicateKeyUnavailable'))
    return
  }
  if (duplicatingIds.has(row.id)) return

  duplicatingIds.add(row.id)
  try {
    const duplicate = await adminAPI.channelMonitor.duplicate(row.id)
    appStore.showSuccess(t('admin.channelMonitor.duplicateSuccess', { name: duplicate.name }))
    await reload()
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.channelMonitor.duplicateFailed'), { encryption_key_required: t('common.encryptionKeyRequired') }))
  } finally {
    duplicatingIds.delete(row.id)
  }
}

function handleDelete(row: ChannelMonitor) {
  deleting.value = row
  showDeleteDialog.value = true
}

async function confirmDelete() {
  if (!deleting.value) return
  try {
    await adminAPI.channelMonitor.del(deleting.value.id)
    appStore.showSuccess(t('admin.channelMonitor.deleteSuccess'))
    showDeleteDialog.value = false
    deleting.value = null
    reload()
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error'), { encryption_key_required: t('common.encryptionKeyRequired') }))
  }
}

watch(adminMonitorTab, (tab) => {
  if (tab === 'probes' && monitors.value.length === 0) void reload()
})
onMounted(() => {
  if (adminMonitorTab.value === 'probes') void reload()
})
onUnmounted(() => {
  if (searchTimeout) clearTimeout(searchTimeout)
  abortController?.abort()
})
</script>

<style scoped>
.pill.monitor-provider-badge {
  background: var(--surface-2);
  border-color: var(--border);
  color: var(--muted-foreground);
}
</style>
