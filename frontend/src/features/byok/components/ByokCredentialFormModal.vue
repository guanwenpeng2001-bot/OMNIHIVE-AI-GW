<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import Input from '@/components/ui/Input.vue'
import Modal from '@/components/ui/Modal.vue'
import Switch from '@/components/ui/Switch.vue'
import ConsoleAsyncBanner from '@/features/console/components/ConsoleAsyncBanner.vue'
import type { ConsoleAsyncNotice } from '@/features/console/asyncState'
import type { IdentityKey, IdentityProjectSummary } from '@/features/console/scope'
import type {
  ByokAuthType,
  ByokClass,
  ByokCreatePayload,
  ByokCredential,
  ByokPolicyPayload
} from '../api'
import { byokAuthTypeLabel, isPositiveDecimalIdList, parseTokenList } from '../format'

const props = defineProps<{
  open: boolean
  mode: 'create' | 'policy'
  scopeLevel: 'workspace' | 'project'
  workspaceId: string
  projectId: string
  actorUserId?: string
  providerClasses: ByokClass[]
  availableProjects: IdentityProjectSummary[]
  projectKeys: IdentityKey[]
  credential?: ByokCredential | null
  saving?: boolean
  errorNotice?: ConsoleAsyncNotice | null
}>()

const emit = defineEmits<{
  'update:open': [open: boolean]
  'submit-create': [payload: ByokCreatePayload]
  'submit-policy': [payload: { id: string; version: string; policy: ByokPolicyPayload }]
  'reload-conflict': []
}>()

const { locale } = useI18n()
const isZh = computed(() => String(locale?.value ?? '').startsWith('zh'))

const selectedClassKey = ref('')
const providerInput = ref('openai')
const authTypeInput = ref<ByokAuthType>('api_key')
const secretInput = ref('')
const selectedProjectIds = ref<string[]>([])
const extraProjectIdsRaw = ref('')
const memberIdsRaw = ref('')
const serviceAccountIdsRaw = ref('')
const selectedKeyIds = ref<string[]>([])
const extraKeyIdsRaw = ref('')
const modelIdsRaw = ref('')
const priorityInput = ref<number>(10)
const allowPlatformFallback = ref(false)
const includeExternalInBudget = ref(false)
const validationError = ref('')

const enabledClasses = computed(() =>
  props.providerClasses.filter((item) => item.supported && item.enabled)
)
const createBlockedByPolicy = computed(
  () => props.mode === 'create' && enabledClasses.value.length === 0
)

const authTypeOptions: ByokAuthType[] = [
  'api_key',
  'oauth',
  'setup-token',
  'bedrock',
  'service_account'
]

function initFromProps(): void {
  validationError.value = ''
  secretInput.value = ''

  if (props.mode === 'policy' && props.credential) {
    const cred = props.credential
    providerInput.value = cred.provider
    authTypeInput.value = cred.auth_type
    selectedClassKey.value = `${cred.provider}::${cred.auth_type}`
    const knownProjIds = new Set(props.availableProjects.map((p) => p.id))
    if (props.scopeLevel === 'project' && props.projectId) {
      selectedProjectIds.value = [props.projectId]
      extraProjectIdsRaw.value = ''
    } else {
      selectedProjectIds.value = cred.scope.project_ids.filter((id) => knownProjIds.has(id))
      extraProjectIdsRaw.value = cred.scope.project_ids
        .filter((id) => !knownProjIds.has(id))
        .join(', ')
    }
    memberIdsRaw.value = (cred.scope.member_ids ?? []).join(', ')
    serviceAccountIdsRaw.value = (cred.scope.service_account_ids ?? []).join(', ')

    const knownKeyIds = new Set(props.projectKeys.map((k) => k.id))
    selectedKeyIds.value = cred.scope.key_ids.filter((id) => knownKeyIds.has(id))
    extraKeyIdsRaw.value = cred.scope.key_ids.filter((id) => !knownKeyIds.has(id)).join(', ')

    modelIdsRaw.value = (cred.scope.model_ids ?? []).join(', ')
    priorityInput.value = cred.priority ?? 0
    allowPlatformFallback.value = Boolean(cred.allow_platform_fallback)
    includeExternalInBudget.value = Boolean(cred.include_external_in_budget)
    return
  }

  const firstClass = enabledClasses.value[0] ?? props.providerClasses[0]
  if (firstClass) {
    providerInput.value = firstClass.provider
    authTypeInput.value = (firstClass.auth_type as ByokAuthType) || 'api_key'
    selectedClassKey.value = `${firstClass.provider}::${firstClass.auth_type}`
  } else {
    providerInput.value = 'openai'
    authTypeInput.value = 'api_key'
    selectedClassKey.value = ''
  }

  if (props.scopeLevel === 'project' && props.projectId) {
    selectedProjectIds.value = [props.projectId]
  } else if (props.projectId) {
    selectedProjectIds.value = [props.projectId]
  } else if (props.availableProjects[0]?.id) {
    selectedProjectIds.value = [props.availableProjects[0].id]
  } else {
    selectedProjectIds.value = []
  }
  extraProjectIdsRaw.value = ''
  memberIdsRaw.value = props.actorUserId && /^[1-9]\d*$/.test(props.actorUserId) ? props.actorUserId : ''
  serviceAccountIdsRaw.value = ''
  selectedKeyIds.value = props.projectKeys[0]?.id ? [props.projectKeys[0].id] : []
  extraKeyIdsRaw.value = ''
  modelIdsRaw.value = ''
  priorityInput.value = 10
  allowPlatformFallback.value = false
  includeExternalInBudget.value = false
}

