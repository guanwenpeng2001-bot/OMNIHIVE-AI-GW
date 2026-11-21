export const DEFAULT_PASSWORD_CHARSET = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789!@#$%^&*'

const UINT32_SPAN = 0x100000000

export function generateSecurePassword(
  length = 16,
  charset: string = DEFAULT_PASSWORD_CHARSET
): string {
  if (!Number.isInteger(length) || length < 1) {
    throw new RangeError('Password length must be a positive integer')
  }
  if (!charset || charset.length < 2) {
    throw new RangeError('Password charset must contain at least 2 characters')
  }
  const cryptoApi = globalThis.crypto
  if (!cryptoApi || typeof cryptoApi.getRandomValues !== 'function') {
    throw new Error('Cryptographically secure random number generator is unavailable')
  }

  const charsetLen = charset.length
  const maxUnbiased = Math.floor(UINT32_SPAN / charsetLen) * charsetLen
  const buffer = new Uint32Array(Math.max(length * 2, 32))
  const chars: string[] = []

  while (chars.length < length) {
    cryptoApi.getRandomValues(buffer)
    for (let idx = 0; idx < buffer.length && chars.length < length; idx += 1) {
      const sample = buffer[idx]!
      if (sample < maxUnbiased) {
        chars.push(charset.charAt(sample % charsetLen))
      }
    }
  }

  return chars.join('')
}
