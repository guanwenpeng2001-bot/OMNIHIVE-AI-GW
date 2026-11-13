<template>
  <AccountEndpointPresets class="rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-2" :presets="presets" :current-url="currentUrl" test-id="cn-base-url-preset" @select="emit('select', $event)" />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AccountEndpointPresets from './AccountEndpointPresets.vue'
import { CN_BASE_URL_PRESETS, type CnBaseUrlPresetPlatform, type CnBaseUrlPreset } from './credentialsBuilder'

// 国产供应商与后加四家（混元 / 火山方舟 / 千帆 / Mistral）的官方站点快捷填充：点击回填地址
// （及对应账号类型/协议）。与 Grok 预设一致，仅作快速填充，输入框仍接受任意第三方转发地址。
// 传入 mode / protocol 时只显示该档的预设：套餐与按量付费的 Base URL 与 Key 互不通用，
// 混在同一列表里点错就会打到错的计费口径（火山方舟明确：用按量地址不会消耗套餐额度，而是额外计费）。
const props = defineProps<{
  platform: CnBaseUrlPresetPlatform
  /** 当前已选账号类型，用于过滤和高亮匹配的预设 */
  mode?: 'payg' | 'coding'
  /** 当前已选 API 协议，用于过滤和高亮匹配的预设 */
  protocol?: 'adaptive' | 'chat_completions' | 'anthropic' | 'responses'
  /** 当前输入框中的 base url，用于高亮完全匹配项 */
  currentUrl?: string
}>()

const emit = defineEmits<{
  (e: 'select', preset: CnBaseUrlPreset): void
}>()

const { t } = useI18n()

const presets = computed<CnBaseUrlPreset[]>(() => {
  const all = CN_BASE_URL_PRESETS[props.platform] ?? []
  const byMode = props.mode == null ? all : all.filter(preset => preset.mode === props.mode)
  const matched = props.protocol == null ? byMode : byMode.filter(preset => preset.protocol === props.protocol)
  // labelKey 只用于需要按语言展示的档位名（千帆套餐的企业版），专有名词仍走 label。
  return matched.map(preset => (preset.labelKey ? { ...preset, label: t(preset.labelKey) } : preset))
})
</script>
