import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { SUPPLIER_FAMILIES, resolveMember, supplierOf, supplierPlatformOptions } from '@/constants/supplierFamilies'
import { COMPATIBLE_PROVIDERS } from '@/constants/compatibleProviders'
import { CONCRETE_PLATFORM_OPTIONS, GROUP_PLATFORM_OPTIONS } from '@/constants/platforms'
import SupplierVariantCards from '../SupplierVariantCards.vue'
import AccountPlatformSelector from '../AccountPlatformSelector.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('supplier facade routing and accessibility', () => {
  it('groups only the 20 approved IDs and leaves group identities intact', () => {
    const members = SUPPLIER_FAMILIES.flatMap(supplier => supplier.members.map(member => member.platform))
    const extras = ['mimo_token_sg', 'mimo_token_eu', 'astron_token'].filter(platform => COMPATIBLE_PROVIDERS.some(provider => provider.id === platform))
    expect(SUPPLIER_FAMILIES).toHaveLength(8 + Number(extras.includes('astron_token')))
    expect(members.sort()).toEqual(['stepfun_api', 'stepfun_api_intl', 'stepfun_plan', 'stepfun_plan_intl', 'mimo', 'mimo_token_cn', 'compshare', 'compshare_coding', 'siliconflow_cn', 'siliconflow_global', 'qwencloud', 'qwencloud_maas_token', 'qwencloud_token', 'dmxapi', 'dmxapi_com', 'dmxapi_ssvip', 'teamorouter', 'teamorouter_com', 'crazyrouter', 'crazyrouter_intl', ...extras, ...(extras.includes('astron_token') ? ['astron_coding'] : [])].sort())
    expect(new Set(members).size).toBe(members.length)
    for (const member of members) {
      expect(COMPATIBLE_PROVIDERS.some(provider => provider.id === member)).toBe(true)
      expect(GROUP_PLATFORM_OPTIONS.some(option => option.value === member)).toBe(true)
    }
    expect(supplierPlatformOptions()).toHaveLength(CONCRETE_PLATFORM_OPTIONS.length + 1 - SUPPLIER_FAMILIES.reduce((collapsed, supplier) => collapsed + supplier.members.length - 1, 0))
    expect(supplierPlatformOptions().filter(option => option.value === 'together')).toHaveLength(1)
    for (const platform of ['qianwen', 'commandcode', 'volcengine', 'byteplus', 'qianfan', 'fluxa', 'hunyuan', 'sudocode_chat', 'sudocode_us', 'aicodemirror', 'bedrock', 'vertex']) expect(supplierOf(platform)).toBeUndefined()
  })

  it('requires every axis and rejects combinations without a routing identity', () => {
    const supplier = supplierOf('stepfun_plan')!
    expect(resolveMember(supplier, {})).toBeUndefined()
    expect(resolveMember(supplier, { region: 'china' })).toBeUndefined()
    expect(resolveMember(supplier, { region: 'global', product: 'stepPlan' })).toBeUndefined()
    for (const family of SUPPLIER_FAMILIES) for (const member of family.members) expect(resolveMember(family, member.selection)?.platform).toBe(member.platform)
  })

  it('finds Step Plan by member label and selects its one supplier chip', async () => {
    const wrapper = mount(AccountPlatformSelector, { props: { modelValue: 'stepfun_plan' }, global: { stubs: { Select: true, PlatformIcon: true } } })
    await wrapper.get('input').setValue('step plan')
    expect(wrapper.findAll('[role="radio"]')).toHaveLength(1)
    expect(wrapper.get('[role="radio"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[role="radio"]').attributes('data-testid')).toBe('stepfun_api-platform')
    wrapper.unmount()
  })

  it('offers one tab stop per axis, arrow selection and a single-column narrow layout', async () => {
    const wrapper = mount(SupplierVariantCards, { attachTo: document.body, props: {
      supplier: supplierOf('stepfun_api')!, modelValue: {},
      'onUpdate:modelValue': value => { void wrapper.setProps({ modelValue: value }) }
    } })
    for (const group of wrapper.findAll('[role="radiogroup"]')) {
      expect(group.classes()).toEqual(expect.arrayContaining(['grid-cols-1', 'sm:grid-cols-2']))
      expect(group.findAll('[tabindex="0"]')).toHaveLength(1)
    }
    const region = wrapper.get('[data-choice="china"]')
    await region.trigger('keydown', { key: 'ArrowRight' })
    expect(wrapper.props('modelValue')).toEqual({ region: 'international' })
    expect(document.activeElement).toBe(wrapper.get('[data-choice="international"]').element)
    await wrapper.get('[data-choice="api"]').trigger('keydown', { key: 'End' })
    expect(wrapper.props('modelValue')).toEqual({ region: 'international', product: 'stepPlan' })
    expect(wrapper.get('[data-testid="supplier-endpoint"]').text()).toContain('stepfun.ai/step_plan')
    await wrapper.setProps({ locked: true })
    expect(wrapper.findAll('button:not([disabled])')).toHaveLength(0)
    expect(wrapper.findAll('[tabindex="0"]')).toHaveLength(0)
    const before = wrapper.emitted('update:modelValue')?.length
    await wrapper.get('[data-choice="api"]').trigger('keydown', { key: 'Home' })
    expect(wrapper.emitted('update:modelValue')?.length).toBe(before)
    wrapper.unmount()
  })
})
