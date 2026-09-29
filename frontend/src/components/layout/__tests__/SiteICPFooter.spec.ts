import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it } from 'vitest'
import { useAppStore } from '@/stores/app'
import type { PublicSettings } from '@/types'
import SiteICPFooter from '../SiteICPFooter.vue'

describe('SiteICPFooter', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('renders the configured number with the official registration link', () => {
    useAppStore().cachedPublicSettings = { site_icp: '蒙ICP备2026xxxx号' } as PublicSettings
    const wrapper = mount(SiteICPFooter)
    const link = wrapper.get('a')
    expect(link.text()).toBe('蒙ICP备2026xxxx号')
    expect(link.attributes()).toMatchObject({ href: 'https://beian.miit.gov.cn', target: '_blank', rel: 'noopener' })
  })

  it.each([undefined, '', '   '])('omits the footer for an empty number: %s', (siteICP) => {
    useAppStore().cachedPublicSettings = { site_icp: siteICP } as PublicSettings
    expect(mount(SiteICPFooter).find('footer').exists()).toBe(false)
  })

  it('updates when settings are changed or cleared', async () => {
    const appStore = useAppStore()
    const wrapper = mount(SiteICPFooter)
    expect(wrapper.find('footer').exists()).toBe(false)
    appStore.cachedPublicSettings = { site_icp: '蒙ICP备2026xxxx号' } as PublicSettings
    await wrapper.vm.$nextTick()
    expect(wrapper.get('a').text()).toBe('蒙ICP备2026xxxx号')
    appStore.cachedPublicSettings = { site_icp: '' } as PublicSettings
    await wrapper.vm.$nextTick()
    expect(wrapper.find('footer').exists()).toBe(false)
  })
})
