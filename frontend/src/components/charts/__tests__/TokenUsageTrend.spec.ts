import { withTrendGaps } from '@/views/trendGaps'
import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import TokenUsageTrend from '../TokenUsageTrend.vue'

const messages: Record<string, string> = {
  'admin.dashboard.tokenUsageTrend': 'Token Usage Trend',
  'admin.dashboard.noDataAvailable': 'No data available',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: { timezone: string }) => params ? `${key}: ${params.timezone}` : messages[key] ?? key,
    }),
  }
})

vi.mock('vue-chartjs', () => ({
  Line: {
    props: ['data', 'options'],
    template: '<div class="chart-data">{{ JSON.stringify(data) }}</div>',
  },
}))

describe('TokenUsageTrend', () => {
  it('calculates cache hit rate against all prompt tokens', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        trendData: [
          {
            date: '2026-05-08',
            requests: 1,
            input_tokens: 500,
            output_tokens: 100,
            cache_creation_tokens: 0,
            cache_read_tokens: 1500,
            cost: 0.01,
            actual_cost: 0.005,
          },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    const hitRateDataset = chartData.datasets.find(
      (ds: any) => ds.label === 'Cache Hit Rate'
    )
    // Hit rate = 1500 / (500 + 1500 + 0) * 100 = 75%
    expect(hitRateDataset.data[0]).toBe(75)
  })

  it('returns 0 hit rate when all prompt tokens are zero', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        trendData: [
          {
            date: '2026-05-08',
            requests: 0,
            input_tokens: 0,
            output_tokens: 0,
            cache_creation_tokens: 0,
            cache_read_tokens: 0,
            cost: 0,
            actual_cost: 0,
          },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    const hitRateDataset = chartData.datasets.find(
      (ds: any) => ds.label === 'Cache Hit Rate'
    )
    expect(hitRateDataset.data[0]).toBe(0)
  })

  it('includes cache_creation_tokens in denominator for Anthropic models', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        trendData: [
          {
            date: '2026-05-08',
            requests: 1,
            input_tokens: 200,
            output_tokens: 50,
            cache_creation_tokens: 300,
            cache_read_tokens: 500,
            cost: 0.02,
            actual_cost: 0.01,
          },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    const hitRateDataset = chartData.datasets.find(
      (ds: any) => ds.label === 'Cache Hit Rate'
    )
    // Hit rate = 500 / (200 + 500 + 300) * 100 = 50%
    expect(hitRateDataset.data[0]).toBe(50)
  })
})

it('preserves query-zone bucket labels and keeps token counts separate from exact money', () => {
  const dates = ['2026-11-01 01:00', '2026-11-01 02:00']
  const wrapper = mount(TokenUsageTrend, {
    props: { timeZone: 'America/New_York', trendData: dates.map(date => ({ date, requests: 1,
      input_tokens: 23, output_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0,
      actual_cost: -1e-8, cost: 1e-8 })) },
  })
  const data = JSON.parse(wrapper.get('.chart-data').text())
  expect(data.labels).toEqual(dates)
  expect(data.datasets[0].data).toEqual([23, 23])
  expect(wrapper.get('h3').text()).toContain('America/New_York')
  const options = wrapper.findComponent('.chart-data').props('options')
  expect(options.plugins.tooltip.callbacks.footer([{ dataIndex: 0 }])).toBe('dashboard.actual: USD -0.00000001 | dashboard.standard: USD 0.00000001')
  wrapper.unmount()
})

it('keeps every chart dataset broken at an unknown bucket', () => {
  const point = (date: string) => ({ date, requests: 1, input_tokens: 10, output_tokens: 10,
    cache_creation_tokens: 10, cache_read_tokens: 10, cost: 1, actual_cost: 1 })
  const wrapper = mount(TokenUsageTrend, { props: { trendData: withTrendGaps([
    point('2026-03-01'), point('2026-03-03')], 'day') } })
  const data = wrapper.findComponent('.chart-data').props('data')
  expect(data.datasets).toHaveLength(5)
  for (const dataset of data.datasets) {
    expect(Number.isNaN(dataset.data[1])).toBe(true)
    expect(Number.isFinite(dataset.data[0])).toBe(true)
    expect(Number.isFinite(dataset.data[2])).toBe(true)
  }
  const rows = wrapper.get('[data-testid="trend-data-table"]').findAll('tbody tr')
  expect(rows).toHaveLength(3)
  expect(rows[1].findAll('td').map(cell => cell.text())).toEqual(Array(7).fill('-'))
  expect(rows[0].findAll('td').slice(0, 4).map(cell => cell.text())).toEqual(['10', '10', '10', '10'])
  wrapper.unmount()
})

it.each(['Asia/Shanghai', 'UTC', 'America/Los_Angeles'])('projects the identical chart labels and values in %s without fetching', async timeZone => {
  const point = { date: '2026-11-01 01:00', requests: 1, input_tokens: 2, output_tokens: 3,
    cache_creation_tokens: 1, cache_read_tokens: 1, cost: 1e-8, actual_cost: -1e-8 }
  const wrapper = mount(TokenUsageTrend, { props: { timeZone, trendData: [point] } })
  const table = wrapper.get('[data-testid="trend-data-table"]')
  expect(table.text()).toContain(timeZone)
  expect(table.findAll('tbody th').map(cell => cell.text())).toEqual([point.date])
  expect(table.findAll('tbody td').map(cell => cell.text())).toEqual(['2', '3', '1', '1', '25.0%', '-0.00000001', '0.00000001'])
  await wrapper.setProps({ trendData: [{ ...point, input_tokens: 6 }] })
  expect(table.findAll('tbody td')[0].text()).toBe('6')
  expect(wrapper.findComponent('.chart-data').props('data').datasets[0].data).toEqual([6])
  wrapper.unmount()
})
