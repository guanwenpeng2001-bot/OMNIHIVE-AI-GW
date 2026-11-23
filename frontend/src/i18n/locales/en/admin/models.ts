export default {
  models: {
    title: 'Model Catalog',
    subtitle:
      'Per-model inspection workbench for the 4-stage funnel (1. Platform Candidates -> 2. Native Discovered -> 3. Account Effective -> 4. Group Visible) and 4 price layers (Reference Price / Sale Rate / Internal Cost / Settlement Amount).',
    actions: {
      openVerificationWorkbench: 'Verification Workbench',
      exportUnpricedCsv: 'Export Unpriced CSV',
      batchActions: 'Batch Actions ({count})',
      refreshCatalog: 'Refresh Snapshot',
      loadMoreModels: 'Load More Models',
      loadMoreAccounts: 'Load More Discovered Accounts',
      loadMoreOffers: 'Load More Route Offers',
      clearFilters: 'Clear Filters',
      inspectDetail: 'Inspect Detail'
    },
    scope: {
      workspaceLabel: 'Workspace ID',
      projectLabel: 'Project ID (empty for all projects)',
      allProjects: 'All Projects (Workspace Overview)',
      catalogVersion: 'Catalog Version',
      asOf: 'Snapshot As Of',
      funnelLegend: '1. Platform Candidates -> 2. Native Discovered -> 3. Account Effective -> 4. Group Visible',
      priceLayerLegend: '1. Reference Price (Non-settlement) -> 2. Sale Rate -> 3. Internal Cost -> 4. Settlement Amount'
    },
    quickFilters: {
      all: 'All Models',
      routable_but_unpriced: 'Routable but Unpriced (P0)',
      discovered_not_enabled: 'Discovered Not Enabled',
      listed_unrouted: 'Listed Unrouted',
      group_price_conflict: 'Group Price Conflict',
      pi_only: 'Community Catalog (pi) Only',
      stale: 'Stale Evidence'
    },
    filters: {
      searchPlaceholder: 'Search canonical ID, public model name, or alias…',
      providerPlaceholder: 'All Providers',
      sourceAll: 'All Sources',
      availabilityAll: 'All Availability',
      billingAll: 'All Price Layers',
      publicationAll: 'All Publication States'
    },
    sources: {
      builtin_reviewed: 'Approved Registry',
      platform_preset: 'Platform Candidates',
      builtin_supplement: 'Custom Supplement',
      pi_ai_reference: 'Community Catalog (pi)',
      account_discovery: 'Native Discovered',
      account_declaration: 'Connection Declared',
      external_reference: 'External Reference',
      heuristic: 'Heuristic Estimate',
      unknown: 'Unknown Source'
    },
    availability: {
      live: 'Live (Routable)',
      listed_unrouted: 'Listed · Unrouted',
      catalog_only: 'Catalog Only',
      requires_scope: 'Requires Project Scope',
      unavailable: 'Unavailable',
      unknown: 'Unknown State'
    },
    publication: {
      published: 'Group Visible',
      unpublished: 'Unpublished',
      unknown: 'Publication Unknown'
    },
    billing: {
      priced: 'Channel Priced (Sale Rate)',
      free: 'Free',
      conditional: 'Conditional Rate',
      partial: 'Partial Dimensions Priced',
      conflict: 'Group Price Conflict',
      varies: 'Group-Specific Pricing',
      unpriced: 'Unpriced (No Sale Rate)',
      error: 'Pricing Unavailable',
      unknown: 'Billing Unknown',
      p0Alert: 'Missing Sale Rate P0',
      referenceSecondary: 'Reference Price (Non-settlement): {rate} · Community Catalog (pi)'
    },
    columns: {
      model: 'Canonical Model / Provider',
      sources: 'Source Badges (Axis 1)',
      availability: 'Availability (Axis 2)',
      bindings: 'Connections / Groups',
      pricing: 'Price Layer / Alerts (Axis 3)',
      actions: 'Actions'
    },
    states: {
      loading: 'Loading model catalog snapshot…',
      empty: 'No models match the active filters.',
      snapshotExpired: 'Catalog snapshot cursor expired (410 SNAPSHOT_EXPIRED). Refresh snapshot to continue.',
      errorTitle: 'Failed to load model catalog'
    },
    drawer: {
      title: 'Model Catalog Detail & Evidence Chain',
      tabs: {
        inspector: 'Provenance · Funnel · Price Waterfall',
        verification: 'Verification & Publication Workbench'
      },
      provenanceTitle: '1. Field-Level Provenance Merge Table (Precedence: Approved > Supplement > Community (pi))',
      provenanceHint:
        'Native Discovered observations are scoped per connection; conflicting source values are highlighted in amber and unknown values are never guessed as 0 or false.',
      conflictBadge: 'Provenance Conflict',
      staleBadge: 'Stale Evidence',
      funnelTitle: '2. 4-Stage Funnel Binding Chain (1. Platform Candidates -> 2. Native Discovered -> 3. Account Effective -> 4. Group Visible)',
      stage1: '1. Platform Candidates',
      stage2: '2. Native Discovered',
      stage3: '3. Account Effective',
      stage4: '4. Group Visible',
      waterfallTitle: '3. 4-Layer Price Waterfall (1. Reference Price -> 2. Sale Rate -> 3. Internal Cost -> 4. Settlement Amount)',
      layer1: '1. Reference Price (Non-settlement)',
      layer1Hint: 'Source: Community Catalog (pi) · For reference comparison only; never used for gateway settlement',
      layer2: '2. Sale Rate',
      layer2Hint: 'Effective channel & routing group rate (group_default)',
      layer3: '3. Internal Cost',
      layer3Hint: 'Upstream connection credential cost (isolated from public sale rate)',
      layer4: '4. Settlement Amount',
      layer4Hint: 'Final billed amount after routing group multiplier and quota settlement',
      groupOnlyChannel: 'Group-only evidence (channel #0)',
      unboundOfferAccount: 'No concrete account binding (group/channel entry)'
    },
    provenanceFields: {
      context_window: 'Context Window',
      max_output_tokens: 'Max Output Tokens',
      modalities: 'Modalities',
      tools: 'Tool Calling',
      supports_vision: 'Vision Input',
      reasoning: 'Reasoning',
      prompt_caching: 'Prompt Caching',
      embedding_dimensions: 'Embedding Dimensions',
      pdf: 'PDF Input',
      reference_price: 'Reference Price (Non-settlement)'
    },
    batch: {
      title: 'Batch Preview & CAS Confirmation',
      subtitle: 'Every bulk mutation requires an explicit Before -> After diff preview and CAS token (If-Match / rules_revision).',
      operationLabel: 'Batch Operation',
      opEnableDiscovered: 'Add to Account Effective (2. Native Discovered -> 3. Account Effective)',
      opDraftChannelPricing: 'Draft Channel Sale Rate from Reference Price (1. Reference Price -> 2. Sale Rate Draft)',
      opTogglePublication: 'Bulk Update Group Visibility (3. Account Effective -> 4. Group Visible)',
      beforeColumn: 'Before State',
      afterColumn: 'After Preview',
      casTokenLabel: 'CAS Revision Token (If-Match / rules_revision)',
      confirmSubmit: 'Confirm & Submit with CAS',
      conflict409Title: 'Concurrent Version Conflict Detected (409 / 412 CAS Conflict)',
      conflict409Message: 'Target connection or channel was modified by another administrator. Your draft is preserved; refresh the snapshot and re-preview.',
      refreshAndRepreview: 'Refresh Snapshot & Re-preview'
    }
  }
}
