import { mount } from '@vue/test-utils'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it, vi } from 'vitest'
import { GROUP_PLATFORM_OPTIONS } from '@/constants/platforms'
import { platformSolidBadgeClass } from '@/utils/platformColors'
import PlatformTypeBadge from '../PlatformTypeBadge.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const platformIds = GROUP_PLATFORM_OPTIONS.map(option => option.value)

/** 第一行是「平台格 + 类型格」，两格都必须取权威表里的色，不允许组件自己维护。 */
function chipClasses(wrapper: ReturnType<typeof mount>) {
  const firstRow = wrapper.element.firstElementChild as HTMLElement
  const [platformChip, typeChip] = Array.from(firstRow.children) as HTMLElement[]
  return {
    platformChip: platformChip.className.split(/\s+/),
    typeChip: typeChip.className.split(/\s+/),
  }
}

describe('PlatformTypeBadge platform colors', () => {
  it.each(platformIds)('renders the shared solid badge class on both chips for %s', platform => {
    const wrapper = mount(PlatformTypeBadge, { props: { platform, type: 'apikey' } })
    const expected = platformSolidBadgeClass(platform).split(' ')
    const { platformChip, typeChip } = chipClasses(wrapper)

    expect(platformChip, platform).toEqual(expect.arrayContaining(expected))
    expect(typeChip, platform).toEqual(expect.arrayContaining(expected))
    wrapper.unmount()
  })

  it('keeps the neutral fallback off the platform chip for an unknown platform', () => {
    const wrapper = mount(PlatformTypeBadge, { props: { platform: 'a-future-upstream', type: 'apikey' } })
    const { platformChip } = chipClasses(wrapper)

    expect(platformChip).toEqual(expect.arrayContaining(platformSolidBadgeClass('a-future-upstream').split(' ')))
    for (const platform of platformIds) {
      expect(platformChip.join(' '), platform).not.toContain(platformSolidBadgeClass(platform))
    }
    wrapper.unmount()
  })

  it('no longer owns a private platform color table', () => {
    const source = readFileSync(resolve(process.cwd(), 'src/components/common/PlatformTypeBadge.vue'), 'utf8')
    // 蓝色是 gemini 的正式色系，只能来自 utils/platformColors.ts；本组件里出现即是手写表回归。
    expect(source).not.toContain('bg-blue-100')
    expect(source).toContain('platformSolidBadgeClass(props.platform)')
  })
})
