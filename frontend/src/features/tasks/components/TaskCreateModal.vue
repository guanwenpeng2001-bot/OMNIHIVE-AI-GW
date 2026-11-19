<script setup lang="ts">
import { reactive } from 'vue'
import Button from '@/components/ui/Button.vue'
import Modal from '@/components/ui/Modal.vue'
import type { ImageTaskCreateInput } from '../types'

withDefaults(
  defineProps<{
    open: boolean
    keys: Array<{ id: string; name: string }>
    models: Array<{ id: string; slug: string; displayName: string }>
    submitting?: boolean
    isZh?: boolean
    teleportDisabled?: boolean
  }>(),
  {
    submitting: false,
    isZh: false,
    teleportDisabled: false
  }
)

const emit = defineEmits<{
  'update:open': [value: boolean]
  submit: [payload: ImageTaskCreateInput]
}>()

const form = reactive({
  apiKeyId: '',
  modelId: '',
  prompt: '',
  size: '1024x1024',
  count: 1,
  acknowledged: false
})

function handleClose(): void {
  emit('update:open', false)
}

function handleSubmit(): void {
  if (!form.apiKeyId || !form.modelId || !form.prompt.trim() || !form.acknowledged) return
  emit('submit', {
    kind: 'image',
    model_id: form.modelId.trim(),
    api_key_id: form.apiKeyId.trim(),
    input: {
      prompt: form.prompt.trim(),
      size: form.size || undefined,
      count: Math.max(1, Math.min(4, Math.trunc(form.count || 1)))
    },
    funding_acknowledged: true
  })
}
</script>

<template>
  <Modal
    :open="open"
    size="md"
    :teleport-disabled="teleportDisabled"
    :title="isZh ? '新建图像生成任务 (BE-15)' : 'New Image Generation Task (BE-15)'"
    :description="
      isZh
        ? '异步生成产物将归档至受控私有存储（保留窗口 24 小时）。'
        : 'Generated artifacts are archived in private scope-bound storage (24h retention window).'
    "
    @update:open="(val) => !val && handleClose()"
    @close="handleClose"
  >
    <div data-testid="task-create-modal" class="space-y-3 text-xs">
      <label class="block space-y-1">
        <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">API Key</span>
        <select
          v-if="keys.length > 0"
          v-model="form.apiKeyId"
          class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs tabular-nums text-[var(--oh-ink,var(--foreground))]"
        >
          <option value="">{{ isZh ? '选择密钥…' : 'Select key…' }}</option>
          <option v-for="k in keys" :key="k.id" :value="k.id">
            {{ k.name }} (#{{ k.id }})
          </option>
        </select>
        <input
          v-else
          v-model="form.apiKeyId"
          type="text"
          placeholder="Key ID"
          class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2.5 font-mono text-xs tabular-nums text-[var(--oh-ink,var(--foreground))]"
        />
      </label>

      <label class="block space-y-1">
        <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Model ID</span>
        <select
          v-if="models.length > 0"
          v-model="form.modelId"
          class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs text-[var(--oh-ink,var(--foreground))]"
        >
          <option value="">{{ isZh ? '选择模型…' : 'Select model…' }}</option>
          <option v-for="m in models" :key="m.id" :value="m.slug || m.id">
            {{ m.displayName }} ({{ m.slug }})
          </option>
        </select>
        <input
          v-model="form.modelId"
          type="text"
          :placeholder="isZh ? '输入图像模型 ID' : 'Image model slug/ID'"
          class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2.5 font-mono text-xs text-[var(--oh-ink,var(--foreground))]"
        />
      </label>

      <div class="grid grid-cols-2 gap-2">
        <label class="block space-y-1">
          <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Size</span>
          <select
            v-model="form.size"
            class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs tabular-nums text-[var(--oh-ink,var(--foreground))]"
          >
            <option value="1024x1024">1024x1024</option>
            <option value="1536x1024">1536x1024</option>
            <option value="1024x1536">1024x1536</option>
          </select>
        </label>
        <label class="block space-y-1">
          <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Count (1–4)</span>
          <input
            v-model.number="form.count"
            type="number"
            min="1"
            max="4"
            class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs tabular-nums text-[var(--oh-ink,var(--foreground))]"
          />
        </label>
      </div>

      <label class="block space-y-1">
        <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">Prompt</span>
        <textarea
          v-model="form.prompt"
          rows="3"
          maxlength="8000"
          :placeholder="isZh ? '输入图像生成提示词…' : 'Describe the image to generate…'"
          class="w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] p-2.5 text-sm text-[var(--oh-ink,var(--foreground))]"
        />
      </label>

      <label class="flex items-start gap-2 rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] p-2.5">
        <input v-model="form.acknowledged" type="checkbox" class="mt-0.5 h-3.5 w-3.5" />
        <span class="text-[var(--oh-ink,var(--foreground))]">
          {{
            isZh
              ? '我确认本任务将按当前价格版本独立冻结预算并产生计费（funding_acknowledged: true）。'
              : 'I acknowledge this task reserves budget and bills under the active price version (funding_acknowledged: true).'
          }}
        </span>
      </label>
    </div>

    <template #footer>
      <div class="flex items-center justify-end gap-2">
        <Button variant="secondary" size="sm" :disabled="submitting" @click="handleClose">
          {{ isZh ? '取消' : 'Cancel' }}
        </Button>
        <Button
          data-testid="submit-create-task-btn"
          variant="primary"
          size="sm"
          :loading="submitting"
          :disabled="submitting || !form.apiKeyId || !form.modelId || !form.prompt.trim() || !form.acknowledged"
          @click="handleSubmit"
        >
          {{ isZh ? '确认计费并提交' : 'Create Paid Task' }}
        </Button>
      </div>
    </template>
  </Modal>
</template>
