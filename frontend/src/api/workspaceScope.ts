const delegatedResources = new Set([
  'model-access-groups', 'accounts', 'groups', 'channels', 'proxies', 'proxy-groups', 'api-keys', 'channel-monitors',
  'channel-monitor-templates', 'channel-monitor-v2', 'scheduled-test-plans',
  'platform-model-presets', 'openai', 'gemini', 'antigravity', 'grok', 'cn-providers',
])

export function workspaceIDFromPath(pathname: string): string | undefined {
  return /^\/workspaces\/([1-9]\d*)(?:\/|$)/.exec(pathname)?.[1]
}

/** Bind existing resource API clients to the workspace named in the page URL. */
export function workspaceRequestURL(url: string | undefined, pathname: string): string | undefined {
  const workspaceID = workspaceIDFromPath(pathname)
  if (!workspaceID || !url) return url
  const adminResource = /^\/admin\/([^/?]+)/.exec(url)?.[1]
  if (adminResource && delegatedResources.has(adminResource)) {
    return `/workspaces/${workspaceID}${url.slice('/admin'.length)}`
  }
  if (/^\/(?:model-access-groups|keys|channel-monitor-v2)(?:\/|\?|$)/.test(url) || /^\/groups\/(?:available|rates)(?:\?|$)/.test(url)) {
    return `/workspaces/${workspaceID}${url}`
  }
  return url
}
