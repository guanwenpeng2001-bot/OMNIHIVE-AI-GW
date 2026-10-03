//go:build unit

package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
)

func compatibleFixture() CompatibleProvider {
	return CompatibleProvider{
		ID: "fixture-cloud", Label: "Fixture Cloud", LabelZh: "测试云", BaseSource: "preset", DefaultMode: "payg",
		EvidenceURL: "https://docs.example.com/api", EvidenceObserved: "2026-10-03",
		Modes: []CompatibleMode{{Mode: "payg", DefaultProtocol: APIProtocolChatCompletions,
			Endpoints: []CompatibleEndpoint{{Protocol: APIProtocolChatCompletions, BaseURL: "https://api.example.com/v1", Auth: "bearer"}},
			Discovery: "none", CapabilityProfile: "fixture-cloud"}},
	}
}

func TestCompatibleProviderValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*CompatibleProvider)
	}{
		{"reserved", func(provider *CompatibleProvider) { provider.ID = "openai" }},
		{"hosted alias", func(provider *CompatibleProvider) { provider.ID = "vertex" }},
		{"group", func(provider *CompatibleProvider) { provider.ID = "composite" }},
		{"id shape", func(provider *CompatibleProvider) { provider.ID = "Bad ID" }},
		{"duplicate mode", func(provider *CompatibleProvider) { provider.Modes = append(provider.Modes, provider.Modes[0]) }},
		{"duplicate protocol", func(provider *CompatibleProvider) {
			provider.Modes[0].Endpoints = append(provider.Modes[0].Endpoints, provider.Modes[0].Endpoints[0])
		}},
		{"http", func(provider *CompatibleProvider) {
			provider.Modes[0].Endpoints[0].BaseURL = "http://api.example.com/v1"
		}},
		{"userinfo", func(provider *CompatibleProvider) {
			provider.Modes[0].Endpoints[0].BaseURL = "https://key@api.example.com/v1"
		}},
		{"query", func(provider *CompatibleProvider) { provider.Modes[0].Endpoints[0].BaseURL += "?key=secret" }},
		{"fragment", func(provider *CompatibleProvider) { provider.Modes[0].Endpoints[0].BaseURL += "#fragment" }},
		{"port", func(provider *CompatibleProvider) {
			provider.Modes[0].Endpoints[0].BaseURL = "https://api.example.com:99999/v1"
		}},
		{"empty port", func(provider *CompatibleProvider) {
			provider.Modes[0].Endpoints[0].BaseURL = "https://api.example.com:/v1"
		}},
		{"empty host", func(provider *CompatibleProvider) { provider.Modes[0].Endpoints[0].BaseURL = "https:///v1" }},
		{"unknown native", func(provider *CompatibleProvider) { provider.Modes[0].Endpoints[0].Protocol = "image.generate" }},
		{"empty modes", func(provider *CompatibleProvider) { provider.Modes = nil }},
		{"empty endpoints", func(provider *CompatibleProvider) { provider.Modes[0].Endpoints = nil }},
		{"unknown mode", func(provider *CompatibleProvider) { provider.Modes[0].Mode = "subscription" }},
		{"auth", func(provider *CompatibleProvider) { provider.Modes[0].Endpoints[0].Auth = "oauth" }},
		{"preset path", func(provider *CompatibleProvider) { provider.Modes[0].Endpoints[0].Path = "/v1" }},
		{"source", func(provider *CompatibleProvider) { provider.BaseSource = "fallback" }},
		{"account preset", func(provider *CompatibleProvider) { provider.BaseSource = "account" }},
		{"default mode", func(provider *CompatibleProvider) { provider.DefaultMode = "coding" }},
		{"default protocol", func(provider *CompatibleProvider) { provider.Modes[0].DefaultProtocol = "responses" }},
		{"discovery", func(provider *CompatibleProvider) { provider.Modes[0].Discovery = "vendor" }},
		{"discovery missing protocol", func(provider *CompatibleProvider) { provider.Modes[0].Discovery = "openai_models" }},
		{"discovery foreign protocol", func(provider *CompatibleProvider) {
			provider.Modes[0].Discovery = "openai_models"
			provider.Modes[0].DiscoveryProtocol = "responses"
		}},
		{"discovery none protocol", func(provider *CompatibleProvider) { provider.Modes[0].DiscoveryProtocol = "chat_completions" }},
		{"foreign profile", func(provider *CompatibleProvider) { provider.Modes[0].CapabilityProfile = "openai-codex" }},
		{"unknown bridge", func(provider *CompatibleProvider) { provider.Modes[0].Bridges = []string{"images_to_chat"} }},
		{"missing bridge target", func(provider *CompatibleProvider) { provider.Modes[0].Bridges = []string{"chat_to_responses"} }},
		{"duplicate bridge", func(provider *CompatibleProvider) {
			provider.Modes[0].Bridges = []string{"messages_to_chat", "messages_to_chat"}
		}},
		{"date", func(provider *CompatibleProvider) { provider.EvidenceObserved = "yesterday" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := compatibleFixture()
			test.change(&provider)
			if err := ValidateCompatibleProviders([]CompatibleProvider{provider}); err == nil {
				t.Fatal("invalid row accepted")
			}
		})
	}
	provider := compatibleFixture()
	if err := ValidateCompatibleProviders([]CompatibleProvider{provider}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCompatibleProviders([]CompatibleProvider{provider, provider}); err == nil {
		t.Fatal("duplicate id accepted")
	}
}

