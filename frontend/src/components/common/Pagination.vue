<template>
  <div
    class="oh-pagination flex items-center justify-between border-t border-[var(--border)] bg-[var(--card)] px-4 py-3 sm:px-6"
  >
    <div class="flex flex-1 items-center justify-between sm:hidden">
      <!-- Mobile pagination -->
      <button
        @click="goToPage(page - 1)"
        :disabled="page === 1"
        class="oh-page-btn relative inline-flex items-center rounded-lg border border-[var(--border)] bg-[var(--card)] px-3.5 py-1.5 text-sm font-medium text-[var(--foreground)] hover:bg-[var(--surface-2)] disabled:cursor-not-allowed disabled:opacity-50"
      >
        {{ t('pagination.previous') }}
      </button>
      <span class="font-mono text-sm tabular-nums text-gray-700 dark:text-gray-300">
        {{ t('pagination.pageOf', { page, total: totalPages }) }}
      </span>
      <button
        @click="goToPage(page + 1)"
        :disabled="page === totalPages"
        class="oh-page-btn relative ml-3 inline-flex items-center rounded-lg border border-[var(--border)] bg-[var(--card)] px-3.5 py-1.5 text-sm font-medium text-[var(--foreground)] hover:bg-[var(--surface-2)] disabled:cursor-not-allowed disabled:opacity-50"
      >
        {{ t('pagination.next') }}
      </button>
    </div>

    <div class="hidden sm:flex sm:flex-1 sm:items-center sm:justify-between">
      <!-- Desktop pagination info -->
      <div class="flex items-center space-x-4">
        <p class="text-sm tabular-nums text-gray-600 dark:text-gray-300">
          {{ t('pagination.showing') }}
          <span class="font-mono font-semibold text-gray-900 dark:text-white">{{ fromItem }}</span>
          {{ t('pagination.to') }}
          <span class="font-mono font-semibold text-gray-900 dark:text-white">{{ toItem }}</span>
          {{ t('pagination.of') }}
          <span class="font-mono font-semibold text-gray-900 dark:text-white">{{ total }}</span>
          {{ t('pagination.results') }}
        </p>

        <!-- Page size selector -->
        <div v-if="showPageSizeSelector" class="flex items-center space-x-2">
          <span class="text-sm text-gray-600 dark:text-gray-300"
            >{{ t('pagination.perPage') }}:</span
          >
          <div class="page-size-select w-20">
            <Select
              :model-value="pageSize"
              :aria-label="t('pagination.perPage')"
              :options="pageSizeSelectOptions"
              @update:model-value="handlePageSizeChange"
            />
          </div>
        </div>

        <div v-if="showJump" class="flex items-center space-x-2">
          <span class="text-sm text-gray-600 dark:text-gray-300">{{ t('pagination.jumpTo') }}</span>
          <input
            v-model="jumpPage"
            type="number"
            min="1"
            :max="totalPages"
            class="input w-20 font-mono text-sm tabular-nums"
            :placeholder="t('pagination.jumpPlaceholder')"
            :aria-label="t('pagination.jumpTo')"
            @keyup.enter="submitJump"
          />
          <button type="button" class="btn btn-ghost btn-sm oh-jump-btn" @click="submitJump">
            {{ t('pagination.jumpAction') }}
          </button>
        </div>
      </div>

      <!-- Desktop pagination buttons -->
      <nav
        class="oh-pagination-nav relative z-0 inline-flex items-center gap-1"
        :aria-label="t('pagination.label')"
      >
        <!-- Previous button -->
        <button
          @click="goToPage(page - 1)"
          :disabled="page === 1"
          class="oh-page-btn relative inline-flex items-center rounded-lg border border-[var(--border)] bg-[var(--card)] px-2.5 py-1.5 text-sm font-medium text-[var(--muted-foreground)] hover:bg-[var(--surface-2)] disabled:cursor-not-allowed disabled:opacity-50"
          :aria-label="t('pagination.previous')"
        >
          <Icon name="chevronLeft" size="md" />
        </button>

        <!-- Page numbers -->
        <button
          v-for="(pageNum, index) in visiblePages"
          :key="`${pageNum}-${index}`"
          @click="typeof pageNum === 'number' && goToPage(pageNum)"
          :disabled="typeof pageNum !== 'number'"
          :class="[
            'oh-page-btn relative inline-flex items-center rounded-lg border px-3.5 py-1.5 font-mono text-sm font-medium tabular-nums transition-all',
            pageNum === page
              ? 'oh-page-active z-10 border-amber-500/50 bg-amber-500/10 text-amber-800 dark:border-amber-400/50 dark:bg-amber-400/15 dark:text-amber-300'
              : 'border-[var(--border)] bg-[var(--card)] text-[var(--foreground)] hover:bg-[var(--surface-2)]',
            typeof pageNum !== 'number' && 'cursor-default'
          ]"
          :aria-label="
            typeof pageNum === 'number' ? t('pagination.goToPage', { page: pageNum }) : undefined
          "
          :aria-current="pageNum === page ? 'page' : undefined"
        >
          {{ pageNum }}
        </button>

        <!-- Next button -->
        <button
          @click="goToPage(page + 1)"
          :disabled="page === totalPages"
          class="oh-page-btn relative inline-flex items-center rounded-lg border border-[var(--border)] bg-[var(--card)] px-2.5 py-1.5 text-sm font-medium text-[var(--muted-foreground)] hover:bg-[var(--surface-2)] disabled:cursor-not-allowed disabled:opacity-50"
          :aria-label="t('pagination.next')"
        >
          <Icon name="chevronRight" size="md" />
        </button>
      </nav>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import Select from './Select.vue'
