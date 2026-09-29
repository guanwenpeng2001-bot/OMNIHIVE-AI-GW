//go:build unit

package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAffinityIdentityTrustedScope(t *testing.T) {
	body := `{"model":"claude","system":"system","messages":[{"role":"user","content":"hello"}]}`
	newRequest := func() *ParsedRequest {
		p := mustParseSessionHashRequest(t, body, &SessionContext{APIKeyID: 1, OwnerID: 10, RouteScope: "messages/shared"})
		group := int64(3)
		p.GroupID = &group
		return p
	}
	base := stableAffinityIdentity(newRequest())
	require.NotEmpty(t, base)
	require.NotEqual(t, (&GatewayService{}).hashContent("hello"), base, "v2 must not read legacy content bindings")
	for name, change := range map[string]func(*ParsedRequest){
		"owner":    func(p *ParsedRequest) { p.SessionContext.OwnerID++ },
		"api key":  func(p *ParsedRequest) { p.SessionContext.APIKeyID++ },
		"route":    func(p *ParsedRequest) { p.SessionContext.RouteScope = "messages/private" },
		"group":    func(p *ParsedRequest) { *p.GroupID++ },
		"model":    func(p *ParsedRequest) { p.Model = "different" },
		"protocol": func(p *ParsedRequest) { p.protocol = "different" },
	} {
		t.Run(name, func(t *testing.T) {
			p := newRequest()
			change(p)
			require.NotEqual(t, base, stableAffinityIdentity(p))
		})
	}
	for _, scope := range []*SessionContext{nil, {}, {ClientIP: "1.2.3.4", UserAgent: "client"}, {APIKeyID: -1}} {
		require.Empty(t, stableAffinityIdentity(mustParseSessionHashRequest(t, body, scope)))
	}
}

