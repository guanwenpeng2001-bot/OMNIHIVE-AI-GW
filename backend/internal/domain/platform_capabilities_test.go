package domain

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"
)

// Exports the real authority for Vitest without a production generator or fixture.
func TestExportPlatformContract(t *testing.T) {
	if os.Getenv("SUB2API_PLATFORM_CONTRACT_TEST_EXTRA") == "1" {
		original := platformDescriptors
		t.Cleanup(func() { platformDescriptors = original })
		platformDescriptors = append(append([]PlatformDescriptor(nil), original...), PlatformDescriptor{ID: "test-only-provider"})
	}
	encoded, err := json.Marshal(PlatformDescriptors())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("PLATFORM_CONTRACT=%s\n", encoded)
}

func TestPlatformCatalog(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range PlatformDescriptors() {
		if p.ID == "" || seen[p.ID] || p.ID == PlatformComposite {
			t.Fatalf("invalid descriptor: %+v", p)
		}
		seen[p.ID] = true
		if !IsConcretePlatform(p.ID) || !IsGroupPlatform(p.ID) {
			t.Fatal(p.ID)
		}
	}
	for _, id := range []string{"", "unknown", "kiro", PlatformComposite} {
		if IsConcretePlatform(id) {
			t.Fatalf("concrete platform accepted: %q", id)
		}
	}
	if !IsGroupPlatform(PlatformComposite) || IsGroupPlatform("unknown") {
		t.Fatal("group identity")
	}
	descriptors := PlatformDescriptors()
	descriptors[0].ID = "changed"
	if IsConcretePlatform("changed") {
		t.Fatal("descriptor alias escaped")
	}
	ids := ConcretePlatforms()
	ids[0] = "changed"
	if ConcretePlatforms()[0] == "changed" {
		t.Fatal("platform slice alias escaped")
	}
}

func TestPlatformViewsIncludeTemporaryDescriptor(t *testing.T) {
	original := platformDescriptors
	t.Cleanup(func() { platformDescriptors = original })
	before := ConcretePlatforms()
	beforeProbes := PlatformsWith(func(p PlatformDescriptor) bool { return p.ActiveProbe })
	platformDescriptors = append(append([]PlatformDescriptor(nil), original...), PlatformDescriptor{ID: "test-only-provider", ActiveProbe: true})
	if !IsConcretePlatform("test-only-provider") {
		t.Fatal("lookup is not exhaustive")
	}
	ids := ConcretePlatforms()
	if len(ids) != len(before)+1 || !slices.Contains(ids, "test-only-provider") {
		t.Fatal("iteration is not exhaustive")
	}
	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate platform in iteration: %s", id)
		}
		seen[id] = true
	}
	for _, id := range before {
		if !seen[id] {
			t.Fatalf("temporary descriptor displaced platform: %s", id)
		}
	}
	probes := PlatformsWith(func(p PlatformDescriptor) bool { return p.ActiveProbe })
	if len(probes) != len(beforeProbes)+1 || !slices.Contains(probes, "test-only-provider") {
		t.Fatal("capability view is not exhaustive")
	}
	for _, id := range beforeProbes {
		if !slices.Contains(probes, id) {
			t.Fatalf("temporary descriptor displaced probe platform: %s", id)
		}
	}
}

