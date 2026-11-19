<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  Badge,
  Button,
  Card,
  Checkbox,
  CommandPalette,
  CopyButton,
  Drawer,
  EmptyState,
  ErrorState,
  IconButton,
  Input,
  Modal,
  PageHeader,
  SegmentedControl,
  Select,
  Skeleton,
  StatNumber,
  Stepper,
  Switch,
  Table,
  Tabs,
  Tag,
  Toast,
  Tooltip,
  useUiToast,
  type UiCommandGroup,
  type UiStepItem,
  type UiTableColumn
} from '@/components/ui'

const themeMode = ref<'dark' | 'light'>('dark')
const densityMode = ref<'comfort' | 'compact'>('comfort')
const activeSectionTab = ref('all')
const viewLayout = ref('table')
const tableDemoState = ref<'ready' | 'loading' | 'refreshing' | 'empty' | 'filtered' | 'error'>('ready')

const modalOpen = ref(false)
const drawerOpen = ref(false)
const paletteOpen = ref(false)
const lastPaletteSelection = ref<string | null>(null)

const endpointUrl = ref('https://gateway.example.internal/v1')
const secretToken = ref('oh-sk-live-9f8a7b6c5d4e3f2a1b0c')
const searchQuery = ref('gpt-6.2')
const invalidBudget = ref('-25')
const selectedProtocol = ref('openai-responses')

const enforceZdr = ref(true)
const indeterminateCheck = ref(false)
const enableSolModel = ref(true)
const enableFlashModel = ref(true)
const enableArchivedModel = ref(false)

const activeStepId = ref('verify')
const wizardSteps: UiStepItem[] = [
  { id: 'provider', title: '1. Provider', description: 'OpenAI / Anthropic / Gemini', status: 'complete' },
  { id: 'credentials', title: '2. Endpoint & Auth', description: 'Secret + Egress Pool', status: 'complete' },
  { id: 'verify', title: '3. Verify Connection', description: 'TLS, scopes & models', status: 'current' },
  { id: 'models', title: '4. Model Mapping', description: 'Catalog slugs & capabilities', status: 'upcoming' },
  { id: 'quotas', title: '5. Pools & Quotas', description: 'RPM, spend cap & enable', status: 'upcoming' }
]

const activeFilterTags = ref(['Protocol: OpenAI', 'ZDR Verified', 'Context >= 128K'])

const sortBy = ref<string>('inputPrice')
const sortOrder = ref<'asc' | 'desc'>('asc')
const selectedRowKeys = ref<Array<string | number>>(['openai/gpt-6.2-sol'])

const modelColumns: UiTableColumn[] = [
  { key: 'slug', label: 'Model', sortable: true, minWidth: '240px' },
  { key: 'access', label: 'Access', align: 'left' },
  {
    key: 'inputPrice',
    label: 'Input $/1M',
    mono: true,
    sortable: true,
    tooltip: 'Confirmed USD rate per 1,000,000 prompt tokens'
  },
  {
    key: 'outputPrice',
    label: 'Output $/1M',
    mono: true,
    sortable: true,
    tooltip: 'Confirmed USD rate per 1,000,000 completion tokens'
  },
  { key: 'context', label: 'Context', mono: true, sortable: true },
  {
    key: 'latencyMs',
    label: 'Median TTFT',
    mono: true,
    sortable: true,
    tooltip: '24h observed gateway median; — when sample < 30'
  },
  { key: 'zdr', label: 'ZDR', align: 'center' }
]

