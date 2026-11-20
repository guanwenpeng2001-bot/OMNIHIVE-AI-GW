<script setup lang="ts">
import { reactive, watch } from 'vue'
import Button from '@/components/ui/Button.vue'
import Modal from '@/components/ui/Modal.vue'
import type { ImageTaskCreateInput } from '../types'

const props = withDefaults(
  defineProps<{
    open: boolean
    keys: Array<{ id: string; name: string; prefix?: string; scopeLabel?: string }>
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
  'navigate-keys': []
}>()

const form = reactive({
  apiKeyId: '',
  modelId: '',
  prompt: '',
  size: '1024x1024',
  count: 1,
  acknowledged: false
})

watch(
  () => [props.open, props.keys] as const,
  ([isOpen, keyList]) => {
    if (isOpen && !form.apiKeyId && keyList.length > 0) {
      form.apiKeyId = keyList[0].id
    }
  },
  { immediate: true }
)

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
    :title="isZh ? '新建图像生成任务' : 'New Image Generation Task'"
    :description="
      isZh
        ? '异步生成产物将归档至受控私有存储（保留窗口 24 小时）。'
        : 'Generated artifacts are archived in private scope-bound storage (24h retention window).'
    "
    @update:open="(val) => !val && handleClose()"
    @close="handleClose"
  >
    <div data-testid="task-create-modal" class="space-y-3 text-xs">
      <div class="space-y-1.5">
        <div class="flex items-center justify-between gap-2">
          <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
            {{ isZh ? 'API 密钥' : 'API Key' }}
          </span>
          <button
            type="button"
            class="oh-focus-ring rounded font-mono text-[11px] text-[var(--oh-accent-text,var(--brand-text,#d97706))] underline"
            @click="emit('navigate-keys')"
          >
            {{ isZh ? '管理密钥 →' : 'Manage Keys →' }}
          </button>
        </div>
        <select
          v-if="keys.length > 0"
          v-model="form.apiKeyId"
          class="h-8 w-full rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-bg-subtle,var(--surface-2))] px-2 font-mono text-xs tabular-nums text-[var(--oh-ink,var(--foreground))]"
        >
          <option value="">{{ isZh ? '选择密钥…' : 'Select key…' }}</option>
          <option v-for="k in keys" :key="k.id" :value="k.id">
            {{ k.name }} (#{{ k.id }}{{ k.prefix ? ` · ${k.prefix}` : '' }}{{ k.scopeLabel ? ` · ${k.scopeLabel}` : '' }})
          </option>
        </select>
        <div
          v-else
          data-testid="task-empty-keys-guidance"
          class="space-y-2 rounded-[var(--oh-r-sm,6px)] border border-dashed border-[var(--oh-accent-border,rgba(245,158,11,0.45))] bg-[var(--oh-accent-subtle,rgba(245,158,11,0.08))] p-2.5"
        >
          <p class="text-[11px] leading-relaxed text-[var(--oh-ink,var(--foreground))]">
            {{
              isZh
                ? '当前项目或个人账户下暂无可用 API 密钥。请先前往「API 密钥」页面创建密钥，或输入已有数字密钥 ID。'
                : 'No active user or project API keys found. Create an API key first, or enter an existing numeric key ID.'
            }}
          </p>
          <div class="flex flex-wrap items-center gap-2">
            <Button variant="secondary" size="xs" @click="emit('navigate-keys')">
              {{ isZh ? '前往创建 API 密钥 →' : 'Go to API Keys →' }}
            </Button>
            <input
              v-model="form.apiKeyId"
              type="text"
              :placeholder="isZh ? '或输入密钥 ID（如 12）' : 'Or enter Key ID (e.g. 12)'"
              class="h-7 flex-1 rounded-[var(--oh-r-sm,6px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] px-2 font-mono text-xs tabular-nums text-[var(--oh-ink,var(--foreground))]"
            />
          </div>
        </div>
      </div>

      <label class="block space-y-1">
        <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
          {{ isZh ? '图像模型' : 'Image Model' }}
        </span>
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
          <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
            {{ isZh ? '图像尺寸' : 'Image Size' }}
          </span>
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
          <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
            {{ isZh ? '生成张数 (1–4)' : 'Count (1–4)' }}
          </span>
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
        <span class="text-[var(--oh-ink-secondary,var(--muted-foreground))]">
          {{ isZh ? '图像提示词' : 'Prompt' }}
        </span>
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
              ? '我确认本任务将按当前生效价格版本独立冻结预算并按回执计费。'
              : 'I acknowledge this task independently reserves budget and settles under the active price version.'
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