watch(
  () => [props.open, props.mode, props.credential?.id, props.credential?.version],
  ([isOpen]) => {
    if (isOpen) initFromProps()
  },
  { immediate: true }
)

function selectProviderClass(item: ByokClass): void {
  if (!item.supported || !item.enabled) return
  providerInput.value = item.provider
  authTypeInput.value = item.auth_type as ByokAuthType
  selectedClassKey.value = `${item.provider}::${item.auth_type}`
}

function toggleProjectId(id: string): void {
  selectedProjectIds.value = selectedProjectIds.value.includes(id)
    ? selectedProjectIds.value.filter((item) => item !== id)
    : [...selectedProjectIds.value, id]
}

function toggleKeyId(id: string): void {
  selectedKeyIds.value = selectedKeyIds.value.includes(id)
    ? selectedKeyIds.value.filter((item) => item !== id)
    : [...selectedKeyIds.value, id]
}

function buildPolicy(): ByokPolicyPayload | null {
  const projectIds =
    props.scopeLevel === 'project' && props.projectId
      ? [props.projectId]
      : [...new Set([...selectedProjectIds.value, ...parseTokenList(extraProjectIdsRaw.value)])]

  if (!isPositiveDecimalIdList(projectIds)) {
    validationError.value = isZh.value
      ? '请至少选择一个有效的目标授权项目 ID。'
      : 'Select or enter at least one valid numeric project ID.'
    return null
  }

  const memberIds = parseTokenList(memberIdsRaw.value)
  const serviceAccountIds = parseTokenList(serviceAccountIdsRaw.value)

  if (memberIds.length === 0 && serviceAccountIds.length === 0) {
    validationError.value = isZh.value
      ? '请填写至少一个授权成员 ID 或服务账号 ID。'
      : 'Specify at least one authorized member ID or service account ID.'
    return null
  }
  if (memberIds.length > 0 && !isPositiveDecimalIdList(memberIds)) {
    validationError.value = isZh.value
      ? '授权成员 ID 必须为正整数（多个 ID 用逗号分隔）。'
      : 'Authorized member IDs must be positive integers.'
    return null
  }
  if (serviceAccountIds.length > 0 && !isPositiveDecimalIdList(serviceAccountIds)) {
    validationError.value = isZh.value
      ? '服务账号 ID 必须为正整数（多个 ID 用逗号分隔）。'
      : 'Service account IDs must be positive integers.'
    return null
  }

  const keyIds = [...new Set([...selectedKeyIds.value, ...parseTokenList(extraKeyIdsRaw.value)])]
  if (!isPositiveDecimalIdList(keyIds)) {
    validationError.value = isZh.value
      ? '请至少绑定一个有效的项目 API Key ID。'
      : 'Select or enter at least one valid numeric API Key ID.'
    return null
  }

  const modelIds = parseTokenList(modelIdsRaw.value)
  if (modelIds.length === 0) {
    validationError.value = isZh.value
      ? '请填写至少一个允许调度的模型名称（例如 gpt-5.6-sol 或 deepseek-chat）。'
      : 'Enter at least one allowed model ID.'
    return null
  }

  const prio = Number(priorityInput.value)
  if (!Number.isInteger(prio) || prio < 0 || prio > 1000) {
    validationError.value = isZh.value
      ? '调度优先级必须为 0–1000 之间的整数（数值越小越优先）。'
      : 'Priority must be an integer between 0 and 1000 (lower is preferred).'
    return null
  }

  return {
    allowed_project_ids: projectIds,
    allowed_member_ids: memberIds,
    allowed_service_account_ids: serviceAccountIds.length > 0 ? serviceAccountIds : undefined,
    allowed_key_ids: keyIds,
    allowed_model_ids: modelIds,
    priority: prio,
    allow_platform_fallback: allowPlatformFallback.value,
    include_external_in_budget: includeExternalInBudget.value
  }
}

