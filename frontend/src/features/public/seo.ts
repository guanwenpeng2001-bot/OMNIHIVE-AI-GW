export interface RouteSeoInput {
  title: string
  description: string
  canonicalPath?: string
  siteName?: string
}

function upsertMetaTag(attrName: 'name' | 'property', attrValue: string, content: string): void {
  if (typeof document === 'undefined') return
  let el = document.head.querySelector(`meta[${attrName}="${attrValue}"]`) as HTMLMetaElement | null
  if (!el) {
    el = document.createElement('meta')
    el.setAttribute(attrName, attrValue)
    document.head.appendChild(el)
  }
  el.setAttribute('content', content)
}

function upsertCanonicalLink(href: string): void {
  if (typeof document === 'undefined') return
  let el = document.head.querySelector('link[rel="canonical"]') as HTMLLinkElement | null
  if (!el) {
    el = document.createElement('link')
    el.setAttribute('rel', 'canonical')
    document.head.appendChild(el)
  }
  el.setAttribute('href', href)
}

export function applyRouteSeo(input: RouteSeoInput): void {
  if (typeof document === 'undefined') return
  const siteName = input.siteName?.trim() || 'OmniHive'
  document.title = input.title
  upsertMetaTag('name', 'description', input.description)
  upsertMetaTag('property', 'og:type', 'website')
  upsertMetaTag('property', 'og:site_name', siteName)
  upsertMetaTag('property', 'og:title', input.title)
  upsertMetaTag('property', 'og:description', input.description)

  if (input.canonicalPath) {
    const origin =
      typeof window !== 'undefined' && window.location?.origin && window.location.origin !== 'null'
        ? window.location.origin
        : ''
    const normalizedPath = input.canonicalPath.startsWith('/')
      ? input.canonicalPath
      : `/${input.canonicalPath}`
    const canonicalUrl = `${origin}${normalizedPath}`
    upsertMetaTag('property', 'og:url', canonicalUrl)
    upsertCanonicalLink(canonicalUrl)
  }
}

const PUBLIC_SEO_MAP: Record<
  string,
  { enTitle: string; zhTitle: string; enDesc: string; zhDesc: string }
> = {
  '/home': {
    enTitle: 'Home',
    zhTitle: '首页',
    enDesc: 'Unified AI API gateway for OpenAI, Anthropic, and Google Gemini protocols with transparent pricing and live telemetry.',
    zhDesc: '统一的多协议 AI API 网关，支持 OpenAI、Anthropic 与 Gemini 协议，提供透明定价与实时遥测。'
  },
  '/model-plaza': {
    enTitle: 'Model Plaza',
    zhTitle: '模型广场',
    enDesc: 'Browse published models, context limits, capabilities, and per-token pricing across providers.',
    zhDesc: '浏览已发布模型目录、上下文窗口、能力声明与精确按 Token 费率。'
  },
  '/model-plaza/compare': {
    enTitle: 'Model Comparison',
    zhTitle: '模型对比',
    enDesc: 'Compare model capabilities, latency telemetry, and exact-decimal scenario quotes side by side.',
    zhDesc: '横向对比模型能力声明、延迟遥测与精确十进制场景报价。'
  },
  '/pricing': {
    enTitle: 'Pricing',
    zhTitle: '定价方案',
    enDesc: 'Pay-as-you-go token pricing, subscription plans, and interactive cost estimator.',
    zhDesc: '按量计费、订阅方案与交互式 Token 费用估算器。'
  },
  '/status': {
    enTitle: 'Service Status',
    zhTitle: '服务状态',
    enDesc: 'Live gateway health, latency percentiles, and incident timeline.',
    zhDesc: '网关实时可用性、延迟分位数与历史事件时间线。'
  },
  '/docs': {
    enTitle: 'Documentation',
    zhTitle: '配置与使用手册',
    enDesc: 'Quickstart guides, SDK examples, API endpoints, and troubleshooting.',
    zhDesc: '快速开始指南、SDK 示例、API 端点说明与常见问题排查。'
  },
  '/help': {
    enTitle: 'Documentation',
    zhTitle: '配置与使用手册',
    enDesc: 'Quickstart guides, SDK examples, API endpoints, and troubleshooting.',
    zhDesc: '快速开始指南、SDK 示例、API 端点说明与常见问题排查。'
  },
  '/compatibility': {
    enTitle: 'Updates & Compatibility',
    zhTitle: '更新与兼容性',
    enDesc: 'Protocol compatibility matrix, plugin migration window, and versioned changelog.',
    zhDesc: '协议兼容矩阵、插件迁移窗口与版本变更日志。'
  },
  '/key-usage': {
    enTitle: 'Key Quota Lookup',
    zhTitle: '密钥额度查询',
    enDesc: 'Inspect API key remaining quota, validity window, and recent usage.',
    zhDesc: '匿名或登录后查询 API Key 剩余额度、有效期与近期用量。'
  }
}

export function resolvePublicRouteSeo(
  path: string,
  siteName = 'OmniHive',
  isZh = false
): RouteSeoInput | null {
  const normalized = path === '/' ? '/home' : path
  const entry = PUBLIC_SEO_MAP[normalized]
  if (!entry) {
    if (normalized.startsWith('/legal/')) {
      return {
        title: `${isZh ? '法律条款' : 'Legal Document'} - ${siteName}`,
        description: isZh
          ? `${siteName} 服务条款、隐私政策与合规说明。`
          : `${siteName} legal terms, privacy policy, and compliance disclosures.`,
        canonicalPath: normalized,
        siteName
      }
    }
    return null
  }
  return {
    title: `${isZh ? entry.zhTitle : entry.enTitle} - ${siteName}`,
    description: isZh ? entry.zhDesc : entry.enDesc,
    canonicalPath: normalized,
    siteName
  }
}
