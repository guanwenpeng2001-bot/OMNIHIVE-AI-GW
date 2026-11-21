<template>
  <component :is="shellComponent">
    <div class="min-w-0 space-y-5">
      <!-- Hive Aura Backup Workbench Status Bar -->
      <div
        class="card flex flex-wrap items-center justify-between gap-3 p-4"
        style="
          background: linear-gradient(135deg, color-mix(in srgb, var(--brand) 6%, var(--card)), var(--card));
          border-color: color-mix(in srgb, var(--brand) 24%, var(--border));
        "
      >
        <div class="flex flex-wrap items-center gap-2.5">
          <span class="chip font-medium">
            <span
              class="pulse-dot"
              :class="creatingBackup ? 'pulse-warn' : 'pulse-ok'"
            ></span>
            <span>{{ t('admin.backup.operations.title') }}</span>
          </span>
          <span class="pill pill-ghost font-mono text-xs tabular-nums">
            S3: {{ s3Form.bucket || '—' }}
          </span>
          <span class="pill pill-ghost font-mono text-xs tabular-nums">
            CRON: {{ scheduleForm.enabled ? scheduleForm.cron_expr : 'OFF' }}
          </span>
          <span class="pill pill-ghost font-mono text-xs tabular-nums">
            {{ backups.length }} snapshots
          </span>
        </div>
        <button
          type="button"
          class="btn btn-secondary btn-sm text-xs"
          @click="showR2Guide = true"
        >
          Cloudflare R2
        </button>
      </div>

      <!-- S3 Storage Config -->
      <div class="card p-5">
        <div
          class="mb-4 flex flex-wrap items-center justify-between gap-3 border-b pb-3.5"
          style="border-color: var(--border)"
        >
          <div>
            <h3 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
              {{ t('admin.backup.s3.title') }}
            </h3>
            <p class="mt-1 text-sm text-[var(--muted-foreground)]">
              {{ t('admin.backup.s3.descriptionPrefix') }}
              <button type="button" class="font-medium underline" style="color: var(--brand-text)" @click="showR2Guide = true">Cloudflare R2</button>
              {{ t('admin.backup.s3.descriptionSuffix') }}
            </p>
          </div>
          <span class="pill pill-ghost font-mono tabular-nums text-xs">
            {{ s3SecretConfigured ? t('admin.backup.s3.secretConfigured') : 'S3 / R2' }}
          </span>
        </div>
        <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
          <div>
            <label for="backup-s3-endpoint" class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">{{ t('admin.backup.s3.endpoint') }}</label>
            <input id="backup-s3-endpoint" v-model="s3Form.endpoint" class="input w-full font-mono tabular-nums text-xs" placeholder="https://<account_id>.r2.cloudflarestorage.com" />
          </div>
          <div>
            <label for="backup-s3-region" class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">{{ t('admin.backup.s3.region') }}</label>
            <input id="backup-s3-region" v-model="s3Form.region" class="input w-full font-mono tabular-nums text-xs" placeholder="auto" />
          </div>
          <div>
            <label for="backup-s3-bucket" class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">{{ t('admin.backup.s3.bucket') }}</label>
            <input id="backup-s3-bucket" v-model="s3Form.bucket" class="input w-full font-mono tabular-nums text-xs" />
          </div>
          <div>
            <label for="backup-s3-prefix" class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">{{ t('admin.backup.s3.prefix') }}</label>
            <input id="backup-s3-prefix" v-model="s3Form.prefix" class="input w-full font-mono tabular-nums text-xs" placeholder="backups/" />
          </div>
          <div>
            <label for="backup-s3-access-key-id" class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">{{ t('admin.backup.s3.accessKeyId') }}</label>
            <input id="backup-s3-access-key-id" v-model="s3Form.access_key_id" class="input w-full font-mono tabular-nums text-xs" />
          </div>
          <div>
            <label for="backup-s3-secret-access-key" class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">{{ t('admin.backup.s3.secretAccessKey') }}</label>
            <input id="backup-s3-secret-access-key" v-model="s3Form.secret_access_key" type="password" class="input w-full font-mono tabular-nums text-xs" :placeholder="s3SecretConfigured ? t('admin.backup.s3.secretConfigured') : ''" />
          </div>
          <label class="inline-flex items-center gap-2 text-sm text-[var(--foreground)] md:col-span-2">
            <input v-model="s3Form.force_path_style" type="checkbox" />
            <span>{{ t('admin.backup.s3.forcePathStyle') }}</span>
          </label>
        </div>
        <div class="mt-4 flex flex-wrap gap-2 border-t pt-3.5" style="border-color: var(--border)">
          <button type="button" class="btn btn-secondary btn-sm" :disabled="testingS3" @click="testS3">
            {{ testingS3 ? t('common.loading') : t('admin.backup.s3.testConnection') }}
          </button>
          <button type="button" class="btn btn-primary btn-sm" :disabled="savingS3" @click="saveS3Config">
            {{ savingS3 ? t('common.loading') : t('common.save') }}
          </button>
        </div>
      </div>

      <!-- Async image object storage -->
      <div class="card p-5">
        <div
          class="mb-4 flex flex-wrap items-center justify-between gap-3 border-b pb-3.5"
          style="border-color: var(--border)"
        >
          <div>
            <h3 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
              {{ t('admin.backup.imageStorage.title') }}
            </h3>
            <p class="mt-1 text-sm text-[var(--muted-foreground)]">
              {{ t('admin.backup.imageStorage.description') }}
            </p>
          </div>
          <label class="chip cursor-pointer text-xs font-medium">
            <input v-model="imageStorageForm.enabled" type="checkbox" />
            <span>{{ t('admin.backup.imageStorage.enabled') }}</span>
          </label>
        </div>

        <label class="inline-flex items-center gap-2 text-sm text-[var(--foreground)]">
          <input v-model="imageStorageForm.reuse_backup_s3" type="checkbox" />
          <span>{{ t('admin.backup.imageStorage.reuseBackupS3') }}</span>
        </label>

        <div class="mt-3 grid grid-cols-1 gap-3 md:grid-cols-2">
          <div>
            <label for="backup-image-bucket" class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">{{ t('admin.backup.imageStorage.bucket') }}</label>
            <input id="backup-image-bucket" v-model="imageStorageForm.bucket" class="input w-full font-mono tabular-nums text-xs" :placeholder="imageStorageForm.reuse_backup_s3 ? t('admin.backup.imageStorage.bucketInherited') : ''" />
          </div>
          <div>
            <label for="backup-image-prefix" class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">{{ t('admin.backup.imageStorage.prefix') }}</label>
            <input id="backup-image-prefix" v-model="imageStorageForm.prefix" class="input w-full font-mono tabular-nums text-xs" placeholder="images/" />
          </div>

          <template v-if="!imageStorageForm.reuse_backup_s3">
            <div>
              <label for="backup-image-endpoint" class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">{{ t('admin.backup.s3.endpoint') }}</label>
              <input id="backup-image-endpoint" v-model="imageStorageForm.endpoint" class="input w-full font-mono tabular-nums text-xs" placeholder="https://<account_id>.r2.cloudflarestorage.com" />
            </div>
            <div>
              <label for="backup-image-region" class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">{{ t('admin.backup.s3.region') }}</label>
              <input id="backup-image-region" v-model="imageStorageForm.region" class="input w-full font-mono tabular-nums text-xs" placeholder="auto" />
            </div>
            <div>
              <label for="backup-image-access-key-id" class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">{{ t('admin.backup.s3.accessKeyId') }}</label>
              <input id="backup-image-access-key-id" v-model="imageStorageForm.access_key_id" class="input w-full font-mono tabular-nums text-xs" />
            </div>
            <div>
              <label for="backup-image-secret-access-key" class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">{{ t('admin.backup.s3.secretAccessKey') }}</label>
              <input id="backup-image-secret-access-key" v-model="imageStorageForm.secret_access_key" type="password" class="input w-full font-mono tabular-nums text-xs" :placeholder="imageStorageSecretConfigured ? t('admin.backup.s3.secretConfigured') : ''" />
            </div>
            <label class="inline-flex items-center gap-2 text-sm text-[var(--foreground)] md:col-span-2">
              <input v-model="imageStorageForm.force_path_style" type="checkbox" />
              <span>{{ t('admin.backup.s3.forcePathStyle') }}</span>
            </label>
          </template>

          <div>
            <label for="backup-image-public-base-url" class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">{{ t('admin.backup.imageStorage.publicBaseUrl') }}</label>
            <input id="backup-image-public-base-url" v-model="imageStorageForm.public_base_url" class="input w-full font-mono tabular-nums text-xs" :placeholder="t('admin.backup.imageStorage.publicBaseUrlPlaceholder')" />
          </div>
          <div>
            <label for="backup-image-presign-expiry-hours" class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">{{ t('admin.backup.imageStorage.presignExpiryHours') }}</label>
            <input id="backup-image-presign-expiry-hours" v-model.number="imageStorageForm.presign_expiry_hours" type="number" min="1" class="input w-full font-mono text-xs tabular-nums" />
          </div>
        </div>

        <div class="mt-4 flex flex-wrap gap-2 border-t pt-3.5" style="border-color: var(--border)">
          <button type="button" class="btn btn-secondary btn-sm" :disabled="testingImageStorage" @click="testImageStorage">
            {{ testingImageStorage ? t('common.loading') : t('admin.backup.s3.testConnection') }}
          </button>
          <button type="button" class="btn btn-primary btn-sm" :disabled="savingImageStorage" @click="saveImageStorageConfig">
            {{ savingImageStorage ? t('common.loading') : t('common.save') }}
          </button>
        </div>
      </div>

      <!-- Schedule Config -->
      <div class="card p-5">
        <div
          class="mb-4 flex flex-wrap items-center justify-between gap-3 border-b pb-3.5"
          style="border-color: var(--border)"
        >
          <div>
            <h3 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
              {{ t('admin.backup.schedule.title') }}
            </h3>
            <p class="mt-1 text-sm text-[var(--muted-foreground)]">
              {{ t('admin.backup.schedule.description') }}
            </p>
          </div>
          <label class="chip cursor-pointer text-xs font-medium">
            <input v-model="scheduleForm.enabled" type="checkbox" />
            <span>{{ t('admin.backup.schedule.enabled') }}</span>
          </label>
        </div>
        <div class="grid grid-cols-1 gap-3 md:grid-cols-3">
          <div>
            <label for="backup-schedule-cron-expr" class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">{{ t('admin.backup.schedule.cronExpr') }}</label>
            <input id="backup-schedule-cron-expr" v-model="scheduleForm.cron_expr" class="input w-full font-mono tabular-nums text-xs" placeholder="0 2 * * *" />
            <p class="mt-1 text-xs text-[var(--muted-foreground)]">{{ t('admin.backup.schedule.cronHint') }}</p>
          </div>
          <div>
            <label for="backup-schedule-retain-days" class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">{{ t('admin.backup.schedule.retainDays') }}</label>
            <input id="backup-schedule-retain-days" v-model.number="scheduleForm.retain_days" type="number" min="0" class="input w-full font-mono text-xs tabular-nums" />
            <p class="mt-1 text-xs text-[var(--muted-foreground)]">{{ t('admin.backup.schedule.retainDaysHint') }}</p>
          </div>
          <div>
            <label for="backup-schedule-retain-count" class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">{{ t('admin.backup.schedule.retainCount') }}</label>
            <input id="backup-schedule-retain-count" v-model.number="scheduleForm.retain_count" type="number" min="0" class="input w-full font-mono text-xs tabular-nums" />
            <p class="mt-1 text-xs text-[var(--muted-foreground)]">{{ t('admin.backup.schedule.retainCountHint') }}</p>
          </div>
        </div>
        <div class="mt-4 border-t pt-3.5" style="border-color: var(--border)">
          <button type="button" class="btn btn-primary btn-sm" :disabled="savingSchedule" @click="saveSchedule">
            {{ savingSchedule ? t('common.loading') : t('common.save') }}
          </button>
        </div>
      </div>

      <RestoreWorkflow :backup-id="selectedRestoreBackup" @lock="restoreLocked = $event" />

      <!-- Backup Operations -->
      <div class="card p-5">
        <div
          class="mb-4 flex flex-wrap items-center justify-between gap-3 border-b pb-3.5"
          style="border-color: var(--border)"
        >
          <div>
            <h3 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
              {{ t('admin.backup.operations.title') }}
            </h3>
            <p class="mt-1 text-sm text-[var(--muted-foreground)]">
              {{ t('admin.backup.operations.description') }}
            </p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <div class="flex items-center gap-1.5">
              <label for="backup-manual-expire-days" class="font-mono tabular-nums text-xs text-[var(--muted-foreground)]">{{ t('admin.backup.operations.expireDays') }}</label>
              <input id="backup-manual-expire-days" v-model.number="manualExpireDays" type="number" min="0" class="input w-20 py-1.5 font-mono text-xs tabular-nums" />
            </div>
            <button type="button" class="btn btn-primary btn-sm" :disabled="creatingBackup" @click="createBackup">
              {{ creatingBackup ? t('admin.backup.operations.backing') : t('admin.backup.operations.createBackup') }}
            </button>
            <button type="button" class="btn btn-secondary btn-sm" :disabled="loadingBackups" @click="loadBackups">
              {{ loadingBackups ? t('common.loading') : t('common.refresh') }}
            </button>
          </div>
        </div>

        <div class="table-container">
          <table class="w-full min-w-[800px] text-sm">
            <thead>
              <tr
                class="border-b text-left font-mono tabular-nums text-[11px] uppercase tracking-wider text-[var(--muted-foreground)]"
                style="border-color: var(--border); background: var(--surface-2)"
              >
                <th class="px-3 py-2.5">ID</th>
                <th class="px-3 py-2.5">{{ t('admin.backup.columns.status') }}</th>
                <th class="px-3 py-2.5">{{ t('admin.backup.columns.fileName') }}</th>
                <th class="px-3 py-2.5">{{ t('admin.backup.columns.size') }}</th>
                <th class="px-3 py-2.5">{{ t('admin.backup.columns.parts') }}</th>
                <th class="px-3 py-2.5">{{ t('admin.backup.columns.expiresAt') }}</th>
                <th class="px-3 py-2.5">{{ t('admin.backup.columns.triggeredBy') }}</th>
                <th class="px-3 py-2.5">{{ t('admin.backup.columns.startedAt') }}</th>
                <th class="px-3 py-2.5">{{ t('admin.backup.columns.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="record in backups"
                :key="record.id"
                class="border-b align-middle transition-colors hover:bg-[var(--surface-2)]"
                style="border-color: var(--border)"
              >
                <td class="px-3 py-3 font-mono text-xs tabular-nums">{{ record.id }}</td>
                <td class="px-3 py-3">
                  <span
                    class="inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium"
                    :class="statusClass(record.status)"
                  >
                    <span class="pulse-dot" :class="statusDotClass(record.status)"></span>
                    {{ record.status === 'running' && record.progress
                      ? t(`admin.backup.progress.${record.progress}`)
                      : t(`admin.backup.status.${record.status}`) }}
                  </span>
                </td>
                <td class="px-3 py-3 font-mono tabular-nums text-xs">{{ record.file_name }}</td>
                <td class="px-3 py-3 font-mono text-xs tabular-nums">{{ formatSize(record.size_bytes) }}</td>
                <td class="px-3 py-3 font-mono text-xs tabular-nums">{{ record.parts?.length || (record.status === 'running' ? '—' : 1) }}</td>
                <td class="px-3 py-3 font-mono text-xs tabular-nums">
                  {{ record.expires_at ? formatDate(record.expires_at) : t('admin.backup.neverExpire') }}
                </td>
                <td class="px-3 py-3 text-xs">
                  <span class="pill pill-ghost font-mono tabular-nums text-[11px]">
                    {{ record.triggered_by === 'scheduled' ? t('admin.backup.trigger.scheduled') : t('admin.backup.trigger.manual') }}
                  </span>
                </td>
                <td class="px-3 py-3 font-mono text-xs tabular-nums">{{ formatDate(record.started_at) }}</td>
                <td class="px-3 py-3 text-xs">
                  <div class="flex flex-wrap gap-1">
                    <button
                      v-if="record.status === 'completed'"
                      type="button"
                      class="btn btn-secondary btn-sm px-2.5 py-1 text-xs"
                      @click="downloadBackup(record.id)"
                    >
                      {{ t('admin.backup.actions.download') }}
                    </button>
                    <button
                      v-if="record.status === 'completed'"
                      type="button"
                      class="btn btn-secondary btn-sm px-2.5 py-1 text-xs"
                      :disabled="restoreLocked"
                      @click="selectRestoreBackup(record.id)"
                    >
                      {{ t('admin.backup.actions.restore') }}
                    </button>
                    <button
                      v-if="record.status !== 'running'"
                      type="button"
                      class="btn btn-danger btn-sm px-2.5 py-1 text-xs"
                      @click="removeBackup(record.id)"
                    >
                      {{ t('common.delete') }}
                    </button>
                  </div>
                </td>
              </tr>
              <tr v-if="backups.length === 0">
                <td colspan="9" class="py-8 text-center text-sm text-[var(--muted-foreground)]">
                  {{ t('admin.backup.empty') }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- Cloudflare R2 Setup Guide Modal -->
    <teleport to="body">
      <transition name="modal">
        <div v-if="showR2Guide" class="fixed inset-0 z-50 flex items-center justify-center p-4" @mousedown.self="showR2Guide = false">
          <div class="fixed inset-0 bg-black/50" @click="showR2Guide = false"></div>
          <div class="relative max-h-[85vh] w-full max-w-2xl overflow-y-auto rounded-xl border border-[var(--border)] bg-[var(--card)] p-6 shadow-2xl">
            <button type="button" class="absolute right-4 top-4 text-[var(--muted-foreground)] hover:text-[var(--muted-foreground)]" @click="showR2Guide = false">
              <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
            </button>

            <h2 class="mb-4 text-lg font-bold text-[var(--foreground)]">{{ t('admin.backup.r2Guide.title') }}</h2>
            <p class="mb-4 text-sm text-[var(--muted-foreground)]">{{ t('admin.backup.r2Guide.intro') }}</p>

            <!-- Step 1 -->
            <div class="mb-5">
              <h3 class="mb-2 flex items-center gap-2 text-sm font-semibold text-[var(--foreground)]">
                <span class="flex h-6 w-6 items-center justify-center rounded-full bg-[var(--accent)] text-xs font-bold text-[var(--brand-text)]">1</span>
                {{ t('admin.backup.r2Guide.step1.title') }}
              </h3>
              <ol class="ml-8 list-decimal space-y-1 text-sm text-[var(--muted-foreground)]">
                <li>{{ t('admin.backup.r2Guide.step1.line1') }}</li>
                <li>{{ t('admin.backup.r2Guide.step1.line2') }}</li>
                <li>{{ t('admin.backup.r2Guide.step1.line3') }}</li>
              </ol>
            </div>

            <!-- Step 2 -->
            <div class="mb-5">
              <h3 class="mb-2 flex items-center gap-2 text-sm font-semibold text-[var(--foreground)]">
                <span class="flex h-6 w-6 items-center justify-center rounded-full bg-[var(--accent)] text-xs font-bold text-[var(--brand-text)]">2</span>
                {{ t('admin.backup.r2Guide.step2.title') }}
              </h3>
              <ol class="ml-8 list-decimal space-y-1 text-sm text-[var(--muted-foreground)]">
                <li>{{ t('admin.backup.r2Guide.step2.line1') }}</li>
                <li>{{ t('admin.backup.r2Guide.step2.line2') }}</li>
                <li>{{ t('admin.backup.r2Guide.step2.line3') }}</li>
                <li>{{ t('admin.backup.r2Guide.step2.line4') }}</li>
              </ol>
              <div class="mt-2 rounded-lg bg-amber-50 p-3 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">
                {{ t('admin.backup.r2Guide.step2.warning') }}
              </div>
            </div>

            <!-- Step 3 -->
            <div class="mb-5">
              <h3 class="mb-2 flex items-center gap-2 text-sm font-semibold text-[var(--foreground)]">
                <span class="flex h-6 w-6 items-center justify-center rounded-full bg-[var(--accent)] text-xs font-bold text-[var(--brand-text)]">3</span>
                {{ t('admin.backup.r2Guide.step3.title') }}
              </h3>
              <p class="ml-8 text-sm text-[var(--muted-foreground)]">{{ t('admin.backup.r2Guide.step3.desc') }}</p>
              <code class="ml-8 mt-1 block break-all rounded bg-[var(--surface-2)] px-3 py-2 text-xs text-[var(--foreground)]">https://&lt;{{ t('admin.backup.r2Guide.step3.accountId') }}&gt;.r2.cloudflarestorage.com</code>
            </div>

            <!-- Step 4: Fill form -->
            <div class="mb-5">
              <h3 class="mb-2 flex items-center gap-2 text-sm font-semibold text-[var(--foreground)]">
                <span class="flex h-6 w-6 items-center justify-center rounded-full bg-[var(--accent)] text-xs font-bold text-[var(--brand-text)]">4</span>
                {{ t('admin.backup.r2Guide.step4.title') }}
              </h3>
              <div class="ml-8 overflow-hidden rounded-lg border border-[var(--border)]">
                <table class="w-full text-sm">
                  <tbody>
                    <tr v-for="(row, i) in r2ConfigRows" :key="i" class="border-b border-[var(--border)] last:border-0">
                      <td class="whitespace-nowrap bg-[var(--surface-2)] px-3 py-2 font-medium text-[var(--foreground)]">{{ row.field }}</td>
                      <td class="px-3 py-2 text-[var(--muted-foreground)]"><code class="text-xs">{{ row.value }}</code></td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>

            <!-- Free tier note -->
            <div class="rounded-lg bg-green-50 p-3 text-xs text-green-700 dark:bg-green-900/20 dark:text-green-300">
              {{ t('admin.backup.r2Guide.freeTier') }}
            </div>

            <div class="mt-4 text-right">
              <button type="button" class="btn btn-primary btn-sm" @click="showR2Guide = false">{{ t('common.close') }}</button>
            </div>
          </div>
        </div>
      </transition>
    </teleport>
    <!-- 分卷下载链接 -->
    <teleport to="body">
      <transition name="modal">
        <div
          v-if="downloadPartsModalOpen"
          class="fixed inset-0 z-50 flex items-center justify-center p-4"
          @mousedown.self="closeDownloadParts"
        >
          <div class="fixed inset-0 bg-black/50" @click="closeDownloadParts"></div>
          <div class="relative max-h-[85vh] w-full max-w-lg overflow-y-auto rounded-xl border border-[var(--border)] bg-[var(--card)] p-6 shadow-2xl">
            <button
              type="button"
              class="absolute right-4 top-4 text-[var(--muted-foreground)] hover:text-[var(--muted-foreground)]"
              :aria-label="t('common.close')"
              @click="closeDownloadParts"
            >
              <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
            </button>
            <h2 class="mb-1 text-lg font-bold text-[var(--foreground)]">{{ t('admin.backup.actions.downloadParts') }}</h2>
            <p class="mb-4 text-sm text-[var(--muted-foreground)]">{{ t('admin.backup.actions.downloadPartsHint') }}</p>
            <div class="space-y-2">
              <div
                v-for="part in downloadParts"
                :key="part.index"
                class="flex items-center justify-between gap-3 rounded-lg border border-[var(--border)] px-3 py-2"
              >
                <span class="text-sm text-[var(--foreground)]">
                  {{ t('admin.backup.actions.partLabel', { index: part.index }) }}
                  <span class="ml-2 text-xs text-[var(--muted-foreground)]">{{ formatSize(part.size_bytes) }}</span>
                </span>
                <a :href="part.url" class="btn btn-secondary btn-xs" rel="noopener">
                  {{ t('admin.backup.actions.download') }}
                </a>
              </div>
            </div>
            <div class="mt-4 text-right">
              <button type="button" class="btn btn-primary btn-sm" @click="closeDownloadParts">{{ t('common.close') }}</button>
            </div>
          </div>
        </div>
      </transition>
    </teleport>
    <TotpStepUpDialog :controller="backupStepUp" />
  </component>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, getCurrentInstance, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api'
import { useAppStore } from '@/stores'
import type {
  BackupS3Config,
  BackupScheduleConfig,
  BackupRecord,
  BackupDownloadPart,
  ImageStorageConfig,
} from '@/api/admin/backup'
import { useStepUp, isStepUpBlocked, isStepUpCancelled, stepUpBlockReason } from '@/composables/useStepUp'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import RestoreWorkflow from '@/features/operations/RestoreWorkflow.vue'

const { t } = useI18n()
const appStore = useAppStore()
const backupStepUp = useStepUp()
const instance = getCurrentInstance()
const AsyncAppLayout = defineAsyncComponent(() => import('@/components/layout/AppLayout.vue'))
const shellComponent = computed(() =>
  instance?.appContext.config.globalProperties.$route ? AsyncAppLayout : 'div'
)

// 敏感操作被 2FA 门控拦截时的统一提示。
function reportStepUpBlocked(error: unknown): boolean {
  if (!isStepUpBlocked(error)) return false
  appStore.showError(
    stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'
      ? t('stepUp.adminApiKeyForbidden')
      : t('stepUp.notEnabled')
  )
  return true
}

// S3 config
const s3Form = ref<BackupS3Config>({
  endpoint: '',
  region: 'auto',
  bucket: '',
  access_key_id: '',
  secret_access_key: '',
  prefix: 'backups/',
  force_path_style: false,
})
const s3SecretConfigured = ref(false)
const savingS3 = ref(false)
const testingS3 = ref(false)

// Async image object storage. Shares the S3 client with backups, so the default is
// to reuse the credentials configured above and only differ by prefix.
const imageStorageForm = ref<ImageStorageConfig>({
  enabled: false,
  reuse_backup_s3: true,
  bucket: '',
  prefix: 'images/',
  public_base_url: '',
  presign_expiry_hours: 24,
  max_download_bytes: 33554432,
  endpoint: '',
  region: 'auto',
  access_key_id: '',
  secret_access_key: '',
  force_path_style: false,
})
const imageStorageSecretConfigured = ref(false)
const savingImageStorage = ref(false)
const testingImageStorage = ref(false)

// Schedule config
const scheduleForm = ref<BackupScheduleConfig>({
  enabled: false,
  cron_expr: '0 2 * * *',
  retain_days: 14,
  retain_count: 10,
})
const savingSchedule = ref(false)

// Backups
const backups = ref<BackupRecord[]>([])
const loadingBackups = ref(false)
const creatingBackup = ref(false)
const selectedRestoreBackup = ref('')
const restoreLocked = ref(false)
const manualExpireDays = ref(14)
const downloadParts = ref<BackupDownloadPart[]>([])
const downloadPartsModalOpen = ref(false)

// Polling
const pollingTimer = ref<ReturnType<typeof setInterval> | null>(null)
const MAX_POLL_COUNT = 900

function updateRecordInList(updated: BackupRecord) {
  const idx = backups.value.findIndex(r => r.id === updated.id)
  if (idx >= 0) {
    backups.value[idx] = updated
  }
}

function startPolling(backupId: string) {
  stopPolling()
  let count = 0
  pollingTimer.value = setInterval(async () => {
    if (count++ >= MAX_POLL_COUNT) {
      stopPolling()
      creatingBackup.value = false
      appStore.showWarning(t('admin.backup.operations.backupRunning'))
      return
    }
    try {
      const record = await adminAPI.backup.getBackup(backupId)
      updateRecordInList(record)
      if (record.status === 'completed' || record.status === 'failed') {
        stopPolling()
        creatingBackup.value = false
        if (record.status === 'completed') {
          appStore.showSuccess(t('admin.backup.operations.backupCreated'))
        } else {
          appStore.showError(record.error_message || t('admin.backup.operations.backupFailed'))
        }
        await loadBackups()
      }
    } catch {
      // 轮询失败时不中断
    }
  }, 2000)
}

function stopPolling() {
  if (pollingTimer.value) {
    clearInterval(pollingTimer.value)
    pollingTimer.value = null
  }
}

function handleVisibilityChange() {
  if (document.hidden) {
    stopPolling()
  } else {
    // 标签页恢复时刷新列表，检查是否仍有活跃操作
    loadBackups().then(() => {
      const running = backups.value.find(r => r.status === 'running')
      if (running) {
        creatingBackup.value = true
        startPolling(running.id)
      }
    })
  }
}

// R2 guide
const showR2Guide = ref(false)
const r2ConfigRows = computed(() => [
  { field: t('admin.backup.s3.endpoint'), value: 'https://<account_id>.r2.cloudflarestorage.com' },
  { field: t('admin.backup.s3.region'), value: 'auto' },
  { field: t('admin.backup.s3.bucket'), value: t('admin.backup.r2Guide.step4.bucketValue') },
  { field: t('admin.backup.s3.prefix'), value: 'backups/' },
  { field: 'Access Key ID', value: t('admin.backup.r2Guide.step4.fromStep2') },
  { field: 'Secret Access Key', value: t('admin.backup.r2Guide.step4.fromStep2') },
  { field: t('admin.backup.s3.forcePathStyle'), value: t('admin.backup.r2Guide.step4.unchecked') },
])

async function loadS3Config() {
  try {
    const cfg = await adminAPI.backup.getS3Config()
    s3Form.value = {
      endpoint: cfg?.endpoint || '',
      region: cfg?.region || 'auto',
      bucket: cfg?.bucket || '',
      access_key_id: cfg?.access_key_id || '',
      secret_access_key: '',
      prefix: cfg?.prefix || 'backups/',
      force_path_style: Boolean(cfg?.force_path_style),
    }
    s3SecretConfigured.value = Boolean(cfg?.access_key_id)
  } catch (error) {
    appStore.showError((error as { message?: string })?.message || t('errors.networkError'))
  }
}

async function saveS3Config() {
  savingS3.value = true
  try {
    await backupStepUp.run(() => adminAPI.backup.updateS3Config(s3Form.value))
    appStore.showSuccess(t('admin.backup.s3.saved'))
    await loadS3Config()
  } catch (error) {
    if (isStepUpCancelled(error)) {
      savingS3.value = false
      return
    }
    appStore.showError((error as { message?: string })?.message || t('errors.networkError'))
  } finally {
    savingS3.value = false
  }
}

async function loadImageStorageConfig() {
  try {
    const res = await adminAPI.backup.getImageStorageConfig()
    const config = res?.config
    imageStorageForm.value = {
      ...imageStorageForm.value,
      ...config,
      prefix: config?.prefix || 'images/',
      region: config?.region || 'auto',
      secret_access_key: '',
    }
    imageStorageSecretConfigured.value = Boolean(res?.secret_configured)
  } catch (error) {
    appStore.showError((error as { message?: string })?.message || t('errors.networkError'))
  }
}

async function saveImageStorageConfig() {
  savingImageStorage.value = true
  try {
    await backupStepUp.run(() => adminAPI.backup.updateImageStorageConfig(imageStorageForm.value))
    appStore.showSuccess(t('admin.backup.imageStorage.saved'))
    await loadImageStorageConfig()
  } catch (error) {
    if (isStepUpCancelled(error)) {
      savingImageStorage.value = false
      return
    }
    appStore.showError((error as { message?: string })?.message || t('errors.networkError'))
  } finally {
    savingImageStorage.value = false
  }
}

async function testImageStorage() {
  testingImageStorage.value = true
  try {
    const result = await adminAPI.backup.testImageStorageConnection(imageStorageForm.value)
    if (result.ok) {
      appStore.showSuccess(result.message || t('admin.backup.s3.testSuccess'))
    } else {
      appStore.showError(result.message || t('admin.backup.s3.testFailed'))
    }
  } catch (error) {
    appStore.showError((error as { message?: string })?.message || t('errors.networkError'))
  } finally {
    testingImageStorage.value = false
  }
}

async function testS3() {
  testingS3.value = true
  try {
    const result = await adminAPI.backup.testS3Connection(s3Form.value)
    if (result.ok) {
      appStore.showSuccess(result.message || t('admin.backup.s3.testSuccess'))
    } else {
      appStore.showError(result.message || t('admin.backup.s3.testFailed'))
    }
  } catch (error) {
    appStore.showError((error as { message?: string })?.message || t('errors.networkError'))
  } finally {
    testingS3.value = false
  }
}

async function loadSchedule() {
  try {
    const cfg = await adminAPI.backup.getSchedule()
    scheduleForm.value = {
      enabled: cfg.enabled,
      cron_expr: cfg.cron_expr || '0 2 * * *',
      retain_days: cfg.retain_days || 14,
      retain_count: cfg.retain_count || 10,
    }
  } catch (error) {
    appStore.showError((error as { message?: string })?.message || t('errors.networkError'))
  }
}

async function saveSchedule() {
  savingSchedule.value = true
  try {
    await adminAPI.backup.updateSchedule(scheduleForm.value)
    appStore.showSuccess(t('admin.backup.schedule.saved'))
  } catch (error) {
    appStore.showError((error as { message?: string })?.message || t('errors.networkError'))
  } finally {
    savingSchedule.value = false
  }
}

async function loadBackups() {
  loadingBackups.value = true
  try {
    const result = await adminAPI.backup.listBackups()
    backups.value = result.items || []
  } catch (error) {
    appStore.showError((error as { message?: string })?.message || t('errors.networkError'))
  } finally {
    loadingBackups.value = false
  }
}

async function createBackup() {
  creatingBackup.value = true
  try {
    const record = await backupStepUp.run(() => adminAPI.backup.createBackup({ expire_days: manualExpireDays.value }))
    // 插入到列表顶部
    backups.value.unshift(record)
    startPolling(record.id)
  } catch (error: any) {
    if (isStepUpCancelled(error)) {
      creatingBackup.value = false
      return
    }
    if (reportStepUpBlocked(error)) {
      creatingBackup.value = false
      return
    }
    if (error?.response?.status === 409) {
      appStore.showWarning(t('admin.backup.operations.alreadyInProgress'))
    } else {
      appStore.showError(error?.message || t('errors.networkError'))
    }
    creatingBackup.value = false
  }
}

async function downloadBackup(id: string) {
  try {
    const result = await backupStepUp.run(() => adminAPI.backup.getDownloadURL(id))
    if (result.parts && result.parts.length > 0) {
      downloadParts.value = result.parts
      downloadPartsModalOpen.value = true
      return
    }
    if (!result.url) {
      throw new Error(t('admin.backup.actions.downloadFailed'))
    }
    // 预签名 URL 带 attachment disposition，同页 anchor 导航直接触发下载；
    // 不用 window.open：step-up 弹窗 await 会耗尽瞬态用户激活，新标签页会被浏览器拦截。
    const link = document.createElement('a')
    link.href = result.url
    link.rel = 'noopener'
    link.click()
  } catch (error) {
    if (isStepUpCancelled(error)) return
    if (reportStepUpBlocked(error)) return
    appStore.showError((error as { message?: string })?.message || t('errors.networkError'))
  }
}

function closeDownloadParts() {
  downloadPartsModalOpen.value = false
  downloadParts.value = []
}

async function selectRestoreBackup(id: string) {
  selectedRestoreBackup.value = id
  await nextTick()
  document.getElementById('restore-workflow-title')?.focus()
}

async function removeBackup(id: string) {
  if (!window.confirm(t('admin.backup.actions.deleteConfirm'))) return
  try {
    await adminAPI.backup.deleteBackup(id)
    appStore.showSuccess(t('admin.backup.actions.deleted'))
    await loadBackups()
  } catch (error) {
    appStore.showError((error as { message?: string })?.message || t('errors.networkError'))
  }
}

function statusClass(status: string): string {
  switch (status) {
    case 'completed':
      return 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300'
    case 'running':
      return 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
    case 'failed':
      return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
    default:
      return 'bg-[var(--surface-2)] text-[var(--foreground)]'
  }
}

function statusDotClass(status: string): string {
  switch (status) {
    case 'completed':
      return 'pulse-ok'
    case 'failed':
      return 'pulse-bad'
    default:
      return 'pulse-warn'
  }
}

function formatSize(bytes: number): string {
  if (!bytes || bytes <= 0) return '—'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function formatDate(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleString()
}

onMounted(async () => {
  document.addEventListener('visibilitychange', handleVisibilityChange)
  await Promise.all([loadS3Config(), loadImageStorageConfig(), loadSchedule(), loadBackups()])

  // 如果有正在 running 的备份，恢复轮询
  const runningBackup = backups.value.find(r => r.status === 'running')
  if (runningBackup) {
    creatingBackup.value = true
    startPolling(runningBackup.id)
  }
})

onBeforeUnmount(() => {
  stopPolling()
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})
</script>

<style scoped>
.modal-enter-active,
.modal-leave-active {
  transition: opacity 0.2s ease;
}
.modal-enter-from,
.modal-leave-to {
  opacity: 0;
}
button:focus-visible,
a:focus-visible,
input:focus-visible,
select:focus-visible,
label.chip:focus-within {
  outline: 2px solid var(--oh-focus-ring);
  outline-offset: 2px;
}
</style>
