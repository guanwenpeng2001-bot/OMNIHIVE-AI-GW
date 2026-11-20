export default {
  consoleScopeBar: {
    scopeLabel: 'ACTIVE SCOPE',
    noVisibleProject: 'No visible project (—)',
    syncingScope: 'Syncing scope…',
    defaultPersonalScope: 'Default Personal Scope',
    refreshing: 'Refreshing',
    sync: 'Sync',
  },
  playgroundKeys: {
    emptyTitle: 'No Active Project Keys',
    emptyDescription:
      'Scoped Playground runs only accept active API keys belonging to the selected project. Personal account keys cannot be used.',
    scopeRequiredTitle: 'Project Scope Required',
    scopeRequiredDescription:
      'Select an active workspace and project, then create or choose a project key before running.',
    loadErrorTitle: 'Unable to Load Project Keys',
    loadErrorDescription:
      'Project credentials could not be loaded for this scope. Retry loading or verify your project permissions.',
    createProjectKeyAction: 'Create Project Key',
    selectScopeAction: 'Manage Workspaces',
    retryAction: 'Retry',
    sideBUnavailable: 'No active project key available for Side B.',
    runBlockedHint: 'Create an active project key before confirming a billable run.',
  },
  playgroundDiagnostics: {
    title: 'Diagnostics Inspector',
    expandDrawer: 'Show Inspector',
    collapseDrawer: 'Hide Inspector',
    togglePane: 'Raw Request / SSE',
    tabsAriaLabel: 'Raw request and SSE event inspector tabs',
    tabRawRequest: 'Raw Request JSON',
    tabSseFrames: 'Raw SSE Frames',
    rawRequestLabel: 'Submitted Request Payload (JSON)',
    rawRequestEmpty: 'No request submitted yet. Run a request to inspect the exact JSON payload.',
    sseFramesLabel: 'SSE Event Frames',
    sseFramesEmpty: 'No SSE event frames recorded yet.',
    rawEventsLabel: 'Upstream Raw Events (rawEvents)',
    rawEventsEmpty: 'No upstream raw events captured.',
    framePayloadLabel: 'Payload',
    copyPayload: 'Copy JSON',
    copyFrames: 'Copy Frames',
  },
}
