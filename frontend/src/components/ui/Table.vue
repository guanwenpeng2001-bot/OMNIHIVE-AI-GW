<script setup lang="ts">
import { computed } from 'vue'
import Checkbox from './Checkbox.vue'
import EmptyState from './EmptyState.vue'
import ErrorState from './ErrorState.vue'
import type { UiSortOrder, UiTableColumn } from './types'
import { cn, useUiI18n } from './utils'

export interface TableProps {
  columns: UiTableColumn[]
  rows: Array<Record<string, unknown>>
  rowKey?: string | ((row: Record<string, unknown>, index: number) => string | number)
  sortBy?: string | null
  sortOrder?: UiSortOrder
  loading?: boolean
  refreshing?: boolean
  skeletonRows?: number
  selectable?: boolean
  selectedKeys?: Array<string | number>
  filtered?: boolean
  emptyTitle?: string
  emptyDescription?: string
  emptyActionLabel?: string
  filteredEmptyTitle?: string
  filteredEmptyDescription?: string
  errorTitle?: string
  errorMessage?: string
  errorRequestId?: string
  stickyHeader?: boolean
  caption?: string
}

const props = withDefaults(defineProps<TableProps>(), {
  rowKey: 'id',
  sortBy: null,
  sortOrder: null,
  loading: false,
  refreshing: false,
  skeletonRows: 5,
  selectable: false,
  selectedKeys: () => [],
  filtered: false,
  emptyTitle: undefined,
  emptyDescription: undefined,
  emptyActionLabel: undefined,
  filteredEmptyTitle: undefined,
  filteredEmptyDescription: undefined,
  errorTitle: undefined,
  errorMessage: undefined,
  errorRequestId: undefined,
  stickyHeader: false,
  caption: undefined
})

const { t } = useUiI18n()
const resolvedEmptyTitle = computed(() => props.emptyTitle ?? t('ui.table.emptyTitle'))
const resolvedFilteredEmptyTitle = computed(
  () => props.filteredEmptyTitle ?? t('ui.table.filteredEmptyTitle')
)
const resolvedFilteredEmptyDescription = computed(
  () => props.filteredEmptyDescription ?? t('ui.table.filteredEmptyDescription')
)

const emit = defineEmits<{
  sort: [payload: { key: string; order: 'asc' | 'desc' }]
  'update:selectedKeys': [keys: Array<string | number>]
  rowClick: [row: Record<string, unknown>, index: number]
  emptyAction: []
  clearFilters: []
  retry: []
}>()

function resolveRowKey(row: Record<string, unknown>, index: number): string | number {
  if (typeof props.rowKey === 'function') {
    return props.rowKey(row, index)
  }
  const candidate = row[props.rowKey]
  if (typeof candidate === 'string' || typeof candidate === 'number') {
    return candidate
  }
  return index
}

const allRowKeys = computed(() =>
  props.rows.map((row, index) => resolveRowKey(row, index))
)

const selectedKeySet = computed(() => new Set(props.selectedKeys))

const allSelected = computed(
  () => allRowKeys.value.length > 0 && allRowKeys.value.every((key) => selectedKeySet.value.has(key))
)

const someSelected = computed(
  () => !allSelected.value && allRowKeys.value.some((key) => selectedKeySet.value.has(key))
)

function toggleSelectAll(checked: boolean): void {
  if (checked) {
    emit('update:selectedKeys', [...allRowKeys.value])
  } else {
    emit('update:selectedKeys', [])
  }
}

function toggleRowSelection(key: string | number, checked: boolean): void {
  const next = new Set(props.selectedKeys)
  if (checked) {
    next.add(key)
  } else {
    next.delete(key)
  }
  emit('update:selectedKeys', Array.from(next))
}

function resolveAriaSort(column: UiTableColumn): 'ascending' | 'descending' | 'none' | undefined {
  if (!column.sortable) return undefined
  if (props.sortBy !== column.key || !props.sortOrder) return 'none'
  return props.sortOrder === 'asc' ? 'ascending' : 'descending'
}

function handleSort(column: UiTableColumn): void {
  if (!column.sortable) return
  const nextOrder: 'asc' | 'desc' =
    props.sortBy === column.key && props.sortOrder === 'asc' ? 'desc' : 'asc'
  emit('sort', { key: column.key, order: nextOrder })
}

function resolveAlignment(column: UiTableColumn): 'left' | 'center' | 'right' {
  if (column.align) return column.align
  return column.mono ? 'right' : 'left'
}

function formatCellValue(column: UiTableColumn, row: Record<string, unknown>): string {
  const raw = row[column.key]
  if (column.format) {
    return column.format(raw, row)
  }
  if (raw === null || raw === undefined || raw === '') {
    return '—'
  }
  if (typeof raw === 'number' && !Number.isFinite(raw)) {
    return '—'
  }
  return String(raw)
}

const totalColSpan = computed(() => props.columns.length + (props.selectable ? 1 : 0))
</script>

