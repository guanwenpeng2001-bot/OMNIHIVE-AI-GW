package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

// Test-only export: no shared TS fixture, production endpoint, or upstream call.
type planBaseURLContractEntry struct {
	Platform          string              `json:"platform"`
	AccountMode       string              `json:"account_mode"`
	Protocol          string              `json:"protocol"`
	SiteVariant       string              `json:"site_variant"`
	Role              string              `json:"role"`
	DefaultURL        string              `json:"default_url"`
	CanonicalURL      string              `json:"canonical_url"`
	CanonicalSource   string              `json:"canonical_source"`
	ResolvedURL       string              `json:"resolved_url"`
	NativeAction      domain.ActionStatus `json:"native_action"`
	ValidatorAccepted bool                `json:"validator_accepted"`
	ValidatorError    string              `json:"validator_error"`
	TargetURL         string              `json:"target_url"`
}

func (entry planBaseURLContractEntry) key() string {
	return entry.Platform + "/" + entry.AccountMode + "/" + entry.Protocol + "/" + entry.SiteVariant
}

func planBaseURLContract(t *testing.T) []planBaseURLContractEntry {
	t.Helper()
	var entries []planBaseURLContractEntry
	gateway := &OpenAIGatewayService{}
	add := func(platform, mode, protocol, site, role, canonical, source string) {
		account := &Account{Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{
			"account_mode": mode, "api_protocol": protocol, "api_key": "contract-test-key",
		}}
		resolveBase := func() string {
			if protocol == APIProtocolAnthropic {
				return account.GetAnthropicProtocolBaseURL()
			}
			return account.GetOpenAIBaseURL()
		}
		defaultURL := resolveBase()
		if canonical == "" {
			canonical = defaultURL
		}
		account.Credentials["base_url"] = canonical
		action := "chat"
		if protocol == APIProtocolAnthropic {
			action = "messages"
		}
		if protocol == APIProtocolResponses {
			action = "responses"
		}
		entry := planBaseURLContractEntry{Platform: platform, AccountMode: mode, Protocol: protocol,
			SiteVariant: site, Role: role, DefaultURL: defaultURL, CanonicalURL: canonical,
			CanonicalSource: source, ResolvedURL: resolveBase(),
			NativeAction: domain.PlatformAction(domain.ActionIdentity{Platform: platform, Protocol: protocol}, action).Status}
		var validationErr error
		if protocol == APIProtocolAnthropic {
			entry.TargetURL, validationErr = gateway.nativeAnthropicTargetURL(account)
		} else {
			switch platform {
			case PlatformHunyuan:
				entry.TargetURL, validationErr = hunyuanActionBaseURL(account)
			case PlatformVolcengine:
				request, err := gateway.prepareVolcengineChatRequest(account, []byte(`{"model":"contract-model","messages":[{"role":"user","content":"test"}]}`), "contract-model")
				validationErr = err
				if err == nil {
					entry.TargetURL = request.TargetURL
				}
			case PlatformQianfan:
				request, err := buildQianfanRequest(context.Background(), account, "models", nil, gateway.validateUpstreamBaseURL)
				validationErr = err
				if err == nil {
					entry.TargetURL = request.URL.String()
				}
			default:
				entry.TargetURL, validationErr = gateway.validateUpstreamBaseURL(entry.ResolvedURL)
			}
		}
		entry.ValidatorAccepted = validationErr == nil
		if validationErr != nil {
			entry.ValidatorError = validationErr.Error()
		}
		entries = append(entries, entry)
	}

	// Defaults follow the real capability declarations; explicit inventory below
	// remains independent so adding/removing a preset cannot silently shrink coverage.
	for _, platform := range []string{PlatformKimi, PlatformZhipu, PlatformMiniMax, PlatformHunyuan, PlatformVolcengine, PlatformQianfan, PlatformOpenCodeGo, PlatformCommandCode, PlatformMistral, PlatformDeepseek, PlatformQianwen} {
		descriptor, exists := domain.LookupPlatform(platform)
		require.True(t, exists)
		protocols := []string{APIProtocolChatCompletions}
		if descriptor.MultiProtocol {
			protocols = append(protocols, APIProtocolAnthropic)
		}
		if descriptor.NativeMultiProtocolResponses || (descriptor.ChatOnly && descriptor.ResponsesPOST == domain.EndpointNative) {
			protocols = append(protocols, APIProtocolResponses)
		}
		if len(protocols) > 1 {
			protocols = append(protocols, APIProtocolAdaptive)
		}
		modes := []string{AccountModeCoding}
		switch platform {
		case PlatformOpenCodeGo:
			modes = []string{AccountModeGo, AccountModeZen}
		case PlatformCommandCode, PlatformMistral, PlatformDeepseek, PlatformQianwen:
			modes = []string{AccountModePayG}
		}
		for _, mode := range modes {
			for _, protocol := range protocols {
				add(platform, mode, protocol, "default", "default", "", "Account runtime default resolver")
				if mode != AccountModeCoding {
					continue
				}
				if protocol == APIProtocolAdaptive {
					continue
				}
				site := "primary"
				if platform == PlatformQianfan {
					site = "personal"
				}
				add(platform, mode, protocol, site, "accepted", "", "Account runtime default resolver")
			}
		}
	}
	// International MiniMax is an optional site, not a backend official-host
	// whitelist. These independent documented candidates are validated as relays;
	// validation alone does not establish their official spelling.
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolResponses, APIProtocolAnthropic} {
		canonical := "https://api.minimax.io/v1"
		if protocol == APIProtocolAnthropic {
			canonical = "https://api.minimax.io/anthropic"
		}
		add(PlatformMiniMax, AccountModeCoding, protocol, "international", "accepted", canonical,
			"MiniMax international API documentation: https://platform.minimax.io/docs/api-reference/api-overview; optional relay, not a backend host whitelist")
	}
	qianfanHost, err := url.Parse(qianfanDefaultBaseURL)
	require.NoError(t, err)
	qianfanHost.Path = qianfanTokenPlanTeamPath
	add(PlatformQianfan, AccountModeCoding, APIProtocolChatCompletions, "team", "accepted", qianfanHost.String(), "qianfanDefaultBaseURL host + qianfanTokenPlanTeamPath")
	add(PlatformHunyuan, AccountModeCoding, APIProtocolChatCompletions, "token-plan", "backend-only", hunyuanTokenPlanBaseURL,
		"hunyuanActionBaseURL legacy Token Plan compatibility; deliberately absent from Coding Plan UI")
	// Parked document inventory has no runtime canonical/default URL. Keep the
	// independently pinned documentation spelling while exporting actual rejection.
	add(PlatformVolcengine, AccountModeCoding, APIProtocolAnthropic, "primary", "parked", "https://ark.cn-beijing.volces.com/api/coding", "Ark Coding Plan documentation; native messages hard conflict")
	add(PlatformQianfan, AccountModeCoding, APIProtocolAnthropic, "personal", "parked", "https://qianfan.baidubce.com/anthropic/tokenplan/personal", "Qianfan personal Token Plan documentation; native messages hard conflict")
	add(PlatformQianfan, AccountModeCoding, APIProtocolAnthropic, "team", "parked", "https://qianfan.baidubce.com/anthropic/tokenplan/team", "Qianfan team Token Plan documentation; native messages hard conflict")
	sort.Slice(entries, func(left, right int) bool { return entries[left].key() < entries[right].key() })
	require.NoError(t, validatePlanBaseURLContract(entries))
	return entries
}

