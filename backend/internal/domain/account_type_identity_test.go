package domain

import "testing"

// These fixtures pin stored aliases independently of the resolver. Include all
// product platforms, the group-only platform, unknown values and exact strings.
var identityPlatformContract = []struct {
	platform, product, hosted string
}{
	{"anthropic", "anthropic", ""},
	{"gemini", "gemini", ""},
	{"bedrock", "anthropic", "bedrock"},
	{"vertex", "gemini", "service_account"},
	{"openai", "openai", ""},
	{"antigravity", "antigravity", ""},
	{"grok", "grok", ""},
	{"kimi", "kimi", ""},
	{"zhipu", "zhipu", ""},
	{"deepseek", "deepseek", ""},
	{"minimax", "minimax", ""},
	{"opencode_go", "opencode_go", ""},
	{"cursor", "cursor", ""},
	{"qianwen", "qianwen", ""},
	{"qianfan", "qianfan", ""},
	{"hunyuan", "hunyuan", ""},
	{"mistral", "mistral", ""},
	{"volcengine", "volcengine", ""},
	{"commandcode", "commandcode", ""},
	{"composite", "composite", ""},
	{"", "", ""},
	{"unknown", "unknown", ""},
	{" bedrock ", " bedrock ", ""},
	{"VERTEX", "VERTEX", ""},
}

var identityTypeContract = []string{"", "apikey", "oauth", "setup-token", "upstream", "bedrock", "service_account", "unknown", " bedrock "}

func TestResolveAccountTypeIdentityProjections(t *testing.T) {
	covered := make(map[string]bool)
	quotaOverrides := map[[2]string]string{
		{"anthropic", "bedrock"}:      "bedrock",
		{"gemini", "service_account"}: "vertex",
	}
	for _, row := range identityPlatformContract {
		covered[row.platform] = true
		t.Run(row.platform, func(t *testing.T) {
			for _, accountType := range identityTypeContract {
				identity := ResolveAccountTypeIdentity(row.platform, accountType)
				required := accountType
				if required == "" {
					required = row.hosted
				}
				quota := row.platform
				if override, ok := quotaOverrides[[2]string{row.platform, accountType}]; ok {
					quota = override
				}
				if identity.Platform() != row.product || identity.RequiredAccountType() != required || identity.QuotaKey() != quota {
					t.Fatalf("type %q: got (%q, %q, %q), want (%q, %q, %q)", accountType, identity.Platform(), identity.RequiredAccountType(), identity.QuotaKey(), row.product, required, quota)
				}
				if CanonicalPlatform(row.platform) != row.product || HostedAccountType(row.platform) != row.hosted || HostedQuotaKey(row.platform, accountType) != quota {
					t.Fatalf("compatibility projections diverged for type %q", accountType)
				}
			}
		})
	}
	for _, platform := range ConcretePlatforms() {
		if !covered[platform] {
			t.Errorf("add platform %q to the identity contract", platform)
		}
	}
}

func TestAccountTypeIdentityMatchingContract(t *testing.T) {
	// A blank type is a wildcard only on the target. A legacy source or target
	// adds its own constraint even if an explicit target type conflicts with it.
	for _, source := range identityPlatformContract {
		t.Run(source.platform, func(t *testing.T) {
			for _, target := range identityPlatformContract {
				for _, accountType := range identityTypeContract {
					for _, required := range identityTypeContract {
						want := source.product == target.product
						for _, constraint := range []string{source.hosted, target.hosted, required} {
							if constraint != "" && constraint != accountType {
								want = false
							}
						}
						account := ResolveAccountTypeIdentity(source.platform, accountType)
						restriction := ResolveAccountTypeIdentity(target.platform, required)
						if got := account.Matches(restriction); got != want {
							t.Fatalf("(%q,%q) -> (%q,%q) = %v, want %v", source.platform, accountType, target.platform, required, got, want)
						}
						if got := MatchesPlatformIdentity(source.platform, accountType, target.platform, required); got != want {
							t.Fatalf("compatibility matcher diverged for (%q,%q) -> (%q,%q)", source.platform, accountType, target.platform, required)
						}
					}
				}
			}
		})
	}
}

func TestAccountTypeIdentityHostedValidationContract(t *testing.T) {
	allowed := map[[2]string]bool{
		{"anthropic", "bedrock"}:         true,
		{"anthropic", "service_account"}: true,
		{"bedrock", "bedrock"}:           true,
		{"gemini", "service_account"}:    true,
		{"vertex", "service_account"}:    true,
	}
	for _, row := range identityPlatformContract {
		for _, accountType := range identityTypeContract {
			want := accountType == "" || allowed[[2]string{row.platform, accountType}]
			if got := ValidateHostedRestriction(row.platform, accountType) == nil; got != want {
				t.Errorf("ValidateHostedRestriction(%q, %q) valid = %v, want %v", row.platform, accountType, got, want)
			}
		}
	}
}
