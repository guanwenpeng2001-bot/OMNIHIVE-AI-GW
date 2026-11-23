<script setup lang="ts">
import { computed, ref, watch, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'

export interface ScopeGateProjectOption {
  workspaceId: string
  workspaceName?: string
  projectId: string
  projectName: string
  isDefaultShared?: boolean
}

export type ScopeGateState = 'denied' | 'empty' | 'unavailable' | 'loading'
export type ScopeGateSurface = 'keys' | 'playground' | 'byok' | 'tasks' | 'dashboard' | 'generic'

const props = withDefaults(
  defineProps<{
    state: ScopeGateState
    surface?: ScopeGateSurface
    title?: string
    description?: string
    workspaceId?: string | null
    workspaceName?: string | null
    projectId?: string | null
    projectName?: string | null
    requiredGrant?: string | null
    requestId?: string | null
    httpStatus?: number | null
    projectOptions?: ScopeGateProjectOption[]
    showPersonalAction?: boolean
    personalActionLabel?: string
    showRequestAccess?: boolean
    requestAccessLabel?: string
    requestAccessComponent?: Component | null
    requestAccessProps?: Record<string, unknown>
    showSwitchProject?: boolean
    switchProjectLabel?: string
    showRetry?: boolean
    retryLabel?: string
    retrying?: boolean
    secondaryActionLabel?: string
    secondaryActionVariant?: 'primary' | 'secondary' | 'ghost'
    compact?: boolean
  }>(),
  {
    surface: 'generic',
    title: undefined,
    description: undefined,
    workspaceId: null,
    workspaceName: null,
    projectId: null,
    projectName: null,
    requiredGrant: null,
    requestId: null,
    httpStatus: null,
    projectOptions: () => [],
    showPersonalAction: true,
    personalActionLabel: undefined,
    showRequestAccess: true,
    requestAccessLabel: undefined,
    requestAccessComponent: null,
    requestAccessProps: () => ({}),
    showSwitchProject: true,
    switchProjectLabel: undefined,
    showRetry: false,
    retryLabel: undefined,
    retrying: false,
    secondaryActionLabel: undefined,
    secondaryActionVariant: 'secondary',
    compact: false
  }
)

const emit = defineEmits<{
  'switch-personal': []
  'request-access': []
  'select-project': [payload: { workspaceId: string; projectId: string }]
  'switch-project': []
  retry: []
  'secondary-action': []
}>()

const { t, locale } = useI18n()
const isZh = computed(() => String(locale?.value ?? 'zh').toLowerCase().startsWith('zh'))

function tr(key: string, params?: Record<string, string>): string {
  return params ? t(key, params) : t(key)
}

const selectedOptionKey = ref('')

function encodeOptionKey(opt: ScopeGateProjectOption): string {
  return `${opt.workspaceId}:${opt.projectId}`
}

watch(
  () => props.projectOptions,
  (options) => {
    if (!Array.isArray(options) || options.length === 0) {
      selectedOptionKey.value = ''
      return
    }
    if (options.some((opt) => encodeOptionKey(opt) === selectedOptionKey.value)) {
      return
    }
    const preferred = options.find((opt) => !opt.isDefaultShared) ?? options[0]
    selectedOptionKey.value = preferred ? encodeOptionKey(preferred) : ''
  },
  { immediate: true }
)

const resolvedProjectLabel = computed(() => {
  if (props.projectName) return props.projectName
  if (props.projectId) return `#${props.projectId}`
  return props.workspaceName || (props.workspaceId ? `#${props.workspaceId}` : 'Default')
})

const resolvedTitle = computed(() => {
  if (props.title) return props.title
  if (props.state === 'unavailable') {
    return props.httpStatus === 404
      ? tr('rbac.probe.scopeUnavailable')
      : props.requestId
        ? tr('rbac.probe.unavailable', { reqId: props.requestId })
        : tr('rbac.probe.unavailableNoReq')
  }
  if (props.surface === 'playground' && props.state === 'denied') {
    return tr('rbac.playground.denied')
  }
  if (props.surface === 'keys' && props.state === 'denied') {
    return tr('rbac.keys.deniedProject', {
      project: resolvedProjectLabel.value,
      grant: props.requiredGrant || 'keys.read'
    })
  }
  if (props.surface === 'byok' && props.state === 'denied') {
    return tr('rbac.byok.denied')
  }
  return tr('rbac.probe.scopeUnavailable')
})

const resolvedPersonalLabel = computed(() => {
  if (props.personalActionLabel) return props.personalActionLabel
  if (props.state === 'unavailable') return tr('scopeGate.returnPersonal')
  if (props.surface === 'keys') return tr('scopeGate.switchPersonalCreateKey')
  if (props.surface === 'playground') return tr('scopeGate.personalLocalCall')
  return tr('scopeGate.switchPersonal')
})

const resolvedRequestAccessLabel = computed(
  () => props.requestAccessLabel || tr('scopeGate.requestAccess')
)

const resolvedSwitchProjectLabel = computed(() => {
  if (props.switchProjectLabel) return props.switchProjectLabel
  return props.projectOptions.length > 0
    ? tr('scopeGate.selectAuthorizedProject')
    : tr('scopeGate.switchProject')
})

const resolvedRetryLabel = computed(() => props.retryLabel || tr('scopeGate.retry'))

const badgeTone = computed<'danger' | 'warning' | 'unknown' | 'neutral'>(() => {
  if (props.state === 'denied') return 'warning'
  if (props.state === 'unavailable') return 'unknown'
  return 'neutral'
})

function handleSelectProject(): void {
  const match = props.projectOptions.find((opt) => encodeOptionKey(opt) === selectedOptionKey.value)
  if (match) {
    emit('select-project', { workspaceId: match.workspaceId, projectId: match.projectId })
  }
}
</script>

<template>
  <section
    data-testid="scope-gate-empty-state"
    :data-state="state"
    :data-surface="surface"
    :role="state === 'denied' || state === 'unavailable' ? 'alert' : 'status'"
    :class="[
      'rounded-oh-lg border border-dashed border-oh-default bg-oh-subtle/50 text-left',
      compact ? 'space-y-3 p-4' : 'space-y-4 p-6'
    ]"
  >
    <div class="flex flex-wrap items-start justify-between gap-2.5">
      <div class="min-w-0 flex-1 space-y-1.5">
        <div class="flex flex-wrap items-center gap-2">
          <Badge :tone="badgeTone" variant="outline" mono size="sm">
            {{ state.toUpperCase() }}
          </Badge>
          <Badge v-if="requiredGrant" tone="neutral" variant="subtle" mono size="sm">
            {{ requiredGrant }}
          </Badge>
          <Badge v-if="workspaceName || workspaceId" tone="neutral" variant="outline" mono size="sm">
            {{ workspaceName || `#${workspaceId}` }}
            <template v-if="projectName || projectId">
              / {{ projectName || `#${projectId}` }}
            </template>
          </Badge>
        </div>
        <h3 class="text-sm font-semibold text-oh-primary">
          {{ resolvedTitle }}
        </h3>
        <p v-if="description" class="max-w-2xl text-xs leading-relaxed text-oh-secondary">
          {{ description }}
        </p>
        <p v-if="requestId" class="font-mono text-[11px] tabular-nums text-oh-muted">
          {{ tr('scopeGate.requestIdLabel', { reqId: requestId }) }}
        </p>
      </div>
    </div>

    <div class="flex flex-wrap items-center gap-2 pt-0.5">
      <slot
        name="request-access"
        :workspace-id="workspaceId"
        :project-id="projectId"
        :required-grant="requiredGrant"
        :state="state"
      >
        <component
          :is="requestAccessComponent"
          v-if="requestAccessComponent"
          v-bind="requestAccessProps"
        />
        <Button
          v-else-if="showRequestAccess"
          data-testid="scope-gate-request-access"
          variant="primary"
          size="sm"
          @click="emit('request-access')"
        >
          {{ resolvedRequestAccessLabel }}
        </Button>
      </slot>

      <slot name="switch-project" :project-options="projectOptions">
        <div v-if="showSwitchProject && projectOptions.length > 0" class="inline-flex flex-wrap items-center gap-1.5">
          <select
            v-model="selectedOptionKey"
            data-testid="scope-gate-project-select"
            :aria-label="isZh ? '选择已有项目' : 'Select project'"
            class="h-8 rounded-oh-md border border-oh-default bg-oh-surface px-2 text-xs text-oh-primary"
          >
            <option value="" disabled>
              {{ tr('scopeGate.selectProjectPlaceholder') }}
            </option>
            <option
              v-for="opt in projectOptions"
              :key="encodeOptionKey(opt)"
              :value="encodeOptionKey(opt)"
            >
              {{ opt.projectName }}
              <template v-if="opt.workspaceName"> ({{ opt.workspaceName }})</template>
              <template v-if="opt.isDefaultShared"> · {{ tr('scopeGate.defaultSharedTag') }}</template>
            </option>
          </select>
          <Button
            data-testid="scope-gate-switch-project"
            variant="secondary"
            size="sm"
            :disabled="!selectedOptionKey"
            @click="handleSelectProject"
          >
            {{ resolvedSwitchProjectLabel }}
          </Button>
        </div>
        <Button
          v-else-if="showSwitchProject"
          data-testid="scope-gate-switch-project"
          variant="secondary"
          size="sm"
          @click="emit('switch-project')"
        >
          {{ resolvedSwitchProjectLabel }}
        </Button>
      </slot>

      <Button
        v-if="showPersonalAction"
        data-testid="scope-gate-switch-personal"
        variant="ghost"
        size="sm"
        @click="emit('switch-personal')"
      >
        {{ resolvedPersonalLabel }}
      </Button>

      <Button
        v-if="secondaryActionLabel"
        data-testid="scope-gate-secondary-action"
        :variant="secondaryActionVariant"
        size="sm"
        @click="emit('secondary-action')"
      >
        {{ secondaryActionLabel }}
      </Button>

      <Button
        v-if="showRetry"
        data-testid="scope-gate-retry"
        variant="secondary"
        size="sm"
        :loading="retrying"
        @click="emit('retry')"
      >
        {{ resolvedRetryLabel }}
      </Button>
    </div>
  </section>
</template>
