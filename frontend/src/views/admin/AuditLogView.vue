<template>
  <AppLayout>
    <TablePageLayout>
      <!-- opencode Audit Style Compact Filter Bar -->
      <template #filters>
        <div class="card space-y-3 p-4">
          <!-- Workbench Status & Action Header -->
          <div
            class="flex flex-wrap items-center justify-between gap-2 border-b pb-3"
            style="border-color: var(--border)"
          >
            <div class="flex flex-wrap items-center gap-2">
              <span class="chip font-mono tabular-nums text-xs font-medium">
                <span class="pulse-dot" :class="loading ? 'pulse-warn' : 'pulse-ok'"></span>
                <span>AUDIT STREAM</span>
              </span>
              <span class="pill pill-ghost font-mono text-xs tabular-nums">
                {{ total }} events
              </span>
            </div>

            <div class="flex flex-wrap items-center gap-2">
              <button type="button" class="btn btn-primary btn-sm" :disabled="loading" @click="search">
                {{ t('common.search') }}
              </button>
              <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="resetFilters">
                {{ t('common.reset') }}
              </button>
              <button type="button" class="btn btn-danger btn-sm" @click="openClearDialog">
                <Icon name="trash" size="sm" />
                {{ t('admin.audit.clearAll') }}
              </button>
            </div>
          </div>

          <!-- High-Density Filter Grid -->
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <div>
              <label class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">
                {{ t('admin.audit.filters.q') }}
              </label>
              <div class="relative">
                <Icon
                  name="search"
                  size="sm"
                  class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-[var(--muted-foreground)]"
                />
                <input
                  v-model.trim="filters.q"
                  type="text"
                  class="input py-2 pl-9 font-mono tabular-nums text-xs"
                  :aria-label="t('admin.audit.filters.qPlaceholder')"
                  :placeholder="t('admin.audit.filters.qPlaceholder')"
                  @keyup.enter="search"
                />
              </div>
            </div>

            <div>
              <label for="audit-filter-actor-email" class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">
                {{ t('admin.audit.filters.actorEmail') }}
              </label>
              <input
                id="audit-filter-actor-email"
                v-model.trim="filters.actor_email"
                type="text"
                class="input py-2 font-mono tabular-nums text-xs"
                @keyup.enter="search"
              />
            </div>

            <div>
              <label for="audit-filter-action" class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">
                {{ t('admin.audit.filters.action') }}
              </label>
              <input
                id="audit-filter-action"
                v-model.trim="filters.action"
                type="text"
                class="input py-2 font-mono tabular-nums text-xs"
                @keyup.enter="search"
              />
            </div>

            <div>
              <label for="audit-filter-client-ip" class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">
                {{ t('admin.audit.filters.clientIp') }}
              </label>
              <input
                id="audit-filter-client-ip"
                v-model.trim="filters.client_ip"
                type="text"
                class="input py-2 font-mono tabular-nums text-xs"
                @keyup.enter="search"
              />
            </div>

            <div>
              <label class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">
                {{ t('admin.audit.filters.method') }}
              </label>
              <Select v-model="filters.method" :options="methodOptions" @change="search" />
            </div>

            <div>
              <label class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">
                {{ t('admin.audit.filters.authMethod') }}
              </label>
              <Select v-model="filters.auth_method" :options="authMethodOptions" @change="search" />
            </div>

            <div>
              <label class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">
                {{ t('admin.audit.filters.result') }}
              </label>
              <Select v-model="filters.success" :options="resultOptions" @change="search" />
            </div>

            <div>
              <label class="mb-1 block font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">
                {{ t('admin.dashboard.timeRange') }}
              </label>
              <Select
                :model-value="timeRange"
                :options="timeRangeOptions"
                @update:model-value="handleTimeRangeChange"
              />
            </div>
          </div>
        </div>
      </template>

      <!-- Table -->
      <template #table>
        <DataTable :columns="columns" :data="logs" :loading="loading" row-key="id">
          <template #cell-created_at="{ value }">
            <span class="whitespace-nowrap font-mono text-xs tabular-nums text-[var(--muted-foreground)]">
              {{ formatTime(value) }}
            </span>
          </template>

          <template #cell-actor="{ row }">
            <div class="min-w-0 max-w-[220px]">
              <div class="truncate text-sm font-medium text-[var(--foreground)]" :title="row.actor_email">
                {{ row.actor_email || '—' }}
              </div>
              <div class="mt-1 flex flex-wrap items-center gap-1">
                <span v-if="row.actor_role" class="pill pill-ghost font-mono tabular-nums text-[10px]">
                  {{ row.actor_role }}
                </span>
                <span v-if="row.auth_method" class="pill pill-ghost font-mono tabular-nums text-[10px]">
                  {{ authMethodLabel(row.auth_method) }}
                </span>
              </div>
            </div>
          </template>

          <template #cell-action="{ row }">
            <div class="min-w-0 max-w-md space-y-1">
              <div class="flex flex-col gap-0.5">
                <div class="truncate text-xs font-semibold text-[var(--foreground)]" :title="formatAuditActionLabel(row.action)">
                  {{ formatAuditActionLabel(row.action) }}
                </div>
                <div
                  v-if="shouldShowRawActionCode(row.action)"
                  class="truncate font-mono text-[11px] tabular-nums text-[var(--muted-foreground)] opacity-65"
                  :title="row.action"
                >
                  {{ row.action }}
                </div>
              </div>
              <div
                class="select-text break-all rounded-md border px-2 py-1 font-mono tabular-nums text-xs text-[var(--muted-foreground)]"
                style="border-color: var(--border); background: var(--surface-2)"
                data-testid="audit-request-path"
              >{{ auditRequestText(row) }}</div>
              <button
                type="button"
                class="btn btn-secondary btn-sm px-2 py-0.5 font-mono tabular-nums text-[11px]"
                :aria-label="`${t('common.copy')} ${auditRequestText(row)}`"
                @click="copyToClipboard(auditRequestText(row))"
              >{{ t('common.copy') }}</button>
            </div>
          </template>

          <template #cell-status_code="{ row }">
            <span :class="statusBadgeClass(row.status_code)">
              <span class="pulse-dot" :class="statusDotClass(row.status_code)"></span>
              {{ row.status_code }}
            </span>
          </template>

          <template #cell-latency_ms="{ value }">
            <span class="pill pill-ghost whitespace-nowrap font-mono text-xs tabular-nums">
              {{ value }} ms
            </span>
          </template>

          <template #cell-client_ip="{ value }">
            <span class="whitespace-nowrap font-mono text-xs tabular-nums text-[var(--muted-foreground)]">
              {{ value || '—' }}
            </span>
          </template>

          <template #cell-actions="{ row }">
            <button
              type="button"
              class="btn btn-ghost btn-sm inline-flex items-center gap-1 text-xs font-medium text-[var(--brand-text)] transition-colors hover:text-[var(--brand-text)]"
              @click="openDetail(row.id)"
            >
              <Icon name="eye" size="sm" />
              {{ t('admin.audit.columns.detail') }}
            </button>
          </template>

          <template #empty>
            <div class="flex flex-col items-center py-10">
              <div class="mb-4 flex flex-col gap-2 opacity-40" aria-hidden="true">
                <div v-for="i in 3" :key="i" class="flex items-center gap-2.5">
                  <span class="h-2.5 w-2.5 rounded-full border border-current"></span>
                  <span class="h-2 w-40 rounded" style="background: var(--border)"></span>
                </div>
              </div>
              <Icon name="shield" size="xl" class="mb-4 h-12 w-12 text-[var(--muted-foreground)]" />
              <p class="text-sm font-medium text-[var(--muted-foreground)]">{{ t('admin.audit.empty') }}</p>
            </div>
          </template>
        </DataTable>
      </template>

      <!-- Pagination -->
      <template #pagination>
        <Pagination
          v-if="total > 0"
          :total="total"
          :page="page"
          :page-size="pageSize"
          @update:page="onPageChange"
          @update:pageSize="onPageSizeChange"
        />
      </template>
    </TablePageLayout>

    <!-- Detail dialog -->
    <BaseDialog
      :show="detailVisible"
      :title="t('admin.audit.detail.title')"
      width="wide"
      :close-on-click-outside="true"
      @close="detailVisible = false"
    >
      <div v-if="detailLoading" class="flex items-center justify-center py-16">
        <div class="flex flex-col items-center gap-3">
          <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-[var(--brand)]"></div>
          <div class="text-sm font-medium text-[var(--muted-foreground)]">{{ t('common.loading') }}</div>
        </div>
      </div>

      <div v-else-if="detail" class="space-y-5 py-2">
        <!-- Hero: action + result at a glance -->
        <div
          class="rounded-2xl border p-5"
          style="border-color: var(--border); background: var(--surface-2)"
        >
          <div class="flex flex-wrap items-center gap-3">
            <span :class="statusBadgeClass(detail.status_code)">
              <span class="pulse-dot" :class="statusDotClass(detail.status_code)"></span>
              {{ detail.status_code }} {{ statusText(detail.status_code) }}
            </span>
            <div class="flex flex-col">
              <span class="break-all text-base font-semibold text-[var(--foreground)]">
                {{ formatAuditActionLabel(detail.action) }}
              </span>
              <span
                v-if="shouldShowRawActionCode(detail.action)"
                class="break-all font-mono text-[11px] tabular-nums text-[var(--muted-foreground)] opacity-65"
              >
                {{ detail.action }}
              </span>
            </div>
          </div>

          <div
            class="mt-3 flex items-center gap-2 rounded-xl border px-3 py-2"
            style="border-color: var(--border); background: var(--card)"
          >
            <span class="pill pill-ghost font-mono tabular-nums text-[11px] font-bold">
              {{ detail.method }}
            </span>
            <span class="select-text break-all font-mono tabular-nums text-xs text-[var(--muted-foreground)]">{{ sanitizeAuditPath(detail.path) }}</span>
            <button type="button" class="btn btn-secondary btn-sm shrink-0 font-mono tabular-nums text-xs" :aria-label="`${t('common.copy')} ${auditRequestText(detail)}`" @click="copyToClipboard(auditRequestText(detail))">{{ t('common.copy') }}</button>
          </div>

          <div class="mt-3 flex flex-wrap items-center gap-x-5 gap-y-1.5 font-mono text-xs tabular-nums text-[var(--muted-foreground)]">
            <span class="inline-flex items-center gap-1.5">
              <Icon name="clock" size="xs" />
              {{ formatTime(detail.created_at) }}
            </span>
            <span>{{ t('admin.audit.detail.latency') }} {{ detail.latency_ms }} ms</span>
            <span v-if="detail.request_id" class="inline-flex items-center gap-1">
              {{ t('admin.audit.detail.requestId') }}
              <span class="break-all font-mono tabular-nums">{{ detail.request_id }}</span>
            </span>
          </div>
        </div>

        <!-- Actor / auth / source -->
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <div class="rounded-xl bg-[var(--surface-2)] p-4">
            <div class="text-xs font-bold uppercase tracking-wider text-[var(--muted-foreground)]">
              {{ t('admin.audit.columns.actor') }}
            </div>
            <div class="mt-1 break-all text-sm font-medium text-[var(--foreground)]">
              {{ detail.actor_email || '—' }}
            </div>
            <div class="mt-0.5 text-xs text-[var(--muted-foreground)]">{{ detail.actor_role }}</div>
          </div>

          <div class="rounded-xl bg-[var(--surface-2)] p-4">
            <div class="text-xs font-bold uppercase tracking-wider text-[var(--muted-foreground)]">
              {{ t('admin.audit.filters.authMethod') }}
            </div>
            <div class="mt-1 text-sm font-medium text-[var(--foreground)]">
              {{ authMethodLabel(detail.auth_method) || '—' }}
            </div>
            <div v-if="detail.credential_masked" class="mt-0.5 break-all font-mono tabular-nums text-xs text-[var(--muted-foreground)]">
              {{ detail.credential_masked }}
            </div>
          </div>

          <div class="rounded-xl bg-[var(--surface-2)] p-4">
            <div class="text-xs font-bold uppercase tracking-wider text-[var(--muted-foreground)]">
              {{ t('admin.audit.columns.clientIp') }}
            </div>
            <div class="mt-1 break-all font-mono tabular-nums text-sm font-medium text-[var(--foreground)]">
              {{ detail.client_ip || '—' }}
            </div>
          </div>
        </div>

        <!-- User-Agent -->
        <section>
          <h4 class="mb-1.5 text-xs font-bold uppercase tracking-wider text-[var(--muted-foreground)]">
            {{ t('admin.audit.detail.userAgent') }}
          </h4>
          <div class="break-all rounded-xl bg-[var(--surface-2)] p-3 font-mono tabular-nums text-xs leading-relaxed text-[var(--muted-foreground)]">
            {{ detail.user_agent || '—' }}
          </div>
        </section>

        <!-- Request body (redacted) -->
        <section v-if="detail.request_body">
          <h4 class="mb-1.5 text-xs font-bold uppercase tracking-wider text-[var(--muted-foreground)]">
            {{ t('admin.audit.detail.requestBody') }}
          </h4>
          <pre class="max-h-72 overflow-auto rounded-xl bg-[var(--surface-2)] p-4 font-mono tabular-nums text-xs leading-relaxed text-[var(--muted-foreground)]">{{ prettyBody(detail.request_body) }}</pre>
        </section>

        <!-- Extra -->
        <section v-if="detail.extra && Object.keys(detail.extra).length">
          <h4 class="mb-1.5 text-xs font-bold uppercase tracking-wider text-[var(--muted-foreground)]">
            {{ t('admin.audit.detail.extra') }}
          </h4>
          <pre class="max-h-48 overflow-auto rounded-xl bg-[var(--surface-2)] p-4 font-mono tabular-nums text-xs leading-relaxed text-[var(--muted-foreground)]">{{ JSON.stringify(detail.extra, null, 2) }}</pre>
        </section>
      </div>
    </BaseDialog>

    <!-- Custom time range dialog (与 /admin/ops 时间下拉一致的自定义范围，支持时分) -->
    <BaseDialog
      :show="showCustomTimeRangeDialog"
      :title="t('admin.ops.timeRange.custom')"
      width="narrow"
      @close="handleCustomTimeRangeCancel"
    >
      <div class="space-y-4 py-2">
        <div>
          <label class="input-label">{{ t('admin.ops.customTimeRange.startTime') }}</label>
          <input v-model="customStartTimeInput" type="datetime-local" class="input" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.ops.customTimeRange.endTime') }}</label>
          <input v-model="customEndTimeInput" type="datetime-local" class="input" />
        </div>
      </div>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="handleCustomTimeRangeCancel">
          {{ t('common.cancel') }}
        </button>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="!customStartTimeInput || !customEndTimeInput"
          @click="handleCustomTimeRangeConfirm"
        >
          {{ t('common.confirm') }}
        </button>
      </template>
    </BaseDialog>

    <!-- Clear confirmation → step-up TOTP -->
    <ConfirmDialog
      :show="clearConfirmVisible"
      :title="t('admin.audit.clearConfirm.title')"
      :message="t('admin.audit.clearConfirm.message')"
      :confirm-text="t('admin.audit.clearAll')"
      :cancel-text="t('common.cancel')"
      danger
      @confirm="onClearConfirmed"
      @cancel="clearConfirmVisible = false"
    />

    <!-- TOTP prompt for the clear operation -->
    <BaseDialog
      :show="clearTotpVisible"
      :title="t('admin.audit.clearConfirm.totpTitle')"
      width="narrow"
      :z-index="60"
      @close="cancelClearTotp"
    >
      <div class="py-2">
        <p class="text-sm text-[var(--muted-foreground)]">{{ t('admin.audit.clearConfirm.totpHint') }}</p>
        <input
          v-model.trim="clearTotpCode"
          type="text"
          inputmode="numeric"
          maxlength="6"
          autocomplete="one-time-code"
          class="input mt-4 text-center text-lg tracking-[0.5em]"
          placeholder="••••••"
          @keyup.enter="submitClear"
        />
      </div>
      <template #footer>
        <button type="button" class="btn btn-secondary" :disabled="clearing" @click="cancelClearTotp">
          {{ t('common.cancel') }}
        </button>
        <button
          type="button"
          class="btn btn-danger"
          :disabled="clearing || clearTotpCode.length !== 6"
          @click="submitClear"
        >
          {{ clearing ? t('common.loading') : t('admin.audit.clearAll') }}
        </button>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { auditRequestText, sanitizeAuditPath } from '@/utils/auditPath'
