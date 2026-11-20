// Package runtime owns process roles, request admission and worker lifetime.
package runtime

import (
	"fmt"
	"net/http"
	"strings"
)

type Mode string

const (
	All     Mode = "all"
	Gateway Mode = "gateway"
	Control Mode = "control"
	Worker  Mode = "worker"
)

func ParseMode(value string) (Mode, error) {
	switch m := Mode(strings.TrimSpace(value)); m {
	case "":
		return All, nil
	case All, Gateway, Control, Worker:
		return m, nil
	default:
		return "", fmt.Errorf("invalid runtime mode %q (expected all, control, gateway or worker)", value)
	}
}
func (m Mode) Workers() bool { return m == All || m == Worker }
func (m Mode) Serving() bool { return m == All || m == Control || m == Gateway }

// Role classifies the Gin path (UseRawPath is enabled in the production router).
// Route project-funded execution directly to gateway listeners so the original
// session, request cancellation and streaming connection survive.
func Role(method, path string) Mode {
	path = strings.TrimSuffix(path, "/")
	switch path {
	case "/health", "/ready":
		return All
	case "/api/v1/public/rankings", "/api/v1/model-plaza/rankings", "/api/v1/user/audit-logs":
		return controlMethod(method == http.MethodGet)
	case "/api/v1/user/routing-policies", "/api/v1/user/onboarding":
		return controlMethod(method == http.MethodGet || method == http.MethodPut)
	case "/api/v1/user/mcp/authorize":
		return controlMethod(method == http.MethodPost)
	case "/api/v1/user/mcp/grants":
		return controlMethod(method == http.MethodGet || method == http.MethodPost)
	}
	if id, ok := strings.CutPrefix(path, "/api/v1/user/mcp/grants/"); ok {
		return controlMethod(id != "" && !strings.Contains(id, "/") && method == http.MethodDelete)
	}
	if strings.HasPrefix(path, "/api/v1/user/mcp/") {
		return ""
	}
	if strings.HasPrefix(path, "/api/v2/workspaces/") {
		fields := strings.Split(strings.TrimPrefix(path, "/api/v2/workspaces/"), "/")
		if len(fields) >= 4 && fields[1] == "projects" {
			action := fields[3:]
			if action[0] == "playground" || (len(action) == 3 && action[0] == "requests" && action[2] == "replay") {
				return Gateway
			}
			if method == http.MethodPost && action[0] == "tasks" && (len(action) == 1 || len(action) == 3 && action[2] == "retry") {
				return Gateway
			}
		}
	}
	if after, ok := strings.CutPrefix(path, "/api/v1/"); ok {
		part, _, _ := strings.Cut(after, "/")
		switch part {
		case "services", "tasks", "apps", "files", "batches", "realtime", "indices":
			return Gateway
		}
	}
	part, _, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	switch part {
	case "v1", "v1beta", "backend-api", "antigravity", "api-ws", "compatible-mode", "sub2api", "files", "batches", "moderations", "messages", "tools", "videos", "responses", "alpha", "models", "usage", "images", "live", "chat", "embeddings", "rerank", "audio", "tts", "stt", "custom-voices", "realtime", "web_search", "x_search":
		return Gateway
	}
	if path == "/api/event_logging/batch" {
		return Gateway
	}
	return Control
}

func controlMethod(allowed bool) Mode {
	if allowed {
		return Control
	}
	return ""
}
