<template>
  <div class="card border-[var(--border)] bg-[var(--card)] shadow-none">
    <div class="border-b border-[var(--border)] px-4 py-4 sm:px-6">
      <h2 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
        {{ t("admin.settings.apiKeyAcl.title") }}
      </h2>
      <p class="mt-1 text-sm text-[var(--muted-foreground)]">
        {{ t("admin.settings.apiKeyAcl.description") }}
      </p>
    </div>
    <div class="space-y-5 p-4 sm:p-6">
      <div class="flex items-center justify-between gap-4">
        <div>
          <label class="font-medium text-[var(--foreground)]">
            {{ t("admin.settings.apiKeyAcl.trustForwardedIp") }}
          </label>
          <p class="text-sm text-[var(--muted-foreground)]">
            {{ t("admin.settings.apiKeyAcl.trustForwardedIpHint") }}
          </p>
        </div>
        <Toggle v-model="form.api_key_acl_trust_forwarded_ip" />
      </div>

      <div
        v-if="form.api_key_acl_trust_forwarded_ip"
        class="border-t border-[var(--border)] pt-4"
      >
        <label
          for="forwarded-client-ip-headers"
          class="font-medium text-[var(--foreground)]"
        >
          {{ t("admin.settings.apiKeyAcl.forwardedClientIpHeaders") }}
        </label>
        <p class="mt-1 text-sm text-[var(--muted-foreground)]">
          {{ t("admin.settings.apiKeyAcl.forwardedClientIpHeadersHint") }}
        </p>
        <div
          class="mt-3 rounded-lg border border-[var(--border)] bg-[var(--card)] p-2"
        >
          <div class="flex flex-wrap items-center gap-2">
            <span
              v-for="header in form.forwarded_client_ip_headers"
              :key="header"
              data-testid="forwarded-client-ip-header-tag"
              class="inline-flex items-center gap-1 rounded bg-[var(--surface-2)] px-2 py-1 text-xs font-mono text-[var(--foreground)]"
            >
              <span>{{ header }}</span>
              <button
                type="button"
                class="rounded-full text-[var(--muted-foreground)] hover:bg-[var(--border)] hover:text-[var(--foreground)]"
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
              class="flex min-w-[220px] flex-1 items-center gap-1 rounded border border-transparent px-2 py-1 focus-within:border-[var(--border)] dark:focus-within:border-[var(--border)]"
            >
              <input
                id="forwarded-client-ip-headers"
                v-model="forwardedClientIpHeaderDraft"
                data-testid="forwarded-client-ip-headers-input"
                type="text"
                class="w-full bg-transparent text-sm font-mono text-[var(--foreground)] outline-none placeholder:text-[var(--muted-foreground)] dark:placeholder:text-[var(--muted-foreground)]"
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
        <p class="mt-2 text-xs text-[var(--muted-foreground)]">
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
