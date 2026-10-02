import { baseCompile } from '@intlify/message-compiler'
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import UserDashboardStats from '../UserDashboardStats.vue'
import en from '@/i18n/locales/en/dashboard'
import zh from '@/i18n/locales/zh/dashboard'
import type { UserDashboardStats as Stats } from '@/api/usage'

// Real dictionaries and interpolation: readable scope and report-zone dates.
describe.each(['en', 'zh'])('localized summary in %s', locale => {
  it.each(['retained', 'range', 'unknown'])('labels %s without showing internal metadata', scope => {
    const stats: Stats = { total_api_keys: 1, active_api_keys: 1,
      total_requests: 7, total_tokens: 0, average_duration_ms: 0,
      total_input_tokens: 0, total_output_tokens: 0, total_cache_creation_tokens: 0, total_cache_read_tokens: 0,
      total_cost: 0, total_actual_cost: 0, today_requests: 0, today_tokens: 0, today_input_tokens: 0,
      today_output_tokens: 0, today_cache_creation_tokens: 0, today_cache_read_tokens: 0,
      today_cost: 0, today_actual_cost: 0, rpm: 0, tpm: 0, coverage: {
      complete: false, retained_complete: scope === 'retained', range_complete: scope === 'range',
      window_from: '2026-09-11T16:00:00Z', window_until: '2026-10-02T05:00:00Z', timezone: 'UTC',
      reason: 'history_before_coverage_floor_unverified', raw_retained_from: '2026-09-11T16:00:00Z', published_before: '', projection_version: 2
    } }
    const wrapper = mount(UserDashboardStats, { props: { stats, timeZone: 'Asia/Shanghai', balance: 0, isSimple: true }, global: {
      stubs: { Icon: true }, plugins: [createI18n({ legacy: false, locale, missingWarn: false, fallbackWarn: false,
        messageCompiler: message => new Function('return ' + baseCompile(message as string).code)(), messages: { en, zh } })]
    } })
    const notice = wrapper.get('[data-testid="summary-scope"]').text()
    const words = locale === 'zh' ? zh.dashboard : en.dashboard
    expect(notice).toContain(scope === 'retained' ? words.scopeRetained : scope === 'range' ? words.scopeRange : words.scopeUnknown)
    expect(wrapper.text()).not.toContain('history_before_coverage_floor_unverified')
    if (scope !== 'unknown') {
      expect(notice).toContain('2026-09-12 00:00')
      expect(notice).toContain('2026-10-02 13:00')
      expect(wrapper.text()).toContain('0ms')
    } else expect(notice).not.toContain('2026-09-12')
    expect(notice).not.toContain('T16:00:00Z')
    wrapper.unmount()
  })
})
