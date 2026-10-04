package admin

import (
	"context"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/testutil/testassert"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type groupAdminRepository struct {
	service.ProxyGroupRepository
	group                              service.ProxyGroup
	members                            []service.ProxyGroupMember
	accounts                           []service.ProxyAccountSummary
	nodes                              map[int64]*service.Proxy
	replacements                       int
	mutations                          int
	listStatus, listTarget, listSearch string
	listPage                           pagination.PaginationParams
}

func (r *groupAdminRepository) GetByID(_ context.Context, id int64) (*service.ProxyGroup, error) {
	if id != r.group.ID || r.group.DeletedAt != nil {
		return nil, service.ErrProxyGroupNotFound
	}
	g := r.group
	return &g, nil
}
func (r *groupAdminRepository) List(_ context.Context, p pagination.PaginationParams, status, target, search string) ([]service.ProxyGroup, *pagination.PaginationResult, error) {
	r.listStatus, r.listTarget, r.listSearch, r.listPage = status, target, search, p
	return []service.ProxyGroup{r.group}, &pagination.PaginationResult{Total: 51, Page: p.Page, PageSize: p.PageSize}, nil
}
func (r *groupAdminRepository) Create(_ context.Context, g *service.ProxyGroup) error {
	r.mutations++
	g.ID = 201
	r.group = *g
	return nil
}
func (r *groupAdminRepository) Update(_ context.Context, g *service.ProxyGroup) error {
	r.mutations++
	r.group = *g
	return nil
}
func (r *groupAdminRepository) Delete(_ context.Context, id int64) error {
	if len(r.accounts) > 0 {
		return service.ErrProxyGroupInUse
	}
	r.mutations++
	now := time.Now()
	r.group.DeletedAt = &now
	return nil
}
func (r *groupAdminRepository) ListMembers(context.Context, int64) ([]service.ProxyGroupMember, error) {
	return r.members, nil
}
func (r *groupAdminRepository) CountAccountsByGroupID(context.Context, int64) (int64, error) {
	return int64(len(r.accounts)), nil
}
func (r *groupAdminRepository) ListAccountSummariesByGroupID(context.Context, int64) ([]service.ProxyAccountSummary, error) {
	return r.accounts, nil
}
func (r *groupAdminRepository) ReplaceMembers(_ context.Context, id int64, ids []int64) error {
	r.replacements++
	r.members = nil
	seen := map[int64]bool{}
	for _, id := range ids {
		if !seen[id] {
			r.members = append(r.members, service.ProxyGroupMember{ProxyID: id, Proxy: r.nodes[id], WorkspaceID: 2})
			seen[id] = true
		}
	}
	return nil
}

type groupAdminProxyRepository struct {
	service.ProxyRepository
	nodes map[int64]*service.Proxy
}

func (r *groupAdminProxyRepository) GetByID(ctx context.Context, id int64) (*service.Proxy, error) {
	p := r.nodes[id]
	if p == nil || p.DeletedAt != nil {
		return nil, service.ErrProxyNotFound
	}
	if ws, ok := domain.WorkspaceScope(ctx); ok && ws != p.WorkspaceID {
		return nil, service.ErrWorkspaceNotFound
	}
	return p, nil
}

type groupAdminCache struct {
	service.ProxyGroupCache // Unexpected writes (NX, touch, cooldown renewal, scan) panic.
	bindings                map[int64]int64
	ttls                    map[int64]time.Duration
	cooldowns               map[int64]*service.ProxyCooldown
	concurrentWinner        int64
	clears                  int
	reads                   int
}

func (c *groupAdminCache) GetBinding(_ context.Context, g, a int64) (int64, error) {
	c.reads++
	return c.bindings[a], nil
}
func (c *groupAdminCache) GetBindingTTL(_ context.Context, g, a int64) (time.Duration, error) {
	return c.ttls[a], nil
}
func (c *groupAdminCache) GetCooldown(_ context.Context, id int64) (*service.ProxyCooldown, error) {
	return c.cooldowns[id], nil
}
func (c *groupAdminCache) ClearCooldown(_ context.Context, id int64) error {
	c.clears++
	delete(c.cooldowns, id)
	return nil
}
func (c *groupAdminCache) DeleteBindingIfValue(_ context.Context, g, a, p int64) (bool, error) {
	if c.concurrentWinner > 0 {
		c.bindings[a] = c.concurrentWinner
		c.concurrentWinner = 0
	}
	if c.bindings[a] != p {
		return false, nil
	}
	delete(c.bindings, a)
	return true, nil
}

