<template>
  <AppLayout>
    <TablePageLayout class="batch-page w-full min-w-0">
      <template #filters>
        <div class="flex flex-col gap-4">
          <header class="flex flex-wrap items-start justify-between gap-3">
            <div>
              <h1 class="text-2xl font-bold tracking-tight text-[var(--foreground)]">{{ t('nav.batchImage') }}</h1>
              <p class="mt-1 text-sm leading-6 text-[var(--muted-foreground)]">{{ copy('提交多条提示词异步并发生成图像，支持子项进度追踪、失败重试与结果打包下载。', 'Generate images asynchronously from multiple prompts, track items, retry failures, and download results together.') }}</p>
            </div>
            <div class="flex shrink-0 gap-2">
              <button type="button" class="btn btn-secondary" @click="showGuideModal = true">
                <Icon name="book" size="sm" class="mr-2" />{{ t('batchImage.actions.usageGuide') }}
              </button>
              <button type="button" class="btn btn-primary" :disabled="loadingKeys || !hasAnyActiveKey" @click="openCreateModal">
                <Icon name="plus" size="sm" class="mr-2" />{{ isZhLocale() ? '创建任务' : t('batchImage.actions.createJob') }}
              </button>
            </div>
          </header>
          <div v-if="!loadingKeys && !hasAnyActiveKey" class="card flex flex-wrap items-center gap-3 bg-[var(--surface-2)] p-3 text-sm text-[var(--muted-foreground)]" role="status">
            <span>{{ copy('当前尚无可用 API 密钥', 'No active API key is available yet.') }}</span>
            <a href="/keys" class="btn btn-secondary btn-sm">{{ copy('管理密钥', 'Manage keys') }}</a>
          </div>
          <div v-else-if="!loadingKeys && !hasGeminiBatchKey && credentialedKeys.length" class="card flex flex-wrap items-center gap-2 p-3 text-sm text-[var(--muted-foreground)]" role="status">
            <Icon name="key" size="sm" />
            {{ copy('可选择当前活跃密钥创建任务，可用模型与权限由服务端确认。', 'Choose an active key to create a job. Available models and permissions are checked by the server.') }}
          </div>
          <div v-if="!loadingKeys && hasAnyActiveKey && !credentialedKeys.length" class="card flex flex-wrap items-center gap-3 p-3 text-sm text-[var(--muted-foreground)]" role="status">
            {{ copy('密钥列表仅显示脱敏值，请在创建任务中输入完整密钥以加载任务与模型。', 'Keys are masked in the list. Enter a full key in Create job to load its jobs and models.') }}
            <button type="button" class="btn btn-secondary btn-sm" @click="openCreateModal">{{ copy('连接密钥', 'Connect key') }}</button>
          </div>
          <div class="card flex flex-col gap-2 p-3 xl:flex-row xl:items-center">
            <div class="filter-grid batch-filter-grid w-full min-w-0 xl:flex-1">
              <div class="min-w-0">
                <SearchInput
                  v-model="filters.taskName"
                  :placeholder="t('batchImage.filters.searchTaskName')"
                  :aria-label="t('batchImage.filters.searchTaskName')"
                  class="w-full"
                  @search="applyFilters"
                />
              </div>
              <Select v-model="filters.apiKeyId" :options="apiKeyFilterOptions" :aria-label="copy('密钥', 'API key')" class="w-full" @change="applyFilters" />
              <Select v-model="filters.status" :options="statusFilterOptions" :aria-label="t('common.status')" class="w-full" @change="applyFilters" />
              <Select v-model="filters.downloaded" :options="downloadFilterOptions" :aria-label="t('batchImage.columns.downloadStatus')" class="w-full" @change="applyFilters" />
            </div>
            <div class="flex shrink-0 items-center gap-2">
              <button type="button" class="btn btn-secondary h-9 w-9 !p-0" :disabled="loadingJobs" :title="t('common.reset')" :aria-label="t('common.reset')" @click="resetFilters">
                <Icon name="x" size="sm" />
              </button>
              <button type="button" class="btn btn-secondary h-9 w-9 !p-0" :disabled="loadingKeys || loadingJobs" :title="t('common.refresh')" :aria-label="t('common.refresh')" @click="refreshPage">
                <Icon name="refresh" size="md" :class="loadingKeys || loadingJobs ? 'motion-safe:animate-spin' : ''" />
              </button>
            </div>
          </div>

          <div class="card flex flex-wrap items-center gap-3 p-3 text-sm" data-test="batch-selection-bar">
            <span class="text-[var(--muted-foreground)]" aria-live="polite">
              {{ copy('已选择', 'Selected') }} <b class="font-semibold text-[var(--foreground)]">{{ selectedJobIds.size }}</b>
              {{ copy('个任务（筛选后仍保留）', 'jobs (kept across filters)') }}
            </span>
            <div class="flex flex-wrap items-center gap-2">
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="bulkDownloading || selectedDownloadableRows.length === 0"
                @click="downloadSelectedJobs"
              >
                <Icon :name="bulkDownloading ? 'refresh' : 'download'" size="sm" class="mr-1.5" :class="bulkDownloading ? 'motion-safe:animate-spin' : ''" />
                {{ copy('打包下载所选 ZIP', 'Download selected ZIPs') }}
              </button>
              <button
                type="button"
                class="btn btn-secondary btn-sm text-red-600 hover:text-red-700 dark:text-red-400"
                :disabled="bulkDeleting || !selectedRows.some(row => canDeleteRecord(row))"
                @click="deleteSelectedJobs"
              >
                <Icon :name="bulkDeleting ? 'refresh' : 'trash'" size="sm" class="mr-1.5" :class="bulkDeleting ? 'motion-safe:animate-spin' : ''" />
                {{ copy('删除所选记录', 'Delete selected records') }}
              </button>
            </div>
            <span class="text-xs text-[var(--muted-foreground)] xl:ml-auto">{{ copy('支持批量导出生成图像与 JSONL 结果清单', 'Export generated images and JSONL result manifests in bulk') }}</span>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable
          :columns="columns"
          :data="visibleBatchJobs"
          :loading="loadingKeys || loadingJobs"
          :expandable-actions="false"
          row-key="id"
        >
          <template #header-select>
            <input
              type="checkbox"
              class="h-4 w-4 rounded border-[var(--border)] text-[var(--brand-text)] focus:ring-[var(--brand)]"
              :checked="allVisibleSelected"
              :indeterminate="someVisibleSelected"
              :aria-label="copy('选择当前页父任务', 'Select parent jobs on this page')"
              @change="toggleAllVisible(($event.target as HTMLInputElement).checked)"
            />
          </template>

          <template #cell-select="{ row }">
            <input
              v-if="!row.is_child"
              type="checkbox"
              class="h-4 w-4 rounded border-[var(--border)] text-[var(--brand-text)] focus:ring-[var(--brand)]"
              :checked="selectedJobIds.has(row.id)"
              :aria-label="copy('选择任务 ', 'Select job ') + row.task_name"
              @change="toggleJobSelection(row.id, ($event.target as HTMLInputElement).checked)"
              @click.stop
            />
          </template>

          <template #cell-id="{ row }">
	            <div class="flex w-[220px] items-start gap-1" :class="row.is_child ? 'pl-6' : ''">
	              <button
	                v-if="row.child_count > 0 && !row.is_child"
	                type="button"
	                class="mt-1 flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-md text-[var(--muted-foreground)] transition-colors hover:bg-[var(--surface-2)] hover:text-[var(--foreground)] focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--brand)] dark:hover:text-white"
	                :title="expandedParentIds.has(row.id) ? t('batchImage.list.collapseChildren') : t('batchImage.list.expandChildren', { n: row.child_count }, row.child_count)"
                    :aria-expanded="expandedParentIds.has(row.id)"
	                @click.stop="toggleChildRows(row.id)"
	              >
	                <Icon :name="expandedParentIds.has(row.id) ? 'chevronDown' : 'chevronRight'" size="xs" />
	              </button>
	              <span v-else class="w-6 flex-shrink-0" />
	              <button type="button" class="min-w-0 flex-1 rounded-lg py-1 text-left transition-colors hover:bg-[var(--surface-2)] focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--brand)]" @click="selectJob(row.id)">
	                <span
	                  class="flex min-w-0 items-center gap-2 text-sm font-medium"
	                  :class="row.task_name ? 'text-[var(--foreground)] ' : 'text-[var(--muted-foreground)] '"
                >
                  <span class="min-w-0 truncate">{{ row.task_name || defaultTaskName(row.created_at) }}</span>
                  <span v-if="row.child_count > 0 && !row.is_child" class="flex-shrink-0 rounded-full bg-[var(--surface-2)] px-2 py-0.5 text-xs font-normal text-[var(--muted-foreground)]">
                    {{ t('batchImage.list.childCount', { n: row.child_count }, row.child_count) }}
                  </span>
                  <span v-if="row.is_child" class="flex-shrink-0 rounded-full bg-amber-50 px-2 py-0.5 text-xs font-normal text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">
                    {{ t('batchImage.list.childBadge') }}
                  </span>
	                </span>
	                <span class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-[var(--muted-foreground)]">
	                  <span class="truncate font-mono" :title="row.id">{{ row.id }}</span>
	                </span>
	              </button>
	            </div>
	          </template>

          <template #cell-model="{ row }">
	            <div class="max-w-[220px] text-left">
	              <p class="truncate text-sm text-[var(--foreground)]" :title="row.model">{{ row.model }}</p>
                  <p class="mt-1 truncate text-xs text-[var(--muted-foreground)]" :title="row.api_key_name">{{ row.api_key_name || t('batchImage.list.keyNotRecorded') }}</p>
	            </div>
	          </template>

          <template #cell-status="{ row }">
            <div class="flex justify-start">
              <span :class="statusBadgeClass(displayJob(row))" class="badge gap-1.5 whitespace-nowrap">
                <span class="h-1.5 w-1.5 rounded-full bg-current" aria-hidden="true" />
                {{ statusLabel(displayJob(row)) }}
              </span>
            </div>
          </template>

          <template #cell-counts="{ row }">
            <div class="text-left text-sm tabular-nums">
              <p class="font-mono">{{ displayJob(row).success_count }} / {{ displayJob(row).item_count }}</p>
              <p class="mt-1 text-xs" :class="displayJob(row).fail_count > 0 ? 'text-red-600 dark:text-red-300' : 'text-[var(--muted-foreground)]'">{{ copy('失败', 'Failed') }} {{ displayJob(row).fail_count }}</p>
            </div>
          </template>

          <template #cell-downloaded="{ row }">
            <span class="block text-left text-xs" :title="row.downloaded_at ? formatDate(row.downloaded_at) : ''" :class="row.downloaded_at ? 'text-emerald-700 dark:text-emerald-300' : 'text-[var(--muted-foreground)] '">
              {{ row.downloaded_at ? t('batchImage.filters.downloaded') : t('batchImage.list.notDownloaded') }}
            </span>
          </template>

	          <template #cell-actions="{ row }">
	            <div class="flex flex-wrap items-center justify-start gap-1">
              <button
                type="button"
                class="btn btn-ghost btn-sm batch-row-action"
                :title="t('batchImage.actions.viewDetail')"
                @click="selectJob(row.id)"
              >
                {{ copy('详情', 'Details') }}
              </button>
              <button v-if="canRetry(row)" type="button" class="btn btn-secondary btn-sm batch-row-action" :disabled="!!retryingBatchId" @click="retryFailedJob(row)">
                {{ t('batchImage.actions.retryFailedItems') }}
              </button>
              <button
                type="button"
                class="btn btn-secondary btn-sm batch-row-action"
                :class="canDownload(row) ? 'text-[var(--muted-foreground)] hover:bg-green-50 hover:text-green-600 dark:hover:bg-green-900/20 dark:hover:text-green-400' : 'text-[var(--muted-foreground)] '"
                :disabled="!canDownload(row) || downloading"
                :title="t('batchImage.actions.downloadZip')"
                @click="downloadJob(row)"
              >
                <Icon v-if="isDownloadingJob(row.id)"
                  :name="isDownloadingJob(row.id) ? 'refresh' : 'download'"
	                  size="sm"
	                  :class="isDownloadingJob(row.id) ? 'motion-safe:animate-spin' : ''"
	                />
                <span>{{ t('batchImage.actions.downloadZip') }}</span>
	              </button>
              <button v-if="canCancel(row)" type="button" class="btn btn-ghost btn-sm batch-row-action" :disabled="cancelling" @click="cancelJob(row)">{{ t('common.cancel') }}</button>
              <button v-if="canDeleteRecord(row)" type="button" class="btn btn-ghost btn-sm batch-row-action text-red-600 dark:text-red-400" :disabled="!!deletingBatchId || bulkDeleting" @click="deleteJob(row)">{{ t('batchImage.actions.deleteRecords') }}</button>
	            </div>
	          </template>

          <template #empty>
            <div class="flex min-h-[260px] flex-col items-center justify-center py-6 md:min-h-[300px]">
              <Icon name="sparkles" size="xl" class="mb-4 h-12 w-12 text-[var(--muted-foreground)]" />
              <p class="text-lg font-medium text-[var(--foreground)]">{{ t('batchImage.list.empty') }}</p>
              <p class="mt-1 text-sm text-[var(--muted-foreground)]">
                {{ t('batchImage.list.emptyHint') }}
              </p>
            </div>
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <div
          class="flex flex-col gap-3 border-t border-[var(--border)] bg-[var(--card)] px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-6"
        >
          <div class="flex flex-wrap items-center gap-3 text-sm text-[var(--foreground)]">
            <span class="text-xs text-[var(--muted-foreground)]">{{ copy('显示', 'Showing') }} {{ visibleParentJobs.length }} / {{ parentJobs.length }} {{ copy('个父任务', 'parent jobs') }}</span>
            <span class="text-xs text-[var(--muted-foreground)]">{{ pagination.page }} / {{ totalPages }}</span>
            <div class="flex items-center gap-2">
              <span>{{ t('pagination.perPage') }}</span>
              <Select
                v-model="pagination.page_size"
                :options="batchPageSizeOptions"
                class="w-24"
                @change="handlePageSizeChange"
              />
            </div>
          </div>
          <div class="flex items-center justify-end gap-2">
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="pagination.page <= 1 || loadingJobs"
              @click="handlePageChange(pagination.page - 1)"
            >
              <Icon name="chevronLeft" size="sm" class="mr-1" />
              {{ t('pagination.previous') }}
            </button>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="!pagination.has_more || loadingJobs"
              @click="handlePageChange(pagination.page + 1)"
            >
              {{ t('pagination.next') }}
              <Icon name="chevronRight" size="sm" class="ml-1" />
            </button>
          </div>
        </div>
      </template>
    </TablePageLayout>

    <Teleport to="body">
      <div
        v-if="promptPopover.visible"
        class="batch-prompt-popover fixed z-[9999] rounded-lg border border-[var(--border)] bg-[var(--popover)] p-3 text-sm text-[var(--popover-foreground)] shadow-xl"
        :style="promptPopover.style"
        @mouseenter="cancelPromptPopoverClose"
        @mouseleave="schedulePromptPopoverClose"
      >
        <div class="mb-2 flex items-center justify-between gap-3">
          <span class="text-xs font-medium text-[var(--muted-foreground)]">{{ t('batchImage.promptPopover.title') }}</span>
          <button
            type="button"
            class="rounded-md px-2 py-1 text-xs font-medium text-[var(--brand-text)] transition-colors hover:bg-[color-mix(in_srgb,var(--brand)_10%,var(--card))] focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--brand)]"
            @click="copyPromptPopover"
          >
            {{ t('common.copy') }}
          </button>
        </div>
        <p class="max-h-48 overflow-y-auto whitespace-pre-wrap break-words leading-6 selection:bg-[color-mix(in_srgb,var(--brand)_10%,var(--card))] selection:text-[var(--brand-text)]">
          {{ promptPopover.text }}
        </p>
      </div>
    </Teleport>

    <BaseDialog :show="!!currentJob" :title="t('batchImage.detail.title')" width="extra-wide" @close="closeDetail">
      <div v-if="currentJob" class="space-y-4">
        <div>
          <h3 class="text-lg font-semibold">{{ currentJob.task_name || defaultTaskName(currentJob.created_at) }}</h3>
          <p class="mt-1 break-all font-mono text-xs text-[var(--muted-foreground)]">{{ currentJob.id }}</p>
          <p class="mt-2 text-sm text-[var(--muted-foreground)]">{{ currentJob.model }} · {{ keyForSelectedBatch()?.name }}</p>
        </div>
        <div class="rounded-lg border border-[var(--border)] bg-[var(--surface-2)] px-4 py-4 tabular-nums">
          <div class="grid gap-x-6 gap-y-3 sm:grid-cols-2 lg:grid-cols-4">
            <div class="min-w-0 text-center">
              <p class="text-xs text-[var(--muted-foreground)]">{{ t('common.status') }}</p>
              <div class="mt-1 flex justify-center">
                <span :class="statusBadgeClass(currentDisplayJob || currentJob)" class="badge whitespace-nowrap">
                  <span class="mr-1.5 h-1.5 w-1.5 rounded-full bg-current" aria-hidden="true" />
                  {{ statusLabel(currentDisplayJob || currentJob) }}
                </span>
              </div>
            </div>
            <div class="min-w-0 text-center">
              <p class="text-xs text-[var(--muted-foreground)]">{{ hasChildJobs(currentJob.id) ? t('batchImage.detail.aggregatedResult') : t('batchImage.detail.result') }}</p>
              <p class="mt-1 flex items-center justify-center gap-2 font-medium tabular-nums">
              <span class="text-emerald-600 dark:text-emerald-300">{{ (currentDisplayJob || currentJob).success_count }}</span>
              <span class="text-[var(--muted-foreground)]">/</span>
              <span>{{ (currentDisplayJob || currentJob).item_count }}</span>
            </p>
              <p class="mt-1 text-xs text-[var(--muted-foreground)]">{{ copy('失败', 'Failed') }} {{ (currentDisplayJob || currentJob).fail_count }}</p>
            </div>
            <div class="min-w-0 text-center">
              <p class="text-xs text-[var(--muted-foreground)]">{{ t('batchImage.detail.cost') }}</p>
              <p class="mt-1 truncate font-medium text-[var(--foreground)]">{{ costLabel(currentDisplayJob || currentJob) }}</p>
            </div>
            <div class="min-w-0 text-center">
              <p class="text-xs text-[var(--muted-foreground)]">{{ t('batchImage.detail.downloadStatus') }}</p>
              <p class="mt-1 truncate font-medium text-[var(--foreground)]">
              {{ currentJob.downloaded_at ? formatDate(currentJob.downloaded_at) : t('batchImage.list.notDownloaded') }}
            </p>
            </div>
          </div>
        </div>

        <div class="flex flex-wrap items-center justify-between gap-3">
          <h3 class="text-sm font-semibold text-[var(--foreground)]">{{ t('batchImage.detail.items') }}</h3>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="refreshing || loadingItems" @click="refreshDetail">
            <Icon name="refresh" size="sm" class="mr-1.5" :class="refreshing || loadingItems ? 'motion-safe:animate-spin' : ''" />
            {{ t('common.refresh') }}
          </button>
        </div>

        <div v-if="items.length" class="overflow-x-auto rounded-lg border border-[var(--border)] bg-[var(--card)]">
          <table class="w-full min-w-[860px] table-fixed divide-y divide-[var(--border)] text-sm">
            <colgroup>
              <col class="w-[18%]" />
              <col class="w-[34%]" />
              <col class="w-[12%]" />
              <col class="w-[10%]" />
              <col class="w-[26%]" />
            </colgroup>
            <thead class="bg-[var(--surface-2)]">
              <tr>
                <th class="px-3 py-3 text-center text-sm font-medium text-[var(--muted-foreground)]">Custom ID</th>
                <th class="px-3 py-3 text-left text-sm font-medium text-[var(--muted-foreground)]">Prompt</th>
                <th class="px-3 py-3 text-center text-sm font-medium text-[var(--muted-foreground)]">{{ t('common.status') }}</th>
                <th class="px-3 py-3 text-center text-sm font-medium text-[var(--muted-foreground)]">{{ t('batchImage.detail.preview') }}</th>
                <th class="px-3 py-3 text-center text-sm font-medium text-[var(--muted-foreground)]">{{ t('batchImage.detail.result') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-[var(--border)]">
              <tr
                v-for="item in items"
                :key="itemPreviewKey(item)"
                class="align-middle"
                :class="detailItemRowClass(item)"
              >
                <td class="px-3 py-2.5 text-center">
                  <span
                    class="block min-w-0 truncate font-mono text-sm"
                    :class="isRecoveredOriginalFailure(item) ? 'text-[var(--muted-foreground)] ' : 'text-[var(--foreground)] '"
                    :title="item.custom_id"
                  >
                    {{ item.custom_id }}
                  </span>
                </td>
                <td class="px-3 py-2.5 text-left" :class="isRecoveredOriginalFailure(item) ? 'text-[var(--muted-foreground)] ' : 'text-[var(--foreground)] '">
                  <div
                    class="batch-prompt-trigger cursor-default truncate rounded px-1 text-sm leading-6 focus:outline-none"
                    tabindex="0"
                    @pointerenter="schedulePromptPopoverOpen($event, item.prompt_preview || '-')"
                    @pointerleave="schedulePromptPopoverClose"
                    @mouseenter="schedulePromptPopoverOpen($event, item.prompt_preview || '-')"
                    @mouseleave="schedulePromptPopoverClose"
                    @click="showPromptPopover($event, item.prompt_preview || '-')"
                    @focus="showPromptPopover($event, item.prompt_preview || '-')"
                    @focusin="showPromptPopover($event, item.prompt_preview || '-')"
                    @blur="schedulePromptPopoverClose"
                  >
                    {{ item.prompt_preview || '-' }}
                  </div>
                </td>
                <td class="px-3 py-2.5 text-center">
                  <span :class="itemDisplayStatusBadgeClass(item)" class="badge max-w-full truncate whitespace-nowrap" :title="itemDisplayStatusLabel(item)">
                    {{ itemDisplayStatusLabel(item) }}
                  </span>
                </td>
                <td class="px-3 py-2.5 text-center">
                  <div class="mx-auto h-12 w-12 overflow-hidden rounded-md border border-[var(--border)] bg-[var(--surface-2)]">
                    <button
                      v-if="itemPreviewUrls[itemPreviewKey(item)] && !previewErrorIds.has(itemPreviewKey(item))"
                      type="button"
                      class="block h-full w-full overflow-hidden"
                      :title="t('batchImage.detail.previewZoom', { id: item.custom_id })"
                      @click="openImagePreview(item)"
                    >
                      <img
                        :src="itemPreviewUrls[itemPreviewKey(item)]"
                        class="h-full w-full object-cover"
                        alt=""
                        @error="handlePreviewError(itemPreviewKey(item))"
                      />
                    </button>
                    <button
                      v-else-if="canLoadItemPreview(item)"
                      type="button"
                      class="flex h-full w-full items-center justify-center text-[var(--muted-foreground)] transition-colors hover:bg-[var(--surface-2)] hover:text-[var(--brand-text)] disabled:cursor-wait disabled:opacity-70"
                      :disabled="previewLoadingIds.has(itemPreviewKey(item))"
                      :title="previewErrorIds.has(itemPreviewKey(item)) ? t('batchImage.detail.previewReload') : t('batchImage.detail.previewLoad')"
                      @click="loadItemPreview(item)"
                    >
                      <Icon :name="previewLoadingIds.has(itemPreviewKey(item)) ? 'refresh' : 'eye'" size="sm" :class="previewLoadingIds.has(itemPreviewKey(item)) ? 'motion-safe:animate-spin' : ''" />
                    </button>
                    <div v-else class="flex h-full w-full items-center justify-center text-[var(--muted-foreground)]" :title="item.image_count > 0 ? t('batchImage.detail.previewUnavailable') : t('batchImage.detail.noImage')">
                      <Icon name="document" size="sm" />
                    </div>
                  </div>
                </td>
                <td class="px-3 py-2.5 text-center">
                  <span
                    class="inline-flex max-w-full items-center justify-center whitespace-normal break-words rounded-md px-2.5 py-1 text-left text-xs font-medium leading-5 ring-1 ring-inset"
                    :class="itemResultClass(item)"
                    :title="itemResultLabel(item)"
                  >
                    {{ itemResultLabel(item) }}
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="rounded-lg border border-dashed border-[var(--border)] py-10 text-center">
          <Icon name="refresh" size="lg" class="mx-auto mb-3 text-[var(--muted-foreground)]" :class="loadingItems ? 'motion-safe:animate-spin' : ''" />
          <p class="text-sm font-medium text-[var(--foreground)]">
            {{ loadingItems ? t('batchImage.detail.loadingItems') : t('batchImage.detail.noItems') }}
          </p>
          <p v-if="!loadingItems" class="mt-1 text-sm text-[var(--muted-foreground)]">
            {{ t('batchImage.detail.noItemsHint') }}
          </p>
        </div>
      </div>

      <template #footer>
        <div class="flex flex-wrap justify-end gap-2">
	          <button type="button" class="btn btn-secondary" :disabled="!currentJob || !canCancel(currentJob) || cancelling" @click="cancelSelected">
	            <Icon v-if="cancelling" name="refresh" size="sm" class="mr-2 motion-safe:animate-spin" />
	            {{ t('batchImage.actions.cancelJob') }}
	          </button>
	          <button
	            v-if="currentJob && currentDisplayJob && canRetry(currentDisplayJob)"
	            type="button"
	            class="btn btn-secondary inline-flex min-w-[116px] items-center justify-center"
	            :disabled="retryingBatchId === currentJob.id"
	            @click="retrySelected"
	          >
	            <Icon name="refresh" size="sm" class="mr-2" :class="currentJob && retryingBatchId === currentJob.id ? 'motion-safe:animate-spin' : ''" />
	            {{ t('batchImage.actions.retryFailedItems') }}
	          </button>
	          <button
            type="button"
            class="btn btn-primary inline-flex min-w-[112px] items-center justify-center"
            :disabled="!currentJob || !canDownload(currentJob) || downloading"
            @click="downloadSelected"
          >
            <Icon
              :name="currentJob && isDownloadingJob(currentJob.id) ? 'refresh' : 'download'"
              size="sm"
              class="mr-2"
              :class="currentJob && isDownloadingJob(currentJob.id) ? 'motion-safe:animate-spin' : ''"
            />
            {{ t('batchImage.actions.downloadZip') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog :show="!!previewImageItem" :title="previewImageItem?.custom_id || t('batchImage.imagePreview.title')" width="extra-wide" :z-index="60" @close="closeImagePreview">
      <div class="space-y-3">
        <div class="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-100">
          {{ t('batchImage.imagePreview.notice') }}
        </div>
        <div class="flex min-h-[420px] items-center justify-center rounded-lg bg-[var(--surface-2)] p-4">
          <img
            v-if="previewImageUrl"
            :src="previewImageUrl"
            class="max-h-[70vh] max-w-full rounded-md object-contain"
            :alt="previewImageItem?.custom_id || ''"
          />
        </div>
      </div>
    </BaseDialog>

    <BaseDialog :show="showCreateModal" :title="t('batchImage.create.title')" width="wide" @close="closeCreateModal">
      <form class="batch-create-form space-y-3" @submit.prevent="submitJob">
        <fieldset :disabled="submitting || readingReferences" class="grid min-w-0 gap-3 sm:grid-cols-2">
          <div class="md:col-span-2">
            <label for="batch-task-name" class="input-label">{{ t('batchImage.create.taskName') }}</label>
            <input
              id="batch-task-name"
              v-model="form.taskName"
              type="text"
              maxlength="255"
              class="input"
              :placeholder="t('batchImage.create.taskNamePlaceholder')"
            />
          </div>

          <div>
            <label for="batch-api-key" class="input-label">API Key</label>
            <select id="batch-api-key" v-model.number="form.apiKeyId" class="input" :disabled="loadingKeys">
              <option :value="0">{{ loadingKeys ? t('batchImage.create.loadingKeys') : copy('请选择活跃 API Key', 'Select an active API key') }}</option>
              <option v-for="key in activeApiKeys" :key="key.id" :value="key.id">
                {{ key.name }}{{ key.group?.name ? ` · ${key.group.name}` : '' }}
              </option>
            </select>
            <p v-if="!loadingKeys && !hasAnyActiveKey" class="input-hint">
              {{ copy('当前尚无可用 API 密钥', 'No active API key is available yet.') }}
            </p>
          </div>

          <div>
            <label for="batch-model" class="input-label">{{ t('batchImage.create.model') }}</label>
            <select id="batch-model" v-model="form.model" class="input" :disabled="loadingModels || availableBatchImageModels.length === 0">
              <option v-if="loadingModels" value="">{{ batchImageText('loadingModels') }}</option>
              <option v-else-if="availableBatchImageModels.length === 0" value="">{{ batchImageText('noModels') }}</option>
              <option v-for="model in availableBatchImageModels" :key="model.value" :value="model.value">
                {{ model.label }}
              </option>
            </select>
            <p v-if="modelLoadError" class="input-hint text-amber-600 dark:text-amber-400">
              {{ modelLoadError }}
            </p>
            <p v-else-if="selectedApiKey && !loadingModels && availableBatchImageModels.length === 0" class="input-hint text-amber-600 dark:text-amber-400">
              {{ batchImageText('noModelsHint') }}
            </p>
          </div>

          <div v-if="selectedKeyNeedsSecret" class="sm:col-span-2">
            <label for="batch-key-secret" class="input-label">{{ copy('完整 API Key', 'Full API key') }}</label>
            <div class="flex gap-2">
              <input id="batch-key-secret" v-model="credentialDraft" type="password" autocomplete="off" spellcheck="false" class="input min-w-0 flex-1" :placeholder="copy('粘贴创建密钥时保存的完整值', 'Paste the full key saved when it was created')" @keydown.enter.prevent="connectApiKey" />
              <button type="button" class="btn btn-secondary" :disabled="!credentialDraft.trim() || loadingModels" @click="connectApiKey">{{ copy('连接', 'Connect') }}</button>
            </div>
            <p class="input-hint">{{ copy('仅在当前页面内存中使用，离开后清除，不写入浏览器存储。', 'Used only in this page’s memory; cleared when you leave and never persisted in browser storage.') }}</p>
          </div>

          <div>
            <label for="batch-image-size" class="input-label">{{ t('batchImage.create.imageSize') }}</label>
            <select id="batch-image-size" v-model="form.imageSize" class="input">
              <option>1K</option><option>2K</option><option>4K</option>
            </select>
            <p v-if="form.imageSize !== '1K'" class="input-hint">{{ copy('此尺寸需服务端支持；如被拒绝，请选择 1K。', 'This size requires server support. Choose 1K if rejected.') }}</p>
          </div>

          <div>
            <label for="batch-output-format" class="input-label">{{ t('batchImage.create.outputFormat') }}</label>
            <select id="batch-output-format" v-model="form.responseMimeType" class="input">
              <option value="image/png">PNG</option>
              <option value="image/jpeg">JPEG</option>
              <option value="image/webp">WebP</option>
            </select>
          </div>

        </fieldset>

        <div class="space-y-3">
          <div class="flex items-center justify-between gap-3">
            <label for="batch-prompts" class="input-label mb-0">{{ copy('Prompt（每行一条）', 'Prompts (one per line)') }}</label>
            <span class="text-xs tabular-nums text-[var(--muted-foreground)]">{{ estimatedOutputCount }} / {{ BATCH_IMAGE_MAX_OUTPUTS_PER_JOB }} {{ copy('张输出', 'images') }}</span>
          </div>
          <div class="rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-3">
            <textarea
              id="batch-prompts"
              v-model="promptDraft"
              :disabled="submitting"
              rows="4"
              class="input w-full resize-y text-sm leading-5"
              :placeholder="copy('每行输入一个提示词，可直接粘贴多行', 'Enter one prompt per line, or paste multiple lines')"
            />
            <div class="mt-2 grid gap-2 sm:grid-cols-2">
              <label for="batch-custom-id" class="text-xs text-[var(--muted-foreground)]">Custom ID {{ copy('前缀（可选）', 'prefix (optional)') }}
              <input
                id="batch-custom-id"
                v-model="customIdDraft"
                :disabled="submitting"
                type="text"
                maxlength="120"
                class="input mt-1 h-9 text-sm"
                placeholder="img"
              />
              </label>
              <label for="batch-output-count" class="text-xs text-[var(--muted-foreground)]">{{ t('batchImage.create.outputCountPerPrompt') }}
              <select
                id="batch-output-count"
                v-model.number="outputCountDraft"
                :disabled="submitting"
                class="batch-output-count-select input mt-1 h-9 text-sm"
                :title="t('batchImage.create.outputCountPerPrompt')"
                :aria-label="t('batchImage.create.outputCountPerPrompt')"
              >
                <option v-for="count in outputCountOptions" :key="count" :value="count">
                  {{ t('batchImage.create.outputCountOption', { n: count }, count) }}
                </option>
              </select>
              </label>
            </div>
            <div class="mt-3 flex flex-wrap items-center gap-2 rounded-lg border border-dashed border-[var(--border)] p-3" :class="{ 'bg-[var(--card)]': draggingReferences }" @dragover.prevent="draggingReferences = true" @dragleave.prevent="draggingReferences = false" @drop.prevent="dropReferenceImages">
              <label
                class="btn btn-secondary relative h-9 cursor-pointer justify-center text-sm focus-within:ring-2 focus-within:ring-[var(--ring)]"
                :class="referenceImageDrafts.length >= selectedModelReferenceLimit ? 'pointer-events-none opacity-60' : ''"
              >
                <Icon name="upload" size="sm" class="mr-1.5" />
                {{ t('batchImage.create.referenceImage') }}
                <input
                  type="file"
                  accept="image/png,image/jpeg,image/webp"
                  multiple
                  class="absolute inset-0 w-full cursor-pointer opacity-0"
                  :aria-label="t('batchImage.create.referenceImage')"
                  :disabled="submitting || readingReferences || referenceImageDrafts.length >= selectedModelReferenceLimit"
                  @change="handleReferenceImageFiles"
                />
              </label>
              <span class="flex-1 text-xs text-[var(--muted-foreground)]">{{ readingReferences ? copy('正在读取参考图…', 'Reading images…') : copy('拖拽或选择 PNG / JPEG / WebP，每张 ≤ 10 MB', 'Drop or choose PNG / JPEG / WebP, up to 10 MB each') }}</span>
              <button type="button" class="btn btn-secondary h-9 justify-center whitespace-nowrap px-4 text-sm" :disabled="submitting || readingReferences || !promptDraft.trim()" @click="addPromptRow">
                <Icon name="plus" size="sm" class="mr-1.5" />
                {{ t('common.add') }}
              </button>
            </div>
            <div v-if="referenceImageDrafts.length" class="mt-3 flex flex-wrap gap-2">
              <span
                v-for="(ref, refIndex) in referenceImageDrafts"
                :key="`${ref.name}-${refIndex}`"
                class="inline-flex max-w-full items-center gap-1 rounded-md border border-[var(--border)] bg-[var(--surface-2)] px-2 py-1 text-xs text-[var(--foreground)]"
              >
                <img :src="`data:${ref.mime_type};base64,${ref.data}`" :alt="ref.name" class="h-10 w-10 rounded object-cover" />
                <span class="max-w-[180px] truncate">{{ ref.name }}</span>
                <button type="button" class="text-[var(--muted-foreground)] hover:text-red-600" :disabled="submitting || readingReferences" :aria-label="t('batchImage.create.removeReferenceImage') + ' ' + ref.name" :title="t('batchImage.create.removeReferenceImage')" @click="removeReferenceImageDraft(refIndex)">
                  <Icon name="x" size="xs" />
                </button>
              </span>
            </div>
            <p class="mt-2 text-xs text-[var(--muted-foreground)]">
              {{ t('batchImage.create.limitsHint', { maxPerItem: BATCH_IMAGE_MAX_OUTPUTS_PER_ITEM, maxPerJob: BATCH_IMAGE_MAX_OUTPUTS_PER_JOB, refLimit: selectedModelReferenceLimit }) }}
            </p>
          </div>
          <div v-if="promptRows.length" class="max-h-36 overflow-y-auto rounded-lg border border-[var(--border)]">
            <div
              v-for="(row, index) in promptRows"
              :key="row.localId"
              class="flex items-center gap-3 border-b border-[var(--border)] px-3 py-2 last:border-b-0"
            >
              <span class="w-20 flex-shrink-0 font-mono text-xs text-[var(--muted-foreground)]">{{ row.custom_id }}</span>
              <p class="min-w-0 flex-1 truncate text-sm text-[var(--foreground)]">{{ row.prompt }}</p>
              <span v-if="row.output_count > 1" class="flex-shrink-0 text-xs text-[var(--muted-foreground)]">
                x{{ row.output_count }}
              </span>
              <span v-if="row.reference_images.length" class="flex-shrink-0 text-xs text-[var(--muted-foreground)]">
                {{ t('batchImage.create.referenceCount', { n: row.reference_images.length }, row.reference_images.length) }}
              </span>
              <button type="button" class="btn-ghost btn-icon flex-shrink-0 text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20" :disabled="submitting" :title="t('common.delete')" @click="removePromptRow(index)">
                <Icon name="trash" size="sm" />
              </button>
            </div>
          </div>
        </div>

	        <div class="text-xs leading-5 text-[var(--muted-foreground)]">
	          {{ t('batchImage.create.cancelNotice') }}
	        </div>
	        <div v-if="submitting" class="rounded-lg border border-sky-200 bg-sky-50 p-3 text-sm leading-6 text-sky-800 dark:border-sky-800 dark:bg-sky-950/30 dark:text-sky-100">
	          {{ t('batchImage.create.submittingNotice') }}
	        </div>
	      </form>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" :disabled="submitting" @click="closeCreateModal">{{ t('common.cancel') }}</button>
	          <button type="button" class="btn btn-primary inline-flex min-w-[120px] justify-center" :disabled="submitting || readingReferences || loadingModels || estimatedOutputCount > BATCH_IMAGE_MAX_OUTPUTS_PER_JOB || (parsedItems.length === 0 && !promptDraft.trim()) || !selectedApiKey || !form.model" @click="submitJob">
            <Icon v-if="submitting" name="refresh" size="sm" class="mr-2 motion-safe:animate-spin" />
            {{ submitting ? t('common.submitting') : t('batchImage.actions.submitJob') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog :show="showGuideModal" :title="t('batchImage.guide.title')" width="wide" @close="showGuideModal = false">
	      <div class="space-y-5">
	        <section class="space-y-3">
	          <h3 class="text-sm font-semibold text-[var(--foreground)]">{{ t('batchImage.guide.uiTitle') }}</h3>
	          <div class="rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-4 text-sm leading-7 text-[var(--foreground)]">
	            <p>{{ copy('1. 选择活跃 API Key，再选择该密钥可用的图像模型；服务端会检查分组权限。', '1. Choose an active API key and an available image model. The server checks group permissions.') }}</p>
	            <p>{{ copy('2. 每行填写一条 Prompt，设置 Custom ID 前缀与每条 1–4 张输出，总计最多 200 张。参考图应用于本次添加的每条 Prompt。', '2. Enter one prompt per line, an optional Custom ID prefix, and 1–4 images per prompt (up to 200 total). Reference images apply to each prompt added together.') }}</p>
	            <p>{{ t('batchImage.guide.step3') }}</p>
	            <p>{{ copy('4. 完成后下载含图像与 JSONL 清单的 ZIP；失败项可直接重试并追踪独立子任务。筛选与翻页会保留所选父任务。', '4. Download ZIPs containing images and JSONL manifests. Retry failed items as child jobs. Selected parent jobs persist across filters and pages.') }}</p>
	          </div>
	        </section>
            <section class="space-y-2 text-sm">
              <h3 class="font-semibold">{{ copy('API 调用', 'API requests') }}</h3>
              <p class="text-xs text-[var(--muted-foreground)]">{{ copy('通过 Authorization: Bearer <API_KEY> 认证，提交时附带唯一 Idempotency-Key。', 'Authenticate with Authorization: Bearer <API_KEY> and use a unique Idempotency-Key for submissions.') }}</p>
              <pre class="overflow-x-auto rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-3 text-xs leading-6">GET    /v1/images/batches/models
POST   /v1/images/batches
GET    /v1/images/batches/{id}
GET    /v1/images/batches/{id}/items
GET    /v1/images/batches/{id}/items/{custom_id}/content
GET    /v1/images/batches/{id}/download
POST   /v1/images/batches/{id}/cancel
DELETE /v1/images/batches/{id}</pre>
            </section>
	        <section class="space-y-3">
	          <div class="flex flex-wrap items-center justify-between gap-3">
	            <h3 class="text-sm font-semibold text-[var(--foreground)]">{{ t('batchImage.guide.skillTitle') }}</h3>
	            <p class="text-xs text-[var(--muted-foreground)]">{{ t('batchImage.guide.skillDesc') }}</p>
	          </div>
	        <textarea
	          :value="agentInstruction"
	          readonly
	          class="min-h-[420px] w-full resize-y rounded-lg border border-white/10 bg-[#161616] p-4 font-mono text-xs leading-6 text-[#f4f4f5] outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)]"
	        />
	        </section>
	      </div>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="showGuideModal = false">{{ t('common.close') }}</button>
          <button type="button" class="btn btn-primary" @click="copyInstruction">
            <Icon name="copy" size="sm" class="mr-2" />
            {{ t('batchImage.actions.copyInstruction') }}
          </button>
        </div>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import { getPersistedPageSize, setPersistedPageSize } from '@/composables/usePersistedPageSize'
import { useAppStore } from '@/stores/app'
import { keysAPI } from '@/api'
import { buildGatewayUrl } from '@/api/client'
import {
  cancelBatchImageJob,
  deleteBatchImageJobRecord,
  downloadBatchImageZip,
  getBatchImageItemContent,
  getBatchImageJob,
  listBatchImageJobs,
  listBatchImageItems,
  listBatchImageModels,
  saveBlob,
  submitBatchImageJob,
  type BatchImageItem,
  type BatchImageJob,
  type BatchImageReferenceImage,
  type BatchImageStatus,
  type BatchImageSubmitItem,
} from '@/api/batchImage'
import type { ApiKey } from '@/types'
import type { Column } from '@/components/common/types'

type BatchImageJobRow = Pick<BatchImageJob, 'id' | 'task_name' | 'parent_batch_id' | 'status' | 'model' | 'provider' | 'item_count' | 'success_count' | 'fail_count' | 'estimated_cost' | 'hold_amount' | 'actual_cost' | 'created_at' | 'downloaded_at'> & {
  api_key_id: number
  api_key_name: string
  child_count: number
  is_child?: boolean
}

type BatchImageDetailItem = BatchImageItem & {
  batch_id: string
  source_task_name: string
}

type PromptRow = {
  localId: string
  custom_id: string
  prompt: string
  output_count: number
  reference_images: BatchImageReferenceImage[]
}

type ReferenceImageDraft = BatchImageReferenceImage & {
  name: string
  size: number
}

type PreviewCacheRecord = {
  key: string
  blob: Blob
  size: number
  createdAt: number
  lastAccessedAt: number
}

type PreviewImageSource = ImageBitmap | HTMLImageElement

const TERMINAL_STATUSES = new Set(['completed', 'failed', 'cancelled', 'output_deleted'])
const PREVIEW_CACHE_DB_NAME = 'omnihive-batch-image-preview-cache'
const LEGACY_PREVIEW_CACHE_DB_NAME = 'sub2api-batch-image-preview-cache'
const PREVIEW_CACHE_STORE_NAME = 'thumbnails'
const PREVIEW_THUMBNAIL_MAX_EDGE = 360
const PREVIEW_THUMBNAIL_QUALITY = 0.72
const PREVIEW_CACHE_MAX_AGE_MS = 3 * 24 * 60 * 60 * 1000
const PREVIEW_CACHE_MAX_ENTRIES = 120
const PREVIEW_CACHE_MAX_BYTES = 48 * 1024 * 1024
const BATCH_IMAGE_MAX_OUTPUTS_PER_ITEM = 4
const BATCH_IMAGE_MAX_OUTPUTS_PER_JOB = 200
const ZIP_CRC_TABLE = Array.from({ length: 256 }, (_, value) => {
  let checksum = value
  for (let bit = 0; bit < 8; bit += 1) checksum = (checksum >>> 1) ^ ((checksum & 1) ? 0xedb88320 : 0)
  return checksum >>> 0
})
const outputCountOptions = Array.from({ length: BATCH_IMAGE_MAX_OUTPUTS_PER_ITEM }, (_, index) => index + 1)
const batchPageSizeOptions: SelectOption[] = [20, 50, 100].map(size => ({ value: size, label: String(size) }))

const appStore = useAppStore()
const { copyToClipboard } = useClipboard()
const { t, locale } = useI18n()
const copy = (zh: string, en: string) => isZhLocale() ? zh : en

const columns = computed<Column[]>(() => [
  { key: 'select', label: '', sortable: false, class: 'w-12 text-center' },
  { key: 'id', label: copy('任务', 'Task'), sortable: false, class: 'w-[240px] max-w-[240px] text-left' },
  { key: 'model', label: copy('模型 / Key', 'Model / Key'), sortable: false, class: 'w-[220px] max-w-[220px] text-left' },
  { key: 'status', label: t('common.status'), sortable: false, class: 'w-28 text-left' },
  { key: 'counts', label: t('batchImage.columns.result'), sortable: false, class: 'w-24 text-left' },
  { key: 'downloaded', label: copy('下载', 'Download'), sortable: false, class: 'w-24 text-left' },
  { key: 'actions', label: t('common.actions'), sortable: false, class: 'w-[300px] text-left' },
])

const statusFilterOptions = computed<SelectOption[]>(() => [
  { value: '', label: t('batchImage.filters.allStatuses') },
  { value: 'queued', label: t('batchImage.status.queued') },
  { value: 'running', label: t('batchImage.status.running') },
  { value: 'processing_results', label: t('batchImage.status.processingResults') },
  { value: 'settling', label: t('batchImage.status.settling') },
  { value: 'completed', label: t('batchImage.status.completed') },
  { value: 'partial', label: t('batchImage.status.partialSuccess') },
  { value: 'failed', label: copy('已失败', t('batchImage.status.failed')) },
  { value: 'cancelled', label: t('batchImage.status.cancelled') },
  { value: 'output_deleted', label: t('batchImage.status.outputDeleted') },
])

const downloadFilterOptions = computed<SelectOption[]>(() => [
  { value: '', label: t('batchImage.filters.allDownloadStates') },
  { value: 'true', label: t('batchImage.filters.downloaded') },
  { value: 'false', label: t('batchImage.filters.notDownloaded') },
])

const form = reactive({
  apiKeyId: 0,
  taskName: '',
  model: '',
  imageSize: '1K',
  responseMimeType: 'image/png',
})

const filters = reactive({
  taskName: '',
  apiKeyId: '',
  status: '',
  downloaded: '',
})

const pagination = reactive({
  page: 1,
  page_size: Math.min(getPersistedPageSize(20), 100),
  has_more: false,
})

const apiKeys = ref<ApiKey[]>([])
const sessionCredentials = reactive<Record<number, string>>({})
const credentialDraft = ref('')
const loadingKeys = ref(false)
const loadingJobs = ref(false)
const submitting = ref(false)
const refreshing = ref(false)
const cancelling = ref(false)
const downloading = ref(false)
const downloadingBatchId = ref('')
const retryingBatchId = ref('')
const bulkDownloading = ref(false)
const bulkDeleting = ref(false)
const deletingBatchId = ref('')
const loadingItems = ref(false)
const loadingModels = ref(false)
const showCreateModal = ref(false)
const showGuideModal = ref(false)
const currentJob = ref<BatchImageJob | null>(null)
const selectedBatchId = ref('')
const selectedBatchApiKeyId = ref(0)
const items = ref<BatchImageDetailItem[]>([])
const batchJobs = ref<BatchImageJobRow[]>([])
const selectedJobIds = ref(new Set<string>())
const expandedParentIds = ref(new Set<string>())
const promptRows = ref<PromptRow[]>([])
const promptDraft = ref('')
const customIdDraft = ref('')
const outputCountDraft = ref(1)
const referenceImageDrafts = ref<ReferenceImageDraft[]>([])
const readingReferences = ref(false)
const draggingReferences = ref(false)
const itemPreviewUrls = reactive<Record<string, string>>({})
const previewLoadingIds = ref(new Set<string>())
const previewErrorIds = ref(new Set<string>())
const previewImageItem = ref<BatchImageItem | null>(null)
const availableBatchImageModels = ref<Array<{ value: string; label: string }>>([])
const modelLoadError = ref('')
const promptPopover = reactive({
  visible: false,
  text: '',
  style: {} as Record<string, string>,
})
let modelRequestSeq = 0
let jobsRequestSeq = 0
let detailRequestSeq = 0
let itemsRequestSeq = 0
let previewGeneration = 0
let pollTimer: ReturnType<typeof setInterval> | null = null
let previewCacheDBPromise: Promise<IDBDatabase | null> | null = null
let previewCacheCleanupTimer: ReturnType<typeof setInterval> | null = null
let promptPopoverCloseTimer: ReturnType<typeof setTimeout> | null = null
let promptPopoverOpenTimer: ReturnType<typeof setTimeout> | null = null
let activePromptPopoverTarget: HTMLElement | null = null

const activeApiKeys = computed(() => apiKeys.value.filter(key => key.status === 'active')
  .map(key => ({ ...key, key: sessionCredentials[key.id] || key.key })))
const credentialedKeys = computed(() => activeApiKeys.value.filter(key => !isMaskedCredential(key.key)))
const hasAnyActiveKey = computed(() => activeApiKeys.value.length > 0)
const hasGeminiBatchKey = computed(() => activeApiKeys.value.some(key =>
  key.group?.platform === 'gemini' && key.group?.allow_batch_image_generation === true,
))

const selectedApiKey = computed(() =>
  activeApiKeys.value.find((key) => key.id === Number(form.apiKeyId)) || null,
)
const selectedKeyNeedsSecret = computed(() => !!selectedApiKey.value && isMaskedCredential(selectedApiKey.value.key))

function isMaskedCredential(value: string | undefined) {
  return !value || /[*•…]|\.\.\./.test(value)
}

async function connectApiKey() {
  if (!selectedApiKey.value || !credentialDraft.value.trim() || loadingModels.value) return
  const keyId = selectedApiKey.value.id
  const secret = credentialDraft.value.trim()
  if (isMaskedCredential(secret)) return
  sessionCredentials[keyId] = secret
  credentialDraft.value = ''
  await loadAvailableModels()
  if (modelLoadError.value) {
    delete sessionCredentials[keyId]
    return
  }
  await loadBatchJobs()
}

const apiKeyFilterOptions = computed<SelectOption[]>(() => [
  { value: '', label: copy('全部 Key', 'All keys') },
  ...activeApiKeys.value.map(key => ({
    value: String(key.id),
    label: key.name || `API Key #${key.id}`,
  })),
])

const selectedRows = computed(() =>
  batchJobs.value.filter(job => selectedJobIds.value.has(job.id)),
)

const childrenByParent = computed(() => {
  const groups = new Map<string, BatchImageJobRow[]>()
  for (const job of batchJobs.value) {
    if (!job.parent_batch_id) continue
    const rows = groups.get(job.parent_batch_id) || []
    rows.push(job)
    groups.set(job.parent_batch_id, rows)
  }
  for (const rows of groups.values()) {
    rows.sort((a, b) => a.created_at - b.created_at)
  }
  return groups
})

const parentJobs = computed(() => batchJobs.value.filter(job => !job.parent_batch_id))
const filteredParentJobs = computed(() => parentJobs.value.filter(job => {
  const display = displayJob(job)
  const partial = display.status === 'completed' && display.success_count > 0 && display.fail_count > 0
  return (!filters.taskName.trim() || job.task_name.toLocaleLowerCase().includes(filters.taskName.trim().toLocaleLowerCase()))
    && (!filters.apiKeyId || job.api_key_id === Number(filters.apiKeyId))
    && (!filters.status || (filters.status === 'partial' ? partial : display.status === filters.status))
    && (!filters.downloaded || Boolean(job.downloaded_at) === (filters.downloaded === 'true'))
}))

const totalPages = computed(() => Math.max(1, Math.ceil(filteredParentJobs.value.length / pagination.page_size)))
const visibleParentJobs = computed(() => filteredParentJobs.value.slice(
  (pagination.page - 1) * pagination.page_size, pagination.page * pagination.page_size,
))

const visibleBatchJobs = computed(() => {
  const rows: BatchImageJobRow[] = []
  for (const job of visibleParentJobs.value) {
    rows.push(job)
    if (expandedParentIds.value.has(job.id)) {
      rows.push(...(childrenByParent.value.get(job.id) || []).map(child => ({ ...child, is_child: true })))
    }
  }
  return rows
})

const selectedDownloadableRows = computed(() =>
  selectedRows.value.flatMap(job => downloadableRows(job.id)),
)

const allVisibleSelected = computed(() =>
  visibleParentJobs.value.length > 0 && visibleParentJobs.value.every(job => selectedJobIds.value.has(job.id)),
)

const someVisibleSelected = computed(() =>
  visibleParentJobs.value.some(job => selectedJobIds.value.has(job.id)) && !allVisibleSelected.value,
)

const previewImageUrl = computed(() => {
  const item = previewImageItem.value
  if (!item) return ''
  return itemPreviewUrls[itemPreviewKey(item)] || ''
})

const recoveredOriginalCustomIds = computed(() => {
  const rootBatchId = detailRootBatchId()
  if (!rootBatchId) return new Set<string>()
  const ids = new Set<string>()
  for (const item of items.value) {
    if (!isChildDetailItem(item) || !isSuccessfulImageItem(item)) continue
    const sourceCustomID = retrySourceCustomID(item.custom_id)
    if (sourceCustomID) ids.add(sourceCustomID)
  }
  return ids
})

const currentDisplayJob = computed(() => {
  if (!currentJob.value) return null
  return displayJob(currentJob.value)
})

const endpointBase = computed(() => {
  const configured = appStore.apiBaseUrl?.trim()
  if (configured) return configured.replace(/\/+$/, '')
  if (typeof window !== 'undefined') return window.location.origin.replace(/\/+$/, '')
  return '<你的 OmniHive API 端点>'
})

const selectedModelReferenceLimit = computed(() => referenceImageLimitForModel(form.model))

const estimatedOutputCount = computed(() =>
  promptRows.value.reduce((sum, row) => sum + normalizeOutputCount(row.output_count), 0)
    + promptDraft.value.split(/\r?\n/).filter(line => line.trim()).length * normalizeOutputCount(outputCountDraft.value),
)

const parsedItems = computed<BatchImageSubmitItem[]>(() => {
  const used = new Set<string>()
  return promptRows.value
    .map((row, index) => {
      const customID = uniqueCustomID(row.custom_id || `img_${String(index + 1).padStart(3, '0')}`, used, index)
      const item: BatchImageSubmitItem = { custom_id: customID, prompt: row.prompt.trim() }
      const outputCount = normalizeOutputCount(row.output_count)
      if (outputCount > 1) {
        item.output_count = outputCount
      }
      if (row.reference_images.length) {
        item.reference_images = row.reference_images
      }
      return item
    })
    .filter(item => item.prompt)
})

function referenceImageLimitForModel(model: string) {
  const normalized = String(model || '').toLowerCase()
  if (normalized.includes('pro-image')) return 14
  if (normalized.includes('flash-image')) return 3
  return 0
}

const agentInstruction = computed(() => `---
name: omnihive-batch-image
description: 当用户希望用 Gemini/Vertex 批量生成图片、批量跑提示词、下载批量生图结果、重试失败图片时使用。
---

你是 Codex 中的批量生图执行 Agent。用户不需要手动填写页面表单；你应从当前聊天、用户给的文件、目录或上下文中整理任务名称、prompt 列表和输出目录，只有缺少关键决策时才向用户提问。

默认端点：
${endpointBase.value}

你需要自己完成：
1. 从用户聊天或附件中提取 prompt。每条 prompt 保留完整文本，按顺序生成稳定 custom_id，例如 img_001、img_002。
2. 从用户要求或上下文推断任务名称；没有明确名称时用当前时间生成任务名。
3. 从用户要求或上下文推断输出目录；如果用户没有说保存到哪里，才询问用户。
4. 提交前必须先计算 expected_output_count = 所有 item 的 output_count 之和。单个批量任务硬性最多 200 张输出图；超过 200 张必须拆成多组任务，不能提交一个超大任务，也不能把参考图附件上限当成生成张数上限。
5. 如果用户提供参考图，把参考图按用途绑定到具体 item。参考图只是输入附件，不是输出图数量。模型单条限制必须按模型执行：Gemini 2.5 Flash Image 每条最多 3 张参考图；Gemini 3 Pro Image 每条最多 14 张参考图。不要把后端附件风控理解成 Pro 单条能力：按 output_count 展开后，所有 item 的参考图附件总数还有内部保护阈值 1000 个，inline base64 参考图解码后总量最多 128MB。这个 1000 只是服务器拒绝异常请求的保护阈值，不是推荐规模；参考图很多或总请求体较大时应主动拆分任务。
6. 参考图会按 output_count 重复消耗输入 token；大量任务、重复复用同一张参考图或参考图总体积较大时，优先使用 gs:// file_uri 或拆分成多组任务。
7. 选择 API Key 和模型：先获取当前可用的批量生图 Key/模型；如果用户指定模型且该 Key 支持，则使用用户指定模型；否则使用该 Key 可用模型中的默认/第一个。不要展示或询问内部 provider 名称。
8. 调用批量生图 API 提交、轮询、下载，不要求用户去页面里手填。

API 调用规范：
- 模型：GET ${joinEndpointPath(endpointBase.value, '/v1/images/batches/models')}
- 提交：POST ${joinEndpointPath(endpointBase.value, '/v1/images/batches')}
- 查询：GET ${joinEndpointPath(endpointBase.value, '/v1/images/batches/{id}')}
- 明细：GET ${joinEndpointPath(endpointBase.value, '/v1/images/batches/{id}/items')}
- 下载：GET ${joinEndpointPath(endpointBase.value, '/v1/images/batches/{id}/download')}
- 取消：POST ${joinEndpointPath(endpointBase.value, '/v1/images/batches/{id}/cancel')}

提交请求体：
{
  "model": "<按所选 Key 可用模型填写>",
  "task_name": "<从聊天推断；为空则用当前时间>",
  "image_size": "1K",
  "response_mime_type": "image/png",
  "items": [
    {
      "custom_id": "img_001",
      "prompt": "<第一条完整 prompt>",
      "output_count": 1,
      "reference_images": [
        {
          "id": "face",
          "type": "subject",
          "mime_type": "image/png",
          "data": "<base64，不含 data:image/png;base64, 前缀>"
        }
      ]
    }
  ]
}

必须遵守：
- 不要把 API Key 写入仓库、日志、提交记录或最终回复。
- 不要把参考图 base64 写入最终回复、日志或公开文件。恢复记录中只保存参考图文件名、用途、数量和请求 JSON 文件路径；若请求 JSON 文件包含 base64，应保存在用户指定输出目录且不要提交到仓库。
- output_count 表示同一 prompt 和参考图重复生成几张，默认 1，每条最多 4；这不是依赖 Gemini 单次请求返回多图，而是系统展开成多个真实任务项。提交前必须确认预计输出图总数不超过 200，超过就拆分成多组任务。绝不能因为参考图附件有更高的内部保护阈值，就提交会生成超过 200 张图的任务。
- 当前对用户的批量生图计费仍按成功输出图片数量结算，不单独对参考图加价。可以向用户说明：参考图会产生少量上游输入 token 和临时存储成本，且会随 output_count 重复计算；页面显示的冻结/结算金额按输出图片数量计算。
- 提交成功后，必须立刻在输出目录写入本地恢复记录，例如 batch-image-resume.json。不要在恢复记录里保存 API Key。
- 恢复记录至少包含：endpoint、task_name、batch_id、model、output_dir、request_file、submitted_at、last_status、status_url、items_url、download_url、prompt_count、expected_output_count，以及可用于失败重试的 custom_id 到 prompt 映射或请求 JSON 文件路径。
- 每次查询状态后更新恢复记录，写入 last_checked_at、last_status、成功数、失败数、实际扣费和失败摘要。会话中断或暂停后，下次必须能凭该文件继续查询、下载或重试。
- 不要高频轮询。首次查询等待约 20 到 30 秒；queued 状态每 60 到 120 秒查询一次；如果连续 3 次仍是 queued，就先停止主动查询，告诉用户任务仍在排队，并保留恢复记录，之后可继续其他任务或等待用户稍后让你恢复。
- running 状态每约 60 秒查询一次，服务器压力大或大批量任务时可以更久；processing_results 等接近完成的状态可每 20 到 45 秒查询一次。
- 任务完成后报告任务名、任务 id、成功数、失败数、实际扣费和保存路径。
- 只下载成功图片。部分失败时，先展示失败 custom_id、错误码、错误来源和简要原因。
- 重试只能重试失败项，不能重复提交已成功项。若历史任务没有保存失败项 prompt，必须告诉用户无法自动重试，并询问用户是否提供原 prompt。
- 取消任务前必须提醒：已被系统索引为成功的图片仍会按成功项结算扣费，其余冻结金额会释放。
- 图片预览按需加载；不要为了查看列表自动批量加载图片内容。`)

function joinEndpointPath(base: string, path: string): string {
  return `${base.replace(/\/+$/, '')}/${path.replace(/^\/+/, '')}`
}

function uniqueCustomID(raw: string, used: Set<string>, index: number): string {
  const base = raw.replace(/[^\w.-]+/g, '_').replace(/^_+|_+$/g, '') || `img_${String(index + 1).padStart(3, '0')}`
  let candidate = base
  let suffix = 2
  while (used.has(candidate)) {
    candidate = `${base}_${suffix}`
    suffix += 1
  }
  used.add(candidate)
  return candidate
}

function normalizeOutputCount(value: unknown): number {
  const parsed = Math.floor(Number(value || 1))
  if (!Number.isFinite(parsed)) return 1
  return Math.min(BATCH_IMAGE_MAX_OUTPUTS_PER_ITEM, Math.max(1, parsed))
}

function addPromptRow() {
  if (submitting.value || readingReferences.value) return
  const prompts = promptDraft.value.split(/\r?\n/).map(prompt => prompt.trim()).filter(Boolean)
  if (!prompts.length) return
  if (estimatedOutputCount.value > BATCH_IMAGE_MAX_OUTPUTS_PER_JOB) {
    appStore.showError(batchImageText('tooManyOutputImages'))
    return
  }
  const outputCount = normalizeOutputCount(outputCountDraft.value)
  const used = new Set(promptRows.value.map(row => row.custom_id))
  promptRows.value = [
    ...promptRows.value,
    ...prompts.map((prompt, index) => {
      const position = promptRows.value.length + index
      const customID = uniqueCustomID(`${customIdDraft.value.trim() || 'img'}_${String(position + 1).padStart(3, '0')}`, used, position)
      return {
        localId: `${Date.now()}-${customID}`,
        custom_id: customID,
        prompt,
        output_count: outputCount,
        reference_images: referenceImageDrafts.value.map(({ name: _name, size: _size, ...reference }) => reference),
      }
    }),
  ]
  promptDraft.value = ''
  customIdDraft.value = ''
  outputCountDraft.value = 1
  referenceImageDrafts.value = []
}

function removePromptRow(index: number) {
  promptRows.value = promptRows.value.filter((_, currentIndex) => currentIndex !== index)
}

function removeReferenceImageDraft(index: number) {
  referenceImageDrafts.value = referenceImageDrafts.value.filter((_, currentIndex) => currentIndex !== index)
}

async function handleReferenceImageFiles(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files || [])
  input.value = ''
  await addReferenceImages(files)
}

async function dropReferenceImages(event: DragEvent) {
  draggingReferences.value = false
  await addReferenceImages(Array.from(event.dataTransfer?.files || []))
}

async function addReferenceImages(files: File[]) {
  if (submitting.value || readingReferences.value) return
  if (files.length === 0) return
  const limit = selectedModelReferenceLimit.value
  if (limit <= 0) {
    appStore.showError(t('batchImage.create.modelNoReferenceImages'))
    return
  }
  const slots = Math.max(0, limit - referenceImageDrafts.value.length)
  if (slots <= 0) {
    appStore.showError(t('batchImage.create.refLimitReached', { limit }))
    return
  }
  const accepted = files.slice(0, slots)
  if (accepted.length < files.length) {
    appStore.showError(t('batchImage.create.refLimitExceededIgnored', { limit }))
  }
  const next: ReferenceImageDraft[] = []
  const selectedModel = form.model
  readingReferences.value = true
  try {
    for (const file of accepted) {
    if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type)) {
      appStore.showError(t('batchImage.create.refFormatUnsupported'))
      continue
    }
    if (file.size > 10 * 1024 * 1024) {
      appStore.showError(t('batchImage.create.refFileTooLarge', { name: file.name }))
      continue
    }
    const data = await readFileAsBase64(file)
    next.push({
      id: file.name,
      type: 'reference',
      mime_type: file.type,
      data,
      name: file.name,
      size: file.size,
    })
    }
    if (showCreateModal.value && form.model === selectedModel) {
      referenceImageDrafts.value = [...referenceImageDrafts.value, ...next].slice(0, selectedModelReferenceLimit.value)
    }
  } catch {
    appStore.showError(copy('参考图读取失败，请重新选择文件。', 'Could not read the reference image. Choose the file again.'))
  } finally {
    readingReferences.value = false
  }
}

function readFileAsBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(reader.error || new Error('Failed to read file'))
    reader.onload = () => {
      const result = String(reader.result || '')
      resolve(result.includes(',') ? result.slice(result.indexOf(',') + 1) : result)
    }
    reader.readAsDataURL(file)
  })
}

