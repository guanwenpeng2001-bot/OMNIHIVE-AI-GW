<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import Modal from '@/components/ui/Modal.vue'
import { formatChargeObject } from '@/features/console/format'
import { formatTaskStateLabel } from '../progressRules'
import type { ImageTaskDetail, ImageTaskItem } from '../types'

const props = withDefaults(
  defineProps<{
    open: boolean
    task: ImageTaskDetail | null
    items: ImageTaskItem[]
    preselectedItemIds?: string[]
    submitting?: boolean
    isZh?: boolean
    teleportDisabled?: boolean
  }>(),
  {
    preselectedItemIds: () => [],
    submitting: false,
    isZh: false,
    teleportDisabled: false
  }
)

const emit = defineEmits<{
  'update:open': [value: boolean]
  confirm: [payload: { itemIds: string[] }]
  cancel: []
}>()

const selectedIds = ref<string[]>([])

const retryableItems = computed(() => props.items.filter((item) => item.retryable))

watch(
  () => props.open,
  (visible) => {
    if (visible) {
      selectedIds.value =
        props.preselectedItemIds.length > 0
          ? [...props.preselectedItemIds]
          : retryableItems.value.map((i) => i.id)
    }
  },
  { immediate: true }
)

function toggleItem(id: string): void {
  const idx = selectedIds.value.indexOf(id)
  if (idx === -1) {
    selectedIds.value.push(id)
  } else {
    selectedIds.value.splice(idx, 1)
  }
}

function handleClose(): void {
  emit('update:open', false)
  emit('cancel')
}
</script>

<template>
  <Modal
    :open="open"
    size="md"
    :teleport-disabled="teleportDisabled"
    :title="isZh ? '确认付费重试任务' : 'Confirm Paid Task Retry'"
    :description="
      isZh
        ? '付费重试将关联原任务创建一条全新的独立计费任务，并冻结相应预算。'
        : 'Paid retry creates a new billable execution linked to the parent task and reserves fresh budget.'
    "
    @update:open="(val) => !val && handleClose()"
    @close="handleClose"
  >
    <div v-if="task" data-testid="task-paid-retry-modal" class="space-y-4 text-xs">
      <div class="rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] p-3">
        <div class="flex items-center justify-between gap-2">
          <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
            {{ isZh ? '原任务 ID' : 'Parent Task' }}
          </span>
          <code class="font-mono font-semibold tabular-nums text-[var(--oh-ink,var(--foreground))]">{{ task.id }}</code>
        </div>
        <div class="mt-1.5 flex items-center justify-between gap-2">
          <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
            {{ isZh ? '模型' : 'Model' }}
          </span>
          <code class="font-mono tabular-nums text-[var(--oh-ink,var(--foreground))]">{{ task.model_id }}</code>
        </div>
        <div class="mt-1.5 flex items-center justify-between gap-2">
          <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
            {{ isZh ? '原任务预估参考' : 'Reference Task Estimate' }}
          </span>
          <span class="font-mono font-bold tabular-nums text-[var(--oh-accent-text,var(--brand-text,#d97706))]">
            {{ formatChargeObject(task.estimate) }}
          </span>
        </div>
      </div>

      <div v-if="items.length > 0" class="space-y-2">
        <p class="font-mono text-[11px] font-semibold uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
          {{ isZh ? '选择要付费重试的子项' : 'Select Retryable Sub-Items' }}
        </p>
        <ul class="max-h-48 space-y-1.5 overflow-auto">
          <li
            v-for="item in items"
            :key="item.id"
            class="flex items-center justify-between gap-2 rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] px-3 py-2"
          >
            <label class="flex items-center gap-2 font-mono tabular-nums" :class="!item.retryable && 'opacity-50'">
              <input
                type="checkbox"
                :checked="selectedIds.includes(item.id)"
                :disabled="!item.retryable"
                :data-testid="`retry-item-checkbox-${item.id}`"
                class="h-3.5 w-3.5"
                @change="toggleItem(item.id)"
              />
              <span>#{{ item.id }} · {{ formatTaskStateLabel(item.state, isZh) }}</span>
              <span v-if="item.reason" class="text-[var(--oh-danger,#dc2626)]">({{ item.reason }})</span>
            </label>
            <Badge :tone="item.retryable ? 'accent' : 'neutral'" variant="outline" mono>
              {{ item.retryable ? (isZh ? '可重试' : 'Retryable') : (isZh ? '已锁定' : 'Locked') }}
            </Badge>
          </li>
        </ul>
      </div>
    </div>

    <template #footer>
      <div class="flex items-center justify-end gap-2">
        <Button variant="secondary" size="sm" :disabled="submitting" @click="handleClose">
          {{ isZh ? '取消' : 'Cancel' }}
        </Button>
        <Button
          data-testid="confirm-paid-retry-btn"
          variant="primary"
          size="sm"
          :loading="submitting"
          :disabled="submitting || (retryableItems.length > 0 && selectedIds.length === 0)"
          @click="emit('confirm', { itemIds: [...selectedIds] })"
        >
          {{ isZh ? '确认计费并提交重试' : 'Acknowledge Cost & Retry' }}
        </Button>
      </div>
    </template>
  </Modal>
</template>
