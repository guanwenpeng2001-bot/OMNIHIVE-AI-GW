package domain

// Annex for the AWS Bedrock hosting profile (PlatformBedrock / AccountTypeBedrock,
// capability profile "bedrock"). Every row is anchored to first-party Amazon
// Bedrock documentation captured 2026-09-21 (Amazon Bedrock Runtime and Agents
// for Amazon Bedrock Runtime API references, the botocore service-2 signing
// metadata, and the per-family model parameter pages) plus the repository's
// existing Bedrock trunk. Nothing here claims an upstream permission: an
// implemented cell binds the gateway adapter that serves it.

// bedrockActionEvidence documents the serving adapter and its first-party
// protocol evidence for implemented Bedrock actions. `gateway.count_tokens`,
// `gateway.chat`, `gateway.messages` and `gateway.responses` travel the existing
// Anthropic-shaped trunk, which already resolves Bedrock targets; the media and
// vector adapters are the `bedrock.*` handlers.
var bedrockActionEvidence = map[string]string{
	"chat":            "gateway.chat trunk with the Bedrock Converse bridge for non-Claude families (bedrock_converse.go; Amazon Bedrock Runtime Converse and InvokeModel, captured 2026-09-21)",
	"messages":        "gateway.messages trunk: Claude targets use InvokeModel with the native anthropic_version body, other serving families use Converse (bedrock_platform.go; Amazon Bedrock Runtime InvokeModel/Converse, captured 2026-09-21)",
	"responses":       "gateway.responses trunk on the same Bedrock adapter as messages (Amazon Bedrock Runtime InvokeModel/Converse, captured 2026-09-21)",
	"count_tokens":    "gateway.count_tokens trunk delegating Bedrock accounts to ForwardBedrockCountTokens: POST /model/{modelId}/count-tokens with the invokeModel union member for anthropic.* targets and the converse member otherwise (Amazon Bedrock Runtime CountTokens, CountTokensInput, InvokeModelTokensRequest and ConverseTokensRequest, captured 2026-09-21); non-billable",
	"embeddings":      "bedrock.embeddings -> ForwardBedrockEmbeddings: InvokeModel on the Titan text embedding family (amazon.titan-embed-text-v2:0 with additive dimensions 1024|512|256, amazon.titan-embed-text-v1), one inputText per call with summed inputTextTokenCount usage (Amazon Titan Text Embeddings model parameters, captured 2026-09-21)",
	"rerank":          "bedrock.rerank -> ForwardBedrockRerank: POST https://bedrock-agent-runtime.{region}.amazonaws.com/rerank with one TEXT query, INLINE/TEXT sources and BEDROCK_RERANKING_MODEL configuration for amazon.rerank-v1:0 or cohere.rerank-v3-5:0 (Agents for Amazon Bedrock Runtime Rerank, captured 2026-09-21); priced per query, response carries no token counters",
	"image.generate":  "bedrock.images -> ForwardBedrockImageGeneration: InvokeModel TEXT_IMAGE for Nova Canvas, Titan Image Generator v1/v2, and Stability text-to-image; answers the OpenAI b64_json envelope (Nova Canvas, Titan Image Generator and Stability model parameter pages, captured 2026-09-21)",
	"image.edit":      "bedrock.images -> ForwardBedrockImageGeneration: documented INPAINTING for a masked edit and TEXT_IMAGE image conditioning for a maskless edit (Nova Canvas, Titan Image Generator v2); Stability image-to-image requires the documented strength; Titan Image Generator v1 has no maskless edit task and is rejected (same evidence, captured 2026-09-21)",
	"image.variation": "bedrock.image.variation -> ForwardBedrockImageGeneration: documented IMAGE_VARIATION task (imageVariationParams.images 1..5) for Nova Canvas and Titan Image Generator v1/v2; the Stability family documents no variation task and is rejected (same evidence, captured 2026-09-21)",
}

// bedrockHardConflicts records the cells whose capability was verified absent
// from the Bedrock hosting profile on 2026-09-21. A Composite group must route
// these actions to a platform that implements them; Bedrock cannot serve them
// natively or through a faithful bridge.
var bedrockHardConflicts = map[string]string{
	"image.async":         "Amazon Bedrock Runtime publishes no asynchronous image operation; batch inference is the control-plane CreateModelInvocationJob with S3 input and output, a different contract (operation lists captured 2026-09-21)",
	"image.batch":         "Same evidence as image.async: no runtime batch-image operation exists; the control-plane S3 job is not the gateway batch-image contract (captured 2026-09-21)",
	"video.edit":          "Nova Reel documents text-to-video and image-to-video only; no video edit task exists (Nova Reel model card, captured 2026-09-21)",
	"video.extend":        "Nova Reel documents no video extend task; length is chosen at submit time in 6 second increments up to 120 seconds (Nova Reel model card, captured 2026-09-21)",
	"audio.tts":           "The Amazon Bedrock Runtime operation list contains no speech synthesis operation; Nova Sonic is bidirectional speech-to-speech, not a unary TTS endpoint (operation list and Nova Sonic model card, captured 2026-09-21)",
	"audio.stt":           "The Amazon Bedrock Runtime operation list contains no speech-to-text operation (captured 2026-09-21)",
	"audio.translate":     "The Amazon Bedrock Runtime operation list contains no speech translation operation (captured 2026-09-21)",
	"voice":               "Bedrock exposes no voice management or voice cloning operation (captured 2026-09-21)",
	"search":              "Neither the Bedrock Runtime nor the Agents for Bedrock Runtime operation list publishes a standalone search operation; Retrieve/RetrieveAndGenerate answer against an operator-ingested knowledge base, which is not the gateway web-search contract (captured 2026-09-21)",
	"alpha_search":        "The alpha_search action is the Codex proprietary /alpha/search contract; Bedrock documents no equivalent endpoint (captured 2026-09-21)",
	"live":                "The live action is the Codex realtime calls contract; Bedrock documents no equivalent endpoint (captured 2026-09-21)",
	"responses.websocket": "Bedrock publishes no Responses-over-WebSocket operation, and the platform descriptor rejects ResponsesWS for this platform (captured 2026-09-21)",
	"generate_content":    "generate_content is the Google generativelanguage surface; Bedrock documents no such endpoint, and its conversational equivalent is Converse through the messages action (captured 2026-09-21)",
	"native":              "The native action is the DashScope /api/v1, /api-ws/v1 and /compatible-mode/v1 ingress, which belongs to the Qianwen platform entry; the Amazon Bedrock Runtime operation set is InvokeModel, InvokeModelWithResponseStream, Converse, ConverseStream, CountTokens, StartAsyncInvoke, GetAsyncInvoke and ListAsyncInvokes (captured 2026-09-21)",
}

