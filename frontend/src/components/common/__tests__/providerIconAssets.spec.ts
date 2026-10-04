import { readFileSync, readdirSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, join } from 'node:path'
import { describe, expect, it } from 'vitest'

const directory = join(process.cwd(), 'src/components/common/providerIcons')
const iconPackage = dirname(createRequire(import.meta.url).resolve('@lobehub/icons/package.json'))
const monoAssets = {
  aihubmix: 'AiHubMix', astron_coding: 'IFlyTekCloud', azure_openai: 'Azure',
  github_copilot: 'GithubCopilot', kat_coder: 'KwaiKAT', longcat: 'LongCat',
  mimo: 'XiaomiMiMo', modelscope: 'ModelScope', nous_research: 'NousResearch',
  novita: 'Novita', nvidia: 'Nvidia', ppio: 'PPIO', qiniu: 'Qiniu',
  siliconflow_cn: 'SiliconCloud', stepfun_api: 'Stepfun', together: 'Together',
}

function svgDocument(filename: string) {
  return new DOMParser().parseFromString(readFileSync(join(directory, filename), 'utf8'), 'image/svg+xml')
}

describe('local provider artwork', () => {
  it.each(Object.entries(monoAssets))('preserves installed LobeHub %s geometry', (id, asset) => {
    const source = readFileSync(join(iconPackage, 'es', asset, 'components/Mono.js'), 'utf8')
    const svg = svgDocument(`${id}.svg`).documentElement
    expect(svg.getAttribute('viewBox')).toBe(source.match(/viewBox: "([^"]+)"/)?.[1])
    expect(svg.getAttribute('fill-rule') ?? undefined).toBe(source.match(/fillRule: "([^"]+)"/)?.[1])
    const shapes = [...source.matchAll(/_jsx\("(path|circle|rect|ellipse|polygon)", \{([\s\S]*?)\}\)/g)]
    expect(svg.children).toHaveLength(shapes.length)
    shapes.forEach((shape, index) => {
      const rendered = svg.children[index]!
      expect(rendered.tagName).toBe(shape[1])
      for (const [, name, value] of shape[2]!.matchAll(/(\w+): ("[^"]*"|[\d.]+)/g)) {
        const attribute = name!.replace(/[A-Z]/g, letter => `-${letter.toLowerCase()}`)
        expect(rendered.getAttribute(attribute)).toBe(value!.replace(/^"|"$/g, ''))
      }
    })
    // No group transforms, defs or shapes were silently dropped during transcription.
    expect(source).not.toMatch(/_jsx[s]?\("(?:g|defs|use|polyline|line)"/)
  })

  it.each(readdirSync(directory).filter(file => file.endsWith('.svg')))('%s is valid, self-contained passive SVG', filename => {
    const doc = svgDocument(filename)
    expect(doc.querySelector('parsererror')).toBeNull()
    expect(doc.documentElement.tagName).toBe('svg')
    expect(doc.documentElement.getAttribute('viewBox')).toMatch(/^[-\d.]+ [-\d.]+ [\d.]+ [\d.]+$/)
    expect(doc.querySelector('script, foreignObject, iframe, title')).toBeNull()
    expect(doc.querySelector('path, rect, circle, polygon, image')).not.toBeNull()
    for (const style of doc.querySelectorAll('style')) {
      expect(style.textContent).not.toMatch(/@import|https?:|data:|expression/i)
      for (const [, target] of style.textContent!.matchAll(/url\(['"]?([^)'" ]+)/g)) expect(target).toMatch(/^#/)
    }
    for (const node of doc.querySelectorAll('*')) {
      for (const attribute of node.attributes) {
        expect(attribute.name).not.toMatch(/^on/i)
        if (attribute.name.includes('href')) expect(attribute.value).toMatch(/^(#|data:image\/png;base64,)/)
        for (const [, target] of attribute.value.matchAll(/url\(['"]?([^)'" ]+)/g)) expect(target).toMatch(/^#/)
      }
    }
  })

  it.each(readdirSync(directory).filter(file => /\.(png|jpg|webp)$/.test(file)))('%s has the expected image signature', filename => {
    const bytes = readFileSync(join(directory, filename))
    if (filename.endsWith('.png')) expect(bytes.subarray(0, 8).toString('hex')).toBe('89504e470d0a1a0a')
    if (filename.endsWith('.jpg')) expect(bytes.subarray(0, 3).toString('hex')).toBe('ffd8ff')
    if (filename.endsWith('.webp')) expect(bytes.subarray(8, 12).toString()).toBe('WEBP')
  })
})
