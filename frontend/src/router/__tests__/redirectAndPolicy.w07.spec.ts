import { describe, expect, it } from 'vitest'
import { sanitizeInternalRedirect } from '../redirect'
import { evaluateRoutePolicy, isModelPlazaPath } from '../routePolicy'
import { resolveRouteDocumentTitle, setDynamicRouteTitle } from '../title'

describe('Redirect sanitization and route access policy', () => {
  it('sanitizes untrusted redirect values and rejects protocol-relative, backslash, and control-char bypasses', () => {
    expect(sanitizeInternalRedirect('/dashboard')).toBe('/dashboard')
    expect(sanitizeInternalRedirect('/model-plaza/openai%2Fgpt-4o?tab=1#top')).toBe(
      '/model-plaza/openai%2Fgpt-4o?tab=1#top'
    )

    expect(sanitizeInternalRedirect('//evil.com')).toBe('/dashboard')
    expect(sanitizeInternalRedirect('/\\evil.com')).toBe('/dashboard')
    expect(sanitizeInternalRedirect('/foo\\bar')).toBe('/dashboard')
    expect(sanitizeInternalRedirect('https://evil.com/phish')).toBe('/dashboard')
    expect(sanitizeInternalRedirect('javascript:alert(1)')).toBe('/dashboard')
    expect(sanitizeInternalRedirect('/dashboard\r\nSet-Cookie: a=b')).toBe('/dashboard')
    expect(sanitizeInternalRedirect(['/a', '/b'])).toBe('/dashboard')
    expect(sanitizeInternalRedirect('', '/purchase')).toBe('/purchase')
    expect(sanitizeInternalRedirect(undefined, '/purchase')).toBe('/purchase')
  })

  it('applies model_plaza_enabled and model_plaza_require_auth uniformly across plaza subroutes, /models, and /pricing', () => {
    const plazaPaths = [
      '/model-plaza',
      '/model-plaza/',
      '/model-plaza/compare',
      '/model-plaza/openai/gpt-4o',
      '/models',
      '/pricing'
    ]

    for (const path of plazaPaths) {
      expect(isModelPlazaPath(path)).toBe(true)

      const disabledEval = evaluateRoutePolicy(
        { requiresAuth: false, featureFlag: 'modelPlaza' },
        {
          path,
          fullPath: path,
          isAuthenticated: false,
          isAdmin: false,
          isSimpleMode: false,
          hasPendingAuthSession: false,
          backendModeEnabled: false,
          publicSettingsLoaded: true,
          publicSettings: { model_plaza_enabled: false }
        }
      )
      expect(disabledEval.state).toBe('disabled')
      expect(disabledEval.redirectTo).toBe('/home')

      const requireAuthEval = evaluateRoutePolicy(
        { requiresAuth: false, featureFlag: 'modelPlaza' },
        {
          path,
          fullPath: `${path}?ref=test`,
          isAuthenticated: false,
          isAdmin: false,
          isSimpleMode: false,
          hasPendingAuthSession: false,
          backendModeEnabled: false,
          publicSettingsLoaded: true,
          publicSettings: { model_plaza_enabled: true, model_plaza_require_auth: true }
        }
      )
      expect(requireAuthEval.state).toBe('disabled')
      expect(requireAuthEval.reason).toBe('unauthenticated')
      expect(requireAuthEval.redirectTo).toEqual({
        path: '/login',
        query: { redirect: `${path}?ref=test` }
      })
    }
  })

  it('blocks /purchase and /orders when payment_enabled is false', () => {
    for (const path of ['/purchase', '/orders']) {
      const userEval = evaluateRoutePolicy(
        { requiresAuth: true, requiresAdmin: false, requiresPayment: true },
        {
          path,
          fullPath: path,
          isAuthenticated: true,
          isAdmin: false,
          isSimpleMode: false,
          hasPendingAuthSession: false,
          backendModeEnabled: false,
          publicSettingsLoaded: true,
          publicSettings: { payment_enabled: false }
        }
      )
      expect(userEval.state).toBe('disabled')
      expect(userEval.redirectTo).toBe('/dashboard')
    }
  })

  it('preserves dynamic model detail title across brand siteName refreshes (P2-5)', () => {
    setDynamicRouteTitle('PublicModelDetail', 'openai/gpt-4o', 'GPT-4o')
    const title = resolveRouteDocumentTitle(
      {
        name: 'PublicModelDetail',
        params: { id: 'openai%2Fgpt-4o' },
        meta: { title: 'Model Detail', titleZh: '模型详情' }
      },
      'Acme Gateway'
    )
    expect(title).toBe('GPT-4o - Acme Gateway')

    setDynamicRouteTitle('PublicModelDetail', '', null)
    const fallbackTitle = resolveRouteDocumentTitle(
      {
        name: 'PublicModelDetail',
        params: { id: 'openai%2Fgpt-4o' },
        meta: { title: 'Model Detail', titleZh: '模型详情' }
      },
      'Acme Gateway'
    )
    expect(fallbackTitle).toContain('Acme Gateway')
    expect(fallbackTitle).not.toContain('GPT-4o')
  })
})
