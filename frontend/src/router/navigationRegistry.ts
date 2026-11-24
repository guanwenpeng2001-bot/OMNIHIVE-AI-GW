import { ref } from 'vue'
import type Icon from '@/components/icons/Icon.vue'
type IconName = InstanceType<typeof Icon>['$props']['name']
import type { RouteFeatureFlag, RoutePolicyContext, ShellTier } from './routePolicy'
import { evaluateRoutePolicy, shouldDisplayNavigationEntry } from './routePolicy'

export type NavSectionId =
  | 'overview'
  | 'models'
  | 'access'
  | 'policies'
  | 'requests'
  | 'billing'
  | 'ops'
  | 'activity'
  | 'connections'
  | 'settlement'
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
  hiddenInSidebar?: boolean
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
  { id: 'overview', eyebrowEn: 'OVERVIEW', eyebrowZh: '概览' },
  { id: 'models', eyebrowEn: 'MODELS', eyebrowZh: '模型与调用' },
  { id: 'access', eyebrowEn: 'ACCESS', eyebrowZh: '访问与空间' },
  { id: 'requests', eyebrowEn: 'REQUESTS & TASKS', eyebrowZh: '请求与任务' },
  { id: 'billing', eyebrowEn: 'BILLING', eyebrowZh: '费用与权益' },
  { id: 'settings', eyebrowEn: 'SETTINGS', eyebrowZh: '账户设置' }
]

export const ADMIN_CONSOLE_SECTIONS: readonly ShellNavSectionMeta[] = [
  { id: 'ops', eyebrowEn: 'OPERATIONS', eyebrowZh: '运行观测' },
  { id: 'connections', eyebrowEn: 'CONNECTIONS & ROUTING', eyebrowZh: '连接与路由' },
  { id: 'settlement', eyebrowEn: 'SETTLEMENT & PLANS', eyebrowZh: '结算与套餐' },
  { id: 'settings', eyebrowEn: 'GOVERNANCE & SYSTEM', eyebrowZh: '系统与治理' }
]

const registeredExtensionItems = ref<ShellNavItemSpec[]>([])

