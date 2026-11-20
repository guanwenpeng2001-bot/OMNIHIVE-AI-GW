export interface RateLike {
  amount: string
  currency: string
  per: string
  unit: string
}

export interface ExactRational {
  num: bigint
  den: bigint
}

export interface PriceFormatOptions {
  minDecimals?: number
  maxDecimals?: number
  locale?: string
  zeroAsFree?: boolean
  groupThousands?: boolean
  unknownLabel?: string
}

export interface ScenarioCostInput {
  inputRate?: RateLike | null
  cachedInputRate?: RateLike | null
  outputRate?: RateLike | null
  priceState?: string | null
  inputTokens?: string | number | null
  cachedInputTokens?: string | number | null
  outputTokens?: string | number | null
  requests?: string | number | null
  locale?: string
}

export interface ScenarioCostBreakdown {
  state: 'free' | 'estimated' | 'unpriced'
  currency: string
  uncachedInputTokens: string
  cachedInputTokens: string
  outputTokens: string
  requests: string
  totalUncachedInputTokens: string
  totalCachedInputTokens: string
  totalOutputTokens: string
  uncachedInputCost: string
  cachedInputCost: string
  outputCost: string
  singleRequestTotal: string
  totalDisplay: string
  usedFallbackCacheRate: boolean
}

export const TOKEN_RATE_UNITS = new Set([
  'token',
  'tokens',
  'input_token',
  'output_token',
  'cached_input_token',
  'cache_read_token',
  'cache_write_token',
  'cache_write_1h_token',
  'reasoning_token',
  'image_input_token',
  'image_output_token'
])

const ONE_MILLION = 1_000_000n

export function isTokenRateUnit(unit?: string | null): boolean {
  return Boolean(unit && TOKEN_RATE_UNITS.has(unit))
}

export function currencyPrefixFor(currency?: string | null): string {
  const normalized = (currency || 'USD').trim().toUpperCase()
  return normalized === 'USD' ? '$' : `${normalized} `
}

function isZhLocaleCode(locale?: string): boolean {
  return Boolean(locale && locale.toLowerCase().startsWith('zh'))
}

function gcdBigInt(a: bigint, b: bigint): bigint {
  let x = a < 0n ? -a : a
  let y = b < 0n ? -b : b
  while (y !== 0n) {
    const remainder = x % y
    x = y
    y = remainder
  }
  return x === 0n ? 1n : x
}

export function reduceRational(num: bigint, den: bigint): ExactRational {
  if (den <= 0n) {
    throw new Error('Denominator must be positive')
  }
  if (num === 0n) {
    return { num: 0n, den: 1n }
  }
  const divisor = gcdBigInt(num, den)
  return { num: num / divisor, den: den / divisor }
}

export function addRationals(a: ExactRational, b: ExactRational): ExactRational {
  return reduceRational(a.num * b.den + b.num * a.den, a.den * b.den)
}

/**
 * Parses a non-negative decimal (or scientific notation) string into an exact
 * BigInt rational `{ num, den }` without touching IEEE-754 floating point.
 */
export function parseNonNegativeDecimal(
  raw?: string | number | null
): ExactRational | null {
  if (raw === null || raw === undefined) return null
  const text = String(raw).trim()
  if (!text) return null

  const match = /^(\d+)(?:\.(\d+))?(?:[eE]([+-]?\d+))?$/.exec(text)
  if (!match) return null

  const whole = match[1]
  const fraction = match[2] ?? ''
  const exponent = match[3] ? Number.parseInt(match[3], 10) : 0
  if (!Number.isSafeInteger(exponent) || Math.abs(exponent) > 60) {
    return null
  }

  const digits = BigInt(whole + fraction)
  if (digits === 0n) {
    return { num: 0n, den: 1n }
  }

  const netPower = exponent - fraction.length
  if (netPower >= 0) {
    return { num: digits * 10n ** BigInt(netPower), den: 1n }
  }
  return reduceRational(digits, 10n ** BigInt(-netPower))
}

export function parseNonNegativeInt(
  raw: string | number | null | undefined,
  fallback: bigint
): bigint {
  if (raw === null || raw === undefined) return fallback
  const cleaned = String(raw).trim().replace(/,/g, '')
  if (!/^\d+$/.test(cleaned)) return fallback
  return BigInt(cleaned)
}

export function scaleRateToPerMillion(
  rate?: RateLike | null
): (ExactRational & { currency: string }) | null {
  if (!rate || !isTokenRateUnit(rate.unit)) return null
  const amount = parseNonNegativeDecimal(rate.amount)
  const per = parseNonNegativeDecimal(rate.per)
  if (!amount || !per || per.num === 0n) return null

  const scaled = reduceRational(
    amount.num * per.den * ONE_MILLION,
    amount.den * per.num
  )
  return {
    ...scaled,
    currency: rate.currency || 'USD'
  }
}

