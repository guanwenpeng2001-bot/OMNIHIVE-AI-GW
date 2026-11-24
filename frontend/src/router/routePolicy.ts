import type { PublicSettings } from '@/types'

export type ShellTier = 'public' | 'user' | 'admin' | 'bare'

export type PolicyState = 'enabled' | 'disabled' | 'unknown'

export type PolicyReason =
  | 'ok'
  | 'unauthenticated'
  | 'authenticated_guest_only'
  | 'insufficient_role'
  | 'compliance_required'
  | 'backend_mode_restricted'
  | 'simple_mode_restricted'
  | 'feature_disabled'
  | 'feature_opt_in_pending'
  | 'settings_unloaded'

export type RouteFeatureFlag =
  | 'payment'
  | 'subscription'
  | 'riskControl'
  | 'modelPlaza'
  | 'channelMonitor'
  | 'availableChannels'
  | 'affiliate'
  | 'pluginManagement'
  | 'opsMonitoring'
  | 'adminPayment'

export interface RoutePolicySpec {
  requiresAuth?: boolean
  requiresAdmin?: boolean
  guestOnly?: boolean
  hideInSimpleMode?: boolean
  requiresPayment?: boolean
  requiresRiskControl?: boolean
  requiresSubscription?: boolean
  featureFlag?: RouteFeatureFlag
}

export interface RoutePolicyContext {
  path: string
  fullPath?: string
  isAuthenticated: boolean
  isAdmin: boolean
  isSimpleMode: boolean
  hasPendingAuthSession: boolean
  backendModeEnabled: boolean
  publicSettingsLoaded: boolean
  publicSettings: Partial<PublicSettings> | null
  adminOpsMonitoringEnabled?: boolean
  adminPaymentEnabled?: boolean
  complianceRequired?: boolean
}

export interface RoutePolicyEvaluation {
  state: PolicyState
  reason: PolicyReason
  flag?: RouteFeatureFlag
  redirectTo?: string | { path: string; query?: Record<string, string> }
}

const SIMPLE_MODE_RESTRICTED_PREFIXES = [
  '/admin/subscriptions',
  '/admin/redeem',
  '/subscriptions',
  '/redeem'
]

const BACKEND_MODE_ALLOWED_PREFIXES = [
  '/login',
  '/key-usage',
  '/setup',
  '/payment/result',
  '/payment/airwallex',
  '/legal'
]

const BACKEND_MODE_CALLBACK_PATHS = new Set([
  '/auth/callback',
  '/auth/oauth/callback',
  '/auth/linuxdo/callback',
  '/auth/dingtalk/callback',
  '/auth/dingtalk/email-completion',
  '/auth/oidc/callback',
  '/auth/wechat/callback',
  '/auth/wechat/payment/callback'
])

const BACKEND_MODE_PENDING_AUTH_PATHS = new Set(['/register', '/email-verify'])

export function normalizeRoutePath(path: string): string {
  const trimmed = (path || '').trim()
  if (!trimmed || trimmed === '/') return '/'
  return trimmed.replace(/\/+$/, '') || '/'
}

export function isModelPlazaPath(path: string): boolean {
  const normalized = normalizeRoutePath(path)
  return (
    normalized === '/model-plaza' ||
    normalized.startsWith('/model-plaza/') ||
    normalized === '/models' ||
    normalized === '/pricing'
  )
}

export function isBackendModePublicRouteAllowed(
  path: string,
  hasPendingAuthSession: boolean,
  allowDevGallery = false
): boolean {
  const normalized = normalizeRoutePath(path)
  if (allowDevGallery && normalized.startsWith('/__design')) {
    return true
  }
  if (
    BACKEND_MODE_ALLOWED_PREFIXES.some(
      (allowedPath) => normalized === allowedPath || normalized.startsWith(`${allowedPath}/`)
    )
  ) {
    return true
  }
  if (BACKEND_MODE_CALLBACK_PATHS.has(normalized)) {
    return true
  }
  if (hasPendingAuthSession && BACKEND_MODE_PENDING_AUTH_PATHS.has(normalized)) {
    return true
  }
  return false
}