<template>
  <div class="relative overflow-hidden rounded-oh-lg border border-oh-default bg-oh-surface">
    <div
      v-if="refreshing"
      role="status"
      :aria-label="t('ui.table.refreshing')"
      class="absolute inset-x-0 top-0 z-10 h-0.5 bg-oh-accent oh-skeleton-pulse"
    />

    <div v-if="errorTitle" class="p-4">
      <ErrorState
        :title="errorTitle"
        :message="errorMessage"
        :request-id="errorRequestId"
        compact
        @retry="emit('retry')"
      />
    </div>

    <div v-else class="overflow-x-auto">
      <table class="w-full border-collapse text-left text-sm">
        <caption v-if="caption" class="sr-only">
          {{ caption }}
        </caption>

        <thead
          :class="
            cn(
              'border-b border-oh-default bg-oh-subtle',
              stickyHeader && 'sticky top-0 z-10'
            )
          "
        >
          <tr>
            <th
              v-if="selectable"
              scope="col"
              class="w-10 px-3 py-2.5 text-left"
            >
              <Checkbox
                :model-value="allSelected"
                :indeterminate="someSelected"
                :disabled="loading || rows.length === 0"
                :aria-label="t('ui.table.selectAllRows')"
                @update:model-value="toggleSelectAll"
              />
            </th>

            <th
              v-for="column in columns"
              :key="column.key"
              scope="col"
              :aria-sort="resolveAriaSort(column)"
              :style="{ width: column.width, minWidth: column.minWidth }"
              :class="
                cn(
                  'px-4 py-2.5 oh-eyebrow select-none whitespace-nowrap',
                  resolveAlignment(column) === 'right' && 'text-right',
                  resolveAlignment(column) === 'center' && 'text-center'
                )
              "
            >
              <button
                v-if="column.sortable"
                type="button"
                :title="column.tooltip"
                :class="
                  cn(
                    'oh-focus-ring inline-flex items-center gap-1 rounded-oh-xs uppercase tracking-wider transition-colors hover:text-oh-primary',
                    sortBy === column.key ? 'text-oh-primary' : 'text-oh-muted',
                    resolveAlignment(column) === 'right' && 'ml-auto flex-row-reverse'
                  )
                "
                @click="handleSort(column)"
              >
                <span
                  :class="
                    cn(
                      column.tooltip && 'decoration-dotted underline underline-offset-4'
                    )
                  "
                >
                  <slot :name="`header-${column.key}`" :column="column">
                    {{ column.label }}
                  </slot>
                </span>
                <span class="inline-flex w-3 justify-center font-mono text-[10px]" aria-hidden="true">
                  <template v-if="sortBy === column.key && sortOrder === 'asc'">↑</template>
                  <template v-else-if="sortBy === column.key && sortOrder === 'desc'">↓</template>
                  <template v-else>↕</template>
                </span>
              </button>

              <span
                v-else
                :title="column.tooltip"
                :class="cn(column.tooltip && 'decoration-dotted underline underline-offset-4 cursor-help')"
              >
                <slot :name="`header-${column.key}`" :column="column">
                  {{ column.label }}
                </slot>
              </span>
            </th>
          </tr>
        </thead>

        <tbody v-if="loading && rows.length === 0" class="divide-y divide-oh-subtle">
          <tr v-for="skeletonIndex in skeletonRows" :key="`sk-${skeletonIndex}`">
            <td v-if="selectable" class="px-3 py-3">
              <div class="oh-skeleton-pulse h-4 w-4 rounded-oh-xs bg-oh-inset" />
            </td>
            <td
              v-for="(column, colIdx) in columns"
              :key="`sk-${skeletonIndex}-${column.key}`"
              class="px-4 py-3"
            >
              <div
                :class="
                  cn(
                    'oh-skeleton-pulse h-4 rounded-oh-xs bg-oh-inset',
                    colIdx === 0 ? 'w-3/4' : 'w-16',
                    resolveAlignment(column) === 'right' && 'ml-auto'
                  )
                "
              />
            </td>
          </tr>
        </tbody>

        <tbody v-else-if="rows.length === 0">
          <tr>
            <td :colspan="totalColSpan">
              <slot name="empty">
                <EmptyState
                  :variant="filtered ? 'filtered' : 'empty'"
                  :title="filtered ? resolvedFilteredEmptyTitle : resolvedEmptyTitle"
                  :description="filtered ? resolvedFilteredEmptyDescription : emptyDescription"
                  :action-label="emptyActionLabel"
                  compact
                  @action="emit('emptyAction')"
                  @clear-filters="emit('clearFilters')"
                />
              </slot>
            </td>
          </tr>
        </tbody>

        <tbody v-else class="divide-y divide-oh-subtle">
          <tr
            v-for="(row, rowIndex) in rows"
            :key="resolveRowKey(row, rowIndex)"
            :class="
              cn(
                'transition-colors duration-100',
                selectedKeySet.has(resolveRowKey(row, rowIndex))
                  ? 'bg-oh-selected'
                  : 'hover:bg-oh-hover/65'
              )
            "
            @click="emit('rowClick', row, rowIndex)"
          >
            <td
              v-if="selectable"
              class="w-10 px-3 py-[var(--oh-cell-py)]"
              @click.stop
            >
              <Checkbox
                :model-value="selectedKeySet.has(resolveRowKey(row, rowIndex))"
                :aria-label="t('ui.table.selectRow', { index: rowIndex + 1 })"
                @update:model-value="toggleRowSelection(resolveRowKey(row, rowIndex), $event)"
              />
            </td>

            <td
              v-for="column in columns"
              :key="column.key"
              :class="
                cn(
                  'px-4 py-[var(--oh-cell-py)] text-[13px] leading-5',
                  resolveAlignment(column) === 'right' && 'text-right',
                  resolveAlignment(column) === 'center' && 'text-center',
                  column.mono
                    ? 'oh-tabular text-oh-primary whitespace-nowrap'
                    : 'text-oh-primary',
                  (row[column.key] === null || row[column.key] === undefined || row[column.key] === '') &&
                    'text-oh-muted'
                )
              "
            >
              <slot
                :name="`cell-${column.key}`"
                :row="row"
                :value="row[column.key]"
                :index="rowIndex"
              >
                {{ formatCellValue(column, row) }}
              </slot>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
