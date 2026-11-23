<template>
  <div class="relative">
    <!-- Admin: Full version badge with dropdown -->
    <template v-if="isAdmin">
      <button
        @click="toggleDropdown"
        :aria-expanded="dropdownOpen"
        class="flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 font-mono text-[11px] transition-colors"
        :class="[
          hasUpdate
            ? 'border-[var(--warn)]/35 bg-[var(--warn)]/12 text-[var(--warn-text)]'
            : 'border-[var(--border)] bg-[var(--surface-2)] text-[var(--muted-foreground)] hover:text-[var(--foreground)]'
        ]"
        :title="hasUpdate ? t('version.updateAvailable') : t('version.currentVersion')"
      >
        <span v-if="currentVersion" class="font-medium">{{ formatVersionTag(currentVersion) }}</span>
        <span
          v-else
          class="h-3 w-12 animate-pulse rounded bg-[var(--surface-2)] font-medium"
        ></span>
        <!-- Update indicator -->
        <span v-if="hasUpdate" class="relative flex h-2 w-2">
          <span
            class="absolute inline-flex h-full w-full animate-ping rounded-full bg-amber-400 opacity-75"
          ></span>
          <span class="relative inline-flex h-2 w-2 rounded-full bg-amber-500"></span>
        </span>
        <span
          v-if="appStore.versionLoaded && !hasUpdate"
          class="h-1.5 w-1.5 rounded-full"
          :class="loading ? 'bg-amber-400' : 'bg-emerald-500/70'"
          aria-hidden="true"
        ></span>
        <span
          v-if="hasUpdate"
          class="hidden rounded-full bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-semibold leading-none text-amber-700 dark:text-amber-300 md:inline"
        >
          {{ isZh ? '有新版本' : 'Update available' }}
        </span>
      </button>

      <!-- Dropdown -->
      <transition name="dropdown">
        <div
          v-if="dropdownOpen"
          ref="dropdownRef"
          class="absolute left-0 z-50 mt-2 max-w-[calc(100vw-2rem)] overflow-hidden whitespace-normal rounded-xl border border-[var(--border)] bg-[var(--card)] shadow-lg transition-all duration-200"
          :class="rollbackPanelOpen || !isReleaseBuild ? 'w-80' : 'w-72'"
        >
          <!-- Header with refresh button -->
          <div
            class="flex items-center justify-between border-b border-[var(--border)] px-4 py-3"
          >
            <div class="flex items-center gap-2">
              <span class="text-sm font-medium text-[var(--foreground)]">{{
                t('version.currentVersion')
              }}</span>
              <span
                class="rounded-full border border-[var(--border)] bg-[var(--surface-2)] px-2 py-0.5 font-mono text-[10px] font-medium text-[var(--muted-foreground)]"
              >
                {{ buildTypeLabel }}
              </span>
            </div>
            <button
              @click="refreshVersion(true)"
              class="rounded-lg p-1.5 text-[var(--muted-foreground)] transition-colors hover:bg-[var(--surface-2)] hover:text-[var(--foreground)] disabled:cursor-not-allowed disabled:opacity-50"
              :disabled="loading || updating || rollingBack || restarting"
              :title="t('version.refresh')"
            >
              <Icon
                name="refresh"
                size="sm"
                :stroke-width="2"
                :class="{ 'animate-spin': loading }"
              />
            </button>
          </div>

          <div class="p-4">
            <!-- Loading state -->
            <div v-if="loading" class="flex items-center justify-center py-6">
              <svg class="h-6 w-6 animate-spin text-[var(--brand)]" fill="none" viewBox="0 0 24 24">
                <circle
                  class="opacity-25"
                  cx="12"
                  cy="12"
                  r="10"
                  stroke="currentColor"
                  stroke-width="4"
                ></circle>
                <path
                  class="opacity-75"
                  fill="currentColor"
                  d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                ></path>
              </svg>
            </div>

            <!-- Content -->
            <template v-else>
              <!-- Version display - centered and prominent -->
              <div class="mb-4 text-center">
                <div class="inline-flex items-center gap-2">
                  <span
                    v-if="currentVersion"
                    class="text-2xl font-bold text-[var(--foreground)]"
                    >{{ formatVersionTag(currentVersion) }}</span
                  >
                  <span v-else class="text-2xl font-bold text-[var(--muted-foreground)]">--</span>
                  <!-- Show check mark when up to date -->
                  <span
                    v-if="appStore.versionLoaded && !hasUpdate"
                    class="flex h-5 w-5 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/30"
                  >
                    <svg
                      class="h-3 w-3 text-green-600 dark:text-green-400"
                      fill="currentColor"
                      viewBox="0 0 20 20"
                    >
                      <path
                        fill-rule="evenodd"
                        d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z"
                        clip-rule="evenodd"
                      />
                    </svg>
                  </span>
                </div>
                <p v-if="appStore.updateCheckUnavailable" role="status" class="mt-1 text-xs text-[var(--muted-foreground)]">
                  {{ t('version.checkUnavailable') }}
                </p>
                <p v-else class="mt-1 text-xs text-[var(--muted-foreground)]">
                  {{
                    hasUpdate
                      ? t('version.latestVersion') + ': v' + latestVersion
                      : t('version.upToDate')
                  }}
                </p>
                <p
                  v-if="releaseSummary"
                  class="mt-2 line-clamp-3 rounded-lg border border-[var(--border)] bg-[var(--surface-2)] px-2.5 py-2 text-left text-[11px] leading-relaxed text-[var(--muted-foreground)]"
                >
                  {{ releaseSummary }}
                </p>
              </div>

              <!-- Priority 1: Update error (must check before hasUpdate) -->
              <div v-if="updateError" class="space-y-2">
                <div
                  class="flex items-center gap-3 rounded-lg border border-red-200 bg-red-50 p-3 dark:border-red-800/50 dark:bg-red-900/20"
                >
                  <div
                    class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-red-100 dark:bg-red-900/50"
                  >
                    <Icon
                      name="x"
                      size="sm"
                      :stroke-width="2"
                      class="text-red-600 dark:text-red-400"
                    />
                  </div>
                  <div class="min-w-0 flex-1">
                    <p class="text-sm font-medium text-red-700 dark:text-red-300">
                      {{ t('version.updateFailed') }}
                    </p>
                    <p class="truncate text-xs text-red-600/70 dark:text-red-400/70">
                      {{ updateError }}
                    </p>
                  </div>
                </div>

                <!-- Retry button -->
                <button
                  @click="requestUpdateConfirm"
                  :disabled="updating"
                  class="flex w-full items-center justify-center gap-2 rounded-lg bg-red-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-red-600 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {{ t('version.retry') }}
                </button>
              </div>

              <!-- Priority 2: Update success - need restart -->
              <div v-else-if="updateSuccess && needRestart" class="space-y-2">
                <div
                  class="flex items-center gap-3 rounded-lg border border-green-200 bg-green-50 p-3 dark:border-green-800/50 dark:bg-green-900/20"
                >
                  <div
                    class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/50"
                  >
                    <svg
                      class="h-4 w-4 text-green-600 dark:text-green-400"
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                      stroke-width="2"
                    >
                      <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
                    </svg>
                  </div>
                  <div class="min-w-0 flex-1">
                    <p class="text-sm font-medium text-green-700 dark:text-green-300">
                      {{
                        successKind === 'rollback'
                          ? t('version.rollbackComplete')
                          : t('version.updateComplete')
                      }}
                    </p>
                    <p class="text-xs text-green-600/70 dark:text-green-400/70">
                      {{ t('version.restartRequired') }}
                    </p>
                  </div>
                </div>

                <!-- Restart button with countdown -->
                <p
                  v-if="restartError"
                  role="alert"
                  data-testid="version-restart-error"
                  class="rounded-lg border border-red-200 bg-red-50 p-2 text-xs text-red-600 dark:border-red-800/50 dark:bg-red-900/20 dark:text-red-400"
                >
                  {{ restartError }}
                </p>
                <button
                  @click="handleRestart"
                  :disabled="restarting"
                  class="flex w-full items-center justify-center gap-2 rounded-lg bg-green-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-green-600 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  <svg
                    v-if="restarting"
                    class="h-4 w-4 animate-spin"
                    fill="none"
                    viewBox="0 0 24 24"
                  >
                    <circle
                      class="opacity-25"
                      cx="12"
                      cy="12"
                      r="10"
                      stroke="currentColor"
                      stroke-width="4"
                    ></circle>
                    <path
                      class="opacity-75"
                      fill="currentColor"
                      d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                    ></path>
                  </svg>
                  <svg
                    v-else
                    class="h-4 w-4"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
                    />
                  </svg>
                  <template v-if="restarting">
                    <span>{{ t('version.restarting') }}</span>
                    <span v-if="restartCountdown > 0" class="tabular-nums"
                      >({{ restartCountdown }}s)</span
                    >
                  </template>
                  <span v-else>{{ t('version.restartNow') }}</span>
                </button>
              </div>

              <!-- Priority 3: Update available for source build - show git pull hint -->
              <div v-else-if="hasUpdate && !isReleaseBuild" class="space-y-2">
                <a
                  :href="releaseURL"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="group flex items-center gap-3 rounded-lg border border-amber-200 bg-amber-50 p-3 transition-colors hover:bg-amber-100 dark:border-amber-800/50 dark:bg-amber-900/20 dark:hover:bg-amber-900/30"
                >
                  <div
                    class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-amber-100 dark:bg-amber-900/50"
                  >
                    <Icon
                      name="download"
                      size="sm"
                      :stroke-width="2"
                      class="text-amber-600 dark:text-amber-400"
                    />
                  </div>
                  <div class="min-w-0 flex-1">
                    <p class="text-sm font-medium text-amber-700 dark:text-amber-300">
                      {{ t('version.updateAvailable') }}
                    </p>
                    <p class="text-xs text-amber-600/70 dark:text-amber-400/70">
                      {{ formatVersionTag(latestVersion) }}
                    </p>
                  </div>
                  <svg
                    class="h-4 w-4 text-amber-500 transition-transform group-hover:translate-x-0.5 dark:text-amber-400"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
                  </svg>
                </a>
                <!-- Source / Docker build hint & copyable upgrade snippet -->
                <div
                  class="flex items-center gap-2 rounded-lg border border-blue-200 bg-blue-50 p-2 dark:border-blue-800/50 dark:bg-blue-900/20"
                >
                  <svg
                    class="h-3.5 w-3.5 flex-shrink-0 text-blue-500 dark:text-blue-400"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                    />
                  </svg>
                  <p class="text-xs text-blue-600 dark:text-blue-400">
                    {{
                      isDockerBuild
                        ? isZh
                          ? '当前为 Docker 部署，可复制下方命令一键拉取更新。'
                          : 'Running in Docker mode. Copy the command below to upgrade.'
                        : t('version.sourceModeHint')
                    }}
                  </p>
                </div>
                <div class="overflow-hidden rounded-lg border border-[var(--border)]">
                  <div
                    class="flex items-center justify-between border-b border-[var(--border)] bg-[var(--surface-2)] px-2.5 py-1.5"
                  >
                    <span class="text-[11px] font-medium text-[var(--foreground)]">
                      {{ isZh ? 'Docker Compose 升级命令' : 'Docker Compose Upgrade' }}
                    </span>
                    <button
                      type="button"
                      @click="copyToClipboard(dockerUpgradeCommand)"
                      class="flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] text-[var(--muted-foreground)] transition-colors hover:bg-[var(--card)] hover:text-[var(--foreground)]"
                    >
                      <Icon
                        :name="copied ? 'check' : 'copy'"
                        size="xs"
                        :stroke-width="2"
                        :class="copied ? 'text-green-500' : ''"
                      />
                      {{ copied ? t('version.copied') : t('version.copyCommand') }}
                    </button>
                  </div>
                  <code
                    class="block select-all whitespace-pre-wrap break-all bg-[var(--surface-2)] p-2.5 font-mono text-[10px] leading-relaxed text-[var(--muted-foreground)]"
                    >{{ dockerUpgradeCommand }}</code
                  >
                </div>
              </div>

              <!-- Priority 4: Update available for release build - show update button -->
              <div v-else-if="hasUpdate && isReleaseBuild" class="space-y-2">
                <!-- Update info card -->
                <div
                  class="flex items-center gap-3 rounded-lg border border-amber-200 bg-amber-50 p-3 dark:border-amber-800/50 dark:bg-amber-900/20"
                >
                <div
                  class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-amber-100 dark:bg-amber-900/50"
                >
                  <Icon
                    name="download"
                    size="sm"
                    :stroke-width="2"
                    class="text-amber-600 dark:text-amber-400"
                  />
                </div>
                  <div class="min-w-0 flex-1">
                    <p class="text-sm font-medium text-amber-700 dark:text-amber-300">
                      {{ t('version.updateAvailable') }}
                    </p>
                    <p class="text-xs text-amber-600/70 dark:text-amber-400/70">
                      {{ formatVersionTag(latestVersion) }}
                    </p>
                  </div>
                </div>

                <!-- Update button -->
                <button
                  @click="requestUpdateConfirm"
                  :disabled="updating"
                  data-testid="version-update-now-btn"
                  class="flex w-full items-center justify-center gap-2 btn btn-primary rounded-lg px-4 py-2 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50"
                >
                  <svg v-if="updating" class="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                    <circle
                      class="opacity-25"
                      cx="12"
                      cy="12"
                      r="10"
                      stroke="currentColor"
                      stroke-width="4"
                    ></circle>
                    <path
                      class="opacity-75"
                      fill="currentColor"
                      d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                    ></path>
                  </svg>
                  <Icon v-else name="download" size="sm" :stroke-width="2" />
                  {{ updating ? t('version.updating') : t('version.updateNow') }}
                </button>

                <!-- View release link -->
                <a
                  :href="releaseURL"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="flex items-center justify-center gap-1 text-xs text-[var(--muted-foreground)] transition-colors hover:text-gray-700"
                >
                  {{ t('version.viewChangelog') }}
                  <Icon name="externalLink" size="xs" :stroke-width="2" />
                </a>
              </div>

              <!-- Priority 5: Up to date - GitHub link + version rollback -->
              <div v-else class="space-y-2">
                <a
                  :href="releaseURL"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="flex items-center justify-center gap-2 py-2 text-sm text-[var(--muted-foreground)] transition-colors hover:text-gray-700"
                >
                  <svg class="h-4 w-4" fill="currentColor" viewBox="0 0 24 24">
                    <path
                      fill-rule="evenodd"
                      clip-rule="evenodd"
                      d="M12 2C6.477 2 2 6.477 2 12c0 4.42 2.865 8.17 6.839 9.49.5.092.682-.217.682-.482 0-.237-.008-.866-.013-1.7-2.782.604-3.369-1.34-3.369-1.34-.454-1.156-1.11-1.464-1.11-1.464-.908-.62.069-.608.069-.608 1.003.07 1.531 1.03 1.531 1.03.892 1.529 2.341 1.087 2.91.831.092-.646.35-1.086.636-1.336-2.22-.253-4.555-1.11-4.555-4.943 0-1.091.39-1.984 1.029-2.683-.103-.253-.446-1.27.098-2.647 0 0 .84-.269 2.75 1.025A9.578 9.578 0 0112 6.836c.85.004 1.705.114 2.504.336 1.909-1.294 2.747-1.025 2.747-1.025.546 1.377.203 2.394.1 2.647.64.699 1.028 1.592 1.028 2.683 0 3.842-2.339 4.687-4.566 4.935.359.309.678.919.678 1.852 0 1.336-.012 2.415-.012 2.743 0 .267.18.578.688.48C19.138 20.167 22 16.418 22 12c0-5.523-4.477-10-10-10z"
                    />
                  </svg>
                  {{ t('version.viewRelease') }}
                </a>

                <!-- Version rollback entry -->
                <div class="border-t border-[var(--border)] pt-2">
                  <button
                    @click="toggleRollbackPanel"
                    class="group flex w-full items-center justify-between rounded-lg px-2 py-1.5 text-xs text-[var(--muted-foreground)] transition-colors hover:bg-[var(--surface-2)] hover:text-[var(--foreground)]"
                  >
                    <span class="flex items-center gap-1.5">
                      <Icon name="clock" size="xs" :stroke-width="2" />
                      {{ t('version.rollback') }}
                    </span>
                    <Icon
                      name="chevronDown"
                      size="xs"
                      :stroke-width="2"
                      class="transition-transform duration-200"
                      :class="{ 'rotate-180': rollbackPanelOpen }"
                    />
                  </button>

                  <transition name="rollback">
                    <div v-if="rollbackPanelOpen" class="mt-2 space-y-2">
                      <!-- Docker/Source build: copyable rollback command snippet -->
                      <div
                        v-if="!isReleaseBuild"
                        class="space-y-2"
                      >
                        <div
                          class="flex items-center gap-2 rounded-lg border border-blue-200 bg-blue-50 p-2 dark:border-blue-800/50 dark:bg-blue-900/20"
                        >
                          <svg
                            class="h-3.5 w-3.5 flex-shrink-0 text-blue-500 dark:text-blue-400"
                            fill="none"
                            viewBox="0 0 24 24"
                            stroke="currentColor"
                            stroke-width="2"
                          >
                            <path
                              stroke-linecap="round"
                              stroke-linejoin="round"
                              d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                            />
                          </svg>
                          <p class="min-w-0 flex-1 text-xs leading-4 text-blue-600 dark:text-blue-400">
                            {{
                              isDockerBuild
                                ? isZh
                                  ? '在 Docker 环境中，可固定目标镜像版本后重建容器完成回滚。'
                                  : 'In Docker deployments, pin the target image tag and recreate the container.'
                                : t('version.rollbackSourceHint')
                            }}
                          </p>
                        </div>
                        <div class="overflow-hidden rounded-lg border border-[var(--border)]">
                          <div
                            class="flex items-center justify-between border-b border-[var(--border)] bg-[var(--surface-2)] px-2.5 py-1.5"
                          >
                            <span class="text-[11px] font-medium text-[var(--foreground)]">
                              {{ isZh ? 'Docker Compose 回滚命令' : 'Docker Compose Rollback' }}
                            </span>
                            <button
                              type="button"
                              @click="copyToClipboard(dockerQuickRollbackCommand)"
                              class="flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] text-[var(--muted-foreground)] transition-colors hover:bg-[var(--card)] hover:text-[var(--foreground)]"
                            >
                              <Icon
                                :name="copied ? 'check' : 'copy'"
                                size="xs"
                                :stroke-width="2"
                                :class="copied ? 'text-green-500' : ''"
                              />
                              {{ copied ? t('version.copied') : t('version.copyCommand') }}
                            </button>
                          </div>
                          <code
                            class="block select-all whitespace-pre-wrap break-all bg-[var(--surface-2)] p-2.5 font-mono text-[10px] leading-relaxed text-[var(--muted-foreground)]"
                            >{{ dockerQuickRollbackCommand }}</code
                          >
                        </div>
                      </div>

                      <!-- Loading versions -->
                      <div
                        v-else-if="rollbackVersionsLoading"
                        class="flex items-center justify-center py-4"
                      >
                        <svg
                          class="h-5 w-5 animate-spin text-[var(--brand)]"
                          fill="none"
                          viewBox="0 0 24 24"
                        >
                          <circle
                            class="opacity-25"
                            cx="12"
                            cy="12"
                            r="10"
                            stroke="currentColor"
                            stroke-width="4"
                          ></circle>
                          <path
                            class="opacity-75"
                            fill="currentColor"
                            d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                          ></path>
                        </svg>
                      </div>

                      <!-- Load error + retry -->
                      <div v-else-if="rollbackRemoteUnavailable || rollbackVersionsError" class="space-y-2">
                        <p
                          class="text-xs text-[var(--muted-foreground)]"
                        >
                          {{ rollbackRemoteUnavailable ? t('version.rollbackCheckUnavailable') : rollbackVersionsError }}
                        </p>
                        <button
                          @click="loadRollbackVersions"
                          class="w-full rounded-lg border border-[var(--border)] py-1.5 text-xs text-[var(--muted-foreground)] transition-colors hover:bg-[var(--surface-2)] hover:text-[var(--foreground)]"
                        >
                          {{ t('version.retry') }}
                        </button>
                      </div>

                      <!-- No versions available -->
                      <p
                        v-else-if="rollbackVersions.length === 0"
                        class="py-3 text-center text-xs text-[var(--muted-foreground)]"
                      >
                        {{ t('version.noRollbackVersions') }}
                      </p>

                      <!-- Version list -->
                      <template v-else>
                        <p class="px-0.5 text-[11px] text-[var(--muted-foreground)]">
                          {{ t('version.rollbackSelectVersion') }}
                        </p>

                        <button
                          v-for="item in rollbackVersions"
                          :key="item.version"
                          @click="selectRollbackVersion(item.version)"
                          :disabled="rollingBack"
                          class="flex w-full items-center justify-between rounded-lg border px-3 py-2 text-left transition-all disabled:cursor-not-allowed disabled:opacity-60"
                          :class="
                            selectedRollbackVersion === item.version
                              ? 'border-amber-300 bg-amber-50 shadow-sm dark:border-amber-700 dark:bg-amber-900/20'
                              : 'border-[var(--border)] hover:border-[var(--brand)] hover:bg-[var(--surface-2)]'
                          "
                        >
                          <span class="flex items-center gap-2">
                            <span
                              class="flex h-3.5 w-3.5 items-center justify-center rounded-full border transition-colors"
                              :class="
                                selectedRollbackVersion === item.version
                                  ? 'border-amber-500'
                                  : 'border-[var(--border)]'
                              "
                            >
                              <span
                                v-if="selectedRollbackVersion === item.version"
                                class="h-1.5 w-1.5 rounded-full bg-amber-500"
                              ></span>
                            </span>
                            <span
                              class="text-sm font-semibold"
                              :class="
                                selectedRollbackVersion === item.version
                                  ? 'text-amber-700 dark:text-amber-300'
                                  : 'text-[var(--foreground)]'
                              "
                              >{{ formatVersionTag(item.version) }}</span
                            >
                          </span>
                          <span class="text-[11px] tabular-nums text-[var(--muted-foreground)]">
                            {{ formatPublishedAt(item.published_at) }}
                          </span>
                        </button>

                        <!-- Selected version: manual command (per deploy method) + confirm -->
                        <transition name="rollback">
                          <div v-if="selectedRollbackVersion" class="space-y-2">
                            <p class="px-0.5 text-[11px] text-[var(--muted-foreground)]">
                              {{ t('version.manualRollbackCommand') }}
                            </p>

                            <!-- Terminal-style block with deploy-method tabs -->
                            <div
                              class="overflow-hidden rounded-lg border border-[var(--border)]"
                            >
                              <div
                                class="flex items-center justify-between border-b border-[var(--border)] bg-[var(--surface-2)] px-2 py-1.5"
                              >
                                <div
                                  class="flex items-center gap-0.5 rounded-md bg-[var(--surface-2)] p-0.5"
                                >
                                  <button
                                    v-for="tab in manualTabs"
                                    :key="tab.key"
                                    @click="manualTab = tab.key"
                                    class="rounded px-2 py-0.5 text-[11px] font-medium transition-colors"
                                    :class="
                                      manualTab === tab.key
                                        ? 'bg-[var(--card)] text-[var(--foreground)] shadow-sm'
                                        : 'text-[var(--muted-foreground)] hover:text-gray-600'
                                    "
                                  >
                                    {{ tab.label }}
                                  </button>
                                </div>
                                <button
                                  @click="copyToClipboard(activeManualCommand)"
                                  class="flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] text-[var(--muted-foreground)] transition-colors hover:bg-[var(--surface-2)] hover:text-[var(--foreground)]"
                                >
                                  <Icon
                                    :name="copied ? 'check' : 'copy'"
                                    size="xs"
                                    :stroke-width="2"
                                    :class="copied ? 'text-green-500' : ''"
                                  />
                                  {{ copied ? t('version.copied') : t('version.copyCommand') }}
                                </button>
                              </div>
                              <code
                                class="block select-all whitespace-pre-wrap break-all bg-[var(--surface-2)] p-2.5 font-mono text-[10px] leading-relaxed text-[var(--muted-foreground)]"
                                >{{ activeManualCommand }}</code
                              >
                            </div>

                            <p
                              class="flex items-start gap-1.5 px-0.5 text-[11px] leading-4 text-amber-600 dark:text-amber-400"
                            >
                              <Icon
                                name="exclamationTriangle"
                                size="xs"
                                :stroke-width="2"
                                class="mt-px flex-shrink-0"
                              />
                              {{ t('version.rollbackWarning') }}
                            </p>

                            <p
                              v-if="rollbackError"
                              class="rounded-lg border border-red-200 bg-red-50 p-2 text-xs text-red-600 dark:border-red-800/50 dark:bg-red-900/20 dark:text-red-400"
                            >
                              {{ rollbackError }}
                            </p>

                            <button
                              @click="requestRollbackConfirm"
                              :disabled="rollingBack"
                              data-testid="version-rollback-btn"
                              class="flex w-full items-center justify-center gap-2 rounded-lg bg-amber-500 px-4 py-2 text-sm font-medium text-white shadow-sm transition-colors hover:bg-amber-600 disabled:cursor-not-allowed disabled:opacity-50"
                            >
                              <svg
                                v-if="rollingBack"
                                class="h-4 w-4 animate-spin"
                                fill="none"
                                viewBox="0 0 24 24"
                              >
                                <circle
                                  class="opacity-25"
                                  cx="12"
                                  cy="12"
                                  r="10"
                                  stroke="currentColor"
                                  stroke-width="4"
                                ></circle>
                                <path
                                  class="opacity-75"
                                  fill="currentColor"
                                  d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                                ></path>
                              </svg>
                              <Icon v-else name="clock" size="sm" :stroke-width="2" />
                              <span>{{
                                rollingBack
                                  ? t('version.rollingBack')
                                  : t('version.rollbackConfirm', {
                                      version: 'v' + selectedRollbackVersion
                                    })
                              }}</span>
                            </button>
                          </div>
                        </transition>
                      </template>
                    </div>
                  </transition>
                </div>
              </div>
            </template>
          </div>
        </div>
      </transition>
    </template>

    <!-- Non-admin: Simple static version text -->
    <span v-else-if="version" class="text-xs text-[var(--muted-foreground)]">
      {{ formatVersionTag(version) }}
    </span>

    <ConfirmDialog
      :show="confirmAction !== null"
      :title="confirmDialogTitle"
      :message="confirmDialogMessage"
      :confirm-text="confirmDialogConfirmText"
      :cancel-text="t('common.cancel')"
      :danger="confirmAction === 'rollback'"
      @confirm="executeConfirmedAction"
      @cancel="confirmAction = null"
    />
  </div>