async function loadApiKeys() {
  loadingKeys.value = true
  try {
    const keys: ApiKey[] = []
    let page = 1
    while (true) {
      const response = await keysAPI.list(page, 100, { status: 'active', sort_by: 'created_at', sort_order: 'desc' })
      keys.push(...(response.items || []))
      if (page >= (response.pages || 1) || !response.items?.length) break
      page += 1
    }
    if (!viewActive) return
    apiKeys.value = keys
    if (!selectedApiKey.value) {
      form.apiKeyId = activeApiKeys.value[0]?.id || 0
    }
    if (filters.apiKeyId && !activeApiKeys.value.some(key => String(key.id) === filters.apiKeyId)) {
      filters.apiKeyId = ''
    }
    if (!selectedApiKey.value) {
      availableBatchImageModels.value = []
      form.model = ''
    }
  } catch (error: any) {
    appStore.showError(batchImageErrorMessage(error, batchImageText('loadKeysFailed')))
  } finally {
    loadingKeys.value = false
  }
}

async function loadAvailableModels() {
  const key = selectedApiKey.value
  const requestID = ++modelRequestSeq
  modelLoadError.value = ''
  availableBatchImageModels.value = []
  form.model = ''
  if (!key || isMaskedCredential(key.key)) {
    loadingModels.value = false
    return
  }

  loadingModels.value = true
  try {
    const result = await listBatchImageModels(key.key)
    if (requestID !== modelRequestSeq) return
    const seen = new Set<string>()
    availableBatchImageModels.value = (result.data || [])
      .map(model => String(model.id || '').trim())
      .filter((model) => {
        if (!model || seen.has(model)) return false
        seen.add(model)
        return true
      })
      .map(model => ({ value: model, label: model }))
    form.model = availableBatchImageModels.value[0]?.value || ''
  } catch (error: any) {
    if (requestID !== modelRequestSeq) return
    modelLoadError.value = batchImageErrorMessage(error, batchImageText('loadModelsFailed'))
  } finally {
    if (requestID === modelRequestSeq) {
      loadingModels.value = false
    }
  }
}

