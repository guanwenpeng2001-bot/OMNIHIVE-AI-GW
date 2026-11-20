import { apiClient } from './client'
import { extractApiErrorStatus } from '@/utils/apiError'

// Keep only failed/in-flight logical submissions; successful creates start a new operation.
// Request bodies stay in memory and are never written to browser storage.
const pendingCreates = new Map<string, string>()

export async function postIdempotentCreate<Result>(path: string, body: unknown): Promise<Result> {
  const submission = JSON.stringify([path, body])
  let key = pendingCreates.get(submission)
  if (!key) {
    key = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
    pendingCreates.set(submission, key)
  }
  try {
    const { data } = await apiClient.post<Result>(path, body, { headers: { 'Idempotency-Key': key } })
    if (pendingCreates.get(submission) === key) pendingCreates.delete(submission)
    return data
  } catch (error) {
    const status = extractApiErrorStatus(error)
    if (status !== undefined && status >= 400 && status < 500 && pendingCreates.get(submission) === key) {
      pendingCreates.delete(submission)
    }
    throw error
  }
}
