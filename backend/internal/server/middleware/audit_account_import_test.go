package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ctxkey"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type auditImportBody struct {
	*bytes.Reader
	reads int
}

func (body *auditImportBody) Read(buffer []byte) (int, error) {
	body.reads++
	return body.Reader.Read(buffer)
}
func (*auditImportBody) Close() error { return nil }

// Exercise real Gin FullPath resolution and the asynchronous audit writer.
func captureImportRouteAudit(t *testing.T, method, route, path string, raw []byte, status int, unread bool) *service.AuditLog {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repository := &auditCaptureRepository{}
	audit := service.NewAuditLogService(repository, nil)
	audit.Start()
	t.Cleanup(audit.Stop)
	body := &auditImportBody{Reader: bytes.NewReader(raw)}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyUser), AuthSubject{UserID: 77})
		c.Set(string(ContextKeyUserRole), "admin")
		c.Request = c.Request.WithContext(domain.WithWorkspaceScope(c.Request.Context(), 3))
		c.Next()
	})
	router.Use(gin.HandlerFunc(NewAuditLogMiddleware(audit)))
	handled := false
	router.Handle(method, route, func(c *gin.Context) {
		handled = true
		if unread {
			require.Same(t, body, c.Request.Body)
			require.Zero(t, body.reads, "omitted import bodies must not be consumed, cloned or parsed by auditing")
		}
		received, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.Equal(t, raw, received, "handler receives the complete body, including above the audit capture limit")
		c.Status(status)
	})
	request := httptest.NewRequest(method, path, body)
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(context.WithValue(request.Context(), ctxkey.RequestID, "route-audit-fixture"))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.True(t, handled)
	require.Equal(t, status, recorder.Code)
	audit.Stop() // drains without sleeps or polling
	repository.mu.Lock()
	defer repository.mu.Unlock()
	require.Len(t, repository.logs, 1, "body omission must not skip the audit event")
	entry := repository.logs[0]
	require.Equal(t, method, entry.Method)
	require.Equal(t, route, entry.Path)
	require.Equal(t, status, entry.StatusCode)
	require.Equal(t, int64(77), *entry.ActorUserID)
	require.Equal(t, "admin", entry.ActorRole)
	require.EqualValues(t, 3, entry.Extra["workspace_id"])
	require.Equal(t, "route-audit-fixture", entry.RequestID)
	return entry
}

func TestAccountImportAuditOmitsAllBodiesWithoutReadingOrSubmittedDiff(t *testing.T) {
	for _, mode := range []string{"staged", "legacy_false", "legacy_omitted", "uppercase_flag", "invalid_flag", "malformed", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			// Include secrets under unknown field names and in a field name itself:
			// SubmittedConfigurationDiff normally preserves top-level field names.
			document := map[string]any{
				"stage_inactive":             true,
				"import-opaque-field-canary": "import-opaque-value-canary",
				"notes":                      "import-free-text-canary",
				"data": map[string]any{"accounts": []any{map[string]any{
					"credentials": map[string]any{"api_key": "import-api-canary", "refresh_token": "import-refresh-canary"},
				}}, "proxies": []any{map[string]any{"proxy_key": "import-proxy-canary"}}},
			}
			status := http.StatusOK
			switch mode {
			case "legacy_false":
				document["stage_inactive"] = false
			case "legacy_omitted":
				delete(document, "stage_inactive")
			case "uppercase_flag":
				delete(document, "stage_inactive")
				document["STAGE_INACTIVE"] = true
			case "invalid_flag":
				document["stage_inactive"] = "invalid"
				status = http.StatusBadRequest
			case "oversized":
				document["notes"] = strings.Repeat("x", service.AuditRequestBodyCaptureLimit+1) + "import-tail-canary"
			}
			raw, err := json.Marshal(document)
			require.NoError(t, err)
			if mode == "malformed" {
				raw = []byte(`{"stage_inactive":true,"api_key":"import-malformed-canary"`)
				status = http.StatusBadRequest
			}
			entry := captureImportRouteAudit(t, http.MethodPost, "/api/v1/admin/accounts/data", "/api/v1/admin/accounts/data?attempt=1", raw, status, true)
			require.Equal(t, "admin.accounts.import", entry.Action)
			require.Equal(t, "<credential-bearing body omitted>", entry.RequestBody)
			require.NotContains(t, entry.Extra, "submitted_diff", "skip diff generation as well as body logging")
			serialized, err := json.Marshal(entry)
			require.NoError(t, err)
			for _, canary := range []string{"import-opaque-field-canary", "import-opaque-value-canary", "import-free-text-canary", "import-api-canary", "import-refresh-canary", "import-proxy-canary", "import-tail-canary", "import-malformed-canary"} {
				require.NotContains(t, string(serialized), canary)
			}
		})
	}
}

