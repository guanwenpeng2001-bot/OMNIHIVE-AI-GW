import { ref } from 'vue'
import type { IconName } from '@/components/icons/Icon.vue'
import type { RouteFeatureFlag, RoutePolicyContext, ShellTier } from './routePolicy'
import { evaluateRoutePolicy, shouldDisplayNavigationEntry } from './routePolicy'

export type NavSectionId =
  | 'overview'
  | 'models'
  | 'access'
  | 'policies'
  | 'requests'
  | 'billing'
  | 'settings'
  | 'public'

export interface ShellNavItemSpec {
  id: string
  path: string
  tier: Exclude<ShellTier, 'bare'>
  section: NavSectionId
  labelKey?: string
  fallbackLabelEn: string
  fallbackLabelZh: string
  icon: IconName
  iconSvg?: string
  tourId?: string
  order?: number
  keywords?: string[]
  hideInSimpleMode?: boolean
  featureFlag?: RouteFeatureFlag
  expandOnly?: boolean
  children?: ShellNavItemSpec[]
}

export interface ShellNavSectionMeta {
  id: NavSectionId
  eyebrowEn: string
  eyebrowZh: string
}

export const USER_CONSOLE_SECTIONS: readonly ShellNavSectionMeta[] = [
  { id: 'overview', eyebrowEn: 'OVERVIEW', eyebrowZh: 'OVERVIEW · 概览' },
  { id: 'models', eyebrowEn: 'MODELS', eyebrowZh: 'MODELS · 模型与调用' },
  { id: 'access', eyebrowEn: 'ACCESS', eyebrowZh: 'ACCESS · 访问与空间' },
  { id: 'requests', eyebrowEn: 'REQUESTS & TASKS', eyebrowZh: 'REQUESTS · 请求与任务' },
  { id: 'billing', eyebrowEn: 'BILLING', eyebrowZh: 'BILLING · 费用与权益' },
  { id: 'settings', eyebrowEn: 'SETTINGS', eyebrowZh: 'SETTINGS · 设置' }
]

export const ADMIN_CONSOLE_SECTIONS: readonly ShellNavSectionMeta[] = [
  { id: 'overview', eyebrowEn: 'OVERVIEW', eyebrowZh: 'OVERVIEW · 运行概览' },
  { id: 'models', eyebrowEn: 'CATALOG', eyebrowZh: 'CATALOG · 模型与目录' },
  { id: 'access', eyebrowEn: 'CONNECTIONS & POOLS', eyebrowZh: 'RESOURCES · 连接与资源池' },
  { id: 'policies', eyebrowEn: 'POLICIES', eyebrowZh: 'POLICIES · 策略与治理' },
  { id: 'requests', eyebrowEn: 'DIAGNOSTICS & OPS', eyebrowZh: 'DIAGNOSTICS · 诊断与监控' },
  { id: 'billing', eyebrowEn: 'SETTLEMENT', eyebrowZh: 'SETTLEMENT · 结算与套餐' },
  { id: 'settings', eyebrowEn: 'SYSTEM', eyebrowZh: 'SYSTEM · 用户与系统' }
]

const registeredExtensionItems = ref<ShellNavItemSpec[]>([])

/**
 * Extension API for WP2–WP7 to register new console or public navigation entries
 * without creating merge hotspots in AppSidebar.vue.
 */
export function registerNavigationItems(items: ShellNavItemSpec[]): () => void {
  const ids = new Set(items.map((item) => item.id))
  registeredExtensionItems.value = [
    ...registeredExtensionItems.value.filter((existing) => !ids.has(existing.id)),
    ...items
  ]
  return () => {
    registeredExtensionItems.value = registeredExtensionItems.value.filter(
      (existing) => !ids.has(existing.id)
    )
  }
}

export function getRegisteredNavigationItems(): ShellNavItemSpec[] {
  return registeredExtensionItems.value
}

