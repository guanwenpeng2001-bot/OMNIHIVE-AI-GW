package service

import (
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCopilotNousEndpoints(t *testing.T) {
	for _, host := range []string{"api.githubcopilot.com", "api.enterprise.githubcopilot.com", "api.business.githubcopilot.com", "copilot-api.tenant-1.ghe.com"} {
		for _, suffix := range []string{"", "/", "/v1", "/v1/"} {
			endpoints, err := ResolveCopilotNousEndpoints(CopilotNousAuthConfig{Platform: "github_copilot", BaseURL: "https://" + host + suffix})
			if err != nil || endpoints.ChatURL != "https://"+host+"/v1/chat/completions" || endpoints.ModelsURL != "https://"+host+"/models" {
				t.Fatalf("host %s suffix %s: %+v %v", host, suffix, endpoints, err)
			}
		}
	}
	for _, base := range []string{"http://api.githubcopilot.com", "https://api.githubcopilot.com:443", "https://api.githubcopilot.com.evil.test", "https://api.githubcopilot.com@evil.test", "https://copilot-api.a.b.ghe.com", "https://copilot-api.-bad.ghe.com", "https://copilot-api..ghe.com", "https://api.githubcopilot.com/v1/models", "https://api.githubcopilot.com/%76%31", "https://api.githubcopilot.com?", "https://api.githubcopilot.com#", "https://API.githubcopilot.com", "https://api.githubcopilot.com\\evil"} {
		if _, err := ResolveCopilotNousEndpoints(CopilotNousAuthConfig{Platform: "github_copilot", BaseURL: base}); !errors.Is(err, ErrCopilotNousBaseURL) {
			t.Errorf("accepted invalid Copilot base %q: %v", base, err)
		}
	}
	for _, base := range []string{"", "https://inference-api.nousresearch.com/v1", "https://inference-api.nousresearch.com/v1/"} {
		endpoints, err := ResolveCopilotNousEndpoints(CopilotNousAuthConfig{Platform: "nous_research", AuthMode: "invocation_token", BaseURL: base})
		if err != nil || endpoints.ModelsURL != "https://inference-api.nousresearch.com/v1/models" {
			t.Fatalf("Nous base %q: %+v %v", base, endpoints, err)
		}
	}
	for _, base := range []string{"https://welcome-api.nousresearch.com/v1", "https://portal.nousresearch.com/v1", "https://operator.test/v1", "https://inference-api.nousresearch.com", "http://inference-api.nousresearch.com/v1"} {
		if _, err := ResolveCopilotNousEndpoints(CopilotNousAuthConfig{Platform: "nous_research", AuthMode: "invocation_token", BaseURL: base}); !errors.Is(err, ErrCopilotNousBaseURL) {
			t.Errorf("accepted conflicting Nous base %q: %v", base, err)
		}
	}
	for _, base := range []string{"http://127.0.0.1:8645/v1", "https://operator.test/hermes/v1/"} {
		endpoints, err := ResolveCopilotNousEndpoints(CopilotNousAuthConfig{Platform: "nous_research", AuthMode: "official_proxy", BaseURL: base})
		if err != nil || endpoints.ChatURL != strings.TrimSuffix(base, "/")+"/chat/completions" {
			t.Fatalf("proxy syntax: %+v %v", endpoints, err)
		}
	}
	for _, base := range []string{"", "ftp://operator.test/v1", "https://user:secret@operator.test/v1", "https://operator.test/v1?token=secret", "https://operator.test/v1#secret", "https://operator.test/a/../v1", "https://operator.test/%2e%2e/v1"} {
		if _, err := ResolveCopilotNousEndpoints(CopilotNousAuthConfig{Platform: "nous_research", AuthMode: "official_proxy", BaseURL: base}); !errors.Is(err, ErrCopilotNousBaseURL) {
			t.Errorf("accepted invalid proxy base %q: %v", base, err)
		}
	}
	for _, config := range []CopilotNousAuthConfig{
		{Platform: "openai"}, {Platform: "github_copilot", AuthMode: "oauth"},
		{Platform: "nous_research"}, {Platform: "nous_research", AuthMode: "anonymous"},
	} {
		if _, err := ResolveCopilotNousEndpoints(config); !errors.Is(err, ErrCopilotNousAuthMode) {
			t.Fatalf("invalid mode: %v", err)
		}
	}
}

func authCoreJWT(payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".c2lnbmF0dXJl"
}

func TestCopilotNousResolvedCredentials(t *testing.T) {
	now := time.Unix(2000000000, 0)
	config := CopilotNousAuthConfig{Platform: "github_copilot"}
	for _, token := range []string{"", "github_pat_", "ghp_classic", "ghs_installation", "gho_oauth", "ghu_user", "ghr_refresh", "Bearer github_pat_value", "github_pat_with space", "github_pat_bad\r\nInjected: value"} {
		if _, err := ValidateCopilotNousResolvedSecret(config, token, "", now); !errors.Is(err, ErrCopilotNousCredential) {
			t.Errorf("invalid PAT accepted: %v", err)
		}
	}
	if _, err := ValidateCopilotNousResolvedSecret(config, "github_pat_test123", "", now); err != nil {
		t.Fatal(err)
	}
	config = CopilotNousAuthConfig{Platform: "nous_research", AuthMode: "invocation_token"}
	for _, payload := range []string{
		`{"exp":2000000060,"scope":"inference:invoke"}`,
		`{"exp":2000000060,"scp":["profile","inference:invoke"]}`,
		`{"exp":2000000060,"scope":"profile,inference:invoke"}`,
		`{"exp":2000000060,"scope":["inference:invoke"]}`,
	} {
		claims, err := ValidateCopilotNousResolvedSecret(config, authCoreJWT(payload), "", now)
		if err != nil || claims.UnverifiedExpiresAt != time.Unix(2000000060, 0) {
			t.Fatalf("unverified claims: %+v %v", claims, err)
		}
	}
	for _, payload := range []string{
		`{"exp":2000000000,"scope":"inference:invoke"}`, `{"exp":1999999999,"scope":"inference:invoke"}`,
		`{"scope":"inference:invoke"}`, `{"exp":"2000000060","scope":"inference:invoke"}`,
		`{"exp":2000000060}`, `{"exp":2000000060,"scope":"not-inference:invoke"}`,
		`{"exp":2000000060,"scope":{},"scp":["inference:invoke"]}`,
		`{"exp":2000000060,"scp":["inference:invoke",4]}`, `null`, `[]`, `{broken`,
	} {
		if _, err := ValidateCopilotNousResolvedSecret(config, authCoreJWT(payload), "", now); err == nil {
			t.Errorf("invalid claims accepted: %s", payload)
		}
	}
	for _, token := range []string{"refresh_secret", "a.b.c", "e30.e30.", authCoreJWT(`{"exp":2000000060,"scope":"inference:invoke"}`) + ".extra"} {
		if _, err := ValidateCopilotNousResolvedSecret(config, token, "", now); err == nil || strings.Contains(err.Error(), token) {
			t.Errorf("malformed JWT or unsafe error: %v", err)
		}
	}
	for _, mode := range []string{"invocation_token", "official_proxy"} {
		config.AuthMode, config.HasRefreshToken = mode, true
		if _, err := ResolveCopilotNousEndpoints(config); !errors.Is(err, ErrCopilotNousRefreshUnsupported) {
			t.Fatal("refresh field must be rejected even before resolving secrets")
		}
	}
}

func TestCopilotNousProtectedHeadersAndRuntimeExpiry(t *testing.T) {
	now := time.Unix(2000000000, 0)
	for _, catalog := range []bool{false, true} {
		config := CopilotNousAuthConfig{Platform: "github_copilot"}
		endpoints, _ := ResolveCopilotNousEndpoints(config)
		method, target := http.MethodPost, endpoints.ChatURL
		if catalog {
			method, target = http.MethodGet, endpoints.ModelsURL
		}
		request, _ := http.NewRequest(method, target, nil)
		request.Header = http.Header{"authorization": {"Basic override"}, "AUTHORIZATION": {"Bearer other"}, "X-Api-Key": {"other"}, "Proxy-Authorization": {"other"}, "Cookie": {"secret"}, "copilot-integration-id": {"agentic-workflows"}, "Editor-Version": {"vscode/1"}, "X-Github-Api-Version": {"override"}, "X-Request-Id": {"preserved"}}
		if err := ApplyCopilotNousAuth(request, config, "github_pat_test123", "", now); err != nil {
			t.Fatal(err)
		}
		if request.Header.Get("Authorization") != "Bearer github_pat_test123" || request.Header.Get("Copilot-Integration-Id") != "omnihive" || request.Header.Get("X-Request-Id") != "preserved" {
			t.Fatal("incorrect protected headers")
		}
		for _, name := range []string{"authorization", "AUTHORIZATION", "X-Api-Key", "Proxy-Authorization", "Cookie", "Editor-Version", "copilot-integration-id"} {
			if _, exists := request.Header[name]; exists {
				t.Errorf("conflicting header survived: %s", name)
			}
		}
		version := ""
		if catalog {
			version = "2026-07-01"
		}
		if request.Header.Get("X-GitHub-Api-Version") != version {
			t.Fatal("catalog version leaked or missing")
		}
	}
	config := CopilotNousAuthConfig{Platform: "nous_research", AuthMode: "invocation_token"}
	token := authCoreJWT(`{"exp":2000000060,"scope":"inference:invoke"}`)
	request, _ := http.NewRequest(http.MethodPost, "https://inference-api.nousresearch.com/v1/chat/completions", nil)
	if err := ApplyCopilotNousAuth(request, config, token, "", now); err != nil {
		t.Fatal(err)
	}
	if request.Header.Get("Authorization") != "Bearer "+token {
		t.Fatal("invocation bearer missing")
	}
	if err := ApplyCopilotNousAuth(request, config, token, "", now.Add(time.Minute)); !errors.Is(err, ErrCopilotNousExpired) {
		t.Fatalf("must recheck expiry at send: %v", err)
	}
	for _, target := range []string{"https://welcome-api.nousresearch.com/v1/chat/completions", "https://inference-api.nousresearch.com/v1/messages", "https://inference-api.nousresearch.com/v1/responses", "https://inference-api.nousresearch.com/v1/models?key=secret"} {
		request, _ := http.NewRequest(http.MethodPost, target, nil)
		before := request.Header.Clone()
		if err := ApplyCopilotNousAuth(request, config, token, "", now); !errors.Is(err, ErrCopilotNousRequest) {
			t.Fatal("unsafe destination accepted")
		}
		if !reflect.DeepEqual(before, request.Header) {
			t.Fatal("mutated rejected request")
		}
	}
	request.Host = "evil.test"
	if err := ApplyCopilotNousAuth(request, config, token, "", now); !errors.Is(err, ErrCopilotNousRequest) {
		t.Fatal("host override accepted")
	}
}

func TestCopilotNousProxySecretIsSeparate(t *testing.T) {
	config := CopilotNousAuthConfig{Platform: "nous_research", AuthMode: "official_proxy", BaseURL: "https://operator.test/hermes/v1"}
	for _, frontdoor := range []string{"", "operator-secret"} {
		request, _ := http.NewRequest(http.MethodGet, config.BaseURL+"/models", nil)
		request.Header.Set("Authorization", "Bearer incoming-user-secret")
		request.Header.Set("X-Nous-Refresh-Token", "refresh-secret")
		if err := ApplyCopilotNousAuth(request, config, "", frontdoor, time.Now()); err != nil {
			t.Fatal(err)
		}
		expected := ""
		if frontdoor != "" {
			expected = "Bearer " + frontdoor
		}
		if request.Header.Get("Authorization") != expected || request.Header.Get("X-Nous-Refresh-Token") != "" {
			t.Fatal("proxy auth not isolated")
		}
	}
	if _, err := ValidateCopilotNousResolvedSecret(config, "portal-secret", "", time.Now()); !errors.Is(err, ErrCopilotNousCredential) {
		t.Fatal("proxy must not reuse invocation credential")
	}
	if _, err := ValidateCopilotNousResolvedSecret(config, "", "bad\r\nsecret", time.Now()); !errors.Is(err, ErrCopilotNousCredential) {
		t.Fatal("unsafe frontdoor accepted")
	}
	config.AuthMode, config.BaseURL = "invocation_token", ""
	if _, err := ValidateCopilotNousResolvedSecret(config, authCoreJWT(`{"exp":2000000060,"scope":"inference:invoke"}`), "proxy-secret", time.Unix(2000000000, 0)); !errors.Is(err, ErrCopilotNousCredential) {
		t.Fatal("frontdoor accepted in direct mode")
	}
}

type authCoreRedirectTransport struct {
	calls       int
	destination string
}

func (transport *authCoreRedirectTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.calls++
	return &http.Response{StatusCode: http.StatusTemporaryRedirect, Header: http.Header{"Location": {transport.destination}}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
}

func TestCopilotNousRedirectPolicy(t *testing.T) {
	for _, destination := range []string{"https://evil.test/steal", "https://api.githubcopilot.com/other", "https://api.enterprise.githubcopilot.com/models"} {
		transport := &authCoreRedirectTransport{destination: destination}
		client := &http.Client{Transport: transport, CheckRedirect: CopilotNousCheckRedirect}
		request, _ := http.NewRequest(http.MethodGet, "https://api.githubcopilot.com/models", nil)
		if err := ApplyCopilotNousAuth(request, CopilotNousAuthConfig{Platform: "github_copilot"}, "github_pat_test123", "", time.Now()); err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if err := response.Body.Close(); err != nil {
			t.Fatal(err)
		}
		if transport.calls != 1 || response.StatusCode != http.StatusTemporaryRedirect {
			t.Fatal("redirect followed")
		}
	}
}

func TestCopilotNousRejectsMalformedTransportAndJWT(t *testing.T) {
	if _, err := ResolveCopilotNousEndpoints(CopilotNousAuthConfig{Platform: "github_copilot", BaseURL: "HTTPS://api.githubcopilot.com/v1"}); !errors.Is(err, ErrCopilotNousBaseURL) {
		t.Error("requires explicit exact https prefix")
	}
	config := CopilotNousAuthConfig{Platform: "github_copilot"}
	for _, request := range []*http.Request{nil, {}, {Method: http.MethodGet}} {
		if err := ApplyCopilotNousAuth(request, config, "github_pat_test123", "", time.Now()); !errors.Is(err, ErrCopilotNousRequest) {
			t.Error("missing request URL accepted")
		}
	}
	request, _ := http.NewRequest(http.MethodPost, "https://api.githubcopilot.com/models", nil)
	if err := ApplyCopilotNousAuth(request, config, "github_pat_test123", "", time.Now()); !errors.Is(err, ErrCopilotNousRequest) {
		t.Error("POST catalog accepted")
	}
	request.Method, request.RequestURI = http.MethodGet, "/models"
	if err := ApplyCopilotNousAuth(request, config, "github_pat_test123", "", time.Now()); !errors.Is(err, ErrCopilotNousRequest) {
		t.Error("server request accepted for forwarding")
	}
	config = CopilotNousAuthConfig{Platform: "nous_research", AuthMode: "invocation_token"}
	now := time.Unix(2000000000, 0)
	valid := authCoreJWT(`{"exp":2000000060,"scope":"inference:invoke"}`)
	segments := strings.Split(valid, ".")
	for _, header := range []string{`{}`, `{"alg":"none"}`, `{"alg":3}`, `[]`, `broken`} {
		token := base64.RawURLEncoding.EncodeToString([]byte(header)) + "." + segments[1] + "." + segments[2]
		if _, err := ValidateCopilotNousResolvedSecret(config, token, "", now); !errors.Is(err, ErrCopilotNousCredential) {
			t.Errorf("invalid JWT header accepted: %v", err)
		}
	}
	for _, payload := range []string{`{"exp":null,"scope":"inference:invoke"}`, `{"exp":1e100,"scope":"inference:invoke"}`, `{"exp":2000000060,"scope":null}`} {
		if _, err := ValidateCopilotNousResolvedSecret(config, authCoreJWT(payload), "", now); err == nil {
			t.Error("invalid claim type accepted")
		}
	}
	request, _ = http.NewRequest(http.MethodPost, "https://inference-api.nousresearch.com/v1/chat/completions", nil)
	request.Header.Set("Authorization", "existing-secret")
	before := request.Header.Clone()
	if err := ApplyCopilotNousAuth(request, config, valid, "", now.Add(time.Hour)); !errors.Is(err, ErrCopilotNousExpired) || !reflect.DeepEqual(before, request.Header) {
		t.Error("expiry rejection must leave request unchanged")
	}
}
