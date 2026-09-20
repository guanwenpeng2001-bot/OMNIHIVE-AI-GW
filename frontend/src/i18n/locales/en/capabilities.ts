export default {
  capabilityDefaults: {
  title: 'Private deployment specifications (JSON)', input: 'Account model capability declaration',
  hint: 'The deployment owner declares actual input/output limits, modalities, reasoning controls and function calling by original model ID, with confirmation and evidence. Native fields take priority; declarations do not grant access or routing. Preview field sources and gaps, then save the account and preview/apply its catalog. Clear to remove declarations.',
  invalid: 'Enter a valid declaration with schema_version: 1, a verification reason, and a nonempty models object indexed by original ID.',
  placeholder: 'Leave empty when no deployment specification is declared', example: 'Format example (replace illustrative values with verified deployment facts)',
  binding: 'The server generates confirmed_by, confirmed_at and identity information. After an identity change, verify the deployment facts and remove the old identity_fingerprint before previewing and saving to reconfirm. reasoning_control.kind supports none / effort / toggle / budget / adaptive / fixed / unknown; declare only controls the deployment actually supports.'
},
  capabilityCatalog: {
  selectFiltered: 'Select all matching models', resolutions: 'Resolutions', duration: 'Maximum duration (seconds)', dimensions: 'Dimensions', width: 'Maximum width', height: 'Maximum height',
  sets: 'Model sets', setsHint: 'D discovered · M mapping targets · U capability catalog (D ∪ M) · E routing eligible; complete capabilities do not grant eligibility.', coverage: 'Complete capabilities', noDiscovery: 'No models discovered', noEligibility: 'Models discovered, none eligible', partial: 'Some capabilities are missing or estimated', complete: 'Capabilities complete', unknownDiscovery: 'Discovery status unknown', search: 'Filter model ID or name', filter: 'Filter capability status', all: 'All statuses', more: 'Show more',
  title: 'Model capabilities', missing: 'Missing fields', estimated: 'Estimated fields', conflict: 'Conflict',
  unknown: 'Status unavailable', unavailable: 'Field completeness was not returned. Preview again; completeness is unverified.',
  source: 'Source tier', version: 'Source version', match: 'Match evidence', noSources: 'No field-level source evidence available.', stale: 'Specifications or account settings changed; these results belong to the previous preview.'
},
  quickstartGuide: {
  title: "Generate examples for your key",
  hint: "Read your keys and model catalogue only after a click; no generation requests run here. Keys stay in request headers and examples read environment variables.",
  loadKeys: "Load my keys",
  loading: "Loading metadata…",
  noKeys: "No active grouped keys on this page. Check other pages, or create or enable a key and assign a group.",
  gatewayUrl: "Gateway URL: ",
  trustGateway: "I trust this gateway URL and agree to send the selected key to it to read models.",
  keyGroup: "My key / group",
  selectKey: "Select a key",
  previous: "Previous",
  next: "Next",
  loadModels: "Load models for this key",
  loaded: "Catalogue loaded; billing receipt: not applicable (no generation executed).",
  noModels: "No visible models in this group.",
  filter: "Filter models",
  noMatches: "No filter matches; clear the filter to try again.",
  visibleModel: "Visible model",
  unknownEndpoints: "The catalogue does not declare endpoint capabilities. Choose a protocol using your group documentation; this example is not an availability check.",
  unsupportedEndpoints: "No supported example endpoints are declared.",
  protocol: "Request protocol",
  selectProtocol: "Select a protocol",
  billingHint: "Running these examples may incur charges. HTTP 200 is transport success only; an unconfirmed billing receipt remains pending receipt. Check My usage after generation.",
  keysFailed: "Could not load keys or public URL. Retry.",
  accessDenied: "Model catalogue access denied. Check the key, group and access permissions.",
  catalogFailed: "Catalogue failed. Check group, public URL and access, then retry."
},
  capabilityMessages: {
  "invalid": "Invalid deployment specifications. Check the declaration format, verification evidence and account identity, then preview again.",
  "missing": "Missing fields",
  "estimated": "Estimated fields",
  "conflicts": "Conflicts"
}
}
