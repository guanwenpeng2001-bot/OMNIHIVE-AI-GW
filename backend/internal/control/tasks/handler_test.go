package tasks

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/tasks"
	"github.com/stretchr/testify/require"
)

func TestImageTaskCenterSignedPagination(t *testing.T) {
	h := Handler{key: []byte("private-test-key")}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	page := func(q string) request {
		c.Request = httptest.NewRequest("GET", "/tasks?"+q, nil)
		return request{Context: c, a: domain.Scope{WorkspaceID: 2, ProjectID: 3, ActorID: 4, Version: 5}}
	}
	p, v, e := h.page(page("kind=image&state=running&limit=1"), false)
	require.NoError(t, e)
	v.After = "example"
	v.AfterTime = p.AsOf
	tk, e := h.sign(v)
	require.NoError(t, e)
	p, _, e = h.page(page("limit=1&state=running&kind=image&cursor="+tk), false)
	require.NoError(t, e)
	require.Equal(t, v.After, p.AfterID)
	_, _, e = h.page(page("limit=1&state=failed&kind=image&cursor="+tk), false)
	require.Error(t, e)
	_, _, e = h.page(page("unrecognized=1"), false)
	require.Error(t, e)
	_, _, e = h.page(page("from=2026-01-02T00:00:00Z&to=2026-01-01T00:00:00Z"), false)
	require.Error(t, e)
	v.Expires = time.Now().Add(-time.Hour)
	tk, e = h.sign(v)
	require.NoError(t, e)
	_, _, e = h.page(page("limit=1&state=running&kind=image&cursor="+tk), false)
	require.Error(t, e)
}
func TestImageTaskCenterTicketBinding(t *testing.T) {
	h := Handler{key: []byte("private-test-key")}
	base := token{Actor: 1, Workspace: 2, Project: 3, Version: 4, Resource: "task/a/content"}
	raw, e := h.sign(base)
	require.NoError(t, e)
	_, ok := h.verify(raw, base)
	require.True(t, ok)
	for _, mutate := range []func(*token){func(v *token) { v.Actor++ }, func(v *token) { v.Workspace++ }, func(v *token) { v.Project++ }, func(v *token) { v.Version++ }, func(v *token) { v.Resource = "another/artifact" }} {
		other := base
		mutate(&other)
		_, ok = h.verify(raw, other)
		require.False(t, ok)
	}
	_, ok = h.verify(raw+"junk", base)
	require.False(t, ok)
}
