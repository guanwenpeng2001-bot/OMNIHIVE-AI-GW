// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { createServer, type IncomingHttpHeaders, type Server } from 'node:http'
import { execFile, spawnSync } from 'node:child_process'
import { promisify } from 'node:util'
import { mkdtemp, writeFile, rm } from 'node:fs/promises'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { existsSync } from 'node:fs'
import { parseModels, powershellChatExample, protocols, publicRoot, requestExample } from '@/components/intro/quickstart'
import { guideExamples } from '@/components/intro/helpExamples'
const run = promisify(execFile)
const hasPowerShell = spawnSync('pwsh', ['-NoProfile', '-NonInteractive', '-Command', 'exit 0']).status === 0
let server: Server
let root: string
let dir: string
const received: { url: string; headers: IncomingHttpHeaders; body: unknown }[] = []
beforeAll(async () => {
  dir = await mkdtemp(join(tmpdir(), 'omnihive-quickstart-'))
  server = createServer(async (req, res) => {
    const chunks: Buffer[] = []
    for await (const chunk of req) chunks.push(Buffer.from(chunk))
    const body = Buffer.concat(chunks).toString()
    received.push({ url: req.url!, headers: req.headers, body: req.headers['content-type']?.startsWith('application/json') ? JSON.parse(body) : body })
    res.setHeader('Content-Type', 'application/json')
    res.end('{"ok":true}')
  })
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve))
  root = `http://127.0.0.1:${(server.address() as { port: number }).port}/deployment/prefix`
})
afterAll(async () => {
  try {
    if (server?.listening) await new Promise<void>(resolve => server.close(() => resolve()))
  } finally {
    if (dir) await rm(dir, { recursive: true, force: true })
  }
})

