<template>
  <div v-show="activeTab === 'gateway'" class="min-w-0 space-y-4 text-[var(--foreground)]">
    <!-- Overload Cooldown (529) Settings -->
    <GatewayRuntimeOverloadCooldownCard />

    <!-- Rate Limit Cooldown (429) Settings -->
    <GatewayRuntimeRateLimit429CooldownCard />

    <!-- Stream Timeout Settings -->
    <GatewayRuntimeStreamTimeoutCard />

    <!-- Request Rectifier Settings -->
    <GatewayRuntimeRectifierCard />
    <!-- Beta Policy Settings -->
    <GatewayRuntimeBetaPolicyCard />
    <!-- OpenAI Fast/Flex Policy Settings -->
    <GatewayRuntimeOpenaiFastPolicyCard />
  </div>
</template>

<script setup lang="ts">
import GatewayRuntimeOpenaiFastPolicyCard from "./GatewayRuntimeOpenaiFastPolicyCard.vue";
import GatewayRuntimeBetaPolicyCard from "./GatewayRuntimeBetaPolicyCard.vue";
import GatewayRuntimeRectifierCard from "./GatewayRuntimeRectifierCard.vue";
import GatewayRuntimeStreamTimeoutCard from "./GatewayRuntimeStreamTimeoutCard.vue";
import GatewayRuntimeRateLimit429CooldownCard from "./GatewayRuntimeRateLimit429CooldownCard.vue";
import GatewayRuntimeOverloadCooldownCard from "./GatewayRuntimeOverloadCooldownCard.vue";

import { useSettingsContext } from "./settingsContext";
const { activeTab } = useSettingsContext();
</script>
