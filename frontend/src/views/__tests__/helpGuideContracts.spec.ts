// @vitest-environment node
import { readFileSync, readdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const root = new URL('../../../../', import.meta.url)
const read = (path: string) => readFileSync(new URL(path, root), 'utf8')
const registryPath = 'backend/internal/domain/data/compatible_providers/'
type Provider = {
  id: string
  modes: { mode: string; endpoints: { protocol: string; base_url?: string }[] }[]
  plans?: { id: string; label_zh: string; query: string }[]
}
const providers = readdirSync(fileURLToPath(new URL(registryPath, root)))
  .filter(file => file.endsWith('.json') && file !== 'index.json')
  .flatMap(file => JSON.parse(read(registryPath + file)) as Provider[])
const guide = read('docs/provider-configuration.md')

describe('provider guide source contracts', () => {
  it('documents every current provider and its registered protocol bases', () => {
    for (const provider of providers) {
      const anchor = `<a id="provider-${provider.id}"></a>`
      expect(guide, provider.id).toContain(anchor)
      const section = guide.split(anchor)[1]!.split('<a id="provider-')[0]!
      for (const mode of provider.modes) {
        expect(section, provider.id).toContain(`\`${mode.mode}\``)
        for (const endpoint of mode.endpoints) {
          expect(section, provider.id).toContain(`\`${endpoint.protocol}\``)
          if (endpoint.base_url) expect(section, provider.id).toContain(endpoint.base_url)
        }
      }
    }
  })

  it('keeps selectable plan IDs and query classification aligned with the registry', () => {
    const planSection = guide.split('## 套餐选择、保存与清除')[1]!.split('## 证据与能力的读法')[0]!
    for (const provider of providers.filter(provider => provider.plans?.length)) {
      const row = planSection.split('\n').find(line => line.startsWith(`| \`${provider.id}\` |`))
      expect(row, provider.id).toBeDefined()
      for (const plan of provider.plans!) {
        expect(row, provider.id).toContain(`\`${plan.id}\`（${plan.label_zh}；\`${plan.query}\`）`)
      }
    }
  })

  it('does not publish frozen compatible-provider counts as current coverage', () => {
    for (const path of ['docs/usage-guide.md', 'docs/provider-configuration.md', 'docs/gateway-platform-protocol-matrix.md']) {
      expect(read(path), path).not.toMatch(/72 (?:个|distinct)|72-ID|six manual balance|六个兼容/)
    }
  })
})
