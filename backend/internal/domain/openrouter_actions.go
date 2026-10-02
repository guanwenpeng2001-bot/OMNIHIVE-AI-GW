package domain

// OpenRouter native Messages/Responses exist, but these public contracts use
// the established chat conversion adapters until native adapters are verified.
func openRouterActionDescriptor(d ActionDescriptor, action, adapter string) ActionDescriptor {
	d.Evidence = "OpenRouter API reference https://openrouter.ai/docs/api/api-reference (checked 2026-10-02); native adapter not yet verified"
	if hosted := d.Identity.HostedAccountType; hosted != "" && hosted != AccountTypeAPIKey {
		return d
	}
	switch action {
	case "image.generate", "audio.tts", "audio.stt", "embeddings", "rerank", "video.generate", "video.status", "video.content":
		d.Status, d.Adapter = ActionImplemented, "openrouter."+action
		d.Evidence = "Official OpenRouter media schema checked 2026-10-02; service/openrouter_media.go validates the translated request and actual result"
		d.CredentialPolicy = "API-key Bearer via account base URL and egress"
		d.ModelPolicy = "Exact mapped model, existing group/key/model admission"
		d.UsagePolicy = "Observed tokens/images/audio seconds or submitted TTS characters; existing customer tariff and receipt settlement"
		d.Lifecycle = "synchronous request"
		if action == "video.generate" || action == "video.status" || action == "video.content" {
			d.Lifecycle = "durable owner-bound job; submit acknowledgement never settles"
			d.UsagePolicy = "terminal artifact with frozen explicit per-request tariff, otherwise completed needs_review without debit"
			d.Evidence = "Official OpenRouter video-generation submit/poll/download schema checked 2026-10-02; service/openrouter_video.go"
		}
		return d
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
