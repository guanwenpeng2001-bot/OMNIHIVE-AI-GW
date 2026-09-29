import { ref, reactive, computed } from "vue";
import { useI18n } from "vue-i18n";
import { useAppStore } from "@/stores/app";
import { useAuthStore } from "@/stores/auth";
import { adminAPI } from "@/api/admin";
import type { AdminGroup, GroupPlatform } from "@/types";
import type { PRODUCT_GROUP_PLATFORM_OPTIONS } from "@/constants/platforms";
import { getPlatformLabel } from "@/constants/platforms";
import type { Column } from "@/components/common/types";
import { getPersistedPageSize } from "@/composables/usePersistedPageSize";

export function useGroupsList(
  platformCatalog: readonly (typeof PRODUCT_GROUP_PLATFORM_OPTIONS)[number][],
) {
  const { t } = useI18n();
  const appStore = useAppStore();
  const authStore = useAuthStore();
  const ALWAYS_VISIBLE_COLUMNS = new Set(["name", "actions"]);

  // Default hidden columns (hidden on first load / after schema bumps).
  const DEFAULT_HIDDEN_COLUMNS = ["id"];

  const HIDDEN_COLUMNS_KEY = "group-hidden-columns";

  // Bump when adding new default-hidden columns so existing admins pick them up once.
  const COLUMN_SETTINGS_VERSION_KEY = "group-column-settings-version";

  const COLUMN_SETTINGS_VERSION = 2;

  const VERSION_NEW_HIDDEN_COLUMNS: Record<number, string[]> = {
    2: ["id"],
  };

  const allColumns = computed<Column[]>(() => {
    const basic: Column[] = [
      { key: "name", label: t("admin.groups.columns.name"), sortable: true },
      { key: "id", label: t("admin.groups.columns.id"), sortable: true },
      {
        key: "platform",
        label: t("admin.groups.columns.platform"),
        sortable: true,
      },
      {
        key: "account_count",
        label: t("admin.groups.columns.accounts"),
        sortable: true,
      },
      {
        key: "status",
        label: t("admin.groups.columns.status"),
        sortable: true,
      },
      {
        key: "actions",
        label: t("admin.groups.columns.actions"),
        sortable: false,
      },
    ];
    if (authStore.isSimpleMode) return basic;
    return [
      ...basic.slice(0, 3),
      {
        key: "billing_type",
        label: t("admin.groups.columns.billingType"),
        sortable: true,
      },
      {
        key: "rate_multiplier",
        label: t("admin.groups.columns.rateMultiplier"),
        sortable: true,
      },
      {
        key: "is_exclusive",
        label: t("admin.groups.columns.type"),
        sortable: true,
      },
      basic[3],
      {
        key: "capacity",
        label: t("admin.groups.columns.capacity"),
        sortable: false,
      },
      { key: "usage", label: t("admin.groups.columns.usage"), sortable: false },
      ...basic.slice(4),
    ];
  });

  const toggleableColumns = computed(() =>
    allColumns.value.filter((col) => !ALWAYS_VISIBLE_COLUMNS.has(col.key)),
  );

  const hiddenColumns = reactive<Set<string>>(new Set());

  const showColumnDropdown = ref(false);

  const columnDropdownRef = ref<HTMLElement | null>(null);

  const getValidHiddenColumnKeys = () =>
    new Set(toggleableColumns.value.map((col) => col.key));

  const loadSavedColumns = () => {
    hiddenColumns.clear();
    try {
      const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY);
      const validKeys = getValidHiddenColumnKeys();

      if (saved) {
        const parsed = JSON.parse(saved);
        if (Array.isArray(parsed)) {
          parsed
            .filter(
              (key): key is string =>
                typeof key === "string" && validKeys.has(key),
            )
            .forEach((key) => hiddenColumns.add(key));
        }

        // Existing admins: auto-hide columns newly added as default-hidden.
        const storedVersion = Number(
          localStorage.getItem(COLUMN_SETTINGS_VERSION_KEY) ?? "1",
        );
        if (storedVersion < COLUMN_SETTINGS_VERSION) {
          let mutated = false;
          for (let v = storedVersion + 1; v <= COLUMN_SETTINGS_VERSION; v++) {
            for (const key of VERSION_NEW_HIDDEN_COLUMNS[v] ?? []) {
              if (validKeys.has(key) && !hiddenColumns.has(key)) {
                hiddenColumns.add(key);
                mutated = true;
              }
            }
          }
          if (mutated) {
            saveColumnsToStorage();
          } else {
            localStorage.setItem(
              COLUMN_SETTINGS_VERSION_KEY,
              String(COLUMN_SETTINGS_VERSION),
            );
          }
        }
      } else {
        DEFAULT_HIDDEN_COLUMNS.forEach((key) => {
          if (validKeys.has(key)) hiddenColumns.add(key);
        });
        saveColumnsToStorage();
      }
    } catch (error) {
      console.error("Failed to load group column settings:", error);
      DEFAULT_HIDDEN_COLUMNS.forEach((key) => hiddenColumns.add(key));
    }
  };

  const saveColumnsToStorage = () => {
    try {
      const validKeys = getValidHiddenColumnKeys();
      const keys = [...hiddenColumns].filter((key) => validKeys.has(key));
      localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify(keys));
      localStorage.setItem(
        COLUMN_SETTINGS_VERSION_KEY,
        String(COLUMN_SETTINGS_VERSION),
      );
    } catch (error) {
      console.error("Failed to save group column settings:", error);
    }
  };

  const isColumnVisible = (key: string) => !hiddenColumns.has(key);

  const hasVisibleUsageSummaryConsumer = computed(
    () =>
      !authStore.isSimpleMode &&
      (isColumnVisible("usage") || isColumnVisible("billing_type")),
  );

  const hasVisibleCapacityColumn = computed(
    () => !authStore.isSimpleMode && isColumnVisible("capacity"),
  );

  const toggleColumn = (key: string) => {
    const validKeys = getValidHiddenColumnKeys();
    if (!validKeys.has(key)) return;

    const wasHidden = hiddenColumns.has(key);
    if (wasHidden) {
      hiddenColumns.delete(key);
    } else {
      hiddenColumns.add(key);
    }
    saveColumnsToStorage();

    if (wasHidden && (key === "usage" || key === "billing_type")) {
      loadUsageSummary();
    }
    if (wasHidden && key === "capacity") {
      loadCapacitySummary();
    }
  };

  const columns = computed<Column[]>(() =>
    allColumns.value.filter(
      (col) =>
        ALWAYS_VISIBLE_COLUMNS.has(col.key) || !hiddenColumns.has(col.key),
    ),
  );

  if (typeof window !== "undefined") {
    loadSavedColumns();
  }

  // Filter options
  const statusOptions = computed(() => [
    { value: "", label: t("admin.groups.allStatus") },
    { value: "active", label: t("admin.accounts.status.active") },
    { value: "inactive", label: t("admin.accounts.status.inactive") },
  ]);

  const exclusiveOptions = computed(() => [
    { value: "", label: t("admin.groups.allGroups") },
    { value: "true", label: t("admin.groups.exclusive") },
    { value: "false", label: t("admin.groups.nonExclusive") },
  ]);

  const platformOptions = computed(() =>
    platformCatalog
      .filter((option) => !authStore.isSimpleMode || option.value !== "composite")
      .map((option) => ({ ...option, label: getPlatformLabel(option.value, t) })),
  );

  const platformFilterOptions = computed(() => [
    { value: "", label: t("admin.groups.allPlatforms") },
    ...platformCatalog.map((option) => ({
      ...option,
      label: getPlatformLabel(option.value, t),
    })),
  ]);

  const groups = ref<AdminGroup[]>([]);

  const loading = ref(false);

  type GroupUsageSummary = {
    today_cost: number;
    yesterday_cost: number;
    total_cost: number;
  };

  const usageMap = ref<Map<number, GroupUsageSummary>>(new Map());

  const usageLoading = ref(false);

  const capacityMap = ref<
    Map<
      number,
      {
        concurrencyUsed: number;
        concurrencyMax: number;
        sessionsUsed: number;
        sessionsMax: number;
        rpmUsed: number;
        rpmMax: number;
      }
    >
  >(new Map());

  const searchQuery = ref("");

  const filters = reactive({
    platform: "",
    status: "",
    is_exclusive: "",
  });

  const pagination = reactive({
    page: 1,
    page_size: getPersistedPageSize(),
    total: 0,
    pages: 0,
  });

  const sortState = reactive({
    sort_by: "sort_order",
    sort_order: "asc" as "asc" | "desc",
  });

  let abortController: AbortController | null = null;

  const loadGroups = async () => {
    if (abortController) {
      abortController.abort();
    }
    const currentController = new AbortController();
    abortController = currentController;
    const { signal } = currentController;
    loading.value = true;
    try {
      const response = await adminAPI.groups.list(
        pagination.page,
        pagination.page_size,
        {
          platform: (filters.platform as GroupPlatform) || undefined,
          status: filters.status as "active" | "inactive",
          is_exclusive:
            !authStore.isSimpleMode && filters.is_exclusive
              ? filters.is_exclusive === "true"
              : undefined,
          search: searchQuery.value.trim() || undefined,
          sort_by: sortState.sort_by,
          sort_order: sortState.sort_order,
        },
        { signal },
      );
      if (signal.aborted) return;
      groups.value = response.items;
      pagination.total = response.total;
      pagination.pages = response.pages;
      if (hasVisibleUsageSummaryConsumer.value) {
        loadUsageSummary();
      } else {
        usageLoading.value = false;
      }
      if (hasVisibleCapacityColumn.value) {
        loadCapacitySummary();
      }
    } catch (error: unknown) {
      if (
        signal.aborted ||
        (error instanceof Error && error.name === "AbortError") ||
        (typeof error === "object" &&
          error !== null &&
          "code" in error &&
          error.code === "ERR_CANCELED")
      ) {
        return;
      }
      appStore.showError(t("admin.groups.failedToLoad"));
      console.error("Error loading groups:", error);
    } finally {
      if (abortController === currentController && !signal.aborted) {
        loading.value = false;
      }
    }
  };

  const formatCost = (cost: number): string => {
    if (cost >= 1000) return cost.toFixed(0);
    if (cost >= 100) return cost.toFixed(1);
    return cost.toFixed(2);
  };

  const formatUsd = (cost: number | null | undefined): string =>
    `$${formatCost(cost ?? 0)}`;

  const getQuotaUsageClass = (
    used: number,
    limit: number | null | undefined,
  ): string => {
    if (!limit || limit <= 0) {
      return "font-medium text-gray-700 dark:text-gray-300";
    }
    const ratio = used / limit;
    if (ratio >= 1) {
      return "font-semibold text-red-600 dark:text-red-400";
    }
    if (ratio >= 0.8) {
      return "font-semibold text-amber-600 dark:text-amber-400";
    }
    return "font-medium text-gray-700 dark:text-gray-300";
  };

  const loadUsageSummary = async () => {
    if (!hasVisibleUsageSummaryConsumer.value) {
      usageLoading.value = false;
      return;
    }
    usageLoading.value = true;
    try {
      const data = await adminAPI.groups.getUsageSummary();
      const map = new Map<number, GroupUsageSummary>();
      for (const item of data) {
        map.set(item.group_id, {
          today_cost: item.today_cost,
          yesterday_cost: item.yesterday_cost,
          total_cost: item.total_cost,
        });
      }
      usageMap.value = map;
    } catch (error) {
      console.error("Error loading group usage summary:", error);
    } finally {
      usageLoading.value = false;
    }
  };

  const loadCapacitySummary = async () => {
    if (!hasVisibleCapacityColumn.value) {
      return;
    }
    try {
      const data = await adminAPI.groups.getCapacitySummary();
      const map = new Map<
        number,
        {
          concurrencyUsed: number;
          concurrencyMax: number;
          sessionsUsed: number;
          sessionsMax: number;
          rpmUsed: number;
          rpmMax: number;
        }
      >();
      for (const item of data) {
        map.set(item.group_id, {
          concurrencyUsed: item.concurrency_used,
          concurrencyMax: item.concurrency_max,
          sessionsUsed: item.sessions_used,
          sessionsMax: item.sessions_max,
          rpmUsed: item.rpm_used,
          rpmMax: item.rpm_max,
        });
      }
      capacityMap.value = map;
    } catch (error) {
      console.error("Error loading group capacity summary:", error);
    }
  };

  let searchTimeout: ReturnType<typeof setTimeout>;

  const handleSearch = () => {
    clearTimeout(searchTimeout);
    searchTimeout = setTimeout(() => {
      pagination.page = 1;
      loadGroups();
    }, 300);
  };

  const handlePageChange = (page: number) => {
    pagination.page = page;
    loadGroups();
  };

  const handlePageSizeChange = (pageSize: number) => {
    pagination.page_size = pageSize;
    pagination.page = 1;
    loadGroups();
  };

  const handleSort = (key: string, order: "asc" | "desc") => {
    sortState.sort_by = key;
    sortState.sort_order = order;
    pagination.page = 1;
    loadGroups();
  };
  return {
    ALWAYS_VISIBLE_COLUMNS,
    DEFAULT_HIDDEN_COLUMNS,
    HIDDEN_COLUMNS_KEY,
    COLUMN_SETTINGS_VERSION_KEY,
    COLUMN_SETTINGS_VERSION,
    VERSION_NEW_HIDDEN_COLUMNS,
    allColumns,
    toggleableColumns,
    hiddenColumns,
    showColumnDropdown,
    columnDropdownRef,
    getValidHiddenColumnKeys,
    loadSavedColumns,
    saveColumnsToStorage,
    isColumnVisible,
    hasVisibleUsageSummaryConsumer,
    hasVisibleCapacityColumn,
    toggleColumn,
    columns,
    statusOptions,
    exclusiveOptions,
    platformOptions,
    platformFilterOptions,
    groups,
    loading,
    usageMap,
    usageLoading,
    capacityMap,
    searchQuery,
    filters,
    pagination,
    sortState,
    loadGroups,
    formatCost,
    formatUsd,
    getQuotaUsageClass,
    loadUsageSummary,
    loadCapacitySummary,
    handleSearch,
    handlePageChange,
    handlePageSizeChange,
    handleSort,
  };
}
