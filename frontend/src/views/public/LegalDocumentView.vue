<template>
  <div class="legal-portal min-h-screen bg-[var(--background)] text-[var(--foreground)]">
    <header class="glass-bar sticky top-0 z-20 border-b border-[var(--border)] ">
      <div class="mx-auto flex max-w-5xl items-center justify-between gap-4 px-4 py-4 sm:px-6">
        <RouterLink to="/home" class="flex min-w-0 items-center gap-2.5" :data-logo="Boolean(siteLogo)" :title="siteName">
          <template v-if="settings">
            <span class="flex h-9 w-9 flex-shrink-0 items-center justify-center overflow-hidden rounded-xl border border-amber-500/25 bg-[var(--card)] shadow-sm ">
              <img v-if="siteLogo" :src="siteLogo" alt="Logo" class="h-full w-full object-contain" />
              <svg v-else width="22" height="22" viewBox="0 0 32 32" fill="none" aria-hidden="true">
                <path d="M16 2 28.1 9v14L16 30 3.9 23V9Z" fill="var(--primary, #16161a)" />
                <path d="M16 5.6 25 10.8v10.4L16 26.4 7 21.2V10.8Z" stroke="var(--brand, #f59e0b)" stroke-width="1.8" />
                <path d="M16 10.2 21 13.1v5.8L16 21.8l-5-2.9v-5.8Z" fill="var(--brand, #f59e0b)" />
                <circle cx="16" cy="16" r="2.1" fill="var(--primary, #16161a)" />
              </svg>
            </span>
            <span class="truncate text-base font-semibold text-[var(--foreground)] ">
              OmniHive
            </span>
          </template>
          <template v-else>
            <span class="h-10 w-10 flex-shrink-0 animate-pulse rounded-xl bg-[var(--surface-2)] " aria-hidden="true"></span>
            <span class="h-5 w-28 animate-pulse rounded bg-[var(--surface-2)] " aria-hidden="true"></span>
          </template>
        </RouterLink>
        <RouterLink
          to="/login"
          class="btn btn-primary flex-shrink-0"
        >
          {{ t('home.login') }}
        </RouterLink>
      </div>
    </header>

    <main class="mx-auto max-w-4xl px-4 py-8 sm:px-6 lg:py-10">
      <div v-if="loading" class="flex min-h-[320px] items-center justify-center">
        <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-[var(--brand)]"></div>
      </div>

      <section
        v-else-if="loadError && !currentDocument"
        class="rounded-lg border border-red-200 bg-red-50 p-6 text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200"
      >
        <h1 class="text-lg font-semibold">{{ t('legal.loadFailed') }}</h1>
        <p class="mt-2 text-sm">{{ t('legal.retryLater') }}</p>
      </section>

      <section
        v-else-if="!currentDocument"
        class="rounded-lg border border-[var(--border)] bg-[var(--card)] p-6  "
      >
        <div class="flex items-start gap-3">
          <span class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-md bg-[var(--surface-2)] text-[var(--muted-foreground)]  ">
            <Icon name="document" size="sm" />
          </span>
          <div>
            <h1 class="text-lg font-semibold text-[var(--foreground)] ">{{ t('legal.notFound') }}</h1>
            <p class="mt-2 text-sm leading-6 text-[var(--muted-foreground)] ">
              {{ t('legal.notFoundDescription') }}
            </p>
          </div>
        </div>
      </section>

      <article v-else class="rounded-xl border border-[var(--border)] bg-[var(--card)] p-5 sm:p-10">
        <div class="mb-8 border-b border-[var(--border)] pb-6 ">
          <div class="flex items-start gap-4">
            <span class="flex h-12 w-12 flex-shrink-0 items-center justify-center rounded-md bg-[var(--surface-2)] text-[var(--brand-text)]">
              <Icon :name="documentIcon" size="md" />
            </span>
            <div class="min-w-0">
              <p class="text-sm font-medium text-[var(--brand-text)]">{{ documentTypeLabel }}</p>
              <h1 class="mt-2 break-words text-2xl font-bold tracking-normal text-[var(--foreground)]  sm:text-3xl">
                {{ currentDocument.title.replace(/sub2api/gi, 'OmniHive') }}
              </h1>
              <p v-if="updatedAt" class="mt-3 text-sm text-[var(--muted-foreground)] ">
                {{ t('legal.updatedAt', { date: updatedAt }) }}
              </p>
            </div>
          </div>
        </div>

        <div
          v-if="hasContent"
          class="legal-document-content"
          v-html="renderedHtml"
        ></div>
        <div
          v-else
          class="rounded-lg border border-dashed border-[var(--border)] bg-[var(--card)] px-6 py-14 text-center text-sm text-[var(--muted-foreground)]   "
        >
          {{ t('legal.empty') }}
        </div>
      </article>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { getLocale } from '@/i18n'
import { sanitizeUrl } from '@/utils/url'
import { useAppStore } from '@/stores/app'
import type { LoginAgreementDocument } from '@/types'
import zhAdminCompliance from '../../../../docs/legal/admin-compliance.zh.md?raw'
import enAdminCompliance from '../../../../docs/legal/admin-compliance.en.md?raw'

