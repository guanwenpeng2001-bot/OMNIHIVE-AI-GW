import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, join } from 'node:path'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { NATIVE_PLATFORM_OPTIONS } from '@/constants/platforms'
import { COMPATIBLE_PROVIDERS } from '@/constants/compatibleProviders'
import { compatibleProviderIcon } from '../providerIcons'
import PlatformIcon from '../PlatformIcon.vue'

const require = createRequire(import.meta.url)
const iconPackage = dirname(require.resolve('@lobehub/icons/package.json'))
const monoAssets = {
  qianfan: 'BaiduCloud', hunyuan: 'Hunyuan', mistral: 'Mistral',
  bedrock: 'Bedrock', volcengine: 'Volcengine', cursor: 'Cursor', qianwen: 'Qwen',
} as const

describe('PlatformIcon', () => {
  it.each(NATIVE_PLATFORM_OPTIONS)('renders $value distinctly from the generic fallback', ({ value }) => {
    const wrapper = mount(PlatformIcon, { props: { platform: value } })
    const fallback = mount(PlatformIcon)
    expect(wrapper.html()).not.toBe(fallback.html())
    expect(wrapper.get('svg').attributes('viewBox')).toBe(value === 'anthropic' ? '0 0 16 16' : '0 0 24 24')
    expect(wrapper.get('svg').attributes('fill')).toBe('currentColor')
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

  it.each(COMPATIBLE_PROVIDERS)('renders verified artwork or explicit initials for $id', ({ id, label }) => {
    const icon = compatibleProviderIcon(id, label)
    const wrapper = mount(PlatformIcon, { props: { platform: id } })
    expect(wrapper.get('svg').attributes('aria-hidden')).toBe('true')
    expect(wrapper.find('title').exists()).toBe(false)
    expect(wrapper.find('[tabindex]').exists()).toBe(false)
    if (icon.kind === 'asset') {
      expect(wrapper.get('image').attributes('href')).toBe(icon.src)
      expect(icon.src).not.toMatch(/^https?:/)
      expect(wrapper.get('svg').attributes('focusable')).toBe('false')
      expect(wrapper.get('svg').classes()).toContain(icon.monochrome ? 'dark:invert' : 'bg-white')
    } else {
      expect(wrapper.get('text').text()).toBe(icon.text)
      expect(wrapper.find('path').exists()).toBe(false)
      expect(wrapper.get('svg').attributes('fill')).toBe('currentColor')
    }
    for (const size of ['xs', 'sm', 'md', 'lg'] as const) {
      const known = mount(PlatformIcon, { props: { platform: id, size } })
      const brand = mount(PlatformIcon, { props: { platform: 'anthropic', size } })
      expect(known.get('svg').classes()).toEqual(expect.arrayContaining(brand.get('svg').classes()))
    }
  })

  it('keeps genuine artwork coverage explicit and unrelated providers distinct', () => {
    const fallbacks = COMPATIBLE_PROVIDERS.filter(p => compatibleProviderIcon(p.id, p.label).kind === 'monogram')
    expect(fallbacks.map(p => p.id).sort()).toEqual(['bailing', 'dmxapi', 'dmxapi_com', 'dmxapi_ssvip', 'therouter'])
    const assets = COMPATIBLE_PROVIDERS.map(p => compatibleProviderIcon(p.id, p.label)).filter(p => p.kind === 'asset')
    expect(assets).toHaveLength(67)
    expect(new Set(assets.map(p => p.src)).size).toBe(57)
  })

  it.each([
    ['compshare', 'compshare_coding'], ['crazyrouter', 'crazyrouter_intl'],
    ['dmxapi', 'dmxapi_com', 'dmxapi_ssvip'], ['mimo', 'mimo_token_cn'],
    ['qwencloud', 'qwencloud_token', 'qwencloud_maas_token'],
    ['siliconflow_cn', 'siliconflow_global'],
    ['stepfun_api', 'stepfun_api_intl', 'stepfun_plan', 'stepfun_plan_intl'],
    ['teamorouter', 'teamorouter_com'],
  ])('shares the actual vendor artwork across %s variants', (...ids) => {
    const expected = mount(PlatformIcon, { props: { platform: ids[0] } }).html()
    for (const platform of ids) expect(mount(PlatformIcon, { props: { platform } }).html()).toBe(expected)
  })

  it.each([
    ['astron_token', 'astron_coding'], ['mimo_token_sg', 'mimo'], ['mimo_token_eu', 'mimo'],
  ])('maps upcoming plan %s to its verified vendor artwork', (plan, vendor) => {
    const icon = compatibleProviderIcon(plan, plan)
    expect(icon.kind).toBe('asset')
    expect(icon).toBe(compatibleProviderIcon(vendor, vendor))
  })

  it('gives future registered providers initials and retains the unknown globe', () => {
    const providers = COMPATIBLE_PROVIDERS as Array<(typeof COMPATIBLE_PROVIDERS)[number]>
    const added = { ...providers[0]!, id: 'test-new-compatible-provider', label: 'Future Provider' }
    const fallback = mount(PlatformIcon).html()
    providers.push(added)
    try {
      expect(mount(PlatformIcon, { props: { platform: added.id } }).get('text').text()).toBe('FU')
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
