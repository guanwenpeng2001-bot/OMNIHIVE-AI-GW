import { it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
import BedrockRegionSelect from '../BedrockRegionSelect.vue'
it('searches known regions and emits a custom region', async () => {
  const wrapper = mount(BedrockRegionSelect, { props: { modelValue: 'us-east-1' }, global: { stubs: { Teleport: true } } })
  await wrapper.get('button').trigger('click')
  const search = wrapper.get('input')
  await search.setValue('ap-custom-9')
  const option = wrapper.findAll('[role="option"]').find(row => row.text().includes('ap-custom-9'))
  expect(option).toBeDefined()
  await option!.trigger('click')
  expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['ap-custom-9'])
  wrapper.unmount()
})
