<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useUiId } from '@/components/ui/utils'

defineProps<{ required?: boolean }>()
const from = defineModel<string>('from', { required: true })
const to = defineModel<string>('to', { required: true })
const timezone = defineModel<string>('timezone', { required: true })
const { t } = useI18n()
const timezoneList = useUiId('oh-date-timezones')
const inputClass = 'oh-focus-ring h-9 w-full min-w-0 rounded-oh-md border border-oh-default bg-oh-surface px-2.5 font-mono text-base text-oh-primary sm:text-sm'
</script>

<template>
  <label class="grid min-w-0 gap-1.5 text-xs font-medium text-oh-secondary">
    {{ t('costLedger.filters.from') }}
    <input v-model="from" type="date" :required="required" :class="inputClass" />
  </label>
  <label class="grid min-w-0 gap-1.5 text-xs font-medium text-oh-secondary">
    {{ t('costLedger.filters.to') }}
    <input v-model="to" type="date" :required="required" :class="inputClass" />
  </label>
  <label class="grid min-w-0 gap-1.5 text-xs font-medium text-oh-secondary">
    {{ t('costLedger.filters.timezone') }}
    <input v-model.trim="timezone" type="text" :list="timezoneList" maxlength="80" required :class="inputClass" />
    <datalist :id="timezoneList">
      <option value="UTC" />
      <option value="Asia/Shanghai" />
      <option value="Asia/Tokyo" />
      <option value="Europe/London" />
      <option value="America/New_York" />
      <option value="America/Los_Angeles" />
    </datalist>
  </label>
</template>