</template>

<script setup lang="ts">
import { extractApiErrorMessage, normalizeApiError } from '@/utils/apiError'

import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import {
  performUpdate,
  restartService,
  getRollbackVersions,
  rollback as rollbackAPI,
  type RollbackVersionInfo
} from '@/api/admin/system'
import { useClipboard } from '@/composables/useClipboard'
import Icon from '@/components/icons/Icon.vue'
import ConfirmDialog from './ConfirmDialog.vue'

const GITHUB_REPO = 'guanwenpeng2001-bot/OMNIHIVE-AI-GW'
// GHCR image published by CI (tags carry no "v" prefix).
const DOCKER_IMAGE = 'ghcr.io/guanwenpeng2001-bot/omnihive-ai-gw'

const { t, locale } = useI18n()
const isZh = computed(() => String(locale?.value || 'zh').toLowerCase().startsWith('zh'))

const props = defineProps<{
  version?: string
}>()

function formatVersionTag(raw?: string | null): string {
  if (!raw) return ''
  const trimmed = raw.trim()
  return /^[vV]/.test(trimmed) ? trimmed : `v${trimmed}`
}

const authStore = useAuthStore()
const appStore = useAppStore()

const isAdmin = computed(() => authStore.isAdmin)

const dropdownOpen = ref(false)
const dropdownRef = ref<HTMLElement | null>(null)