async function refreshPage() {
  await loadApiKeys()
  if (!viewActive) return
  await Promise.all([loadAvailableModels(), loadBatchJobs()])
}

function applyFilters() {
  pagination.page = 1
}

function resetFilters() {
  filters.taskName = ''
  filters.apiKeyId = ''
  filters.status = ''
  filters.downloaded = ''
  applyFilters()
}

function toJobRow(job: BatchImageJob, key = selectedApiKey.value): BatchImageJobRow {
  return {
    id: job.id,
    task_name: job.task_name || defaultTaskName(job.created_at),
    parent_batch_id: job.parent_batch_id || null,
    status: job.status,
    model: job.model,
    provider: job.provider,
    item_count: job.item_count,
    success_count: job.success_count,
    fail_count: job.fail_count,
    estimated_cost: job.estimated_cost,
    hold_amount: job.hold_amount,
    actual_cost: job.actual_cost,
    created_at: job.created_at,
    downloaded_at: job.downloaded_at,
    api_key_id: key?.id || 0,
    api_key_name: key?.name || '',
    child_count: 0,
  }
}

function applyChildCounts(rows: BatchImageJobRow[]) {
  const counts = new Map<string, number>()
  for (const row of rows) {
    if (!row.parent_batch_id) continue
    counts.set(row.parent_batch_id, (counts.get(row.parent_batch_id) || 0) + 1)
  }
  return rows.map(row => ({ ...row, child_count: counts.get(row.id) || 0 }))
}

