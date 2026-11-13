<template>
  <AdminAffiliateRecordsTable type="invites" class="affiliate-invites-workspace" />
</template>

<script setup lang="ts">
import AdminAffiliateRecordsTable from './AdminAffiliateRecordsTable.vue'
</script>

<style scoped>
.affiliate-invites-workspace :deep(.table-page-layout h1) { font-size: 1.25rem; line-height: 1.5; }
.affiliate-invites-workspace :deep(.table-scroll-container) { border-color: var(--border); box-shadow: none; }
.affiliate-invites-workspace :deep(.affiliate-records-table) { scrollbar-gutter: stable; }
.affiliate-invites-workspace :deep(.affiliate-records-table td) { padding-top: 12px; padding-bottom: 12px; }
.affiliate-invites-workspace :deep(.affiliate-records-table td:nth-last-child(2)) { font-variant-numeric: tabular-nums; background: color-mix(in srgb, var(--brand) 3%, var(--card)); }
.affiliate-invites-workspace :deep(nav.inline-flex) { display: flex; width: fit-content; }
</style>
