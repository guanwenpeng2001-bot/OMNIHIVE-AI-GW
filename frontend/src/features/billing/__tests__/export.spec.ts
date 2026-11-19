import { effectScope, ref } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { billingApi, scopedDownload, type CostWindow, type Operation } from '../api'
import { exportResult, useCsvExport } from '../useCsvExport'

const scope = { workspaceId: '3', projectId: '5' }
const window: CostWindow = {
  from: '2026-11-01T04:00:00Z', to: '2026-11-02T05:00:00Z', timezone: 'America/New_York', currency: 'USD',
}
const operation: Operation = {
  state: 'succeeded', operation_id: 'd27bbfbf-6710-42c2-91fe-5996b2330fd5', error: null,
  status_url: '/api/v2/workspaces/3/operations/d27bbfbf-6710-42c2-91fe-5996b2330fd5', resource_id: null,
  result: {
    download_url: '/api/v2/workspaces/3/projects/5/billing/exports/d27bbfbf-6710-42c2-91fe-5996b2330fd5/download',
    expires_at: '2099-11-19T08:30:00Z',
  },
}

afterEach(() => vi.restoreAllMocks())

describe('CSV export operation', () => {
  it('accepts only a download path for the exact registered scope and operation', () => {
    expect(scopedDownload(scope, operation)).toBe(operation.result?.download_url)
    expect(scopedDownload({ ...scope, projectId: '9' }, operation)).toBeNull()
    expect(exportResult(scope, { ...operation, state: 'running' })).toBe('pending')
    expect(exportResult(scope, { ...operation, result: { download_url: null } })).toBe('expired')
  })

  it('preserves the key after uncertain submission, and renews only after explicit expiration recovery', async () => {
    const runtime = effectScope()
    const create = vi.spyOn(billingApi, 'createExport')
      .mockRejectedValueOnce({ status: 503, reason: 'DEPENDENCY_UNAVAILABLE' })
      .mockResolvedValue(operation)
    const download = vi.spyOn(billingApi, 'download').mockRejectedValue({ status: 410, reason: 'ARTIFACT_EXPIRED' })
    const command = runtime.run(() => useCsvExport(() => 'scope/window', () => scope, () => window))!
    await command.create()
    expect(command.state.value).toBe('unknown')
    expect(create).toHaveBeenCalledTimes(1)
    await command.create()
    expect(command.state.value).toBe('ready')
    expect(create.mock.calls[0][2]).toBe(create.mock.calls[1][2])
    await command.download()
    expect(download).toHaveBeenCalledTimes(1)
    expect(command.state.value).toBe('expired')
    expect(create).toHaveBeenCalledTimes(2)
    await command.create(true)
    expect(create.mock.calls[2][2]).not.toBe(create.mock.calls[1][2])
    runtime.stop()
  })

  it('aborts and ignores an old result when the scope changes', async () => {
    const runtime = effectScope()
    const current = ref('scope-a')
    let finish: (result: Operation) => void = () => undefined
    const create = vi.spyOn(billingApi, 'createExport').mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const command = runtime.run(() => useCsvExport(() => current.value, () => scope, () => window))!
    const request = command.create()
    current.value = 'scope-b'
    expect(create.mock.calls[0][3].aborted).toBe(true)
    finish(operation)
    await request
    expect(command.state.value).toBe('idle')
    expect(command.operationID.value).toBeNull()
    runtime.stop()
  })
})
