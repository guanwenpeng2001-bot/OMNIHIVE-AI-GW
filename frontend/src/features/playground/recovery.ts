// Recovery metadata contains no prompt, response, credential or payer secret.
export interface PlaygroundRecovery {
  principalId: string
  workspaceId: string
  projectId: string
  idempotencyKey: string
  runId: string | null
  slot: string
  savedAt: number
}
const storageKey = 'omnihive.playground.recovery.v1'
let memory: PlaygroundRecovery[] = []
function read(): PlaygroundRecovery[] {
  try {
    const parsed: unknown = JSON.parse(sessionStorage.getItem(storageKey) ?? '[]')
    if (Array.isArray(parsed)) memory = parsed.filter(item => item && typeof item.principalId === 'string' && typeof item.workspaceId === 'string' && typeof item.projectId === 'string' && typeof item.idempotencyKey === 'string' && item.idempotencyKey.length <= 200)
  } catch { /* The in-memory journal still survives page component remounts. */ }
  return memory
}
function write(items: PlaygroundRecovery[]) {
  memory = items
  try { sessionStorage.setItem(storageKey, JSON.stringify(items)) } catch { /* Keep the same-tab memory journal. */ }
}
export function rememberRecovery(item: PlaygroundRecovery) {
  if (!item.principalId || !item.workspaceId || !item.projectId || !item.idempotencyKey) return
  write([...read().filter(existing => existing.principalId !== item.principalId || existing.idempotencyKey !== item.idempotencyKey), item])
}
export function forgetRecovery(principalId: string, idempotencyKey: string) {
  write(read().filter(item => item.principalId !== principalId || item.idempotencyKey !== idempotencyKey))
}
export function listRecoveries(principalId: string, workspaceId: string, projectId: string): PlaygroundRecovery[] {
  return read().filter(item => item.principalId === principalId && item.workspaceId === workspaceId && item.projectId === projectId).sort((a, b) => b.savedAt - a.savedAt)
}
