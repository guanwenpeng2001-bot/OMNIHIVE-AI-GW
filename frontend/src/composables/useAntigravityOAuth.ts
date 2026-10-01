import { oauthProxyPayload, type OAuthProxySelection } from '@/utils/accountProxy'
import { extractApiErrorMessage } from '@/utils/apiError'
import { useOAuthRequestState } from './useOAuthRequestState'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { AntigravityTokenInfo } from '@/api/admin/antigravity'

export function useAntigravityOAuth() {
  const { t } = useI18n()

  const { authUrl, sessionId, state, loading, error, resetState, beginRequest } = useOAuthRequestState()

  const generateAuthUrl = async (proxyId: OAuthProxySelection | undefined): Promise<boolean> => {
    const request = beginRequest()
    authUrl.value = ''
    sessionId.value = ''
    state.value = ''
    error.value = ''

    try {
      const payload: Record<string, unknown> = {}
      Object.assign(payload, oauthProxyPayload(proxyId))

      const response = await request.wait(adminAPI.antigravity.generateAuthUrl(payload as any))
      authUrl.value = response.auth_url
      sessionId.value = response.session_id
      state.value = response.state
      return true
    } catch (err: unknown) {
      if (!request.isCurrent()) return false
      error.value =
        extractApiErrorMessage(err, '') || t('admin.accounts.oauth.antigravity.failedToGenerateUrl')
      request.fail(error.value, true)
      return false
    } finally {
      request.finish()
    }
  }

  const exchangeAuthCode = async (params: {
    code: string
    sessionId: string
    state: string
    proxyId?: OAuthProxySelection
  }): Promise<AntigravityTokenInfo | null> => {
    const code = params.code?.trim()
    if (!code || !params.sessionId || !params.state) {
      error.value = t('admin.accounts.oauth.antigravity.missingExchangeParams')
      return null
    }

    const request = beginRequest()
    error.value = ''

    try {
      const payload: Record<string, unknown> = {
        session_id: params.sessionId,
        state: params.state,
        code
      }
      Object.assign(payload, oauthProxyPayload(params.proxyId))

      const tokenInfo = await request.wait(adminAPI.antigravity.exchangeCode(payload as any))
      return tokenInfo as AntigravityTokenInfo
    } catch (err: unknown) {
      if (!request.isCurrent()) return null
      error.value =
        extractApiErrorMessage(err, '') || t('admin.accounts.oauth.antigravity.failedToExchangeCode')
      request.fail(error.value, true)
      return null
    } finally {
      request.finish()
    }
  }

  const validateRefreshToken = async (
    refreshToken: string,
    proxyId?: OAuthProxySelection
  ): Promise<AntigravityTokenInfo | null> => {
    if (!refreshToken.trim()) {
      error.value = t('admin.accounts.oauth.antigravity.pleaseEnterRefreshToken')
      return null
    }

    const request = beginRequest()
    error.value = ''

    try {
      const tokenInfo = await request.wait(adminAPI.antigravity.refreshAntigravityToken(
        refreshToken.trim(),
        proxyId
      ))
      return tokenInfo as AntigravityTokenInfo
    } catch (err: unknown) {
      if (!request.isCurrent()) return null
      error.value =
        extractApiErrorMessage(err, '') || t('admin.accounts.oauth.antigravity.failedToValidateRT')
      // Don't show global error toast for batch validation to avoid spamming

      request.fail(error.value, false)
      return null
    } finally {
      request.finish()
    }
  }

  const buildCredentials = (
    tokenInfo: AntigravityTokenInfo,
    fallbackRefreshToken?: string
  ): Record<string, unknown> => {
    let expiresAt: string | undefined
    if (typeof tokenInfo.expires_at === 'number' && Number.isFinite(tokenInfo.expires_at)) {
      expiresAt = Math.floor(tokenInfo.expires_at).toString()
    } else if (typeof tokenInfo.expires_at === 'string' && tokenInfo.expires_at.trim()) {
      expiresAt = tokenInfo.expires_at.trim()
    }
    const refreshToken = tokenInfo.refresh_token?.trim()
      ? tokenInfo.refresh_token
      : fallbackRefreshToken

    return {
      access_token: tokenInfo.access_token,
      refresh_token: refreshToken,
      token_type: tokenInfo.token_type,
      expires_at: expiresAt,
      project_id: tokenInfo.project_id,
      email: tokenInfo.email,
      ...(tokenInfo.plan_type ? { plan_type: tokenInfo.plan_type } : {})
    }
  }

  return {
    authUrl,
    sessionId,
    state,
    loading,
    error,
    resetState,
    generateAuthUrl,
    exchangeAuthCode,
    validateRefreshToken,
    buildCredentials
  }
}
