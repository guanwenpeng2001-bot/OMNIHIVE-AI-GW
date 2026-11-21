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

export interface SettingsSectionMeta {
  id: SettingsSectionId
  label: string
  labelZh: string
  description: string
  descriptionZh: string
  fields: string[]
  secretFields?: string[]
}

export const SETTINGS_SECTIONS: SettingsSectionMeta[] = [
  {
    id: 'general',
    label: 'General & Brand',
    labelZh: '基础与品牌',
    description: 'Site identity, frontend URL, and public registration toggles.',
    descriptionZh: '站点名称、前端访问地址以及公开注册开关配置。',
    fields: ['site_name', 'site_subtitle', 'frontend_url', 'registration_enabled', 'invitation_code_enabled']
  },
  {
    id: 'security',
    label: 'Security & Auth',
    labelZh: '安全与认证',
    description: 'TOTP 2FA, Passkeys, session binding, step-up verification, and Turnstile.',
    descriptionZh: 'TOTP 两步验证、Passkey、会话绑定、敏感操作二次校验及人机验证。',
    fields: [
      'totp_enabled',
      'passkey_enabled',
      'passkey_rp_id',
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
    label: 'User Defaults',
    labelZh: '用户默认配额',
    description: 'Default balance, concurrency, RPM limits, and affiliate rebates.',
    descriptionZh: '新用户默认余额、并发与速率上限，以及推广返利策略。',
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
    label: 'Gateway & Docs',
    labelZh: '网关与文档',
    description: 'API base URL, contact info, and documentation links.',
    descriptionZh: 'API 接入地址、运营商联系方式与文档入口链接。',
    fields: ['api_base_url', 'contact_info', 'doc_url', 'hide_ccs_import_button']
  },
  {
    id: 'payment',
    label: 'Payment & Top-Up',
    labelZh: '支付与充值',
    description: 'Payment feature toggle, promo codes, and checkout policy.',
    descriptionZh: '在线支付能力、优惠码功能及结算策略开关。',
    fields: ['payment_enabled', 'promo_code_enabled']
  },
  {
    id: 'email',
    label: 'Email & SMTP',
    labelZh: '邮件与 SMTP',
    description: 'SMTP relay host, port, TLS, sender address, and verification policy.',
    descriptionZh: 'SMTP 发信服务器、端口、TLS 加密、发件人及邮箱验证策略。',
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
    label: 'Feature Flags',
    labelZh: '功能开关',
    description: 'Domain quota enforcement and runtime feature toggles.',
    descriptionZh: '邮箱域名配额限制与运行时扩展能力开关。',
    fields: ['registration_email_domain_quota_enabled', 'user_custom_key_enabled']
  },
  {
    id: 'agreement',
    label: 'Login Agreement',
    labelZh: '登录协议',
    description: 'Terms and privacy agreement enforcement mode on sign-in.',
    descriptionZh: '登录与注册时的服务条款和隐私协议展示方式。',
    fields: ['login_agreement_enabled', 'login_agreement_mode']
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

export function validateSettingsSection(
  sectionId: SettingsSectionId,
  draft: SettingsDraft
): Record<string, string> {
  const errors: Record<string, string> = {}

  if (sectionId === 'general') {
    const url = String(draft.frontend_url ?? '').trim()
    if (url && !/^https?:\/\/.+/i.test(url)) {
      errors.frontend_url = 'Frontend URL must start with http:// or https://'
    }
  }

  if (sectionId === 'security') {
    const retention = Number(draft.audit_log_retention_days ?? 0)
    if (!Number.isFinite(retention) || retention < 1 || retention > 3650) {
      errors.audit_log_retention_days = 'Retention days must be between 1 and 3650'
    }
  }

  if (sectionId === 'users') {
    const balance = Number(draft.default_balance ?? 0)
    if (!Number.isFinite(balance) || balance < 0) {
      errors.default_balance = 'Default balance cannot be negative'
    }
    const concurrency = Number(draft.default_concurrency ?? 0)
    if (!Number.isFinite(concurrency) || concurrency < 1) {
      errors.default_concurrency = 'Default concurrency must be at least 1'
    }
    const rebateRate = Number(draft.affiliate_rebate_rate ?? 0)
    if (!Number.isFinite(rebateRate) || rebateRate < 0 || rebateRate > 1) {
      errors.affiliate_rebate_rate = 'Rebate rate must be between 0 and 1'
    }
  }

  if (sectionId === 'gateway') {
    const apiBase = String(draft.api_base_url ?? '').trim()
    if (apiBase && !/^https?:\/\/.+/i.test(apiBase)) {
      errors.api_base_url = 'API Base URL must start with http:// or https://'
    }
  }

  if (sectionId === 'email') {
    const port = Number(draft.smtp_port ?? 0)
    if (draft.smtp_host && (!Number.isInteger(port) || port < 1 || port > 65535)) {
      errors.smtp_port = 'SMTP port must be between 1 and 65535'
    }
  }

  return errors
}
