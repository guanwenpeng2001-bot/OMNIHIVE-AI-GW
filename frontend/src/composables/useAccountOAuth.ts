import { oauthProxyPayload, type OAuthProxySelection } from '@/utils/accountProxy'
import { extractApiErrorMessage } from '@/utils/apiError'
import { useOAuthRequestState } from './useOAuthRequestState'
import { adminAPI } from '@/api/admin'

export type AddMethod = 'oauth' | 'setup-token'
export type AuthInputMethod =
  | 'manual'
  | 'cookie'
  | 'refresh_token'
  | 'mobile_refresh_token'
  | 'session_token'
  | 'access_token'
  | 'codex_session'
  | 'agent_identity'
  | 'codex_pat'
  | 'sso_cookie'
  | 'email_password'

export interface OAuthState {
  authUrl: string
  authCode: string
  sessionId: string
  sessionKey: string
  loading: boolean
  error: string
}

export interface TokenInfo {
  org_uuid?: string
  account_uuid?: string
  email_address?: string
  [key: string]: unknown
}

export function useAccountOAuth() {

  // State
  const { authUrl, sessionId, authCode, sessionKey, loading, error, resetState, beginRequest } = useOAuthRequestState()

  // Generate auth URL
  const generateAuthUrl = async (
    addMethod: AddMethod,
    proxyId?: OAuthProxySelection
  ): Promise<boolean> => {
    const request = beginRequest()
    authUrl.value = ''
    sessionId.value = ''
    error.value = ''

    try {
      const proxyConfig = oauthProxyPayload(proxyId)
      const endpoint =
        addMethod === 'oauth'
          ? '/admin/accounts/generate-auth-url'
          : '/admin/accounts/generate-setup-token-url'

      const response = await request.wait(adminAPI.accounts.generateAuthUrl(endpoint, proxyConfig))
      authUrl.value = response.auth_url
      sessionId.value = response.session_id
      return true
    } catch (err: unknown) {
      if (!request.isCurrent()) return false
      error.value = extractApiErrorMessage(err, '') || 'Failed to generate auth URL'
      request.fail(error.value, true)
      return false
    } finally {
      request.finish()
    }
  }

  // Exchange auth code for tokens
  const exchangeAuthCode = async (
    addMethod: AddMethod,
    proxyId?: OAuthProxySelection
  ): Promise<TokenInfo | null> => {
    if (!authCode.value.trim() || !sessionId.value) {
      error.value = 'Missing auth code or session ID'
      return null
    }

    const request = beginRequest()
    error.value = ''

    try {
      const proxyConfig = oauthProxyPayload(proxyId)
      const endpoint =
        addMethod === 'oauth'
          ? '/admin/accounts/exchange-code'
          : '/admin/accounts/exchange-setup-token-code'

      const tokenInfo = await request.wait(adminAPI.accounts.exchangeCode(endpoint, {
        session_id: sessionId.value,
        code: authCode.value.trim(),
        ...proxyConfig
      }))

      return tokenInfo as TokenInfo
    } catch (err: unknown) {
      if (!request.isCurrent()) return null
      error.value = extractApiErrorMessage(err, '') || 'Failed to exchange auth code'
      request.fail(error.value, true)
      return null
    } finally {
      request.finish()
    }
  }

  // Cookie-based authentication
  const cookieAuth = async (
    addMethod: AddMethod,
    sessionKeyValue: string,
    proxyId?: OAuthProxySelection
  ): Promise<TokenInfo | null> => {
    if (!sessionKeyValue.trim()) {
      error.value = 'Please enter sessionKey'
      return null
    }

    const request = beginRequest()
    error.value = ''

    try {
      const proxyConfig = oauthProxyPayload(proxyId)
      const endpoint =
        addMethod === 'oauth'
          ? '/admin/accounts/cookie-auth'
          : '/admin/accounts/setup-token-cookie-auth'

      const tokenInfo = await request.wait(adminAPI.accounts.exchangeCode(endpoint, {
        session_id: '',
        code: sessionKeyValue.trim(),
        ...proxyConfig
      }))

      return tokenInfo as TokenInfo
    } catch (err: unknown) {
      if (!request.isCurrent()) return null
      error.value = extractApiErrorMessage(err, '') || 'Cookie authorization failed'
      request.fail(error.value, false)
      return null
    } finally {
      request.finish()
    }
  }

  // Parse multiple session keys
  const parseSessionKeys = (input: string): string[] => {
    return input
      .split('\n')
      .map((k) => k.trim())
      .filter((k) => k)
  }

  // Build extra info from token response
  const buildExtraInfo = (tokenInfo: TokenInfo): Record<string, string> | undefined => {
    const extra: Record<string, string> = {}
    if (tokenInfo.org_uuid) {
      extra.org_uuid = tokenInfo.org_uuid
    }
    if (tokenInfo.account_uuid) {
      extra.account_uuid = tokenInfo.account_uuid
    }
    if (tokenInfo.email_address) {
      extra.email_address = tokenInfo.email_address
    }
    return Object.keys(extra).length > 0 ? extra : undefined
  }

  return {
    // State
    authUrl,
    authCode,
    sessionId,
    sessionKey,
    loading,
    error,
    // Methods
    resetState,
    generateAuthUrl,
    exchangeAuthCode,
    cookieAuth,
    parseSessionKeys,
    buildExtraInfo
  }
}
