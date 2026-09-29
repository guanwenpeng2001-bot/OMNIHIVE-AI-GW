import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import Pagination from '../Pagination.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

function mountPagination() {
  return mount(Pagination, {
    props: { total: 200, page: 1, pageSize: 20, showJump: true, showPageSizeSelector: false },
    global: { stubs: { Icon: true } }
  })
}

describe('pagination jump input', () => {
  it.each(['click', 'enter'])('jumps to the entered numeric page using %s', async (action) => {
    const wrapper = mountPagination()
    const input = wrapper.get('input[type="number"]')
    await input.setValue('3')
    if (action === 'click') await wrapper.get('.btn').trigger('click')
    else await input.trigger('keyup', { key: 'Enter' })
    expect(wrapper.emitted('update:page')).toEqual([[3]])
    expect((input.element as HTMLInputElement).value).toBe('')
  })

  it('clamps an entered page to the last page', async () => {
    const wrapper = mountPagination()
    await wrapper.get('input').setValue('99')
    await wrapper.get('.btn').trigger('click')
    expect(wrapper.emitted('update:page')).toEqual([[10]])
  })

  it('ignores an empty jump input', async () => {
    const wrapper = mountPagination()
    await wrapper.get('.btn').trigger('click')
    expect(wrapper.emitted('update:page')).toBeUndefined()
  })

  it.each(['0', '-5', '1'])('clamps %s to the first page without re-emitting the current page', async (value) => {
    const wrapper = mountPagination()
    await wrapper.setProps({ page: 3 })
    await wrapper.get('input').setValue(value)
    await wrapper.get('.btn').trigger('click')
    expect(wrapper.emitted('update:page')).toEqual([[1]])
    await wrapper.setProps({ page: 1 })
    await wrapper.get('input').setValue(value)
    await wrapper.get('.btn').trigger('click')
    expect(wrapper.emitted('update:page')).toHaveLength(1)
  })

  it('ignores invalid numeric input and a jump with no records', async () => {
    const wrapper = mountPagination()
    await wrapper.get('input').setValue('invalid')
    await wrapper.get('.btn').trigger('click')
    expect(wrapper.emitted('update:page')).toBeUndefined()
    await wrapper.setProps({ total: 0 })
    await wrapper.get('input').setValue('2')
    await wrapper.get('.btn').trigger('click')
    expect(wrapper.emitted('update:page')).toBeUndefined()
  })
})
