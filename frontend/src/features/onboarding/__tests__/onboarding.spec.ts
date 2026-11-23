import { baseCompile } from '@intlify/message-compiler'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import * as controlClient from '@/features/shared/controlClient'
import {
  accessRequestsAPI,
  normalizeAccessRequestPage,
  validateAccessRequestNote
} from '../accessRequests'
import ProjectAccessRequest from '../ProjectAccessRequest.vue'
import FirstRunChecklist from '../FirstRunChecklist.vue'
import AdminSetupBanner from '../AdminSetupBanner.vue'
import WorkspaceAccessRequestsInbox from '@/features/members/WorkspaceAccessRequestsInbox.vue'

function makeI18n(locale: 'zh' | 'en' = 'zh') {
  return createI18n({
    messageCompiler: (message) =>
      new Function('return ' + baseCompile(message as string).code)(),
    legacy: false,
    locale,
    messages: { zh, en }
  })
}

describe('accessRequestsAPI & fail-closed validation', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('validates note length (1-500 trimmed chars) and rejects empty or >500 notes', () => {
    expect(validateAccessRequestNote('   ').valid).toBe(false)
    expect(validateAccessRequestNote(' Need playground.run ').valid).toBe(true)
    expect(validateAccessRequestNote('a'.repeat(501)).valid).toBe(false)
  })

  it('fails closed on malformed access request page or item payloads', () => {
    expect(() => normalizeAccessRequestPage(null)).toThrow(/Malformed/)
    expect(() => normalizeAccessRequestPage({ items: [{ id: '1' }] })).toThrow(/Malformed/)
  })

  it('sends Idempotency-Key on POST access-requests and quoted If-Match on decision', async () => {
    const postSpy = vi.spyOn(controlClient, 'controlPost').mockResolvedValueOnce({
      id: 'req_1',
      workspace_id: '12',
      requested_project_id: '24',
      note: 'Need Playground access',
      state: 'pending',
      version: '1'
    })

    const created = await accessRequestsAPI.create(
      '12',
      { note: '  Need Playground access ', requested_project_id: '24' },
      'idem-fixed-1'
    )
    expect(created.state).toBe('pending')
    expect(postSpy).toHaveBeenCalledWith(
      '/workspaces/12/access-requests',
      { note: 'Need Playground access', requested_project_id: '24' },
      { 'Idempotency-Key': 'idem-fixed-1' }
    )

    postSpy.mockResolvedValueOnce({
      id: 'req_1',
      workspace_id: '12',
      approved_project_id: '24',
      note: 'Need Playground access',
      state: 'approved',
      version: '2'
    })

    const decided = await accessRequestsAPI.decide('12', 'req_1', '1', {
      decision: 'approve',
      project_id: '24',
      add_actions: ['text.chat', 'playground.run'],
      member_version: '3'
    })
    expect(decided.state).toBe('approved')
    expect(postSpy).toHaveBeenLastCalledWith(
      '/workspaces/12/access-requests/req_1/decision',
      {
        decision: 'approve',
        project_id: '24',
        add_actions: ['playground.run', 'text.chat'],
        member_version: '3'
      },
      { 'If-Match': '"1"' }
    )
  })
})

