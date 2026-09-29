package domain

// The first-party search schema was captured on 2026-09-21 and is reproduced
// in service/kimi_actions.go. Coding credentials have no published search API;
// the account adapter enforces that restriction before sending credentials.
func kimiSearchActionDescriptor(descriptor ActionDescriptor) ActionDescriptor {
	if descriptor.Identity.HostedAccountType != "" && descriptor.Identity.HostedAccountType != AccountTypeAPIKey {
		return descriptor
	}
	descriptor.Status, descriptor.Adapter = ActionImplemented, "kimi.search"
	descriptor.Evidence = "platform.kimi.ai/docs/api/tools-search (captured 2026-09-21, source excerpt in service/kimi_actions.go): POST /v1/tools/search and /v1/tools/search_pro; text_query required; search_results returned; platform.kimi.com/docs/pricing/tools: HTTP 200 with nonempty search_results is billable"
	descriptor.CredentialPolicy = "Kimi payg API key; coding mode rejected before upstream submission"
	descriptor.ModelPolicy = "native standalone search tool; Basic or Pro selected by route"
	descriptor.UsagePolicy = "one web search call only for a nonempty successful result; shared configured search pricing"
	descriptor.Lifecycle = "synchronous request plan with cancellation and attempt budget"
	return descriptor
}
