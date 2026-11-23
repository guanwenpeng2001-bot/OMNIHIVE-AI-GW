import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { AuthorizeMCPGrantPayload, CreateMCPGrantPayload } from '../mcp'

const { get, post, put, remove } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  remove: vi.fn()
}))
vi.mock('@/api/client', () => ({ apiClient: { get, post, put, delete: remove } }))

const payload: CreateMCPGrantPayload = {
  name: 'Test client',
  redirect_uri: 'https://client.test/callback',
  scope: 'user',
  permission: 'read'
}

const authPayload: AuthorizeMCPGrantPayload = {
  ...payload,
  client_id: 'client-1',
  decision: 'approve'
}

let mcpAPI: typeof import('../mcp').mcpAPI
let MCPApiUnavailableError: typeof import('../mcp').MCPApiUnavailableError

beforeEach(async () => {
  vi.resetAllMocks()
  vi.resetModules()
  localStorage.clear()
  localStorage.setItem('auth_user', JSON.stringify({ id: 1, role: 'user' }))
  const mod = await import('../mcp')
  mcpAPI = mod.mcpAPI
  MCPApiUnavailableError = mod.MCPApiUnavailableError
})

function mockFailure(error: unknown) {
  get.mockRejectedValue(error)
  post.mockRejectedValue(error)
  put.mockRejectedValue(error)
  remove.mockRejectedValue(error)
}

