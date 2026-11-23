<template>
  <AppLayout>
    <div class="oh-keys-workbench min-w-0 space-y-4">
      <section
        data-testid="keys-scope-attribution-banner"
        class="card flex flex-col gap-2.5 p-3 sm:px-4"
      >
        <div class="flex flex-wrap items-center justify-between gap-2.5">
          <div class="flex min-w-0 flex-wrap items-center gap-2">
            <span class="font-mono text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">
              {{ isZh ? '归属作用域' : 'ACTIVE SCOPE' }}
            </span>
            <span
              v-if="activeConsoleScope.workspaceId"
              class="rounded border border-[var(--oh-accent-border,rgba(245,158,11,0.4))] bg-[var(--oh-accent-subtle,rgba(245,158,11,0.12))] px-2 py-0.5 font-mono text-xs font-semibold tabular-nums text-[var(--foreground)]"
            >
              {{ consoleScopeStore.activeWorkspace?.name || `#${activeConsoleScope.workspaceId}` }}
              <template v-if="activeConsoleScope.projectId">
                · {{ consoleScopeStore.activeProject?.name || `#${activeConsoleScope.projectId}` }}
              </template>
              <template v-else>
                · {{ t('scope.workspace.overviewShort') }}
              </template>
            </span>
            <span v-else class="pill pill-ghost font-mono text-xs">
              {{ t('scope.personal.badge') }}
            </span>
          </div>

          <div class="flex flex-wrap items-center gap-2 text-xs">
            <a
              href="/byok"
              data-testid="keys-link-byok"
              class="btn btn-secondary btn-sm px-2.5 py-1 text-xs"
              @click.prevent="navigateConsole('/byok')"
            >
              {{ isZh ? '自带密钥 (BYOK)' : 'Bring Your Own Key (BYOK)' }}
            </a>
            <a
              :href="scopedBillingHref"
              data-testid="keys-link-billing"
              class="btn btn-secondary btn-sm px-2.5 py-1 text-xs"
              @click.prevent="navigateConsole(scopedBillingHref)"
            >
              {{ isZh ? '费用与账单' : 'Billing & Costs' }}
            </a>
            <a
              :href="scopedRequestsHref"
              data-testid="keys-link-requests"
              class="btn btn-ghost btn-sm px-2 py-1 text-xs"
              @click.prevent="navigateConsole(scopedRequestsHref)"
            >
              {{ isZh ? '请求记录' : 'Request Logs' }}
            </a>
            <a
              href="/workspaces"
              class="btn btn-ghost btn-sm px-2 py-1 text-xs"
              @click.prevent="navigateConsole('/workspaces')"
            >
              {{ isZh ? '切换作用域' : 'Switch Scope' }}
            </a>
          </div>
        </div>

        <p class="text-xs leading-relaxed text-[var(--muted-foreground)]">
          {{
            activeConsoleScope.workspaceId && activeConsoleScope.projectId
              ? isZh
                ? `当前密钥请求与计量回执将自动归集至「工作空间 #${activeConsoleScope.workspaceId} / 项目 #${activeConsoleScope.projectId}」，可在「费用与账单」查看项目预算消耗，在「请求记录」追踪每次调用；如需托管上游厂商凭证请前往「自带密钥 (BYOK)」。`
                : `Calls made with scoped credentials attribute usage and receipts to Workspace #${activeConsoleScope.workspaceId} / Project #${activeConsoleScope.projectId} across Billing & Costs and Request Logs. Enroll upstream provider keys in BYOK.`
              : isZh
                ? '每个下游 API 密钥均绑定至对应工作空间与项目以归集「请求记录」与「费用与账单」明细；如需托管自有上游厂商密钥请前往「自带密钥 (BYOK)」。'
                : 'Downstream API keys attribute usage to your active Workspace & Project in Request Logs and Billing & Costs. Manage upstream provider credentials in BYOK.'
          }}
        </p>
      </section>

      <section
        v-if="lastIssuedProjectKeyScope"
        data-testid="issued-project-key-scope-banner"
        class="card space-y-2 border-[var(--oh-accent-border,rgba(245,158,11,0.35))] bg-[var(--oh-accent-subtle,rgba(245,158,11,0.06))] p-3 sm:px-4"
      >
        <div class="flex items-center justify-between gap-2">
          <span class="text-xs font-semibold text-[var(--foreground)]">
            {{ t('keys.issuedProjectKeyScopeTitle', { name: lastIssuedProjectKeyScope.name }) }}
          </span>
          <button
            type="button"
            class="text-xs text-[var(--muted-foreground)] underline"
            @click="lastIssuedProjectKeyScope = null"
          >
            {{ t('keys.dismissScopeBanner') }}
          </button>
        </div>
        <div class="flex flex-wrap items-center gap-1.5 text-[11px]">
          <span class="font-medium text-[var(--muted-foreground)]">{{ t('keys.projectScopeGrantedActions') }}:</span>
          <code
            v-for="act in lastIssuedProjectKeyScope.actions"
            :key="act"
            class="rounded border border-[var(--border)] bg-[var(--card)] px-1.5 py-0.5 font-mono text-[11px] text-[var(--foreground)]"
          >
            {{ act }}
          </code>
        </div>
        <p
          class="text-[11px]"
          :class="
            lastIssuedProjectKeyScope.actions.includes('models.read')
              ? 'text-emerald-700 dark:text-emerald-300'
              : 'text-[var(--muted-foreground)]'
          "
        >
          {{
            lastIssuedProjectKeyScope.actions.includes('models.read')
              ? t('keys.projectScopeModelsReadIncluded')
              : t('keys.projectScopeModelsReadExcluded')
          }}
        </p>
      </section>

      <section class="oh-gateway-banner card p-3 sm:px-4" aria-labelledby="pay-as-you-go-title">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="flex flex-wrap items-center gap-2.5">
            <span class="pulse-dot pulse-ok" aria-hidden="true" />
            <h2 id="pay-as-you-go-title" class="text-sm font-semibold tracking-tight text-[var(--foreground)]">
              {{ t('keys.payAsYouGo.title') }}
            </h2>
            <span class="oh-amber-pill">
              {{ t('keys.payAsYouGo.mode') }}
            </span>
          </div>
          <span class="pill pill-ghost font-mono text-xs tabular-nums">
            {{
              isZh
                ? `${gatewayEndpoints.length} 类协议 · ${generationEndpoints.length} 个生成入口`
                : `${gatewayEndpoints.length} PROTOCOLS · ${generationEndpoints.length} GEN`
            }}
          </span>
          <div v-if="selectedIds.length && !isProjectMode" class="flex flex-wrap items-center gap-3 text-sm">
            <template v-if="canWriteKeys">
            <span class="text-[var(--muted-foreground)]">
              {{ t('keys.bulkEdit.selectedCount', { count: selectedIds.length }) }}
            </span>
            <button
              class="btn btn-primary btn-sm"
              :disabled="loading"
              data-test="bulk-edit-keys"
              @click="showBulkEditModal = true"
            >
              {{ t('keys.bulkEdit.title') }}
            </button>
            <button class="btn btn-secondary btn-sm" @click="selectedIds = []">
              {{ t('keys.bulkEdit.clearSelection') }}
            </button>
            </template>
          </div>
        </div>
        <p class="sr-only">{{ t('keys.payAsYouGo.description') }}</p>
        <dl class="mt-2 grid grid-cols-1 gap-x-4 lg:grid-cols-3">
          <div
            v-for="endpoint in gatewayEndpoints"
            :key="endpoint.protocol"
            class="oh-endpoint-item flex min-w-0 items-center justify-between gap-2 py-2"
          >
            <dt class="w-20 shrink-0 font-mono text-[11px] font-semibold tracking-wide text-[var(--muted-foreground)]" :title="t(endpoint.label)">
              <span aria-hidden="true">{{ endpoint.protocol.toUpperCase() }}</span>
              <span class="sr-only">{{ t(endpoint.label) }}</span>
            </dt>
            <dd class="flex min-w-0 flex-1 items-center justify-end gap-2">
              <code class="oh-endpoint-code min-w-0 flex-1 truncate font-mono text-xs tabular-nums" :title="endpoint.url">{{ endpoint.url }}</code>
              <button
                type="button"
                class="btn btn-secondary btn-sm shrink-0 px-2 py-1"
                :aria-label="t('keys.payAsYouGo.copyAddress', { protocol: t(endpoint.label) })"
                @click="copyEndpoint(endpoint.url)"
              >
                <Icon :name="copiedEndpoint === endpoint.url ? 'check' : 'clipboard'" size="sm" />
              </button>
            </dd>
          </div>
        </dl>
        <p v-if="copiedEndpoint" class="sr-only" role="status">{{ t('keys.payAsYouGo.copied') }}</p>
        <!-- Generation endpoints are a different entry from the Messages protocol: an
             Anthropic-compatible /v1/messages key sends text and vision input, it does
             not generate images, videos or audio. -->
        <details class="oh-gen-endpoints mt-2 border-t pt-2">
          <summary class="flex cursor-pointer list-none flex-wrap items-baseline justify-between gap-2">
            <p class="text-xs font-semibold text-amber-900 dark:text-amber-200">{{ t('keys.payAsYouGo.generationTitle') }}</p>
            <span class="text-xs text-[var(--muted-foreground)]" aria-hidden="true">▾</span>
          </summary>
          <p class="mt-2 text-xs text-[var(--muted-foreground)]">{{ t('keys.payAsYouGo.addressHint') }}</p>
          <p class="mt-2 text-xs text-[var(--muted-foreground)]">{{ t('keys.payAsYouGo.generationHint') }}</p>
          <ul class="mt-2 grid grid-cols-1 gap-1.5 sm:grid-cols-2 lg:grid-cols-3">
            <li
              v-for="endpoint in generationEndpoints"
              :key="endpoint.path"
              class="oh-gen-endpoint-chip flex items-center justify-between gap-2 rounded-md px-2.5 py-1.5"
            >
              <span class="shrink-0 text-xs font-medium text-amber-900 dark:text-amber-200">{{ t(endpoint.label) }}</span>
              <div class="flex min-w-0 items-center gap-1.5">
                <code class="min-w-0 truncate font-mono text-[11px] tabular-nums text-[var(--foreground)]" :title="endpoint.url">{{ endpoint.url }}</code>
                <button
                  type="button"
                  class="oh-focus-ring shrink-0 rounded p-1 text-amber-700 transition-colors hover:bg-amber-500/15 hover:text-amber-950 dark:text-amber-300 dark:hover:text-amber-100"
                  :aria-label="t('keys.payAsYouGo.copyAddress', { protocol: t(endpoint.label) })"
                  @click="copyEndpoint(endpoint.url)"
                >
                  <Icon :name="copiedEndpoint === endpoint.url ? 'check' : 'clipboard'" size="sm" />
                </button>
              </div>
            </li>
          </ul>
        </details>
      </section>

      <ScopeGateEmptyState
        v-if="scopeGateState !== null"
        :state="scopeGateState"
        surface="keys"
        :title="isWorkspaceOverview ? t('scope.workspace.overview') : undefined"
        :workspace-id="activeConsoleScope.workspaceId"
        :workspace-name="consoleScopeStore.activeWorkspace?.name ?? null"
        :project-id="activeConsoleScope.projectId"
        :project-name="consoleScopeStore.activeProject?.name ?? null"
        required-grant="keys.read"
        :description="projectAccessEntry?.error?.message ?? undefined"
        :http-status="projectAccessEntry?.error?.status ?? null"
        :request-id="projectAccessEntry?.error?.requestId ?? null"
        :project-options="consoleScopeStore.availableProjectOptions"
        :show-retry="scopeGateState === 'unavailable'"
        @switch-personal="handleSwitchToPersonalScope"
        @select-project="handleSelectProjectScope"
        @request-access="navigateConsole('/workspaces?requestAccess=1')"
        @retry="handleRetryScopeOrAccess"
      />

      <TablePageLayout v-else>
        <template #filters>
          <div class="flex flex-col gap-2.5">
            <div class="oh-filter-bar card flex flex-wrap items-center justify-between gap-3 p-3">
              <div class="flex flex-1 flex-wrap items-center gap-2.5">
                <SearchInput
                  v-model="filterSearch"
                  :placeholder="t('keys.searchPlaceholder')"
                  class="w-full sm:w-64"
                  @search="onFilterChange"
                />
                <Select
                  :model-value="filterGroupId"
                  class="w-40"
                  :options="groupFilterOptions"
                  @update:model-value="onGroupFilterChange"
                />
                <Select
                  :model-value="filterStatus"
                  class="w-40"
                  :options="statusFilterOptions"
                  @update:model-value="onStatusFilterChange"
                />
              </div>
              <div class="flex flex-wrap items-center gap-2">
                <span class="pill pill-ghost font-mono text-xs tabular-nums">
                  {{ apiKeys.length }} / {{ pagination.total || apiKeys.length }}
                </span>
                <EndpointPopover
                  v-if="publicSettings?.api_base_url || (publicSettings?.custom_endpoints?.length ?? 0) > 0"
                  :api-base-url="publicSettings?.api_base_url || ''"
                  :custom-endpoints="publicSettings?.custom_endpoints || []"
                />
              </div>
            </div>

            <!-- Hive Aura 琥珀微光批量操作横条 (.bulk-bar-real) -->
            <div
              id="kbKeyBulkBar"
              data-testid="keys-bulk-bar"
              class="bulk-bar-real"
              :class="{ 'has-selection': selectedKeyCount > 0 }"
            >
              <label class="inline-flex cursor-pointer items-center gap-2 text-xs font-medium">
                <input
                  type="checkbox"
                  class="h-3.5 w-3.5 rounded border-amber-400/60 text-amber-600 focus:ring-amber-500"
                  :checked="allVisibleKeysSelected"
                  @change="toggleAllVisibleKeys(($event.target as HTMLInputElement).checked)"
                />
                <span>
                  <template v-if="isZh">
                    已选择 <b id="kbKeySel" class="font-mono tabular-nums">{{ selectedKeyCount }}</b> 个密钥（筛选后仍保留）
                  </template>
                  <template v-else>
                    Selected <b id="kbKeySel" class="font-mono tabular-nums">{{ selectedKeyCount }}</b> key(s) (preserved across filters)
                  </template>
                </span>
              </label>
              <span class="pill pill-ghost font-mono text-[11px] tabular-nums">
                {{
                  isZh
                    ? `启用 ${activeKeyCount} · 共 ${pagination.total || apiKeys.length}`
                    : `Active ${activeKeyCount} · Total ${pagination.total || apiKeys.length}`
                }}
              </span>
              <div class="flex-1" />
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="selectedKeyCount === 0 || bulkUpdating"
                @click="bulkToggleSelectedKeys('active')"
              >
                {{ isZh ? '批量启用' : 'Enable Selected' }}
              </button>
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="selectedKeyCount === 0 || bulkUpdating"
                @click="bulkToggleSelectedKeys('inactive')"
              >
                {{ isZh ? '批量停用' : 'Disable Selected' }}
              </button>
              <button
                type="button"
                class="btn btn-ghost btn-sm"
                :disabled="selectedKeyCount === 0"
                @click="clearSelectedKeys"
              >
                {{ isZh ? '清除选择' : 'Clear Selection' }}
              </button>
            </div>
          </div>
        </template>

        <template #actions>
          <div class="oh-keys-toolbar flex flex-wrap items-center justify-between gap-3">
            <div class="flex items-center gap-2.5">
              <span class="oh-brand-bar" aria-hidden="true" />
              <div>
                <h1 class="text-lg font-bold tracking-tight text-[var(--foreground)] sm:text-xl">
                  {{ t('keys.apiKey') }}
                </h1>
              </div>
            </div>
            <div class="flex flex-wrap items-center justify-end gap-2.5">
              <button
                @click="loadApiKeys"
                :disabled="loading"
                class="btn btn-secondary"
                :title="t('common.refresh')"
              >
                <Icon name="refresh" size="md" :class="loading ? 'motion-safe:animate-spin' : ''" />
              </button>
              <div class="relative" ref="columnDropdownRef">
                <button
                  @click="showColumnDropdown = !showColumnDropdown"
                  class="btn btn-secondary px-2 md:px-3"
                  :title="t('keys.columnSettings')"
                >
                  <svg class="h-4 w-4 md:mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M9 4.5v15m6-15v15m-10.875 0h15.75c.621 0 1.125-.504 1.125-1.125V5.625c0-.621-.504-1.125-1.125-1.125H4.125C3.504 4.5 3 5.004 3 5.625v12.75c0 .621.504 1.125 1.125 1.125z" />
                  </svg>
                  <span class="hidden md:inline">{{ t('keys.columnSettings') }}</span>
                </button>
                <div
                  v-if="showColumnDropdown"
                  class="absolute right-0 top-full z-50 mt-1 max-h-80 w-48 overflow-y-auto rounded-lg border border-[var(--border)] bg-[var(--card)] py-1 shadow-lg"
                >
                  <button
                    v-for="col in toggleableColumns"
                    :key="col.key"
                    @click="toggleColumn(col.key)"
                    class="flex w-full items-center justify-between px-4 py-2 text-left text-sm text-[var(--foreground)] hover:bg-[var(--surface-2)]"
                  >
                    <span>{{ col.label }}</span>
                    <Icon
                      v-if="isColumnVisible(col.key)"
                      name="check"
                      size="sm"
                      class="text-[var(--brand-text)]"
                      :stroke-width="2"
                    />
                  </button>
                </div>
              </div>
              <button
                v-if="canWriteKeys"
                @click="openCreateModal"
                class="btn btn-primary"
                data-tour="keys-create-btn"
                data-testid="keys-create-btn"
              >
                <Icon name="plus" size="md" class="mr-1.5" />
                {{ t('keys.createKey') }}
              </button>
            </div>
          </div>
        </template>

      <template #table>
        <ConsoleAsyncBanner
          v-if="keysAccessNotice"
          :notice="keysAccessNotice"
          :retry-label="t('common.refresh')"
          class="mb-3"
          @retry="handleRetryScopeOrAccess"
        />
        <DataTable
          v-if="canReadKeys"
          :columns="columns"
          :data="apiKeys"
          :loading="loading"
          :selectable="canWriteKeys && !isProjectMode"
          row-key="id"
          :selected-keys="selectedIds"
          :selection-label="(key: ApiKey) => t('keys.bulkEdit.selectKey', { name: key.name })"
          @update:selected-keys="handleSelectionChange"
          :server-side-sort="true"
          default-sort-key="created_at"
          default-sort-order="desc"
          @sort="handleSort"
        >
          <template #cell-id="{ value }">
            <span class="font-mono text-xs tabular-nums text-[var(--muted-foreground)]">#{{ value }}</span>
          </template>

          <template #cell-key="{ value }">
            <code class="code oh-key-secret font-mono text-xs tabular-nums">
              {{ displayCredential(value) }}
            </code>
          </template>

          <template #cell-name="{ value, row }">
            <div class="flex items-center gap-2">
              <input
                v-if="canMutateRow(row) && !isProjectMode"
                type="checkbox"
                class="h-3.5 w-3.5 rounded border-[var(--border)] text-amber-600 focus:ring-amber-500"
                :checked="selectedKeyIds.has(row.id)"
                :aria-label="isZh ? `选择密钥 ${value}` : `Select key ${value}`"
                @change="toggleKeySelection(row.id, ($event.target as HTMLInputElement).checked)"
              />
              <span class="font-semibold text-[var(--foreground)]">{{ value }}</span>
              <Icon
                v-if="row.ip_whitelist?.length > 0 || row.ip_blacklist?.length > 0"
                name="shield"
                size="sm"
                class="text-blue-500"
                :title="t('keys.ipRestrictionEnabled')"
              />
            </div>
          </template>

          <template #cell-group="{ row }">
            <div class="group/dropdown relative min-w-0 max-w-full">
              <button
                :ref="(el) => setGroupButtonRef(row.id, el)"
                :disabled="!canMutateRow(row)"
                @click="canMutateRow(row) && openGroupSelector(row)"
                class="oh-focus-ring -my-1 flex max-w-full min-w-0 flex-wrap items-center gap-1.5 rounded-lg px-2 py-1 transition-all duration-200 sm:-mx-2 sm:gap-2"
                :class="canMutateRow(row) ? 'cursor-pointer hover:bg-[var(--surface-2)]' : 'cursor-default'"
                :title="canMutateRow(row) ? t('keys.clickToChangeGroup') : undefined"
              >
                <GroupBadge
                  v-if="row.group"
                  :name="row.group.name"
                  :platform="row.group.platform"
                  :subscription-type="row.group.subscription_type"
                  :rate-multiplier="row.group.rate_multiplier"
                  :user-rate-multiplier="userGroupRates[row.group.id]"
                  :peak-rate-enabled="row.group.peak_rate_enabled"
                  :peak-start="row.group.peak_start"
                  :peak-end="row.group.peak_end"
                  :peak-rate-multiplier="row.group.peak_rate_multiplier"
                />
                <span v-else class="text-sm text-[var(--muted-foreground)]">{{
                  t('keys.noGroup')
                }}</span>
                <span v-if="canMutateRow(row)" class="text-xs text-[var(--muted-foreground)]">{{ t('keys.selectGroup') }}</span>
                <svg
                  v-if="canMutateRow(row)"
                  class="h-3.5 w-3.5 text-[var(--muted-foreground)] opacity-60 transition-opacity group-hover/dropdown:opacity-100"
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                  stroke-width="2"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M8.25 15L12 18.75 15.75 15m-7.5-6L12 5.25 15.75 9"
                  />
                </svg>
              </button>
            </div>
          </template>

          <template #cell-current_concurrency="{ value }">
            <span
              :class="[
                'inline-flex min-w-8 items-center justify-center rounded-md px-2 py-0.5 font-mono text-xs font-semibold tabular-nums stat-num',
                (value ?? 0) > 0
                  ? 'border border-[var(--oh-accent-border,rgba(245,158,11,0.35))] bg-[var(--oh-accent-subtle,rgba(245,158,11,0.12))] text-[var(--oh-accent-text,var(--brand-text,#d97706))]'
                  : 'bg-[var(--surface-2)] text-[var(--muted-foreground)]  '
              ]"
            >
              {{ value ?? 0 }}
            </span>
          </template>

          <template #cell-usage="{ row }">
            <div class="text-sm tabular-nums stat-num">
              <UsageStatsState :state="usageState" :history-gap="usageHistoryGap">
                <div class="flex items-center gap-1.5">
                  <span class="text-[var(--muted-foreground)]">{{ t('keys.today') }}:</span>
                  <span class="font-mono font-medium tabular-nums text-[var(--foreground)]">
                    ${{ (usageStats[row.id]?.today_actual_cost ?? 0).toFixed(4) }}
                  </span>
                </div>
                <div class="mt-0.5 flex items-center gap-1.5">
                  <span class="text-[var(--muted-foreground)]">{{ t('keys.total') }}:</span>
                  <span class="font-mono font-medium tabular-nums text-[var(--foreground)]">
                    ${{ (usageStats[row.id]?.total_actual_cost ?? 0).toFixed(4) }}
                  </span>
                </div>
              </UsageStatsState>
              <div class="mt-1.5 font-mono text-xs tabular-nums text-[var(--muted-foreground)]" data-testid="key-tpm-readonly">{{ t('admin.accounts.tpmLimit') }}: {{ row.tpm_limit || t('admin.accounts.tpmUnlimited') }}</div>
              <!-- Quota progress (if quota is set) -->
              <div v-if="row.quota > 0" class="mt-1.5 min-w-[118px] space-y-1">
                <div class="flex items-center justify-between gap-1.5 text-xs">
                  <span class="text-[var(--muted-foreground)]">{{ t('keys.quota') }}</span>
                  <span :class="[
                    'font-mono font-semibold tabular-nums stat-num',
                    row.quota_used >= row.quota ? 'text-red-500' :
                    row.quota_used >= row.quota * 0.8 ? 'text-amber-600 dark:text-amber-400' :
                    'text-[var(--foreground)] '
                  ]">
                    ${{ row.quota_used?.toFixed(2) || '0.00' }} / ${{ row.quota?.toFixed(2) }}
                  </span>
                </div>
                <div class="meter">
                  <span
                    :class="[
                      row.quota_used >= row.quota ? '!bg-red-500' :
                      row.quota_used >= row.quota * 0.8 ? '!bg-amber-500' :
                      ''
                    ]"
                    :style="{ width: Math.min((row.quota_used / row.quota) * 100, 100) + '%' }"
                  />
                </div>
              </div>
            </div>
          </template>

          <template #cell-rate_limit="{ row }">
            <div class="min-w-[156px] space-y-2 tabular-nums stat-num">
            <div v-if="row.rate_limit_5h > 0 || row.rate_limit_1d > 0 || row.rate_limit_7d > 0" class="space-y-1.5">
              <!-- 5h window -->
              <div v-if="row.rate_limit_5h > 0" class="space-y-1">
                <div class="flex items-center justify-between gap-2 text-xs">
                  <span class="pill pill-ghost font-mono text-[10px]">5h</span>
                  <span :class="[
                    'font-mono font-medium tabular-nums stat-num',
                    row.usage_5h >= row.rate_limit_5h ? 'text-red-500' :
                    row.usage_5h >= row.rate_limit_5h * 0.8 ? 'text-amber-600 dark:text-amber-400' :
                    'text-[var(--foreground)] '
                  ]">
                    ${{ row.usage_5h?.toFixed(2) || '0.00' }}/${{ row.rate_limit_5h?.toFixed(2) }}
                  </span>
                </div>
                <div class="meter">
                  <span
                    :class="[
                      row.usage_5h >= row.rate_limit_5h ? '!bg-red-500' :
                      row.usage_5h >= row.rate_limit_5h * 0.8 ? '!bg-amber-500' :
                      '!bg-[var(--oh-accent,#f59e0b)]'
                    ]"
                    :style="{ width: Math.min((row.usage_5h / row.rate_limit_5h) * 100, 100) + '%' }"
                  />
                </div>
                <div v-if="row.reset_5h_at && formatResetTime(row.reset_5h_at)" class="font-mono text-[10px] tabular-nums text-[var(--muted-foreground)]">
                  ⟳ {{ formatResetTime(row.reset_5h_at) }}
                </div>
              </div>
              <!-- 1d window -->
              <div v-if="row.rate_limit_1d > 0" class="space-y-1">
                <div class="flex items-center justify-between gap-2 text-xs">
                  <span class="pill pill-ghost font-mono text-[10px]">1d</span>
                  <span :class="[
                    'font-mono font-medium tabular-nums stat-num',
                    row.usage_1d >= row.rate_limit_1d ? 'text-red-500' :
                    row.usage_1d >= row.rate_limit_1d * 0.8 ? 'text-amber-600 dark:text-amber-400' :
                    'text-[var(--foreground)] '
                  ]">
                    ${{ row.usage_1d?.toFixed(2) || '0.00' }}/${{ row.rate_limit_1d?.toFixed(2) }}
                  </span>
                </div>
                <div class="meter">
                  <span
                    :class="[
                      row.usage_1d >= row.rate_limit_1d ? '!bg-red-500' :
                      row.usage_1d >= row.rate_limit_1d * 0.8 ? '!bg-amber-500' :
                      '!bg-[var(--oh-accent,#f59e0b)]'
                    ]"
                    :style="{ width: Math.min((row.usage_1d / row.rate_limit_1d) * 100, 100) + '%' }"
                  />
                </div>
                <div v-if="row.reset_1d_at && formatResetTime(row.reset_1d_at)" class="font-mono text-[10px] tabular-nums text-[var(--muted-foreground)]">
                  ⟳ {{ formatResetTime(row.reset_1d_at) }}
                </div>
              </div>
              <!-- 7d window -->
              <div v-if="row.rate_limit_7d > 0" class="space-y-1">
                <div class="flex items-center justify-between gap-2 text-xs">
                  <span class="pill pill-ghost font-mono text-[10px]">7d</span>
                  <span :class="[
                    'font-mono font-medium tabular-nums stat-num',
                    row.usage_7d >= row.rate_limit_7d ? 'text-red-500' :
                    row.usage_7d >= row.rate_limit_7d * 0.8 ? 'text-amber-600 dark:text-amber-400' :
                    'text-[var(--foreground)] '
                  ]">
                    ${{ row.usage_7d?.toFixed(2) || '0.00' }}/${{ row.rate_limit_7d?.toFixed(2) }}
                  </span>
                </div>
                <div class="meter">
                  <span
                    :class="[
                      row.usage_7d >= row.rate_limit_7d ? '!bg-red-500' :
                      row.usage_7d >= row.rate_limit_7d * 0.8 ? '!bg-amber-500' :
                      '!bg-[var(--oh-accent,#f59e0b)]'
                    ]"
                    :style="{ width: Math.min((row.usage_7d / row.rate_limit_7d) * 100, 100) + '%' }"
                  />
                </div>
                <div v-if="row.reset_7d_at && formatResetTime(row.reset_7d_at)" class="font-mono text-[10px] tabular-nums text-[var(--muted-foreground)]">
                  ⟳ {{ formatResetTime(row.reset_7d_at) }}
                </div>
              </div>
            </div>
            <span v-else class="pill pill-ghost font-mono text-xs text-[var(--muted-foreground)]">—</span>

            <!-- Effective RPM: which dimension owns each limit, the limit value and
                 the real reset of the current minute window. Deliberately separate
                 from the USD windows above (money) and from the period budget below. -->
            <div
              v-if="row.effective_rpm && row.effective_rpm.limits.length > 0"
              class="space-y-1 border-t border-[var(--border)] pt-1.5"
            >
              <div class="flex items-center justify-between gap-2 text-xs">
                <span class="text-[var(--muted-foreground)]" :title="t('keys.effectiveRpmPriorityHint')">
                  {{ t('keys.effectiveRpmTitle') }}
                </span>
              </div>
              <div
                v-for="limit in row.effective_rpm.limits"
                :key="`${limit.source}-${limit.scope_id}`"
                class="flex items-center justify-between gap-2 text-[11px] tabular-nums"
              >
                <span class="text-[var(--muted-foreground)]">{{ effectiveRpmSourceLabel(limit.source) }}</span>
                <span class="font-mono font-medium tabular-nums text-[var(--foreground)]">
                  {{ t('keys.effectiveRpmValue', { limit: limit.limit }) }}
                </span>
              </div>
              <div
                v-if="row.effective_rpm.window_resets_at && formatRpmReset(row.effective_rpm.window_resets_at)"
                class="font-mono text-[10px] tabular-nums text-[var(--muted-foreground)]"
              >
                ⟳ {{ t('keys.effectiveRpmResetsIn', { value: formatRpmReset(row.effective_rpm.window_resets_at) }) }}
              </div>
            </div>

            <!-- Period budget (USD): a spend limit over one explicit period, kept
                 visually separate from the RPM/TPM rate limits above. -->
            <div v-if="row.budget_period_mode !== 'off' && row.budget_limit > 0" class="space-y-1 border-t border-[var(--border)] pt-1.5">
              <div class="flex items-center justify-between gap-2 text-xs">
                <span class="text-[var(--muted-foreground)]" :title="t('keys.budgetPeriodHint')">
                  {{ t('keys.budgetColumn') }}
                </span>
                <span :class="[
                  'font-mono font-medium tabular-nums stat-num',
                  row.budget_remaining <= 0 ? 'text-red-500' :
                  row.budget_remaining <= row.budget_limit * 0.2 ? 'text-amber-600 dark:text-amber-400' :
                  'text-[var(--foreground)] '
                ]">
                  ${{ row.budget_used?.toFixed(2) || '0.00' }}/${{ row.budget_limit?.toFixed(2) || '0.00' }}
                </span>
              </div>
              <div class="meter">
                <span
                  :class="[
                    row.budget_remaining <= 0 ? '!bg-red-500' :
                    row.budget_remaining <= row.budget_limit * 0.2 ? '!bg-amber-500' :
                    '!bg-[var(--oh-accent,#f59e0b)]'
                  ]"
                  :style="{ width: Math.min((row.budget_used / row.budget_limit) * 100, 100) + '%' }"
                />
              </div>
              <div class="flex items-center justify-between gap-2 text-[10px] text-[var(--muted-foreground)]">
                <span class="pill pill-ghost truncate font-mono text-[10px]" :title="row.budget_period_display">
                  {{ row.budget_legacy_anchor || row.budget_period_display || row.budget_period_mode }}
                </span>
                <span v-if="row.budget_period_end && formatResetTime(row.budget_period_end)" class="font-mono tabular-nums">
                  ⟳ {{ formatResetTime(row.budget_period_end) }}
                </span>
              </div>
            </div>
            </div>
          </template>

          <template #cell-expires_at="{ value }">
            <span v-if="value" :class="[
              'font-mono text-xs tabular-nums',
              new Date(value) < new Date() ? 'text-red-500 dark:text-red-400' : 'text-[var(--muted-foreground)] '
            ]">
              {{ formatDateTime(value) }}
            </span>
            <span v-else class="text-sm text-[var(--muted-foreground)]">{{ t('keys.noExpiration') }}</span>
          </template>

          <template #cell-status="{ value }">
            <span :class="[
              'badge oh-status-badge',
              value === 'active' ? 'badge-success oh-status-ok' :
              value === 'quota_exhausted' ? 'badge-warning oh-status-warn' :
              value === 'expired' ? 'badge-danger oh-status-bad' :
              value === 'disabled' ? 'oh-status-disabled' :
              'badge-gray oh-status-neutral'
            ]">
              <span
                :class="[
                  'pulse-dot',
                  value === 'active' ? 'pulse-ok' :
                  value === 'quota_exhausted' ? 'pulse-warn' :
                  value === 'expired' || value === 'disabled' ? 'pulse-bad' :
                  'oh-dot-neutral'
                ]"
                aria-hidden="true"
              />
              {{ t('keys.status.' + value) }}
            </span>
          </template>

          <template #cell-last_used_at="{ value }">
            <span v-if="value" class="font-mono text-xs tabular-nums text-[var(--muted-foreground)]">
              {{ formatDateTime(value) }}
            </span>
            <span v-else class="text-sm text-[var(--muted-foreground)]">—</span>
          </template>

          <template #cell-last_used_ip="{ value }">
            <span v-if="value" class="font-mono text-xs tabular-nums text-[var(--muted-foreground)]">
              {{ value }}
            </span>
            <span v-else class="text-sm text-[var(--muted-foreground)]">—</span>
          </template>

          <template #cell-created_at="{ value }">
            <span class="font-mono text-xs tabular-nums text-[var(--muted-foreground)]">{{ formatDateTime(value) }}</span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center gap-1">
              <!-- Use Key Button -->
              <button
                @click="openUseKeyModal(row)"
                class="flex flex-col items-center gap-0.5 rounded-[var(--oh-r-sm,6px)] p-1.5 text-[var(--muted-foreground)] transition-colors hover:bg-[var(--surface-2)] hover:text-[var(--foreground)]"
              >
                <Icon name="terminal" size="sm" />
                <span class="text-xs">{{ t('keys.useKey') }}</span>
              </button>
              <!-- Import to CC Switch Button -->
              <button
                v-if="canMutateRow(row) && !publicSettings?.hide_ccs_import_button"
                @click="importToCcswitch(row)"
                class="flex flex-col items-center gap-0.5 rounded-[var(--oh-r-sm,6px)] p-1.5 text-[var(--muted-foreground)] transition-colors hover:bg-[var(--surface-2)] hover:text-[var(--foreground)]"
              >
                <Icon name="upload" size="sm" />
                <span class="text-xs">{{ t('keys.importToCcSwitch') }}</span>
              </button>
              <!-- Toggle Status Button -->
              <button
                v-if="canMutateRow(row)"
                @click="toggleKeyStatus(row)"
                :class="[
                  'flex flex-col items-center gap-0.5 rounded-[var(--oh-r-sm,6px)] p-1.5 transition-colors',
                  row.status === 'active'
                    ? 'text-[var(--muted-foreground)] hover:bg-[var(--surface-2)] hover:text-[var(--brand-text)]'
                    : 'text-[var(--muted-foreground)] hover:bg-[var(--surface-2)] hover:text-[var(--foreground)]'
                ]"
              >
                <Icon v-if="row.status === 'active'" name="ban" size="sm" />
                <Icon v-else name="checkCircle" size="sm" />
                <span class="text-xs">{{ row.status === 'active' ? t('keys.disable') : t('keys.enable') }}</span>
              </button>
              <!-- Rotate Button -->
              <button
                v-if="canMutateRow(row)"
                @click="openRotateModal(row)"
                class="flex flex-col items-center gap-0.5 rounded-[var(--oh-r-sm,6px)] p-1.5 text-[var(--muted-foreground)] transition-colors hover:bg-[var(--surface-2)] hover:text-[var(--brand-text)]"
              >
                <Icon name="refresh" size="sm" />
                <span class="text-xs">{{ t('keys.rotateKey') }}</span>
              </button>
              <!-- Edit Button -->
              <button
                v-if="canMutateRow(row)"
                @click="editKey(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-[var(--muted-foreground)] transition-colors hover:bg-[var(--surface-2)] hover:text-[var(--brand-text)]"
              >
                <Icon name="edit" size="sm" />
                <span class="text-xs">{{ t('common.edit') }}</span>
              </button>
              <!-- Delete Button -->
              <button
                v-if="canMutateRow(row)"
                @click="confirmDelete(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-[var(--muted-foreground)] transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
              >
                <Icon name="trash" size="sm" />
                <span class="text-xs">{{ t('common.delete') }}</span>
              </button>
            </div>
          </template>

          <template #empty>
            <EmptyState
              :title="t('keys.noKeysYet')"
              :description="canWriteKeys ? t('keys.createFirstKey') : (isZh ? '当前项目内暂无可查看的 API 密钥' : 'No readable API keys in this project')"
              :action-text="canWriteKeys ? t('keys.createKey') : undefined"
              @action="canWriteKeys && openCreateModal()"
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
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
      </TablePageLayout>
    </div>

    <!-- Create/Edit Modal -->
    <BaseDialog
      :show="showCreateModal || showEditModal"
      :title="showEditModal ? t('keys.editKey') : t('keys.createKey')"
      width="normal"
      @close="closeModals"
    >
      <ModelAccessRuleEditor v-if="showEditModal && selectedKey" :key="selectedKey.id" :path="`/keys/${selectedKey.id}`" />
      <form id="key-form" @submit.prevent="handleSubmit" class="space-y-5">
        <div
          v-if="!showEditModal"
          class="rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-3 text-xs"
        >
          <div class="flex flex-wrap items-center justify-between gap-2">
            <span class="font-medium text-[var(--foreground)]">
              {{
                isProjectMode
                  ? isZh
                    ? `签发作用域已锁定：${modalScopePin?.label || buildScopeLabel(activeConsoleScope.workspaceId, activeConsoleScope.projectId)}`
                    : `Pinned Issuance Scope: ${modalScopePin?.label || buildScopeLabel(activeConsoleScope.workspaceId, activeConsoleScope.projectId)}`
                  : isZh
                    ? `归属作用域：${t('scope.personal.badge')}`
                    : `Attribution Scope: ${t('scope.personal.badge')}`
              }}
            </span>
          </div>
          <div
            v-if="modalScopeMismatch"
            role="alert"
            data-testid="project-key-scope-mismatch-alert"
            class="mt-2 rounded border border-red-300 bg-red-50 px-2.5 py-2 text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300"
          >
            {{
              isZh
                ? '控制台作用域已变更，请关闭此弹窗后重新打开以防跨作用域误签发。'
                : 'Active scope changed while dialog was open. Reopen before issuing a key.'
            }}
          </div>
          <div
            v-if="isProjectMode"
            data-testid="project-key-scope-preview"
            class="mt-2.5 space-y-1.5 rounded-lg border border-[var(--border)] bg-[var(--card)] p-2.5 text-[11px]"
          >
            <p class="font-semibold text-[var(--foreground)]">
              {{ t('keys.projectScopePreviewTitle') }}
            </p>
            <div
              v-if="issuableActionsResult.status !== 'allowed' || selectedProjectKeyActions.length === 0"
              role="alert"
              data-testid="project-key-empty-actions-alert"
              class="rounded border border-amber-300 bg-amber-50 px-2.5 py-2 text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-300"
            >
              <p>{{ t('rbac.keys.emptyActions') }}</p>
              <button
                type="button"
                data-testid="project-key-request-grant-btn"
                class="mt-1 font-semibold underline"
                @click="navigateConsole('/workspaces?requestAccess=1')"
              >
                {{ t('rbac.keys.requestCallGrantAction') }}
              </button>
            </div>
            <label
              v-if="issuableSupportsModelsRead"
              class="flex cursor-pointer items-center gap-2 text-[var(--foreground)]"
            >
              <input
                v-model="projectKeyAllowModelsRead"
                type="checkbox"
                data-testid="project-key-models-read-checkbox"
                class="h-3.5 w-3.5 rounded border-[var(--border)]"
              />
              <span>{{ t('keys.projectScopeModelsReadLabel') }}</span>
            </label>
            <div class="flex flex-wrap items-center gap-1.5">
              <span class="text-[var(--muted-foreground)]">{{ t('keys.projectScopeGrantedActions') }}:</span>
              <code
                v-for="act in selectedProjectKeyActions"
                :key="act"
                class="rounded border border-[var(--border)] bg-[var(--surface-2,transparent)] px-1.5 py-0.5 font-mono text-[var(--foreground)]"
              >
                {{ act }}
              </code>
            </div>
            <div class="space-y-1.5 border-t border-[var(--border)] pt-2">
              <label class="block font-medium text-[var(--foreground)]">
                {{ isZh ? '模型访问策略 (model_access)' : 'Model Access Policy (model_access)' }}
              </label>
              <select
                v-model="projectModelAccessMode"
                data-testid="project-key-model-access-mode"
                class="input py-1 text-xs"
              >
                <option value="inherit">{{ isZh ? '继承路由分组默认目录 (inherit)' : 'Inherit routing group (inherit)' }}</option>
                <option value="allow_list">{{ isZh ? '仅允许指定模型白名单 (allow_list)' : 'Specific model allow-list (allow_list)' }}</option>
                <option value="deny_all">{{ isZh ? '拒绝所有模型调用 (deny_all)' : 'Deny all model invocations (deny_all)' }}</option>
              </select>
              <input
                v-if="projectModelAccessMode === 'allow_list'"
                v-model="projectModelAccessIdsText"
                type="text"
                data-testid="project-key-model-access-ids"
                class="input py-1 font-mono text-xs"
                :placeholder="isZh ? '输入允许的模型 ID，英文逗号分隔，如 gpt-4o, claude-sonnet-4' : 'Comma-separated model IDs, e.g. gpt-4o, claude-sonnet-4'"
              />
            </div>
            <p
              v-if="issuableSupportsModelsRead"
              :class="
                projectKeyAllowModelsRead
                  ? 'text-emerald-700 dark:text-emerald-300'
                  : 'text-[var(--muted-foreground)]'
              "
            >
              {{
                projectKeyAllowModelsRead
                  ? t('keys.projectScopeModelsReadIncluded')
                  : t('keys.projectScopeModelsReadExcluded')
              }}
            </p>
          </div>
        </div>

        <div>
          <label class="input-label">{{ t('keys.nameLabel') }}</label>
          <input
            v-model="formData.name"
            type="text"
            required
            class="input"
            :placeholder="t('keys.namePlaceholder')"
            data-tour="key-form-name"
          />
        </div>

        <fieldset v-if="!showEditModal" data-tour="key-form-provider">
          <legend class="input-label">{{ t('keys.providerLabel') }}</legend>
          <div class="grid grid-cols-2 gap-2 sm:grid-cols-4">
            <label
              v-for="provider in createProviderOptions"
              :key="provider.value"
              class="relative min-w-0"
              :class="provider.count === 0 ? 'cursor-not-allowed' : 'cursor-pointer'"
            >
              <input
                type="radio"
                name="key-provider"
                :value="provider.value"
                :checked="createProvider === provider.value"
                :disabled="provider.count === 0"
                class="peer sr-only"
                @change="selectCreateProvider(provider.value)"
              />
              <span
                class="flex h-full flex-col items-center gap-2 rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] px-2 py-3 text-center transition-colors peer-checked:border-[var(--oh-accent,#f59e0b)] peer-checked:bg-[var(--oh-accent-subtle,rgba(245,158,11,0.1))] peer-focus-visible:outline peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-[var(--oh-accent,#f59e0b)] peer-disabled:opacity-40"
                :class="provider.count > 0 && 'hover:border-[var(--oh-border-strong,var(--border))]'"
              >
                <span class="flex h-8 items-center justify-center gap-1.5" aria-hidden="true">
                  <span
                    v-for="platform in KEY_GROUP_PROVIDER_ICONS[provider.value]"
                    :key="platform"
                    class="flex h-8 w-8 items-center justify-center rounded-lg"
                    :class="platformBadgeLightClass(platform)"
                  >
                    <PlatformIcon :platform="platform" size="lg" />
                  </span>
                </span>
                <span class="text-sm font-semibold text-gray-800 dark:text-gray-100">{{ provider.label }}</span>
              </span>
              <span
                v-if="createProvider === provider.value"
                class="absolute right-1.5 top-1.5 flex h-4 w-4 items-center justify-center rounded-full bg-[var(--oh-accent,#f59e0b)] text-[#141008]"
                aria-hidden="true"
              >
                <Icon name="check" size="xs" :stroke-width="3" />
              </span>
            </label>
          </div>
          <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-gray-400" aria-live="polite">
            {{ groups.length === 0 ? t('common.noGroupsAvailable') : t(`keys.providerHints.${createProvider}`) }}
          </p>
        </fieldset>

        <div>
          <label class="input-label" for="key-form-group">{{ t('keys.groupLabel') }}</label>
          <p v-if="showCreateModal && groupsLoading" role="status" class="mb-2 text-sm text-[var(--muted-foreground)]">{{ t('keys.groupsLoading') }}</p>
          <p v-else-if="showCreateModal && groupsLoadFailed" role="alert" class="mb-2 text-sm text-red-600">
            {{ t('keys.groupsLoadFailed') }}
            <button type="button" class="underline" @click="loadGroups">{{ t('keys.retryGroups') }}</button>
          </p>
          <p v-else-if="showCreateModal && groups.length === 0" role="alert" class="mb-2 text-sm text-amber-600">{{ t('keys.noAvailableGroups') }}</p>
          <Select
            :key="showEditModal ? 'edit' : createProvider"
            id="key-form-group"
            :aria-label="t('keys.groupLabel')"
            v-model="formData.group_id"
            :options="formGroupOptions"
            :placeholder="t('keys.selectGroup')"
            :empty-text="t('common.noGroupsAvailable')"
            :searchable="true"
            :search-placeholder="t('keys.searchGroup')"
            data-tour="key-form-group"
          >
            <template #selected="{ option }">
              <GroupBadge
                v-if="option"
                :name="(option as unknown as GroupOption).label"
                :platform="(option as unknown as GroupOption).platform"
                :subscription-type="(option as unknown as GroupOption).subscriptionType"
                :rate-multiplier="(option as unknown as GroupOption).rate"
                :user-rate-multiplier="(option as unknown as GroupOption).userRate"
                :peak-rate-enabled="(option as unknown as GroupOption).peakRateEnabled"
                :peak-start="(option as unknown as GroupOption).peakStart"
                :peak-end="(option as unknown as GroupOption).peakEnd"
                :peak-rate-multiplier="(option as unknown as GroupOption).peakRateMultiplier"
              />
              <span v-else class="text-[var(--muted-foreground)]">{{ t('keys.selectGroup') }}</span>
            </template>
            <template #option="{ option, selected }">
              <GroupOptionItem
                :name="(option as unknown as GroupOption).label"
                :platform="(option as unknown as GroupOption).platform"
                :subscription-type="(option as unknown as GroupOption).subscriptionType"
                :rate-multiplier="(option as unknown as GroupOption).rate"
                :user-rate-multiplier="(option as unknown as GroupOption).userRate"
                :peak-rate-enabled="(option as unknown as GroupOption).peakRateEnabled"
                :peak-start="(option as unknown as GroupOption).peakStart"
                :peak-end="(option as unknown as GroupOption).peakEnd"
                :peak-rate-multiplier="(option as unknown as GroupOption).peakRateMultiplier"
                :description="(option as unknown as GroupOption).description"
                :selected="selected"
              />
            </template>
          </Select>
          <div
            v-if="!showEditModal && !groupsLoading && !groupsLoadFailed && recommendedDefaultGroup"
            data-testid="key-group-binding-guide"
            class="mt-2 flex flex-wrap items-center justify-between gap-2 rounded-lg border border-[var(--oh-accent-border,rgba(245,158,11,0.35))] bg-[var(--oh-accent-subtle,rgba(245,158,11,0.08))] px-3 py-2 text-xs"
          >
            <span class="text-[var(--foreground)]">
              {{
                formData.group_id
                  ? isZh
                    ? `已绑定至可用路由分组：${selectedFormGroupLabel}`
                    : `Bound to routing group: ${selectedFormGroupLabel}`
                  : isZh
                    ? `默认绑定至可用路由分组：推荐绑定「${recommendedDefaultGroup.label}」以防止无分组密钥触发 403`
                    : `Default routing group binding: bind "${recommendedDefaultGroup.label}" to prevent ungrouped 403 errors`
              }}
            </span>
            <button
              v-if="!formData.group_id"
              type="button"
              data-testid="key-apply-recommended-group"
              class="font-semibold text-[var(--oh-accent-text,var(--brand-text,#d97706))] underline underline-offset-2"
              @click="applyRecommendedGroup"
            >
              {{ isZh ? `一键绑定「${recommendedDefaultGroup.label}」` : `Bind "${recommendedDefaultGroup.label}"` }}
            </button>
          </div>
          <p
            v-if="!showEditModal && isProjectMode && !groupsLoading && !groupsLoadFailed && resolvedProjectGroupId === null"
            role="alert"
            data-testid="project-key-group-required-alert"
            class="mt-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300"
          >
            {{ t('keys.projectGroupRequired') }}
          </p>
        </div>

        <!-- Custom Key Section (only for create) -->
        <div v-if="!showEditModal" class="space-y-3">
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('keys.customKeyLabel') }}</label>
            <button
              type="button"
              @click="formData.use_custom_key = !formData.use_custom_key"
              :class="[
                'relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none',
                formData.use_custom_key ? 'bg-[var(--brand)]' : 'bg-[var(--surface-2)] '
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-[var(--card)] shadow ring-0 transition duration-200 ease-in-out',
                  formData.use_custom_key ? 'translate-x-4' : 'translate-x-0'
                ]"
              />
            </button>
          </div>
          <div v-if="formData.use_custom_key">
            <input
              v-model="formData.custom_key"
              type="text"
              class="input font-mono"
              :placeholder="t('keys.customKeyPlaceholder')"
              :class="{ 'border-red-500 dark:border-red-500': customKeyError }"
            />
            <p v-if="customKeyError" class="mt-1 text-sm text-red-500">{{ customKeyError }}</p>
            <p v-else class="input-hint">{{ t('keys.customKeyHint') }}</p>
          </div>
        </div>

        <div v-if="showEditModal">
          <label class="input-label">{{ t('keys.statusLabel') }}</label>
          <Select
            v-model="formData.status"
            :options="statusOptions"
            :disabled="statusLockedByAdmin"
            :placeholder="t('keys.selectStatus')"
          />
          <p v-if="statusLockedByAdmin" class="input-hint">
            {{ t('keys.statusDisabledHint') }}
          </p>
        </div>

        <!-- IP Restriction Section -->
        <div class="space-y-3">
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('keys.ipRestriction') }}</label>
            <button
              type="button"
              @click="formData.enable_ip_restriction = !formData.enable_ip_restriction"
              :class="[
                'relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none',
                formData.enable_ip_restriction ? 'bg-[var(--brand)]' : 'bg-[var(--surface-2)] '
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-[var(--card)] shadow ring-0 transition duration-200 ease-in-out',
                  formData.enable_ip_restriction ? 'translate-x-4' : 'translate-x-0'
                ]"
              />
            </button>
          </div>

          <div v-if="formData.enable_ip_restriction" class="space-y-4 pt-2">
            <div>
              <label class="input-label">{{ t('keys.ipWhitelist') }}</label>
              <textarea
                v-model="formData.ip_whitelist"
                rows="3"
                class="input font-mono text-sm"
                :placeholder="t('keys.ipWhitelistPlaceholder')"
              />
              <p class="input-hint">{{ t('keys.ipWhitelistHint') }}</p>
            </div>

            <div>
              <label class="input-label">{{ t('keys.ipBlacklist') }}</label>
              <textarea
                v-model="formData.ip_blacklist"
                rows="3"
                class="input font-mono text-sm"
                :placeholder="t('keys.ipBlacklistPlaceholder')"
              />
              <p class="input-hint">{{ t('keys.ipBlacklistHint') }}</p>
            </div>
          </div>
        </div>

        <!-- Quota Limit Section -->
        <div class="space-y-3">
          <label class="input-label">{{ t('keys.quotaLimit') }}</label>
          <!-- Switch commented out - always show input, 0 = unlimited
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('keys.quotaLimit') }}</label>
            <button
              type="button"
              @click="formData.enable_quota = !formData.enable_quota"
              :class="[
                'relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none',
                formData.enable_quota ? 'bg-[var(--brand)]' : 'bg-[var(--surface-2)] '
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-[var(--card)] shadow ring-0 transition duration-200 ease-in-out',
                  formData.enable_quota ? 'translate-x-4' : 'translate-x-0'
                ]"
              />
            </button>
          </div>
          -->

          <div class="space-y-4">
            <div>
              <div class="relative">
                <span class="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--muted-foreground)]">$</span>
                <input
                  v-model.number="formData.quota"
                  type="number"
                  step="0.01"
                  min="0"
                  class="input pl-7 font-mono tabular-nums"
                  :placeholder="t('keys.quotaAmountPlaceholder')"
                />
              </div>
              <p class="input-hint">{{ t('keys.quotaAmountHint') }}</p>
            </div>

            <!-- Quota used display (only in edit mode) -->
            <div v-if="showEditModal && selectedKey && selectedKey.quota > 0">
              <label class="input-label">{{ t('keys.quotaUsed') }}</label>
              <div class="flex items-center gap-2">
                <div class="flex-1 rounded-lg bg-[var(--surface-2)] px-3 py-2 font-mono tabular-nums">
                  <span class="font-medium text-[var(--foreground)]">
                    ${{ selectedKey.quota_used?.toFixed(4) || '0.0000' }}
                  </span>
                  <span class="mx-2 text-[var(--muted-foreground)]">/</span>
                  <span class="text-[var(--muted-foreground)]">
                    ${{ selectedKey.quota?.toFixed(2) || '0.00' }}
                  </span>
                </div>
              </div>
              <p class="input-hint">{{ t('keys.usageSelfServiceHint') }}</p>
            </div>
          </div>
        </div>

        <!-- Rate Limit Section -->
        <div class="space-y-3">
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('keys.rateLimitSection') }}</label>
            <button
              type="button"
              @click="formData.enable_rate_limit = !formData.enable_rate_limit"
              :class="[
                'relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none',
                formData.enable_rate_limit ? 'bg-[var(--brand)]' : 'bg-[var(--surface-2)] '
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-[var(--card)] shadow ring-0 transition duration-200 ease-in-out',
                  formData.enable_rate_limit ? 'translate-x-4' : 'translate-x-0'
                ]"
              />
            </button>
          </div>

          <div v-if="formData.enable_rate_limit" class="space-y-4 pt-2">
            <p class="input-hint -mt-2">{{ t('keys.rateLimitHint') }}</p>
            <!-- 5-Hour Limit -->
            <div>
              <label class="input-label">{{ t('keys.rateLimit5h') }}</label>
              <div class="relative">
                <span class="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--muted-foreground)]">$</span>
                <input
                  v-model.number="formData.rate_limit_5h"
                  type="number"
                  step="0.01"
                  min="0"
                  class="input pl-7 font-mono tabular-nums"
                  :placeholder="'0'"
                />
              </div>
              <!-- Usage info (edit mode only) -->
              <div v-if="showEditModal && selectedKey && selectedKey.rate_limit_5h > 0" class="mt-2">
                <div class="flex items-center gap-2">
                  <div class="flex-1 rounded-lg bg-[var(--surface-2)] px-3 py-2 font-mono text-sm tabular-nums">
                    <span :class="[
                      'font-medium',
                      selectedKey.usage_5h >= selectedKey.rate_limit_5h ? 'text-red-500' :
                      selectedKey.usage_5h >= selectedKey.rate_limit_5h * 0.8 ? 'text-yellow-500' :
                      'text-[var(--foreground)] '
                    ]">
                      ${{ selectedKey.usage_5h?.toFixed(4) || '0.0000' }}
                    </span>
                    <span class="mx-2 text-[var(--muted-foreground)]">/</span>
                    <span class="text-[var(--muted-foreground)]">
                      ${{ selectedKey.rate_limit_5h?.toFixed(2) || '0.00' }}
                    </span>
                  </div>
                </div>
                <div class="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-[var(--surface-2)]">
                  <div
                    :class="[
                      'h-full rounded-full transition-all',
                      selectedKey.usage_5h >= selectedKey.rate_limit_5h ? 'bg-red-500' :
                      selectedKey.usage_5h >= selectedKey.rate_limit_5h * 0.8 ? 'bg-yellow-500' :
                      'bg-green-500'
                    ]"
                    :style="{ width: Math.min((selectedKey.usage_5h / selectedKey.rate_limit_5h) * 100, 100) + '%' }"
                  />
                </div>
              </div>
            </div>

            <!-- Daily Limit -->
            <div>
              <label class="input-label">{{ t('keys.rateLimit1d') }}</label>
              <div class="relative">
                <span class="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--muted-foreground)]">$</span>
                <input
                  v-model.number="formData.rate_limit_1d"
                  type="number"
                  step="0.01"
                  min="0"
                  class="input pl-7 font-mono tabular-nums"
                  :placeholder="'0'"
                />
              </div>
              <!-- Usage info (edit mode only) -->
              <div v-if="showEditModal && selectedKey && selectedKey.rate_limit_1d > 0" class="mt-2">
                <div class="flex items-center gap-2">
                  <div class="flex-1 rounded-lg bg-[var(--surface-2)] px-3 py-2 font-mono text-sm tabular-nums">
                    <span :class="[
                      'font-medium',
                      selectedKey.usage_1d >= selectedKey.rate_limit_1d ? 'text-red-500' :
                      selectedKey.usage_1d >= selectedKey.rate_limit_1d * 0.8 ? 'text-yellow-500' :
                      'text-[var(--foreground)] '
                    ]">
                      ${{ selectedKey.usage_1d?.toFixed(4) || '0.0000' }}
                    </span>
                    <span class="mx-2 text-[var(--muted-foreground)]">/</span>
                    <span class="text-[var(--muted-foreground)]">
                      ${{ selectedKey.rate_limit_1d?.toFixed(2) || '0.00' }}
                    </span>
                  </div>
                </div>
                <div class="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-[var(--surface-2)]">
                  <div
                    :class="[
                      'h-full rounded-full transition-all',
                      selectedKey.usage_1d >= selectedKey.rate_limit_1d ? 'bg-red-500' :
                      selectedKey.usage_1d >= selectedKey.rate_limit_1d * 0.8 ? 'bg-yellow-500' :
                      'bg-green-500'
                    ]"
                    :style="{ width: Math.min((selectedKey.usage_1d / selectedKey.rate_limit_1d) * 100, 100) + '%' }"
                  />
                </div>
              </div>
            </div>

            <!-- 7-Day Limit -->
            <div>
              <label class="input-label">{{ t('keys.rateLimit7d') }}</label>
              <div class="relative">
                <span class="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--muted-foreground)]">$</span>
                <input
                  v-model.number="formData.rate_limit_7d"
                  type="number"
                  step="0.01"
                  min="0"
                  class="input pl-7 font-mono tabular-nums"
                  :placeholder="'0'"
                />
              </div>
              <!-- Usage info (edit mode only) -->
              <div v-if="showEditModal && selectedKey && selectedKey.rate_limit_7d > 0" class="mt-2">
                <div class="flex items-center gap-2">
                  <div class="flex-1 rounded-lg bg-[var(--surface-2)] px-3 py-2 font-mono text-sm tabular-nums">
                    <span :class="[
                      'font-medium',
                      selectedKey.usage_7d >= selectedKey.rate_limit_7d ? 'text-red-500' :
                      selectedKey.usage_7d >= selectedKey.rate_limit_7d * 0.8 ? 'text-yellow-500' :
                      'text-[var(--foreground)] '
                    ]">
                      ${{ selectedKey.usage_7d?.toFixed(4) || '0.0000' }}
                    </span>
                    <span class="mx-2 text-[var(--muted-foreground)]">/</span>
                    <span class="text-[var(--muted-foreground)]">
                      ${{ selectedKey.rate_limit_7d?.toFixed(2) || '0.00' }}
                    </span>
                  </div>
                </div>
                <div class="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-[var(--surface-2)]">
                  <div
                    :class="[
                      'h-full rounded-full transition-all',
                      selectedKey.usage_7d >= selectedKey.rate_limit_7d ? 'bg-red-500' :
                      selectedKey.usage_7d >= selectedKey.rate_limit_7d * 0.8 ? 'bg-yellow-500' :
                      'bg-green-500'
                    ]"
                    :style="{ width: Math.min((selectedKey.usage_7d / selectedKey.rate_limit_7d) * 100, 100) + '%' }"
                  />
                </div>
              </div>
            </div>

          </div>
        </div>

        <!-- Period Budget Section (USD, separate from RPM/TPM) -->
        <div class="space-y-3">
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('keys.budgetSection') }}</label>
            <button
              type="button"
              data-test="key-form-budget-toggle"
              :disabled="formData.budget_enforced"
              @click="formData.enable_budget = !formData.enable_budget"
              :class="[
                'relative inline-flex h-5 w-9 flex-shrink-0 rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none',
                formData.budget_enforced ? 'cursor-not-allowed opacity-60' : 'cursor-pointer',
                formData.enable_budget ? 'bg-[var(--brand)]' : 'bg-[var(--surface-2)] '
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-[var(--card)] shadow ring-0 transition duration-200 ease-in-out',
                  formData.enable_budget ? 'translate-x-4' : 'translate-x-0'
                ]"
              />
            </button>
          </div>

          <p v-if="formData.budget_enforced" class="input-hint -mt-2 text-amber-600 dark:text-amber-400">
            {{ t('keys.budgetEnforced') }}
          </p>
          <p v-else class="input-hint -mt-2">{{ t('keys.budgetHint') }}</p>

          <div v-if="formData.enable_budget" class="space-y-4 pt-2">
            <div>
              <label class="input-label">{{ t('keys.budgetAmount') }}</label>
              <div class="relative">
                <span class="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--muted-foreground)]">$</span>
                <input
                  v-model.number="formData.budget_limit"
                  data-test="key-form-budget-limit"
                  type="number"
                  step="0.01"
                  min="0"
                  class="input pl-7"
                  :disabled="formData.budget_enforced"
                  placeholder="0"
                />
              </div>
              <p class="input-hint">{{ t('keys.budgetAmountHint') }}</p>
            </div>

            <div>
              <label class="input-label">{{ t('keys.budgetPeriod') }}</label>
              <select v-model="formData.budget_period_kind" data-test="key-form-budget-period" class="input" :disabled="formData.budget_enforced">
                <option value="legacy_5h">{{ t('keys.budgetPeriodLegacy5h') }}</option>
                <option value="legacy_1d">{{ t('keys.budgetPeriodLegacy1d') }}</option>
                <option value="legacy_7d">{{ t('keys.budgetPeriodLegacy7d') }}</option>
                <option value="duration">{{ t('keys.budgetPeriodDuration') }}</option>
                <option value="calendar_day">{{ t('keys.budgetPeriodCalendarDay') }}</option>
                <option value="calendar_week">{{ t('keys.budgetPeriodCalendarWeek') }}</option>
                <option value="calendar_month">{{ t('keys.budgetPeriodCalendarMonth') }}</option>
              </select>
              <p class="input-hint">{{ t('keys.budgetPeriodHint') }}</p>
            </div>

            <div v-if="formData.budget_period_kind === 'duration'" class="flex items-end gap-2">
              <div class="flex-1">
                <label class="input-label">{{ t('keys.budgetDurationValue') }}</label>
                <input
                  v-model.number="formData.budget_duration_value"
                  data-test="key-form-budget-duration-value"
                  type="number"
                  min="1"
                  step="1"
                  class="input"
                  :disabled="formData.budget_enforced"
                />
              </div>
              <div class="w-32">
                <label class="input-label">{{ t('keys.budgetDurationUnit') }}</label>
                <select v-model="formData.budget_duration_unit" class="input" :disabled="formData.budget_enforced">
                  <option value="minutes">{{ t('keys.budgetDurationMinutes') }}</option>
                  <option value="hours">{{ t('keys.budgetDurationHours') }}</option>
                  <option value="days">{{ t('keys.budgetDurationDays') }}</option>
                </select>
              </div>
            </div>

            <div v-if="formData.budget_period_kind.startsWith('calendar_')">
              <label class="input-label">{{ t('keys.budgetTimezone') }}</label>
              <input
                v-model.trim="formData.budget_timezone"
                type="text"
                class="input"
                :disabled="formData.budget_enforced"
                placeholder="Asia/Shanghai"
              />
              <p class="input-hint">{{ t('keys.budgetTimezoneHint') }}</p>
            </div>

            <div v-if="showEditModal && selectedKey && selectedKey.budget_period_mode !== 'off'" class="space-y-1">
              <label class="input-label">{{ t('keys.budgetUsage') }}</label>
              <div class="rounded-lg bg-[var(--surface-2)] px-3 py-2 font-mono text-sm tabular-nums">
                <span class="font-medium text-[var(--foreground)]">
                  ${{ selectedKey.budget_used?.toFixed(4) || '0.0000' }}
                </span>
                <span class="mx-2 text-[var(--muted-foreground)]">/</span>
                <span class="text-[var(--muted-foreground)]">
                  ${{ selectedKey.budget_limit?.toFixed(2) || '0.00' }}
                </span>
                <span v-if="selectedKey.budget_reserved > 0" class="ml-2 text-xs text-[var(--muted-foreground)]">
                  {{ t('keys.budgetReserved') }} ${{ selectedKey.budget_reserved.toFixed(4) }}
                </span>
                <span v-if="selectedKey.budget_overrun > 0" class="ml-2 text-xs text-amber-600 dark:text-amber-400">
                  {{ t('keys.budgetOverrun') }} ${{ selectedKey.budget_overrun.toFixed(4) }}
                </span>
              </div>
              <p class="input-hint">
                {{ selectedKey.budget_legacy_anchor || selectedKey.budget_period_display }}
                <template v-if="selectedKey.budget_period_end">
                  · {{ t('keys.budgetNextReset') }} {{ formatResetTime(selectedKey.budget_period_end) }}
                </template>
              </p>
              <p class="input-hint">{{ t('keys.usageSelfServiceHint') }}</p>
            </div>
          </div>
        </div>

        <!-- Expiration Section -->
        <div class="space-y-3">
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('keys.expiration') }}</label>
            <button
              type="button"
              @click="formData.enable_expiration = !formData.enable_expiration"
              :class="[
                'relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none',
                formData.enable_expiration ? 'bg-[var(--brand)]' : 'bg-[var(--surface-2)] '
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-[var(--card)] shadow ring-0 transition duration-200 ease-in-out',
                  formData.enable_expiration ? 'translate-x-4' : 'translate-x-0'
                ]"
              />
            </button>
          </div>

          <div v-if="formData.enable_expiration" class="space-y-4 pt-2">
            <!-- Quick select buttons (for both create and edit mode) -->
            <div class="flex flex-wrap gap-2">
              <button
                v-for="days in ['7', '30', '90']"
                :key="days"
                type="button"
                @click="setExpirationDays(parseInt(days))"
                :class="[
                  'rounded-lg px-3 py-1.5 text-sm transition-colors',
                  formData.expiration_preset === days
                    ? 'bg-[color-mix(in_srgb,var(--brand)_10%,var(--card))] text-[var(--brand-text)]  '
                    : 'bg-[var(--surface-2)] text-[var(--muted-foreground)] hover:bg-[var(--surface-2)]   '
                ]"
              >
                {{ showEditModal ? t('keys.extendDays', { days }) : t('keys.expiresInDays', { days }) }}
              </button>
              <button
                type="button"
                @click="formData.expiration_preset = 'custom'"
                :class="[
                  'rounded-lg px-3 py-1.5 text-sm transition-colors',
                  formData.expiration_preset === 'custom'
                    ? 'bg-[color-mix(in_srgb,var(--brand)_10%,var(--card))] text-[var(--brand-text)]  '
                    : 'bg-[var(--surface-2)] text-[var(--muted-foreground)] hover:bg-[var(--surface-2)]   '
                ]"
              >
                {{ t('keys.customDate') }}
              </button>
            </div>

            <!-- Date picker (always show for precise adjustment) -->
            <div>
              <label class="input-label">{{ t('keys.expirationDate') }}</label>
              <input
                v-model="formData.expiration_date"
                type="datetime-local"
                class="input"
              />
              <p class="input-hint">{{ t('keys.expirationDateHint') }}</p>
            </div>

            <!-- Current expiration display (only in edit mode) -->
            <div v-if="showEditModal && selectedKey?.expires_at" class="text-sm">
              <span class="text-[var(--muted-foreground)]">{{ t('keys.currentExpiration') }}: </span>
              <span class="font-medium text-[var(--foreground)]">
                {{ formatDateTime(selectedKey.expires_at) }}
              </span>
            </div>
          </div>
        </div>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button @click="closeModals" type="button" class="btn btn-secondary">
            {{ t('common.cancel') }}
          </button>
          <button
            form="key-form"
            type="submit"
            :disabled="submitting || (!showEditModal && (groupsLoading || groupsLoadFailed || modalScopeMismatch || (isProjectMode ? (resolvedProjectGroupId === null || issuableActionsResult.status !== 'allowed' || selectedProjectKeyActions.length === 0) : (!formData.group_id || !groups.some(group => group.id === formData.group_id)))))"
            class="btn btn-primary"
            data-tour="key-form-submit"
          >
            <svg
              v-if="submitting"
              class="-ml-1 mr-2 h-4 w-4 motion-safe:animate-spin"
              fill="none"
              viewBox="0 0 24 24"
            >
              <circle
                class="opacity-25"
                cx="12"
                cy="12"
                r="10"
                stroke="currentColor"
                stroke-width="4"
              ></circle>
              <path
                class="opacity-75"
                fill="currentColor"
                d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
              ></path>
            </svg>
            {{
              submitting
                ? t('keys.saving')
                : showEditModal
                  ? t('common.update')
                  : t('common.create')
            }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BulkEditKeysModal
      :show="showBulkEditModal"
      :selected-keys="selectedApiKeys"
      :groups="groups"
      @close="showBulkEditModal = false"
      @updated="handleBulkUpdated"
    />

    <!-- Delete Confirmation Dialog -->
    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('keys.deleteKey')"
      :message="t('keys.deleteConfirmMessage', { name: selectedKey?.name })"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="handleDelete"
      @cancel="showDeleteDialog = false"
    />

    <!-- Use Key Modal -->
    <UseKeyModal
      :show="showUseKeyModal"
      :api-key="'<YOUR_API_KEY>'"
      :base-url="publicSettings?.api_base_url || defaultGatewayOrigin"
      :site-name="publicSettings?.site_name"
      :platform="selectedKey?.group?.platform || null"
      :allow-messages-dispatch="selectedKey?.group?.allow_messages_dispatch || false"
      @close="closeUseKeyModal"
    />

    <!-- Key Rotation Dialog -->
    <KeyRotationDialog
      :show="showRotateModal"
      :api-key="rotateTarget"
      :rotate-fn="rotateKeyForScope"
      @close="showRotateModal = false"
      @issued="onCredentialIssued"
    />

    <!-- One-time secret dialog -->
    <KeySecretDialog
      :show="showSecretDialog"
      :secret="issuedSecret"
      :grace-until="issuedGraceUntil"
      :scope-label="issuedSecretScopeLabel || null"
      @close="closeSecretDialog"
    />

    <!-- CCS Client Selection Dialog for Antigravity -->
    <BaseDialog
      :show="showCcsClientSelect"
      :title="t('keys.ccsClientSelect.title')"
      width="narrow"
      @close="closeCcsClientSelect"
    >
      <div class="space-y-4">
        <p class="text-sm text-[var(--muted-foreground)]">
          {{ t('keys.ccsClientSelect.description') }}
	        </p>
	        <div class="grid grid-cols-2 gap-3">
	          <button
	            @click="handleCcsClientSelect('claude')"
	            class="flex flex-col items-center gap-2 p-4 rounded-xl border-2 border-[var(--border)] hover:border-[var(--brand)] hover:bg-[color-mix(in_srgb,var(--brand)_10%,var(--card))] transition-all"
	          >
	            <Icon name="terminal" size="xl" class="text-[var(--muted-foreground)]" />
	            <span class="font-medium text-[var(--foreground)]">{{
	              t('keys.ccsClientSelect.claudeCode')
	            }}</span>
	            <span class="text-xs text-[var(--muted-foreground)]">{{
	              t('keys.ccsClientSelect.claudeCodeDesc')
	            }}</span>
	          </button>
	          <button
	            @click="handleCcsClientSelect('gemini')"
	            class="flex flex-col items-center gap-2 p-4 rounded-xl border-2 border-[var(--border)] hover:border-[var(--brand)] hover:bg-[color-mix(in_srgb,var(--brand)_10%,var(--card))] transition-all"
	          >
	            <Icon name="sparkles" size="xl" class="text-[var(--muted-foreground)]" />
	            <span class="font-medium text-[var(--foreground)]">{{
	              t('keys.ccsClientSelect.geminiCli')
	            }}</span>
	            <span class="text-xs text-[var(--muted-foreground)]">{{
	              t('keys.ccsClientSelect.geminiCliDesc')
	            }}</span>
	          </button>
	        </div>
	      </div>
      <template #footer>
        <div class="flex justify-end">
          <button @click="closeCcsClientSelect" class="btn btn-secondary">
            {{ t('common.cancel') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Group Selector Dropdown (Teleported to body to avoid overflow clipping) -->
    <Teleport to="body">
      <div
        v-if="groupSelectorKeyId !== null && dropdownPosition"
        ref="dropdownRef"
        class="animate-in fade-in slide-in-from-top-2 fixed z-[100000020] w-max max-w-[calc(100vw-16px)] overflow-hidden rounded-xl bg-[var(--card)] shadow-lg ring-1 ring-black/5 duration-200 sm:min-w-[380px] dark:ring-white/10"
        style="pointer-events: auto !important;"
        :style="{
          top: dropdownPosition.top !== undefined ? dropdownPosition.top + 'px' : undefined,
          bottom: dropdownPosition.bottom !== undefined ? dropdownPosition.bottom + 'px' : undefined,
          left: dropdownPosition.left + 'px'
        }"
      >
        <!-- Search box -->
        <div class="border-b border-[var(--border)] p-2">
          <div class="relative">
            <svg class="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-[var(--muted-foreground)]" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
            <input
              v-model="groupSearchQuery"
              type="text"
              class="w-full rounded-lg border border-[var(--border)] bg-[var(--surface-2)] py-1.5 pl-8 pr-3 text-sm text-[var(--foreground)] placeholder-gray-400 outline-none focus:border-[var(--brand)] focus:ring-1 focus:ring-[var(--brand)] dark:placeholder-gray-500"
              :placeholder="t('keys.searchGroup')"
              @click.stop
            />
          </div>
        </div>
        <!-- Group list -->
        <div class="max-h-80 overflow-y-auto p-1.5">
          <button
            v-for="option in filteredGroupOptions"
            :key="option.value ?? 'null'"
            @click="changeGroup(selectedKeyForGroup!, option.value)"
            :class="[
              'flex w-full items-center justify-between rounded-lg px-3 py-2.5 text-sm transition-colors',
              'border-b border-[var(--border)] last:border-0 ',
              selectedKeyForGroup?.group_id === option.value ||
              (!selectedKeyForGroup?.group_id && option.value === null)
                ? 'bg-[color-mix(in_srgb,var(--brand)_10%,var(--card))] '
                : 'hover:bg-[var(--surface-2)] '
            ]"
            :title="option.description || undefined"
          >
            <GroupOptionItem
              :name="option.label"
              :platform="option.platform"
              :subscription-type="option.subscriptionType"
              :rate-multiplier="option.rate"
              :user-rate-multiplier="option.userRate"
              :peak-rate-enabled="option.peakRateEnabled"
              :peak-start="option.peakStart"
              :peak-end="option.peakEnd"
              :peak-rate-multiplier="option.peakRateMultiplier"
              :description="option.description"
              :selected="
                selectedKeyForGroup?.group_id === option.value ||
                (!selectedKeyForGroup?.group_id && option.value === null)
              "
            />
          </button>
          <!-- Empty state when search has no results -->
          <div v-if="filteredGroupOptions.length === 0" class="py-4 text-center text-sm text-[var(--muted-foreground)]">
            {{ t('keys.noGroupFound') }}
          </div>
        </div>
      </div>
    </Teleport>
  </AppLayout>
</template>

<script setup lang="ts">
import UsageStatsState from '@/components/common/UsageStatsState.vue'
import { historyGapFromError, type HistoryGap } from '@/views/trendGaps'
import ModelAccessRuleEditor from '@/components/model-access/ModelAccessRuleEditor.vue'
import { extractApiErrorMessage } from '@/utils/apiError'
	import { ref, reactive, computed, getCurrentInstance, watch, onMounted, onUnmounted, type ComponentPublicInstance } from 'vue'
	import { useI18n } from 'vue-i18n'
	import { useAppStore } from '@/stores/app'
	import { useOnboardingStore } from '@/stores/onboarding'
	import { useClipboard } from '@/composables/useClipboard'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { identityApi, type IdentityKey, type IdentityKeyGroup } from '@/api/identity'
import { useAuthStore } from '@/stores/auth'
import { getConsoleScopeStore, type ScopeSelection } from '@/stores/consoleScope'
import ScopeGateEmptyState from '@/features/console/components/ScopeGateEmptyState.vue'
import {
  ensureProjectAccess,
  getAccessActionState,
  getIssuableKeyActions,
  recheckProjectActionBeforeMutation,
  type AccessProbeEntry,
  type PermissionProbeState
} from '@/features/console/scopeAccess'
import {
  resolveDefaultRoutingGroupId,
  SCOPE_CHANGED_EVENT as CONSOLE_SCOPE_CHANGED_EVENT
} from '@/features/console/scope'
import ConsoleAsyncBanner from '@/features/console/components/ConsoleAsyncBanner.vue'
import { classifyConsoleError, type ConsoleAsyncNotice } from '@/features/console/asyncState'

const { t, locale } = useI18n()
const isZh = computed(() => String(locale?.value || '').startsWith('zh'))
const instance = getCurrentInstance()
const consoleScopeStore = getConsoleScopeStore()
const authStore = useAuthStore()

type ScopedApiKeyRow = ApiKey & {
  isProjectRow?: boolean
  isOwnedByActor?: boolean
  rawIdentityKey?: IdentityKey
}

const currentUserId = computed(() =>
  authStore?.user?.id !== undefined && authStore?.user?.id !== null
    ? String(authStore.user.id)
    : ''
)
const activeConsoleScope = computed<ScopeSelection>(() => consoleScopeStore.activeScope)
const isPersonalMode = computed(() => activeConsoleScope.value.mode === 'personal')
const isWorkspaceOverview = computed(
  () =>
    activeConsoleScope.value.mode === 'workspace' &&
    Boolean(activeConsoleScope.value.workspaceId) &&
    !activeConsoleScope.value.projectId
)
const isProjectMode = computed(
  () =>
    activeConsoleScope.value.mode === 'workspace' &&
    Boolean(activeConsoleScope.value.workspaceId) &&
    Boolean(activeConsoleScope.value.projectId)
)
const isScopeUnavailable = computed(
  () =>
    activeConsoleScope.value.mode === 'workspace' &&
    consoleScopeStore.scopeValidationStatus === 'unavailable'
)

const projectAccessEntry = ref<AccessProbeEntry | null>(null)
const projectGroups = ref<IdentityKeyGroup[]>([])
const projectGroupRawIdByLocalId = ref<Map<number, string>>(new Map())
const canReadKeys = ref(true)
const canWriteKeys = ref(true)
const keysAccessNotice = ref<ConsoleAsyncNotice | null>(null)
const projectKeyAllowModelsRead = ref(false)
const projectModelAccessMode = ref<'inherit' | 'allow_list' | 'deny_all'>('inherit')
const projectModelAccessIdsText = ref('')
const modalScopePin = ref<{
  revision: number
  workspaceId: string | null
  projectId: string | null
  label: string
} | null>(null)
const createIdempotencyKey = ref('')
const lastCreatePayloadSignature = ref('')
const issuedSecretScopeLabel = ref('')

const issuableActionsResult = computed(() => {
  if (!isProjectMode.value) {
    return { status: 'allowed' as PermissionProbeState, actions: [] as string[] }
  }
  return getIssuableKeyActions(projectAccessEntry.value)
})
const availableProjectKeyBaseActions = computed<string[]>(() => {
  if (issuableActionsResult.value.status !== 'allowed') {
    return []
  }
  return issuableActionsResult.value.actions.filter((act) => act !== 'models.read')
})
const issuableSupportsModelsRead = computed(
  () =>
    issuableActionsResult.value.status === 'allowed' &&
    issuableActionsResult.value.actions.includes('models.read')
)
const selectedProjectKeyActions = computed<string[]>(() => {
  if (issuableActionsResult.value.status !== 'allowed') return []
  return projectKeyAllowModelsRead.value && issuableSupportsModelsRead.value
    ? [...availableProjectKeyBaseActions.value, 'models.read']
    : [...availableProjectKeyBaseActions.value]
})
const scopeGateState = computed<'empty' | 'denied' | 'unavailable' | null>(() => {
  if (isPersonalMode.value) return null
  if (isScopeUnavailable.value) return 'unavailable'
  if (isWorkspaceOverview.value) return 'empty'
  const entry = projectAccessEntry.value
  if (!entry) return null
  if (entry.status === 'unavailable') return 'unavailable'
  if (entry.status === 'denied') return 'denied'
  if (entry.status === 'ready' && !canReadKeys.value && !canWriteKeys.value) {
    return 'denied'
  }
  return null
})
const modalScopeMismatch = computed(() => {
  if (!modalScopePin.value || !showCreateModal.value || showEditModal.value) return false
  return (
    modalScopePin.value.revision !== consoleScopeStore.scopeRevision ||
    modalScopePin.value.workspaceId !== activeConsoleScope.value.workspaceId ||
    modalScopePin.value.projectId !== activeConsoleScope.value.projectId
  )
})
const lastIssuedProjectKeyScope = ref<{
  name: string
  actions: string[]
} | null>(null)

const scopedBillingHref = computed(() => {
  const ws = activeConsoleScope.value.workspaceId
  const proj = activeConsoleScope.value.projectId
  return ws && proj ? `/billing?workspace_id=${encodeURIComponent(ws)}&project_id=${encodeURIComponent(proj)}` : '/billing'
})

const scopedRequestsHref = computed(() => {
  const ws = activeConsoleScope.value.workspaceId
  const proj = activeConsoleScope.value.projectId
  return ws && proj ? `/requests?workspace_id=${encodeURIComponent(ws)}&project_id=${encodeURIComponent(proj)}` : '/requests'
})

function navigateConsole(path: string): void {
  const router = instance?.appContext.config.globalProperties.$router
  if (router && typeof router.push === 'function') {
    void router.push(path)
  } else if (typeof window !== 'undefined') {
    window.location.assign(path)
  }
}

function buildScopeLabel(wsId: string | null, projId: string | null): string {
  if (!wsId) return t('scope.personal.label')
  const wsName = consoleScopeStore.activeWorkspace?.name || `#${wsId}`
  if (!projId) return wsName
  const projName = consoleScopeStore.activeProject?.name || `#${projId}`
  return `${wsName} / ${projName}`
}

function generateIdempotencyToken(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return `key_${Date.now()}_${Math.random().toString(36).slice(2, 10)}`
}

function openCreateModal(): void {
  modalScopePin.value = {
    revision: consoleScopeStore.scopeRevision,
    workspaceId: activeConsoleScope.value.workspaceId,
    projectId: activeConsoleScope.value.projectId,
    label: buildScopeLabel(activeConsoleScope.value.workspaceId, activeConsoleScope.value.projectId)
  }
  createIdempotencyKey.value = generateIdempotencyToken()
  lastCreatePayloadSignature.value = ''
  showCreateModal.value = true
}

function getScopeMutationOptions(): { workspaceId?: string | null } | undefined {
  if (activeConsoleScope.value.mode === 'workspace' && activeConsoleScope.value.workspaceId) {
    return { workspaceId: activeConsoleScope.value.workspaceId }
  }
  return undefined
}

function canMutateRow(row: ScopedApiKeyRow): boolean {
  if (row.isProjectRow) {
    return Boolean(
      row.isOwnedByActor &&
        activeConsoleScope.value.mode === 'workspace' &&
        activeConsoleScope.value.workspaceId
    )
  }
  return canWriteKeys.value
}

const rotateKeyForScope = (
  id: number,
  options: { grace_ttl_hours?: number; revoke_previous?: boolean }
) => keysAPI.rotate(id, { ...options, ...getScopeMutationOptions() })

function handleSwitchToPersonalScope(): void {
  consoleScopeStore.selectPersonalScope()
}

function handleSelectProjectScope(payload: { workspaceId: string; projectId: string }): void {
  consoleScopeStore.selectWorkspaceScope(payload.workspaceId, payload.projectId)
}

async function handleRetryScopeOrAccess(): Promise<void> {
  if (isScopeUnavailable.value) {
    await consoleScopeStore.fetchScopes({ force: true })
  }
  await refreshKeysSurface(true)
}

async function refreshKeyAccess(force = false): Promise<{ canRead: boolean; canWrite: boolean }> {
  if (isPersonalMode.value) {
    projectAccessEntry.value = null
    canReadKeys.value = true
    canWriteKeys.value = true
    keysAccessNotice.value = null
    return { canRead: true, canWrite: true }
  }

  if (isWorkspaceOverview.value || isScopeUnavailable.value) {
    abortController?.abort()
    projectAccessEntry.value = null
    canReadKeys.value = false
    canWriteKeys.value = false
    keysAccessNotice.value = null
    apiKeys.value = []
    return { canRead: false, canWrite: false }
  }

  if (consoleScopeStore.untrustedPending && consoleScopeStore.scopeValidationStatus === 'validating') {
    await consoleScopeStore.fetchScopes({ force: false })
  }
  if (consoleScopeStore.untrustedPending && !consoleScopeStore.activeProject) {
    canReadKeys.value = false
    canWriteKeys.value = false
    apiKeys.value = []
    return { canRead: false, canWrite: false }
  }

  const ws = activeConsoleScope.value.workspaceId!
  const proj = activeConsoleScope.value.projectId!
  const entry = await ensureProjectAccess(ws, proj, { force })
  if (disposed) {
    return { canRead: false, canWrite: false }
  }
  projectAccessEntry.value = entry

  if (entry.status !== 'ready') {
    canReadKeys.value = false
    canWriteKeys.value = false
    apiKeys.value = []
    keysAccessNotice.value =
      entry.status === 'denied'
        ? {
            code: 403,
            kind: 'permission',
            title: t('rbac.keys.deniedProject', {
              grant: 'keys.read',
              project: consoleScopeStore.activeProject?.name || `#${proj}`
            }),
            message: entry.error?.message || '',
            requiredGrant: 'keys.read',
            requestId: entry.error?.requestId
          }
        : {
            code: entry.error?.status || 503,
            kind: 'error',
            title: entry.error?.requestId
              ? t('rbac.probe.unavailable', { reqId: entry.error.requestId })
              : t('rbac.probe.unavailableNoReq'),
            message: entry.error?.message || '',
            requestId: entry.error?.requestId
          }
    return { canRead: false, canWrite: false }
  }

  const canRead = getAccessActionState(entry, 'keys.read') === 'allowed'
  const canWrite = getAccessActionState(entry, 'keys.create') === 'allowed'
  canReadKeys.value = canRead
  canWriteKeys.value = canWrite

  if (!canRead && canWrite) {
    keysAccessNotice.value = {
      code: 403,
      kind: 'permission',
      title: t('rbac.keys.createOnlyHint'),
      message: '',
      requiredGrant: 'keys.read'
    }
    apiKeys.value = []
  } else if (!canRead) {
    keysAccessNotice.value = {
      code: 403,
      kind: 'permission',
      title: t('rbac.keys.deniedProject', {
        grant: 'keys.read',
        project: consoleScopeStore.activeProject?.name || `#${proj}`
      }),
      message: '',
      requiredGrant: 'keys.read'
    }
    apiKeys.value = []
  } else {
    keysAccessNotice.value = null
  }
  return { canRead, canWrite }
}

async function refreshKeysSurface(force = false): Promise<void> {
  const { canRead, canWrite } = await refreshKeyAccess(force)
  if (disposed) return
  if (canRead) {
    void loadApiKeys()
  }
  if (isPersonalMode.value || canRead || canWrite) {
    void loadGroups()
    if (isPersonalMode.value) {
      void loadUserGroupRates()
    }
  } else {
    groups.value = []
    projectGroups.value = []
  }
}

function handleConsoleScopeSync(): void {
  if (disposed) return
  void refreshKeysSurface(false)
}

watch(
  () => [
    activeConsoleScope.value.mode,
    activeConsoleScope.value.workspaceId,
    activeConsoleScope.value.projectId,
    consoleScopeStore.scopeValidationStatus
  ],
  () => {
    if (disposed) return
    void refreshKeysSurface(false)
  }
)
import { keysAPI, authAPI, usageAPI, userGroupsAPI } from '@/api'
import { buildGatewayUrl } from '@/api/url'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import BulkEditKeysModal from '@/components/keys/BulkEditKeysModal.vue'
	import DataTable from '@/components/common/DataTable.vue'
	import Pagination from '@/components/common/Pagination.vue'
	import BaseDialog from '@/components/common/BaseDialog.vue'
	import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
	import EmptyState from '@/components/common/EmptyState.vue'
	import Select from '@/components/common/Select.vue'
	import SearchInput from '@/components/common/SearchInput.vue'
	import Icon from '@/components/icons/Icon.vue'
	import UseKeyModal from '@/components/keys/UseKeyModal.vue'
	import KeyRotationDialog from '@/components/keys/KeyRotationDialog.vue'
	import KeySecretDialog from '@/components/keys/KeySecretDialog.vue'
	import EndpointPopover from '@/components/keys/EndpointPopover.vue'
	import GroupBadge from '@/components/common/GroupBadge.vue'
	import GroupOptionItem from '@/components/common/GroupOptionItem.vue'
	import type { ApiKey, ApiKeyEffectiveRPMLimit, ApiKeyPeriodBudgetRequest, Group, PublicSettings, SubscriptionType, GroupPlatform, UpdateApiKeyRequest } from '@/types'
import type { Column } from '@/components/common/types'
import type { BatchApiKeyUsageStats } from '@/api/usage'
import { formatDateTime } from '@/utils/format'
import { maskApiKey } from '@/utils/maskApiKey'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { platformBadgeLightClass } from '@/utils/platformColors'
import { KEY_GROUP_PROVIDERS, KEY_GROUP_PROVIDER_ICONS, getKeyGroupProvider, type KeyGroupProvider } from '@/utils/keyGroupProviders'
import {
  CC_SWITCH_USAGE_SCRIPT,
  buildCcSwitchImportDeeplink,
  type CcSwitchClientType
} from '@/utils/ccswitchImport'
import { keyVisibleModels } from '@/api/modelAccess'

// Helper to format date for datetime-local input
const formatDateTimeLocal = (isoDate: string): string => {
  const date = new Date(isoDate)
  const pad = (n: number) => n.toString().padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

interface GroupOption {
  value: number
  label: string
  description: string | null
  rate: number
  userRate: number | null
  peakRateEnabled: boolean
  peakStart: string
  peakEnd: string
  peakRateMultiplier: number
  subscriptionType: SubscriptionType
  platform: GroupPlatform
}

const appStore = useAppStore()
const onboardingStore = useOnboardingStore()
const { copyToClipboard: clipboardCopy } = useClipboard()
const copiedEndpoint = ref('')
let endpointCopyRevision = 0
let endpointCopyTimer: ReturnType<typeof setTimeout> | undefined
const copyEndpoint = async (url: string) => {
  const revision = ++endpointCopyRevision
  const copied = await clipboardCopy(url, t('keys.payAsYouGo.copied'))
  if (!copied || revision !== endpointCopyRevision) return
  copiedEndpoint.value = url
  clearTimeout(endpointCopyTimer)
  endpointCopyTimer = setTimeout(() => { copiedEndpoint.value = '' }, 2000)
}

const gatewayEndpoints = [
  { protocol: 'openai', label: 'keys.payAsYouGo.openai', path: '/v1' },
  { protocol: 'anthropic', label: 'keys.payAsYouGo.anthropic', path: '/v1/messages' },
  { protocol: 'gemini', label: 'keys.payAsYouGo.gemini', path: '/v1beta' }
].map((endpoint) => ({
  ...endpoint,
  url: new URL(buildGatewayUrl(endpoint.path), window.location.origin).href
}))

// Copyable generation entries. Availability still depends on the key's group
// platform and, for images, the group image-generation switch; the list never
// claims they are enabled for this key.
const generationEndpoints = [
  { label: 'keys.payAsYouGo.imageGeneration', path: '/v1/images/generations' },
  { label: 'keys.payAsYouGo.imageEdit', path: '/v1/images/edits' },
  { label: 'keys.payAsYouGo.videoGeneration', path: '/v1/videos/generations' },
  { label: 'keys.payAsYouGo.speech', path: '/v1/tts' },
  { label: 'keys.payAsYouGo.transcription', path: '/v1/stt' },
  { label: 'keys.payAsYouGo.embeddings', path: '/v1/embeddings' },
  { label: 'keys.payAsYouGo.rerank', path: '/v1/rerank' }
].map((endpoint) => ({
  ...endpoint,
  url: new URL(buildGatewayUrl(endpoint.path), window.location.origin).href
}))

const allColumns = computed<Column[]>(() => [
  { key: 'name', label: t('common.name'), sortable: true },
  { key: 'id', label: t('keys.id'), sortable: true },
  { key: 'key', label: t('keys.apiKey'), sortable: false },
  { key: 'group', label: t('keys.group'), sortable: false },
  { key: 'current_concurrency', label: t('keys.currentConcurrency'), sortable: true },
  { key: 'usage', label: t('keys.usage'), sortable: false },
  { key: 'rate_limit', label: t('keys.rateLimitColumn'), sortable: false },
  { key: 'expires_at', label: t('keys.expiresAt'), sortable: true },
  { key: 'status', label: t('common.status'), sortable: true },
  { key: 'last_used_at', label: t('keys.lastUsedAt'), sortable: true },
  { key: 'last_used_ip', label: t('keys.lastUsedIP'), sortable: false },
  { key: 'created_at', label: t('keys.created'), sortable: true },
  { key: 'actions', label: t('common.actions'), sortable: false }
])

const ALWAYS_VISIBLE_COLUMNS = new Set(['name', 'actions'])
const DEFAULT_HIDDEN_COLUMNS = ['id', 'rate_limit', 'last_used_at', 'last_used_ip']
const HIDDEN_COLUMNS_KEY = 'api-key-hidden-columns'
const V1_HIDDEN_COLUMNS_KEY = 'omnihive.keys.hidden_columns.v1'
const COLUMN_SETTINGS_VERSION_KEY = 'api-key-column-settings-version'
const V1_COLUMN_SETTINGS_VERSION_KEY = 'omnihive.keys.column_version.v1'
const COLUMN_SETTINGS_VERSION = 3
const VERSION_NEW_HIDDEN_COLUMNS: Record<number, string[]> = {
  2: ['last_used_ip'],
  3: ['id']
}

const toggleableColumns = computed(() =>
  allColumns.value.filter((col) => !ALWAYS_VISIBLE_COLUMNS.has(col.key))
)

const hiddenColumns = reactive<Set<string>>(new Set())

const saveColumnsToStorage = () => {
  try {
    const serialized = JSON.stringify([...hiddenColumns])
    localStorage.setItem(V1_HIDDEN_COLUMNS_KEY, serialized)
    localStorage.setItem(HIDDEN_COLUMNS_KEY, serialized)
    localStorage.setItem(V1_COLUMN_SETTINGS_VERSION_KEY, String(COLUMN_SETTINGS_VERSION))
    localStorage.setItem(COLUMN_SETTINGS_VERSION_KEY, String(COLUMN_SETTINGS_VERSION))
  } catch (error) {
    console.error('Failed to save API key table columns:', error)
  }
}

const loadSavedColumns = () => {
  hiddenColumns.clear()
  try {
    const saved = localStorage.getItem(V1_HIDDEN_COLUMNS_KEY) ?? localStorage.getItem(HIDDEN_COLUMNS_KEY)
    if (saved) {
      const parsed = JSON.parse(saved) as string[]
      const validColumnKeys = new Set(allColumns.value.map((col) => col.key))
      parsed
        .filter((key) =>
          typeof key === 'string' &&
          validColumnKeys.has(key) &&
          !ALWAYS_VISIBLE_COLUMNS.has(key)
        )
        .forEach((key) => hiddenColumns.add(key))
      const storedVersion = Number(
        localStorage.getItem(V1_COLUMN_SETTINGS_VERSION_KEY) ??
        localStorage.getItem(COLUMN_SETTINGS_VERSION_KEY) ??
        '1'
      )
      if (storedVersion < COLUMN_SETTINGS_VERSION) {
        for (let v = storedVersion + 1; v <= COLUMN_SETTINGS_VERSION; v++) {
          for (const key of VERSION_NEW_HIDDEN_COLUMNS[v] ?? []) {
            if (validColumnKeys.has(key) && !ALWAYS_VISIBLE_COLUMNS.has(key)) {
              hiddenColumns.add(key)
            }
          }
        }
        saveColumnsToStorage()
      } else {
        localStorage.setItem(V1_COLUMN_SETTINGS_VERSION_KEY, String(COLUMN_SETTINGS_VERSION))
        localStorage.setItem(COLUMN_SETTINGS_VERSION_KEY, String(COLUMN_SETTINGS_VERSION))
      }
    } else {
      DEFAULT_HIDDEN_COLUMNS.forEach((key) => hiddenColumns.add(key))
      localStorage.setItem(V1_COLUMN_SETTINGS_VERSION_KEY, String(COLUMN_SETTINGS_VERSION))
      localStorage.setItem(COLUMN_SETTINGS_VERSION_KEY, String(COLUMN_SETTINGS_VERSION))
    }
  } catch (error) {
    console.error('Failed to load API key table columns:', error)
    DEFAULT_HIDDEN_COLUMNS.forEach((key) => hiddenColumns.add(key))
  }
}

const toggleColumn = (key: string) => {
  if (ALWAYS_VISIBLE_COLUMNS.has(key)) return
  if (hiddenColumns.has(key)) {
    hiddenColumns.delete(key)
  } else {
    hiddenColumns.add(key)
  }
  saveColumnsToStorage()
}

const isColumnVisible = (key: string) => !hiddenColumns.has(key)

const columns = computed<Column[]>(() =>
  allColumns.value.filter((col) => ALWAYS_VISIBLE_COLUMNS.has(col.key) || !hiddenColumns.has(col.key))
)

const apiKeys = ref<ScopedApiKeyRow[]>([])
const selectedIds = ref<number[]>([])
const showBulkEditModal = ref(false)
const selectedApiKeys = computed(() =>
  apiKeys.value.filter(
    (key) => selectedIds.value.includes(key.id) && canMutateRow(key)
  )
)

const handleSelectionChange = (ids: Array<string | number>) => {
  const visibleIds = new Set(
    apiKeys.value.filter((key) => canMutateRow(key)).map((key) => key.id)
  )
  selectedIds.value = [...new Set(ids.map(Number))].filter((id) => visibleIds.has(id))
}

const handleBulkUpdated = (succeededIds: number[]) => {
  const succeeded = new Set(succeededIds)
  selectedIds.value = selectedIds.value.filter((id) => !succeeded.has(id))
  loadApiKeys()
}

const groups = ref<Group[]>([])
const groupsLoading = ref(true)
const groupsLoadFailed = ref(false)
const loading = ref(false)
const submitting = ref(false)
const now = ref(new Date())
let resetTimer: ReturnType<typeof setInterval> | null = null
const usageStats = ref<Record<string, BatchApiKeyUsageStats>>({})
const usageState = ref<'loading' | 'known' | 'unknown' | 'error'>('loading')
const usageHistoryGap = ref<HistoryGap | null>(null)
const userGroupRates = ref<Record<number, number>>({})

const pagination = ref({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
  pages: 0
})
const sortState = ref({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc'
})

// Filter state
const filterSearch = ref('')
const filterStatus = ref('')
const filterGroupId = ref<string | number>('')
const selectedKeyIds = reactive<Set<number>>(new Set())
const bulkUpdating = ref(false)

const selectedKeyCount = computed(() => selectedKeyIds.size)
const activeKeyCount = computed(() => apiKeys.value.filter((k) => k.status === 'active').length)
const allVisibleKeysSelected = computed(
  () => apiKeys.value.length > 0 && apiKeys.value.every((k) => selectedKeyIds.has(k.id))
)

const toggleKeySelection = (keyId: number, checked: boolean) => {
  if (checked) selectedKeyIds.add(keyId)
  else selectedKeyIds.delete(keyId)
}

const toggleAllVisibleKeys = (checked: boolean) => {
  for (const key of apiKeys.value) {
    if (checked) selectedKeyIds.add(key.id)
    else selectedKeyIds.delete(key.id)
  }
}

const clearSelectedKeys = () => {
  selectedKeyIds.clear()
}

const bulkToggleSelectedKeys = async (targetStatus: 'active' | 'inactive') => {
  const ids = [...selectedKeyIds].filter((id) => {
    const row = apiKeys.value.find((item) => item.id === id)
    return row ? canMutateRow(row) : false
  })
  if (ids.length === 0 || bulkUpdating.value) return
  bulkUpdating.value = true
  try {
    const opts = getScopeMutationOptions()
    await Promise.all(ids.map((id) => keysAPI.toggleStatus(id, targetStatus, opts)))
    appStore.showSuccess(
      targetStatus === 'active' ? t('keys.keyEnabledSuccess') : t('keys.keyDisabledSuccess')
    )
    selectedKeyIds.clear()
    await loadApiKeys()
  } catch {
    appStore.showError(t('keys.failedToUpdateStatus'))
  } finally {
    bulkUpdating.value = false
  }
}

const showCreateModal = ref(false)
const showEditModal = ref(false)
const showDeleteDialog = ref(false)
const showUseKeyModal = ref(false)
const defaultGatewayOrigin = typeof window !== 'undefined' ? window.location.origin : ''
const showRotateModal = ref(false)
const rotateTarget = ref<ApiKey | null>(null)
const showSecretDialog = ref(false)
const issuedSecret = ref('')
const issuedGraceUntil = ref<string | null>(null)
const showCcsClientSelect = ref(false)
const showColumnDropdown = ref(false)
const pendingCcsRow = ref<ApiKey | null>(null)
// Plaintext credentials exist only in this session: the panel can no longer read
// them back after creation or rotation, so config exports may only use them while
// they are still held here.
const sessionSecrets = new Map<number, string>()
const selectedKey = ref<ApiKey | null>(null)
const groupSelectorKeyId = ref<number | null>(null)
const publicSettings = ref<PublicSettings | null>(null)
const dropdownRef = ref<HTMLElement | null>(null)
const columnDropdownRef = ref<HTMLElement | null>(null)
const dropdownPosition = ref<{ top?: number; bottom?: number; left: number } | null>(null)
const groupButtonRefs = ref<Map<number, HTMLElement>>(new Map())
let abortController: AbortController | null = null
const auxiliaryControllers = new Map<string, AbortController>()
let disposed = false

// Get the currently selected key for group change
const selectedKeyForGroup = computed(() => {
  if (groupSelectorKeyId.value === null) return null
  return apiKeys.value.find((k) => k.id === groupSelectorKeyId.value) || null
})

const setGroupButtonRef = (keyId: number, el: Element | ComponentPublicInstance | null) => {
  if (el instanceof HTMLElement) {
    groupButtonRefs.value.set(keyId, el)
  } else {
    groupButtonRefs.value.delete(keyId)
  }
}

const formData = ref({
  name: '',
  group_id: null as number | null,
  status: 'active' as 'active' | 'inactive',
  use_custom_key: false,
  custom_key: '',
  enable_ip_restriction: false,
  ip_whitelist: '',
  ip_blacklist: '',
  // Quota settings (empty = unlimited)
  enable_quota: false,
  quota: null as number | null,
  // Rate limit settings
  enable_rate_limit: false,
  rate_limit_5h: null as number | null,
  rate_limit_1d: null as number | null,
  rate_limit_7d: null as number | null,
  // Period budget (USD): one explicit period, separate from rate limits.
  enable_budget: false,
  budget_limit: null as number | null,
  budget_period_kind: 'legacy_5h' as
    | 'legacy_5h'
    | 'legacy_1d'
    | 'legacy_7d'
    | 'duration'
    | 'calendar_day'
    | 'calendar_week'
    | 'calendar_month',
  budget_duration_value: 12 as number | null,
  budget_duration_unit: 'hours' as 'minutes' | 'hours' | 'days',
  budget_timezone: 'UTC',
  budget_enforced: false,
  enable_expiration: false,
  expiration_preset: '30' as '7' | '30' | '90' | 'custom',
  expiration_date: ''
})

// 自定义Key验证
const customKeyError = computed(() => {
  if (!formData.value.use_custom_key || !formData.value.custom_key) {
    return ''
  }
  const key = formData.value.custom_key
  if (key.length < 16) {
    return t('keys.customKeyTooShort')
  }
  // 检查字符：只允许字母、数字、下划线、连字符
  if (!/^[a-zA-Z0-9_-]+$/.test(key)) {
    return t('keys.customKeyInvalidChars')
  }
  return ''
})

const statusOptions = computed(() => [
  { value: 'active', label: t('common.active') },
  { value: 'inactive', label: t('common.inactive') }
])

// 管理员（或风控）停用的密钥是管理侧决定：面板不能把它改回 active，
// 状态控件直接锁死，编辑其它字段时也不提交 status。
const statusLockedByAdmin = computed(() =>
  showEditModal.value && selectedKey.value?.status === 'disabled'
)

const shouldSubmitEditStatus = (key: ApiKey, status: 'active' | 'inactive') => {
  if (key.status === 'disabled') {
    return false
  }
  if (key.status === 'quota_exhausted' || key.status === 'expired') {
    return status === 'active'
  }
  return true
}

// Filter dropdown options
const groupFilterOptions = computed(() => [
  { value: '', label: t('keys.allGroups') },
  { value: 0, label: t('keys.noGroup') },
  ...groups.value.map((g) => ({ value: g.id, label: g.name }))
])

const statusFilterOptions = computed(() => [
  { value: '', label: t('keys.allStatus') },
  { value: 'active', label: t('keys.status.active') },
  { value: 'inactive', label: t('keys.status.inactive') },
  { value: 'disabled', label: t('keys.status.disabled') },
  { value: 'quota_exhausted', label: t('keys.status.quota_exhausted') },
  { value: 'expired', label: t('keys.status.expired') }
])

const onFilterChange = () => {
  selectedIds.value = []
  pagination.value.page = 1
  loadApiKeys()
}

const onGroupFilterChange = (value: string | number | boolean | null) => {
  filterGroupId.value = value as string | number
  onFilterChange()
}

const onStatusFilterChange = (value: string | number | boolean | null) => {
  filterStatus.value = value as string
  onFilterChange()
}

// Convert groups to Select options format with rate multiplier and subscription type
const groupOptions = computed(() =>
  groups.value.map((group) => ({
    value: group.id,
    label: group.name,
    description: group.description,
    rate: group.rate_multiplier,
    userRate: (userGroupRates.value ?? {})[group.id] ?? null,
    peakRateEnabled: group.peak_rate_enabled,
    peakStart: group.peak_start,
    peakEnd: group.peak_end,
    peakRateMultiplier: group.peak_rate_multiplier,
    subscriptionType: group.subscription_type,
    platform: group.platform
  }))
)

const createProvider = ref<KeyGroupProvider>('anthropic')
const createProviderOptions = computed(() => KEY_GROUP_PROVIDERS.map((value) => ({
  value,
  label: t(`keys.providers.${value}`),
  count: groups.value.filter((group) => getKeyGroupProvider(group.platform) === value).length
})))

const formGroupOptions = computed(() => showEditModal.value
  ? groupOptions.value
  : groupOptions.value.filter((group) => getKeyGroupProvider(group.platform) === createProvider.value)
)

const recommendedDefaultGroup = computed<GroupOption | null>(() => {
  const candidates = formGroupOptions.value.length > 0 ? formGroupOptions.value : groupOptions.value
  if (candidates.length === 0) return null
  const defaultId = resolveDefaultRoutingGroupId(
    candidates.map((item) => ({ id: item.value, subscription_type: item.subscriptionType }))
  )
  return (
    (defaultId !== null
      ? candidates.find((item) => String(item.value) === String(defaultId))
      : undefined) ??
    candidates[0] ??
    null
  )
})

const resolvedProjectGroupId = computed<number | string | null>(() => {
  if (
    formData.value.group_id !== null &&
    groups.value.some((group) => String(group.id) === String(formData.value.group_id))
  ) {
    return projectGroupRawIdByLocalId.value.get(formData.value.group_id) ?? formData.value.group_id
  }
  const recommended = recommendedDefaultGroup.value?.value ?? null
  if (recommended !== null && groups.value.some((group) => String(group.id) === String(recommended))) {
    return projectGroupRawIdByLocalId.value.get(recommended) ?? recommended
  }
  const fallback = resolveDefaultRoutingGroupId(groups.value)
  if (fallback !== null) {
    const matched = groups.value.find((group) => String(group.id) === String(fallback))
    if (matched) return projectGroupRawIdByLocalId.value.get(matched.id) ?? matched.id
  }
  return null
})

const selectedFormGroupLabel = computed(() => {
  if (!formData.value.group_id) return ''
  return (
    groupOptions.value.find((item) => item.value === formData.value.group_id)?.label ??
    `#${formData.value.group_id}`
  )
})

const applyRecommendedGroup = () => {
  const target = recommendedDefaultGroup.value
  if (!target) return
  const targetProvider = getKeyGroupProvider(target.platform)
  if (createProvider.value !== targetProvider) {
    createProvider.value = targetProvider
  }
  formData.value.group_id = target.value
}

const selectCreateProvider = (provider: KeyGroupProvider) => {
  if (createProvider.value === provider) return
  createProvider.value = provider
  formData.value.group_id = null
}

// Also handles groups arriving after the create dialog has already opened.
watch([showCreateModal, createProviderOptions], ([isOpen, providers], [wasOpen]) => {
  if (!isOpen) return
  if (!wasOpen || !providers.some((provider) => provider.value === createProvider.value && provider.count > 0)) {
    selectCreateProvider(providers.find((provider) => provider.count > 0)?.value ?? 'anthropic')
  }
  if (!formGroupOptions.value.some((group) => group.value === formData.value.group_id)) {
    formData.value.group_id = null
  }
  if (groups.value.length === 1 && formData.value.group_id === null) {
    formData.value.group_id = groups.value[0]!.id
  }
})

// Group dropdown search
const groupSearchQuery = ref('')
const filteredGroupOptions = computed(() => {
  const query = groupSearchQuery.value.trim().toLowerCase()
  if (!query) return groupOptions.value
  return groupOptions.value.filter((opt) => {
    return opt.label.toLowerCase().includes(query) ||
      (opt.description && opt.description.toLowerCase().includes(query))
  })
})

const isAbortError = (error: unknown) => {
  if (!error || typeof error !== 'object') return false
  const { name, code } = error as { name?: string; code?: string }
  return name === 'AbortError' || code === 'ERR_CANCELED' || code === 'AUTH_SESSION_CHANGED'
}

function normalizeProjectRowStatus(pk: IdentityKey): ApiKey['status'] {
  const raw = String(pk.state || pk.status || '').trim().toLowerCase()
  if (raw === 'active') return 'active'
  if (raw === 'disabled' || raw === 'revoked') return 'disabled'
  if (raw === 'expired') return 'expired'
  if (raw === 'quota_exhausted') return 'quota_exhausted'
  if (raw === 'inactive') return 'inactive'
  return (raw || 'inactive') as ApiKey['status']
}

function mapIdentityKeyToRow(pk: IdentityKey): ScopedApiKeyRow {
  const matchedProjGroup = projectGroups.value.find((g) => String(g.id) === String(pk.group_id))
  const matchedLegacyGroup = groups.value.find(
    (g) => String(projectGroupRawIdByLocalId.value.get(g.id) ?? g.id) === String(pk.group_id)
  )
  const ownerIdStr = pk.owner_id !== undefined && pk.owner_id !== null ? String(pk.owner_id) : ''
  const numericGroupId = matchedLegacyGroup?.id ?? (pk.group_id && /^\d+$/.test(String(pk.group_id)) ? Number(pk.group_id) : null)
  const groupInfo: Group | null = matchedLegacyGroup
    ? matchedLegacyGroup
    : matchedProjGroup
      ? ({
          id: numericGroupId ?? 0,
          name: matchedProjGroup.name,
          platform: (matchedProjGroup.platform || 'openai') as GroupPlatform,
          rate_multiplier: matchedProjGroup.rate_multiplier ?? 1,
          subscription_type: (matchedProjGroup.subscription_type || 'standard') as SubscriptionType
        } as Group)
      : null

  return {
    id: Number(pk.id) || 0,
    user_id: Number(pk.owner_id) || 0,
    name: pk.name || pk.prefix,
    key: pk.prefix,
    group_id: numericGroupId,
    group: groupInfo,
    status: normalizeProjectRowStatus(pk),
    ip_whitelist: [],
    ip_blacklist: [],
    quota: 0,
    quota_used: 0,
    expires_at: pk.expires_at ?? null,
    rate_limit_5h: 0,
    rate_limit_1d: 0,
    rate_limit_7d: 0,
    usage_5h: 0,
    usage_1d: 0,
    usage_7d: 0,
    reset_5h_at: null,
    reset_1d_at: null,
    reset_7d_at: null,
    budget_limit: 0,
    budget_used: 0,
    budget_period_mode: 'off',
    budget_enforced: false,
    created_at: pk.created_at || '',
    updated_at: pk.created_at || '',
    isProjectRow: true,
    isOwnedByActor: Boolean(ownerIdStr && currentUserId.value && ownerIdStr === currentUserId.value),
    rawIdentityKey: pk
  } as unknown as ScopedApiKeyRow
}

const loadApiKeys = async () => {
  if (disposed) return
  if (!canReadKeys.value || scopeGateState.value !== null) {
    loading.value = false
    return
  }
  abortController?.abort()
  const controller = new AbortController()
  abortController = controller
  const { signal } = controller
  const isCurrentRequest = () => !disposed && !signal.aborted && abortController === controller
  loading.value = true
  usageStats.value = {}
  usageHistoryGap.value = null
  usageState.value = 'loading'
  try {
    if (isProjectMode.value && activeConsoleScope.value.workspaceId && activeConsoleScope.value.projectId) {
      const page = await identityApi.listProjectKeys(
        activeConsoleScope.value.workspaceId,
        activeConsoleScope.value.projectId,
        { limit: 100 }
      )
      if (!isCurrentRequest()) return
      let rows = (page?.items ?? []).map(mapIdentityKeyToRow)
      if (filterSearch.value.trim()) {
        const q = filterSearch.value.trim().toLowerCase()
        rows = rows.filter(
          (row) =>
            row.name.toLowerCase().includes(q) ||
            row.key.toLowerCase().includes(q) ||
            String(row.id).toLowerCase().includes(q)
        )
      }
      if (filterStatus.value) {
        const target = filterStatus.value.toLowerCase()
        rows = rows.filter((row) => String(row.status).toLowerCase() === target)
      }
      if (filterGroupId.value !== '') {
        const targetGroup = String(
          projectGroupRawIdByLocalId.value.get(Number(filterGroupId.value)) ?? filterGroupId.value
        )
        rows = rows.filter(
          (row) =>
            row.rawIdentityKey?.group_id !== undefined &&
            row.rawIdentityKey?.group_id !== null &&
            String(row.rawIdentityKey.group_id) === targetGroup
        )
      }
      apiKeys.value = rows
      handleSelectionChange(selectedIds.value)
      pagination.value.total = rows.length
      pagination.value.pages = 1
      usageStats.value = {}
      usageState.value = 'known'
      return
    }

    // Build filters
    const filters: {
      search?: string
      status?: string
      group_id?: number | string
      sort_by?: string
      sort_order?: 'asc' | 'desc'
    } = {}
    if (filterSearch.value) filters.search = filterSearch.value
    if (filterStatus.value) filters.status = filterStatus.value
    if (filterGroupId.value !== '') filters.group_id = filterGroupId.value
    filters.sort_by = sortState.value.sort_by
    filters.sort_order = sortState.value.sort_order

    const response = await keysAPI.list(pagination.value.page, pagination.value.page_size, filters, {
      signal
    })
    if (!isCurrentRequest()) return
    usageStats.value = {}
    apiKeys.value = response.items
    handleSelectionChange(selectedIds.value)
    pagination.value.total = response.total
    pagination.value.pages = response.pages

    // Load usage stats for all API keys in the list
    if (response.items.length > 0) {
      const keyIds = response.items.map((k) => k.id)
      try {
        const usageResponse = await usageAPI.getDashboardApiKeysUsage(keyIds, { signal })
        if (!isCurrentRequest()) return
        usageStats.value = usageResponse.stats
        usageState.value = 'known'
      } catch (e) {
        if (isCurrentRequest() && !isAbortError(e)) {
          usageStats.value = {}
          usageHistoryGap.value = historyGapFromError(e)
          usageState.value = usageHistoryGap.value ? 'unknown' : 'error'
          if (!usageHistoryGap.value) console.error('Failed to load usage stats:', e)
        }
      }
    }
  } catch (error) {
    if (!isCurrentRequest() || isAbortError(error)) {
      return
    }
    const status = (error as { status?: number; response?: { status?: number } })?.status ??
      (error as { response?: { status?: number } })?.response?.status
    if (status === 401 || status === 403 || status === 404 || status === 503) {
      canReadKeys.value = false
      if (status !== 403 || !canWriteKeys.value) {
        canWriteKeys.value = false
      }
      keysAccessNotice.value = classifyConsoleError(error, {
        requiredGrantFallback: 'keys.read',
        isZh: isZh.value
      })
      apiKeys.value = []
      pagination.value.total = 0
      pagination.value.pages = 0
      return
    }
    appStore.showError(t('keys.failedToLoad'))
  } finally {
    if (isCurrentRequest()) {
      loading.value = false
    }
  }
}

async function loadAuxiliary<Result>(
  name: string,
  request: (options: { signal: AbortSignal }) => Promise<Result>,
  apply: (result: Result) => void
) {
  if (disposed) return
  auxiliaryControllers.get(name)?.abort()
  const controller = new AbortController()
  auxiliaryControllers.set(name, controller)
  const isCurrent = () => !disposed && !controller.signal.aborted && auxiliaryControllers.get(name) === controller
  try {
    const result = await request({ signal: controller.signal })
    if (isCurrent()) apply(result)
  } catch (error) {
    if (isCurrent() && !isAbortError(error)) console.error(`Failed to load ${name}:`, error)
  } finally {
    if (auxiliaryControllers.get(name) === controller) auxiliaryControllers.delete(name)
  }
}

const loadGroups = async () => {
  groupsLoading.value = true
  groupsLoadFailed.value = false
  await loadAuxiliary('groups', async options => {
    try {
      if (
        isProjectMode.value &&
        !showEditModal.value &&
        activeConsoleScope.value.workspaceId &&
        activeConsoleScope.value.projectId
      ) {
        const page = await identityApi.listProjectKeyGroups(
          activeConsoleScope.value.workspaceId,
          activeConsoleScope.value.projectId,
          { limit: 100 }
        )
        const rawGroups = page?.items ?? []
        projectGroups.value = rawGroups
        const idMap = new Map<number, string>()
        const mapped = rawGroups.map((pg, idx) => {
          const localId = /^\d+$/.test(String(pg.id)) ? Number(pg.id) : -(idx + 1)
          idMap.set(localId, String(pg.id))
          return {
            id: localId,
            name: pg.name,
            description: pg.description ?? null,
            platform: (pg.platform || 'openai') as GroupPlatform,
            rate_multiplier: pg.rate_multiplier ?? 1,
            is_exclusive: false,
            status: (pg.status === 'inactive' ? 'inactive' : 'active') as Group['status'],
            subscription_type: (pg.subscription_type || 'standard') as SubscriptionType,
            daily_limit_usd: null,
            weekly_limit_usd: null,
            monthly_limit_usd: null,
            image_price_1k: null,
            image_price_2k: null,
            image_price_4k: null,
            claude_code_only: false,
            allow_messages_dispatch: false,
            default_mapped_model: '',
            created_at: '',
            updated_at: ''
          } as Group
        })
        projectGroupRawIdByLocalId.value = idMap
        return mapped
      }
      if (!isPersonalMode.value && !showEditModal.value) {
        projectGroups.value = []
        projectGroupRawIdByLocalId.value = new Map()
        return []
      }
      projectGroups.value = []
      projectGroupRawIdByLocalId.value = new Map()
      const result = await userGroupsAPI.getAvailable(options)
      return result
    } catch (error) {
      if (!options.signal.aborted && !disposed) groupsLoadFailed.value = true
      throw error
    } finally {
      if (!options.signal.aborted && !disposed) groupsLoading.value = false
    }
  }, result => {
    const list = Array.isArray(result) ? result : []
    groups.value = list
    if (showCreateModal.value) {
      if (!list.some(group => group.id === formData.value.group_id)) formData.value.group_id = null
      if (list.length === 1) formData.value.group_id = list[0]!.id
    }
  })
}
watch(
  [
    showCreateModal,
    () => activeConsoleScope.value.mode,
    () => activeConsoleScope.value.workspaceId,
    () => activeConsoleScope.value.projectId
  ],
  ([isOpen, mode, wsId, projId], [wasOpen, prevMode, prevWsId, prevProjId]) => {
    if (isOpen && !wasOpen && !showEditModal.value && !modalScopePin.value) {
      modalScopePin.value = {
        revision: consoleScopeStore.scopeRevision,
        workspaceId: wsId ?? null,
        projectId: projId ?? null,
        label: buildScopeLabel(wsId ?? null, projId ?? null)
      }
      createIdempotencyKey.value = generateIdempotencyToken()
      lastCreatePayloadSignature.value = ''
    }
    const scopeChanged = mode !== prevMode || wsId !== prevWsId || projId !== prevProjId
    if (scopeChanged) {
      groups.value = []
      projectGroups.value = []
      userGroupRates.value = {}
      formData.value.group_id = null
    }
    if (!isOpen || showEditModal.value) return
    if (!wasOpen || scopeChanged) {
      void loadGroups()
      if (mode === 'personal') {
        void loadUserGroupRates()
      }
    }
  }
)
const loadUserGroupRates = () => {
  if (!isPersonalMode.value) {
    userGroupRates.value = {}
    return Promise.resolve()
  }
  return loadAuxiliary(
    'group rates',
    options => userGroupsAPI.getUserGroupRates(options),
    result => {
      userGroupRates.value = result ?? {}
    }
  )
}
const loadPublicSettings = () => loadAuxiliary('public settings', authAPI.getPublicSettings, result => { publicSettings.value = result })

const openUseKeyModal = (key: ApiKey) => {
  selectedKey.value = key
  showUseKeyModal.value = true
}

const closeUseKeyModal = () => {
  showUseKeyModal.value = false
  selectedKey.value = null
}

const handlePageChange = (page: number) => {
  selectedIds.value = []
  pagination.value.page = page
  loadApiKeys()
}

const handlePageSizeChange = (pageSize: number) => {
  selectedIds.value = []
  pagination.value.page_size = pageSize
  pagination.value.page = 1
  loadApiKeys()
}

const handleSort = (key: string, order: 'asc' | 'desc') => {
  selectedIds.value = []
  sortState.value.sort_by = key
  sortState.value.sort_order = order
  pagination.value.page = 1
  loadApiKeys()
}

const editKey = (key: ApiKey) => {
  selectedKey.value = key
  const hasIPRestriction = (key.ip_whitelist?.length > 0) || (key.ip_blacklist?.length > 0)
  const hasExpiration = !!key.expires_at
  formData.value = {
    name: key.name,
    group_id: key.group_id,
    status: key.status === 'quota_exhausted' || key.status === 'expired' || key.status === 'disabled' ? 'inactive' : key.status,
    use_custom_key: false,
    custom_key: '',
    enable_ip_restriction: hasIPRestriction,
    ip_whitelist: (key.ip_whitelist || []).join('\n'),
    ip_blacklist: (key.ip_blacklist || []).join('\n'),
    enable_quota: key.quota > 0,
    quota: key.quota > 0 ? key.quota : null,
    enable_rate_limit: (key.rate_limit_5h > 0) || (key.rate_limit_1d > 0) || (key.rate_limit_7d > 0),
    rate_limit_5h: key.rate_limit_5h || null,
    rate_limit_1d: key.rate_limit_1d || null,
    rate_limit_7d: key.rate_limit_7d || null,
    enable_budget: (key.budget_period_mode || 'off') !== 'off' && key.budget_limit > 0,
    budget_limit: key.budget_limit > 0 ? key.budget_limit : null,
    budget_period_kind: budgetPeriodKindOf(key),
    budget_duration_value: budgetDurationValueOf(key),
    budget_duration_unit: budgetDurationUnitOf(key),
    budget_timezone: key.budget_timezone || 'UTC',
    budget_enforced: !!key.budget_enforced,
    enable_expiration: hasExpiration,
    expiration_preset: 'custom',
    expiration_date: key.expires_at ? formatDateTimeLocal(key.expires_at) : ''
  }
  showEditModal.value = true
}

const toggleKeyStatus = async (key: ScopedApiKeyRow) => {
  if (!canMutateRow(key)) return
  const newStatus = key.status === 'active' ? 'inactive' : 'active'
  try {
    await keysAPI.toggleStatus(key.id, newStatus, getScopeMutationOptions())
    appStore.showSuccess(
      newStatus === 'active' ? t('keys.keyEnabledSuccess') : t('keys.keyDisabledSuccess')
    )
    loadApiKeys()
  } catch (error) {
    appStore.showError(t('keys.failedToUpdateStatus'))
  }
}

const openGroupSelector = (key: ApiKey) => {
  if (groupSelectorKeyId.value === key.id) {
    groupSelectorKeyId.value = null
    dropdownPosition.value = null
  } else {
    const buttonEl = groupButtonRefs.value.get(key.id)
    if (buttonEl) {
      const rect = buttonEl.getBoundingClientRect()
      const dropdownEstHeight = 400 // estimated max dropdown height
      const dropdownEstWidth = Math.min(380, window.innerWidth - 16)
      const spaceBelow = window.innerHeight - rect.bottom
      const spaceAbove = rect.top
      // 夹取 left，避免窄屏下浮层超出视口右缘
      const left = Math.max(8, Math.min(rect.left, window.innerWidth - dropdownEstWidth - 8))

      if (spaceBelow < dropdownEstHeight && spaceAbove > spaceBelow) {
        // Not enough space below, pop upward
        dropdownPosition.value = {
          bottom: window.innerHeight - rect.top + 4,
          left
        }
      } else {
        // Default: pop downward
        dropdownPosition.value = {
          top: rect.bottom + 4,
          left
        }
      }
    }
    groupSelectorKeyId.value = key.id
    groupSearchQuery.value = ''
  }
}

const changeGroup = async (key: ScopedApiKeyRow, newGroupId: number | null) => {
  if (!canMutateRow(key)) return
  groupSelectorKeyId.value = null
  dropdownPosition.value = null
  if (key.group_id === newGroupId) return

  try {
    // group_id 0 is the explicit unbind value; null would mean "leave unchanged".
    await keysAPI.update(key.id, { group_id: newGroupId ?? 0 }, getScopeMutationOptions())
    appStore.showSuccess(t('keys.groupChangedSuccess'))
    loadApiKeys()
  } catch (error) {
    appStore.showError(t('keys.failedToChangeGroup'))
  }
}

// The list response already carries a masked credential, so its display suffix stays
// visible; a plaintext value from an older backend is masked before it is rendered.
const displayCredential = (value: string) => (value?.startsWith('****') ? value : maskApiKey(value))

const openRotateModal = (key: ApiKey) => {
  rotateTarget.value = key
  showRotateModal.value = true
}

const onCredentialIssued = (payload: { secret: string; graceUntil: string | null }) => {
  if (rotateTarget.value && payload.secret) {
    sessionSecrets.set(rotateTarget.value.id, payload.secret)
  }
  issuedSecret.value = payload.secret
  issuedGraceUntil.value = payload.graceUntil
  issuedSecretScopeLabel.value = buildScopeLabel(
    activeConsoleScope.value.workspaceId,
    activeConsoleScope.value.projectId
  )
  showSecretDialog.value = true
  loadApiKeys()
}

const closeSecretDialog = () => {
  showSecretDialog.value = false
  issuedSecret.value = ''
  issuedGraceUntil.value = null
}

const closeGroupSelector = (event: MouseEvent) => {
  const target = event.target as HTMLElement
  // Check if click is inside the dropdown or the trigger button
  if (!target.closest('.group\\/dropdown') && !dropdownRef.value?.contains(target)) {
    groupSelectorKeyId.value = null
    dropdownPosition.value = null
  }
  if (columnDropdownRef.value && !columnDropdownRef.value.contains(target)) {
    showColumnDropdown.value = false
  }
}

const confirmDelete = (key: ApiKey) => {
  selectedKey.value = key
  showDeleteDialog.value = true
}

// Period-budget form helpers. The legacy 5h/1d/7d anchors stay selectable and
// are reported by name, so the panel never has to explain a raw duration.
const budgetPeriodKindOf = (key: ApiKey): 'legacy_5h' | 'legacy_1d' | 'legacy_7d' | 'duration' | 'calendar_day' | 'calendar_week' | 'calendar_month' => {
  if (key.budget_legacy_anchor === '5h') return 'legacy_5h'
  if (key.budget_legacy_anchor === '1d') return 'legacy_1d'
  if (key.budget_legacy_anchor === '7d') return 'legacy_7d'
  if (key.budget_period_mode === 'calendar') {
    if (key.budget_calendar_unit === 'week') return 'calendar_week'
    if (key.budget_calendar_unit === 'month') return 'calendar_month'
    return 'calendar_day'
  }
  return 'duration'
}

const budgetDurationValueOf = (key: ApiKey): number | null => {
  if (!key.budget_duration_seconds || key.budget_duration_seconds <= 0) return null
  if (key.budget_duration_seconds % 86400 === 0) return key.budget_duration_seconds / 86400
  if (key.budget_duration_seconds % 3600 === 0) return key.budget_duration_seconds / 3600
  return Math.max(1, Math.round(key.budget_duration_seconds / 60))
}

const budgetDurationUnitOf = (key: ApiKey): 'minutes' | 'hours' | 'days' => {
  if (!key.budget_duration_seconds || key.budget_duration_seconds <= 0) return 'hours'
  if (key.budget_duration_seconds % 86400 === 0) return 'days'
  if (key.budget_duration_seconds % 3600 === 0) return 'hours'
  return 'minutes'
}

const budgetDurationSeconds = (): number => {
  const value = formData.value.budget_duration_value && formData.value.budget_duration_value > 0
    ? formData.value.budget_duration_value
    : 1
  const unit = formData.value.budget_duration_unit
  if (unit === 'days') return Math.round(value * 86400)
  if (unit === 'hours') return Math.round(value * 3600)
  return Math.round(value * 60)
}

// buildBudgetPayload renders the form into the create/update contract. It returns
// null when the owner keeps an enforced budget untouched, and clear_budget when
// the toggle is switched off.
const buildBudgetPayload = (): ApiKeyPeriodBudgetRequest | null => {
  if (formData.value.budget_enforced) {
    return null
  }
  if (!formData.value.enable_budget) {
    if (showEditModal.value && selectedKey.value && selectedKey.value.budget_period_mode !== 'off') {
      return { clear_budget: true }
    }
    return null
  }
  const limit = formData.value.budget_limit && formData.value.budget_limit > 0 ? formData.value.budget_limit : 0
  if (limit <= 0) {
    return null
  }
  switch (formData.value.budget_period_kind) {
    case 'legacy_5h':
      return { legacy_budget_anchor: '5h', budget_limit: limit }
    case 'legacy_1d':
      return { legacy_budget_anchor: '1d', budget_limit: limit }
    case 'legacy_7d':
      return { legacy_budget_anchor: '7d', budget_limit: limit }
    case 'calendar_day':
    case 'calendar_week':
    case 'calendar_month':
      return {
        budget_period_mode: 'calendar',
        budget_calendar_unit: formData.value.budget_period_kind.replace('calendar_', ''),
        budget_timezone: formData.value.budget_timezone || 'UTC',
        budget_limit: limit
      }
    default:
      return {
        budget_period_mode: 'duration',
        budget_duration_seconds: budgetDurationSeconds(),
        budget_anchor: 'first_use',
        budget_limit: limit
      }
  }
}

const handleSubmit = async () => {
  if (!showEditModal.value && isProjectMode.value) {
    if (modalScopeMismatch.value) {
      appStore.showError(
        isZh.value
          ? '当前控制台作用域已切换，请关闭弹窗并重新打开后再试。'
          : 'Active scope changed while dialog was open. Reopen before issuing a key.'
      )
      return
    }
    if (groupsLoading.value || groupsLoadFailed.value) {
      appStore.showError(t(groupsLoadFailed.value ? 'keys.groupsLoadFailed' : 'keys.projectGroupRequired'))
      return
    }
    if (resolvedProjectGroupId.value === null) {
      appStore.showError(t('keys.projectGroupRequired'))
      return
    }
    if (issuableActionsResult.value.status !== 'allowed' || selectedProjectKeyActions.value.length === 0) {
      appStore.showError(t('rbac.keys.emptyActions'))
      return
    }
  }
  if (!showEditModal.value && !isProjectMode.value && (groupsLoading.value || groupsLoadFailed.value || !groups.value.some(group => group.id === formData.value.group_id))) {
    appStore.showError(t(groupsLoadFailed.value ? 'keys.groupsLoadFailed' : 'keys.groupRequired'))
    return
  }
  // Validate group_id is required
  if (!showEditModal.value && !isProjectMode.value && formData.value.group_id === null) {
    appStore.showError(t('keys.groupRequired'))
    return
  }

  // Validate custom key if enabled
  if (!showEditModal.value && formData.value.use_custom_key) {
    if (!formData.value.custom_key) {
      appStore.showError(t('keys.customKeyRequired'))
      return
    }
    if (customKeyError.value) {
      appStore.showError(customKeyError.value)
      return
    }
  }

  // Parse IP lists only if IP restriction is enabled
  const parseIPList = (text: string): string[] =>
    text.split('\n').map(ip => ip.trim()).filter(ip => ip.length > 0)
  const ipWhitelist = formData.value.enable_ip_restriction ? parseIPList(formData.value.ip_whitelist) : []
  const ipBlacklist = formData.value.enable_ip_restriction ? parseIPList(formData.value.ip_blacklist) : []

  // Calculate quota value (null/empty/0 = unlimited, stored as 0)
  const quota = formData.value.quota && formData.value.quota > 0 ? formData.value.quota : 0

  // Calculate expiration
  let expiresInDays: number | undefined
  let expiresAt: string | null | undefined
  if (formData.value.enable_expiration && formData.value.expiration_date) {
    if (!showEditModal.value) {
      // Create mode: calculate days from date
      const expDate = new Date(formData.value.expiration_date)
      const now = new Date()
      const diffDays = Math.ceil((expDate.getTime() - now.getTime()) / (1000 * 60 * 60 * 24))
      expiresInDays = diffDays > 0 ? diffDays : 1
      if (!Number.isNaN(expDate.getTime())) {
        expiresAt = expDate.toISOString()
      }
    } else {
      // Edit mode: use custom date directly
      expiresAt = new Date(formData.value.expiration_date).toISOString()
    }
  } else if (showEditModal.value) {
    // Edit mode: if expiration disabled or date cleared, send empty string to clear
    expiresAt = ''
  }

  // Calculate rate limit values (send 0 when toggle is off)
  const rateLimitData = formData.value.enable_rate_limit ? {
    rate_limit_5h: formData.value.rate_limit_5h && formData.value.rate_limit_5h > 0 ? formData.value.rate_limit_5h : 0,
    rate_limit_1d: formData.value.rate_limit_1d && formData.value.rate_limit_1d > 0 ? formData.value.rate_limit_1d : 0,
    rate_limit_7d: formData.value.rate_limit_7d && formData.value.rate_limit_7d > 0 ? formData.value.rate_limit_7d : 0,
  } : { rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0 }

  submitting.value = true
  try {
    if (showEditModal.value && selectedKey.value) {
      const updates: UpdateApiKeyRequest = {
        name: formData.value.name,
        group_id: formData.value.group_id,
        ip_whitelist: ipWhitelist,
        ip_blacklist: ipBlacklist,
        quota: quota,
        expires_at: expiresAt,
        rate_limit_5h: rateLimitData.rate_limit_5h,
        rate_limit_1d: rateLimitData.rate_limit_1d,
        rate_limit_7d: rateLimitData.rate_limit_7d,
      }
      const budgetPayload = buildBudgetPayload()
      if (budgetPayload) {
        Object.assign(updates, budgetPayload)
      }
      if (shouldSubmitEditStatus(selectedKey.value, formData.value.status)) {
        updates.status = formData.value.status
      }
      await keysAPI.update(selectedKey.value.id, updates, getScopeMutationOptions())
      appStore.showSuccess(t('keys.keyUpdatedSuccess'))
    } else if (
      isProjectMode.value &&
      activeConsoleScope.value.workspaceId &&
      activeConsoleScope.value.projectId
    ) {
      const targetWsId = modalScopePin.value?.workspaceId || activeConsoleScope.value.workspaceId
      const targetProjId = modalScopePin.value?.projectId || activeConsoleScope.value.projectId
      const recheck = await recheckProjectActionBeforeMutation(targetWsId, targetProjId, 'keys.create')
      projectAccessEntry.value = recheck.entry
      if (recheck.state !== 'allowed') {
        appStore.showError(
          t('rbac.keys.deniedProject', {
            grant: 'keys.create',
            project: consoleScopeStore.activeProject?.name || `#${targetProjId}`
          })
        )
        return
      }
      if (issuableActionsResult.value.status !== 'allowed' || selectedProjectKeyActions.value.length === 0) {
        appStore.showError(t('rbac.keys.emptyActions'))
        return
      }
      const projectGroupId = resolvedProjectGroupId.value
      if (projectGroupId === null) {
        appStore.showError(t('keys.projectGroupRequired'))
        return
      }
      const parsedAllowIds = projectModelAccessIdsText.value
        .split(/[\s,]+/)
        .map((id) => id.trim())
        .filter(Boolean)
      const modelAccess =
        projectModelAccessMode.value === 'allow_list'
          ? { mode: 'allow_list' as const, ids: parsedAllowIds }
          : projectModelAccessMode.value === 'deny_all'
            ? { mode: 'deny_all' as const, ids: [] }
            : { mode: 'inherit' as const, ids: [] }
      const payloadSignature = JSON.stringify({
        targetWsId,
        targetProjId,
        group_id: String(projectGroupId),
        name: formData.value.name.trim(),
        model_access: modelAccess,
        actions: selectedProjectKeyActions.value,
        expires_at: expiresAt || null
      })
      if (payloadSignature !== lastCreatePayloadSignature.value || !createIdempotencyKey.value) {
        createIdempotencyKey.value = generateIdempotencyToken()
        lastCreatePayloadSignature.value = payloadSignature
      }
      const created = await identityApi.createProjectKey(
        targetWsId,
        targetProjId,
        {
          group_id: String(projectGroupId),
          name: formData.value.name.trim(),
          model_access: modelAccess,
          actions: [...selectedProjectKeyActions.value] as Parameters<
            typeof identityApi.createProjectKey
          >[2]['actions'],
          expires_at: expiresAt || null
        },
        createIdempotencyKey.value
      )
      const createdActions = (created as { actions?: string[] } | undefined)?.actions
      lastIssuedProjectKeyScope.value = {
        name: created?.name || formData.value.name.trim(),
        actions:
          Array.isArray(createdActions) && createdActions.length > 0
            ? createdActions
            : [...selectedProjectKeyActions.value]
      }
      appStore.showSuccess(t('keys.keyCreatedSuccess'))
      if (created?.credential) {
        issuedSecret.value = created.credential
        issuedGraceUntil.value = null
        issuedSecretScopeLabel.value = modalScopePin.value?.label || buildScopeLabel(targetWsId, targetProjId)
        showSecretDialog.value = true
      }
    } else {
      const customKey = formData.value.use_custom_key ? formData.value.custom_key : undefined
      const issue = await keysAPI.create(
        formData.value.name,
        formData.value.group_id,
        customKey,
        ipWhitelist,
        ipBlacklist,
        quota,
        expiresInDays,
        rateLimitData,
        buildBudgetPayload() ?? undefined
      )
      appStore.showSuccess(t('keys.keyCreatedSuccess'))
      if (issue?.secret) {
        sessionSecrets.set(issue.api_key.id, issue.secret)
        issuedSecret.value = issue.secret
        issuedGraceUntil.value = issue.grace_until ?? null
        issuedSecretScopeLabel.value = buildScopeLabel(null, null)
        showSecretDialog.value = true
      }
      // Only advance tour if active, on submit step, and creation succeeded
      if (onboardingStore.isCurrentStep('[data-tour="key-form-submit"]')) {
        onboardingStore.nextStep(500)
      }
    }
    closeModals()
    loadApiKeys()
  } catch (error: any) {
    const errorMsg = extractApiErrorMessage(error, '') || t('keys.failedToSave')
    appStore.showError(errorMsg)
    // Don't advance tour on error
  } finally {
    submitting.value = false
  }
}

/**
 * 处理删除 API Key 的操作
 * 优化：错误处理改进，优先显示后端返回的具体错误消息（如权限不足等），
 * 若后端未返回消息则显示默认的国际化文本
 */
const handleDelete = async () => {
  if (!selectedKey.value) return

  try {
    await keysAPI.delete(selectedKey.value.id, getScopeMutationOptions())
    appStore.showSuccess(t('keys.keyDeletedSuccess'))
    showDeleteDialog.value = false
    loadApiKeys()
  } catch (error: any) {
    // 优先使用后端返回的错误消息，提供更具体的错误信息给用户
    const errorMsg = error?.message || t('keys.failedToDelete')
    appStore.showError(errorMsg)
  }
}

const closeModals = () => {
  const hadProjectScope = Boolean(modalScopePin.value?.workspaceId && modalScopePin.value?.projectId)
  showCreateModal.value = false
  showEditModal.value = false
  selectedKey.value = null
  modalScopePin.value = null
  createIdempotencyKey.value = ''
  lastCreatePayloadSignature.value = ''
  projectModelAccessMode.value = 'inherit'
  projectModelAccessIdsText.value = ''
  projectKeyAllowModelsRead.value = false
  formData.value = {
    name: '',
    group_id: null,
    status: 'active',
    use_custom_key: false,
    custom_key: '',
    enable_ip_restriction: false,
    ip_whitelist: '',
    ip_blacklist: '',
    enable_quota: false,
    quota: null,
    enable_rate_limit: false,
    rate_limit_5h: null,
    rate_limit_1d: null,
    rate_limit_7d: null,
    enable_budget: false,
    budget_limit: null,
    budget_period_kind: 'legacy_5h',
    budget_duration_value: 12,
    budget_duration_unit: 'hours',
    budget_timezone: 'UTC',
    budget_enforced: false,
    enable_expiration: false,
    expiration_preset: '30',
    expiration_date: ''
  }
  if (hadProjectScope) {
    groups.value = []
    userGroupRates.value = {}
    void loadGroups()
    void loadUserGroupRates()
  }
}

// Set expiration date based on quick select days
const setExpirationDays = (days: number) => {
  formData.value.expiration_preset = days.toString() as '7' | '30' | '90'
  const expDate = new Date()
  expDate.setDate(expDate.getDate() + days)
  formData.value.expiration_date = formatDateTimeLocal(expDate.toISOString())
}

const importToCcswitch = (row: ApiKey) => {
  const platform = row.group?.platform || 'anthropic'

  // A masked credential cannot configure a client. Import needs the one-time
  // secret from this session; never write the list's mask into a deep link.
  if (!sessionSecrets.get(row.id)) {
    appStore.showInfo(t('keys.ccSwitchSecretUnavailable'))
    return
  }

  // For antigravity platform, show client selection dialog
  if (platform === 'antigravity') {
    pendingCcsRow.value = row
    showCcsClientSelect.value = true
    return
  }

  // For other platforms, execute directly
  void executeCcsImport(row, platform === 'gemini' ? 'gemini' : 'claude')
}

const executeCcsImport = async (row: ApiKey, clientType: CcSwitchClientType) => {
  const secret = sessionSecrets.get(row.id)
  if (!secret) {
    appStore.showInfo(t('keys.ccSwitchSecretUnavailable'))
    return
  }
  const baseUrl = publicSettings.value?.api_base_url
  if (!baseUrl) {
    appStore.showError(t('keys.ccSwitchBaseUrlMissing'))
    return
  }
  const platform = row.group?.platform || 'anthropic'

  const usageScript = CC_SWITCH_USAGE_SCRIPT
  // The import model must come from this Key's visible set. When the server
  // cannot answer, the deep link simply carries no model.
  const visible = await keyVisibleModels(row.id)
  const providerName = (publicSettings.value?.site_name || 'OmniHive').trim() || 'OmniHive'
  const deeplink = buildCcSwitchImportDeeplink({
    baseUrl,
    platform,
    clientType,
    providerName,
    apiKey: secret,
    usageScript,
    visibleModels: visible?.models ?? null
  })

  try {
    window.open(deeplink, '_self')

    // Check if the protocol handler worked by detecting if we're still focused
    setTimeout(() => {
      if (document.hasFocus()) {
        // Still focused means the protocol handler likely failed
        appStore.showError(t('keys.ccSwitchNotInstalled'))
      }
    }, 100)
  } catch (error) {
    appStore.showError(t('keys.ccSwitchNotInstalled'))
  }
}

const handleCcsClientSelect = (clientType: CcSwitchClientType) => {
  if (pendingCcsRow.value) {
    void executeCcsImport(pendingCcsRow.value, clientType)
  }
  showCcsClientSelect.value = false
  pendingCcsRow.value = null
}

const closeCcsClientSelect = () => {
  showCcsClientSelect.value = false
  pendingCcsRow.value = null
}

function effectiveRpmSourceLabel(source: ApiKeyEffectiveRPMLimit['source']): string {
  switch (source) {
    case 'user_group_override':
      return t('keys.effectiveRpmSourceUserGroupOverride')
    case 'group':
      return t('keys.effectiveRpmSourceGroup')
    default:
      return t('keys.effectiveRpmSourceUser')
  }
}

// RPM windows are minutes long, so the shared day/hour formatter would always
// print "0m". Show the real countdown to the window boundary.
function formatRpmReset(resetAt: string | null | undefined): string {
  if (!resetAt) return ''
  const diff = new Date(resetAt).getTime() - now.value.getTime()
  if (diff <= 0) return t('keys.resetNow')
  return `${Math.max(1, Math.ceil(diff / 1000))}s`
}

function formatResetTime(resetAt: string | null): string {
  if (!resetAt) return ''
  const diff = new Date(resetAt).getTime() - now.value.getTime()
  if (diff <= 0) return t('keys.resetNow')
  const days = Math.floor(diff / 86400000)
  const hours = Math.floor((diff % 86400000) / 3600000)
  const mins = Math.floor((diff % 3600000) / 60000)
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${mins}m`
  return `${mins}m`
}

onMounted(async () => {
  loadSavedColumns()
  if (activeConsoleScope.value.mode === 'workspace' && !consoleScopeStore.scopesLoaded && authStore?.user) {
    void consoleScopeStore.fetchScopes({ force: false })
  }
  const { canRead, canWrite } = await refreshKeyAccess()
  if (disposed) return
  if (canRead) {
    void loadApiKeys()
  }
  if (isPersonalMode.value || canRead || canWrite) {
    loadGroups()
  }
  if (isPersonalMode.value) {
    loadUserGroupRates()
  }
  loadPublicSettings()
  if (typeof window !== 'undefined') {
    window.addEventListener(CONSOLE_SCOPE_CHANGED_EVENT, handleConsoleScopeSync)
  }
  document.addEventListener('click', closeGroupSelector)
  resetTimer = setInterval(() => { now.value = new Date() }, 60000)
})

onUnmounted(() => {
  endpointCopyRevision++
  clearTimeout(endpointCopyTimer)
  issuedSecret.value = ''
  issuedSecretScopeLabel.value = ''
  issuedGraceUntil.value = null
  sessionSecrets.clear()
  disposed = true
  auxiliaryControllers.forEach(controller => controller.abort())
  auxiliaryControllers.clear()
  abortController?.abort()
  if (typeof window !== 'undefined') {
    window.removeEventListener(CONSOLE_SCOPE_CHANGED_EVENT, handleConsoleScopeSync)
  }
  document.removeEventListener('click', closeGroupSelector)
  if (resetTimer) clearInterval(resetTimer)
})
</script>

<style scoped>
.oh-gateway-banner {
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md, 12px);
  box-shadow: none;
}

.oh-amber-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 2px 10px;
  border-radius: 9999px;
  font-size: 11.5px;
  font-weight: 600;
  background: color-mix(in srgb, var(--brand, #f59e0b) 14%, var(--card, #ffffff));
  color: var(--brand-text, #92610a);
  border: 1px solid color-mix(in srgb, var(--brand, #f59e0b) 32%, transparent);
}

.oh-endpoint-item {
  border-bottom: 1px solid var(--border);
  transition: border-color 0.18s ease;
}

.oh-endpoint-item:hover {
  border-color: color-mix(in srgb, var(--brand, #f59e0b) 35%, var(--border, #e9e9ee));
}

.oh-endpoint-code {
  color: var(--foreground);
}

.oh-gen-endpoints {
  border-color: var(--border);
}

.oh-gen-endpoint-chip {
  background: color-mix(in srgb, var(--card, #ffffff) 82%, transparent);
  border: 1px solid color-mix(in srgb, var(--brand, #f59e0b) 18%, var(--border, #e9e9ee));
}

.oh-filter-bar {
  border: 1px solid var(--border, #e9e9ee);
  border-radius: var(--radius-md, 12px);
  box-shadow: none;
}

/*  .bulk-bar-real 琥珀微光横条风格 */
.bulk-bar-real {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  border-radius: 10px;
  background: var(--surface-2);
  border: 1px solid var(--border);
  font-size: 13px;
  color: var(--muted-foreground);
  transition: background-color .15s, border-color .15s;
}

.bulk-bar-real.has-selection {
  background: color-mix(in srgb, var(--brand) 8%, var(--card));
  border-color: color-mix(in srgb, var(--brand) 35%, var(--border));
  color: var(--foreground);
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--brand) 8%, transparent);
}

.oh-brand-bar {
  width: 3.5px;
  height: 18px;
  border-radius: 9999px;
  background: var(--brand, #f59e0b);
}

.oh-key-secret {
  display: inline-flex;
  align-items: center;
  padding: 3px 8px;
  border-radius: 6px;
  background: var(--surface-2, #f4f4f5);
  border: 1px solid var(--border, #e9e9ee);
  color: var(--foreground, #131316);
}

.oh-status-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
  border: 1px solid transparent;
}

.oh-status-ok {
  color: var(--ok-text, #15803d);
  background: color-mix(in srgb, var(--ok, #16a34a) 12%, transparent);
  border-color: color-mix(in srgb, var(--ok, #16a34a) 24%, transparent);
}

.oh-status-warn {
  color: var(--warn-text, #92400e);
  background: color-mix(in srgb, var(--warn, #d97706) 12%, transparent);
  border-color: color-mix(in srgb, var(--warn, #d97706) 24%, transparent);
}

.oh-status-bad {
  color: var(--bad-text, #b91c1c);
  background: color-mix(in srgb, var(--bad, #dc2626) 12%, transparent);
  border-color: color-mix(in srgb, var(--bad, #dc2626) 24%, transparent);
}

.oh-status-disabled {
  color: var(--oh-danger, #dc2626);
  background: color-mix(in srgb, var(--oh-danger, #dc2626) 10%, transparent);
  border-color: color-mix(in srgb, var(--oh-danger, #dc2626) 24%, transparent);
}

.oh-status-neutral {
  color: var(--muted-foreground, #6f6f78);
  background: var(--surface-2, #f4f4f5);
  border-color: var(--border, #e9e9ee);
}

.oh-dot-neutral {
  background: var(--muted-foreground, #6f6f78);
}

summary:focus-visible, .oh-keys-workbench button:focus-visible {
  outline: 2px solid var(--brand);
  outline-offset: 3px;
}

@media (prefers-reduced-motion: reduce) {
  .bulk-bar-real, .oh-endpoint-item { transition: none; }
}
</style>
