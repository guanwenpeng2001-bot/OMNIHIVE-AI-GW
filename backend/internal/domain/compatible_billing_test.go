//go:build unit

package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCompatibleBillingValidationAndCopies(t *testing.T) {
	provider, _ := LookupCompatibleProvider("ccsub")
	original := copyCompatibleProvider(provider)
	for _, change := range []func(*CompatibleBilling){
		func(b *CompatibleBilling) { b.Selection = "selected" },
		func(b *CompatibleBilling) { b.Behavior = "unlimited" },
		func(b *CompatibleBilling) { b.Summary = " " },
		func(b *CompatibleBilling) { b.SummaryZh = "" },
		func(b *CompatibleBilling) { b.EvidenceURL = "javascript:alert(1)" },
		func(b *CompatibleBilling) { b.EvidenceObserved = "yesterday" },
	} {
		mutated := copyCompatibleProvider(provider)
		change(mutated.Billing)
		if ValidateCompatibleProviders([]CompatibleProvider{mutated}) == nil {
			t.Fatal("malformed billing accepted")
		}
	}
	encoded, _ := json.Marshal([]CompatibleProvider{provider})
	for _, invalid := range []string{
		strings.Replace(string(encoded), `"same_key":true`, `"same_key":"true"`, 1),
		strings.Replace(string(encoded), `"selection":"described"`, `"selection":"described","account_mode":"coding"`, 1),
	} {
		if _, err := ParseCompatibleProviders([]byte(invalid)); err == nil {
			t.Fatal("invalid descriptor JSON accepted")
		}
	}
	provider.Billing.Summary = "changed"
	*provider.Billing.SameKey = false
	*provider.Billing.SameBases = false
	fresh, _ := LookupCompatibleProvider("ccsub")
	if !reflect.DeepEqual(original, fresh) {
		t.Fatal("lookup alias escaped")
	}
	for _, row := range CompatibleProviders() {
		if row.ID == "ccsub" {
			*row.Billing.SameKey = false
		}
	}
	fresh, _ = LookupCompatibleProvider("ccsub")
	if !*fresh.Billing.SameKey {
		t.Fatal("list alias escaped")
	}
	if _, ok := fresh.ResolveMode("coding"); ok {
		t.Fatal("billing became mode")
	}
	before, _ := fresh.ResolveMode("")
	fresh.Billing = nil
	after, _ := fresh.ResolveMode("")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("billing changes routing")
	}
	legacy := compatibleFixture()
	encoded, _ = json.Marshal([]CompatibleProvider{legacy})
	if strings.Contains(string(encoded), "billing") {
		t.Fatal("absent descriptor materialized")
	}
	if _, err := ParseCompatibleProviders(encoded); err != nil {
		t.Fatal(err)
	}
}

func TestCompatiblePlanContracts(t *testing.T) {
	for _, test := range []struct {
		id, mode  string
		protocols []string
	}{
		{"atlascloud", "coding", []string{"chat_completions", "anthropic"}},
		{"qiniu", "subscription", []string{"chat_completions"}},
		{"astron_token", "coding", []string{"chat_completions", "anthropic"}},
		{"mimo_token_sg", "coding", []string{"chat_completions", "anthropic", "responses"}},
		{"mimo_token_eu", "coding", []string{"chat_completions", "anthropic", "responses"}},
	} {
		provider, ok := LookupCompatibleProvider(test.id)
		if !ok {
			t.Fatal(test.id)
		}
		mode, ok := provider.LookupMode(test.mode)
		if !ok {
			t.Fatal(test)
		}
		if len(mode.Endpoints) != len(test.protocols) {
			t.Fatal("unexpected native endpoint", test.id)
		}
		for _, protocol := range test.protocols {
			if _, ok := mode.LookupEndpoint(protocol); !ok {
				t.Fatal(protocol)
			}
		}
		for _, unknown := range []string{"unknown", "Subscription", "subscription ", "monthly_credits"} {
			if _, ok := provider.ResolveMode(unknown); ok {
				t.Fatal("unknown mode accepted", unknown)
			}
		}
	}
	for _, id := range []string{"atlascloud", "qiniu", "ccsub"} {
		provider, _ := LookupCompatibleProvider(id)
		if provider.DefaultMode != "payg" {
			t.Fatal("default changed", id)
		}
	}
}
