package identity

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/stretchr/testify/require"
)

type grantsBackend struct {
	Backend
	grants domain.MemberGrants
}

func (s *grantsBackend) MemberGrants(_ context.Context, actor, w, member int64) (domain.MemberGrants, error) {
	if actor != 1 || w != 2 || member != s.grants.MemberID {
		return domain.MemberGrants{}, fault.NotFound
	}
	return s.grants, nil
}

func (s *grantsBackend) SetGrants(_ context.Context, actor, w, member, version int64, ids []int64, actions []string) (domain.GrantVersion, error) {
	if actor != 1 || w != 2 || member != s.grants.MemberID {
		return domain.GrantVersion{}, fault.NotFound
	}
	if version != s.grants.Version {
		return domain.GrantVersion{}, fault.Stale(s.grants.Version)
	}
	s.grants.Version++
	s.grants.ProjectIDs = []string{}
	for _, id := range ids {
		s.grants.ProjectIDs = append(s.grants.ProjectIDs, strconv.FormatInt(id, 10))
	}
	s.grants.Actions = actions
	return s.grants.GrantVersion, nil
}

func TestMemberGrantsRoundTrip(t *testing.T) {
	initial := domain.MemberGrants{GrantVersion: domain.GrantVersion{MemberID: 7, Version: 19},
		ProjectIDs: []string{"5"}, Actions: []string{"keys.read"}}
	engine := gin.New()
	Register(engine.Group("/api/v2"), New(&grantsBackend{grants: initial}, domain.Operations{}, &config.Config{}),
		func(c *gin.Context) { c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1}) })
	call := func(method, body, match string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v2/workspaces/2/members/7/grants", strings.NewReader(body))
		req.Header.Set("If-Match", match)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}
	read := func() (domain.MemberGrants, string) {
		t.Helper()
		r := call("GET", "", "")
		require.Equal(t, 200, r.Code)
		var result struct{ Data domain.MemberGrants }
		require.NoError(t, json.Unmarshal(r.Body.Bytes(), &result))
		etag := r.Header().Get("ETag")
		require.Equal(t, strconv.Quote(strconv.FormatInt(result.Data.Version, 10)), etag)
		return result.Data, etag
	}
	current, etag := read()
	require.Equal(t, initial, current)
	body := `{"project_ids":["6","8"],"actions":["usage.read"]}`
	updated := call("PUT", body, etag)
	require.Equal(t, 200, updated.Code)
	current, newTag := read()
	require.Equal(t, updated.Header().Get("ETag"), newTag)
	require.NotEqual(t, etag, newTag)
	require.Equal(t, []string{"6", "8"}, current.ProjectIDs)
	require.Equal(t, []string{"usage.read"}, current.Actions)
	require.Equal(t, 412, call("PUT", body, etag).Code)
	require.Equal(t, 200, call("PUT", body, newTag).Code)
}
