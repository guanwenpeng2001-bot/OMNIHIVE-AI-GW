<script setup lang="ts">
import { computed } from 'vue'
import {
  buildAdminProvenanceMatrix,
  type AdminCatalogBilling,
  type AdminCatalogCommonFacts,
  type AdminCatalogFieldProvenance,
  type AdminCatalogSourceKind
} from '@/api/admin/modelCatalog'

const props = withDefaults(
  defineProps<{
    commonFacts: AdminCatalogCommonFacts
    provenance: AdminCatalogFieldProvenance[]
    billing?: AdminCatalogBilling
    isZh?: boolean
  }>(),
  {
    isZh: true
  }
)

const matrixRows = computed(() =>
  buildAdminProvenanceMatrix(props.commonFacts, props.provenance, props.billing)
)

const conflictCount = computed(
  () => matrixRows.value.filter((row) => row.hasConflict).length
)

const FIELD_LABELS: Record<string, { zh: string; en: string }> = {
  context_window: { zh: '上下文窗口', en: 'Context Window' },
  max_output_tokens: { zh: '最大输出 Token', en: 'Max Output Tokens' },
  modalities: { zh: '支持模态', en: 'Modalities' },
  tools: { zh: '工具调用 (Tools)', en: 'Tool Calling' },
  supports_vision: { zh: '视觉输入 (Vision)', en: 'Vision Input' },
  reasoning: { zh: '深度推理 (Reasoning)', en: 'Reasoning' },
  prompt_caching: { zh: '提示词缓存 (Caching)', en: 'Prompt Caching' },
  embedding_dimensions: { zh: '向量维度', en: 'Embedding Dimensions' },
  pdf: { zh: 'PDF 文档输入', en: 'PDF Input' },
  reference_price: { zh: '参考价（非结算价）', en: 'Reference Price (Non-settlement)' }
}

const SOURCE_SHORT_LABELS: Record<AdminCatalogSourceKind, { zh: string; en: string }> = {
  builtin_reviewed: { zh: '内置审阅', en: 'Approved' },
  platform_preset: { zh: '平台候选', en: 'Candidate' },
  builtin_supplement: { zh: '内置补充', en: 'Supplement' },
  pi_ai_reference: { zh: '社区目录（pi）', en: 'Community (pi)' },
  account_discovery: { zh: '账号原生发现', en: 'Discovered' },
  account_declaration: { zh: '连接声明', en: 'Declared' },
  external_reference: { zh: '外部参考', en: 'External' },
  heuristic: { zh: '启发式估算', en: 'Heuristic' },
  unknown: { zh: '未知', en: 'Unknown' }
}

function fieldLabel(field: string): string {
  const entry = FIELD_LABELS[field]
  if (entry) return props.isZh ? entry.zh : entry.en
  return field
}

function winningSourceLabel(kind: AdminCatalogSourceKind): string {
  if (kind === 'unknown') return ''
  const entry = SOURCE_SHORT_LABELS[kind]
  return props.isZh ? entry.zh : entry.en
}
</script>

