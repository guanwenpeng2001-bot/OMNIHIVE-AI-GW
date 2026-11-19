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

export const shellQuote = (value: string) => "'" + value.replace(/'/g, "'\"'\"'") + "'"
export function requestExample(root: string, model: string, protocol: Protocol) {
  const path = protocol === 'gemini'
    ? `/v1beta/models/${encodeURIComponent(model)}:generateContent`
    : `/v1/${protocol}`
  const body = protocol === 'responses' ? { model, input: 'Hello' }
    : protocol === 'embeddings' ? { model, input: 'Hello' }
    : protocol === 'images/generations' ? { model, prompt: 'A small blue circle', n: 1 }
    : protocol === 'gemini' ? { contents: [{ parts: [{ text: 'Hello' }] }] }
    : { model, ...(protocol === 'messages' ? { max_tokens: 256 } : {}), messages: [{ role: 'user', content: 'Hello' }] }
  const authHeader = protocol === 'messages' ? 'x-api-key' : protocol === 'gemini' ? 'x-goog-api-key' : 'Authorization'
  const headers: Record<string, string> = { 'Content-Type': 'application/json', ...(protocol === 'messages' ? { 'anthropic-version': '2023-06-01' } : {}) }
  return jsonRequestExample(root, path, body, authHeader, headers)
}

/** Generate language variants from one request body and deployment-relative path. */
export function jsonRequestExample(root: string, path: string, body: Record<string, unknown>, authHeader = 'Authorization', headers: Record<string, string> = { 'Content-Type': 'application/json' }) {
  const url = `${root}${path}`
  const prefix = authHeader === 'Authorization' ? 'Bearer ' : ''
  const json = JSON.stringify(body)
  return {
    url, body, authHeader,
    examples: [
      { label: 'curl · Bash', code: `# Set OMNIHIVE_API_KEY securely in your environment first.\nexport OMNIHIVE_REQUEST_URL=${shellQuote(url)}\ncurl${body.stream === true ? ' -N' : ''} --fail-with-body "$OMNIHIVE_REQUEST_URL" \\\n  -H "${authHeader}: ${prefix}$OMNIHIVE_API_KEY" \\\n${Object.entries(headers).map(([key, value]) => `  -H ${shellQuote(`${key}: ${value}`)} \\`).join('\n')}\n  --data-raw ${shellQuote(json)}` },
      { label: 'Python · standard library', code: `import json, os, urllib.request, urllib.error\n\nurl = ${JSON.stringify(url)}\nheaders = json.loads(${JSON.stringify(JSON.stringify(headers))})\nheaders[${JSON.stringify(authHeader)}] = ${JSON.stringify(prefix)} + os.environ["OMNIHIVE_API_KEY"]\nbody = json.loads(${JSON.stringify(json)})\nrequest = urllib.request.Request(url, data=json.dumps(body).encode(), headers=headers, method="POST")\ntry:\n    with urllib.request.urlopen(request) as response:\n        print(json.dumps(json.load(response), ensure_ascii=False, indent=2))\nexcept urllib.error.HTTPError as error:\n    print(error.read().decode())\n    raise SystemExit(f"HTTP {error.code}")` },
      { label: 'Node · fetch', code: `const key = process.env.OMNIHIVE_API_KEY;\nif (!key) throw new Error("Set OMNIHIVE_API_KEY");\nconst response = await fetch(${JSON.stringify(url)}, {\n  method: "POST",\n  headers: { ...${JSON.stringify(headers)}, ${JSON.stringify(authHeader)}: ${JSON.stringify(prefix)} + key },\n  body: JSON.stringify(${json}),\n});\nconst result = await response.json();\nif (!response.ok) throw new Error(\`HTTP \${response.status}: \${JSON.stringify(result)}\`);\nconsole.log(JSON.stringify(result, null, 2)); // Inspect protocol completion and usage fields.` }
    ]
  }
}

/** PowerShell uses literal strings for addresses/payloads, never interpolated model IDs. */
export function powershellChatExample(root: string, model: string) {
  const sample = requestExample(root, model, 'chat/completions')
  const literal = (value: string) => "'" + value.replace(/'/g, "''") + "'"
  return {
    label: 'PowerShell · Chat Completions',
    code: `$env:OMNIHIVE_BASE_URL = ${literal(`${root}/v1`)}
$env:OMNIHIVE_API_KEY = 'sk-your-api-key' # Replace with your complete key.
$headers = @{ Authorization = "Bearer $env:OMNIHIVE_API_KEY" }
Invoke-RestMethod -Method Get -Uri "$env:OMNIHIVE_BASE_URL/models" -Headers $headers
# Replace the model below with an ID from this key's catalog.
$body = ${literal(JSON.stringify(sample.body))}
Invoke-RestMethod -Method Post -Uri "$env:OMNIHIVE_BASE_URL/chat/completions" -Headers $headers -ContentType 'application/json; charset=utf-8' -Body ([System.Text.Encoding]::UTF8.GetBytes($body))`
  }
}

/** Resource reads and uploads use the same root and environment-only credential. */
export function curlResourceExample(root: string, path: string, label: string, fields: string[] = [], output?: string) {
  return {
    label,
    code: `# Set OMNIHIVE_API_KEY securely in your environment first.\ncurl --fail-with-body ${shellQuote(`${root}${path}`)} \\\n  -H "Authorization: Bearer $OMNIHIVE_API_KEY"${fields.map(field => ` \\\n  -F ${shellQuote(field)}`).join('')}${output ? ` \\\n  --output ${shellQuote(output)}` : ''}`
  }
}
