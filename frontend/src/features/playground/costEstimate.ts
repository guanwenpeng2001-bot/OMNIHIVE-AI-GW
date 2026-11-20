import type { CatalogModelItem, CatalogModelRate } from '@/features/console/catalogApi'
import type {
  ExactQuote,
  PlaygroundCostEstimate,
  PlaygroundMessageDraft,
  PlaygroundProtocol,
  PlaygroundSideConfig,
  PlaygroundSubmit
} from './types'

export const MAX_PLAYGROUND_BODY_BYTES = 256 * 1024
export const DEFAULT_MAX_OUTPUT_TOKENS = 1024
export const MAX_ALLOWED_OUTPUT_TOKENS = 32768

export function clampMaxOutputTokens(value: number | null | undefined): number {
  if (value === null || value === undefined || !Number.isFinite(value)) {
    return DEFAULT_MAX_OUTPUT_TOKENS
  }
  return Math.max(1, Math.min(MAX_ALLOWED_OUTPUT_TOKENS, Math.trunc(value)))
}

export function buildNativePlaygroundInput(
  protocol: PlaygroundProtocol,
  params: {
    systemPrompt: string
    messages: PlaygroundMessageDraft[]
    temperature: number
    maxOutputTokens: number
    reasoningEffort?: '' | 'low' | 'medium' | 'high'
    toolsJson?: string
  }
): Record<string, unknown> {
  const maxOut = clampMaxOutputTokens(params.maxOutputTokens)
  const temp = Number.isFinite(params.temperature)
    ? Math.max(0, Math.min(2, Number(params.temperature.toFixed(2))))
    : 0.7
  const system = params.systemPrompt.trim()
  let parsedTools: unknown[] | undefined
  if (params.toolsJson && params.toolsJson.trim()) {
    const raw = JSON.parse(params.toolsJson)
    if (!Array.isArray(raw)) {
      throw new Error('Tools definition must be a JSON array')
    }
    parsedTools = raw
  }

  const activeMessages = params.messages.filter((m) => m.content.trim().length > 0)

  if (protocol === 'openai_chat') {
    const chatMessages: Array<Record<string, unknown>> = []
    if (system) {
      chatMessages.push({ role: 'system', content: system })
    }
    for (const msg of activeMessages) {
      if (msg.role === 'tool') {
        chatMessages.push({
          role: 'tool',
          tool_call_id: msg.toolCallId || 'call_1',
          content: msg.content
        })
      } else {
        chatMessages.push({ role: msg.role, content: msg.content })
      }
    }
    const body: Record<string, unknown> = {
      messages: chatMessages,
      max_completion_tokens: maxOut,
      temperature: temp
    }
    if (params.reasoningEffort) {
      body.reasoning_effort = params.reasoningEffort
    }
    if (parsedTools && parsedTools.length > 0) {
      body.tools = parsedTools
    }
    return body
  }

  if (protocol === 'openai_responses') {
    const inputItems = activeMessages.map((msg) => ({
      role: msg.role === 'tool' ? 'user' : msg.role,
      content: msg.content
    }))
    const body: Record<string, unknown> = {
      input: inputItems,
      max_output_tokens: maxOut,
      temperature: temp
    }
    if (system) {
      body.instructions = system
    }
    if (params.reasoningEffort) {
      body.reasoning = { effort: params.reasoningEffort }
    }
    if (parsedTools && parsedTools.length > 0) {
      body.tools = parsedTools
    }
    return body
  }

  if (protocol === 'anthropic_messages') {
    const anthropicMessages = activeMessages.map((msg) => ({
      role: msg.role === 'assistant' ? 'assistant' : 'user',
      content: msg.content
    }))
    const body: Record<string, unknown> = {
      messages: anthropicMessages,
      max_tokens: maxOut,
      temperature: temp
    }
    if (system) {
      body.system = system
    }
    if (params.reasoningEffort) {
      const budget =
        params.reasoningEffort === 'high'
          ? Math.min(maxOut - 1, 8192)
          : params.reasoningEffort === 'medium'
            ? Math.min(maxOut - 1, 4096)
            : Math.min(maxOut - 1, 1024)
      if (budget >= 256) {
        body.thinking = { type: 'enabled', budget_tokens: budget }
      }
    }
    if (parsedTools && parsedTools.length > 0) {
      body.tools = parsedTools
    }
    return body
  }

  // gemini_generate
  const contents = activeMessages.map((msg) => ({
    role: msg.role === 'assistant' ? 'model' : 'user',
    parts: [{ text: msg.content }]
  }))
  const body: Record<string, unknown> = {
    contents,
    generationConfig: {
      maxOutputTokens: maxOut,
      temperature: temp
    }
  }
  if (system) {
    body.systemInstruction = { parts: [{ text: system }] }
  }
  if (parsedTools && parsedTools.length > 0) {
    body.tools = parsedTools
  }
  return body
}

