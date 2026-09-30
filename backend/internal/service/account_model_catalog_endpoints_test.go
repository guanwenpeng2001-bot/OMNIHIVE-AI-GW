package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountModelCatalogDeclaredEndpointCoverage(t *testing.T) {
	// The scheduler passes requiredCapability directly as the binding endpoint.
	// Discover the enum so additions cannot silently escape projection coverage.
	file, err := parser.ParseFile(token.NewFileSet(), "account.go", nil, 0)
	require.NoError(t, err)
	capabilities := []string{}
	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || !strings.HasPrefix(spec.Names[0].Name, "OpenAIEndpointCapability") || len(spec.Values) != 1 {
			return true
		}
		literal, ok := spec.Values[0].(*ast.BasicLit)
		if ok && literal.Kind == token.STRING {
			value, err := strconv.Unquote(literal.Value)
			require.NoError(t, err)
			capabilities = append(capabilities, value)
		}
		return true
	})
	require.NotEmpty(t, capabilities)
	for _, endpoint := range append(capabilities, "messages", "generate_content") {
		t.Run(endpoint, func(t *testing.T) {
			account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
			catalog := &UpstreamModelCatalog{Models: []string{"declared-model"}, Metadata: map[string]UpstreamModelMetadata{
				"declared-model": {SupportedMethods: []string{endpoint, endpoint}},
			}}
			snapshot := projectAccountModelCatalog(account, catalog)
			require.Empty(t, snapshot.ExcludedModels)
			require.Len(t, snapshot.EligibleModels, 1)
			require.Equal(t, []string{endpoint}, snapshot.EligibleModels[0].Endpoints)
		})
	}
}

func TestAccountModelCatalogCapabilityInference(t *testing.T) {
	tests := []struct {
		name        string
		platform    string
		model       string
		metadata    UpstreamModelMetadata
		credentials map[string]any
		want        []string
	}{
		{name: "search task", platform: PlatformOpenAI, metadata: UpstreamModelMetadata{TaskType: "alpha_search"}, want: []string{"alpha_search"}},
		{name: "search account declaration", platform: PlatformOpenAI, credentials: map[string]any{openAIEndpointCapabilitiesCredentialKey: []string{"alpha_search"}}, want: []string{"alpha_search", "chat_completions"}},
		{name: "search supplements chat", platform: PlatformOpenAI, metadata: UpstreamModelMetadata{TaskType: "text_generation"}, credentials: map[string]any{openAIEndpointCapabilitiesCredentialKey: map[string]bool{"alpha_search": true}}, want: []string{"alpha_search", "chat_completions"}},
		{name: "search not inferred from permissive default", platform: PlatformOpenAI, metadata: UpstreamModelMetadata{TaskType: "text_generation"}, want: []string{"chat_completions"}},
		{name: "search disabled", platform: PlatformOpenAI, credentials: map[string]any{openAIEndpointCapabilitiesCredentialKey: map[string]bool{"alpha_search": false}}, want: []string{"chat_completions"}},
		{name: "imagine image", platform: PlatformGrok, model: "grok-imagine-image", want: []string{"grok_media_generation"}},
		{name: "imagine video", platform: PlatformGrok, model: "grok-imagine-video", want: []string{"grok_media_generation"}},
		{name: "qualified imagine", platform: PlatformGrok, model: "xai/grok-imagine-image-quality", want: []string{"grok_media_generation"}},
		{name: "media task", platform: PlatformGrok, metadata: UpstreamModelMetadata{TaskType: "video_generation"}, want: []string{"grok_media_generation"}},
		{name: "openai video task", platform: PlatformOpenAI, metadata: UpstreamModelMetadata{TaskType: "video_generation"}, want: []string{"images"}},
		{name: "openai video output modality", platform: PlatformOpenAI, metadata: UpstreamModelMetadata{OutputModalities: []string{"video"}}, want: []string{"images"}},
		{name: "openai declared video task", platform: PlatformOpenAI, metadata: UpstreamModelMetadata{TaskType: "video_generation", SupportedMethods: []string{"images", "chat_completions"}}, want: []string{"chat_completions", "images"}},
		{name: "chat not media", platform: PlatformGrok, model: "grok-4.3", metadata: UpstreamModelMetadata{TaskType: "text_generation"}, want: []string{"chat_completions"}},
		{name: "known grok text without metadata", platform: PlatformGrok, model: "grok-4.5", want: []string{"chat_completions"}},
		{name: "unknown grok task permits text protocol", platform: PlatformGrok, model: "grok-private", want: []string{"chat_completions"}},
		{name: "other platform not grok media", platform: PlatformOpenAI, model: "grok-imagine-image", want: []string{"chat_completions"}},
		{name: "unknown method", platform: PlatformOpenAI, metadata: UpstreamModelMetadata{SupportedMethods: []string{"unknown"}}, want: []string{}},
		{name: "gemini methods deduplicated", platform: PlatformGemini, metadata: UpstreamModelMetadata{SupportedMethods: []string{"generateContent", "streamGenerateContent"}}, want: []string{"generate_content"}},
		{name: "multiple declared methods", platform: PlatformOpenAI, metadata: UpstreamModelMetadata{SupportedMethods: []string{"responses", "alpha_search", "chat_completions"}}, want: []string{"alpha_search", "chat_completions", "responses"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{Platform: tt.platform, Type: AccountTypeAPIKey, Credentials: tt.credentials}
			require.Equal(t, tt.want, accountCatalogModelEndpoints(account, tt.model, tt.metadata))
		})
	}
}

