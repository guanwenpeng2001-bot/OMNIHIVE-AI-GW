import { computed, onScopeDispose, ref, toValue, watch, type MaybeRefOrGetter } from 'vue'
import { getPlatformModelPresets, type PlatformModelPreset } from '@/api/admin/platformModelPresets'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { ModelCandidateIdentity } from './useModelWhitelist'

// Presets are editorial candidates, never evidence of account or Key access.
export function selectPlatformModelPresets(
  presets: PlatformModelPreset[], platforms: string[], identity: ModelCandidateIdentity = {}
): PlatformModelPreset[] {
  if (['bedrock', 'service_account'].includes(identity.type ?? '') && !identity.profile) return []
  return presets.filter(preset => platforms.includes(preset.platform) &&
    (!identity.profile || preset.provider_profile === identity.profile))
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
