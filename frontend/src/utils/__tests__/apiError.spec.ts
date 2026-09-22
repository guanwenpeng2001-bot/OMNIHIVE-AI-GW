import { describe, expect, it } from 'vitest'
import { normalizeApiError, extractApiErrorCode, extractApiErrorMessage, extractApiErrorMetadata, extractApiErrorStatus, extractI18nErrorMessage, decodeErrorEnvelopeBody, ERROR_ENVELOPE_DECODE_LIMIT_BYTES } from '../apiError'

describe('management error normalization', () => {
  it.each([
    { code: 409, reason: 'mixed_channel_warning', metadata: { group_id: 27 } },
    { code: 'mixed_channel_warning', metadata: { group_id: 27 } },
    { error: 'mixed_channel_warning', details: { group_id: 27 } },
  ])('reads current and transitional conflict envelopes', (data) => {
    const error = normalizeApiError({ message: 'Request failed with status code 409', response: { status: 409, data: { ...data, message: 'Mixed group' } } })
    expect(error).toEqual({ status: 409, code: 409, reason: 'mixed_channel_warning', message: 'Mixed group', metadata: { group_id: 27 } })
    expect(normalizeApiError(error)).toBe(error)
    expect(extractApiErrorCode(error)).toBe('mixed_channel_warning')
    expect(extractApiErrorStatus(error)).toBe(409)
    expect(extractApiErrorMetadata(error)).toEqual({ group_id: 27 })
  })

  it.each(['GROUP_NOT_ALLOWED', 'KEY_EXISTS', 'IP_NOT_ALLOWED', 'OPS_DISABLED', 'ADMIN_COMPLIANCE_ACK_REQUIRED', 'INSUFFICIENT_BALANCE', 'FORBIDDEN'])('preserves machine reason %s', (reason) => {
    expect(extractApiErrorCode({ response: { status: 403, data: { code: 403, reason } } })).toBe(reason)
    expect(extractApiErrorCode({ status: 403, code: reason })).toBe(reason)
  })

  it('prefers server reason and message over Axios transport fields', () => {
    expect(normalizeApiError({ code: 'ERR_BAD_REQUEST', message: 'Axios failure', response: { status: 400, data: { code: 'KEY_EXISTS', detail: 'Already exists' } } }))
      .toMatchObject({ code: 400, reason: 'KEY_EXISTS', message: 'Already exists' })
  })

  it.each([undefined, null, {}, [], { message: {} }, { response: { data: '<html>error</html>' } }])('never stringifies unknown data', (error) => {
    expect(extractApiErrorMessage(error, 'Fallback')).toBe('Fallback')
  })

  it('supports local and legacy string failures without object coercion', () => {
    expect(extractApiErrorMessage(new Error('Invalid JSON'))).toBe('Invalid JSON')
    expect(extractApiErrorMessage('Request failed')).toBe('Request failed')
    expect(extractApiErrorMessage({ error: 'Request failed' })).toBe('Request failed')
    expect(extractApiErrorCode({ code: 403 })).toBeUndefined()
  })

  it('translates by machine reason and metadata', () => {
    const error = { response: { status: 403, data: { code: 403, reason: 'GROUP_NOT_ALLOWED', message: 'Denied', metadata: { group: 'Private' } } } }
    expect(extractI18nErrorMessage(error, (key, params) => key === 'keys.GROUP_NOT_ALLOWED' ? `Denied: ${params?.group}` : key, 'keys', 'Fallback')).toBe('Denied: Private')
  })
})

// 导出走 responseType: 'blob'，409 信封在正文里还是 JSON；不解码就只剩一句通用失败。
describe('binary error bodies', () => {
  const conflict = {
    code: 409,
    reason: 'USAGE_HISTORY_INCOMPLETE',
    message: 'Usage history is incomplete: raw history or a verified archive is unavailable',
    metadata: { raw_retained_from: '2026-03-05T00:00:00Z' },
  }

  it('keeps the 409 envelope that arrives as a plain object (control)', () => {
    expect(normalizeApiError({ message: 'Request failed with status code 409', response: { status: 409, data: conflict } }))
      .toMatchObject({ status: 409, reason: 'USAGE_HISTORY_INCOMPLETE', metadata: { raw_retained_from: '2026-03-05T00:00:00Z' } })
  })

  it('keeps the same envelope when the body is a blob', async () => {
    const blob = new Blob([JSON.stringify(conflict)], { type: 'application/json' })
    expect(await decodeErrorEnvelopeBody(blob)).toEqual(conflict)

    // 对照：不先解码时 reason 与 metadata 就是丢的这一格。
    const undecoded = normalizeApiError({ response: { status: 409, data: new Blob([JSON.stringify(conflict)]) } })
    expect(undecoded.reason).toBeUndefined()
    expect(undecoded.metadata).toEqual({})
    expect(normalizeApiError({ response: { status: 409, data: await decodeErrorEnvelopeBody(blob) } }))
      .toMatchObject({ status: 409, reason: 'USAGE_HISTORY_INCOMPLETE', metadata: { raw_retained_from: '2026-03-05T00:00:00Z' } })
  })

  it('reads an ArrayBuffer body and leaves everything else alone', async () => {
    expect(await decodeErrorEnvelopeBody(new TextEncoder().encode(JSON.stringify(conflict)).buffer)).toEqual(conflict)
    expect(await decodeErrorEnvelopeBody('<html>error</html>')).toBeUndefined()
    expect(await decodeErrorEnvelopeBody(null)).toBeUndefined()
    expect(await decodeErrorEnvelopeBody({ ok: true })).toBeUndefined()
  })

  it('never reads a body over the envelope limit and never throws', async () => {
    expect(await decodeErrorEnvelopeBody(new Blob(['x'.repeat(ERROR_ENVELOPE_DECODE_LIMIT_BYTES + 1)]))).toBeUndefined()
    expect(await decodeErrorEnvelopeBody(new Blob(['']))).toBeUndefined()
    expect(await decodeErrorEnvelopeBody(new Blob(['<html>error</html>']))).toBeUndefined()
    expect(await decodeErrorEnvelopeBody({ size: 12 })).toBeUndefined()
  })
})