type groupAdminWorkspaceRepository struct{ service.WorkspaceRepository }

func (r *groupAdminWorkspaceRepository) ContainsResources(_ context.Context, ws int64, resource string, ids []int64) (bool, error) {
	switch resource {
	case "proxy_groups", "proxies":
	default:
		panic("unexpected workspace resource: " + resource)
	}
	for _, id := range ids {
		if id/100 != ws {
			return false, nil
		}
	}
	return true, nil
}

type groupAdminQualityService struct {
	service.AdminService
	result *service.ProxyQualityCheckResult
	calls  int
}

func (s *groupAdminQualityService) CheckProxyQuality(context.Context, int64) (*service.ProxyQualityCheckResult, error) {
	s.calls++
	return s.result, nil
}

type groupAdminFixture struct {
	repo    *groupAdminRepository
	cache   *groupAdminCache
	quality *groupAdminQualityService
	router  *gin.Engine
}

func newGroupAdminFixture(t *testing.T) *groupAdminFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	nodes := map[int64]*service.Proxy{
		211: {ID: 211, WorkspaceID: 2, Name: "unknown quality", Status: service.StatusActive, Protocol: "http", Host: "example.invalid", Port: 80, Username: "test", Password: "credential-secret"},
		212: {ID: 212, WorkspaceID: 2, Name: "inactive", Status: "inactive"},
		111: {ID: 111, WorkspaceID: 1, Name: "other workspace", Status: service.StatusActive},
	}
	repo := &groupAdminRepository{group: service.ProxyGroup{ID: 201, WorkspaceID: 2, Name: "original", Description: "keep", Region: "JP", TargetServices: []string{"grok"}, Status: service.StatusActive, FallbackMode: service.FallbackModeNone, BindingTTLSeconds: 86400}, nodes: nodes}
	cache := &groupAdminCache{bindings: map[int64]int64{}, ttls: map[int64]time.Duration{}, cooldowns: map[int64]*service.ProxyCooldown{}}
	quality := &groupAdminQualityService{result: &service.ProxyQualityCheckResult{ProxyID: 211}}
	svc := service.NewProxyGroupAdminService(repo, &groupAdminProxyRepository{nodes: nodes}, cache)
	h := NewProxyGroupHandler(svc, quality)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		role := service.RoleAdmin
		if c.GetHeader("X-Test-Role") != "" {
			role = c.GetHeader("X-Test-Role")
		}
		c.Set(string(middleware.ContextKeyUserRole), role)
		c.Request = c.Request.WithContext(domain.WithWorkspaceScope(c.Request.Context(), 2))
	})
	router.Use(middleware.AdminOnly(), middleware.WorkspaceResources(service.NewWorkspaceService(&groupAdminWorkspaceRepository{})))
	g := router.Group("/api/v1/admin/proxy-groups")
	g.GET("", h.List)
	g.POST("", h.Create)
	g.GET("/:id", h.GetByID)
	g.PUT("/:id", h.Update)
	g.DELETE("/:id", h.Delete)
	g.PUT("/:id/members", h.ReplaceMembers)
	g.GET("/:id/bindings", h.Bindings)
	g.DELETE("/:id/bindings", h.ResetBindings)
	router.POST("/api/v1/admin/proxies/:id/probe-now", h.ProbeNow)
	return &groupAdminFixture{repo, cache, quality, router}
}
func (f *groupAdminFixture) request(t *testing.T, method, path, body string) (int, map[string]any, string) {
	t.Helper()
	req := httptest.NewRequest(method, "/api/v1/admin/"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	data, _ := out["data"].(map[string]any)
	return w.Code, data, w.Body.String()
}

func TestProxyGroupPartialCRUDAndSafeDetail(t *testing.T) {
	f := newGroupAdminFixture(t)
	code, data, _ := f.request(t, "POST", "proxy-groups", `{"name":" new "}`)
	require.Equal(t, 200, code)
	require.Equal(t, "new", data["name"])
	require.Equal(t, float64(86400), data["binding_ttl_seconds"])
	require.Equal(t, "active", data["status"])
	require.Equal(t, "none", data["fallback_mode"])
	code, _, _ = f.request(t, "PUT", "proxy-groups/201", `{"description":"retained","target_services":["grok"],"fallback_mode":"proxy","fallback_proxy_id":211}`)
	require.Equal(t, 200, code)
	code, data, _ = f.request(t, "PUT", "proxy-groups/201", `{"name":"renamed"}`)
	require.Equal(t, 200, code)
	require.Equal(t, "retained", data["description"])
	require.Equal(t, float64(211), data["fallback_proxy_id"])
	require.Equal(t, []any{"grok"}, data["target_services"])
	for _, clear := range []string{"0", "null"} {
		code, _, _ = f.request(t, "PUT", "proxy-groups/201", `{"fallback_mode":"proxy","fallback_proxy_id":211}`)
		require.Equal(t, 200, code)
		code, data, _ = f.request(t, "PUT", "proxy-groups/201", `{"fallback_mode":"none","fallback_proxy_id":`+clear+`,"description":"","target_services":[]}`)
		require.Equal(t, 200, code)
		require.Nil(t, data["fallback_proxy_id"])
		require.Equal(t, "", data["description"])
		require.Empty(t, data["target_services"])
	}
	code, _, _ = f.request(t, "PUT", "proxy-groups/201/members", `{"proxy_ids":[211,212]}`)
	require.Equal(t, 200, code)
	f.cache.cooldowns[211] = &service.ProxyCooldown{ProxyID: 211, ExpiresAt: time.Now().Add(time.Minute), Reason: "http://test:credential-secret@example.invalid"}
	code, data, raw := f.request(t, "GET", "proxy-groups/201", "")
	require.Equal(t, 200, code)
	require.Equal(t, float64(2), data["member_count"])
	require.Equal(t, float64(0), data["schedulable_member_count"])
	require.NotContains(t, raw, "credential-secret")
	require.NotContains(t, raw, "password")
	require.NotContains(t, raw, "http://")
	f.repo.accounts = []service.ProxyAccountSummary{{ID: 221, Name: "logical account"}}
	code, _, raw = f.request(t, "DELETE", "proxy-groups/201", "")
	require.Equal(t, 409, code)
	require.Contains(t, raw, "PROXY_GROUP_IN_USE")
	f.repo.accounts = nil
	code, _, _ = f.request(t, "DELETE", "proxy-groups/201", "")
	require.Equal(t, 200, code)
	code, _, _ = f.request(t, "GET", "proxy-groups/201", "")
	require.Equal(t, 404, code)
	require.Empty(t, f.cache.bindings, "CRUD must never auto-bind accounts")
}

func TestProxyGroupAtomicMembersAndEligibility(t *testing.T) {
	f := newGroupAdminFixture(t)
	expired := time.Now().Add(-time.Minute)
	deleted := time.Now()
	f.repo.nodes[213] = &service.Proxy{ID: 213, WorkspaceID: 2, Status: service.StatusActive, ExpiresAt: &expired}
	f.repo.nodes[214] = &service.Proxy{ID: 214, WorkspaceID: 2, Status: service.StatusActive, DeletedAt: &deleted}
	code, data, _ := f.request(t, "PUT", "proxy-groups/201/members", `{"proxy_ids":[211,212,213]}`)
	require.Equal(t, 200, code)
	require.Equal(t, float64(3), data["member_count"])
	require.Equal(t, float64(1), data["schedulable_member_count"])
	for _, payload := range []string{`{"proxy_ids":[211,111]}`, `{"proxy_ids":[211,214]}`, `{"proxy_ids":[211,299]}`, `{"proxy_ids":[0]}`, `{}`, `{"proxy_ids":null}`} {
		code, _, _ = f.request(t, "PUT", "proxy-groups/201/members", payload)
		require.GreaterOrEqual(t, code, 400)
		require.Equal(t, 1, f.repo.replacements)
		require.Len(t, f.repo.members, 3)
	}
	code, data, _ = f.request(t, "PUT", "proxy-groups/201/members", `{"proxy_ids":[]}`)
	require.Equal(t, 200, code)
	require.Equal(t, float64(0), data["member_count"])
	require.Empty(t, data["members"])
}

func TestProxyGroupPermissionAndWorkspaceGuards(t *testing.T) {
	f := newGroupAdminFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/proxy-groups", nil)
	req.Header.Set("X-Test-Role", service.RoleUser)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	require.Equal(t, 403, w.Code)
	for _, path := range []string{"proxy-groups/101", "proxy-groups/101/bindings"} {
		code, _, _ := f.request(t, "GET", path, "")
		require.Equal(t, 404, code)
	}
	for _, body := range []string{`{"fallback_mode":"proxy","fallback_proxy_id":111}`, `{"proxy_group_id":101}`} {
		code, _, _ := f.request(t, "PUT", "proxy-groups/201", body)
		require.Equal(t, 404, code)
	}
	code, _, _ := f.request(t, "POST", "proxies/111/probe-now", "")
	require.Equal(t, 404, code)
	require.Zero(t, f.quality.calls)
	require.Zero(t, f.repo.mutations)
	require.Zero(t, f.cache.reads)
	// Defense in depth when a repository returns a wrong-workspace group.
	f.repo.group.WorkspaceID = 1
	code, _, _ = f.request(t, "GET", "proxy-groups/201", "")
	require.Equal(t, 404, code)
}

func TestProxyGroupPaginationAndInvalidUpdates(t *testing.T) {
	f := newGroupAdminFixture(t)
	code, data, _ := f.request(t, "GET", "proxy-groups?page=3&page_size=7&target_service=grok&status=inactive&search=Japan", "")
	require.Equal(t, 200, code)
	require.Equal(t, float64(51), data["total"])
	require.Equal(t, 3, f.repo.listPage.Page)
	require.Equal(t, 7, f.repo.listPage.PageSize)
	require.Equal(t, "grok", f.repo.listTarget)
	require.Equal(t, "inactive", f.repo.listStatus)
	require.Equal(t, "Japan", f.repo.listSearch)
	for _, body := range []string{`{"name":" "}`, `{"status":"bad"}`, `{"binding_ttl_seconds":0}`, `{"binding_ttl_seconds":-1}`, `{"fallback_mode":"proxy"}`, `{"fallback_proxy_id":-2}`, `{"fallback_mode":"bad"}`} {
		code, _, _ = f.request(t, "PUT", "proxy-groups/201", body)
		require.Equal(t, 400, code)
		require.Equal(t, "original", f.repo.group.Name)
		require.Zero(t, f.repo.mutations)
	}
}

func TestProxyGroupBindingObservationTTLAndCASReset(t *testing.T) {
	f := newGroupAdminFixture(t)
	f.repo.accounts = []service.ProxyAccountSummary{{ID: 221, Name: "bound"}, {ID: 222, Name: "unbound"}, {ID: 223, Name: "stale foreign"}}
	f.repo.members = []service.ProxyGroupMember{{ProxyID: 211, Proxy: f.repo.nodes[211]}}
	f.cache.bindings[221] = 211
	f.cache.ttls[221] = 90 * time.Second
	f.cache.bindings[223] = 111
	f.cache.ttls[223] = time.Minute
	f.cache.bindings[999] = 211 // Not logically in this group: must never enumerate or reset.
	before := time.Now()
	code, data, _ := f.request(t, "GET", "proxy-groups/201/bindings", "")
	require.Equal(t, 200, code)
	require.Equal(t, float64(3), data["total"])
	items := testassert.Type[[]any](t, data["items"])
	bound := testassert.Type[map[string]any](t, items[0])
	require.Equal(t, true, bound["eligible"])
	require.Equal(t, float64(211), bound["proxy_id"])
	expires, err := time.Parse(time.RFC3339Nano, testassert.Type[string](t, bound["expires_at"]))
	require.NoError(t, err)
	require.WithinDuration(t, before.Add(90*time.Second), expires, 2*time.Second)
	require.Nil(t, testassert.Type[map[string]any](t, items[1])["proxy_id"])
	require.Nil(t, testassert.Type[map[string]any](t, items[2])["proxy_id"])
	f.repo.group.Status = "inactive"
	f.repo.group.FallbackMode = service.FallbackModeDirect
	code, data, _ = f.request(t, "GET", "proxy-groups/201/bindings", "")
	require.Equal(t, 200, code)
	require.Equal(t, false, testassert.Type[map[string]any](t, testassert.Type[[]any](t, data["items"])[0])["eligible"])
	f.cache.concurrentWinner = 212
	code, data, _ = f.request(t, "DELETE", "proxy-groups/201/bindings", "")
	require.Equal(t, 200, code)
	require.Equal(t, float64(1), data["cleared"])
	require.Equal(t, int64(212), f.cache.bindings[221])
	require.Equal(t, int64(211), f.cache.bindings[999])
	f.cache.ttls[221] = 0
	code, data, _ = f.request(t, "GET", "proxy-groups/201/bindings", "")
	require.Equal(t, 200, code)
	require.Nil(t, testassert.Type[map[string]any](t, testassert.Type[[]any](t, data["items"])[0])["expires_at"])
}

func TestProxyGroupProbeRecoveryRequiresReachability(t *testing.T) {
	for _, tc := range []struct {
		name      string
		item      service.ProxyQualityCheckItem
		recovered bool
	}{
		{"failed", service.ProxyQualityCheckItem{Target: "base_connectivity", Status: "fail", Message: "http://test:credential-secret@example.invalid"}, false},
		{"echo reachable", service.ProxyQualityCheckItem{Target: "base_connectivity", Status: "pass"}, true},
		{"target rejects", service.ProxyQualityCheckItem{Target: "provider", Status: "fail", HTTPStatus: 403}, true},
		{"unknown", service.ProxyQualityCheckItem{Target: "provider", Status: "warn"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newGroupAdminFixture(t)
			until := time.Now().Add(time.Minute)
			f.cache.cooldowns[211] = &service.ProxyCooldown{ProxyID: 211, ExpiresAt: until}
			f.quality.result.Items = []service.ProxyQualityCheckItem{tc.item}
			code, data, raw := f.request(t, "POST", "proxies/211/probe-now", "")
			require.Equal(t, 200, code)
			require.Equal(t, tc.recovered, data["cooldown_cleared"])
			require.NotContains(t, raw, "credential-secret")
			if tc.recovered {
				require.Equal(t, 1, f.cache.clears)
			} else {
				require.Zero(t, f.cache.clears)
				require.Equal(t, until, f.cache.cooldowns[211].ExpiresAt)
			}
		})
	}
}

func TestProxyGroupShadowBindingsUseCanonicalOwner(t *testing.T) {
	f := newGroupAdminFixture(t)
	parentID := int64(221)
	f.repo.accounts = []service.ProxyAccountSummary{
		{ID: 222, Name: "shadow one", ParentAccountID: &parentID},
		{ID: 221, Name: "parent"},
		{ID: 223, Name: "shadow two", ParentAccountID: &parentID},
		{ID: 224, Name: "normal"},
	}
	f.repo.members = []service.ProxyGroupMember{{ProxyID: 211, Proxy: f.repo.nodes[211]}}
	f.cache.bindings[221], f.cache.bindings[224] = 211, 211
	f.cache.bindings[222] = 212 // A stale logical-shadow key is not the runtime owner.
	f.cache.ttls[221], f.cache.ttls[224] = 45*time.Second, 90*time.Second
	before := time.Now()
	code, data, raw := f.request(t, "GET", "proxy-groups/201/bindings", "")
	require.Equal(t, 200, code)
	require.Equal(t, 4, f.cache.reads, "two consistency reads per distinct bound owner")
	require.NotContains(t, raw, "parent_account_id")
	require.NotContains(t, raw, "credential-secret")
	items := testassert.Type[[]any](t, data["items"])
	require.Len(t, items, 4)
	for i, account := range f.repo.accounts {
		item := testassert.Type[map[string]any](t, items[i])
		require.Equal(t, float64(account.ID), item["account_id"])
		require.Equal(t, account.Name, item["account_name"])
		require.Equal(t, float64(211), item["proxy_id"])
		require.Equal(t, true, item["eligible"])
		expires, err := time.Parse(time.RFC3339Nano, testassert.Type[string](t, item["expires_at"]))
		require.NoError(t, err)
		ttl := 45 * time.Second
		if account.ID == 224 {
			ttl = 90 * time.Second
		}
		require.WithinDuration(t, before.Add(ttl), expires, 2*time.Second)
	}
	require.Equal(t, testassert.Type[map[string]any](t, items[0])["expires_at"], testassert.Type[map[string]any](t, items[2])["expires_at"])
	f.cache.concurrentWinner = 212
	f.cache.reads = 0
	code, data, _ = f.request(t, "DELETE", "proxy-groups/201/bindings", "")
	require.Equal(t, 200, code)
	require.Equal(t, float64(1), data["cleared"])
	require.Equal(t, 2, f.cache.reads)
	require.EqualValues(t, 212, f.cache.bindings[221], "duplicate shadows must not retry CAS against the replacement")
	require.EqualValues(t, 212, f.cache.bindings[222], "only canonical keys are reset")
	f.cache.concurrentWinner = 0
	code, data, _ = f.request(t, "DELETE", "proxy-groups/201/bindings", "")
	require.Equal(t, 200, code)
	require.Equal(t, float64(1), data["cleared"], "one owner, not three displayed rows")
	encoded, err := json.Marshal(f.repo.accounts[0])
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "ParentAccountID")
}

