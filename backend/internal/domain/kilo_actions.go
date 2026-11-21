package domain

// Kilo's only native inference endpoint is Chat Completions. The shared text
// bridges preserve normal credential, model evidence and frozen pricing gates.
func kiloActionDescriptor(d ActionDescriptor, action string) ActionDescriptor {
	switch action {
	case "chat", "responses", "messages":
		d.Status, d.Adapter = ActionImplemented, "openai."+action
		d.Evidence = "our-free-model/src/kilo.mjs: POST /api/gateway/chat/completions; Responses and Messages use the shared Chat bridges"
		d.CredentialPolicy = "Kilo free public lane without Authorization, or apikey Bearer credential"
		d.ModelPolicy = "account catalog and free-pool admission policy"
		d.UsagePolicy = "observed input/output/reasoning counters and frozen request pricing"
		d.Lifecycle = "request plan"
	default:
		d.Status = ActionHardConflict
		d.Evidence = "Kilo adapter supports text Chat Completions and its Responses/Messages bridges only; no adapter or verified endpoint for " + action
	}
	return d
}
