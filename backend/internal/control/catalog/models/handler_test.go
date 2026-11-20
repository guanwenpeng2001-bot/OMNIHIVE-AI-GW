package models

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
)

type fixtureCatalog struct{ revision string }

func (s *fixtureCatalog) Snapshot(_ context.Context, p catalog.Perspective, at time.Time) (catalog.Snapshot, error) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	input := catalog.Input{RegistryVersion: s.revision, Models: []catalog.Candidate{
		{Provider: "openai", Model: "sample-a"}, {Provider: "openai", Model: "sample-b"}, {Provider: "gemini", Model: "sample-c"},
	}}
	for _, model := range input.Models {
		input.Registry = append(input.Registry, catalog.RegistryModel{Provider: model.Provider, Model: model.Model,
			DisplayName: "Fixture", Facts: catalog.ModelFacts{Evidence: catalog.Observation("approved_registry", "stable", time.Time{}, time.Time{}, at)}})
	}
	return catalog.Project(p, input, catalog.Window{}, at), nil
}

func TestCatalogCursorBindsCurrentSnapshotAndFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	source := &fixtureCatalog{revision: "one"}
	Register(r.Group("/api/v2"), New(source, nil, &config.Config{}), nil, nil)
	const path = "/api/v2/public/models"
	read := func(query string, status int) page {
		t.Helper()
		res := httptest.NewRecorder()
		r.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path+query, nil))
		require.Equal(t, status, res.Code, res.Body.String())
		var response struct{ Data page }
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
		return response.Data
	}
	first := read("?provider=openai&limit=1", 200)
	require.Len(t, first.Items, 1)
	require.NotNil(t, first.Next)
	token := url.QueryEscape(*first.Next)
	second := read("?limit=1&provider=openai&cursor="+token, 200)
	require.Len(t, second.Items, 1)
	require.NotEqual(t, first.Items[0].ID, second.Items[0].ID)
	require.Nil(t, second.Next)
	require.Equal(t, first.AsOf, second.AsOf)
	require.Equal(t, first.Version, second.Version)
	read("?provider=gemini&cursor="+token, 410)
	read("?cursor="+token+"-tampered", 422)
	read("?accessible=true", 422)
	source.revision = "two"
	read("?provider=openai&cursor="+token, 410)
}

func TestCatalogExactCompareEstimate(t *testing.T) {
	rate := func(unit money.Unit, amount string) *money.Rate {
		r, err := money.NewRate("USD", amount, unit, "1000000")
		require.NoError(t, err)
		return &r
	}
	v := "ref"
	d := catalog.Detail{PriceVersion: &v, Model: catalog.Summary{Input: rate("input_token", "1.25"), Output: rate("output_token", "10")},
		AdditionalRates: []catalog.RateBand{{Kind: "cache_read", Rate: *rate("cache_read_token", "0.5")}}}
	qs, err := scenarioQuantities(scenario{Input: "1000", Cached: "100", Output: "100", Requests: "2"})
	require.NoError(t, err)
	result := simpleEstimate(d, qs)
	require.Equal(t, "estimated", result.State)
	require.Equal(t, "0.00435000", result.Charge.String())
	require.Len(t, result.Components, 3)
	require.Equal(t, "1800", result.Components[0].Quantity)
	require.Equal(t, "200", result.Components[1].Quantity)
	_, err = scenarioQuantities(scenario{Input: "1000", Cached: "1001"})
	require.Error(t, err)
}

type detailCatalog struct{}

func (detailCatalog) Snapshot(context.Context, catalog.Perspective, time.Time) (catalog.Snapshot, error) {
	const model = "auto/gemini-3.8-flash"
	return catalog.Snapshot{Details: []catalog.Detail{
		{Model: catalog.Summary{ID: catalog.StableID("openai", model), Provider: "openai", Model: model}},
	}}, nil
}

type catalogDetailAccess struct{}

func (catalogDetailAccess) Evaluate(_ context.Context, actor, workspace, project int64, action string) (identity.Access, error) {
	return identity.Access{ActorID: actor, WorkspaceID: workspace, ProjectID: project, Actions: []string{action}}, nil
}

func TestCatalogDetailResolvesCanonicalModelSlugs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	modelID := catalog.StableID("openai", "auto/gemini-3.8-flash")
	for _, rawPath := range []bool{false, true} {
		router := gin.New()
		router.UseRawPath = rawPath
		auth := func(c *gin.Context) {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
			c.Next()
		}
		Register(router.Group("/api/v2"), New(detailCatalog{}, catalogDetailAccess{}, &config.Config{}), nil, []gin.HandlerFunc{auth})
		for _, prefix := range []string{"/api/v2/public/models/", "/api/v2/workspaces/1/projects/2/models/"} {
			for _, identifier := range []string{
				modelID,
				"auto%2Fgemini-3.8-flash",
				"openai%2Fauto%2Fgemini-3.8-flash",
				"auto/gemini-3.8-flash",
				"openai/auto/gemini-3.8-flash",
			} {
				path := prefix + identifier
				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
				require.Equal(t, http.StatusOK, w.Code, "%s (UseRawPath=%t): %s", path, rawPath, w.Body.String())
				var response struct{ Data catalog.Detail }
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
				require.Equal(t, modelID, response.Data.Model.ID)
				require.Equal(t, "openai", response.Data.Model.Provider)
				require.Equal(t, "auto/gemini-3.8-flash", response.Data.Model.Model)
			}
		}
	}
}
