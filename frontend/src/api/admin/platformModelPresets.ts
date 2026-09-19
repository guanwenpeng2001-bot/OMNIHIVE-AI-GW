import { apiClient } from '../client'
import type { PlatformModelPreset } from '@/types'
export type { PlatformModelPreset } from '@/types'

// Editorial candidates never establish account entitlement.
export async function getPlatformModelPresets(): Promise<PlatformModelPreset[]> {
  const { data } = await apiClient.get<PlatformModelPreset[]>('/admin/platform-model-presets')
  return data
}
