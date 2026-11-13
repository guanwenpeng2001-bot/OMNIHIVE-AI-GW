<template>
  <div class="card border-[var(--border)] bg-[var(--card)] shadow-none">
    <div class="border-b border-[var(--border)] px-4 py-4 sm:px-6">
      <h2 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
        {{ t("admin.settings.wechatConnect.title") }}
      </h2>
      <p class="mt-1 text-sm text-[var(--muted-foreground)]">
        {{ t("admin.settings.wechatConnect.description") }}
      </p>
    </div>
    <div class="space-y-5 p-4 sm:p-6">
      <div class="flex items-center justify-between">
        <div>
          <label class="font-medium text-[var(--foreground)]">{{
            t("admin.settings.wechatConnect.enabledLabel")
          }}</label>
          <p class="text-sm text-[var(--muted-foreground)]">
            {{ t("admin.settings.wechatConnect.enabledHint") }}
          </p>
        </div>
        <Toggle
          v-model="form.wechat_connect_enabled"
          data-testid="wechat-connect-enabled"
        />
      </div>

      <div
        v-if="form.wechat_connect_enabled"
        class="space-y-6 border-t border-[var(--border)] pt-4"
      >
        <div class="space-y-4">
          <div
            class="rounded-lg border border-[var(--border)] p-4"
          >
            <div class="flex items-start justify-between gap-4">
              <div>
                <h3 class="font-medium text-[var(--foreground)]">
                  {{ localText("PC 应用", "PC App") }}
                </h3>
                <p class="mt-1 text-sm text-[var(--muted-foreground)]">
                  {{
                    localText(
                      "桌面浏览器通过微信开放平台扫码登录。可与公众号或移动应用同时存在。",
                      "Desktop browsers sign in through WeChat Open Platform QR login. This can coexist with Official Account or Mobile App.",
                    )
                  }}
                </p>
              </div>
              <Toggle
                :model-value="form.wechat_connect_open_enabled"
                data-testid="wechat-connect-open-enabled"
                @update:model-value="handleWeChatOpenEnabledChange"
              />
            </div>
            <div
              v-if="form.wechat_connect_open_enabled"
              class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-2"
            >
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-[var(--foreground)]"
                >
                  {{ localText("PC AppID", "PC App ID") }}
                </label>
                <input
                  v-model="form.wechat_connect_open_app_id"
                  data-testid="wechat-connect-open-app-id"
                  type="text"
                  class="input font-mono text-sm"
                  :placeholder="
                    localText(
                      '微信开放平台 PC 应用 AppID',
                      'WeChat Open Platform PC App ID',
                    )
                  "
                />
              </div>
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-[var(--foreground)]"
                >
                  {{ localText("PC AppSecret", "PC App Secret") }}
                </label>
                <input
                  v-model="form.wechat_connect_open_app_secret"
                  data-testid="wechat-connect-open-app-secret"
                  type="password"
                  class="input font-mono text-sm"
                  :placeholder="
                    form.wechat_connect_open_app_secret_configured
                      ? localText(
                          '密钥已配置，留空以保留当前值。',
                          'Secret configured. Leave empty to keep the current value.',
                        )
                      : localText(
                          '微信开放平台 PC 应用 AppSecret',
                          'WeChat Open Platform PC App Secret',
                        )
                  "
                />
              </div>
            </div>
          </div>

          <div
            class="rounded-lg border border-[var(--border)] p-4"
          >
            <div class="flex items-start justify-between gap-4">
              <div>
                <h3 class="font-medium text-[var(--foreground)]">
                  {{ localText("公众号", "Official Account") }}
                </h3>
                <p class="mt-1 text-sm text-[var(--muted-foreground)]">
                  {{
                    localText(
                      "仅在微信内浏览器可用；非微信环境下会显示不可用。",
                      "Only available inside the WeChat browser. It is shown as unavailable outside WeChat.",
                    )
                  }}
                </p>
              </div>
              <Toggle
                :model-value="form.wechat_connect_mp_enabled"
                data-testid="wechat-connect-mp-enabled"
                @update:model-value="handleWeChatMPEnabledChange"
              />
            </div>
            <div
              v-if="form.wechat_connect_mp_enabled"
              class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-2"
            >
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-[var(--foreground)]"
                >
                  {{ localText("公众号 AppID", "Official Account App ID") }}
                </label>
                <input
                  v-model="form.wechat_connect_mp_app_id"
                  data-testid="wechat-connect-mp-app-id"
                  type="text"
                  class="input font-mono text-sm"
                  :placeholder="
                    localText('公众号 AppID', 'Official Account App ID')
                  "
                />
              </div>
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-[var(--foreground)]"
                >
                  {{
                    localText("公众号 AppSecret", "Official Account App Secret")
                  }}
                </label>
                <input
                  v-model="form.wechat_connect_mp_app_secret"
                  data-testid="wechat-connect-mp-app-secret"
                  type="password"
                  class="input font-mono text-sm"
                  :placeholder="
                    form.wechat_connect_mp_app_secret_configured
                      ? localText(
                          '密钥已配置，留空以保留当前值。',
                          'Secret configured. Leave empty to keep the current value.',
                        )
                      : localText(
                          '公众号 AppSecret',
                          'Official Account App Secret',
                        )
                  "
                />
              </div>
            </div>
          </div>

          <div
            class="rounded-lg border border-[var(--border)] p-4"
          >
            <div class="flex items-start justify-between gap-4">
              <div>
                <h3 class="font-medium text-[var(--foreground)]">
                  {{ localText("移动应用", "Mobile App") }}
                </h3>
                <p class="mt-1 text-sm text-[var(--muted-foreground)]">
                  {{
                    localText(
                      "原生移动端通过微信 SDK 唤起授权，网页端不会直接发起该流程。",
                      "Native mobile clients start authorization through the WeChat SDK. The web UI does not launch this flow directly.",
                    )
                  }}
                </p>
              </div>
              <Toggle
                :model-value="form.wechat_connect_mobile_enabled"
                data-testid="wechat-connect-mobile-enabled"
                @update:model-value="handleWeChatMobileEnabledChange"
              />
            </div>
            <div
              v-if="form.wechat_connect_mobile_enabled"
              class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-2"
            >
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-[var(--foreground)]"
                >
                  {{ localText("移动应用 AppID", "Mobile App ID") }}
                </label>
                <input
                  v-model="form.wechat_connect_mobile_app_id"
                  data-testid="wechat-connect-mobile-app-id"
                  type="text"
                  class="input font-mono text-sm"
                  :placeholder="localText('移动应用 AppID', 'Mobile App ID')"
                />
              </div>
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-[var(--foreground)]"
                >
                  {{ localText("移动应用 AppSecret", "Mobile App Secret") }}
                </label>
                <input
                  v-model="form.wechat_connect_mobile_app_secret"
                  data-testid="wechat-connect-mobile-app-secret"
                  type="password"
                  class="input font-mono text-sm"
                  :placeholder="
                    form.wechat_connect_mobile_app_secret_configured
                      ? localText(
                          '密钥已配置，留空以保留当前值。',
                          'Secret configured. Leave empty to keep the current value.',
                        )
                      : localText('移动应用 AppSecret', 'Mobile App Secret')
                  "
                />
              </div>
            </div>
          </div>
        </div>

        <div
          v-if="
            form.wechat_connect_open_enabled &&
            (form.wechat_connect_mp_enabled ||
              form.wechat_connect_mobile_enabled)
          "
          class="rounded-lg border border-[var(--border)] bg-[color-mix(in_srgb,var(--warn)_8%,var(--card))] px-4 py-3 text-sm text-[var(--warn-text)]"
        >
          {{
            localText(
              "如果同时启用 PC 应用和公众号/移动应用，这些应用需要挂在同一个微信开放平台主体下，否则 UnionID 无法稳定归并账号。",
              "When PC App is enabled together with Official Account or Mobile App, they should belong to the same WeChat Open Platform account so UnionID can merge identities reliably.",
            )
          }}
        </div>

        <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <div>
            <label
              class="mb-2 block text-sm font-medium text-[var(--foreground)]"
            >
              {{ localText("浏览器回调地址", "Browser Redirect URL") }}
            </label>
            <input
              data-testid="wechat-connect-redirect-url"
              v-model="form.wechat_connect_redirect_url"
              type="url"
              class="input font-mono text-sm"
              :placeholder="
                t('admin.settings.wechatConnect.redirectUrlPlaceholder')
              "
            />
            <p class="mt-1.5 text-xs text-[var(--muted-foreground)]">
              {{
                localText(
                  "用于 PC 应用和公众号的网页回调。移动应用走原生 SDK 时不直接使用这个浏览器回调。",
                  "Used by PC App and Official Account browser callbacks. Native mobile SDK flows do not start from this browser callback directly.",
                )
              }}
            </p>
            <div
              class="mt-2 flex flex-col gap-2 sm:flex-row sm:items-center sm:gap-3"
            >
              <button
                type="button"
                class="btn btn-secondary btn-sm w-fit"
                @click="setAndCopyWeChatRedirectUrl"
              >
                {{ t("admin.settings.wechatConnect.generateAndCopy") }}
              </button>
              <code
                v-if="wechatRedirectUrlSuggestion"
                class="select-all break-all rounded bg-[var(--surface-2)] px-2 py-1 font-mono text-xs text-[var(--muted-foreground)]"
              >
                {{ wechatRedirectUrlSuggestion }}
              </code>
            </div>
          </div>
        </div>

        <div>
          <label
            class="mb-2 block text-sm font-medium text-[var(--foreground)]"
          >
            {{ t("admin.settings.wechatConnect.frontendRedirectUrlLabel") }}
          </label>
          <input
            data-testid="wechat-connect-frontend-redirect-url"
            v-model="form.wechat_connect_frontend_redirect_url"
            type="text"
            class="input font-mono text-sm"
            :placeholder="
              t('admin.settings.wechatConnect.frontendRedirectUrlPlaceholder')
            "
          />
          <p class="mt-1.5 text-xs text-[var(--muted-foreground)]">
            {{ t("admin.settings.wechatConnect.frontendRedirectUrlHint") }}
          </p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import Toggle from "@/components/common/Toggle.vue";
import { useSettingsContext } from "./settingsContext";
const {
  t,
  localText,
  wechatRedirectUrlSuggestion,
  handleWeChatOpenEnabledChange,
  handleWeChatMPEnabledChange,
  handleWeChatMobileEnabledChange,
  setAndCopyWeChatRedirectUrl,
  form,
} = useSettingsContext();
</script>
