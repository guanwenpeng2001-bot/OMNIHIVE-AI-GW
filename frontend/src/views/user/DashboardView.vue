<template>
  <AppLayout>
    <section class="oh-dashboard w-full min-w-0 space-y-5">
      <ConsoleScopeBar
        v-if="scope.scopes.value.length > 0"
        :workspaces="scope.scopes.value"
        :projects="scope.availableProjects.value"
        :workspace-id="scope.activeWorkspaceId.value"
        :project-id="scope.activeProjectId.value"
        :loading="scope.loading.value"
        :refreshing="scope.refreshing.value || refreshingOverview"
        :last-synced-at="scope.lastSyncedAt.value"
        active-surface="dashboard"
        @select-scope="handleScopeSelect"
        @refresh="refreshAll(true)"
        @navigate="navigateTo"
      />
      <header class="flex flex-wrap items-start justify-between gap-4">
        <div class="min-w-0 space-y-1.5">
          <h1 class="text-2xl font-bold tracking-tight sm:text-3xl">{{ activeTabLabel }}</h1>
          <p class="text-sm text-[var(--oh-ink-secondary,var(--muted-foreground))]">
            {{ isZh ? '你的 OmniHive 用量、花费、成功率与近期请求概览' : 'Your OmniHive usage, spend, observed success rate, and recent requests' }}
          </p>
          <ReportTimeContext :time-zone="reportTimeZone" />
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <span class="oh-hive-badge font-mono text-xs tabular-nums">
            <span class="pulse-dot pulse-amber" aria-hidden="true" />
            {{ isZh ? '网关就绪' : 'Gateway Ready' }}
          </span>
          <button type="button" class="btn btn-secondary btn-sm font-mono" @click="triggerCommandPalette">
            {{ isZh ? '⌘K 命令' : '⌘K Command' }}
          </button>
          <button type="button" class="btn btn-secondary btn-sm" @click="navigateTo('/playground')">Playground</button>
          <button type="button" class="btn btn-secondary btn-sm" @click="navigateTo('/tasks')">{{ isZh ? '任务中心' : 'Tasks' }}</button>
          <button type="button" class="btn btn-secondary btn-sm" aria-controls="getStarted" :aria-expanded="!onboardingHidden" @click="toggleOnboardingVisibility">
            {{ onboardingHidden ? (isZh ? '恢复引导' : 'Show Guide') : (isZh ? '隐藏引导' : 'Hide Guide') }}
          </button>
        </div>
      </header>

      <ConsoleAsyncBanner
        v-if="staleOverviewNotice"
        :notice="staleOverviewNotice"
        :retry-label="isZh ? '重试同步' : 'Retry Sync'"
        @retry="refreshAll(true)"
      />

      <section data-testid="overview-reliability-strip" class="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <div class="rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] p-3.5">
          <p class="font-mono text-[11px] uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
            {{ isZh ? '近期成功率' : 'Observed Success Rate' }}
          </p>
          <p data-testid="overview-success-rate" class="mt-1 font-mono text-xl font-bold tabular-nums text-[var(--oh-ink,var(--foreground))]">
            {{ observedSuccessRateLabel }}
          </p>
          <p class="mt-0.5 font-mono text-[11px] tabular-nums text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
            {{ scopedSampleCount > 0 ? `n=${scopedSampleCount}` : (isZh ? '无近期终态样本' : 'No terminal sample') }}
          </p>
        </div>
        <div class="rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] p-3.5">
          <p class="font-mono text-[11px] uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
            {{ isZh ? '今日实际花费' : 'Today Actual Spend' }}
          </p>
          <p class="mt-1 font-mono text-xl font-bold tabular-nums text-[var(--oh-accent-text,var(--brand-text,#d97706))]">
            {{ stats ? money(stats.today_actual_cost) : '—' }}
          </p>
          <p class="mt-0.5 font-mono text-[11px] tabular-nums text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
            {{ isZh ? '累计：' : 'Total: ' }}{{ stats && statsState === 'known' ? money(stats.total_actual_cost) : '—' }}
          </p>
        </div>
        <div class="rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] p-3.5">
          <p class="font-mono text-[11px] uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
            {{ isZh ? '窗口请求数' : 'Window Requests' }}
          </p>
          <p class="mt-1 font-mono text-xl font-bold tabular-nums text-[var(--oh-ink,var(--foreground))]">
            {{ chartError || historyGap ? '—' : trendRequestCount.toLocaleString() }}
          </p>
          <p class="mt-0.5 font-mono text-[11px] tabular-nums text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
            {{ startDate }} → {{ endDate }}
          </p>
        </div>
        <div class="rounded-[var(--oh-r-md,8px)] border border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] p-3.5">
          <p class="font-mono text-[11px] uppercase tracking-wider text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
            {{ isZh ? '失败 / 未确认终态' : 'Failed / Unknown' }}
          </p>
          <p class="mt-1 font-mono text-xl font-bold tabular-nums text-[var(--oh-ink,var(--foreground))]">
            {{ scopedSampleCount > 0 ? scopedFailureOrUnknownCount : '—' }}
          </p>
          <p class="mt-0.5 font-mono text-[11px] tabular-nums text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
            {{ isZh ? '按实际回执统计' : 'Verified receipt outcomes' }}
          </p>
        </div>
      </section>

      <div class="activity-tabs" role="group" :aria-label="isZh ? '工作台视图' : 'Console View Tabs'">
        <button v-for="tab in tabItems" :key="tab.id" type="button" :class="{ on: activeTab === tab.id }" :aria-pressed="activeTab === tab.id" @click="activeTab = tab.id">
          {{ tab.label }}
          <span v-if="tab.id === 'logs'" class="tab-count">{{ loadingUsage || recentState !== 'known' ? '—' : recentUsage.length }}</span>
          <span v-else-if="tab.id === 'audit'" class="tab-count">{{ auditLoading || auditError ? '—' : auditPreview.length }}</span>
          <span v-else-if="activeTab === tab.id" class="tab-dot" aria-hidden="true" />
        </button>
      </div>
      <div class="grid w-full items-start gap-5 lg:grid-cols-[minmax(0,1fr)_340px] xl:grid-cols-[minmax(0,1fr)_360px]">
        <div class="min-w-0 space-y-5">
          <section class="client-connect card overflow-hidden">
            <button type="button" class="w-full p-4 text-left" aria-controls="client-config-panel" :aria-expanded="connectExpanded" @click="connectExpanded = !connectExpanded">
              <span class="flex items-center justify-between gap-3"><b>{{ isZh ? '连接一个客户端' : 'Connect a Client' }}</b><span class="font-mono text-lg text-[var(--muted-foreground)]" aria-hidden="true">{{ connectExpanded ? '−' : '+' }}</span></span>
              <span class="mt-1 block text-sm text-[var(--muted-foreground)]">{{ isZh ? 'OpenCode、Claude Code、Codex CLI、Cursor 与 OpenAI SDK' : 'OpenCode, Claude Code, Codex CLI, Cursor & OpenAI SDK' }}</span>
            </button>
            <div v-if="connectExpanded" id="client-config-panel" class="border-t border-[var(--border)]">
              <div class="client-tabs flex overflow-x-auto px-4" role="group" :aria-label="isZh ? '客户端配置' : 'Client configurations'">
                <button v-for="client in clientConfigs" :key="client.id" type="button" :aria-pressed="activeClient === client.id" :class="{ on: activeClient === client.id }" @click="activeClient = client.id; copyStatus = ''">{{ client.name }}</button>
              </div>
              <div class="flex flex-wrap items-center justify-between gap-2 border-y border-[var(--border)] bg-[var(--surface-2)] px-4 py-2">
                <span class="font-mono text-xs text-[var(--muted-foreground)]">{{ selectedClient.filename }}</span>
                <select v-if="activeClient === 'opencode'" v-model="opencodeVersion" class="rounded border border-[var(--border)] bg-[var(--card)] px-2 py-1 text-xs text-[var(--foreground)]" :aria-label="isZh ? 'OpenCode 版本' : 'OpenCode version'" @change="copyStatus = ''">
                  <option value="2">OpenCode 2</option>
                  <option value="1">OpenCode 1</option>
                </select>
                <button type="button" class="btn btn-secondary btn-sm" @click="copyClientConfig">{{ copyStatus === 'success' ? (isZh ? '已复制 ✓' : 'Copied ✓') : (isZh ? '复制配置' : 'Copy Config') }}</button>
              </div>
              <pre class="max-h-80 overflow-auto p-4 font-mono text-xs leading-6" tabindex="0" :aria-label="isZh ? '客户端配置代码' : 'Client configuration snippet'"><code>{{ selectedClient.code }}</code></pre>
              <div class="space-y-2 border-t border-[var(--border)] px-4 py-3 text-xs text-[var(--muted-foreground)]">
                <p class="break-all">{{ isZh ? '网关地址：' : 'Gateway URL: ' }}<code>{{ gatewayBase }}</code></p>
                <p>{{ selectedClient.hint }}</p>
                <p v-if="copyStatus" role="status">{{ copyStatus === 'success' ? (isZh ? '配置已复制到剪贴板。' : 'Configuration copied to clipboard.') : (isZh ? '复制失败，请选中代码后手动复制。' : 'Copy failed. Select the snippet and copy manually.') }}</p>
                <button type="button" class="font-medium text-[var(--brand-text)] underline underline-offset-4" @click="navigateTo('/keys')">{{ isZh ? '创建或管理 API 密钥' : 'Create or Manage API Keys' }}</button>
              </div>
            </div>
          </section>
          <section v-if="activeTab === 'audit'" class="card space-y-4 p-5" :aria-label="isZh ? '个人安全与操作审计时间线' : 'Personal security and operation audit timeline'">
            <div class="flex flex-wrap items-center justify-between gap-2">
              <h2 class="font-semibold">{{ isZh ? '个人安全与操作审计' : 'Personal Security & Operation Audit' }}</h2>
              <button type="button" class="btn btn-secondary btn-sm" @click="openPersonalAuditDrawer">{{ isZh ? '查看详情 →' : 'View Details →' }}</button>
            </div>
            <p class="text-sm opacity-60">{{ isZh ? '记录 API 密钥、MCP 授权与账户安全事件' : 'API key, MCP grant & account security events' }}</p>
            <ol class="space-y-4 border-l pl-4" style="border-color: var(--border)">
              <li v-for="event in auditPreview" :key="event.id"><b class="text-sm">{{ event.title }}</b><p class="mt-1 text-sm opacity-60">{{ event.description }}</p><time v-if="event.createdAt" :datetime="event.createdAt" class="mt-1 block font-mono text-xs tabular-nums opacity-50">{{ formatDateTimeWithZone(event.createdAt, reportTimeZone) }}</time></li>
            </ol>
            <div v-if="!auditPreview.length" class="audit-empty" role="status" :aria-busy="auditLoading">
              <div aria-hidden="true" class="audit-shape">
                <div v-for="row in 4" :key="row" class="audit-shape-row"><i /><span :style="{ width: `${72 - row * 9}%` }" /><b /></div>
              </div>
              <span class="empty-status">{{ auditLoading ? (isZh ? '正在读取审计记录' : 'Loading audit log') : auditError ? (isZh ? '审计记录暂不可用' : 'Audit log unavailable') : (isZh ? '暂无安全与操作记录' : 'No security or operation records yet') }}</span>
              <button v-if="auditError" type="button" class="btn btn-secondary btn-sm ml-2" @click="loadPersonalSecurity">{{ isZh ? '重试' : 'Retry' }}</button>
            </div>
          </section>
          <div v-if="activeTab === 'logs' || activeTab === 'usage'" class="card flex flex-wrap items-center justify-between gap-3 p-4">
            <div>
              <h2 class="font-semibold">{{ activeTab === 'logs' ? (isZh ? '近期调用日志预览' : 'Recent Request Log Preview') : (isZh ? '用量与成本概览' : 'Usage & Spend Summary') }}</h2>
              <p class="mt-1 text-sm opacity-60">{{ isZh ? '查看下方实时数据，或进入完整用量页筛选与导出。' : 'Inspect live telemetry below or open full usage logs.' }}</p>
            </div>
            <button type="button" class="btn btn-secondary btn-sm" @click="navigateTo('/usage')">{{ isZh ? '查看完整明细 →' : 'Full Usage Logs →' }}</button>
          </div>
          <div v-if="loading" class="flex items-center justify-center py-12"><LoadingSpinner /></div>
          <template v-else>
            <UsageStatsState v-if="statsState !== 'known'" :state="statsState" />
            <div
              v-if="stats && statsState === 'known' && (stats.total_requests || 0) === 0 && recentUsage.length === 0"
              data-testid="dashboard-empty-overview"
              class="flex flex-wrap items-center justify-between gap-3 rounded-[var(--oh-r-md,8px)] border border-dashed border-[var(--oh-border,var(--border))] bg-[var(--oh-surface,var(--card))] p-4"
            >
              <div>
                <p class="text-sm font-semibold text-[var(--oh-ink,var(--foreground))]">
                  {{ isZh ? '尚未产生任何网关调用记录' : 'No Gateway Requests Recorded Yet' }}
                </p>
                <p class="mt-0.5 text-xs text-[var(--oh-ink-secondary,var(--muted-foreground))]">
                  {{ isZh ? '创建 API 密钥或在 Playground 发送首条请求后，此处将实时展示真实花费、成功率与调用轨迹。' : 'Create an API key or run your first prompt in Playground to populate live spend, success rate, and request traces.' }}
                </p>
              </div>
              <div class="flex flex-wrap items-center gap-2">
                <button type="button" class="btn btn-secondary btn-sm" @click="navigateTo('/keys')">{{ isZh ? '创建密钥' : 'Create API Key' }}</button>
                <button type="button" class="btn btn-primary btn-sm" @click="navigateTo('/playground')">Playground →</button>
              </div>
            </div>
            <UserDashboardStats v-if="stats" :time-zone="reportTimeZone" :stats="stats" :balance="user?.balance || 0" :is-simple="authStore.isSimpleMode" :platform-quotas="platformQuotas" />
            <div v-if="historyGap || chartError" data-testid="chart-state" class="card space-y-3 p-4">
              <DateRangePicker v-model:start-date="startDate" v-model:end-date="endDate" @change="loadCharts" />
              <Select v-model="granularity" :options="[{ value: 'day', label: t('dashboard.day') }, { value: 'hour', label: t('dashboard.hour') }]" @change="loadCharts" />
              <UsageStatsState :state="historyGap ? 'unknown' : 'error'" />
              <button class="btn btn-secondary" @click="loadCharts">{{ t('common.refresh') }}</button>
            </div>
            <UserDashboardCharts v-show="!historyGap && !chartError" :time-zone="reportTimeZone" v-model:startDate="startDate" v-model:endDate="endDate" v-model:granularity="granularity" :loading="loadingCharts" :trend="trendData" :models="modelStats" @dateRangeChange="loadCharts" @granularityChange="loadCharts" @refresh="refreshAll" />
            <UsageStatsState :state="recentState"><UserDashboardRecentUsage :time-zone="reportTimeZone" :data="recentUsage" :loading="loadingUsage" /></UsageStatsState>
            <UserDashboardQuickActions />
          </template>
          <div class="grid gap-4 md:grid-cols-3">
            <button type="button" class="card interactive p-4 text-left" @click="navigateTo('/usage')">
              <span class="flex justify-between"><b>{{ isZh ? '我的用量' : 'My Usage' }}</b><span class="opacity-40">›</span></span>
              <span class="mt-1 block text-sm opacity-60">{{ isZh ? '你的请求、成本与额度' : 'Your requests, spend & quota' }}</span><span class="mt-4 block font-mono text-xl font-bold tabular-nums">{{ stats ? money(stats.total_actual_cost) : '—' }}</span>
            </button>
            <button type="button" class="card interactive p-4 text-left" @click="navigateTo('/purchase')">
              <span class="flex justify-between"><b>{{ isZh ? '账单' : 'Billing' }}</b><span class="opacity-40">›</span></span>
              <span class="mt-1 block text-sm opacity-60">{{ isZh ? `按量付费 · 余额 ${money(user?.balance)}` : `Pay-as-you-go · Balance ${money(user?.balance)}` }}</span><span class="mt-4 block font-mono text-xl font-bold tabular-nums">{{ money(user?.balance) }}</span>
            </button>
            <button type="button" class="card interactive p-4 text-left" @click="navigateTo('/monitor')">
              <span class="flex justify-between"><b>{{ isZh ? '渠道状态' : 'Channel Status' }}</b><span class="opacity-40">›</span></span>
              <span class="mt-1 block text-sm opacity-60">{{ isZh ? '查看渠道健康与窗口' : 'Inspect channel health & quota windows' }}</span><span class="mt-4 block text-xl font-bold">{{ isZh ? '查看状态 →' : 'View Status →' }}</span>
            </button>
          </div>
        </div>
        <aside class="min-w-0 space-y-5" :aria-label="isZh ? '工作台信息' : 'Workspace Sidebar'">
          <section v-show="!onboardingHidden" id="getStarted" class="card p-4" :aria-label="isZh ? '开始使用' : 'Get Started'">
            <div class="flex items-center justify-between"><h2 class="font-semibold">{{ isZh ? '开始使用' : 'Get Started' }}</h2><button type="button" class="btn btn-ghost btn-sm" @click="toggleOnboardingVisibility">{{ isZh ? '隐藏' : 'Hide' }}</button></div>
            <p class="mt-1 text-sm opacity-60">{{ isZh ? `${completedStepCount} / 3 已完成` : `${completedStepCount} / 3 Completed` }}</p>
            <div class="meter mt-2" role="progressbar" :aria-label="isZh ? '上手进度' : 'Onboarding progress'" :aria-valuenow="completedStepCount" :aria-valuemin="0" :aria-valuemax="3"><span :style="{ width: onboardingProgressPct + '%' }" /></div>
            <div class="oh-onboarding-hero mt-3 rounded-xl p-4">
              <div class="mb-2 font-mono text-xs opacity-50">ONBOARDING</div>
              <h3 class="font-semibold">{{ currentStep.title }}</h3><p class="mt-2 text-sm leading-relaxed opacity-70">{{ currentStep.description }}</p>
              <div class="mt-4 flex flex-wrap gap-2">
                <button type="button" class="btn btn-primary btn-sm" @click="openStepGuide">{{ isZh ? '› 查看指南' : '› View Guide' }}</button>
                <button type="button" class="btn btn-secondary btn-sm" @click="toggleStepManualDone(activeStepIndex)">{{ isStepCompleted(activeStepIndex) ? (isZh ? '撤销完成' : 'Undo Complete') : (isZh ? '标记完成' : 'Mark Complete') }}</button>
              </div>
            </div>
            <div class="mt-3 flex items-center justify-between">
              <button type="button" class="btn btn-ghost btn-sm" :aria-label="isZh ? '上一步' : 'Previous step'" @click="stepOnboarding(-1)">←</button>
              <div class="flex items-center gap-1">
                <button v-for="(step, index) in onboardingSteps" :key="step.id" type="button" class="oh-step-dot-button" :aria-label="step.title" :aria-pressed="activeStepIndex === index" @click="activeStepIndex = index"><span class="oh-step-dot" :class="{ active: activeStepIndex === index }" /></button>
              </div>
              <button type="button" class="btn btn-ghost btn-sm" :aria-label="isZh ? '下一步' : 'Next step'" @click="stepOnboarding(1)">→</button>
            </div>
          </section>
          <button type="button" class="card interactive w-full p-4 text-left" @click="navigateTo('/usage')">
            <span class="flex justify-between"><b>{{ isZh ? '日志' : 'Logs' }}</b><span class="opacity-40">›</span></span>
            <span class="mt-1 block text-sm opacity-60">{{ chartError || historyGap ? (isZh ? '当前范围趋势暂不可用' : 'Trend unavailable for current range') : (isZh ? `当前范围 ${trendRequestCount.toLocaleString()} 条调用` : `${trendRequestCount.toLocaleString()} requests in current range`) }}</span>
            <span id="dashboardMiniTrend" class="mt-3 flex h-12 items-end gap-[3px]" aria-hidden="true"><span v-for="(bar, index) in miniTrend" :key="index" class="oh-trend-bar flex-1 rounded-t-sm" :style="{ height: bar + '%' }" /></span>
          </button>
          <button type="button" class="card interactive w-full p-4 text-left" @click="openPersonalAuditDrawer">
            <span class="flex justify-between"><b>{{ isZh ? '审计' : 'Audit' }}</b><span class="opacity-40">›</span></span>
            <span class="mt-1 block text-sm opacity-60">{{ isZh ? '个人安全与操作审计 · 记录 API 密钥、MCP 授权与账户安全事件' : 'Personal Security & Operation Audit · API key, MCP grant & account security events' }}</span>
            <span v-for="event in auditPreview" :key="event.id" class="mt-3 block text-sm opacity-70">· {{ event.title }}</span>
            <span v-if="!auditPreview.length" class="mt-3 block">
              <span class="audit-shape block" aria-hidden="true"><span v-for="row in 4" :key="row" class="audit-shape-row"><i /><span :style="{ width: `${72 - row * 9}%` }" /><b /></span></span>
              <span class="empty-status">{{ auditLoading ? (isZh ? '正在读取审计记录' : 'Loading audit log') : auditError ? (isZh ? '审计记录暂不可用' : 'Audit log unavailable') : (isZh ? '暂无安全与操作记录' : 'No security or operation records yet') }}</span>
            </span>
          </button>
          <section class="card space-y-3 p-4">
            <h2 class="font-semibold">{{ isZh ? '按平台拆分' : 'By Platform' }}</h2><p class="text-sm opacity-60">{{ isZh ? '累计请求、Token 与实际花费' : 'Cumulative requests, tokens & actual spend' }}</p>
            <UsageStatsState v-if="statsState !== 'known'" :state="statsState" />
            <p v-else-if="!platformSummary.length" class="text-sm opacity-60">{{ loading ? (isZh ? '正在加载…' : 'Loading…') : (isZh ? '暂无平台用量' : 'No platform usage yet') }}</p>
            <div v-for="platform in platformSummary" :key="platform.platform" class="space-y-1.5">
              <div class="flex flex-wrap justify-between gap-2 text-sm"><b>{{ platform.platform }}</b><span>{{ money(platform.total_actual_cost) }}</span></div>
              <p class="text-xs opacity-60">{{ isZh ? `${platform.total_requests.toLocaleString()} 次 · ${platform.total_tokens.toLocaleString()} Token` : `${platform.total_requests.toLocaleString()} reqs · ${platform.total_tokens.toLocaleString()} tokens` }}</p>
              <div class="meter"><span :style="{ width: Math.max(0, platform.total_actual_cost / platformMaxCost * 100) + '%' }" /></div>
            </div>
          </section>
        </aside>
      </div>
    </section>
    <Drawer
      v-model:open="auditDrawerOpen"
      :title="isZh ? '个人安全与操作审计' : 'Personal Security & Operation Audit'"
      :description="isZh ? '记录 API 密钥、MCP 授权与账户安全事件' : 'API key, MCP grant & account security timeline'"
    >
      <div class="space-y-4">
        <ol v-if="auditPreview.length" class="space-y-3 divide-y divide-[var(--oh-border,var(--border))]">
          <li v-for="event in auditPreview" :key="event.id" class="pt-3 first:pt-0">
            <h3 class="text-sm font-semibold text-[var(--oh-ink,var(--foreground))]">{{ event.title }}</h3>
            <p class="mt-1 text-xs text-[var(--oh-ink-secondary,var(--muted-foreground))]">{{ event.description }}</p>
            <p v-if="event.createdAt" class="mt-1 font-mono text-[11px] tabular-nums text-[var(--oh-ink-tertiary,var(--muted-foreground))]">
              {{ formatDateTimeWithZone(event.createdAt, reportTimeZone) }}
            </p>
          </li>
        </ol>
        <p v-else role="status" class="py-6 text-center text-xs text-[var(--oh-ink-secondary,var(--muted-foreground))]">
          {{ auditLoading ? (isZh ? '正在读取审计记录…' : 'Loading audit log…') : auditError ? (isZh ? '审计记录暂不可用。' : 'Audit log unavailable.') : (isZh ? '暂无安全与操作记录' : 'No security events recorded yet') }}
        </p>
      </div>
      <template #footer>
        <div class="flex flex-wrap justify-end gap-2">
          <button type="button" class="btn btn-secondary btn-sm" @click="auditDrawerOpen = false; navigateTo('/settings/mcp')">
            {{ isZh ? 'MCP 授权管理' : 'MCP Grants' }}
          </button>
          <button type="button" class="btn btn-primary btn-sm" @click="auditDrawerOpen = false; navigateTo('/profile')">
            {{ isZh ? '账户安全设置' : 'Account Security' }}
          </button>
        </div>
      </template>
    </Drawer>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, getCurrentInstance } from 'vue'