describe('MCP API honest truthfulness contract', () => {
  it('calls backend endpoints directly and unwraps authentic responses', async () => {
    const listed = { endpoint: '/mcp', role: 'user', grants: [] }
    get.mockResolvedValueOnce({ data: listed })
    expect(await mcpAPI.listGrants()).toEqual(listed)
    expect(get).toHaveBeenLastCalledWith('/user/mcp/grants')

    const created = {
      ...payload,
      id: 'server-grant',
      client_id: 'client-1',
      status: 'active',
      created_at: '2026-11-20T00:00:00Z',
      expires_at: '2026-12-20T00:00:00Z',
      last_used_at: null
    }
    post.mockResolvedValueOnce({ data: created })
    expect(await mcpAPI.createGrant(payload)).toEqual(created)
    expect(post).toHaveBeenLastCalledWith('/user/mcp/grants', payload)

    remove.mockResolvedValueOnce({ data: { ...created, status: 'revoked' } })
    expect((await mcpAPI.revokeGrant('a/b')).status).toBe('revoked')
    expect(remove).toHaveBeenLastCalledWith('/user/mcp/grants/a%2Fb')

    get.mockResolvedValueOnce({ data: [{ id: 'audit-1', action: 'mcp.grant.created' }] })
    expect(await mcpAPI.listUserAuditLogs()).toEqual([{ id: 'audit-1', action: 'mcp.grant.created' }])
    expect(get).toHaveBeenLastCalledWith('/user/audit-logs')

    post.mockResolvedValueOnce({ data: { decision: 'approved', grant: created } })
    expect((await mcpAPI.authorizeGrant(authPayload)).decision).toBe('approved')
    expect(post).toHaveBeenLastCalledWith('/user/mcp/authorize', authPayload)

    get.mockResolvedValueOnce({
      data: {
        hidden: false,
        steps: [false, false, false],
        base_url: 'https://gw.test/v1',
        snippets: { claude_code: '', codex: '', openai_sdk: '' }
      }
    })
    expect((await mcpAPI.getOnboarding()).hidden).toBe(false)
    expect(get).toHaveBeenLastCalledWith('/user/onboarding')

    put.mockResolvedValueOnce({
      data: {
        hidden: true,
        steps: [true, false, false],
        base_url: 'https://gw.test/v1',
        snippets: { claude_code: '', codex: '', openai_sdk: '' }
      }
    })
    expect((await mcpAPI.updateOnboarding({ hidden: true })).hidden).toBe(true)
    expect(put).toHaveBeenLastCalledWith('/user/onboarding', { hidden: true })

    get.mockResolvedValueOnce({ data: { models: [], channels: [], cost: [] } })
    expect(await mcpAPI.getModelRankings()).toEqual({ models: [], channels: [], cost: [] })
    expect(get).toHaveBeenLastCalledWith('/model-plaza/rankings')
  })

  it.each([
    ['404 status', { status: 404, message: 'Not Found' }, 404],
    ['502 response', { response: { status: 502, data: { message: 'Bad Gateway' } } }, 502],
    ['503 response', { response: { status: 503, data: { message: 'Service Unavailable' } } }, 503],
    ['network failure', Object.assign(new TypeError('Failed to fetch'), { code: 'ERR_NETWORK' }), undefined]
  ])(
    'never returns fabricated grants, fake security audit logs, or fake rankings on %s',
    async (_label, failure, expectedStatus) => {
      mockFailure(failure)

      for (const invoke of [
        () => mcpAPI.listGrants(),
        () => mcpAPI.createGrant(payload),
        () => mcpAPI.revokeGrant('grant-1'),
        () => mcpAPI.authorizeGrant(authPayload),
        () => mcpAPI.listUserAuditLogs(),
        () => mcpAPI.getOnboarding(),
        () => mcpAPI.updateOnboarding({ hidden: true }),
        () => mcpAPI.getModelRankings(),
        () => mcpAPI.getRoutingPolicies()
      ]) {
        let caught: unknown
        try {
          await invoke()
        } catch (err) {
          caught = err
        }
        expect(caught).toBeInstanceOf(MCPApiUnavailableError)
        expect(caught).toMatchObject({
          state: 'unavailable',
          status: expectedStatus
        })
        const serialized = JSON.stringify(caught)
        expect(serialized).not.toContain('Claude Desktop MCP')
        expect(serialized).not.toContain('Cursor MCP Bridge')
        expect(serialized).not.toContain('工作台安全会话巡检通过')
        expect(serialized).not.toContain('99.98%')
      }

      expect(await mcpAPI.getAdminPulse()).toBeNull()
      expect(localStorage.getItem('mcp-onboarding:1')).toBeNull()
    }
  )

  it.each([400, 401, 403, 428, 500])(
    'preserves non-unavailable HTTP %s errors without fabricating fallback records',
    async (status) => {
      mockFailure({ status, response: { status } })
      await expect(mcpAPI.listGrants()).rejects.toMatchObject({ status })
      await expect(mcpAPI.createGrant(payload)).rejects.toMatchObject({ status })
      await expect(mcpAPI.revokeGrant('id')).rejects.toMatchObject({ status })
      await expect(mcpAPI.authorizeGrant(authPayload)).rejects.toMatchObject({ status })
      await expect(mcpAPI.listUserAuditLogs()).rejects.toMatchObject({ status })
      await expect(mcpAPI.getOnboarding()).rejects.toMatchObject({ status })
      await expect(mcpAPI.updateOnboarding({ hidden: true })).rejects.toMatchObject({ status })
      await expect(mcpAPI.getModelRankings()).rejects.toMatchObject({ status })
    }
  )

  it('aborts routing policy updates if the authenticated session changes mid-flight', async () => {
    get.mockImplementationOnce(async () => {
      localStorage.setItem('auth_user', JSON.stringify({ id: 2, role: 'user' }))
      return {
        data: {
          zdr_enabled: false,
          allow_fallback: true,
          auto_prompt_cache: true,
          strategy_mode: 'smart',
          region_preference: 'global',
          monthly_budget_usd: 100,
          alert_threshold_percent: 80,
          provider_whitelist: []
        }
      }
    })

    await expect(mcpAPI.updateRoutingPolicies({ zdr_enabled: true })).rejects.toMatchObject({
      status: 401
    })
    expect(put).not.toHaveBeenCalled()
  })
})