func TestCompatibleRegistryCopiesAndBindings(t *testing.T) {
	provider := compatibleFixture()
	restore := SetCompatibleProvidersForTest([]CompatibleProvider{provider})
	defer restore()
	provider.Modes[0].Endpoints[0].BaseURL = "https://mutated.example.com"
	rows := CompatibleProviders()
	rows[0].Modes[0].Endpoints[0].Auth = "mutated"
	found, ok := LookupCompatibleProvider(provider.ID)
	if !ok || found.Modes[0].Endpoints[0].Auth != "bearer" || found.Modes[0].Endpoints[0].BaseURL != "https://api.example.com/v1" {
		t.Fatal("registry alias escaped")
	}
	mode, ok := found.LookupMode("payg")
	if !ok {
		t.Fatal("mode missing")
	}
	mode.Endpoints[0].Auth = "mutated"
	if found.Modes[0].Endpoints[0].Auth != "bearer" {
		t.Fatal("mode alias escaped")
	}
	if _, ok := found.LookupMode("coding"); ok {
		t.Fatal("invalid mode accepted")
	}
	identity := ActionIdentity{Platform: provider.ID, HostedAccountType: AccountTypeAPIKey}
	if action := PlatformAction(identity, "chat"); action.Adapter != "openai.chat" {
		t.Fatalf("chat binding: %+v", action)
	}
	for _, action := range GatewayActions() {
		if action != "chat" && PlatformAction(identity, action).Status != ActionUnverified {
			t.Fatal("implicit action:", action)
		}
	}
	descriptor, ok := LookupPlatform(provider.ID)
	if !ok || !descriptor.ChatOnly || !descriptor.PassiveMonitor || descriptor.ActiveProbe || descriptor.QuotaMonitor || descriptor.OAuth || descriptor.ModelCatalog {
		t.Fatalf("descriptor: %+v", descriptor)
	}
	if HostedQuotaKey(provider.ID, AccountTypeAPIKey) != provider.ID {
		t.Fatal("named identity lost")
	}
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeBedrock, AccountTypeServiceAccount} {
		identity.HostedAccountType = accountType
		if PlatformAction(identity, "chat").Status != ActionUnverified {
			t.Fatal("foreign credentials accepted")
		}
	}
}

