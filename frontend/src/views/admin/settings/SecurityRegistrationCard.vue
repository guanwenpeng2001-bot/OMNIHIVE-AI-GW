<template>
  <div class="card border-[var(--border)] bg-[var(--card)] shadow-none">
    <div class="border-b border-[var(--border)] px-4 py-4 sm:px-6">
      <h2 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
        {{ t("admin.settings.registration.title") }}
      </h2>
      <p class="mt-1 text-sm text-[var(--muted-foreground)]">
        {{ t("admin.settings.registration.description") }}
      </p>
    </div>
    <div class="space-y-5 p-4 sm:p-6">
      <!-- Enable Registration -->
      <div class="flex items-center justify-between">
        <div>
          <label class="font-medium text-[var(--foreground)]">{{
            t("admin.settings.registration.enableRegistration")
          }}</label>
          <p class="text-sm text-[var(--muted-foreground)]">
            {{ t("admin.settings.registration.enableRegistrationHint") }}
          </p>
        </div>
        <Toggle v-model="form.registration_enabled" />
      </div>

      <!-- Email Verification -->
      <div
        class="flex items-center justify-between border-t border-[var(--border)] pt-4"
      >
        <div>
          <label class="font-medium text-[var(--foreground)]">{{
            t("admin.settings.registration.emailVerification")
          }}</label>
          <p class="text-sm text-[var(--muted-foreground)]">
            {{ t("admin.settings.registration.emailVerificationHint") }}
          </p>
        </div>
        <Toggle v-model="form.email_verify_enabled" />
      </div>

      <!-- Email Suffix Whitelist -->
      <div class="border-t border-[var(--border)] pt-4">
        <label class="font-medium text-[var(--foreground)]">{{
          t("admin.settings.registration.emailSuffixWhitelist")
        }}</label>
        <p class="mt-1 text-sm text-[var(--muted-foreground)]">
          {{ t("admin.settings.registration.emailSuffixWhitelistHint") }}
        </p>
        <div
          class="mt-3 rounded-lg border border-[var(--border)] bg-[var(--card)] p-2"
        >
          <div class="flex flex-wrap items-center gap-2">
            <span
              v-for="suffix in registrationEmailSuffixWhitelistTags"
              :key="suffix"
              class="inline-flex items-center gap-1 rounded bg-[var(--surface-2)] px-2 py-1 text-xs font-mono text-[var(--foreground)]"
            >
              <span>{{ suffix }}</span>
              <button
                type="button"
                class="rounded-full text-[var(--muted-foreground)] hover:bg-[var(--border)] hover:text-[var(--foreground)]"
                @click="removeRegistrationEmailSuffixWhitelistTag(suffix)"
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
                v-model="registrationEmailSuffixWhitelistDraft"
                type="text"
                class="w-full bg-transparent text-sm font-mono text-[var(--foreground)] outline-none placeholder:text-[var(--muted-foreground)] dark:placeholder:text-[var(--muted-foreground)]"
                :placeholder="
                  t(
                    'admin.settings.registration.emailSuffixWhitelistPlaceholder',
                  )
                "
                @input="handleRegistrationEmailSuffixWhitelistDraftInput"
                @keydown="handleRegistrationEmailSuffixWhitelistDraftKeydown"
                @blur="commitRegistrationEmailSuffixWhitelistDraft"
                @paste="handleRegistrationEmailSuffixWhitelistPaste"
              />
            </div>
          </div>
        </div>
        <p class="mt-2 text-xs text-[var(--muted-foreground)]">
          {{ t("admin.settings.registration.emailSuffixWhitelistInputHint") }}
        </p>
      </div>

      <!-- Email Domain Quota -->
      <div
        class="flex items-center justify-between border-t border-[var(--border)] pt-4"
      >
        <div>
          <label class="font-medium text-[var(--foreground)]">{{
            t("admin.settings.registration.emailDomainQuota")
          }}</label>
          <p class="text-sm text-[var(--muted-foreground)]">
            {{ t("admin.settings.registration.emailDomainQuotaHint") }}
          </p>
        </div>
        <Toggle v-model="form.registration_email_domain_quota_enabled" />
      </div>

      <!-- Promo Code -->
      <div
        class="flex items-center justify-between border-t border-[var(--border)] pt-4"
      >
        <div>
          <label class="font-medium text-[var(--foreground)]">{{
            t("admin.settings.registration.promoCode")
          }}</label>
          <p class="text-sm text-[var(--muted-foreground)]">
            {{ t("admin.settings.registration.promoCodeHint") }}
          </p>
        </div>
        <Toggle v-model="form.promo_code_enabled" />
      </div>

      <!-- Invitation Code -->
      <div
        class="flex items-center justify-between border-t border-[var(--border)] pt-4"
      >
        <div>
          <label class="font-medium text-[var(--foreground)]">{{
            t("admin.settings.registration.invitationCode")
          }}</label>
          <p class="text-sm text-[var(--muted-foreground)]">
            {{ t("admin.settings.registration.invitationCodeHint") }}
          </p>
        </div>
        <Toggle v-model="form.invitation_code_enabled" />
      </div>
      <!-- Password Reset - Only show when email verification is enabled -->
      <div
        v-if="form.email_verify_enabled"
        class="flex items-center justify-between border-t border-[var(--border)] pt-4"
      >
        <div>
          <label class="font-medium text-[var(--foreground)]">{{
            t("admin.settings.registration.passwordReset")
          }}</label>
          <p class="text-sm text-[var(--muted-foreground)]">
            {{ t("admin.settings.registration.passwordResetHint") }}
          </p>
        </div>
        <Toggle v-model="form.password_reset_enabled" />
      </div>
      <!-- Frontend URL - Only show when password reset is enabled -->
      <div
        v-if="form.email_verify_enabled && form.password_reset_enabled"
        class="border-t border-[var(--border)] pt-4"
      >
        <label
          class="mb-2 block text-sm font-medium text-[var(--foreground)]"
        >
          {{ t("admin.settings.registration.frontendUrl") }}
        </label>
        <input
          v-model="form.frontend_url"
          type="url"
          class="input"
          :placeholder="t('admin.settings.registration.frontendUrlPlaceholder')"
        />
        <p class="mt-1.5 text-xs text-[var(--muted-foreground)]">
          {{ t("admin.settings.registration.frontendUrlHint") }}
        </p>
      </div>

      <!-- TOTP 2FA -->
      <div
        class="flex items-center justify-between border-t border-[var(--border)] pt-4"
      >
        <div>
          <label class="font-medium text-[var(--foreground)]">{{
            t("admin.settings.registration.totp")
          }}</label>
          <p class="text-sm text-[var(--muted-foreground)]">
            {{ t("admin.settings.registration.totpHint") }}
          </p>
          <!-- Warning when encryption key not configured -->
          <p
            v-if="!form.totp_encryption_key_configured"
            class="mt-2 text-sm text-[var(--warn-text)]"
          >
            {{ t("admin.settings.registration.totpKeyNotConfigured") }}
          </p>
        </div>
        <Toggle
          v-model="form.totp_enabled"
          :disabled="!form.totp_encryption_key_configured"
        />
      </div>

      <!-- Passkey sign-in -->
      <div
        class="border-t border-[var(--border)] pt-4"
        data-testid="passkey-settings"
      >
        <div class="flex items-start justify-between gap-4">
          <div>
            <label class="font-medium text-[var(--foreground)]">{{
              t("admin.settings.security.passkey")
            }}</label>
            <p class="text-sm text-[var(--muted-foreground)]">
              {{ t("admin.settings.security.passkeyHint") }}
            </p>
          </div>
          <Toggle
            v-model="form.passkey_enabled"
            data-testid="passkey-toggle"
            :disabled="!form.passkey_configured"
          />
        </div>
        <div
          class="mt-3 rounded-lg border px-3 py-2 text-sm"
          :class="
            form.passkey_configured
              ? 'border-[var(--border)] bg-[color-mix(in_srgb,var(--ok)_8%,var(--card))] text-[var(--ok-text)]   '
              : 'border-[var(--border)] bg-[color-mix(in_srgb,var(--warn)_8%,var(--card))] text-[var(--warn-text)]   '
          "
          data-testid="passkey-config-status"
        >
          <p class="font-medium">
            {{
              form.passkey_configured
                ? t("admin.settings.security.passkeyConfigured")
                : t("admin.settings.security.passkeyNotConfigured")
            }}
          </p>
          <p class="mt-1 break-all">
            {{ t("admin.settings.security.passkeyRPID") }}:
            {{
              form.passkey_rp_id ||
              t("admin.settings.security.passkeyValueNotConfigured")
            }}
          </p>
          <p class="mt-1 break-all">
            {{ t("admin.settings.security.passkeyOrigins") }}:
            {{
              form.passkey_rp_origins.length > 0
                ? form.passkey_rp_origins.join(", ")
                : t("admin.settings.security.passkeyValueNotConfigured")
            }}
          </p>
          <p v-if="!form.passkey_configured" class="mt-2">
            {{ t("admin.settings.security.passkeyDeploymentHint") }}
          </p>
        </div>
      </div>

      <!-- 敏感操作 step-up 2FA -->
      <div
        class="flex items-center justify-between border-t border-[var(--border)] pt-4"
      >
        <div>
          <label class="font-medium text-[var(--foreground)]">{{
            t("admin.settings.security.stepUp")
          }}</label>
          <p class="text-sm text-[var(--muted-foreground)]">
            {{ t("admin.settings.security.stepUpHint") }}
          </p>
        </div>
        <Toggle v-model="form.step_up_enabled" />
      </div>

      <!-- 会话 IP/UA 绑定 -->
      <div
        class="flex items-center justify-between border-t border-[var(--border)] pt-4"
      >
        <div>
          <label class="font-medium text-[var(--foreground)]">{{
            t("admin.settings.security.sessionBinding")
          }}</label>
          <p class="text-sm text-[var(--muted-foreground)]">
            {{ t("admin.settings.security.sessionBindingHint") }}
          </p>
        </div>
        <Toggle v-model="form.session_binding_enabled" />
      </div>

      <!-- 审计日志保留天数 -->
      <div
        class="flex items-center justify-between border-t border-[var(--border)] pt-4"
      >
        <div>
          <label class="font-medium text-[var(--foreground)]">{{
            t("admin.settings.security.auditRetention")
          }}</label>
          <p class="text-sm text-[var(--muted-foreground)]">
            {{ t("admin.settings.security.auditRetentionHint") }}
          </p>
        </div>
        <input
          v-model.number="form.audit_log_retention_days"
          type="number"
          min="0"
          class="input w-28 text-right"
        />
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
  registrationEmailSuffixWhitelistTags,
  registrationEmailSuffixWhitelistDraft,
  removeRegistrationEmailSuffixWhitelistTag,
  commitRegistrationEmailSuffixWhitelistDraft,
  handleRegistrationEmailSuffixWhitelistDraftInput,
  handleRegistrationEmailSuffixWhitelistDraftKeydown,
  handleRegistrationEmailSuffixWhitelistPaste,
  form,
} = useSettingsContext();
</script>
