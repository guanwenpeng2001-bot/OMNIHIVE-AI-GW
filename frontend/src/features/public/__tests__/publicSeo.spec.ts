import fs from 'node:fs'
import path from 'node:path'
import { describe, expect, it } from 'vitest'
import { applyRouteSeo } from '../seo'

const frontendRoot = path.resolve(__dirname, '../../../..')

describe('Public SEO & static pages [E1-6]', () => {
  it('provides robots.txt, sitemap.xml, and maintenance.html in frontend/public/', () => {
    const robotsPath = path.join(frontendRoot, 'public', 'robots.txt')
    const sitemapPath = path.join(frontendRoot, 'public', 'sitemap.xml')
    const maintenancePath = path.join(frontendRoot, 'public', 'maintenance.html')

    expect(fs.existsSync(robotsPath)).toBe(true)
    expect(fs.existsSync(sitemapPath)).toBe(true)
    expect(fs.existsSync(maintenancePath)).toBe(true)

    const robots = fs.readFileSync(robotsPath, 'utf-8')
    expect(robots).toContain('User-agent:')
    expect(robots).toContain('Sitemap:')
    expect(robots).toContain('Disallow: /admin')

    const sitemap = fs.readFileSync(sitemapPath, 'utf-8')
    expect(sitemap).toContain('<urlset')
    expect(sitemap).toContain('/model-plaza')
    expect(sitemap).toContain('/pricing')
    expect(sitemap).toContain('/docs')
    expect(sitemap).toContain('/legal/terms')

    const maintenance = fs.readFileSync(maintenancePath, 'utf-8')
    expect(maintenance).toContain('<!doctype html>')
    expect(maintenance).toContain('503')
  })

  it('includes default OG metadata in index.html', () => {
    const indexHtml = fs.readFileSync(path.join(frontendRoot, 'index.html'), 'utf-8')
    expect(indexHtml).toContain('property="og:title"')
    expect(indexHtml).toContain('property="og:description"')
    expect(indexHtml).toContain('property="og:type"')
  })

  it('updates title, description, OG tags, and canonical link via applyRouteSeo', () => {
    applyRouteSeo({
      title: 'Gemini 3.8 Flash - OmniHive',
      description: 'Model pricing and latency details for Gemini 3.8 Flash.',
      canonicalPath: '/model-plaza/auto%2Fgemini-3.8-flash',
      siteName: 'OmniHive'
    })

    expect(document.title).toBe('Gemini 3.8 Flash - OmniHive')
    expect(document.querySelector('meta[name="description"]')?.getAttribute('content')).toBe(
      'Model pricing and latency details for Gemini 3.8 Flash.'
    )
    expect(document.querySelector('meta[property="og:title"]')?.getAttribute('content')).toBe(
      'Gemini 3.8 Flash - OmniHive'
    )
    expect(document.querySelector('meta[property="og:description"]')?.getAttribute('content')).toBe(
      'Model pricing and latency details for Gemini 3.8 Flash.'
    )
    expect(document.querySelector('link[rel="canonical"]')?.getAttribute('href')).toContain(
      '/model-plaza/auto%2Fgemini-3.8-flash'
    )
  })
})
