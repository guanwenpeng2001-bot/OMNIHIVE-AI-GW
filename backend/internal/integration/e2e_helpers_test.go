//go:build e2e

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func requireE2EServer(testingT *testing.T) {
	testingT.Helper()
	if baseURL == "" {
		testingT.Skip("BASE_URL 未配置，外部 E2E 未执行；本地失败判定 fixture 仍运行")
	}
}

// Exercise the real assertions in child processes, proving errors fail instead of skip.
func TestE2EFailuresAreNotSkipped(testingT *testing.T) {
	cases := []struct {
		name, testName string
		status         int
	}{
		{"tools_rate_limited", "TestE2EFailureFixture/tools", http.StatusTooManyRequests},
		{"tools_unavailable", "TestE2EFailureFixture/tools", http.StatusServiceUnavailable},
		{"thinking_unavailable", "TestE2EFailureFixture/thinking", http.StatusServiceUnavailable},
		{"no_signature_rate_limited", "TestE2EFailureFixture/no_signature", http.StatusTooManyRequests},
		{"registration_unavailable", "TestUserRegistrationAndLogin", http.StatusServiceUnavailable},
		{"login_rejected", "TestAPIKeyLifecycle", http.StatusUnauthorized},
		{"login_missing_token", "TestAPIKeyLifecycle", http.StatusOK},
	}
	for _, testCase := range cases {
		testingT.Run(testCase.name, func(testingT *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(testCase.status)
				_, _ = writer.Write([]byte(`{"error":"fixture failure"}`))
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+strings.ReplaceAll(testCase.testName, "/", "$/^")+"$", "-test.v")
			// Strip inherited targets and credentials so fixtures never hit real providers.
			for _, entry := range os.Environ() {
				name, _, _ := strings.Cut(entry, "=")
				switch strings.ToUpper(name) {
				case "BASE_URL", "ENDPOINT_PREFIX", "CLAUDE_API_KEY", "GEMINI_API_KEY", "ADMIN_EMAIL", "ADMIN_PASSWORD", "E2E_FAILURE_CHILD":
					continue
				}
				command.Env = append(command.Env, entry)
			}
			command.Env = append(command.Env, "BASE_URL="+server.URL, "ENDPOINT_PREFIX=", "CLAUDE_API_KEY=fixture-key", "ADMIN_EMAIL=fixture@example.test", "ADMIN_PASSWORD=fixture-password", "E2E_FAILURE_CHILD=1")
			output, err := command.CombinedOutput()
			if ctx.Err() != nil || err == nil || !strings.Contains(string(output), "--- FAIL:") || strings.Contains(string(output), "--- SKIP:") {
				testingT.Fatalf("expected a failed, non-skipped test; error=%v context=%v\n%s", err, ctx.Err(), output)
			}
		})
	}
}

func TestE2EFailureFixture(testingT *testing.T) {
	if os.Getenv("E2E_FAILURE_CHILD") != "1" {
		return
	}
	testingT.Run("tools", func(testingT *testing.T) { testClaudeMessageWithTools(testingT, "fixture-key", "fixture-model") })
	testingT.Run("thinking", func(testingT *testing.T) { testClaudeThinkingWithToolHistory(testingT, "fixture-key", "fixture-model") })
	testingT.Run("no_signature", func(testingT *testing.T) { testClaudeWithNoSignature(testingT, "fixture-key", "fixture-model") })
}

// Local fixtures validate the E2E client contract without a running application,
// credentials, a shared database, or paid provider requests.
func TestE2ELocalUserFlow(testingT *testing.T) {
	previousURL := baseURL
	testingT.Cleanup(func() { baseURL = previousURL })
	testingT.Setenv("ADMIN_EMAIL", "fixture@example.test")
	testingT.Setenv("ADMIN_PASSWORD", "fixture-password")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.Method + " " + request.URL.Path {
		case "POST /api/v1/auth/register", "POST /api/v1/auth/login":
			_, _ = writer.Write([]byte(`{"code":0,"data":{"access_token":"fixture-access-token"}}`))
		case "GET /api/v1/user/profile", "GET /api/v1/usage/dashboard/stats":
			if request.Header.Get("Authorization") != "Bearer fixture-access-token" {
				writer.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = writer.Write([]byte(`{"code":0,"data":{}}`))
		case "POST /api/v1/keys":
			if request.Header.Get("Authorization") != "Bearer fixture-access-token" {
				writer.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = writer.Write([]byte(`{"code":0,"data":{"key":"fixture-api-key"}}`))
		case "GET /v1/models":
			if request.Header.Get("Authorization") != "Bearer fixture-api-key" {
				writer.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = writer.Write([]byte(`{"object":"list","data":[]}`))
		default:
			testingT.Errorf("unexpected E2E request: %s %s", request.Method, request.URL.Path)
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	baseURL = server.URL
	testingT.Run("registration_and_login", TestUserRegistrationAndLogin)
	testingT.Run("api_key_lifecycle", TestAPIKeyLifecycle)
}