func TestProxyGroupExplicitBackupBindingEligibility(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		configure func(*groupAdminFixture)
		eligible  bool
	}{
		{name: "configured nonmember backup", eligible: true},
		{name: "unconfigured nonmember", configure: func(fixture *groupAdminFixture) { fixture.repo.group.FallbackProxyID = nil }},
		{name: "different configured backup", configure: func(fixture *groupAdminFixture) {
			differentID := int64(212)
			fixture.repo.group.FallbackProxyID = &differentID
		}},
		{name: "proxy fallback mode required", configure: func(fixture *groupAdminFixture) { fixture.repo.group.FallbackMode = service.FallbackModeDirect }},
		{name: "inactive group", configure: func(fixture *groupAdminFixture) { fixture.repo.group.Status = "inactive" }},
		{name: "inactive backup", configure: func(fixture *groupAdminFixture) { fixture.repo.nodes[213].Status = "inactive" }},
		{name: "expired backup", configure: func(fixture *groupAdminFixture) {
			expired := time.Now().Add(-time.Minute)
			fixture.repo.nodes[213].ExpiresAt = &expired
		}},
		{name: "cooled backup", configure: func(fixture *groupAdminFixture) {
			fixture.cache.cooldowns[213] = &service.ProxyCooldown{ProxyID: 213, ExpiresAt: time.Now().Add(time.Minute)}
		}},
		{name: "foreign workspace backup", configure: func(fixture *groupAdminFixture) { fixture.repo.nodes[213].WorkspaceID = 1 }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newGroupAdminFixture(t)
			backupID := int64(213)
			fixture.repo.group.FallbackMode = service.FallbackModeProxy
			fixture.repo.group.FallbackProxyID = &backupID
			fixture.repo.nodes[backupID] = &service.Proxy{ID: backupID, WorkspaceID: 2, Name: "explicit backup", Status: service.StatusActive}
			fixture.repo.members = []service.ProxyGroupMember{{ProxyID: 211, Proxy: fixture.repo.nodes[211]}}
			fixture.repo.accounts = []service.ProxyAccountSummary{{ID: 221, Name: "bound account"}}
			fixture.cache.bindings[221] = backupID
			fixture.cache.ttls[221] = time.Minute
			if testCase.configure != nil {
				testCase.configure(fixture)
			}
			code, data, _ := fixture.request(t, "GET", "proxy-groups/201/bindings", "")
			require.Equal(t, 200, code)
			items := testassert.Type[[]any](t, data["items"])
			require.Len(t, items, 1)
			observation := testassert.Type[map[string]any](t, items[0])
			require.Equal(t, testCase.eligible, observation["eligible"])
			if testCase.eligible {
				require.Equal(t, float64(backupID), observation["proxy_id"])
				require.Equal(t, "explicit backup", observation["proxy_name"])
				require.NotNil(t, observation["expires_at"])
			}
		})
	}
}

