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
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/stretchr/testify/require"
)

func fixtureDefault(c *gin.Context) {
	c.Request = c.Request.WithContext(domain.WithDefaultWorkspaceID(c.Request.Context(), 17))
	c.Next()
}

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
	r.Use(fixtureDefault)
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

func TestCatalogCompareScenarioUsesExactQuantities(t *testing.T) {
	qs, err := scenarioQuantities(scenario{Input: "1000", Cached: "100", Output: "100", Requests: "2"})
	require.NoError(t, err)
	var amounts []string
	for _, q := range qs {
		amounts = append(amounts, q.Amount().String())
	}
	require.Equal(t, []string{"1800", "200", "200", "2"}, amounts)
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
		router.Use(fixtureDefault)
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

type multimodalCatalog struct{}

func (multimodalCatalog) Snapshot(_ context.Context, p catalog.Perspective, at time.Time) (catalog.Snapshot, error) {
	return catalog.Project(p, catalog.Input{Models: []catalog.Candidate{{Provider: "grok", Model: "video-catalog", Offers: []catalog.Offer{{CatalogOnly: true, Access: true}}}},
		Registry: []catalog.RegistryModel{{Provider: "grok", Model: "video-catalog", Actions: []string{"video.generate"}, Facts: catalog.ModelFacts{
			Facts: catalog.Facts{Modalities: []string{"video"}}, Evidence: catalog.Evidence{Source: "approved_registry"},
		}}}}, catalog.Window{}, at), nil
}

func TestCatalogOnlyMultimodalHTTPFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(fixtureDefault)
	Register(r.Group("/api/v2"), New(multimodalCatalog{}, nil, &config.Config{}), nil, nil)
	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v2/public/models?modality=video&action=video.generate", nil))
	require.Equal(t, 200, res.Code)
	var response struct{ Data page }
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
	require.Len(t, response.Data.Items, 1)
	model := response.Data.Items[0]
	require.Equal(t, "catalog_only", model.AvailabilityState)
	require.Equal(t, "listed", model.CatalogState)
	require.Equal(t, "unpriced", model.PriceState)
	require.Equal(t, "requires_scope", model.RouteState)
	require.Nil(t, model.Input)
}

func TestCatalogDescriptiveActionDoesNotInventProtocolCompatibility(t *testing.T) {
	snapshot, err := (multimodalCatalog{}).Snapshot(context.Background(), catalog.Perspective{}, time.Now())
	require.NoError(t, err)
	detail := snapshot.Details[0]
	detail.Capabilities = []catalog.Capability{{Action: "text.chat", Protocol: "openai_chat"}}
	f := filter{Values: map[string][]string{"action": {"video.generate"}, "protocol": {"openai_chat"}}}
	require.False(t, f.matches(detail))
	delete(f.Values, "protocol")
	require.True(t, f.matches(detail))
}

type availabilityCatalog struct{}

func (availabilityCatalog) Snapshot(_ context.Context, _ catalog.Perspective, at time.Time) (catalog.Snapshot, error) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return catalog.Snapshot{Version: "availability-v1", AsOf: at, Details: []catalog.Detail{
		{Model: catalog.Summary{ID: "a", DisplayName: "A catalog", AvailabilityState: "catalog_only", Actions: []string{" Chat ", "tool.image_generation", "tool.video_generation", "voice.tts", "text.embeddings", "session.realtime"}}},
		{Model: catalog.Summary{ID: "z", DisplayName: "Z live", AvailabilityState: "live", Actions: []string{" Chat ", "tool.image_generation", "tool.video_generation", "voice.tts", "text.embeddings", "session.realtime"}}},
		{Model: catalog.Summary{ID: "b", DisplayName: "a catalog", AvailabilityState: "catalog_only", Modalities: []string{"speech", "text"}}},
	}}, nil
}

func TestAvailabilityAndModalityPagination(t *testing.T) {
	engine := gin.New()
	engine.Use(fixtureDefault)
	Register(engine.Group("/api/v2"), New(availabilityCatalog{}, nil, &config.Config{}), nil, nil)
	read := func(query string, status int) page {
		t.Helper()
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v2/public/models?"+query, nil))
		require.Equal(t, status, response.Code, response.Body.String())
		var result struct{ Data page }
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		return result.Data
	}
	first := read("sort=availability&limit=1", 200)
	require.Equal(t, "z", first.Items[0].ID)
	require.NotNil(t, first.Next)
	second := read("sort=availability&limit=1&cursor="+url.QueryEscape(*first.Next), 200)
	require.Equal(t, "a", second.Items[0].ID)
	third := read("sort=availability&limit=1&cursor="+url.QueryEscape(*second.Next), 200)
	require.Equal(t, "b", third.Items[0].ID)
	require.Nil(t, third.Next)
	require.Equal(t, "a", read("sort=name&limit=1", 200).Items[0].ID)
	require.Equal(t, "z", read("availability_state=live", 200).Items[0].ID)
	require.Len(t, read("availability_state=catalog_only", 200).Items, 2)
	read("sort=availability&availability_state=live&cursor="+url.QueryEscape(*first.Next), 410)
	for _, query := range []string{"availability_state=invalid", "availability_state=", "availability_state=live&availability_state=catalog_only"} {
		read(query, 400)
	}
	for _, modality := range []string{"text", "image", "video", "audio", "embedding", "realtime"} {
		query := "modality=" + modality + "&limit=1&sort=availability"
		ids := []string{}
		for {
			result := read(query, 200)
			for _, item := range result.Items {
				ids = append(ids, item.ID)
			}
			if result.Next == nil {
				break
			}
			query = "modality=" + modality + "&limit=1&sort=availability&cursor=" + url.QueryEscape(*result.Next)
		}
		want := []string{"z", "a"}
		if modality == "audio" || modality == "text" {
			want = append(want, "b")
		}
		require.Equal(t, want, ids, modality)
	}
	// Numeric sorts retain their existing ID tie-break; availability and name
	// must not influence ties for context, price, or latency.
	a := catalog.Detail{Model: catalog.Summary{ID: "a", DisplayName: "Z", AvailabilityState: "catalog_only"}}
	b := catalog.Detail{Model: catalog.Summary{ID: "b", DisplayName: "A", AvailabilityState: "live"}}
	for _, order := range []string{"context", "price_input", "ttft"} {
		require.Less(t, compareModels(a, b, order), 0, order)
	}
	for _, perspective := range []catalog.Perspective{{}, {Workspace: 1, Project: 1}} {
		f, _, err := filters(url.Values{"availability_state": {"live"}, "sort": {"availability"}}, perspective)
		require.NoError(t, err)
		require.True(t, f.matches(b))
		require.False(t, f.matches(a))
	}
}