import { useI18n } from 'vue-i18n'
import { resolveReportTimeZone, formatReportDateInput, formatDateTimeWithZone } from '@/utils/format'
import type { OnboardingDTO, UpdateOnboardingPayload, UserAuditEventDTO } from '@/api/mcp'
import ReportTimeContext from '@/components/charts/ReportTimeContext.vue'
import Select from '@/components/common/Select.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import UsageStatsState from '@/components/common/UsageStatsState.vue'
import Drawer from '@/components/ui/Drawer.vue'
import { historyGapFromError, withTrendGaps, type HistoryGap } from '@/views/trendGaps'
import { useAuthStore } from '@/stores/auth'
import { usageAPI, type UserDashboardStats as UserStatsType } from '@/api/usage'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import UserDashboardStats from '@/components/user/dashboard/UserDashboardStats.vue'
import UserDashboardCharts from '@/components/user/dashboard/UserDashboardCharts.vue'
import UserDashboardRecentUsage from '@/components/user/dashboard/UserDashboardRecentUsage.vue'
import UserDashboardQuickActions from '@/components/user/dashboard/UserDashboardQuickActions.vue'
import type { UsageLog, TrendDataPoint, ModelStat, PlatformQuotaItem } from '@/types'
import { getMyPlatformQuotas } from '@/api/user'
import { buildGatewayUrl } from '@/api/url'
import { buildClientBaseUrl } from '@/utils/url'
import { classifyConsoleError, type ConsoleAsyncNotice } from '@/features/console/asyncState'
import ConsoleAsyncBanner from '@/features/console/components/ConsoleAsyncBanner.vue'
import ConsoleScopeBar from '@/features/console/components/ConsoleScopeBar.vue'
import { EM_DASH, formatNullablePercent } from '@/features/console/format'
import { useConsoleScope } from '@/features/console/scope'