/**
 * Extension API for feature modules to register new console or public navigation entries
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

export const ADMIN_MODELS_NAV_ITEM: ShellNavItemSpec = {
  id: 'admin-models',
  path: '/admin/models',
  tier: 'admin',
  section: 'connections',
  labelKey: 'nav.modelCatalog',
  fallbackLabelEn: 'Model Catalog',
  fallbackLabelZh: '模型目录',
  icon: 'cube',
  order: 27,
  keywords: ['models', 'catalog', 'provenance', 'pricing', '模型目录']
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
    fallbackLabelEn: 'Model Plaza',
    fallbackLabelZh: '模型广场',
    icon: 'grid',
    order: 20,
    keywords: ['models', 'plaza', 'catalog', 'pricing', '模型广场']
  },
  {
    id: 'public-model-compare',
    path: '/model-plaza/compare',
    tier: 'public',
    section: 'public',
    labelKey: 'nav.modelCompare',
    fallbackLabelEn: 'Compare Models',
    fallbackLabelZh: '模型对比',
    icon: 'cube',
    featureFlag: 'modelPlaza',
    order: 25,
    keywords: ['compare', 'comparison', 'models', 'diff', '对比', '模型对比']
  },
  {
    id: 'public-pricing',
    path: '/pricing',
    tier: 'public',
    section: 'public',
    labelKey: 'nav.pricing',
    fallbackLabelEn: 'Pricing',
    fallbackLabelZh: '定价估算',
    icon: 'dollar',
    order: 30,
    keywords: ['pricing', 'plans', 'rates', '定价', '价格']
  },
  {
    id: 'public-key-usage',
    path: '/key-usage',
    tier: 'public',
    section: 'public',
    labelKey: 'nav.keyUsage',
    fallbackLabelEn: 'Key Quota',
    fallbackLabelZh: '密钥额度',
    icon: 'chart',
    order: 40,
    keywords: ['key', 'quota', 'inspector', 'balance', '额度查询', '密钥额度']
  },
  {
    id: 'public-status',
    path: '/status',
    tier: 'public',
    section: 'public',
    fallbackLabelEn: 'Service Status',
    fallbackLabelZh: '服务状态',
    icon: 'shield',
    order: 50,
    keywords: ['status', 'uptime', 'health', '服务状态', '健康']
  },
  {
    id: 'public-docs',
    path: '/docs',
    tier: 'public',
    section: 'public',
    fallbackLabelEn: 'Documentation',
    fallbackLabelZh: '接入文档',
    icon: 'book',
    order: 60,
    keywords: ['docs', 'documentation', 'help', 'guide', 'quickstart', 'sdk', 'api', '文档', '接入文档', '使用指南']
  },
  {
    id: 'public-compatibility',
    path: '/compatibility',
    tier: 'public',
    section: 'public',
    fallbackLabelEn: 'Updates & Compatibility',
    fallbackLabelZh: '更新与兼容性',
    icon: 'shield',
    order: 70,
    keywords: ['compatibility', 'updates', 'protocol', 'plugin', 'ohplugin', 's2plugin', 'checksum', '兼容', '兼容性', '更新与兼容']
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
    fallbackLabelZh: '接入文档',
    icon: 'book',
    order: 20,
    keywords: ['docs', 'guide', 'sdk', 'openai', 'anthropic', 'gemini', '文档']
  },
  {
    id: 'user-playground',
    path: '/playground',
    tier: 'user',
    section: 'models',
    labelKey: 'nav.playground',
    fallbackLabelEn: 'Playground',
    fallbackLabelZh: '模型调试台',
    icon: 'play',
    order: 15,
    keywords: ['playground', 'chat', 'stream', 'compare', '调试', '流式']
  },
  {
    id: 'user-keys',
    path: '/keys',
    tier: 'user',
    section: 'access',
    labelKey: 'nav.apiKeys',
    fallbackLabelEn: 'API Keys',
    fallbackLabelZh: 'API 密钥',
    icon: 'key',
    tourId: 'sidebar-my-keys',
    order: 10,
    keywords: ['api', 'keys', 'token', 'credentials', '密钥']
  },
  {
    id: 'user-byok',
    path: '/byok',
    tier: 'user',
    section: 'access',
    labelKey: 'nav.byok',
    fallbackLabelEn: 'BYOK Credential',
    fallbackLabelZh: '自带上游凭据',
    icon: 'shield',
    order: 15,
    keywords: ['byok', 'keys', 'credentials', 'provider', '自带上游凭据']
  },
  {
    id: 'user-workspaces',
    path: '/workspaces',
    tier: 'user',
    section: 'access',
    labelKey: 'workspaces.title',
    fallbackLabelEn: 'Workspaces & Access',
    fallbackLabelZh: '工作空间与授权',
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
    fallbackLabelEn: 'My Available Models',
    fallbackLabelZh: '我的可用模型',
    icon: 'server',
    hideInSimpleMode: true,
    featureFlag: 'availableChannels',
    order: 30,
    keywords: ['channels', 'pools', 'groups', '我的可用模型']
  },
  {
    id: 'user-usage',
    path: '/usage',
    tier: 'user',
    section: 'requests',
    labelKey: 'nav.usage',
    fallbackLabelEn: 'Usage Analytics',
    fallbackLabelZh: '用量统计',
    icon: 'chart',
    hideInSimpleMode: true,
    order: 10,
    keywords: ['requests', 'logs', 'usage', 'tokens', 'latency', '用量统计']
  },
  {
    id: 'user-requests',
    path: '/requests',
    tier: 'user',
    section: 'requests',
    labelKey: 'nav.requests',
    fallbackLabelEn: 'Request Records',
    fallbackLabelZh: '请求记录',
    icon: 'document',
    order: 9,
    keywords: ['requests', 'attempts', 'waterfall', 'route', '请求', '重试']
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
    order: 21,
    keywords: ['tasks', 'batch', 'image', 'async', '异步任务', '批量生图']
  },
  {
    id: 'user-task-center',
    path: '/tasks',
    tier: 'user',
    section: 'requests',
    labelKey: 'nav.taskCenter',
    fallbackLabelEn: 'Task Center',
    fallbackLabelZh: '任务中心',
    icon: 'inbox',
    order: 15,
    keywords: ['tasks', 'artifacts', 'download', 'retry', '任务', '产物']
  },
  {
    id: 'user-monitor',
    path: '/monitor',
    tier: 'user',
    section: 'requests',
    labelKey: 'nav.channelStatus',
    fallbackLabelEn: 'Service Health',
    fallbackLabelZh: '服务健康',
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
    id: 'user-costs',
    path: '/billing',
    tier: 'user',
    section: 'billing',
    labelKey: 'nav.billing',
    fallbackLabelEn: 'Billing & Ledger',
    fallbackLabelZh: '费用与账单',
    icon: 'calculator',
    order: 5,
    keywords: ['billing', 'cost', 'quota', 'ledger', '费用与账单']
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
    labelKey: 'nav.mcpAuth',
    fallbackLabelEn: 'MCP App Authorization',
    fallbackLabelZh: 'MCP 应用授权',
    icon: 'link',
    order: 20,
    keywords: ['mcp', 'oauth', 'authorized', 'apps', '授权']
  },

  // ==================== Admin Console Tier ====================
  {
    id: 'admin-dashboard',
    path: '/admin/dashboard',
    tier: 'admin',
    section: 'ops',
    labelKey: 'nav.adminDashboard',
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
    section: 'connections',
    labelKey: 'nav.channelPricing',
    fallbackLabelEn: 'Channels & Pricing',
    fallbackLabelZh: '渠道与定价',
    icon: 'dollar',
    hideInSimpleMode: true,
    order: 28,
    keywords: ['catalog', 'models', 'mapping', 'pricing', '渠道与定价']
  },
  ADMIN_MODELS_NAV_ITEM,
  {
    id: 'admin-accounts',
    path: '/admin/accounts',
    tier: 'admin',
    section: 'connections',
    labelKey: 'nav.accounts',
    fallbackLabelEn: 'Upstream Connections',
    fallbackLabelZh: '上游连接',
    icon: 'globe',
    tourId: 'sidebar-channel-manage',
    order: 10,
    keywords: ['connections', 'credentials', 'accounts', 'providers', '上游连接']
  },
  {
    id: 'admin-connections-new',
    path: '/admin/connections/new',
    tier: 'admin',
    section: 'connections',
    fallbackLabelEn: 'New Upstream Connection',
    fallbackLabelZh: '新建上游连接',
    icon: 'plus',
    order: 15,
    hiddenInSidebar: true,
    keywords: ['connection', 'wizard', 'provider', 'credential', '新建连接']
  },
  {
    id: 'admin-groups',
    path: '/admin/groups',
    tier: 'admin',
    section: 'connections',
    labelKey: 'nav.groups',
    fallbackLabelEn: 'Routing Groups',
    fallbackLabelZh: '路由分组',
    icon: 'grid',
    tourId: 'sidebar-group-manage',
    order: 26,
    keywords: ['pools', 'groups', 'routing', 'priority', '路由分组']
  },
  {
    id: 'admin-pools',
    path: '/admin/pools',
    tier: 'admin',
    section: 'connections',
    labelKey: 'nav.pools',
    fallbackLabelEn: 'Resource Pools',
    fallbackLabelZh: '上游资源池',
    icon: 'server',
    order: 20,
    keywords: ['pools', 'members', 'concurrency', '上游资源池']
  },
  {
    id: 'admin-routing',
    path: '/admin/routing',
    tier: 'admin',
    section: 'connections',
    labelKey: 'nav.routing',
    fallbackLabelEn: 'Routing Workbench',
    fallbackLabelZh: '路由工作台',
    icon: 'swap',
    order: 25,
    keywords: ['routing', 'policy', 'simulator', '路由', '模拟']
  },
  {
    id: 'admin-proxies',
    path: '/admin/proxies',
    tier: 'admin',
    section: 'connections',
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
    section: 'connections',
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
    section: 'settings',
    labelKey: 'nav.securityAudit',
    fallbackLabelEn: 'Safety & Content Policy',
    fallbackLabelZh: '安全与内容治理',
    icon: 'shield',
    expandOnly: true,
    featureFlag: 'riskControl',
    order: 25,
    keywords: ['policy', 'risk', 'prompt', 'audit', 'moderation', '风控'],
    children: [
      {
        id: 'admin-risk-control',
        path: '/admin/risk-control',
        tier: 'admin',
        section: 'settings',
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
        section: 'settings',
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
    id: 'admin-project-audit',
    path: '/admin/audit',
    tier: 'admin',
    section: 'settings',
    labelKey: 'nav.projectAudit',
    fallbackLabelEn: 'Project Audit',
    fallbackLabelZh: '项目级审计',
    icon: 'shield',
    order: 15,
    keywords: ['audit', 'project', 'evidence', '项目级审计']
  },
  {
    id: 'admin-ops',
    path: '/admin/ops',
    tier: 'admin',
    section: 'ops',
    labelKey: 'nav.ops',
    fallbackLabelEn: 'Operations Monitor',
    fallbackLabelZh: '运维监控',
    icon: 'chart',
    featureFlag: 'opsMonitoring',
    order: 15,
    keywords: ['ops', 'realtime', 'qps', 'latency', 'errors', '运维监控']
  },
  {
    id: 'admin-channel-monitor',
    path: '/admin/channels/monitor',
    tier: 'admin',
    section: 'ops',
    labelKey: 'nav.channelMonitor',
    fallbackLabelEn: 'Health Probes',
    fallbackLabelZh: '健康探测',
    icon: 'chart',
    hideInSimpleMode: true,
    featureFlag: 'channelMonitor',
    order: 20,
    keywords: ['channel', 'monitor', 'probes', 'availability', '健康探测']
  },
  {
    id: 'admin-usage',
    path: '/admin/usage',
    tier: 'admin',
    section: 'ops',
    labelKey: 'nav.adminUsage',
    fallbackLabelEn: 'Global Usage Ledger',
    fallbackLabelZh: '全局账本',
    icon: 'document',
    order: 30,
    keywords: ['usage', 'requests', 'waterfall', 'ledger', '用量记录']
  },
  {
    id: 'admin-requests',
    path: '/admin/requests',
    tier: 'admin',
    section: 'ops',
    labelKey: 'nav.adminRequests',
    fallbackLabelEn: 'Request Diagnostics',
    fallbackLabelZh: '请求诊断',
    icon: 'document',
    order: 12,
    keywords: ['requests', 'attempts', 'waterfall', 'diagnostics', '请求诊断']
  },
  {
    id: 'admin-orders-dashboard',
    path: '/admin/orders/dashboard',
    tier: 'admin',
    section: 'settlement',
    labelKey: 'nav.paymentDashboard',
    fallbackLabelEn: 'Revenue & Settlement',
    fallbackLabelZh: '营收与结算大盘',
    icon: 'chart',
    hideInSimpleMode: true,
    featureFlag: 'adminPayment',
    order: 25,
    keywords: ['revenue', 'payment', 'settlement', '支付大盘']
  },
  {
    id: 'admin-billing',
    path: '/admin/billing',
    tier: 'admin',
    section: 'settlement',
    labelKey: 'nav.adminBilling',
    fallbackLabelEn: 'Reconciliation & Ledger',
    fallbackLabelZh: '对账与账本',
    icon: 'calculator',
    order: 5,
    keywords: ['billing', 'reconciliation', 'ledger', '对账', '账本']
  },
  {
    id: 'admin-orders',
    path: '/admin/orders',
    tier: 'admin',
    section: 'settlement',
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
    section: 'settlement',
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
    section: 'settlement',
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
    section: 'settlement',
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
    section: 'settlement',
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
    section: 'settlement',
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
    section: 'settlement',
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
    section: 'settlement',
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
    id: 'admin-members',
    path: '/admin/members',
    tier: 'admin',
    section: 'settings',
    labelKey: 'nav.membersAccess',
    fallbackLabelEn: 'Members & Access',
    fallbackLabelZh: '成员与授权',
    icon: 'users',
    hideInSimpleMode: true,
    order: 12,
    keywords: ['members', 'access', 'workspaces', 'grants', 'permissions', '成员', '授权']
  },
  {
    id: 'admin-audit-logs',
    path: '/admin/audit-logs',
    tier: 'admin',
    section: 'settings',
    labelKey: 'nav.auditLogs',
    fallbackLabelEn: 'System Audit Logs',
    fallbackLabelZh: '系统审计日志',
    icon: 'shield',
    hideInSimpleMode: true,
    order: 20,
    keywords: ['audit', 'logs', 'security', 'compliance', '系统审计日志']
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
    labelKey: 'nav.backup',
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
  },
  {
    id: 'admin-settings-workbench',
    path: '/admin/settings/general',
    tier: 'admin',
    section: 'settings',
    labelKey: 'nav.settingsWorkbench',
    fallbackLabelEn: 'Settings Workbench',
    fallbackLabelZh: '分区设置工作台',
    icon: 'cog',
    order: 65,
    keywords: ['settings', 'workbench', 'sections', 'security', 'smtp', '分区设置']
  },
  {
    id: 'admin-settings-diagnostics',
    path: '/admin/settings/diagnostics',
    tier: 'admin',
    section: 'settings',
    labelKey: 'settingsDiagnostics.title',
    fallbackLabelEn: 'Configuration Diagnostics',
    fallbackLabelZh: '配置生效与来源诊断',
    icon: 'cog',
    order: 66,
    hiddenInSidebar: true,
    keywords: ['diagnostics', 'effective', 'config', 'override', '配置诊断', '生效配置']
  }
]

export interface ResolvedNavigationBreadcrumb {
  tier: Exclude<ShellTier, 'bare'>
  section: NavSectionId
  domainLabelEn: string
  domainLabelZh: string
  item: ShellNavItemSpec | null
}

function flattenRegistryEntries(entries: readonly ShellNavItemSpec[]): ShellNavItemSpec[] {
  return entries.flatMap((entry) =>
    entry.children && entry.children.length > 0
      ? [entry, ...flattenRegistryEntries(entry.children)]
      : [entry]
  )
}

function mapAdminSectionMeta(section: NavSectionId): ShellNavSectionMeta {
  switch (section) {
    case 'ops':
    case 'overview':
    case 'requests':
      return ADMIN_CONSOLE_SECTIONS[0]
    case 'connections':
    case 'models':
    case 'access':
      return ADMIN_CONSOLE_SECTIONS[1]
    case 'settlement':
    case 'billing':
      return ADMIN_CONSOLE_SECTIONS[2]
    default:
      return ADMIN_CONSOLE_SECTIONS[3]
  }
}

function mapUserSectionMeta(section: NavSectionId): ShellNavSectionMeta {
  const normalized: NavSectionId = section === 'activity' ? 'requests' : section
  return (
    USER_CONSOLE_SECTIONS.find((item) => item.id === normalized) ?? USER_CONSOLE_SECTIONS[0]
  )
}

/**
 * Resolves the canonical navigation section and item metadata from `navigationRegistry`
 * as the single source of truth for breadcrumbs in `AppHeader`.
 */
