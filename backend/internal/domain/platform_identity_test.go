package domain

import "testing"

func TestHostedPlatformIdentityCompatibility(t *testing.T) {
	for _, example := range []struct {
		name, platform, accountType, target, required string
		want                                          bool
	}{
		{"legacy bedrock read", "bedrock", "bedrock", "anthropic", "", true},
		{"canonical bedrock old route", "anthropic", "bedrock", "bedrock", "", true},
		{"old route stays restricted", "anthropic", "apikey", "bedrock", "", false},
		{"explicit restriction", "anthropic", "apikey", "anthropic", "bedrock", false},
		{"malformed legacy account", "bedrock", "apikey", "anthropic", "", false},
		{"vertex legacy read", "vertex", "service_account", "gemini", "", true},
		{"vertex route stays restricted", "gemini", "oauth", "vertex", "", false},
		{"vertex anthropic preserved", "anthropic", "service_account", "anthropic", "service_account", true},
		{"unrelated platform", "openai", "apikey", "anthropic", "", false},
	} {
		t.Run(example.name, func(t *testing.T) {
			if got := MatchesPlatformIdentity(example.platform, example.accountType, example.target, example.required); got != example.want {
				t.Fatalf("identity match = %v, want %v", got, example.want)
			}
		})
	}
}

func TestCanonicalPlatformIsIdempotent(t *testing.T) {
	for _, platform := range []string{"bedrock", "vertex", "anthropic", "gemini", "openai", "", "unknown"} {
		canonical := CanonicalPlatform(platform)
		if CanonicalPlatform(canonical) != canonical {
			t.Fatalf("projection is not idempotent for %q", platform)
		}
	}
}
