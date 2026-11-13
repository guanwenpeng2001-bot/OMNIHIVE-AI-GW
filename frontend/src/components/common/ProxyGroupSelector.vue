<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { proxyGroupsAPI, type ProxyGroup } from '@/api/admin/proxyGroups'
import { unwrapOAuthProxyGroups } from '@/composables/useOAuthProxyGroups'
const props = defineProps<{ modelValue: number | null; selectedName?: string; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: number | null] }>()
const { t } = useI18n()
const groups = ref<ProxyGroup[]>([])
const loading = ref(false)
const failed = ref(false)
let request = 0
async function load() {
  const current = ++request
  loading.value = true
  failed.value = false
  try {
    const result: ProxyGroup[] = []
    let page = 1
    let total = 0
    do {
      const data: unknown = await proxyGroupsAPI.list(page++, 100, { status: 'active' })
      const items = unwrapOAuthProxyGroups(data)
      result.push(...items)
      total = data && typeof data === 'object' && 'total' in data &&
        typeof data.total === 'number' && Number.isFinite(data.total) ? data.total : 0
      if (!items.length) break
    } while (result.length < total)
    if (props.modelValue && !result.some(g => g.id === props.modelValue)) {
      try { result.unshift(await proxyGroupsAPI.get(props.modelValue)) } catch { /* Preserve the selected ID below. */ }
    }
    if (current === request) groups.value = result
  } catch { if (current === request) failed.value = true }
  finally { if (current === request) loading.value = false }
}
watch(() => props.modelValue, load, { immediate: true })
</script>

<template>
  <div class="min-w-0 space-y-2" :aria-busy="loading">
    <select class="input font-mono text-xs" :value="modelValue ?? ''" :disabled="disabled || loading" :aria-label="t('admin.proxyGroups.chooseGroup')" required @change="emit('update:modelValue', Number(($event.target as HTMLSelectElement).value) || null)">
      <option value="" disabled>{{ t('admin.proxyGroups.chooseGroup') }}</option>
      <option v-if="modelValue && !groups.some(g => g.id === modelValue)" :value="modelValue">{{ selectedName || t('admin.proxyGroups.unavailableGroup', { id: modelValue }) }}</option>
      <option v-for="group in groups" :key="group.id" :value="group.id">{{ group.name }} · {{ t(`admin.proxyGroups.${group.status}`) }} · {{ group.schedulable_member_count }}/{{ group.member_count }}</option>
    </select>
    <p v-if="failed" role="alert" class="rounded-md border border-[var(--border)] bg-[var(--surface-2)] p-2 text-xs text-[var(--bad-text)]">{{ t('admin.proxyGroups.groupLoadFailed') }} <button type="button" class="underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-[var(--ring)]" @click="load">{{ t('admin.proxyGroups.retry') }}</button></p>
  </div>
</template>
