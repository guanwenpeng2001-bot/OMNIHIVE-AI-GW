const SAMPLE_RATE_PATTERN = /^(0(\.[0-9]{1,6})?|1(\.0{1,6})?)$/
const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i
const ENTRY_ID_PATTERN = /^m_[a-f0-9]{32}$/
const TARGET_ID_PATTERN = /^vt_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const ADAPTER_VERSION_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$/
const POSITIVE_INT_PATTERN = /^[1-9][0-9]*$/
const NON_NEGATIVE_INT_PATTERN = /^(0|[1-9][0-9]*)$/

export function formatIfMatch(version: string | number): string {
  const clean = String(version).trim().replace(/^"+|"+$/g, '')
  return `"${clean || '0'}"`
}

export function isValidUuid(value: string): boolean {
  return UUID_PATTERN.test(value.trim())
}

export function isValidPositiveIntString(value: string): boolean {
  return POSITIVE_INT_PATTERN.test(value.trim())
}

export function isValidNonNegativeIntString(value: string): boolean {
  return NON_NEGATIVE_INT_PATTERN.test(value.trim())
}

export function isValidSampleRate(value: string): boolean {
  return SAMPLE_RATE_PATTERN.test(value.trim())
}

export function isValidEntryId(value: string): boolean {
  return ENTRY_ID_PATTERN.test(value.trim())
}

export function isValidTargetId(value: string): boolean {
  return TARGET_ID_PATTERN.test(value.trim())
}

export function isValidAdapterVersion(value: string): boolean {
  return ADAPTER_VERSION_PATTERN.test(value.trim())
}

export function validateTelemetryEndpoint(raw: string, isZh: boolean): string | null {
  const value = raw.trim()
  if (!value) {
    return isZh ? '请输入采集端 HTTPS 地址。' : 'Collector HTTPS endpoint is required.'
  }
  if (value.length > 2048) {
    return isZh ? '采集端地址长度不能超过 2048 个字符。' : 'Collector endpoint must not exceed 2048 characters.'
  }
  if (value.includes('?') || value.includes('#')) {
    return isZh
      ? '采集端地址不允许包含查询参数（?）或片段标识（#）。'
      : 'Collector endpoint must not include query parameters or URL fragments.'
  }
  let parsed: URL
  try {
    parsed = new URL(value)
  } catch {
    return isZh ? '请输入格式合法的完整 URL。' : 'Enter a valid full URL.'
  }
  if (parsed.protocol !== 'https:') {
    return isZh ? '采集端必须使用 https:// 协议。' : 'Collector endpoint must use https://.'
  }
  if (parsed.username || parsed.password) {
    return isZh ? '采集端地址不允许内嵌账号或密码凭据。' : 'Collector endpoint must not contain embedded URL credentials.'
  }
  const host = parsed.hostname.toLowerCase()
  if (!host || host === 'localhost' || host === '127.0.0.1' || host === '[::1]' || host === '::1') {
    return isZh ? '采集端必须是公网可达的 HTTPS 地址，不支持本地回环地址。' : 'Collector endpoint must be a public HTTPS host (loopback addresses are not allowed).'
  }
  return null
}

export function validateVerificationInterval(raw: string, isZh: boolean): { value: number; error: string | null } {
  const trimmed = raw.trim()
  if (!NON_NEGATIVE_INT_PATTERN.test(trimmed)) {
    return {
      value: 0,
      error: isZh ? '核验周期必须为整数秒（0 表示手动触发，或 300 至 604800 秒）。' : 'Interval must be an integer in seconds (0 for manual, or 300..604800).'
    }
  }
  const num = Number(trimmed)
  if (num !== 0 && (num < 300 || num > 604800)) {
    return {
      value: num,
      error: isZh ? '周期核验间隔须为 0（手动）或介于 300 至 604800 秒之间。' : 'Interval must be 0 (manual) or between 300 and 604800 seconds.'
    }
  }
  return { value: num, error: null }
}

// Pure synchronous SHA-256 implementation matching Go catalog.StableID(provider, model)
const SHA256_K = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2
])

function rotr(value: number, shift: number): number {
  return (value >>> shift) | (value << (32 - shift))
}

function sha256Hex(input: string): string {
  const bytes = new TextEncoder().encode(input)
  const bitLen = bytes.length * 8
  const paddedLen = Math.ceil((bytes.length + 9) / 64) * 64
  const buffer = new Uint8Array(paddedLen)
  buffer.set(bytes)
  buffer[bytes.length] = 0x80
  const view = new DataView(buffer.buffer)
  view.setUint32(paddedLen - 8, Math.floor(bitLen / 0x100000000), false)
  view.setUint32(paddedLen - 4, bitLen >>> 0, false)

  let h0 = 0x6a09e667
  let h1 = 0xbb67ae85
  let h2 = 0x3c6ef372
  let h3 = 0xa54ff53a
  let h4 = 0x510e527f
  let h5 = 0x9b05688c
  let h6 = 0x1f83d9ab
  let h7 = 0x5be0cd19

  const w = new Uint32Array(64)
  for (let offset = 0; offset < paddedLen; offset += 64) {
    for (let index = 0; index < 16; index++) {
      w[index] = view.getUint32(offset + index * 4, false)
    }
    for (let index = 16; index < 64; index++) {
      const s0 = rotr(w[index - 15], 7) ^ rotr(w[index - 15], 18) ^ (w[index - 15] >>> 3)
      const s1 = rotr(w[index - 2], 17) ^ rotr(w[index - 2], 19) ^ (w[index - 2] >>> 10)
      w[index] = (w[index - 16] + s0 + w[index - 7] + s1) >>> 0
    }

    let a = h0
    let b = h1
    let c = h2
    let d = h3
    let e = h4
    let f = h5
    let g = h6
    let h = h7

    for (let index = 0; index < 64; index++) {
      const s1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25)
      const ch = (e & f) ^ (~e & g)
      const temp1 = (h + s1 + ch + SHA256_K[index] + w[index]) >>> 0
      const s0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22)
      const maj = (a & b) ^ (a & c) ^ (b & c)
      const temp2 = (s0 + maj) >>> 0

      h = g
      g = f
      f = e
      e = (d + temp1) >>> 0
      d = c
      c = b
      b = a
      a = (temp1 + temp2) >>> 0
    }

    h0 = (h0 + a) >>> 0
    h1 = (h1 + b) >>> 0
    h2 = (h2 + c) >>> 0
    h3 = (h3 + d) >>> 0
    h4 = (h4 + e) >>> 0
    h5 = (h5 + f) >>> 0
    h6 = (h6 + g) >>> 0
    h7 = (h7 + h) >>> 0
  }

  return [h0, h1, h2, h3, h4, h5, h6, h7]
    .map((word) => word.toString(16).padStart(8, '0'))
    .join('')
}

export function computeCatalogEntryId(provider: string, model: string): string {
  const trimmedProvider = provider.trim()
  const trimmedModel = model.trim()
  if (!trimmedProvider || !trimmedModel) return ''
  return `m_${sha256Hex(JSON.stringify([trimmedProvider, trimmedModel])).slice(0, 32)}`
}

export function formatDateTimeDisplay(value?: string | null): string {
  if (!value) return '—'
  const parsed = Date.parse(value)
  return Number.isFinite(parsed) ? new Date(parsed).toLocaleString() : '—'
}
