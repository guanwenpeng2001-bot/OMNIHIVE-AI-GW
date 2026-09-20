import { afterEach, expect, it, vi } from 'vitest'
import type { Plugin } from 'vite'
import viteConfig from '../../vite.config'

vi.mock('vite', () => ({ defineConfig: (config: unknown) => config, loadEnv: () => ({}) }))
vi.mock('@vitejs/plugin-vue', () => ({ default: () => ({ name: 'vue' }) }))
vi.mock('vite-plugin-checker', () => ({ default: () => ({ name: 'checker' }) }))

afterEach(() => vi.unstubAllGlobals())

it('serializes public settings without script termination or replacement-string expansion', async () => {
  const settings = {
    site_name: '$& $\' </script><script>window.compromised=true</script>',
    home_content: '<!-- </ScRiPt> & > \u2028 \u2029',
  }
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ code: 0, data: settings }) }))
  if (typeof viteConfig !== 'function') throw new Error('Expected Vite config factory')
  const config = await viteConfig({ command: 'serve', mode: 'test' })
  const plugin = config.plugins?.find(candidate => (candidate as Plugin)?.name === 'inject-public-settings') as Plugin
  const hook = plugin.transformIndexHtml
  if (!hook || typeof hook === 'function' || !('handler' in hook) || !hook.handler) throw new Error('Missing settings injection')
  const output = await hook.handler('<html><head><title>Default</title></head><body></body></html>', { path: '/', filename: 'index.html' })
  expect(typeof output).toBe('string')
  const parsed = new DOMParser().parseFromString(output as string, 'text/html')
  expect(parsed.querySelectorAll('script')).toHaveLength(1)
  expect(parsed.title).toBe(`${settings.site_name} - AI API Gateway`)
  const content = parsed.querySelector('script')!.textContent!
  expect(content).not.toContain('</script>')
  expect(content).not.toContain('\u2028')
  expect(JSON.parse(content.slice('window.__APP_CONFIG__='.length, -1))).toEqual(settings)
})