import { getConfiguredTablePageSizeOptions, normalizeTablePageSize } from '@/utils/tablePreferences'
import { setPersistedPageSize } from '@/composables/usePersistedPageSize'

const { t } = useI18n()

interface Props {
  total: number
  page: number
  pageSize: number
  pageSizeOptions?: number[]
  showPageSizeSelector?: boolean
  showJump?: boolean
}

interface Emits {
  (e: 'update:page', page: number): void
  (e: 'update:pageSize', pageSize: number): void
}

const props = withDefaults(defineProps<Props>(), {
  pageSizeOptions: () => getConfiguredTablePageSizeOptions(),
  showPageSizeSelector: true,
  showJump: false
})

const emit = defineEmits<Emits>()

const totalPages = computed(() => Math.ceil(props.total / props.pageSize))

const fromItem = computed(() => {
  if (props.total === 0) return 0
  return (props.page - 1) * props.pageSize + 1
})

const toItem = computed(() => {
  const to = props.page * props.pageSize
  return to > props.total ? props.total : to
})

const pageSizeSelectOptions = computed(() => {
  const options = Array.from(
    new Set([
      ...getConfiguredTablePageSizeOptions(),
      normalizeTablePageSize(props.pageSize)
    ])
  ).sort((a, b) => a - b)

  return options.map((size) => ({
    value: size,
    label: String(size)
  }))
})

const jumpPage = ref('')

const visiblePages = computed(() => {
  const pages: (number | string)[] = []
  const maxVisible = 7
  const total = totalPages.value

  if (total <= maxVisible) {
    // Show all pages if total is small
    for (let i = 1; i <= total; i++) {
      pages.push(i)
    }
  } else {
    // Always show first page
    pages.push(1)

    const start = Math.max(2, props.page - 2)
    const end = Math.min(total - 1, props.page + 2)

    // Add ellipsis before if needed
    if (start > 2) {
      pages.push('...')
    }

    // Add middle pages
    for (let i = start; i <= end; i++) {
      pages.push(i)
    }

    // Add ellipsis after if needed
    if (end < total - 1) {
      pages.push('...')
    }

    // Always show last page
    pages.push(total)
  }

  return pages
})

const goToPage = (newPage: number) => {
  if (newPage >= 1 && newPage <= totalPages.value && newPage !== props.page) {
    emit('update:page', newPage)
  }
}

const handlePageSizeChange = (value: string | number | boolean | null) => {
  if (value === null || typeof value === 'boolean') return
  const newPageSize = normalizeTablePageSize(typeof value === 'string' ? parseInt(value, 10) : value)
  setPersistedPageSize(newPageSize)
  emit('update:pageSize', newPageSize)
}

const submitJump = () => {
  const value = String(jumpPage.value).trim()
  if (!value) return
  const pageNum = Number.parseInt(value, 10)
  if (Number.isNaN(pageNum)) return
  const nextPage = Math.min(Math.max(pageNum, 1), totalPages.value)
  jumpPage.value = ''
  goToPage(nextPage)
}
</script>

<style scoped>
.page-size-select :deep(.select-trigger) {
  @apply px-3 py-1.5 text-sm;
}

.oh-pagination {
  border-top-color: var(--border, #e9e9ee);
  background: var(--card, #ffffff);
}

.dark .oh-pagination {
  border-top-color: var(--border, #232329);
  background: var(--card, #131316);
}

.oh-page-btn {
  border-color: var(--border, #e9e9ee);
  transition: all 0.16s cubic-bezier(0.2, 0.8, 0.2, 1);
}

.dark .oh-page-btn {
  border-color: var(--border, #232329);
}

.oh-page-btn:not(:disabled):hover {
  border-color: color-mix(in srgb, var(--brand, #f59e0b) 40%, var(--border, #e9e9ee));
}

.oh-page-active {
  font-weight: 600;
  border-color: color-mix(in srgb, var(--brand, #f59e0b) 50%, var(--border, #e9e9ee)) !important;
  background: color-mix(in srgb, var(--brand, #f59e0b) 13%, var(--card, #ffffff)) !important;
  color: var(--brand-text, #92610a) !important;
}

.dark .oh-page-active {
  color: var(--brand-text, #fbbf24) !important;
  background: color-mix(in srgb, var(--brand, #fbbf24) 16%, var(--card, #131316)) !important;
}
</style>
