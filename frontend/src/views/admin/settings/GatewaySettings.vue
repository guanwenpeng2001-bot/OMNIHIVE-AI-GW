<template>
  <div v-show="activeTab === 'gateway'" class="min-w-0 space-y-4 text-[var(--foreground)]">
    <!-- Claude Code Settings -->
    <GatewayClaudeCodeCard />

    <!-- Codex Settings -->
    <GatewayCodexClientCard />

    <!-- Upstream Billing Probe Settings -->
    <GatewayUpstreamBillingProbeCard />

    <!-- Ollama Cloud Usage Settings -->
    <GatewayOllamaCloudUsageCard />

    <!-- Gateway Scheduling Settings -->
    <GatewaySchedulingCard />

    <!-- Gateway Forwarding Behavior -->
    <GatewayForwardingCard />

    <!-- Web Search Emulation -->
    <GatewayWebSearchEmulationCard />

    <!-- Web Search Test Dialog -->
    <GatewayWebSearchTestDialog />

    <!-- Usage Records Settings -->
    <GatewayUsageRecordsCard />
  </div>
</template>

<script setup lang="ts">
import GatewayUsageRecordsCard from "./GatewayUsageRecordsCard.vue";
import GatewayWebSearchTestDialog from "./GatewayWebSearchTestDialog.vue";
import GatewayWebSearchEmulationCard from "./GatewayWebSearchEmulationCard.vue";
import GatewayForwardingCard from "./GatewayForwardingCard.vue";
import GatewaySchedulingCard from "./GatewaySchedulingCard.vue";
import GatewayOllamaCloudUsageCard from "./GatewayOllamaCloudUsageCard.vue";
import GatewayUpstreamBillingProbeCard from "./GatewayUpstreamBillingProbeCard.vue";
import GatewayCodexClientCard from "./GatewayCodexClientCard.vue";
import GatewayClaudeCodeCard from "./GatewayClaudeCodeCard.vue";

import { useSettingsContext } from "./settingsContext";
const { activeTab } = useSettingsContext();
</script>