func TestPlatformResponsesSurfaces(t *testing.T) {
	for _, tt := range []struct {
		id       string
		post, ws EndpointSupport
		compact  bool
	}{
		{PlatformOpenAI, EndpointNative, EndpointNative, true},
		// Grok compact: native xAI POST /v1/responses/compact (docs.x.ai
		// Context Compaction, retrieved 2026-09-21), served through the
		// existing Grok compact bridge on the responses trunk.
		{PlatformGrok, EndpointNative, EndpointBridge, true},
		{PlatformGemini, EndpointBridge, EndpointRejected, false},
		{PlatformAnthropic, EndpointBridge, EndpointRejected, false},
		{PlatformCursor, EndpointBridge, EndpointRejected, false},
		{PlatformKimi, EndpointNative, EndpointRejected, false},
		{PlatformZhipu, EndpointNative, EndpointRejected, false},
		{PlatformCommandCode, EndpointNative, EndpointRejected, false},
		// The late CN/TokenHub entries publish the native Responses API on their
		// OpenAI-shaped trunk (Ark docs/82379/1298459, Qianfan /v2/responses live
		// probe and TokenHub 1823/133813, all captured 2026-09-21), while Mistral
		// publishes none.
		{PlatformVolcengine, EndpointNative, EndpointRejected, false},
		{PlatformQianfan, EndpointNative, EndpointRejected, false},
		{PlatformHunyuan, EndpointNative, EndpointRejected, false},
		{PlatformMistral, EndpointBridge, EndpointRejected, false},
		{PlatformComposite, EndpointRejected, EndpointRejected, false},
		{"unknown", EndpointRejected, EndpointRejected, false},
	} {
		p, _ := LookupPlatform(tt.id)
		if p.ResponsesPOST != tt.post || p.ResponsesWS != tt.ws || p.ResponsesCompact != tt.compact {
			t.Fatalf("%s: POST=%s WS=%s compact=%v", tt.id, p.ResponsesPOST, p.ResponsesWS, p.ResponsesCompact)
		}
	}
}

// Pin the admitted B corpus independently of descriptor construction. Additional
// providers may be admitted without changing these named provider contracts.
func TestCompatibleMarketplacePlatformSelectors(t *testing.T) {
	for _, tt := range []struct {
		id                                      string
		messages, responses                     EndpointSupport
		multiProtocol, nativeResponses, catalog bool
	}{
		{"astron_coding", EndpointNative, EndpointNative, true, true, false},
		{"atlascloud", EndpointNative, EndpointNative, true, true, false},
		{"azure_openai", EndpointBridge, EndpointNative, true, true, false},
		{"compshare", EndpointNative, EndpointNative, true, true, true},
		{"compshare_coding", EndpointNative, EndpointNative, true, true, false},
		{"kat_coder", EndpointNative, EndpointBridge, true, false, false},
		{"modelscope", EndpointBridge, EndpointBridge, false, false, true},
		{"novita", EndpointBridge, EndpointBridge, false, false, true},
		{"nvidia", EndpointBridge, EndpointBridge, false, false, true},
		{"ppio", EndpointNative, EndpointBridge, true, false, false},
		{"qiniu", EndpointNative, EndpointNative, true, true, true},
		{"siliconflow_cn", EndpointNative, EndpointBridge, true, false, true},
		{"siliconflow_global", EndpointNative, EndpointBridge, true, false, true},
		{"together", EndpointBridge, EndpointBridge, false, false, true},
	} {
		t.Run(tt.id, func(t *testing.T) {
			if _, ok := LookupCompatibleProvider(tt.id); !ok {
				t.Fatal("admitted provider missing from registry")
			}
			p, ok := LookupPlatform(tt.id)
			want := PlatformDescriptor{ID: tt.id, MessagesPOST: tt.messages, ResponsesPOST: tt.responses,
				MultiProtocol: tt.multiProtocol, NativeMultiProtocolResponses: tt.nativeResponses,
				ModelCatalog: tt.catalog, PassiveMonitor: true}
			if !ok || p != want {
				t.Fatalf("descriptor = %+v, want %+v", p, want)
			}
			for _, selector := range []struct {
				name    string
				include func(PlatformDescriptor) bool
				want    bool
			}{
				{"concrete", func(PlatformDescriptor) bool { return true }, true},
				{"passive", func(p PlatformDescriptor) bool { return p.PassiveMonitor }, true},
				{"catalog", func(p PlatformDescriptor) bool { return p.ModelCatalog }, tt.catalog},
				{"multi-protocol", func(p PlatformDescriptor) bool { return p.MultiProtocol }, tt.multiProtocol},
				{"active", func(p PlatformDescriptor) bool { return p.ActiveProbe }, false},
				{"quota", func(p PlatformDescriptor) bool { return p.QuotaMonitor }, false},
				{"oauth", func(p PlatformDescriptor) bool { return p.OAuth }, false},
			} {
				count := 0
				for _, id := range PlatformsWith(selector.include) {
					if id == tt.id {
						count++
					}
				}
				if (count == 1) != selector.want || count > 1 {
					t.Errorf("%s selector count = %d, want membership %v", selector.name, count, selector.want)
				}
			}
		})
	}
}
