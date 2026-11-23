import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import ModelPriceCell from '../ModelPriceCell.vue'
import { toCatalogModelVM } from '@/features/catalog/viewModel'

async function mountCell(summary: Record<string, unknown>, isZh = true) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }]
  })
  await router.push('/')
  const vm = toCatalogModelVM(summary as any, null, { isZh })
  return mount(ModelPriceCell, {
    props: {
      billing: vm.billing,
      kind: 'input',
      isZh,
      detailTo: '/model-plaza/m_1'
    },
    global: { plugins: [router] }
  })
}

describe('ModelPriceCell', () => {
  it('renders sale price primary and isolates pi reference price in secondary muted line', async () => {
    const wrapper = await mountCell({
      id: 'm_1',
      model: 'claude-sonnet-4-5',
      billing: {
        state: 'priced',
        sale_price: {
          input_rate: { currency: 'USD', amount: '3', per: '1000000', unit: 'input_token' }
        },
        reference_price: {
          source: 'pi-ai',
          input_rate: { currency: 'USD', amount: '5', per: '1000000', unit: 'input_token' }
        }
      }
    })

    expect(wrapper.get('[data-testid="price-cell-primary"]').text()).toBe('$3.00')
    const secondary = wrapper.get('[data-testid="reference-price-secondary-input"]').text()
    expect(secondary).toContain('参考价 $5.00')
    expect(secondary).toContain('社区目录（pi）')
    expect(secondary).toContain('非网关结算价')
    expect(secondary).not.toContain('pi-ai')
  })

  it('never places reference_price into primary slot when unpriced', async () => {
    const wrapper = await mountCell({
      id: 'm_unpriced',
      model: 'gpt-image-1',
      billing: {
        state: 'unpriced',
        reference_price: {
          source: 'pi-ai',
          input_rate: { currency: 'USD', amount: '5', per: '1000000', unit: 'input_token' }
        }
      }
    })

    expect(wrapper.get('[data-testid="price-cell-primary"]').text()).toBe('未定价')
    expect(wrapper.get('[data-testid="reference-price-secondary-input"]').text()).toContain('参考价 $5.00')
  })

  it('renders group-varying badge and retryable error badge distinctly', async () => {
    const variesWrapper = await mountCell({
      id: 'm_varies',
      model: 'deepseek-v4',
      billing: {
        state: 'varies',
        sale_price: {
          min_input_rate: { currency: 'USD', amount: '0.5', per: '1000000', unit: 'input_token' },
          max_input_rate: { currency: 'USD', amount: '1.2', per: '1000000', unit: 'input_token' }
        }
      }
    })
    expect(variesWrapper.get('[data-testid="price-cell-varies"]').text()).toContain('按分组定价')
    expect(variesWrapper.get('[data-testid="price-cell-varies"]').text()).toContain('$0.50–$1.20')

    const errorWrapper = await mountCell({
      id: 'm_err',
      model: 'gpt-5',
      billing: { state: 'error', reason: 'price_read_failed' }
    })
    expect(errorWrapper.get('[data-testid="price-cell-error"]').text()).toBe('价格加载失败')
    await errorWrapper.get('[data-testid="price-cell-retry"]').trigger('click')
    expect(errorWrapper.emitted('retry')).toHaveLength(1)
  })
})
