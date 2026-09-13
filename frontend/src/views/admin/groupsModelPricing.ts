import type { ChannelModelPricing } from '@/api/admin/channels'
import {
  apiIntervalsToForm, createDefaultTimePricingForm, formIntervalsToAPI,
  mTokToPerToken, perTokenToMTok, toNullableNumber,
  type PricingFormEntry,
} from '@/components/admin/channel/types'

export interface GroupPricingEntry extends PricingFormEntry {
  platform: string
  original?: ChannelModelPricing
}

const tokenPriceFields = [
  'input_price', 'output_price', 'cache_write_price', 'cache_write_1h_price',
  'cache_read_price', 'image_input_price', 'image_output_price',
] as const

export function emptyGroupPricing(platform = ''): GroupPricingEntry {
  return {
    platform, models: [], billing_mode: 'token', input_price: null, output_price: null,
    cache_write_price: null, cache_write_1h_price: null, cache_read_price: null,
    image_input_price: null, image_output_price: null, per_request_price: null,
    intervals: [], time_pricing: createDefaultTimePricingForm(),
  }
}

export function groupPricingFromAPI(pricing?: ChannelModelPricing[]): GroupPricingEntry[] {
  return (pricing || []).map(original => {
    const entry = {
      ...emptyGroupPricing(original.platform),
      models: [...original.models], billing_mode: original.billing_mode || 'token',
      per_request_price: original.per_request_price,
      intervals: apiIntervalsToForm(original.intervals || []),
      // Keep the wire snapshot, including fields this editor does not understand.
      original: JSON.parse(JSON.stringify(original)) as ChannelModelPricing,
    }
    for (const field of tokenPriceFields) entry[field] = perTokenToMTok(original[field])
    return entry
  })
}

export function isReadOnlyGroupPricing(entry: GroupPricingEntry): boolean {
  const original = entry.original
  return !!original && !!(
    original.intervals?.length || original.native_pricing_source ||
    original.thinking_pricing || original.explicit_cache_pricing ||
    original.batch_chat_pricing || original.time_pricing
  )
}

export function groupPricingToAPI(pricing: GroupPricingEntry[], platform: string): ChannelModelPricing[] {
  return pricing.map(entry => {
    if (!entry.models.length || entry.models.some(model => !model.trim())) {
      throw new Error('Each pricing card requires a model; use Delete to remove a card.')
    }
    const provider = entry.platform || (platform === 'composite' ? '' : platform)
    if (!provider || provider === 'composite') {
      throw new Error('Select the actual provider for each pricing card.')
    }
    if (isReadOnlyGroupPricing(entry)) return JSON.parse(JSON.stringify(entry.original))

    const original = entry.original
    const result: ChannelModelPricing = original
      ? JSON.parse(JSON.stringify(original))
      : { ...emptyGroupPricing(provider), intervals: [], time_pricing: null } as ChannelModelPricing
    result.platform = provider
    if (!original || JSON.stringify(entry.models) !== JSON.stringify(original.models)) result.models = [...entry.models]
    if (!original || entry.billing_mode !== (original.billing_mode || 'token')) result.billing_mode = entry.billing_mode
    for (const field of tokenPriceFields) {
      // Compare display values before conversion to avoid rounding untouched wire prices.
      if (!original || toNullableNumber(entry[field]) !== perTokenToMTok(original[field])) {
        result[field] = mTokToPerToken(entry[field])
      }
    }
    if (!original || toNullableNumber(entry.per_request_price) !== toNullableNumber(original.per_request_price)) {
      result.per_request_price = toNullableNumber(entry.per_request_price)
    }
    if (!original || JSON.stringify(entry.intervals) !== JSON.stringify(apiIntervalsToForm(original.intervals || []))) {
      result.intervals = formIntervalsToAPI(entry.intervals)
    }
    return result
  })
}
