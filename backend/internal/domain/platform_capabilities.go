package domain

// EndpointSupport describes a static path; the zero value rejects.
type EndpointSupport string

const (
	EndpointRejected EndpointSupport = ""
	EndpointNative   EndpointSupport = "native"
	EndpointBridge   EndpointSupport = "bridge"
)

// PlatformDescriptor contains shared static facts, not model routing, transport,
// credentials, or measured quota. Composite belongs to groups, never this table.
// A capability is an eligibility bound: account settings and specialized
// services still decide whether a particular request can actually execute.
type PlatformDescriptor struct {
	HostedAccountType string `json:"hostedAccountType,omitempty"`
	ProductPlatform   string `json:"productPlatform,omitempty"`
	// ResponsesPOST describes an available protocol path; multi-protocol accounts resolve
	// their actual native protocol in the existing account/model services.
	ResponsesPOST                EndpointSupport `json:"responsesPost"`
	ResponsesWS                  EndpointSupport `json:"responsesWS"`
	ResponsesCompact             bool            `json:"responsesCompact"`
	NativeMultiProtocolResponses bool            `json:"nativeMultiProtocolResponses"`

	ChatOnly            bool   `json:"chatOnly"`
	ID                  string `json:"id"`
	OAuth               bool   `json:"oauth"`
	CNProvider          bool   `json:"cnProvider"`
	MultiProtocol       bool   `json:"multiProtocol"`
	SchedulingThreshold bool   `json:"schedulingThreshold"`
	ActiveProbe         bool   `json:"activeProbe"`
	QuotaMonitor        bool   `json:"quotaMonitor"`
	PassiveMonitor      bool   `json:"passiveMonitor"`
	ModelCatalog        bool   `json:"modelCatalog"`
}

var platformDescriptors = []PlatformDescriptor{
	{ID: PlatformAnthropic, ResponsesPOST: EndpointBridge, OAuth: true, SchedulingThreshold: true, ActiveProbe: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformGemini, OAuth: true, ActiveProbe: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformOpenAI, ResponsesCompact: true, ResponsesWS: EndpointNative, ResponsesPOST: EndpointNative, OAuth: true, SchedulingThreshold: true, ActiveProbe: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformAntigravity, ResponsesPOST: EndpointBridge, OAuth: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformGrok, ResponsesCompact: true, ResponsesWS: EndpointBridge, ResponsesPOST: EndpointNative, OAuth: true, SchedulingThreshold: true, ActiveProbe: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformKimi, NativeMultiProtocolResponses: true, ResponsesPOST: EndpointNative, CNProvider: true, MultiProtocol: true, SchedulingThreshold: true, ActiveProbe: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformZhipu, ResponsesPOST: EndpointBridge, CNProvider: true, MultiProtocol: true, SchedulingThreshold: true, ActiveProbe: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformDeepseek, NativeMultiProtocolResponses: true, ResponsesPOST: EndpointNative, CNProvider: true, MultiProtocol: true, ActiveProbe: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformMiniMax, NativeMultiProtocolResponses: true, ResponsesPOST: EndpointNative, CNProvider: true, MultiProtocol: true, SchedulingThreshold: true, ActiveProbe: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformOpenCodeGo, NativeMultiProtocolResponses: true, ResponsesPOST: EndpointNative, MultiProtocol: true, SchedulingThreshold: true, ActiveProbe: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformCursor, ResponsesPOST: EndpointBridge, OAuth: true, SchedulingThreshold: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformQianwen, NativeMultiProtocolResponses: true, ResponsesPOST: EndpointNative, CNProvider: true, MultiProtocol: true, ActiveProbe: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformCommandCode, ResponsesPOST: EndpointBridge, MultiProtocol: true, SchedulingThreshold: true, ActiveProbe: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformVolcengine, ChatOnly: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformQianfan, ChatOnly: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformHunyuan, ChatOnly: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformMistral, ChatOnly: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformBedrock, HostedAccountType: AccountTypeBedrock, ProductPlatform: PlatformAnthropic, ResponsesPOST: EndpointBridge, PassiveMonitor: true, ModelCatalog: true},
}

// PlatformDescriptors returns a copy so callers cannot mutate the authority.
func PlatformDescriptors() []PlatformDescriptor {
	return append([]PlatformDescriptor(nil), platformDescriptors...)
}

