import { ref } from "vue";
import { useI18n } from "vue-i18n";
import { useAppStore } from "@/stores/app";
import { adminAPI } from "@/api/admin";
import type { AdminGroup } from "@/types";
import { extractApiErrorMessage } from "@/utils/apiError";

export function useGroupSorting(loadGroups: () => Promise<void>) {
  const { t } = useI18n();
  const appStore = useAppStore();
  const showSortModal = ref(false);

  const sortSubmitting = ref(false);

  const sortableGroups = ref<AdminGroup[]>([]);

  // 打开排序弹窗
  const openSortModal = async () => {
    try {
      // 获取所有分组（不分页）
      const allGroups = await adminAPI.groups.getAll();
      // 按 sort_order 排序
      sortableGroups.value = [...allGroups].sort(
        (a, b) => a.sort_order - b.sort_order,
      );
      showSortModal.value = true;
    } catch (error) {
      appStore.showError(t("admin.groups.failedToLoad"));
      console.error("Error loading groups for sorting:", error);
    }
  };

  // 关闭排序弹窗
  const closeSortModal = () => {
    showSortModal.value = false;
    sortableGroups.value = [];
  };

  // 保存排序
  const saveSortOrder = async () => {
    sortSubmitting.value = true;
    try {
      const updates = sortableGroups.value.map((g, index) => ({
        id: g.id,
        sort_order: index * 10,
      }));
      await adminAPI.groups.updateSortOrder(updates);
      appStore.showSuccess(t("admin.groups.sortOrderUpdated"));
      closeSortModal();
      loadGroups();
    } catch (error: unknown) {
      appStore.showError(
        extractApiErrorMessage(error, "") ||
          t("admin.groups.failedToUpdateSortOrder"),
      );
      console.error("Error updating sort order:", error);
    } finally {
      sortSubmitting.value = false;
    }
  };
  return {
    showSortModal,
    sortSubmitting,
    sortableGroups,
    openSortModal,
    closeSortModal,
    saveSortOrder,
  };
}