func TestAccountModelCatalogGrokMediaProjectionPreservesLiveEligibility(t *testing.T) {
	for _, eligible := range []bool{false, true} {
		account := &Account{Platform: PlatformGrok, Type: AccountTypeAPIKey, Extra: map[string]any{GrokMediaEligibleExtraKey: eligible}}
		catalog := &UpstreamModelCatalog{Models: []string{"grok-imagine-video"}}
		snapshot := projectAccountModelCatalog(account, catalog)
		require.Empty(t, snapshot.ExcludedModels)
		require.Len(t, snapshot.EligibleModels, 1)
		require.Equal(t, []string{"grok_media_generation"}, snapshot.EligibleModels[0].Endpoints)
		require.Equal(t, eligible, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityGrokMediaGeneration))
	}
}

func TestAccountModelCatalogOpenAIAPIKeyProtocolEvidence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra map[string]any
		want  []string
	}{
		{name: "unobserved", want: []string{"chat_completions"}},
		{name: "supported", extra: map[string]any{"openai_responses_supported": true}, want: []string{"responses"}},
		{name: "unsupported", extra: map[string]any{"openai_responses_supported": false}, want: []string{"chat_completions"}},
		{name: "force responses", extra: map[string]any{"openai_responses_mode": "force_responses", "openai_responses_supported": false}, want: []string{"responses"}},
		{name: "force chat", extra: map[string]any{"openai_responses_mode": "force_chat_completions", "openai_responses_supported": true}, want: []string{"chat_completions"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: tc.extra}
			require.Equal(t, tc.want, accountCatalogNativeTextEndpoints(account, "gpt-4o"))
			require.Equal(t, tc.want, accountCatalogModelEndpoints(account, "gpt-4o", UpstreamModelMetadata{TaskType: "text_generation"}))
			// Model-specific declarations stay authoritative over platform inference.
			require.Equal(t, []string{"messages"}, accountCatalogModelEndpoints(account, "custom", UpstreamModelMetadata{SupportedMethods: []string{"messages"}}))
		})
	}
}

