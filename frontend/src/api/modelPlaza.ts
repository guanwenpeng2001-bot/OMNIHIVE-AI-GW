/**
 * Model Plaza API（公开端点，可匿名访问）
 * 以分组为中心的模型价目：分组信息 + 模型渠道定价。
 * 带 token 请求时后端会额外返回专属分组与用户专属倍率。
 */

import { apiClient } from './client'
import type { UserPricingInterval, UserSupportedModelPricing } from './channels'

/**
 * 官方参考价（USD per token，由服务端从计费目录取得；字段缺失/为 null = 无有来源的参考价）。
 * 产品已退役该展示：当前后端不再查询参考价，因此实际响应里恒为 null；
 * 前端只在它确实带有数值时才把它当作折扣基准，绝不自行推算。
 */
export interface PlazaOfficialPricing {
  input_price: number | null
  output_price: number | null
  /** 5m 缓存写入（= LiteLLM cache_creation）。 */
  cache_write_price: number | null
  /** 1h 缓存写入（LiteLLM cache_creation_above_1hr），多数模型缺失。 */
  cache_write_1h_price?: number | null
  cache_read_price: number | null
  /** 官方长上下文阶梯（多档模型才有），不受分组开关影响。 */
  intervals?: UserPricingInterval[]
}

/** 单个观测事实：guaranteed = 所有观测一致时的保证值；available = 出现的取值集合；unknown = 无依据。 */
export interface PlazaFact<Value> {
  guaranteed: Value | null
  available: Value[]
  unknown: boolean
}

/** 上下文/长度类事实：kind 说明这个上限的含义（input/total/unspecified）。 */
export interface PlazaLimit extends PlazaFact<number> {
  kind?: string
  observed_minimum: number | null
}

/** 模态集合：available 为出现过的模态，guaranteed 为所有观测一致时的交集。 */
export interface PlazaModalities {
  guaranteed: string[] | null
  available: string[]
  unknown: boolean
}

/**
 * 该模型在当前分组下的有效规格：来自与 /v1/models 同一次账号目录读取的观测聚合。
 * 缺失或互相冲突的字段保持 unknown，不当作 0/false。
 */
export interface PlazaModelCapabilities {
  input_context: PlazaLimit
  total_context: PlazaLimit
  unspecified_context: PlazaLimit
  max_output_tokens: PlazaLimit
  embedding_dimensions: PlazaFact<number>
  reasoning: PlazaFact<boolean>
  supports_vision: PlazaFact<boolean>
  supports_function_calling: PlazaFact<boolean>
  supports_prompt_caching: PlazaFact<boolean>
  supports_pdf: PlazaFact<boolean>
  input_modalities: PlazaModalities
  output_modalities: PlazaModalities
}

/**
 * 多档时的计价基准：
 * - whole_request：整单按所在档单价计价（目录阶梯、渠道区间）；
 * - marginal：仅超出阈值的部分按该档单价计价（平台旧规则）。
 */
export type PlazaLongContextBasis = 'whole_request' | 'marginal'

/** 分时倍率时段：配置时区当天 [start_time, end_time) 内整单实付乘 multiplier。 */
export interface PlazaTimePricingPeriod {
  start_time: string
  end_time: string
  multiplier: number
}

/** 计费会生效的分时倍率（仅倍率 ≠ 1 的时段，已按开始时间升序）。 */
export interface PlazaTimePricing {
  /** IANA 时区名，如 Asia/Shanghai。 */
  timezone: string
  /** true 时时段仅周一至周五生效，周末整天按标准价计费。 */
  weekdays_only?: boolean
  periods: PlazaTimePricingPeriod[]
}

export interface PlazaModel {
  name: string
  platform: string
  /** 实收口径的展示定价：档位可提供绝对单价或相对基础价倍率；均为标准时段价。 */
  pricing: UserSupportedModelPricing | null
  /**
   * 有来源的参考价；null = 服务端没有可引用的参考价，此时界面不展示任何折扣/划线价。
   * @see PlazaOfficialPricing
   */
  official_pricing: PlazaOfficialPricing | null
  /** 有效规格（上下文/模态/能力）；服务端无任何可核实事实时省略。 */
  capabilities?: PlazaModelCapabilities
  /** 仅多档模型返回。 */
  long_context_basis?: PlazaLongContextBasis
  /** 仅配置了分时倍率的模型返回。 */
  time_pricing?: PlazaTimePricing
}

export interface ModelPlazaGroup {
  id: number
  name: string
  description: string
  platform: string
  /** 'standard' | 'subscription' */
  subscription_type: string
  rate_multiplier: number
  /** 登录且管理员为该用户配了专属倍率时返回；生效倍率 = user_rate ?? rate_multiplier。 */
  user_rate_multiplier?: number
  peak_rate_enabled: boolean
  peak_start: string
  peak_end: string
  peak_rate_multiplier: number
  is_exclusive: boolean
  /** 生图独立倍率：true 时图片计费模型的实付倍率取 image_rate_multiplier，不取分组/专属倍率。 */
  image_rate_independent: boolean
  image_rate_multiplier: number
  /** 分组是否启用长上下文阶梯计费；false 时实付列只展示最低档。 */
  long_context_pricing_enabled: boolean
  models: PlazaModel[]
}

export interface ModelPlazaResponse {
  /** 管理员配置的全局价格说明（Markdown）。 */
  description: string
  groups: ModelPlazaGroup[]
}

/** 获取模型广场数据。开关未启用时后端返回 404。 */
export async function getModelPlaza(options?: { signal?: AbortSignal }): Promise<ModelPlazaResponse> {
  const { data } = await apiClient.get<ModelPlazaResponse>('/model-plaza', {
    signal: options?.signal
  })
  return data
}

export const modelPlazaAPI = { getModelPlaza }

export default modelPlazaAPI
