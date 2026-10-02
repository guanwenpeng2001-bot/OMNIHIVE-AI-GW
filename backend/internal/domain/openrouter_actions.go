package domain

// OpenRouter native Messages/Responses exist, but these public contracts use
// the established chat conversion adapters until native adapters are verified.
func openRouterActionDescriptor(d ActionDescriptor, action, adapter string) ActionDescriptor {
	d.Evidence = "OpenRouter API reference https://openrouter.ai/docs/api/api-reference (checked 2026-10-02); native adapter not yet verified"
	if hosted := d.Identity.HostedAccountType; hosted != "" && hosted != AccountTypeAPIKey {
		return d
	}
	switch action {
	case "chat", "messages", "responses", "generate_content":
		if adapter == "" {
			return d
		}
		d.Status, d.Adapter = ActionImplemented, adapter
		d.Evidence = "Public text converted through the existing POST /chat/completions adapter; https://openrouter.ai/docs/api-reference/overview (checked 2026-10-02)"
		d.CredentialPolicy = "OpenRouter API key with Authorization: Bearer; account base_url or https://openrouter.ai/api/v1"
		d.ModelPolicy = "Exact account model ID including provider prefix and colon variant"
		d.UsagePolicy = "Observed response token counters and frozen customer pricing; upstream cost is separate"
		d.Lifecycle = "request plan"
	}
	return d
}
