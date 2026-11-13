<template>
  <div v-show="activeTab === 'backup'" class="min-w-0 space-y-4 text-[var(--foreground)]">
    <BackupSettings />
  </div>
</template>

<script setup lang="ts">
import BackupSettings from "@/views/admin/BackupView.vue";
import { useSettingsContext } from "./settingsContext";
const { activeTab } = useSettingsContext();
</script>
