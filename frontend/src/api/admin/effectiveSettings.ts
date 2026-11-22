import { apiClient } from '@/api/client'
import type { components } from '@/api/generated/openapi'

export type EffectiveSettings = components['schemas']['EffectiveSettings']
export type SettingEffectiveValue = components['schemas']['SettingEffectiveValue']
export type ResetSettingOverridesInput = components['schemas']['ResetSettingOverridesInput']

export async function getEffectiveSettings(signal?: AbortSignal): Promise<EffectiveSettings> {
  const { data } = await apiClient.get<EffectiveSettings>('/admin/settings/effective', { signal })
  return data
}

export async function resetSettingOverrides(input: ResetSettingOverridesInput): Promise<EffectiveSettings> {
  const { data } = await apiClient.post<EffectiveSettings>('/admin/settings/reset', input)
  return data
}