func LookupPlatform(id string) (PlatformDescriptor, bool) {
	for _, descriptor := range platformDescriptors {
		if descriptor.ID == id {
			return descriptor, true
		}
	}
	return PlatformDescriptor{}, false
}

func ConcretePlatforms() []string {
	return PlatformsWith(func(PlatformDescriptor) bool { return true })
}

func PlatformsWith(include func(PlatformDescriptor) bool) []string {
	platforms := make([]string, 0, len(platformDescriptors))
	for _, descriptor := range platformDescriptors {
		if include(descriptor) {
			platforms = append(platforms, descriptor.ID)
		}
	}
	return platforms
}

func IsConcretePlatform(id string) bool {
	_, ok := LookupPlatform(id)
	return ok
}

func IsGroupPlatform(id string) bool { return id == PlatformComposite || IsConcretePlatform(id) }

// ActionStatus describes adapter evidence, not a provider-wide capability claim.
type ActionStatus string

const (
	ActionImplemented  ActionStatus = "implemented"
	ActionUnverified   ActionStatus = "unverified"
	ActionHardConflict ActionStatus = "hard_conflict"
)

// ActionIdentity keeps hosted credentials and protocol/profile selection separate
// from the product platform. Empty dimensions select the platform ingress bound;
// account/model evidence must still be checked before sending.
type ActionIdentity struct{ Platform, HostedAccountType, Protocol, Profile string }
type ActionDescriptor struct {
	Identity         ActionIdentity `json:"identity"`
	Action           string         `json:"action"`
	Status           ActionStatus   `json:"status"`
	Adapter          string         `json:"adapter,omitempty"`
	Evidence         string         `json:"evidence"`
	CredentialPolicy string         `json:"credentialPolicy,omitempty"`
	ModelPolicy      string         `json:"modelPolicy,omitempty"`
	UsagePolicy      string         `json:"usagePolicy,omitempty"`
	Lifecycle        string         `json:"lifecycle,omitempty"`
}

func GatewayActions() []string {
	return []string{"chat", "messages", "responses", "responses.compact", "count_tokens", "generate_content", "embeddings", "rerank", "image.generate", "image.edit", "image.variation", "image.async", "image.batch", "video.generate", "video.edit", "video.extend", "video.status", "video.content", "audio.tts", "audio.stt", "audio.translate", "voice", "realtime", "search", "alpha_search", "live", "responses.websocket", "native"}
}