<template>
  <section
    data-testid="admin-model-provenance-section"
    class="space-y-3 rounded-xl border border-[var(--border)] bg-[var(--card)] p-4"
  >
    <div class="flex flex-wrap items-start justify-between gap-2">
      <div>
        <h3 class="text-sm font-semibold text-[var(--foreground)]">
          {{
            isZh
              ? '1. 字段级来源合并表（优先级：内置审阅 > 内置补充 > 社区目录（pi））'
              : '1. Field-Level Provenance Merge Table (Precedence: Approved > Supplement > Community (pi))'
          }}
        </h3>
        <p class="mt-0.5 text-xs text-[var(--muted-foreground)]">
          {{
            isZh
              ? '账号原生发现按连接作用域独立记录；不同来源规格不一致时以琥珀色高亮冲突，绝不静默丢弃或用 0/false 猜测未知值。'
              : 'Native Discovered observations are scoped per connection; conflicting source values are highlighted in amber and unknown values stay —.'
          }}
        </p>
      </div>
      <span
        v-if="conflictCount > 0"
        data-testid="provenance-conflict-summary"
        class="inline-flex items-center gap-1 rounded-md border border-amber-500/40 bg-amber-500/15 px-2.5 py-1 text-xs font-semibold text-amber-700 dark:text-amber-300"
      >
        {{
          isZh
            ? `检测到 ${conflictCount} 项规格来源冲突`
            : `${conflictCount} Provenance Conflict(s)`
        }}
      </span>
    </div>

    <div class="overflow-x-auto rounded-lg border border-[var(--border)]">
      <table
        data-testid="admin-model-provenance-table"
        class="w-full text-left text-xs tabular-nums"
      >
        <thead class="border-b border-[var(--border)] bg-[var(--surface-2)] text-[var(--muted-foreground)]">
          <tr>
            <th class="px-3 py-2 font-semibold">{{ isZh ? '规格字段' : 'Field' }}</th>
            <th class="px-3 py-2 font-semibold">{{ isZh ? '内置审阅' : 'Approved Registry' }}</th>
            <th class="px-3 py-2 font-semibold">{{ isZh ? '内置补充' : 'Custom Supplement' }}</th>
            <th class="px-3 py-2 font-semibold">{{ isZh ? '社区目录（pi）' : 'Community (pi)' }}</th>
            <th class="px-3 py-2 font-semibold">{{ isZh ? '账号原生发现' : 'Native Discovered' }}</th>
            <th class="px-3 py-2 font-semibold">{{ isZh ? '生效值' : 'Winning Value' }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-[var(--border)]">
          <tr
            v-for="row in matrixRows"
            :key="row.field"
            data-testid="provenance-row"
            :data-provenance-field="row.field"
            :data-provenance-conflict="row.hasConflict ? 'true' : 'false'"
            :class="
              row.hasConflict
                ? 'bg-amber-500/10 text-[var(--foreground)]'
                : 'bg-[var(--card)] text-[var(--foreground)]'
            "
          >
            <th class="px-3 py-2.5 font-medium">
              <div class="flex flex-wrap items-center gap-1.5">
                <span>{{ fieldLabel(row.field) }}</span>
                <span
                  v-if="row.hasConflict"
                  data-testid="provenance-conflict-badge"
                  class="rounded border border-amber-500/50 bg-amber-500/20 px-1.5 py-0.5 text-[10px] font-semibold text-amber-700 dark:text-amber-300"
                >
                  {{ isZh ? '规格来源冲突' : 'Provenance Conflict' }}
                </span>
                <span
                  v-if="row.hasStale"
                  data-testid="provenance-stale-badge"
                  class="rounded border border-orange-500/40 bg-orange-500/15 px-1.5 py-0.5 text-[10px] font-medium text-orange-700 dark:text-orange-300"
                >
                  {{ isZh ? '证据已过期' : 'Stale' }}
                </span>
              </div>
            </th>
            <td class="px-3 py-2.5 font-mono">{{ row.approvedValue }}</td>
            <td class="px-3 py-2.5 font-mono">{{ row.supplementValue }}</td>
            <td class="px-3 py-2.5 font-mono">{{ row.piValue }}</td>
            <td class="px-3 py-2.5 font-mono">{{ row.discoveredValue }}</td>
            <td class="px-3 py-2.5 font-mono font-semibold">
              <span>{{ row.effectiveValue }}</span>
              <span
                v-if="row.effectiveValue !== '—' && winningSourceLabel(row.winningSource)"
                class="ml-1.5 rounded bg-[var(--surface-2)] px-1.5 py-0.5 font-sans text-[10px] font-normal text-[var(--muted-foreground)]"
              >
                {{ winningSourceLabel(row.winningSource) }}
              </span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
