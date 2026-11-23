package service

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func BenchmarkPricingExtractBaseName(b *testing.B) {
	svc := NewPricingService(nil, nil)
	for b.Loop() {
		for _, model := range []string{"claude-opus-4-5-20251101", "gpt-mock", "gemini-3.6-flash-high", "accounts/test/models/model-v1:0"} {
			if result := svc.extractBaseName(model); result == "" {
				b.Fatal("empty base name")
			}
		}
	}
}

func TestPricingBaseNameAllocationBudget(t *testing.T) {
	svc := NewPricingService(nil, nil)
	if got := testing.AllocsPerRun(100, func() { svc.extractBaseName("gpt-mock") }); got > 1 {
		t.Fatalf("plain model base name allocated %.0f times", got)
	}
}

func BenchmarkPricingIdentifiedUnknownModel(b *testing.B) {
	svc := NewPricingService(nil, nil)
	svc.pricingData = make(map[string]*LiteLLMModelPricing, 4000)
	for i := range 4000 {
		svc.pricingData[fmt.Sprintf("vendor-model-%d-20260101", i)] = &LiteLLMModelPricing{}
	}
	svc.rebuildBaseNameIndex()
	for b.Loop() {
		if svc.GetIdentifiedModelPricing("unknown-model") != nil {
			b.Fatal("unknown model was priced")
		}
	}
}

func TestPricingIdentifiedUnknownModelUsesCatalogIndex(t *testing.T) {
	svc := NewPricingService(nil, nil)
	svc.pricingData = make(map[string]*LiteLLMModelPricing, 1000)
	for i := range 1000 {
		svc.pricingData[fmt.Sprintf("vendor-model-%d-20260101", i)] = &LiteLLMModelPricing{}
	}
	svc.rebuildBaseNameIndex()
	if got := testing.AllocsPerRun(100, func() { svc.GetIdentifiedModelPricing("unknown-model") }); got > 6 {
		t.Fatalf("unknown model lookup allocated %.0f times; catalog index is not being used", got)
	}
}

func TestPricingBaseNameIndexTracksReloadsAndFixtureFallback(t *testing.T) {
	first := &LiteLLMModelPricing{InputCostPerToken: 1}
	second := &LiteLLMModelPricing{InputCostPerToken: 2}
	svc := NewPricingService(nil, nil)
	svc.pricingData = map[string]*LiteLLMModelPricing{"known-20260101": first}
	svc.rebuildBaseNameIndex()
	if got := svc.GetIdentifiedModelPricing("known"); got != first {
		t.Fatal("initial base-name lookup missed")
	}
	svc.replacePricingData(map[string]*LiteLLMModelPricing{"known-20260101": second})
	if got := svc.GetIdentifiedModelPricing("known"); got != second {
		t.Fatal("replacement catalog used stale base-name index")
	}
	fixture := NewPricingService(nil, nil)
	fixture.pricingData = map[string]*LiteLLMModelPricing{"known-20260101": first}
	if got := fixture.GetIdentifiedModelPricing("known"); got != first {
		t.Fatal("direct fixture did not use legacy scan")
	}
	delete(svc.pricingData, "known-20260101")
	svc.pricingData["other-20260101"] = first
	svc.rebuildBaseNameIndex()
	if got := svc.GetIdentifiedModelPricing("known"); got != nil {
		t.Fatal("rebuilt catalog used stale base-name index")
	}
	if got := svc.GetIdentifiedModelPricing("other"); got != first {
		t.Fatal("new catalog key missing from rebuilt base-name index")
	}
}

func TestPricingIdentityIndexTracksReloadAndFrozenPriceMap(t *testing.T) {
	svc := NewPricingService(nil, nil)
	svc.replacePricingData(map[string]*LiteLLMModelPricing{
		"provider-a/same-20260101": {InputCostPerToken: 1},
		"provider-b/same-20260201": {InputCostPerToken: 2},
	})
	check := func(prices *PricingService, provider, want string, wantError bool) {
		t.Helper()
		got, err := prices.BoundPricingModel(provider, "same")
		if wantError {
			if err == nil {
				t.Fatalf("provider %q unexpectedly bound to %q", provider, got)
			}
			return
		}
		if err != nil || got != want {
			t.Fatalf("provider %q bound to %q (%v), want %q", provider, got, err, want)
		}
	}
	check(svc, "provider-a", "provider-a/same-20260101", false)
	check(svc, "provider-b", "provider-b/same-20260201", false)
	check(svc, "", "", true)
	check(svc, "provider-c", "", true)

	frozen := frozenBillingService(&BillingService{pricingService: svc}, nil, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	svc.replacePricingData(map[string]*LiteLLMModelPricing{
		"provider-b/same-20260301": {InputCostPerToken: 3},
	})
	check(svc, "provider-b", "provider-b/same-20260301", false)
	check(svc, "", "provider-b/same-20260301", false)
	check(svc, "provider-a", "", true)
	check(frozen.pricingService, "provider-a", "provider-a/same-20260101", false)
	check(frozen.pricingService, "provider-b", "provider-b/same-20260201", false)
	check(frozen.pricingService, "", "", true)
}

func TestPricingBaseNameMatchesLegacyImplementation(t *testing.T) {
	legacy := func(model string) string {
		parts := strings.Split(model, "-")
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			if len(part) == 8 && isNumeric(part) {
				continue
			}
			if strings.Contains(part, ":") {
				continue
			}
			result = append(result, part)
		}
		return strings.Join(result, "-")
	}
	svc := NewPricingService(nil, nil)
	alphabet := []string{"", "-", "a", "v1:0", "20250101", "1", ":", "12345678", "x-y"}
	for i := range 1000 {
		var input strings.Builder
		for j := 0; j <= i%6; j++ {
			if j > 0 {
				input.WriteByte('-')
			}
			input.WriteString(alphabet[(i*(j+3)+j*7)%len(alphabet)])
		}
		if got, want := svc.extractBaseName(input.String()), legacy(input.String()); !reflect.DeepEqual(got, want) {
			t.Fatalf("extractBaseName(%q) = %q, want %q", input.String(), got, want)
		}
	}
}

func TestPricingBaseNameCompatibility(t *testing.T) {
	svc := NewPricingService(nil, nil)
	for input, want := range map[string]string{
		"gpt-mock":                 "gpt-mock",
		"claude-opus-4-5-20251101": "claude-opus-4-5",
		"model-v1:0":               "model",
		"model-20250101-v1:0":      "model",
		"20250101":                 "",
		"-20250101-":               "-",
		strings.Repeat("a", 1024):  strings.Repeat("a", 1024),
	} {
		if got := svc.extractBaseName(input); got != want {
			t.Errorf("extractBaseName(%q) = %q, want %q", input, got, want)
		}
	}
}
