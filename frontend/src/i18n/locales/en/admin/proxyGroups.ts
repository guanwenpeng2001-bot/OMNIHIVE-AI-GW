export default {
  proxyGroups: {
    title: 'IP groups', description: 'Organize IP nodes and observe account egress bindings.',
    create: 'Create group', edit: 'Edit group', name: 'Name', descriptionLabel: 'Description', region: 'Region label', services: 'Target service labels',
    tagsHint: 'Labels describe intended use; they do not prove service access.',
    search: 'Search groups', allStatus: 'All statuses', allServices: 'All services', active: 'Active', inactive: 'Inactive', expired: 'Expired',
    counts: 'Schedulable / total', countsHint: 'Enabled, unexpired nodes outside cooldown. This does not guarantee a successful request.',
    accounts: 'Bound accounts', fallback: 'When no node is schedulable', none: 'Fail request (default)', direct: 'Direct connection', proxy: 'Backup node',
    directHint: 'Direct connection sends requests without a proxy when the group has no schedulable nodes.',
    inactiveHint: 'A disabled group fails explicitly, regardless of its fallback setting.',
    ttl: 'Binding lifetime (seconds)', advanced: 'Advanced settings', members: 'Members', bindings: 'Binding observations',
    empty: 'No IP groups found. Create a group or adjust the filters.', loadFailed: 'Could not load IP groups. Try again.',
    saveFailed: 'Could not save changes.', saved: 'Changes saved.', delete: 'Delete group', deleteConfirm: 'Delete this IP group? Its nodes will be kept.',
    inUse: 'Remove account assignments before deleting this group.', deleteFailed: 'Could not delete group. It may still have bound accounts.',
    memberSearch: 'Search nodes by name, host or region', allRegions: 'All regions', selected: 'Selected ({count})', selectVisible: 'Select visible nodes', clearSelection: 'Clear selection',
    noNodes: 'No matching nodes.', observations: 'Probe observations', unknown: 'Not probed', cooldown: 'Cooldown until {time}',
    membersHint: 'Unknown health is selectable. Inactive or expired members remain visible but are not schedulable.',
    membersLoadFailed: 'Could not load members. Close this dialog and try again.',
    account: 'Account', node: 'Current node', expires: 'Binding expires', eligible: 'Eligible', ineligible: 'Not eligible', unbound: 'Not currently bound',
    noBindings: 'No accounts are assigned to this group.', reset: 'Reset all bindings', resetConfirm: 'Accounts will select a node on their next request. Ongoing streams and WebSocket connections are not disconnected.',
    resetDone: 'Cleared {count} bindings.', bindingsFailed: 'Could not load or reset bindings.',
    egress: 'Account egress', noProxy: 'No proxy', defaultEgress: 'Use the existing local or server default egress. No IP node or group is selected automatically.', selectionRequired: 'Choose a node or group, or select No proxy.', single: 'Single node', group: 'IP group', chooseGroup: 'Choose an IP group', chooseNode: 'Choose a node',
    groupHint: 'The group schedules the current egress node.', unavailableGroup: 'Selected group #{id} (unavailable)',
    groupLoadFailed: 'Could not load groups. The current selection is preserved.', retry: 'Retry',
    health: { healthy: 'Healthy observation', warn: 'Warning observation', challenge: 'Challenge observed', failed: 'Failed observation' }
  }
}