// Use store's cached version state
const loading = computed(() => appStore.versionLoading)
const currentVersion = computed(() => appStore.currentVersion || props.version || '')
const latestVersion = computed(() => appStore.latestVersion)
const hasUpdate = computed(() => appStore.hasUpdate)
const releaseInfo = computed(() => appStore.releaseInfo)
const releaseURL = computed(() => {
  const url = releaseInfo.value?.html_url
  return url && url !== '#' ? url : `https://github.com/${GITHUB_REPO}/releases`
})
const buildType = computed(() => appStore.buildType || 'source')
const isDockerBuild = computed(() => buildType.value === 'docker')
const buildTypeLabel = computed(() => {
  if (buildType.value === 'release') return isZh.value ? '发行版' : 'Release'
  if (buildType.value === 'docker') return 'Docker'
  return isZh.value ? '源码构建' : 'Source'
})
const releaseSummary = computed(() => {
  const raw = releaseInfo.value?.body?.trim()
  if (!raw) return ''
  return raw
    .replace(/^#+\s*/gm, '')
    .replace(/\r\n/g, '\n')
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .slice(0, 3)
    .join(' · ')
})
const dockerUpgradeCommand = computed(() => {
  const tag = latestVersion.value || 'latest'
  return [
    `# ${isZh.value ? '拉取并切换至最新版本' : 'Pull and restart with latest image'}`,
    `docker pull ${DOCKER_IMAGE}:${tag}`,
    'docker compose up -d'
  ].join('\n')
})
const dockerQuickRollbackCommand = computed(() => {
  const target = currentVersion.value || '<target-version>'
  return [
    `# ${isZh.value ? '在 docker-compose.yml 中固定历史镜像标签' : 'Pin target image tag in docker-compose.yml'}`,
    `image: ${DOCKER_IMAGE}:${target}`,
    'docker compose up -d'
  ].join('\n')
})

// Update process states (local to this component)
const updating = ref(false)
const restarting = ref(false)
const needRestart = ref(false)
const updateError = ref('')
const restartError = ref('')
const updateSuccess = ref(false)
const restartCountdown = ref(0)
const confirmAction = ref<'update' | 'rollback' | null>(null)
// Distinguishes the success + restart panel between update and rollback flows
const successKind = ref<'update' | 'rollback'>('update')

let isUnmounted = false
let countdownInterval: ReturnType<typeof setInterval> | null = null
let healthRetryTimer: ReturnType<typeof setTimeout> | null = null
let healthAbortController: AbortController | null = null

function clearRestartTimers() {
  if (countdownInterval !== null) {
    clearInterval(countdownInterval)
    countdownInterval = null
  }
  if (healthRetryTimer !== null) {
    clearTimeout(healthRetryTimer)
    healthRetryTimer = null
  }
  if (healthAbortController) {
    healthAbortController.abort()
    healthAbortController = null
  }
}

const confirmDialogTitle = computed(() => {
  if (confirmAction.value === 'rollback') {
    return isZh.value ? '确认回滚服务版本' : 'Confirm Version Rollback'
  }
  return isZh.value ? '确认一键更新服务' : 'Confirm Service Update'
})

const confirmDialogMessage = computed(() => {
  if (confirmAction.value === 'rollback') {
    const target = selectedRollbackVersion.value ? `v${selectedRollbackVersion.value}` : ''
    return isZh.value
      ? `将把服务端二进制替换为 ${target} 并需要重启服务，可能导致短暂中断。建议操作前先完成数据备份，确认继续吗？`
      : `This will replace the server binary with ${target} and require a restart, which may cause brief downtime. Back up data first. Continue?`
  }
  const target = latestVersion.value ? formatVersionTag(latestVersion.value) : ''
  return isZh.value
    ? `将把服务端更新至 ${target} 并需要重启服务，可能导致短暂中断。建议操作前先完成数据备份，确认继续吗？`
    : `This will update the server to ${target} and require a restart, which may cause brief downtime. Back up data first. Continue?`
})

const confirmDialogConfirmText = computed(() => {
  if (confirmAction.value === 'rollback') {
    return selectedRollbackVersion.value
      ? t('version.rollbackConfirm', { version: 'v' + selectedRollbackVersion.value })
      : t('common.confirm')
  }
  return t('version.updateNow')
})

function requestUpdateConfirm() {
  if (updating.value || restarting.value) return
  confirmAction.value = 'update'
}

function requestRollbackConfirm() {
  if (!isAdmin.value || rollingBack.value || restarting.value || !selectedRollbackVersion.value) return
  confirmAction.value = 'rollback'
}

function executeConfirmedAction() {
  const action = confirmAction.value
  confirmAction.value = null
  if (action === 'update') {
    void handleUpdate()
  } else if (action === 'rollback') {
    void handleRollback()
  }
}

// Rollback states
const rollbackPanelOpen = ref(false)
const rollbackVersions = ref<RollbackVersionInfo[]>([])
const rollbackVersionsLoading = ref(false)
const rollbackRemoteUnavailable = ref(false)
const rollbackVersionsError = ref('')
const selectedRollbackVersion = ref('')
const rollingBack = ref(false)
const rollbackError = ref('')

const { copied, copyToClipboard } = useClipboard()

// Manual rollback methods differ by deployment: script installs use install.sh,
// docker deployments pin the image tag instead
const manualTab = ref<'script' | 'docker'>('script')

const manualTabs = computed(() => [
  { key: 'script' as const, label: t('version.deployScript') },
  { key: 'docker' as const, label: t('version.deployDocker') }
])

const scriptRollbackCommand = computed(() => {
  if (!selectedRollbackVersion.value) return ''
  const tag = `v${selectedRollbackVersion.value}`
  return `curl -sSL https://raw.githubusercontent.com/${GITHUB_REPO}/${tag}/deploy/install.sh | sudo bash -s -- rollback ${tag}`
})

const dockerRollbackCommand = computed(() => {
  if (!selectedRollbackVersion.value) return ''
  return [
    `# ${t('version.dockerEditCompose')}`,
    `image: ${DOCKER_IMAGE}:${selectedRollbackVersion.value}`,
    '',
    `# ${t('version.dockerRecreate')}`,
    'docker compose up -d'
  ].join('\n')
})

const activeManualCommand = computed(() =>
  manualTab.value === 'docker' ? dockerRollbackCommand.value : scriptRollbackCommand.value
)

// Only show update check for release builds (binary/docker deployment)
const isReleaseBuild = computed(() => buildType.value === 'release')

function toggleDropdown() {
  dropdownOpen.value = !dropdownOpen.value
  if (dropdownOpen.value) void appStore.fetchVersion(false)
}

function closeDropdown() {
  dropdownOpen.value = false
}

async function refreshVersion(force = true) {
  if (!isAdmin.value || updating.value || rollingBack.value || restarting.value) return

  // Preserve pending restart state so users do not lose the Restart button
  if (!needRestart.value) {
    updateError.value = ''
    restartError.value = ''
    updateSuccess.value = false
    resetRollbackState()
  }

  await appStore.fetchVersion(force)
}

async function handleUpdate() {
  if (updating.value) return

  updating.value = true
  updateError.value = ''
  updateSuccess.value = false

  try {
    const result = await performUpdate()
    successKind.value = 'update'
    updateSuccess.value = true
    needRestart.value = result.need_restart
    // Clear version cache to reflect update completed
    appStore.clearVersionCache()
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string } }; message?: string }
    updateError.value = extractApiErrorMessage(err, '') || t('version.updateFailed')
  } finally {
    updating.value = false
  }
}

