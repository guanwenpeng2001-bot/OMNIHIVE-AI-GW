<script setup lang="ts">
import { computed, defineAsyncComponent, ref, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import { useConsoleScopeStore } from '@/stores/consoleScope'
import { persistConsoleScope, readPersistedConsoleScope } from '@/features/console/scope'
import { isProtectedSharedDefaultProject } from '@/features/members/cas'
import type { IdentityProject, IdentityWorkspace } from '@/features/members/types'
import ProjectAccessRequest from './ProjectAccessRequest.vue'

export interface PersonalRoutingGroupSummary {
  id: number | string
  name?: string
  rate_multiplier?: number
  subscription_type?: string | null
}

const props = withDefaults(
  defineProps<{
    completedStepCount: number
    onboardingProgressPct: number
    onboardingSyncUnavailable?: boolean
    step0Completed?: boolean
    step1Completed?: boolean
    step2Completed?: boolean
    activeApiKeysCount?: number
    totalApiKeysCount?: number
    totalRequestsCount?: number
    userBalance?: number
    availableGroups?: ReadonlyArray<PersonalRoutingGroupSummary>
    availableGroupsLoaded?: boolean
    hasActiveSubscription?: boolean
    hasInactiveSubscription?: boolean
    subscriptionsLoaded?: boolean
    projectProbeState?: 'idle' | 'loading' | 'allowed' | 'denied' | 'unavailable'
    projectActions?: ReadonlyArray<string>
    probeRequestId?: string
  }>(),
  {
    onboardingSyncUnavailable: false,
    step0Completed: false,
    step1Completed: false,
    step2Completed: false,
    activeApiKeysCount: 0,
    totalApiKeysCount: 0,
    totalRequestsCount: 0,
    userBalance: 0,
    availableGroups: () => [],
    availableGroupsLoaded: false,
    hasActiveSubscription: false,
    hasInactiveSubscription: false,
    subscriptionsLoaded: false,
    projectProbeState: 'idle',
    projectActions: () => [],
    probeRequestId: ''
  }
)

const emit = defineEmits<{
  (e: 'hide'): void
  (e: 'toggle-step', index: number): void
  (e: 'expand-connect'): void
  (e: 'navigate', path: string): void
  (e: 'retry-probe'): void
  (e: 'scope-changed'): void
}>()

// Optional reuse of FE-1's shared ScopeGateEmptyState when present in the tree
const fe1ScopeGateModules = import.meta.glob<{ default: Component }>(
  '@/features/console/components/ScopeGateEmptyState.vue'
)
const SharedScopeGateEmptyState = computed<Component | null>(() => {
  const loader = Object.values(fe1ScopeGateModules)[0]
  return loader ? defineAsyncComponent(loader) : null
})

const { t, locale } = useI18n()
const isZh = computed(() => String(locale?.value || 'zh').toLowerCase().startsWith('zh'))

function tr(key: string, zhFallback: string, enFallback: string, params?: Record<string, unknown>): string {
  const translated = params ? t(key, params) : t(key)
  if (translated && translated !== key) return translated
  const raw = isZh.value ? zhFallback : enFallback
  if (!params) return raw
  return raw.replace(/\{(\w+)\}/g, (_, token: string) =>
    params[token] !== undefined ? String(params[token]) : `{${token}}`
  )
}

let consoleScopeStore: ReturnType<typeof useConsoleScopeStore> | null = null
try {
  consoleScopeStore = useConsoleScopeStore()
} catch {
  consoleScopeStore = null
}

const activeBranch = ref<'personal' | 'project'>('personal')
const showScopeChooser = ref(false)
const draftScopeMode = ref<'personal' | 'workspace'>('personal')
const draftWorkspaceId = ref<string>('')
const draftProjectId = ref<string>('')

const activeScope = computed(() => {
  if (consoleScopeStore?.activeScope) {
    return consoleScopeStore.activeScope
  }
  return readPersistedConsoleScope()
})

const isPersonalScope = computed(
  () => activeScope.value.mode === 'personal' || !activeScope.value.workspaceId
    ? true
    : false
)

const isWorkspaceOverviewScope = computed(
  () =>
    activeScope.value.mode === 'workspace' &&
    Boolean(activeScope.value.workspaceId) &&
    !activeScope.value.projectId
)

const isProjectScope = computed(
  () =>
    activeScope.value.mode === 'workspace' &&
    Boolean(activeScope.value.workspaceId) &&
    Boolean(activeScope.value.projectId)
)

const scopesList = computed<IdentityWorkspace[]>(
  () => (consoleScopeStore?.scopes as IdentityWorkspace[] | undefined) ?? []
)

const activeWorkspaceObj = computed<IdentityWorkspace | null>(
  () =>
    scopesList.value.find((w) => w.workspace_id === activeScope.value.workspaceId) ?? null
)

const activeProjectObj = computed<IdentityProject | null>(() => {
  const projects = (activeWorkspaceObj.value?.projects as IdentityProject[] | undefined) ?? []
  return projects.find((p) => p.id === activeScope.value.projectId) ?? null
})

const isSharedDefaultActiveProject = computed(() =>
  isProjectScope.value &&
  isProtectedSharedDefaultProject(activeWorkspaceObj.value, activeProjectObj.value)
)

const activeProjectDisplayLabel = computed(() => {
  if (activeProjectObj.value?.name) {
    return `${activeProjectObj.value.name} (#${activeProjectObj.value.id})`
  }
  if (activeScope.value.projectId) {
    return `#${activeScope.value.projectId}`
  }
  return isZh.value ? '未选择项目' : 'No project selected'
})

// Personal prerequisites diagnostics
const defaultPersonalGroup = computed<PersonalRoutingGroupSummary | null>(
  () => props.availableGroups[0] ?? null
)

const hasFreeGroup = computed(() =>
  props.availableGroups.some(
    (g) => typeof g.rate_multiplier === 'number' && g.rate_multiplier === 0
  )
)

const missingPersonalGroup = computed(
  () => props.availableGroupsLoaded && props.availableGroups.length === 0
)

const inactivePersonalSubscription = computed(
  () =>
    props.subscriptionsLoaded &&
    props.hasInactiveSubscription &&
    !props.hasActiveSubscription
)

const insufficientPersonalBalance = computed(
  () =>
    Number(props.userBalance ?? 0) <= 0 &&
    !props.hasActiveSubscription &&
    !hasFreeGroup.value
)

const personalKeySummaryLabel = computed(() => {
  const keyCount = props.activeApiKeysCount || props.totalApiKeysCount || 0
  const groupPart = defaultPersonalGroup.value?.name
    ? ` · ${defaultPersonalGroup.value.name}${
        typeof defaultPersonalGroup.value.rate_multiplier === 'number'
          ? ` ${defaultPersonalGroup.value.rate_multiplier}x`
          : ''
      }`
    : ''
  return isZh.value
    ? `${keyCount} 把启用${groupPart}`
    : `${keyCount} active${groupPart}`
})

// Project capability checks (fail-closed)
function hasProjectAction(action: string): boolean {
  if (props.projectProbeState !== 'allowed') return false
  return props.projectActions.includes('*') || props.projectActions.includes(action)
}

const canAccessProjectKeys = computed(
  () =>
    isProjectScope.value &&
    !isSharedDefaultActiveProject.value &&
    props.projectProbeState === 'allowed' &&
    (hasProjectAction('keys.read') || hasProjectAction('keys.create'))
)

const canRunProjectPlayground = computed(
  () =>
    isProjectScope.value &&
    !isSharedDefaultActiveProject.value &&
    props.projectProbeState === 'allowed' &&
    hasProjectAction('playground.run')
)

function handleSelectPersonalAndGoToKeys(): void {
  consoleScopeStore?.selectPersonalScope?.()
  persistConsoleScope({ mode: 'personal', workspaceId: null, projectId: null })
  emit('scope-changed')
  emit('navigate', '/keys?create=personal')
}

function handleSwitchToPersonalScope(): void {
  consoleScopeStore?.selectPersonalScope?.()
  persistConsoleScope({ mode: 'personal', workspaceId: null, projectId: null })
  emit('scope-changed')
}

function openScopeChooserModal(): void {
  draftScopeMode.value = isPersonalScope.value ? 'personal' : 'workspace'
  draftWorkspaceId.value = activeScope.value.workspaceId || scopesList.value[0]?.workspace_id || ''
  draftProjectId.value = activeScope.value.projectId || ''
  showScopeChooser.value = true
  if (scopesList.value.length === 0 && typeof consoleScopeStore?.fetchScopes === 'function') {
    void consoleScopeStore.fetchScopes()
  }
}

function cancelScopeChooser(): void {
  // Preserve current selection on cancel
  showScopeChooser.value = false
}

function applyScopeChooser(): void {
  if (draftScopeMode.value === 'personal' || !draftWorkspaceId.value) {
    handleSwitchToPersonalScope()
  } else {
    const projId = draftProjectId.value || null
    consoleScopeStore?.selectWorkspaceScope?.(draftWorkspaceId.value, projId)
    persistConsoleScope({
      mode: 'workspace',
      workspaceId: draftWorkspaceId.value,
      projectId: projId
    })
    emit('scope-changed')
  }
  showScopeChooser.value = false
}

function handleSwitchedFromAccessRequest(payload: { workspaceId: string; projectId: string }): void {
  consoleScopeStore?.selectWorkspaceScope?.(payload.workspaceId, payload.projectId)
  persistConsoleScope({
    mode: 'workspace',
    workspaceId: payload.workspaceId,
    projectId: payload.projectId
  })
  emit('scope-changed')
}

function switchToProjectBranchForRequest(): void {
  activeBranch.value = 'project'
}
</script>

<template>
  <section
    id="getStarted"
    data-testid="first-run-checklist"
    class="card p-4 space-y-3"
    :aria-label="tr('onboarding.checklist.title', '开始使用 OmniHive', 'Get Started with OmniHive')"
  >
    <div class="flex items-center justify-between gap-2">
      <h2 class="font-semibold text-[var(--foreground)]">
        {{ tr('onboarding.checklist.title', '开始使用 OmniHive', 'Get Started with OmniHive') }}
      </h2>
      <div class="flex items-center gap-2">
        <span class="font-mono text-xs opacity-60">
          {{ isZh ? `${completedStepCount} / 3 已完成` : `${completedStepCount} / 3 Completed` }}
        </span>
        <button
          type="button"
          class="btn btn-ghost btn-sm text-xs"
          @click="emit('hide')"
        >
          {{ isZh ? '隐藏' : 'Hide' }}
        </button>
      </div>
    </div>

    <p
      v-if="onboardingSyncUnavailable"
      role="status"
      data-testid="onboarding-sync-unavailable"
      class="text-xs text-[var(--oh-ink-tertiary,var(--muted-foreground))]"
    >
      {{ isZh ? '引导进度云端同步暂不可用' : 'Onboarding progress sync unavailable' }}
    </p>

    <div
      class="meter"
      role="progressbar"
      :aria-label="isZh ? '上手进度' : 'Onboarding progress'"
      :aria-valuenow="completedStepCount"
      :aria-valuemin="0"
      :aria-valuemax="3"
    >
      <span :style="{ width: onboardingProgressPct + '%' }" />
    </div>

    <!-- 2-Branch Tab Switcher -->
    <div
      role="tablist"
      class="grid grid-cols-2 gap-1.5 rounded-lg border border-[var(--border)] bg-[var(--surface-2,rgba(0,0,0,0.03))] p-1"
    >
      <button
        type="button"
        role="tab"
        data-testid="onboarding-branch-personal"
        :aria-selected="activeBranch === 'personal'"
        class="rounded-md px-2.5 py-1.5 text-xs font-semibold transition-colors"
        :class="
          activeBranch === 'personal'
            ? 'bg-[var(--card)] text-[var(--foreground)] shadow-sm'
            : 'text-[var(--muted-foreground)] hover:text-[var(--foreground)]'
        "
        @click="activeBranch = 'personal'"
      >
        {{ tr('onboarding.checklist.branchPersonal', '个人自助调用 (推荐)', 'Personal Self-Service (Recommended)') }}
      </button>
      <button
        type="button"
        role="tab"
        data-testid="onboarding-branch-project"
        :aria-selected="activeBranch === 'project'"
        class="rounded-md px-2.5 py-1.5 text-xs font-semibold transition-colors"
        :class="
          activeBranch === 'project'
            ? 'bg-[var(--card)] text-[var(--foreground)] shadow-sm'
            : 'text-[var(--muted-foreground)] hover:text-[var(--foreground)]'
        "
        @click="activeBranch = 'project'"
      >
        {{ tr('onboarding.checklist.branchProject', '团队项目协作', 'Team Project Collaboration') }}
      </button>
    </div>

    <!-- ==================== BRANCH 1: PERSONAL SELF-SERVICE ==================== -->
    <div v-if="activeBranch === 'personal'" data-testid="personal-branch-panel" class="space-y-3">
      <div
        data-testid="personal-scope-attribution"
        class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-[var(--border)] bg-[var(--surface-2,rgba(0,0,0,0.02))] px-3 py-2 text-xs"
      >
        <span class="font-medium text-[var(--foreground)]">
          {{ t('scope.personal.badge') }}
        </span>
        <button
          v-if="!isPersonalScope"
          type="button"
          data-testid="onboarding-switch-personal-btn"
          class="btn btn-secondary btn-sm px-2 py-0.5 text-[11px]"
          @click="handleSwitchToPersonalScope"
        >
          {{ tr('onboarding.checklist.switchToPersonalBtn', '切回个人空间', 'Switch to Personal Space') }}
        </button>
      </div>

      <!-- Real Personal Prerequisite Guardrails (Missing Group / Inactive Subscription / $0.00 Balance) -->
      <div
        v-if="missingPersonalGroup"
        data-testid="personal-prereq-no-group"
        role="alert"
        class="rounded-lg border border-amber-500/40 bg-amber-500/10 p-2.5 text-xs space-y-1.5"
      >
        <p class="font-semibold text-[var(--foreground)]">
          {{ tr('onboarding.checklist.prereqNoGroup', '尚未分配可用路由分组', 'No available routing group assigned') }}
        </p>
        <div class="flex flex-wrap gap-2">
          <button
            type="button"
            data-testid="prereq-group-redeem-cta"
            class="btn btn-secondary btn-sm px-2 py-0.5 text-[11px]"
            @click="emit('navigate', '/redeem')"
          >
            {{ tr('onboarding.checklist.ctaRedeem', '兑换码激活', 'Redeem Code') }}
          </button>
          <button
            type="button"
            data-testid="prereq-group-purchase-cta"
            class="btn btn-secondary btn-sm px-2 py-0.5 text-[11px]"
            @click="emit('navigate', '/purchase')"
          >
            {{ tr('onboarding.checklist.ctaPurchase', '充值或购买套餐', 'Top Up or Buy Plan') }}
          </button>
        </div>
      </div>

      <div
        v-if="inactivePersonalSubscription"
        data-testid="personal-prereq-inactive-sub"
        role="alert"
        class="rounded-lg border border-amber-500/40 bg-amber-500/10 p-2.5 text-xs space-y-1.5"
      >
        <p class="font-semibold text-[var(--foreground)]">
          {{ tr('onboarding.checklist.prereqInactiveSub', '当前订阅未生效或已过期', 'Subscription is inactive or expired') }}
        </p>
        <div class="flex flex-wrap gap-2">
          <button
            type="button"
            data-testid="prereq-sub-subscriptions-cta"
            class="btn btn-secondary btn-sm px-2 py-0.5 text-[11px]"
            @click="emit('navigate', '/subscriptions')"
          >
            {{ tr('onboarding.checklist.ctaSubscriptions', '查看我的订阅', 'My Subscriptions') }}
          </button>
          <button
            type="button"
            data-testid="prereq-sub-redeem-cta"
            class="btn btn-secondary btn-sm px-2 py-0.5 text-[11px]"
            @click="emit('navigate', '/redeem')"
          >
            {{ tr('onboarding.checklist.ctaRedeem', '兑换码激活', 'Redeem Code') }}
          </button>
        </div>
      </div>

      <div
        v-if="insufficientPersonalBalance"
        data-testid="personal-prereq-no-balance"
        role="alert"
        class="rounded-lg border border-amber-500/40 bg-amber-500/10 p-2.5 text-xs space-y-1.5"
      >
        <p class="font-semibold text-[var(--foreground)]">
          {{ tr('onboarding.checklist.prereqNoBalance', '个人余额不足 ($0.00)', 'Insufficient personal balance ($0.00)') }}
        </p>
        <div class="flex flex-wrap gap-2">
          <button
            type="button"
            data-testid="prereq-balance-purchase-cta"
            class="btn btn-secondary btn-sm px-2 py-0.5 text-[11px]"
            @click="emit('navigate', '/purchase')"
          >
            {{ tr('onboarding.checklist.ctaPurchase', '充值或购买套餐', 'Top Up or Buy Plan') }}
          </button>
          <button
            type="button"
            data-testid="prereq-balance-redeem-cta"
            class="btn btn-secondary btn-sm px-2 py-0.5 text-[11px]"
            @click="emit('navigate', '/redeem')"
          >
            {{ tr('onboarding.checklist.ctaRedeem', '兑换码激活', 'Redeem Code') }}
          </button>
        </div>
      </div>

      <!-- Step 1: Create Personal API Key -->
      <div
        data-testid="personal-step-1"
        class="rounded-lg border border-[var(--border)] bg-[var(--card)] p-3 space-y-1.5"
      >
        <div class="flex items-center justify-between gap-2">
          <div class="flex items-center gap-2">
            <span
              class="inline-flex h-4 w-4 items-center justify-center rounded-full font-mono text-[10px] font-bold"
              :class="
                step1Completed
                  ? 'bg-emerald-500/20 text-emerald-600 dark:text-emerald-400'
                  : 'border border-[var(--border)] text-[var(--muted-foreground)]'
              "
            >
              {{ step1Completed ? '✓' : '1' }}
            </span>
            <h3 class="text-xs font-semibold text-[var(--foreground)]">
              {{ tr('onboarding.checklist.personalStep1Title', '1. 创建个人 API 密钥', '1. Create a Personal API Key') }}
            </h3>
          </div>
          <span class="font-mono text-[11px] text-[var(--muted-foreground)]">
            {{ personalKeySummaryLabel }}
          </span>
        </div>
        <p class="text-[11px] text-[var(--muted-foreground)]">
          {{ tr('onboarding.checklist.personalStep1Desc', '个人空间下无需项目审批，签发个人 API 密钥即可直接调用网关。', 'Create a personal key for your own calls in Personal Space without waiting for project approval.') }}
        </p>
        <div class="flex flex-wrap items-center gap-2 pt-1">
          <button
            type="button"
            data-testid="personal-step-1-cta"
            class="btn btn-primary btn-sm px-2.5 py-1 text-xs"
            @click="handleSelectPersonalAndGoToKeys"
          >
            {{ tr('onboarding.checklist.personalStep1Cta', '管理个人密钥 →', 'Manage Personal Keys →') }}
          </button>
          <button
            type="button"
            class="btn btn-ghost btn-sm px-2 py-1 text-[11px]"
            @click="emit('toggle-step', 1)"
          >
            {{ step1Completed ? (isZh ? '撤销完成' : 'Undo') : (isZh ? '标记完成' : 'Mark Done') }}
          </button>
        </div>
      </div>

      <!-- Step 2: Configure Client / Copy cURL or SDK Snippet -->
      <div
        data-testid="personal-step-2"
        class="rounded-lg border border-[var(--border)] bg-[var(--card)] p-3 space-y-1.5"
      >
        <div class="flex items-center gap-2">
          <span
            class="inline-flex h-4 w-4 items-center justify-center rounded-full font-mono text-[10px] font-bold"
            :class="
              step0Completed
                ? 'bg-emerald-500/20 text-emerald-600 dark:text-emerald-400'
                : 'border border-[var(--border)] text-[var(--muted-foreground)]'
            "
          >
            {{ step0Completed ? '✓' : '2' }}
          </span>
          <h3 class="text-xs font-semibold text-[var(--foreground)]">
            {{ tr('onboarding.checklist.personalStep2Title', '2. 配置客户端或复制命令 (cURL/Claude/Codex)', '2. Configure Client or Copy Snippet (cURL/Claude/Codex)') }}
          </h3>
        </div>
        <p class="text-[11px] text-[var(--muted-foreground)]">
          {{ tr('onboarding.checklist.personalStep2Desc', '复制本站网关地址与凭证安全的代码片段，一键接入主流客户端。', 'Copy the gateway endpoint and credential-safe SDK snippet for your client.') }}
        </p>
        <div class="flex flex-wrap items-center gap-2 pt-1">
          <button
            type="button"
            data-testid="personal-step-2-expand"
            class="btn btn-secondary btn-sm px-2.5 py-1 text-xs"
            @click="emit('expand-connect')"
          >
            {{ tr('onboarding.checklist.personalStep2ExpandCta', '展开配置', 'Expand Config') }}
          </button>
          <button
            type="button"
            data-testid="personal-step-2-toggle"
            class="btn btn-ghost btn-sm px-2 py-1 text-[11px]"
            @click="emit('toggle-step', 0)"
          >
            {{ step0Completed ? (isZh ? '撤销完成' : 'Undo') : (isZh ? '完成' : 'Mark Done') }}
          </button>
        </div>
      </div>

      <!-- Step 3: First Gateway Call & Settlement Receipt -->
      <div
        data-testid="personal-step-3"
        class="rounded-lg border border-[var(--border)] bg-[var(--card)] p-3 space-y-1.5"
      >
        <div class="flex items-center gap-2">
          <span
            class="inline-flex h-4 w-4 items-center justify-center rounded-full font-mono text-[10px] font-bold"
            :class="
              step2Completed
                ? 'bg-emerald-500/20 text-emerald-600 dark:text-emerald-400'
                : 'border border-[var(--border)] text-[var(--muted-foreground)]'
            "
          >
            {{ step2Completed ? '✓' : '3' }}
          </span>
          <h3 class="text-xs font-semibold text-[var(--foreground)]">
            {{ tr('onboarding.checklist.personalStep3Title', '3. 发起首次调用并核对账单回执', '3. Make First Call & Verify Settlement Receipt') }}
          </h3>
        </div>
        <p class="text-[11px] text-[var(--muted-foreground)]">
          {{ tr('onboarding.checklist.personalStep3Desc', '首条网关请求完成后自动打勾，可实时核对 Token 用量、延迟与实收结算价。', 'Completes automatically after your first call; inspect live token usage, latency, and settlement amount.') }}
        </p>
        <div class="flex flex-wrap items-center gap-2 pt-1">
          <button
            type="button"
            data-testid="personal-step-3-cta"
            class="btn btn-secondary btn-sm px-2.5 py-1 text-xs"
            @click="emit('navigate', '/requests')"
          >
            {{ tr('onboarding.checklist.personalStep3Cta', '请求记录 →', 'Request Records →') }}
          </button>
          <button
            type="button"
            class="btn btn-ghost btn-sm px-2 py-1 text-[11px]"
            @click="emit('toggle-step', 2)"
          >
            {{ step2Completed ? (isZh ? '撤销完成' : 'Undo') : (isZh ? '标记完成' : 'Mark Done') }}
          </button>
        </div>
      </div>

      <!-- Footer prompt to Project Access Request -->
      <div class="flex flex-wrap items-center justify-between gap-2 border-t border-[var(--border)] pt-2.5 text-xs">
        <span class="text-[var(--muted-foreground)]">
          {{ tr('onboarding.checklist.footerPrompt', '需要团队项目预算或模型调试台 (Playground)？', 'Need team project budget or Model Playground?') }}
        </span>
        <button
          type="button"
          data-testid="open-project-access-request-btn"
          class="font-semibold text-[var(--brand,#d97706)] hover:underline"
          @click="switchToProjectBranchForRequest"
        >
          {{ tr('onboarding.checklist.footerRequestCta', '向管理员申请项目权限 →', 'Request Project Access from Admin →') }}
        </button>
      </div>
    </div>

    <!-- ==================== BRANCH 2: TEAM PROJECT COLLABORATION ==================== -->
    <div v-else data-testid="project-branch-panel" class="space-y-3">
      <!-- Current Scope Status & Scope Switcher Trigger -->
      <div
        data-testid="project-scope-attribution"
        class="rounded-lg border border-[var(--border)] bg-[var(--surface-2,rgba(0,0,0,0.02))] p-3 text-xs space-y-2"
      >
        <div class="flex flex-wrap items-center justify-between gap-2">
          <span class="font-medium text-[var(--foreground)]">
            <template v-if="isPersonalScope">
              {{ t('scope.personal.badge') }}
            </template>
            <template v-else-if="isWorkspaceOverviewScope">
              {{ t('scope.workspace.overview') }}
            </template>
            <template v-else-if="isSharedDefaultActiveProject">
              {{ t('scope.project.defaultShared') }}
            </template>
            <template v-else>
              {{ activeWorkspaceObj?.name || `#${activeScope.workspaceId}` }} / {{ activeProjectDisplayLabel }}
            </template>
          </span>
          <button
            type="button"
            data-testid="onboarding-scope-chooser-toggle"
            class="btn btn-secondary btn-sm px-2.5 py-0.5 text-[11px]"
            @click="openScopeChooserModal"
          >
            {{ tr('onboarding.checklist.switchScopeBtn', '切换作用域', 'Switch Scope') }}
          </button>
        </div>

        <!-- Inline Scope Chooser (Preserves current selection on cancel) -->
        <div
          v-if="showScopeChooser"
          data-testid="onboarding-scope-switcher"
          class="rounded-lg border border-[var(--border)] bg-[var(--card)] p-3 space-y-2.5"
        >
          <div class="flex items-center gap-3">
            <label class="flex items-center gap-1.5 text-xs">
              <input
                v-model="draftScopeMode"
                type="radio"
                value="personal"
                data-testid="chooser-mode-personal"
              />
              <span>{{ isZh ? '个人空间 (PER)' : 'Personal Space (PER)' }}</span>
            </label>
            <label class="flex items-center gap-1.5 text-xs">
              <input
                v-model="draftScopeMode"
                type="radio"
                value="workspace"
                data-testid="chooser-mode-workspace"
              />
              <span>{{ isZh ? '工作空间 / 项目' : 'Workspace / Project' }}</span>
            </label>
          </div>

          <div v-if="draftScopeMode === 'workspace'" class="grid gap-2 sm:grid-cols-2">
            <select
              v-model="draftWorkspaceId"
              data-testid="chooser-workspace-select"
              class="input text-xs"
              :aria-label="isZh ? '选择工作空间' : 'Select workspace'"
            >
              <option
                v-for="ws in scopesList"
                :key="ws.workspace_id"
                :value="ws.workspace_id"
              >
                {{ ws.name }} (#{{ ws.workspace_id }})
              </option>
            </select>
            <select
              v-model="draftProjectId"
              data-testid="chooser-project-select"
              class="input text-xs"
              :aria-label="isZh ? '选择项目' : 'Select project'"
            >
              <option value="">
                {{ isZh ? '全部项目（工作空间总览）' : 'All Projects (Workspace Overview)' }}
              </option>
              <option
                v-for="proj in scopesList.find((w) => w.workspace_id === draftWorkspaceId)?.projects ?? []"
                :key="proj.id"
                :value="proj.id"
              >
                {{ proj.name }} (#{{ proj.id }})
              </option>
            </select>
          </div>

          <div class="flex justify-end gap-2">
            <button
              type="button"
              data-testid="chooser-cancel-btn"
              class="btn btn-ghost btn-sm text-xs"
              @click="cancelScopeChooser"
            >
              {{ isZh ? '取消' : 'Cancel' }}
            </button>
            <button
              type="button"
              data-testid="chooser-apply-btn"
              class="btn btn-primary btn-sm text-xs"
              @click="applyScopeChooser"
            >
              {{ isZh ? '应用作用域' : 'Apply Scope' }}
            </button>
          </div>
        </div>
      </div>

      <!-- Step 1: Request or Select Authorized Team Project -->
      <div
        data-testid="project-step-1"
        class="rounded-lg border border-[var(--border)] bg-[var(--card)] p-3 space-y-2"
      >
        <h3 class="text-xs font-semibold text-[var(--foreground)]">
          {{ tr('onboarding.checklist.projectStep1Title', '1. 申请或选择已授权团队项目', '1. Request or Select an Authorized Team Project') }}
        </h3>
        <p class="text-[11px] text-[var(--muted-foreground)]">
          {{ tr('onboarding.checklist.projectStep1Desc', '选择已加入的工作空间提交项目访问申请，或切换至已获授权的团队项目。', 'Submit a project access request in your workspace or switch to an authorized team project.') }}
        </p>
        <ProjectAccessRequest
          mode="inline"
          :initial-workspace-id="activeScope.workspaceId || ''"
          @switched-project="handleSwitchedFromAccessRequest"
        />
      </div>

      <!-- Probe Unavailable (503 / Timeout) Banner -->
      <div
        v-if="isProjectScope && projectProbeState === 'unavailable'"
        data-testid="project-probe-unavailable-notice"
        role="alert"
        class="rounded-lg border border-amber-500/40 bg-amber-500/10 p-3 text-xs space-y-2"
      >
        <p class="font-medium text-[var(--foreground)]">
          {{ t('rbac.probe.unavailable', { reqId: probeRequestId || 'unknown' }) }}
        </p>
        <div class="flex flex-wrap gap-2">
          <button
            type="button"
            data-testid="retry-project-probe-btn"
            class="btn btn-primary btn-sm px-2.5 py-1 text-xs"
            @click="emit('retry-probe')"
          >
            {{ isZh ? '立即重试' : 'Retry Now' }}
          </button>
          <button
            type="button"
            class="btn btn-secondary btn-sm px-2.5 py-1 text-xs"
            @click="handleSwitchToPersonalScope"
          >
            {{ tr('onboarding.checklist.switchToPersonalBtn', '切回个人空间', 'Switch to Personal Space') }}
          </button>
        </div>
      </div>

      <!-- Step 2: Obtain or Issue Project API Key -->
      <div
        data-testid="project-step-2"
        class="rounded-lg border border-[var(--border)] bg-[var(--card)] p-3 space-y-1.5"
      >
        <h3 class="text-xs font-semibold text-[var(--foreground)]">
          {{ tr('onboarding.checklist.projectStep2Title', '2. 获取或签发项目 API 密钥', '2. Obtain or Issue a Project API Key') }}
        </h3>
        <p class="text-[11px] text-[var(--muted-foreground)]">
          {{ tr('onboarding.checklist.projectStep2Desc', '在已获 keys.read / keys.create 授权的团队项目内查看或签发受限项目密钥。', 'List or issue scoped project keys inside a team project with keys.read / keys.create.') }}
        </p>
        <p
          v-if="isProjectScope && (projectProbeState === 'denied' || isSharedDefaultActiveProject || (projectProbeState === 'allowed' && !canAccessProjectKeys))"
          data-testid="project-keys-denied-notice"
          class="rounded border border-amber-500/30 bg-amber-500/10 px-2.5 py-1.5 text-[11px] text-[var(--foreground)]"
        >
          {{
            t('rbac.keys.deniedProject', {
              project: activeProjectDisplayLabel,
              grant: 'keys.read / keys.create'
            })
          }}
        </p>
        <div class="pt-1">
          <button
            type="button"
            data-testid="project-step-2-cta"
            class="btn btn-secondary btn-sm px-2.5 py-1 text-xs"
            :disabled="!canAccessProjectKeys"
            @click="emit('navigate', '/keys')"
          >
            {{ tr('onboarding.checklist.projectStep2Cta', '前往项目密钥 →', 'Go to Project Keys →') }}
          </button>
        </div>
      </div>

      <!-- Step 3: Quote & Run in Model Playground -->
      <div
        data-testid="project-step-3"
        class="rounded-lg border border-[var(--border)] bg-[var(--card)] p-3 space-y-1.5"
      >
        <h3 class="text-xs font-semibold text-[var(--foreground)]">
          {{ tr('onboarding.checklist.projectStep3Title', '3. 在模型调试台预览报价并运行', '3. Quote & Run in Model Playground') }}
        </h3>
        <p class="text-[11px] text-[var(--muted-foreground)]">
          {{ tr('onboarding.checklist.projectStep3Desc', '模型调试台绑定项目预算冻结与权威销售价，需在具备 playground.run 的团队项目运行。', 'Playground binds to team project budget reservation and authoritative Sale Rate (requires playground.run).') }}
        </p>
        <component
          :is="SharedScopeGateEmptyState"
          v-if="!canRunProjectPlayground && SharedScopeGateEmptyState"
          surface="playground"
          required-grant="playground.run"
        />
        <p
          v-else-if="!canRunProjectPlayground"
          data-testid="project-playground-denied-notice"
          class="rounded border border-amber-500/30 bg-amber-500/10 px-2.5 py-1.5 text-[11px] text-[var(--foreground)]"
        >
          {{ t('rbac.playground.denied') }}
        </p>
        <div class="pt-1">
          <button
            type="button"
            data-testid="project-step-3-cta"
            class="btn btn-primary btn-sm px-2.5 py-1 text-xs"
            :disabled="!canRunProjectPlayground"
            @click="emit('navigate', '/playground')"
          >
            {{ tr('onboarding.checklist.projectStep3Cta', '打开模型调试台 →', 'Open Model Playground →') }}
          </button>
        </div>
      </div>
    </div>
  </section>
</template>
