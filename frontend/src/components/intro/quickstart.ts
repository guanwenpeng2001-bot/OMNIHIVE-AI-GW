import { buildClientBaseUrl, sanitizeUrl } from '@/utils/url'

export type Protocol = 'chat/completions' | 'responses' | 'messages' | 'embeddings' | 'images/generations' | 'gemini'
export const protocols: Protocol[] = ['chat/completions', 'responses', 'messages', 'embeddings', 'images/generations', 'gemini']

/** Public gateway root, never discard a reverse-proxy deployment prefix. */
export function publicRoot(configured: string, fallback: string): string {
  const normalized = sanitizeUrl(configured.trim() || fallback)
  if (!normalized) throw new Error('Invalid public gateway URL')
  const url = new URL(normalized)
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash) {
    throw new Error('Invalid public gateway URL')
  }
  return buildClientBaseUrl(url.toString()).replace(/\/+$/, '')
}

export interface VisibleModel { id: string; endpoints: Protocol[] | null }
export function parseModels(payload: unknown): VisibleModel[] {
  if (!payload || typeof payload !== 'object' || !('data' in payload) || !Array.isArray(payload.data)) {
    throw new Error('Invalid model catalogue')
  }
  const aliases: Record<string, Protocol> = { chat_completions: 'chat/completions', images: 'images/generations', generateContent: 'gemini', generate_content: 'gemini' }
  return payload.data.flatMap((item: unknown) => {
    if (!item || typeof item !== 'object' || !('id' in item) || typeof item.id !== 'string' || !item.id.trim()) return []
    const raw = 'endpoints' in item ? item.endpoints : 'supported_endpoints' in item ? item.supported_endpoints : null
    const endpoints = Array.isArray(raw) ? raw.flatMap((value: unknown) => {
      if (typeof value !== 'string') return []
      const endpoint = value.replace(/^\/?v1\//, '')
      const mapped = aliases[endpoint] ?? endpoint
      return protocols.includes(mapped as Protocol) ? [mapped as Protocol] : []
    }) : null
    return [{ id: item.id, endpoints }]
  })
}

const shellQuote = (value: string) => "'" + value.replace(/'/g, "'\"'\"'") + "'"
export function requestExample(root: string, model: string, protocol: Protocol) {
  const url = protocol === 'gemini'
    ? `${root}/v1beta/models/${encodeURIComponent(model)}:generateContent`
    : `${root}/v1/${protocol}`
  const body = protocol === 'responses' ? { model, input: 'Hello' }
    : protocol === 'embeddings' ? { model, input: 'Hello' }
    : protocol === 'images/generations' ? { model, prompt: 'A small blue circle', n: 1 }
    : protocol === 'gemini' ? { contents: [{ parts: [{ text: 'Hello' }] }] }
    : { model, ...(protocol === 'messages' ? { max_tokens: 256 } : {}), messages: [{ role: 'user', content: 'Hello' }] }
  const authHeader = protocol === 'messages' ? 'x-api-key' : protocol === 'gemini' ? 'x-goog-api-key' : 'Authorization'
  const prefix = authHeader === 'Authorization' ? 'Bearer ' : ''
  const headers: Record<string, string> = { 'Content-Type': 'application/json', ...(protocol === 'messages' ? { 'anthropic-version': '2023-06-01' } : {}) }
  const json = JSON.stringify(body)
  return {
    url, body, authHeader,
    examples: [
      { label: 'curl · Bash', code: `# Set SUB2API_API_KEY securely in your environment first.\nexport SUB2API_REQUEST_URL=${shellQuote(url)}\ncurl --fail-with-body "$SUB2API_REQUEST_URL" \\\n  -H "${authHeader}: ${prefix}$SUB2API_API_KEY" \\\n${Object.entries(headers).map(([key, value]) => `  -H ${shellQuote(`${key}: ${value}`)} \\`).join('\n')}\n  --data-raw ${shellQuote(json)}` },
      { label: 'Python · standard library', code: `import json, os, urllib.request\n\nurl = ${JSON.stringify(url)}\nheaders = json.loads(${JSON.stringify(JSON.stringify(headers))})\nheaders[${JSON.stringify(authHeader)}] = ${JSON.stringify(prefix)} + os.environ["SUB2API_API_KEY"]\nbody = json.loads(${JSON.stringify(json)})\nrequest = urllib.request.Request(url, data=json.dumps(body).encode(), headers=headers, method="POST")\nwith urllib.request.urlopen(request) as response:\n    print(response.status)  # Transport status; billing receipt is separate.` },
      { label: 'Node · fetch', code: `const key = process.env.SUB2API_API_KEY;\nif (!key) throw new Error("Set SUB2API_API_KEY");\nconst response = await fetch(${JSON.stringify(url)}, {\n  method: "POST",\n  headers: { ...${JSON.stringify(headers)}, ${JSON.stringify(authHeader)}: ${JSON.stringify(prefix)} + key },\n  body: JSON.stringify(${json}),\n});\nif (!response.ok) throw new Error(\`HTTP \${response.status}\`);\nconsole.log(response.status); // Transport status; billing receipt is separate.` }
    ]
  }
}
