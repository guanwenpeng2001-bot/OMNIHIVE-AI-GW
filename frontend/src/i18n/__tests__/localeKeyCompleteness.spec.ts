import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'
import { SCANNER_CATALOG } from '@/features/prompt-audit/viewModel'
import { useSettingsNavigation } from '@/views/admin/settings/useSettingsNavigation'

type LocaleValue = Record<string, unknown>

function flattenLeafKeys(value: unknown, prefix = ''): string[] {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    return prefix ? [prefix] : []
  }

  return Object.entries(value as LocaleValue).flatMap(([key, child]) => {
    const path = prefix ? `${prefix}.${key}` : key
    return flattenLeafKeys(child, path)
  })
}

function getLeafString(messages: unknown, keyPath: string): string {
  let current: unknown = messages
  for (const segment of keyPath.split('.')) {
    if (current === null || typeof current !== 'object') {
      return ''
    }
    current = (current as LocaleValue)[segment]
  }
  return typeof current === 'string' ? current : ''
}

function extractPlaceholders(message: string): string[] {
  const matches = message.matchAll(/\{([a-zA-Z0-9_]+)\}/g)
  return [...new Set(Array.from(matches, (match) => match[1]))].sort()
}

function collectStaticSourceKeys(source: string): string[] {
  const keys = new Set<string>()
  const addKey = (key: string) => {
    if (key && !key.endsWith('.')) {
      keys.add(key)
    }
  }

  // Covers useI18n().t(), the global $t(), and direct i18n.t() calls in both
  // TypeScript and Vue script/template source. Dynamic suffixes are checked by
  // the runtime type of the value and cannot be proven from source text alone.
  const translationCalls = /(?:\bi18n\.t|\$t|\bt)\s*\(\s*(['"])([^'"\r\n]+)\1/g
  for (const match of source.matchAll(translationCalls)) {
    addKey(match[2])
  }

  // Covers ternary expressions such as t(cond ? 'key.a' : 'key.b').
  const ternaryTranslationCalls =
    /(?:\bi18n\.t|\$t|\bt)\s*\(\s*(?!`)[^()`\r\n]+?\?\s*(['"])([^'"\r\n]+)\1\s*:\s*(['"])([^'"\r\n]+)\3/g
  for (const match of source.matchAll(ternaryTranslationCalls)) {
    addKey(match[2])
    addKey(match[4])
  }

  // Covers template literal ternary expressions such as t(`prefix.${cond ? 'a' : 'b'}`).
  const templateTernaryCalls =
    /(?:\bi18n\.t|\$t|\bt)\s*\(\s*`([^`$\r\n]*)\$\{[^}\r\n]*?\?\s*(['"])([^'"\r\n]+)\2\s*:\s*(['"])([^'"\r\n]+)\4\s*\}([^`$\r\n]*)`/g
  for (const match of source.matchAll(templateTernaryCalls)) {
    addKey(`${match[1]}${match[3]}${match[6]}`)
    addKey(`${match[1]}${match[5]}${match[6]}`)
  }

  // Router metadata and i18n-t are key references without a t() call.
  const keyReferences = /(?:keypath|titleKey|descriptionKey)\s*[:=]\s*(['"])([^'"\r\n]+)\1/g
  for (const match of source.matchAll(keyReferences)) {
    keys.add(match[2])
  }

  const i18nTKeypaths = /<i18n-t\b[^>]*\bkeypath\s*=\s*(['"])([^'"\r\n]+)\1/gi
  for (const match of source.matchAll(i18nTKeypaths)) {
    keys.add(match[2])
  }

  return [...keys]
}

function sourceKeys(): string[] {
  const sourceFiles = import.meta.glob('../../**/*.{ts,vue}', {
    query: '?raw',
    import: 'default',
    eager: true
  }) as Record<string, string>

  return Object.entries(sourceFiles)
    .filter(([path]) => !path.includes('/__tests__/') && !/\.(spec|test)\.ts$/.test(path))
    .flatMap(([, source]) => collectStaticSourceKeys(source))
}

function missingKeys(usedKeys: string[], availableKeys: Set<string>): string[] {
  return usedKeys.filter((key) => !availableKeys.has(key)).sort()
}

describe('locale key completeness', () => {
  const enKeys = new Set(flattenLeafKeys(en))
  const zhKeys = new Set(flattenLeafKeys(zh))
  const usedKeys = [...new Set(sourceKeys())].sort()

  it('keeps English and Chinese locale schemas identical', () => {
    expect([...enKeys].filter((key) => !zhKeys.has(key)).sort()).toEqual([])
    expect([...zhKeys].filter((key) => !enKeys.has(key)).sort()).toEqual([])
  })

  it('contains a non-empty message for every locale leaf', () => {
    for (const [locale, messages] of Object.entries({ en, zh })) {
      const emptyKeys = flattenLeafKeys(messages).filter((key) => {
        let current: unknown = messages
        for (const segment of key.split('.')) {
          current = (current as LocaleValue)[segment]
        }
        return typeof current !== 'string' || current.trim() === ''
      })
      expect(emptyKeys, `${locale} has empty or non-string messages`).toEqual([])
    }
  })

  it('contains actual settings tab and prompt audit dynamic keys', () => {
    const dynamicKeys = [
      ...useSettingsNavigation().settingsTabs.map(tab => `admin.settings.tabs.${tab.key}`),
      ...SCANNER_CATALOG.flatMap(scanner => [`admin.promptAudit.scanners.${scanner.id}`, `admin.promptAudit.scannerDescriptions.${scanner.id}`]),
      ...['summary', 'risks', 'technical'].map(tab => `admin.promptAudit.events.tabs.${tab}`),
      ...['pass', 'flag', 'critical'].map(decision => `admin.promptAudit.decisions.${decision}`),
      ...['Allow', 'Warn', 'Block'].map(action => `admin.promptAudit.actions.${action}`),
      ...['low', 'medium', 'high', 'critical'].map(risk => `admin.promptAudit.riskLevels.${risk}`),
    ]
    expect(missingKeys(dynamicKeys, enKeys)).toEqual([])
    expect(missingKeys(dynamicKeys, zhKeys)).toEqual([])
  })

  it('contains every statically referenced production key', () => {
    expect(missingKeys(usedKeys, enKeys), 'English locale is missing referenced keys').toEqual([])
    expect(missingKeys(usedKeys, zhKeys), 'Chinese locale is missing referenced keys').toEqual([])
  })

  it('keeps interpolation placeholders identical between en and zh', () => {
    const mismatchedKeys = [...enKeys]
      .filter((key) => zhKeys.has(key))
      .filter((key) => {
        const enPlaceholders = extractPlaceholders(getLeafString(en, key))
        const zhPlaceholders = extractPlaceholders(getLeafString(zh, key))
        return enPlaceholders.join(',') !== zhPlaceholders.join(',')
      })
      .map((key) => ({
        key,
        en: extractPlaceholders(getLeafString(en, key)),
        zh: extractPlaceholders(getLeafString(zh, key))
      }))

    expect(mismatchedKeys).toEqual([])
  })
})