const reportProps = defineProps<{ timeZone?: string }>()
const reportTimeZone = resolveReportTimeZone(reportProps.timeZone)
const reportQueryContext = { timezone: reportTimeZone }
const { t, locale } = useI18n()
const isZh = computed(() => String(locale?.value || '').startsWith('zh'))
const instance = getCurrentInstance()
const scope = useConsoleScope()
const tabItems = computed<Array<{ id: 'activity' | 'usage' | 'logs' | 'audit'; label: string }>>(() => [
  { id: 'activity', label: isZh.value ? '活动概览' : 'Overview' },
  { id: 'usage', label: isZh.value ? '我的用量' : 'Usage' },
  { id: 'logs', label: isZh.value ? '调用日志' : 'Recent Logs' },
  { id: 'audit', label: isZh.value ? '安全审计' : 'Security Audit' }
])
const activeTab = ref<'activity' | 'usage' | 'logs' | 'audit'>('activity')
const activeTabLabel = computed(
  () => tabItems.value.find((item) => item.id === activeTab.value)?.label ?? 'Overview'
)
const connectExpanded = ref(false)
const auditDrawerOpen = ref(false)
const activeClient = ref('opencode')
const opencodeVersion = ref('2')
const copyStatus = ref('')
let copyRevision = 0
let copyTimer: ReturnType<typeof setTimeout> | undefined
const gatewayBase = computed(() => {
  const candidate = onboarding.value?.base_url || buildGatewayUrl('/')
  try {
    const url = new URL(candidate, window.location.origin)
    return buildClientBaseUrl(/^https?:$/.test(url.protocol) ? url.href : buildGatewayUrl('/'))
  } catch {
    return buildClientBaseUrl(buildGatewayUrl('/'))
  }
})
const clientConfigs = computed(() => {
  const root = gatewayBase.value
  const base = buildClientBaseUrl(root, 'v1')
  const hint = isZh.value
    ? '将 YOUR_API_KEY 替换为密钥，将 YOUR_MODEL_ID 替换为可用渠道中的模型 ID。'
    : 'Replace YOUR_API_KEY with your API key and YOUR_MODEL_ID with an available model ID.'
  const models = { YOUR_MODEL_ID: { name: 'YOUR_MODEL_ID' } }
  const opencode = opencodeVersion.value === '2'
    ? { providers: { omnihive: { name: 'OmniHive', env: ['OPENAI_API_KEY'], package: '@opencode/ai/providers/openai-compatible', settings: { baseURL: base }, models } } }
    : { provider: { omnihive: { npm: '@ai-sdk/openai-compatible', name: 'OmniHive', options: { baseURL: base, apiKey: '{env:OPENAI_API_KEY}' }, models } } }
  return [
    { id: 'opencode', name: 'OpenCode', filename: 'opencode.json', hint: isZh.value ? `${hint} 按已安装的 OpenCode 版本选择格式，运行前设置 OPENAI_API_KEY 环境变量。` : `${hint} Pick the format matching your installed OpenCode version and set OPENAI_API_KEY before running.`, code: JSON.stringify({ $schema: 'https://opencode.ai/config.json', ...opencode, model: 'omnihive/YOUR_MODEL_ID' }, null, 2) },
    { id: 'claude', name: 'Claude Code', filename: '~/.claude/settings.json', hint, code: JSON.stringify({ env: { ANTHROPIC_BASE_URL: root, ANTHROPIC_AUTH_TOKEN: 'YOUR_API_KEY', ANTHROPIC_MODEL: 'YOUR_MODEL_ID' } }, null, 2) },
    { id: 'codex', name: 'Codex CLI', filename: '~/.codex/config.toml', hint: isZh.value ? `${hint} 运行前设置 OPENAI_API_KEY 环境变量。` : `${hint} Set the OPENAI_API_KEY environment variable before launching Codex CLI.`, code: `model = "YOUR_MODEL_ID"\nmodel_provider = "omnihive"\n\n[model_providers.omnihive]\nname = "OmniHive"\nbase_url = ${JSON.stringify(base)}\nenv_key = "OPENAI_API_KEY"\nwire_api = "responses"` },
    { id: 'cursor', name: 'Cursor', filename: 'Settings → Models → API Keys', hint: isZh.value ? '在 Cursor 的模型设置中启用 OpenAI API Key，并填写 Override OpenAI Base URL；按模型支持情况选择模型。' : 'Enable OpenAI API Key in Cursor Model Settings, set Override OpenAI Base URL, and select a supported model.', code: `OpenAI API Key: YOUR_API_KEY\nOverride OpenAI Base URL: ${base}\nModel: YOUR_MODEL_ID` },
    { id: 'python', name: 'Python SDK', filename: 'client.py', hint: isZh.value ? `${hint} 安装 openai，并设置 OPENAI_API_KEY 环境变量。` : `${hint} Install the openai package and export OPENAI_API_KEY.`, code: `import os\nfrom openai import OpenAI\n\nclient = OpenAI(\n    base_url=${JSON.stringify(base)},\n    api_key=os.environ["OPENAI_API_KEY"],\n)\nresponse = client.chat.completions.create(\n    model="YOUR_MODEL_ID",\n    messages=[{"role": "user", "content": "Hello"}],\n)\nprint(response.choices[0].message.content)` },
    { id: 'typescript', name: 'TypeScript SDK', filename: 'client.ts', hint: isZh.value ? `${hint} 安装 openai，在服务端设置 OPENAI_API_KEY，避免把密钥放入浏览器。` : `${hint} Install openai and keep OPENAI_API_KEY on the server side.`, code: `import OpenAI from "openai";\n\nconst client = new OpenAI({\n  baseURL: ${JSON.stringify(base)},\n  apiKey: process.env.OPENAI_API_KEY,\n});\nconst response = await client.chat.completions.create({\n  model: "YOUR_MODEL_ID",\n  messages: [{ role: "user", content: "Hello" }],\n});\nconsole.log(response.choices[0]?.message.content);` }
  ]
})
const selectedClient = computed(() => clientConfigs.value.find(client => client.id === activeClient.value) ?? clientConfigs.value[0]!)
const copyClientConfig = async () => {
  const revision = ++copyRevision
  const clientId = activeClient.value
  const code = selectedClient.value.code
  try {
    const { useClipboard } = await import('@/composables/useClipboard')
    const success = await useClipboard().copyToClipboard(code)
    if (revision !== copyRevision || clientId !== activeClient.value || code !== selectedClient.value.code) return
    copyStatus.value = success ? 'success' : 'error'
  } catch {
    if (revision !== copyRevision || clientId !== activeClient.value || code !== selectedClient.value.code) return
    copyStatus.value = 'error'
  }
  clearTimeout(copyTimer)
  copyTimer = setTimeout(() => { copyStatus.value = '' }, 2500)
}
const onboardingSteps = computed(() => [
  {
    id: 'connect',
    title: isZh.value ? '1. 连接你的 AI 客户端' : '1. Connect Your AI Client',
    description: isZh.value ? '复制本站网关地址，选择客户端配置，开始接入蜂巢。' : 'Copy the gateway URL and select a client snippet to connect to OmniHive.',
    route: ''
  },
  {
    id: 'keys',
    title: isZh.value ? '2. 创建 API 密钥' : '2. Create an API Key',
    description: isZh.value ? '创建专属 API 密钥并绑定渠道分组，将密钥填入客户端配置。' : 'Create a dedicated API key bound to a routing group and paste it into your client.',
    route: '/keys'
  },
  {
    id: 'usage',
    title: isZh.value ? '3. 查看实时调用明细' : '3. Inspect Live Request Telemetry',
    description: isZh.value ? '发起首次请求，核对 Token、请求耗时与实际账单。' : 'Send your first request and verify tokens, latency, and actual billing.',
    route: '/usage'
  }
])
const activeStepIndex = ref(0)
const onboardingHidden = ref(false)
const manualDoneSteps = ref<Array<boolean | null>>([null, null, null])
const onboarding = ref<OnboardingDTO | null>(null)
let onboardingRevision = 0
let onboardingSave = Promise.resolve()
const currentStep = computed(() => onboardingSteps.value[activeStepIndex.value] ?? onboardingSteps.value[0]!)
const isStepCompleted = (index: number): boolean => {
  const manual = manualDoneSteps.value[index]
  if (typeof manual === 'boolean') return manual
  if (onboarding.value?.steps[index]) return true
  if (index === 0) return (stats.value?.total_requests || 0) > 0
  if (index === 1) return (stats.value?.total_api_keys || 0) > 0
  return index === 2 && ((stats.value?.total_requests || 0) > 0 || recentUsage.value.length > 0)
}
const completedStepCount = computed(() => onboardingSteps.value.filter((_, index) => isStepCompleted(index)).length)
const onboardingProgressPct = computed(() => completedStepCount.value / 3 * 100)
const stepOnboarding = (delta: number) => { activeStepIndex.value = (activeStepIndex.value + delta + 3) % 3 }
const persistOnboarding = (payload: UpdateOnboardingPayload) => {
  onboardingRevision++
  const userId = authStore.user?.id
  if (!userId) return
  onboardingSave = onboardingSave.then(async () => {
    if (auditController.signal.aborted || authStore.user?.id !== userId) return
    const { mcpAPI } = await import('@/api/mcp')
    if (auditController.signal.aborted || authStore.user?.id !== userId) return
    await mcpAPI.updateOnboarding(payload)
  }).catch(() => undefined)
}
const toggleStepManualDone = (index: number) => {
  const completed = !isStepCompleted(index)
  manualDoneSteps.value[index] = completed
  persistOnboarding({ step_index: index, completed })
}
const toggleOnboardingVisibility = () => {
  onboardingHidden.value = !onboardingHidden.value
  persistOnboarding({ hidden: onboardingHidden.value })
  try {
    const encoded = onboardingHidden.value ? '1' : '0'
    localStorage.setItem('omnihive.dashboard.onboarding_hidden.v1', encoded)
    localStorage.setItem('omnihive.dashboard.onboardingHidden', encoded)
  } catch { return }
}
const navigateTo = (path: string) => {
  const router = instance?.appContext.config.globalProperties.$router
  if (router && typeof router.push === 'function') void router.push(path)
  else if (typeof window !== 'undefined') window.location.hash = path
}
const triggerCommandPalette = () => {
  if (typeof window === 'undefined') return
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true, ctrlKey: true, bubbles: true }))
}
const openConnectClientDrawer = () => { connectExpanded.value = true }
const openStepGuide = () => { if (activeStepIndex.value === 0) openConnectClientDrawer(); else navigateTo(currentStep.value.route) }
const auditPreview = ref<Array<{ id: string; title: string; description: string; createdAt?: string }>>([])
const auditLoading = ref(false)
const auditError = ref(false)
const auditController = new AbortController()
const loadPersonalSecurity = async () => {
  const userId = authStore.user?.id
  if (!userId) return
  auditLoading.value = true
  auditError.value = false
  try {
    const { apiClient } = await import('@/api/client')
    const { data } = await apiClient.get<UserAuditEventDTO[]>('/user/audit-logs', { signal: auditController.signal })
    if (auditController.signal.aborted || authStore.user?.id !== userId) return
    if (!Array.isArray(data)) throw new Error('Invalid audit response')
    const titles: Record<string, string> = {
      'mcp.grant.created': isZh.value ? '授权 MCP 客户端' : 'Authorized MCP Client',
      'mcp.grant.revoked': isZh.value ? '撤销 MCP 授权' : 'Revoked MCP Grant',
      'mcp.grant.denied': isZh.value ? '拒绝 MCP 授权' : 'Denied MCP Grant',
      'onboarding.step.completed': isZh.value ? '完成接入引导' : 'Completed Onboarding Step'
    }
    auditPreview.value = data.slice(0, 5).map(event => ({
      id: event.id,
      title: titles[event.action] ?? event.action,
      description: event.detail,
      createdAt: event.created_at
    }))
  } catch {
    if (!auditController.signal.aborted) auditError.value = true
  } finally {
    if (!auditController.signal.aborted) auditLoading.value = false
  }
}
const loadOnboarding = async () => {
  const userId = authStore.user?.id
  const revision = onboardingRevision
  if (!userId) return
  try {
    const { mcpAPI } = await import('@/api/mcp')
    if (auditController.signal.aborted || authStore.user?.id !== userId) return
    const state = await mcpAPI.getOnboarding()
    if (auditController.signal.aborted || authStore.user?.id !== userId) return
    onboarding.value = state
    if (revision === onboardingRevision) onboardingHidden.value = state.hidden
  } catch {
    return
  }
}
const openPersonalAuditDrawer = () => { auditDrawerOpen.value = true }
const money = (value?: number) => typeof value === 'number' && Number.isFinite(value) ? `$${value.toFixed(4)}` : '—'
const historyGap = ref<HistoryGap | null>(null)
const statsState = ref<'known' | 'unknown' | 'error'>('known')
const chartError = ref(false)
const recentState = ref<'known' | 'unknown' | 'error'>('known')
const refreshingOverview = ref(false)
const staleOverviewNotice = ref<ConsoleAsyncNotice | null>(null)
const scopedSampleCount = ref(0)
const scopedSucceededCount = ref(0)
const scopedFailureOrUnknownCount = ref(0)
const observedSuccessRateLabel = computed(() => {
  if (scopedSampleCount.value <= 0) return EM_DASH
  return formatNullablePercent((scopedSucceededCount.value / scopedSampleCount.value) * 100, 1)
})
let statsRequestSequence = 0
let recentRequestSequence = 0
const authStore = useAuthStore(); const user = computed(() => authStore.user)
const stats = ref<UserStatsType | null>(null); const loading = ref(false); const loadingUsage = ref(false); const loadingCharts = ref(false)
const trendData = ref<TrendDataPoint[]>([]); const modelStats = ref<ModelStat[]>([]); const recentUsage = ref<UsageLog[]>([])
const platformQuotas = ref<PlatformQuotaItem[] | null>(null)
const platformSummary = computed(() => stats.value?.by_platform ?? [])
const platformMaxCost = computed(() => Math.max(0.000001, ...platformSummary.value.map(platform => platform.total_actual_cost)))
const trendRequestCount = computed(() => trendData.value.reduce((total, point) => total + (point.requests || 0), 0))
const miniTrend = computed(() => {
  const values = trendData.value.slice(-14).map(point => Math.max(0, Number(point.total_tokens ?? point.requests) || 0))
  const padded = [...Array<number>(14 - values.length).fill(0), ...values]
  const max = Math.max(1, ...padded)
  return padded.map(value => value ? value / max * 100 : 0)
})
const startDate = ref(formatReportDateInput(new Date(Date.now() - 6 * 86400000), reportTimeZone)); const endDate = ref(formatReportDateInput(new Date(), reportTimeZone)); const granularity = ref('day')
const loadStats = async (isBackgroundRefresh = false) => {
  const sequence = ++statsRequestSequence
  const previousStats = stats.value
  if (isBackgroundRefresh && previousStats) {
    refreshingOverview.value = true
  } else {
    loading.value = true
    stats.value = null
  }
  statsState.value = 'known'
  staleOverviewNotice.value = null
  try {
    await authStore.refreshUser()
    const response = await usageAPI.getDashboardStats()
    if (sequence !== statsRequestSequence) return
    stats.value = response
  } catch (error) {
    if (sequence !== statsRequestSequence) return
    if (isBackgroundRefresh && previousStats) {
      stats.value = previousStats
      staleOverviewNotice.value = classifyConsoleError(error, { hasCachedData: true, isZh: isZh.value })
    } else {
      statsState.value = historyGapFromError(error) ? 'unknown' : 'error'
    }
  } finally {
    if (sequence === statsRequestSequence) {
      loading.value = false
      refreshingOverview.value = false
    }
  }
}
let chartRequestSequence = 0
const loadCharts = async () => {
  const sequence = ++chartRequestSequence
  loadingCharts.value = true
  historyGap.value = null
  chartError.value = false
  try {
    const [trend, models] = await Promise.all([
      usageAPI.getDashboardTrend({ ...reportQueryContext, start_date: startDate.value, end_date: endDate.value, granularity: granularity.value as 'day' | 'hour' }),
      usageAPI.getDashboardModels({ ...reportQueryContext, start_date: startDate.value, end_date: endDate.value }),
    ])
    if (sequence !== chartRequestSequence) return
    historyGap.value = null
    trendData.value = withTrendGaps(trend.trend || [], granularity.value)
    modelStats.value = models.models || []
  } catch (error) {
    if (sequence !== chartRequestSequence) return
    historyGap.value = historyGapFromError(error)
    chartError.value = !historyGap.value
    trendData.value = []
    modelStats.value = []
    if (!historyGap.value) {
      console.error('Failed to load charts:', error)
    }
  } finally {
    if (sequence === chartRequestSequence) loadingCharts.value = false
  }
}
const loadRecent = async () => {
  const sequence = ++recentRequestSequence
  loadingUsage.value = true
  recentUsage.value = []
  recentState.value = 'known'
  try {
    const response = await usageAPI.query({ ...reportQueryContext, start_date: startDate.value, end_date: endDate.value, page: 1, page_size: 100 })
    if (sequence !== recentRequestSequence) return
    recentUsage.value = response.items.slice(0, 5)
  } catch (error) {
    if (sequence !== recentRequestSequence) return
    recentState.value = historyGapFromError(error) ? 'unknown' : 'error'
  } finally {
    if (sequence === recentRequestSequence) loadingUsage.value = false
  }
}
const loadPlatformQuotas = async () => { try { const data = await getMyPlatformQuotas(); platformQuotas.value = data.platform_quotas ?? [] } catch (error) { console.warn('Failed to load platform quotas:', error); platformQuotas.value = [] } }
const loadScopedReliabilitySample = async () => {
  const userId = authStore.user?.id
  if (!userId) return
  try {
    await scope.refreshScopes(false)
    const wsId = scope.activeWorkspaceId.value
    const projId = scope.activeProjectId.value
    if (!wsId || !projId) return
    const { apiClient } = await import('@/api/client')
    const { data } = await apiClient.get<{ items?: Array<{ outcome?: string }> }>(
      buildGatewayUrl(`/api/v2/workspaces/${wsId}/projects/${projId}/requests`),
      { baseURL: '', params: { limit: 50 }, signal: auditController.signal }
    )
    const rows = Array.isArray(data?.items) ? data.items : []
    const terminal = rows.filter((r) => r.outcome && r.outcome !== 'admitted' && r.outcome !== 'running')
    scopedSampleCount.value = terminal.length
    scopedSucceededCount.value = terminal.filter((r) => r.outcome === 'succeeded').length
    scopedFailureOrUnknownCount.value = terminal.filter((r) => r.outcome !== 'succeeded').length
  } catch {
    scopedSampleCount.value = 0
    scopedSucceededCount.value = 0
    scopedFailureOrUnknownCount.value = 0
  }
}
const handleScopeSelect = (payload: { workspaceId: string; projectId: string | null }) => {
  scope.selectWorkspaceProject(payload.workspaceId, payload.projectId)
  void loadScopedReliabilitySample()
}
const refreshAll = (isBackgroundRefresh = false) => {
  void loadStats(isBackgroundRefresh)
  void loadCharts()
  void loadRecent()
  void loadPlatformQuotas()
  void loadScopedReliabilitySample()
}
onMounted(() => {
  try {
    const v1Hidden = localStorage.getItem('omnihive.dashboard.onboarding_hidden.v1')
    const legacyHidden = localStorage.getItem('omnihive.dashboard.onboardingHidden')
    onboardingHidden.value = (v1Hidden ?? legacyHidden) === '1'
  } catch { onboardingHidden.value = false }
  refreshAll()
  void loadPersonalSecurity()
  void loadOnboarding()
})
onUnmounted(() => { auditController.abort(); statsRequestSequence++; chartRequestSequence++; recentRequestSequence++; copyRevision++; clearTimeout(copyTimer) })
</script>

