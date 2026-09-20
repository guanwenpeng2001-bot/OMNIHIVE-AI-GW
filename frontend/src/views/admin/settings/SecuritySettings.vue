<template>
  <div v-show="activeTab === 'security'" class="space-y-6">
    <!-- Registration Settings -->
    <SecurityRegistrationCard />

    <!-- API Key IP ACL Settings -->
    <SecurityApiKeyAclCard />

    <!-- Panel API Rate Limit Settings -->
    <SecurityPanelRateLimitCard />

    <!-- 人机验证 Settings -->
    <SecurityCaptchaCard />

    <!-- LinuxDo Connect OAuth 登录 -->
    <SecurityLinuxdoCard />

    <!-- GitHub / Google 邮箱快捷登录 -->
    <SecurityGithubOauthEnabledCard />

    <!-- WeChat Connect OAuth 登录 -->
    <SecurityWechatConnectCard />

    <!-- DingTalk Connect OAuth 登录 -->
    <SecurityDingtalkCard />

    <!-- Generic OIDC OAuth 登录 -->
    <SecurityOidcCard />
  </div>
</template>

<script setup lang="ts">
import SecurityOidcCard from "./SecurityOidcCard.vue";
import SecurityDingtalkCard from "./SecurityDingtalkCard.vue";
import SecurityWechatConnectCard from "./SecurityWechatConnectCard.vue";
import SecurityGithubOauthEnabledCard from "./SecurityGithubOauthEnabledCard.vue";
import SecurityLinuxdoCard from "./SecurityLinuxdoCard.vue";
import SecurityCaptchaCard from "./SecurityCaptchaCard.vue";
import SecurityPanelRateLimitCard from "./SecurityPanelRateLimitCard.vue";
import SecurityApiKeyAclCard from "./SecurityApiKeyAclCard.vue";
import SecurityRegistrationCard from "./SecurityRegistrationCard.vue";

import { useSettingsContext } from "./settingsContext";
const { activeTab } = useSettingsContext();
</script>