import { useClipboard } from '@/composables/useClipboard'
const { copyToClipboard } = useClipboard()
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI, type AuditLog } from '@/api/admin'
import { totpAPI } from '@/api'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import type { Column } from '@/components/common/types'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores'

const i18n = useI18n()
const { t } = i18n
const appStore = useAppStore()

const isZhLocale = computed(() => {
  const rawLocale = typeof i18n.locale === 'string' ? i18n.locale : i18n.locale?.value
  if (rawLocale) {
    return String(rawLocale).toLowerCase().startsWith('zh')
  }
  const probe = t('common.search')
  if (probe && probe !== 'common.search') {
    return /[\u4e00-\u9fa5]/.test(probe)
  }
  if (typeof document !== 'undefined' && document.documentElement?.lang) {
    return document.documentElement.lang.toLowerCase().startsWith('zh')
  }
  return true
})

const AUDIT_ACTION_EXACT_LABELS: Record<string, { zh: string; en: string }> = {
  'usage.dashboard.api_keys_usage.create': { zh: '查询 API 密钥用量统计', en: 'Query API Keys Usage Stats' },
  'admin.dashboard.api_keys_usage.create': { zh: '查询全局 API 密钥用量统计', en: 'Query Global API Keys Usage Stats' },
  'admin.backups.s3_config.read': { zh: '读取 S3 备份配置', en: 'Read S3 Backup Configuration' },
  'admin.backups.s3_config.update': { zh: '更新 S3 备份配置', en: 'Update S3 Backup Configuration' },
  'admin.backups.s3_config.create': { zh: '保存或测试 S3 备份配置', en: 'Save or Test S3 Backup Configuration' },
  'admin.backups.s3_config.test.create': { zh: '测试 S3 备份连接', en: 'Test S3 Backup Connection' },
  'admin.backups.create': { zh: '创建系统数据备份', en: 'Create System Backup' },
  'admin.backups.restore': { zh: '恢复系统数据备份', en: 'Restore System Backup' },
  'admin.backups.delete': { zh: '删除系统数据备份', en: 'Delete System Backup' },
  'admin.backups.download': { zh: '下载系统数据备份', en: 'Download System Backup' },
  'admin.settings.email_template_preview.create': { zh: '预览邮件模板渲染', en: 'Preview Email Template' },
  'admin.settings.test_smtp.create': { zh: '测试 SMTP 邮件服务连接', en: 'Test SMTP Connection' },
  'admin.settings.send_test_email.create': { zh: '发送测试邮件', en: 'Send Test Email' },
  'admin.admin_api_key.read': { zh: '读取管理员 API 密钥配置', en: 'Read Admin API Key Configuration' },
  'admin.admin_api_key.regenerate': { zh: '轮换管理员 API 密钥', en: 'Regenerate Admin API Key' },
  'admin.admin_api_key.create': { zh: '创建管理员 API 密钥', en: 'Create Admin API Key' },
  'admin.admin_api_key.delete': { zh: '吊销管理员 API 密钥', en: 'Revoke Admin API Key' },
  'admin.settings.read': { zh: '读取系统全局设置', en: 'Read System Settings' },
  'admin.settings.update': { zh: '更新系统全局设置', en: 'Update System Settings' },
  'admin.accounts.read': { zh: '读取上游账号列表', en: 'Read Upstream Accounts' },
  'admin.accounts.create': { zh: '创建上游账号连接', en: 'Create Upstream Account' },
  'admin.accounts.update': { zh: '更新上游账号配置', en: 'Update Upstream Account' },
  'admin.accounts.delete': { zh: '删除上游账号连接', en: 'Delete Upstream Account' },
  'admin.accounts.export': { zh: '导出上游账号数据', en: 'Export Upstream Accounts' },
  'admin.accounts.import': { zh: '导入上游账号数据', en: 'Import Upstream Accounts' },
  'workspace.accounts.export': { zh: '导出工作区账号数据', en: 'Export Workspace Accounts' },
  'workspace.accounts.import': { zh: '导入工作区账号数据', en: 'Import Workspace Accounts' },
  'admin.users.api_keys.read': { zh: '读取用户 API 密钥列表', en: 'Read User API Keys' },
  'admin.groups.api_keys.read': { zh: '读取分组 API 密钥列表', en: 'Read Group API Keys' },
  'admin.proxies.export': { zh: '导出代理节点数据', en: 'Export Proxy Nodes' },
  'admin.proxies.probe_now': { zh: '立即探测代理节点连通性', en: 'Probe Proxy Node Immediately' },
  'admin.proxy_groups.create': { zh: '创建代理出口池分组', en: 'Create Proxy Group' },
  'admin.proxy_groups.update': { zh: '更新代理出口池分组', en: 'Update Proxy Group' },
  'admin.proxy_groups.delete': { zh: '删除代理出口池分组', en: 'Delete Proxy Group' },
  'admin.proxy_groups.members.replace': { zh: '替换代理分组节点成员', en: 'Replace Proxy Group Members' },
  'admin.proxy_groups.bindings.reset': { zh: '重置代理分组绑定关系', en: 'Reset Proxy Group Bindings' },
  'admin.redeem_codes.export': { zh: '导出兑换码数据', en: 'Export Redeem Codes' },
  'admin.extensions.list': { zh: '读取系统扩展与插件清单', en: 'List System Extensions' },
  'admin.audit_logs.clear': { zh: '清空安全审计日志', en: 'Clear Audit Logs' },
  'admin.prompt_audit.config.update': { zh: '更新内容审查策略配置', en: 'Update Content Guard Configuration' },
  'admin.prompt_audit.endpoint.probe': { zh: '探测内容审查端点', en: 'Probe Content Guard Endpoint' },
  'admin.prompt_audit.event.delete': { zh: '删除内容审查事件', en: 'Delete Content Guard Event' },
  'admin.prompt_audit.events.batch_delete': { zh: '批量删除内容审查事件', en: 'Batch Delete Content Guard Events' },
  'admin.prompt_audit.events.delete_preview': { zh: '预览内容审查事件清理范围', en: 'Preview Content Guard Event Deletion' },
  'admin.prompt_audit.events.filter_delete': { zh: '按条件清理内容审查事件', en: 'Delete Content Guard Events by Filter' },
  'billing.payment_evidence.read': { zh: '读取账单调账支付凭证', en: 'Read Billing Payment Evidence' },
  'billing.payment_evidence.link': { zh: '关联账单调账支付凭证', en: 'Link Billing Payment Evidence' },
  'auth.login': { zh: '用户登录认证', en: 'User Login Authentication' },
  'auth.login_2fa': { zh: '二步验证登录 (2FA)', en: 'Two-Factor Login Verification' },
  'auth.register': { zh: '注册新用户账号', en: 'User Account Registration' },
  'auth.token_refresh': { zh: '刷新会话访问令牌', en: 'Refresh Session Token' },
  'user.totp.step_up': { zh: '敏感操作 TOTP 二次验证', en: 'Step-Up TOTP Verification' },
  'playground.run': { zh: '执行调试台模型推理', en: 'Execute Playground Model Run' },
  'tasks.create': { zh: '创建异步批处理任务', en: 'Create Async Task' },
  'tasks.retry': { zh: '重试异步批处理任务', en: 'Retry Async Task' },
  'keys.create': { zh: '创建 API 密钥', en: 'Create API Key' },
  'keys.update': { zh: '更新 API 密钥', en: 'Update API Key' },
  'keys.delete': { zh: '删除 API 密钥', en: 'Delete API Key' }
}

