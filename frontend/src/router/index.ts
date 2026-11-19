import { helpSectionIds, helpScrollOffset } from '@/components/intro/helpSections'
import { resolveSiteBillingMode } from '@/utils/siteBillingMode'
import { normalizeApiError } from '@/utils/apiError'
/**
 * Vue Router configuration for OmniHive frontend
 * Defines all application routes with lazy loading and navigation guards
 */

import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { routerChunkReloadStorage } from '@/api/tokenRefresh'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import { useAdminComplianceStore } from '@/stores/adminCompliance'
import { useNavigationLoadingState } from '@/composables/useNavigationLoading'
import { useRoutePrefetch } from '@/composables/useRoutePrefetch'
import { getSetupStatus } from '@/api/setup'
import {
  evaluateRoutePolicy,
  shouldAllowNavigation
} from './routePolicy'
import { resolveCompletedSetupRedirectPath } from './setupRedirect'
import { resolveRouteDocumentTitle } from './title'
import { lazyIdentityScoped } from './identityScope'
import { paymentRouteComponent } from '@/components/payment/paymentRoute'
import { workspacesAPI } from '@/api/workspaces'

/**
 * Route definitions with lazy loading
 */
const routes: RouteRecordRaw[] = [
  {
    path: '/workspaces', name: 'Workspaces',
    component: () => import('@/views/user/WorkspacesView.vue'),
    meta: { requiresAuth: true, requiresAdmin: false, titleKey: 'workspaces.title' },
  },
  {
    path: '/admin/compliance',
    name: 'AdminCompliance',
    // The global mandatory dialog owns this gate; mount no restricted view.
    component: { render: () => null },
    meta: { requiresAuth: true, requiresAdmin: true, title: 'Compliance' }
  },
  // ==================== Setup Routes ====================
  {
    path: '/setup',
    name: 'Setup',
    component: () => import('@/views/setup/SetupWizardView.vue'),
    meta: {
      requiresAuth: false,
      title: 'Setup'
    }
  },

  // ==================== Public Routes ====================
  {
    path: '/home',
    name: 'Home',
    component: () => import('@/views/public/PublicHomeView.vue'),
    meta: {
      requiresAuth: false,
      title: 'Home'
    }
  },
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/auth/LoginView.vue'),
    meta: {
      requiresAuth: false,
      title: 'Login',
      titleKey: 'home.login'
    }
  },
  {
    path: '/register',
    name: 'Register',
    component: () => import('@/views/auth/RegisterView.vue'),
    meta: {
      requiresAuth: false,
      title: 'Register',
      titleKey: 'auth.createAccount'
    }
  },
  {
    path: '/email-verify',
    name: 'EmailVerify',
    component: () => import('@/views/auth/EmailVerifyView.vue'),
    meta: {
      requiresAuth: false,
      title: 'Verify Email'
    }
  },
  {
    path: '/auth/callback',
    name: 'OAuthCallback',
    alias: '/auth/oauth/callback',
    component: () => import('@/views/auth/OAuthCallbackView.vue'),
    meta: {
      requiresAuth: false,
      title: 'OAuth Callback',
      titleKey: 'auth.oauthCallbackPageTitle'
    }
  },
  {
    path: '/auth/linuxdo/callback',
    name: 'LinuxDoOAuthCallback',
    component: () => import('@/views/auth/LinuxDoCallbackView.vue'),
    meta: {
      requiresAuth: false,
      title: 'LinuxDo OAuth Callback',
      titleKey: 'auth.linuxdoCallbackPageTitle'
    }
  },
  {
    path: '/auth/wechat/callback',
    name: 'WeChatOAuthCallback',
    component: () => import('@/views/auth/WechatCallbackView.vue'),
    meta: {
      requiresAuth: false,
      title: 'WeChat OAuth Callback',
      titleKey: 'auth.wechatCallbackPageTitle'
    }
  },
  {
    path: '/auth/wechat/payment/callback',
    name: 'WeChatPaymentOAuthCallback',
    component: () => import('@/views/auth/WechatPaymentCallbackView.vue'),
    meta: {
      requiresAuth: false,
      title: 'WeChat Payment Callback',
      titleKey: 'auth.wechatPaymentCallbackPageTitle'
    }
  },
  {
    path: '/auth/dingtalk/callback',
    name: 'DingTalkOAuthCallback',
    component: () => import('@/views/auth/DingTalkCallbackView.vue'),
    meta: {
      requiresAuth: false,
      title: 'DingTalk OAuth Callback',
      titleKey: 'auth.dingtalkCallbackPageTitle'
    }
  },
  {
    path: '/auth/dingtalk/email-completion',
    name: 'dingtalk-email-completion',
    component: () => import('@/views/auth/DingTalkEmailCompletionView.vue'),
    meta: {
      requiresAuth: false,
      title: 'DingTalk Email Completion'
    }
  },
  {
    path: '/auth/oidc/callback',
    name: 'OIDCOAuthCallback',
    component: () => import('@/views/auth/OidcCallbackView.vue'),
    meta: {
      requiresAuth: false,
      title: 'OIDC OAuth Callback',
      titleKey: 'auth.oidcCallbackPageTitle'
    }
  },
  {
    path: '/oauth/mcp/authorize',
    name: 'McpAuthorize',
    component: () => import('@/views/auth/McpAuthorizeView.vue'),
    meta: {
      requiresAuth: false,
      title: 'MCP 授权同意页'
    }
  },
  {
    path: '/forgot-password',
    name: 'ForgotPassword',
    component: () => import('@/views/auth/ForgotPasswordView.vue'),
    meta: {
      requiresAuth: false,
      title: 'Forgot Password',
      titleKey: 'auth.forgotPasswordTitle'
    }
  },
  {
    path: '/reset-password',
    name: 'ResetPassword',
    component: () => import('@/views/auth/ResetPasswordView.vue'),
    meta: {
      requiresAuth: false,
      title: 'Reset Password'
    }
  },
  {
    path: '/key-usage',
    name: 'KeyUsage',
    component: () => import('@/views/KeyUsageView.vue'),
    meta: {
      requiresAuth: false,
      title: 'Key Usage',
    }
  },
  {
    path: '/legal/:documentId',
    name: 'LegalDocument',
    component: () => import('@/views/public/LegalDocumentView.vue'),
    meta: {
      requiresAuth: false,
      title: 'Legal Document'
    }
  },
  {
    path: '/model-plaza',
    name: 'ModelPlaza',
    alias: ['/models'],
    component: () => import('@/views/public/PublicModelsView.vue'),
    meta: {
      requiresAuth: false,
      title: 'Model Plaza',
      titleKey: 'modelPlaza.title'
    }
  },
  {
    path: '/models/compare',
    name: 'ModelCompare',
    component: () => import('@/views/public/PublicCompareView.vue'),
    meta: {
      requiresAuth: false,
      title: 'Compare Models'
    }
  },
  {
    path: '/models/:id',
    name: 'PublicModelDetail',
    component: () => import('@/views/public/PublicModelDetailView.vue'),
    meta: {
      requiresAuth: false,
      title: 'Model Detail'
    }
  },
  {
    path: '/pricing',
    name: 'PublicPricing',
    component: () => import('@/views/public/PublicPricingView.vue'),
    meta: {
      requiresAuth: false,
      title: 'Pricing'
    }
  },
  {
    path: '/status',
    name: 'PublicStatus',
    component: () => import('@/views/public/PublicStatusView.vue'),
    meta: {
      requiresAuth: false,
      title: 'Service Status'
    }
  },

  // ==================== User Routes ====================
  {
    path: '/',
    redirect: '/home'
  },
  {
    path: '/dashboard',
    name: 'Dashboard',
    component: () => import('@/views/user/DashboardView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Dashboard',
      titleKey: 'dashboard.title',
      descriptionKey: 'dashboard.welcomeMessage'
    }
  },
  {
    path: '/help',
    name: 'HelpGuide',
    alias: ['/docs'],
    component: () => import('@/views/HelpGuide.vue'),
    meta: { requiresAuth: true, requiresAdmin: false, title: 'Guide', titleKey: 'helpGuide.title' }
  },
  {
    path: '/keys',
    name: 'Keys',
    component: () => import('@/views/user/KeysView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'API Keys',
      titleKey: 'keys.title',
      descriptionKey: 'keys.description'
    }
  },
  {
    path: '/batch-image',
    name: 'BatchImageGuide',
    alias: '/docs/batch-image',
    component: () => import('@/views/user/BatchImageGuideView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Batch Image Guide',
      titleKey: 'batchImageGuide.title',
      descriptionKey: 'batchImageGuide.description'
    }
  },
  {
    path: '/requests',
    name: 'ProjectRequests',
    component: () => import('@/features/requests/RequestListView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Requests',
      titleKey: 'wp6.requests.title'
    }
  },
  {
    path: '/requests/:id',
    name: 'ProjectRequest',
    component: () => import('@/features/requests/RequestDetailView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Request Detail',
      titleKey: 'wp6.requests.detail'
    }
  },
  {
    path: '/admin/requests',
    name: 'AdminRequests',
    component: () => import('@/features/requests/RequestListView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Requests',
      titleKey: 'wp6.requests.title'
    }
  },
  {
    path: '/admin/requests/:id',
    name: 'AdminRequest',
    component: () => import('@/features/requests/RequestDetailView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Request Detail',
      titleKey: 'wp6.requests.detail'
    }
  },
  {
    path: '/billing',
    name: 'ProjectBilling',
    component: () => import('@/features/billing/UserCostsView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Costs & Quota',
      titleKey: 'wp6.billing.title'
    }
  },
  {
    path: '/playground',
    name: 'Playground',
    component: () => import('@/views/user/PlaygroundView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Playground',
      titleKey: 'playgroundConsole.title',
      descriptionKey: 'playgroundConsole.description'
    }
  },
  {
    path: '/tasks',
    name: 'TaskCenter',
    component: () => import('@/views/user/TaskCenterView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Task Center',
      titleKey: 'taskCenter.title',
      descriptionKey: 'taskCenter.description'
    }
  },
  {
    path: '/admin/billing',
    name: 'AdminBilling',
    component: () => import('@/features/billing/ReconciliationView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Reconciliation',
      titleKey: 'wp6.reconciliation.title'
    }
  },
  {
    path: '/usage',
    name: 'Usage',
    component: () => import('@/views/user/UsageView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Usage Records',
      titleKey: 'usage.title',
      descriptionKey: 'usage.description'
    }
  },
  {
    path: '/redeem',
    name: 'Redeem',
    component: () => import('@/views/user/RedeemView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Redeem Code',
      titleKey: 'redeem.title',
      descriptionKey: 'redeem.description'
    }
  },
  {
    path: '/affiliate',
    name: 'Affiliate',
    component: () => import('@/views/user/AffiliateView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Affiliate',
      titleKey: 'affiliate.title',
      descriptionKey: 'affiliate.description'
    }
  },
  {
    path: '/available-channels',
    name: 'UserAvailableChannels',
    component: () => import('@/views/user/AvailableChannelsView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Available Channels',
      titleKey: 'availableChannels.title',
      descriptionKey: 'availableChannels.description'
    }
  },
  {
    path: '/profile',
    name: 'Profile',
    component: () => import('@/views/user/ProfileView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Profile',
      titleKey: 'profile.title',
      descriptionKey: 'profile.description'
    }
  },
  {
    path: '/settings/mcp',
    name: 'McpSettings',
    component: () => import('@/views/user/McpSettingsView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'MCP 授权'
    }
  },
  {
    path: '/subscriptions',
    name: 'Subscriptions',
    component: () => import('@/views/user/SubscriptionsView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'My Subscriptions',
      titleKey: 'userSubscriptions.title',
      descriptionKey: 'userSubscriptions.description',
      requiresSubscription: true
    }
  },
  {
    path: '/purchase',
    name: 'PurchaseSubscription',
    component: () => import('@/views/user/PaymentView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Purchase Subscription',
      titleKey: 'nav.buySubscription',
      descriptionKey: 'purchase.description',
      requiresPayment: true
    }
  },
  {
    path: '/orders',
    name: 'OrderList',
    component: () => import('@/views/user/UserOrdersView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'My Orders',
      titleKey: 'nav.myOrders',
      requiresPayment: true
    }
  },
  {
    path: '/payment/qrcode',
    name: 'PaymentQRCode',
    component: () => import('@/views/user/PaymentQRCodeView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Payment',
      titleKey: 'payment.qr.scanToPay',
      requiresPayment: true
    }
  },
  {
    path: '/payment/result',
    name: 'PaymentResult',
    component: () => import('@/views/user/PaymentResultView.vue').then(({ default: component }) => paymentRouteComponent(component, 'result')),
    meta: {
      requiresAuth: false,
      requiresAdmin: false,
      title: 'Payment Result',
      titleKey: 'payment.result.success',
      requiresPayment: false
    }
  },
  {
    path: '/payment/stripe',
    name: 'StripePayment',
    component: () => import('@/views/user/StripePaymentView.vue').then(({ default: component }) => paymentRouteComponent(component, 'stripe')),
    meta: {
      requiresAuth: false,
      requiresAdmin: false,
      title: 'Stripe Payment',
      titleKey: 'payment.stripePay',
      requiresPayment: false
    }
  },
  {
    path: '/payment/airwallex',
    name: 'AirwallexPayment',
    component: () => import('@/views/user/AirwallexPaymentView.vue').then(({ default: component }) => paymentRouteComponent(component, 'airwallex')),
    meta: {
      requiresAuth: false,
      requiresAdmin: false,
      title: 'Airwallex Payment',
      titleKey: 'payment.airwallexPay',
      requiresPayment: false
    }
  },
  {
    path: '/payment/stripe-popup',
    name: 'StripePopup',
    component: () => import('@/views/user/StripePopupView.vue'),
    meta: {
      requiresAuth: false,
      requiresAdmin: false,
      title: 'Payment',
      requiresPayment: false
    }
  },
  {
    path: '/custom/:id',
    name: 'CustomPage',
    component: () => import('@/views/user/CustomPageView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Custom Page',
      titleKey: 'customPage.title',
    }
  },

  // ==================== Admin Routes ====================
  {
    path: '/admin',
    redirect: '/admin/dashboard'
  },
  {
    path: '/admin/dashboard',
    name: 'AdminDashboard',
    component: () => import('@/views/admin/DashboardView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Admin Dashboard',
      titleKey: 'admin.dashboard.title',
      descriptionKey: 'admin.dashboard.description'
    }
  },
  {
    path: '/admin/ops',
    name: 'AdminOps',
    component: () => import('@/views/admin/ops/OpsDashboard.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Ops Monitoring',
      titleKey: 'admin.ops.title',
      descriptionKey: 'admin.ops.description'
    }
  },
  {
    path: '/admin/audit-logs',
    name: 'AdminAuditLogs',
    component: () => import('@/views/admin/AuditLogView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Audit Logs',
      titleKey: 'admin.audit.title',
      descriptionKey: 'admin.audit.description'
    }
  },
  {
    path: '/admin/audit',
    name: 'AdminScopedAudit',
    component: () => import('@/features/operations/ScopedAuditView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Scoped Audit',
      titleKey: 'admin.audit.title',
      descriptionKey: 'admin.audit.description'
    }
  },
  {
    path: '/admin/users',
    name: 'AdminUsers',
    component: () => import('@/views/admin/UsersView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'User Management',
      titleKey: 'admin.users.title',
      descriptionKey: 'admin.users.description'
    }
  },
  {
    path: '/admin/groups',
    name: 'AdminGroups',
    component: () => import('@/views/admin/GroupsView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Group Management',
      titleKey: 'admin.groups.title',
      descriptionKey: 'admin.groups.description'
    }
  },
  {
    path: '/admin/pools',
    name: 'AdminPools',
    component: () => import('@/views/admin/GroupsView.vue'),
    props: { initialSection: 'pools' },
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Resource Pools',
      titleKey: 'admin.groups.title',
      descriptionKey: 'admin.groups.description'
    }
  },
  {
    path: '/admin/routing',
    name: 'AdminRouting',
    component: () => import('@/views/admin/GroupsView.vue'),
    props: { initialSection: 'routing' },
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Routing Policies',
      titleKey: 'routing.title',
      descriptionKey: 'admin.groups.description'
    }
  },
  {
    path: '/admin/channels',
    redirect: '/admin/channels/pricing'
  },
  {
    path: '/admin/channels/pricing',
    name: 'AdminChannels',
    component: () => import('@/views/admin/ChannelsView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Channel Management',
      titleKey: 'admin.channels.title',
      descriptionKey: 'admin.channels.description'
    }
  },
  {
    path: '/admin/channels/monitor',
    name: 'AdminChannelMonitor',
    component: () => import('@/views/admin/ChannelMonitorView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Channel Monitor',
      titleKey: 'admin.channelMonitor.title',
      descriptionKey: 'admin.channelMonitor.description'
    }
  },
  {
    path: '/monitor',
    name: 'ChannelStatus',
    component: () => import('@/views/user/ChannelStatusView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Channel Status',
      titleKey: 'nav.channelStatus'
    }
  },
  {
    path: '/admin/subscriptions',
    name: 'AdminSubscriptions',
    component: () => import('@/views/admin/SubscriptionsView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Subscription Management',
      titleKey: 'admin.subscriptions.title',
      descriptionKey: 'admin.subscriptions.description'
    }
  },
  {
    path: '/admin/accounts',
    name: 'AdminAccounts',
    component: () => import('@/views/admin/AccountsView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Account Management',
      titleKey: 'admin.accounts.title',
      descriptionKey: 'admin.accounts.description'
    }
  },
  {
    path: '/admin/connections/new',
    name: 'AdminConnectionNew',
    component: () => import('@/views/admin/connections/ConnectionWizardView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'New Upstream Connection',
      titleKey: 'admin.accounts.title',
      descriptionKey: 'admin.accounts.description'
    }
  },
  {
    path: '/admin/connections/:id/edit',
    name: 'AdminConnectionEdit',
    component: () => import('@/views/admin/connections/ConnectionWizardView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Edit Upstream Connection',
      titleKey: 'admin.accounts.title',
      descriptionKey: 'admin.accounts.description'
    }
  },
  {
    path: '/admin/plugins',
    name: 'AdminPlugins',
    component: () => import('@/views/admin/PluginsView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Plugin Management',
      titleKey: 'admin.plugins.title',
      descriptionKey: 'admin.plugins.description'
    }
  },
  {
    path: '/admin/announcements',
    name: 'AdminAnnouncements',
    component: () => import('@/views/admin/AnnouncementsView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Announcements',
      titleKey: 'admin.announcements.title',
      descriptionKey: 'admin.announcements.description'
    }
  },
  {
    path: '/admin/proxies',
    name: 'AdminProxies',
    component: () => import('@/views/admin/ProxiesView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Proxy Management',
      titleKey: 'admin.proxies.title',
      descriptionKey: 'admin.proxies.description'
    }
  },
  {
    path: '/admin/proxy-groups',
    name: 'AdminProxyGroups',
    component: () => import('@/views/admin/ProxyGroupsView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'IP Groups',
      titleKey: 'admin.proxyGroups.title',
      descriptionKey: 'admin.proxyGroups.description'
    }
  },
  {
    path: '/admin/redeem',
    name: 'AdminRedeem',
    component: () => import('@/views/admin/RedeemView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Redeem Code Management',
      titleKey: 'admin.redeem.title',
      descriptionKey: 'admin.redeem.description'
    }
  },
  {
    path: '/admin/promo-codes',
    name: 'AdminPromoCodes',
    component: () => import('@/views/admin/PromoCodesView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Promo Code Management',
      titleKey: 'admin.promo.title',
      descriptionKey: 'admin.promo.description'
    }
  },
  {
    path: '/admin/settings',
    name: 'AdminSettings',
    component: () => import('@/views/admin/SettingsView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'System Settings',
      titleKey: 'admin.settings.title',
      descriptionKey: 'admin.settings.description'
    }
  },
  {
    path: '/admin/backup',
    name: 'AdminBackup',
    component: () => import('@/views/admin/BackupView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: '备份与归档'
    }
  },
  {
    path: '/admin/risk-control',
    name: 'AdminRiskControl',
    component: () => import('@/views/admin/RiskControlView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Risk Control',
      titleKey: 'admin.riskControl.title',
      descriptionKey: 'admin.riskControl.description',
      requiresRiskControl: true
    }
  },
  {
    path: '/admin/prompt-audit',
    name: 'AdminPromptAudit',
    component: () => import('@/features/prompt-audit/PromptAuditView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Prompt Audit',
      titleKey: 'admin.promptAudit.title',
      descriptionKey: 'admin.promptAudit.description',
      requiresRiskControl: true
    }
  },
  {
    path: '/admin/usage',
    name: 'AdminUsage',
    component: () => import('@/views/admin/UsageView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Usage Records',
      titleKey: 'admin.usage.title',
      descriptionKey: 'admin.usage.description'
    }
  },
  {
    path: '/admin/affiliates',
    redirect: '/admin/affiliates/invites'
  },
  {
    path: '/admin/affiliates/invites',
    name: 'AdminAffiliateInvites',
    component: () => import('@/views/admin/affiliates/AdminAffiliateInvitesView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Affiliate Invite Records',
      titleKey: 'nav.affiliateInviteRecords',
      descriptionKey: 'admin.affiliates.invitesDescription'
    }
  },
  {
    path: '/admin/affiliates/rebates',
    name: 'AdminAffiliateRebates',
    component: () => import('@/views/admin/affiliates/AdminAffiliateRebatesView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Affiliate Rebate Records',
      titleKey: 'nav.affiliateRebateRecords',
      descriptionKey: 'admin.affiliates.rebatesDescription'
    }
  },
  {
    path: '/admin/affiliates/transfers',
    name: 'AdminAffiliateTransfers',
    component: () => import('@/views/admin/affiliates/AdminAffiliateTransfersView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Affiliate Transfer Records',
      titleKey: 'nav.affiliateTransferRecords',
      descriptionKey: 'admin.affiliates.transfersDescription'
    }
  },


  // ==================== Payment Admin Routes ====================
  {
    path: '/admin/orders/dashboard',
    name: 'AdminPaymentDashboard',
    component: () => import('@/views/admin/orders/AdminPaymentDashboardView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Payment Dashboard',
      titleKey: 'nav.paymentDashboard',
      requiresPayment: true
    }
  },
  {
    path: '/admin/orders',
    name: 'AdminOrders',
    component: () => import('@/views/admin/orders/AdminOrdersView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Order Management',
      titleKey: 'nav.orderManagement',
      requiresPayment: true
    }
  },
  {
    path: '/admin/orders/plans',
    name: 'AdminPaymentPlans',
    component: () => import('@/views/admin/orders/AdminPaymentPlansView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Subscription Plans',
      titleKey: 'nav.paymentPlans',
      requiresPayment: true
    }
  },

  // ==================== 404 Not Found ====================
  ...(import.meta.env.DEV
    ? [
        {
          path: '/__design',
          name: 'DesignSystemGallery',
          component: () => import('@/components/ui/__gallery__/DesignGalleryView.vue'),
          meta: {
            requiresAuth: false,
            title: 'Design System Gallery'
          }
        }
      ]
    : []),
  {
    path: '/:pathMatch(.*)*',
    name: 'NotFound',
    component: () => import('@/views/NotFoundView.vue'),
    meta: {
      requiresAuth: false,
      title: '404 Not Found'
    }
  }
]

