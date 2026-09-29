import { apiClient } from '../client'
import type { PlatformModelPreset } from '@/types/model'
export type { PlatformModelPreset } from '@/types/model'

// Editorial candidates never establish account entitlement.
export async function getPlatformModelPresets(): Promise<PlatformModelPreset[]> {
  const { data } = await apiClient.get<PlatformModelPreset[]>('/admin/platform-model-presets')
  return data
}
