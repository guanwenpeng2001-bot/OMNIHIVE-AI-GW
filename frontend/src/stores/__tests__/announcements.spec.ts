import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { announcementsAPI } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useAnnouncementStore } from '@/stores/announcements'
import type { User, UserAnnouncement } from '@/types'

vi.mock('@/api', () => ({
  announcementsAPI: { list: vi.fn(), markRead: vi.fn() }
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

function announcement(id: number, popup = false): UserAnnouncement {
  return {
    id, title: `Announcement ${id}`, content: 'Content',
    notify_mode: popup ? 'popup' : 'silent',
    created_at: '2026-09-17', updated_at: '2026-09-17'
  }
}

function login(id = 1) {
  const auth = useAuthStore()
  auth.token = `token-${id}`
  auth.user = { id, role: 'user' } as User
  return auth
}

describe('announcement request lifecycle', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.useFakeTimers()
    vi.resetAllMocks()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    login()
    vi.mocked(announcementsAPI.markRead).mockResolvedValue({ message: 'ok' })
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('discards a popup response after reset', async () => {
    const old = deferred<UserAnnouncement[]>()
    vi.mocked(announcementsAPI.list).mockReturnValueOnce(old.promise)
    const store = useAnnouncementStore()
    const request = store.fetchAnnouncements()
    store.reset()
    old.resolve([announcement(1, true)])
    await request
    expect(store.announcements).toEqual([])
    expect(store.currentPopup).toBeNull()
    expect(store.loading).toBe(false)
  })

  it.each(['success', 'failure'])('old %s cannot change the new session loading or throttle', async (outcome) => {
    const old = deferred<UserAnnouncement[]>()
    const next = deferred<UserAnnouncement[]>()
    vi.mocked(announcementsAPI.list).mockReturnValueOnce(old.promise).mockReturnValueOnce(next.promise)
    const store = useAnnouncementStore()
    const first = store.fetchAnnouncements()
    login(2)
    const second = store.fetchAnnouncements()
    if (outcome === 'success') old.resolve([announcement(1, true)])
    else old.reject(new Error('old failure'))
    await first
    expect(store.loading).toBe(true)
    expect(store.announcements).toEqual([])
    await store.fetchAnnouncements()
    expect(announcementsAPI.list).toHaveBeenCalledTimes(2)
    next.resolve([announcement(2, true)])
    await second
    expect(store.currentPopup?.id).toBe(2)
    expect(store.loading).toBe(false)
  })

  it.each(['success', 'failure'])('ignores an older forced-fetch %s after the latest response', async (outcome) => {
    const old = deferred<UserAnnouncement[]>()
    vi.mocked(announcementsAPI.list).mockReturnValueOnce(old.promise).mockResolvedValueOnce([announcement(2, true)])
    const store = useAnnouncementStore()
    const first = store.fetchAnnouncements()
    await store.fetchAnnouncements(true)
    expect(store.loading).toBe(false)
    if (outcome === 'success') old.resolve([announcement(1, true)])
    else old.reject(new Error('old failure'))
    await first
    expect(store.announcements.map(a => a.id)).toEqual([2])
    expect(store.currentPopup?.id).toBe(2)
    await store.fetchAnnouncements()
    expect(announcementsAPI.list).toHaveBeenCalledTimes(2)
  })

  it('invalidates logout/relogin in one tick, including the same user', async () => {
    const old = deferred<UserAnnouncement[]>()
    vi.mocked(announcementsAPI.list).mockReturnValueOnce(old.promise)
    const store = useAnnouncementStore()
    const request = store.fetchAnnouncements()
    useAuthStore().token = null
    login()
    old.resolve([announcement(1, true)])
    await request
    expect(store.announcements).toEqual([])
  })

  it('keeps requests and cache during same-user token rotation', async () => {
    const pending = deferred<UserAnnouncement[]>()
    vi.mocked(announcementsAPI.list).mockReturnValueOnce(pending.promise)
    const store = useAnnouncementStore()
    const request = store.fetchAnnouncements()
    useAuthStore().token = 'rotated'
    pending.resolve([announcement(1)])
    await request
    await store.fetchAnnouncements()
    expect(store.announcements).toHaveLength(1)
    expect(announcementsAPI.list).toHaveBeenCalledTimes(1)
  })

  it('does not fetch from a delayed trigger after logout', async () => {
    const store = useAnnouncementStore()
    useAuthStore().token = null
    await store.fetchAnnouncements(true)
    expect(announcementsAPI.list).not.toHaveBeenCalled()
  })

  it.each(['single', 'all'])('ignores old %s read completion for the same announcement in a new session', async (mode) => {
    const pending = deferred<{ message: string }>()
    vi.mocked(announcementsAPI.markRead).mockReturnValueOnce(pending.promise)
    const store = useAnnouncementStore()
    store.announcements = [announcement(1)]
    const request = mode === 'single' ? store.markAsRead(1) : store.markAllAsRead()
    login(2)
    store.announcements = [announcement(1)]
    pending.resolve({ message: 'ok' })
    await request
    expect(store.unreadCount).toBe(1)
  })

  it('marks only the IDs included in a pending bulk-read request', async () => {
    const pending = deferred<{ message: string }>()
    vi.mocked(announcementsAPI.markRead).mockReturnValueOnce(pending.promise)
    const store = useAnnouncementStore()
    store.announcements = [announcement(1)]
    const request = store.markAllAsRead()
    store.announcements.push(announcement(2))
    pending.resolve({ message: 'ok' })
    await request
    expect(store.announcements[0].read_at).toBeTruthy()
    expect(store.announcements[1].read_at).toBeUndefined()
  })

  it('does not undo a successful read with a list that started earlier', async () => {
    const pending = deferred<UserAnnouncement[]>()
    vi.mocked(announcementsAPI.list).mockReturnValueOnce(pending.promise)
    const store = useAnnouncementStore()
    store.announcements = [announcement(1)]
    const request = store.fetchAnnouncements()
    await store.markAsRead(1)
    pending.resolve([announcement(1, true)])
    await request
    expect(store.unreadCount).toBe(0)
    expect(store.currentPopup).toBeNull()
  })

  it('cancels the previous session popup timer before it can advance the new queue', async () => {
    vi.mocked(announcementsAPI.list)
      .mockResolvedValueOnce([announcement(1, true), announcement(2, true)])
      .mockResolvedValueOnce([announcement(3, true), announcement(4, true)])
    const store = useAnnouncementStore()
    await store.fetchAnnouncements()
    await store.dismissPopup()
    login(2)
    await store.fetchAnnouncements()
    vi.advanceTimersByTime(300)
    expect(store.currentPopup?.id).toBe(3)
  })

  it('allows retry after a current request fails', async () => {
    vi.mocked(announcementsAPI.list).mockRejectedValueOnce(new Error('failure')).mockResolvedValueOnce([announcement(1)])
    const store = useAnnouncementStore()
    await store.fetchAnnouncements()
    await store.fetchAnnouncements()
    expect(announcementsAPI.list).toHaveBeenCalledTimes(2)
    expect(store.announcements).toHaveLength(1)
  })
})
