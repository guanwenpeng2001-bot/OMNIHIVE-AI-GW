import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import postcss from 'postcss'
import tailwindcss from 'tailwindcss'
import config from '../../../../tailwind.config.js'

const luminance = (color) => {
  const rgb = color.startsWith('#')
    ? color.slice(1, 7).match(/../g).map(channel => parseInt(channel, 16))
    : color.match(/[\d.]+/g).slice(0, 3).map(Number)
  const channels = rgb.map(channel => {
    const normalized = channel / 255
    return normalized <= 0.04045 ? normalized / 12.92 : ((normalized + 0.055) / 1.055) ** 2.4
  })
  return channels[0] * 0.2126 + channels[1] * 0.7152 + channels[2] * 0.0722
}

const contrast = (foreground, background) => {
  const values = [luminance(foreground), luminance(background)].sort((left, right) => right - left)
  return (values[0] + 0.05) / (values[1] + 0.05)
}

// Compile the real @apply rules: this also catches missing Tailwind tokens without writing a build.
const source = readFileSync(resolve(process.cwd(), 'src/style.css'), 'utf8')
const tokensSource = readFileSync(resolve(process.cwd(), 'src/styles/tokens.css'), 'utf8')
const badgeSource = readFileSync(resolve(process.cwd(), 'src/components/common/PlatformTypeBadge.vue'), 'utf8')
const compiled = await postcss([tailwindcss({ ...config, content: [{ raw: badgeSource + '<div class="btn btn-primary btn-danger btn-success btn-warning input dark badge-primary badge-success badge-warning badge-danger badge-gray badge-purple empty-state-description"/>', extension: 'html' }] })]).process(source, { from: undefined })

const extractTokenVars = (block) => {
  const vars = {}
  const regex = /(--oh-[a-z0-9-]+)\s*:\s*([^;]+);/g
  let match
  while ((match = regex.exec(block)) !== null) {
    vars[match[1]] = match[2].trim()
  }
  return vars
}
const rootTokens = extractTokenVars(tokensSource.match(/:root\s*\{([^}]+)\}/s)?.[1] ?? '')
const darkTokens = {
  ...rootTokens,
  ...extractTokenVars(tokensSource.match(/\.dark\s*\{([^}]+)\}/s)?.[1] ?? '')
}
const resolveTokenValue = (value, dark = false) => {
  if (!value) return value
  const palette = dark ? darkTokens : rootTokens
  const resolved = value.replace(/var\((--oh-[a-z0-9-]+)(?:,\s*([^)]+))?\)/g, (_, name, fallback) => palette[name] ?? fallback ?? '')
  const borderMatch = resolved.match(/#[a-fA-F0-9]{6}|#[a-fA-F0-9]{3}|rgb\([^)]*\)/)
  return borderMatch && value.includes('solid') ? borderMatch[0] : resolved
}

const declarations = (selector) => {
  const values = {}
  compiled.root.walkRules(rule => {
    if (rule.selector === selector) rule.walkDecls(declaration => { values[declaration.prop] = declaration.value })
  })
  return values
}

const themedDeclarations = (selector, theme = '') => {
  const dark = theme.includes('dark')
  const base = declarations(selector)
  const themed = theme ? declarations(`${selector}${theme}`) : {}
  const merged = { ...base, ...themed }
  if (!merged['border-color'] && merged.border) {
    merged['border-color'] = merged.border
  }
  const out = {}
  for (const [prop, val] of Object.entries(merged)) {
    out[prop] = resolveTokenValue(val, dark)
  }
  return out
}

