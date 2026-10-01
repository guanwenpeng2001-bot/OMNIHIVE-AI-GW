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
    <p class="mb-4 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.proxyGroups.resetConfirm') }}</p>
    <p v-if="error" role="alert" class="text-red-600 dark:text-red-400">{{ error }}</p><p v-if="notice" role="status" class="text-sm text-primary-600 dark:text-primary-400">{{ notice }}</p>
    <p v-if="busy" role="status">{{ t('common.loading') }}</p>
    <div v-else class="space-y-3"><p v-if="!items.length && !error">{{ t('admin.proxyGroups.noBindings') }}</p><article v-for="item in items" :key="item.account_id" class="rounded-lg border border-gray-200 p-3 dark:border-dark-600"><h4 class="break-words font-medium">{{ item.account_name }} #{{ item.account_id }}</h4><dl class="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm"><dt>{{ t('admin.proxyGroups.node') }}</dt><dd class="break-words">{{ item.proxy_name || (item.proxy_id ? `#${item.proxy_id}` : t('admin.proxyGroups.unbound')) }}</dd><dt>{{ t('admin.proxyGroups.expires') }}</dt><dd>{{ item.expires_at ? new Date(item.expires_at).toLocaleString() : '—' }}</dd><dt>{{ t('common.status') }}</dt><dd :class="!item.eligible && 'text-amber-700 dark:text-amber-400'">{{ t(item.eligible ? 'admin.proxyGroups.eligible' : 'admin.proxyGroups.ineligible') }}</dd></dl></article></div>
    <template #footer><button class="btn btn-secondary" :disabled="busy" @click="load">{{ t('common.refresh') }}</button><button class="btn btn-danger" :disabled="busy || !items.length" @click="confirm = true">{{ t('admin.proxyGroups.reset') }}</button></template>
  </BaseDialog>
  <ConfirmDialog :show="confirm" :title="t('admin.proxyGroups.reset')" :message="t('admin.proxyGroups.resetConfirm')" danger @cancel="confirm = false" @confirm="reset" />
</template>