func TestAffinityIdentityNoConversationAnchor(t *testing.T) {
	for _, body := range []string{
		`{}`, `{"model":"claude"}`, `{"system":"shared system"}`,
		`{"messages":[]}`, `{"messages":[{"role":"assistant","content":"hello"}]}`,
		`{"messages":[{"role":"user","content":""},{"role":"user","content":"later"}]}`,
		`{"messages":[{"role":"user","content":[]}]}`,
		`{"messages":[{"role":"user","content":[{"type":"text","text":""}]}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			require.Empty(t, stableAffinityIdentity(mustParseSessionHashRequest(t, body, &SessionContext{APIKeyID: 1, OwnerID: 10})))
		})
	}
}

func TestAffinityIdentitySemanticBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, a, b string }{
		{"system user framing", `{"system":"ab","messages":[{"role":"user","content":"c"}]}`, `{"system":"a","messages":[{"role":"user","content":"bc"}]}`},
		{"text block framing", `{"messages":[{"role":"user","content":[{"type":"text","text":"ab"},{"type":"text","text":"c"}]}]}`, `{"messages":[{"role":"user","content":[{"type":"text","text":"a"},{"type":"text","text":"bc"}]}]}`},
		{"content type", `{"messages":[{"role":"user","content":"hello"}]}`, `{"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`},
		{"media", `{"messages":[{"role":"user","content":[{"type":"image","source":{"data":"a","type":"base64"}}]}]}`, `{"messages":[{"role":"user","content":[{"type":"image","source":{"data":"b","type":"base64"}}]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := stableAffinityIdentity(mustParseSessionHashRequest(t, tc.a, &SessionContext{APIKeyID: 1, OwnerID: 10}))
			b := stableAffinityIdentity(mustParseSessionHashRequest(t, tc.b, &SessionContext{APIKeyID: 1, OwnerID: 10}))
			require.NotEmpty(t, a)
			require.NotEmpty(t, b)
			require.NotEqual(t, a, b)
		})
	}
	first := mustParseSessionHashRequest(t, `{"system":[{"type":"text","text":"rules"}],"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"a"}]}`, &SessionContext{APIKeyID: 1, OwnerID: 10})
	second := mustParseSessionHashRequest(t, `{ "system": [{"text": "rules", "type": "text"}], "messages": [{"content": "hello", "role": "user"}, {"role":"assistant","content":"b"}] }`, &SessionContext{APIKeyID: 1, OwnerID: 10})
	require.NotEmpty(t, stableAffinityIdentity(first))
	require.Equal(t, stableAffinityIdentity(first), stableAffinityIdentity(second))
}

func TestAffinityIdentityGeminiMediaAndImplicitUser(t *testing.T) {
	parse := func(body string) *ParsedRequest {
		return mustParseGeminiSessionHashRequest(t, body, &SessionContext{APIKeyID: 1, OwnerID: 10})
	}
	a := stableAffinityIdentity(parse(`{"contents":[{"parts":[{"inlineData":{"mimeType":"image/png","data":"a"}}]}]}`))
	b := stableAffinityIdentity(parse(`{"contents":[{"parts":[{"inlineData":{"mimeType":"image/png","data":"b"}}]}]}`))
	require.NotEmpty(t, a)
	require.NotEqual(t, a, b)
	c := stableAffinityIdentity(parse(`{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"image/png","data":"a"}}]},{"role":"model","parts":[{"text":"reply"}]},{"role":"user","parts":[{"text":"next"}]}]}`))
	require.Equal(t, a, c)
}

func TestAffinityIdentityUsesCurrentBody(t *testing.T) {
	p := mustParseSessionHashRequest(t, `{"messages":[{"role":"user","content":"hello"}]}`, &SessionContext{APIKeyID: 1, OwnerID: 10})
	original := stableAffinityIdentity(p)
	require.NotEmpty(t, original)
	require.NoError(t, p.ReplaceBody([]byte(`{"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"reply"}]}`)))
	require.Equal(t, original, stableAffinityIdentity(p))
	require.NoError(t, p.ReplaceBody([]byte(`{"messages":[{"role":"user","content":"changed"}]}`)))
	require.NotEqual(t, original, stableAffinityIdentity(p))
}

func TestAffinityIdentityRouteModelFraming(t *testing.T) {
	body := `{"messages":[{"role":"user","content":"hello"}]}`
	a := mustParseSessionHashRequest(t, body, &SessionContext{APIKeyID: 1, OwnerID: 10, RouteScope: "ab"})
	a.Model = "c"
	b := mustParseSessionHashRequest(t, body, &SessionContext{APIKeyID: 1, OwnerID: 10, RouteScope: "a"})
	b.Model = "bc"
	require.NotEqual(t, stableAffinityIdentity(a), stableAffinityIdentity(b))
}

func TestAffinityIdentityHMACVersions(t *testing.T) {
	parse := func() *ParsedRequest {
		return mustParseSessionHashRequest(t, `{"messages":[{"role":"user","content":"private prompt"}]}`, &SessionContext{APIKeyID: 1, OwnerID: 10})
	}
	key := []byte("shared-instance-key-12345678901234")
	a := hmacAffinityIdentity(affinityPrefixPayload(parse()), key, "key-1")
	b := hmacAffinityIdentity(affinityPrefixPayload(parse()), key, "key-1")
	require.NotEmpty(t, a)
	require.Equal(t, a, b, "independent request parsing shares the same identity")
	require.NotContains(t, a, "private prompt")
	require.NotContains(t, a, string(key))
	require.NotEqual(t, a, hmacAffinityIdentity(affinityPrefixPayload(parse()), key, "key-2"))
	require.NotEqual(t, a, hmacAffinityIdentity(affinityPrefixPayload(parse()), []byte("other-instance-key-123456789012345"), "key-1"))
	require.Empty(t, hmacAffinityIdentity(affinityPrefixPayload(parse()), nil, "key-1"))
	require.Empty(t, hmacAffinityIdentity(affinityPrefixPayload(parse()), key, ""))
	require.Empty(t, hmacAffinityIdentity(nil, key, "key-1"))
}

func TestAffinityIdentityHMACConfiguration(t *testing.T) {
	parse := func() *ParsedRequest {
		return mustParseSessionHashRequest(t, `{"messages":[{"role":"user","content":"hello"}]}`, &SessionContext{APIKeyID: 1, OwnerID: 10})
	}
	p := parse()
	unkeyed := stableAffinityIdentity(p)
	require.True(t, strings.HasPrefix(unkeyed, "v2:sha256:"))
	p.SessionContext.AffinityHMACKey = []byte("shared-instance-key-12345678901234")
	require.Empty(t, stableAffinityIdentity(p), "missing key version must not silently downgrade")
	p.SessionContext.AffinityHMACKeyVersion = "key-1"
	keyed := stableAffinityIdentity(p)
	require.True(t, strings.HasPrefix(keyed, "v2:hmac:"))
	require.NotEqual(t, unkeyed, keyed)
	other := parse()
	other.SessionContext = &SessionContext{APIKeyID: 1, OwnerID: 10, AffinityHMACKey: append([]byte(nil), p.SessionContext.AffinityHMACKey...), AffinityHMACKeyVersion: "key-1"}
	require.Equal(t, keyed, stableAffinityIdentity(other), "shared key works across service instances")
	require.NoError(t, other.ReplaceBody([]byte(`{"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"reply"},{"role":"user","content":"next"}]}`)))
	require.Equal(t, keyed, stableAffinityIdentity(other))
	p.SessionContext.AffinityHMACKey = nil
	require.Empty(t, stableAffinityIdentity(p), "missing key material must not silently downgrade")
}

func TestAffinityIdentityResponsesAnchors(t *testing.T) {
	parse := func(body string) *ParsedRequest {
		parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(body)), "responses")
		require.NoError(t, err)
		parsed.SessionContext = &SessionContext{OwnerID: 10, APIKeyID: 1, RouteScope: "/v1/responses"}
		return parsed
	}
	for _, pair := range [][2]string{
		{`{"input":"hello"}`, `{"input":[{"role":"user","content":"hello"},{"role":"assistant","content":"reply"},{"role":"user","content":"next"}]}`},
		{`{"input":[{"content":[{"type":"input_image","image_url":"data:image/png;base64,a"}]}]}`, `{"input":[{"role":"user","content":[{"image_url":"data:image/png;base64,a","type":"input_image"}]},{"type":"function_call_output","call_id":"call","output":"later"}]}`},
	} {
		first := stableAffinityIdentity(parse(pair[0]))
		require.NotEmpty(t, first)
		require.Equal(t, first, stableAffinityIdentity(parse(pair[1])))
	}
	for _, body := range []string{
		`{}`, `{"previous_response_id":"unverified"}`, `{"input":" "}`, `{"input":[{"type":"function_call_output","output":"result"}]}`,
		`{"input":[{"role":"assistant","content":"reply"}]}`, `{"input":[{"role":"user","content":null},{"role":"user","content":"later"}]}`,
		`{"input":[{"content":[{"type":"input_image","image_url":null}]}]}`,
	} {
		require.Empty(t, stableAffinityIdentity(parse(body)), body)
	}
}

func TestAffinityIdentityExactNumbersAndNetworkIndependence(t *testing.T) {
	parse := func(number string) *ParsedRequest {
		return mustParseSessionHashRequest(t, `{"messages":[{"role":"user","content":[{"type":"image","source":{"data":"image","reference":`+number+`}}]}]}`, &SessionContext{OwnerID: 10, APIKeyID: 1})
	}
	first := parse("9007199254740992")
	original := stableAffinityIdentity(first)
	require.NotEmpty(t, original)
	require.NotEqual(t, original, stableAffinityIdentity(parse("9007199254740993")))
	first.SessionContext.ClientIP = "192.0.2.1"
	first.SessionContext.UserAgent = "updated/9.0"
	require.Equal(t, original, stableAffinityIdentity(first))
	require.Nil(t, canonicalAffinityJSON([]byte(`{} {}`)))
}