const baseRows: Array<Record<string, unknown>> = [
  {
    id: 'openai/gpt-6.2-sol',
    slug: 'openai/gpt-6.2-sol',
    provider: 'OpenAI',
    access: 'Eligible',
    inputPrice: 2.0,
    variants: '+2 more',
    outputPrice: 10.0,
    context: '1,050,000',
    latencyMs: 410,
    zdr: 'verified'
  },
  {
    id: 'anthropic/claude-opus-5-5',
    slug: 'anthropic/claude-opus-5-5',
    provider: 'Anthropic',
    access: 'Eligible',
    inputPrice: 5.0,
    variants: '+3 more',
    outputPrice: 25.0,
    context: '200,000',
    latencyMs: 620,
    zdr: 'verified'
  },
  {
    id: 'deepseek/deepseek-v4.1-flash',
    slug: 'deepseek/deepseek-v4.1-flash',
    provider: 'DeepSeek',
    access: 'BYOK / Pool',
    inputPrice: 0.14,
    variants: '+1 more',
    outputPrice: 0.28,
    context: '128,000',
    latencyMs: null,
    zdr: 'unknown'
  },
  {
    id: 'long-org-identifier/extremely-long-enterprise-finetuned-multilingual-reasoning-model-2026-v4',
    slug: 'long-org-identifier/extremely-long-enterprise-finetuned-multilingual-reasoning-model-2026-v4',
    provider: 'Custom Pool',
    access: 'Restricted',
    inputPrice: 0,
    variants: 'Internal',
    outputPrice: 0,
    context: '32,768',
    latencyMs: 1290,
    zdr: 'no'
  }
]

const sortedTableRows = computed(() => {
  if (tableDemoState.value === 'loading' || tableDemoState.value === 'empty' || tableDemoState.value === 'filtered') {
    return []
  }
  const copy = [...baseRows]
  copy.sort((left, right) => {
    const leftVal = left[sortBy.value]
    const rightVal = right[sortBy.value]
    if (leftVal === null || leftVal === undefined) return 1
    if (rightVal === null || rightVal === undefined) return -1
    if (typeof leftVal === 'number' && typeof rightVal === 'number') {
      return sortOrder.value === 'asc' ? leftVal - rightVal : rightVal - leftVal
    }
    return sortOrder.value === 'asc'
      ? String(leftVal).localeCompare(String(rightVal))
      : String(rightVal).localeCompare(String(leftVal))
  })
  return copy
})

const commandGroups: UiCommandGroup[] = [
  {
    id: 'models',
    label: 'Models',
    items: [
      {
        id: 'cmd-gpt',
        label: 'openai/gpt-6.2-sol',
        description: '$2.00/M in · $10.00/M out · 1.05M ctx',
        badge: 'ZDR',
        shortcut: '↵',
        keywords: ['gpt', 'openai', 'sol']
      },
      {
        id: 'cmd-claude',
        label: 'anthropic/claude-opus-5-5',
        description: '$5.00/M in · $25.00/M out · 200K ctx',
        badge: 'Tools',
        keywords: ['claude', 'anthropic', 'opus']
      }
    ]
  },
  {
    id: 'navigation',
    label: 'Console Navigation',
    items: [
      {
        id: 'cmd-wizard',
        label: 'Connect Upstream Provider',
        description: 'Launch 5-step upstream connection wizard',
        shortcut: 'G C',
        keywords: ['account', 'upstream', 'wizard']
      },
      {
        id: 'cmd-sim',
        label: 'Simulate Routing Policy',
        description: 'Dry-run candidate selection & failover rules',
        shortcut: 'G R',
        keywords: ['routing', 'policy', 'simulate']
      }
    ]
  }
]

const toast = useUiToast()

function applyTheme(mode: 'dark' | 'light'): void {
  themeMode.value = mode
  document.documentElement.classList.toggle('dark', mode === 'dark')
}

function applyDensity(mode: 'comfort' | 'compact'): void {
  densityMode.value = mode
  document.documentElement.dataset.density = mode
}

function handleTableSort(payload: { key: string; order: 'asc' | 'desc' }): void {
  sortBy.value = payload.key
  sortOrder.value = payload.order
}

function removeFilterTag(tag: string): void {
  activeFilterTags.value = activeFilterTags.value.filter((item) => item !== tag)
}

