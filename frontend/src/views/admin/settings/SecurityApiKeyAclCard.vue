<template>
  <div class="card">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
        {{ t("admin.settings.apiKeyAcl.title") }}
      </h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        {{ t("admin.settings.apiKeyAcl.description") }}
      </p>
    </div>
    <div class="space-y-5 p-6">
      <div class="flex items-center justify-between gap-4">
        <div>
          <label class="font-medium text-gray-900 dark:text-white">
            {{ t("admin.settings.apiKeyAcl.trustForwardedIp") }}
          </label>
          <p class="text-sm text-gray-500 dark:text-gray-400">
            {{ t("admin.settings.apiKeyAcl.trustForwardedIpHint") }}
          </p>
        </div>
        <Toggle v-model="form.api_key_acl_trust_forwarded_ip" />
      </div>

      <div
        v-if="form.api_key_acl_trust_forwarded_ip"
        class="border-t border-gray-100 pt-4 dark:border-dark-700"
      >
        <label
          for="forwarded-client-ip-headers"
          class="font-medium text-gray-900 dark:text-white"
        >
          {{ t("admin.settings.apiKeyAcl.forwardedClientIpHeaders") }}
        </label>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
          {{ t("admin.settings.apiKeyAcl.forwardedClientIpHeadersHint") }}
        </p>
        <div
          class="mt-3 rounded-lg border border-gray-300 bg-white p-2 dark:border-dark-500 dark:bg-dark-700"
        >
          <div class="flex flex-wrap items-center gap-2">
            <span
              v-for="header in form.forwarded_client_ip_headers"
              :key="header"
              data-testid="forwarded-client-ip-header-tag"
              class="inline-flex items-center gap-1 rounded bg-gray-100 px-2 py-1 text-xs font-mono text-gray-700 dark:bg-dark-600 dark:text-gray-200"
            >
              <span>{{ header }}</span>
              <button
                type="button"
                class="rounded-full text-gray-500 hover:bg-gray-200 hover:text-gray-700 dark:text-gray-300 dark:hover:bg-dark-500 dark:hover:text-white"
                :aria-label="
                  t('admin.settings.apiKeyAcl.removeForwardedClientIpHeader', {
                    header,
                  })
                "
                @click="removeForwardedClientIpHeader(header)"
              >
                <Icon
                  name="x"
                  size="xs"
                  class="h-3.5 w-3.5"
                  :stroke-width="2"
                />
              </button>
            </span>
            <div
              class="flex min-w-[220px] flex-1 items-center gap-1 rounded border border-transparent px-2 py-1 focus-within:border-primary-300 dark:focus-within:border-primary-700"
            >
              <input
                id="forwarded-client-ip-headers"
                v-model="forwardedClientIpHeaderDraft"
                data-testid="forwarded-client-ip-headers-input"
                type="text"
                class="w-full bg-transparent text-sm font-mono text-gray-900 outline-none placeholder:text-gray-400 dark:text-white dark:placeholder:text-gray-500"
                :placeholder="
                  t(
                    'admin.settings.apiKeyAcl.forwardedClientIpHeadersPlaceholder',
                  )
                "
                @keydown="handleForwardedClientIpHeaderKeydown"
                @blur="commitForwardedClientIpHeaderDraft"
                @paste="handleForwardedClientIpHeaderPaste"
              />
            </div>
          </div>
        </div>
        <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
          {{ t("admin.settings.apiKeyAcl.forwardedClientIpHeadersRiskHint") }}
        </p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import Icon from "@/components/icons/Icon.vue";
import Toggle from "@/components/common/Toggle.vue";
import { useSettingsContext } from "./settingsContext";
const {
  t,
  forwardedClientIpHeaderDraft,
  removeForwardedClientIpHeader,
  commitForwardedClientIpHeaderDraft,
  handleForwardedClientIpHeaderKeydown,
  handleForwardedClientIpHeaderPaste,
  form,
} = useSettingsContext();
</script>