const workspaceResourcePaths = new Set(['/admin/accounts', '/admin/groups', '/admin/routing', '/admin/proxies', '/admin/proxy-groups', '/admin/channels/pricing', '/admin/channels/monitor', '/keys', '/monitor'])
for (const resourceRoute of [...routes]) {
  if (!workspaceResourcePaths.has(resourceRoute.path)) continue
  const suffix = resourceRoute.path.replace(/^\/admin/, '')
  routes.push({
    ...resourceRoute,
    path: `/workspaces/:workspace_id${suffix}`,
    name: `Workspace${String(resourceRoute.name)}`,
    meta: { ...resourceRoute.meta, requiresAdmin: false },
    beforeEnter: async to => {
      try {
        const workspace = (await workspacesAPI.list()).find(item => String(item.id) === to.params.workspace_id)
        return workspace && (suffix === '/keys' || workspace.role === 'admin') ? true : '/workspaces'
      } catch { return '/workspaces' }
    },
  } as RouteRecordRaw)
}

// Wrap lazy protected pages so their local state belongs to one login session.
for (const route of routes) {
  if (route.meta?.requiresAuth !== true || typeof route.component !== 'function') continue
  const load = route.component as () => Promise<{ default: import('vue').Component }>
  const admin = route.meta.requiresAdmin === true
  route.component = lazyIdentityScoped(load, admin)
}

