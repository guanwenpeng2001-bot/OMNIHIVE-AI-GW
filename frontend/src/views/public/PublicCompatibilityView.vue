<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import PublicShell from '@/components/layout/PublicShell.vue'
import Badge from '@/components/ui/Badge.vue'
import type { UiTone } from '@/components/ui/types'

const COMPAT_MESSAGES = {
  en: {
    eyebrow: 'UPDATES & COMPATIBILITY',
    title: 'Protocol Compatibility, Migration Windows & Release Guarantees',
    subtitle:
      'Authoritative reference for wire-level API compatibility, extension package migration (.ohplugin v2 vs .s2plugin), binary checksum enforcement, and rollback contracts. Unobserved or unsupported capabilities are surfaced explicitly—never silently dropped.',
    ctaDocs: 'Integration Docs →',
    ctaModels: 'Browse Model Plaza',
    ctaStatus: 'Live Service Status',

    matrixEyebrow: 'WIRE PROTOCOL MATRIX',
    matrixTitle: 'API Wire Protocol Compatibility',
    matrixDesc:
      'Every gateway entrypoint enforces strict schema and capability governance. Unsupported upstream-specific server tools return explicit 4xx validation errors rather than silent omission.',
    colProtocol: 'Surface',
    colEndpoint: 'Canonical Endpoint',
    colMaturity: 'Maturity',
    colCapabilities: 'Streaming · Tools · Reasoning',
    colBoundary: 'Compatibility Contract & Known Boundaries',

    maturityStable: 'Stable',
    maturityBridge: 'Migration Window',

    protoOpenAIChatName: 'OpenAI Chat Completions',
    protoOpenAIChatCap: 'SSE [DONE] · Function Tools · reasoning_effort · Vision/Audio',
    protoOpenAIChatNote:
      'Native & bridged execution under unified attempt.Controller budget; incompatible server-side native tools fail closed instead of dropping tool_choice.',

    protoOpenAIResponsesName: 'OpenAI Responses API',
    protoOpenAIResponsesCap: 'Typed Event SSE · Function & Hosted Items · Reasoning Summary',
    protoOpenAIResponsesNote:
      'First-class wire_api = "responses" support for Codex CLI and stateful/stateless agent workflows with deterministic terminal journal states.',

    protoAnthropicName: 'Anthropic Messages API',
    protoAnthropicCap: 'Content Block Delta SSE · tool_use / tool_result · Thinking · Ephemeral Cache',
    protoAnthropicNote:
      'Preserves message_start/delta/stop framing, cache_control breakpoints, and multi-turn tool blocks for Claude Code and Anthropic SDKs.',

    protoGeminiName: 'Google Gemini v1beta',
    protoGeminiCap: 'generateContent & streamGenerateContent · functionDeclarations · Multimodal',
    protoGeminiNote:
      'Supports native contents, systemInstruction, generationConfig, and inline/file multimodal inputs via shared project API keys.',

    protoMediaName: 'Embeddings, Rerank & Async Media',
    protoMediaCap: 'Sync Vectors/Rerank · Unified Async Image / Audio / Video Jobs',
    protoMediaNote:
      'Async tasks report honest stage badges when upstream omits percentage progress; unsupported task cancellation states explicit action_reasons.',

    pluginEyebrow: 'EXTENSION & STORAGE MIGRATION',
    pluginTitle: 'Plugin Format (.ohplugin v2 vs .s2plugin) & Namespace Window',
    pluginDesc:
      'OmniHive v2 standardizes extension packaging, host messaging, and browser storage keys to eliminate legacy namespace coupling.',
    pluginV2Badge: 'Current Standard · v2',
    pluginV2Title: 'omnihive.plugin.v2 (.ohplugin)',
    pluginV2Body:
      'Canonical signed archive format with v2 manifest schema, SHA-256 package digest verification, explicit host capability permissions (omnihive-plugin-host), and tenant-scoped storage keys (omnihive.<domain>.v1).',
    pluginV2Action: 'Required for all newly published plugins and production upgrades.',

    pluginV1Badge: 'Deprecated · Compatibility Bridge',
    pluginV1Title: 'Legacy .s2plugin (v1 Bridge)',
    pluginV1Body:
      'Accepted only through the transitional compatibility loader during the v1 -> v2 migration window. Legacy postMessage sources and un-scoped localStorage keys are deprecated and scheduled for removal.',
    pluginV1Action: 'Repackage existing extensions to .ohplugin with manifest_version: 2 before cutover.',

    releaseEyebrow: 'RELEASE SAFETY & ROLLBACK',
    releaseTitle: 'Release Checksums, Policy Versioning & Restore Guarantees',
    releaseCard1Title: 'Mandatory Checksum & Product Manifest Gate',
    releaseCard1Body:
      'In-place self-update, deploy/install.sh, and targeted version rollback verify SHA256SUMS and the OmniHive release manifest before unpacking. Missing checksums or mismatched product archives abort immediately.',
    releaseCard2Title: 'Immutable Routing & Pricing Version Rollback',
    releaseCard2Body:
      'Routing policies (policy_version) and rate publications (price_version) publish atomically after simulation diffing. Operators can roll back to any prior revision while in-flight ledger holds settle against their original version.',
    releaseCard3Title: 'Rehearsal-Gated Backup Restore & Route Aliases',
    releaseCard3Body:
      'Production database restore requires a verified isolated rehearsal_id and generation fencing. Public deep links (/docs, /help, /models -> /model-plaza, and /model-plaza/<provider>/<model>) remain stable across releases.',

    limitsEyebrow: 'KNOWN LIMITATIONS & HONEST TELEMETRY',
    limitsTitle: 'Current Operational Boundaries',
    limit1:
      'Sparse or freshly published models display Unobserved / Insufficient samples until live traffic meets the disclosure threshold—never fabricated 0 ms or 100% uptime.',
    limit2:
      'Compatible upstream connections without active capability probes require explicit model declaration rather than synthetic verification badges.',
    limit3:
      'Cross-protocol bridges reject provider-exclusive hosted tools when target upstreams cannot honor them, preventing silent behavioral drift.'
  },
  zh: {
    eyebrow: '更新与兼容性 · UPDATES & COMPATIBILITY',
    title: '协议兼容矩阵、插件迁移窗口与发布回滚保障',
    subtitle:
      '集中披露 OmniHive 网关线级 API 兼容范围、扩展包规范演进（.ohplugin v2 与 .s2plugin 兼容期）、发行版 SHA-256 强校验与无损回滚契约。未观测指标与不受支持的上游专有特性均诚实声明，绝不静默丢弃或伪造。',
    ctaDocs: '查看接入文档 →',
    ctaModels: '浏览模型广场',
    ctaStatus: '实时服务状态',

    matrixEyebrow: 'API 线级兼容矩阵',
    matrixTitle: '多协议网关兼容规格与边界',
    matrixDesc:
      '所有推理入口共享统一鉴权、预算预留与尝试控制器（attempt.Controller）。遇到目标上游无法等价承载的专有服务端工具时，网关显式返回 4xx 契约错误而非静默吞掉参数。',
    colProtocol: '协议面',
    colEndpoint: '规范端点路径',
    colMaturity: '成熟度',
    colCapabilities: '流式 · 工具 · 推理与缓存',
    colBoundary: '兼容性契约与已知边界',

    maturityStable: '稳定版 Stable',
    maturityBridge: '迁移兼容期',

    protoOpenAIChatName: 'OpenAI Chat Completions',
    protoOpenAIChatCap: 'SSE [DONE] · Function Tools · reasoning_effort · 多模态视觉/音频',
    protoOpenAIChatNote:
      '原生与跨协议桥接统一接入 attempt.Controller 故障切换预算；不兼容的服务端专有工具显式拒绝，绝不静默丢弃 tool_choice。',

    protoOpenAIResponsesName: 'OpenAI Responses API',
    protoOpenAIResponsesCap: '结构化事件流 SSE · Function/Hosted 工具项 · 推理摘要透传',
    protoOpenAIResponsesNote:
      '原生兼容 Codex CLI（wire_api = "responses"）与多轮 Agent 调用，响应生命周期具备唯一确定性账务终态。',

    protoAnthropicName: 'Anthropic Messages API',
    protoAnthropicCap: 'Content Block Delta 流 · tool_use / tool_result · 扩展思考 · 提示词缓存',
    protoAnthropicNote:
      '完整保留 message_start / content_block_delta / message_stop 帧结构与 cache_control 断点，无缝对接 Claude Code。',

    protoGeminiName: 'Google Gemini v1beta',
    protoGeminiCap: 'generateContent 与 streamGenerateContent · functionDeclarations · 多模态',
    protoGeminiNote:
      '共用标准项目级 API Key 鉴权，原生支持 contents、systemInstruction、generationConfig 与多模态内容块。',

    protoMediaName: '向量检索与统一异步媒体任务',
    protoMediaCap: 'Embeddings / Rerank · 图像 / 音频 / 视频异步任务中心',
    protoMediaNote:
      '上游未返回数值进度时诚实展示队列阶段徽标（不伪造百分比）；不支持取消的任务明确披露 action_reasons 原因。',

    pluginEyebrow: '插件规范与存储命名空间演进',
    pluginTitle: '.ohplugin v2 标准包与 .s2plugin 兼容迁移窗口',
    pluginDesc:
      'OmniHive v2 统一了插件包扩展名、宿主通信协议与本地非敏感缓存键前缀，彻底隔离多租户会话状态。',
    pluginV2Badge: '当前主标准 · v2',
    pluginV2Title: 'omnihive.plugin.v2 (.ohplugin)',
    pluginV2Body:
      '采用 v2 Manifest 清单、SHA-256 包摘要强校验、omnihive-plugin-host 受控通信桥与绑定工作空间/用户的 omnihive.<domain>.v1 存储键。',
    pluginV2Action: '所有新上架扩展与生产环境自动化部署均须使用 .ohplugin v2 格式。',

    pluginV1Badge: '已弃用 · 迁移窗口兼容',
    pluginV1Title: '旧版 .s2plugin（v1 兼容桥接）',
    pluginV1Body:
      '仅在 v1 → v2 过渡窗口内以显式兼容模式加载；旧版 postMessage source 与未分区缓存键已标记弃用，将在兼容窗口结束后下线。',
    pluginV1Action: '请在迁移窗口期内升级清单至 manifest_version: 2 并重新打包为 .ohplugin。',

    releaseEyebrow: '版本更新、校验和与回滚保障',
    releaseTitle: '发行制品完整性校验与零停机回滚契约',
    releaseCard1Title: '强制 SHA-256 校验和与产品清单核验',
    releaseCard1Body:
      '在线更新器、deploy/install.sh 与指定版本回滚均强制校验 SHA256SUMS 及 OmniHive 产品 Manifest；缺失校验和或非本产品归档一律阻断替换。',
    releaseCard2Title: '路由策略与费率快照原子版本回滚',
    releaseCard2Body:
      '路由策略（policy_version）与生效费率（price_version）均以不可变快照发布并附变更说明，支持一键回滚至历史版本，在途预留按原版本幂等结算。',
    releaseCard3Title: '演练门禁恢复与长期深链接兼容',
    releaseCard3Body:
      '生产备份恢复强制要求通过隔离演练（rehearsal_id）与代次围栏校验；公开路径（/docs、/help、/models → /model-plaza、/model-plaza/<provider>/<model>）跨版本保持可达。',

    limitsEyebrow: '已知限制与诚实遥测原则',
    limitsTitle: '当前已知边界说明',
    limit1:
      '公开状态页与模型广场对低样本模型统一标注「未观测 / 样本不足」，绝不回退或伪造为 0 ms 延迟或 99.99% 可用性。',
    limit2:
      '无主动探针的兼容协议上游连接需显式声明可用模型范围，不会生成伪造的自动探测通过凭证。',
    limit3:
      '跨协议桥接遇到目标供应商无法执行的专有托管工具时直接报错阻断，防止生产调用静默偏离预期行为。'
  }
} as const