describe('copyable quickstart examples against a local mock', () => {
  it('omits every guide example without a configured public address', () => {
    expect(guideExamples('')).toEqual({})
  })

  it.runIf(hasPowerShell)('executes the actual PowerShell quickstart with a quoted model against loopback only', async () => {
    const model = "model-'quote-$(not-executed)"
    const sample = powershellChatExample(root, model)
    const file = join(dir, 'quickstart.ps1')
    await writeFile(file, sample.code)
    await run('pwsh', ['-NoProfile', '-NonInteractive', '-File', file], { timeout: 15000 })
    const request = received.at(-1)!
    expect(request.url).toBe('/deployment/prefix/v1/chat/completions')
    expect(request.headers.authorization).toBe('Bearer sk-your-api-key')
    expect(request.body).toEqual(requestExample(root, model, 'chat/completions').body)
  })

  it('executes every formal curl request, multipart upload and resource read against loopback', async () => {
    const bash = process.platform === 'win32' && existsSync('C:/Program Files/Git/bin/bash.exe') ? 'C:/Program Files/Git/bin/bash.exe' : 'bash'
    const env = { ...process.env, OMNIHIVE_API_KEY: 'mock-key-only', NO_PROXY: '127.0.0.1', no_proxy: '127.0.0.1' }
    await writeFile(join(dir, 'reference.png'), 'fixture image bytes')
    await writeFile(join(dir, 'speech.wav'), 'fixture audio bytes')
    const examples = guideExamples(root)
    for (const [section, samples] of Object.entries(examples)) {
      if (section === 'clients') continue // Never launch installed clients or SDKs.
      for (const sample of samples.filter(item => item.code.includes('curl '))) {
        const file = join(dir, 'guide.sh')
        await writeFile(file, sample.code)
        const previous = received.length
        await run(bash, [file], { env, cwd: dir, timeout: 15000 })
        expect(received.length, sample.label).toBe(previous + 1)
        const request = received.at(-1)!
        expect(request.url, sample.label).toMatch(/^\/deployment\/prefix\/(v1|v1beta|backend-api)\//)
        const auth = request.headers.authorization || request.headers['x-api-key'] || request.headers['x-goog-api-key']
        expect(auth, sample.label).toMatch(/(mock-key-only|sk-your-api-key)$/)
        if (sample.code.includes('-F ')) {
          expect(request.headers['content-type']).toMatch(/^multipart\/form-data; boundary=/)
          expect(request.body).toContain('name="model"')
          expect(request.body).toContain('filename=')
        }
        if (request.url.endsWith('/rerank')) expect(request.body).toMatchObject({ query: 'coast', documents: expect.any(Array), top_n: 1 })
        if (request.url.endsWith('/images/generations/async')) expect(request.body).toMatchObject({ model: 'ASYNC_IMAGE_MODEL_ID_FROM_LIST' })
      }
    }
  }, 60000)

  it('parses the displayed OpenCode and Pi configurations and runs the reasoning JavaScript', async () => {
    const samples = guideExamples(root)
    for (const sample of samples.clients.filter(item => item.label.startsWith('JSON'))) {
      const parsed = JSON.parse(sample.code)
      expect(parsed).toBeTypeOf('object')
      expect(sample.code).toContain(`${root}/v1`)
      expect(sample.code).toContain('MODEL_ID_FROM_LIST')
    }
    const file = join(dir, 'reasoning.mjs')
    await writeFile(file, samples.reasoning[0].code)
    await run(process.execPath, [file], { env: { ...process.env, OMNIHIVE_API_KEY: 'mock-key-only', MODEL_REASONING_EFFORT: 'low' }, timeout: 15000 })
    expect(received.at(-1)?.body).toMatchObject({ reasoning: { effort: 'low' } })
  })
  it('preserves prefixes and rejects credentials, query secrets and non-HTTP URLs', () => {
    expect(publicRoot(`${root}/v1///`, root)).toBe(root)
    for (const value of ['https://user:secret@example.com', 'https://example.com?key=secret', 'file:///secret', 'https://example.com/#secret']) {
      expect(() => publicRoot(value, root)).toThrow()
    }
    expect(publicRoot('', `${root}/`)).toBe(root)
  })
  it.each(['', '/', '///', '/v1', '/v1///', '///v1///', '/v1beta', '/v1beta///', '///v1beta///'])(
    'normalizes public HTTPS bases ending in "%s" without losing the deployment prefix', (suffix) => {
      const gatewayRoot = 'https://gateway.example:8443/proxy/omnihive'
      expect(publicRoot(`  ${gatewayRoot}${suffix}  `, '')).toBe(gatewayRoot)
      expect(publicRoot('', `${gatewayRoot}${suffix}`)).toBe(gatewayRoot)
      expect(requestExample(publicRoot(`${gatewayRoot}${suffix}`, ''), 'gemini-test', 'gemini').url)
        .toBe(`${gatewayRoot}/v1beta/models/gemini-test:generateContent`)
    }
  )
  it('only strips a terminal API version, preserving version-like deployment names', () => {
    expect(publicRoot('https://gateway.example/v1beta/proxy/v1beta/', '')).toBe('https://gateway.example/v1beta/proxy')
    expect(publicRoot('https://gateway.example/proxy/v1beta-custom/', '')).toBe('https://gateway.example/proxy/v1beta-custom')
    expect(publicRoot('https://gateway.example/v1beta/', '')).toBe('https://gateway.example')
  })
  it('keeps unknown capability distinct from an explicitly empty endpoint list', () => {
    expect(parseModels({ data: [{ id: 'unknown' }, { id: 'empty', endpoints: [] }, { id: 'known', endpoints: ['/v1/responses'] }] }))
      .toEqual([{ id: 'unknown', endpoints: null }, { id: 'empty', endpoints: [] }, { id: 'known', endpoints: ['responses'] }])
    expect(() => parseModels({ error: 'unauthorized' })).toThrow()
  })
  it.each(['endpoints', 'supported_endpoints'])('recognizes backend and existing aliases in %s without hiding unknown models', (field) => {
    expect(parseModels({ data: [
      { id: 'google-backend', [field]: ['generate_content'] },
      { id: 'google-existing', [field]: ['generateContent', 'gemini'] },
      { id: 'existing-aliases', [field]: ['chat_completions', 'images', '/v1/responses', 'messages'] },
      { id: 'unknown-protocol', [field]: ['future_protocol'] },
      { id: 'unknown-capability' }
    ] })).toEqual([
      { id: 'google-backend', endpoints: ['gemini'] },
      { id: 'google-existing', endpoints: ['gemini', 'gemini'] },
      { id: 'existing-aliases', endpoints: ['chat/completions', 'images/generations', 'responses', 'messages'] },
      { id: 'unknown-protocol', endpoints: [] },
      { id: 'unknown-capability', endpoints: null }
    ])
  })
  for (const protocol of protocols) {
    it(`runs curl, Python and Node ${protocol} examples with environment-only auth`, async () => {
      const sample = requestExample(root, "model-'quote-$(not-executed)", protocol)
      const env = { ...process.env, OMNIHIVE_API_KEY: 'mock-key-only', NO_PROXY: '127.0.0.1', no_proxy: '127.0.0.1' }
      const bash = process.platform === 'win32' && existsSync('C:/Program Files/Git/bin/bash.exe') ? 'C:/Program Files/Git/bin/bash.exe' : 'bash'
      const commands = [
        { bin: bash, args: (file: string) => [file], ext: 'sh' },
        { bin: process.platform === 'win32' ? 'python' : 'python3', args: (file: string) => [file], ext: 'py' },
        { bin: process.execPath, args: (file: string) => [file], ext: 'mjs' }
      ]
      for (const [index, command] of commands.entries()) {
        const file = join(dir, `example.${command.ext}`)
        const code = sample.examples[index].code
        expect(code).not.toContain(env.OMNIHIVE_API_KEY)
        await writeFile(file, code)
        const { stdout } = await run(command.bin, command.args(file), { env, timeout: 15000 })
        expect(JSON.parse(stdout)).toEqual({ ok: true })
        const request = received.at(-1)!
        expect(request.url).toBe(new URL(sample.url).pathname)
        expect(request.url).not.toContain(env.OMNIHIVE_API_KEY)
        expect(request.body).toEqual(sample.body)
        expect(request.headers[sample.authHeader.toLowerCase()]).toBe((sample.authHeader === 'Authorization' ? 'Bearer ' : '') + env.OMNIHIVE_API_KEY)
        if (protocol === 'messages') expect(request.headers['anthropic-version']).toBe('2023-06-01')
      }
    }, 60000)
  }
})