const firstColor = value => value.match(/#[a-fA-F0-9]{6}|rgb\([^)]*\)/)?.[0]

describe('Shared control contrast', () => {
  it.each(['primary', 'danger', 'success', 'warning'])('keeps %s button text readable across themes and hover states', variant => {
    for (const theme of ['', ':is(.dark *)']) {
      const normal = themedDeclarations(`.btn-${variant}`, theme)
      const hover = {
        ...normal,
        ...themedDeclarations(`.btn-${variant}:hover`, theme),
        ...themedDeclarations(`.btn-${variant}:hover:not(:disabled)`, theme)
      }
      for (const state of [normal, hover]) {
        if (state['--tw-gradient-from'] && state['--tw-gradient-to']) {
          for (const stop of ['--tw-gradient-from', '--tw-gradient-to']) {
            expect(contrast(normal.color, firstColor(state[stop]))).toBeGreaterThanOrEqual(4.5)
          }
        } else {
          expect(contrast(state.color, firstColor(state['background-color']))).toBeGreaterThanOrEqual(4.5)
        }
      }
    }
  })

  it('keeps light input placeholders at 4.5:1 and boundaries at 3:1', () => {
    const input = themedDeclarations('.input')
    expect(contrast(themedDeclarations('.input::placeholder').color, input['background-color'])).toBeGreaterThanOrEqual(4.5)
    expect(contrast(input['border-color'], input['background-color'])).toBeGreaterThanOrEqual(3)
  })
})


it.each(['', ':is(.dark *)'])('keeps input text, placeholder, border, and focus contrast in theme %s', theme => {
  const input = themedDeclarations('.input', theme)
  const background = input['background-color']
  expect(contrast(input.color, background)).toBeGreaterThanOrEqual(4.5)
  expect(contrast(themedDeclarations('.input::placeholder', theme).color, background)).toBeGreaterThanOrEqual(4.5)
  expect(contrast(input['border-color'], background)).toBeGreaterThanOrEqual(3)
  const focus = themedDeclarations('.input:focus', theme)
  expect(contrast(focus['--tw-ring-color'] || focus['border-color'], background)).toBeGreaterThanOrEqual(3)
})


const composite = (color, background) => {
  const channels = color.match(/[\d.]+/g).map(Number)
  const alphaMatch = color.match(/\/\s*([\d.]+)\)/)
  const alpha = alphaMatch ? Number(alphaMatch[1]) : 1
  const base = background.slice(1).match(/../g).map(channel => parseInt(channel, 16))
  return `rgb(${channels.slice(0, 3).map((channel, index) => channel * alpha + base[index] * (1 - alpha)).join(' ')})`
}

it.each(['primary', 'success', 'warning', 'danger', 'gray', 'purple'])('keeps actual %s badge text readable in both themes', variant => {
  for (const theme of ['', ':is(.dark *)']) {
    const styles = declarations(`.badge-${variant}${theme}`)
    const backdrop = theme ? config.theme.extend.colors.dark[800] : '#ffffff'
    expect(contrast(styles.color, composite(styles['background-color'], backdrop))).toBeGreaterThanOrEqual(4.5)
  }
})

const utilityColor = (className, property) => {
  const selector = '.' + className.replace(/[:/]/g, character => `\\${character}`) + (className.startsWith('dark:') ? ':is(.dark *)' : '')
  return declarations(selector)[property]
}

it('checks the actual privacy badge class pairs, including translucent dark backgrounds', () => {
  const privacyClasses = [...badgeSource.matchAll(/class: '([^']+)'/g)].map(match => match[1].split(' '))
  expect(privacyClasses).toHaveLength(5)
  for (const classes of privacyClasses) {
    for (const theme of ['', 'dark:']) {
      const foreground = classes.find(name => name.startsWith(`${theme}text-`))
      const background = classes.find(name => name.startsWith(`${theme}bg-`))
      const backdrop = theme ? config.theme.extend.colors.dark[800] : '#ffffff'
      expect(contrast(utilityColor(foreground, 'color'), composite(utilityColor(background, 'background-color'), backdrop))).toBeGreaterThanOrEqual(4.5)
    }
  }
})

it('keeps actual empty-state and subscription helper text readable in both themes', () => {
  for (const theme of ['', ':is(.dark *)']) {
    const backdrop = theme ? config.theme.extend.colors.dark[800] : '#ffffff'
    expect(contrast(declarations(`.empty-state-description${theme}`).color, backdrop)).toBeGreaterThanOrEqual(4.5)
    const helper = badgeSource.match(/v-if="expiresLabel" class="([^"]+)"/)[1].split(' ')
    const foreground = helper.find(name => name.startsWith(theme ? 'dark:text-' : 'text-gray-'))
    expect(contrast(utilityColor(foreground, 'color'), backdrop)).toBeGreaterThanOrEqual(4.5)
  }
})

it('keeps accent as an identical compatibility alias of the existing dark palette', () => {
  expect(config.theme.extend.colors.accent).toBe(config.theme.extend.colors.dark)
})