/**
 * Create router instance
 */
const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes,
  scrollBehavior(_to, _from, savedPosition) {
    // Scroll to saved position when using browser back/forward
    if (savedPosition) {
      return savedPosition
    }
    if (_to.path === '/help' && helpSectionIds.some(id => `#${id}` === _to.hash)) {
      return { el: _to.hash, top: helpScrollOffset }
    }
    // Scroll to top for new routes
    return { top: 0 }
  }
})

/**
 * Navigation guard: Authentication check
 */
let authInitialized = false

// 初始化导航加载状态和预加载
const navigationLoading = useNavigationLoadingState()
// 延迟初始化预加载，传入 router 实例
let routePrefetch: ReturnType<typeof useRoutePrefetch> | null = null

router.beforeEach(async (to, _from, next) => {
  // 开始导航加载状态
  navigationLoading.startNavigation()

  const authStore = useAuthStore()

  // Restore auth state from localStorage on first navigation (page refresh)
  if (!authInitialized) {
    authStore.checkAuth()
    authInitialized = true
  }

  // Set page title
  const appStore = useAppStore()
  const adminSettingsStore = useAdminSettingsStore()
  const customMenuItems = [
    ...(appStore.cachedPublicSettings?.custom_menu_items ?? []),
    ...(authStore.isAdmin ? adminSettingsStore.customMenuItems : []),
  ]
  document.title = resolveRouteDocumentTitle(to, appStore.siteName, customMenuItems, {
    billingMode: resolveSiteBillingMode(appStore.cachedPublicSettings),
  })

  // Check if route requires authentication
  const requiresAuth = to.meta.requiresAuth !== false // Default to true
  const requiresAdmin = to.meta.requiresAdmin === true

  if (to.path === '/setup') {
    try {
      const status = await getSetupStatus()
      if (!status.needs_setup) {
        next(resolveCompletedSetupRedirectPath(authStore.isAuthenticated, authStore.isAdmin))
        return
      }
    } catch {
      // If setup status cannot be determined, keep the setup page reachable.
    }
  }

  // If route doesn't require auth, allow access
  if (!requiresAuth) {
    if (to.path === '/model-plaza' || to.path === '/models' || to.meta.featureFlag === 'modelPlaza') {
      if (!appStore.publicSettingsLoaded) {
        try {
          await appStore.fetchPublicSettings()
        } catch (error) {
          console.warn('Failed to load public settings in route guard', error)
        }
      }
    }

    const publicPolicy = evaluateRoutePolicy(
      {
        requiresAuth: false,
        featureFlag: to.meta.featureFlag
      },
      {
        path: to.path,
        fullPath: to.fullPath,
        isAuthenticated: authStore.isAuthenticated,
        isAdmin: authStore.isAdmin,
        isSimpleMode: authStore.isSimpleMode,
        hasPendingAuthSession: authStore.hasPendingAuthSession,
        backendModeEnabled: appStore.backendModeEnabled,
        publicSettingsLoaded: appStore.publicSettingsLoaded,
        publicSettings: appStore.cachedPublicSettings
      },
      { allowDevGallery: Boolean(import.meta.env.DEV) }
    )
    if (!shouldAllowNavigation(publicPolicy) && publicPolicy.redirectTo) {
      next(publicPolicy.redirectTo)
      return
    }
    next()
    return
  }

  // Route requires authentication
  if (!authStore.isAuthenticated) {
    // Not authenticated, redirect to login
    next({
      path: '/login',
      query: { redirect: to.fullPath } // Save intended destination
    })
    return
  }

  // Check admin requirement
  if (requiresAdmin && !authStore.isAdmin) {
    // User is authenticated but not admin, redirect to user dashboard
    next('/dashboard')
    return
  }

  let stillAuthorizedAdmin: (() => boolean) | undefined
  if (requiresAdmin && authStore.isAdmin) {
    const adminComplianceStore = useAdminComplianceStore()
    const identityRevision = adminComplianceStore.getIdentityRevision()
    const identity = [authStore.user?.id, authStore.user?.role, authStore.sessionRevision]
    const sameIdentity = () => identityRevision === adminComplianceStore.getIdentityRevision() &&
      identity[0] === authStore.user?.id &&
      identity[1] === authStore.user?.role && identity[2] === authStore.sessionRevision
    stillAuthorizedAdmin = () => sameIdentity() && authStore.isAuthenticated && authStore.isAdmin &&
      adminComplianceStore.status?.required === false
    try {
      await adminComplianceStore.fetchStatus()
    } catch (error) {
      if (!sameIdentity()) {
        next(false)
        return
      }
      const err = normalizeApiError(error)
      // Unknown status is fail-closed too. The gate retains the logout path.
      adminComplianceStore.requireAcknowledgement(
        err.status === 423 && err.reason === 'ADMIN_COMPLIANCE_ACK_REQUIRED' ? err.metadata : undefined
      )
    }
    if (!sameIdentity() || !authStore.isAuthenticated || !authStore.isAdmin) {
      next(false)
      return
    }
    if (adminComplianceStore.status?.required !== false && to.path !== '/admin/compliance') {
      next({ path: '/admin/compliance', query: { redirect: to.fullPath } })
      return
    }
    if (to.path === '/admin/compliance') {
      if (adminComplianceStore.status?.required === false) {
        const redirect = to.query.redirect
        next(typeof redirect === 'string' && redirect.startsWith('/admin/') &&
          !redirect.startsWith('/admin/compliance') ? redirect : '/admin/dashboard')
      } else {
        next()
      }
      return
    }
  }


  // 公共设置可能尚未加载（App.vue 的 onMounted 异步拉取晚于首次导航，且纯静态部署
  // 无 __APP_CONFIG__ 注入）。此时 cachedPublicSettings 为空会把 payment/risk_control
  // 误判为“未启用”而错误拦截，故这里先确保设置加载完成。
  if (
    (to.meta.requiresPayment ||
      to.meta.requiresRiskControl ||
      to.meta.requiresSubscription ||
      to.meta.featureFlag) &&
    !appStore.publicSettingsLoaded
  ) {
    try {
      await appStore.fetchPublicSettings()
    } catch (error) {
      console.warn('Failed to load public settings in route guard', error)
    }
  }

  if (stillAuthorizedAdmin && !stillAuthorizedAdmin()) {
    next(false)
    return
  }

  const protectedPolicy = evaluateRoutePolicy(
    {
      requiresAuth: true,
      requiresAdmin,
      hideInSimpleMode: to.meta.hideInSimpleMode,
      requiresPayment: to.meta.requiresPayment,
      requiresRiskControl: to.meta.requiresRiskControl,
      requiresSubscription: to.meta.requiresSubscription,
      featureFlag: to.meta.featureFlag
    },
    {
      path: to.path,
      fullPath: to.fullPath,
      isAuthenticated: authStore.isAuthenticated,
      isAdmin: authStore.isAdmin,
      isSimpleMode: authStore.isSimpleMode,
      hasPendingAuthSession: authStore.hasPendingAuthSession,
      backendModeEnabled: appStore.backendModeEnabled,
      publicSettingsLoaded: appStore.publicSettingsLoaded,
      publicSettings: appStore.cachedPublicSettings
    },
    { allowDevGallery: Boolean(import.meta.env.DEV) }
  )

  if (!shouldAllowNavigation(protectedPolicy) && protectedPolicy.redirectTo) {
    next(protectedPolicy.redirectTo)
    return
  }

  // All checks passed, allow navigation
  next()
})

