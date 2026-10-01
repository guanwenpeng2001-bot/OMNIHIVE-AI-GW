<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { proxyGroupsAPI, type ProxyGroupMember } from '@/api/admin/proxyGroups'
import { list } from '@/api/admin/proxies'
import type { Proxy } from '@/types/admin'
import { extractApiErrorMessage } from '@/utils/apiError'
const props = defineProps<{ groupId: number }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const nodes = ref<Proxy[]>([])
const members = ref<ProxyGroupMember[]>([])
const selected = ref<number[]>([])
const search = ref('')
const status = ref('')
const region = ref('')
const loading = ref(true)
const ready = ref(false)
const busy = ref(false)
const error = ref('')
function nodeStatus(node: Proxy) { return node.expires_at && Date.parse(node.expires_at) <= Date.now() ? 'expired' : node.status }
const regions = computed(() => [...new Set(nodes.value.map(n => n.region || n.country || '').filter(Boolean))].sort())
const filtered = computed(() => nodes.value.filter(n => (!status.value || nodeStatus(n) === status.value) && (!region.value || (n.region || n.country) === region.value) && `${n.name} ${n.host} ${n.region ?? ''} ${n.country ?? ''}`.toLowerCase().includes(search.value.toLowerCase())))
const chosen = computed(() => nodes.value.filter(n => selected.value.includes(n.id)))
function cooldown(id: number) { return members.value.find(m => m.proxy.id === id) }
onMounted(async () => {
  try {
    const detail = await proxyGroupsAPI.get(props.groupId)
    members.value = detail.members
    selected.value = detail.members.map(m => m.proxy.id)
    const all = new Map(detail.members.map(m => [m.proxy.id, m.proxy]))
    let page = 1
    let count = 0
    let total = 0
    do {
      const result = await list(page++, 100)
      result.items.forEach(n => all.set(n.id, n))
      count += result.items.length
      total = result.total
      if (!result.items.length) break
    } while (count < total)
    nodes.value = [...all.values()]
    ready.value = true
  } catch (err) { error.value = extractApiErrorMessage(err, t('admin.proxyGroups.membersLoadFailed')) }
  finally { loading.value = false }
})
async function save() {
  if (!ready.value || busy.value) return
  busy.value = true
  error.value = ''
  try { await proxyGroupsAPI.setMembers(props.groupId, selected.value); emit('saved') }
  catch (err) { error.value = extractApiErrorMessage(err, t('admin.proxyGroups.saveFailed')) }
  finally { busy.value = false }
}
</script>

<template>
  <BaseDialog :show="true" :title="t('admin.proxyGroups.members')" width="extra-wide" @close="!busy && emit('close')">
    <p class="mb-4 text-sm text-gray-500">{{ t('admin.proxyGroups.membersHint') }}</p>
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
    <div v-if="ready" class="grid gap-4 md:grid-cols-[2fr_1fr]">
      <section class="min-w-0 space-y-3">
        <input v-model="search" class="input" :placeholder="t('admin.proxyGroups.memberSearch')" :aria-label="t('admin.proxyGroups.memberSearch')" />
        <div class="flex flex-wrap gap-2"><select v-model="status" class="input flex-1" :aria-label="t('common.status')"><option value="">{{ t('admin.proxyGroups.allStatus') }}</option><option v-for="s in ['active', 'inactive', 'expired']" :key="s" :value="s">{{ t(`admin.proxyGroups.${s}`) }}</option></select><select v-model="region" class="input flex-1" :aria-label="t('admin.proxyGroups.region')"><option value="">{{ t('admin.proxyGroups.allRegions') }}</option><option v-for="r in regions" :key="r">{{ r }}</option></select></div>
        <button class="btn btn-secondary" @click="selected = [...new Set([...selected, ...filtered.map(n => n.id)])]">{{ t('admin.proxyGroups.selectVisible') }}</button>
        <div class="max-h-96 space-y-2 overflow-y-auto">
          <label v-for="node in filtered" :key="node.id" class="flex items-start gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
            <input v-model="selected" type="checkbox" :value="node.id" class="mt-1" />
            <span class="min-w-0 flex-1 space-y-1"><span class="block break-words font-medium">{{ node.name }}</span><span class="block text-sm text-gray-500">{{ node.region || node.country || '—' }} · {{ t(`admin.proxyGroups.${nodeStatus(node)}`) }}</span>
              <span class="block border-t border-gray-100 pt-1 text-xs text-gray-500 dark:border-dark-700">{{ t('admin.proxyGroups.observations') }}: {{ node.quality_status ? t(`admin.proxyGroups.health.${node.quality_status}`) : t('admin.proxyGroups.unknown') }}<span v-if="node.latency_ms != null"> · {{ node.latency_ms }} ms</span></span>
              <span v-if="cooldown(node.id)?.cooldown_until && Date.parse(cooldown(node.id)!.cooldown_until!) > Date.now()" class="block text-xs text-amber-700 dark:text-amber-400">{{ t('admin.proxyGroups.cooldown', { time: new Date(cooldown(node.id)!.cooldown_until!).toLocaleString() }) }} · {{ cooldown(node.id)?.cooldown_reason }}</span>
            </span>
          </label>
          <p v-if="!filtered.length" class="p-4 text-sm text-gray-500">{{ t('admin.proxyGroups.noNodes') }}</p>
        </div>
      </section>
      <section class="min-w-0 space-y-3"><h4 class="font-medium">{{ t('admin.proxyGroups.selected', { count: selected.length }) }}</h4><button class="btn btn-secondary" @click="selected = []">{{ t('admin.proxyGroups.clearSelection') }}</button><ul class="max-h-96 space-y-2 overflow-y-auto"><li v-for="node in chosen" :key="node.id" class="flex items-start justify-between gap-2 text-sm"><span class="break-words">{{ node.name }}</span><button class="shrink-0 underline" @click="selected = selected.filter(id => id !== node.id)">{{ t('common.remove') }}</button></li></ul></section>
    </div>
    <template #footer><button class="btn btn-secondary" :disabled="busy" @click="emit('close')">{{ t('common.cancel') }}</button><button class="btn btn-primary" :disabled="!ready || busy" @click="save">{{ t('common.save') }}</button></template>
  </BaseDialog>
</template>