func TestCompatibleNativeBridgeModeSeparation(t *testing.T) {
	provider := compatibleFixture()
	provider.Modes[0].Endpoints = append(provider.Modes[0].Endpoints, CompatibleEndpoint{Protocol: "anthropic", BaseURL: "https://api.example.com/anthropic", Auth: "x_api_key"})
	provider.Modes[0].Endpoints = append(provider.Modes[0].Endpoints, CompatibleEndpoint{Protocol: "responses", BaseURL: "https://api.example.com/v1", Auth: "bearer"})
	provider.Modes[0].Bridges = []string{"messages_to_chat", "responses_to_chat", "gemini_to_chat"}
	provider.Modes = append(provider.Modes, CompatibleMode{Mode: "coding", DefaultProtocol: "chat_completions", Endpoints: []CompatibleEndpoint{{Protocol: "chat_completions", BaseURL: "https://coding.example.com/v1", Auth: "bearer"}}, Discovery: "none", CapabilityProfile: "fixture-cloud-coding"})
	if err := ValidateCompatibleProviders([]CompatibleProvider{provider}); err != nil {
		t.Fatal(err)
	}
	mode, _ := provider.LookupMode("payg")
	endpoint, support, ok := mode.ResolveAction("messages", "")
	if !ok || support != EndpointNative || endpoint.Auth != "x_api_key" {
		t.Fatal("native preference lost")
	}
	endpoint, support, ok = mode.ResolveAction("messages", "chat_completions")
	if !ok || support != EndpointBridge || endpoint.Protocol != "chat_completions" {
		t.Fatal("explicit alternate bridge lost")
	}
	identity := ActionIdentity{Platform: provider.ID}
	if action := provider.Action("coding", identity, "responses"); action.Status != ActionUnverified {
		t.Fatal("cross-mode capability leak")
	}
	if action := provider.Action("payg", identity, "responses"); action.Adapter != "openai.responses" {
		t.Fatal("bridge binding absent")
	}
	if action := provider.Action("missing", identity, "chat"); action.Status != ActionUnverified {
		t.Fatal("unknown mode fallback")
	}
	provider.Modes[0].Bridges = append(provider.Modes[0].Bridges, "chat_to_messages")
	if err := ValidateCompatibleProviders([]CompatibleProvider{provider}); err == nil {
		t.Fatal("bridge cycle accepted")
	}
}

func TestCompatibleAzureResourceShape(t *testing.T) {
	provider := compatibleFixture()
	provider.ID, provider.BaseSource = "fixture-azure", "account"
	provider.Modes[0].CapabilityProfile = provider.ID
	provider.Modes[0].Endpoints = []CompatibleEndpoint{{Protocol: "chat_completions", Path: "/openai/v1", Auth: "api_key_header"}, {Protocol: "responses", Path: "/openai/v1", Auth: "api_key_header"}}
	if err := ValidateCompatibleProviders([]CompatibleProvider{provider}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "https://other.example/v1", "//other.example/v1", "/../v1", "/%2e%2e/v1", "/v1?api-key=secret", "/v1#fragment", "/v1\\evil", "/v1%00", "/v1%7f"} {
		provider.Modes[0].Endpoints[0].Path = path
		if err := ValidateCompatibleProviders([]CompatibleProvider{provider}); err == nil {
			t.Fatal("invalid account path accepted:", path)
		}
	}
	provider.Modes[0].Endpoints[0].Path = "/openai/v1"
	provider.Modes[0].Discovery, provider.Modes[0].DiscoveryProtocol = "openai_models", "chat_completions"
	if err := ValidateCompatibleProviders([]CompatibleProvider{provider}); err == nil {
		t.Fatal("global account discovery accepted")
	}
}

func TestCompatibleCodingOnlyAndBridgeDescriptor(t *testing.T) {
	provider := compatibleFixture()
	provider.DefaultMode, provider.Modes[0].Mode = "coding", "coding"
	provider.Modes[0].Bridges = []string{CompatibleBridgeMessagesToChat, CompatibleBridgeResponsesToChat}
	if err := ValidateCompatibleProviders([]CompatibleProvider{provider}); err != nil {
		t.Fatal(err)
	}
	if _, ok := provider.LookupMode("payg"); ok {
		t.Fatal("coding-only provider accepted PAYG")
	}
	if mode, ok := provider.ResolveMode(""); !ok || mode.Mode != "coding" {
		t.Fatal("coding default ignored")
	}
	descriptor := provider.descriptor()
	if descriptor.MessagesPOST != EndpointBridge || descriptor.ResponsesPOST != EndpointBridge || descriptor.ChatOnly || descriptor.MultiProtocol || descriptor.NativeMultiProtocolResponses {
		t.Fatalf("bridge invented native flags: %+v", descriptor)
	}
	if action := provider.Action("payg", ActionIdentity{Platform: provider.ID}, "chat"); action.Status != ActionUnverified {
		t.Fatal("coding-only provider fell back from PAYG")
	}
}

