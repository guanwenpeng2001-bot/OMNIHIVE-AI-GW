import type { LoginAgreementDocument } from '@/types'

export interface SignupAgreementPayload {
  login_agreement_accepted: boolean
  login_agreement_revision: string
}

export type CanonicalLegalSlotId =
  | 'terms'
  | 'privacy-policy'
  | 'usage-policy'
  | 'refund-policy'
  | 'supported-regions'
  | 'service-specific-terms'

const ALIAS_TO_CANONICAL: Record<string, CanonicalLegalSlotId> = {
  terms: 'terms',
  'terms-of-service': 'terms',
  privacy: 'privacy-policy',
  'privacy-policy': 'privacy-policy',
  'usage-policy': 'usage-policy',
  'refund-policy': 'refund-policy',
  'supported-regions': 'supported-regions',
  'service-specific-terms': 'service-specific-terms',
}

export const DEFAULT_LEGAL_NAV_SLOTS: CanonicalLegalSlotId[] = [
  'terms',
  'privacy-policy',
  'usage-policy',
  'refund-policy',
]

export function canonicalLegalDocumentId(rawId: string): string {
  const normalized = rawId.trim().toLowerCase()
  return ALIAS_TO_CANONICAL[normalized] ?? normalized
}

export function isPrivacyLegalDocument(doc: Pick<LoginAgreementDocument, 'id' | 'title'>): boolean {
  const canonical = canonicalLegalDocumentId(doc.id || '')
  if (canonical === 'privacy-policy') return true
  const title = (doc.title || '').toLowerCase()
  return title.includes('privacy') || doc.title.includes('隐私')
}

export function isTermsLegalDocument(doc: Pick<LoginAgreementDocument, 'id' | 'title'>): boolean {
  const canonical = canonicalLegalDocumentId(doc.id || '')
  if (canonical === 'terms') return true
  const title = (doc.title || '').toLowerCase()
  return title.includes('terms') || doc.title.includes('服务条款')
}

export function findConfiguredLegalDocument(
  documents: LoginAgreementDocument[],
  documentId: string
): LoginAgreementDocument | undefined {
  const normalized = documentId.trim().toLowerCase()
  if (!normalized) return undefined
  const exact = documents.find((doc) => (doc.id || '').trim().toLowerCase() === normalized)
  if (exact) return exact
  const canonical = canonicalLegalDocumentId(normalized)
  return documents.find((doc) => canonicalLegalDocumentId(doc.id || '') === canonical)
}

export function resolveAgreementState(settings: {
  login_agreement_enabled?: boolean
  login_agreement_mode?: string
  login_agreement_updated_at?: string
  login_agreement_revision?: string
  login_agreement_documents?: LoginAgreementDocument[]
}): {
  enabled: boolean
  mode: 'modal' | 'checkbox'
  updatedAt: string
  revision: string
  documents: LoginAgreementDocument[]
} {
  const documents = Array.isArray(settings.login_agreement_documents)
    ? settings.login_agreement_documents.filter((doc) => Boolean(doc?.id?.trim() || doc?.title?.trim()))
    : []
  const enabled = settings.login_agreement_enabled === true
  const mode = settings.login_agreement_mode === 'checkbox' ? 'checkbox' : 'modal'
  const updatedAt = settings.login_agreement_updated_at || ''
  const revision =
    settings.login_agreement_revision?.trim() ||
    `${updatedAt}:${documents.map((doc) => `${doc.id}:${doc.title}`).join('|')}`

  return {
    enabled,
    mode,
    updatedAt,
    revision,
    documents,
  }
}

export function buildSignupAgreementPayload(
  enabled: boolean,
  accepted: boolean,
  revision: string
): Partial<SignupAgreementPayload> {
  if (!enabled) {
    return {}
  }
  return {
    login_agreement_accepted: accepted,
    login_agreement_revision: revision,
  }
}

export function legalDocumentRouteParam(doc: Pick<LoginAgreementDocument, 'id' | 'title'>): string {
  if (isPrivacyLegalDocument(doc) && (!doc.id || doc.id === 'privacy-policy' || doc.id === 'privacy')) {
    return doc.id || 'privacy'
  }
  return doc.id || doc.title
}

export function getLegalSlotTemplateKeys(documentId: string): {
  titleKey: string
  templateKey: string
} | null {
  switch (canonicalLegalDocumentId(documentId)) {
    case 'terms':
      return {
        titleKey: 'publicPortal.legal.slotTitles.terms',
        templateKey: 'publicPortal.legal.templates.terms',
      }
    case 'privacy-policy':
      return {
        titleKey: 'publicPortal.legal.slotTitles.privacy',
        templateKey: 'publicPortal.legal.templates.privacy',
      }
    case 'usage-policy':
      return {
        titleKey: 'publicPortal.legal.slotTitles.usagePolicy',
        templateKey: 'publicPortal.legal.templates.usagePolicy',
      }
    case 'refund-policy':
      return {
        titleKey: 'publicPortal.legal.slotTitles.refundPolicy',
        templateKey: 'publicPortal.legal.templates.refundPolicy',
      }
    case 'supported-regions':
      return {
        titleKey: 'publicPortal.legal.slotTitles.supportedRegions',
        templateKey: 'publicPortal.legal.templates.supportedRegions',
      }
    case 'service-specific-terms':
      return {
        titleKey: 'publicPortal.legal.slotTitles.serviceSpecificTerms',
        templateKey: 'publicPortal.legal.templates.serviceSpecificTerms',
      }
    default:
      return null
  }
}