export const CORE_NAVIGATION_ITEMS: readonly ShellNavItemSpec[] = [
  // ==================== Public Tier ====================
  {
    id: 'public-home',
    path: '/home',
    tier: 'public',
    section: 'public',
    fallbackLabelEn: 'Home',
    fallbackLabelZh: '首页',
    icon: 'globe',
    order: 10,
    keywords: ['home', 'landing', 'portal', '首页']
  },
  {
    id: 'public-models',
    path: '/model-plaza',
    tier: 'public',
    section: 'public',
    labelKey: 'nav.modelPlaza',
    fallbackLabelEn: 'Models',
    fallbackLabelZh: '模型广场',
    icon: 'grid',
    featureFlag: 'modelPlaza',
    order: 20,
    keywords: ['models', 'plaza', 'catalog', 'pricing', '模型广场']
  },
  {
    id: 'public-key-usage',
    path: '/key-usage',
    tier: 'public',
    section: 'public',
    fallbackLabelEn: 'Key Quota',
    fallbackLabelZh: '密钥额度查询',
    icon: 'chart',
    order: 30,
    keywords: ['key', 'quota', 'inspector', 'balance', '额度查询']
  },

  // ==================== User Console Tier ====================
  {
    id: 'user-dashboard',
    path: '/dashboard',
    tier: 'user',
    section: 'overview',
    labelKey: 'nav.dashboard',
    fallbackLabelEn: 'Overview',
    fallbackLabelZh: '概览',
    icon: 'grid',
    order: 10,
    keywords: ['dashboard', 'overview', 'spend', '概览', '控制台']
  },
  {
    id: 'user-models',
    path: '/model-plaza',
    tier: 'user',
    section: 'models',
    labelKey: 'nav.modelPlaza',
    fallbackLabelEn: 'Model Plaza',
    fallbackLabelZh: '模型广场',
    icon: 'cube',
    featureFlag: 'modelPlaza',
    order: 10,
    keywords: ['models', 'plaza', 'catalog', 'compare', '模型广场']
  },
  {
    id: 'user-docs',
    path: '/help',
    tier: 'user',
    section: 'models',
    labelKey: 'nav.helpGuide',
    fallbackLabelEn: 'API Docs & Quickstart',
    fallbackLabelZh: 'API 文档与接入',
    icon: 'book',
    order: 20,
    keywords: ['docs', 'guide', 'sdk', 'openai', 'anthropic', 'gemini', '文档']
  },
  {
    id: 'user-keys',
    path: '/keys',
    tier: 'user',
    section: 'access',
    labelKey: 'nav.apiKeys',
    fallbackLabelEn: 'Access Keys',
    fallbackLabelZh: '访问密钥',
    icon: 'key',
    order: 10,
    keywords: ['api', 'keys', 'token', 'credentials', '密钥']
  },
  {
    id: 'user-workspaces',
    path: '/workspaces',
    tier: 'user',
    section: 'access',
    labelKey: 'workspaces.title',
    fallbackLabelEn: 'Workspaces',
    fallbackLabelZh: '工作空间',
    icon: 'users',
    order: 20,
    keywords: ['workspaces', 'projects', 'members', 'scopes', '工作空间']
  },
  {
    id: 'user-available-channels',
    path: '/available-channels',
    tier: 'user',
    section: 'access',
    labelKey: 'nav.availableChannels',
    fallbackLabelEn: 'Available Pools',
    fallbackLabelZh: '可用资源池',
    icon: 'server',
    hideInSimpleMode: true,
    featureFlag: 'availableChannels',
    order: 30,
    keywords: ['channels', 'pools', 'groups', '可用渠道']
  },
  {
    id: 'user-usage',
    path: '/usage',
    tier: 'user',
    section: 'requests',
    labelKey: 'nav.usage',
    fallbackLabelEn: 'Request Logs & Usage',
    fallbackLabelZh: '请求与用量明细',
    icon: 'chart',
    hideInSimpleMode: true,
    order: 10,
    keywords: ['requests', 'logs', 'usage', 'tokens', 'latency', '用量', '请求日志']
  },
  {
    id: 'user-tasks',
    path: '/batch-image',
    tier: 'user',
    section: 'requests',
    labelKey: 'nav.batchImage',
    fallbackLabelEn: 'Async Tasks & Media',
    fallbackLabelZh: '异步任务与生图',
    icon: 'sparkles',
    hideInSimpleMode: true,
    order: 20,
    keywords: ['tasks', 'batch', 'image', 'async', '异步任务', '批量生图']
  },
  {
    id: 'user-monitor',
    path: '/monitor',
    tier: 'user',
    section: 'requests',
    labelKey: 'nav.channelStatus',
    fallbackLabelEn: 'Service Health',
    fallbackLabelZh: '服务健康状态',
    icon: 'shield',
    featureFlag: 'channelMonitor',
    order: 30,
    keywords: ['status', 'health', 'monitor', 'uptime', '状态', '监控']
  },
  {
    id: 'user-purchase',
    path: '/purchase',
    tier: 'user',
    section: 'billing',
    labelKey: 'nav.buySubscription',
    fallbackLabelEn: 'Top Up & Plans',
    fallbackLabelZh: '充值与套餐',
    icon: 'dollar',
    hideInSimpleMode: true,
    featureFlag: 'payment',
    order: 10,
    keywords: ['billing', 'topup', 'recharge', 'plans', '充值', '订阅']
  },
  {
    id: 'user-subscriptions',
    path: '/subscriptions',
    tier: 'user',
    section: 'billing',
    labelKey: 'nav.mySubscriptions',
    fallbackLabelEn: 'Subscriptions',
    fallbackLabelZh: '我的订阅',
    icon: 'creditCard',
    hideInSimpleMode: true,
    featureFlag: 'subscription',
    order: 20,
    keywords: ['subscriptions', 'quota', 'plan', '我的订阅']
  },
  {
    id: 'user-orders',
    path: '/orders',
    tier: 'user',
    section: 'billing',
    labelKey: 'nav.myOrders',
    fallbackLabelEn: 'Orders',
    fallbackLabelZh: '账单与订单',
    icon: 'document',
    hideInSimpleMode: true,
    featureFlag: 'payment',
    order: 30,
    keywords: ['orders', 'invoices', 'receipts', '订单']
  },
  {
    id: 'user-redeem',
    path: '/redeem',
    tier: 'user',
    section: 'billing',
    labelKey: 'nav.redeem',
    fallbackLabelEn: 'Redeem Code',
    fallbackLabelZh: '兑换码',
    icon: 'gift',
    hideInSimpleMode: true,
    order: 40,
    keywords: ['redeem', 'voucher', 'credit', '兑换']
  },
  {
    id: 'user-affiliate',
    path: '/affiliate',
    tier: 'user',
    section: 'billing',
    labelKey: 'nav.affiliate',
    fallbackLabelEn: 'Referrals',
    fallbackLabelZh: '邀请返利',
    icon: 'users',
    hideInSimpleMode: true,
    featureFlag: 'affiliate',
    order: 50,
    keywords: ['affiliate', 'referrals', 'invite', '邀请']
  },
  {
    id: 'user-profile',
    path: '/profile',
    tier: 'user',
    section: 'settings',
    labelKey: 'nav.profile',
    fallbackLabelEn: 'Security & Profile',
    fallbackLabelZh: '个人与安全',
    icon: 'user',
    order: 10,
    keywords: ['profile', 'password', 'totp', 'passkey', 'security', '个人资料']
  },
  {
    id: 'user-mcp',
    path: '/settings/mcp',
    tier: 'user',
    section: 'settings',
    fallbackLabelEn: 'Authorized Apps',
    fallbackLabelZh: '授权应用 (MCP)',
    icon: 'link',
    order: 20,
    keywords: ['mcp', 'oauth', 'authorized', 'apps', '授权']
  },

  // ==================== Admin Console Tier ====================
  {
    id: 'admin-dashboard',
    path: '/admin/dashboard',
    tier: 'admin',
    section: 'overview',
    labelKey: 'nav.dashboard',
    fallbackLabelEn: 'Control Overview',
    fallbackLabelZh: '运行总览',
    icon: 'grid',
    order: 10,
    keywords: ['admin', 'dashboard', 'traffic', 'throughput', '总览仪表盘']
  },
  {
    id: 'admin-channels-pricing',
    path: '/admin/channels/pricing',
    tier: 'admin',
    section: 'models',
    labelKey: 'nav.channelPricing',
    fallbackLabelEn: 'Model Catalog & Pricing',
    fallbackLabelZh: '模型目录与定价',
    icon: 'dollar',
    hideInSimpleMode: true,
    order: 10,
    keywords: ['catalog', 'models', 'mapping', 'pricing', '渠道定价', '模型目录']
  },
  {
    id: 'admin-accounts',
    path: '/admin/accounts',
    tier: 'admin',
    section: 'access',
    labelKey: 'nav.accounts',
    fallbackLabelEn: 'Upstream Connections',
    fallbackLabelZh: '上游连接与凭据',
    icon: 'globe',
    tourId: 'sidebar-channel-manage',
    order: 10,
    keywords: ['connections', 'credentials', 'accounts', 'providers', '上游连接', '上游账号']
  },
  {
    id: 'admin-groups',
    path: '/admin/groups',
    tier: 'admin',
    section: 'access',
    labelKey: 'nav.groups',
    fallbackLabelEn: 'Resource Pools & Routing',
    fallbackLabelZh: '资源池与路由策略',
    icon: 'grid',
    tourId: 'sidebar-group-manage',
    order: 20,
    keywords: ['pools', 'groups', 'routing', 'priority', '资源池', '分组']
  },
  {
    id: 'admin-proxies',
    path: '/admin/proxies',
    tier: 'admin',
    section: 'access',
    labelKey: 'nav.ipNodes',
    fallbackLabelEn: 'Egress Proxies',
    fallbackLabelZh: '出口代理节点',
    icon: 'server',
    order: 30,
    keywords: ['egress', 'proxies', 'ip', 'nodes', '代理']
  },
  {
    id: 'admin-proxy-groups',
    path: '/admin/proxy-groups',
    tier: 'admin',
    section: 'access',
    labelKey: 'nav.ipGroups',
    fallbackLabelEn: 'Egress Pools',
    fallbackLabelZh: '出口代理池',
    icon: 'server',
    order: 40,
    keywords: ['egress', 'proxy', 'groups', 'pools', '代理组']
  },
  {
    id: 'admin-security-audit',
    path: '/admin/security-audit',
    tier: 'admin',
    section: 'policies',
    labelKey: 'nav.securityAudit',
    fallbackLabelEn: 'Safety & Content Policy',
    fallbackLabelZh: '安全与内容治理',
    icon: 'shield',
    expandOnly: true,
    featureFlag: 'riskControl',
    order: 10,
    keywords: ['policy', 'risk', 'prompt', 'audit', 'moderation', '风控'],
    children: [
      {
        id: 'admin-risk-control',
        path: '/admin/risk-control',
        tier: 'admin',
        section: 'policies',
        labelKey: 'nav.contentModeration',
        fallbackLabelEn: 'Risk & Rate Policy',
        fallbackLabelZh: '风控策略中心',
        icon: 'shield',
        featureFlag: 'riskControl',
        order: 10
      },
      {
        id: 'admin-prompt-audit',
        path: '/admin/prompt-audit',
        tier: 'admin',
        section: 'policies',
        labelKey: 'nav.promptAudit',
        fallbackLabelEn: 'Prompt Audit',
        fallbackLabelZh: '提示词审计',
        icon: 'shield',
        featureFlag: 'riskControl',
        order: 20
      }
    ]
  },
  {
    id: 'admin-ops',
    path: '/admin/ops',
    tier: 'admin',
    section: 'requests',
    labelKey: 'nav.ops',
    fallbackLabelEn: 'Realtime Operations',
    fallbackLabelZh: '实时运维监控',
    icon: 'chart',
    featureFlag: 'opsMonitoring',
    order: 10,
    keywords: ['ops', 'realtime', 'qps', 'latency', 'errors', '运维监控']
  },
  {
    id: 'admin-channel-monitor',
    path: '/admin/channels/monitor',
    tier: 'admin',
    section: 'requests',
    labelKey: 'nav.channelMonitor',
    fallbackLabelEn: 'Upstream Health Probes',
    fallbackLabelZh: '上游健康探测',
    icon: 'chart',
    hideInSimpleMode: true,
    featureFlag: 'channelMonitor',
    order: 20,
    keywords: ['channel', 'monitor', 'probes', 'availability', '渠道监控']
  },
  {
    id: 'admin-usage',
    path: '/admin/usage',
    tier: 'admin',
    section: 'requests',
    labelKey: 'nav.usage',
    fallbackLabelEn: 'Request & Usage Ledger',
    fallbackLabelZh: '全局请求与账本',
    icon: 'document',
    order: 30,
    keywords: ['usage', 'requests', 'waterfall', 'ledger', '用量记录']
  },
  {
    id: 'admin-orders-dashboard',
    path: '/admin/orders/dashboard',
    tier: 'admin',
    section: 'billing',
    labelKey: 'nav.paymentDashboard',
    fallbackLabelEn: 'Revenue & Settlement',
    fallbackLabelZh: '营收与结算大盘',
    icon: 'chart',
    hideInSimpleMode: true,
    featureFlag: 'adminPayment',
    order: 10,
    keywords: ['revenue', 'payment', 'settlement', '支付大盘']
  },
  {
    id: 'admin-orders',
    path: '/admin/orders',
    tier: 'admin',
    section: 'billing',
    labelKey: 'nav.orderManagement',
    fallbackLabelEn: 'Orders',
    fallbackLabelZh: '支付订单管理',
    icon: 'document',
    hideInSimpleMode: true,
    featureFlag: 'adminPayment',
    order: 20,
    keywords: ['orders', 'payments', 'refunds', '订单管理']
  },
  {
    id: 'admin-orders-plans',
    path: '/admin/orders/plans',
    tier: 'admin',
    section: 'billing',
    labelKey: 'nav.paymentPlans',
    fallbackLabelEn: 'Subscription Plans',
    fallbackLabelZh: '订阅套餐配置',
    icon: 'creditCard',
    hideInSimpleMode: true,
    featureFlag: 'adminPayment',
    order: 30,
    keywords: ['plans', 'tiers', 'subscriptions', '订阅套餐']
  },
  {
    id: 'admin-subscriptions',
    path: '/admin/subscriptions',
    tier: 'admin',
    section: 'billing',
    labelKey: 'nav.subscriptions',
    fallbackLabelEn: 'Assigned Subscriptions',
    fallbackLabelZh: '用户订阅分配',
    icon: 'creditCard',
    hideInSimpleMode: true,
    featureFlag: 'subscription',
    order: 40,
    keywords: ['subscriptions', 'entitlements', 'quota', '订阅分配']
  },
  {
    id: 'admin-redeem',
    path: '/admin/redeem',
    tier: 'admin',
    section: 'billing',
    labelKey: 'nav.redeemCodes',
    fallbackLabelEn: 'Redeem Codes',
    fallbackLabelZh: '兑换码管理',
    icon: 'gift',
    tourId: 'sidebar-wallet',
    hideInSimpleMode: true,
    order: 50,
    keywords: ['redeem', 'codes', 'vouchers', '兑换码']
  },
  {
    id: 'admin-promo',
    path: '/admin/promo-codes',
    tier: 'admin',
    section: 'billing',
    labelKey: 'nav.promoCodes',
    fallbackLabelEn: 'Promo Codes',
    fallbackLabelZh: '优惠码管理',
    icon: 'gift',
    hideInSimpleMode: true,
    order: 60,
    keywords: ['promo', 'discounts', '优惠码']
  },
  {
    id: 'admin-affiliates-invites',
    path: '/admin/affiliates/invites',
    tier: 'admin',
    section: 'billing',
    labelKey: 'nav.affiliateInviteRecords',
    fallbackLabelEn: 'Affiliate Invites',
    fallbackLabelZh: '联盟邀请记录',
    icon: 'users',
    hideInSimpleMode: true,
    featureFlag: 'affiliate',
    order: 70
  },
  {
    id: 'admin-affiliates-rebates',
    path: '/admin/affiliates/rebates',
    tier: 'admin',
    section: 'billing',
    labelKey: 'nav.affiliateRebateRecords',
    fallbackLabelEn: 'Affiliate Rebates',
    fallbackLabelZh: '联盟返利记录',
    icon: 'dollar',
    hideInSimpleMode: true,
    featureFlag: 'affiliate',
    order: 80
  },
  {
    id: 'admin-affiliates-transfers',
    path: '/admin/affiliates/transfers',
    tier: 'admin',
    section: 'billing',
    labelKey: 'nav.affiliateTransferRecords',
    fallbackLabelEn: 'Payout Reviews',
    fallbackLabelZh: '提现审核',
    icon: 'creditCard',
    hideInSimpleMode: true,
    featureFlag: 'affiliate',
    order: 90
  },
  {
    id: 'admin-users',
    path: '/admin/users',
    tier: 'admin',
    section: 'settings',
    labelKey: 'nav.users',
    fallbackLabelEn: 'Users & Roles',
    fallbackLabelZh: '用户与角色',
    icon: 'users',
    hideInSimpleMode: true,
    order: 10,
    keywords: ['users', 'members', 'roles', 'balance', '用户管理']
  },
  {
    id: 'admin-audit-logs',
    path: '/admin/audit-logs',
    tier: 'admin',
    section: 'settings',
    labelKey: 'nav.auditLogs',
    fallbackLabelEn: 'Audit Logs',
    fallbackLabelZh: '审计日志',
    icon: 'shield',
    hideInSimpleMode: true,
    order: 20,
    keywords: ['audit', 'logs', 'security', 'compliance', '审计日志']
  },
  {
    id: 'admin-announcements',
    path: '/admin/announcements',
    tier: 'admin',
    section: 'settings',
    labelKey: 'nav.announcements',
    fallbackLabelEn: 'Announcements',
    fallbackLabelZh: '系统公告',
    icon: 'bell',
    order: 30,
    keywords: ['announcements', 'notices', '公告']
  },
  {
    id: 'admin-backup',
    path: '/admin/backup',
    tier: 'admin',
    section: 'settings',
    fallbackLabelEn: 'Backup & Restore',
    fallbackLabelZh: '备份与恢复',
    icon: 'download',
    order: 40,
    keywords: ['backup', 'restore', 'archive', '备份']
  },
  {
    id: 'admin-plugins',
    path: '/admin/plugins',
    tier: 'admin',
    section: 'settings',
    labelKey: 'nav.plugins',
    fallbackLabelEn: 'Extensions & Plugins',
    fallbackLabelZh: '扩展与插件',
    icon: 'cube',
    featureFlag: 'pluginManagement',
    order: 50,
    keywords: ['plugins', 'extensions', '插件']
  },
  {
    id: 'admin-settings',
    path: '/admin/settings',
    tier: 'admin',
    section: 'settings',
    labelKey: 'nav.settings',
    fallbackLabelEn: 'System Settings',
    fallbackLabelZh: '系统设置',
    icon: 'cog',
    order: 60,
    keywords: ['settings', 'system', 'branding', 'oauth', 'smtp', '系统设置']
  }
]

export function resolveVisibleTierNavItems(
  tier: Exclude<ShellTier, 'bare'>,
  context: Omit<RoutePolicyContext, 'path' | 'fullPath'>
): ShellNavItemSpec[] {
  const combined = [...CORE_NAVIGATION_ITEMS, ...registeredExtensionItems.value]
    .filter((item) => item.tier === tier)
    .sort((a, b) => (a.order ?? 100) - (b.order ?? 100))

  const filterList = (entries: ShellNavItemSpec[]): ShellNavItemSpec[] =>
    entries.flatMap((entry) => {
      const evaluation = evaluateRoutePolicy(
        {
          requiresAuth: entry.tier !== 'public',
          requiresAdmin: entry.tier === 'admin',
          hideInSimpleMode: entry.hideInSimpleMode,
          featureFlag: entry.featureFlag
        },
        {
          ...context,
          path: entry.path,
          fullPath: entry.path,
          complianceRequired: false
        }
      )
      if (!shouldDisplayNavigationEntry(evaluation)) return []
      if (entry.children && entry.children.length > 0) {
        const nextChildren = filterList(entry.children)
        if (nextChildren.length === 0) return []
        return [{ ...entry, children: nextChildren }]
      }
      return [entry]
    })

  return filterList(combined)
}
