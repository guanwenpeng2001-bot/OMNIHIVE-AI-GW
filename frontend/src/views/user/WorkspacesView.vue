<template>
  <AppLayout>
    <div data-testid="workspaces-view" class="min-w-0 space-y-5">
      <header class="card p-5 sm:p-6">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <div class="flex flex-wrap items-center gap-2.5">
              <h1 class="text-2xl font-bold tracking-tight text-[var(--foreground)]">
                {{ t('workspaces.title') }}
              </h1>
              <span class="chip text-xs">
                <span class="inline-block h-1.5 w-1.5 rounded-full bg-[var(--oh-accent,#f59e0b)]" aria-hidden="true" />
                <span class="font-mono tabular-nums">{{ workspaces.length }}</span>
              </span>
              <span
                v-if="selected?.is_default"
                data-testid="default-workspace-badge"
                class="rounded border border-[var(--border)] bg-[var(--surface-2)] px-2 py-0.5 font-mono text-[11px] font-medium text-[var(--foreground)]"
              >
                {{ t('workspaces.default') }}
              </span>
              <span
                v-if="selected"
                data-testid="workspace-role-badge"
                class="pill pill-ghost font-mono text-[11px] uppercase"
              >
                {{ selected.role === 'admin' ? t('workspaces.admin') : t('workspaces.member') }}
              </span>
              <span
                v-if="selected && workspaceRoleSourceLabel"
                data-testid="workspace-role-source-badge"
                class="rounded border border-[var(--oh-accent-border,rgba(245,158,11,0.4))] bg-[var(--surface-2)] px-2 py-0.5 font-mono text-[11px] font-medium text-[var(--foreground)]"
              >
                {{ workspaceRoleSourceLabel }}
              </span>
              <span
                v-if="selected && String(selected.id) === activeScope.workspaceId"
                data-testid="active-workspace-badge"
                class="rounded border border-[var(--oh-accent-border,rgba(245,158,11,0.4))] bg-[var(--oh-accent-subtle,rgba(245,158,11,0.12))] px-2 py-0.5 font-mono text-[11px] font-semibold text-[var(--oh-ink,var(--foreground))]"
              >
                {{ t('workspaces.activeScopeBadge') }}
                <template v-if="activeScope.projectId">
                  · {{ activeProject?.name ? `${activeProject.name} (#${activeScope.projectId})` : `#${activeScope.projectId}` }}
                </template>
              </span>
            </div>
            <p class="mt-1.5 text-sm text-[var(--muted-foreground)]">
              {{ t('workspaces.description') }}
            </p>
          </div>
          <div v-if="selected" class="flex flex-wrap items-center gap-2">
            <button
              v-if="hasAuthenticatedUser"
              type="button"
              data-testid="open-workspace-access-request-btn"
              class="btn btn-secondary btn-sm font-mono text-xs"
              @click="showAccessRequestModal = true"
            >
              {{ t('workspaces.requestAccessCta') }}
            </button>
            <button
              type="button"
              data-testid="activate-workspace-scope-btn"
              class="btn btn-secondary btn-sm font-mono text-xs"
              @click="activateScope(String(selected.id), preferredProject?.id ?? null)"
            >
              {{ t('workspaces.activateScope') }}
            </button>
          </div>
        </div>
      </header>

      <section
        v-if="selected"
        data-testid="workspace-active-scope-banner"
        class="card flex flex-wrap items-center justify-between gap-3 border border-[var(--oh-accent-border,rgba(245,158,11,0.35))] bg-[var(--oh-accent-subtle,rgba(245,158,11,0.08))] p-4"
      >
        <div class="min-w-0 space-y-1">
          <div class="flex flex-wrap items-center gap-2">
            <span class="font-mono text-[11px] uppercase tracking-wider text-[var(--muted-foreground)]">
              {{ t('workspaces.activeScopeBannerTitle') }}
            </span>
            <span
              data-testid="workspace-banner-role-badge"
              class="rounded border border-[var(--border)] bg-[var(--surface-1)] px-1.5 py-0.5 font-mono text-[10px] font-semibold uppercase text-[var(--foreground)]"
            >
              {{ selected.role === 'admin' ? t('workspaces.admin') : t('workspaces.member') }}
            </span>
          </div>
          <p class="text-sm font-medium text-[var(--foreground)]">
            <template v-if="isSelectedWorkspaceActive">
              {{ selected.name }} (#{{ selected.id }})
              <template v-if="activeProject">
                / {{ activeProject.name }} (#{{ activeProject.id }})
              </template>
              <template v-else-if="activeScope.projectId">
                / #{{ activeScope.projectId }}
              </template>
            </template>
            <template v-else>
              {{ t('workspaces.activeScopeNone') }}
            </template>
          </p>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <button
            v-if="preferredProject && (!isSelectedWorkspaceActive || activeScope.projectId !== preferredProject.id)"
            type="button"
            data-testid="activate-default-project-btn"
            class="btn btn-primary btn-sm font-mono text-xs"
            @click="activateScope(String(selected.id), preferredProject.id)"
          >
            {{ t('workspaces.activateDefaultProject') }}
            ({{ preferredProject.name }})
          </button>
          <RouterLink
            v-if="showQuickActionKeys"
            :to="`/workspaces/${selected.id}/keys`"
            data-testid="quick-action-keys"
            class="btn btn-secondary btn-sm text-xs"
          >
            {{ t('nav.apiKeys') }}
          </RouterLink>
          <RouterLink
            v-if="showQuickActionPlayground"
            to="/playground"
            data-testid="quick-action-playground"
            class="btn btn-secondary btn-sm text-xs"
          >
            {{ isZh ? '调试台' : 'Playground' }}
          </RouterLink>
          <RouterLink
            v-if="showQuickActionUsage"
            to="/usage"
            data-testid="quick-action-usage"
            class="btn btn-secondary btn-sm text-xs"
          >
            {{ t('nav.usage') }}
          </RouterLink>
          <RouterLink
            v-if="showQuickActionTasks"
            to="/tasks"
            data-testid="quick-action-tasks"
            class="btn btn-secondary btn-sm text-xs"
          >
            {{ isZh ? '任务中心' : 'Tasks' }}
          </RouterLink>
        </div>
      </section>

      <ConsoleAsyncBanner
        v-if="scopeConfirmedNotice"
        :notice="scopeConfirmedNotice"
        dismissible
        @dismiss="scopeConfirmedNotice = null"
      />

      <form v-if="auth.isAdmin" class="card flex flex-wrap items-end gap-3 p-5" @submit.prevent="createWorkspace">
        <label class="flex-1 space-y-2 text-sm font-medium text-[var(--foreground)]">
          <span class="flex items-center gap-2">
            <span class="inline-block h-1.5 w-1.5 rounded-full bg-[var(--oh-accent,#f59e0b)]" aria-hidden="true" />
            <span>{{ t('workspaces.name') }}</span>
          </span>
          <input v-model="workspaceName" required maxlength="100" class="input w-full" />
        </label>
        <button class="btn btn-primary" :disabled="busy || !workspaceName.trim()">{{ t('workspaces.create') }}</button>
      </form>

      <div class="card p-5">
        <label class="block space-y-2 text-sm font-medium text-[var(--foreground)]">
          <span class="flex items-center justify-between gap-2">
            <span class="flex items-center gap-2">
              <span class="inline-block h-1.5 w-1.5 rounded-full bg-[var(--oh-accent,#f59e0b)]" aria-hidden="true" />
              <span>{{ t('workspaces.select') }}</span>
            </span>
            <span v-if="selected" class="pill pill-ghost font-mono text-xs tabular-nums">
              #{{ selected.id }}
            </span>
          </span>
          <select v-model="selectedID" class="input w-full font-mono tabular-nums" :disabled="loading || busy" @change="onWorkspaceSelectionChange">
            <option :value="null" disabled>{{ t('workspaces.select') }}</option>
            <option v-for="workspace in workspaces" :key="workspace.id" :value="workspace.id">{{ workspace.name }}</option>
          </select>
        </label>
      </div>

      <p v-if="!loading && workspaces.length === 0" class="card p-8 text-center text-sm text-[var(--muted-foreground)]">
        {{ t('workspaces.empty') }}
      </p>

      <section v-if="selected && selected.role === 'admin'" class="card space-y-4 p-5">
        <div class="flex flex-wrap items-center justify-between gap-2 border-b border-[var(--border)] pb-3">
          <div class="flex items-center gap-2">
            <span class="inline-block h-1.5 w-1.5 rounded-full bg-[var(--oh-accent,#f59e0b)]" aria-hidden="true" />
            <h2 class="text-lg font-semibold text-[var(--foreground)]">
              {{ selected.name }} · {{ t('workspaces.resources') }}
            </h2>
          </div>
          <span class="chip font-mono text-xs tabular-nums">
            {{ resources.length }}
          </span>
        </div>
        <div data-testid="workspace-quick-actions" class="flex flex-wrap gap-2.5">
          <RouterLink v-for="resource in resources" :key="resource.path" :to="`/workspaces/${selected.id}/${resource.path}`" class="btn btn-secondary">{{ t(resource.label) }}</RouterLink>
        </div>
      </section>

      <!-- Projects & Scope Switcher Section (uses div/table so legacy form/li selectors stay untouched) -->
      <section v-if="selected" data-testid="workspace-projects-section" class="card space-y-4 p-5">
        <div class="flex flex-wrap items-start justify-between gap-3 border-b border-[var(--border)] pb-3">
          <div>
            <div class="flex items-center gap-2">
              <span class="inline-block h-1.5 w-1.5 rounded-full bg-[var(--oh-accent,#f59e0b)]" aria-hidden="true" />
              <h2 class="text-lg font-semibold text-[var(--foreground)]">{{ t('workspaces.projectsTitle') }}</h2>
              <span class="pill pill-ghost font-mono text-xs tabular-nums">{{ projects.length }}</span>
            </div>
            <p class="mt-1 text-xs text-[var(--muted-foreground)]">{{ t('workspaces.projectsDesc') }}</p>
          </div>
          <button
            type="button"
            data-testid="refresh-projects-btn"
            class="btn btn-secondary btn-sm"
            :disabled="projectsLoading"
            @click="loadProjects(selected.id, true)"
          >
            {{ t('common.refresh') }}
          </button>
        </div>

        <ConsoleAsyncBanner
          v-if="projectsNotice"
          :notice="projectsNotice"
          @retry="loadProjects(selected.id, true)"
        />

        <!-- Create Project Bar (admin or projects.create grant, div container instead of form) -->
        <div
          v-if="canCreateProject"
          class="flex flex-wrap items-center gap-2 rounded-[var(--oh-r-md,8px)] border border-[var(--border)] bg-[var(--surface-2)] p-3"
        >
          <input
            v-model="newProjectName"
            data-testid="new-project-name-input"
            type="text"
            maxlength="100"
            :placeholder="t('workspaces.newProjectPlaceholder')"
            class="input flex-1 text-sm"
            @keydown.enter.prevent="handleCreateProject"
          />
          <button
            type="button"
            data-testid="create-project-btn"
            class="btn btn-primary btn-sm"
            :disabled="projectActionBusy || !newProjectName.trim()"
            @click="handleCreateProject"
          >
            {{ t('workspaces.createProject') }}
          </button>
        </div>

        <div v-if="projects.length > 0" class="overflow-x-auto">
          <table class="w-full border-collapse text-left text-xs">
            <thead>
              <tr class="border-b border-[var(--border)] font-mono text-[11px] uppercase tracking-wider text-[var(--muted-foreground)]">
                <th class="py-2 pr-3">ID</th>
                <th class="px-3 py-2">{{ isZh ? '项目名称' : 'Project' }}</th>
                <th class="px-3 py-2">{{ isZh ? '状态' : 'State' }}</th>
                <th class="py-2 pl-3 text-right">{{ isZh ? '操作' : 'Actions' }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-[var(--border)]">
              <tr v-for="proj in projects" :key="proj.id" :data-testid="`project-row-${proj.id}`">
                <td class="py-2.5 pr-3 font-mono tabular-nums text-[var(--muted-foreground)]">#{{ proj.id }}</td>
                <td class="px-3 py-2.5">
                  <div v-if="renamingProjectId === proj.id" class="flex items-center gap-1.5">
                    <input
                      v-model="renameProjectDraft"
                      data-testid="rename-project-input"
                      type="text"
                      maxlength="100"
                      class="input h-7 px-2 text-xs"
                    />
                    <button
                      type="button"
                      data-testid="confirm-rename-project-btn"
                      class="btn btn-primary btn-sm px-2 py-1 text-xs"
                      :disabled="projectActionBusy || !renameProjectDraft.trim()"
                      @click="confirmRenameProject(proj)"
                    >
                      {{ isZh ? '保存' : 'Save' }}
                    </button>
                    <button
                      type="button"
                      class="btn btn-secondary btn-sm px-2 py-1 text-xs"
                      @click="renamingProjectId = null"
                    >
                      {{ isZh ? '取消' : 'Cancel' }}
                    </button>
                  </div>
                  <div v-else class="flex items-center gap-2">
                    <span class="font-medium text-[var(--foreground)]">{{ proj.name }}</span>
                    <span
                      v-if="isSharedDefaultProject(proj)"
                      data-testid="shared-default-project-badge"
                      class="rounded border border-[var(--border)] bg-[var(--surface-2)] px-1.5 py-0.5 font-mono text-[10px] text-[var(--muted-foreground)]"
                    >
                      {{ t('workspaces.defaultSharedProjectBadge') }}
                    </span>
                    <span
                      v-if="
                        String(selected.id) === activeScope.workspaceId &&
                        proj.id === activeScope.projectId
                      "
                      class="rounded border border-[var(--oh-accent-border,rgba(245,158,11,0.4))] bg-[var(--oh-accent-subtle,rgba(245,158,11,0.12))] px-1.5 py-0.5 font-mono text-[10px] font-semibold text-[var(--foreground)]"
                    >
                      {{ isZh ? '当前生效' : 'ACTIVE' }}
                    </span>
                  </div>
                </td>
                <td class="px-3 py-2.5 font-mono text-[11px] uppercase text-[var(--muted-foreground)]">
                  {{
                    (proj.state ?? 'active') === 'archived'
                      ? isZh
                        ? '已归档'
                        : 'Archived'
                      : isZh
                        ? '启用中'
                        : 'Active'
                  }}
                </td>
                <td class="py-2.5 pl-3 text-right">
                  <div class="inline-flex flex-wrap items-center justify-end gap-1.5">
                    <button
                      type="button"
                      :data-testid="`switch-project-${proj.id}`"
                      class="btn btn-secondary btn-sm px-2.5 py-1 font-mono text-xs"
                      @click="activateScope(String(selected.id), proj.id)"
                    >
                      {{ t('workspaces.activateScope') }}
                    </button>
                    <button
                      v-if="canEditProjectRow(proj) && (proj.state ?? 'active') === 'active'"
                      type="button"
                      :data-testid="`start-rename-project-${proj.id}`"
                      class="btn btn-secondary btn-sm px-2 py-1 text-xs"
                      @click="startRenameProject(proj)"
                    >
                      {{ t('workspaces.renameProject') }}
                    </button>
                    <button
                      v-if="canDeleteProjectRow(proj) && (proj.state ?? 'active') === 'active'"
                      type="button"
                      :data-testid="`archive-project-${proj.id}`"
                      class="btn btn-secondary btn-sm px-2 py-1 text-xs text-[var(--oh-danger,#dc2626)]"
                      :disabled="projectActionBusy"
                      @click="handleArchiveProject(proj)"
                    >
                      {{ t('workspaces.archiveProject') }}
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-else-if="!projectsLoading" class="text-xs text-[var(--muted-foreground)]">
          {{ t('workspaces.noProjects') }}
        </p>
      </section>

      <!-- Pending Access Requests Inbox for Workspace Admins -->
      <WorkspaceAccessRequestsInbox
        v-if="selected && canManageWorkspaceGrants && hasAuthenticatedUser"
        :workspace-id="String(selected.id)"
        :workspace-is-default="Boolean(selected.is_default)"
        :projects="projects"
        :grant-action-options="grantActionOptions"
        @decided="loadMembers"
      />

      <section v-if="canAdministerWorkspace" class="card space-y-4 p-5" :aria-busy="loadingMembers">
        <div class="flex flex-wrap items-center justify-between gap-2 border-b border-[var(--border)] pb-3">
          <div class="flex items-center gap-2">
            <span class="inline-block h-1.5 w-1.5 rounded-full bg-[var(--oh-accent,#f59e0b)]" aria-hidden="true" />
            <h2 class="text-lg font-semibold text-[var(--foreground)]">{{ t('workspaces.members') }}</h2>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <input
              v-if="members.length > 0"
              v-model="memberSearchQuery"
              type="search"
              data-testid="workspace-member-filter-input"
              :placeholder="t('workspaces.filterMembersPlaceholder')"
              class="input h-8 px-2.5 text-xs"
            />
            <span class="chip font-mono text-xs tabular-nums">{{ filteredWorkspaceMembers.length }}</span>
          </div>
        </div>
        <form class="flex flex-wrap items-end gap-3 rounded-[var(--oh-r-md,8px)] border border-[var(--border)] bg-[var(--surface-2)] p-4" @submit.prevent="saveMember">
          <label class="space-y-2 text-sm font-medium text-[var(--foreground)]"><span class="block">{{ t('workspaces.userID') }}</span><input v-model.number="userID" type="number" min="1" required class="input font-mono tabular-nums" /></label>
          <label class="space-y-2 text-sm font-medium text-[var(--foreground)]"><span class="block">{{ t('workspaces.role') }}</span><select v-model="memberRole" class="input"><option value="member">{{ t('workspaces.member') }}</option><option value="admin">{{ t('workspaces.admin') }}</option></select></label>
          <button class="btn btn-primary" :disabled="busy || !userID">{{ t('workspaces.save') }}</button>
        </form>
        <ul class="divide-y divide-[var(--border)]">
          <li v-for="member in filteredWorkspaceMembers" :key="member.user_id" class="flex flex-wrap items-center justify-between gap-3 py-3">
            <div class="flex flex-wrap items-center gap-2">
              <span class="font-mono text-sm tabular-nums text-[var(--foreground)]">{{ t('workspaces.userID') }}: {{ member.user_id }}</span>
              <span
                :data-testid="`member-role-badge-${member.user_id}`"
                class="rounded border px-2 py-0.5 font-mono text-[11px] font-medium"
                :class="
                  member.role === 'admin'
                    ? 'border-[var(--oh-accent-border,rgba(245,158,11,0.4))] bg-[var(--oh-accent-subtle,rgba(245,158,11,0.12))] text-[var(--foreground)]'
                    : 'border-[var(--border)] bg-[var(--surface-2)] text-[var(--muted-foreground)]'
                "
              >
                {{ member.role === 'admin' ? t('workspaces.admin') : t('workspaces.member') }}
              </span>
            </div>
            <div class="flex flex-wrap items-center gap-2">
              <button
                type="button"
                :data-testid="`open-member-grants-${member.user_id}`"
                class="btn btn-secondary btn-sm text-xs"
                :disabled="busy"
                @click="openMemberGrantMatrix(member)"
              >
                {{ t('workspaces.memberGrants') }}
              </button>
              <button type="button" class="btn btn-secondary" :disabled="busy" @click="removeMember(member.user_id)">{{ t('workspaces.remove') }}</button>
            </div>
          </li>
        </ul>
      </section>
      <section class="card space-y-5 p-5 sm:p-6" :aria-busy="policyLoading || policySaving" aria-labelledby="workspace-policy-title">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 id="workspace-policy-title" class="text-lg font-semibold text-[var(--foreground)]">
              {{ isZh ? '工作空间路由与合规策略中心' : 'Workspace Routing & Compliance Policy' }}
            </h2>
            <p class="mt-2 text-sm text-[var(--muted-foreground)]">
              {{
                isZh
                  ? '管理当前账户的路由偏好与合规配置；切换工作空间不会更改此账户级设置。'
                  : 'Manage account-level routing preferences and compliance guardrails across workspaces.'
              }}
            </p>
          </div>
          <button type="button" class="btn btn-secondary" :disabled="policyLoading || policySaving" @click="loadRoutingPolicies">
            {{ policyLoading ? (isZh ? '正在读取…' : 'Loading…') : (isZh ? '重新读取' : 'Reload') }}
          </button>
        </div>
        <p v-if="policyError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ policyError }}</p>
        <fieldset v-if="routingPolicy" :disabled="policyLoading || policySaving" class="space-y-5">
          <legend class="sr-only">{{ isZh ? '路由与合规策略配置' : 'Routing & Compliance Policy' }}</legend>
          <div class="grid gap-4 sm:grid-cols-3">
            <label class="flex items-center gap-2 text-sm">
              <input v-model="routingPolicy.zdr_enabled" type="checkbox" class="h-4 w-4" />
              {{ isZh ? '零数据留存 (ZDR)' : 'Zero Data Retention (ZDR)' }}
            </label>
            <label class="flex items-center gap-2 text-sm">
              <input v-model="routingPolicy.allow_fallback" type="checkbox" class="h-4 w-4" />
              {{ isZh ? '允许回退渠道' : 'Allow Fallback Channels' }}
            </label>
            <label class="flex items-center gap-2 text-sm">
              <input v-model="routingPolicy.auto_prompt_cache" type="checkbox" class="h-4 w-4" />
              {{ isZh ? '自动提示词缓存' : 'Automatic Prompt Caching' }}
            </label>
          </div>
          <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            <label class="space-y-2 text-sm">
              <span class="block">{{ isZh ? '路由偏好' : 'Routing Strategy' }}</span>
              <select v-model="routingPolicy.strategy_mode" class="input w-full">
                <option value="smart">{{ isZh ? '智能均衡' : 'Balanced (Smart)' }}</option>
                <option value="price">{{ isZh ? '最低成本' : 'Lowest Cost' }}</option>
                <option value="latency">{{ isZh ? '最低延迟' : 'Lowest Latency' }}</option>
                <option value="throughput">{{ isZh ? '最高吞吐' : 'Highest Throughput' }}</option>
              </select>
            </label>
            <label class="space-y-2 text-sm">
              <span class="block">{{ isZh ? '数据处理区域' : 'Data Processing Region' }}</span>
              <select v-model="routingPolicy.region_preference" class="input w-full">
                <option value="global">{{ isZh ? '全球自动' : 'Global Auto' }}</option>
                <option value="us">{{ isZh ? '美国 (US)' : 'United States (US)' }}</option>
                <option value="eu">{{ isZh ? '欧盟 (EU)' : 'European Union (EU)' }}</option>
                <option value="ap">{{ isZh ? '亚太 (APAC)' : 'Asia Pacific (APAC)' }}</option>
              </select>
            </label>
            <label class="space-y-2 text-sm">
              <span class="block">{{ isZh ? '月度预算告警阈值 (%)' : 'Monthly Budget Alert Threshold (%)' }}</span>
              <input v-model.number="routingPolicy.alert_threshold_percent" type="number" min="1" max="100" step="1" required class="input w-full font-mono" />
            </label>
          </div>
          <button type="button" class="btn btn-primary" @click="saveRoutingPolicies">
            {{ policySaving ? (isZh ? '正在保存…' : 'Saving…') : (isZh ? '保存路由策略' : 'Save Routing Policy') }}
          </button>
        </fieldset>
        <p v-else-if="policyLoading" role="status" class="text-sm text-[var(--muted-foreground)]">
          {{ isZh ? '正在读取路由策略…' : 'Loading routing policy…' }}
        </p>
        <p v-else-if="!policyError" role="status" class="text-sm text-[var(--muted-foreground)]">
          {{ isZh ? '暂无可用的路由策略配置。' : 'Routing policy configuration is currently unavailable.' }}
        </p>
      </section>

      <!-- Shared Per-Project Grant Matrix Modal -->
      <MemberGrantMatrixModal
        v-if="selected"
        :open="Boolean(grantMatrixMember)"
        :workspace-id="String(selected.id)"
        :workspace-is-default="Boolean(selected.is_default)"
        :member="grantMatrixMember"
        :projects="projects"
        :grant-action-options="grantActionOptions"
        @close="grantMatrixMember = null"
      />

      <!-- Project Access Request Modal for Requester -->
      <ProjectAccessRequest
        v-if="selected && showAccessRequestModal"
        mode="modal"
        :open="showAccessRequestModal"
        :initial-workspace-id="String(selected.id)"
        :workspaces-override="requesterWorkspacesSummary"
        @close="showAccessRequestModal = false"
        @switched-project="handleAccessRequestSwitchedProject"
      />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import { workspacesAPI, type Workspace, type WorkspaceMember } from '@/api/workspaces'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { normalizeApiError } from '@/utils/apiError'
import type { RoutingPolicyConfig } from '@/api/mcp'
import { classifyConsoleError, type ConsoleAsyncNotice } from '@/features/console/asyncState'
import ConsoleAsyncBanner from '@/features/console/components/ConsoleAsyncBanner.vue'
import {
  identityApi,
  persistConsoleScope,
  readPersistedConsoleScope,
  type ConsoleScopeSelection,
  type IdentityProject
} from '@/features/console/scope'
import { membersV2API } from '@/features/members/api'
import { isProtectedSharedDefaultProject } from '@/features/members/cas'
import MemberGrantMatrixModal, {
  type GrantMatrixMemberTarget
} from '@/features/members/MemberGrantMatrixModal.vue'
import WorkspaceAccessRequestsInbox from '@/features/members/WorkspaceAccessRequestsInbox.vue'
import type { IdentityAccess, IdentityWorkspace } from '@/features/members/types'
import ProjectAccessRequest from '@/features/onboarding/ProjectAccessRequest.vue'

const { t, locale } = useI18n()
const isZh = computed(() => String(locale?.value || '').startsWith('zh'))
const auth = useAuthStore()
const app = useAppStore()
const workspaces = ref<Workspace[]>([])
const members = ref<WorkspaceMember[]>([])
const memberSearchQuery = ref('')
const grantMatrixMember = ref<GrantMatrixMemberTarget | null>(null)
const props = defineProps<{ requestAccess?: boolean }>()
const showAccessRequestModal = ref(props.requestAccess === true)
watch(() => props.requestAccess, value => { showAccessRequestModal.value = value === true })
const selectedID = ref<number | null>(null)
const selected = computed(() => workspaces.value.find(workspace => workspace.id === selectedID.value))
const hasAuthenticatedUser = computed(() => Boolean((auth as { user?: { id?: number } })?.user?.id))

const workspaceAccess = ref<IdentityAccess | null>(null)
const workspaceAccessLoaded = ref(false)
const projectAccessById = ref<Record<string, string[]>>({})

const activeScope = ref<ConsoleScopeSelection>(readPersistedConsoleScope())
const isSelectedWorkspaceActive = computed(
  () => Boolean(selected.value && String(selected.value.id) === activeScope.value.workspaceId)
)
const preferredProject = computed(
  () => projects.value.find((proj) => (proj.state ?? 'active') === 'active') ?? projects.value[0] ?? null
)
const activeProject = computed(
  () => projects.value.find((proj) => proj.id === activeScope.value.projectId) ?? null
)
const scopeConfirmedNotice = ref<ConsoleAsyncNotice | null>(null)
const projects = ref<IdentityProject[]>([])
const projectsLoading = ref(false)
const projectActionBusy = ref(false)
const projectsNotice = ref<ConsoleAsyncNotice | null>(null)
const newProjectName = ref('')
const renamingProjectId = ref<string | null>(null)
const renameProjectDraft = ref('')
let projectRequestSeq = 0
const projectActions = ref<string[]>([])
const projectAccessLoaded = ref(false)
let accessRequestSeq = 0

function hasProjectAction(action: string): boolean {
  return projectActions.value.includes('*') || projectActions.value.includes(action)
}

function hasWorkspaceAction(action: string): boolean {
  const actions = workspaceAccess.value?.actions ?? []
  return (
    Boolean(workspaceAccess.value?.effective_admin) ||
    actions.includes('*') ||
    actions.includes(action)
  )
}

const canAdministerWorkspace = computed(
  () =>
    selected.value?.role === 'admin' ||
    (workspaceAccessLoaded.value &&
      (Boolean(workspaceAccess.value?.effective_admin) ||
        hasWorkspaceAction('members.grants.write')))
)

const canManageWorkspaceGrants = computed(
  () =>
    selected.value?.role === 'admin' ||
    (workspaceAccessLoaded.value && hasWorkspaceAction('members.grants.write')) ||
    (projectAccessLoaded.value && hasProjectAction('members.grants.write'))
)

const workspaceRoleSourceLabel = computed<string>(() => {
  const src = workspaceAccess.value?.role_source
  if (src === 'inherited') return t('workspaces.adminInherited')
  if (src === 'delegated') return t('workspaces.adminDelegated')
  return ''
})

const filteredWorkspaceMembers = computed(() => {
  const q = memberSearchQuery.value.trim().toLowerCase()
  if (!q) return members.value
  return members.value.filter((m) => {
    const idMatch = String(m.user_id).toLowerCase().includes(q)
    const roleMatch = String(m.role || '').toLowerCase().includes(q)
    return idMatch || roleMatch
  })
})

const DEFAULT_GRANTABLE_ACTIONS = [
  { action: 'text.chat', labelKey: 'membersAccess.actions.textChat' },
  { action: 'text.responses', labelKey: 'membersAccess.actions.textResponses' },
  { action: 'text.embeddings', labelKey: 'membersAccess.actions.textEmbeddings' },
  { action: 'image.generate', labelKey: 'membersAccess.actions.imageGenerate' },
  { action: 'projects.read', labelKey: 'membersAccess.actions.projectsRead' },
  { action: 'projects.edit', labelKey: 'membersAccess.actions.projectsEdit' },
  { action: 'projects.delete', labelKey: 'membersAccess.actions.projectsDelete' },
  { action: 'models.read', labelKey: 'membersAccess.actions.modelsRead' },
  { action: 'keys.read', labelKey: 'membersAccess.actions.keysRead' },
  { action: 'keys.create', labelKey: 'membersAccess.actions.keysCreate' },
  { action: 'keys.revoke', labelKey: 'membersAccess.actions.keysRevoke' },
  { action: 'credentials.byok.manage', labelKey: 'membersAccess.actions.credentialsByokManage' },
  { action: 'playground.read', labelKey: 'membersAccess.actions.playgroundRead' },
  { action: 'playground.run', labelKey: 'membersAccess.actions.playgroundRun' },
  { action: 'requests.read', labelKey: 'membersAccess.actions.requestsRead' },
  { action: 'usage.read', labelKey: 'membersAccess.actions.usageRead' },
  { action: 'billing.read', labelKey: 'membersAccess.actions.billingRead' },
  { action: 'tasks.read', labelKey: 'membersAccess.actions.tasksRead' },
  { action: 'tasks.create', labelKey: 'membersAccess.actions.tasksCreate' },
  { action: 'tasks.artifacts.read', labelKey: 'membersAccess.actions.tasksArtifactsRead' }
] as const

const grantActionOptions = computed(() => {
  const explicit = workspaceAccess.value?.grantable_actions
  if (Array.isArray(explicit) && explicit.length > 0) {
    const knownMap = new Map<string, string>(
      DEFAULT_GRANTABLE_ACTIONS.map((item) => [item.action, t(item.labelKey)])
    )
    return explicit.map((action) => ({
      action,
      label: knownMap.get(action) ?? action
    }))
  }
  return DEFAULT_GRANTABLE_ACTIONS.map((item) => ({
    action: item.action,
    label: t(item.labelKey)
  }))
})

const requesterWorkspacesSummary = computed<IdentityWorkspace[]>(() =>
  workspaces.value.map((ws) => ({
    workspace_id: String(ws.id),
    name: ws.name,
    is_default: ws.is_default,
    projects:
      selected.value?.id === ws.id
        ? projects.value.map((p) => ({
            id: p.id,
            name: p.name,
            is_default: p.is_default
          }))
        : [],
    projects_next_cursor: null
  }))
)

function isSharedDefaultProject(proj: IdentityProject): boolean {
  return isProtectedSharedDefaultProject(selected.value, proj as { is_default?: boolean })
}

const canCreateProject = computed(
  () =>
    selected.value?.role === 'admin' ||
    (workspaceAccessLoaded.value && hasWorkspaceAction('projects.create')) ||
    (projectAccessLoaded.value && hasProjectAction('projects.create'))
)
const canEditProject = computed(
  () => selected.value?.role === 'admin' || (projectAccessLoaded.value && hasProjectAction('projects.edit'))
)
const canDeleteProject = computed(
  () => selected.value?.role === 'admin' || (projectAccessLoaded.value && hasProjectAction('projects.delete'))
)

function canEditProjectRow(proj: IdentityProject): boolean {
  if (isSharedDefaultProject(proj)) return false
  const rowActions = projectAccessById.value[proj.id]
  if (Array.isArray(rowActions)) {
    return selected.value?.role === 'admin' || rowActions.includes('*') || rowActions.includes('projects.edit')
  }
  return canEditProject.value
}

function canDeleteProjectRow(proj: IdentityProject): boolean {
  if (isSharedDefaultProject(proj)) return false
  const rowActions = projectAccessById.value[proj.id]
  if (Array.isArray(rowActions)) {
    return selected.value?.role === 'admin' || rowActions.includes('*') || rowActions.includes('projects.delete')
  }
  return canDeleteProject.value
}

function openMemberGrantMatrix(member: WorkspaceMember): void {
  grantMatrixMember.value = {
    user_id: member.user_id,
    role: member.role
  }
}

function handleAccessRequestSwitchedProject(payload: { workspaceId: string; projectId: string }): void {
  showAccessRequestModal.value = false
  activateScope(payload.workspaceId, payload.projectId)
}
const showQuickActionKeys = computed(() => !projectAccessLoaded.value || hasProjectAction('keys.read'))
const showQuickActionPlayground = computed(() => !projectAccessLoaded.value || hasProjectAction('playground.run'))
const showQuickActionUsage = computed(
  () =>
    !projectAccessLoaded.value ||
    hasProjectAction('usage.read') ||
    hasProjectAction('requests.read') ||
    hasProjectAction('billing.read')
)
const showQuickActionTasks = computed(() => !projectAccessLoaded.value || hasProjectAction('tasks.read'))
const workspaceName = ref('')
const userID = ref<number | null>(null)
const memberRole = ref<WorkspaceMember['role']>('member')
const busy = ref(false)
const loading = ref(true)
const loadingMembers = ref(false)
let memberRequest = 0
const routingPolicy = ref<RoutingPolicyConfig | null>(null)
const policyLoading = ref(false)
const policySaving = ref(false)
const policyError = ref('')
let policyRequest = 0

async function loadRoutingPolicies() {
  if (policySaving.value) return
  const request = ++policyRequest
  policyLoading.value = true
  policyError.value = ''
  try {
    const { mcpAPI } = await import('@/api/mcp')
    if (request !== policyRequest) return
    const config = await mcpAPI.getRoutingPolicies()
    if (request === policyRequest) routingPolicy.value = config
  } catch (error) {
    if (request === policyRequest) {
      routingPolicy.value = null
      policyError.value =
        normalizeApiError(error).message ||
        (isZh.value ? '路由策略暂时无法读取，请重试。' : 'Failed to load routing policy. Please try again.')
    }
  } finally {
    if (request === policyRequest) policyLoading.value = false
  }
}

async function saveRoutingPolicies() {
  if (!routingPolicy.value || policySaving.value || policyLoading.value) return
  const config = routingPolicy.value
  if (!Number.isInteger(config.alert_threshold_percent) || config.alert_threshold_percent < 1 || config.alert_threshold_percent > 100) {
    policyError.value = isZh.value
      ? '月度预算告警阈值须为 1 至 100 的整数。'
      : 'Monthly budget alert threshold must be an integer between 1 and 100.'
    return
  }
  const request = ++policyRequest
  policySaving.value = true
  policyError.value = ''
  try {
    const { mcpAPI } = await import('@/api/mcp')
    if (request !== policyRequest) return
    const saved = await mcpAPI.updateRoutingPolicies({
      zdr_enabled: config.zdr_enabled,
      allow_fallback: config.allow_fallback,
      auto_prompt_cache: config.auto_prompt_cache,
      strategy_mode: config.strategy_mode,
      region_preference: config.region_preference,
      alert_threshold_percent: config.alert_threshold_percent,
    })
    if (request !== policyRequest) return
    routingPolicy.value = saved
    app.showSuccess(isZh.value ? '路由与合规策略已保存。' : 'Routing and compliance policy saved.')
  } catch (error) {
    if (request !== policyRequest) return
    policyError.value =
      normalizeApiError(error).message ||
      (isZh.value ? '保存失败，请稍后重试。' : 'Failed to save policy. Please try again.')
    app.showError(policyError.value)
  } finally {
    if (request === policyRequest) policySaving.value = false
  }
}

watch(() => auth.user?.id, () => {
  policyRequest++
  routingPolicy.value = null
  policySaving.value = false
  void loadRoutingPolicies()
})

onBeforeUnmount(() => { policyRequest++ })

const resources = [
  { path: 'accounts', label: 'nav.accounts' },
  { path: 'groups', label: 'nav.groups' },
  { path: 'proxies', label: 'nav.proxies' },
  { path: 'channels/pricing', label: 'nav.channels' },
  { path: 'channels/monitor', label: 'nav.channelMonitor' },
  { path: 'monitor', label: 'nav.channelStatus' },
]

async function loadMembers() {
  const request = ++memberRequest
  members.value = []
  if (!canAdministerWorkspace.value || !selected.value) return
  loadingMembers.value = true
  try {
    const result = await workspacesAPI.members(selected.value.id)
    if (request === memberRequest) members.value = result
  } catch (error) {
    if (request === memberRequest) app.showError(normalizeApiError(error).message)
  } finally {
    if (request === memberRequest) loadingMembers.value = false
  }
}

async function loadWorkspaceLevelAccess(workspaceId: string) {
  if (!workspaceId || !hasAuthenticatedUser.value || typeof membersV2API.getWorkspaceAccess !== 'function') {
    workspaceAccess.value = null
    workspaceAccessLoaded.value = false
    return
  }
  try {
    const acc = await membersV2API.getWorkspaceAccess(workspaceId)
    if (acc && typeof acc === 'object') {
      workspaceAccess.value = acc
      workspaceAccessLoaded.value = true
    }
  } catch {
    workspaceAccess.value = null
    workspaceAccessLoaded.value = false
  }
}

async function loadActiveProjectAccess(workspaceId: string, projectId: string | null | undefined) {
  const seq = ++accessRequestSeq
  if (!workspaceId || !projectId || typeof identityApi.getProjectAccess !== 'function') {
    projectAccessLoaded.value = false
    projectActions.value = []
    return
  }
  try {
    const access = await identityApi.getProjectAccess(workspaceId, projectId)
    if (seq !== accessRequestSeq) return
    if (Array.isArray(access?.actions)) {
      projectActions.value = access.actions
      projectAccessById.value = {
        ...projectAccessById.value,
        [projectId]: access.actions
      }
      projectAccessLoaded.value = true
    } else {
      projectAccessLoaded.value = false
      projectActions.value = []
    }
  } catch (err) {
    if (seq !== accessRequestSeq) return
    const status = (err as { status?: number; response?: { status?: number } })?.status ??
      (err as { response?: { status?: number } })?.response?.status
    if (status === 403) {
      projectActions.value = []
      projectAccessLoaded.value = true
    } else {
      projectAccessLoaded.value = false
      projectActions.value = []
    }
  }
}

async function loadProjects(workspaceID: number | null | undefined, explicit = false) {
  const seq = ++projectRequestSeq
  projectsNotice.value = null
  if (!workspaceID) {
    projects.value = []
    projectAccessLoaded.value = false
    projectActions.value = []
    projectAccessById.value = {}
    return
  }
  if (!explicit && !(auth as { user?: { id?: number } }).user?.id) {
    return
  }
  const hadCached = projects.value.length > 0
  projectsLoading.value = true
  try {
    void loadWorkspaceLevelAccess(String(workspaceID))
    const page = await identityApi.listProjects(String(workspaceID), { limit: 100 })
    if (seq === projectRequestSeq) {
      projects.value = page.items ?? []
      if (
        selected.value &&
        String(selected.value.id) === String(workspaceID) &&
        !activeScope.value.workspaceId
      ) {
        const defaultProj =
          projects.value.find((proj) => (proj.state ?? 'active') === 'active') ??
          projects.value[0] ??
          null
        const initialScope: ConsoleScopeSelection = {
          mode: 'workspace',
          workspaceId: String(selected.value.id),
          projectId: defaultProj?.id ?? null
        }
        persistConsoleScope(initialScope)
        activeScope.value = initialScope
      }
      const targetProjectId =
        activeScope.value.workspaceId === String(workspaceID)
          ? (activeScope.value.projectId ?? preferredProject.value?.id ?? null)
          : (preferredProject.value?.id ?? null)
      void loadActiveProjectAccess(String(workspaceID), targetProjectId)
    }
  } catch (err) {
    if (seq === projectRequestSeq) {
      projectsNotice.value = classifyConsoleError(err, {
        hasCachedData: hadCached,
        requiredGrantFallback: 'projects.read',
        isZh: isZh.value
      })
    }
  } finally {
    if (seq === projectRequestSeq) {
      projectsLoading.value = false
    }
  }
}

function onWorkspaceSelectionChange() {
  projects.value = []
  projectAccessLoaded.value = false
  projectActions.value = []
  renamingProjectId.value = null
  void loadMembers()
  void loadProjects(selectedID.value, false)
}

function activateScope(workspaceId: string, projectId: string | null) {
  const next: ConsoleScopeSelection = {
    mode: 'workspace',
    workspaceId,
    projectId
  }
  persistConsoleScope(next)
  activeScope.value = next
  void loadActiveProjectAccess(workspaceId, projectId)
  scopeConfirmedNotice.value = {
    kind: 'confirmed',
    title: projectId
      ? isZh.value
        ? `当前作用域已切换至工作空间 #${workspaceId} / 项目 #${projectId}`
        : `Active scope switched to Workspace #${workspaceId} / Project #${projectId}`
      : isZh.value
        ? `当前作用域已切换至工作空间 #${workspaceId}`
        : `Active scope switched to Workspace #${workspaceId}`
  }
}

async function handleCreateProject() {
  if (!selected.value || !newProjectName.value.trim() || projectActionBusy.value) return
  projectActionBusy.value = true
  projectsNotice.value = null
  try {
    const created = await identityApi.createProject(String(selected.value.id), {
      name: newProjectName.value.trim()
    })
    newProjectName.value = ''
    projects.value = [created, ...projects.value.filter((p) => p.id !== created.id)]
    scopeConfirmedNotice.value = {
      kind: 'confirmed',
      title: isZh.value
        ? `已创建项目「${created.name}」(#${created.id})`
        : `Created project "${created.name}" (#${created.id})`
    }
  } catch (err) {
    projectsNotice.value = classifyConsoleError(err, {
      requiredGrantFallback: 'projects.create',
      isZh: isZh.value
    })
  } finally {
    projectActionBusy.value = false
  }
}

function startRenameProject(proj: IdentityProject) {
  renamingProjectId.value = proj.id
  renameProjectDraft.value = proj.name
}

async function confirmRenameProject(proj: IdentityProject) {
  if (!selected.value || !renameProjectDraft.value.trim() || projectActionBusy.value) return
  projectActionBusy.value = true
  projectsNotice.value = null
  try {
    const updated = await identityApi.renameProject(
      String(selected.value.id),
      proj.id,
      { name: renameProjectDraft.value.trim() },
      proj.version
    )
    projects.value = projects.value.map((item) => (item.id === updated.id ? updated : item))
    renamingProjectId.value = null
    scopeConfirmedNotice.value = {
      kind: 'confirmed',
      title: isZh.value
        ? `已将项目 #${updated.id} 重命名为「${updated.name}」`
        : `Renamed project #${updated.id} to "${updated.name}"`
    }
  } catch (err) {
    projectsNotice.value = classifyConsoleError(err, {
      requiredGrantFallback: 'projects.edit',
      isZh: isZh.value
    })
  } finally {
    projectActionBusy.value = false
  }
}

async function handleArchiveProject(proj: IdentityProject) {
  if (!selected.value || projectActionBusy.value) return
  projectActionBusy.value = true
  projectsNotice.value = null
  try {
    const archived = await identityApi.archiveProject(
      String(selected.value.id),
      proj.id,
      proj.version
    )
    projects.value = projects.value.map((item) => (item.id === archived.id ? archived : item))
    scopeConfirmedNotice.value = {
      kind: 'confirmed',
      title: isZh.value
        ? `已归档项目「${archived.name}」(#${archived.id})`
        : `Archived project "${archived.name}" (#${archived.id})`
    }
  } catch (err) {
    projectsNotice.value = classifyConsoleError(err, {
      requiredGrantFallback: 'projects.delete',
      isZh: isZh.value
    })
  } finally {
    projectActionBusy.value = false
  }
}

async function refresh() {
  workspaces.value = await workspacesAPI.list()
  if (!workspaces.value.some(workspace => workspace.id === selectedID.value)) {
    const scoped = activeScope.value.workspaceId
      ? workspaces.value.find((workspace) => String(workspace.id) === activeScope.value.workspaceId)
      : undefined
    const defaultWs = workspaces.value.find((workspace) => workspace.is_default)
    selectedID.value = scoped?.id ?? defaultWs?.id ?? workspaces.value[0]?.id ?? null
  }
  await loadMembers()
  await loadProjects(selectedID.value, false)
}

async function createWorkspace() {
  busy.value = true
  try {
    const workspace = await workspacesAPI.create(workspaceName.value.trim())
    selectedID.value = workspace.id
    workspaceName.value = ''
    await refresh()
  } catch (error) { app.showError(normalizeApiError(error).message) }
  finally { busy.value = false }
}

async function saveMember() {
  if (!selected.value || !userID.value) return
  busy.value = true
  try {
    await workspacesAPI.setMember(selected.value.id, userID.value, memberRole.value)
    app.showSuccess(t('workspaces.saved'))
    await refresh()
  } catch (error) { app.showError(normalizeApiError(error).message) }
  finally { busy.value = false }
}

async function removeMember(memberID: number) {
  if (!selected.value) return
  busy.value = true
  try { await workspacesAPI.removeMember(selected.value.id, memberID); await refresh() }
  catch (error) { app.showError(normalizeApiError(error).message) }
  finally { busy.value = false }
}

onMounted(async () => {
  void loadRoutingPolicies()
  try { await refresh() }
  catch (error) { app.showError(normalizeApiError(error).message) }
  finally { loading.value = false }
})
</script>