// PlatformAction resolves only adapters already present in the gateway. Missing
// adapters stay unverified: absence in this repository is not proof a supplier
// cannot implement an action. No exposure switch grants an adapter.
func PlatformAction(identity ActionIdentity, action string) ActionDescriptor {
	descriptor := ActionDescriptor{Identity: identity, Action: action, Status: ActionUnverified, Evidence: "No verified gateway adapter; provider actions task must verify this cell"}
	platform, known := LookupPlatform(identity.Platform)
	if !known {
		return descriptor
	}
	if identity.HostedAccountType == AccountTypeBedrock && identity.Platform != PlatformAnthropic && identity.Platform != PlatformBedrock {
		return descriptor
	}
	if identity.HostedAccountType == AccountTypeServiceAccount && identity.Platform != PlatformAnthropic && identity.Platform != PlatformGemini {
		return descriptor
	}
	// Bedrock cells are finalized in domain/bedrock_actions.go: the runtime
	// publishes CountTokens (so ForwardCountTokens delegates Bedrock accounts to
	// the native adapter in bedrock_actions.go), and every media/vector cell carries
	// its own evidence, hard conflict or deferral reason.
	// Credential restrictions mirror the existing endpoint adapters. Missing
	// identity dimensions are ingress bounds, never account authorization.
	if identity.Platform == PlatformOpenAI {
		// Verified against the official OpenAI API specification
		// (openai/openai-openapi@master, 224 paths enumerated 2026-09-21): there is
		// no rerank endpoint and no standalone search endpoint, so no native or
		// faithful bridge exists. Web search remains available as a Responses API
		// tool, already served by the responses action.
		switch action {
		case "rerank":
			descriptor.Status = ActionHardConflict
			descriptor.Evidence = "Official OpenAI API specification (openai/openai-openapi@master, checked 2026-09-21) publishes no rerank endpoint and no rerank model; no faithful bridge exists"
			return descriptor
		case "search":
			descriptor.Status = ActionHardConflict
			descriptor.Evidence = "Official OpenAI API specification (openai/openai-openapi@master, checked 2026-09-21) publishes no standalone search endpoint; web search exists only as a Responses API tool, already served by the responses action"
			return descriptor
		}
	}
	if identity.HostedAccountType != "" {
		if platform.ChatOnly && identity.HostedAccountType != AccountTypeAPIKey {
			return descriptor
		}
		if identity.Platform == PlatformOpenAI {
			switch action {
			case "embeddings", "image.variation", "video.generate", "video.edit", "video.extend", "video.status", "video.content", "audio.translate", "realtime":
				// Open-API data-plane actions are verified for first-party API keys
				// only; Codex OAuth credentials do not grant these endpoints.
				if identity.HostedAccountType != AccountTypeAPIKey {
					return descriptor
				}
			case "live":
				if identity.HostedAccountType != AccountTypeOAuth {
					return descriptor
				}
			case "alpha_search":
				if identity.HostedAccountType != AccountTypeAPIKey && identity.HostedAccountType != AccountTypeOAuth {
					return descriptor
				}
			case "image.generate", "image.edit", "image.async", "image.batch":
				if identity.HostedAccountType != AccountTypeAPIKey && identity.HostedAccountType != AccountTypeOAuth && identity.HostedAccountType != AccountTypeSetupToken {
					return descriptor
				}
			}
		}
	}
	adapter := ""
	openAITrunk := platform.ResponsesPOST == EndpointNative || identity.Platform == PlatformZhipu || identity.Platform == PlatformCommandCode
	switch action {
	case "chat":
		adapter = "gateway.chat"
		if openAITrunk || platform.ChatOnly {
			adapter = "openai.chat"
		}
	case "messages", "responses", "count_tokens":
		if !platform.ChatOnly {
			adapter = "gateway." + action
			if openAITrunk {
				adapter = "openai." + action
			}
			if action == "count_tokens" && identity.Platform == PlatformCursor {
				adapter = "cursor.count_tokens"
			}
			if action == "count_tokens" && identity.Platform == PlatformGrok {
				adapter = "grok.count_tokens"
			}
		}
	case "generate_content":
		if identity.Platform == PlatformGemini || identity.Platform == PlatformAntigravity {
			adapter = "gateway.generate_content"
		}
		if identity.Platform == PlatformOpenCodeGo {
			// Zen publishes a Google-protocol column; Go does not. The adapter
			// itself rejects the Go plan, since the plan is account mode rather
			// than an action identity dimension.
			adapter = "opencode.generate_content"
		}
	case "responses.compact":
		if platform.ResponsesCompact {
			adapter = "openai.responses"
		}
	case "embeddings":
		if identity.Platform == PlatformOpenAI {
			adapter = "openai.embeddings"
		}
		if identity.Platform == PlatformQianwen {
			adapter = "qianwen.embeddings"
		}
		if identity.Platform == PlatformBedrock {
			adapter = "bedrock.embeddings"
		}
	case "rerank":
		if identity.Platform == PlatformQianwen {
			adapter = "qianwen.rerank"
		}
		if identity.Platform == PlatformBedrock {
			adapter = "bedrock.rerank"
		}
	case "image.generate", "image.edit":
		if identity.Platform == PlatformOpenAI {
			adapter = "openai.images"
		}
		if identity.Platform == PlatformGrok {
			adapter = "grok.images"
		}
		if identity.Platform == PlatformQianwen {
			adapter = "qianwen.native"
			if action == "image.generate" {
				adapter = "qianwen.images"
			}
		}
		if identity.Platform == PlatformBedrock {
			adapter = "bedrock.images"
		}
	case "image.variation":
		if identity.Platform == PlatformOpenAI {
			adapter = "openai.image.variation"
		}
		if identity.Platform == PlatformBedrock {
			adapter = "bedrock.image.variation"
		}
	case "image.async", "image.batch":
		if identity.Platform == PlatformOpenAI || identity.Platform == PlatformGrok {
			adapter = action
		}
		if identity.Platform == PlatformQianwen && action == "image.async" {
			adapter = "qianwen.native"
		}
	case "video.generate", "video.edit", "video.extend", "video.status", "video.content", "audio.tts", "audio.stt", "voice", "realtime", "search":
		if identity.Platform == PlatformGrok {
			adapter = "grok." + action
		}
		if identity.Platform == PlatformOpenAI {
			switch action {
			case "video.generate", "video.edit", "video.extend", "video.status", "video.content", "realtime":
				adapter = "openai." + action
			}
		}
		if identity.Platform == PlatformQianwen && action != "search" {
			adapter = "qianwen.native"
		}
	case "audio.translate":
		if identity.Platform == PlatformOpenAI {
			adapter = "openai.audio.translate"
		}
	case "alpha_search", "live":
		if identity.Platform == PlatformOpenAI {
			adapter = "openai." + action
		}
	case "responses.websocket":
		if platform.ResponsesWS != EndpointRejected {
			adapter = "openai.responses.websocket"
		}
	case "native":
		if identity.Platform == PlatformQianwen {
			adapter = "qianwen.native"
		}
	}
	if identity.Platform == PlatformQianwen {
		return qianwenActionDescriptor(descriptor, action, adapter)
	}
	if identity.Platform == PlatformQianfan {
		return qianfanActionDescriptor(descriptor, action, adapter)
	}
	if identity.Platform == PlatformAntigravity {
		return antigravityActionDescriptor(descriptor, action, adapter)
	if identity.Platform == PlatformBedrock {
		return bedrockActionDescriptor(descriptor, action, adapter)
	if identity.Platform == PlatformOpenCodeGo {
		return openCodeActionDescriptor(descriptor, action, adapter)
	if identity.Platform == PlatformVolcengine {
		return volcengineActionDescriptor(descriptor, action, adapter)
	}
	if adapter == "" {
		return descriptor
	}
	descriptor.Status, descriptor.Adapter = ActionImplemented, adapter
	descriptor.Evidence = "Existing gateway adapter: " + adapter
	descriptor.CredentialPolicy = "account credential owner; native adapter authentication"
	if identity.HostedAccountType == AccountTypeBedrock || identity.Platform == PlatformBedrock {
		descriptor.CredentialPolicy = "bedrock account authentication (SigV4 or bearer)"
	}
	if identity.HostedAccountType == AccountTypeServiceAccount {
		descriptor.CredentialPolicy = "service account project and region"
	}
	descriptor.ModelPolicy = "resolved account model evidence and task limits"
	descriptor.UsagePolicy = "existing adapter usage and frozen request pricing"
	descriptor.Lifecycle = "request plan"
	if action == "count_tokens" {
		descriptor.UsagePolicy = "non-billable token counting"
		descriptor.Lifecycle = "synchronous request"
	}
	if identity.Platform == PlatformOpenAI {
		switch action {
		case "image.variation", "video.generate", "video.edit", "video.extend", "video.status", "video.content", "audio.translate", "realtime":
			descriptor.Evidence = "Native adapter in openai_actions.go/openai_video_jobs.go; official OpenAI API specification (openai/openai-openapi@master) and video/audio/realtime references checked 2026-09-21"
			descriptor.CredentialPolicy = "OpenAI API key account only (open-API data plane); Codex OAuth credentials are not valid for this action"
		}
		if action == "video.status" || action == "video.content" {
			descriptor.Lifecycle = "request plan and upstream job"
		}
	}
	if action == "image.async" || action == "image.batch" || action == "video.generate" || action == "video.edit" || action == "video.extend" {
		descriptor.Lifecycle = "request plan and upstream job"
	}
	if identity.HostedAccountType == AccountTypeBedrock {
		// Anthropic product entry with Bedrock hosted credentials. The trunk
		// adapter for these cells is inherited from the Anthropic column; only the
		// cell this branch changed (count_tokens now reaches the native Bedrock
		// operation) gets Bedrock-specific evidence.
		if evidence, ok := bedrockHostedTrunkEvidence[action]; ok {
			descriptor.Evidence = evidence
		}
	}
	return descriptor
}

func PlatformActionMatrix(identity ActionIdentity) []ActionDescriptor {
	actions := GatewayActions()
	result := make([]ActionDescriptor, 0, len(actions))
	for _, action := range actions {
		result = append(result, PlatformAction(identity, action))
	}
	return result
}
