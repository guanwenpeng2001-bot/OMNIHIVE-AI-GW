//go:build integration

package observability_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/observability"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/ops/export"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceTelemetryRoutes(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("requires an isolated job_test PostgreSQL database")
	}
	ctx := t.Context()
	db, err := testresources.OpenPostgres(ctx, dsn, "job_test")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	id := func(query string, params ...any) int64 {
		t.Helper()
		var id int64
		require.NoError(t, db.QueryRowContext(ctx, query, params...).Scan(&id))
		return id
	}
	createWorkspace := func() int64 {
		t.Helper()
		id := id("INSERT INTO workspaces(name) VALUES('telemetry API fixture') RETURNING id")
		t.Cleanup(func() {
			_, err := db.Exec("DELETE FROM workspaces WHERE id=$1", id)
			require.NoError(t, err)
		})
		return id
	}
	admin := id("INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id", uuid.NewString()+"@example.test")
	member := id("INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id", uuid.NewString()+"@example.test")
	workspace, outside := createWorkspace(), createWorkspace()
	_, err = db.ExecContext(ctx, "INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$3,'admin'),($2,$3,'admin'),($1,$4,'member')",
		workspace, outside, admin, member)
	require.NoError(t, err)

	engine := gin.New()
	store := export.New(db, postgres.New(db), nil)
	session := func(c *gin.Context) {
		id, _ := strconv.ParseInt(c.GetHeader("Fixture-Actor"), 10, 64)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: id})
		c.Next()
	}
	observability.Register(engine.Group("/api/v2"), observability.New(store), session)
	call := func(actor int64, method, path, key, version, body string, code int) map[string]any {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Fixture-Actor", strconv.FormatInt(actor, 10))
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		if version != "" {
			req.Header.Set("If-Match", version)
		}
		out := httptest.NewRecorder()
		engine.ServeHTTP(out, req)
		require.Equal(t, code, out.Code, out.Body.String())
		require.Equal(t, "private, no-store", out.Header().Get("Cache-Control"))
		require.NotContains(t, out.Body.String(), "collector.example.org")
		require.NotContains(t, out.Body.String(), "credential-canary")
		var envelope map[string]any
		require.NoError(t, json.Unmarshal(out.Body.Bytes(), &envelope))
		data, _ := envelope["data"].(map[string]any)
		if code < 400 {
			require.Equal(t, float64(0), envelope["code"])
			require.Equal(t, "success", envelope["message"])
			return data
		}
		require.Nil(t, data)
		if code == http.StatusPreconditionFailed {
			v := envelope["metadata"].(map[string]any)["current_version"].(string)
			require.Equal(t, strconv.Quote(v), out.Header().Get("ETag"))
		}
		return envelope
	}
	read := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "api", "observability", name+".json"))
		require.NoError(t, err)
		return string(b)
	}
	expect := func(expected string, actual map[string]any) {
		t.Helper()
		raw, err := json.Marshal(actual)
		require.NoError(t, err)
		require.JSONEq(t, expected, string(raw))
	}

	base := fmt.Sprintf("/api/v2/workspaces/%d/observability", workspace)
	foreign := fmt.Sprintf("/api/v2/workspaces/%d/observability", outside)
	destinationInput := read("destination-input")
	call(0, "POST", base+"/destinations", "same", "", destinationInput, 401)
	call(member, "POST", base+"/destinations", "same", "", destinationInput, 403)
	call(admin, "POST", base+"/destinations", "same", "", `{"endpoint":"https://user:credential-canary@collector.example.org"}`, 422)
	badBody := call(admin, "POST", base+"/destinations", "same", "", `{"credential-canary":null}`, 422)
	require.Equal(t, "INVALID_INPUT", badBody["reason"])
	dest := call(admin, "POST", base+"/destinations", "same", "", destinationInput, 201)
	destID := dest["id"].(string)
	bindDestination := func(fixture string) string {
		return strings.ReplaceAll(read(fixture), "00000000-0000-4000-8000-000000000029", destID)
	}
	expect(bindDestination("destination"), dest)
	expect(bindDestination("destination"), call(admin, "POST", base+"/destinations", "same", "", destinationInput, 201))

	createInput := bindDestination("create-input")
	call(admin, "POST", foreign+"/exports", "same", "", createInput, 404)
	call(admin, "POST", base+"/exports", "", "", createInput, 422)
	call(admin, "POST", base+"/exports", "same", "", strings.Replace(createInput, "timings", "prompts", 1), 422)
	call(admin, "POST", base+"/exports", "same", "", strings.Replace(createInput, `"0.05"`, "0.05", 1), 422)
	created := call(admin, "POST", base+"/exports", "same", "", createInput, 201)
	exportID := created["id"].(string)
	bindExport := func(fixture string) string {
		return strings.ReplaceAll(bindDestination(fixture), "00000000-0000-4000-8000-000000000318", exportID)
	}
	expect(bindExport("created"), created)
	expect(bindExport("created"), call(admin, "POST", base+"/exports", "same", "", strings.Replace(createInput, "0.05", "0.0500", 1), 201))
	call(admin, "POST", base+"/exports", "same", "", strings.Replace(createInput, "0.05", "0.06", 1), 409)

	path := "/exports/" + exportID
	expect(bindExport("detail"), call(admin, "GET", base+path, "", "", "", 200))
	call(admin, "GET", foreign+path, "", "", "", 404)
	call(member, "GET", base+path, "", "", "", 403)
	call(member, "PATCH", base+path, "", `"1"`, read("disable-input"), 403)
	call(admin, "PATCH", foreign+path, "", `"1"`, read("disable-input"), 404)
	call(admin, "PATCH", base+path, "", "", read("disable-input"), 422)
	stale := call(admin, "PATCH", base+path, "", `"2"`, read("disable-input"), 412)
	require.Equal(t, "1", stale["metadata"].(map[string]any)["current_version"])
	out := call(admin, "PATCH", base+path, "", `"1"`, `{"sample_rate":"0.5","retention_days":3}`, 200)
	require.Equal(t, "2", out["version"])
	out = call(admin, "GET", base+path, "", "", "", 200)
	require.Equal(t, "0.5", out["sample_rate"])
	require.EqualValues(t, 3, out["retention_days"])
	out = call(admin, "PATCH", base+path, "", `"2"`, read("disable-input"), 200)
	require.Equal(t, "disabled", out["state"])
	require.Equal(t, "3", out["version"])
	out = call(admin, "PATCH", base+path, "", `"3"`, `{"enabled":true}`, 200)
	require.Equal(t, "validating", out["state"])

	destPath := "/destinations/" + destID
	call(admin, "DELETE", foreign+destPath, "", `"1"`, "", 404)
	call(admin, "DELETE", base+destPath, "", `"2"`, "", 412)
	out = call(admin, "DELETE", base+destPath, "", `"1"`, "", 200)
	require.Equal(t, "revoked", out["state"])
	out = call(admin, "GET", base+path, "", "", "", 200)
	require.Equal(t, "disabled", out["state"])
	require.Equal(t, "DESTINATION_REVOKED", out["last_error_reason"])
	require.Equal(t, "0", out["queue_depth"])
	call(admin, "PATCH", base+path, "", `"5"`, `{"enabled":true}`, 404)
	expect(bindExport("created"), call(admin, "POST", base+"/exports", "same", "", createInput, 201))
}
