//go:build integration

package connections_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	control "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/connections"
	identityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/connections"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	identitydb "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

type grantMatrixProbe struct{}

func (grantMatrixProbe) ProbeConnection(context.Context, *service.Account, string) service.ConnectionProbe {
	return service.ConnectionProbe{Network: "passed", Authorization: "passed", Discovery: "passed", Models: []string{"model-a"}}
}

func TestProjectGrantsWorkspaceConnectionMatrix(t *testing.T) {
	ctx := t.Context()
	db, _, _ := testutil.NewCapabilityPostgres(t)
	cfg := &config.Config{}
	cfg.JWT.Secret = uuid.NewString()
	cfg.Totp.EncryptionKey = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	cfg.Totp.EncryptionKeyConfigured = true
	enc, err := repository.NewAESEncryptor(cfg)
	require.NoError(t, err)
	access := identitydb.New(db)
	store := connections.New(db, access, enc, grantMatrixProbe{}, cfg)
	h := control.New(store)
	id := func(query string, args ...any) int64 {
		t.Helper()
		var value int64
		require.NoError(t, db.QueryRowContext(ctx, query, args...).Scan(&value))
		return value
	}
	newMember := func() int64 {
		return id("INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id", uuid.NewString()+"@example.test")
	}
	admin, reader, manager, outsider := newMember(), newMember(), newMember(), newMember()
	workspace := id("INSERT INTO workspaces(name) VALUES('project grant connection matrix') RETURNING id")
	otherWorkspace := id("INSERT INTO workspaces(name) VALUES('project grant other workspace') RETURNING id")
	_, err = db.ExecContext(ctx, "INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin'),($1,$3,'member'),($1,$4,'member')", workspace, admin, reader, manager)
	require.NoError(t, err)
	project := id("SELECT identity_default_project($1)", workspace)
	grantedProject, err := access.CreateProject(ctx, admin, workspace, "Scoped operations", uuid.NewString())
	require.NoError(t, err)
	project = grantedProject.ID
	otherProject := id("SELECT identity_default_project($1)", otherWorkspace)
	grant := func(actor, target int64, actions ...string) {
		_, err = access.SetGrants(ctx, admin, workspace, actor, 1, []int64{target}, actions)
		require.NoError(t, err)
	}
	grant(reader, project, "connections.read", "pools.read", "routing.read")
	grant(manager, project, "connections.read", "connections.edit", "connections.verify", "pools.read", "pools.edit", "pools.assign", "credentials.read_metadata", "credentials.write", "credentials.rotate")

	var actor int64 = admin
	engine := gin.New()
	session := func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: actor})
	}
	v2 := engine.Group("/api/v2", session)
	control.Register(v2, h)
	identityhttp.Register(v2, identityhttp.New(access, identity.Operations{Readers: []identity.OperationReader{store}}, cfg))
	base := fmt.Sprintf("/api/v2/workspaces/%d", workspace)
	var request func(int64, string, string, any) (int, map[string]any)
	request = func(user int64, method, path string, body any) (int, map[string]any) {
		t.Helper()
		actor = user
		defer func() { actor = user }()
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		if method != http.MethodGet && method != http.MethodDelete {
			req.Header.Set("Idempotency-Key", uuid.NewString())
		}
		if method == http.MethodPatch || strings.HasSuffix(path, "/credential") || strings.HasSuffix(path, "/activate") {
			lookup := path
			for _, suffix := range []string{"/credential", "/activate"} {
				lookup = strings.TrimSuffix(lookup, suffix)
			}
			_, current := request(user, http.MethodGet, lookup, nil)
			actor = user
			if data, ok := current["data"].(map[string]any); ok {
				req.Header.Set("If-Match", strconv.Quote(data["version"].(string)))
			} else {
				req.Header.Set("If-Match", `"1"`)
			}
		}
		if strings.HasSuffix(path, "/verify") || strings.HasSuffix(path, "/rotations") {
			connectionPath := strings.TrimSuffix(strings.TrimSuffix(path, "/verify"), "/rotations")
			_, current := request(user, http.MethodGet, connectionPath, nil)
			actor = user
			if data, ok := current["data"].(map[string]any); ok {
				req.Header.Set("If-Match", strconv.Quote(data["version"].(string)))
			} else {
				req.Header.Set("If-Match", `"1"`)
			}
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	expect := func(user int64, want int, method, path string, body any) map[string]any {
		t.Helper()
		status, out := request(user, method, path, body)
		require.Equal(t, want, status, "%s %s: %v", method, path, out)
		return out
	}

	connection := expect(admin, 201, "POST", base+"/connections", map[string]any{"name": "granted", "schema_version": "1", "provider": "openai", "auth_type": "api_key"})["data"].(map[string]any)
	connectionID := connection["id"].(string)
	pool := expect(admin, 201, "POST", base+"/pools", map[string]any{"name": "granted pool", "max_concurrency": 1, "connection_ids": []string{}})["data"].(map[string]any)
	poolID := pool["id"].(string)

	expect(reader, 200, "GET", base+"/connections", nil)
	expect(reader, 200, "GET", base+"/connections/"+connectionID, nil)
	expect(reader, 200, "GET", base+"/pools", nil)
	for _, test := range []struct {
		method, path string
		body         any
	}{
		{"PATCH", base + "/connections/" + connectionID, map[string]any{"name": "denied"}},
		{"PUT", base + "/connections/" + connectionID + "/credential", map[string]any{"operation": "set", "type": "api_key", "secret": "denied"}},
		{"POST", base + "/connections/" + connectionID + "/verify", map[string]any{"checks": []string{"network", "authorization"}, "credential_version": "1"}},
		{"POST", base + "/connections/" + connectionID + "/rotations", map[string]any{"expected_credential_version": "1", "replacement": map[string]any{"type": "api_key", "secret": "denied"}}},
		{"POST", base + "/pools", map[string]any{"name": "denied", "max_concurrency": 1, "connection_ids": []string{}}},
		{"PATCH", base + "/pools/" + poolID, map[string]any{"name": "denied"}},
	} {
		expect(reader, 403, test.method, test.path, test.body)
	}
	require.Equal(t, "granted", expect(reader, 200, "GET", base+"/connections/"+connectionID, nil)["data"].(map[string]any)["name"])
	require.Equal(t, 0, int(id("SELECT count(*) FROM credential_secrets WHERE workspace_id=$1", workspace)))
	require.Equal(t, 0, int(id("SELECT count(*) FROM connection_verifications WHERE workspace_id=$1", workspace)))

	expect(manager, 200, "GET", base+"/connections", nil)
	expect(manager, 200, "GET", base+"/pools", nil)
	expect(manager, 200, "PATCH", base+"/connections/"+connectionID, map[string]any{"name": "managed"})
	expect(manager, 200, "PATCH", base+"/pools/"+poolID, map[string]any{"name": "managed pool"})
	expect(manager, 200, "PUT", base+"/connections/"+connectionID+"/credential", map[string]any{"operation": "set", "type": "api_key", "secret": "initial"})
	verification := expect(manager, 202, "POST", base+"/connections/"+connectionID+"/verify", map[string]any{"checks": []string{"network", "authorization", "discover_models"}, "credential_version": "1"})["data"].(map[string]any)
	expect(manager, 200, "GET", verification["status_url"].(string), nil)
	rotation := expect(manager, 202, "POST", base+"/connections/"+connectionID+"/rotations", map[string]any{"expected_credential_version": "1", "replacement": map[string]any{"type": "api_key", "secret": "rotated"}})["data"].(map[string]any)
	expect(manager, 200, "GET", rotation["status_url"].(string), nil)
	for _, test := range []struct {
		method, path string
		body         any
	}{
		{"POST", base + "/connections", map[string]any{"name": "allowed draft", "schema_version": "1", "provider": "openai", "auth_type": "api_key"}},
		{"POST", base + "/connections/" + connectionID + "/activate", map[string]any{"preview_id": uuid.NewString(), "verification_id": uuid.NewString(), "expected_credential_version": "2"}},
	} {
		want := 403
		if test.body.(map[string]any)["name"] == "allowed draft" {
			want = 201
		}
		expect(manager, want, test.method, test.path, test.body)
	}

	expect(outsider, 404, "GET", base+"/connections", nil)
	expect(reader, 404, "GET", "/api/v2/workspaces/"+strconv.FormatInt(otherWorkspace, 10)+"/connections", nil)
	_, err = access.Evaluate(ctx, reader, workspace, project, "routing.read")
	require.NoError(t, err)
	_, err = access.Evaluate(ctx, reader, workspace, 0, "routing.read")
	require.ErrorIs(t, err, fault.Forbidden)
	_, err = access.Evaluate(ctx, manager, workspace, 0, "connections.activate")
	require.Error(t, err)
	_, err = access.Evaluate(ctx, reader, otherWorkspace, otherProject, "connections.read")
	require.Error(t, err)
	currentProject, err := access.Project(ctx, admin, workspace, project)
	require.NoError(t, err)
	_, err = access.ChangeProject(ctx, admin, workspace, project, currentProject.Version, "", true)
	require.NoError(t, err)
	expect(reader, 403, "GET", base+"/connections", nil)
	_, err = access.SetGrants(ctx, admin, workspace, manager, 2, nil, []string{})
	require.NoError(t, err)
	expect(manager, 403, "GET", base+"/connections", nil)
	expect(manager, 403, "PATCH", base+"/connections/"+connectionID, map[string]any{"name": "revoked"})
	expect(manager, 403, "PUT", base+"/connections/"+connectionID+"/credential", map[string]any{"operation": "clear"})
}
