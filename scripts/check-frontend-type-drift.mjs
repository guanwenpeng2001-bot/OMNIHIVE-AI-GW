#!/usr/bin/env node
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const REQUIRED_OPENAPI_SCHEMAS = [
  'ModelSummary',
  'ModelDetail',
  'CatalogModelPage',
  'CatalogSourceAxis',
  'CatalogAvailability',
  'CatalogBilling',
  'CatalogCompareInput',
  'CatalogCompareResult',
  'CatalogCompareRow',
  'CatalogEstimate',
  'CatalogReferencePrice',
  'CatalogRate',
  'CatalogMetrics',
  'ExactQuoteInput',
  'ExactQuote',
  'StatusSummary',
  'StatusComponent',
  'StatusIncidentSummary',
  'StatusIncident'
]

const FORBIDDEN_DUPLICATE_INTERFACE_PATTERNS = [
  /\bexport\s+(?:interface|type)\s+PublicModelSummary\s*=/,
  /\bexport\s+interface\s+(?:PublicModelSummary|PublicModelDetail|PublicModelPage|PublicCompareRow|PublicCompareResponse|PublicCompareInput)\b/,
  /\binterface\s+RawCatalogModel\b/,
  /\binterface\s+RawCatalogPage\b/,
  /\binterface\s+(?:ModelSummary|ModelDetail|CatalogModelPage)\s*\{/
]

const FORBIDDEN_HEURISTIC_PATTERNS = [
  /\bresolveAvailabilityState\b/,
  /\bavailabilityStateLabel\b/,
  /\bavailabilityStateTone\b/
]

const BANNED_CATALOG_COPY_PATTERNS = [
  /按需接入/,
  /On [Dd]emand/,
  /平台目录\s*·\s*按需/,
  /同步到白名单/,
  /平台预置/
]

function collectSourceFiles(dir, result = []) {
  if (!fs.existsSync(dir)) return result
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const fullPath = path.join(dir, entry.name)
    if (entry.isDirectory()) {
      if (entry.name === 'node_modules' || entry.name === '__tests__' || entry.name === 'generated') {
        continue
      }
      collectSourceFiles(fullPath, result)
    } else if (entry.isFile() && /\.(?:ts|tsx|vue|mjs|js)$/.test(entry.name)) {
      if (entry.name.endsWith('.spec.ts') || entry.name.endsWith('.test.ts')) continue
      result.push(fullPath)
    }
  }
  return result
}

function stripComments(source) {
  return source
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/\/\/.*$/gm, '')
}

export function runTypeDriftChecks(repoRoot) {
  const violations = []
  const openapiPath = path.join(repoRoot, 'frontend/src/api/generated/openapi.d.ts')
  if (!fs.existsSync(openapiPath)) {
    violations.push(`Missing generated OpenAPI definitions at ${openapiPath}`)
    return { ok: false, violations }
  }

  const openapiSource = fs.readFileSync(openapiPath, 'utf8')
  for (const schemaName of REQUIRED_OPENAPI_SCHEMAS) {
    const schemaRe = new RegExp(`^\\s{8}${schemaName}:\\s`, 'm')
    if (!schemaRe.test(openapiSource)) {
      violations.push(`openapi.d.ts is missing required schema: ${schemaName}`)
    }
  }

  const publicApiPath = path.join(repoRoot, 'frontend/src/features/public/api.ts')
  if (fs.existsSync(publicApiPath)) {
    const publicApi = fs.readFileSync(publicApiPath, 'utf8')
    if (!publicApi.includes("from '@/api/catalog'") && !publicApi.includes("from '@/features/catalog/contract'")) {
      violations.push('frontend/src/features/public/api.ts must delegate to @/api/catalog and @/features/catalog/contract')
    }
  }

  const consoleCatalogPath = path.join(repoRoot, 'frontend/src/features/console/catalogApi.ts')
  if (fs.existsSync(consoleCatalogPath)) {
    const consoleApi = fs.readFileSync(consoleCatalogPath, 'utf8')
    if (!consoleApi.includes("from '@/api/catalog'")) {
      violations.push('frontend/src/features/console/catalogApi.ts must delegate HTTP calls to @/api/catalog')
    }
    if (/listPublicModels\s*\(/.test(consoleApi) && /listScopedModels[\s\S]*this\.listPublicModels/.test(consoleApi)) {
      violations.push('frontend/src/features/console/catalogApi.ts must not fall back from listScopedModels to listPublicModels')
    }
  }

  const srcRoot = path.join(repoRoot, 'frontend/src')
  const files = collectSourceFiles(srcRoot)
  const catalogClientRel = 'frontend/src/api/catalog.ts'
  const contractRel = 'frontend/src/features/catalog/contract.ts'

  const publicCatalogScopedDirs = [
    'frontend/src/features/catalog/',
    'frontend/src/features/public/',
    'frontend/src/components/catalog/',
    'frontend/src/composables/useCatalogBrowser.ts',
    'frontend/src/views/public/',
    'frontend/src/components/layout/ShellCommandPalette.vue'
  ]

  for (const absPath of files) {
    const rel = path.relative(repoRoot, absPath).split(path.sep).join('/')
    const content = fs.readFileSync(absPath, 'utf8')
    const codeOnly = stripComments(content)

    if (rel !== publicApiPath && rel !== contractRel) {
      for (const pattern of FORBIDDEN_DUPLICATE_INTERFACE_PATTERNS) {
        if (pattern.test(codeOnly)) {
          // Allow thin type re-export aliases in features/public/api.ts ONLY when sourced from contract.ts
          if (rel === 'frontend/src/features/public/api.ts' && /from '@\/features\/catalog\/contract'/.test(content)) {
            continue
          }
          violations.push(`Duplicate hand-written catalog schema detected in ${rel} (matched ${pattern})`)
        }
      }
    }

    for (const pattern of FORBIDDEN_HEURISTIC_PATTERNS) {
      if (pattern.test(codeOnly)) {
        violations.push(`Removed availability heuristic reference in ${rel} (matched ${pattern})`)
      }
    }

    if (rel !== catalogClientRel && /\/api\/v2\/(?:public|workspaces\/[^/]+\/projects\/[^/]+)\/models/.test(codeOnly)) {
      violations.push(`Direct v2 catalog endpoint call outside ${catalogClientRel}: ${rel}`)
    }

    if (rel === 'frontend/src/components/layout/ShellCommandPalette.vue' && /\bgetModelPlaza\b/.test(codeOnly)) {
      violations.push('ShellCommandPalette.vue must not call legacy v1 getModelPlaza()')
    }

    if (publicCatalogScopedDirs.some((prefix) => rel.startsWith(prefix))) {
      for (const pattern of BANNED_CATALOG_COPY_PATTERNS) {
        if (pattern.test(codeOnly)) {
          violations.push(`Banned catalog copy in ${rel} (matched ${pattern})`)
        }
      }
    }
  }

  return {
    ok: violations.length === 0,
    violations
  }
}

const isDirectExecution =
  process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)

if (isDirectExecution) {
  const scriptDir = path.dirname(fileURLToPath(import.meta.url))
  const repoRoot = path.resolve(scriptDir, '..')
  const result = runTypeDriftChecks(repoRoot)
  if (!result.ok) {
    console.error('[type-drift-guard] FAILED:')
    for (const item of result.violations) {
      console.error(`  - ${item}`)
    }
    process.exit(1)
  }
  console.log('[type-drift-guard] OK: generated OpenAPI types, single catalog client, and 3-axis view-model clean.')
}
