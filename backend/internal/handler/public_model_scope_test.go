package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type publicModelScopeRepository struct {
	publicMetadataRepository
	t       *testing.T
	groupID *int64
	reads   int
}

func (repo *publicModelScopeRepository) ListModelCatalogAccounts(_ context.Context, groupID *int64, platforms []string, includeGrouped bool) ([]service.Account, error) {
	repo.t.Helper()
	repo.reads++
	require.Equal(repo.t, repo.groupID, groupID)
	require.Equal(repo.t, domain.ConcretePlatforms(), platforms)
	require.False(repo.t, includeGrouped)
	accounts := []service.Account{}
	for _, account := range repo.accounts {
		if !slices.Contains(platforms, account.Platform) {
			continue
		}
		if groupID != nil && !slices.Contains(account.GroupIDs, *groupID) {
			continue
		}
		if groupID == nil && !includeGrouped && len(account.GroupIDs) > 0 {
			continue
		}
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func (repo *publicModelScopeRepository) ListSchedulableByGroupID(_ context.Context, groupID int64) ([]service.Account, error) {
	accounts := []service.Account{}
	for _, account := range repo.accounts {
		if slices.Contains(account.GroupIDs, groupID) {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

func TestPublicModelsAllPlatformsScope(t *testing.T) {
	for _, group := range []*service.Group{{ID: 9, Platform: service.PlatformComposite}, {ID: 9, Platform: ""}, nil} {
		for _, catalog := range []bool{false, true} {
			for _, policy := range []struct {
				name   string
				models []string
				want   []string
			}{
				{name: "all selected models", want: []string{"gpt-alias", "denied"}},
				{name: "key excludes selected model", models: []string{"gpt-alias"}, want: []string{"gpt-alias"}},
				{name: "key allows selected model without metadata", models: []string{"denied"}, want: []string{"denied"}},
				{name: "key cannot grant outside group or unconfigured models", models: []string{"outside", "unconfigured"}, want: []string{}},
			} {
				t.Run(fmt.Sprintf("group=%v/catalog=%t/%s", group, catalog, policy.name), func(t *testing.T) {
					account := publicMetadataAccount()
					outsider := publicMetadataAccount()
					outsider.ID = 92
					outsider.GroupIDs = []int64{10}
					outsider.Credentials = map[string]any{"model_mapping": map[string]any{"outside": "gpt-native"}}
					repo := &publicModelScopeRepository{t: t}
					if group != nil {
						account.GroupIDs = []int64{group.ID}
						repo.groupID = &group.ID
					}
					repo.accounts = []service.Account{account, outsider}
					handler := newGatewayModelsHandlerForTest(repo)
					recorder := httptest.NewRecorder()
					ctx, _ := gin.CreateTestContext(recorder)
					ctx.Request = httptest.NewRequest("GET", "/v1/models", nil)
					ctx.Request = ctx.Request.WithContext(service.WithVisibleModelKeyPolicy(ctx.Request.Context(), policy.models))
					ctx.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 17, GroupID: repo.groupID, Group: group})
					if catalog {
						handler.ModelCatalog(ctx)
					} else {
						handler.Models(ctx)
					}
					require.Equal(t, 200, recorder.Code, recorder.Body.String())
					require.Equal(t, 1, repo.reads)
					var response struct {
						Data []struct {
							ID string `json:"id"`
						} `json:"data"`
					}
					require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
					ids := make([]string, 0, len(response.Data))
					for _, model := range response.Data {
						ids = append(ids, model.ID)
					}
					require.ElementsMatch(t, policy.want, ids)
					require.NotContains(t, ids, "outside", "another group's configured alias is never visible")
					require.NotContains(t, ids, "gpt-native", "mapping targets are not public aliases")
				})
			}
		}
	}
}
