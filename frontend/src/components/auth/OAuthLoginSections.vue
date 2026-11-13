<template>
  <div class="auth-oauth-grid" aria-label="第三方账户登录">
    <EmailOAuthButtons
      v-if="githubEnabled || googleEnabled"
      class="auth-email-providers"
      :disabled="disabled"
      :aff-code="affCode"
      :promo-code="promoCode"
      :github-enabled="githubEnabled"
      :google-enabled="googleEnabled"
      :show-divider="false"
      @start="emit('start', $event)"
    />
    <LinuxDoOAuthSection v-if="linuxdoEnabled" :disabled="disabled" :aff-code="affCode" :promo-code="promoCode" :show-divider="false" @start="emit('start', $event)" />
    <DingTalkOAuthSection v-if="dingtalkEnabled" :disabled="disabled" :aff-code="affCode" :show-divider="false" @start="emit('start', $event)" />
    <OidcOAuthSection v-if="oidcEnabled" :disabled="disabled" :aff-code="affCode" :provider-name="oidcProviderName" :show-divider="false" @start="emit('start', $event)" />
    <WechatOAuthSection v-if="wechatEnabled" :disabled="disabled" :aff-code="affCode" :show-divider="false" @start="emit('start', $event)" />
  </div>
</template>

<script setup lang="ts">
import EmailOAuthButtons from './EmailOAuthButtons.vue'
import LinuxDoOAuthSection from './LinuxDoOAuthSection.vue'
import DingTalkOAuthSection from './DingTalkOAuthSection.vue'
import OidcOAuthSection from './OidcOAuthSection.vue'
import WechatOAuthSection from './WechatOAuthSection.vue'
import type { OAuthLoginStart } from '@/api/auth'

withDefaults(defineProps<{
  disabled?: boolean
  affCode?: string
  promoCode?: string
  githubEnabled?: boolean
  googleEnabled?: boolean
  linuxdoEnabled?: boolean
  dingtalkEnabled?: boolean
  oidcEnabled?: boolean
  wechatEnabled?: boolean
  oidcProviderName?: string
}>(), { oidcProviderName: 'OIDC' })

const emit = defineEmits<{ start: [request: OAuthLoginStart] }>()
</script>

<style scoped>
.auth-oauth-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
.auth-oauth-grid :deep(.auth-email-providers), .auth-oauth-grid :deep(.auth-email-providers > .grid) { display: contents; }
.auth-oauth-grid :deep(.btn) { width: 100%; height: 100%; min-height: 40px; padding: 8px; margin: 0; border: 1px solid var(--border); border-radius: 8px; background: var(--card); color: var(--foreground); font-size: 12px; white-space: normal; box-shadow: none; }
.auth-oauth-grid :deep(.btn:hover:not(:disabled)) { background: var(--surface-2); }
.auth-oauth-grid :deep(.btn:focus-visible) { outline: 2px solid var(--ring); outline-offset: 3px; }
@media (min-width: 480px) { .auth-oauth-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); } }
</style>