function resetRollbackState() {
  rollbackPanelOpen.value = false
  rollbackVersions.value = []
  rollbackRemoteUnavailable.value = false
  rollbackVersionsError.value = ''
  selectedRollbackVersion.value = ''
  rollbackError.value = ''
  manualTab.value = 'script'
}

async function toggleRollbackPanel() {
  if (!isAdmin.value) return
  rollbackPanelOpen.value = !rollbackPanelOpen.value
  // Source builds only show a hint, no version list to fetch
  if (
    rollbackPanelOpen.value &&
    isReleaseBuild.value &&
    rollbackVersions.value.length === 0 &&
    !rollbackVersionsLoading.value
  ) {
    await loadRollbackVersions()
  }
}

async function loadRollbackVersions() {
  if (!isAdmin.value) return
  rollbackVersionsLoading.value = true
  rollbackRemoteUnavailable.value = false
  rollbackVersionsError.value = ''
  try {
    const data = await getRollbackVersions()
    rollbackRemoteUnavailable.value = data.remote_status === 'unavailable'
    rollbackVersions.value = rollbackRemoteUnavailable.value ? [] : data.versions || []
  } catch (error: unknown) {
    const err = normalizeApiError(error)
    rollbackRemoteUnavailable.value = err.status === 503 || err.reason === 'UPDATE_CHECK_UNAVAILABLE'
    if (!rollbackRemoteUnavailable.value) rollbackVersionsError.value = err.message || t('version.loadVersionsFailed')
  } finally {
    rollbackVersionsLoading.value = false
  }
}