func TestAccountModelCatalogMixedTasksDoNotUseTextFallback(t *testing.T) {
	for _, platform := range []string{PlatformQianwen, PlatformGrok, PlatformOpenAI, PlatformDeepseek, PlatformAnthropic, PlatformCursor, PlatformAntigravity, PlatformGemini} {
		for _, task := range []string{"automatic_speech_recognition", "speech_to_text", "text_to_speech", "audio_generation", "rerank"} {
			t.Run(platform+"/"+task, func(t *testing.T) {
				account := &Account{Platform: platform, Type: AccountTypeAPIKey}
				require.Empty(t, accountCatalogModelEndpoints(account, "native", UpstreamModelMetadata{TaskType: task, OutputModalities: []string{"text"}}))
			})
		}
	}
	for _, platform := range []string{PlatformQianwen, PlatformOpenAI} {
		account := &Account{Platform: platform, Type: AccountTypeAPIKey}
		require.Empty(t, accountCatalogModelEndpoints(account, "native", UpstreamModelMetadata{OutputModalities: []string{"text", "audio"}}))
		require.Equal(t, []string{"images"}, accountCatalogModelEndpoints(account, "native", UpstreamModelMetadata{OutputModalities: []string{"image"}}))
		require.Equal(t, []string{"embeddings"}, accountCatalogModelEndpoints(account, "native", UpstreamModelMetadata{TaskType: "embedding"}))
	}
	// A video task is projected only for the OpenAI column, whose native video
	// adapter executes on the media family; the CN platforms reach video through
	// their own native ingress and keep no catalogue family for it.
	require.Empty(t, accountCatalogModelEndpoints(&Account{Platform: PlatformQianwen, Type: AccountTypeAPIKey}, "native", UpstreamModelMetadata{TaskType: "video_generation"}))
	require.Equal(t, []string{"images"}, accountCatalogModelEndpoints(&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, "native", UpstreamModelMetadata{TaskType: "video_generation"}))
}

func TestAccountModelCatalogTextPlatformProtocols(t *testing.T) {
	for _, tc := range []struct {
		name, platform, kind, protocol string
		want                           []string
	}{
		{"qianwen chat", PlatformQianwen, AccountTypeAPIKey, APIProtocolChatCompletions, []string{"chat_completions"}},
		{"qianwen anthropic", PlatformQianwen, AccountTypeAPIKey, APIProtocolAnthropic, []string{"messages"}},
		{"qianwen responses", PlatformQianwen, AccountTypeAPIKey, APIProtocolResponses, []string{"responses"}},
		{"qianwen adaptive", PlatformQianwen, AccountTypeAPIKey, APIProtocolAdaptive, []string{"chat_completions", "messages", "responses"}},
		{"grok key", PlatformGrok, AccountTypeAPIKey, "", []string{"chat_completions"}},
		{"grok oauth", PlatformGrok, AccountTypeOAuth, "", []string{"chat_completions", "responses"}},
		{"openai key", PlatformOpenAI, AccountTypeAPIKey, "", []string{"chat_completions"}},
		{"openai oauth", PlatformOpenAI, AccountTypeOAuth, "", []string{"responses"}},
		{"gemini requires methods", PlatformGemini, AccountTypeAPIKey, "", []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{Platform: tc.platform, Type: tc.kind, Credentials: map[string]any{"api_protocol": tc.protocol}}
			require.Equal(t, tc.want, accountCatalogModelEndpoints(account, "native", UpstreamModelMetadata{TaskType: "text_generation"}))
		})
	}
}