// bedrockDeferredActions records cells whose native capability is verified but
// whose adapter is not part of this round, each with the concrete blocker. They
// stay unverified: the platform can serve them, the gateway does not yet.
var bedrockDeferredActions = map[string]string{
	"video.generate":    "Verified native: Nova Reel text-to-video and image-to-video through StartAsyncInvoke with outputDataConfig.s3OutputDataConfig.s3Uri (Amazon Bedrock Runtime StartAsyncInvoke and Nova Reel model card, captured 2026-09-21); blocked on an account-level S3 output bucket credential and on wiring the async job family, which is owned by the upstream job service",
	"video.status":      "Verified native: GetAsyncInvoke / async-invoke/{invocationArn} returns InProgress|Completed|Failed with a failureMessage (captured 2026-09-21); blocked on the same async job and S3 output wiring as video.generate",
	"video.content":     "Verified native: the finished artifact is <s3Uri>/output.mp4 (Nova Reel examples, captured 2026-09-21); serving it needs the S3 signing name and the bucket credential, both missing from the account model",
	"realtime":          "Verified native: Amazon Nova Sonic is reachable only through InvokeModelWithBidirectionalStream, an HTTP/2 bidirectional event stream, not the gateway's session-based realtime contract (operation list and Nova Sonic model card, captured 2026-09-21); bridging it is a transport decision, not a session adapter",
	"responses.compact": "No distinct Bedrock operation exists for the compact contract; Anthropic compaction travels inside InvokeModel through the compact-2026-01-12 / context-management-2026-06-27 beta tokens already whitelisted by bedrock_request.go (captured 2026-09-21). Serving the OpenAI-shaped compact path on Bedrock is unverified rather than impossible",
}

// bedrockHostedTrunkEvidence refines the evidence of cells served by the
// Anthropic trunk when the credentials are hosted on Bedrock. Those cells keep
// their Anthropic adapter; only the trunk behavior this branch changed is named.
var bedrockHostedTrunkEvidence = map[string]string{
	"count_tokens": bedrockActionEvidence["count_tokens"],
}

// bedrockActionDescriptor finalizes the tri-state for every Bedrock cell:
// implemented cells carry adapter, evidence and lifecycle bindings, verified
// absences become hard conflicts with evidence, and verified-but-deferred cells
// keep the blocker instead of a silent unverified row.
func bedrockActionDescriptor(descriptor ActionDescriptor, action, adapter string) ActionDescriptor {
	if adapter == "" {
		if evidence, conflict := bedrockHardConflicts[action]; conflict {
			descriptor.Status = ActionHardConflict
			descriptor.Evidence = evidence
			return descriptor
		}
		if blocker, deferred := bedrockDeferredActions[action]; deferred {
			descriptor.Evidence = blocker
			return descriptor
		}
		descriptor.Evidence = "No verified Bedrock gateway adapter for this action cell (first-party surface rechecked 2026-09-21)"
		return descriptor
	}
	descriptor.Status, descriptor.Adapter = ActionImplemented, adapter
	if evidence, ok := bedrockActionEvidence[action]; ok {
		descriptor.Evidence = evidence
	} else {
		descriptor.Evidence = "Existing gateway adapter: " + adapter
	}
	descriptor.CredentialPolicy = "bedrock account authentication (SigV4 or bearer)"
	descriptor.ModelPolicy = "resolved Bedrock target (model ID, inference profile ARN) with account model evidence and task limits"
	descriptor.UsagePolicy = "upstream usage fields with frozen request pricing"
	descriptor.Lifecycle = "request plan"
	switch action {
	case "count_tokens":
		descriptor.UsagePolicy = "non-billable token counting"
		descriptor.Lifecycle = "synchronous request"
	case "chat", "messages", "responses":
		descriptor.Lifecycle = "request plan"
	case "rerank":
		// The Rerank response exposes no token counters and AWS prices it per
		// query, so this cell must settle on a per-request price.
		descriptor.UsagePolicy = "no upstream token counters; per-query price required"
		descriptor.Lifecycle = "synchronous request"
	case "embeddings":
		descriptor.UsagePolicy = "summed documented inputTextTokenCount per InvokeModel call"
		descriptor.Lifecycle = "synchronous request"
	case "image.generate", "image.edit", "image.variation":
		descriptor.UsagePolicy = "returned image count with the requested or detected size tier"
		descriptor.Lifecycle = "synchronous request"
	}
	return descriptor
}
