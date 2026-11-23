import { afterEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_PASSWORD_CHARSET, generateSecurePassword } from '../securePassword'

describe('generateSecurePassword CSPRNG & rejection sampling', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('generates passwords of requested length using crypto.getRandomValues without Math.random', () => {
    const mathRandomSpy = vi.spyOn(Math, 'random')
    const getRandomValuesSpy = vi.spyOn(globalThis.crypto, 'getRandomValues')

    const pwd16 = generateSecurePassword()
    const pwd32 = generateSecurePassword(32)

    expect(pwd16).toHaveLength(16)
    expect(pwd32).toHaveLength(32)
    for (const ch of pwd16 + pwd32) {
      expect(DEFAULT_PASSWORD_CHARSET.includes(ch)).toBe(true)
    }
    expect(getRandomValuesSpy).toHaveBeenCalled()
    expect(mathRandomSpy).not.toHaveBeenCalled()
  })

  it('rejects biased tail uint32 values via rejection sampling', () => {
    // Charset of size 3: 2^32 = 4_294_967_296 = 1_431_655_765 * 3 + 1
    // maxUnbiased = 4_294_967_295 -> 0xffffffff must be rejected!
    let callCount = 0
    vi.stubGlobal('crypto', {
      getRandomValues: (arr: Uint32Array) => {
        callCount += 1
        if (callCount === 1) {
          arr.fill(0xffffffff)
        } else {
          arr[0] = 0
          arr[1] = 1
          arr[2] = 2
        }
        return arr
      }
    })

    const result = generateSecurePassword(3, 'ABC')
    expect(callCount).toBe(2)
    expect(result).toBe('ABC')
  })

  it('throws an explicit error instead of degrading to Math.random when Web Crypto is unavailable', () => {
    vi.stubGlobal('crypto', undefined)
    expect(() => generateSecurePassword(16)).toThrow(
      /Cryptographically secure random number generator is unavailable/
    )
  })

  it('validates positive integer length and minimum charset length', () => {
    expect(() => generateSecurePassword(0)).toThrow(RangeError)
    expect(() => generateSecurePassword(-4)).toThrow(RangeError)
    expect(() => generateSecurePassword(1.5)).toThrow(RangeError)
    expect(() => generateSecurePassword(8, 'A')).toThrow(RangeError)
  })
})
