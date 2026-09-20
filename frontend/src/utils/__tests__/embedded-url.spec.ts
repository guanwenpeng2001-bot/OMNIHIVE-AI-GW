import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { buildEmbeddedUrl, detectTheme, sanitizeEmbeddedHtml } from '../embedded-url'

describe('embedded-url', () => {
  const originalLocation = window.location

  beforeEach(() => {
    Object.defineProperty(window, 'location', {
      value: {
        origin: 'https://app.example.com',
        href: 'https://app.example.com/callback?code=secret&token=jwt#refresh_token=secret',
      },
      writable: true,
      configurable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', {
      value: originalLocation,
      writable: true,
      configurable: true,
    })
    document.documentElement.classList.remove('dark')
    vi.restoreAllMocks()
  })

  it('adds embedded query parameters including locale and source context', () => {
    const result = buildEmbeddedUrl(
      'https://pay.example.com/checkout?plan=pro',
      'dark',
      'zh-CN',
    )

    const url = new URL(result)
    expect(url.searchParams.get('plan')).toBe('pro')
    expect(url.searchParams.has('user_id')).toBe(false)
    expect(url.searchParams.has('token')).toBe(false)
    expect(url.searchParams.get('theme')).toBe('dark')
    expect(url.searchParams.get('lang')).toBe('zh-CN')
    expect(url.searchParams.get('ui_mode')).toBe('embedded')
    expect(url.searchParams.get('src_host')).toBe('https://app.example.com')
    expect(url.searchParams.get('src_url')).toBe('https://app.example.com')
    expect(result).not.toContain('secret')
  })

  it('omits optional params when they are empty', () => {
    const result = buildEmbeddedUrl('https://pay.example.com/checkout', 'light')

    const url = new URL(result)
    expect(url.searchParams.get('theme')).toBe('light')
    expect(url.searchParams.get('ui_mode')).toBe('embedded')
    expect(url.searchParams.has('user_id')).toBe(false)
    expect(url.searchParams.has('token')).toBe(false)
    expect(url.searchParams.has('lang')).toBe(false)
  })

  it.each(['not a url', 'javascript:alert(1)', 'https://user:secret@example.com'])('rejects invalid or credential-bearing URL %s', (url) => {
    expect(buildEmbeddedUrl(url)).toBe('')
  })

  it('removes legacy session parameters already in configured URLs', () => {
    const url = new URL(buildEmbeddedUrl('https://example.com/?token=jwt&user_id=7&access_token=jwt&refresh_token=refresh&plan=pro'))
    for (const key of ['token', 'user_id', 'access_token', 'refresh_token']) expect(url.searchParams.has(key)).toBe(false)
    expect(url.searchParams.get('plan')).toBe('pro')
  })

  it('sanitizes active HTML and forces nested frames into opaque origins', () => {
    const html = sanitizeEmbeddedHtml('<script>alert(1)</script><img src=x onerror="alert(1)"><iframe src="https://example.com" srcdoc="secret" sandbox="allow-scripts allow-same-origin"></iframe><a href="javascript:alert(1)">bad</a>')
    const container = document.createElement('div')
    container.innerHTML = html
    expect(container.querySelector('script')).toBeNull()
    expect(container.querySelector('img')?.hasAttribute('onerror')).toBe(false)
    expect(container.querySelector('a')?.hasAttribute('href')).toBe(false)
    const frame = container.querySelector('iframe')!
    expect(frame.getAttribute('sandbox')).toBe('allow-scripts')
    expect(frame.hasAttribute('srcdoc')).toBe(false)
    expect(frame.getAttribute('referrerpolicy')).toBe('no-referrer')
    expect(frame.getAttribute('title')).toBeTruthy()
  })

  it('detects dark mode from document root class', () => {
    document.documentElement.classList.add('dark')
    expect(detectTheme()).toBe('dark')
  })
})
