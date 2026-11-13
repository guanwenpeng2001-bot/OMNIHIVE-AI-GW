import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { CreateMCPGrantPayload } from '../mcp'

const { get, post, put, remove } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), remove: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get, post, put, delete: remove } }))

const payload: CreateMCPGrantPayload = { name: 'Test client', redirect_uri: 'https://client.test/callback', scope: 'user', permission: 'read' }
let mcpAPI: typeof import('../mcp').mcpAPI

beforeEach(async () => {
  vi.resetAllMocks()
  vi.resetModules()
  localStorage.clear()
  localStorage.setItem('auth_user', JSON.stringify({ id: 1, role: 'user' }))
  mcpAPI = (await import('../mcp')).mcpAPI
})

function offline(status = 404) {
  put.mockRejectedValue({ status })
  get.mockRejectedValue({ status })
  post.mockRejectedValue({ response: { status } })
  remove.mockRejectedValue({ status })
}

describe('MCP API contract', () => {
  it('uses all four backend endpoints and unwraps client data', async () => {
    const listed = { endpoint: '/mcp', role: 'user', grants: [] }
    get.mockResolvedValueOnce({ data: listed })
    expect(await mcpAPI.listGrants()).toEqual(listed)
    expect(get).toHaveBeenLastCalledWith('/user/mcp/grants')
    const created = { ...payload, id: 'server-grant', status: 'active' }
    post.mockResolvedValueOnce({ data: created })
    expect(await mcpAPI.createGrant(payload)).toEqual(created)
    expect(post).toHaveBeenCalledWith('/user/mcp/grants', payload)
    remove.mockResolvedValueOnce({ data: { ...created, status: 'revoked' } })
    expect((await mcpAPI.revokeGrant('a/b')).status).toBe('revoked')
    expect(remove).toHaveBeenCalledWith('/user/mcp/grants/a%2Fb')
    get.mockResolvedValueOnce({ data: [{ id: 'audit' }] })
    expect(await mcpAPI.listUserAuditLogs()).toEqual([{ id: 'audit' }])
    expect(get).toHaveBeenLastCalledWith('/user/audit-logs')
  })

  it.each([404, 502])('maintains local lifecycle and audit events for %s', async status => {
    offline(status)
    const first = await mcpAPI.listGrants()
    expect(first.grants).toHaveLength(2)
    const created = await mcpAPI.createGrant({ ...payload, totp_code: '123456' })
    expect(created).not.toHaveProperty('totp_code')
    expect(created.last_used_at).toBeNull()
    expect(new Date(created.expires_at).getTime()).toBeGreaterThan(new Date(created.created_at).getTime())
    expect((await mcpAPI.listGrants()).grants).toHaveLength(3)
    expect((await mcpAPI.revokeGrant(created.id)).status).toBe('revoked')
    await mcpAPI.revokeGrant(created.id)
    const logs = await mcpAPI.listUserAuditLogs()
    expect(logs).toHaveLength(5)
    expect(logs.slice(0, 2).map(e => e.action)).toEqual(['mcp.grant.revoked', 'mcp.grant.created'])
    expect(logs[0].resource).toBe(created.id)
    first.grants[0].name = 'mutated'
    expect((await mcpAPI.listGrants()).grants[0].name).toBe('Claude Desktop MCP')
    await expect(mcpAPI.revokeGrant('missing')).rejects.toMatchObject({ status: 404 })
  })

  it.each([400, 401, 403, 428, 500])('does not hide backend %s errors', async status => {
    offline(status)
    await expect(mcpAPI.listGrants()).rejects.toMatchObject({ status })
    await expect(mcpAPI.createGrant(payload)).rejects.toMatchObject({ response: { status } })
    await expect(mcpAPI.revokeGrant('id')).rejects.toMatchObject({ status })
    await expect(mcpAPI.listUserAuditLogs()).rejects.toMatchObject({ status })
  })

  it('validates callback, scope, permission and administrator step-up in fallback', async () => {
    offline()
    for (const patch of [{ name: ' ' }, { redirect_uri: 'javascript:alert(1)' }, { redirect_uri: '/callback' }, { redirect_uri: 'https://' }, { scope: 'all' }, { permission: 'all' }]) {
      await expect(mcpAPI.createGrant({ ...payload, ...patch } as CreateMCPGrantPayload)).rejects.toMatchObject({ status: 400 })
    }
    await expect(mcpAPI.createGrant({ ...payload, scope: 'admin' })).rejects.toMatchObject({ status: 403 })
    localStorage.setItem('auth_user', JSON.stringify({ id: 1, role: 'admin' }))
    for (const code of ['', '12345', '1234567', 'abcdef', '１２３４５６']) {
      await expect(mcpAPI.createGrant({ ...payload, scope: 'admin', permission: 'write', totp_code: code })).rejects.toMatchObject({ status: 428 })
    }
    expect((await mcpAPI.createGrant({ ...payload, scope: 'admin', permission: 'write', totp_code: '012345' })).scope).toBe('admin')
  })

  it('isolates accounts and rejects fallback after an in-flight account change', async () => {
    offline()
    const created = await mcpAPI.createGrant(payload)
    localStorage.setItem('auth_user', JSON.stringify({ id: 2, role: 'user' }))
    expect((await mcpAPI.listGrants()).grants).toHaveLength(2)
    await expect(mcpAPI.revokeGrant(created.id)).rejects.toMatchObject({ status: 404 })
    expect(await mcpAPI.listUserAuditLogs()).toHaveLength(3)
    get.mockImplementationOnce(async () => {
      localStorage.setItem('auth_user', JSON.stringify({ id: 3, role: 'user' }))
      throw { status: 404 }
    })
    await expect(mcpAPI.listGrants()).rejects.toMatchObject({ status: 401 })
    localStorage.clear()
    await expect(mcpAPI.listGrants()).rejects.toMatchObject({ status: 401 })
  })

  it('uses upgraded backend responses after a local fallback', async () => {
    offline()
    await mcpAPI.listGrants()
    get.mockResolvedValueOnce({ data: { endpoint: '/mcp', role: 'admin', grants: [] } })
    expect((await mcpAPI.listGrants()).grants).toEqual([])
  })
})