func TestCompatibleEmptyRegistryPreservesLegacy(t *testing.T) {
	admitted := CompatibleProviders()
	descriptors := PlatformDescriptors()
	emptyRestore := SetCompatibleProvidersForTest(nil)
	t.Cleanup(emptyRestore)
	if len(CompatibleProviders()) != 0 {
		t.Fatal("empty registry fixture was not installed")
	}
	if !reflect.DeepEqual(PlatformDescriptors(), platformDescriptors) || len(PlatformDescriptors()) != 19 {
		t.Fatal("legacy descriptor changed")
	}
	before := make(map[string][]ActionDescriptor)
	for _, descriptor := range platformDescriptors {
		before[descriptor.ID] = PlatformActionMatrix(ActionIdentity{Platform: descriptor.ID})
	}
	emptyRestore() // Fixture installations must not nest.
	for _, descriptor := range platformDescriptors {
		if !reflect.DeepEqual(before[descriptor.ID], PlatformActionMatrix(ActionIdentity{Platform: descriptor.ID})) {
			t.Fatal("admitted providers changed legacy actions:", descriptor.ID)
		}
	}
	restore := SetCompatibleProvidersForTest([]CompatibleProvider{compatibleFixture()})
	t.Cleanup(restore)
	for _, descriptor := range platformDescriptors {
		if !reflect.DeepEqual(before[descriptor.ID], PlatformActionMatrix(ActionIdentity{Platform: descriptor.ID})) {
			t.Fatal("legacy actions changed:", descriptor.ID)
		}
	}
	restore()
	restore() // Restore is idempotent.
	if !reflect.DeepEqual(CompatibleProviders(), admitted) || !reflect.DeepEqual(PlatformDescriptors(), descriptors) {
		t.Fatal("restore did not recover admitted registry and descriptors")
	}
	encoded, err := json.Marshal([]CompatibleProvider{compatibleFixture()})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ParseCompatibleProviders(encoded)
	if err != nil || !reflect.DeepEqual(decoded, []CompatibleProvider{compatibleFixture()}) {
		t.Fatalf("JSON roundtrip: %v", err)
	}
}

func TestCompatibleAllExplicitBridges(t *testing.T) {
	for _, test := range []struct{ bridge, action, target, adapter string }{
		{CompatibleBridgeMessagesToChat, "messages", "chat_completions", "openai.messages"},
		{CompatibleBridgeResponsesToChat, "responses", "chat_completions", "openai.responses"},
		{CompatibleBridgeGeminiToChat, "generate_content", "chat_completions", "gateway.generate_content_compat"},
		{CompatibleBridgeChatToMessages, "chat", "anthropic", "openai.chat"},
		{CompatibleBridgeResponsesToMessages, "responses", "anthropic", "openai.responses"},
		{CompatibleBridgeGeminiToMessages, "generate_content", "anthropic", "gateway.generate_content_compat"},
		{CompatibleBridgeChatToResponses, "chat", "responses", "openai.chat"},
		{CompatibleBridgeMessagesToResponses, "messages", "responses", "openai.messages"},
		{CompatibleBridgeGeminiToResponses, "generate_content", "responses", "gateway.generate_content_compat"},
	} {
		t.Run(test.bridge, func(t *testing.T) {
			provider := compatibleFixture()
			provider.Modes[0].DefaultProtocol = test.target
			provider.Modes[0].Endpoints[0].Protocol = test.target
			provider.Modes[0].Bridges = []string{test.bridge}
			if err := ValidateCompatibleProviders([]CompatibleProvider{provider}); err != nil {
				t.Fatal(err)
			}
			endpoint, support, ok := provider.Modes[0].ResolveAction(test.action, "adaptive")
			if !ok || support != EndpointBridge || endpoint.Protocol != test.target {
				t.Fatalf("bridge resolution: %+v %s %v", endpoint, support, ok)
			}
			if action := provider.Action("", ActionIdentity{Platform: provider.ID}, test.action); action.Adapter != test.adapter {
				t.Fatalf("bridge adapter: %+v", action)
			}
			if _, _, ok := provider.Modes[0].ResolveAction(test.action, "unknown"); ok {
				t.Fatal("unknown selection accepted")
			}
			if action := provider.Action("", ActionIdentity{Platform: provider.ID, Profile: "openai"}, test.action); action.Status != ActionUnverified {
				t.Fatal("foreign identity profile accepted")
			}
		})
	}
}

