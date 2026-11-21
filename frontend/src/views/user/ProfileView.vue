<template>
  <AppLayout>
    <div
      data-testid="profile-shell"
      class="w-full min-w-0 space-y-6"
    >
      <!-- 界面风格「账户与安全中心」工作台概览横幅 -->
      <section
        class="card oh-profile-banner relative overflow-hidden p-5 sm:p-6"
        aria-label="账户与安全中心概览"
      >
        <div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div class="min-w-0 space-y-2">
            <div class="flex flex-wrap items-center gap-2.5">
              <h1 class="text-xl font-bold tracking-tight text-[var(--foreground)] sm:text-2xl">
                账户与安全中心
              </h1>
              <span class="chip text-xs font-medium">
                <span
                  class="pulse-dot"
                  :class="user?.status === 'active' ? 'pulse-ok' : 'pulse-warn'"
                  aria-hidden="true"
                />
                <span>{{ user?.status === 'active' ? '安全状态正常' : '安全防护巡检' }}</span>
              </span>
              <span class="pill pill-ghost font-mono text-[11px] uppercase">
                {{ user?.role || 'user' }}
              </span>
            </div>
            <p class="text-xs text-[var(--muted-foreground)] sm:text-sm">
              集中管理身份资料、OAuth 绑定、账单时间线、密码凭证与多重身份验证（2FA / Passkey）
            </p>

            <!-- 安全状态徽章组 -->
            <div class="flex flex-wrap items-center gap-2 pt-1">
              <span class="chip text-xs">
                <span class="pulse-dot pulse-ok" aria-hidden="true" />
                <span>登录密码保护</span>
              </span>
              <span class="chip text-xs">
                <span
                  class="pulse-dot"
                  :class="userTotpEnabled ? 'pulse-ok' : 'pulse-warn'"
                  aria-hidden="true"
                />
                <span>TOTP 2FA · {{ userTotpEnabled ? '已开启' : '可配置' }}</span>
              </span>
              <span class="chip text-xs">
                <span
                  class="pulse-dot"
                  :class="passkeyEnabled ? 'pulse-ok' : 'pulse-warn'"
                  aria-hidden="true"
                />
                <span>Passkey · {{ passkeyEnabled ? '已就绪' : '按需启用' }}</span>
              </span>
              <span v-if="balanceLowNotifyEnabled" class="chip text-xs">
                <span
                  class="pulse-dot"
                  :class="(user?.balance_notify_enabled ?? true) ? 'pulse-ok' : 'pulse-warn'"
                  aria-hidden="true"
                />
                <span>余额预警 · {{ (user?.balance_notify_enabled ?? true) ? '开启' : '关闭' }}</span>
              </span>
            </div>
          </div>

          <div
            v-if="user"
            class="flex shrink-0 items-center gap-4 rounded-xl border border-[var(--border)] bg-[var(--surface-2)] px-4 py-3"
          >
            <div>
              <div class="text-[11px] text-[var(--muted-foreground)]">当前可用余额</div>
              <div class="font-mono text-lg font-bold tabular-nums text-[var(--foreground)]">
                {{ typeof user.balance === 'number' && Number.isFinite(user.balance) ? `$${user.balance.toFixed(2)}` : '—' }}
              </div>
            </div>
            <div class="h-7 w-px bg-[var(--border)]" aria-hidden="true" />
            <div>
              <div class="text-[11px] text-[var(--muted-foreground)]">并发配额</div>
              <div class="font-mono text-sm font-semibold tabular-nums text-amber-700 dark:text-amber-400">
                {{ typeof user.concurrency === 'number' && Number.isFinite(user.concurrency) ? `${user.concurrency} 路` : '—' }}
              </div>
            </div>
          </div>
        </div>

        <!-- 快捷锚点导航条 -->
        <nav
          class="mt-4 flex flex-wrap items-center gap-1.5 border-t border-[var(--border)] pt-3.5"
          aria-label="账户与安全分区快捷导航"
        >
          <span class="mr-1 font-mono text-[11px] uppercase tracking-wider text-[var(--muted-foreground)]">
            JUMP TO
          </span>
          <a
            v-for="anchor in sectionAnchors"
            :key="anchor.id"
            :href="`#${anchor.id}`"
            class="oh-anchor-pill oh-focus-ring"
          >
            <span class="font-mono text-[10px] opacity-60">#</span>
            <span>{{ anchor.label }}</span>
          </a>
        </nav>
      </section>

      <div id="profile-section-info" class="scroll-mt-20">
        <ProfileInfoCard
          :user="user"
          :linuxdo-enabled="linuxdoOAuthEnabled"
          :dingtalk-enabled="dingtalkOAuthEnabled"
          :oidc-enabled="oidcOAuthEnabled"
          :oidc-provider-name="oidcOAuthProviderName"
          :wechat-enabled="wechatOAuthEnabled"
          :wechat-open-enabled="wechatOAuthOpenEnabled"
          :wechat-mp-enabled="wechatOAuthMPEnabled"
        />
      </div>

      <HumanSupportCard :contact-info="contactInfo" />

      <div id="profile-section-billing" class="scroll-mt-20">
        <ProfileBillingTimelineCard />
      </div>

      <div id="profile-section-password" class="scroll-mt-20">
        <ProfilePasswordForm />
      </div>

      <div
        v-if="user && balanceLowNotifyEnabled"
        id="profile-section-notify"
        class="scroll-mt-20"
      >
        <ProfileBalanceNotifyCard
          :enabled="user.balance_notify_enabled ?? true"
          :threshold="user.balance_notify_threshold"
          :extra-emails="user.balance_notify_extra_emails ?? []"
          :system-default-threshold="systemDefaultThreshold"
          :user-email="user.email"
        />
      </div>

      <div id="profile-section-totp" class="scroll-mt-20">
        <ProfileTotpCard />
      </div>
      <div id="profile-section-sessions" class="scroll-mt-20">
        <ProfileSessions />
      </div>
      <div id="profile-section-passkey" class="scroll-mt-20">
        <ProfilePasskeyCard :enabled="passkeyEnabled" />
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import HumanSupportCard from '@/components/intro/HumanSupportCard.vue'
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Icon } from '@/components/icons'
import AppLayout from '@/components/layout/AppLayout.vue'
import ProfileBalanceNotifyCard from '@/components/user/profile/ProfileBalanceNotifyCard.vue'
import ProfileBillingTimelineCard from '@/components/user/profile/ProfileBillingTimelineCard.vue'
import ProfileInfoCard from '@/components/user/profile/ProfileInfoCard.vue'
import ProfilePasswordForm from '@/components/user/profile/ProfilePasswordForm.vue'
import ProfileTotpCard from '@/components/user/profile/ProfileTotpCard.vue'
import ProfileSessions from '@/features/operations/ProfileSessions.vue'
import ProfilePasskeyCard from '@/components/user/profile/ProfilePasskeyCard.vue'
import { isWeChatWebOAuthEnabled } from '@/api/auth'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const user = computed(() => authStore.user)
const userTotpEnabled = computed(() =>
  Boolean((user.value as Record<string, unknown> | null)?.totp_enabled)
)