describe('MCP extended contracts', () => {
  it('uses authorization, onboarding and public ranking endpoints', async () => {
    const approval = { ...payload, client_id: 'client', decision: 'approve' as const }
    post.mockResolvedValueOnce({ data: { decision: 'approved', grant: payload } })
    expect((await mcpAPI.authorizeGrant(approval)).decision).toBe('approved')
    expect(post).toHaveBeenLastCalledWith('/user/mcp/authorize', approval)
    get.mockResolvedValueOnce({ data: { hidden: false } })
    expect(await mcpAPI.getOnboarding()).toEqual({ hidden: false })
    expect(get).toHaveBeenLastCalledWith('/user/onboarding')
    put.mockResolvedValueOnce({ data: { hidden: true } })
    expect(await mcpAPI.updateOnboarding({ hidden: true })).toEqual({ hidden: true })
    expect(put).toHaveBeenLastCalledWith('/user/onboarding', { hidden: true })
    get.mockResolvedValueOnce({ data: { models: [], channels: [], cost: [] } })
    expect(await mcpAPI.getModelRankings()).toEqual({ models: [], channels: [], cost: [] })
    expect(get).toHaveBeenLastCalledWith('/model-plaza/rankings')
  })
  it.each([404, 502])('supports decisions and persistent isolated onboarding on %s', async status => {
    offline(status)
    const auth = { ...payload, client_id: 'client', decision: 'deny' as const, scope: 'admin' as const, permission: 'write' as const }
    expect((await mcpAPI.authorizeGrant(auth)).decision).toBe('denied')
    expect((await mcpAPI.listGrants()).grants).toHaveLength(2)
    expect((await mcpAPI.listUserAuditLogs())[0]).toMatchObject({ action: 'mcp.grant.denied', status: 'denied' })
    await expect(mcpAPI.authorizeGrant({ ...auth, decision: 'approve' })).rejects.toMatchObject({ status: 403 })
    localStorage.setItem('auth_user', JSON.stringify({ id: 1, role: 'admin' }))
    await expect(mcpAPI.authorizeGrant({ ...auth, decision: 'approve' })).rejects.toMatchObject({ status: 428 })
    expect((await mcpAPI.authorizeGrant({ ...auth, decision: 'approve', totp_code: '012345' })).decision).toBe('approved')
    await expect(mcpAPI.authorizeGrant({ ...auth, resource: 'javascript:alert(1)' })).rejects.toMatchObject({ status: 400 })
    expect((await mcpAPI.getOnboarding()).steps).toEqual([false, false, false])
    const updated = await mcpAPI.updateOnboarding({ hidden: true, step_index: 0, completed: true })
    updated.steps[0] = false
    await mcpAPI.updateOnboarding({ step_index: 0, completed: true })
    expect((await mcpAPI.listUserAuditLogs()).filter(e => e.action === 'onboarding.step.completed')).toHaveLength(1)
    vi.resetModules()
    mcpAPI = (await import('../mcp')).mcpAPI
    expect(await mcpAPI.getOnboarding()).toMatchObject({ hidden: true, steps: [true, false, false] })
    localStorage.setItem('auth_user', JSON.stringify({ id: 2, role: 'user' }))
    expect(await mcpAPI.getOnboarding()).toMatchObject({ hidden: false, steps: [false, false, false] })
    for (const patch of [{ step_index: -1, completed: true }, { step_index: 3, completed: true }, { step_index: 1.5, completed: true }, { completed: true }, { step_index: 1 }]) {
      await expect(mcpAPI.updateOnboarding(patch)).rejects.toMatchObject({ status: 400 })
    }
    localStorage.clear()
    expect((await mcpAPI.getModelRankings()).models.length).toBeGreaterThan(0)
    await expect(mcpAPI.getOnboarding()).rejects.toMatchObject({ status: 401 })
  })
  it.each([401, 403, 500])('preserves extended endpoint failures on %s', async status => {
    offline(status)
    await expect(mcpAPI.authorizeGrant({ ...payload, decision: 'approve', client_id: 'client' })).rejects.toMatchObject({ response: { status } })
    await expect(mcpAPI.getOnboarding()).rejects.toMatchObject({ status })
    await expect(mcpAPI.updateOnboarding({})).rejects.toMatchObject({ status })
    await expect(mcpAPI.getModelRankings()).rejects.toMatchObject({ status })
  })
})