func TestAccountImportAuditFlagCannotHideOtherRoutes(t *testing.T) {
	for _, target := range []struct{ method, route, path string }{
		{http.MethodPost, "/api/v1/admin/accounts", "/api/v1/admin/accounts"},
		{http.MethodPut, "/api/v1/admin/accounts/data", "/api/v1/admin/accounts/data"},
		{http.MethodPost, "/api/v1/admin/accounts/:id", "/api/v1/admin/accounts/data"}, // URL alone is not the import route
		{http.MethodPost, "/api/v1/admin/accounts/data/preview", "/api/v1/admin/accounts/data/preview"},
		{http.MethodPost, "/api/v1/admin/proxies/data", "/api/v1/admin/proxies/data"},
		{http.MethodPut, "/api/v1/admin/settings", "/api/v1/admin/settings"},
	} {
		for _, flag := range []string{"stage_inactive", "STAGE_INACTIVE"} {
			t.Run(target.method+target.route+"/"+flag, func(t *testing.T) {
				credentials := make(map[string]any)
				for index, key := range service.SensitiveCredentialKeys {
					credentials[key] = fmt.Sprintf("nested-credential-canary-%d", index)
				}
				raw, err := json.Marshal(map[string]any{
					flag: true, "name": "auditable-change", "ordinary_field": "keep-this-evidence", "credentials": credentials,
					"proxy_key": "route-proxy-canary", "custom_key": "route-custom-canary",
				})
				require.NoError(t, err)
				entry := captureImportRouteAudit(t, target.method, target.route, target.path, raw, http.StatusOK, false)
				var visible map[string]any
				require.NoError(t, json.Unmarshal([]byte(entry.RequestBody), &visible), "stage_inactive must not suppress ordinary route audits")
				require.Equal(t, true, visible[flag])
				require.Equal(t, "auditable-change", visible["name"])
				require.Equal(t, "keep-this-evidence", visible["ordinary_field"])
				diff, ok := entry.Extra["submitted_diff"].([]service.ConfigurationFieldDiff)
				require.True(t, ok)
				fields := make([]string, 0, len(diff))
				for _, change := range diff {
					fields = append(fields, change.Field)
					require.Equal(t, "submitted", change.Operation)
					require.Equal(t, "[redacted]", change.After)
				}
				require.Contains(t, fields, flag)
				require.Contains(t, fields, "name")
				require.Contains(t, fields, "credentials")
				serialized, err := json.Marshal(entry)
				require.NoError(t, err)
				for _, canary := range []string{"nested-credential-canary-", "route-proxy-canary", "route-custom-canary"} {
					require.NotContains(t, string(serialized), canary)
				}
			})
		}
	}
}

func TestWorkspaceAccountImportAuditOmitsBodyAndKeepsEvent(t *testing.T) {
	route := "/api/v1/workspaces/:workspace_id/accounts/data"
	path := "/api/v1/workspaces/3/accounts/data"
	for _, raw := range [][]byte{
		[]byte(`{"stage_inactive":true,"migration_blob":"workspace-opaque-canary"}`),
		[]byte(`{"stage_inactive":true,"workspace-field-canary":"secret"}`),
		[]byte(`{"stage_inactive":true,"broken":"workspace-malformed-canary"`),
	} {
		entry := captureImportRouteAudit(t, http.MethodPost, route, path, raw, http.StatusBadRequest, true)
		require.Equal(t, "workspace.accounts.import", entry.Action)
		require.Equal(t, "<credential-bearing body omitted>", entry.RequestBody)
		require.NotContains(t, entry.Extra, "submitted_diff")
		encoded, err := json.Marshal(entry)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "workspace-opaque-canary")
		require.NotContains(t, string(encoded), "workspace-field-canary")
		require.NotContains(t, string(encoded), "workspace-malformed-canary")
	}
}

func TestWorkspaceAccountExportIsAudited(t *testing.T) {
	entry := captureImportRouteAudit(t, http.MethodGet,
		"/api/v1/workspaces/:workspace_id/accounts/data",
		"/api/v1/workspaces/3/accounts/data", []byte{}, http.StatusOK, false)
	require.Equal(t, "workspace.accounts.export", entry.Action)
}