function displayJob<T extends Pick<BatchImageJob, 'id' | 'parent_batch_id' | 'status' | 'item_count' | 'success_count' | 'fail_count' | 'estimated_cost' | 'hold_amount' | 'actual_cost'>>(job: T): T {
  if (job.parent_batch_id) return job
  const children = childrenByParent.value.get(job.id) || []
  if (!children.length) return job

  const childSuccess = children.reduce((sum, child) => sum + child.success_count, 0)
  const childEstimated = children.reduce((sum, child) => sum + child.estimated_cost, 0)
  const childHold = children.reduce((sum, child) => sum + child.hold_amount, 0)
  const childActual = children.reduce((sum, child) => sum + (child.actual_cost || 0), 0)
  const childActualReady = children.every(child => child.actual_cost !== null)
  const successCount = Math.min(job.item_count, job.success_count + childSuccess)
  const failCount = Math.max(0, job.fail_count - childSuccess)
  const runningChild = children.find(child => !TERMINAL_STATUSES.has(child.status))
  const actualCost = job.actual_cost === null
    ? (childActualReady ? childActual : null)
    : job.actual_cost + childActual

  return {
    ...job,
    success_count: successCount,
    fail_count: failCount,
    status: runningChild?.status || (successCount === job.item_count && job.status !== 'output_deleted' ? 'completed' : job.status),
    estimated_cost: job.estimated_cost + childEstimated,
    hold_amount: job.hold_amount + childHold,
    actual_cost: actualCost,
  }
}

