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
const compiled = await postcss([tailwindcss({ ...config, content: [{ raw: '<div class="btn btn-primary btn-danger btn-success btn-warning input dark"/>', extension: 'html' }] })]).process(source, { from: undefined })
const declarations = (selector) => {
  const values = {}
  compiled.root.walkRules(rule => {
    if (rule.selector === selector) rule.walkDecls(declaration => { values[declaration.prop] = declaration.value })
  })
  return values
}

const firstColor = value => value.match(/#[a-fA-F0-9]{6}|rgb\([^)]*\)/)?.[0]

describe('Shared control contrast', () => {
  it.each(['primary', 'danger', 'success', 'warning'])('keeps white text readable at both %s gradient endpoints and on hover', variant => {
    const normal = declarations(`.btn-${variant}`)
    const hover = declarations(`.btn-${variant}:hover`)
    for (const state of [normal, { ...normal, ...hover }]) {
      for (const stop of ['--tw-gradient-from', '--tw-gradient-to']) {
        const ratio = contrast(normal.color, firstColor(state[stop]))
        expect(ratio).toBeGreaterThanOrEqual(4.5)
      }
    }
  })

  it('keeps light input placeholders at 4.5:1 and boundaries at 3:1', () => {
    const input = declarations('.input')
    expect(contrast(declarations('.input::placeholder').color, input['background-color'])).toBeGreaterThanOrEqual(4.5)
    expect(contrast(input['border-color'], input['background-color'])).toBeGreaterThanOrEqual(3)
  })
})


it.each(['', ':is(.dark *)'])('keeps input text, placeholder, border, and focus contrast in theme %s', theme => {
  const input = declarations(`.input${theme}`)
  const background = input['background-color']
  expect(contrast(input.color, background)).toBeGreaterThanOrEqual(4.5)
  expect(contrast(declarations(`.input${theme}::placeholder`).color, background)).toBeGreaterThanOrEqual(4.5)
  expect(contrast(input['border-color'], background)).toBeGreaterThanOrEqual(3)
  expect(contrast(declarations(`.input:focus${theme}`)['--tw-ring-color'], background)).toBeGreaterThanOrEqual(3)
})