export function resolveNavigationBreadcrumb(rawPath: string | null | undefined): ResolvedNavigationBreadcrumb {
  const cleanPath = String(rawPath || '').split(/[?#]/)[0] || '/'
  const wsNormalizedPath = cleanPath.replace(/^\/workspaces\/[^/]+(?=\/|$)/, '') || '/workspaces'
  const isAdminPath = cleanPath.startsWith('/admin')
  const targetTier: Exclude<ShellTier, 'bare'> = isAdminPath ? 'admin' : 'user'
  const lookupPath = isAdminPath ? cleanPath : wsNormalizedPath

  const allEntries = flattenRegistryEntries([
    ...CORE_NAVIGATION_ITEMS,
    ...registeredExtensionItems.value
  ]).filter((entry) => entry.tier === targetTier)

  const matched =
    allEntries
      .filter(
        (entry) => lookupPath === entry.path || lookupPath.startsWith(`${entry.path}/`)
      )
      .sort((a, b) => {
        if (a.expandOnly !== b.expandOnly) return a.expandOnly ? 1 : -1
        return b.path.length - a.path.length
      })[0] ?? null

  if (isAdminPath) {
    let resolvedSection: NavSectionId = matched?.section ?? 'settings'
    if (!matched) {
      if (/^\/admin\/(connections|channels)(\/|$)/.test(lookupPath)) {
        resolvedSection = 'connections'
      } else if (/^\/admin\/affiliates(\/|$)/.test(lookupPath)) {
        resolvedSection = 'settlement'
      }
    }
    const secMeta = mapAdminSectionMeta(resolvedSection)
    return {
      tier: 'admin',
      section: secMeta.id,
      domainLabelEn: `ADMIN · ${secMeta.eyebrowEn}`,
      domainLabelZh: `管理 · ${secMeta.eyebrowZh}`,
      item: matched
    }
  }

  if (matched) {
    const secMeta = mapUserSectionMeta(matched.section)
    return {
      tier: 'user',
      section: secMeta.id,
      domainLabelEn: `CONSOLE · ${secMeta.eyebrowEn}`,
      domainLabelZh: `开发者台 · ${secMeta.eyebrowZh}`,
      item: matched
    }
  }

  if (/^\/payment(\/|$)/.test(lookupPath)) {
    const billingMeta = mapUserSectionMeta('billing')
    return {
      tier: 'user',
      section: 'billing',
      domainLabelEn: `CONSOLE · ${billingMeta.eyebrowEn}`,
      domainLabelZh: `开发者台 · ${billingMeta.eyebrowZh}`,
      item: null
    }
  }

  return {
    tier: 'user',
    section: 'overview',
    domainLabelEn: 'CONSOLE',
    domainLabelZh: '开发者台',
    item: null
  }
}

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
