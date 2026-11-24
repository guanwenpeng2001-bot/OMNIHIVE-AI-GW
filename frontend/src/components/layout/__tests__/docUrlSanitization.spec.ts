import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const dir = dirname(fileURLToPath(import.meta.url))
const headerSource = readFileSync(resolve(dir, '../AppHeader.vue'), 'utf8')
const publicHeaderSource = readFileSync(resolve(dir, '../PublicHeader.vue'), 'utf8')
const publicShellSource = readFileSync(resolve(dir, '../PublicShell.vue'), 'utf8')
const homeViewSource = readFileSync(resolve(dir, '../../../views/HomeView.vue'), 'utf8')
const keyUsageViewSource = readFileSync(resolve(dir, '../../../views/KeyUsageView.vue'), 'utf8')

describe('doc_url sanitization', () => {
  it('AppHeader imports sanitizeUrl', () => {
    expect(headerSource).toContain("import { sanitizeUrl } from '@/utils/url'")
  })

  it('AppHeader applies sanitizeUrl to docUrl', () => {
    expect(headerSource).toContain('sanitizeUrl(appStore.docUrl)')
  })

  it('HomeView imports sanitizeUrl', () => {
    expect(homeViewSource).toContain("import { sanitizeUrl } from '@/utils/url'")
  })

  it('HomeView applies sanitizeUrl to docUrl', () => {
    expect(homeViewSource).toContain('sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl')
  })

  it('KeyUsageView delegates header and footer docUrl rendering to PublicShell', () => {
    expect(keyUsageViewSource).toContain('<PublicShell>')
    expect(keyUsageViewSource).not.toContain('doc_url')
  })

  it('PublicHeader and PublicShell apply sanitizeUrl to docUrl', () => {
    for (const src of [publicHeaderSource, publicShellSource]) {
      expect(src).toContain("import { sanitizeUrl } from '@/utils/url'")
      expect(src).toContain('sanitizeUrl(appStore')
      expect(src).toContain('cachedPublicSettings?.doc_url')
    }
  })
})
