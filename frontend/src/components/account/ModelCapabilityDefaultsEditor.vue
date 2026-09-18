<template>
  <details class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
    <summary class="cursor-pointer text-sm font-medium text-gray-700 dark:text-gray-200">{{ labels.title }}</summary>
    <div class="mt-3 space-y-2">
      <p :id="`${id}-hint`" class="input-hint">{{ labels.hint }}</p>
      <label :for="id" class="input-label">{{ labels.input }}</label>
      <textarea :id="id" :value="modelValue" rows="8" spellcheck="false"
        class="input font-mono text-xs" :aria-invalid="!!error" :aria-describedby="`${id}-hint ${id}-error`"
        :placeholder="labels.placeholder" @input="$emit('update:modelValue', ($event.target as HTMLTextAreaElement).value)" />
      <p :id="`${id}-error`" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error ? labels.invalid : '' }}</p>
      <details class="text-xs text-secondary">
        <summary class="cursor-pointer">{{ labels.example }}</summary>
        <p class="my-2">{{ labels.binding }}</p>
        <pre class="max-h-64 overflow-auto rounded-lg bg-gray-50 p-3 dark:bg-dark-800">{{ example }}</pre>
      </details>
    </div>
  </details>
</template>

<script setup lang="ts">
import { computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { parseModelCapabilityDefaults } from './modelCapabilityDefaults'

const props = defineProps<{ modelValue: string }>()
defineEmits<{ 'update:modelValue': [value: string] }>()
const id = useId()
const { locale } = useI18n()
const labels = computed(() => (locale?.value ?? 'zh').startsWith('zh') ? {
  title: '私有部署规格（JSON）', input: '账号模型能力声明',
  hint: '由部署持有人按原始模型 ID 声明实际输入/输出上限、模态、推理控制和函数调用能力，并填写依据。原生有效字段优先；声明不会增加授权或路由。预览检查具体采用字段和缺口，保存账号后重新预览并应用目录。清空表示移除声明。',
  invalid: '请输入有效声明：schema_version 为 1，reason 为核验依据，models 为按原始模型 ID 索引的非空对象。',
  placeholder: '留空表示不提供账号部署规格', example: '查看格式示例（数值仅演示，请替换为真实部署规格）',
  binding: 'confirmed_by、confirmed_at 和身份信息由服务端生成。账号身份变化后，核验部署规格并移除旧 identity_fingerprint，再预览和保存以重新确认。reasoning_control.kind 支持 none / effort / toggle / budget / adaptive / fixed / unknown；仅填写实际支持的控制形式。'
} : {
  title: 'Private deployment specifications (JSON)', input: 'Account model capability declaration',
  hint: 'The deployment owner declares actual input/output limits, modalities, reasoning controls and function calling by original model ID, with confirmation and evidence. Native fields take priority; declarations do not grant access or routing. Preview field sources and gaps, then save the account and preview/apply its catalog. Clear to remove declarations.',
  invalid: 'Enter a valid declaration with schema_version: 1, a verification reason, and a nonempty models object indexed by original ID.',
  placeholder: 'Leave empty when no deployment specification is declared', example: 'Format example (replace illustrative values with verified deployment facts)',
  binding: 'The server generates confirmed_by, confirmed_at and identity information. After an identity change, verify the deployment facts and remove the old identity_fingerprint before previewing and saving to reconfirm. reasoning_control.kind supports none / effort / toggle / budget / adaptive / fixed / unknown; declare only controls the deployment actually supports.'
})
const example = JSON.stringify({
  schema_version: 1,
  reason: 'Deployment configuration verified by the deployment owner',
  models: {
    'original-private-model-id': {
      task_type: 'text_generation', max_input_tokens: 32768, max_output_tokens: 4096,
      input_modalities: ['text'], input_modalities_status: 'complete', output_modalities: ['text'],
      reasoning: true, reasoning_control: { kind: 'budget', budget_min: 0, budget_max: 2048 },
      supports_function_calling: false
    }
  }
}, null, 2)
const error = computed(() => {
  try { parseModelCapabilityDefaults(props.modelValue); return false } catch { return true }
})
</script>