const AUDIT_VERB_LABELS: Record<string, { zh: string; en: string }> = {
  read: { zh: '读取', en: 'Read' },
  get: { zh: '读取', en: 'Get' },
  list: { zh: '查询', en: 'List' },
  query: { zh: '查询', en: 'Query' },
  create: { zh: '创建', en: 'Create' },
  update: { zh: '更新', en: 'Update' },
  delete: { zh: '删除', en: 'Delete' },
  batch_delete: { zh: '批量删除', en: 'Batch Delete' },
  filter_delete: { zh: '按条件清理', en: 'Delete by Filter' },
  delete_preview: { zh: '预览清理', en: 'Preview Deletion' },
  clear: { zh: '清空', en: 'Clear' },
  export: { zh: '导出', en: 'Export' },
  import: { zh: '导入', en: 'Import' },
  restore: { zh: '恢复', en: 'Restore' },
  download: { zh: '下载', en: 'Download' },
  regenerate: { zh: '轮换', en: 'Regenerate' },
  rotate: { zh: '轮换', en: 'Rotate' },
  probe: { zh: '探测', en: 'Probe' },
  probe_now: { zh: '立即探测', en: 'Probe' },
  retry: { zh: '重试', en: 'Retry' },
  cancel: { zh: '取消', en: 'Cancel' },
  replace: { zh: '替换', en: 'Replace' },
  reset: { zh: '重置', en: 'Reset' },
  run: { zh: '执行', en: 'Run' },
  link: { zh: '关联', en: 'Link' },
  sync: { zh: '同步', en: 'Sync' },
  refresh: { zh: '刷新', en: 'Refresh' },
  verify: { zh: '核验', en: 'Verify' },
  test: { zh: '测试', en: 'Test' },
  login: { zh: '登录', en: 'Login' },
  register: { zh: '注册', en: 'Register' }
}