function handleSubmit(): void {
  validationError.value = ''
  if (createBlockedByPolicy.value) {
    return
  }
  const policy = buildPolicy()
  if (!policy) return

  if (props.mode === 'create') {
    if (!providerInput.value.trim()) {
      validationError.value = isZh.value ? '请选择或输入上游供应商标识。' : 'Provider identifier is required.'
      return
    }
    if (!secretInput.value.trim()) {
      validationError.value = isZh.value ? '请输入真实上游密钥或凭据内容。' : 'Upstream credential secret is required.'
      return
    }
    emit('submit-create', {
      provider: providerInput.value.trim(),
      auth_type: authTypeInput.value,
      secret: secretInput.value.trim(),
      ...policy
    })
    return
  }

  if (props.credential) {
    emit('submit-policy', {
      id: props.credential.id,
      version: props.credential.version,
      policy
    })
  }
}
</script>

<template>
  <Modal
    :open="open"
    size="lg"
    :title="
      mode === 'create'
        ? (scopeLevel === 'project'
            ? (isZh ? '登记项目自带上游凭据 (BYOK)' : 'Enroll Project BYOK Credential')
            : (isZh ? '登记工作空间自带上游凭据 (BYOK)' : 'Enroll Workspace BYOK Credential'))
        : (isZh ? '编辑凭据授权范围与路由策略' : 'Edit Credential Scope & Routing Policy')
    "
    :description="
      mode === 'create'
        ? (isZh
            ? '密钥经信封加密存储，明文不会在接口中回显。登记后建议立即执行上游连通性探测验证。'
            : 'Secrets are envelope-encrypted at rest and never echoed back. Run an upstream verification probe after enrollment.')
        : (isZh
            ? '基于乐观并发锁安全更新授权项目、绑定主体、允许模型与平台回退策略。'
            : 'Update authorized projects, principals, keys, model allowlists, and fallback behavior safely.')
    "
    :close-on-backdrop="!saving"
    :close-on-escape="!saving"
    @update:open="emit('update:open', $event)"
  >
    <form id="byok-credential-form" class="space-y-4 text-sm" @submit.prevent="handleSubmit">
      <ConsoleAsyncBanner
        v-if="errorNotice"
        :notice="errorNotice"
        :retry-label="errorNotice.kind === 'conflict' ? (isZh ? '同步最新版本' : 'Reload Latest') : (isZh ? '重试' : 'Retry')"
        @retry="errorNotice.kind === 'conflict' ? emit('reload-conflict') : handleSubmit()"
      />

      <div
        v-if="validationError"
        role="alert"
        class="rounded-oh-md border border-oh-status-danger-border bg-oh-status-danger-bg px-3.5 py-2.5 text-xs text-oh-primary"
      >
        {{ validationError }}
      </div>

      <template v-if="mode === 'create'">
        <div
          v-if="createBlockedByPolicy"
          data-testid="byok-policy-disabled-card"
          class="space-y-2 rounded-oh-md border border-dashed border-oh-status-warn-border bg-oh-status-warn-bg/60 px-4 py-3.5 text-xs"
        >
          <div class="flex flex-wrap items-center gap-2">
            <Badge tone="warning" size="xs">
              {{ isZh ? '策略未开启' : 'Policy Disabled' }}
            </Badge>
            <span class="font-semibold text-oh-primary">
              {{
                isZh
                  ? '当前工作空间尚未启用自带上游凭据 (BYOK) 供应商类别'
                  : 'No BYOK provider classes are currently enabled for this workspace'
              }}
            </span>
          </div>
          <p class="leading-relaxed text-oh-secondary">
            {{
              isZh
                ? '管理员尚未为当前工作空间开通可用的自有凭据供应商策略，新建凭据提交已禁用。请联系工作空间管理员启用 BYOK 供应商类别后再试。'
                : 'Tenant-owned credential enrollment is unavailable until an administrator enables at least one supported provider class for this workspace.'
            }}
          </p>
          <div v-if="providerClasses.length > 0" class="flex flex-wrap gap-1.5 pt-1">
            <span
              v-for="cls in providerClasses"
              :key="`disabled-${cls.provider}::${cls.auth_type}`"
              class="inline-flex items-center gap-1 rounded-oh-xs border border-oh-default bg-oh-surface px-2 py-0.5 font-mono text-[11px] text-oh-muted"
            >
              <span>{{ cls.provider }} · {{ cls.auth_type }}</span>
              <span>({{ isZh ? '未启用' : 'disabled' }})</span>
            </span>
          </div>
        </div>

        <div v-if="providerClasses.length > 0" class="space-y-1.5">
          <label class="block text-xs font-medium text-oh-secondary">
            {{ isZh ? '支持的上游供应商与认证类型' : 'Supported Provider & Auth Classes' }}
          </label>
          <div class="flex flex-wrap gap-1.5">
            <button
              v-for="cls in providerClasses"
              :key="`${cls.provider}::${cls.auth_type}`"
              type="button"
              :disabled="!cls.supported || !cls.enabled"
              :class="[
                'oh-focus-ring inline-flex items-center gap-1.5 rounded-oh-sm border px-2.5 py-1 font-mono text-xs transition-colors disabled:cursor-not-allowed disabled:opacity-45',
                selectedClassKey === `${cls.provider}::${cls.auth_type}`
                  ? 'border-oh-accent bg-oh-accent-subtle font-semibold text-oh-accent-text'
                  : 'border-oh-default bg-oh-surface text-oh-secondary hover:border-oh-strong hover:text-oh-primary'
              ]"
              @click="selectProviderClass(cls)"
            >
              <span>{{ cls.provider }}</span>
              <span class="text-[10px] text-oh-muted">· {{ cls.auth_type }}</span>
            </button>
          </div>
        </div>

        <div class="grid gap-3 sm:grid-cols-2">
          <Input
            v-model="providerInput"
            mono
            required
            :label="isZh ? '上游供应商标识 (Provider)' : 'Provider Identifier'"
            placeholder="openai / anthropic / gemini / deepseek"
          />
          <div class="space-y-1.5">
            <label class="block text-xs font-medium text-oh-secondary" for="byok-auth-type-select">
              {{ isZh ? '认证方式 (Auth Type)' : 'Authentication Type' }}
            </label>
            <select
              id="byok-auth-type-select"
              v-model="authTypeInput"
              class="oh-focus-ring h-9 w-full rounded-oh-md border border-oh-default bg-oh-surface px-3 font-mono text-xs text-oh-primary"
            >
              <option v-for="at in authTypeOptions" :key="at" :value="at">
                {{ byokAuthTypeLabel(at, isZh) }} ({{ at }})
              </option>
            </select>
          </div>
        </div>

        <Input
          v-model="secretInput"
          type="password"
          revealable
          mono
          required
          autocomplete="off"
          :label="isZh ? '上游密钥 / 凭据明文 (Secret)' : 'Upstream Credential Secret'"
          :placeholder="isZh ? '输入 sk-... 或 JSON 服务账号密钥' : 'Enter sk-... or upstream credential payload'"
          :hint="isZh ? '仅在创建或轮换时单向写入，保存后仅保留不可逆指纹。' : 'Write-only; stored encrypted and represented by an irreversible fingerprint.'"
        />
      </template>

      <div class="space-y-1.5">
        <label class="block text-xs font-medium text-oh-secondary">
          {{ isZh ? '授权项目范围 (Allowed Projects)' : 'Authorized Projects' }}
        </label>
        <div v-if="scopeLevel === 'project' && projectId" class="flex items-center gap-2">
          <Badge tone="accent" variant="subtle" mono>#{{ projectId }}</Badge>
          <span class="text-xs text-oh-muted">
            {{ isZh ? '项目级凭据自动锁定至当前项目作用域' : 'Project-scoped credentials bind exclusively to the active project' }}
          </span>
        </div>
        <template v-else>
          <div v-if="availableProjects.length > 0" class="flex flex-wrap gap-1.5">
            <button
              v-for="proj in availableProjects"
              :key="proj.id"
              type="button"
              :class="[
                'oh-focus-ring rounded-oh-sm border px-2.5 py-1 text-xs transition-colors',
                selectedProjectIds.includes(proj.id)
                  ? 'border-oh-accent bg-oh-accent-subtle font-medium text-oh-accent-text'
                  : 'border-oh-default bg-oh-surface text-oh-secondary hover:border-oh-strong'
              ]"
              @click="toggleProjectId(proj.id)"
            >
              {{ proj.name }} <span class="font-mono text-[11px] text-oh-muted">#{{ proj.id }}</span>
            </button>
          </div>
          <Input
            v-model="extraProjectIdsRaw"
            mono
            size="sm"
            :placeholder="isZh ? '补充项目数字 ID（逗号分隔，例如 1, 2）' : 'Additional numeric project IDs (comma-separated)'"
          />
        </template>
      </div>

      <div class="grid gap-3 sm:grid-cols-2">
        <Input
          v-model="memberIdsRaw"
          mono
          :label="isZh ? '授权成员用户 ID (Member IDs)' : 'Authorized Member IDs'"
          :placeholder="isZh ? '例如 1, 2' : 'e.g. 1, 2'"
          :hint="isZh ? '成员 ID 与服务账号 ID 至少填写一项' : 'At least one member ID or service account ID is required'"
        />
        <Input
          v-model="serviceAccountIdsRaw"
          mono
          :label="isZh ? '授权服务账号 ID (Service Account IDs)' : 'Service Account Principal IDs'"
          :placeholder="isZh ? '可选，填写已注册的服务账号 ID' : 'Optional registered service account IDs'"
        />
      </div>

      <div class="space-y-1.5">
        <label class="block text-xs font-medium text-oh-secondary">
          {{ isZh ? '允许调用的项目 API Key ID' : 'Allowed Project API Key IDs' }}
        </label>
        <div v-if="projectKeys.length > 0" class="flex flex-wrap gap-1.5">
          <button
            v-for="k in projectKeys"
            :key="k.id"
            type="button"
            :class="[
              'oh-focus-ring rounded-oh-sm border px-2.5 py-1 text-xs transition-colors',
              selectedKeyIds.includes(k.id)
                ? 'border-oh-accent bg-oh-accent-subtle font-medium text-oh-accent-text'
                : 'border-oh-default bg-oh-surface text-oh-secondary hover:border-oh-strong'
            ]"
            @click="toggleKeyId(k.id)"
          >
            {{ k.name }} <span class="font-mono text-[11px] text-oh-muted">#{{ k.id }}</span>
          </button>
        </div>
        <Input
          v-model="extraKeyIdsRaw"
          mono
          size="sm"
          :placeholder="isZh ? '输入项目 API Key 数字 ID（逗号分隔，例如 101, 102）' : 'Enter numeric API Key IDs (comma-separated, e.g. 101, 102)'"
        />
      </div>

      <div class="grid gap-3 sm:grid-cols-3">
        <div class="sm:col-span-2">
          <Input
            v-model="modelIdsRaw"
            mono
            required
            :label="isZh ? '允许路由的模型列表 (Allowed Models)' : 'Allowed Model IDs'"
            :placeholder="isZh ? '逗号或空格分隔，例如 gpt-5.6-sol, deepseek-chat' : 'Comma-separated, e.g. gpt-5.6-sol, deepseek-chat'"
          />
        </div>
        <Input
          :model-value="priorityInput"
          type="number"
          mono
          :label="isZh ? '优先级 (0–1000)' : 'Priority (0–1000)'"
          :hint="isZh ? '数值越小越优先匹配' : 'Lower value = higher priority'"
          @update:model-value="priorityInput = Number($event)"
        />
      </div>

      <div class="grid gap-3 rounded-oh-md border border-oh-default bg-oh-subtle/40 p-3.5 sm:grid-cols-2">
        <Switch
          v-model="allowPlatformFallback"
          size="sm"
          :label="isZh ? '允许回退至平台共享池' : 'Allow Platform Pool Fallback'"
          :description="
            isZh
              ? '当自带上游凭据配额耗尽或上游不可用时，允许按项目计费回退至平台上游资源池。'
              : 'Fall back to platform-funded pools when this BYOK credential is unavailable.'
          "
        />
        <Switch
          v-model="includeExternalInBudget"
          size="sm"
          :label="isZh ? '外部估算成本计入项目预算' : 'Count External Spend in Budget'"
          :description="
            isZh
              ? '将该密钥产生的上游参考估算金额纳入项目预算阈值与熔断控制。'
              : 'Include estimated external upstream token spend in project budget enforcement.'
          "
        />
      </div>
    </form>

    <template #footer>
      <Button variant="secondary" :disabled="saving" @click="emit('update:open', false)">
        {{ isZh ? '取消' : 'Cancel' }}
      </Button>
      <Button
        type="submit"
        form="byok-credential-form"
        data-testid="byok-submit-btn"
        :disabled="createBlockedByPolicy || Boolean(saving)"
        :loading="saving"
      >
        {{
          mode === 'create'
            ? (isZh ? '创建并保存密钥' : 'Enroll Credential')
            : (isZh ? '保存策略变更' : 'Save Policy')
        }}
      </Button>
    </template>
  </Modal>
</template>