type CompatKey = keyof typeof COMPAT_MESSAGES.en

function toMessageFunctions<T extends Record<string, string>>(dict: T) {
  return Object.fromEntries(
    Object.entries(dict).map(([k, v]) => [k, () => v])
  ) as Record<keyof T, () => string>
}

const { t, locale } = useI18n({
  useScope: 'local',
  messages: {
    en: toMessageFunctions(COMPAT_MESSAGES.en),
    zh: toMessageFunctions(COMPAT_MESSAGES.zh),
    'zh-CN': toMessageFunctions(COMPAT_MESSAGES.zh)
  }
})

function tr(key: CompatKey): string {
  const translated = t(key)
  if (translated && translated !== key) return translated
  const active = String(locale?.value || 'en').toLowerCase()
  const dict = active.startsWith('zh') ? COMPAT_MESSAGES.zh : COMPAT_MESSAGES.en
  return dict[key]
}

interface ProtocolMatrixRow {
  id: string
  nameKey: CompatKey
  endpoint: string
  maturityTone: UiTone
  maturityKey: CompatKey
  capabilitiesKey: CompatKey
  noteKey: CompatKey
}

const protocolRows: readonly ProtocolMatrixRow[] = [
  {
    id: 'openai-chat',
    nameKey: 'protoOpenAIChatName',
    endpoint: 'POST /v1/chat/completions',
    maturityTone: 'success',
    maturityKey: 'maturityStable',
    capabilitiesKey: 'protoOpenAIChatCap',
    noteKey: 'protoOpenAIChatNote'
  },
  {
    id: 'openai-responses',
    nameKey: 'protoOpenAIResponsesName',
    endpoint: 'POST /v1/responses',
    maturityTone: 'success',
    maturityKey: 'maturityStable',
    capabilitiesKey: 'protoOpenAIResponsesCap',
    noteKey: 'protoOpenAIResponsesNote'
  },
  {
    id: 'anthropic-messages',
    nameKey: 'protoAnthropicName',
    endpoint: 'POST /v1/messages',
    maturityTone: 'success',
    maturityKey: 'maturityStable',
    capabilitiesKey: 'protoAnthropicCap',
    noteKey: 'protoAnthropicNote'
  },
  {
    id: 'google-gemini',
    nameKey: 'protoGeminiName',
    endpoint: 'POST /v1beta/models/{model}:generateContent',
    maturityTone: 'success',
    maturityKey: 'maturityStable',
    capabilitiesKey: 'protoGeminiCap',
    noteKey: 'protoGeminiNote'
  },
  {
    id: 'retrieval-media',
    nameKey: 'protoMediaName',
    endpoint: 'POST /v1/embeddings · /v1/rerank · /v1/images · /v1/audio/*',
    maturityTone: 'success',
    maturityKey: 'maturityStable',
    capabilitiesKey: 'protoMediaCap',
    noteKey: 'protoMediaNote'
  }
]