const AUDIT_DOMAIN_LABELS: Record<string, { zh: string; en: string }> = {
  admin: { zh: '管理台', en: 'Admin' },
  workspace: { zh: '工作区', en: 'Workspace' },
  workspaces: { zh: '工作区', en: 'Workspaces' },
  project: { zh: '项目', en: 'Project' },
  projects: { zh: '项目', en: 'Projects' },
  usage: { zh: '用量', en: 'Usage' },
  dashboard: { zh: '仪表盘', en: 'Dashboard' },
  api_keys_usage: { zh: 'API 密钥用量统计', en: 'API Keys Usage Stats' },
  api_keys: { zh: 'API 密钥', en: 'API Keys' },
  api_key: { zh: 'API 密钥', en: 'API Key' },
  keys: { zh: 'API 密钥', en: 'API Keys' },
  admin_api_key: { zh: '管理员 API 密钥配置', en: 'Admin API Key' },
  backups: { zh: '数据备份', en: 'Backups' },
  backup: { zh: '数据备份', en: 'Backup' },
  s3_config: { zh: 'S3 备份配置', en: 'S3 Backup Configuration' },
  settings: { zh: '系统设置', en: 'Settings' },
  setting: { zh: '系统设置', en: 'Setting' },
  email_template_preview: { zh: '邮件模板预览', en: 'Email Template Preview' },
  email_template: { zh: '邮件模板', en: 'Email Template' },
  email_templates: { zh: '邮件模板', en: 'Email Templates' },
  accounts: { zh: '上游账号', en: 'Upstream Accounts' },
  account: { zh: '上游账号', en: 'Upstream Account' },
  connections: { zh: '上游连接', en: 'Connections' },
  connection: { zh: '上游连接', en: 'Connection' },
  users: { zh: '用户', en: 'Users' },
  user: { zh: '用户', en: 'User' },
  groups: { zh: '路由分组', en: 'Routing Groups' },
  group: { zh: '路由分组', en: 'Routing Group' },
  proxies: { zh: '代理节点', en: 'Proxy Nodes' },
  proxy: { zh: '代理节点', en: 'Proxy Node' },
  proxy_groups: { zh: '代理出口池', en: 'Proxy Groups' },
  redeem_codes: { zh: '兑换码', en: 'Redeem Codes' },
  redeem: { zh: '兑换码', en: 'Redeem Code' },
  subscriptions: { zh: '订阅套餐', en: 'Subscriptions' },
  subscription: { zh: '订阅套餐', en: 'Subscription' },
  announcements: { zh: '系统公告', en: 'Announcements' },
  announcement: { zh: '系统公告', en: 'Announcement' },
  ops: { zh: '运维监控', en: 'Operations' },
  alert_rules: { zh: '告警规则', en: 'Alert Rules' },
  rules: { zh: '规则', en: 'Rules' },
  alert_events: { zh: '告警事件', en: 'Alert Events' },
  alerts: { zh: '告警', en: 'Alerts' },
  audit_logs: { zh: '审计日志', en: 'Audit Logs' },
  audit: { zh: '审计日志', en: 'Audit' },
  prompt_audit: { zh: '内容安全审查', en: 'Content Guard' },
  billing: { zh: '账单计费', en: 'Billing' },
  payment_evidence: { zh: '调账支付凭证', en: 'Payment Evidence' },
  adjustments: { zh: '账单调账', en: 'Billing Adjustments' },
  orders: { zh: '支付订单', en: 'Orders' },
  payment: { zh: '支付', en: 'Payment' },
  byok: { zh: '自管凭据 (BYOK)', en: 'BYOK' },
  credentials: { zh: '凭据', en: 'Credentials' },
  credential: { zh: '凭据', en: 'Credential' },
  rotations: { zh: '密钥轮换', en: 'Key Rotations' },
  authorization: { zh: '授权配置', en: 'Authorization' },
  totp: { zh: 'TOTP 二步验证', en: 'TOTP 2FA' },
  passkeys: { zh: '通行密钥 (Passkey)', en: 'Passkeys' },
  passkey: { zh: '通行密钥 (Passkey)', en: 'Passkey' },
  auth: { zh: '身份认证', en: 'Authentication' },
  plugins: { zh: '插件扩展', en: 'Plugins' },
  extensions: { zh: '系统扩展', en: 'Extensions' },
  models: { zh: '模型目录', en: 'Models' },
  model_catalog: { zh: '模型目录', en: 'Model Catalog' },
  model_pricing: { zh: '模型定价', en: 'Model Pricing' },
  tasks: { zh: '异步任务', en: 'Async Tasks' },
  playground: { zh: '调试台', en: 'Playground' },
  requests: { zh: '请求记录', en: 'Requests' },
  members: { zh: '成员权限', en: 'Members' },
  bindings: { zh: '绑定关系', en: 'Bindings' },
  config: { zh: '配置', en: 'Configuration' },
  endpoint: { zh: '端点', en: 'Endpoint' },
  endpoints: { zh: '端点', en: 'Endpoints' },
  event: { zh: '事件', en: 'Event' },
  events: { zh: '事件', en: 'Events' },
  data: { zh: '数据', en: 'Data' },
  session: { zh: '会话', en: 'Session' },
  ollama_cloud_usage: { zh: 'Ollama Cloud 用量', en: 'Ollama Cloud Usage' },
  codex_session: { zh: 'Codex 会话', en: 'Codex Session' }
}

