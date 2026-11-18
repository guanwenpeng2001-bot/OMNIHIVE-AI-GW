//go:build unit

package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/gemini"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGeminiNativeModelsUsesAccountMappings(t *testing.T) {
	for _, forced := range []bool{false, true} {
		platform := service.PlatformGemini
		if forced {
			platform = service.PlatformAntigravity
		}
		account := publicMetadataAccount()
		account.Platform = platform
		account.Credentials["model_mapping"] = map[string]any{"gemini-public": "gemini-private"}
		account.Credentials["base_url"] = "https://generativelanguage.googleapis.com"
		account.Extra[service.UpstreamModelMetadataExtraKey] = service.UpstreamModelMetadataSnapshot{Models: map[string]service.UpstreamModelMetadata{"gemini-private": {ID: "gemini-private", ProviderProfile: platform, SupportedMethods: []string{"generateContent", "streamGenerateContent"}, ContextLimitKind: "input", ContextWindow: 123}}}

		account.Extra[service.UpstreamModelCatalogExtraKey] = service.AccountModelCatalogSnapshot{Complete: true, Revision: 1, DiscoveredModels: []string{"gemini-private"}, EligibleModels: []service.AccountCatalogModel{{ID: "gemini-private", Endpoints: []string{"generate_content"}, EvidenceKind: "synthetic-contract"}}}
		gateway := newGatewayModelsHandlerForTest(&publicMetadataRepository{accounts: []service.Account{account}})
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest("GET", "/v1beta/models", nil)
		ctx.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 1, Group: &service.Group{ID: 45, Platform: service.PlatformGemini}})
		if forced {
			ctx.Set(string(middleware.ContextKeyForcePlatform), platform)
		}
		gateway.GeminiV1BetaListModels(ctx)
		require.Equal(t, 200, recorder.Code)
		require.Contains(t, recorder.Body.String(), "models/gemini-public")
		require.NotContains(t, recorder.Body.String(), "gemini-private")
		require.Contains(t, recorder.Body.String(), "generateContent")
	}
}

func TestAppendUpstreamGeminiModelsPreservesMetadata(t *testing.T) {
	body := []byte(`{"models":[{"name":"models/gemini-native","inputTokenLimit":123,"custom":{"a":true}}],"nextPageToken":"next","other":42}`)
	extra := []gemini.Model{gemini.FallbackModel("gemini-native"), gemini.FallbackModel("gemini-synced-custom"), gemini.FallbackModel("gemini-synced-custom")}
	merged, ok := appendUpstreamGeminiModels(body, extra)
	require.True(t, ok)
	require.JSONEq(t, `{"models":[{"name":"models/gemini-native","inputTokenLimit":123,"custom":{"a":true}},{"name":"models/gemini-synced-custom","supportedGenerationMethods":["generateContent","streamGenerateContent"]}],"nextPageToken":"next","other":42}`, string(merged))
	filtered, dropped, ok := filterUpstreamGeminiModelsBody(merged, service.GroupModelAllowlist{Enabled: true, Models: []string{"gemini-synced-*"}})
	require.True(t, ok)
	require.True(t, dropped)
	require.NotContains(t, string(filtered), "gemini-native")
	require.Contains(t, string(filtered), "gemini-synced-custom")
	for _, invalid := range []string{`null`, `not-json`, `{"error":"bad"}`, `{"models":{}}`, `{"models":null}`, `{"Models":[]}`} {
		got, ok := appendUpstreamGeminiModels([]byte(invalid), extra)
		require.False(t, ok)
		require.Equal(t, invalid, string(got))
	}
}

type geminiDiscoveryAccountRepo struct {
	geminiAllowlistAccountRepoStub
	agErr error
}

func (r *geminiDiscoveryAccountRepo) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, groupID int64, platforms []string) ([]service.Account, error) {
	for _, platform := range platforms {
		if platform == service.PlatformAntigravity && r.agErr != nil {
			return nil, r.agErr
		}
	}
	return r.geminiAllowlistAccountRepoStub.ListSchedulableByGroupIDAndPlatforms(ctx, groupID, platforms)
}

