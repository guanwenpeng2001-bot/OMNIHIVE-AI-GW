<template>
  <AdminAffiliateRecordsTable type="transfers" class="affiliate-transfers-workspace" />
</template>

<script setup lang="ts">
import AdminAffiliateRecordsTable from './AdminAffiliateRecordsTable.vue'
</script>

<style scoped>
.affiliate-transfers-workspace :deep(.table-page-layout h1) { font-size: 1.25rem; line-height: 1.5; }
.affiliate-transfers-workspace :deep(.table-scroll-container) { border-color: var(--border); box-shadow: none; }
.affiliate-transfers-workspace :deep(.affiliate-records-table) { scrollbar-gutter: stable; }
.affiliate-transfers-workspace :deep(.affiliate-records-table td) { padding-top: 12px; padding-bottom: 12px; font-variant-numeric: tabular-nums; }
.affiliate-transfers-workspace :deep(.affiliate-records-table td:nth-child(2)) { background: color-mix(in srgb, var(--brand) 3%, var(--card)); }
.affiliate-transfers-workspace :deep(nav.inline-flex) { display: flex; width: fit-content; }
</style>