func TestAccountModelCatalogQianwenMissingMetadataRecovery(t *testing.T) {
	account := &Account{Platform: PlatformQianwen, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"base_url": DefaultQianwenBaseURL, "api_protocol": APIProtocolChatCompletions}}
	models := []string{"qwen-1.8b-chat", "qwen-7b-chat", "qwen-14b-chat", "qwen-72b-chat", "qwen-coder-plus",
		"qwen-vl-ocr", "qwen-vl-plus", "qwen-vl-max", "qwen3-vl-flash", "qwen3-vl-plus",
		"qwen-image", "qwen-image-2.0", "qwen-image-2.0-pro", "qwen-image-max", "qwen-image-plus", "qwen-image-edit-max",
		"qwen-image-2.0-pro-2026-06-22", "qwen-image-edit-plus-2025-12-15", "qwen3-vl-flash-2026-01-22",
		"text-embedding-v3", "qwen3-embedding-8b"}
	catalog := &UpstreamModelCatalog{Models: models}
	snapshot := projectAccountModelCatalog(account, catalog)
	require.Empty(t, snapshot.ExcludedModels)
	require.Len(t, snapshot.EligibleModels, len(models))
	require.ElementsMatch(t, models, snapshot.DiscoveredModels)
	for _, model := range snapshot.EligibleModels {
		want := []string{"chat_completions"}
		if strings.HasPrefix(model.ID, "qwen-image") {
			want = []string{"images"}
		} else if strings.Contains(model.ID, "embedding") {
			want = []string{"embeddings"}
		}
		require.Equal(t, want, model.Endpoints, model.ID)
	}
	require.Nil(t, catalog.Metadata, "endpoint recovery must not fabricate capability specifications")
	// Missing task facts permit the saved text protocol without inventing a task;
	// explicit non-text declarations still keep their own endpoint family.
	require.Equal(t, []string{"chat_completions"}, accountCatalogModelEndpoints(account, "qwen-private", UpstreamModelMetadata{Category: "chat"}))
	require.Equal(t, []string{"chat_completions"}, accountCatalogModelEndpoints(account, "qwen-plus", UpstreamModelMetadata{TaskType: "unknown"}))
	require.Equal(t, []string{"embeddings"}, accountCatalogModelEndpoints(account, "qwen-plus", UpstreamModelMetadata{TaskType: "embeddings"}))

	account.Extra = map[string]any{UpstreamModelAllowlistExtraKey: AccountModelAllowlist{Enabled: true}}
	restricted := projectAccountModelCatalog(account, &UpstreamModelCatalog{Models: []string{"qwen-7b-chat"}})
	require.Len(t, restricted.EligibleModels, 1)
	require.Empty(t, restricted.ExcludedModels)
	account.Extra[UpstreamModelCatalogExtraKey] = restricted
	require.Empty(t, account.EffectiveUpstreamModelCatalog().EligibleModels)
}

func TestAccountModelCatalogQianwenUnsupportedAndUnknownTasks(t *testing.T) {
	account := &Account{Platform: PlatformQianwen, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": DefaultQianwenBaseURL}}
	catalog := &UpstreamModelCatalog{
		Models: []string{"qwen-audio-3.0-asr", "qwen-audio-3.0-realtime", "native-tts", "native-rerank", "native-video", "qwen-private", "qwen-unknown-task", "qwen1.5-72b-chat"},
		Metadata: map[string]UpstreamModelMetadata{
			"qwen-audio-3.0-asr": {TaskType: "text_generation", OutputModalities: []string{"text"}},
			"native-tts":         {TaskType: "text_to_speech"},
			"native-rerank":      {TaskType: "rerank"},
			"native-video":       {OutputModalities: []string{"video"}},
			"qwen-unknown-task":  {TaskType: "unknown"},
		},
	}
	snapshot := projectAccountModelCatalog(account, catalog)
	require.Len(t, snapshot.EligibleModels, 2)
	for _, member := range snapshot.EligibleModels {
		require.Contains(t, []string{"qwen-private", "qwen-unknown-task"}, member.ID)
		require.Equal(t, []string{"chat_completions", "messages", "responses"}, member.Endpoints)
	}
	require.Len(t, snapshot.DiscoveredModels, 8)
	require.ElementsMatch(t, []ExcludedCatalogModel{
		{ID: "qwen-audio-3.0-asr", Reason: CatalogReasonEndpointUnsupported},
		{ID: "qwen-audio-3.0-realtime", Reason: CatalogReasonEndpointUnsupported},
		{ID: "native-tts", Reason: CatalogReasonEndpointUnsupported},
		{ID: "native-rerank", Reason: CatalogReasonEndpointUnsupported},
		{ID: "native-video", Reason: CatalogReasonEndpointUnsupported},
		{ID: "qwen1.5-72b-chat", Reason: CatalogReasonRetired},
	}, snapshot.ExcludedModels)
}