function hasChildJobs(batchId: string) {
  return (childrenByParent.value.get(batchId) || []).length > 0
}

function toggleChildRows(batchId: string) {
  const next = new Set(expandedParentIds.value)
  if (next.has(batchId)) next.delete(batchId)
  else next.add(batchId)
  expandedParentIds.value = next
}

function cancelPromptPopoverClose() {
  if (!promptPopoverCloseTimer) return
  clearTimeout(promptPopoverCloseTimer)
  promptPopoverCloseTimer = null
}

function cancelPromptPopoverOpen() {
  if (!promptPopoverOpenTimer) return
  clearTimeout(promptPopoverOpenTimer)
  promptPopoverOpenTimer = null
}

function closePromptPopover() {
  cancelPromptPopoverOpen()
  cancelPromptPopoverClose()
  promptPopover.visible = false
  promptPopover.text = ''
  promptPopover.style = {}
  activePromptPopoverTarget = null
}

function schedulePromptPopoverClose() {
  cancelPromptPopoverOpen()
  cancelPromptPopoverClose()
  promptPopoverCloseTimer = setTimeout(() => {
    closePromptPopover()
  }, 180)
}

function schedulePromptPopoverOpen(event: MouseEvent | PointerEvent, text: string) {
  const target = event.currentTarget as HTMLElement | null
  if (!target) return
  const value = String(text || '').trim()
  if (!value || value === '-') return
  activePromptPopoverTarget = target
  cancelPromptPopoverOpen()
  cancelPromptPopoverClose()
  promptPopoverOpenTimer = setTimeout(() => {
    if (activePromptPopoverTarget !== target || !document.body.contains(target)) return
    openPromptPopover(target, value)
  }, 520)
}

function showPromptPopover(event: MouseEvent | FocusEvent, text: string) {
  const value = String(text || '').trim()
  if (!value || value === '-') return
  const target = event.currentTarget as HTMLElement | null
  cancelPromptPopoverClose()
  cancelPromptPopoverOpen()
  if (!target) return
  activePromptPopoverTarget = target
  openPromptPopover(target, value)
}

function openPromptPopover(target: HTMLElement, value: string) {
  const rect = target.getBoundingClientRect()
  if (!rect) return
  const viewportWidth = window.innerWidth || 1280
  const viewportHeight = window.innerHeight || 720
  const width = Math.min(440, Math.max(320, viewportWidth - 32))
  const left = Math.max(16, Math.min(rect.left, viewportWidth - width - 16))
  const estimatedHeight = 178
  const preferredTop = rect.bottom + 8
  const top = preferredTop + estimatedHeight > viewportHeight
    ? Math.max(16, rect.top - estimatedHeight - 8)
    : preferredTop
  promptPopover.text = value
  promptPopover.style = {
    left: `${left}px`,
    top: `${top}px`,
    width: `${width}px`,
  }
  promptPopover.visible = true
}

function copyPromptPopover() {
  if (!promptPopover.text) return
  void copyToClipboard(promptPopover.text, t('batchImage.promptPopover.copied'))
}

async function loadBatchJobs() {
  const requestID = ++jobsRequestSeq
  const keys = credentialedKeys.value
  if (!keys.length) {
    batchJobs.value = []
    selectedJobIds.value = new Set()
    loadingJobs.value = false
    pagination.has_more = false
    stopPolling()
    return
  }
  loadingJobs.value = true
  try {
    const results = await Promise.allSettled(keys.map(async (key) => {
      const rows = new Map<string, BatchImageJobRow>()
      let offset = 0
      while (true) {
        const result = await listBatchImageJobs(key.key, { limit: 100, cursor: String(offset) })
        if (requestID !== jobsRequestSeq) return []
        const previousSize = rows.size
        for (const job of result.data || []) rows.set(job.id, toJobRow(job, key))
        if (!result.has_more || rows.size === previousSize) break
        offset += result.data.length
      }
      return [...rows.values()]
    }))
    if (requestID !== jobsRequestSeq) return
    const failures = results.filter(result => result.status === 'rejected')
    if (failures.length) {
      appStore.showError(batchImageErrorMessage((failures[0] as PromiseRejectedResult).reason, batchImageText('loadJobsFailed')))
    }
    batchJobs.value = applyChildCounts(results
      .flatMap((result, index) => result.status === 'fulfilled' ? result.value : batchJobs.value.filter(job => job.api_key_id === keys[index].id))
      .sort((first, second) => second.created_at - first.created_at))
    selectedJobIds.value = new Set([...selectedJobIds.value].filter(id => batchJobs.value.some(job => job.id === id)))
    startPolling()
  } catch (error: any) {
    appStore.showError(batchImageErrorMessage(error, batchImageText('loadJobsFailed')))
  } finally {
    if (requestID === jobsRequestSeq) loadingJobs.value = false
  }
}

function upsertJob(job: BatchImageJob, key = keyForSelectedBatch() || selectedApiKey.value) {
  const next = toJobRow(job, key)
  const index = batchJobs.value.findIndex(item => item.id === job.id)
  if (index >= 0) {
    const rows = [...batchJobs.value]
    rows[index] = { ...next, is_child: rows[index].is_child }
    batchJobs.value = applyChildCounts(rows)
    return
  }
  batchJobs.value = applyChildCounts([next, ...batchJobs.value])
}

function handlePageChange(page: number) {
  if (page < 1 || page > totalPages.value || page === pagination.page) return
  pagination.page = page
}

function handlePageSizeChange(value: string | number | boolean | null) {
  if (value === null || typeof value === 'boolean') return
  const nextSize = Math.min(Math.max(Number(value) || 20, 1), 100)
  pagination.page_size = nextSize
  pagination.page = 1
  setPersistedPageSize(nextSize)
}

function openCreateModal() {
  showCreateModal.value = true
  if (!apiKeys.value.length) {
    void loadApiKeys()
  }
}

function closeCreateModal() {
  if (submitting.value) return
  showCreateModal.value = false
  resetCreateDraft()
}

function resetCreateDraft() {
  form.taskName = ''
  form.responseMimeType = 'image/png'
  form.imageSize = '1K'
  promptRows.value = []
  promptDraft.value = ''
  customIdDraft.value = ''
  outputCountDraft.value = 1
  referenceImageDrafts.value = []
  credentialDraft.value = ''
}

function closeDetail() {
  detailRequestSeq += 1
  itemsRequestSeq += 1
  closePromptPopover()
  currentJob.value = null
  selectedBatchId.value = ''
  selectedBatchApiKeyId.value = 0
  items.value = []
  clearItemPreviews()
}

function keyForSelectedBatch(): ApiKey | null {
  if (selectedBatchApiKeyId.value) {
    const key = activeApiKeys.value.find(item => item.id === selectedBatchApiKeyId.value)
    if (key) return key
  }
  return selectedApiKey.value
}

function requireApiKey(): ApiKey | null {
  if (!selectedApiKey.value || isMaskedCredential(selectedApiKey.value.key)) {
    appStore.showError(batchImageText('selectApiKey'))
    return null
  }
  return selectedApiKey.value
}

function validateForm(): boolean {
  if (!requireApiKey()) return false
  if (!form.model) {
    appStore.showError(availableBatchImageModels.value.length === 0 ? batchImageText('noModelsForKey') : batchImageText('selectModel'))
    return false
  }
  if (parsedItems.value.length === 0) {
    appStore.showError(batchImageText('promptRequired'))
    return false
  }
  if (estimatedOutputCount.value > BATCH_IMAGE_MAX_OUTPUTS_PER_JOB) {
    appStore.showError(batchImageText('tooManyOutputImages'))
    return false
  }
  const refLimit = selectedModelReferenceLimit.value
  if (promptRows.value.some(row => row.reference_images.length > refLimit)) {
    appStore.showError(batchImageText('tooManyReferenceImages'))
    return false
  }
  return true
}

