import { describe, expect, it } from 'vitest'
import { workspaceRequestURL } from '../workspaceScope'

describe('workspace API scope', () => {
  it('binds reusable resource clients to the page workspace', () => {
    for (const resource of ['accounts', 'groups', 'channels', 'proxies', 'channel-monitors', 'scheduled-test-plans']) {
      expect(workspaceRequestURL(`/admin/${resource}/42`, '/workspaces/7/accounts')).toBe(`/workspaces/7/${resource}/42`)
    }
    expect(workspaceRequestURL('/keys/42', '/workspaces/7/keys')).toBe('/workspaces/7/keys/42')
  })
  it('keeps global system management and ordinary pages explicit', () => {
    expect(workspaceRequestURL('/admin/settings', '/workspaces/7/accounts')).toBe('/admin/settings')
    expect(workspaceRequestURL('/admin/accounts', '/admin/accounts')).toBe('/admin/accounts')
    expect(workspaceRequestURL('/admin/accounts', '/workspaces')).toBe('/admin/accounts')
    expect(workspaceRequestURL('/admin/accounts', '/workspaces/not-a-number/accounts')).toBe('/admin/accounts')
  })
})
