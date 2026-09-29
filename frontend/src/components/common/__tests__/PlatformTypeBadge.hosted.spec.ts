import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import PlatformIcon from '../PlatformIcon.vue'
import PlatformTypeBadge from '../PlatformTypeBadge.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('hosted identity display', () => {
  it('keeps the same product and hosting badge after Bedrock migration', () => {
    const legacy = mount(PlatformTypeBadge, { props: { platform: 'bedrock', type: 'bedrock' } })
    const migrated = mount(PlatformTypeBadge, { props: { platform: 'anthropic', type: 'bedrock' } })
    expect(legacy.text()).toBe(migrated.text())
    expect(migrated.text()).toContain('Anthropic')
    expect(migrated.text()).toContain('AWS')
    expect(legacy.findComponent(PlatformIcon).html()).toBe(migrated.findComponent(PlatformIcon).html())
  })
  it('keeps Vertex authentication visible on both supported products', () => {
    for (const platform of ['anthropic', 'gemini'] as const) {
      const wrapper = mount(PlatformTypeBadge, { props: { platform, type: 'service_account' } })
      expect(wrapper.text()).toContain('Vertex')
    }
  })
})
