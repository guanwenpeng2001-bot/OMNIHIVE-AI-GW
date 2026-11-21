package middleware

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestProtocolDecisionFixtures(t *testing.T) {
	data, err := os.ReadFile("../../../../contracts/protocol/auth/decisions.json")
	require.NoError(t, err)
	var cases []struct {
		Name, Method, Path, Bearer string
		GoogleHeader               string `json:"google_header"`
		Google, Expired            bool
		EmptyBalance               bool `json:"empty_balance"`
		Status                     int
	}
	require.NoError(t, json.Unmarshal(data, &cases))
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			key := &service.APIKey{ID: 5, UserID: 3, Status: "active", User: &service.User{ID: 3, Status: "active", Balance: 1}}
			if tc.Expired {
				v := time.Now().Add(-time.Hour)
				key.ExpiresAt = &v
			}
			if tc.EmptyBalance {
				key.User.Balance = 0
			}
			repo := fakeAPIKeyRepo{getByKey: func(_ context.Context, s string) (*service.APIKey, error) {
				if s != "valid" {
					return nil, service.ErrAPIKeyNotFound
				}
				return key, nil
			}}
			cfg := &config.Config{RunMode: config.RunModeStandard}
			keys := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
			r := gin.New()
			if tc.Google {
				r.Use(APIKeyAuthGoogle(keys, cfg))
			} else {
				r.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(keys, nil, cfg)))
			}
			path, _, _ := strings.Cut(tc.Path, "?")
			r.Handle(tc.Method, path, func(c *gin.Context) { c.Status(200) })
			req := httptest.NewRequest(tc.Method, tc.Path, nil)
			if tc.Bearer != "" {
				req.Header.Set("Authorization", "Bearer "+tc.Bearer)
			}
			req.Header.Set("x-goog-api-key", tc.GoogleHeader)
			out := httptest.NewRecorder()
			r.ServeHTTP(out, req)
			require.Equal(t, tc.Status, out.Code)
			if tc.Google && tc.Status >= 400 {
				var v struct {
					Error struct {
						Code   int
						Status string
					}
				}
				require.NoError(t, json.Unmarshal(out.Body.Bytes(), &v))
				require.Equal(t, tc.Status, v.Error.Code)
				require.NotEmpty(t, v.Error.Status)
			}
		})
	}
}
func TestWorkspaceSwitchDropsPreviousProject(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request = c.Request.WithContext(scope.With(c.Request.Context(), scope.Authorized{WorkspaceID: 3, ProjectID: 7, KeyID: 5, ActorID: 9, FundingUserID: 8, Version: 4}))
	installWorkspace(c, 2, 11)
	got, ok := scope.From(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, scope.Authorized{WorkspaceID: 2, ActorID: 11}, got)
}
