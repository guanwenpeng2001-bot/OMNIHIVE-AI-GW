package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
)

func TestAntigravityOAuthValidateRefreshTokenPreservesCredentials(test *testing.T) {
	// DefaultTransport is process-global, so this test and its subtests must not run in parallel.
	test.Setenv(antigravity.AntigravityOAuthClientSecretEnv, "synthetic-client-secret")
	for _, scenario := range []struct {
		name                 string
		responseRefreshToken string
		wantRefreshToken     string
	}{
		{name: "missing response refresh token", wantRefreshToken: "synthetic-original-refresh"},
		{name: "rotated response refresh token", responseRefreshToken: "synthetic-rotated-refresh", wantRefreshToken: "synthetic-rotated-refresh"},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			originalTransport := http.DefaultTransport
			test.Cleanup(func() { http.DefaultTransport = originalTransport })
			requests := make(map[string]int)
			http.DefaultTransport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requests[request.URL.Path]++
				var body string
				switch {
				case request.URL.String() == antigravity.TokenURL:
					if err := request.ParseForm(); err != nil {
						test.Fatal(err)
					}
					if request.Method != http.MethodPost || request.Form.Get("grant_type") != "refresh_token" || request.Form.Get("refresh_token") != "synthetic-original-refresh" {
						test.Fatal("refresh request did not preserve the input or grant type")
					}
					response := map[string]any{"access_token": "synthetic-access", "expires_in": 3600, "token_type": "Bearer"}
					if scenario.responseRefreshToken != "" {
						response["refresh_token"] = scenario.responseRefreshToken
					}
					encoded, err := json.Marshal(response)
					if err != nil {
						test.Fatal(err)
					}
					body = string(encoded)
				case request.URL.String() == antigravity.UserInfoURL:
					body = `{"email":"synthetic@example.invalid"}`
				case request.URL.Path == "/v1internal:loadCodeAssist":
					body = `{"cloudaicompanionProject":"synthetic-project","paidTier":{"id":"g1-pro-tier"}}`
				case request.URL.Path == "/v1internal:setUserSettings", request.URL.Path == "/v1internal:fetchUserInfo":
					body = `{"userSettings":{}}`
				default:
					test.Fatal("unexpected HTTP request; real network access is forbidden")
				}
				if request.URL.String() != antigravity.TokenURL && request.Header.Get("Authorization") != "Bearer synthetic-access" {
					test.Fatal("validation did not use the refreshed access token")
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
			})
			service := NewAntigravityOAuthService(nil)
			test.Cleanup(service.Stop)
			started := time.Now().Unix()
			info, err := service.ValidateRefreshToken(context.Background(), "synthetic-original-refresh", nil)
			finished := time.Now().Unix()
			if err != nil {
				test.Fatal(err)
			}
			if info.AccessToken != "synthetic-access" || info.ExpiresIn != 3600 || info.TokenType != "Bearer" || info.ExpiresAt < started+3300 || info.ExpiresAt > finished+3300 {
				test.Error("refreshed access token, token type, or expiry changed")
			}
			if info.RefreshToken != scenario.wantRefreshToken {
				test.Error("refresh token was lost or rotation was ignored")
			}
			if info.ProjectIDMissing || info.PrivacyMode != AntigravityPrivacySet || len(requests) != 5 {
				test.Error("import validation did not complete all enrichment and privacy requests")
			}
			// Exercise the OAuth JSON boundary before constructing persisted credentials.
			encoded, err := json.Marshal(info)
			if err != nil {
				test.Fatal(err)
			}
			var decoded AntigravityTokenInfo
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				test.Fatal(err)
			}
			credentials := service.BuildAccountCredentials(&decoded)
			encoded, err = json.Marshal(credentials)
			if err != nil {
				test.Fatal(err)
			}
			var persisted map[string]any
			if err := json.Unmarshal(encoded, &persisted); err != nil {
				test.Fatal(err)
			}
			want := map[string]any{
				"access_token": "synthetic-access", "refresh_token": scenario.wantRefreshToken,
				"expires_at": strconv.FormatInt(info.ExpiresAt, 10), "token_type": "Bearer",
				"email": "synthetic@example.invalid", "project_id": "synthetic-project", "plan_type": "Pro",
			}
			if !reflect.DeepEqual(persisted, want) {
				test.Error("persisted credential fields did not match the complete refreshed credentials")
			}
		})
	}
}

func TestResolveDefaultTierID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		loadRaw map[string]any
		want    string
	}{
		{
			name:    "nil loadRaw",
			loadRaw: nil,
			want:    "",
		},
		{
			name: "missing allowedTiers",
			loadRaw: map[string]any{
				"paidTier": map[string]any{"id": "g1-pro-tier"},
			},
			want: "",
		},
		{
			name:    "empty allowedTiers",
			loadRaw: map[string]any{"allowedTiers": []any{}},
			want:    "",
		},
		{
			name: "tier missing id field",
			loadRaw: map[string]any{
				"allowedTiers": []any{
					map[string]any{"isDefault": true},
				},
			},
			want: "",
		},
		{
			name: "allowedTiers but no default",
			loadRaw: map[string]any{
				"allowedTiers": []any{
					map[string]any{"id": "free-tier", "isDefault": false},
					map[string]any{"id": "standard-tier", "isDefault": false},
				},
			},
			want: "",
		},
		{
			name: "default tier found",
			loadRaw: map[string]any{
				"allowedTiers": []any{
					map[string]any{"id": "free-tier", "isDefault": true},
					map[string]any{"id": "standard-tier", "isDefault": false},
				},
			},
			want: "free-tier",
		},
		{
			name: "default tier id with spaces",
			loadRaw: map[string]any{
				"allowedTiers": []any{
					map[string]any{"id": "  standard-tier  ", "isDefault": true},
				},
			},
			want: "standard-tier",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := resolveDefaultTierID(tc.loadRaw)
			if got != tc.want {
				t.Fatalf("resolveDefaultTierID() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The OAuth handler serializes this DTO before the browser creates or reauthorizes an account.
func TestAntigravityTokenInfoPreservesPlanTypeJSON(t *testing.T) {
	for _, plan := range []string{"pro", "ultra", "free", ""} {
		t.Run(plan, func(t *testing.T) {
			data, err := json.Marshal(AntigravityTokenInfo{PlanType: plan})
			if err != nil {
				t.Fatal(err)
			}
			var response map[string]any
			if err := json.Unmarshal(data, &response); err != nil {
				t.Fatal(err)
			}
			if plan == "" {
				if _, exists := response["plan_type"]; exists {
					t.Fatal("unknown plan must remain omitted")
				}
			} else if response["plan_type"] != plan {
				t.Fatalf("OAuth response plan_type = %v, want %q", response["plan_type"], plan)
			}
		})
	}
}
