import { describe, expect, it } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'

function parseHex(h: string): [number, number, number] {
  const clean = h.trim().replace(/^#/, '')
  const full = clean.length === 3 ? clean.split('').map((c) => c + c).join('') : clean
  const num = parseInt(full, 16)
  return [(num >> 16) & 255, (num >> 8) & 255, num & 255]
}

function sRGBtoLin(c: number): number {
  const val = c / 255
  return val <= 0.04045 ? val / 12.92 : Math.pow((val + 0.055) / 1.055, 2.4)
}

function relLum(hex: string): number {
  const [r, g, b] = parseHex(hex)
  return 0.2126 * sRGBtoLin(r) + 0.7152 * sRGBtoLin(g) + 0.0722 * sRGBtoLin(b)
}

function getContrast(h1: string, h2: string): number {
  const l1 = relLum(h1)
  const l2 = relLum(h2)
  return (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05)
}

function parseTokens(cssText: string) {
  const rootMatch = cssText.match(/:root\s*\{([^}]+)\}/s)
  const darkMatch = cssText.match(/\.dark\s*\{([^}]+)\}/s)

  function extractVars(block: string) {
    const vars: Record<string, string> = {}
    const regex = /(--oh-[a-z0-9-]+)\s*:\s*([^;]+);/g
    let m: RegExpExecArray | null
    while ((m = regex.exec(block)) !== null) {
      vars[m[1]] = m[2].trim()
    }
    return vars
  }

  const rootVars = rootMatch ? extractVars(rootMatch[1]) : {}
  const darkVars = darkMatch ? extractVars(darkMatch[1]) : {}
  return {
    light: rootVars,
    dark: { ...rootVars, ...darkVars }
  }
}

describe('Design System WCAG Contrast Gating (tokens.css)', () => {
  const tokensPath = path.resolve(__dirname, '../../../styles/tokens.css')
  const css = fs.readFileSync(tokensPath, 'utf-8')
  const { light, dark } = parseTokens(css)

  const surfaces = [
    '--oh-bg-canvas',
    '--oh-bg-subtle',
    '--oh-bg-surface',
    '--oh-bg-elevated'
  ]

  describe('Light Theme Contrast', () => {
    it('--oh-accent-active with --oh-accent-ink reaches >= 4.5:1 pressed contrast', () => {
      const ratio = getContrast(light['--oh-accent-active'], light['--oh-accent-ink'])
      expect(ratio).toBeGreaterThanOrEqual(4.5)
    })

    it('--oh-accent and --oh-accent-hover maintain >= 4.5:1 with --oh-accent-ink', () => {
      expect(getContrast(light['--oh-accent'], light['--oh-accent-ink'])).toBeGreaterThanOrEqual(4.5)
      expect(getContrast(light['--oh-accent-hover'], light['--oh-accent-ink'])).toBeGreaterThanOrEqual(4.5)
    })

    it('--oh-fg-muted reaches >= 4.5:1 on every surface token', () => {
      for (const surface of surfaces) {
        const ratio = getContrast(light['--oh-fg-muted'], light[surface])
        expect(ratio, `light --oh-fg-muted on ${surface}`).toBeGreaterThanOrEqual(4.5)
      }
    })

    it('--oh-fg-primary and --oh-fg-secondary reach WCAG AA (>= 4.5:1) on all surfaces', () => {
      for (const surface of surfaces) {
        expect(getContrast(light['--oh-fg-primary'], light[surface])).toBeGreaterThanOrEqual(7.0)
        expect(getContrast(light['--oh-fg-secondary'], light[surface])).toBeGreaterThanOrEqual(4.5)
      }
    })

    it('--oh-accent-text reaches >= 4.5:1 on all surfaces', () => {
      for (const surface of surfaces) {
        expect(getContrast(light['--oh-accent-text'], light[surface])).toBeGreaterThanOrEqual(4.5)
      }
    })

    it('semantic status colors reach >= 4.5:1 on their soft status backgrounds', () => {
      const statuses = ['ok', 'warn', 'danger', 'info', 'neutral', 'unknown']
      for (const s of statuses) {
        const fg = light[`--oh-${s}-fg`]
        const bg = light[`--oh-${s}-bg`]
        expect(getContrast(fg, bg), `light status ${s}`).toBeGreaterThanOrEqual(4.5)
      }
    })
  })

  describe('Dark Theme Contrast', () => {
    it('--oh-accent-active with --oh-accent-ink reaches >= 4.5:1 pressed contrast', () => {
      const ratio = getContrast(dark['--oh-accent-active'], dark['--oh-accent-ink'])
      expect(ratio).toBeGreaterThanOrEqual(4.5)
    })

    it('--oh-accent and --oh-accent-hover maintain >= 4.5:1 with --oh-accent-ink', () => {
      expect(getContrast(dark['--oh-accent'], dark['--oh-accent-ink'])).toBeGreaterThanOrEqual(4.5)
      expect(getContrast(dark['--oh-accent-hover'], dark['--oh-accent-ink'])).toBeGreaterThanOrEqual(4.5)
    })

    it('--oh-fg-muted reaches >= 4.5:1 on every surface token', () => {
      for (const surface of surfaces) {
        const ratio = getContrast(dark['--oh-fg-muted'], dark[surface])
        expect(ratio, `dark --oh-fg-muted on ${surface}`).toBeGreaterThanOrEqual(4.5)
      }
    })

    it('--oh-fg-primary and --oh-fg-secondary reach WCAG AA (>= 4.5:1) on all surfaces', () => {
      for (const surface of surfaces) {
        expect(getContrast(dark['--oh-fg-primary'], dark[surface])).toBeGreaterThanOrEqual(7.0)
        expect(getContrast(dark['--oh-fg-secondary'], dark[surface])).toBeGreaterThanOrEqual(4.5)
      }
    })

    it('--oh-accent-text reaches >= 4.5:1 on all surfaces', () => {
      for (const surface of surfaces) {
        expect(getContrast(dark['--oh-accent-text'], dark[surface])).toBeGreaterThanOrEqual(4.5)
      }
    })

    it('semantic status colors reach >= 4.5:1 on their soft status backgrounds', () => {
      const statuses = ['ok', 'warn', 'danger', 'info', 'neutral', 'unknown']
      for (const s of statuses) {
        const fg = dark[`--oh-${s}-fg`]
        const bg = dark[`--oh-${s}-bg`]
        expect(getContrast(fg, bg), `dark status ${s}`).toBeGreaterThanOrEqual(4.5)
      }
    })
  })
})
