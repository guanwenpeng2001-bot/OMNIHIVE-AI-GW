<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'

const props = withDefaults(
  defineProps<{
    totalAccounts?: number | null
    liveModels?: number | null
    forceVisible?: boolean
  }>(),
  {
    totalAccounts: 0,
    liveModels: 0,
    forceVisible: false
  }
)

const { t } = useI18n()

const isVisible = computed<boolean>(() => {
  if (props.forceVisible) return true
  const noAccounts = typeof props.totalAccounts === 'number' && props.totalAccounts === 0
  const noLiveModels = props.liveModels == null || props.liveModels === 0
  return noAccounts || noLiveModels
})

const stages = computed(() => [
  {
    id: 'stage-1',
    done: typeof props.totalAccounts === 'number' && props.totalAccounts > 0,
    title: t('onboarding.adminSetup.stage1Title'),
    description: t('onboarding.adminSetup.stage1Desc'),
    cta: t('onboarding.adminSetup.stage1Cta'),
    to: '/admin/accounts'
  },
  {
    id: 'stage-2',
    done: false,
    title: t('onboarding.adminSetup.stage2Title'),
    description: t('onboarding.adminSetup.stage2Desc'),
    cta: t('onboarding.adminSetup.stage2Cta'),
    to: '/admin/channels/pricing'
  },
  {
    id: 'stage-3',
    done: typeof props.liveModels === 'number' && props.liveModels > 0,
    title: t('onboarding.adminSetup.stage3Title'),
    description: t('onboarding.adminSetup.stage3Desc'),
    cta: t('onboarding.adminSetup.stage3Cta'),
    to: '/admin/models'
  },
  {
    id: 'stage-4',
    done: false,
    title: t('onboarding.adminSetup.stage4Title'),
    description: t('onboarding.adminSetup.stage4Desc'),
    cta: t('onboarding.adminSetup.stage4Cta'),
    to: '/admin/members'
  }
])
</script>

<template>
  <section
    v-if="isVisible"
    data-testid="admin-setup-banner"
    class="rounded-xl border border-amber-500/40 bg-amber-500/5 p-5 space-y-4"
    :aria-label="t('onboarding.adminSetup.title')"
  >
    <div class="space-y-1">
      <p class="font-mono text-[11px] font-semibold uppercase tracking-wider text-amber-600 dark:text-amber-400">
        {{ t('onboarding.adminSetup.eyebrow') }}
      </p>
      <h2 class="text-base font-bold text-[var(--foreground)]">
        {{ t('onboarding.adminSetup.title') }}
      </h2>
      <p class="text-xs text-[var(--muted-foreground)]">
        {{ t('onboarding.adminSetup.subtitle') }}
      </p>
    </div>

    <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
      <div
        v-for="stage in stages"
        :key="stage.id"
        :data-testid="`admin-setup-${stage.id}`"
        class="flex flex-col justify-between rounded-lg border border-[var(--border)] bg-[var(--card)] p-3.5 space-y-2.5"
      >
        <div class="space-y-1.5">
          <div class="flex items-center justify-between gap-2">
            <h3 class="text-xs font-semibold text-[var(--foreground)]">
              {{ stage.title }}
            </h3>
            <span
              v-if="stage.done"
              class="rounded bg-emerald-500/15 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-emerald-600 dark:text-emerald-400"
            >
              ✓
            </span>
          </div>
          <p class="text-[11px] leading-relaxed text-[var(--muted-foreground)]">
            {{ stage.description }}
          </p>
        </div>
        <RouterLink
          :to="stage.to"
          :data-testid="`admin-setup-link-${stage.id}`"
          class="inline-flex items-center text-xs font-semibold text-[var(--brand,#d97706)] hover:underline"
        >
          {{ stage.cta }}
        </RouterLink>
      </div>
    </div>
  </section>
</template>