type LegalDocumentIcon = 'document' | 'shield' | 'globe' | 'cog'

const route = useRoute()
const { t } = useI18n()
const appStore = useAppStore()
const settings = computed(() => appStore.cachedPublicSettings)
const loading = ref(!settings.value)
const loadError = ref(false)

marked.setOptions({
  breaks: true,
  gfm: true,
})

const documentId = computed(() => String(route.params.documentId || ''))
const isAdminComplianceDocument = computed(() => documentId.value === 'admin-compliance')
const documents = computed(() => settings.value?.login_agreement_documents ?? [])
const siteName = computed(() => (settings.value?.site_name || 'OmniHive').replace(/sub2api/gi, 'OmniHive'))
const siteLogo = computed(() => sanitizeUrl(settings.value?.site_logo || '', {
  allowRelative: true,
  allowDataUrl: true,
}))
const updatedAt = computed(() =>
  isAdminComplianceDocument.value ? '' : settings.value?.login_agreement_updated_at || ''
)
const documentTypeLabel = computed(() =>
  isAdminComplianceDocument.value ? t('legal.adminCompliance') : t('legal.loginAgreement')
)

const currentDocument = computed<LoginAgreementDocument | null>(() => {
  if (isAdminComplianceDocument.value) {
    return {
      id: 'admin-compliance',
      title: t('adminCompliance.title'),
      content_md: getLocale() === 'zh' ? zhAdminCompliance : enAdminCompliance
    }
  }
  const id = documentId.value
  if (!id) {
    return null
  }
  const configuredDocument = documents.value.find((doc) => doc.id === id)
  if (configuredDocument && (configuredDocument.content_md?.trim() || (id !== 'terms' && id !== 'privacy'))) {
    return configuredDocument
  }
  const chinese = getLocale() === 'zh'
  if (id === 'terms') {
    return {
      id,
      title: configuredDocument?.title?.trim() || (chinese ? 'OmniHive 服务条款' : 'OmniHive Terms of Service'),
      content_md: chinese
        ? `## 服务范围
OmniHive 提供账户管理、模型接口访问、用量查询及相关网关功能。具体可用功能、模型与额度以当前站点展示和账户权限为准。

## 账户与凭证
请提供准确的注册信息，妥善保管密码与 API 密钥，并对账户下的操作负责。发现凭证泄露或未经授权的使用时，请及时更新凭证并联系站点管理员。

## 使用要求
请遵守适用法律及所选模型提供方的使用要求。不得利用服务实施未经授权的访问、滥用接口或侵害他人权益。请仅提交您有权处理的内容，并核验模型输出后再使用。

## 费用与额度
使用付费功能前，请核对站点展示的价格、计费规则、额度与有效期。订单、退款及账单疑问请通过站点提供的支持渠道联系管理员处理。

## 第三方服务与可用性
模型请求可能交由您所选的上游服务处理，其可用性和输出受该服务影响。维护、容量限制或上游故障可能导致请求失败；请根据实际返回结果决定是否重试。

## 条款更新与联系
请在使用前阅读站点公布的条款及更新说明。对服务范围、账户处理或条款有疑问时，请联系当前站点的运营方。`
        : `## Service scope
OmniHive provides account management, model API access, usage information, and related gateway features. Available features, models, and quotas depend on this site's settings and your account permissions.

## Accounts and credentials
Provide accurate registration information, protect your password and API keys, and take responsibility for activity under your account. If credentials are exposed or used without authorization, update them and contact the site administrator.

## Acceptable use
Follow applicable laws and the usage requirements of your selected model provider. Do not use the service for unauthorized access, API abuse, or infringement of others' rights. Submit only content you are authorized to process and review model outputs before using them.

## Charges and quotas
Before using paid features, review the site's displayed prices, billing rules, quotas, and validity periods. Contact the administrator through the site's support channels for orders, refunds, or billing questions.

## Third-party services and availability
Model requests may be processed by your selected upstream provider. Availability and outputs depend on that service. Maintenance, capacity limits, or upstream failures may interrupt requests; review actual responses before retrying.

## Updates and contact
Read the terms and update notices published by this site before use. Contact the operator of this site with questions about service scope, account handling, or these terms.`
    }
  }
  if (id === 'privacy') {
    return {
      id,
      title: configuredDocument?.title?.trim() || (chinese ? 'OmniHive 隐私政策' : 'OmniHive Privacy Policy'),
      content_md: chinese
        ? `## 信息范围
注册、登录和使用 OmniHive 涉及您提供的邮箱、用户名等账户信息，以及验证信息、接口请求、用量和订单记录。实际处理的信息取决于您使用的功能和当前站点配置。

## 信息用途
相关信息用于账户验证、提供模型服务、统计用量、计费、排查故障和保障服务安全。请避免在请求中提交与任务无关的个人信息或敏感凭证。

## 服务提供方
模型请求可能发送至所选上游提供方；启用第三方登录、验证或支付功能时，相应服务也会参与处理必要信息。使用前请阅读相关提供方的隐私说明。

## 本地存储
浏览器本地存储及会话存储用于登录状态、界面偏好、协议确认及注册流程等功能。您可以通过浏览器设置清除这些数据；清除后可能需要重新登录或填写信息。

## 保存与管理
具体日志范围、保存期限、部署地区及访问权限由当前站点的运营配置决定。请向站点运营方了解这些安排，并通过其支持渠道提出信息查询、更正或删除请求。

## 联系与更新
如有隐私或账户安全问题，请联系当前站点管理员，并提供处理请求所需的最少信息。请同时关注站点公布的隐私政策及更新说明。`
        : `## Information involved
Registration, sign-in, and use of OmniHive involve account information you provide, such as email and username, along with verification information, API requests, usage, and order records. The information processed depends on the features you use and this site's configuration.

## Purposes
This information supports account verification, model services, usage measurement, billing, troubleshooting, and service security. Avoid submitting personal information or sensitive credentials unrelated to your task.

## Service providers
Model requests may be sent to the selected upstream provider. When third-party sign-in, verification, or payment features are enabled, those services also process necessary information. Review their privacy information before use.

## Browser storage
Local and session storage support sign-in state, interface preferences, agreement acceptance, and registration workflows. You can clear this data in your browser settings; you may then need to sign in or enter information again.

## Retention and management
Logging scope, retention periods, deployment regions, and access permissions depend on the operator's configuration. Contact the site operator for details and use its support channels to request access, correction, or deletion.

## Contact and updates
Contact the site administrator about privacy or account security, providing only the information needed to address your request. Review the privacy policy and update notices published by this site.`
    }
  }
  return null
})