<style scoped>
.oh-dashboard { color: var(--foreground); }
.oh-hive-badge { display: inline-flex; align-items: center; gap: 7px; padding: 4px 10px; border-radius: 6px; border: 1px solid var(--oh-border, var(--border)); background: var(--oh-surface, var(--card)); color: var(--oh-ink, var(--foreground)); }
.pulse-dot { display: inline-block; width: 6px; height: 6px; flex: none; border-radius: 50%; }
.pulse-amber { background: var(--oh-accent, #f59e0b); }
.activity-tabs { display: flex; gap: 24px; overflow-x: auto; border-bottom: 1px solid var(--border); }
.activity-tabs button, .client-tabs button { display: inline-flex; align-items: center; flex: none; gap: 8px; padding: 12px 0; border-bottom: 2px solid transparent; font-size: 13px; color: var(--muted-foreground); }
.activity-tabs button.on, .client-tabs button.on { color: var(--foreground); border-bottom-color: var(--brand); font-weight: 600; }
.tab-count { padding: 1px 6px; border-radius: 4px; background: var(--surface-2); font-family: var(--font-mono, monospace); font-size: 11px; font-variant-numeric: tabular-nums; }
.tab-dot { width: 5px; height: 5px; border-radius: 50%; background: var(--brand); }
.client-tabs { gap: 20px; }
.client-connect { border: 1px solid var(--border); background: var(--card); }
.audit-empty { padding: 12px 0; color: var(--muted-foreground); }
.audit-shape { max-width: 420px; margin-bottom: 16px; }
.audit-shape-row { display: flex; align-items: center; gap: 16px; height: 32px; border-left: 1px dashed var(--border); margin-left: 8px; }
.audit-shape-row i { width: 16px; height: 16px; margin-left: -8px; border-radius: 50%; border: 1px solid var(--border); background: var(--card); }
.audit-shape-row span { height: 4px; background: var(--border); border-radius: 2px; }
.audit-shape-row b { width: 34px; height: 12px; border: 1px solid var(--border); border-radius: 999px; }
.empty-status { display: inline-flex; border: 1px solid var(--border); border-radius: 999px; padding: 4px 10px; font-size: 12px; }
.interactive { transition: border-color .18s; }
.interactive:hover { border-color: var(--brand, #f59e0b); }
.meter { height: 6px; overflow: hidden; border-radius: 999px; background: var(--border, #e4e4e7); }
.meter > span { display: block; height: 100%; border-radius: inherit; background: var(--brand, #f59e0b); transition: width .25s; }
.oh-onboarding-hero { background: var(--background); color: var(--foreground); border: 1px solid var(--border); }
.oh-step-dot-button { display: grid; min-height: 28px; min-width: 24px; place-items: center; }
.oh-step-dot { display: block; height: 6px; width: 6px; border-radius: 999px; background: var(--border); transition: width .2s; }
.oh-step-dot.active { width: 20px; background: var(--brand, #f59e0b); }
.oh-trend-bar { background: color-mix(in srgb, var(--brand, #f59e0b) 55%, var(--card)); transition: height .25s; }
button:focus-visible, select:focus-visible, pre:focus-visible { outline: 2px solid var(--brand, #f59e0b); outline-offset: 3px; }
@media (prefers-reduced-motion: reduce) { .pulse-ok { animation: none; } .interactive, .meter > span, .oh-step-dot, .oh-trend-bar { transition: none; } }
</style>
