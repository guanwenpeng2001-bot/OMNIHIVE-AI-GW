<template>
  <div class="card border-[var(--border)] bg-[var(--card)] shadow-none">
    <div class="border-b border-[var(--border)] px-4 py-4 sm:px-6">
      <h2 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
        {{ t("admin.settings.linuxdo.title") }}
      </h2>
      <p class="mt-1 text-sm text-[var(--muted-foreground)]">
        {{ t("admin.settings.linuxdo.description") }}
      </p>
    </div>
    <div class="space-y-5 p-4 sm:p-6">
      <div class="flex items-center justify-between">
        <div>
          <label class="font-medium text-[var(--foreground)]">{{
            t("admin.settings.linuxdo.enable")
          }}</label>
          <p class="text-sm text-[var(--muted-foreground)]">
            {{ t("admin.settings.linuxdo.enableHint") }}
          </p>
        </div>
        <Toggle v-model="form.linuxdo_connect_enabled" />
      </div>

      <div
        v-if="form.linuxdo_connect_enabled"
        class="border-t border-[var(--border)] pt-4"
      >
        <div class="grid grid-cols-1 gap-6">
          <div>
            <label
              class="mb-2 block text-sm font-medium text-[var(--foreground)]"
            >
              {{ t("admin.settings.linuxdo.clientId") }}
            </label>
            <input
              v-model="form.linuxdo_connect_client_id"
              type="text"
              class="input font-mono text-sm"
              :placeholder="t('admin.settings.linuxdo.clientIdPlaceholder')"
            />
            <p class="mt-1.5 text-xs text-[var(--muted-foreground)]">
              {{ t("admin.settings.linuxdo.clientIdHint") }}
            </p>
          </div>

          <div>
            <label
              class="mb-2 block text-sm font-medium text-[var(--foreground)]"
            >
              {{ t("admin.settings.linuxdo.clientSecret") }}
            </label>
            <input
              v-model="form.linuxdo_connect_client_secret"
              type="password"
              class="input font-mono text-sm"
              :placeholder="
                form.linuxdo_connect_client_secret_configured
                  ? t(
                      'admin.settings.linuxdo.clientSecretConfiguredPlaceholder',
                    )
                  : t('admin.settings.linuxdo.clientSecretPlaceholder')
              "
            />
            <p class="mt-1.5 text-xs text-[var(--muted-foreground)]">
              {{
                form.linuxdo_connect_client_secret_configured
                  ? t("admin.settings.linuxdo.clientSecretConfiguredHint")
                  : t("admin.settings.linuxdo.clientSecretHint")
              }}
            </p>
          </div>

          <div>
            <label
              class="mb-2 block text-sm font-medium text-[var(--foreground)]"
            >
              {{ t("admin.settings.linuxdo.redirectUrl") }}
            </label>
            <input
              v-model="form.linuxdo_connect_redirect_url"
              type="url"
              class="input font-mono text-sm"
              :placeholder="t('admin.settings.linuxdo.redirectUrlPlaceholder')"
            />
            <div
              class="mt-2 flex flex-col gap-2 sm:flex-row sm:items-center sm:gap-3"
            >
              <button
                type="button"
                class="btn btn-secondary btn-sm w-fit"
                @click="setAndCopyLinuxdoRedirectUrl"
              >
                {{ t("admin.settings.linuxdo.quickSetCopy") }}
              </button>
              <code
                v-if="linuxdoRedirectUrlSuggestion"
                class="select-all break-all rounded bg-[var(--surface-2)] px-2 py-1 font-mono text-xs text-[var(--muted-foreground)]"
              >
                {{ linuxdoRedirectUrlSuggestion }}
              </code>
            </div>
            <p class="mt-1.5 text-xs text-[var(--muted-foreground)]">
              {{ t("admin.settings.linuxdo.redirectUrlHint") }}
            </p>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import Toggle from "@/components/common/Toggle.vue";
import { useSettingsContext } from "./settingsContext";
const { t, linuxdoRedirectUrlSuggestion, setAndCopyLinuxdoRedirectUrl, form } =
  useSettingsContext();
</script>
