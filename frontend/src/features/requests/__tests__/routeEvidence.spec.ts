import { baseCompile } from '@intlify/message-compiler'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en/costLedger'
import zh from '@/i18n/locales/zh/costLedger'
import type { Schema } from '../api'
import RouteEvidence from '../components/RouteEvidence.vue'

function makeI18n(locale: 'zh' | 'en') {
  return createI18n({
    legacy: false,
    locale,
    messageCompiler: (message) => new Function('return ' + baseCompile(message as string).code)(),
    messages: {
      en: { costLedger: en },
      zh: { costLedger: zh }
    }
  })
}

describe('RouteEvidence partial / legacy trace completeness', () => {
  it('explains TRACE_NOT_RECORDED for legacy requests even when trace is null', () => {
    const explanation: Schema['RequestsRouteExplanation'] = {
      request_id: 'req-legacy-1',
      trace: null,
      data_completeness: {
        state: 'partial',
        missing: [{ section: 'route', reason: 'TRACE_NOT_RECORDED' }]
      }
    }

    const zhWrapper = mount(RouteEvidence, {
      props: { explanation, financialAccess: true },
      global: { plugins: [makeI18n('zh')] }
    })
    expect(zhWrapper.get('[data-testid="route-completeness-notice"]').text()).toContain(
      'TRACE_NOT_RECORDED'
    )
    expect(zhWrapper.get('[data-testid="route-completeness-notice"]').text()).toContain(
      '早期兼容请求未记录完整选路轨迹'
    )

    const enWrapper = mount(RouteEvidence, {
      props: { explanation, financialAccess: true },
      global: { plugins: [makeI18n('en')] }
    })
    expect(enWrapper.get('[data-testid="route-completeness-notice"]').text()).toContain(
      'Legacy request prior to full route tracing'
    )
  })
})
