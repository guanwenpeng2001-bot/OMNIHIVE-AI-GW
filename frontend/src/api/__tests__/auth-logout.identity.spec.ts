import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { logout } from '../auth'
import { advanceAuthSession } from '../tokenRefresh'

const post = vi.hoisted(() => vi.fn())
vi.mock('../client', () => ({ default: { post }, apiClient: { post } }))
beforeEach(() => {
  localStorage.clear()
  vi.resetAllMocks()
  localStorage.setItem('auth_token', 'old-access')
  localStorage.setItem('refresh_token', 'old-refresh')
  localStorage.setItem('auth_user', JSON.stringify({ id: 1 }))
})
afterEach(() => localStorage.clear())

it.each(['resolve', 'reject'])('keeps a new login when old logout finishes with %s', async outcome => {
  let finish!: () => void
  post.mockReturnValue(new Promise<void>((resolve, reject) => {
    finish = () => outcome === 'resolve' ? resolve() : reject(new Error('offline'))
  }))
  const pending = logout()
  localStorage.setItem('auth_token', 'new-access')
  localStorage.setItem('refresh_token', 'new-refresh')
  advanceAuthSession()
  finish()
  await pending
  expect(post).toHaveBeenCalledWith('/auth/logout', { refresh_token: 'old-refresh' })
  expect(localStorage.getItem('auth_token')).toBe('new-access')
  expect(localStorage.getItem('refresh_token')).toBe('new-refresh')
})

it('clears the current session even if remote logout fails', async () => {
  post.mockRejectedValue(new Error('offline'))
  await logout()
  expect(localStorage.getItem('auth_token')).toBeNull()
  expect(localStorage.getItem('refresh_token')).toBeNull()
})
