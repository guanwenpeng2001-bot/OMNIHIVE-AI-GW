<template>
  <AccountEndpointPresets :presets="presets" :current-url="currentUrl" test-id="cn-base-url-preset" @select="emit('select', $event)" />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import AccountEndpointPresets from './AccountEndpointPresets.vue'
import { CN_BASE_URL_PRESETS, type CnProviderPlatform, type CnBaseUrlPreset } from './credentialsBuilder'

// 国产供应商快捷端点：点击把预设地址（及对应账号类型/协议）回填到调用方。
// 与 Grok 预设一致，仅作快速填充，输入框仍接受任意第三方转发地址。
// 传入 protocol 时只显示该协议档的预设（协议 × 账号类型正交分档）。
const props = defineProps<{
  platform: CnProviderPlatform
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

const presets = computed(() => {
  const all = CN_BASE_URL_PRESETS[props.platform] ?? []
  // 只按协议过滤：同协议下 payg/coding 两档都展示，点击即同时切换账号类型。
  if (props.protocol == null) return all
  return all.filter(p => p.protocol === props.protocol)
})

</script>
