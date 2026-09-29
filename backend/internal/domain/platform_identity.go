package domain

import "fmt"

// AccountTypeIdentity is a read-only projection of a persisted platform/type pair.
// Account types are evidence, not defaults: Matches never fills in a missing
// account credential type. RequiredAccountType supplies legacy defaults only for
// group/route restrictions. Resolution does not validate or rewrite stored data.
type AccountTypeIdentity struct {
	platform    string
	accountType string
	product     string
	hostedType  string
}

// ResolveAccountTypeIdentity is the semantic source for product normalization,
// hosted restrictions, account matching and quota dimensions. Inputs are exact:
// boundary-specific whitespace/case normalization remains with the caller.
// SQL projections in repository/platform_identity.go, account_repo.go and
// migrations/266_hosted_account_identity.sql mirror this read contract; migration
// activation, write triggers and catalog generations remain separate concerns.
func ResolveAccountTypeIdentity(platform, accountType string) AccountTypeIdentity {
	identity := AccountTypeIdentity{platform: platform, accountType: accountType, product: platform}
	switch platform {
	case PlatformBedrock:
		identity.product, identity.hostedType = PlatformAnthropic, AccountTypeBedrock
	case "vertex":
		identity.product, identity.hostedType = PlatformGemini, AccountTypeServiceAccount
	}
	return identity
}

// Platform returns the product platform without changing the persisted identity.
func (i AccountTypeIdentity) Platform() string { return i.product }

// RequiredAccountType preserves an explicit group/route restriction, falling back
// to the legacy hosting platform only when that restriction is empty.
func (i AccountTypeIdentity) RequiredAccountType() string {
	if i.accountType != "" {
		return i.accountType
	}
	return i.hostedType
}

// QuotaKey preserves legacy budget/cache/settlement keys, even for inconsistent
// input pairs. Resolving an identity is not a migration or a validation step.
func (i AccountTypeIdentity) QuotaKey() string {
	if i.platform == PlatformAnthropic && i.accountType == AccountTypeBedrock {
		return PlatformBedrock
	}
	if i.platform == PlatformGemini && i.accountType == AccountTypeServiceAccount {
		return "vertex"
	}
	return i.platform
}

// Matches treats the receiver as an account and target as a group/route. Both
// legacy platform restrictions and the explicit target type must hold, including
// when a malformed target has conflicting legacy and explicit restrictions.
func (i AccountTypeIdentity) Matches(target AccountTypeIdentity) bool {
	return i.product == target.product &&
		(i.hostedType == "" || i.accountType == i.hostedType) &&
		(target.hostedType == "" || i.accountType == target.hostedType) &&
		(target.accountType == "" || i.accountType == target.accountType)
}

// CanonicalPlatform maps legacy hosting identities to their product platform.
// It is a read projection: callers must retain the persisted account type and
// credentials, and must not rewrite historical usage or amounts.
func CanonicalPlatform(platform string) string {
	return ResolveAccountTypeIdentity(platform, "").Platform()
}

// HostedAccountType returns the type restriction implied by a legacy platform.
// In particular, converting a Bedrock-only route to Anthropic must not make
// ordinary Anthropic accounts eligible for that route.
func HostedAccountType(platform string) string {
	return ResolveAccountTypeIdentity(platform, "").RequiredAccountType()
}

// HostedQuotaKey preserves existing quota rows, cache keys and settlement keys.
// It is an internal budget identity, not an additional product platform.
func HostedQuotaKey(platform, accountType string) string {
	return ResolveAccountTypeIdentity(platform, accountType).QuotaKey()
}

// MatchesPlatformIdentity compares product identity while preserving restrictions
// encoded by old hosted platform values. An explicit type further narrows it.
func MatchesPlatformIdentity(platform, accountType, targetPlatform, requiredType string) bool {
	return ResolveAccountTypeIdentity(platform, accountType).Matches(ResolveAccountTypeIdentity(targetPlatform, requiredType))
}

// ValidateHostedRestriction rejects combinations that would broaden a legacy
// hosting-only group or route. Other account types remain outside this field.
func ValidateHostedRestriction(platform, accountType string) error {
	identity := ResolveAccountTypeIdentity(platform, accountType)
	legacyType := identity.hostedType
	if legacyType != "" && accountType != "" && accountType != legacyType {
		return fmt.Errorf("account_type conflicts with hosted platform")
	}
	if accountType == "" {
		return nil
	}
	switch accountType {
	case AccountTypeBedrock:
		if identity.Platform() == PlatformAnthropic {
			return nil
		}
	case AccountTypeServiceAccount:
		if canonical := identity.Platform(); canonical == PlatformAnthropic || canonical == PlatformGemini {
			return nil
		}
	}
	return fmt.Errorf("unsupported platform and hosted account_type combination")
}