async function submitJob() {
  if (submitting.value || readingReferences.value || loadingModels.value) return
  if (estimatedOutputCount.value > BATCH_IMAGE_MAX_OUTPUTS_PER_JOB) {
    appStore.showError(batchImageText('tooManyOutputImages'))
    return
  }
  if (promptDraft.value.trim()) addPromptRow()
  if (!validateForm()) return
  const key = requireApiKey()
  if (!key) return
	  submitting.value = true
	  try {
	    const job = await submitBatchImageJob(
	      key.key,
	      {
	        model: form.model,
        task_name: form.taskName.trim() || defaultTaskName(),
        image_size: form.imageSize,
        response_mime_type: form.responseMimeType,
        items: parsedItems.value,
	      },
	      `omnihive-ui-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`,
	    )
	    currentJob.value = job
	    selectedBatchId.value = job.id
	    selectedBatchApiKeyId.value = key.id
	    items.value = []
	    upsertJob(job)
	    showCreateModal.value = false
	    resetCreateDraft()
	    appStore.showSuccess(batchImageText('submitted'))
	    void loadItems()
	    startPolling()
  } catch (error: any) {
    appStore.showError(batchImageErrorMessage(error, batchImageText('submitFailed')))
  } finally {
    submitting.value = false
  }
}

async function refreshSelected() {
  if (!selectedBatchId.value) return
  const batchId = selectedBatchId.value
  const requestID = ++detailRequestSeq
  const key = keyForSelectedBatch() || requireApiKey()
  if (!key) return
  refreshing.value = true
  try {
    const job = await getBatchImageJob(key.key, batchId)
    if (requestID !== detailRequestSeq || selectedBatchId.value !== batchId) return
    currentJob.value = job
    upsertJob(job, key)
    startPolling()
  } catch (error: any) {
    appStore.showError(batchImageErrorMessage(error, batchImageText('refreshFailed')))
  } finally {
    if (requestID === detailRequestSeq) refreshing.value = false
  }
}

async function refreshDetail() {
  await Promise.all([
    refreshSelected(),
    loadItems(),
  ])
}

function selectJob(batchId: string) {
  const row = batchJobs.value.find(job => job.id === batchId)
  if (row?.api_key_id && activeApiKeys.value.some(key => key.id === row.api_key_id)) {
    selectedBatchApiKeyId.value = row.api_key_id
  } else {
    selectedBatchApiKeyId.value = 0
  }
  selectedBatchId.value = batchId
  currentJob.value = null
  items.value = []
  clearItemPreviews()
  void refreshSelected()
  void loadItems()
}

function startPolling() {
  if (!batchJobs.value.some(job => canCancel(job))) {
    stopPolling()
    return
  }
  if (!pollTimer) pollTimer = setInterval(() => { void pollJobs() }, 8000)
}

let pollingJobs = false
async function pollJobs() {
  if (pollingJobs) return
  pollingJobs = true
  const activeJobs = batchJobs.value.filter(job => canCancel(job))
  try {
    await Promise.all(activeJobs.map(async row => {
      const key = apiKeyForJob(row)
      if (!key) return
      try {
        const job = await getBatchImageJob(key.key, row.id)
        if (!viewActive) return
        upsertJob(job, key)
        if (selectedBatchId.value === job.id) currentJob.value = job
      } catch (error) {
        if (viewActive) appStore.showError(batchImageErrorMessage(error, batchImageText('refreshFailed')))
      }
    }))
    if (!viewActive) return
    if (selectedBatchId.value) await loadItems()
    if (!batchJobs.value.some(job => canCancel(job))) stopPolling()
  } finally {
    pollingJobs = false
  }
}

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

function canCancel(job: Pick<BatchImageJob, 'status'>) {
  return !TERMINAL_STATUSES.has(job.status)
}

function canDownload(job: Pick<BatchImageJob, 'status' | 'success_count'> & { id?: string }) {
  return (job.status === 'completed' && job.success_count > 0)
    || (!!job.id && (childrenByParent.value.get(job.id) || []).some(child => child.status === 'completed' && child.success_count > 0))
}

function downloadableRows(batchId: string) {
  const row = batchJobs.value.find(job => job.id === batchId)
  return [...(row ? [row] : []), ...(childrenByParent.value.get(batchId) || [])]
    .filter(job => job.status === 'completed' && job.success_count > 0)
}

function canRetry(job: Pick<BatchImageJob, 'status' | 'fail_count'>) {
  const display = 'id' in job ? displayJob(job as BatchImageJob) : job
  return TERMINAL_STATUSES.has(display.status) && display.fail_count > 0
}

function isDownloadingJob(batchId: string) {
  return downloading.value && downloadingBatchId.value === batchId
}

function apiKeyForJob(job: BatchImageJobRow | Pick<BatchImageJob, 'id'>): ApiKey | null {
  const row = 'api_key_id' in job ? job : batchJobs.value.find(item => item.id === job.id)
  if (row?.api_key_id) return activeApiKeys.value.find(key => key.id === row.api_key_id) || null
  return keyForSelectedBatch()
}

function toggleJobSelection(batchId: string, checked: boolean) {
  const next = new Set(selectedJobIds.value)
  if (checked) next.add(batchId)
  else next.delete(batchId)
  selectedJobIds.value = next
}

function toggleAllVisible(checked: boolean) {
  const next = new Set(selectedJobIds.value)
  for (const job of visibleParentJobs.value) {
    if (checked) next.add(job.id)
    else next.delete(job.id)
  }
  selectedJobIds.value = next
}

function canDeleteRecord(job: Pick<BatchImageJob, 'status'> & { id?: string }) {
  return TERMINAL_STATUSES.has(job.status)
    && (!job.id || !(childrenByParent.value.get(job.id) || []).some(child => canCancel(child)))
}

async function cancelSelected() {
  if (!currentJob.value) return
  await cancelJob(currentJob.value)
}

async function cancelJob(target: BatchImageJobRow | BatchImageJob) {
  if (cancelling.value || !canCancel(target)) return
  const key = apiKeyForJob(target)
  if (!key) return
  if (!window.confirm(batchImageText('cancelConfirm'))) return
  cancelling.value = true
  try {
    const job = await cancelBatchImageJob(key.key, target.id)
    if (selectedBatchId.value === job.id) currentJob.value = job
    upsertJob(job, key)
    startPolling()
    appStore.showSuccess(batchImageText('cancelled'))
  } catch (error: any) {
    appStore.showError(batchImageErrorMessage(error, batchImageText('cancelFailed')))
  } finally {
    cancelling.value = false
  }
}

async function downloadSelected() {
  if (!currentJob.value) return
  await downloadJob(currentJob.value)
}

async function retrySelected() {
  if (!currentJob.value) return
  await retryFailedJob(currentJob.value)
}

async function retryFailedJob(job: BatchImageJobRow | BatchImageJob) {
  if (!canRetry(job) || retryingBatchId.value) return
  const key = apiKeyForJob(job) || keyForSelectedBatch() || requireApiKey()
  if (!key) return
  retryingBatchId.value = job.id
  try {
    const sourceItems = await ensureItemsForRetry(key.key, job.id)
    const failedItems = sourceItems
      .filter(item => item.status === 'failed')
      .map(item => ({ custom_id: retryCustomID(item.custom_id), prompt: String(item.prompt_preview || '').trim() }))
      .filter(item => item.prompt)
    if (failedItems.length === 0) {
      appStore.showError(batchImageText('retryMissingPrompts'))
      return
    }
    const retryJob = await submitBatchImageJob(
      key.key,
      {
        model: job.model,
        task_name: `${job.task_name || defaultTaskName()} ${t('batchImage.messages.retryTaskNameSuffix')}`,
        parent_batch_id: rootBatchIdForRetry(job),
        provider: job.provider,
        image_size: '1K',
        response_mime_type: form.responseMimeType,
        items: failedItems,
      },
      `omnihive-ui-retry-${job.id}-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`,
    )
    currentJob.value = retryJob
    selectedBatchId.value = retryJob.id
    selectedBatchApiKeyId.value = key.id
    items.value = []
    upsertJob(retryJob, key)
    if (retryJob.parent_batch_id) {
      expandedParentIds.value = new Set([...expandedParentIds.value, retryJob.parent_batch_id])
    }
    appStore.showSuccess(batchImageText('retrySubmitted'))
    void loadItems()
    startPolling()
  } catch (error: any) {
    appStore.showError(batchImageErrorMessage(error, batchImageText('retryFailed')))
  } finally {
    retryingBatchId.value = ''
  }
}

async function ensureItemsForRetry(apiKey: string, batchId: string) {
  const source = await listAllItems(apiKey, batchId)
  const children = childrenByParent.value.get(batchId) || []
  const childItems = await Promise.all(children.map(child => listAllItems(apiKey, child.id)))
  const recovered = new Set(childItems.flat().filter(isSuccessfulImageItem).map(item => retrySourceCustomID(item.custom_id)))
  return source.filter(item => !recovered.has(retrySourceCustomID(item.custom_id)))
}

async function listAllItems(apiKey: string, batchId: string): Promise<BatchImageItem[]> {
  let result = await listBatchImageItems(apiKey, batchId)
  const collected = [...(result.data || [])]
  while (result.has_more && result.data.length) {
    const response = await fetch(buildGatewayUrl(`/v1/images/batches/${encodeURIComponent(batchId)}/items?limit=500&cursor=${collected.length}`), {
      headers: { Authorization: `Bearer ${apiKey}` },
    })
    if (!response.ok) throw Object.assign(new Error(batchImageText('loadItemsFailed')), { status: response.status })
    result = await response.json()
    const known = new Set(collected.map(item => item.custom_id))
    const nextItems = result.data.filter(item => !known.has(item.custom_id))
    if (!nextItems.length) break
    collected.push(...nextItems)
  }
  return collected
}

function retryCustomID(customID: string) {
  const base = String(customID || 'item').replace(/[^\w.-]+/g, '_').replace(/^_+|_+$/g, '') || 'item'
  return `${base}_retry_${Date.now().toString(36)}`
}

function rootBatchIdForRetry(job: BatchImageJobRow | BatchImageJob) {
  return job.parent_batch_id || job.id
}

async function downloadJob(job: (BatchImageJobRow | Pick<BatchImageJob, 'id'>)) {
  if (downloading.value) return
  const key = apiKeyForJob(job) || requireApiKey()
  if (!key) return
  downloading.value = true
  downloadingBatchId.value = job.id
  try {
    const rows = downloadableRows(job.id)
    if (rows.some(row => row.id !== job.id)) {
      await downloadJobBundle(rows, `${job.id}.zip`)
    } else {
      const blob = await downloadBatchImageZip(key.key, job.id)
      saveBlob(blob, `${job.id}.zip`)
    }
    markJobDownloaded(job.id)
  } catch (error: any) {
    appStore.showError(batchImageErrorMessage(error, batchImageText('downloadFailed')))
  } finally {
    downloading.value = false
    downloadingBatchId.value = ''
  }
}

async function downloadSelectedJobs() {
  if (bulkDownloading.value || downloading.value || selectedDownloadableRows.value.length === 0) return
  bulkDownloading.value = true
  const rows = [...selectedDownloadableRows.value]
  const parents = [...selectedRows.value]
  try {
    await downloadJobBundle(rows, `batch-images-${Date.now()}.zip`)
    parents.filter(row => canDownload(row)).forEach(row => markJobDownloaded(row.id))
    appStore.showSuccess(batchImageText('batchDownloadStarted'))
  } catch (error: any) {
    appStore.showError(batchImageErrorMessage(error, batchImageText('downloadFailed')))
  } finally {
    bulkDownloading.value = false
    downloading.value = false
    downloadingBatchId.value = ''
  }
}

async function downloadJobBundle(rows: BatchImageJobRow[], filename: string) {
    const files: Array<{ name: string; data: Uint8Array }> = []
    let totalBytes = 0
    for (const row of rows) {
      const key = apiKeyForJob(row)
      if (!key) throw new Error(batchImageText('authRequired'))
      downloading.value = true
      downloadingBatchId.value = row.id
      const blob = await downloadBatchImageZip(key.key, row.id)
      totalBytes += blob.size
      if (totalBytes > 256 * 1024 * 1024) throw new Error(batchImageText('downloadTooLarge'))
      files.push({ name: `${encodeURIComponent(row.id)}.zip`, data: new Uint8Array(await blob.arrayBuffer()) })
    }
    files.push({
      name: 'tasks.jsonl',
      data: new TextEncoder().encode(rows.map(row => JSON.stringify({
        batch_id: row.id, task_name: row.task_name, model: row.model,
        success_count: row.success_count, fail_count: row.fail_count,
        archive: `${encodeURIComponent(row.id)}.zip`,
      })).join('\n') + '\n'),
    })
    saveBlob(packZip(files), filename)
    rows.forEach(row => markJobDownloaded(row.id))
}

function packZip(files: Array<{ name: string; data: Uint8Array }>): Blob {
  const entries: Uint8Array[] = []
  const directory: Uint8Array[] = []
  let offset = 0
  let directorySize = 0
  for (const file of files) {
    const name = new TextEncoder().encode(file.name)
    let checksum = 0xffffffff
    for (const byte of file.data) {
      checksum = (checksum >>> 8) ^ ZIP_CRC_TABLE[(checksum ^ byte) & 0xff]
    }
    checksum = (checksum ^ 0xffffffff) >>> 0
    const local = new Uint8Array(30)
    const header = new DataView(local.buffer)
    header.setUint32(0, 0x04034b50, true)
    header.setUint16(4, 20, true)
    header.setUint16(6, 0x0800, true)
    header.setUint16(12, 33, true)
    header.setUint32(14, checksum, true)
    header.setUint32(18, file.data.length, true)
    header.setUint32(22, file.data.length, true)
    header.setUint16(26, name.length, true)
    const central = new Uint8Array(46)
    const record = new DataView(central.buffer)
    record.setUint32(0, 0x02014b50, true)
    record.setUint16(4, 20, true)
    record.setUint16(6, 20, true)
    record.setUint16(8, 0x0800, true)
    record.setUint16(14, 33, true)
    record.setUint32(16, checksum, true)
    record.setUint32(20, file.data.length, true)
    record.setUint32(24, file.data.length, true)
    record.setUint16(28, name.length, true)
    record.setUint32(42, offset, true)
    entries.push(local, name, file.data)
    directory.push(central, name)
    offset += local.length + name.length + file.data.length
    directorySize += central.length + name.length
  }
  const end = new Uint8Array(22)
  const footer = new DataView(end.buffer)
  footer.setUint32(0, 0x06054b50, true)
  footer.setUint16(8, files.length, true)
  footer.setUint16(10, files.length, true)
  footer.setUint32(12, directorySize, true)
  footer.setUint32(16, offset, true)
  return new Blob([...entries, ...directory, end] as never, { type: 'application/zip' })
}

async function deleteJob(job: BatchImageJobRow) {
  if (!canDeleteRecord(job) || deletingBatchId.value) return
  const key = apiKeyForJob(job)
  if (!key) return
  if (!window.confirm(batchImageText('deleteConfirm'))) return
  deletingBatchId.value = job.id
  try {
    await deleteBatchImageJobRecord(key.key, job.id)
    removeJobFromList(job.id)
    appStore.showSuccess(batchImageText('deleted'))
  } catch (error: any) {
    appStore.showError(batchImageErrorMessage(error, batchImageText('deleteFailed')))
  } finally {
    deletingBatchId.value = ''
  }
}

async function deleteSelectedJobs() {
  const rows = selectedRows.value.filter(job => canDeleteRecord(job))
  if (bulkDeleting.value || rows.length === 0) return
  if (!window.confirm(batchImageText('deleteSelectedConfirm'))) return
  bulkDeleting.value = true
  try {
    for (const row of rows) {
      const key = apiKeyForJob(row)
      if (!key) continue
      deletingBatchId.value = row.id
      await deleteBatchImageJobRecord(key.key, row.id)
      removeJobFromList(row.id)
    }
    appStore.showSuccess(batchImageText('deleted'))
  } catch (error: any) {
    appStore.showError(batchImageErrorMessage(error, batchImageText('deleteFailed')))
  } finally {
    bulkDeleting.value = false
    deletingBatchId.value = ''
  }
}

