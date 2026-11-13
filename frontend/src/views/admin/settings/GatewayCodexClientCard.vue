<template>
  <div class="card border-[var(--border)] bg-[var(--card)] shadow-none">
    <div class="border-b border-[var(--border)] px-4 py-4 sm:px-6">
      <h2 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
        {{ t("admin.settings.gatewayForwarding.codexHardeningTitle") }}
      </h2>
    </div>
    <div class="p-6 space-y-4">
      <div>
        <h3 class="text-base font-semibold text-[var(--foreground)]">
          {{
            t("admin.settings.gatewayForwarding.codexClientRestrictionTitle")
          }}
        </h3>
        <p class="mt-1 text-sm text-[var(--muted-foreground)]">
          {{ t("admin.settings.gatewayForwarding.codexHardeningDesc") }}
        </p>
      </div>
      <div class="grid gap-4 sm:grid-cols-2">
        <div>
          <label
            class="mb-2 block text-sm font-medium text-[var(--foreground)]"
          >
            {{ t("admin.settings.gatewayForwarding.minCodexVersion") }}
          </label>
          <input
            v-model="form.min_codex_version"
            type="text"
            class="input w-full font-mono text-sm"
            :placeholder="
              t('admin.settings.gatewayForwarding.minCodexVersionPlaceholder')
            "
          />
        </div>
        <div>
          <label
            class="mb-2 block text-sm font-medium text-[var(--foreground)]"
          >
            {{ t("admin.settings.gatewayForwarding.maxCodexVersion") }}
          </label>
          <input
            v-model="form.max_codex_version"
            type="text"
            class="input w-full font-mono text-sm"
            :placeholder="
              t('admin.settings.gatewayForwarding.maxCodexVersionPlaceholder')
            "
          />
        </div>
      </div>
      <p class="text-xs text-[var(--muted-foreground)]">
        {{ t("admin.settings.gatewayForwarding.codexVersionHint") }}
      </p>

      <div>
        <label
          class="block text-sm font-medium text-[var(--foreground)]"
        >
          {{ t("admin.settings.gatewayForwarding.codexFingerprintSignals") }}
        </label>
        <p class="mb-2 mt-1 text-xs text-[var(--muted-foreground)]">
          {{
            t("admin.settings.gatewayForwarding.codexFingerprintSignalsDesc")
          }}
        </p>
        <div
          v-for="(row, i) in codexFingerprintRows"
          :key="`codex-fp-${i}`"
          class="mb-2 flex items-center gap-2"
        >
          <select v-model="row.type" class="input w-32 text-sm">
            <option value="header_exact">
              {{ t("admin.settings.gatewayForwarding.codexFpTypeHeaderExact") }}
            </option>
            <option value="header_prefix">
              {{
                t("admin.settings.gatewayForwarding.codexFpTypeHeaderPrefix")
              }}
            </option>
            <option value="body_path">
              {{ t("admin.settings.gatewayForwarding.codexFpTypeBodyPath") }}
            </option>
          </select>
          <input
            v-model="row.match"
            type="text"
            class="input flex-1 font-mono text-sm"
            :placeholder="
              t('admin.settings.gatewayForwarding.codexFpMatchPlaceholder')
            "
          />
          <label
            class="flex shrink-0 items-center gap-1 text-xs text-[var(--muted-foreground)]"
          >
            <input v-model="row.required" type="checkbox" />
            {{ t("admin.settings.gatewayForwarding.codexFpRequired") }}
          </label>
          <button
            type="button"
            class="btn btn-secondary btn-sm shrink-0 text-[var(--bad-text)] hover:text-[var(--bad-text)]"
            @click="removeCodexFingerprintRow(i)"
          >
            {{ t("admin.settings.gatewayForwarding.codexRemoveRow") }}
          </button>
        </div>
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          @click="addCodexFingerprintRow"
        >
          {{ t("admin.settings.gatewayForwarding.codexAddRow") }}
        </button>
        <p
          v-if="codexFingerprintNoRequired"
          class="mt-2 text-xs text-[var(--warn-text)]"
        >
          {{
            t("admin.settings.gatewayForwarding.codexFingerprintNoRequiredWarn")
          }}
        </p>
      </div>

      <div class="flex items-center justify-between">
        <div class="pr-4">
          <label
            class="block text-sm font-medium text-[var(--foreground)]"
          >
            {{ t("admin.settings.gatewayForwarding.codexAllowAppServer") }}
          </label>
          <p class="mt-1 text-xs text-[var(--muted-foreground)]">
            {{ t("admin.settings.gatewayForwarding.codexAllowAppServerDesc") }}
          </p>
        </div>
        <Toggle v-model="form.codex_cli_only_allow_app_server_clients" />
      </div>

      <div>
        <label
          class="block text-sm font-medium text-[var(--foreground)]"
        >
          {{ t("admin.settings.gatewayForwarding.codexBlacklist") }}
        </label>
        <p class="mb-2 mt-1 text-xs text-[var(--muted-foreground)]">
          {{ t("admin.settings.gatewayForwarding.codexBlacklistDesc") }}
        </p>
        <div
          v-for="(row, i) in codexBlacklistRows"
          :key="`codex-bl-${i}`"
          class="mb-2 flex gap-2"
        >
          <input
            v-model="row.originator"
            type="text"
            class="input w-1/3 font-mono text-sm"
            :placeholder="
              t('admin.settings.gatewayForwarding.codexOriginatorPlaceholder')
            "
          />
          <input
            v-model="row.uaContains"
            type="text"
            class="input flex-1 font-mono text-sm"
            :placeholder="
              t('admin.settings.gatewayForwarding.codexUaContainsPlaceholder')
            "
          />
          <button
            type="button"
            class="btn btn-secondary btn-sm shrink-0 text-[var(--bad-text)] hover:text-[var(--bad-text)]"
            @click="removeCodexBlacklistRow(i)"
          >
            {{ t("admin.settings.gatewayForwarding.codexRemoveRow") }}
          </button>
        </div>
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          @click="addCodexBlacklistRow"
        >
          {{ t("admin.settings.gatewayForwarding.codexAddRow") }}
        </button>
      </div>

      <div>
        <label
          class="block text-sm font-medium text-[var(--foreground)]"
        >
          {{ t("admin.settings.gatewayForwarding.codexWhitelist") }}
        </label>
        <p class="mb-2 mt-1 text-xs text-[var(--muted-foreground)]">
          {{ t("admin.settings.gatewayForwarding.codexWhitelistDesc") }}
        </p>
        <div
          v-for="(row, i) in codexWhitelistRows"
          :key="`codex-wl-${i}`"
          class="mb-2 flex gap-2"
        >
          <input
            v-model="row.originator"
            type="text"
            class="input w-1/3 font-mono text-sm"
            :placeholder="
              t('admin.settings.gatewayForwarding.codexOriginatorPlaceholder')
            "
          />
          <input
            v-model="row.uaContains"
            type="text"
            class="input flex-1 font-mono text-sm"
            :placeholder="
              t('admin.settings.gatewayForwarding.codexUaContainsPlaceholder')
            "
          />
          <label
            class="flex shrink-0 items-center gap-1 text-xs text-[var(--muted-foreground)]"
            :title="
              t(
                'admin.settings.gatewayForwarding.codexWhitelistSkipFingerprintTooltip',
              )
            "
          >
            <input v-model="row.skipEngineFingerprint" type="checkbox" />
            {{
              t(
                "admin.settings.gatewayForwarding.codexWhitelistSkipFingerprint",
              )
            }}
          </label>
          <button
            type="button"
            class="btn btn-secondary btn-sm shrink-0 text-[var(--bad-text)] hover:text-[var(--bad-text)]"
            @click="removeCodexWhitelistRow(i)"
          >
            {{ t("admin.settings.gatewayForwarding.codexRemoveRow") }}
          </button>
        </div>
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          @click="addCodexWhitelistRow"
        >
          {{ t("admin.settings.gatewayForwarding.codexAddRow") }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import Toggle from "@/components/common/Toggle.vue";
import { useSettingsContext } from "./settingsContext";
const {
  t,
  codexBlacklistRows,
  codexWhitelistRows,
  codexFingerprintRows,
  codexFingerprintNoRequired,
  addCodexFingerprintRow,
  removeCodexFingerprintRow,
  addCodexBlacklistRow,
  removeCodexBlacklistRow,
  addCodexWhitelistRow,
  removeCodexWhitelistRow,
  form,
} = useSettingsContext();
</script>
