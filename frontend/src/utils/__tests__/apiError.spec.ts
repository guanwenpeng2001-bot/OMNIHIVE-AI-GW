import { describe, expect, it } from 'vitest'
import { normalizeApiError, extractApiErrorCode, extractApiErrorMessage, extractApiErrorMetadata, extractApiErrorStatus, extractI18nErrorMessage } from '../apiError'

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