describe('ProjectAccessRequest & WorkspaceAccessRequestsInbox loop (AC-2)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.restoreAllMocks()
  })

  it('excludes protected shared Default project, submits request, and renders approved switch CTA', async () => {
    vi.spyOn(controlClient, 'controlGet').mockResolvedValueOnce({
      items: [],
      next_cursor: null
    })
    vi.spyOn(controlClient, 'controlPost').mockResolvedValueOnce({
      id: 'req_9',
      workspace_id: '1',
      note: 'Join Core Product team',
      state: 'pending',
      version: '1'
    })

    const wrapper = mount(ProjectAccessRequest, {
      props: {
        initialWorkspaceId: '1',
        workspacesOverride: [
          {
            workspace_id: '1',
            name: 'Default',
            is_default: true,
            projects: [
              { id: '1', name: 'Default', is_default: true },
              { id: '24', name: 'Core Product', is_default: false }
            ],
            projects_next_cursor: null
          }
        ]
      },
      global: { plugins: [makeI18n('zh')] }
    })
    await flushPromises()

    // Shared Default project (#1) is filtered out from requestable projects
    const projectOptions = wrapper
      .findAll('[data-testid="access-request-project-select"] option')
      .map((o) => o.attributes('value'))
    expect(projectOptions).toEqual(['', '24'])
    expect(wrapper.find('[data-testid="access-request-default-project-hint"]').exists()).toBe(true)

    // Submit access request
    await wrapper.get('[data-testid="access-request-note-input"]').setValue('Join Core Product team')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.get('[data-testid="access-request-pending-banner"]').text()).toContain(
      '项目权限申请已提交（状态：待工作空间管理员审批）'
    )

    // Simulate admin approval when requester clicks Refresh Status
    vi.spyOn(controlClient, 'controlGet').mockResolvedValueOnce({
      items: [
        {
          id: 'req_9',
          workspace_id: '1',
          approved_project_id: '24',
          approved_project_name: 'Core Product',
          note: 'Join Core Product team',
          state: 'approved',
          version: '2'
        }
      ],
      next_cursor: null
    })
    await wrapper.get('[data-testid="refresh-access-requests-btn"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="access-request-approved-banner"]').text()).toContain(
      '您已获得项目「Core Product」访问授权，可立即切换并调用。'
    )

    await wrapper.get('[data-testid="switch-to-approved-project-btn"]').trigger('click')
    expect(wrapper.emitted('switched-project')?.[0]).toEqual([
      { workspaceId: '1', projectId: '24' }
    ])
  })

  it('allows workspace admin to review, preview additive diff, and approve pending access request', async () => {
    vi.spyOn(controlClient, 'controlGet').mockImplementation(async (path: string) => {
      if (path.includes('/access-requests')) {
        return {
          items: [
            {
              id: 'req_9',
              workspace_id: '1',
              requester_user_id: '7',
              requester_username: 'bob',
              requested_project_id: '24',
              note: 'Need Playground access',
              state: 'pending',
              version: '1'
            }
          ],
          next_cursor: null
        }
      }
      if (path.includes('/members/7/grants')) {
        return {
          member_id: '7',
          version: '5',
          project_grants: [{ project_id: '24', actions: ['projects.read'] }]
        }
      }
      return {}
    })

    const postSpy = vi.spyOn(controlClient, 'controlPost').mockResolvedValueOnce({
      id: 'req_9',
      workspace_id: '1',
      requester_user_id: '7',
      approved_project_id: '24',
      note: 'Need Playground access',
      state: 'approved',
      version: '2'
    })

    const wrapper = mount(WorkspaceAccessRequestsInbox, {
      props: {
        workspaceId: '1',
        workspaceIsDefault: true,
        projects: [
          { id: '1', workspace_id: '1', name: 'Default', state: 'active', version: '1', is_default: true },
          { id: '24', workspace_id: '1', name: 'Core Product', state: 'active', version: '1', is_default: false }
        ],
        grantActionOptions: [
          { action: 'projects.read', label: '项目读取' },
          { action: 'playground.run', label: '调试台执行' },
          { action: 'text.chat', label: '对话生成' }
        ]
      },
      global: { plugins: [makeI18n('zh')] }
    })
    await flushPromises()

    await wrapper.get('[data-testid="open-approve-request-req_9"]').trigger('click')
    await flushPromises()

    // Shared Default project (#1) is locked/excluded from approval target picker
    const targetOpts = wrapper
      .findAll('[data-testid="approve-target-project-select"] option')
      .map((o) => o.attributes('value'))
    expect(targetOpts).toEqual(['24'])

    // Additive diff shows existing projects.read and newly added playground.run + text.chat
    const diffText = wrapper.get('[data-testid="approve-additive-diff-preview"]').text()
    expect(diffText).toContain('projects.read')
    expect(diffText).toContain('+ playground.run')

    await wrapper.get('[data-testid="confirm-approve-request-btn"]').trigger('click')
    await flushPromises()

    expect(postSpy).toHaveBeenCalledWith(
      '/workspaces/1/access-requests/req_9/decision',
      {
        decision: 'approve',
        project_id: '24',
        add_actions: ['playground.run', 'projects.read', 'text.chat'],
        member_version: '5'
      },
      { 'If-Match': '"1"' }
    )
  })
})

describe('FirstRunChecklist & AdminSetupBanner', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
  })

  it('renders separate personal prerequisite diagnostics and fail-closed project capability CTAs', async () => {
    const wrapper = mount(FirstRunChecklist, {
      props: {
        completedStepCount: 1,
        onboardingProgressPct: 33.3,
        availableGroups: [],
        availableGroupsLoaded: true,
        hasActiveSubscription: false,
        hasInactiveSubscription: true,
        subscriptionsLoaded: true,
        userBalance: 0,
        projectProbeState: 'denied',
        projectActions: []
      },
      global: { plugins: [makeI18n('zh')] }
    })

    // All 3 personal prerequisite diagnostics render distinctly
    expect(wrapper.find('[data-testid="personal-prereq-no-group"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="personal-prereq-inactive-sub"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="personal-prereq-no-balance"]').exists()).toBe(true)

    // Switch to Team Project branch
    await wrapper.get('[data-testid="onboarding-branch-project"]').trigger('click')
    await flushPromises()

    expect(
      (wrapper.get('[data-testid="project-step-2-cta"]').element as HTMLButtonElement).disabled
    ).toBe(true)
    expect(
      (wrapper.get('[data-testid="project-step-3-cta"]').element as HTMLButtonElement).disabled
    ).toBe(true)
    expect(wrapper.get('[data-testid="project-playground-denied-notice"]').text()).toContain(
      '模型调试台为团队项目工具'
    )
  })

  it('shows AdminSetupBanner when totalAccounts=0 or liveModels=0 and hides when both > 0', () => {
    const visible = mount(AdminSetupBanner, {
      props: { totalAccounts: 0, liveModels: 5 },
      global: {
        plugins: [makeI18n('zh')],
        stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } }
      }
    })
    expect(visible.find('[data-testid="admin-setup-banner"]').exists()).toBe(true)
    expect(visible.get('[data-testid="admin-setup-link-stage-3"]').attributes('href')).toBe(
      '/admin/models'
    )

    const hidden = mount(AdminSetupBanner, {
      props: { totalAccounts: 2, liveModels: 10 },
      global: {
        plugins: [makeI18n('zh')],
        stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } }
      }
    })
    expect(hidden.find('[data-testid="admin-setup-banner"]').exists()).toBe(false)
  })
})