const releaseCards = computed(() => [
  {
    id: 'checksum',
    code: 'SHA256SUMS + MANIFEST',
    title: tr('releaseCard1Title'),
    body: tr('releaseCard1Body')
  },
  {
    id: 'policy-rollback',
    code: 'policy_version · price_version',
    title: tr('releaseCard2Title'),
    body: tr('releaseCard2Body')
  },
  {
    id: 'restore-rehearsal',
    code: 'rehearsal_id · URL Aliases',
    title: tr('releaseCard3Title'),
    body: tr('releaseCard3Body')
  }
])

const limitationItems = computed(() => [tr('limit1'), tr('limit2'), tr('limit3')])
</script>

<template>
  <PublicShell>
    <div data-testid="public-compatibility-view" class="space-y-10">
      <header class="flex flex-wrap items-end justify-between gap-6 border-b border-oh-default pb-7">
        <div class="min-w-0 max-w-[74ch]">
          <p class="font-mono text-xs uppercase tracking-[0.18em] text-oh-accent-text">
            {{ tr('eyebrow') }}
          </p>
          <h1 class="mt-3 text-3xl font-semibold tracking-tight text-oh-primary sm:text-4xl">
            {{ tr('title') }}
          </h1>
          <p class="mt-3 text-sm leading-7 text-oh-secondary">
            {{ tr('subtitle') }}
          </p>
        </div>

        <div class="flex flex-wrap items-center gap-2.5">
          <RouterLink
            to="/docs"
            class="oh-focus-ring inline-flex h-9 items-center rounded-oh-md bg-oh-accent px-3.5 text-xs font-semibold text-oh-accent-ink transition-colors hover:bg-oh-accent-hover"
          >
            {{ tr('ctaDocs') }}
          </RouterLink>
          <RouterLink
            to="/model-plaza"
            class="oh-focus-ring inline-flex h-9 items-center rounded-oh-md border border-oh-default bg-oh-surface px-3.5 text-xs font-medium text-oh-primary transition-colors hover:border-oh-strong hover:bg-oh-hover"
          >
            {{ tr('ctaModels') }}
          </RouterLink>
          <RouterLink
            to="/status"
            class="oh-focus-ring inline-flex h-9 items-center rounded-oh-md border border-oh-default bg-oh-surface px-3.5 text-xs font-medium text-oh-secondary transition-colors hover:border-oh-strong hover:text-oh-primary"
          >
            {{ tr('ctaStatus') }}
          </RouterLink>
        </div>
      </header>

      <!-- 1. Wire Protocol Compatibility Matrix -->
      <section aria-labelledby="compat-matrix-heading" class="space-y-4">
        <div>
          <p class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">
            {{ tr('matrixEyebrow') }}
          </p>
          <h2 id="compat-matrix-heading" class="mt-1 text-xl font-semibold text-oh-primary">
            {{ tr('matrixTitle') }}
          </h2>
          <p class="mt-1.5 max-w-[78ch] text-sm text-oh-secondary">
            {{ tr('matrixDesc') }}
          </p>
        </div>

        <div
          class="oh-focus-ring overflow-x-auto rounded-oh-md border border-oh-default bg-oh-surface"
          tabindex="0"
          role="region"
          aria-labelledby="compat-matrix-heading"
        >
          <table class="w-full min-w-[760px] border-collapse text-left text-sm">
            <thead>
              <tr class="border-b border-oh-default bg-oh-subtle font-mono text-[11px] uppercase tracking-wider text-oh-muted">
                <th scope="col" class="px-4 py-3">{{ tr('colProtocol') }}</th>
                <th scope="col" class="px-4 py-3">{{ tr('colEndpoint') }}</th>
                <th scope="col" class="px-4 py-3">{{ tr('colMaturity') }}</th>
                <th scope="col" class="px-4 py-3">{{ tr('colCapabilities') }}</th>
                <th scope="col" class="px-4 py-3">{{ tr('colBoundary') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="row in protocolRows"
                :key="row.id"
                class="border-b border-oh-subtle align-top last:border-0 hover:bg-oh-hover"
              >
                <th scope="row" class="px-4 py-3.5 font-medium text-oh-primary">
                  {{ tr(row.nameKey) }}
                </th>
                <td class="px-4 py-3.5 font-mono text-xs text-oh-accent-text">
                  <code>{{ row.endpoint }}</code>
                </td>
                <td class="px-4 py-3.5">
                  <Badge :tone="row.maturityTone" variant="subtle">
                    {{ tr(row.maturityKey) }}
                  </Badge>
                </td>
                <td class="px-4 py-3.5 font-mono text-xs leading-6 text-oh-secondary">
                  {{ tr(row.capabilitiesKey) }}
                </td>
                <td class="max-w-md px-4 py-3.5 text-xs leading-6 text-oh-secondary">
                  {{ tr(row.noteKey) }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- 2. Plugin .ohplugin v2 vs .s2plugin Migration Window -->
      <section aria-labelledby="compat-plugin-heading" class="space-y-4">
        <div>
          <p class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">
            {{ tr('pluginEyebrow') }}
          </p>
          <h2 id="compat-plugin-heading" class="mt-1 text-xl font-semibold text-oh-primary">
            {{ tr('pluginTitle') }}
          </h2>
          <p class="mt-1.5 max-w-[78ch] text-sm text-oh-secondary">
            {{ tr('pluginDesc') }}
          </p>
        </div>

        <div class="grid gap-4 lg:grid-cols-2">
          <article class="rounded-oh-md border border-oh-accent-border bg-oh-surface p-5">
            <div class="flex flex-wrap items-center justify-between gap-2">
              <Badge tone="success" variant="subtle">{{ tr('pluginV2Badge') }}</Badge>
              <code class="font-mono text-xs text-oh-accent-text">.ohplugin · manifest v2</code>
            </div>
            <h3 class="mt-3 font-mono text-base font-semibold text-oh-primary">
              {{ tr('pluginV2Title') }}
            </h3>
            <p class="mt-2 text-sm leading-6 text-oh-secondary">
              {{ tr('pluginV2Body') }}
            </p>
            <p class="mt-3 border-t border-oh-subtle pt-2.5 font-mono text-xs text-oh-primary">
              {{ tr('pluginV2Action') }}
            </p>
          </article>

          <article class="rounded-oh-md border border-oh-default bg-oh-subtle p-5">
            <div class="flex flex-wrap items-center justify-between gap-2">
              <Badge tone="warning" variant="subtle">{{ tr('pluginV1Badge') }}</Badge>
              <code class="font-mono text-xs text-oh-muted">.s2plugin · legacy v1</code>
            </div>
            <h3 class="mt-3 font-mono text-base font-semibold text-oh-primary">
              {{ tr('pluginV1Title') }}
            </h3>
            <p class="mt-2 text-sm leading-6 text-oh-secondary">
              {{ tr('pluginV1Body') }}
            </p>
            <p class="mt-3 border-t border-oh-default pt-2.5 font-mono text-xs text-oh-secondary">
              {{ tr('pluginV1Action') }}
            </p>
          </article>
        </div>
      </section>

      <!-- 3. Release Integrity, Checksum & Rollback Guarantees -->
      <section aria-labelledby="compat-release-heading" class="space-y-4">
        <div>
          <p class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">
            {{ tr('releaseEyebrow') }}
          </p>
          <h2 id="compat-release-heading" class="mt-1 text-xl font-semibold text-oh-primary">
            {{ tr('releaseTitle') }}
          </h2>
        </div>

        <div class="grid gap-4 md:grid-cols-3">
          <article
            v-for="card in releaseCards"
            :key="card.id"
            class="flex flex-col justify-between rounded-oh-md border border-oh-default bg-oh-surface p-5"
          >
            <div>
              <code class="inline-block rounded-oh-xs border border-oh-subtle bg-oh-subtle px-2 py-0.5 font-mono text-[11px] text-oh-accent-text">
                {{ card.code }}
              </code>
              <h3 class="mt-3 text-sm font-semibold text-oh-primary">
                {{ card.title }}
              </h3>
              <p class="mt-2 text-xs leading-6 text-oh-secondary">
                {{ card.body }}
              </p>
            </div>
          </article>
        </div>
      </section>

      <!-- 4. Known Limitations -->
      <section
        aria-labelledby="compat-limits-heading"
        class="rounded-oh-md border border-oh-default bg-oh-surface p-5"
      >
        <p class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">
          {{ tr('limitsEyebrow') }}
        </p>
        <h2 id="compat-limits-heading" class="mt-1 text-base font-semibold text-oh-primary">
          {{ tr('limitsTitle') }}
        </h2>
        <ul class="mt-3 list-disc space-y-2 pl-5 text-xs leading-6 text-oh-secondary">
          <li v-for="(item, idx) in limitationItems" :key="idx">
            {{ item }}
          </li>
        </ul>
      </section>
    </div>
  </PublicShell>
</template>
