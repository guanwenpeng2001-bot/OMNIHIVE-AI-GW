<template>
  <ProfileIdentityBindingsSection
    class="account-bindings-surface"
    :user="user"
    :linuxdo-enabled="linuxdoEnabled"
    :dingtalk-enabled="dingtalkEnabled"
    :oidc-enabled="oidcEnabled"
    :oidc-provider-name="oidcProviderName"
    :wechat-enabled="wechatEnabled"
    :wechat-open-enabled="wechatOpenEnabled"
    :wechat-mp-enabled="wechatMpEnabled"
  />
</template>

<script setup lang="ts">
import ProfileIdentityBindingsSection from '@/components/user/profile/ProfileIdentityBindingsSection.vue'
import type { User } from '@/types'

withDefaults(
  defineProps<{
    user: User | null
    linuxdoEnabled?: boolean
    dingtalkEnabled?: boolean
    oidcEnabled?: boolean
    oidcProviderName?: string
    wechatEnabled?: boolean
    wechatOpenEnabled?: boolean
    wechatMpEnabled?: boolean
  }>(),
  {
    linuxdoEnabled: false,
    dingtalkEnabled: false,
    oidcEnabled: false,
    oidcProviderName: 'OIDC',
    wechatEnabled: false,
    wechatOpenEnabled: undefined,
    wechatMpEnabled: undefined
  }
)
</script>

<style scoped>
.account-bindings-surface { border: 1px solid var(--border); background: var(--card); color: var(--foreground); border-radius: 8px; }
.account-bindings-surface :deep(button:focus-visible) { outline: 2px solid var(--brand); outline-offset: 3px; }
</style>