function markJobDownloaded(batchId: string) {
  const downloadedAt = Math.floor(Date.now() / 1000)
  batchJobs.value = batchJobs.value.map(job => job.id === batchId ? { ...job, downloaded_at: job.downloaded_at || downloadedAt } : job)
  if (currentJob.value?.id === batchId && !currentJob.value.downloaded_at) {
    currentJob.value = { ...currentJob.value, downloaded_at: downloadedAt }
  }
}

function removeJobFromList(batchId: string) {
  const removed = new Set([batchId, ...(childrenByParent.value.get(batchId) || []).map(job => job.id)])
  batchJobs.value = applyChildCounts(batchJobs.value.filter(job => !removed.has(job.id)))
  selectedJobIds.value = new Set([...selectedJobIds.value].filter(id => !removed.has(id)))
  if (currentJob.value && removed.has(currentJob.value.id)) closeDetail()
}

function canLoadItemPreview(item: BatchImageItem) {
  return (item.status === 'succeeded' || item.status === 'success') && item.image_count > 0
}

function isSuccessfulImageItem(item: Pick<BatchImageItem, 'status' | 'image_count'>) {
  return (item.status === 'succeeded' || item.status === 'success') && item.image_count > 0
}

function detailRootBatchId() {
  return currentJob.value?.parent_batch_id || selectedBatchId.value || currentJob.value?.id || ''
}

function isChildDetailItem(item: Pick<BatchImageDetailItem, 'batch_id'>) {
  const rootBatchId = detailRootBatchId()
  return Boolean(rootBatchId && item.batch_id && item.batch_id !== rootBatchId)
}

function retrySourceCustomID(customID: string) {
  return String(customID || '').replace(/(?:_retry_[a-z0-9]+)+$/i, '')
}

function isRecoveredOriginalFailure(item: BatchImageDetailItem) {
  const rootBatchId = detailRootBatchId()
  return Boolean(
    rootBatchId
    && item.batch_id === rootBatchId
    && item.status === 'failed'
    && recoveredOriginalCustomIds.value.has(item.custom_id),
  )
}

function detailItemRowClass(item: BatchImageDetailItem) {
  if (isRecoveredOriginalFailure(item)) {
    return 'bg-[var(--surface-2)] text-[var(--muted-foreground)] hover:bg-[var(--surface-2)]   '
  }
  return 'hover:bg-[var(--surface-2)] '
}

function previewCacheSupported() {
  return typeof window !== 'undefined' && 'indexedDB' in window
}

function previewCacheKey(batchId: string, customID: string, imageIndex = 0) {
  return [batchId, customID, imageIndex].map(part => encodeURIComponent(String(part))).join(':')
}

function itemPreviewKey(item: Pick<BatchImageItem, 'batch_id' | 'custom_id'>) {
  return previewCacheKey(item.batch_id || selectedBatchId.value || currentJob.value?.id || '', item.custom_id, 0)
}

function idbRequest<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
}

