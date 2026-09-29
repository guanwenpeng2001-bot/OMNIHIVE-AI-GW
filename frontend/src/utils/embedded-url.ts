/**
 * Shared URL builder for iframe-embedded pages.
 * Used by CustomPageView for credential-free embeds with presentation settings.
 */
import DOMPurify from 'dompurify'

const EMBEDDED_THEME_QUERY_KEY = 'theme'
const EMBEDDED_LANG_QUERY_KEY = 'lang'
const EMBEDDED_UI_MODE_QUERY_KEY = 'ui_mode'
const EMBEDDED_UI_MODE_VALUE = 'embedded'
const EMBEDDED_SRC_HOST_QUERY_KEY = 'src_host'
const EMBEDDED_SRC_QUERY_KEY = 'src_url'

export function buildEmbeddedUrl(
  baseUrl: string,
  theme: 'light' | 'dark' = 'light',
  lang?: string,
): string {
  if (!baseUrl) return baseUrl
  try {
    const url = new URL(baseUrl)
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) return ''
    for (const key of ['user_id', 'token', 'access_token', 'refresh_token']) url.searchParams.delete(key)
    url.searchParams.set(EMBEDDED_THEME_QUERY_KEY, theme)
    if (lang) {
      url.searchParams.set(EMBEDDED_LANG_QUERY_KEY, lang)
    }
    url.searchParams.set(EMBEDDED_UI_MODE_QUERY_KEY, EMBEDDED_UI_MODE_VALUE)
    // Source tracking: let the embedded page know where it's being loaded from
    if (typeof window !== 'undefined') {
      url.searchParams.set(EMBEDDED_SRC_HOST_QUERY_KEY, window.location.origin)
      // Source URLs can contain OAuth codes or other credentials in query/fragment.
      url.searchParams.set(EMBEDDED_SRC_QUERY_KEY, window.location.origin)
    }
    return url.toString()
  } catch {
    return ''
  }
}

// Scripts run in an opaque origin; never combine with allow-same-origin.
export const embeddedSandbox = 'allow-scripts'

export function sanitizeEmbeddedHtml(html: string): string {
  const fragment = DOMPurify.sanitize(html, {
    ADD_TAGS: ['iframe'],
    ADD_ATTR: ['allowfullscreen'],
    FORBID_ATTR: ['srcdoc'],
    RETURN_DOM_FRAGMENT: true,
  })
  fragment.querySelectorAll('iframe').forEach((frame) => {
    frame.setAttribute('sandbox', embeddedSandbox)
    frame.setAttribute('referrerpolicy', 'no-referrer')
    frame.setAttribute('title', frame.getAttribute('title') || 'Embedded content')
  })
  const container = document.createElement('div')
  container.append(fragment)
  return container.innerHTML
}

export function detectTheme(): 'light' | 'dark' {
  if (typeof document === 'undefined') return 'light'
  return document.documentElement.classList.contains('dark') ? 'dark' : 'light'
}
