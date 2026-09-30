package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type catalogAvailableSchedulingQueryFails struct{ *gatewayModelsAccountRepoStub }

func (repo *catalogAvailableSchedulingQueryFails) ListSchedulableByGroupID(context.Context, int64) ([]service.Account, error) {
	return nil, errors.New("scheduling query unavailable after catalog lookup")
}

func TestGatewayModelsDenyAllDoesNotFallback(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformDeepseek, service.PlatformMiniMax} {
		for _, codex := range []bool{false, true} {
			for _, schedulingFailure := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/codex=%t/query_failure=%t", platform, codex, schedulingFailure), func(t *testing.T) {
					group := &service.Group{ID: 91, Platform: platform}
					repo := &gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{91: {{ID: 9, Platform: platform, Credentials: map[string]any{"model_mapping": map[string]any{}}, Extra: map[string]any{"openai_passthrough": true, service.UpstreamModelAllowlistExtraKey: service.AccountModelAllowlist{Enabled: true}}}}}}
					var accountRepo service.AccountRepository = repo
					if schedulingFailure {
						accountRepo = &catalogAvailableSchedulingQueryFails{repo}
					}
					handler := newGatewayModelsHandlerForTest(accountRepo, &publicListingGroupRepository{current: group})
					recorder := httptest.NewRecorder()
					request, _ := gin.CreateTestContext(recorder)
					request.Request = httptest.NewRequest(http.MethodGet, "/v1/models?client_version=0.147.0", nil).WithContext(context.WithValue(context.Background(), ctxkey.Group, group))
					request.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{Group: group, GroupID: &group.ID})
					if codex {
						handler.CodexModels(request)
					} else {
						handler.Models(request)
					}
					require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
					field := "data"
					if codex {
						field = "models"
					}
					require.True(t, gjson.Get(recorder.Body.String(), field).IsArray(), recorder.Body.String())
					require.Empty(t, gjson.Get(recorder.Body.String(), field).Array(), "deny-all must not publish default model members")
				})
			}
		}
	}
}