func validatePlanBaseURLContract(entries []planBaseURLContractEntry) error {
	seen := map[string]bool{}
	for _, entry := range entries {
		if seen[entry.key()] {
			return fmt.Errorf("duplicate plan URL key: %s", entry.key())
		}
		seen[entry.key()] = true
		if entry.Platform == "" || entry.AccountMode == "" || entry.Protocol == "" || entry.SiteVariant == "" || entry.CanonicalURL == "" || entry.CanonicalSource == "" {
			return fmt.Errorf("incomplete plan URL entry: %s", entry.key())
		}
		switch entry.Role {
		case "default", "accepted", "backend-only", "parked":
		default:
			return fmt.Errorf("unknown plan URL role: %s", entry.key())
		}
	}
	return nil
}

func TestExportPlanBaseURLContract(t *testing.T) {
	encoded, err := json.Marshal(planBaseURLContract(t))
	require.NoError(t, err)
	fmt.Printf("PLAN_BASE_URL_CONTRACT=%s\n", encoded)
}

func TestPlanBaseURLContractHasUniqueKeys(t *testing.T) {
	entries := planBaseURLContract(t)
	require.NoError(t, validatePlanBaseURLContract(entries))
	require.ErrorContains(t, validatePlanBaseURLContract(append(entries, entries[0])), "duplicate")
	for _, entry := range entries {
		if entry.Role == "parked" {
			require.Equal(t, domain.ActionHardConflict, entry.NativeAction, entry.key())
			require.False(t, entry.ValidatorAccepted, entry.key())
			require.Empty(t, entry.ResolvedURL, entry.key())
		} else {
			require.True(t, entry.ValidatorAccepted, "%s: %s", entry.key(), entry.ValidatorError)
			require.Equal(t, entry.CanonicalURL, entry.ResolvedURL, entry.key())
		}
	}
}
