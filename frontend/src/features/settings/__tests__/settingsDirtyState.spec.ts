import { describe, expect, it } from 'vitest'
import {
  SETTINGS_SECTIONS,
  buildSectionPatch,
  getDirtySections,
  isSectionDirty,
  resetSectionDraft,
  validateSettingsSection,
  type SettingsDraft
} from '../sections'

const BASELINE: SettingsDraft = {
  site_name: 'OmniHive',
  site_subtitle: 'Gateway',
  frontend_url: 'https://omnihive.example.com',
  registration_enabled: true,
  invitation_code_enabled: false,
  totp_enabled: true,
  audit_log_retention_days: 90,
  default_balance: 5,
  default_concurrency: 10,
  affiliate_rebate_rate: 0.1,
  smtp_host: 'smtp.example.com',
  smtp_port: 587,
  smtp_password: ''
}

describe('settings dirty-state & validation', () => {
  it('tracks dirty state per section without cross-section bleed', () => {
    const draft: SettingsDraft = {
      ...BASELINE,
      site_name: 'OmniHive Prod',
      default_concurrency: 20
    }

    expect(isSectionDirty('general', BASELINE, draft)).toBe(true)
    expect(isSectionDirty('users', BASELINE, draft)).toBe(true)
    expect(isSectionDirty('security', BASELINE, draft)).toBe(false)
    expect(getDirtySections(BASELINE, draft)).toEqual(['general', 'users'])
  })

  it('resets a single section without clobbering other dirty sections', () => {
    const draft: SettingsDraft = {
      ...BASELINE,
      site_name: 'OmniHive Prod',
      default_concurrency: 20
    }

    const afterReset = resetSectionDraft('general', BASELINE, draft)
    expect(afterReset.site_name).toBe('OmniHive')
    expect(afterReset.default_concurrency).toBe(20)
    expect(getDirtySections(BASELINE, afterReset)).toEqual(['users'])
  })

  it('extracts minimal section patch and omits empty secret fields', () => {
    const draft: SettingsDraft = {
      ...BASELINE,
      smtp_port: 465,
      smtp_password: ''
    }
    expect(buildSectionPatch('email', BASELINE, draft)).toEqual({
      smtp_port: 465
    })

    const withSecret: SettingsDraft = {
      ...draft,
      smtp_password: 'new-secret-value'
    }
    expect(buildSectionPatch('email', BASELINE, withSecret)).toEqual({
      smtp_port: 465,
      smtp_password: 'new-secret-value'
    })
  })

  it('validates section fields with clear error messages', () => {
    expect(validateSettingsSection('general', { frontend_url: 'not-a-url' })).toHaveProperty('frontend_url')
    expect(validateSettingsSection('security', { audit_log_retention_days: -1 })).toHaveProperty(
      'audit_log_retention_days'
    )
    expect(validateSettingsSection('users', { default_balance: -1, default_concurrency: 0, affiliate_rebate_rate: 101 })).toEqual({
      default_balance: 'adminPlatform.settings.default_balance',
      default_concurrency: 'adminPlatform.settings.default_concurrency',
      affiliate_rebate_rate: 'adminPlatform.settings.affiliate_rebate_rate'
    })
    expect(validateSettingsSection('email', { smtp_host: 'smtp.example.com', smtp_port: 70000 })).toHaveProperty(
      'smtp_port'
    )
  })

  it('registers diagnostics as a read-only section without affecting dirty tracking', () => {
    expect(SETTINGS_SECTIONS.some((section) => section.id === 'diagnostics')).toBe(true)
    expect(isSectionDirty('diagnostics', BASELINE, { ...BASELINE, site_name: 'Changed' })).toBe(false)
    expect(buildSectionPatch('diagnostics', BASELINE, { ...BASELINE, site_name: 'Changed' })).toEqual({})
    expect(validateSettingsSection('diagnostics', BASELINE)).toEqual({})
  })
})
