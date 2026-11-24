import type { UpdateSettingsRequest } from '@/api/admin/settings'

export type SettingsSectionId =
  | 'general'
  | 'security'
  | 'users'
  | 'gateway'
  | 'payment'
  | 'email'
  | 'features'
  | 'agreement'
  | 'diagnostics'

export interface SettingsSectionMeta {
  id: SettingsSectionId
  fields: string[]
  secretFields?: string[]
}

export const SETTINGS_SECTIONS: SettingsSectionMeta[] = [
  {
    id: 'general',
    fields: ['site_name', 'site_subtitle', 'frontend_url', 'registration_enabled', 'invitation_code_enabled']
  },
  {
    id: 'security',
    fields: [
      'totp_enabled',
      'passkey_enabled',
      'session_binding_enabled',
      'step_up_enabled',
      'audit_log_retention_days',
      'turnstile_enabled',
      'turnstile_site_key',
      'turnstile_secret_key'
    ],
    secretFields: ['turnstile_secret_key']
  },
  {
    id: 'users',
    fields: [
      'default_balance',
      'default_concurrency',
      'default_user_rpm_limit',
      'affiliate_rebate_rate',
      'affiliate_rebate_freeze_hours',
      'affiliate_rebate_duration_days'
    ]
  },
  {
    id: 'gateway',
    fields: ['api_base_url', 'contact_info', 'doc_url', 'hide_ccs_import_button']
  },
  {
    id: 'payment',
    fields: ['payment_enabled', 'promo_code_enabled']
  },
  {
    id: 'email',
    fields: [
      'email_verify_enabled',
      'password_reset_enabled',
      'smtp_host',
      'smtp_port',
      'smtp_username',
      'smtp_password',
      'smtp_from_email',
      'smtp_from_name',
      'smtp_use_tls'
    ],
    secretFields: ['smtp_password']
  },
  {
    id: 'features',
    fields: ['registration_email_domain_quota_enabled', 'user_custom_key_enabled']
  },
  {
    id: 'agreement',
    fields: ['login_agreement_enabled', 'login_agreement_mode']
  },
  {
    id: 'diagnostics',
    fields: []
  }
]

export type SettingsDraft = Record<string, unknown>

export function isFieldEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if ((a === undefined || a === null || a === '') && (b === undefined || b === null || b === '')) {
    return true
  }
  return JSON.stringify(a) === JSON.stringify(b)
}

export function isSectionDirty(
  sectionId: SettingsSectionId,
  baseline: SettingsDraft,
  draft: SettingsDraft
): boolean {
  const meta = SETTINGS_SECTIONS.find((item) => item.id === sectionId)
  if (!meta) return false
  return meta.fields.some((field) => !isFieldEqual(baseline[field], draft[field]))
}

export function getDirtySections(baseline: SettingsDraft, draft: SettingsDraft): SettingsSectionId[] {
  return SETTINGS_SECTIONS.filter((meta) => isSectionDirty(meta.id, baseline, draft)).map((meta) => meta.id)
}

export function resetSectionDraft(
  sectionId: SettingsSectionId,
  baseline: SettingsDraft,
  draft: SettingsDraft
): SettingsDraft {
  const meta = SETTINGS_SECTIONS.find((item) => item.id === sectionId)
  if (!meta) return { ...draft }
  const next = { ...draft }
  for (const field of meta.fields) {
    next[field] = baseline[field]
  }
  return next
}

export function buildSectionPatch(
  sectionId: SettingsSectionId,
  baseline: SettingsDraft,
  draft: SettingsDraft
): UpdateSettingsRequest {
  const meta = SETTINGS_SECTIONS.find((item) => item.id === sectionId)
  if (!meta) return {}
  const patch: Record<string, unknown> = {}
  const secretSet = new Set(meta.secretFields ?? [])

  for (const field of meta.fields) {
    const value = draft[field]
    if (secretSet.has(field)) {
      if (typeof value === 'string' && value.trim() !== '') {
        patch[field] = value
      }
      continue
    }
    if (!isFieldEqual(baseline[field], value)) {
      patch[field] = value
    }
  }
  return patch as UpdateSettingsRequest
}

const numericFields: Record<string, { min: number; max: number; integer: boolean }> = {
  audit_log_retention_days: { min: 0, max: Number.MAX_SAFE_INTEGER, integer: true },
  default_balance: { min: 0, max: Infinity, integer: false },
  default_concurrency: { min: 1, max: Number.MAX_SAFE_INTEGER, integer: true },
  default_user_rpm_limit: { min: 0, max: Number.MAX_SAFE_INTEGER, integer: true },
  affiliate_rebate_rate: { min: 0, max: 100, integer: false },
  affiliate_rebate_freeze_hours: { min: 0, max: 720, integer: true },
  affiliate_rebate_duration_days: { min: 0, max: 3650, integer: true },
  smtp_port: { min: 1, max: 65535, integer: true }
}

export function validateSettingsSection(
  sectionId: SettingsSectionId,
  draft: SettingsDraft,
  baseline?: SettingsDraft
): Record<string, string> {
  const errors: Record<string, string> = {}

  const urlField = sectionId === 'general' ? 'frontend_url' : sectionId === 'gateway' ? 'api_base_url' : null
  if (urlField) {
    const url = String(draft[urlField] ?? '').trim()
    if (url && !/^https?:\/\/.+/i.test(url)) errors[urlField] = 'adminPlatform.settings.httpUrl'
  }

  for (const field of SETTINGS_SECTIONS.find(section => section.id === sectionId)?.fields ?? []) {
    const rule = numericFields[field]
    if (!rule || (baseline && isFieldEqual(draft[field], baseline[field]))) continue
    const value = draft[field]
    // Older deployments can omit a field. An explicitly cleared input must be corrected.
    if (value === undefined) continue
    if (typeof value !== 'number' || !Number.isFinite(value) || value < rule.min ||
        value > rule.max || (rule.integer && !Number.isInteger(value))) {
      errors[field] = 'adminPlatform.settings.' + field
    }
  }
  return errors
}
