//go:build unit

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func affinityScopedRequest(t *testing.T) *ParsedRequest {
	t.Helper()
	parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(`{"model":"m","messages":[{"role":"user","content":"hello"}]}`)), "anthropic")
	require.NoError(t, err)
	parsed.SessionContext = &SessionContext{OwnerID: 1, APIKeyID: 2, RouteScope: "/v1/messages", AffinityHMACKey: []byte(strings.Repeat("k", 32)), AffinityHMACKeyVersion: "2026-09"}
	group := int64(3)
	parsed.GroupID = &group
	return parsed
}

func TestAffinityScopedExplicitSignals(t *testing.T) {
	svc := &GatewayService{}
	base := svc.GenerateScopedSessionHash(affinityScopedRequest(t), "trusted-session")
	require.True(t, strings.HasPrefix(base, "v2:binding:hmac:"))
	require.Equal(t, base, (&GatewayService{}).GenerateScopedSessionHash(affinityScopedRequest(t), "trusted-session"))
	for name, mutate := range map[string]func(*ParsedRequest){
		"owner":    func(p *ParsedRequest) { p.SessionContext.OwnerID++ },
		"key":      func(p *ParsedRequest) { p.SessionContext.APIKeyID++ },
		"group":    func(p *ParsedRequest) { *p.GroupID++ },
		"route":    func(p *ParsedRequest) { p.SessionContext.RouteScope = "/private/messages" },
		"protocol": func(p *ParsedRequest) { p.protocol = "responses" },
		"model":    func(p *ParsedRequest) { p.Model = "other" },
		"rotation": func(p *ParsedRequest) { p.SessionContext.AffinityHMACKeyVersion = "rotated" },
	} {
		t.Run(name, func(t *testing.T) {
			p := affinityScopedRequest(t)
			mutate(p)
			require.NotEqual(t, base, svc.GenerateScopedSessionHash(p, "trusted-session"))
		})
	}
	require.NotContains(t, base, "trusted-session")
	p := affinityScopedRequest(t)
	require.NoError(t, p.ReplaceBody([]byte(`{"model":"m"}`)))
	require.Equal(t, base, svc.GenerateScopedSessionHash(p, "trusted-session"))
	require.Empty(t, svc.GenerateScopedSessionHash(p, ""))
	p.SessionContext.OwnerID = 0
	require.Empty(t, svc.GenerateScopedSessionHash(p, "trusted-session"))
}

func TestAffinityScopedContentAndLegacyCompatibility(t *testing.T) {
	svc := &GatewayService{}
	p := affinityScopedRequest(t)
	legacy := svc.GenerateSessionHash(p)
	scoped := svc.GenerateScopedSessionHash(p, "")
	require.True(t, IsV2AffinityKey(scoped))
	require.False(t, IsV2AffinityKey(legacy))
	p.SessionContext.ClientIP = "changed"
	p.SessionContext.UserAgent = "different/1.0"
	require.Equal(t, scoped, svc.GenerateScopedSessionHash(p, ""))
	require.NotEqual(t, legacy, svc.GenerateSessionHash(p), "legacy path must retain IP/UA behavior")
	p.SessionContext = &SessionContext{APIKeyID: 2}
	require.Equal(t, legacy, svc.GenerateSessionHash(p), "new authenticated fields must not change legacy hash")
	require.Empty(t, svc.GenerateScopedSessionHash(p, ""))
	require.False(t, IsV2AffinityKey("v2:sha256:unmigrated"))
	require.False(t, IsV2AffinityKey("gemini:v2:hmac:unmigrated"))
	require.True(t, IsV2AffinityKey("gemini:v2:binding:hmac:scoped"))
}

func TestAffinityBindingStateSurvivesNestedPlan(t *testing.T) {
	ctx := WithAffinityBindingState(context.Background())
	state := AffinityBindingStateFromContext(ctx)
	state.Versions.Store("key", "first")
	nested := WithAffinityBindingState(ctx)
	require.Same(t, state, AffinityBindingStateFromContext(nested))
	require.False(t, AffinityAdmitted(nested))
	require.True(t, AffinityAdmitted(WithAffinityAdmission(nested)))
	require.False(t, AffinityAdmitted(ctx))
	require.NotSame(t, state, AffinityBindingStateFromContext(WithAffinityBindingState(context.Background())))
}

func TestAffinityScopedMissingScopeAndHMACPair(t *testing.T) {
	svc := &GatewayService{}
	for name, mutate := range map[string]func(*ParsedRequest){
		"owner":        func(p *ParsedRequest) { p.SessionContext.OwnerID = 0 },
		"key":          func(p *ParsedRequest) { p.SessionContext.APIKeyID = 0 },
		"route":        func(p *ParsedRequest) { p.SessionContext.RouteScope = " " },
		"protocol":     func(p *ParsedRequest) { p.protocol = "" },
		"model":        func(p *ParsedRequest) { p.Model = "" },
		"hmac key":     func(p *ParsedRequest) { p.SessionContext.AffinityHMACKey = nil },
		"hmac version": func(p *ParsedRequest) { p.SessionContext.AffinityHMACKeyVersion = "" },
	} {
		t.Run(name, func(t *testing.T) {
			p := affinityScopedRequest(t)
			mutate(p)
			require.Empty(t, svc.GenerateScopedSessionHash(p, "trusted"))
			require.Empty(t, svc.GenerateScopedSessionHash(p, ""))
		})
	}
	p := affinityScopedRequest(t)
	p.SessionContext.AffinityHMACKey = nil
	p.SessionContext.AffinityHMACKeyVersion = ""
	require.True(t, strings.HasPrefix(svc.GenerateScopedSessionHash(p, ""), "v2:binding:sha256:"))
}