function triggerSampleToast(tone: 'info' | 'success' | 'warning' | 'danger'): void {
  const messages = {
    info: {
      title: 'Routing simulation queued',
      description: 'Evaluating 6 candidate pools against workspace ZDR policy.'
    },
    success: {
      title: 'Routing policy v15 published',
      description: 'Active across 3 pools; rollback target v14 retained.'
    },
    warning: {
      title: 'Upstream pool entering cooldown',
      description: 'conn-openai-east returned HTTP 429; traffic shifted to fallback.'
    },
    danger: {
      title: 'Upstream credential verification failed',
      description: 'HTTP 401 Unauthorized returned by provider endpoint.',
      requestId: 'req_01J9Z8K4V2M7Q6P5R4'
    }
  }
  const entry = messages[tone]
  toast.push({
    tone,
    title: entry.title,
    description: entry.description,
    requestId: 'requestId' in entry ? entry.requestId : undefined
  })
}

onMounted(() => {
  const isDark = document.documentElement.classList.contains('dark')
  applyTheme(isDark ? 'dark' : 'dark')
  applyDensity('comfort')
})
</script>

<template>
  <div class="min-h-screen bg-oh-canvas text-oh-primary font-sans">
    <div class="sticky top-0 z-30 border-b border-oh-default bg-oh-subtle/95 backdrop-blur-xs">
      <div class="mx-auto flex h-14 max-w-[1440px] items-center justify-between gap-4 px-4 sm:px-6">
        <div class="flex items-center gap-3 min-w-0">
          <svg width="22" height="22" viewBox="0 0 32 32" fill="none" aria-hidden="true" class="shrink-0">
            <path d="M16 2.5L28 9.4v13.2L16 29.5 4 22.6V9.4L16 2.5z" fill="var(--oh-bg-surface)" stroke="var(--oh-border-strong)" stroke-width="1.5" />
            <path d="M16 8.5l6.5 3.75v7.5L16 23.5l-6.5-3.75v-7.5L16 8.5z" fill="var(--oh-accent)" />
          </svg>
          <span class="truncate text-sm font-semibold tracking-tight">{{ 'OmniHive' }}</span>
          <Badge tone="accent" mono size="xs">DESIGN SYSTEM v1 · DEV GALLERY</Badge>
        </div>

        <div class="flex flex-wrap items-center gap-2">
          <Button variant="secondary" size="sm" mono @click="paletteOpen = true">
            Search / Command (⌘K)
          </Button>
          <SegmentedControl
            :model-value="densityMode"
            size="sm"
            aria-label="Density mode"
            :options="[
              { value: 'comfort', label: 'Comfort' },
              { value: 'compact', label: 'Compact' }
            ]"
            @update:model-value="applyDensity($event as 'comfort' | 'compact')"
          />
          <SegmentedControl
            :model-value="themeMode"
            size="sm"
            aria-label="Theme polarity"
            :options="[
              { value: 'dark', label: 'Dark' },
              { value: 'light', label: 'Light' }
            ]"
            @update:model-value="applyTheme($event as 'dark' | 'light')"
          />
        </div>
      </div>
    </div>

    <main class="mx-auto max-w-[1440px] space-y-8 px-4 py-8 sm:px-6">
      <PageHeader
        eyebrow="FOUNDATION & PRIMITIVES"
        title="Component & State Verification Workbench"
        description="Interactive verification of semantic tokens, self-hosted Inter & JetBrains Mono tabular typography, keyboard accessibility, and all ten async/edge states."
        :breadcrumbs="[
          { label: 'Console', to: '/home' },
          { label: 'Design System v1' }
        ]"
      >
        <template #badges>
          <Badge tone="success" dot>WCAG AA Contrast Verified</Badge>
          <Badge tone="neutral" mono>Zero External CDN</Badge>
        </template>
        <template #actions>
          <Button variant="secondary" @click="drawerOpen = true">Open Drawer</Button>
          <Button variant="primary" @click="modalOpen = true">Open Modal</Button>
        </template>
        <template #tabs>
          <Tabs
            v-model="activeSectionTab"
            :items="[
              { value: 'all', label: 'All Primitives', count: 22 },
              { value: 'tokens', label: 'Tokens & Numbers', count: 6 },
              { value: 'forms', label: 'Actions & Forms', count: 8 },
              { value: 'data', label: 'Tables & States', count: 8 },
              { value: 'disabled-demo', label: 'Locked Scope', disabled: true }
            ]"
          />
        </template>
      </PageHeader>

      <section
        v-if="activeSectionTab === 'all' || activeSectionTab === 'tokens'"
        class="space-y-4"
      >
        <Card
          eyebrow="01 · SEMANTIC TOKENS"
          title="Honeycomb · Charcoal · Amber Palette"
          subtitle="Amber is the single chromatic accent; >90% of viewport remains architectural charcoal or warm alabaster."
        >
          <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-6">
            <div class="rounded-oh-md border border-oh-default bg-oh-canvas p-3">
              <p class="oh-eyebrow">Canvas</p>
              <p class="mt-1 font-mono text-xs text-oh-secondary">--oh-bg-canvas</p>
            </div>
            <div class="rounded-oh-md border border-oh-default bg-oh-subtle p-3">
              <p class="oh-eyebrow">Subtle Rail</p>
              <p class="mt-1 font-mono text-xs text-oh-secondary">--oh-bg-subtle</p>
            </div>
            <div class="rounded-oh-md border border-oh-default bg-oh-surface p-3">
              <p class="oh-eyebrow">Surface</p>
              <p class="mt-1 font-mono text-xs text-oh-secondary">--oh-bg-surface</p>
            </div>
            <div class="rounded-oh-md border border-oh-default bg-oh-elevated p-3">
              <p class="oh-eyebrow">Elevated</p>
              <p class="mt-1 font-mono text-xs text-oh-secondary">--oh-bg-elevated</p>
            </div>
            <div class="rounded-oh-md border border-oh-accent bg-oh-accent-subtle p-3">
              <p class="oh-eyebrow text-oh-accent-text">Accent Subtle</p>
              <p class="mt-1 font-mono text-xs text-oh-accent-text">--oh-accent-subtle</p>
            </div>
            <div class="rounded-oh-md border border-transparent bg-oh-accent p-3 text-oh-accent-ink">
              <p class="font-mono text-[11px] font-semibold uppercase tracking-wider">Primary Fill</p>
              <p class="mt-1 font-mono text-xs font-semibold">#F59E0B + #141008</p>
            </div>
          </div>
        </Card>

        <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
          <Card padding="sm">
            <StatNumber
              label="SETTLED SPEND (30D)"
              :value="'1,284.4200'"
              currency="$"
              unit="USD"
              size="lg"
              settlement-state="settled"
              delta="+12.4% vs prior 30d"
              delta-tone="neutral"
            />
          </Card>
          <Card padding="sm">
            <StatNumber
              label="IN-FLIGHT RESERVATION"
              :value="'0.0482'"
              currency="$"
              unit="USD"
              size="lg"
              settlement-state="reserved"
              delta="3 active streams"
            />
          </Card>
          <Card padding="sm">
            <StatNumber
              label="PROCESSED TOKENS"
              :value="94218402110"
              compact
              unit="tokens"
              size="lg"
              accent
              delta="99.94% availability"
              delta-tone="success"
            />
          </Card>
          <Card padding="sm">
            <StatNumber
              label="EXPLICIT ZERO USAGE"
              :value="0"
              unit="errors"
              size="lg"
              delta="Healthy window"
              delta-tone="success"
            />
          </Card>
          <Card padding="sm">
            <StatNumber
              label="UNKNOWN / NO SAMPLE"
              :value="null"
              unit="ms"
              size="lg"
              unknown-label="Insufficient sample in 24h window"
              delta="Sample n < 30"
            />
          </Card>
          <Card padding="sm">
            <StatNumber label="LOADING METRIC" :value="null" size="lg" loading />
          </Card>
        </div>
      </section>

      <section
        v-if="activeSectionTab === 'all' || activeSectionTab === 'forms'"
        class="grid gap-6 lg:grid-cols-2"
      >
        <Card
          eyebrow="02 · ACTIONS & FEEDBACK"
          title="Buttons, Copy Feedback, Tooltips & Badges"
          subtitle="Double-submit protection, in-place clipboard confirmation, and multi-cue status badges."
        >
          <div class="space-y-5">
            <div class="flex flex-wrap items-center gap-2.5">
              <Button variant="primary">Create API Key</Button>
              <Button variant="secondary">Simulate Policy</Button>
              <Button variant="outline-accent">Compare (2/3)</Button>
              <Button variant="ghost">Reset View</Button>
              <Button variant="danger">Revoke Secret</Button>
              <Button variant="primary" loading>Publishing…</Button>
              <Button variant="secondary" disabled>Disabled</Button>
            </div>

            <div class="flex flex-wrap items-center gap-2.5">
              <Button size="xs" variant="secondary">Size XS</Button>
              <Button size="sm" variant="secondary">Size SM</Button>
              <Button size="md" variant="secondary">Size MD</Button>
              <Button size="lg" variant="primary">Size LG Action</Button>
              <Tooltip content="Inspect raw JSON payload (⌘I)">
                <IconButton aria-label="Inspect raw JSON payload" ariaLabel="Inspect raw JSON payload" variant="secondary">
                  <span class="font-mono text-xs">{}</span>
                </IconButton>
              </Tooltip>
              <CopyButton
                value="curl https://gateway.example.internal/v1/models -H 'Authorization: Bearer $OH_API_KEY'"
                variant="secondary"
                label="Copy cURL"
              />
            </div>

            <div class="flex flex-wrap items-center gap-2">
              <Badge tone="success" dot>Verified Healthy</Badge>
              <Badge tone="warning" dot>Cooldown 18s</Badge>
              <Badge tone="danger" dot>401 Invalid Secret</Badge>
              <Badge tone="info" dot>Draining In-Flight</Badge>
              <Badge tone="accent" mono>pv_2026_11_19</Badge>
              <Badge tone="neutral">Disabled by Admin</Badge>
              <Badge tone="unknown" mono>— Unverified</Badge>
            </div>

            <div class="flex flex-wrap items-center gap-2">
              <Tag
                v-for="tag in activeFilterTags"
                :key="tag"
                active
                removable
                @remove="removeFilterTag(tag)"
              >
                {{ tag }}
              </Tag>
              <Button
                v-if="activeFilterTags.length < 3"
                size="xs"
                variant="ghost"
                @click="activeFilterTags = ['Protocol: OpenAI', 'ZDR Verified', 'Context >= 128K']"
              >
                Restore demo tags
              </Button>
            </div>

            <div class="flex flex-wrap items-center gap-2 border-t border-oh-subtle pt-4">
              <span class="oh-eyebrow mr-1">TOAST TRIGGERS:</span>
              <Button size="xs" variant="secondary" @click="triggerSampleToast('info')">Info Toast</Button>
              <Button size="xs" variant="secondary" @click="triggerSampleToast('success')">Success Toast</Button>
              <Button size="xs" variant="secondary" @click="triggerSampleToast('warning')">Warning Toast</Button>
              <Button size="xs" variant="danger" @click="triggerSampleToast('danger')">Error + Request ID</Button>
            </div>
          </div>
        </Card>

        <Card
          eyebrow="03 · FORMS & MODEL TOGGLES"
          title="Inputs, Secret Reveal, Selects & OpenCode Switches"
          subtitle="Accessible label/hint/error wiring and monochrome model enablement cards."
        >
          <div class="space-y-4">
            <div class="grid gap-4 sm:grid-cols-2">
              <Input
                v-model="endpointUrl"
                label="Upstream Base URL"
                mono
                clearable
                required
                hint="Must expose /v1/models or explicit health probe."
              />
              <Input
                v-model="secretToken"
                type="password"
                label="Upstream Credential Secret"
                mono
                revealable
                hint="Stored encrypted; never echoed after save."
              />
            </div>

            <div class="grid gap-4 sm:grid-cols-2">
              <Select
                v-model="selectedProtocol"
                label="Wire Protocol"
                mono
                :options="[
                  { value: 'openai-responses', label: 'OpenAI Responses (/v1/responses)' },
                  { value: 'openai-chat', label: 'OpenAI Chat (/v1/chat/completions)' },
                  { value: 'anthropic-messages', label: 'Anthropic Messages (/v1/messages)' },
                  { value: 'gemini-native', label: 'Gemini Native (/v1beta/models)' }
                ]"
              />
              <Input
                v-model="invalidBudget"
                type="number"
                label="Monthly Hard Budget Cap (USD)"
                mono
                error="Budget ceiling must be a non-negative decimal amount."
              />
            </div>

            <div class="flex flex-wrap items-center gap-6 pt-1">
              <Checkbox
                v-model="enforceZdr"
                label="Require Zero Data Retention (ZDR)"
                description="Exclude providers without verified zero-retention contracts."
              />
              <Checkbox
                v-model="indeterminateCheck"
                indeterminate
                label="Partial Pool Selection (2 / 5)"
              />
            </div>

            <div class="space-y-2 border-t border-oh-subtle pt-4">
              <p class="oh-eyebrow">OPENCODE-STYLE MODEL ENABLEMENT CARDS</p>
              <div class="grid gap-2 sm:grid-cols-3">
                <div class="flex items-center justify-between gap-3 rounded-oh-md border border-oh-default bg-oh-subtle/60 px-3 py-2.5">
                  <div class="min-w-0">
                    <p class="truncate text-xs font-semibold text-oh-primary">GPT-6.2 Sol</p>
                    <p class="truncate font-mono text-[11px] text-oh-muted">openai/gpt-6.2-sol</p>
                  </div>
                  <Switch v-model="enableSolModel" size="sm" aria-label="Enable GPT-6.2 Sol" />
                </div>
                <div class="flex items-center justify-between gap-3 rounded-oh-md border border-oh-default bg-oh-subtle/60 px-3 py-2.5">
                  <div class="min-w-0">
                    <p class="truncate text-xs font-semibold text-oh-primary">DeepSeek V4.1</p>
                    <p class="truncate font-mono text-[11px] text-oh-muted">deepseek-v4.1-flash</p>
                  </div>
                  <Switch v-model="enableFlashModel" size="sm" aria-label="Enable DeepSeek V4.1" />
                </div>
                <div class="flex items-center justify-between gap-3 rounded-oh-md border border-oh-subtle bg-oh-subtle/30 px-3 py-2.5">
                  <div class="min-w-0">
                    <p class="truncate text-xs font-semibold text-oh-muted">Legacy Deprecated</p>
                    <p class="truncate font-mono text-[11px] text-oh-muted">retired-endpoint</p>
                  </div>
                  <Switch v-model="enableArchivedModel" size="sm" disabled aria-label="Disabled retired model" />
                </div>
              </div>
            </div>
          </div>
        </Card>
      </section>

      <section
        v-if="activeSectionTab === 'all' || activeSectionTab === 'data'"
        class="space-y-6"
      >
        <Card
          eyebrow="04 · OPENROUTER / VERCEL MODEL TABLE & ASYNC STATES"
          title="Sortable Tabular Directory"
          subtitle="Switch states below to verify Skeleton, Background Refresh, Empty, Zero-Filter, and Request-ID Error handling."
        >
          <template #actions>
            <SegmentedControl
              v-model="tableDemoState"
              size="sm"
              aria-label="Table state preview"
              :options="[
                { value: 'ready', label: 'Ready' },
                { value: 'refreshing', label: 'Refreshing' },
                { value: 'loading', label: 'Skeleton' },
                { value: 'filtered', label: 'Zero Filter' },
                { value: 'empty', label: 'Empty' },
                { value: 'error', label: 'Error' }
              ]"
            />
          </template>

          <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
            <div class="w-full sm:w-72">
              <Input
                v-model="searchQuery"
                type="search"
                size="sm"
                clearable
                placeholder="Filter models or slugs…"
              />
            </div>
            <div class="flex items-center gap-2">
              <span v-if="selectedRowKeys.length > 0" class="font-mono text-xs text-oh-accent-text">
                {{ selectedRowKeys.length }} selected for compare
              </span>
              <SegmentedControl
                v-model="viewLayout"
                size="sm"
                :options="[
                  { value: 'table', label: 'Table' },
                  { value: 'list', label: 'List' }
                ]"
              />
            </div>
          </div>

          <Table
            v-model:selected-keys="selectedRowKeys"
            :columns="modelColumns"
            :rows="sortedTableRows"
            :sort-by="sortBy"
            :sort-order="sortOrder"
            :loading="tableDemoState === 'loading'"
            :refreshing="tableDemoState === 'refreshing'"
            :filtered="tableDemoState === 'filtered'"
            :error-title="tableDemoState === 'error' ? 'Failed to load model directory' : undefined"
            :error-message="tableDemoState === 'error' ? 'Upstream catalog projection timed out after 5,000ms.' : undefined"
            :error-request-id="tableDemoState === 'error' ? 'req_01J9Z90F7X8K2M3N4P' : undefined"
            selectable
            empty-title="No models published in this workspace"
            empty-description="Connect an upstream provider or enable platform catalog pools to expose models."
            empty-action-label="Connect Provider"
            @sort="handleTableSort"
            @clear-filters="tableDemoState = 'ready'"
            @empty-action="tableDemoState = 'ready'"
            @retry="tableDemoState = 'ready'"
          >
            <template #cell-slug="{ row }">
              <div class="flex items-center gap-2 min-w-0 max-w-xs sm:max-w-md">
                <span class="truncate font-mono text-xs font-medium text-oh-primary" :title="String(row.slug)">
                  {{ row.slug }}
                </span>
                <CopyButton :value="String(row.slug)" variant="inline" size="xs" icon-only aria-label="Copy model slug" />
              </div>
            </template>

            <template #cell-access="{ row }">
              <Badge
                :tone="row.access === 'Eligible' ? 'success' : row.access === 'Restricted' ? 'warning' : 'info'"
                size="xs"
              >
                {{ row.access }}
              </Badge>
            </template>

            <template #cell-inputPrice="{ row }">
              <div class="inline-flex flex-col items-end">
                <span class="oh-tabular decoration-dotted underline underline-offset-4">
                  {{ row.inputPrice === 0 ? 'Free' : `$${Number(row.inputPrice).toFixed(2)}` }}
                </span>
                <span class="font-mono text-[10px] text-oh-muted">{{ row.variants }}</span>
              </div>
            </template>

            <template #cell-outputPrice="{ row }">
              <span class="oh-tabular">
                {{ row.outputPrice === 0 ? 'Free' : `$${Number(row.outputPrice).toFixed(2)}` }}
              </span>
            </template>

            <template #cell-latencyMs="{ row }">
              <span v-if="row.latencyMs === null" class="oh-tabular text-oh-muted" title="Insufficient sample (n < 30)">
                —
              </span>
              <span v-else class="oh-tabular">{{ row.latencyMs }}ms</span>
            </template>

            <template #cell-zdr="{ row }">
              <span v-if="row.zdr === 'verified'" class="font-mono text-oh-status-ok-fg" aria-label="ZDR Verified">✓</span>
              <span v-else-if="row.zdr === 'no'" class="font-mono text-oh-muted" aria-label="Standard Retention">×</span>
              <span v-else class="font-mono text-oh-muted" aria-label="Unverified">—</span>
            </template>
          </Table>
        </Card>

        <Card
          eyebrow="05 · UPSTREAM CONNECTION WIZARD STEPPER"
          title="5-Step Guided Flow"
          subtitle="Replaces legacy 7,036-line modal with deep-linkable steps and explicit verification."
        >
          <Stepper v-model:current-step-id="activeStepId" :steps="wizardSteps" />
        </Card>

        <div class="grid gap-4 lg:grid-cols-3">
          <EmptyState
            title="No API Keys in Workspace"
            description="Create a scoped key bound to specific models and a monthly USD ceiling before sending traffic."
            action-label="Create Scoped Key"
          />
          <ErrorState
            variant="permission"
            title="Permission Denied"
            code="403 FORBIDDEN"
            message="Viewing raw upstream credentials and egress proxy bindings requires elevated workspace privileges."
            required-role="workspace:admin"
          />
          <ErrorState
            variant="stale"
            title="Telemetry Stream Delayed"
            message="Live capacity counters are lagging behind Redis stream ingestion; destructive pool edits are paused."
            last-synced-at="2026-11-19 03:20:14 UTC"
            retry-label="Refresh Now"
          />
        </div>

        <Card eyebrow="06 · GEOMETRY SKELETONS" title="Layout-Stable Loading Placeholders">
          <div class="grid gap-6 md:grid-cols-3">
            <Skeleton variant="stat" />
            <Skeleton variant="line" :lines="3" />
            <Skeleton variant="rect" />
          </div>
        </Card>
      </section>

      <p v-if="lastPaletteSelection" class="font-mono text-xs text-oh-muted">
        Last ⌘K selection: <span class="text-oh-accent-text">{{ lastPaletteSelection }}</span>
      </p>
    </main>

    <Modal
      v-model:open="modalOpen"
      title="Publish Routing Policy v15"
      description="Applies hard ZDR filtering and failover weights to 3 production pools. Previous revision v14 remains available for instant rollback."
    >
      <div class="space-y-3 text-xs text-oh-secondary">
        <div class="rounded-oh-md border border-oh-default bg-oh-subtle p-3 font-mono">
          <p class="text-oh-status-ok-fg">+ require_zdr: true</p>
          <p class="text-oh-accent-text">~ max_input_usd_per_1m: 12.00 → 8.00</p>
        </div>
        <p>Focus stays trapped inside this dialog; pressing ESC or closing restores focus to the trigger button.</p>
      </div>
      <template #footer>
        <Button variant="ghost" @click="modalOpen = false">Cancel</Button>
        <Button
          variant="primary"
          @click="
            modalOpen = false;
            triggerSampleToast('success')
          "
        >
          Confirm & Publish v15
        </Button>
      </template>
    </Modal>

    <Drawer
      v-model:open="drawerOpen"
      title="Request Attempt Waterfall"
      description="req_01J9Z8K4V2M7Q6P5R4 · openai/gpt-6.2-sol · Settled $0.020500"
    >
      <div class="space-y-4 text-xs">
        <div class="rounded-oh-md border border-oh-status-warn-border bg-oh-status-warn-bg/50 p-3">
          <p class="font-mono font-semibold text-oh-status-warn-fg">Attempt #1 · 180ms · HTTP 429</p>
          <p class="mt-1 text-oh-secondary">Pool primary rate-limited; automatic failover triggered.</p>
        </div>
        <div class="rounded-oh-md border border-oh-status-ok-border bg-oh-status-ok-bg/50 p-3">
          <p class="font-mono font-semibold text-oh-status-ok-fg">Attempt #2 · TTFT 410ms · Total 2,140ms · HTTP 200</p>
          <p class="mt-1 text-oh-secondary">4,200 in / 18,000 cached / 850 out · Price pv_2026_11_18.</p>
        </div>
      </div>
      <template #footer>
        <Button variant="secondary" @click="drawerOpen = false">Close</Button>
      </template>
    </Drawer>

    <CommandPalette
      v-model:open="paletteOpen"
      :groups="commandGroups"
      register-shortcut
      @select="lastPaletteSelection = $event.label"
    />

    <Toast />
  </div>
</template>
