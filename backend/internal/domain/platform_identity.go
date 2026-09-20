package domain

import "fmt"

// CanonicalPlatform maps legacy hosting identities to their product platform.
// It is a read projection: callers must retain the persisted account type and
// credentials, and must not rewrite historical usage or amounts.
func CanonicalPlatform(platform string) string {
	switch platform {
	case PlatformBedrock:
		return PlatformAnthropic
	case "vertex":
		return PlatformGemini
	default:
		return platform
	}
}

// HostedAccountType returns the type restriction implied by a legacy platform.
// In particular, converting a Bedrock-only route to Anthropic must not make
// ordinary Anthropic accounts eligible for that route.
func HostedAccountType(platform string) string {
	switch platform {
	case PlatformBedrock:
		return AccountTypeBedrock
	case "vertex":
		return AccountTypeServiceAccount
	default:
		return ""
	}
}

// HostedQuotaKey preserves existing quota rows, cache keys and settlement keys.
// It is an internal budget identity, not an additional product platform.
func HostedQuotaKey(platform, accountType string) string {
	if platform == PlatformAnthropic && accountType == AccountTypeBedrock {
		return PlatformBedrock
	}
	if platform == PlatformGemini && accountType == AccountTypeServiceAccount {
		return "vertex"
	}
	return platform
}

// MatchesPlatformIdentity compares product identity while preserving restrictions
// encoded by old hosted platform values. An explicit type further narrows it.
func MatchesPlatformIdentity(platform, accountType, targetPlatform, requiredType string) bool {
	if CanonicalPlatform(platform) != CanonicalPlatform(targetPlatform) {
		return false
	}
	if legacyType := HostedAccountType(platform); legacyType != "" && accountType != legacyType {
		return false
	}
	if legacyType := HostedAccountType(targetPlatform); legacyType != "" && accountType != legacyType {
		return false
	}
	return requiredType == "" || accountType == requiredType
}

// ValidateHostedRestriction rejects combinations that would broaden a legacy
// hosting-only group or route. Other account types remain outside this field.
func ValidateHostedRestriction(platform, accountType string) error {
	legacyType := HostedAccountType(platform)
	if legacyType != "" && accountType != "" && accountType != legacyType {
		return fmt.Errorf("account_type conflicts with hosted platform")
	}
	if accountType == "" {
		return nil
	}
	switch accountType {
	case AccountTypeBedrock:
		if CanonicalPlatform(platform) == PlatformAnthropic {
			return nil
		}
	case AccountTypeServiceAccount:
		if canonical := CanonicalPlatform(platform); canonical == PlatformAnthropic || canonical == PlatformGemini {
			return nil
		}
	}
	return fmt.Errorf("unsupported platform and hosted account_type combination")
}
