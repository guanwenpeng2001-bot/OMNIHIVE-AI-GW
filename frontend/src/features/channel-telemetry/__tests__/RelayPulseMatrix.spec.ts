const i18nT = (key: string, params?: Record<string, unknown>) => {
  const map: Record<string, string> = {
    'channelTelemetry.matrix.title': '色块矩阵',
    'channelTelemetry.matrix.description': '趋势色块视图',
    'channelTelemetry.bucket.minutes': '{count}分钟',
    'channelTelemetry.matrix.wheelZoomX': '滚轮缩放横向',
    'channelTelemetry.matrix.resetZoom': '重置缩放',
    'channelTelemetry.matrix.dimension': '维度',
    'channelTelemetry.metrics.successRate': '成功率',
    'channelTelemetry.metrics.ttft': '首 Token',
    'channelTelemetry.metrics.tps': '每秒 Token',
    'channelTelemetry.metrics.cacheRate': '缓存率',
    'channelTelemetry.matrix.scoreLine': '评分 {score}',
    'channelTelemetry.metrics.successRateValue': '成功率 {value}',
    'channelTelemetry.metrics.errorRateValue': '错误率 {value}',
    'channelTelemetry.metrics.rpmValue': 'RPM {value}',
    'channelTelemetry.metrics.tpmValue': 'TPM {value}',
    'channelTelemetry.metrics.tpsValue': '每秒 Token {value}',
    'channelTelemetry.metrics.ttftValue': '首 Token {value}',
    'channelTelemetry.metrics.durationValue': '时长 {value}',
    'channelTelemetry.metrics.cacheRateValue': '缓存率 {value}',
    'channelTelemetry.matrix.noTrafficAt': '{time} 无流量',
    'channelTelemetry.matrix.noTraffic': '无流量',
    'channelTelemetry.matrix.legendAria': '图例',
    'channelTelemetry.matrix.bad': '差',
    'channelTelemetry.matrix.good': '好',
    'channelTelemetry.matrix.healthyLegend': '健康',
    'channelTelemetry.matrix.warningLegend': '警告',
    'channelTelemetry.matrix.criticalLegend': '异常',
    'channelTelemetry.matrix.unknownLegend': '未知',
  }
  const template = map[key] || key
  return template.replace(/\{(\w+)\}/g, (_, name) => String(params?.[name] ?? ''))
}

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: i18nT,
      te: (key: string) => key.startsWith('channelTelemetry.'),
      locale: { value: 'zh' },
    }),
  }
})

import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import RelayPulseMatrix from '../RelayPulseMatrix.vue'
import type { MonitorHealth, MonitorMetric } from '@/api/channelTelemetry'

const health: MonitorHealth = {
  overall: 'warning',
  error_rate: 'warning',
  ttft: 'healthy',
  cache: 'warning',
  score: 52,
  error_rate_score: 40,
  ttft_score: 100,
  cache_score: 50,
  minimum_sample: 20,
}

function metrics(requestCount: number): MonitorMetric {
  return {
    success_requests: requestCount ? requestCount - 1 : 0,
    error_requests: requestCount ? 1 : 0,
    request_count: requestCount,
    token_count: 100,
    rpm: 1.5,
    tpm: 10.2,
    error_rate: requestCount ? 1 / requestCount : 0,
    cache_rate: 0.5,
    cache_rate_numerator: 50,
    cache_rate_denominator: 100,
    ttft: { sample_count: requestCount, p50_ms: 100, p95_ms: 300, avg_ms: 150 },
    duration: { sample_count: requestCount, p50_ms: 500, p95_ms: 900, avg_ms: 600 },
    upstream_affected_requests: 2,
    upstream_attempt_count: 3,
  }
}

describe('RelayPulseMatrix', () => {
  it('shows privacy-safe hover tooltips and multi-band colors without click modal', async () => {
    const wrapper = mount(RelayPulseMatrix, {
      props: {
        rows: [{
          platform: 'openai',
          group_id: 7,
          group_name: '默认组',
          model: 'gpt-5',
          metrics: metrics(10),
          health,
          buckets: [
            { bucket_start: '2026-08-01T00:00:00Z', metrics: metrics(10), health },
            { bucket_start: '2026-08-01T00:01:00Z', metrics: metrics(0), health },
          ],
        }],
        coverage: {
          requested_start: '2026-08-01T00:00:00Z',
          requested_end: '2026-08-01T00:03:00Z',
          coverage_start: '2026-08-01T00:00:00Z',
          data_through: '2026-08-01T00:03:00Z',
          computed_at: '2026-08-01T00:03:00Z',
          aggregation_lag_seconds: 0,
          coverage_complete: true,
          bucket_seconds: 60,
        },
        healthMode: 'overall',
      },
    })

    const cells = wrapper.findAll('.pulse-cell')
    expect(cells).toHaveLength(3)
    // Hover tooltip content (privacy-safe: no absolute request/error counts)
    const tip = cells[0].text()
    expect(tip).toContain('成功率')
    expect(tip).toContain('90.0%')
    expect(tip).toContain('首 Token')
    expect(tip).toContain('每秒 Token')
    expect(tip).toContain('缓存率')
    expect(tip).toContain('RPM')
    expect(tip).not.toContain('请求数')
    expect(tip).not.toContain('用户错误')
    expect(tip).not.toContain('上游受影响')
    // Summary columns: success · ttft · tokens/s · cache
    const header = wrapper.find('.matrix-header').text()
    expect(header).toContain('成功率')
    expect(header).toContain('首 Token')
    expect(header).toContain('每秒 Token')
    expect(header).toContain('缓存率')
    // Multi-band class from score 52 → score5
    expect(cells[0].classes().some((c) => c.startsWith('health-score'))).toBe(true)
    // Redacted user payloads may have request_count=0 but still include score.
    expect(cells[1].classes().some((c) => c.startsWith('health-score'))).toBe(true)
    expect(cells[2].classes()).toContain('health-unknown')

    // No click-to-open modal
    await cells[0].trigger('click')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })
})

describe('RelayPulseMatrix axis range', () => {
  it('uses selected requested range for the X axis even with partial coverage', () => {
    const wrapper = mount(RelayPulseMatrix, {
      props: {
        rows: [{
          platform: 'openai',
          group_id: 7,
          group_name: '默认组',
          model: 'gpt-5',
          metrics: metrics(10),
          health,
          buckets: [
            { bucket_start: '2026-08-01T00:02:00Z', metrics: metrics(10), health },
          ],
        }],
        coverage: {
          requested_start: '2026-08-01T00:00:00Z',
          requested_end: '2026-08-01T00:05:00Z',
          coverage_start: '2026-08-01T00:02:00Z',
          data_through: '2026-08-01T00:03:00Z',
          computed_at: '2026-08-01T00:03:00Z',
          aggregation_lag_seconds: 0,
          coverage_complete: false,
          bucket_seconds: 60,
        },
        healthMode: 'overall',
      },
    })
    // 5 minutes @ 60s buckets → 5 cells spanning the selected range
    expect(wrapper.findAll('.pulse-cell')).toHaveLength(5)
  })
})
