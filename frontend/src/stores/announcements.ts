import { defineStore } from 'pinia'
import { ref, computed, watch, onScopeDispose } from 'vue'
import { announcementsAPI } from '@/api'
import type { UserAnnouncement } from '@/types'
import { useAuthStore } from './auth'

const THROTTLE_MS = 20 * 60 * 1000 // 20 minutes

export const useAnnouncementStore = defineStore('announcements', () => {
  const authStore = useAuthStore()
  // State
  const announcements = ref<UserAnnouncement[]>([])
  const loading = ref(false)
  const lastFetchTime = ref(0)
  const popupQueue = ref<UserAnnouncement[]>([])
  const currentPopup = ref<UserAnnouncement | null>(null)

  // Session-scoped dedup set — not reactive, used as plain lookup only
  let shownPopupIds = new Set<number>()
  let revision = 0
  let fetchVersion = 0
  let popupTimer: ReturnType<typeof setTimeout> | undefined
  let pendingFetch: symbol | undefined
  const pendingLoads = new Set<symbol>()

  // Invalidate synchronously, including logout/relogin within the same tick.
  // Access-token rotation for the same authenticated user keeps the cache.
  watch(
    () => [authStore.user?.id, authStore.user?.role, authStore.isAuthenticated, authStore.sessionRevision],
    (identity, previous) => {
      if (identity.some((value, index) => value !== previous[index])) reset()
    },
    { flush: 'sync' }
  )

  onScopeDispose(reset)

  function startLoading() {
    const request = Symbol()
    pendingLoads.add(request)
    loading.value = true
    return request
  }

  function finishLoading(request: symbol) {
    if (pendingLoads.delete(request)) loading.value = pendingLoads.size > 0
  }

  // Getters
  const unreadCount = computed(() =>
    announcements.value.filter((a) => !a.read_at).length
  )

  // Actions
  async function fetchAnnouncements(force = false) {
    if (!authStore.isAuthenticated) return
    const now = Date.now()
    if (!force && lastFetchTime.value > 0 && now - lastFetchTime.value < THROTTLE_MS) {
      return
    }

    // Set immediately to prevent concurrent duplicate requests
    lastFetchTime.value = now
    const requestRevision = revision
    const requestVersion = ++fetchVersion
    const isCurrent = () => requestRevision === revision && requestVersion === fetchVersion
    if (pendingFetch) pendingLoads.delete(pendingFetch)
    const request = startLoading()
    pendingFetch = request

    try {
      const all = await announcementsAPI.list(false)
      if (!isCurrent()) return
      // A list started before a successful read must not make that item unread.
      const readTimes = new Map(announcements.value.map((a) => [a.id, a.read_at]))
      announcements.value = all.slice(0, 20).map((a) => ({
        ...a,
        read_at: a.read_at || readTimes.get(a.id)
      }))
      enqueueNewPopups()
    } catch (err: any) {
      if (!isCurrent()) return
      // Revert throttle timestamp on failure so retry is allowed
      lastFetchTime.value = 0
      console.error('Failed to fetch announcements:', err)
    } finally {
      if (pendingFetch === request) pendingFetch = undefined
      finishLoading(request)
    }
  }

  function enqueueNewPopups() {
    const newPopups = announcements.value.filter(
      (a) => a.notify_mode === 'popup' && !a.read_at && !shownPopupIds.has(a.id)
    )
    if (newPopups.length === 0) return

    for (const p of newPopups) {
      if (!popupQueue.value.some((q) => q.id === p.id)) {
        popupQueue.value.push(p)
      }
    }

    if (!currentPopup.value) {
      showNextPopup()
    }
  }

  function showNextPopup() {
    if (popupQueue.value.length === 0) {
      currentPopup.value = null
      return
    }
    currentPopup.value = popupQueue.value.shift()!
    shownPopupIds.add(currentPopup.value.id)
  }

  async function dismissPopup() {
    if (!currentPopup.value) return
    const id = currentPopup.value.id
    currentPopup.value = null

    // Mark as read (fire-and-forget, UI already updated)
    markAsRead(id)

    // Show next popup after a short delay
    if (popupQueue.value.length > 0) {
      const popupRevision = revision
      clearTimeout(popupTimer)
      popupTimer = setTimeout(() => {
        if (popupRevision === revision) showNextPopup()
      }, 300)
    }
  }

  async function markAsRead(id: number) {
    if (!authStore.isAuthenticated) return
    const requestRevision = revision
    try {
      await announcementsAPI.markRead(id)
      if (requestRevision !== revision) return
      const ann = announcements.value.find((a) => a.id === id)
      if (ann) {
        ann.read_at = new Date().toISOString()
      }
    } catch (err: any) {
      if (requestRevision !== revision) return
      console.error('Failed to mark announcement as read:', err)
    }
  }

  async function markAllAsRead() {
    if (!authStore.isAuthenticated) return
    const unread = announcements.value.filter((a) => !a.read_at)
    if (unread.length === 0) return
    const requestRevision = revision
    const request = startLoading()

    try {
      const results = await Promise.allSettled(unread.map(async (item) => {
        await announcementsAPI.markRead(item.id)
        if (requestRevision !== revision) return
        const current = announcements.value.find((announcement) => announcement.id === item.id)
        if (current && !current.read_at) current.read_at = new Date().toISOString()
      }))
      if (requestRevision !== revision) return
      const failure = results.find((result) => result.status === 'rejected')
      if (failure?.status === 'rejected') throw failure.reason
    } catch (err: any) {
      if (requestRevision !== revision) return
      console.error('Failed to mark all as read:', err)
      throw err
    } finally {
      finishLoading(request)
    }
  }

  function reset() {
    revision += 1
    fetchVersion += 1
    pendingLoads.clear()
    pendingFetch = undefined
    clearTimeout(popupTimer)
    popupTimer = undefined
    announcements.value = []
    lastFetchTime.value = 0
    shownPopupIds = new Set()
    popupQueue.value = []
    currentPopup.value = null
    loading.value = false
  }

  return {
    // State
    announcements,
    loading,
    currentPopup,
    // Getters
    unreadCount,
    // Actions
    fetchAnnouncements,
    dismissPopup,
    markAsRead,
    markAllAsRead,
    reset,
  }
})
