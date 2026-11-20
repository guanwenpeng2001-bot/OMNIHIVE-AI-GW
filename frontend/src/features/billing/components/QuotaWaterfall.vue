<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Badge } from '@/components/ui'
import { displayNumber, moneyText, timestamp } from '@/features/requests/presentation'
import type { Schema } from '@/features/requests/api'
import { orderedQuotaRules, quotaUsage } from '../presentation'

const props = defineProps<{ quota: Schema['CostQuota'] }>()
const { t, te, locale } = useI18n()
const isZh = computed(() => String(locale?.value || '').startsWith('zh'))
const rules = computed(() => orderedQuotaRules(props.quota).map(rule => ({
  rule,
  share: quotaUsage(rule),
  blocking: props.quota.blocking_rule_ids.includes(rule.id),
})))
function label(source: string) {
  return te(`costLedger.quotaSource.${source}`) ? t(`costLedger.quotaSource.${source}`) : source
}
function scopeTypeLabel(scopeType: string) {
  if (!isZh.value) return scopeType
  const map: Record<string, string> = {
    project: '项目',
    workspace: '工作空间',
    key: '密钥',
    member: '成员',
    user: '账户',
    group: '分组',
  }
  return map[scopeType] || scopeType
}
</script>

<template>
  <div class="space-y-3">
    <div class="flex flex-wrap items-center justify-between gap-2 text-xs text-oh-muted">
      <span class="font-mono tabular-nums">{{ timestamp(quota.as_of, locale) }}</span>
      <Badge v-if="quota.blocked" tone="warning" dot>{{ t('costLedger.billing.quotaBlocked') }}</Badge>
    </div>
    <p v-if="rules.length === 0" class="text-sm text-oh-muted">{{ t('costLedger.billing.noQuota') }}</p>
    <ol v-else class="space-y-2">
      <li
        v-for="{ rule, share, blocking } in rules" :key="rule.id"
        class="grid min-w-0 gap-2 rounded-oh-md border px-3 py-2.5"
        :class="blocking ? 'border-oh-accent bg-oh-accent-subtle' : 'border-oh-subtle bg-oh-subtle/45'"
      >
        <div class="flex flex-wrap items-start justify-between gap-1.5">
          <div class="min-w-0">
            <span class="text-sm font-medium text-oh-primary">{{ label(rule.source) }}</span>
            <p class="mt-0.5 break-all font-mono text-[11px] text-oh-muted">
              {{ scopeTypeLabel(rule.scope.type) }} #{{ rule.scope.id }} · {{ rule.unit }}
              <template v-if="rule.resets_at"> / {{ timestamp(rule.resets_at, locale, rule.timezone) }}</template>
            </p>
          </div>
          <div class="flex flex-wrap gap-1.5">
            <Badge v-if="blocking" tone="warning" size="xs" dot>{{ t('costLedger.billing.blockingRule') }}</Badge>
            <Badge v-if="rule.freshness !== 'fresh'" tone="unknown" size="xs">{{ t('costLedger.billing.quotaUnknown') }}</Badge>
            <Badge size="xs">{{ rule.mode === 'hard' ? t('costLedger.billing.hardLimit') : t('costLedger.billing.softLimit') }}</Badge>
          </div>
        </div>
        <div class="flex h-2 w-full overflow-hidden rounded-oh-xs bg-oh-inset" aria-hidden="true">
          <template v-if="share">
            <span class="h-full bg-oh-accent" :style="{ width: `${share.used}%` }" />
            <span v-if="share.reserved > 0" class="h-full border border-oh-accent bg-oh-accent-subtle" :style="{ width: `${share.reserved}%` }" />
          </template>
          <span v-else class="h-full w-full border border-dashed border-oh-strong" />
        </div>
        <dl class="grid min-w-0 grid-cols-2 gap-2 text-xs sm:grid-cols-4">
          <div><dt class="text-oh-muted">{{ t('costLedger.billing.limit') }}</dt><dd class="break-words font-mono tabular-nums text-oh-primary">{{ displayNumber(rule.limit, locale) }}</dd></div>
          <div><dt class="text-oh-muted">{{ t('costLedger.billing.used') }}</dt><dd class="break-words font-mono tabular-nums text-oh-primary">{{ displayNumber(rule.used, locale) }}</dd></div>
          <div><dt class="text-oh-muted">{{ t('costLedger.requests.reserved') }}</dt><dd class="break-words font-mono tabular-nums text-oh-primary">{{ displayNumber(rule.reserved, locale) }}</dd></div>
          <div><dt class="text-oh-muted">{{ t('costLedger.billing.available') }}</dt><dd class="break-words font-mono tabular-nums text-oh-primary">{{ displayNumber(rule.available, locale) }}</dd></div>
        </dl>
      </li>
    </ol>
    <dl class="flex flex-wrap items-center justify-between gap-2 border-t border-oh-subtle pt-2 text-xs">
      <dt class="text-oh-secondary">{{ t('costLedger.billing.byokExposure') }}</dt>
      <dd class="font-mono tabular-nums text-oh-primary">{{ moneyText(quota.external_exposure.risk_limit, locale) }}</dd>
    </dl>
  </div>
</template>
