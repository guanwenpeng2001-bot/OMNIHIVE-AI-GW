import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, join } from 'node:path'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'
import { COMPATIBLE_PROVIDERS, compatibleProvider } from '@/constants/compatibleProviders'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '../PlatformIcon.vue'

const require = createRequire(import.meta.url)
const iconPackage = dirname(require.resolve('@lobehub/icons/package.json'))
const monoAssets = {
  qianfan: 'BaiduCloud', hunyuan: 'Hunyuan', mistral: 'Mistral',
  bedrock: 'Bedrock', volcengine: 'Volcengine', cursor: 'Cursor', qianwen: 'Qwen',
} as const

describe('PlatformIcon', () => {
  it.each(CONCRETE_PLATFORM_OPTIONS)('renders $value distinctly from the generic fallback', ({ value }) => {
    const wrapper = mount(PlatformIcon, { props: { platform: value } })
    const fallback = mount(PlatformIcon)
    expect(wrapper.html()).not.toBe(fallback.html())
    expect(wrapper.get('svg').attributes('viewBox')).toBe(value === 'anthropic' ? '0 0 16 16' : '0 0 24 24')
    expect(wrapper.get('svg').attributes('fill')).toBe(compatibleProvider(value) ? 'none' : 'currentColor')
    expect(wrapper.get('svg').attributes('aria-hidden')).toBe('true')
    expect(wrapper.get('svg').attributes('focusable')).toBe('false')
    expect(wrapper.get('svg').attributes('tabindex')).toBeUndefined()
    expect(wrapper.find('title').exists()).toBe(false)
    if (value === 'commandcode') {
      expect(wrapper.get('text').text()).toBe('CC')
      expect(wrapper.find('path').exists()).toBe(false)
      expect(wrapper.find('rect').exists()).toBe(false)
    } else {
      expect(wrapper.findAll('path').length).toBeGreaterThan(0)
      expect(wrapper.find('text').exists()).toBe(false)
    }
  })

  it.each(COMPATIBLE_PROVIDERS)('uses the shared decorative server mark for $id', ({ id }) => {
    const wrapper = mount(PlatformIcon, { props: { platform: id } })
    const server = mount(Icon, { props: { name: 'server' } })
    expect(wrapper.get('path').attributes('d')).toBe(server.get('path').attributes('d'))
    expect(wrapper.get('svg').attributes('stroke')).toBe('currentColor')
    for (const size of ['xs', 'sm', 'md', 'lg'] as const) {
      const known = mount(PlatformIcon, { props: { platform: id, size } })
      const brand = mount(PlatformIcon, { props: { platform: 'anthropic', size } })
      expect(known.classes()).toEqual(brand.classes())
    }
  })

  it('uses the shared mark when the registry grows and the globe for unregistered values', () => {
    const providers = COMPATIBLE_PROVIDERS as Array<(typeof COMPATIBLE_PROVIDERS)[number]>
    const added = { ...providers[0]!, id: 'test-new-compatible-provider' }
    const fallback = mount(PlatformIcon).html()
    expect(mount(PlatformIcon, { props: { platform: added.id } }).html()).toBe(fallback)
    providers.push(added)
    try {
      const wrapper = mount(PlatformIcon, { props: { platform: added.id } })
      expect(wrapper.html()).toBe(mount(PlatformIcon, { props: { platform: providers[0]!.id } }).html())
      expect(wrapper.html()).not.toBe(fallback)
    } finally {
      providers.pop()
    }
    for (const platform of ['', 'compatible', '__proto__', 'unknown-provider']) {
      expect(mount(PlatformIcon, { props: { platform } }).html()).toBe(fallback)
    }
  })

  // Compare against the installed source, not a second copy of SVG snapshots.
  it.each(Object.entries(monoAssets))('preserves %s Mono geometry and fill rules', (platform, asset) => {
    const source = readFileSync(join(iconPackage, 'es', asset, 'components/Mono.js'), 'utf8')
    const wrapper = mount(PlatformIcon, { props: { platform: platform as keyof typeof monoAssets } })
    expect(wrapper.get('svg').attributes('viewBox')).toBe(source.match(/viewBox: "([^"]+)"/)?.[1])
    expect(wrapper.get('svg').attributes('fill-rule')).toBe(source.match(/fillRule: "([^"]+)"/)?.[1])
    const paths = [...source.matchAll(/_jsx\("path", \{([\s\S]*?)\}\)/g)]
    expect(wrapper.findAll('path')).toHaveLength(paths.length)
    paths.forEach((path, index) => {
      const rendered = wrapper.findAll('path')[index]!
      expect(rendered.attributes('d')).toBe(path[1]!.match(/d: "([^"]+)"/)?.[1])
      expect(rendered.attributes('clip-rule')).toBe(path[1]!.match(/clipRule: "([^"]+)"/)?.[1])
    })
    expect(wrapper.get('svg').attributes('stroke')).toBeUndefined()
    expect(wrapper.find('path').attributes('d')).not.toBe('M4 2l16 10-7 2-3 8z')
  })

  it.each([
    ['xs', 'w-3', 'h-3'], ['sm', 'w-3.5', 'h-3.5'],
    ['md', 'w-4', 'h-4'], ['lg', 'w-5', 'h-5'],
  ] as const)('keeps the %s CSS display size', (size, width, height) => {
    const wrapper = mount(PlatformIcon, { props: { platform: 'anthropic', size } })
    expect(wrapper.get('svg').classes()).toEqual(expect.arrayContaining([width, height, 'flex-shrink-0']))
    expect(wrapper.get('svg').attributes('viewBox')).toBe('0 0 16 16')
  })

  it('keeps the curved Gemini star and existing OpenCode filled O', () => {
    const gemini = mount(PlatformIcon, { props: { platform: 'gemini' } })
    expect(gemini.get('path').attributes('d')).toContain('a14.147 14.147')
    expect(gemini.get('path').attributes('d')).toContain('c-.65.28')
    const openCode = mount(PlatformIcon, { props: { platform: 'opencode_go' } })
    expect(openCode.get('svg').attributes('fill-rule')).toBe('evenodd')
    expect(openCode.get('path').attributes('d')).toBe('M16 6H8v12h8V6zm4 16H4V2h16v20z')
  })

  it('keeps the composite connection diagram and unknown globe fallback', () => {
    const composite = mount(PlatformIcon, { props: { platform: 'composite' } })
    expect(composite.findAll('circle').map(circle => [circle.attributes('cx'), circle.attributes('cy')])).toEqual([
      ['6', '12'], ['18', '6'], ['18', '18'],
    ])
    expect(composite.get('svg').attributes('stroke')).toBe('currentColor')
    expect(composite.get('svg').attributes('fill')).toBe('none')
    expect(composite.get('svg').attributes('aria-hidden')).toBe('true')
    expect(composite.get('svg').attributes('focusable')).toBe('false')
    expect(mount(PlatformIcon).get('path').attributes('d')).toContain('M12 2C6.48 2')
  })
})
