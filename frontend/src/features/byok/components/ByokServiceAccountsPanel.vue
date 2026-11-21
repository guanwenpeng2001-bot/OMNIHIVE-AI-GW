<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import CopyButton from '@/components/ui/CopyButton.vue'
import Input from '@/components/ui/Input.vue'
import { pushUiToast } from '@/components/ui/useToast'
import ConsoleAsyncBanner from '@/features/console/components/ConsoleAsyncBanner.vue'
import { classifyConsoleError, type ConsoleAsyncNotice } from '@/features/console/asyncState'
import type { IdentityKey } from '@/features/console/scope'
import { byokApi, type ByokServiceAccount } from '../api'
import { formatByokTime } from '../format'

const props = defineProps<{
  workspaceId: string
  projectId: string
  projectKeys: IdentityKey[]
}>()

const { locale } = useI18n()
const isZh = computed(() => String(locale?.value ?? '').startsWith('zh'))

const selectedKeyId = ref('')
const statusRecord = ref<ByokServiceAccount | null>(null)
const busy = ref(false)
const notice = ref<ConsoleAsyncNotice | null>(null)

watch(
  () => props.projectKeys,
  (keys) => {
    if (!selectedKeyId.value && keys.length > 0) {
      selectedKeyId.value = keys[0].id
    }
  },
  { immediate: true }
)

async function inspectDesignation(): Promise<void> {
  const keyId = selectedKeyId.value.trim()
  if (!keyId || !props.workspaceId || !props.projectId || busy.value) return
  busy.value = true
  notice.value = null
  try {
    statusRecord.value = await byokApi.getProjectServiceAccount(
      props.workspaceId,
      props.projectId,
      keyId
    )
  } catch (err) {
    statusRecord.value = null
    notice.value = classifyConsoleError(err, {
      isZh: isZh.value,
      requiredGrantFallback: 'credentials.byok.manage'
    })
  } finally {
    busy.value = false
  }
}

async function registerDesignation(): Promise<void> {
  const keyId = selectedKeyId.value.trim()
  if (!keyId || !props.workspaceId || !props.projectId || busy.value) return
  busy.value = true
  notice.value = null
  try {
    statusRecord.value = await byokApi.registerProjectServiceAccount(
      props.workspaceId,
      props.projectId,
      keyId
    )
    pushUiToast({
      tone: 'success',
      title: isZh.value ? '已注册为项目服务账号主体' : 'Service Account Registered',
      description: `Principal #${statusRecord.value.id}`
    })
  } catch (err) {
    notice.value = classifyConsoleError(err, {
      isZh: isZh.value,
      requiredGrantFallback: 'credentials.byok.manage'
    })
  } finally {
    busy.value = false
  }
}

async function revokeDesignation(): Promise<void> {
  const keyId = selectedKeyId.value.trim()
  if (!keyId || !props.workspaceId || !props.projectId || busy.value) return
  busy.value = true
  notice.value = null
  try {
    statusRecord.value = await byokApi.revokeProjectServiceAccount(
      props.workspaceId,
      props.projectId,
      keyId
    )
    pushUiToast({
      tone: 'info',
      title: isZh.value ? '已撤销服务账号主体资格' : 'Service Account Designation Revoked'
    })
  } catch (err) {
    notice.value = classifyConsoleError(err, {
      isZh: isZh.value,
      requiredGrantFallback: 'credentials.byok.manage'
    })
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section
    data-testid="byok-service-accounts-panel"
    class="space-y-3.5 rounded-oh-lg border border-oh-default bg-oh-surface p-4 sm:p-5"
  >
    <div class="flex flex-wrap items-start justify-between gap-2">
      <div>
        <h2 class="text-sm font-semibold text-oh-primary">
          {{ isZh ? '项目服务账号主体绑定 (Service Account Principals)' : 'Project Service Account Principals' }}
        </h2>
        <p class="mt-0.5 text-xs text-oh-secondary">
          {{
            isZh
              ? '将项目 API Key 注册为无人值守的服务账号主体，获取专属主体 ID 以用于 BYOK 策略授权。'
              : 'Designate a project API key as an automated service account principal for BYOK policy authorization.'
          }}
        </p>
      </div>
    </div>

    <div class="flex flex-wrap items-end gap-2.5">
      <div v-if="projectKeys.length > 0" class="w-full sm:w-64">
        <label class="mb-1 block text-xs font-medium text-oh-secondary" for="byok-sa-key-select">
          {{ isZh ? '选择项目 API Key' : 'Project API Key' }}
        </label>
        <select
          id="byok-sa-key-select"
          v-model="selectedKeyId"
          class="oh-focus-ring h-9 w-full rounded-oh-md border border-oh-default bg-oh-surface px-3 font-mono text-xs text-oh-primary"
        >
          <option v-for="k in projectKeys" :key="k.id" :value="k.id">
            {{ k.name }} (#{{ k.id }})
          </option>
        </select>
      </div>
      <div class="w-full sm:w-48">
        <Input
          v-model="selectedKeyId"
          mono
          size="md"
          :label="isZh ? '或直接输入 Key ID' : 'Or Key ID'"
          placeholder="e.g. 101"
        />
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <Button variant="secondary" size="md" :disabled="!selectedKeyId.trim()" :loading="busy" @click="inspectDesignation">
          {{ isZh ? '查询状态' : 'Check Status' }}
        </Button>
        <Button variant="primary" size="md" :disabled="!selectedKeyId.trim()" :loading="busy" @click="registerDesignation">
          {{ isZh ? '启用服务账号' : 'Register Principal' }}
        </Button>
        <Button
          v-if="statusRecord?.active"
          variant="danger"
          size="md"
          :loading="busy"
          @click="revokeDesignation"
        >
          {{ isZh ? '撤销主体资格' : 'Revoke Principal' }}
        </Button>
      </div>
    </div>

    <ConsoleAsyncBanner
      v-if="notice"
      :notice="notice"
      dismissible
      :retry-label="isZh ? '重试' : 'Retry'"
      @retry="inspectDesignation"
      @dismiss="notice = null"
    />

    <div
      v-if="statusRecord"
      data-testid="byok-service-account-record"
      class="flex flex-wrap items-center justify-between gap-3 rounded-oh-md border border-oh-subtle bg-oh-subtle/40 px-3.5 py-2.5 font-mono text-xs"
    >
      <div class="flex flex-wrap items-center gap-2.5">
        <Badge :tone="statusRecord.active ? 'success' : 'neutral'" size="xs" variant="subtle" dot>
          {{ statusRecord.active ? (isZh ? '已激活服务账号' : 'Active Principal') : (isZh ? '已停用' : 'Revoked') }}
        </Badge>
        <span class="text-oh-primary">
          {{ isZh ? '服务账号 ID：' : 'Principal ID: ' }}<strong>#{{ statusRecord.id }}</strong>
        </span>
        <CopyButton :value="statusRecord.id" size="xs" :aria-label="isZh ? '复制服务账号 ID' : 'Copy principal ID'" />
        <span class="text-oh-muted">· Key #{{ statusRecord.key_id }}</span>
      </div>
      <span v-if="statusRecord.revoked_at" class="text-[11px] text-oh-muted">
        {{ isZh ? `撤销于 ${formatByokTime(statusRecord.revoked_at)}` : `Revoked ${formatByokTime(statusRecord.revoked_at)}` }}
      </span>
    </div>
  </section>
</template>
