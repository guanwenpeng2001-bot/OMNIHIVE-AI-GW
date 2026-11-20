import { sanitizeUrl } from '@/utils/url'

export const DEFAULT_BRAND_NAME = 'OmniHive'
export const DEFAULT_BRAND_LOGO = '/logo.svg'
export const DEFAULT_BRAND_SUBTITLE_EN = 'Unified AI Inference Gateway'
export const DEFAULT_BRAND_SUBTITLE_ZH = '统一 AI 推理网关'
export const DEFAULT_DOCUMENT_SUFFIX = 'AI API Gateway'

export interface BrandConfigSnapshot {
  siteName: string
  siteLogo: string
  defaultLogo: string
  subtitle: string
  docUrl: string
  contactInfo: string
  apiBaseUrl: string
  siteIcp: string
  isCustomBrand: boolean
}

export function resolveBrandSiteName(rawSiteName?: string | null): string {
  if (typeof rawSiteName !== 'string') return DEFAULT_BRAND_NAME
  const trimmed = rawSiteName.trim()
  if (!trimmed) {
    return DEFAULT_BRAND_NAME
  }
  return trimmed
}

export function resolveBrandLogoUrl(rawLogo?: string | null): string {
  if (typeof rawLogo !== 'string' || !rawLogo.trim()) return ''
  return sanitizeUrl(rawLogo, { allowRelative: true, allowDataUrl: true })
}

export function formatBrandSplashTitle(rawSiteName?: string | null): string {
  const siteName = resolveBrandSiteName(rawSiteName)
  return `${siteName} - ${DEFAULT_DOCUMENT_SUFFIX}`
}

export function buildBrandConfig(input: {
  siteName?: string | null
  siteLogo?: string | null
  siteSubtitle?: string | null
  docUrl?: string | null
  contactInfo?: string | null
  apiBaseUrl?: string | null
  siteIcp?: string | null
  locale?: string
}): BrandConfigSnapshot {
  const siteName = resolveBrandSiteName(input.siteName)
  const siteLogo = resolveBrandLogoUrl(input.siteLogo)
  const rawSubtitle = (input.siteSubtitle || '').trim()
  const isZh = (input.locale || 'zh').toLowerCase().startsWith('zh')
  const subtitle = rawSubtitle || (isZh ? DEFAULT_BRAND_SUBTITLE_ZH : DEFAULT_BRAND_SUBTITLE_EN)
  const docUrl = input.docUrl ? sanitizeUrl(input.docUrl) : ''

  return {
    siteName,
    siteLogo,
    defaultLogo: DEFAULT_BRAND_LOGO,
    subtitle,
    docUrl,
    contactInfo: (input.contactInfo || '').trim(),
    apiBaseUrl: (input.apiBaseUrl || '').trim(),
    siteIcp: (input.siteIcp || '').trim(),
    isCustomBrand: siteName !== DEFAULT_BRAND_NAME || Boolean(siteLogo)
  }
}
