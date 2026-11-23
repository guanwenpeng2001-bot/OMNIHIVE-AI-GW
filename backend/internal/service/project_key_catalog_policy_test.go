package service

import (
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/stretchr/testify/require"
)

func TestEffectiveKeyPolicySurvivesAttachingIssuedPolicyForDiscovery(t *testing.T) {
	model := "fixture"
	id := catalog.StableID(PlatformOpenAI, model)
	issued := &identity.KeyPolicy{Mode: "allow_list", Models: []string{id}, Platform: PlatformOpenAI, Actions: []string{"text.chat", "models.read"}}
	for _, test := range []struct {
		name                    string
		rule, named, exclusions []string
		allowed                 bool
	}{
		{"unchanged mirror", []string{id}, nil, nil, true},
		{"native narrowing", []string{model}, []string{id}, nil, true},
		{"new list below ceiling", []string{"other"}, nil, nil, false},
		{"explicit empty", []string{}, nil, nil, false},
		{"named empty", nil, []string{}, nil, false},
		{"excluded stable ID", []string{model}, nil, []string{id}, false},
		{"excluded native name", []string{id}, nil, []string{model}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := ModelAccessSnapshot{RestrictedKey: true, ProjectKeyPlatform: PlatformOpenAI, KeyExclude: test.exclusions,
				KeyRestriction: &identity.EffectiveKeyModels{Ceiling: issued.Models, Rule: test.rule, Named: test.named, Exclude: test.exclusions}}
			ctx := withModelAccessSnapshot(t.Context(), s)
			ctx = WithIssuedKeyPolicy(ctx, issued)
			require.Equal(t, test.allowed, visibleModelPolicyAllows(ctx, model))
			require.Equal(t, test.allowed, AllowsAuthenticatedKeyModels(ctx, issued, "text.chat", []string{model}))
			require.True(t, AllowsAuthenticatedKeyModels(ctx, issued, "models.read", nil))
			require.False(t, AllowsAuthenticatedKeyModels(ctx, issued, "text.chat", []string{"other"}))
		})
	}
	issued.Mode = "deny_all"
	ctx := WithIssuedKeyPolicy(t.Context(), issued)
	require.False(t, visibleModelPolicyAllows(ctx, model))
	require.False(t, AllowsAuthenticatedKeyModels(ctx, issued, "text.chat", []string{model}))
}
