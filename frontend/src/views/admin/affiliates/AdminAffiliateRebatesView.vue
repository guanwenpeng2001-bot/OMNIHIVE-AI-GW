<template>
  <AdminAffiliateRecordsTable type="rebates" class="affiliate-rebates-workspace" />
</template>

<script setup lang="ts">
import AdminAffiliateRecordsTable from './AdminAffiliateRecordsTable.vue'
</script>

<style scoped>
.affiliate-rebates-workspace :deep(.table-page-layout h1) { font-size: 1.25rem; line-height: 1.5; }
.affiliate-rebates-workspace :deep(.table-scroll-container) { border-color: var(--border); box-shadow: none; }
.affiliate-rebates-workspace :deep(.affiliate-records-table) { scrollbar-gutter: stable; }
.affiliate-rebates-workspace :deep(.affiliate-records-table td) { padding-top: 12px; padding-bottom: 12px; font-variant-numeric: tabular-nums; }
.affiliate-rebates-workspace :deep(.affiliate-records-table td:nth-child(6)) { background: color-mix(in srgb, var(--brand) 3%, var(--card)); }
.affiliate-rebates-workspace :deep(nav.inline-flex) { display: flex; width: fit-content; }
</style>