func TestProxyGroupNonpositiveParentFallsBackAndMissingOwnerIsDeduplicated(t *testing.T) {
	f := newGroupAdminFixture(t)
	zero, negative, missing := int64(0), int64(-1), int64(229)
	f.repo.accounts = []service.ProxyAccountSummary{
		{ID: 221, ParentAccountID: &zero}, {ID: 222, ParentAccountID: &negative},
		{ID: 223, ParentAccountID: &missing}, {ID: 224, ParentAccountID: &missing},
	}
	f.cache.bindings[221], f.cache.bindings[222] = 211, 211
	f.cache.ttls[221], f.cache.ttls[222] = time.Minute, time.Minute
	code, data, _ := f.request(t, "GET", "proxy-groups/201/bindings", "")
	require.Equal(t, 200, code)
	require.Equal(t, 5, f.cache.reads)
	items := testassert.Type[[]any](t, data["items"])
	require.Equal(t, float64(211), testassert.Type[map[string]any](t, items[0])["proxy_id"])
	require.Equal(t, float64(211), testassert.Type[map[string]any](t, items[1])["proxy_id"])
	require.Nil(t, testassert.Type[map[string]any](t, items[2])["proxy_id"])
	require.Nil(t, testassert.Type[map[string]any](t, items[3])["expires_at"])
	f.cache.reads = 0
	code, data, _ = f.request(t, "DELETE", "proxy-groups/201/bindings", "")
	require.Equal(t, 200, code)
	require.Equal(t, float64(2), data["cleared"])
	require.Equal(t, 3, f.cache.reads)
}