export function evaluateFeatureFlagPolicy(
  flag: RouteFeatureFlag,
  context: Pick<
    RoutePolicyContext,
    | 'publicSettingsLoaded'
    | 'publicSettings'
    | 'adminOpsMonitoringEnabled'
    | 'adminPaymentEnabled'
    | 'isAdmin'
    | 'isAuthenticated'
  > & Partial<Pick<RoutePolicyContext, 'path'>>
): RoutePolicyEvaluation {
  const settings = context.publicSettings
  const defaultDashboard = context.isAdmin ? '/admin/dashboard' : '/dashboard'

  switch (flag) {
    case 'opsMonitoring': {
      if (context.adminOpsMonitoringEnabled === false) {
        return {
          state: 'disabled',
          reason: 'feature_disabled',
          flag,
          redirectTo: '/admin/settings'
        }
      }
      return { state: 'enabled', reason: 'ok', flag }
    }
    case 'adminPayment': {
      if (context.adminPaymentEnabled === false) {
        return {
          state: 'disabled',
          reason: 'feature_disabled',
          flag,
          redirectTo: '/admin/settings'
        }
      }
      if (!context.publicSettingsLoaded && settings?.payment_enabled === undefined) {
        return { state: 'unknown', reason: 'settings_unloaded', flag }
      }
      if (settings?.payment_enabled === false) {
        return {
          state: 'disabled',
          reason: 'feature_disabled',
          flag,
          redirectTo: '/admin/settings'
        }
      }
      return { state: 'enabled', reason: 'ok', flag }
    }
    case 'payment': {
      if (!context.publicSettingsLoaded && settings?.payment_enabled === undefined) {
        return { state: 'unknown', reason: 'settings_unloaded', flag }
      }
      if (settings?.payment_enabled === false) {
        return {
          state: 'disabled',
          reason: 'feature_disabled',
          flag,
          redirectTo: defaultDashboard
        }
      }
      return { state: 'enabled', reason: 'ok', flag }
    }
    case 'subscription': {
      if (!context.publicSettingsLoaded && settings?.subscription_enabled === undefined) {
        return { state: 'unknown', reason: 'settings_unloaded', flag }
      }
      if (settings?.subscription_enabled === false) {
        return {
          state: 'disabled',
          reason: 'feature_disabled',
          flag,
          redirectTo: defaultDashboard
        }
      }
      return { state: 'enabled', reason: 'ok', flag }
    }
    case 'riskControl': {
      if (!context.publicSettingsLoaded && settings?.risk_control_enabled === undefined) {
        return { state: 'unknown', reason: 'settings_unloaded', flag }
      }
      if (settings?.risk_control_enabled === false) {
        return {
          state: 'disabled',
          reason: 'feature_disabled',
          flag,
          redirectTo: context.isAdmin ? '/admin/settings' : defaultDashboard
        }
      }
      return { state: 'enabled', reason: 'ok', flag }
    }
    case 'modelPlaza': {
      if (!context.publicSettingsLoaded && settings?.model_plaza_enabled === undefined) {
        return { state: 'unknown', reason: 'settings_unloaded', flag }
      }
      if (settings?.model_plaza_enabled === false) {
        return {
          state: 'disabled',
          reason: 'feature_disabled',
          flag,
          redirectTo: context.isAuthenticated ? defaultDashboard : '/home'
        }
      }
      return { state: 'enabled', reason: 'ok', flag }
    }
    case 'channelMonitor': {
      if (!context.publicSettingsLoaded && settings?.channel_monitor_enabled === undefined) {
        return { state: 'unknown', reason: 'settings_unloaded', flag }
      }
      if (settings?.channel_monitor_enabled === false) {
        return {
          state: 'disabled',
          reason: 'feature_disabled',
          flag,
          redirectTo: defaultDashboard
        }
      }
      return { state: 'enabled', reason: 'ok', flag }
    }
    case 'availableChannels': {
      if (settings?.available_channels_enabled === true) {
        return { state: 'enabled', reason: 'ok', flag }
      }
      if (!context.publicSettingsLoaded && settings?.available_channels_enabled === undefined) {
        return { state: 'unknown', reason: 'feature_opt_in_pending', flag }
      }
      return {
        state: 'disabled',
        reason: 'feature_disabled',
        flag,
        redirectTo: defaultDashboard
      }
    }
    case 'affiliate': {
      if (settings?.affiliate_enabled === true) {
        return { state: 'enabled', reason: 'ok', flag }
      }
      if (!context.publicSettingsLoaded && settings?.affiliate_enabled === undefined) {
        return { state: 'unknown', reason: 'feature_opt_in_pending', flag }
      }
      return {
        state: 'disabled',
        reason: 'feature_disabled',
        flag,
        redirectTo: context.path?.startsWith('/admin/') ? '/admin/settings' : defaultDashboard
      }
    }
    case 'pluginManagement': {
      if (settings?.plugin_management_enabled === true) {
        return { state: 'enabled', reason: 'ok', flag }
      }
      if (!context.publicSettingsLoaded && settings?.plugin_management_enabled === undefined) {
        return { state: 'unknown', reason: 'feature_opt_in_pending', flag }
      }
      return {
        state: 'disabled',
        reason: 'feature_disabled',
        flag,
        redirectTo: '/admin/dashboard'
      }
    }
  }
}