const contactInfo = ref('')
const balanceLowNotifyEnabled = ref(false)
const systemDefaultThreshold = ref(0)
const linuxdoOAuthEnabled = ref(false)
const dingtalkOAuthEnabled = ref(false)
const wechatOAuthEnabled = ref(false)
const wechatOAuthOpenEnabled = ref<boolean | undefined>(undefined)
const wechatOAuthMPEnabled = ref<boolean | undefined>(undefined)
const oidcOAuthEnabled = ref(false)
const oidcOAuthProviderName = ref('OIDC')
const passkeyEnabled = ref(false)

const sectionAnchors = computed(() => {
  const items = [
    { id: 'profile-section-info', label: '基本资料' },
    { id: 'profile-section-billing', label: '账单时间线' },
    { id: 'profile-section-password', label: '修改密码' },
  ]
  if (user.value && balanceLowNotifyEnabled.value) {
    items.push({ id: 'profile-section-notify', label: '余额预警' })
  }
  items.push(
    { id: 'profile-section-totp', label: '两步验证 (2FA)' },
    { id: 'profile-section-sessions', label: '登录会话' },
    { id: 'profile-section-passkey', label: 'Passkey 通行密钥' }
  )
  return items
})

onMounted(async () => {
  const profileRefresh = authStore.refreshUser().catch((error) => {
    console.error('Failed to refresh profile:', error)
  })

  const settingsLoad = appStore.fetchPublicSettings()
    .then((settings) => {
      if (!settings) {
        return
      }
      contactInfo.value = settings.contact_info || ''
      balanceLowNotifyEnabled.value = settings.balance_low_notify_enabled ?? false
      systemDefaultThreshold.value = settings.balance_low_notify_threshold ?? 0
      linuxdoOAuthEnabled.value = settings.linuxdo_oauth_enabled ?? false
      dingtalkOAuthEnabled.value = settings.dingtalk_oauth_enabled ?? false
      wechatOAuthEnabled.value = isWeChatWebOAuthEnabled(settings)
      wechatOAuthOpenEnabled.value = typeof settings.wechat_oauth_open_enabled === 'boolean'
        ? settings.wechat_oauth_open_enabled
        : undefined
      wechatOAuthMPEnabled.value = typeof settings.wechat_oauth_mp_enabled === 'boolean'
        ? settings.wechat_oauth_mp_enabled
        : undefined
      oidcOAuthEnabled.value = settings.oidc_oauth_enabled ?? false
      oidcOAuthProviderName.value = settings.oidc_oauth_provider_name || 'OIDC'
      passkeyEnabled.value = settings.passkey_enabled === true
    })
    .catch((error) => {
      console.error('Failed to load settings:', error)
    })

  await Promise.all([profileRefresh, settingsLoad])
})
</script>

<style scoped>
.oh-profile-banner {
  background:
    radial-gradient(460px circle at 100% 0%, color-mix(in srgb, var(--brand, #f59e0b) 10%, transparent), transparent 70%),
    var(--card, #ffffff);
}

.oh-anchor-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 10px;
  border-radius: 7px;
  font-size: 12px;
  font-weight: 500;
  color: var(--muted-foreground, #6f6f78);
  background: var(--surface-2, #f4f4f5);
  border: 1px solid var(--border, #e9e9ee);
  transition: all 0.16s var(--ease, ease);
}

.oh-anchor-pill:hover {
  color: var(--brand-text, #92610a);
  border-color: color-mix(in srgb, var(--brand, #f59e0b) 45%, var(--border, #e9e9ee));
  background: color-mix(in srgb, var(--brand, #f59e0b) 10%, var(--card, #ffffff));
}

.oh-amber-support-card {
  border-color: color-mix(in srgb, var(--brand, #f59e0b) 42%, var(--border, #e9e9ee));
  background:
    radial-gradient(360px circle at 0% 50%, color-mix(in srgb, var(--brand, #f59e0b) 16%, transparent), transparent 75%),
    color-mix(in srgb, var(--brand, #f59e0b) 7%, var(--card, #ffffff));
}

.oh-amber-icon-box {
  background: color-mix(in srgb, var(--brand, #f59e0b) 18%, var(--card, #ffffff));
  color: var(--brand-text, #92610a);
  border: 1px solid color-mix(in srgb, var(--brand, #f59e0b) 35%, transparent);
}
</style>
