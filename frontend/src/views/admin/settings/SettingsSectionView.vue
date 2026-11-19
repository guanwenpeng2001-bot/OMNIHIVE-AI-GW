<script setup lang="ts">
import SettingsWorkbenchPage from '@/features/settings/SettingsWorkbenchPage.vue'
import type { SettingsSectionId } from '@/features/settings/sections'

defineProps<{
  section?: SettingsSectionId
}>()
</script>

<template>
  <SettingsWorkbenchPage :initial-section="section" />
</template>
