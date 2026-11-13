<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { proxyGroupsAPI, type ProxyGroupBinding } from '@/api/admin/proxyGroups'
import { extractApiErrorMessage } from '@/utils/apiError'
const props = defineProps<{ groupId: number }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const items = ref<ProxyGroupBinding[]>([])
const busy = ref(false)
const confirm = ref(false)
const error = ref('')
const notice = ref('')
async function load() {
  busy.value = true
  error.value = ''
  try { items.value = (await proxyGroupsAPI.bindings(props.groupId)).items }
  catch (err) { error.value = extractApiErrorMessage(err, t('admin.proxyGroups.bindingsFailed')) }
  finally { busy.value = false }
}
async function reset() {
  if (busy.value) return
  confirm.value = false
  busy.value = true
  error.value = ''
  try {
    const result = await proxyGroupsAPI.resetBindings(props.groupId)
    notice.value = t('admin.proxyGroups.resetDone', { count: result.cleared })
    await load()
  } catch (err) { error.value = extractApiErrorMessage(err, t('admin.proxyGroups.bindingsFailed')) }
  finally { busy.value = false }
}
onMounted(load)
</script>

<template>
  <BaseDialog :show="true" :title="t('admin.proxyGroups.bindings')" width="wide" @close="!busy && emit('close')">
    <div class="proxy-group-bindings space-y-4 text-[var(--foreground)]">
    <p class="mb-4 text-sm text-[var(--muted-foreground)]">{{ t('admin.proxyGroups.resetConfirm') }}</p>
    <p v-if="error" role="alert" class="text-[var(--bad-text)]">{{ error }}</p><p v-if="notice" role="status" class="text-sm text-[var(--brand-text)]">{{ notice }}</p>
    <p v-if="busy" role="status">{{ t('common.loading') }}</p>
    <div v-else class="space-y-3"><p v-if="!items.length && !error" class="rounded-lg border border-dashed border-[var(--border)] p-6 text-center text-sm text-[var(--muted-foreground)]">{{ t('admin.proxyGroups.noBindings') }}</p><article v-for="item in items" :key="item.account_id" class="rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-4"><h4 class="break-words text-sm font-semibold">{{ item.account_name }} <span class="font-mono text-xs text-[var(--muted-foreground)]">#{{ item.account_id }}</span></h4><dl class="mt-3 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-2 text-sm"><dt class="text-[var(--muted-foreground)]">{{ t('admin.proxyGroups.node') }}</dt><dd class="break-words">{{ item.proxy_name || (item.proxy_id ? `#${item.proxy_id}` : t('admin.proxyGroups.unbound')) }}</dd><dt class="text-[var(--muted-foreground)]">{{ t('admin.proxyGroups.expires') }}</dt><dd class="tabular-nums">{{ item.expires_at ? new Date(item.expires_at).toLocaleString() : '—' }}</dd><dt class="text-[var(--muted-foreground)]">{{ t('common.status') }}</dt><dd :class="!item.eligible && 'text-[var(--warn-text)]'"><span class="pill pill-ghost"><span class="pulse-dot" :class="item.eligible ? 'bg-[var(--ok)]' : 'bg-[var(--warn)]'" aria-hidden="true" />{{ t(item.eligible ? 'admin.proxyGroups.eligible' : 'admin.proxyGroups.ineligible') }}</span></dd></dl></article></div>
    </div>
    <template #footer><button class="btn btn-secondary" :disabled="busy" @click="load">{{ t('common.refresh') }}</button><button class="btn btn-danger" :disabled="busy || !items.length" @click="confirm = true">{{ t('admin.proxyGroups.reset') }}</button></template>
  </BaseDialog>
  <ConfirmDialog :show="confirm" :title="t('admin.proxyGroups.reset')" :message="t('admin.proxyGroups.resetConfirm')" danger @cancel="confirm = false" @confirm="reset" />
</template>

<style scoped>
:global(.oh-modal-overlay:has(.proxy-group-bindings)) {
  justify-content: flex-end;
  padding: 0;
}

:global(.oh-modal-content:has(.proxy-group-bindings)) {
  width: min(100%, 36rem);
  max-width: 36rem;
  height: 100dvh;
  max-height: 100dvh;
  margin: 0;
  border-radius: 0;
  border-left: 1px solid var(--border);
  background: var(--card);
}
</style>