func TestCompatibleDeterministicActionAndDescriptor(t *testing.T) {
	provider := compatibleFixture()
	provider.Modes[0].Endpoints = []CompatibleEndpoint{
		{Protocol: "responses", BaseURL: "https://api.example.com/responses", Auth: "bearer"},
		{Protocol: "anthropic", BaseURL: "https://api.example.com/messages", Auth: "x_api_key"},
		{Protocol: "chat_completions", BaseURL: "https://api.example.com/v1", Auth: "bearer"},
	}
	provider.Modes[0].Bridges = []string{CompatibleBridgeGeminiToResponses, CompatibleBridgeGeminiToMessages}
	provider.Modes[0].Discovery, provider.Modes[0].DiscoveryProtocol = "openai_models", "responses"
	if err := ValidateCompatibleProviders([]CompatibleProvider{provider}); err != nil {
		t.Fatal(err)
	}
	endpoint, _, ok := provider.Modes[0].ResolveAction("generate_content", "")
	if !ok || endpoint.Protocol != "anthropic" {
		t.Fatal("fallback depends on JSON endpoint order")
	}
	provider.Modes[0].DefaultProtocol = "responses"
	endpoint, _, _ = provider.Modes[0].ResolveAction("generate_content", "")
	if endpoint.Protocol != "responses" {
		t.Fatal("default protocol not preferred")
	}
	descriptor := provider.descriptor()
	if !descriptor.ModelCatalog || !descriptor.MultiProtocol || !descriptor.NativeMultiProtocolResponses || descriptor.ChatOnly || descriptor.MessagesPOST != EndpointNative || descriptor.ResponsesPOST != EndpointNative {
		t.Fatalf("native descriptor: %+v", descriptor)
	}
	provider.Modes[0].Bridges = []string{CompatibleBridgeMessagesToChat, CompatibleBridgeResponsesToMessages}
	if err := ValidateCompatibleProviders([]CompatibleProvider{provider}); err == nil {
		t.Fatal("chained bridge declarations accepted")
	}
}

func TestCompatibleJSONFilesAndStrictSchema(t *testing.T) {
	provider := compatibleFixture()
	encoded, err := json.Marshal([]CompatibleProvider{provider})
	if err != nil {
		t.Fatal(err)
	}
	for _, malformed := range []string{
		"null", "{}", "[] []", "[", strings.Replace(string(encoded), `"id":`, `"unknown":true,"id":`, 1),
		strings.Replace(string(encoded), `"id":`, `"required_account_fields":["deployment_id"],"id":`, 1),
	} {
		if _, err := ParseCompatibleProviders([]byte(malformed)); err == nil {
			t.Fatalf("invalid JSON schema accepted: %s", malformed)
		}
	}
	files := fstest.MapFS{
		"data/compatible_providers/first.json":  &fstest.MapFile{Data: encoded},
		"data/compatible_providers/second.json": &fstest.MapFile{Data: encoded},
	}
	if _, err := loadCompatibleProviders(files); err == nil {
		t.Fatal("cross-file duplicate id accepted")
	}
	provider.ID = "earlier-cloud"
	other, _ := json.Marshal([]CompatibleProvider{provider})
	files["data/compatible_providers/second.json"].Data = other
	rows, err := loadCompatibleProviders(files)
	if err != nil || len(rows) != 2 || rows[0].ID != "earlier-cloud" {
		t.Fatalf("ID sorting: %+v %v", rows, err)
	}
	restore := SetCompatibleProvidersForTest(rows)
	defer restore()
	ids := PlatformsWith(func(descriptor PlatformDescriptor) bool {
		return descriptor.ID == "earlier-cloud" || descriptor.ID == "fixture-cloud"
	})
	if !reflect.DeepEqual(ids, []string{"earlier-cloud", "fixture-cloud"}) {
		t.Fatalf("selectors: %v", ids)
	}
	files["data/compatible_providers/second.json"].Data = []byte("null")
	if _, err := loadCompatibleProviders(files); err == nil || !strings.Contains(err.Error(), "second.json") {
		t.Fatal("missing invalid-file diagnostic")
	}
}

func TestCompatibleRegistryConcurrentReadsAndRestore(t *testing.T) {
	provider := compatibleFixture()
	provider.Modes[0].Bridges = []string{CompatibleBridgeMessagesToChat}
	restore := SetCompatibleProvidersForTest([]CompatibleProvider{provider})
	defer restore()
	var readers sync.WaitGroup
	for reader := 0; reader < 8; reader++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for iteration := 0; iteration < 100; iteration++ {
				found, ok := LookupCompatibleProvider(provider.ID)
				if ok {
					if found.Modes[0].Bridges[0] != CompatibleBridgeMessagesToChat {
						t.Error("reader observed mutation")
					}
					found.Modes[0].Bridges[0] = "mutated"
				}
				_ = PlatformDescriptors()
			}
		}()
	}
	restore()
	readers.Wait()
	if _, ok := LookupCompatibleProvider(provider.ID); ok {
		t.Fatal("fixture survived restore")
	}
}