const loading = ref(false)
const logs = ref<AuditLog[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)

const filters = reactive({
  q: '',
  actor_email: '',
  action: '',
  client_ip: '',
  method: '',
  auth_method: '',
  success: ''
})

// 时间范围：预设窗口（同 /admin/ops 时间下拉）+ 自定义起止（datetime-local，支持时分）
const timeRange = ref('')
const customStartTime = ref('')
const customEndTime = ref('')
const showCustomTimeRangeDialog = ref(false)
const customStartTimeInput = ref('')
const customEndTimeInput = ref('')

const TIME_RANGE_MINUTES: Record<string, number> = {
  '30m': 30,
  '1h': 60,
  '6h': 6 * 60,
  '24h': 24 * 60,
  '7d': 7 * 24 * 60,
  '30d': 30 * 24 * 60
}

const timeRangeOptions = computed(() => [
  { value: '', label: t('admin.audit.filters.all') },
  { value: '30m', label: t('admin.ops.timeRange.30m') },
  { value: '1h', label: t('admin.ops.timeRange.1h') },
  { value: '6h', label: t('admin.ops.timeRange.6h') },
  { value: '24h', label: t('admin.ops.timeRange.24h') },
  { value: '7d', label: t('admin.ops.timeRange.7d') },
  { value: '30d', label: t('admin.ops.timeRange.30d') },
  {
    value: 'custom',
    label:
      timeRange.value === 'custom' && customStartTime.value && customEndTime.value
        ? `${t('admin.ops.timeRange.custom')} (${formatCustomTimeRangeLabel(customStartTime.value, customEndTime.value)})`
        : t('admin.ops.timeRange.custom')
  }
])