/**
 * Navigation guard: End loading and trigger prefetch
 */
router.afterEach((to, _from, failure) => {
  // 结束导航加载状态
  navigationLoading.endNavigation()

  if (failure) return

  // 懒初始化预加载（首次导航时创建，传入 router 实例）
  if (!routePrefetch) {
    routePrefetch = useRoutePrefetch(router, (route) => {
      const auth = useAuthStore()
      return auth.isAuthenticated && (!route.meta.requiresAdmin ||
        (auth.isAdmin && useAdminComplianceStore().status?.required === false))
    })
  }
  // 触发路由预加载（在浏览器空闲时执行）
  routePrefetch.triggerPrefetch(to)
})

/**
 * Navigation guard: Error handling
 * Handles dynamic import failures caused by deployment updates
 */
router.onError((error) => {
  console.error('Router error:', error)

  // Check if this is a dynamic import failure (chunk loading error)
  const isChunkLoadError =
    error.message?.includes('Failed to fetch dynamically imported module') ||
    error.message?.includes('Loading chunk') ||
    error.message?.includes('Loading CSS chunk') ||
    error.name === 'ChunkLoadError'

  if (isChunkLoadError) {
    const lastReload = routerChunkReloadStorage.get()
    const now = Date.now()

    // Allow reload if never attempted or more than 10 seconds ago
    if (!lastReload || now - parseInt(lastReload) > 10000) {
      routerChunkReloadStorage.set(now.toString())
      console.warn('Chunk load error detected, reloading page to fetch latest version...')
      window.location.reload()
    } else {
      console.error('Chunk load error persists after reload. Please clear browser cache.')
    }
  }
})

export default router
