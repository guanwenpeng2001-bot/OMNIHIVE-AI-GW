import { describe, expect, it } from 'vitest'
import {
  ALLOWED_CREDENTIAL_TYPES,
  buildMixedChannelDetails,
  buildTempUnschedRules,
  canPerformConnectionAction,
  extractOperationVerification,
  formatPoolModeRetryStatusCodes,
  formatProviderIdLabel,
  formatVerificationCheckReason,
  getAuthModeLabel,
  getPrimaryVerificationDiagnostic,
  getProviderDisplayMeta,
  isAllowedCredentialType,
  isConnectionRedacted,
  isHeaderOverrideSupported,
  isOAuthWorkflowSupported,
  isVerificationFresh,
  mapApiErrorToAsyncContext,
  normalizeConnectionEndpointInput,
  parsePoolModeRetryStatusCodes,
  parseValidationFieldIssues,
  splitTempUnschedKeywords,
  toggleErrorCodeInList,
  validateConnectionEndpoint,
  validateCustomErrorCode,
  validateHeaderOverrideRows
} from '../rules'

describe('connections/rules (UIR01)', () => {
  it('parses and formats pool mode retry status codes deterministically', () => {
    expect(parsePoolModeRetryStatusCodes('503, 429 500 429 99 600 abc')).toEqual([429, 500, 503])
    expect(parsePoolModeRetryStatusCodes('   ')).toEqual([])
    expect(formatPoolModeRetryStatusCodes([503, '429', 99, 500, 429])).toBe('429, 500, 503')
    expect(formatPoolModeRetryStatusCodes(null)).toBe('')
  })

  it('validates custom error codes and flags 429/529 confirmation', () => {
    expect(validateCustomErrorCode(null, [])).toEqual({ ok: false, reason: 'invalid' })
    expect(validateCustomErrorCode(99, [])).toEqual({ ok: false, reason: 'invalid' })
    expect(validateCustomErrorCode(500, [500])).toEqual({ ok: false, reason: 'duplicate' })
    expect(validateCustomErrorCode(429, [])).toEqual({ ok: true, requiresConfirm: 429 })
    expect(validateCustomErrorCode(529, [])).toEqual({ ok: true, requiresConfirm: 529 })
    expect(validateCustomErrorCode(502, [500])).toEqual({ ok: true })
  })

  it('toggles error codes without mutating input array', () => {
    const source = [500, 502]
    expect(toggleErrorCodeInList(source, 503)).toEqual([500, 502, 503])
    expect(toggleErrorCodeInList(source, 500)).toEqual([502])
    expect(source).toEqual([500, 502])
  })

  it('splits keywords and builds temp unschedulable rules', () => {
    expect(splitTempUnschedKeywords('rate limit; quota exceeded, overloaded')).toEqual([
      'rate limit',
      'quota exceeded',
      'overloaded'
    ])
    expect(
      buildTempUnschedRules([
        { error_code: 429, keywords: 'rate limit, too many', duration_minutes: 15.8, description: ' Rate cap ' },
        { error_code: 99, keywords: 'bad', duration_minutes: 10, description: '' },
        { error_code: 500, keywords: '   ', duration_minutes: 10, description: '' }
      ])
    ).toEqual([
      {
        error_code: 429,
        keywords: ['rate limit', 'too many'],
        duration_minutes: 15,
        description: 'Rate cap'
      }
    ])
  })

  it('maps mixed channel details and provider metadata', () => {
    expect(buildMixedChannelDetails(undefined)).toBeNull()
    expect(
      buildMixedChannelDetails({
        details: { group_name: 'pool-a', current_platform: 'anthropic' }
      })
    ).toEqual({
      groupName: 'pool-a',
      currentPlatform: 'anthropic',
      otherPlatform: 'Unknown'
    })
    expect(getProviderDisplayMeta('openai').label).toBe('OpenAI')
    expect(getProviderDisplayMeta('custom_gw').label).toBe('Custom GW')
    expect(getProviderDisplayMeta('custom_gw', true).protocolSummary).toBe('自定义兼容协议')
    expect(getProviderDisplayMeta('custom_gw', false).protocolSummary).toBe('Custom Compatible Schema')
    expect(getProviderDisplayMeta('siliconflow_cn', true).label).toContain('硅基流动')
    expect(formatProviderIdLabel('SILICONFLOW_CN')).toBe('SiliconFlow (CN)')
    expect(getAuthModeLabel('oauth')).toBe('OAuth 2.0')
    expect(getAuthModeLabel('setup-token', true)).toContain('Setup Token')
    expect(ALLOWED_CREDENTIAL_TYPES).toEqual([
      'api_key',
      'oauth',
      'setup-token',
      'service_account',
      'bedrock'
    ])
    expect(isAllowedCredentialType('setup-token')).toBe(true)
    expect(isAllowedCredentialType('session_token')).toBe(false)
    expect(isHeaderOverrideSupported('cursor', 'upstream')).toBe(false)
    expect(isHeaderOverrideSupported('opencode_go', 'api_key')).toBe(true)
    expect(isHeaderOverrideSupported('opencodego', 'api_key')).toBe(false)
  })

  it('checks OAuth workflow support, verification freshness, redaction, and role actions', () => {
    expect(
      isOAuthWorkflowSupported(
        { provider: 'openai', schema_version: '1', auth_types: ['oauth'], fields: [], supported_actions: [] },
        'oauth'
      )
    ).toBe(true)
    expect(
      isOAuthWorkflowSupported(
        {
          provider: 'openai',
          schema_version: '1',
          auth_types: ['api_key'],
          fields: [],
          supported_actions: [],
          authorization_workflows: { device_code: 'oauth' }
        },
        'device_code'
      )
    ).toBe(true)
    expect(
      isVerificationFresh(
        {
          verification_id: 'ver_1',
          models: [],
          verified_at: '2026-11-19T00:00:00Z',
          expires_at: '2026-11-19T01:00:00Z',
          checks: []
        },
        Date.parse('2026-11-19T00:30:00Z')
      )
    ).toBe(true)
    expect(
      isVerificationFresh(
        {
          verification_id: 'ver_1',
          models: [],
          verified_at: '2026-11-19T00:00:00Z',
          expires_at: '2026-11-19T01:00:00Z',
          checks: []
        },
        Date.parse('2026-11-19T01:30:00Z')
      )
    ).toBe(false)
    expect(isConnectionRedacted({ version: '2', redactions: ['credential'] })).toBe(true)
    expect(canPerformConnectionAction({ allowed_actions: ['update', 'verify'] }, 'verify')).toBe(true)
    expect(canPerformConnectionAction({ allowed_actions: ['update'] }, 'activate')).toBe(false)
  })

  it('validates endpoint URLs and maps API errors to async states', () => {
    expect(validateConnectionEndpoint('', true)).toBe('Endpoint URL is required by provider schema.')
    expect(validateConnectionEndpoint('ftp://example.com', true)).toBe('Endpoint must use https:// or http://')
    expect(validateConnectionEndpoint('https://api.openai.com', true)).toBeNull()

    const err403 = {
      response: { status: 403, data: { error: { code: 'FORBIDDEN', message: 'Denied' } } }
    }
    expect(mapApiErrorToAsyncContext(err403).state).toBe('permission_denied')

    const err409 = {
      response: { status: 409, data: { error: { code: 'VERSION_CONFLICT', message: 'Stale version' } } }
    }
    expect(mapApiErrorToAsyncContext(err409).state).toBe('version_conflict')

    const err504 = {
      response: { status: 504, data: { error: { code: 'TIMEOUT', message: 'Timed out' } } }
    }
    expect(mapApiErrorToAsyncContext(err504).state).toBe('unknown_outcome')

    const err422Endpoint = {
      response: {
        status: 422,
        data: {
          code: 422,
          reason: 'INVALID_INPUT',
          message: 'Invalid field value.',
          metadata: {
            request_id: 'req_422',
            fields: [{ path: '/endpoint', reason: 'INVALID_INPUT' }]
          }
        }
      }
    }
    const mappedZh = mapApiErrorToAsyncContext(err422Endpoint, '请求失败', true)
    expect(mappedZh.state).toBe('partial_failure')
    expect(mappedZh.message).toContain('/endpoint')
    expect(mappedZh.message).toContain('上游服务地址')
    expect(mappedZh.fields?.[0]?.path).toBe('/endpoint')

    const err409Credential = {
      response: {
        status: 409,
        data: {
          code: 409,
          reason: 'CREDENTIAL_UNAVAILABLE',
          message: 'Reload the connection and review its current state.'
        }
      }
    }
    const mappedCredZh = mapApiErrorToAsyncContext(err409Credential, '请求失败', true)
    expect(mappedCredZh.state).toBe('partial_failure')
    expect(mappedCredZh.message).toContain('尚未配置有效凭据')
    expect(mappedCredZh.message).not.toContain('Reload the connection')

    const mappedConflictZh = mapApiErrorToAsyncContext(
      {
        response: {
          status: 409,
          data: {
            code: 409,
            reason: 'VERSION_CONFLICT',
            message: 'Reload the connection and review its current state.'
          }
        }
      },
      '请求失败',
      true
    )
    expect(mappedConflictZh.state).toBe('version_conflict')
    expect(mappedConflictZh.message).toContain('重新加载最新修订')

    const fieldIssues = parseValidationFieldIssues([
      { path: '/secret', reason: 'INVALID_INPUT' },
      { path: '/type', reason: 'INVALID_INPUT' },
      { path: '/operation', reason: 'INVALID_INPUT' },
      { path: '/refresh_token', reason: 'INVALID_INPUT' },
      { path: '/checks', reason: 'INVALID_INPUT' },
      { path: '/network_policy_id', reason: 'INVALID_INPUT' },
      { path: '/schema_version', reason: 'INVALID_INPUT' },
      { path: '/auth_type', reason: 'INVALID_INPUT' }
    ])
    expect(fieldIssues).toHaveLength(8)
    expect(fieldIssues.every((issue) => !issue.labelZh.startsWith('字段 ('))).toBe(true)

    const extracted = extractOperationVerification({
      operation_id: 'op_1',
      state: 'failed',
      status_url: '/api/v2/operations/op_1',
      result: {
        verification_id: 'ver_custom',
        checks: [{ name: 'network', state: 'unknown', reason: 'EVIDENCE_UNAVAILABLE' }],
        models: [],
        verified_at: '2026-11-20T00:00:00Z',
        expires_at: '2026-11-21T00:00:00Z'
      }
    })
    expect(extracted?.verification_id).toBe('ver_custom')
    expect(extracted?.checks[0]?.reason).toBe('EVIDENCE_UNAVAILABLE')
  })

  it('normalizes pasted endpoint suffixes and validates header overrides', () => {
    expect(normalizeConnectionEndpointInput('  http://127.0.0.1:3425/  ')).toEqual({
      normalized: 'http://127.0.0.1:3425',
      strippedSuffix: undefined
    })
    expect(
      normalizeConnectionEndpointInput('https://api.moonshot.cn/v1/chat/completions/')
    ).toEqual({
      normalized: 'https://api.moonshot.cn',
      strippedSuffix: '/v1/chat/completions'
    })
    expect(
      validateHeaderOverrideRows(
        [{ key: 'User-Agent', value: 'curl/8.7.1' }],
        'kimi',
        'api_key',
        true
      )
    ).toBeNull()
    expect(
      validateHeaderOverrideRows(
        [{ key: 'Authorization', value: 'Bearer bad' }],
        'openai',
        'api_key',
        true
      )
    ).toContain('Authorization')
  })

  it('formats specific verification check reasons and prioritizes primary check.detail diagnostics', () => {
    const urlMsgEn = formatVerificationCheckReason('network', 'URL_NOT_ALLOWED', false)
    expect(urlMsgEn).toContain('SECURITY_URL_ALLOWLIST_UPSTREAM_HOSTS')
    expect(urlMsgEn).toContain('.env')

    const urlMsgZh = formatVerificationCheckReason('network', 'URL_NOT_ALLOWED', true)
    expect(urlMsgZh).toContain('SECURITY_URL_ALLOWLIST_UPSTREAM_HOSTS')
    expect(urlMsgZh).toContain('.env')

    expect(formatVerificationCheckReason('network', 'UPSTREAM_RATE_LIMITED', false)).toContain('429')
    expect(formatVerificationCheckReason('authorization', 'AUTH_REJECTED', true)).toContain('401/403')
    expect(formatVerificationCheckReason('network', 'INVALID_ENDPOINT', false)).toContain('http(s)://')
    expect(formatVerificationCheckReason('network', 'NETWORK_UNREACHABLE', false)).toContain('DNS')

    const primary = getPrimaryVerificationDiagnostic(
      {
        verification_id: 'ver_diag',
        models: [],
        verified_at: '2026-11-22T00:00:00Z',
        expires_at: '2026-11-22T02:00:00Z',
        checks: [
          { name: 'authorization', state: 'unknown', reason: 'EVIDENCE_UNAVAILABLE' },
          {
            name: 'network',
            state: 'failed',
            reason: 'URL_NOT_ALLOWED',
            detail: 'host custom.internal.example is not in SECURITY_URL_ALLOWLIST_UPSTREAM_HOSTS'
          }
        ]
      },
      false
    )
    expect(primary?.reason).toBe('URL_NOT_ALLOWED')
    expect(primary?.detail).toContain('custom.internal.example')
  })
})
