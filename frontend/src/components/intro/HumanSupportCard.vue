<template>
  <div
    data-testid="human-support-card"
    class="rounded-oh-md border border-oh-default bg-oh-surface p-5 sm:p-6"
  >
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div class="space-y-1">
        <div class="flex items-center gap-2">
          <h3 class="text-base font-semibold text-oh-primary">
            {{ t('publicPortal.support.sectionTitle') }}
          </h3>
          <span class="rounded-oh-xs border border-oh-subtle bg-oh-subtle px-2 py-0.5 font-mono text-[11px] text-oh-secondary">
            {{ t('publicPortal.support.badge') }}
          </span>
        </div>
        <p class="text-sm text-oh-secondary">
          {{ t('publicPortal.support.sectionSummary') }}
        </p>
      </div>
    </div>

    <dl class="mt-4 grid gap-3 border-t border-oh-subtle pt-4 text-sm sm:grid-cols-3">
      <div class="space-y-1">
        <dt class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">
          {{ t('publicPortal.support.contactLabel') }}
        </dt>
        <dd class="font-medium text-oh-primary break-words">
          <span v-if="resolvedContact">{{ resolvedContact }}</span>
          <span v-else class="text-oh-muted">
            {{ t('publicPortal.support.contactPlaceholder') }}
          </span>
        </dd>
      </div>

      <div class="space-y-1">
        <dt class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">
          {{ t('publicPortal.support.scopeLabel') }}
        </dt>
        <dd class="text-oh-secondary">
          {{ t('publicPortal.support.scopeValue') }}
        </dd>
      </div>

      <div class="space-y-1">
        <dt class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">
          {{ t('publicPortal.support.slaLabel') }}
        </dt>
        <dd class="text-oh-secondary">
          {{ t('publicPortal.support.slaValue') }}
        </dd>
      </div>
    </dl>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import * as stores from '@/stores'

const props = defineProps<{
  contactInfo?: string
}>()

const { t } = useI18n()

const appStore = (() => {
  try {
    return typeof stores.useAppStore === 'function' ? stores.useAppStore() : null
  } catch {
    return null
  }
})()

const resolvedContact = computed(() => {
  const direct = props.contactInfo?.trim()
  if (direct) return direct
  const cached = appStore?.cachedPublicSettings?.contact_info?.trim()
  if (cached) return cached
  return appStore?.contactInfo?.trim() || ''
})
</script>
