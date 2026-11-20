<template>
  <div class="table-page-layout" :class="{ 'mobile-mode': isMobile }">
    <!-- 固定区域：操作按钮 -->
    <div v-if="$slots.actions" class="layout-section-fixed">
      <slot name="actions" />
    </div>

    <!-- 固定区域：搜索和过滤器 -->
    <div v-if="$slots.filters" class="layout-section-fixed">
      <slot name="filters" />
    </div>

    <!-- 滚动区域：表格 -->
    <div class="layout-section-scrollable">
      <div class="card table-scroll-container">
        <slot name="table" />
      </div>
    </div>

    <!-- 固定区域：分页器 -->
    <div v-if="$slots.pagination" class="layout-section-fixed">
      <slot name="pagination" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'

const isMobile = ref(false)

const checkMobile = () => {
  isMobile.value = window.innerWidth < 1024
}

onMounted(() => {
  checkMobile()
  window.addEventListener('resize', checkMobile)
})

onUnmounted(() => {
  window.removeEventListener('resize', checkMobile)
})
</script>

<style scoped>
/* 自然文档流纵向伸展布局：彻底消除页面级 + 表格内双纵向滚动条冲突与平板挤压 */
.table-page-layout {
  @apply flex flex-col gap-4 min-w-0;
}

.layout-section-fixed {
  @apply flex-shrink-0 min-w-0;
}

.layout-section-scrollable {
  @apply flex flex-col min-w-0;
}

/* 表格滚动容器 - OmniHive 细边框高密度工作台表格外壳 */
.table-scroll-container {
  @apply flex flex-col min-w-0 overflow-hidden rounded-oh-lg border;
  background: var(--oh-bg-surface);
  border-color: var(--oh-border-default);
  color: var(--oh-fg-primary);
  box-shadow: var(--oh-shadow-xs);
}

.table-scroll-container :deep(.table-wrapper) {
  @apply w-full overflow-x-auto;
  /* 确保横向滚动条显示在最底部 */
  scrollbar-gutter: stable;
}

.table-scroll-container :deep(table) {
  @apply w-full;
  min-width: max-content; /* 关键：确保表格宽度根据内容撑开，从而触发横向滚动 */
  display: table; /* 使用标准 table 布局以支持 sticky 列 */
}

.table-scroll-container :deep(thead) {
  @apply backdrop-blur-sm;
  background: color-mix(in srgb, var(--oh-bg-subtle) 68%, var(--oh-bg-surface));
}

.table-scroll-container :deep(tbody) {
  /* 保持默认 table-row-group 显示，不使用 block */
}

.table-scroll-container :deep(tbody tr) {
  transition: background-color 0.16s ease;
}

.table-scroll-container :deep(tbody tr:hover) {
  background: color-mix(in srgb, var(--oh-accent) 4.5%, transparent);
}

.table-scroll-container :deep(th) {
  @apply px-4 py-2.5 text-left border-b;
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.02em;
  color: var(--oh-fg-muted);
  border-color: var(--oh-border-default);
  user-select: none;
}

.table-scroll-container :deep(td) {
  @apply px-4 py-3 text-sm border-b;
  color: var(--oh-fg-primary);
  border-color: var(--oh-border-subtle);
}

/* 移动端：恢复正常滚动 */
.table-page-layout.mobile-mode .table-scroll-container {
  @apply h-auto border-none shadow-none bg-transparent;
}

.table-page-layout.mobile-mode .layout-section-scrollable {
  @apply flex-none min-h-fit;
}

.table-page-layout.mobile-mode .table-scroll-container :deep(table) {
  @apply flex-none;
  display: table;
  min-width: 100%;
}
</style>
