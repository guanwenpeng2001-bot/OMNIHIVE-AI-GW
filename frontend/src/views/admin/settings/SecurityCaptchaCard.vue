<template>
  <div class="card">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
        {{ t("admin.settings.captcha.title") }}
      </h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        {{ t("admin.settings.captcha.description") }}
      </p>
    </div>
    <div class="space-y-5 p-6">
      <!-- Enable Captcha -->
      <div class="flex items-center justify-between">
        <div>
          <label class="font-medium text-gray-900 dark:text-white">{{
            t("admin.settings.captcha.enable")
          }}</label>
          <p class="text-sm text-gray-500 dark:text-gray-400">
            {{ t("admin.settings.captcha.enableHint") }}
          </p>
        </div>
        <Toggle
          v-model="captchaMasterEnabled"
          data-testid="captcha-enabled-toggle"
        />
      </div>

      <!-- Provider fields - Only show when enabled -->
      <div
        v-if="captchaMasterEnabled"
        class="border-t border-gray-100 pt-4 dark:border-dark-700"
      >
        <!-- Provider Selector -->
        <div class="mb-6">
          <label
            class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
          >
            {{ t("admin.settings.captcha.provider") }}
          </label>
          <div
            class="grid grid-cols-3 gap-2 rounded-lg bg-gray-100 p-1 dark:bg-dark-700"
          >
            <button
              type="button"
              data-testid="captcha-provider-turnstile"
              class="inline-flex items-center justify-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition"
              :class="
                captchaProviderSelection === 'turnstile'
                  ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-800 dark:text-primary-300'
                  : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'
              "
              @click="selectCaptchaProvider('turnstile')"
            >
              {{ t("admin.settings.captcha.providerTurnstile") }}
            </button>
            <button
              type="button"
              data-testid="captcha-provider-tencent"
              class="inline-flex items-center justify-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition"
              :class="
                captchaProviderSelection === 'tencent'
                  ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-800 dark:text-primary-300'
                  : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'
              "
              @click="selectCaptchaProvider('tencent')"
            >
              {{ t("admin.settings.captcha.providerTencent") }}
            </button>
            <button
              type="button"
              data-testid="captcha-provider-aliyun"
              class="inline-flex items-center justify-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition"
              :class="
                captchaProviderSelection === 'aliyun'
                  ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-800 dark:text-primary-300'
                  : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'
              "
              @click="selectCaptchaProvider('aliyun')"
            >
              {{ t("admin.settings.captcha.providerAliyun") }}
            </button>
          </div>
        </div>

        <!-- Cloudflare Turnstile fields -->
        <div
          v-if="captchaProviderSelection === 'turnstile'"
          class="grid grid-cols-1 gap-6"
        >
          <div>
            <label
              class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
            >
              {{ t("admin.settings.turnstile.siteKey") }}
            </label>
            <input
              v-model="form.turnstile_site_key"
              type="text"
              class="input font-mono text-sm"
              placeholder="0x4AAAAAAA..."
            />
            <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.turnstile.siteKeyHint") }}
              <a
                href="https://dash.cloudflare.com/"
                target="_blank"
                class="text-primary-600 hover:text-primary-500"
                >{{ t("admin.settings.turnstile.cloudflareDashboard") }}</a
              >
            </p>
          </div>
          <div>
            <label
              class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
            >
              {{ t("admin.settings.turnstile.secretKey") }}
            </label>
            <input
              v-model="form.turnstile_secret_key"
              type="password"
              class="input font-mono text-sm"
              placeholder="0x4AAAAAAA..."
            />
            <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
              {{
                form.turnstile_secret_key_configured
                  ? t("admin.settings.turnstile.secretKeyConfiguredHint")
                  : t("admin.settings.turnstile.secretKeyHint")
              }}
            </p>
          </div>
        </div>

        <!-- Tencent Captcha fields -->
        <div v-else-if="captchaProviderSelection === 'tencent'">
          <div class="mb-6 max-w-sm">
            <label
              class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
            >
              {{ t("admin.settings.tencentCaptcha.region") }}
            </label>
            <div
              class="grid grid-cols-2 gap-2 rounded-lg bg-gray-100 p-1 dark:bg-dark-700"
            >
              <button
                type="button"
                data-testid="tencent-captcha-region-cn"
                class="inline-flex items-center justify-center rounded-md px-3 py-1.5 text-sm font-medium transition"
                :class="
                  form.tencent_captcha_region !== 'intl'
                    ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-800 dark:text-primary-300'
                    : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'
                "
                @click="form.tencent_captcha_region = 'cn'"
              >
                {{ t("admin.settings.tencentCaptcha.regionCn") }}
              </button>
              <button
                type="button"
                data-testid="tencent-captcha-region-intl"
                class="inline-flex items-center justify-center rounded-md px-3 py-1.5 text-sm font-medium transition"
                :class="
                  form.tencent_captcha_region === 'intl'
                    ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-800 dark:text-primary-300'
                    : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'
                "
                @click="form.tencent_captcha_region = 'intl'"
              >
                {{ t("admin.settings.tencentCaptcha.regionIntl") }}
              </button>
            </div>
            <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.tencentCaptcha.regionHint") }}
            </p>
          </div>
          <div class="grid grid-cols-1 gap-6 md:grid-cols-2">
            <div class="md:col-span-2">
              <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.tencentCaptcha.appCredentialsTitle") }}
              </h3>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.tencentCaptcha.appCredentialsHint") }}
              </p>
            </div>
            <div>
              <label
                class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
              >
                {{ t("admin.settings.tencentCaptcha.appId") }}
              </label>
              <input
                v-model="form.tencent_captcha_app_id"
                type="text"
                inputmode="numeric"
                class="input font-mono text-sm"
                placeholder="123456789"
              />
            </div>
            <div>
              <label
                class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
              >
                {{ t("admin.settings.tencentCaptcha.appSecretKey") }}
              </label>
              <input
                v-model="form.tencent_captcha_app_secret_key"
                type="password"
                autocomplete="new-password"
                class="input font-mono text-sm"
                :placeholder="t('admin.settings.tencentCaptcha.keepExisting')"
              />
              <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                {{
                  form.tencent_captcha_app_secret_key_configured
                    ? t("admin.settings.tencentCaptcha.configured")
                    : t("admin.settings.tencentCaptcha.required")
                }}
              </p>
            </div>
            <div
              class="border-t border-gray-100 pt-5 md:col-span-2 dark:border-dark-700"
            >
              <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.tencentCaptcha.cloudCredentialsTitle") }}
              </h3>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.tencentCaptcha.cloudCredentialsHint") }}
              </p>
            </div>
            <div>
              <label
                class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
              >
                {{ t("admin.settings.tencentCaptcha.cloudSecretId") }}
              </label>
              <input
                v-model="form.tencent_captcha_cloud_secret_id"
                type="password"
                autocomplete="new-password"
                class="input font-mono text-sm"
                :placeholder="t('admin.settings.tencentCaptcha.keepExisting')"
              />
              <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                {{
                  form.tencent_captcha_cloud_secret_id_configured
                    ? t("admin.settings.tencentCaptcha.configured")
                    : t("admin.settings.tencentCaptcha.required")
                }}
              </p>
            </div>
            <div>
              <label
                class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
              >
                {{ t("admin.settings.tencentCaptcha.cloudSecretKey") }}
              </label>
              <input
                v-model="form.tencent_captcha_cloud_secret_key"
                type="password"
                autocomplete="new-password"
                class="input font-mono text-sm"
                :placeholder="t('admin.settings.tencentCaptcha.keepExisting')"
              />
              <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                {{
                  form.tencent_captcha_cloud_secret_key_configured
                    ? t("admin.settings.tencentCaptcha.configured")
                    : t("admin.settings.tencentCaptcha.required")
                }}
              </p>
            </div>
          </div>
          <p class="mt-5 text-xs text-gray-500 dark:text-gray-400">
            {{ t("admin.settings.tencentCaptcha.camPermissionHint") }}
          </p>
          <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
            {{ t("admin.settings.tencentCaptcha.aidEncryptedHint") }}
          </p>
          <div class="mt-3 flex flex-wrap gap-x-4 gap-y-2 text-sm">
            <a
              :href="tencentCaptchaLinks.console"
              target="_blank"
              rel="noopener noreferrer"
              class="text-primary-600 hover:text-primary-500"
            >
              {{ t("admin.settings.tencentCaptcha.openCaptchaConsole") }}
            </a>
            <a
              :href="tencentCaptchaLinks.cloudKeys"
              target="_blank"
              rel="noopener noreferrer"
              class="text-primary-600 hover:text-primary-500"
            >
              {{ t("admin.settings.tencentCaptcha.createCloudKeys") }}
            </a>
            <a
              :href="tencentCaptchaLinks.webDocs"
              target="_blank"
              rel="noopener noreferrer"
              class="text-primary-600 hover:text-primary-500"
            >
              {{ t("admin.settings.tencentCaptcha.openWebDocs") }}
            </a>
          </div>
        </div>

        <!-- Aliyun Captcha 2.0 fields -->
        <div v-else class="grid grid-cols-1 gap-6">
          <div class="grid grid-cols-1 gap-6 sm:grid-cols-2">
            <div>
              <label
                class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
              >
                {{ t("admin.settings.aliyunCaptcha.region") }}
              </label>
              <div
                class="grid grid-cols-2 gap-2 rounded-lg bg-gray-100 p-1 dark:bg-dark-700"
              >
                <button
                  type="button"
                  class="inline-flex items-center justify-center rounded-md px-3 py-1.5 text-sm font-medium transition"
                  :class="
                    form.aliyun_captcha_region !== 'sgp'
                      ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-800 dark:text-primary-300'
                      : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'
                  "
                  @click="form.aliyun_captcha_region = 'cn'"
                >
                  {{ t("admin.settings.aliyunCaptcha.regionCn") }}
                </button>
                <button
                  type="button"
                  class="inline-flex items-center justify-center rounded-md px-3 py-1.5 text-sm font-medium transition"
                  :class="
                    form.aliyun_captcha_region === 'sgp'
                      ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-800 dark:text-primary-300'
                      : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'
                  "
                  @click="form.aliyun_captcha_region = 'sgp'"
                >
                  {{ t("admin.settings.aliyunCaptcha.regionSgp") }}
                </button>
              </div>
              <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.aliyunCaptcha.regionHint") }}
              </p>
            </div>
            <div>
              <label
                class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
              >
                {{ t("admin.settings.aliyunCaptcha.prefix") }}
              </label>
              <input
                v-model="form.aliyun_captcha_prefix"
                type="text"
                class="input font-mono text-sm"
                placeholder="14xxxxx"
              />
              <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.aliyunCaptcha.prefixHint") }}
              </p>
            </div>
          </div>
          <div>
            <label
              class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
            >
              {{ t("admin.settings.aliyunCaptcha.sceneId") }}
            </label>
            <input
              v-model="form.aliyun_captcha_scene_id"
              type="text"
              class="input font-mono text-sm"
              placeholder="1cxxxxxx"
            />
            <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.aliyunCaptcha.sceneIdHint") }}
            </p>
          </div>
          <div>
            <label
              class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
            >
              {{ t("admin.settings.aliyunCaptcha.accessKeyId") }}
            </label>
            <input
              v-model="form.aliyun_captcha_access_key_id"
              type="text"
              class="input font-mono text-sm"
              placeholder="LTAI..."
            />
            <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.aliyunCaptcha.accessKeyIdHint") }}
            </p>
          </div>
          <div>
            <label
              class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
            >
              {{ t("admin.settings.aliyunCaptcha.accessKeySecret") }}
            </label>
            <input
              v-model="form.aliyun_captcha_access_key_secret"
              type="password"
              autocomplete="new-password"
              class="input font-mono text-sm"
              placeholder="••••••••"
            />
            <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
              {{
                form.aliyun_captcha_access_key_secret_configured
                  ? t(
                      "admin.settings.aliyunCaptcha.accessKeySecretConfiguredHint",
                    )
                  : t("admin.settings.aliyunCaptcha.accessKeySecretHint")
              }}
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
const {
  t,
  captchaProviderSelection,
  captchaMasterEnabled,
  selectCaptchaProvider,
  tencentCaptchaLinks,
  form,
} = useSettingsContext();
</script>
