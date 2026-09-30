package domain

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// Exports the real authority for Vitest without a production generator or fixture.
func TestExportPlatformContract(t *testing.T) {
	if os.Getenv("SUB2API_PLATFORM_CONTRACT_TEST_EXTRA") == "1" {
		original := platformDescriptors
		t.Cleanup(func() { platformDescriptors = original })
		platformDescriptors = append(PlatformDescriptors(), PlatformDescriptor{ID: "test-only-provider"})
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
	platformDescriptors = append(PlatformDescriptors(), PlatformDescriptor{ID: "test-only-provider", ActiveProbe: true})
	if !IsConcretePlatform("test-only-provider") {
		t.Fatal("lookup is not exhaustive")
	}
	ids := ConcretePlatforms()
	if ids[len(ids)-1] != "test-only-provider" {
		t.Fatal("iteration is not exhaustive")
	}
	probes := PlatformsWith(func(p PlatformDescriptor) bool { return p.ActiveProbe })
	if probes[len(probes)-1] != "test-only-provider" {
		t.Fatal("capability view is not exhaustive")
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
		{PlatformGemini, EndpointRejected, EndpointRejected, false},
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
		{PlatformMistral, EndpointRejected, EndpointRejected, false},
		{PlatformComposite, EndpointRejected, EndpointRejected, false},
		{"unknown", EndpointRejected, EndpointRejected, false},
	} {
		p, _ := LookupPlatform(tt.id)
		if p.ResponsesPOST != tt.post || p.ResponsesWS != tt.ws || p.ResponsesCompact != tt.compact {
			t.Fatalf("%s: POST=%s WS=%s compact=%v", tt.id, p.ResponsesPOST, p.ResponsesWS, p.ResponsesCompact)
		}
	}
}
