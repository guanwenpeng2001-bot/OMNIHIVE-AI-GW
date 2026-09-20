<template>
  <div v-show="activeTab === 'backup'">
    <BackupSettings />
  </div>
</template>

<script setup lang="ts">
import BackupSettings from "@/views/admin/BackupView.vue";
import { useSettingsContext } from "./settingsContext";
const { activeTab } = useSettingsContext();
</script>