function formatCustomTimeRangeLabel(startTime: string, endTime: string): string {
  const fmt = (raw: string) => {
    const d = new Date(raw)
    if (Number.isNaN(d.getTime())) return raw
    const pad = (n: number) => String(n).padStart(2, '0')
    return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
  }
  return `${fmt(startTime)} ~ ${fmt(endTime)}`
}

function toDatetimeLocal(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function handleTimeRangeChange(val: string | number | boolean | null) {
  const value = String(val ?? '')
  if (value === 'custom') {
    // 预填：已有自定义值沿用，否则默认最近1小时（本地时区）
    const now = new Date()
    customStartTimeInput.value = customStartTime.value || toDatetimeLocal(new Date(now.getTime() - 60 * 60 * 1000))
    customEndTimeInput.value = customEndTime.value || toDatetimeLocal(now)
    showCustomTimeRangeDialog.value = true
    return
  }
  timeRange.value = value
  search()
}

function handleCustomTimeRangeConfirm() {
  if (!customStartTimeInput.value || !customEndTimeInput.value) return
  customStartTime.value = customStartTimeInput.value
  customEndTime.value = customEndTimeInput.value
  timeRange.value = 'custom'
  showCustomTimeRangeDialog.value = false
  search()
}

function handleCustomTimeRangeCancel() {
  // 未确认不改变当前时间范围；Select 是受控组件，展示值保持不变。
  showCustomTimeRangeDialog.value = false
}

const columns = computed<Column[]>(() => [
  { key: 'created_at', label: t('admin.audit.columns.time') },
  { key: 'actor', label: t('admin.audit.columns.actor') },
  { key: 'action', label: t('admin.audit.columns.action') },
  { key: 'status_code', label: t('admin.audit.columns.result') },
  { key: 'latency_ms', label: t('admin.audit.detail.latency') },
  { key: 'client_ip', label: t('admin.audit.columns.clientIp') },
  { key: 'actions', label: t('common.actions') }
])

const methodOptions = computed(() => [
  { value: '', label: t('admin.audit.filters.all') },
  { value: 'POST', label: 'POST' },
  { value: 'PUT', label: 'PUT' },
  { value: 'PATCH', label: 'PATCH' },
  { value: 'DELETE', label: 'DELETE' },
  { value: 'GET', label: 'GET' }
])

const authMethodOptions = computed(() => [
  { value: '', label: t('admin.audit.filters.all') },
  { value: 'jwt', label: 'JWT' },
  { value: 'admin_api_key', label: 'Admin API Key' }
])

const resultOptions = computed(() => [
  { value: '', label: t('admin.audit.filters.all') },
  { value: 'true', label: t('admin.audit.filters.resultSuccess') },
  { value: 'false', label: t('admin.audit.filters.resultFailure') }
])

function authMethodLabel(method: string): string {
  const found = authMethodOptions.value.find((o) => o.value === method)
  return found && found.value ? found.label : method
}

function toRFC3339(local: string): string | undefined {
  if (!local) return undefined
  const d = new Date(local)
  if (Number.isNaN(d.getTime())) return undefined
  return d.toISOString()
}

function buildTimeRangeQuery(): { start_time?: string; end_time?: string } {
  if (timeRange.value === 'custom') {
    return {
      start_time: toRFC3339(customStartTime.value),
      end_time: toRFC3339(customEndTime.value)
    }
  }
  const minutes = TIME_RANGE_MINUTES[timeRange.value]
  if (!minutes) return {}
  return { start_time: new Date(Date.now() - minutes * 60 * 1000).toISOString() }
}

function buildQuery() {
  return {
    page: page.value,
    page_size: pageSize.value,
    q: filters.q || undefined,
    actor_email: filters.actor_email || undefined,
    action: filters.action || undefined,
    client_ip: filters.client_ip || undefined,
    method: filters.method || undefined,
    auth_method: filters.auth_method || undefined,
    success: filters.success || undefined,
    ...buildTimeRangeQuery()
  }
}

async function fetchLogs() {
  loading.value = true
  try {
    const res = await adminAPI.audit.list(buildQuery())
    logs.value = res.items
    total.value = res.total
  } catch (err: any) {
    appStore.showError(err?.message || t('admin.audit.loadFailed'))
  } finally {
    loading.value = false
  }
}

function search() {
  page.value = 1
  fetchLogs()
}

function resetFilters() {
  filters.q = ''
  filters.actor_email = ''
  filters.action = ''
  filters.client_ip = ''
  filters.method = ''
  filters.auth_method = ''
  filters.success = ''
  timeRange.value = ''
  customStartTime.value = ''
  customEndTime.value = ''
  search()
}

function onPageChange(p: number) {
  page.value = p
  fetchLogs()
}

function onPageSizeChange(ps: number) {
  pageSize.value = ps
  page.value = 1
  fetchLogs()
}

// Detail dialog
const detailVisible = ref(false)
const detailLoading = ref(false)
const detail = ref<AuditLog | null>(null)

async function openDetail(id: number) {
  detailVisible.value = true
  detailLoading.value = true
  detail.value = null
  try {
    detail.value = await adminAPI.audit.get(id)
  } catch (err: any) {
    appStore.showError(err?.message || t('admin.audit.loadFailed'))
    detailVisible.value = false
  } finally {
    detailLoading.value = false
  }
}

function prettyBody(body: string): string {
  try {
    return JSON.stringify(JSON.parse(body), null, 2)
  } catch {
    return body
  }
}

// Clear-all flow: confirm → TOTP → clear
const clearConfirmVisible = ref(false)
const clearTotpVisible = ref(false)
const clearTotpCode = ref('')
const clearing = ref(false)
const checkingTotpStatus = ref(false)

// 与其他敏感操作一致：未启用 2FA 时直接提示去个人资料启用 TOTP，
// 而不是弹出一个无法完成的验证码输入框（后端会以 TOTP_NOT_SETUP 拒绝）。
async function openClearDialog() {
  if (checkingTotpStatus.value) return
  checkingTotpStatus.value = true
  try {
    const status = await totpAPI.getStatus()
    if (!status.enabled) {
      appStore.showError(t('stepUp.notEnabled'))
      return
    }
  } catch (err: any) {
    appStore.showError(err?.message || t('admin.audit.loadFailed'))
    return
  } finally {
    checkingTotpStatus.value = false
  }
  clearConfirmVisible.value = true
}

function onClearConfirmed() {
  clearConfirmVisible.value = false
  clearTotpCode.value = ''
  clearTotpVisible.value = true
}

function cancelClearTotp() {
  if (clearing.value) return
  clearTotpVisible.value = false
}

async function submitClear() {
  if (clearTotpCode.value.length !== 6) return
  clearing.value = true
  try {
    const res = await adminAPI.audit.clear(clearTotpCode.value)
    clearTotpVisible.value = false
    appStore.showSuccess(t('admin.audit.clearConfirm.success', { count: res.deleted }))
    search()
  } catch (err: any) {
    appStore.showError(err?.message || t('admin.audit.clearConfirm.failed'))
    clearTotpCode.value = ''
  } finally {
    clearing.value = false
  }
}

// Helpers
function formatTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

function statusText(status: number): string {
  return status < 400 ? t('admin.audit.filters.resultSuccess') : t('admin.audit.filters.resultFailure')
}

function statusBadgeClass(status: number): string {
  const base = 'inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 font-mono text-xs font-semibold tabular-nums '
  if (status >= 500) return base + 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
  if (status >= 400) return base + 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
  return base + 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300'
}

function statusDotClass(status: number): string {
  if (status >= 500) return 'pulse-bad'
  if (status >= 400) return 'pulse-warn'
  return 'pulse-ok'
}

function humanizeTokenEn(token: string): string {
  return token
    .replace(/[_-]+/g, ' ')
    .replace(/\b\w/g, (ch) => ch.toUpperCase())
}

function formatAuditActionLabel(action: string): string {
  const raw = String(action || '').trim()
  if (!raw) return '—'
  const exact = AUDIT_ACTION_EXACT_LABELS[raw]
  if (exact) {
    return isZhLocale.value ? exact.zh : exact.en
  }
  if (!raw.includes('.')) {
    return raw
  }
  const segments = raw.split('.').filter(Boolean)
  if (segments.length < 2) {
    return raw
  }
  const lastSegment = segments[segments.length - 1]!
  const verbEntry = AUDIT_VERB_LABELS[lastSegment]
  const domainSegments = verbEntry ? segments.slice(0, -1) : segments
  const filteredDomains = domainSegments.filter((seg, idx) => {
    if (seg === 'v1' || seg === 'v2') return false
    if (idx === 0 && seg === 'admin' && domainSegments.length > 1) return false
    if (seg === 'dashboard' && domainSegments.some((s) => s.includes('usage'))) return false
    if (seg === 'backups' && domainSegments.includes('s3_config')) return false
    return true
  })
  const effectiveDomains = filteredDomains.length > 0 ? filteredDomains : domainSegments

  let verbZh = verbEntry?.zh ?? ''
  let verbEn = verbEntry?.en ?? ''
  if (lastSegment === 'create') {
    if (effectiveDomains.some((s) => s.includes('preview'))) {
      verbZh = '预览'
      verbEn = 'Preview'
    } else if (effectiveDomains.some((s) => s.includes('usage') || s.includes('stats') || s.includes('query'))) {
      verbZh = '查询'
      verbEn = 'Query'
    } else if (effectiveDomains.some((s) => s.includes('test') || s.includes('probe'))) {
      verbZh = '测试'
      verbEn = 'Test'
    }
  }

  if (isZhLocale.value) {
    const domainZh = effectiveDomains
      .map((seg) => AUDIT_DOMAIN_LABELS[seg]?.zh ?? humanizeTokenEn(seg))
      .join('')
    return verbZh ? `${verbZh}${domainZh}` : domainZh
  }

  const domainEn = effectiveDomains
    .map((seg) => AUDIT_DOMAIN_LABELS[seg]?.en ?? humanizeTokenEn(seg))
    .join(' ')
  return verbEn ? `${verbEn} ${domainEn}` : domainEn
}

function shouldShowRawActionCode(action: string): boolean {
  const raw = String(action || '').trim()
  return Boolean(raw && formatAuditActionLabel(raw) !== raw)
}

onMounted(fetchLogs)
</script>
