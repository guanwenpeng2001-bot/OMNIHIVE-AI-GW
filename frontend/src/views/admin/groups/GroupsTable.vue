<template>
  <TablePageLayout class="omnihive-groups-table">
    <template #filters>
      <div
        class="flex flex-col justify-between gap-3 rounded-lg border border-[var(--border)] bg-[var(--card)] p-3 xl:flex-row xl:items-start"
      >
        <!-- Left: fuzzy search + filters (can wrap to multiple lines) -->
        <div class="flex flex-1 flex-wrap items-center gap-2">
          <div class="relative w-full sm:w-64">
            <Icon
              name="search"
              size="md"
              class="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--muted-foreground)]"
            />
            <input
              v-model="searchQuery"
              type="text"
              :placeholder="t('admin.groups.searchGroups')"
              class="input pl-10"
              @input="handleSearch"
            />
          </div>
          <Select
            v-model="filters.platform"
            :options="platformFilterOptions"
            :placeholder="t('admin.groups.allPlatforms')"
            class="w-full sm:w-44"
            @change="loadGroups"
          />
          <Select
            v-model="filters.status"
            :options="statusOptions"
            :placeholder="t('admin.groups.allStatus')"
            class="w-full sm:w-40"
            @change="loadGroups"
          />
          <Select
            v-if="!authStore.isSimpleMode"
            v-model="filters.is_exclusive"
            :options="exclusiveOptions"
            :placeholder="t('admin.groups.allGroups')"
            class="w-full sm:w-44"
            @change="loadGroups"
          />
        </div>

        <!-- Right: actions -->
        <div
          class="flex w-full flex-shrink-0 flex-wrap items-center justify-end gap-2 xl:w-auto"
        >
          <button
            @click="loadGroups"
            :disabled="loading"
            class="btn btn-secondary"
            :title="t('common.refresh')"
          >
            <Icon
              name="refresh"
              size="md"
              :class="loading ? 'animate-spin' : ''"
            />
          </button>
          <div class="relative" ref="columnDropdownRef">
            <button
              @click="showColumnDropdown = !showColumnDropdown"
              class="btn btn-secondary"
              :title="t('admin.groups.columnSettings')"
            >
              <Icon name="grid" size="md" class="mr-2" />
              <span class="hidden md:inline">{{
                t("admin.groups.columnSettings")
              }}</span>
            </button>
            <div
              v-if="showColumnDropdown"
              class="absolute right-0 top-full z-50 mt-1 max-h-80 w-48 overflow-y-auto rounded-lg border border-[var(--border)] bg-[var(--card)] py-1 shadow-lg"
            >
              <button
                v-for="col in toggleableColumns"
                :key="col.key"
                @click="toggleColumn(col.key)"
                class="oh-focus-ring flex w-full items-center justify-between px-4 py-2 text-left text-sm text-[var(--foreground)] hover:bg-[var(--surface-2)]"
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
            v-if="!authStore.isSimpleMode"
            @click="emit('sort')"
            class="btn btn-secondary"
            :title="t('admin.groups.sortOrder')"
          >
            <Icon name="arrowsUpDown" size="md" class="mr-2" />
            {{ t("admin.groups.sortOrder") }}
          </button>
          <button
            @click="emit('create')"
            class="btn btn-primary"
            data-tour="groups-create-btn"
          >
            <Icon name="plus" size="md" class="mr-2" />
            {{ t("admin.groups.createGroup") }}
          </button>
        </div>
      </div>
    </template>

    <template #table>
      <DataTable
        :columns="columns"
        :data="groups"
        :loading="loading"
        :server-side-sort="true"
        default-sort-key="sort_order"
        default-sort-order="asc"
        @sort="handleSort"
      >
        <template #cell-name="{ value }">
          <span class="font-medium text-[var(--foreground)]">{{
            value
          }}</span>
        </template>

        <template #cell-id="{ value }">
          <span class="font-mono text-xs text-[var(--muted-foreground)]"
            >#{{ value }}</span
          >
        </template>

        <template #cell-platform="{ value }">
          <span class="pill pill-ghost gap-1.5 text-[var(--foreground)]">
            <PlatformIcon :platform="value" size="xs" />
            {{ getPlatformLabel(value, t) }}
          </span>
        </template>

        <template #cell-billing_type="{ row }">
          <div class="space-y-1">
            <!-- Type Badge -->
            <span class="pill pill-ghost text-[var(--muted-foreground)]">
              {{
                row.subscription_type === "subscription"
                  ? t("admin.groups.subscription.subscription")
                  : t("admin.groups.subscription.standard")
              }}
            </span>
            <!-- Subscription Limits - compact single line -->
            <div
              v-if="row.subscription_type === 'subscription'"
              class="space-y-0.5 text-xs text-[var(--muted-foreground)]"
            >
              <div
                v-if="
                  row.daily_limit_usd ||
                  row.weekly_limit_usd ||
                  row.monthly_limit_usd
                "
                class="flex flex-wrap items-center gap-x-1 gap-y-0.5"
              >
                <span v-if="row.daily_limit_usd" class="whitespace-nowrap">
                  <span
                    v-if="usageLoading"
                    class="font-medium text-[var(--muted-foreground)]"
                    >—</span
                  >
                  <span
                    v-else
                    :class="
                      getQuotaUsageClass(
                        usageMap.get(row.id)?.today_cost ?? 0,
                        row.daily_limit_usd,
                      )
                    "
                    >{{
                      formatUsd(usageMap.get(row.id)?.today_cost ?? 0)
                    }}</span
                  >
                  <span class="text-[var(--muted-foreground)]">
                    / {{ formatUsd(row.daily_limit_usd) }}/{{
                      t("admin.groups.limitDay")
                    }}</span
                  >
                </span>
                <span
                  v-if="
                    row.daily_limit_usd &&
                    (row.weekly_limit_usd || row.monthly_limit_usd)
                  "
                  class="mx-1 text-[var(--muted-foreground)]"
                  >·</span
                >
                <span v-if="row.weekly_limit_usd" class="whitespace-nowrap"
                  >{{ formatUsd(row.weekly_limit_usd) }}/{{
                    t("admin.groups.limitWeek")
                  }}</span
                >
                <span
                  v-if="row.weekly_limit_usd && row.monthly_limit_usd"
                  class="mx-1 text-[var(--muted-foreground)]"
                  >·</span
                >
                <span v-if="row.monthly_limit_usd" class="whitespace-nowrap"
                  >{{ formatUsd(row.monthly_limit_usd) }}/{{
                    t("admin.groups.limitMonth")
                  }}</span
                >
              </div>
              <span v-else class="text-[var(--muted-foreground)]">{{
                t("admin.groups.subscription.noLimit")
              }}</span>
              <div class="text-[var(--muted-foreground)]">
                {{ t("admin.groups.usageTotal") }}
                <span
                  class="ml-1 font-medium text-[var(--muted-foreground)]"
                  >{{
                    usageLoading
                      ? "—"
                      : formatUsd(usageMap.get(row.id)?.total_cost ?? 0)
                  }}</span
                >
              </div>
            </div>
          </div>
        </template>

        <template #cell-rate_multiplier="{ value }">
          <span class="text-sm text-[var(--foreground)]"
            >{{ value }}x</span
          >
        </template>

        <template #cell-is_exclusive="{ value }">
          <span class="pill pill-ghost" :class="value ? 'text-[var(--brand-text)]' : 'text-[var(--muted-foreground)]'">
            {{ value ? t("admin.groups.exclusive") : t("admin.groups.public") }}
          </span>
        </template>

        <template #cell-account_count="{ row }">
          <div class="space-y-0.5 text-xs">
            <div>
              <span class="text-[var(--muted-foreground)]">{{
                t("admin.groups.accountsAvailable")
              }}</span>
              <span
                class="ml-1 font-medium text-[var(--ok-text)]"
                >{{ row.active_account_count || 0 }}</span
              >
              <span
                class="ml-1 inline-flex items-center rounded bg-[var(--surface-2)] px-1.5 py-0.5 font-medium text-[var(--foreground)]"
                >{{ t("admin.groups.accountsUnit") }}</span
              >
            </div>
            <div v-if="row.rate_limited_account_count">
              <span class="text-[var(--muted-foreground)]">{{
                t("admin.groups.accountsRateLimited")
              }}</span>
              <span
                class="ml-1 font-medium text-[var(--warn-text)]"
                >{{ row.rate_limited_account_count }}</span
              >
              <span
                class="ml-1 inline-flex items-center rounded bg-[var(--surface-2)] px-1.5 py-0.5 font-medium text-[var(--foreground)]"
                >{{ t("admin.groups.accountsUnit") }}</span
              >
            </div>
            <div>
              <span class="text-[var(--muted-foreground)]">{{
                t("admin.groups.accountsTotal")
              }}</span>
              <span class="ml-1 font-medium text-[var(--foreground)]">{{
                row.account_count || 0
              }}</span>
              <span
                class="ml-1 inline-flex items-center rounded bg-[var(--surface-2)] px-1.5 py-0.5 font-medium text-[var(--foreground)]"
                >{{ t("admin.groups.accountsUnit") }}</span
              >
            </div>
          </div>
        </template>

        <template #cell-capacity="{ row }">
          <GroupCapacityBadge
            v-if="capacityMap.get(row.id)"
            :concurrency-used="capacityMap.get(row.id)!.concurrencyUsed"
            :concurrency-max="capacityMap.get(row.id)!.concurrencyMax"
            :sessions-used="capacityMap.get(row.id)!.sessionsUsed"
            :sessions-max="capacityMap.get(row.id)!.sessionsMax"
            :rpm-used="capacityMap.get(row.id)!.rpmUsed"
            :rpm-max="capacityMap.get(row.id)!.rpmMax"
          />
          <span v-else class="text-xs text-[var(--muted-foreground)]">—</span>
        </template>

        <template #cell-usage="{ row }">
          <div v-if="usageLoading" class="text-xs text-[var(--muted-foreground)]">—</div>
          <div v-else class="space-y-0.5 text-xs">
            <div class="text-[var(--muted-foreground)]">
              <span class="text-[var(--muted-foreground)]">{{
                t("admin.groups.usageToday")
              }}</span>
              <span class="ml-1 font-medium text-[var(--foreground)]"
                >${{ formatCost(usageMap.get(row.id)?.today_cost ?? 0) }}</span
              >
            </div>
            <div class="text-[var(--muted-foreground)]">
              <span class="text-[var(--muted-foreground)]">{{
                t("admin.groups.usageYesterday")
              }}</span>
              <span class="ml-1 font-medium text-[var(--foreground)]"
                >${{
                  formatCost(usageMap.get(row.id)?.yesterday_cost ?? 0)
                }}</span
              >
            </div>
            <div class="text-[var(--muted-foreground)]">
              <span class="text-[var(--muted-foreground)]">{{
                t("admin.groups.usageTotal")
              }}</span>
              <span class="ml-1 font-medium text-[var(--foreground)]"
                >${{ formatCost(usageMap.get(row.id)?.total_cost ?? 0) }}</span
              >
            </div>
          </div>
        </template>

        <template #cell-status="{ value }">
          <span
            :class="[
              'badge',
              value === 'active' ? 'badge-success' : 'badge-danger',
            ]"
          >
            <span class="pulse-dot bg-current" aria-hidden="true" />
            {{ t("admin.accounts.status." + value) }}
          </span>
        </template>

        <template #cell-actions="{ row }">
          <div class="group-table-actions grid grid-cols-2 gap-1">
            <button
              @click="emit('edit', row)"
              class="oh-focus-ring flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-[var(--muted-foreground)] transition-colors hover:bg-[var(--surface-2)] hover:text-[var(--brand-text)]"
            >
              <Icon name="edit" size="sm" />
              <span class="text-xs">{{ t("common.edit") }}</span>
            </button>
            <button
              v-if="!authStore.isSimpleMode"
              data-testid="group-duplicate"
              :title="
                duplicatingGroupIds.has(row.id)
                  ? t('admin.groups.duplicating')
                  : t('admin.groups.duplicate')
              "
              :disabled="duplicatingGroupIds.has(row.id)"
              @click="emit('duplicate', row)"
              class="oh-focus-ring flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-[var(--muted-foreground)] transition-colors hover:bg-[var(--surface-2)] hover:text-[var(--brand-text)] disabled:cursor-not-allowed disabled:opacity-50"
            >
              <Icon name="copy" size="sm" />
              <span class="text-xs">
                {{
                  duplicatingGroupIds.has(row.id)
                    ? t("admin.groups.duplicating")
                    : t("admin.groups.duplicate")
                }}
              </span>
            </button>
            <button
              v-if="!authStore.isSimpleMode && row.platform === 'composite'"
              data-testid="group-composite-routes"
              @click="emit('compositeRoutes', row)"
              class="oh-focus-ring flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-[var(--muted-foreground)] transition-colors hover:bg-[var(--surface-2)] hover:text-[var(--foreground)]"
            >
              <Icon name="swap" size="sm" />
              <span class="text-xs">{{
                t("admin.groups.compositeRoutes.action")
              }}</span>
            </button>
            <button
              v-if="!authStore.isSimpleMode"
              data-testid="group-rate-multipliers"
              @click="emit('rateMultipliers', row)"
              class="oh-focus-ring flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-[var(--muted-foreground)] transition-colors hover:bg-[var(--surface-2)] hover:text-[var(--foreground)]"
            >
              <Icon name="dollar" size="sm" />
              <span class="text-xs">{{
                t("admin.groups.rateMultipliers")
              }}</span>
            </button>
            <button
              v-if="!authStore.isSimpleMode"
              data-testid="group-rpm-overrides"
              @click="emit('rpmOverrides', row)"
              class="oh-focus-ring flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-[var(--muted-foreground)] transition-colors hover:bg-[var(--surface-2)] hover:text-[var(--foreground)]"
            >
              <Icon name="bolt" size="sm" />
              <span class="text-xs">{{ t("admin.groups.rpmOverrides") }}</span>
            </button>
            <button
              @click="emit('delete', row)"
              class="oh-focus-ring flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-[var(--muted-foreground)] transition-colors hover:bg-[color-mix(in_srgb,var(--bad)_8%,var(--card))] hover:text-[var(--bad-text)]"
            >
              <Icon name="trash" size="sm" />
              <span class="text-xs">{{ t("common.delete") }}</span>
            </button>
          </div>
        </template>

        <template #empty>
          <EmptyState
            :title="t('admin.groups.noGroupsYet')"
            :description="t('admin.groups.createFirstGroup')"
            :action-text="t('admin.groups.createGroup')"
            @action="emit('create')"
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
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { useAuthStore } from "@/stores/auth";
import type { AdminGroup } from "@/types";
import TablePageLayout from "@/components/layout/TablePageLayout.vue";
import DataTable from "@/components/common/DataTable.vue";
import Pagination from "@/components/common/Pagination.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import Select from "@/components/common/Select.vue";
import PlatformIcon from "@/components/common/PlatformIcon.vue";
import { getPlatformLabel } from "@/constants/platforms";
import Icon from "@/components/icons/Icon.vue";
import GroupCapacityBadge from "@/components/common/GroupCapacityBadge.vue";
import type { useGroupsList } from "./useGroupsList";
const props = defineProps<{
  controller: ReturnType<typeof useGroupsList>;
  duplicatingGroupIds: Set<number>;
}>();
const emit = defineEmits<{
  create: [];
  sort: [];
  edit: [group: AdminGroup];
  delete: [group: AdminGroup];
  duplicate: [group: AdminGroup];
  rateMultipliers: [group: AdminGroup];
  rpmOverrides: [group: AdminGroup];
  compositeRoutes: [group: AdminGroup];
}>();
const { t } = useI18n();
const authStore = useAuthStore();
const {
  toggleableColumns,
  showColumnDropdown,
  columnDropdownRef,
  isColumnVisible,
  toggleColumn,
  columns,
  statusOptions,
  exclusiveOptions,
  platformFilterOptions,
  groups,
  loading,
  usageMap,
  usageLoading,
  capacityMap,
  searchQuery,
  filters,
  pagination,
  loadGroups,
  formatCost,
  formatUsd,
  getQuotaUsageClass,
  handleSearch,
  handlePageChange,
  handlePageSizeChange,
  handleSort,
} = props.controller;
</script>

<style scoped>
.omnihive-groups-table :deep(td) {
  padding-top: 0.625rem;
  padding-bottom: 0.625rem;
  font-variant-numeric: tabular-nums;
}

.group-table-actions {
  min-width: 11rem;
}

.group-table-actions > button {
  flex-direction: row;
  justify-content: flex-start;
  gap: 0.375rem;
  padding: 0.375rem 0.5rem;
  border: 1px solid transparent;
  border-radius: 0.375rem;
  color: var(--muted-foreground);
  white-space: nowrap;
}

.group-table-actions > button:hover {
  border-color: var(--border);
  background: var(--surface-2);
  color: var(--foreground);
}

.group-table-actions > button:last-child:hover {
  color: var(--bad-text);
}

.group-table-actions > button:focus-visible {
  outline: 2px solid var(--ring);
  outline-offset: 2px;
}
</style>
