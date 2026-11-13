<template>
  <section
    class="omnihive-group-card overflow-hidden rounded-xl border bg-[var(--card)] shadow-sm transition-shadow hover:shadow-md "
    :class="[platformBorderStrongClass(group.platform)]"
  >
    <!-- 分组头部:名称/平台/倍率徽章/专属/订阅徽章 + 描述 -->
    <header class="border-b border-[var(--border)] bg-[var(--surface-2)] px-5 py-4  ">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <div class="flex flex-wrap items-center gap-2">
          <span class="pulse-dot pulse-ok" title="目录已加载"></span>
          <GroupBadge
            :name="group.name"
            :platform="group.platform as GroupPlatform"
            :subscription-type="(group.subscription_type || 'standard') as SubscriptionType"
            :rate-multiplier="group.rate_multiplier"
            :user-rate-multiplier="group.user_rate_multiplier ?? null"
            :peak-rate-enabled="group.peak_rate_enabled"
            :peak-start="group.peak_start"
            :peak-end="group.peak_end"
            :peak-rate-multiplier="group.peak_rate_multiplier"
            always-show-rate
          />
          <span
            v-if="group.is_exclusive"
            class="inline-flex items-center gap-1 rounded-md bg-purple-50 px-2 py-0.5 text-xs font-medium text-purple-600 dark:bg-purple-900/20 dark:text-purple-400"
          >
            <Icon name="shield" size="xs" class="h-3 w-3" />
            {{ t('modelPlaza.badges.exclusive') }}
          </span>
          <span
            v-if="group.subscription_type === 'subscription'"
            class="inline-flex items-center rounded-md bg-violet-50 px-2 py-0.5 text-xs font-medium text-violet-600 dark:bg-violet-900/20 dark:text-violet-400"
          >
            {{ t('modelPlaza.badges.subscription') }}
          </span>
        </div>

        <span
          class="inline-flex items-center gap-1 rounded-full border border-[var(--border)] bg-[var(--card)] px-2.5 py-0.5 font-mono text-xs text-[var(--muted-foreground)] tabular-nums   "
        >
          {{ group.models.length }} models
        </span>
      </div>
      <p v-if="group.description" class="mt-2 text-sm text-[var(--muted-foreground)] ">
        {{ group.description }}
      </p>
      <p
        v-if="peakNote"
        class="mt-1.5 inline-flex items-center gap-1 text-xs text-amber-600 dark:text-amber-400"
      >
        <Icon name="clock" size="xs" class="h-3 w-3" />
        {{ peakNote }}
      </p>
      <p
        v-if="longContextNote"
        class="mt-1.5 flex items-center gap-1 text-xs text-[var(--muted-foreground)] "
      >
        <Icon name="infoCircle" size="xs" class="h-3 w-3" />
        {{ longContextNote }}
      </p>
    </header>

    <!-- 模型价格表:整行(含 hover 底色/分区底色)顶到卡片边缘,左右留白由表格首列/末列的 padding 提供 -->
    <div>
      <PlazaModelPricingTable
        v-if="group.models.length > 0"
        :models="group.models"
        :platform="group.platform"
        :rate-multiplier="group.rate_multiplier"
        :user-rate-multiplier="group.user_rate_multiplier ?? null"
        :image-rate-independent="group.image_rate_independent"
        :image-rate-multiplier="group.image_rate_multiplier"
        :video-rate-independent="group.video_rate_independent"
        :video-rate-multiplier="group.video_rate_multiplier"
        :peak-window="peakWindow"
        :peak-rate-multiplier="group.peak_rate_multiplier"
        :favorites="favorites"
        :compared="compared"
        :compare-limit-reached="compareLimitReached"
        :model-order="modelOrder"
        @favorite="$emit('favorite', $event)"
        @compare="$emit('compare', $event)"
      />
      <p v-else class="px-5 py-4 text-center text-sm text-[var(--muted-foreground)] ">
        {{ t('modelPlaza.detail.noModels') }}
      </p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import PlazaModelPricingTable from './PlazaModelPricingTable.vue'
import type { ModelPlazaGroup } from '@/api/modelPlaza'
import type { GroupPlatform, SubscriptionType } from '@/types'
import { platformBorderStrongClass } from '@/utils/platformColors'
import { hasPeakRate, formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
import { useAppStore } from '@/stores/app'

const props = defineProps<{
  group: ModelPlazaGroup
  favorites?: string[]
  compared?: string[]
  compareLimitReached?: boolean
  modelOrder?: string[]
}>()
defineEmits<{ favorite: [id: string]; compare: [id: string] }>()

const { t } = useI18n()
const appStore = useAppStore()

/** 高峰窗口描述(含倍率与服务器时区标注);分组未启用高峰为空串。 */
const peakWindow = computed(() => {
  if (!hasPeakRate(props.group)) return ''
  return formatPeakRateWindow(
    props.group,
    serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset)
  )
})

const peakNote = computed(() => {
  if (!peakWindow.value) return ''
  return t('modelPlaza.detail.peakNote', {
    window: peakWindow.value,
    multiplier: props.group.peak_rate_multiplier
  })
})

const longContextNote = computed(() => props.group.long_context_pricing_enabled === false
  ? t('modelPlaza.detail.longContextDisabledNote') : '')
</script>

<style scoped>
.omnihive-group-card { border-color: var(--border); background: var(--card); box-shadow: none; }
.omnihive-group-card header { border-color: var(--border); background: var(--surface-2); }
.pulse-dot {
  width: 7px;
  height: 7px;
  border-radius: 9999px;
  display: inline-block;
  flex: none;
}

.pulse-ok {
  background: var(--ok, #16a34a);
  color: var(--ok, #16a34a);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--ok, #16a34a) 22%, transparent);
}
</style>
