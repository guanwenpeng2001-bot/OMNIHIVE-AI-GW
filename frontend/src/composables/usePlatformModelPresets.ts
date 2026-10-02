import { computed, onScopeDispose, ref, toValue, watch, type MaybeRefOrGetter } from 'vue'
import { getPlatformModelPresets, type PlatformModelPreset } from '@/api/admin/platformModelPresets'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { ModelCandidateIdentity } from './useModelWhitelist'

// Presets are editorial candidates, never evidence of account or Key access.
export function selectPlatformModelPresets(
  presets: PlatformModelPreset[], platforms: string[], identity: ModelCandidateIdentity = {}
): PlatformModelPreset[] {
  return presets.filter(preset => platforms.some(platform => {
    const concrete = platform === 'vertex' ? 'gemini' : platform
    if (preset.platform !== concrete) return false
    if (identity.profile) return preset.provider_profile === identity.profile
    if (identity.type === 'bedrock' || platform === 'bedrock') return false
    if (identity.type === 'service_account' || platform === 'vertex') {
      const hosted = concrete === 'gemini' ? 'vertex-gemini'
        : concrete === 'anthropic' ? 'vertex-anthropic' : ''
      return preset.provider_profile === hosted
    }
    if (['vertex-gemini', 'vertex-anthropic'].includes(preset.provider_profile)) return false
    const profile = inferredPresetProfile(concrete, identity)
    return profile === undefined || preset.provider_profile === profile
  }))
}

// Only known bases establish a regional/plan identity. Region is an account
// location, not permission to override the product selected by its endpoint.
function inferredPresetProfile(platform: string, identity: ModelCandidateIdentity): string | undefined {
  const coding = identity.mode === 'coding'
  const defaults: Record<string, string> = {
    kimi: coding ? 'https://api.kimi.com/coding/v1' : 'https://api.moonshot.cn/v1',
    zhipu: coding ? 'https://open.bigmodel.cn/api/coding/paas/v4' : 'https://open.bigmodel.cn/api/paas/v4',
    minimax: 'https://api.minimaxi.com/v1',
    qianwen: coding ? 'https://coding.dashscope.aliyuncs.com/v1' : 'https://dashscope.aliyuncs.com/compatible-mode/v1'
  }
  if (!(platform in defaults)) return undefined
  let url: URL
  try { url = new URL(identity.host?.trim() || defaults[platform]) } catch { return '' }
  if (!['https:', 'http:'].includes(url.protocol)) return ''
  const host = url.hostname.toLowerCase()
  const path = url.pathname.replace(/\/+$/, '')
  const at = (...paths: string[]) => paths.includes(path)
  switch (platform) {
    case 'kimi':
      if (host === 'api.kimi.com' && at('/coding', '/coding/v1') && identity.mode !== 'payg') return 'kimi-for-coding'
      if (!coding && at('', '/v1', '/anthropic')) {
        if (host === 'api.moonshot.cn') return 'moonshotai-cn'
        if (host === 'api.moonshot.ai') return 'moonshotai'
      }
      break
    case 'zhipu': {
      const profile = host === 'open.bigmodel.cn' ? 'zhipuai' : host === 'api.z.ai' ? 'zai' : ''
      if (!profile) break
      if (at('/api/coding/paas/v4') && identity.mode !== 'payg') return profile + '-coding-plan'
      if (at('', '/api/paas/v4', '/api/v1', '/api/anthropic')) return profile + (coding ? '-coding-plan' : '')
      break
    }
    case 'minimax': {
      const profile = host === 'api.minimaxi.com' ? 'minimax-cn' : host === 'api.minimax.io' ? 'minimax' : ''
      if (profile && at('', '/v1', '/anthropic')) return profile + (coding ? '-coding-plan' : '')
      break
    }
    case 'qianwen':
      if (host === 'coding.dashscope.aliyuncs.com' && at('', '/v1', '/apps/anthropic') && identity.mode !== 'payg') return 'alibaba-coding-plan-cn'
      if (!coding && at('', '/compatible-mode/v1', '/apps/anthropic', '/api/v1')) {
        if (host === 'dashscope.aliyuncs.com') return 'alibaba-cn'
        if (host === 'dashscope-intl.aliyuncs.com') return 'alibaba'
      }
  }
  return ''
}

export function usePlatformModelPresets(
  platforms: MaybeRefOrGetter<string[]>,
  identity: MaybeRefOrGetter<ModelCandidateIdentity> = {},
  active: MaybeRefOrGetter<boolean> = true
) {
  const presets = ref<PlatformModelPreset[]>([])
  const loading = ref(false)
  const error = ref('')
  const loaded = ref(false)
  let generation = 0
  let disposed = false
  const context = computed(() => JSON.stringify([toValue(active), toValue(platforms), toValue(identity)]))
  async function reload() {
    const request = ++generation
    presets.value = []
    error.value = ''
    loaded.value = false
    loading.value = false
    if (disposed || !toValue(active) || !toValue(platforms).length) return
    const requestedPlatforms = [...toValue(platforms)]
    const requestedIdentity = { ...toValue(identity) }
    loading.value = true
    try {
      const response = await getPlatformModelPresets()
      if (disposed || request !== generation) return
      presets.value = selectPlatformModelPresets(response, requestedPlatforms, requestedIdentity)
      loaded.value = true
    } catch (cause) {
      if (disposed || request !== generation) return
      error.value = extractApiErrorMessage(cause, 'Failed to load platform model presets')
    } finally {
      if (request === generation) loading.value = false
    }
  }
  watch(context, () => { void reload() }, { immediate: true, flush: 'sync' })
  onScopeDispose(() => { disposed = true; generation++ })
  const models = computed(() => [...new Set(presets.value.flatMap(preset => (preset.models ?? []).map(model => model.id)))])
  return { presets, models, loading, loaded, error, reload }
}