function selectRollbackVersion(version: string) {
  if (rollingBack.value) return
  rollbackError.value = ''
  selectedRollbackVersion.value = selectedRollbackVersion.value === version ? '' : version
}

function formatPublishedAt(publishedAt: string): string {
  if (!publishedAt) return ''
  const date = new Date(publishedAt)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleDateString()
}

async function handleRollback() {
  if (!isAdmin.value) return
  if (rollingBack.value || !selectedRollbackVersion.value) return

  rollingBack.value = true
  rollbackError.value = ''

  try {
    const result = await rollbackAPI(selectedRollbackVersion.value)
    successKind.value = 'rollback'
    updateSuccess.value = true
    needRestart.value = result.need_restart
    rollbackPanelOpen.value = false
    // Clear version cache so the next check reflects the rolled-back version
    appStore.clearVersionCache()
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string } }; message?: string }
    rollbackError.value = extractApiErrorMessage(err, '') || t('version.rollbackFailed')
  } finally {
    rollingBack.value = false
  }
}

async function handleRestart() {
  if (restarting.value) return

  restarting.value = true
  restartError.value = ''
  restartCountdown.value = 8
  clearRestartTimers()

  try {
    await restartService()
    // Service will restart, page will reload automatically or show disconnected
  } catch (error: unknown) {
    const err = error as { response?: { status?: number }; status?: number }
    const httpStatus = err?.response?.status ?? err?.status
    // Explicit HTTP errors (e.g. 401/403/500) mean restart was rejected or failed; do not swallow
    if (typeof httpStatus === 'number' && httpStatus >= 400) {
      restarting.value = false
      restartCountdown.value = 0
      restartError.value =
        extractApiErrorMessage(error, '') ||
        (isZh.value ? '服务重启失败，请检查权限或服务端日志。' : 'Service restart failed. Check permissions or server logs.')
      appStore.showError?.(restartError.value)
      return
    }
    // Connection drop / network error is expected while service shuts down for restart
  }

  if (isUnmounted) return

  // Start countdown
  countdownInterval = setInterval(() => {
    if (isUnmounted) {
      clearRestartTimers()
      return
    }
    restartCountdown.value--
    if (restartCountdown.value <= 0) {
      if (countdownInterval !== null) {
        clearInterval(countdownInterval)
        countdownInterval = null
      }
      // Try to check if service is back before reload
      void checkServiceAndReload()
    }
  }, 1000)
}

