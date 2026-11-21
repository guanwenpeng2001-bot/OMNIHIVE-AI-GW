package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type availabilityHTTPRepository struct {
	service.AccountRepository
	account *service.Account
	calls   int
	actor   int64
	err     error
}

func (repository *availabilityHTTPRepository) ChangeModelAvailability(_ context.Context, accountID int64, change service.ModelAvailabilityChange, actor int64, revoke bool) error {
	repository.calls++
	repository.actor = actor
	if repository.err != nil {
		return repository.err
	}
	state := repository.account.ModelCatalogState()
	state.RulesRevision++
	repository.account.Extra[service.UpstreamModelCatalogStateExtraKey] = state
	now := time.Now().UTC()
	declaration := service.ModelAvailabilityDeclaration{AccountID: accountID, Model: change.Model, Endpoint: change.Endpoint, IdentityRevision: state.IdentityRevision, RulesRevision: state.RulesRevision, Reason: change.Reason, ConfirmedBy: actor, ConfirmedAt: now, EvidenceKind: service.ModelEvidenceAdministrator, ExpiresAt: change.ExpiresAt}
	if revoke {
		declaration.RevokedAt = &now
	}
	repository.account.Extra[service.ModelAvailabilityExtraKey] = []service.ModelAvailabilityDeclaration{declaration}
	return nil
}

func TestUnlistedModelDeclarationHTTP(test *testing.T) {
	for _, testCase := range []struct {
		name   string
		method string
		patch  map[string]any
		role   string
		err    error
		status int
		calls  int
	}{
		{name: "read empty", method: http.MethodGet, role: service.RoleAdmin, status: 200},
		{name: "declare", method: http.MethodPut, role: service.RoleAdmin, status: 200, calls: 1},
		{name: "revoke", method: http.MethodDelete, role: service.RoleAdmin, status: 200, calls: 1},
		{name: "read needs admin", method: http.MethodGet, role: service.RoleUser, status: 403},
		{name: "write needs admin", method: http.MethodPut, role: service.RoleUser, status: 403},
		{name: "server actor", method: http.MethodPut, role: service.RoleAdmin, patch: map[string]any{"confirmed_by": 999, "confirmed_at": "2000-01-01T00:00:00Z"}, status: 200, calls: 1},
		{name: "second confirmation", method: http.MethodPut, role: service.RoleAdmin, patch: map[string]any{"confirmed": false}, status: 400},
		{name: "reason required", method: http.MethodPut, role: service.RoleAdmin, patch: map[string]any{"reason": " "}, status: 400},
		{name: "missing revision", method: http.MethodPut, role: service.RoleAdmin, patch: map[string]any{"identity_revision": nil}, status: 400},
		{name: "negative revision", method: http.MethodPut, role: service.RoleAdmin, patch: map[string]any{"identity_revision": "-1"}, status: 400},
		{name: "stale identity", method: http.MethodPut, role: service.RoleAdmin, patch: map[string]any{"identity_revision": "1"}, status: 409},
		{name: "stale rules", method: http.MethodPut, role: service.RoleAdmin, patch: map[string]any{"rules_revision": "9007199254740992"}, status: 409},
		{name: "stale catalog", method: http.MethodPut, role: service.RoleAdmin, patch: map[string]any{"snapshot_revision": "1"}, status: 409},
		{name: "unmapped target", method: http.MethodPut, role: service.RoleAdmin, patch: map[string]any{"model": "other"}, status: 400},
		{name: "wildcard endpoint", method: http.MethodPut, role: service.RoleAdmin, patch: map[string]any{"endpoint": "any"}, status: 400},
		{name: "expired", method: http.MethodPut, role: service.RoleAdmin, patch: map[string]any{"expires_at": "2000-01-01T00:00:00Z"}, status: 400},
		{name: "persistence CAS", method: http.MethodPut, role: service.RoleAdmin, err: service.ErrAccountModelCatalogConflict, status: 409, calls: 1},
		{name: "missing revoke", method: http.MethodDelete, role: service.RoleAdmin, err: service.ErrModelAvailabilityNotFound, status: 404, calls: 1},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			admin := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{ID: 42, Credentials: map[string]any{"model_mapping": map[string]string{"alias": "private"}}, Extra: map[string]any{service.UpstreamModelCatalogStateExtraKey: service.AccountModelCatalogState{RulesRevision: 9007199254740993}}}}
			repository := &availabilityHTTPRepository{account: &admin.account, err: testCase.err}
			handler := &AccountHandler{adminService: admin, accountTestService: service.NewAccountTestService(repository, nil, nil, nil, nil, nil, nil, nil)}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
				c.Set(string(middleware.ContextKeyUserRole), testCase.role)
			})
			router.GET("/accounts/:id/model-availability", handler.GetModelAvailability)
			router.PUT("/accounts/:id/model-availability", handler.DeclareModelAvailability)
			router.DELETE("/accounts/:id/model-availability", handler.RevokeModelAvailability)
			body := map[string]any{"identity_revision": "0", "rules_revision": "9007199254740993", "snapshot_revision": "0", "model": "private", "endpoint": "chat_completions", "reason": "verified deployment", "confirmed": true}
			for key, value := range testCase.patch {
				body[key] = value
			}
			encoded, err := json.Marshal(body)
			require.NoError(test, err)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(testCase.method, "/accounts/42/model-availability", strings.NewReader(string(encoded))))
			require.Equal(test, testCase.status, recorder.Code, recorder.Body.String())
			require.Equal(test, testCase.calls, repository.calls)
			if repository.calls > 0 {
				require.EqualValues(test, 7, repository.actor)
			}
			if testCase.status == 200 {
				if testCase.method == http.MethodGet {
					require.Contains(test, recorder.Body.String(), `"declarations":[]`)
				} else {
					require.Contains(test, recorder.Body.String(), `"rules_revision":"9007199254740994"`)
					if testCase.method == http.MethodDelete {
						require.Contains(test, recorder.Body.String(), `"valid":false`)
					} else {
						require.Contains(test, recorder.Body.String(), `"valid":true`)
					}
				}
			}
		})
	}
}
