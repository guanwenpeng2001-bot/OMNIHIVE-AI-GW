<template>
  <BaseDialog
    :show="showSortModal"
    :title="t('admin.groups.sortOrder')"
    width="normal"
    @close="closeSortModal"
  >
    <div class="space-y-4">
      <p class="text-sm text-[var(--muted-foreground)]">
        {{ t("admin.groups.sortOrderHint") }}
      </p>
      <VueDraggable v-model="sortableGroups" :animation="200" class="space-y-2">
        <div
          v-for="group in sortableGroups"
          :key="group.id"
          class="flex cursor-grab items-center gap-3 rounded-lg border border-[var(--border)] bg-[var(--card)] p-3 transition-colors hover:bg-[var(--surface-2)] active:cursor-grabbing"
        >
          <div class="text-[var(--muted-foreground)]">
            <Icon name="menu" size="md" />
          </div>
          <div class="min-w-0 flex-1 space-y-1">
            <div class="break-words text-sm font-semibold text-[var(--foreground)]">
              {{ group.name }}
            </div>
            <div class="text-xs text-[var(--muted-foreground)]">
              <span class="pill pill-ghost">
                {{ getPlatformLabel(group.platform, t) }}
              </span>
            </div>
          </div>
          <div class="font-mono text-xs tabular-nums text-[var(--muted-foreground)]">#{{ group.id }}</div>
        </div>
      </VueDraggable>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3 pt-4">
        <button @click="closeSortModal" type="button" class="btn btn-secondary">
          {{ t("common.cancel") }}
        </button>
        <button
          @click="saveSortOrder"
          :disabled="sortSubmitting"
          class="btn btn-primary"
        >
          <svg
            v-if="sortSubmitting"
            class="-ml-1 mr-2 h-4 w-4 animate-spin"
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
          {{ sortSubmitting ? t("common.saving") : t("common.save") }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import BaseDialog from "@/components/common/BaseDialog.vue";
import Icon from "@/components/icons/Icon.vue";
import { getPlatformLabel } from "@/constants/platforms";
import { VueDraggable } from "vue-draggable-plus";
import type { useGroupSorting } from "./useGroupSorting";
const props = defineProps<{ controller: ReturnType<typeof useGroupSorting> }>();
const { t } = useI18n();
const {
  showSortModal,
  sortSubmitting,
  sortableGroups,
  closeSortModal,
  saveSortOrder,
} = props.controller;
</script>