function groupIntegerDigits(digits: string): string {
  return digits.replace(/\B(?=(\d{3})+(?!\d))/g, ',')
}

export function formatExactRationalDecimal(
  rational: ExactRational,
  options: {
    minDecimals?: number
    maxDecimals?: number
    groupThousands?: boolean
  } = {}
): { text: string; underflow: boolean; isZero: boolean } {
  const minDecimals = Math.max(0, options.minDecimals ?? 2)
  const maxDecimals = Math.max(minDecimals, options.maxDecimals ?? 4)

  if (rational.num <= 0n) {
    const zeros = minDecimals > 0 ? `.${'0'.repeat(minDecimals)}` : ''
    return { text: `0${zeros}`, underflow: false, isZero: true }
  }

  const scale = 10n ** BigInt(maxDecimals)
  const rounded = (2n * rational.num * scale + rational.den) / (2n * rational.den)
  if (rounded === 0n) {
    const threshold =
      maxDecimals > 0 ? `0.${'0'.repeat(maxDecimals - 1)}1` : '1'
    return { text: threshold, underflow: true, isZero: false }
  }

  const intRaw = String(rounded / scale)
  const intFormatted = options.groupThousands ? groupIntegerDigits(intRaw) : intRaw
  if (maxDecimals === 0) {
    return { text: intFormatted, underflow: false, isZero: false }
  }

  let frac = String(rounded % scale).padStart(maxDecimals, '0')
  while (frac.length > minDecimals && frac.endsWith('0')) {
    frac = frac.slice(0, -1)
  }
  return {
    text: frac.length > 0 ? `${intFormatted}.${frac}` : intFormatted,
    underflow: false,
    isZero: false
  }
}

export function formatExactRationalCurrency(
  rational: ExactRational,
  currency = 'USD',
  options: PriceFormatOptions = {}
): string {
  const zh = isZhLocaleCode(options.locale)
  const zeroAsFree = options.zeroAsFree ?? false
  if (rational.num === 0n && zeroAsFree) {
    return zh ? '免费' : 'Free'
  }
  const prefix = currencyPrefixFor(currency)
  const formatted = formatExactRationalDecimal(rational, {
    minDecimals: options.minDecimals ?? 2,
    maxDecimals: options.maxDecimals ?? 4,
    groupThousands: options.groupThousands ?? false
  })
  if (formatted.underflow) {
    return `< ${prefix}${formatted.text}`
  }
  return `${prefix}${formatted.text}`
}

/**
 * Unified per-1M token rate presentation with exact string scaling and
 * adaptive 2..4 significant decimal digits across list, detail, compare, and pricing views.
 */
export function formatTokenRatePerMillion(
  rate?: RateLike | null,
  options: PriceFormatOptions = {}
): string {
  const unknown = options.unknownLabel ?? '—'
  const scaled = scaleRateToPerMillion(rate)
  if (!scaled) return unknown
  return formatExactRationalCurrency(scaled, scaled.currency, {
    minDecimals: options.minDecimals ?? 2,
    maxDecimals: options.maxDecimals ?? 4,
    locale: options.locale,
    zeroAsFree: options.zeroAsFree ?? true,
    groupThousands: options.groupThousands ?? false
  })
}

export function formatCatalogRateBand(
  rate?: RateLike | null,
  options: PriceFormatOptions = {}
): string {
  const unknown = options.unknownLabel ?? '—'
  if (!rate) return unknown
  if (isTokenRateUnit(rate.unit)) {
    const tokenPrice = formatTokenRatePerMillion(rate, {
      ...options,
      minDecimals: options.minDecimals ?? 2,
      maxDecimals: options.maxDecimals ?? 4
    })
    return tokenPrice === unknown ? unknown : `${tokenPrice} / 1M`
  }
  const parsedAmount = parseNonNegativeDecimal(rate.amount)
  const prefix = currencyPrefixFor(rate.currency)
  const amountDisplay = parsedAmount
    ? formatExactRationalCurrency(parsedAmount, rate.currency, {
        minDecimals: options.minDecimals ?? 2,
        maxDecimals: options.maxDecimals ?? 4,
        locale: options.locale,
        zeroAsFree: false
      })
    : `${prefix}${rate.amount}`
  const perText = rate.per === '1' ? rate.unit : `${rate.per} ${rate.unit}`
  return `${amountDisplay} / ${perText}`
}

export function computeExactQuantityCost(
  quantity: bigint,
  rate?: RateLike | null
): ExactRational | null {
  if (quantity <= 0n) {
    return { num: 0n, den: 1n }
  }
  if (!rate) return null
  const amount = parseNonNegativeDecimal(rate.amount)
  const per = parseNonNegativeDecimal(rate.per)
  if (!amount || !per || per.num === 0n) return null
  return reduceRational(quantity * amount.num * per.den, amount.den * per.num)
}