export function buildPlaygroundSubmitPayload(
  side: PlaygroundSideConfig,
  nativeInput: Record<string, unknown>,
  payerUserId?: string
): PlaygroundSubmit {
  const payload: PlaygroundSubmit = {
    api_key_id: String(side.apiKeyId).trim(),
    model_id: side.modelId.trim(),
    protocol: side.protocol,
    action: 'text.chat',
    input: nativeInput,
    options: {
      stream: side.stream,
      include_raw_events: side.stream ? side.includeRawEvents : false,
      retain_text_preview: side.retainTextPreview
    },
    funding_acknowledged: true
  }
  if (payerUserId && /^[1-9]\d*$/.test(payerUserId)) {
    payload.payer_user_id = payerUserId
  }
  return payload
}

function computeRateCost(tokens: number, rate: CatalogModelRate | null): number | null {
  if (!rate) return null
  const amount = Number(rate.amount)
  const per = Number(rate.per || '1')
  if (!Number.isFinite(amount) || !Number.isFinite(per) || per <= 0) return null
  return (tokens * amount) / per
}

function utf8ByteLength(text: string): number {
  if (typeof TextEncoder !== 'undefined') {
    return new TextEncoder().encode(text).byteLength
  }
  return text.length
}

export function estimateInputTokensFromPayload(nativeInput: Record<string, unknown>): number {
  const serialized = JSON.stringify(nativeInput)
  const bytes = utf8ByteLength(serialized)
  return Math.max(1, Math.ceil(bytes / 4))
}

export function estimatePlaygroundRunCost(
  side: PlaygroundSideConfig,
  payload: PlaygroundSubmit,
  catalogModel: CatalogModelItem | null
): PlaygroundCostEstimate {
  const serialized = JSON.stringify(payload)
  const payloadBytes = utf8ByteLength(serialized)
  const estimatedInputTokens = estimateInputTokensFromPayload(payload.input)
  const maxOutputTokens = clampMaxOutputTokens(side.maxOutputTokens)
  const inputRate = catalogModel?.inputRate ?? null
  const outputRate = catalogModel?.outputRate ?? null
  const currency = inputRate?.currency || outputRate?.currency || 'USD'

  const inCostNum = computeRateCost(estimatedInputTokens, inputRate)
  const outCostNum = computeRateCost(maxOutputTokens, outputRate)
  const rateKnown = inCostNum !== null || outCostNum !== null
  const totalNum =
    rateKnown ? (inCostNum ?? 0) + (outCostNum ?? 0) : null
  const isFreeCatalog = catalogModel?.priceState === 'free'

  return {
    sideLabel: side.label,
    modelId: side.modelId,
    protocol: side.protocol,
    apiKeyId: side.apiKeyId,
    estimatedInputTokens,
    maxOutputTokens,
    currency,
    inputRate,
    outputRate,
    estimatedInputCost: inCostNum !== null ? inCostNum.toFixed(8) : null,
    maxOutputCost: outCostNum !== null ? outCostNum.toFixed(8) : null,
    maxTotalCost: isFreeCatalog ? '0.00000000' : totalNum !== null ? totalNum.toFixed(8) : null,
    rateKnown: isFreeCatalog || rateKnown,
    payloadBytes,
    exceedsBodyLimit: payloadBytes > MAX_PLAYGROUND_BODY_BYTES,
    quoteState: isFreeCatalog ? 'free' : rateKnown ? 'fallback_catalog' : 'unpriced',
    quoteSource: rateKnown ? 'catalog' : null,
    priceVersion: null,
    fundingSource: null,
    budgetBlocked: false,
    unpricedReason: isFreeCatalog || rateKnown ? null : 'catalog_unpriced'
  }
}

export function resolveCatalogQuoteModelId(
  modelId: string,
  catalogModel: CatalogModelItem | null
): string | null {
  const fromCatalog = catalogModel?.id?.trim() || ''
  if (/^m_[0-9a-f]{32}$/.test(fromCatalog)) {
    return fromCatalog
  }
  const raw = modelId.trim()
  if (/^m_[0-9a-f]{32}$/.test(raw)) {
    return raw
  }
  return null
}

