<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import Badge from '@/components/ui/Badge.vue'
import Tooltip from '@/components/ui/Tooltip.vue'
import { getCatalogCopy } from '@/features/catalog/copy'
import { formatPublicTimestamp } from '@/features/catalog/statusPresentation'
import type { CatalogModelVM } from '@/features/catalog/viewModel'

const props = withDefaults(
  defineProps<{
    billing: CatalogModelVM['billing']
    kind?: 'input' | 'output' | 'pair'
    isZh?: boolean
    detailTo?: string
    seeDetailTestId?: string
    referenceTestId?: string
    compact?: boolean
  }>(),
  {
    kind: 'input',
    isZh: false,
    detailTo: '',
    seeDetailTestId: '',
    referenceTestId: '',
    compact: false
  }
)

const emit = defineEmits<{
  (e: 'retry'): void
}>()

const copy = computed(() => getCatalogCopy(props.isZh))

const primarySaleRateText = computed(() => {
  if (props.kind === 'output') return props.billing.outputDisplay
  if (props.kind === 'pair') {
    if (props.billing.isFree) return copy.value.billing.free
    if (props.billing.isError) return copy.value.billing.error
    if (props.billing.state === 'varies') return copy.value.billing.varies
    if (props.billing.state === 'unpriced' || props.billing.state === 'unknown') {
      return '—'
    }
    return `${props.billing.inputDisplay} / ${props.billing.outputDisplay}`
  }
  return props.billing.inputDisplay
})

const hasRateForKind = computed(() => {
  if (props.kind === 'output') return Boolean(props.billing.outputRate)
  if (props.kind === 'pair') return Boolean(props.billing.inputRate || props.billing.outputRate)
  return Boolean(props.billing.inputRate)
})

const showSeeDetailLink = computed(() => {
  if (!props.detailTo || !props.seeDetailTestId) return false
  if (props.billing.isFree || props.billing.isError || props.billing.isGroupVarying) return false
  return !hasRateForKind.value
})

const referenceLineText = computed(() => {
  const ref = props.billing.reference
  if (!ref) return null
  const observedPart = ref.observedAt
    ? ` · ${copy.value.billing.observedAtPrefix} ${formatPublicTimestamp(ref.observedAt)}`
    : ''
  if (props.kind === 'output') {
    if (!ref.outputDisplay) return null
    return `${copy.value.billing.referencePrefix} ${ref.outputDisplay} · ${ref.sourceLabel}${observedPart} · ${ref.disclaimer}`
  }
  if (props.kind === 'pair') {
    if (!ref.inputDisplay && !ref.outputDisplay) return null
    const ratePart = `${ref.inputDisplay ?? '—'} / ${ref.outputDisplay ?? '—'}`
    return `${copy.value.billing.referencePrefix} ${ratePart} · ${ref.sourceLabel}${observedPart} · ${ref.disclaimer}`
  }
  if (!ref.inputDisplay) return null
  return `${copy.value.billing.referencePrefix} ${ref.inputDisplay} · ${ref.sourceLabel}${observedPart} · ${ref.disclaimer}`
})

const resolvedReferenceTestId = computed(
  () => props.referenceTestId || `reference-price-secondary-${props.kind}`
)

const groupRangeHint = computed(() => {
  if (props.kind === 'output') return props.billing.rangeOutputDisplay
  if (props.kind === 'pair') {
    if (!props.billing.rangeInputDisplay && !props.billing.rangeOutputDisplay) return null
    return `${props.billing.rangeInputDisplay ?? '—'} / ${props.billing.rangeOutputDisplay ?? '—'}`
  }
  return props.billing.rangeInputDisplay
})
</script>

<template>
  <div class="inline-flex flex-col items-end gap-0.5 text-right">
    <div v-if="billing.isError" class="inline-flex flex-wrap items-center justify-end gap-1.5">
      <Tooltip :content="copy.billing.errorFull" placement="left">
        <Badge data-testid="price-cell-error" tone="warning" size="xs" variant="subtle">
          {{ copy.billing.error }}
        </Badge>
      </Tooltip>
      <button
        type="button"
        data-testid="price-cell-retry"
        class="oh-focus-ring rounded-oh-xs font-mono text-[11px] font-medium text-oh-accent-text underline-offset-2 hover:underline"
        @click.stop="emit('retry')"
      >
        {{ copy.billing.retryPricing }}
      </button>
    </div>

    <span
      v-else-if="billing.isFree"
      data-testid="price-cell-free"
      class="font-mono font-semibold text-emerald-600 dark:text-emerald-400"
    >
      {{ copy.billing.free }}
    </span>

    <div
      v-else-if="billing.isGroupVarying"
      class="inline-flex flex-wrap items-center justify-end gap-1.5"
    >
      <Tooltip :content="billing.reasonLabel || copy.billing.variesFull" placement="left">
        <RouterLink
          v-if="detailTo"
          :to="detailTo"
          data-testid="price-cell-varies"
          class="oh-focus-ring inline-flex items-center gap-1 rounded-oh-xs"
        >
          <Badge tone="info" size="xs" variant="subtle">
            {{ billing.modeBadge?.label ?? copy.billing.varies }}
          </Badge>
          <span v-if="groupRangeHint" class="font-mono text-xs tabular-nums text-oh-primary">
            {{ groupRangeHint }}
          </span>
        </RouterLink>
        <span v-else data-testid="price-cell-varies" class="inline-flex items-center gap-1">
          <Badge tone="info" size="xs" variant="subtle">
            {{ billing.modeBadge?.label ?? copy.billing.varies }}
          </Badge>
          <span v-if="groupRangeHint" class="font-mono text-xs tabular-nums text-oh-primary">
            {{ groupRangeHint }}
          </span>
        </span>
      </Tooltip>
    </div>

    <div
      v-else-if="showSeeDetailLink"
      class="inline-flex flex-wrap items-center justify-end gap-1.5"
    >
      <Tooltip :content="copy.billing.seeDetailTooltip" placement="left">
        <RouterLink
          :to="detailTo"
          :data-testid="seeDetailTestId"
          class="oh-focus-ring inline-flex rounded-oh-xs"
        >
          <Badge tone="unknown" size="xs" variant="subtle">
            {{ billing.state === 'unpriced' ? `${copy.billing.unpriced} · ${copy.billing.seeDetail}` : copy.billing.seeDetail }}
          </Badge>
        </RouterLink>
      </Tooltip>
    </div>

    <div v-else class="inline-flex flex-wrap items-baseline justify-end gap-1.5">
      <span
        data-testid="price-cell-primary"
        class="font-mono tabular-nums"
        :class="
          hasRateForKind
            ? 'font-medium text-oh-primary'
            : 'text-oh-muted'
        "
      >
        {{
          !hasRateForKind && billing.state === 'unpriced' && !compact
            ? copy.billing.unpriced
            : primarySaleRateText
        }}
      </span>
      <Badge
        v-if="
          !compact &&
          kind !== 'output' &&
          hasRateForKind &&
          billing.modeBadge &&
          billing.pricingMode !== 'per_token'
        "
        tone="info"
        size="xs"
        variant="outline"
      >
        {{ billing.modeBadge.label }}
      </Badge>
    </div>

    <p
      v-if="referenceLineText"
      :data-testid="resolvedReferenceTestId"
      class="max-w-[22rem] text-[11px] leading-tight text-oh-muted"
    >
      {{ referenceLineText }}
    </p>
  </div>
</template>