const hasContent = computed(() => Boolean(currentDocument.value?.content_md?.trim()))

const renderedHtml = computed(() => {
  const content = currentDocument.value?.content_md?.trim() || ''
  if (!content) {
    return ''
  }
  const html = marked.parse(content) as string
  const fragment = DOMPurify.sanitize(html, { RETURN_DOM_FRAGMENT: true })
  const walker = document.createTreeWalker(fragment, NodeFilter.SHOW_TEXT)
  while (walker.nextNode()) {
    walker.currentNode.textContent = walker.currentNode.textContent?.replace(/sub2api/gi, 'OmniHive') ?? ''
  }
  const container = document.createElement('div')
  container.append(fragment)
  return container.innerHTML
})

const documentIcon = computed<LegalDocumentIcon>(() => {
  const title = currentDocument.value?.title || ''
  if (/privacy|policy|政策|隐私/i.test(title)) {
    return 'shield'
  }
  if (title.includes('国家') || title.includes('地区')) {
    return 'globe'
  }
  if (title.includes('特定')) {
    return 'cog'
  }
  return 'document'
})

onMounted(async () => {
  loadError.value = false
  try {
    const loadedSettings = await appStore.fetchPublicSettings()
    loadError.value = !loadedSettings
  } catch {
    loadError.value = true
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.legal-document-content {
  line-height: 1.75;
  overflow-wrap: anywhere;
  color: inherit;
}

.legal-document-content :deep(h1) {
  @apply mb-4 mt-8 border-b border-[var(--border)] pb-3 text-3xl font-bold ;
}

.legal-document-content :deep(h2) {
  @apply mb-3 mt-7 text-2xl font-bold;
}

.legal-document-content :deep(h3) {
  @apply mb-2 mt-6 text-xl font-semibold;
}

.legal-document-content :deep(h4) {
  @apply mb-2 mt-5 text-lg font-semibold;
}

.legal-document-content :deep(p) {
  @apply mb-4 text-[var(--foreground)] ;
}

.legal-document-content :deep(a) {
  @apply text-[var(--brand-text)] underline underline-offset-4;
}

.legal-document-content :deep(ul) {
  @apply mb-4 list-disc pl-6;
}

.legal-document-content :deep(ol) {
  @apply mb-4 list-decimal pl-6;
}

.legal-document-content :deep(li) {
  @apply mb-1 text-[var(--foreground)] ;
}

.legal-document-content :deep(blockquote) {
  @apply my-5 border-l-4 border-[var(--border)] pl-4 text-[var(--muted-foreground)]  ;
}

.legal-document-content :deep(code) {
  @apply rounded bg-[var(--surface-2)] px-1.5 py-0.5 font-mono text-sm ;
}

.legal-document-content :deep(pre) {
  @apply my-5 overflow-x-auto rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-4 text-[var(--foreground)];
}

.legal-document-content :deep(pre code) {
  @apply bg-transparent p-0 text-inherit;
}

.legal-document-content :deep(table) {
  @apply my-5 block w-full overflow-x-auto border-collapse;
}

.legal-document-content :deep(th) {
  @apply border border-[var(--border)] bg-[var(--surface-2)] px-3 py-2 text-left font-semibold  ;
}

.legal-document-content :deep(td) {
  @apply border border-[var(--border)] px-3 py-2 ;
}

.legal-document-content :deep(img) {
  @apply my-5 h-auto max-w-full rounded-lg;
}

.legal-document-content :deep(hr) {
  @apply my-7 border-[var(--border)] ;
}
</style>