/**
 * Exact decimal scenario cost calculator matching backend `compare.go` (`scenarioQuantities`):
 * - `uncached_input = max(0, input_tokens - cached_input_tokens)`
 * - `total_tokens = per_request_tokens * max(1, requests)`
 */
export function calculateExactScenarioBreakdown(
  input: ScenarioCostInput
): ScenarioCostBreakdown {
  const rawInput = parseNonNegativeInt(input.inputTokens, 0n)
  const rawCached = parseNonNegativeInt(input.cachedInputTokens, 0n)
  const cachedPerReq = rawCached > rawInput ? rawInput : rawCached
  const uncachedPerReq = rawInput - cachedPerReq
  const outputPerReq = parseNonNegativeInt(input.outputTokens, 0n)
  const rawRequests = parseNonNegativeInt(input.requests, 1n)
  const requests = rawRequests <= 0n ? 1n : rawRequests

  const totalUncached = uncachedPerReq * requests
  const totalCached = cachedPerReq * requests
  const totalOutput = outputPerReq * requests

  const currency =
    input.inputRate?.currency ||
    input.outputRate?.currency ||
    input.cachedInputRate?.currency ||
    'USD'

  if (input.priceState === 'free') {
    const zeroText = formatExactRationalCurrency(
      { num: 0n, den: 1n },
      currency,
      { locale: input.locale, zeroAsFree: false, minDecimals: 2, maxDecimals: 4 }
    )
    const freeLabel = formatExactRationalCurrency(
      { num: 0n, den: 1n },
      currency,
      { locale: input.locale, zeroAsFree: true }
    )
    return {
      state: 'free',
      currency,
      uncachedInputTokens: String(uncachedPerReq),
      cachedInputTokens: String(cachedPerReq),
      outputTokens: String(outputPerReq),
      requests: String(requests),
      totalUncachedInputTokens: String(totalUncached),
      totalCachedInputTokens: String(totalCached),
      totalOutputTokens: String(totalOutput),
      uncachedInputCost: zeroText,
      cachedInputCost: zeroText,
      outputCost: zeroText,
      singleRequestTotal: freeLabel,
      totalDisplay: freeLabel,
      usedFallbackCacheRate: false
    }
  }

  const usedFallbackCacheRate =
    totalCached > 0n && !input.cachedInputRate && Boolean(input.inputRate)
  const effectiveCacheRate = input.cachedInputRate ?? input.inputRate

  const uncachedCost = computeExactQuantityCost(totalUncached, input.inputRate)
  const cachedCost = computeExactQuantityCost(totalCached, effectiveCacheRate)
  const outputCost = computeExactQuantityCost(totalOutput, input.outputRate)

  const hasAnyTokens = totalUncached > 0n || totalCached > 0n || totalOutput > 0n
  const missingRequiredRate =
    !uncachedCost ||
    !cachedCost ||
    !outputCost ||
    (!hasAnyTokens && !input.inputRate && !input.outputRate)

  if (missingRequiredRate) {
    return {
      state: 'unpriced',
      currency,
      uncachedInputTokens: String(uncachedPerReq),
      cachedInputTokens: String(cachedPerReq),
      outputTokens: String(outputPerReq),
      requests: String(requests),
      totalUncachedInputTokens: String(totalUncached),
      totalCachedInputTokens: String(totalCached),
      totalOutputTokens: String(totalOutput),
      uncachedInputCost: '—',
      cachedInputCost: '—',
      outputCost: '—',
      singleRequestTotal: '—',
      totalDisplay: '—',
      usedFallbackCacheRate
    }
  }

  const totalRational = addRationals(addRationals(uncachedCost, cachedCost), outputCost)
  const singleRational = reduceRational(totalRational.num, totalRational.den * requests)

  const fmt = (value: ExactRational) =>
    formatExactRationalCurrency(value, currency, {
      locale: input.locale,
      minDecimals: 2,
      maxDecimals: 4,
      zeroAsFree: false,
      groupThousands: true
    })

  return {
    state: 'estimated',
    currency,
    uncachedInputTokens: String(uncachedPerReq),
    cachedInputTokens: String(cachedPerReq),
    outputTokens: String(outputPerReq),
    requests: String(requests),
    totalUncachedInputTokens: String(totalUncached),
    totalCachedInputTokens: String(totalCached),
    totalOutputTokens: String(totalOutput),
    uncachedInputCost: fmt(uncachedCost),
    cachedInputCost: fmt(cachedCost),
    outputCost: fmt(outputCost),
    singleRequestTotal: fmt(singleRational),
    totalDisplay: fmt(totalRational),
    usedFallbackCacheRate
  }
}