async function checkServiceAndReload() {
  const maxRetries = 5
  const retryDelay = 1000
  healthAbortController = new AbortController()
  const signal = healthAbortController.signal

  for (let i = 0; i < maxRetries; i++) {
    if (isUnmounted || signal.aborted) return
    try {
      const response = await fetch('/health', {
        method: 'GET',
        cache: 'no-cache',
        signal
      })
      if (response.ok && !isUnmounted && !signal.aborted) {
        // Service is back, reload page
        window.location.reload()
        return
      }
    } catch {
      if (isUnmounted || signal.aborted) return
      // Service not ready yet
    }

    if (i < maxRetries - 1) {
      await new Promise<void>((resolve) => {
        healthRetryTimer = setTimeout(() => {
          healthRetryTimer = null
          resolve()
        }, retryDelay)
        signal.addEventListener(
          'abort',
          () => {
            if (healthRetryTimer !== null) {
              clearTimeout(healthRetryTimer)
              healthRetryTimer = null
            }
            resolve()
          },
          { once: true }
        )
      })
    }
  }

  // After retries, reload anyway
  if (!isUnmounted && !signal.aborted) {
    window.location.reload()
  }
}

function handleClickOutside(event: MouseEvent) {
  if (confirmAction.value !== null) return
  const target = event.target as Node
  const button = (event.target as Element).closest('button')
  if (dropdownRef.value && !dropdownRef.value.contains(target) && !button?.contains(target)) {
    closeDropdown()
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
})

onBeforeUnmount(() => {
  isUnmounted = true
  clearRestartTimers()
  document.removeEventListener('click', handleClickOutside)
})
</script>

<style scoped>
.dropdown-enter-active,
.dropdown-leave-active {
  transition: all 0.2s ease;
}

.dropdown-enter-from,
.dropdown-leave-to {
  opacity: 0;
  transform: scale(0.95) translateY(-4px);
}

.rollback-enter-active,
.rollback-leave-active {
  transition: all 0.2s ease;
}

.rollback-enter-from,
.rollback-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

.line-clamp-3 {
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
</style>
