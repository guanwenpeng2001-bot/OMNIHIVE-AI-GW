<template>
  <fieldset class="min-w-0">
    <legend class="input-label flex items-center justify-between gap-2">
      {{ t('admin.users.groups') }}
      <span class="pill pill-ghost font-mono text-[11px] font-normal tabular-nums text-[var(--muted-foreground)]">{{ t('common.selectedCount', { count: modelValue.length }) }}</span>
    </legend>
    <p v-if="warnWhenEmpty && modelValue.length === 0" role="status" class="mb-2 rounded-lg border border-[var(--warn)]/30 bg-[var(--warn)]/10 px-2.5 py-1.5 text-xs text-[var(--warn-text)]">{{ t('admin.accounts.noGroupWarning') }}</p>
    <div
      v-if="isSearchable"
      class="flex items-center gap-2 rounded-t-xl border border-b-0 border-[var(--border)] bg-[var(--surface-2)]/70 px-3 py-2"
    >
      <Icon name="search" size="sm" class="shrink-0 text-[var(--muted-foreground)]" />
      <input
        v-model="searchText"
        type="text"
        :placeholder="t('common.searchPlaceholder')"
        :aria-label="`${t('common.search')}: ${t('admin.users.groups')}`"
        class="min-w-0 flex-1 bg-transparent text-xs text-[var(--foreground)] placeholder:text-[var(--muted-foreground)] focus:outline-none"
      />
    </div>
    <div
      :class="[
        'grid max-h-48 grid-cols-1 gap-1.5 overflow-y-auto p-2.5 sm:grid-cols-2',
        isSearchable
          ? 'rounded-b-xl border border-[var(--border)] bg-[var(--surface-2)]/40'
          : 'rounded-xl border border-[var(--border)] bg-[var(--surface-2)]/40'
      ]"
    >
      <label
        v-for="group in filteredGroups"
        :key="group.id"
        :class="[
          'flex cursor-pointer items-center justify-between gap-2 rounded-lg border px-2.5 py-1.5 transition-all',
          modelValue.includes(group.id)
            ? 'border-[var(--foreground)]/25 bg-[var(--card)] shadow-2xs ring-1 ring-[var(--brand)]/20'
            : 'border-[var(--border)]/60 bg-[var(--card)]/70 hover:border-[var(--border)] hover:bg-[var(--card)]'
        ]"
        :title="group.rate_multiplier == null ? group.name : t('admin.groups.rateAndAccounts', { rate: group.rate_multiplier, count: group.account_count || 0 })"
      >
        <div class="flex min-w-0 flex-1 items-center gap-2">
          <input
            type="checkbox"
            :aria-label="`${group.name} (${group.platform})`"
            :value="group.id"
            :checked="modelValue.includes(group.id)"
            @change="handleChange(group.id, ($event.target as HTMLInputElement).checked)"
            class="h-3.5 w-3.5 shrink-0 rounded border-[var(--input)] accent-[var(--primary)] focus:ring-[var(--ring)]"
          />
          <GroupBadge
            :name="group.name"
            :platform="group.platform"
            :subscription-type="group.subscription_type || undefined"
            :rate-multiplier="group.rate_multiplier == null ? undefined : group.rate_multiplier"
            class="min-w-0"
          />
        </div>
        <span class="pill pill-ghost shrink-0 font-mono text-[10px] tabular-nums text-[var(--muted-foreground)]">{{ group.account_count || 0 }}</span>
      </label>
      <div
        v-if="filteredGroups.length === 0"
        class="col-span-2 py-2 text-center text-sm text-gray-500 dark:text-gray-400"
      >
        {{ t('common.noGroupsAvailable') }}
      </div>
    </div>
  </fieldset>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import GroupBadge from './GroupBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Group, GroupPlatform } from '@/types'
import { useAuthStore } from '@/stores'

const { t } = useI18n()
const authStore = useAuthStore()

interface Props {
  modelValue: number[]
  groups: (Group & { account_count?: number })[]
  platform?: GroupPlatform // Optional platform filter
  mixedScheduling?: boolean // For antigravity accounts: allow anthropic/gemini groups
  warnWhenEmpty?: boolean
  searchable?: boolean | 'auto'
}

const props = withDefaults(defineProps<Props>(), {
  searchable: 'auto'
})
const emit = defineEmits<{
  'update:modelValue': [value: number[]]
}>()

const searchText = ref('')

const isSearchable = computed(() => {
  if (props.searchable === 'auto') return props.groups.length > 5
  return props.searchable
})

// Filter groups by platform if specified
const filteredGroups = computed(() => {
  let result = authStore.isSimpleMode
    ? props.groups.filter((g) => g.platform !== 'composite')
    : props.groups
  if (props.platform) {
    // antigravity 账户启用混合调度后，可选择 anthropic/gemini 分组
    if (props.platform === 'antigravity' && props.mixedScheduling) {
      result = result.filter(
        (g) => g.platform === 'antigravity' || g.platform === 'anthropic' || g.platform === 'gemini' || g.platform === 'composite'
      )
    } else {
      // 默认：只能选择同 platform 的分组；composite 分组可接收任意具体平台账号
      result = result.filter((g) => g.platform === props.platform || g.platform === 'composite')
    }
  }
  if (isSearchable.value && searchText.value) {
    const q = searchText.value.toLowerCase()
    result = result.filter(
      (g) => g.name.toLowerCase().includes(q) || g.description?.toLowerCase().includes(q)
    )
  }
  return result
})

watch(
  () => [authStore.isSimpleMode, props.groups, props.modelValue] as const,
  () => {
    if (!authStore.isSimpleMode || props.groups.length === 0) return
    const visibleIDs = new Set(props.groups.filter((group) => group.platform !== 'composite').map((group) => group.id))
    const cleaned = props.modelValue.filter((id) => visibleIDs.has(id))
    if (cleaned.length !== props.modelValue.length) emit('update:modelValue', cleaned)
  },
  { immediate: true, deep: true }
)

const handleChange = (groupId: number, checked: boolean) => {
  const newValue = checked
    ? [...props.modelValue, groupId]
    : props.modelValue.filter((id) => id !== groupId)
  emit('update:modelValue', newValue)
}
</script>
