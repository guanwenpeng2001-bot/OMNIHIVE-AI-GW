import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const dir = dirname(fileURLToPath(import.meta.url))
const sidebarSource = readFileSync(resolve(dir, '../AppSidebar.vue'), 'utf8')
const brandMarkSource = readFileSync(resolve(dir, '../BrandMark.vue'), 'utf8')
const homeViewSource = readFileSync(resolve(dir, '../../../views/HomeView.vue'), 'utf8')
const keyUsageViewSource = readFileSync(resolve(dir, '../../../views/KeyUsageView.vue'), 'utf8')

describe('site_logo sanitization', () => {
  it('AppSidebar imports sanitizeUrl and applies it to siteLogo', () => {
    expect(sidebarSource).toContain("import { sanitizeUrl } from '@/utils/url'")
    expect(sidebarSource).toContain('sanitizeUrl(appStore.siteLogo')
  })

  it('HomeView applies sanitizeUrl to siteLogo', () => {
    expect(homeViewSource).toContain('sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo')
  })

  it('KeyUsageView delegates brand logo rendering to PublicShell and BrandMark', () => {
    expect(keyUsageViewSource).toContain('<PublicShell>')
    expect(keyUsageViewSource).not.toContain('site_logo')
    expect(brandMarkSource).toContain("import { sanitizeUrl } from '@/utils/url'")
    expect(brandMarkSource).toContain('sanitizeUrl(props.siteLogo')
  })

  it('all logo renderers pass allowRelative and allowDataUrl options', () => {
    for (const src of [sidebarSource, homeViewSource, brandMarkSource]) {
      expect(src).toContain('allowRelative: true')
      expect(src).toContain('allowDataUrl: true')
    }
  })
})