function rateFromQuoteLine(
  numerator: string | undefined,
  denominator: string | undefined,
  currency: string,
  unit: string
): CatalogModelRate | null {
  if (!numerator || !denominator) return null
  const num = Number(numerator)
  const den = Number(denominator)
  if (!Number.isFinite(num) || !Number.isFinite(den) || den <= 0) return null
  return {
    currency,
    amount: numerator,
    unit,
    per: denominator
  }
}

export function formatModelRatePerMillion(
  rate: CatalogModelRate | null,
  fallbackCurrency = 'USD'
): string | null {
  if (!rate) return null
  const amount = Number(rate.amount)
  const per = Number(rate.per || '1')
  if (!Number.isFinite(amount) || !Number.isFinite(per) || per <= 0) return null
  const perMillion = (amount * 1_000_000) / per
  const cur = (rate.currency || fallbackCurrency).toUpperCase()
  const symbol = cur === 'USD' ? '$' : `${cur} `
  const formatted = perMillion === 0
    ? '0.00'
    : perMillion >= 0.01
      ? perMillion.toFixed(4).replace(/\.?0+$/, (m) => (m.startsWith('.') ? '.00' : ''))
      : perMillion.toFixed(6).replace(/0+$/, '').replace(/\.$/, '.00')
  return `${symbol}${formatted} / 1M`
}

export function applyAuthoritativeQuoteToEstimate(
  base: PlaygroundCostEstimate,
  quote: ExactQuote,
  options?: {
    keyScoped?: boolean
    fundingSource?: string | null
    budgetBlocked?: boolean
  }
): PlaygroundCostEstimate {
  const currency = quote.estimated_charge?.currency || base.currency || 'USD'
  const inputLine = quote.components?.find((c) => c.kind === 'input')
  const outputLine = quote.components?.find((c) => c.kind === 'output')
  const derivedInputRate =
    rateFromQuoteLine(inputLine?.numerator, inputLine?.denominator, currency, 'input_token') ??
    base.inputRate
  const derivedOutputRate =
    rateFromQuoteLine(outputLine?.numerator, outputLine?.denominator, currency, 'output_token') ??
    base.outputRate

  if (quote.state === 'free') {
    return {
      ...base,
      currency,
      inputRate: derivedInputRate,
      outputRate: derivedOutputRate,
      estimatedInputCost: '0.00000000',
      maxOutputCost: '0.00000000',
      maxTotalCost: quote.estimated_charge?.amount ?? '0.00000000',
      rateKnown: true,
      quoteState: 'free',
      quoteSource: quote.source || (options?.keyScoped ? 'frozen' : 'reference'),
      priceVersion: quote.price_version ?? null,
      fundingSource: options?.fundingSource ?? base.fundingSource ?? null,
      budgetBlocked: Boolean(options?.budgetBlocked),
      unpricedReason: null,
      authoritativeComponents: quote.components ?? []
    }
  }

  if (quote.state === 'estimated' && quote.estimated_charge) {
    return {
      ...base,
      currency: quote.estimated_charge.currency || currency,
      inputRate: derivedInputRate,
      outputRate: derivedOutputRate,
      estimatedInputCost: inputLine?.raw_amount ?? base.estimatedInputCost,
      maxOutputCost: outputLine?.raw_amount ?? base.maxOutputCost,
      maxTotalCost: quote.estimated_charge.amount,
      rateKnown: true,
      quoteState: 'authoritative',
      quoteSource: quote.source || (options?.keyScoped ? 'frozen' : 'reference'),
      priceVersion: quote.price_version ?? null,
      fundingSource: options?.fundingSource ?? base.fundingSource ?? null,
      budgetBlocked: Boolean(options?.budgetBlocked),
      unpricedReason: null,
      authoritativeComponents: quote.components ?? []
    }
  }

  return {
    ...base,
    inputRate: derivedInputRate,
    outputRate: derivedOutputRate,
    maxTotalCost: null,
    rateKnown: false,
    quoteState: 'unpriced',
    quoteSource: quote.source ?? null,
    priceVersion: quote.price_version ?? null,
    fundingSource: options?.fundingSource ?? base.fundingSource ?? null,
    budgetBlocked: Boolean(options?.budgetBlocked),
    unpricedReason: 'server_unpriced',
    authoritativeComponents: quote.components ?? []
  }
}
