// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { createServer, type IncomingHttpHeaders, type Server } from 'node:http'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import { mkdtemp, writeFile, rm } from 'node:fs/promises'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { existsSync } from 'node:fs'
import { parseModels, protocols, publicRoot, requestExample } from '@/components/intro/quickstart'
const run = promisify(execFile)
let server: Server
let root: string
let dir: string
const received: { url: string; headers: IncomingHttpHeaders; body: unknown }[] = []
beforeAll(async () => {
  dir = await mkdtemp(join(tmpdir(), 'sub2api-quickstart-'))
  server = createServer(async (req, res) => {
    const chunks: Buffer[] = []
    for await (const chunk of req) chunks.push(Buffer.from(chunk))
    received.push({ url: req.url!, headers: req.headers, body: JSON.parse(Buffer.concat(chunks).toString()) })
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
  it('preserves prefixes and rejects credentials, query secrets and non-HTTP URLs', () => {
    expect(publicRoot(`${root}/v1///`, root)).toBe(root)
    for (const value of ['https://user:secret@example.com', 'https://example.com?key=secret', 'file:///secret', 'https://example.com/#secret']) {
      expect(() => publicRoot(value, root)).toThrow()
    }
    expect(publicRoot('', `${root}/`)).toBe(root)
  })
  it('keeps unknown capability distinct from an explicitly empty endpoint list', () => {
    expect(parseModels({ data: [{ id: 'unknown' }, { id: 'empty', endpoints: [] }, { id: 'known', endpoints: ['/v1/responses'] }] }))
      .toEqual([{ id: 'unknown', endpoints: null }, { id: 'empty', endpoints: [] }, { id: 'known', endpoints: ['responses'] }])
    expect(() => parseModels({ error: 'unauthorized' })).toThrow()
  })
  for (const protocol of protocols) {
    it(`runs curl, Python and Node ${protocol} examples with environment-only auth`, async () => {
      const sample = requestExample(root, "model-'quote-$(not-executed)", protocol)
      const env = { ...process.env, SUB2API_API_KEY: 'mock-key-only', NO_PROXY: '127.0.0.1', no_proxy: '127.0.0.1' }
      const bash = process.platform === 'win32' && existsSync('C:/Program Files/Git/bin/bash.exe') ? 'C:/Program Files/Git/bin/bash.exe' : 'bash'
      const commands = [
        { bin: bash, args: (file: string) => [file], ext: 'sh' },
        { bin: process.platform === 'win32' ? 'python' : 'python3', args: (file: string) => [file], ext: 'py' },
        { bin: process.execPath, args: (file: string) => [file], ext: 'mjs' }
      ]
      for (const [index, command] of commands.entries()) {
        const file = join(dir, `example.${command.ext}`)
        const code = sample.examples[index].code
        expect(code).not.toContain(env.SUB2API_API_KEY)
        await writeFile(file, code)
        await run(command.bin, command.args(file), { env, timeout: 15000 })
        const request = received.at(-1)!
        expect(request.url).toBe(new URL(sample.url).pathname)
        expect(request.url).not.toContain(env.SUB2API_API_KEY)
        expect(request.body).toEqual(sample.body)
        expect(request.headers[sample.authHeader.toLowerCase()]).toBe((sample.authHeader === 'Authorization' ? 'Bearer ' : '') + env.SUB2API_API_KEY)
        if (protocol === 'messages') expect(request.headers['anthropic-version']).toBe('2023-06-01')
      }
    }, 60000)
  }
})