function openPreviewCacheDB(): Promise<IDBDatabase | null> {
  if (!previewCacheSupported()) return Promise.resolve(null)
  if (previewCacheDBPromise) return previewCacheDBPromise

  try {
    window.indexedDB.deleteDatabase(LEGACY_PREVIEW_CACHE_DB_NAME)
  } catch {
    // A cache from the previous product name is disposable.
  }

  previewCacheDBPromise = new Promise((resolve) => {
    const request = window.indexedDB.open(PREVIEW_CACHE_DB_NAME, 1)
    request.onupgradeneeded = () => {
      const db = request.result
      if (!db.objectStoreNames.contains(PREVIEW_CACHE_STORE_NAME)) {
        const store = db.createObjectStore(PREVIEW_CACHE_STORE_NAME, { keyPath: 'key' })
        store.createIndex('lastAccessedAt', 'lastAccessedAt', { unique: false })
      }
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => resolve(null)
    request.onblocked = () => resolve(null)
  })
  return previewCacheDBPromise
}

async function getCachedPreviewBlob(cacheKey: string): Promise<Blob | null> {
  const db = await openPreviewCacheDB()
  if (!db) return null
  const record = await idbRequest<PreviewCacheRecord | undefined>(
    db.transaction(PREVIEW_CACHE_STORE_NAME, 'readonly').objectStore(PREVIEW_CACHE_STORE_NAME).get(cacheKey),
  ).catch(() => undefined)
  if (!record?.blob) return null

  const now = Date.now()
  if (now - record.createdAt > PREVIEW_CACHE_MAX_AGE_MS) {
    void deleteCachedPreview(cacheKey)
    return null
  }
  void touchCachedPreview(cacheKey, now)
  return record.blob
}

async function hydrateCachedItemPreviews(detailItems: BatchImageDetailItem[]) {
  const generation = previewGeneration
  const previewableItems = detailItems.filter(item => canLoadItemPreview(item))
  if (!previewableItems.length || !previewCacheSupported()) return

  await Promise.all(previewableItems.map(async (item) => {
    const batchId = item.batch_id || selectedBatchId.value || currentJob.value?.id || ''
    const previewKey = itemPreviewKey(item)
    if (!batchId || itemPreviewUrls[previewKey] || previewErrorIds.value.has(previewKey)) return
    const cached = await getCachedPreviewBlob(previewCacheKey(batchId, item.custom_id, 0)).catch(() => null)
    if (generation !== previewGeneration) return
    if (!cached || itemPreviewUrls[previewKey]) return
    itemPreviewUrls[previewKey] = URL.createObjectURL(cached)
  }))
}

async function putCachedPreviewBlob(cacheKey: string, blob: Blob) {
  const db = await openPreviewCacheDB()
  if (!db) return
  const now = Date.now()
  const record: PreviewCacheRecord = {
    key: cacheKey,
    blob,
    size: blob.size,
    createdAt: now,
    lastAccessedAt: now,
  }
  await idbRequest(db.transaction(PREVIEW_CACHE_STORE_NAME, 'readwrite').objectStore(PREVIEW_CACHE_STORE_NAME).put(record)).catch(() => null)
  void cleanupPreviewCache()
}

async function touchCachedPreview(cacheKey: string, lastAccessedAt: number) {
  const db = await openPreviewCacheDB()
  if (!db) return
  const record = await idbRequest<PreviewCacheRecord | undefined>(
    db.transaction(PREVIEW_CACHE_STORE_NAME, 'readonly').objectStore(PREVIEW_CACHE_STORE_NAME).get(cacheKey),
  ).catch(() => undefined)
  if (!record) return
  record.lastAccessedAt = lastAccessedAt
  await idbRequest(db.transaction(PREVIEW_CACHE_STORE_NAME, 'readwrite').objectStore(PREVIEW_CACHE_STORE_NAME).put(record)).catch(() => null)
}

async function deleteCachedPreview(cacheKey: string) {
  const db = await openPreviewCacheDB()
  if (!db) return
  await idbRequest(db.transaction(PREVIEW_CACHE_STORE_NAME, 'readwrite').objectStore(PREVIEW_CACHE_STORE_NAME).delete(cacheKey)).catch(() => null)
}

async function cleanupPreviewCache() {
  const db = await openPreviewCacheDB()
  if (!db) return
  const records = await idbRequest<PreviewCacheRecord[]>(
    db.transaction(PREVIEW_CACHE_STORE_NAME, 'readonly').objectStore(PREVIEW_CACHE_STORE_NAME).getAll(),
  ).catch(() => [])
  if (!records.length) return

  const now = Date.now()
  const sorted = [...records].sort((a, b) => a.lastAccessedAt - b.lastAccessedAt)
  const deleteKeys = new Set<string>()
  let totalBytes = 0
  let keptCount = 0

  for (const record of sorted) {
    if (now - record.createdAt > PREVIEW_CACHE_MAX_AGE_MS) {
      deleteKeys.add(record.key)
      continue
    }
    totalBytes += record.size || record.blob?.size || 0
    keptCount += 1
  }

  for (const record of sorted) {
    if (deleteKeys.has(record.key)) continue
    if (keptCount <= PREVIEW_CACHE_MAX_ENTRIES && totalBytes <= PREVIEW_CACHE_MAX_BYTES) break
    deleteKeys.add(record.key)
    totalBytes -= record.size || record.blob?.size || 0
    keptCount -= 1
  }

  if (!deleteKeys.size) return
  const store = db.transaction(PREVIEW_CACHE_STORE_NAME, 'readwrite').objectStore(PREVIEW_CACHE_STORE_NAME)
  for (const key of deleteKeys) {
    store.delete(key)
  }
}

async function createThumbnailBlob(blob: Blob): Promise<Blob> {
  const source = await loadPreviewImageSource(blob)
  const width = source.width
  const height = source.height
  const scale = Math.min(1, PREVIEW_THUMBNAIL_MAX_EDGE / Math.max(width, height))
  const targetWidth = Math.max(1, Math.round(width * scale))
  const targetHeight = Math.max(1, Math.round(height * scale))
  const canvas = document.createElement('canvas')
  canvas.width = targetWidth
  canvas.height = targetHeight
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error('canvas unavailable')
  ctx.drawImage(source.image, 0, 0, targetWidth, targetHeight)
  source.close()
  return await new Promise<Blob>((resolve, reject) => {
    canvas.toBlob((thumbnail) => {
      if (thumbnail) resolve(thumbnail)
      else reject(new Error('thumbnail unavailable'))
    }, 'image/webp', PREVIEW_THUMBNAIL_QUALITY)
  })
}

async function loadPreviewImageSource(blob: Blob): Promise<{ image: PreviewImageSource, width: number, height: number, close: () => void }> {
  if ('createImageBitmap' in window) {
    const bitmap = await window.createImageBitmap(blob)
    return {
      image: bitmap,
      width: bitmap.width,
      height: bitmap.height,
      close: () => bitmap.close(),
    }
  }

  const url = URL.createObjectURL(blob)
  try {
    const image = await new Promise<HTMLImageElement>((resolve, reject) => {
      const img = new Image()
      img.onload = () => resolve(img)
      img.onerror = () => reject(new Error('image unavailable'))
      img.src = url
    })
    return {
      image,
      width: image.naturalWidth || image.width,
      height: image.naturalHeight || image.height,
      close: () => URL.revokeObjectURL(url),
    }
  } catch (error) {
    URL.revokeObjectURL(url)
    throw error
  }
}

async function loadItems() {
  const requestID = ++itemsRequestSeq
  const batchId = selectedBatchId.value || currentJob.value?.id || ''
  if (!batchId) return
  const key = keyForSelectedBatch() || requireApiKey()
  if (!key) return
  loadingItems.value = true
  try {
    const jobs = detailJobsForBatch(batchId)
    const results = await Promise.all(jobs.map(async (job) => {
      const result = await listAllItems(key.key, job.id)
      return result.map(item => ({
        ...item,
        batch_id: job.id,
        source_task_name: detailSourceName(job, batchId),
      }))
    }))
    const detailItems = results.flat()
    if (requestID !== itemsRequestSeq || selectedBatchId.value !== batchId) return
    items.value = detailItems
    void hydrateCachedItemPreviews(detailItems)
  } catch (error: any) {
    appStore.showError(batchImageErrorMessage(error, batchImageText('loadItemsFailed')))
  } finally {
    if (requestID === itemsRequestSeq) loadingItems.value = false
  }
}

function detailJobsForBatch(batchId: string): BatchImageJobRow[] {
  const row = batchJobs.value.find(job => job.id === batchId)
  const base = row || (currentJob.value && currentJob.value.id === batchId ? toJobRow(currentJob.value, keyForSelectedBatch() || selectedApiKey.value) : null)
  if (!base) return []
  if (base.parent_batch_id) return [base]
  return [base, ...(childrenByParent.value.get(base.id) || [])]
}

function detailSourceName(job: Pick<BatchImageJobRow, 'id' | 'task_name' | 'parent_batch_id'>, rootBatchId: string) {
  const name = job.task_name || job.id
  if (job.id === rootBatchId) return t('batchImage.detail.mainTask', { name })
  return t('batchImage.detail.childTask', { name })
}

async function loadItemPreview(item: BatchImageItem) {
  const generation = previewGeneration
  const batchId = item.batch_id || selectedBatchId.value || currentJob.value?.id || ''
  const previewKey = itemPreviewKey(item)
  if (!batchId || !canLoadItemPreview(item) || (itemPreviewUrls[previewKey] && !previewErrorIds.value.has(previewKey))) return
  const key = keyForSelectedBatch() || requireApiKey()
  if (!key) return
  const cacheKey = previewCacheKey(batchId, item.custom_id, 0)
  previewLoadingIds.value = new Set([...previewLoadingIds.value, previewKey])
  try {
    previewErrorIds.value = new Set([...previewErrorIds.value].filter(id => id !== previewKey))
    if (itemPreviewUrls[previewKey]) {
      URL.revokeObjectURL(itemPreviewUrls[previewKey])
      delete itemPreviewUrls[previewKey]
    }
    const cached = await getCachedPreviewBlob(cacheKey)
    if (generation !== previewGeneration) return
    if (cached) {
      itemPreviewUrls[previewKey] = URL.createObjectURL(cached)
      return
    }
    const blob = await getBatchImageItemContent(key.key, batchId, item.custom_id, 0)
    const thumbnail = await createThumbnailBlob(blob).catch(() => blob)
    if (generation !== previewGeneration) return
    itemPreviewUrls[previewKey] = URL.createObjectURL(thumbnail)
    if (thumbnail !== blob || thumbnail.size <= 1024 * 1024) {
      void putCachedPreviewBlob(cacheKey, thumbnail)
    }
  } catch (error: any) {
    if (generation !== previewGeneration) return
    previewErrorIds.value = new Set([...previewErrorIds.value, previewKey])
    appStore.showError(batchImageErrorMessage(error, batchImageText('loadPreviewFailed')))
  } finally {
    const next = new Set(previewLoadingIds.value)
    next.delete(previewKey)
    previewLoadingIds.value = next
  }
}

function openImagePreview(item: BatchImageItem) {
  const previewKey = itemPreviewKey(item)
  if (!itemPreviewUrls[previewKey] || previewErrorIds.value.has(previewKey)) return
  previewImageItem.value = item
}

function closeImagePreview() {
  previewImageItem.value = null
}

function handlePreviewError(customID: string) {
  if (itemPreviewUrls[customID]) {
    URL.revokeObjectURL(itemPreviewUrls[customID])
    delete itemPreviewUrls[customID]
  }
  previewErrorIds.value = new Set([...previewErrorIds.value, customID])
}

function clearItemPreviews() {
  previewGeneration += 1
  closePromptPopover()
  for (const url of Object.values(itemPreviewUrls)) {
    if (url) URL.revokeObjectURL(url)
  }
  for (const key of Object.keys(itemPreviewUrls)) {
    delete itemPreviewUrls[key]
  }
  previewLoadingIds.value = new Set()
  previewErrorIds.value = new Set()
  previewImageItem.value = null
}

function copyInstruction() {
  void copyToClipboard(agentInstruction.value, batchImageText('copiedInstruction'))
}

function statusLabel(jobOrStatus: BatchImageStatus | Pick<BatchImageJob, 'status' | 'success_count' | 'fail_count'>) {
  const status = typeof jobOrStatus === 'string' ? jobOrStatus : jobOrStatus.status
  if (typeof jobOrStatus !== 'string' && status === 'completed' && jobOrStatus.fail_count > 0) {
    if (jobOrStatus.success_count > 0) return t('batchImage.status.partialSuccess')
    return copy('已失败', t('batchImage.status.allFailed'))
  }
  if (status === 'failed') return copy('已失败', t('batchImage.status.failed'))
  const statusKeys: Record<string, string> = {
    queued: 'queued',
    running: 'running',
    indexing: 'processingResults',
    processing_results: 'processingResults',
    settling: 'settling',
    completed: 'completed',
    failed: 'failed',
    cancelled: 'cancelled',
    output_deleted: 'outputDeleted',
  }
  const key = statusKeys[status]
  return key ? t(`batchImage.status.${key}`) : status
}

function statusBadgeClass(jobOrStatus: BatchImageStatus | Pick<BatchImageJob, 'status' | 'success_count' | 'fail_count'>) {
  const status = typeof jobOrStatus === 'string' ? jobOrStatus : jobOrStatus.status
  if (typeof jobOrStatus !== 'string' && status === 'completed' && jobOrStatus.fail_count > 0) {
    if (jobOrStatus.success_count > 0) return 'badge-warning'
    return 'badge-danger'
  }
  if (status === 'completed') return 'badge-success'
  if (status === 'failed' || status === 'cancelled') return 'badge-danger'
  if (status === 'output_deleted') return 'badge-gray'
  return 'badge-primary'
}

function itemStatusLabel(status: string) {
  const statusKeys: Record<string, string> = {
    pending: 'pending',
    succeeded: 'succeeded',
    success: 'succeeded',
    failed: 'failed',
    cancelled: 'cancelled',
  }
  const key = statusKeys[status]
  return key ? t(`batchImage.itemStatus.${key}`) : status
}

function itemDisplayStatusLabel(item: BatchImageDetailItem) {
  if (isRecoveredOriginalFailure(item)) return t('batchImage.itemStatus.recovered')
  return itemStatusLabel(item.status)
}

function itemStatusBadgeClass(status: string) {
  if (status === 'succeeded' || status === 'success') return 'badge-success'
  if (status === 'failed' || status === 'cancelled') return 'badge-danger'
  return 'badge-primary'
}

function itemDisplayStatusBadgeClass(item: BatchImageDetailItem) {
  if (isRecoveredOriginalFailure(item)) return 'badge-gray'
  return itemStatusBadgeClass(item.status)
}

function itemResultLabel(item: BatchImageDetailItem) {
  if (isRecoveredOriginalFailure(item)) return t('batchImage.itemResult.recoveredByRetry')
  if (item.error) return friendlyItemError(item.error)
  if (item.status === 'succeeded' || item.status === 'success') {
    return itemPreviewUrls[itemPreviewKey(item)] ? t('batchImage.itemResult.readyPreview') : t('batchImage.itemResult.readyDownload')
  }
  if (item.status === 'failed') return t('batchImage.itemResult.noUsableImage')
  if (item.status === 'cancelled') return t('batchImage.itemResult.cancelled')
  return t('batchImage.itemResult.waiting')
}

function itemResultClass(item: BatchImageDetailItem) {
  if (isRecoveredOriginalFailure(item)) return 'bg-[var(--surface-2)] text-[var(--muted-foreground)] ring-[var(--border)]   '
  if (item.error || item.status === 'failed' || item.status === 'cancelled') return 'bg-red-50 text-red-700 ring-red-100 dark:bg-red-950/30 dark:text-red-300 dark:ring-red-900/50'
  if (item.status === 'succeeded' || item.status === 'success') return 'bg-emerald-50 text-emerald-700 ring-emerald-100 dark:bg-emerald-950/30 dark:text-emerald-300 dark:ring-emerald-900/50'
  return 'bg-[var(--surface-2)] text-[var(--muted-foreground)] ring-[var(--border)]   '
}

function friendlyItemError(error: BatchImageItem['error']) {
  if (!error) return '-'
  if (error.code === 'EMPTY_IMAGE_OUTPUT') return t('batchImage.itemResult.emptyImageOutput')
  if (error.code === 'PROVIDER_ITEM_FAILED') return t('batchImage.itemResult.providerItemFailed')
  return error.message || error.code || '-'
}

function formatMoney(value: number | null | undefined) {
  if (value === null || value === undefined || Number.isNaN(Number(value))) return '$0.00'
  return `$${Number(value).toFixed(2)}`
}

function terminalZeroCost(job: Pick<BatchImageJob, 'status' | 'actual_cost'>) {
  return job.actual_cost === null && (job.status === 'failed' || job.status === 'cancelled')
}

function costLabel(job: Pick<BatchImageJob, 'status' | 'hold_amount' | 'actual_cost'>) {
  if (job.actual_cost !== null) return formatMoney(job.actual_cost)
  if (terminalZeroCost(job)) return formatMoney(0)
  return t('batchImage.detail.holdCost', { amount: formatMoney(job.hold_amount) })
}

type BatchImageTextKey =
  | 'loadKeysFailed'
  | 'loadModelsFailed'
  | 'loadJobsFailed'
  | 'selectApiKey'
  | 'noModelsForKey'
  | 'selectModel'
  | 'promptRequired'
  | 'submitted'
  | 'submitFailed'
  | 'refreshFailed'
  | 'cancelConfirm'
  | 'cancelled'
  | 'cancelFailed'
  | 'batchDownloadStarted'
	  | 'downloadFailed'
	  | 'retrySubmitted'
	  | 'retryFailed'
	  | 'retryMissingPrompts'
  | 'deleteConfirm'
  | 'deleteSelectedConfirm'
  | 'deleted'
  | 'deleteFailed'
	  | 'loadItemsFailed'
	  | 'loadPreviewFailed'
  | 'copiedInstruction'
  | 'loadingModels'
  | 'noModels'
  | 'noModelsHint'
  | 'noCompatibleAccount'
  | 'unsupportedProvider'
  | 'providerSubmitFailed'
  | 'vertexGcsBucketMissing'
  | 'queueFailed'
  | 'billingHoldFailed'
  | 'groupDisabled'
  | 'pricingMissing'
  | 'insufficientBalance'
  | 'invalidModel'
  | 'invalidItems'
  | 'duplicateCustomId'
  | 'promptTooLong'
  | 'invalidReferenceImage'
  | 'tooManyReferenceImages'
  | 'referenceImagesTooLarge'
  | 'tooManyOutputImages'
  | 'idempotencyConflict'
  | 'notReady'
  | 'outputDeleted'
  | 'resultMissing'
  | 'itemFailed'
  | 'itemImageIndexOutOfRange'
  | 'downloadLimited'
  | 'downloadTooLarge'
  | 'deleteNotReady'
  | 'disabled'
  | 'authRequired'
  | 'adminReference'
  | 'errorReference'

function isZhLocale() {
  return String(locale.value || '').toLowerCase().startsWith('zh')
}

function batchImageText(key: BatchImageTextKey) {
  return t(`batchImage.messages.${key}`)
}

function batchImageErrorReference(error: any) {
  const parts: string[] = []
  const code = String(error?.code || '').trim()
  const requestId = String(error?.requestId || '').trim()
  const status = String(error?.status || '').trim()
  if (code) parts.push(t('batchImage.messages.errorCodeRef', { code }))
  if (requestId) parts.push(t('batchImage.messages.requestIdRef', { id: requestId }))
  if (!code && status) parts.push(t('batchImage.messages.httpStatusRef', { status }))
  return parts.length ? `（${parts.join(isZhLocale() ? '，' : ', ')}）` : ''
}

function batchImageAdminError(base: string, error: any) {
  const reference = batchImageErrorReference(error)
  return `${base}${reference ? ` ${reference}` : ''} ${batchImageText('adminReference')}`
}

function batchImagePlainError(base: string) {
  return base
}

function batchImageErrorMessage(error: any, fallback: string) {
  const code = String(error?.code || '').trim()
  const message = String(error?.message || '').trim()
  if (code === 'API_KEY_REQUIRED' || code === '401') {
    return batchImagePlainError(batchImageText('authRequired'))
  }
  if (code === 'BATCH_IMAGE_NO_ACCOUNT_AVAILABLE' || /no compatible batch image account/i.test(message)) {
    return batchImageAdminError(batchImageText('noCompatibleAccount'), error)
  }
  if (code === 'BATCH_IMAGE_UNSUPPORTED_PROVIDER' || /unsupported batch image provider/i.test(message)) {
    return batchImageAdminError(batchImageText('unsupportedProvider'), error)
  }
  if (code === 'BATCH_IMAGE_VERTEX_GCS_BUCKET_MISSING' || code === 'VERTEX_MANAGED_GCS_BUCKET_MISSING') {
    return batchImageAdminError(batchImageText('vertexGcsBucketMissing'), error)
  }
  if (
    code === 'BATCH_IMAGE_PROVIDER_SUBMIT_FAILED' ||
    code === 'BATCH_IMAGE_PROVIDER_MISSING_API_KEY' ||
    code === 'BATCH_IMAGE_PROVIDER_MISSING_SERVICE_ACCOUNT' ||
    code === 'BATCH_IMAGE_PROVIDER_UNSUPPORTED_ACCOUNT'
  ) {
    return batchImageAdminError(batchImageText('providerSubmitFailed'), error)
  }
  if (code === 'BATCH_IMAGE_QUEUE_FAILED' || code === 'BATCH_IMAGE_QUEUE_NOT_CONFIGURED') {
    return batchImageAdminError(batchImageText('queueFailed'), error)
  }
  if (code === 'BATCH_IMAGE_BILLING_HOLD_FAILED') {
    return batchImageAdminError(batchImageText('billingHoldFailed'), error)
  }
  if (code === 'BATCH_IMAGE_GROUP_DISABLED') {
    return batchImagePlainError(batchImageText('groupDisabled'))
  }
  if (code === 'BATCH_IMAGE_SETTLEMENT_PRICING_MISSING') {
    return batchImageAdminError(batchImageText('pricingMissing'), error)
  }
  if (code === 'BATCH_IMAGE_INSUFFICIENT_BALANCE') {
    return batchImagePlainError(batchImageText('insufficientBalance'))
  }
  if (code === 'BATCH_IMAGE_INVALID_MODEL') {
    return batchImageText('invalidModel')
  }
  if (code === 'BATCH_IMAGE_INVALID_ITEMS') {
    return batchImageText('invalidItems')
  }
  if (code === 'BATCH_IMAGE_DUPLICATE_CUSTOM_ID') {
    return batchImageText('duplicateCustomId')
  }
  if (code === 'BATCH_IMAGE_PROMPT_TOO_LONG') {
    return batchImageText('promptTooLong')
  }
  if (code === 'BATCH_IMAGE_INVALID_REFERENCE_IMAGE') {
    return batchImageText('invalidReferenceImage')
  }
  if (code === 'BATCH_IMAGE_TOO_MANY_REFERENCE_IMAGES') {
    return batchImageText('tooManyReferenceImages')
  }
  if (code === 'BATCH_IMAGE_REFERENCE_IMAGES_TOO_LARGE') {
    return batchImageText('referenceImagesTooLarge')
  }
  if (code === 'BATCH_IMAGE_TOO_MANY_OUTPUT_IMAGES') {
    return batchImageText('tooManyOutputImages')
  }
  if (code === 'BATCH_IMAGE_IDEMPOTENCY_CONFLICT') {
    return batchImagePlainError(batchImageText('idempotencyConflict'))
  }
  if (code === 'BATCH_IMAGE_NOT_READY') {
    return batchImageText('notReady')
  }
  if (code === 'BATCH_IMAGE_OUTPUT_DELETED') {
    return batchImageText('outputDeleted')
  }
  if (code === 'BATCH_IMAGE_RESULT_MISSING') {
    return batchImageAdminError(batchImageText('resultMissing'), error)
  }
  if (code === 'BATCH_IMAGE_ITEM_FAILED') {
    return batchImagePlainError(batchImageText('itemFailed'))
  }
  if (code === 'BATCH_IMAGE_ITEM_IMAGE_INDEX_OUT_OF_RANGE') {
    return batchImagePlainError(batchImageText('itemImageIndexOutOfRange'))
  }
  if (code === 'BATCH_IMAGE_DOWNLOAD_LIMITED') {
    return batchImageText('downloadLimited')
  }
  if (code === 'BATCH_IMAGE_DOWNLOAD_TOO_LARGE') {
    return batchImageText('downloadTooLarge')
  }
  if (code === 'BATCH_IMAGE_RECORD_DELETE_NOT_READY') {
    return batchImagePlainError(batchImageText('deleteNotReady'))
  }
  if (code === 'BATCH_IMAGE_DISABLED') {
    return batchImageAdminError(batchImageText('disabled'), error)
  }
  if (code === 'INTERNAL_ERROR' || code === '500') {
    return batchImageAdminError(fallback, error)
  }
  if (isZhLocale()) {
    const detail = message ? `${batchImageText('errorReference')}：${message}` : batchImageText('adminReference')
    return `${fallback}。${detail} ${batchImageErrorReference(error)}`
  }
  return message || fallback
}

function formatDate(timestamp: number) {
  if (!timestamp) return ''
  return new Date(timestamp * 1000).toLocaleString()
}

function defaultTaskName(timestamp?: number) {
  const date = timestamp ? new Date(timestamp * 1000) : new Date()
  return date.toLocaleString()
}

onMounted(() => {
  void appStore.fetchPublicSettings()
  void refreshPage()
  void cleanupPreviewCache()
  previewCacheCleanupTimer = setInterval(() => {
    void cleanupPreviewCache()
  }, 60 * 60 * 1000)
  window.addEventListener('resize', closePromptPopover)
  window.addEventListener('scroll', closePromptPopover, true)
})

watch(
  () => form.apiKeyId,
  () => {
    credentialDraft.value = ''
    void loadAvailableModels()
  },
)

watch(
  () => form.model,
  () => {
    const limit = selectedModelReferenceLimit.value
    if (limit <= 0) {
      referenceImageDrafts.value = []
      return
    }
    if (referenceImageDrafts.value.length > limit) {
      referenceImageDrafts.value = referenceImageDrafts.value.slice(0, limit)
    }
  },
)

watch([totalPages, () => pagination.page], () => {
  pagination.page = Math.min(pagination.page, totalPages.value)
  pagination.has_more = pagination.page < totalPages.value
})

let viewActive = true
onBeforeUnmount(() => {
  viewActive = false
  jobsRequestSeq += 1
  modelRequestSeq += 1
  detailRequestSeq += 1
  itemsRequestSeq += 1
  for (const keyId of Object.keys(sessionCredentials)) delete sessionCredentials[Number(keyId)]
  credentialDraft.value = ''
  stopPolling()
  if (previewCacheCleanupTimer) {
    clearInterval(previewCacheCleanupTimer)
    previewCacheCleanupTimer = null
  }
  clearItemPreviews()
  window.removeEventListener('resize', closePromptPopover)
  window.removeEventListener('scroll', closePromptPopover, true)
})
</script>

<style scoped>
.batch-filter-grid {
  display: grid;
  gap: 0.5rem;
  grid-template-columns: minmax(200px, 1.6fr) repeat(3, minmax(150px, 1fr));
}
.batch-page :deep(td) {
  padding-block: 0.625rem;
}
.batch-page :deep(.badge) {
  border: 1px solid var(--border);
  border-radius: 999px;
  font-weight: 500;
}
.batch-row-action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: 28px;
  padding: 0.25rem 0.5rem;
  font-size: 12px;
  line-height: 1.25rem;
  white-space: nowrap;
}

.batch-row-action:focus-visible {
  outline: 2px solid var(--ring);
  outline-offset: 2px;
}

.batch-page .btn-primary {
  background: var(--foreground);
  border-color: var(--foreground);
  color: var(--background);
}

.batch-page :deep(th) {
  text-transform: none;
}

.batch-create-form .input-label {
  margin-bottom: 0.25rem;
  font-size: 12px;
}

.batch-create-form .input {
  min-height: 36px;
}

@media (max-width: 900px) {
  .batch-filter-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 480px) {
  .batch-filter-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}

.batch-prompt-trigger:focus-visible {
  outline: 2px solid var(--ring);
  outline-offset: 2px;
}

.batch-prompt-popover {
  user-select: text;
}

.batch-prompt-popover p {
  scrollbar-width: thin;
}

.batch-output-count-select {
  height: 36px;
  min-height: 36px;
  padding-top: 0;
  padding-bottom: 0;
  padding-left: 14px;
  padding-right: 34px;
  line-height: 36px;
}
</style>