export function evaluateRoutePolicy(
  spec: RoutePolicySpec,
  context: RoutePolicyContext,
  options: { allowDevGallery?: boolean } = {}
): RoutePolicyEvaluation {
  const requiresAuth = spec.requiresAuth !== false
  const requiresAdmin = spec.requiresAdmin === true
  const fullPath = context.fullPath || context.path

  if (!requiresAuth) {
    if (
      context.isAuthenticated &&
      (spec.guestOnly || context.path === '/login' || context.path === '/register')
    ) {
      if (context.backendModeEnabled && !context.isAdmin) {
        return { state: 'enabled', reason: 'ok' }
      }
      return {
        state: 'disabled',
        reason: 'authenticated_guest_only',
        redirectTo: context.isAdmin ? '/admin/dashboard' : '/dashboard'
      }
    }

    if (isModelPlazaPath(context.path) || spec.featureFlag === 'modelPlaza') {
      const plazaEval = evaluateFeatureFlagPolicy('modelPlaza', context)
      if (plazaEval.state === 'disabled') {
        return plazaEval
      }
      if (context.publicSettings?.model_plaza_require_auth === true && !context.isAuthenticated) {
        return {
          state: 'disabled',
          reason: 'unauthenticated',
          redirectTo: { path: '/login', query: { redirect: fullPath } }
        }
      }
      if (context.backendModeEnabled && context.isAuthenticated && !context.isAdmin) {
        return {
          state: 'disabled',
          reason: 'backend_mode_restricted',
          redirectTo: '/login'
        }
      }
    }

    if (context.backendModeEnabled && !context.isAuthenticated) {
      const allowed = isBackendModePublicRouteAllowed(
        context.path,
        context.hasPendingAuthSession,
        options.allowDevGallery
      )
      if (!allowed) {
        return {
          state: 'disabled',
          reason: 'backend_mode_restricted',
          redirectTo: '/login'
        }
      }
    }

    return { state: 'enabled', reason: 'ok' }
  }

  if (!context.isAuthenticated) {
    return {
      state: 'disabled',
      reason: 'unauthenticated',
      redirectTo: { path: '/login', query: { redirect: fullPath } }
    }
  }

  if (requiresAdmin && !context.isAdmin) {
    return {
      state: 'disabled',
      reason: 'insufficient_role',
      redirectTo: '/dashboard'
    }
  }

  if (
    requiresAdmin &&
    context.complianceRequired &&
    context.path !== '/admin/compliance'
  ) {
    return {
      state: 'disabled',
      reason: 'compliance_required',
      redirectTo: { path: '/admin/compliance', query: { redirect: fullPath } }
    }
  }

  if (spec.requiresPayment) {
    const evalPayment = evaluateFeatureFlagPolicy(spec.requiresAdmin ? 'adminPayment' : 'payment', context)
    if (evalPayment.state !== 'enabled') return evalPayment
  }

  if (spec.requiresRiskControl) {
    const evalRisk = evaluateFeatureFlagPolicy('riskControl', context)
    if (evalRisk.state !== 'enabled') return evalRisk
  }

  if (spec.requiresSubscription) {
    const evalSub = evaluateFeatureFlagPolicy('subscription', context)
    if (evalSub.state !== 'enabled') return evalSub
  }

  if (spec.featureFlag) {
    const evalFlag = evaluateFeatureFlagPolicy(spec.featureFlag, context)
    if (evalFlag.state !== 'enabled') return evalFlag
  }

  if (context.isSimpleMode) {
    if (
      spec.hideInSimpleMode ||
      SIMPLE_MODE_RESTRICTED_PREFIXES.some((prefix) => context.path.startsWith(prefix))
    ) {
      return {
        state: 'disabled',
        reason: 'simple_mode_restricted',
        redirectTo: context.isAdmin ? '/admin/dashboard' : '/dashboard'
      }
    }
  }

  if (context.backendModeEnabled && !context.isAdmin) {
    const allowed = isBackendModePublicRouteAllowed(
      context.path,
      context.hasPendingAuthSession,
      options.allowDevGallery
    )
    if (!allowed) {
      return {
        state: 'disabled',
        reason: 'backend_mode_restricted',
        redirectTo: '/login'
      }
    }
  }

  return { state: 'enabled', reason: 'ok' }
}

/**
 * Router guard policy: allow navigation on `enabled` OR `unknown`
 * (transient settings fetch failure falls through to authoritative backend).
 */
export function shouldAllowNavigation(evaluation: RoutePolicyEvaluation): boolean {
  return evaluation.state !== 'disabled'
}

/**
 * Sidebar / CommandPalette visibility policy:
 * - `disabled` is always hidden
 * - `unknown` with `feature_opt_in_pending` hides opt-in features until confirmed
 * - `unknown` with `settings_unloaded` keeps opt-out features visible
 */
export function shouldDisplayNavigationEntry(evaluation: RoutePolicyEvaluation): boolean {
  if (evaluation.state === 'disabled') return false
  if (evaluation.state === 'unknown' && evaluation.reason === 'feature_opt_in_pending') {
    return false
  }
  return true
}