func TestGeminiModelDiscoveryOptionalFailure(t *testing.T) {
	repo := &publicMetadataRepository{err: errors.New("private database endpoint secret-token")}
	gateway := newGatewayModelsHandlerForTest(repo)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/v1beta/models", nil)
	ctx.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{Group: &service.Group{ID: 47, Platform: service.PlatformGemini}})
	gateway.GeminiV1BetaListModels(ctx)
	require.Equal(t, 503, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "secret-token")
	require.Contains(t, recorder.Body.String(), "UNAVAILABLE")
}

func TestAntigravityGeminiModelIDsEffectiveMappings(t *testing.T) {
	id := int64(48)
	repo := &geminiAllowlistAccountRepoStub{gatewayModelsAccountRepoStub: gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{id: {
		{ID: 1, Platform: service.PlatformAntigravity, Extra: map[string]any{"mixed_scheduling": true}, Credentials: map[string]any{"model_mapping": map[string]any{
			"gemini-alias": "gemini-3.8-flash-high", "gemini-custom-*": "gemini-3.8-flash-high", "claude-hidden": "claude-sonnet-4-6",
		}}},
		{ID: 2, Platform: service.PlatformAntigravity, Extra: map[string]any{"mixed_scheduling": true}, Credentials: map[string]any{"model_mapping": map[string]any{"gemini-alias": "gemini-3.8-flash-low"}}},
		{ID: 3, Platform: service.PlatformAntigravity, Credentials: map[string]any{"model_mapping": map[string]any{"gemini-forced-only": "gemini-3.8-flash-high"}}},
	}}}}
	s := service.NewGeminiMessagesCompatService(repo, nil, nil, nil, nil, nil, nil, nil, nil)
	mixed, err := s.AntigravityGeminiModelIDs(context.Background(), &id, true)
	require.NoError(t, err)
	require.Contains(t, mixed, "gemini-alias")
	// Listing retains mapping keys, including patterns, as the existing gateway does.
	require.Contains(t, mixed, "gemini-custom-*")
	require.NotContains(t, mixed, "claude-hidden")
	require.NotContains(t, mixed, "gemini-forced-only")
	require.IsIncreasing(t, mixed)
	forced, err := s.AntigravityGeminiModelIDs(context.Background(), &id, false)
	require.NoError(t, err)
	require.Contains(t, forced, "gemini-forced-only")
}

// Exercise the real handler's native-upstream branch, including scope fallback.
type geminiMixedModelsUpstream struct {
	service.HTTPUpstream
	status int
	body   string
}

func (u *geminiMixedModelsUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return &http.Response{StatusCode: u.status, Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"native-id"}}, Body: io.NopCloser(strings.NewReader(u.body))}, nil
}

// Public requests read stored evidence, never fetch or splice an unbound upstream page.
func TestGeminiNativeModelsMergesNativeUpstream(t *testing.T) {
	for _, status := range []int{200, 403, 429} {
		upstream := &geminiMixedModelsUpstream{status: status, body: `{"models":[{"name":"models/unconfirmed"}],"nextPageToken":"unbound"}`}
		repo := &geminiDiscoveryAccountRepo{}
		gateway := newGatewayModelsHandlerForTest(repo)
		gateway.geminiCompatService = service.NewGeminiMessagesCompatService(repo, nil, nil, nil, nil, nil, upstream, nil, &config.Config{})
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest("GET", "/v1beta/models", nil)
		ctx.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{Group: &service.Group{ID: 48, Platform: service.PlatformGemini}})
		gateway.GeminiV1BetaListModels(ctx)
		require.Equal(t, 200, recorder.Code)
		require.JSONEq(t, `{"models":[]}`, recorder.Body.String())
		require.Empty(t, recorder.Header().Get("X-Request-Id"))
	}
}
