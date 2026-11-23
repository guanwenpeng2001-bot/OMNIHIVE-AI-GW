export default {
  eyebrow: 'CONFIGURATION AUTHORITY',
  title: 'Configuration Diagnostics',
  description:
    'Inspect effective values, resolution sources, deployment hard gates vs. database soft switches, seed provenance, and resettable overrides. Secret plaintext is never displayed.',
  syncBadges: {
    loading: 'Loading…',
    synced: 'Synced',
    notSynced: 'Not Synced',
    stale: 'Sync Failed',
    issuesCount: '{count} Issues'
  },
  actions: {
    refresh: 'Refresh',
    clearFilters: 'Clear Filters',
    loadMore: 'Load More',
    loadingMore: 'Loading…',
    resetToBaseline: 'Reset to Baseline',
    confirmReset: 'Confirm Reset',
    cancel: 'Cancel',
    retry: 'Retry'
  },
  meta: {
    configFileLabel: 'CONFIG_FILE',
    configFileUnset: 'Not set (environment & defaults)',
    generatedAtLabel: 'Snapshot',
    showingCount: '{count} entries loaded',
    moreAvailable: 'More entries available'
  },
  filters: {
    searchPlaceholder: 'Search setting key or group…',
    searchAriaLabel: 'Search configuration key or group',
    groupLabel: 'Group',
    allGroups: 'All Groups',
    sourceLabel: 'Effective Source',
    allSources: 'All Sources',
    stateLabel: 'Health / Gate State',
    allStates: 'All States'
  },
  groups: {
    system: 'System',
    security: 'Security & Auth',
    onboarding: 'Onboarding & User Grants',
    gateway: 'Gateway Runtime',
    email: 'Email & SMTP',
    plaza: 'Model Plaza',
    ops: 'Operations & Telemetry',
    catalog: 'Model Catalog',
    logging: 'Logging',
    image_storage: 'Image Storage',
    backup: 'Backup & Recovery',
    linuxdo_connect: 'LinuxDo Connect',
    dingtalk_connect: 'DingTalk Connect',
    wechat_connect: 'WeChat Connect',
    oidc_connect: 'OIDC Connect',
    github_oauth: 'GitHub OAuth',
    google_oauth: 'Google OAuth'
  },
  sources: {
    database: 'Database',
    environment: 'Environment',
    yaml: 'YAML File',
    default: 'Registry Default',
    derived: 'Derived Gate',
    artifact: 'Versioned Artifact'
  },
  authorities: {
    deployment: 'Deployment Static',
    database_override: 'Database Override',
    database_only: 'Database Only',
    versioned_artifact: 'Versioned Artifact'
  },
  states: {
    effective: 'Effective',
    invalid: 'Invalid Config',
    unavailable: 'Storage Unavailable',
    blocked_by_hard_gate: 'Blocked by Hard Gate'
  },
  seedSources: {
    install_seed: 'Install Seed',
    environment: 'Environment Seed',
    yaml: 'YAML Seed',
    default: 'Registry Default',
    admin: 'Admin Saved',
    unknown: 'Pre-v0.3.4 / Unknown'
  },
  columns: {
    setting: 'Setting Key & Authority',
    effectiveValue: 'Effective Value',
    sourceAndChain: 'Active Source & Fallback Chain',
    stateAndGate: 'Gate / Health Explanation',
    actions: 'Override Actions'
  },
  values: {
    secretConfigured: 'Configured (secret redacted)',
    secretNotConfigured: 'Not configured (secret redacted)',
    secretUnknown: 'Status unavailable (secret redacted)',
    emptyString: '"" (empty string)',
    unavailableValue: 'Unavailable (fail-closed)',
    seedProvenanceValue: 'Initial seed: {source}',
    restartRequired: 'Restart required',
    overrideFrozenHint:
      'Database override active — environment and YAML changes remain shadowed until reset.'
  },
  seed: {
    badge: 'Initial Seed: {source}',
    newUsersOnlyHint:
      'Active value is read from the database; deployment seed ran on initial install only, and changes apply to subsequent new users only.',
    provenanceRowHint:
      'Migration provenance record only — indicates initial seed origin, not the active runtime value.',
    plazaSeedHint:
      'Database-only authority for Model Plaza (fails closed when missing or unreadable; new installs seed true, upgrades preserve existing state).'
  },
  upgradeNotes: {
    catalogRefreshOptIn:
      'v0.3.4 deployment default is false (explicit opt-in required for Model Catalog background refresh).',
    logFormatJson:
      'v0.3.4 deployment default is json; runtime Ops log settings do not override log.format.'
  },
  gates: {
    guideTitle: 'Hard Deployment Gates vs. Database Soft Switches',
    guideBody:
      'Deployment hard gates (environment/YAML such as webauthn.enabled, ops.enabled, and release turnstile.required) bound runtime behavior. A disabled hard gate blocks database soft switches so stale database true values cannot re-enable the feature; an active release hard requirement with incomplete soft credentials fails closed.',
    blockedByKey: 'Blocked by hard gate: {blockedBy}',
    turnstileRequiredIncomplete:
      'Release hard requirement (turnstile.required) is active, but the database Turnstile switch or secret key is incomplete — sign-in verification fails closed.',
    passkeyGateHint:
      'Requires both webauthn.enabled (deployment hard gate + RP config) AND passkey_enabled (database soft switch).',
    opsGateHint:
      'Requires both ops.enabled (deployment hard gate) AND ops_monitoring_enabled (database soft switch).',
    healthyHint: 'Resolved cleanly from authoritative source.',
    invalidHint:
      'Stored or deployment value failed validation ({code}); runtime fails closed instead of assuming a healthy default.',
    unavailableHint:
      'Backing configuration store could not be read ({code}); runtime fails closed instead of substituting defaults.'
  },
  alerts: {
    unhealthyBannerTitle: '{count} configuration validation or storage errors detected',
    unhealthyBannerBody:
      'Invalid or unavailable settings are reported explicitly and never masked as healthy defaults. Inspect the highlighted entries below.',
    forbiddenTitle: 'Administrator Permission Required (403)',
    loadFailedTitle: 'Failed to Load Effective Configuration',
    staleTitle: 'Refresh Failed — Displayed Snapshot May Be Stale',
    emptyTitle: 'No Registered Settings Returned',
    filteredEmptyTitle: 'No Settings Match Current Filters',
    filteredEmptyDescription:
      'Clear search or filter criteria to inspect all registered configuration keys.'
  },
  reset: {
    notResettableDeployment: 'Deployment static',
    notResettableDbOnly: 'Database only',
    notResettableDerived: 'Derived gate',
    notResettableArtifact: 'Versioned artifact',
    modalTitle: 'Reset Database Override to Deployment Baseline',
    modalDescriptionSingle:
      'Delete the database override for "{key}" and immediately re-resolve its effective value from deployment configuration (environment → YAML → registry default).',
    modalDescriptionGroup:
      '"{key}" belongs to the "{group}" atomic override group. Confirming reset deletes all database overrides in "{group}" in one transaction and re-resolves every field from deployment configuration.',
    groupScopeBadge: 'Atomic group reset: {group}',
    previewHeading: 'Post-reset baseline preview (secrets are never displayed)',
    currentLabel: 'Current ({source})',
    baselineLabel: 'Restored Baseline',
    secretBaselineRedacted:
      'Redacted — secret baseline is resolved server-side without displaying plaintext',
    baselineUnavailable: 'Resolved server-side from deployment baseline',
    restartNotice:
      'One or more affected keys require a service restart for static listeners to apply deployment changes.',
    stepUpNotice:
      'Credential or provider override changes may prompt for two-factor (TOTP) step-up verification.',
    failedTitle: 'Reset Failed — Override Was Not Cleared',
    unknownOutcomeTitle: 'Reset Outcome Uncertain — Refresh to Verify',
    successToastTitle: 'Override Reset Complete',
    successToastBody: 'Updated {count} effective configuration entries.'
  },
  workbench: {
    loadingBadge: 'Loading…',
    notSyncedBadge: 'Not Synced',
    syncFailedBadge: 'Sync Failed',
    refreshFailedTitle: 'Refresh Failed — Showing Last Loaded Snapshot',
    saveFailedTitle: 'Failed to Save Section'
  }
}
