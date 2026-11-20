package handler

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	executioncompat "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/execution"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func InstallExecutionJournal(db *sql.DB, scopes identity.GatewayScopes) gin.HandlerFunc {
	store := journal.New(db)
	validator := executioncompat.Validator{DB: db, Scopes: scopes}
	return func(c *gin.Context) {
		switch c.FullPath() {
		case "/v1/responses", "/responses", "/backend-api/codex/responses", "/v1/images/generations/async", "/images/generations/async",
			"/v1/chat/completions", "/chat/completions", "/v1/messages", "/messages", "/antigravity/v1/messages":
			if journal.From(c.Request.Context()) == nil {
				ctx := journal.WithStore(c.Request.Context(), store)
				c.Request = c.Request.WithContext(validator.Install(ctx))
			}
		}
		c.Next()
	}
}

func beginResponsesExecution(c *gin.Context, capturer gatewayPlanCapturer, key *service.APIKey, model string, body []byte, switches int, writeError func(*gin.Context, int, string, string)) (func(), bool) {
	return beginJournaledExecution(c, capturer, key, model, "responses", body, switches, writeError)
}

func beginJournaledExecution(c *gin.Context, capturer gatewayPlanCapturer, key *service.APIKey, model, protocol string, body []byte, switches int, writeError func(*gin.Context, int, string, string), geminiSwitches ...int) (func(), bool) {
	var reg *journal.Registration
	if journal.From(c.Request.Context()) != nil {
		var err error
		reg, err = journal.Register(c.GetHeader("Idempotency-Key"), body)
		if err != nil {
			writeError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
			return func() {}, false
		}
	}
	finish := beginRegisteredGatewayRequestPlan(c, capturer, key, model, protocol, switches, reg, geminiSwitches...)
	p := service.GatewayRequestPlanFromContext(c.Request.Context())
	e, err := p.AdmittedExecution()
	if e.RequestID != "" {
		c.Header("X-Execution-ID", e.RequestID)
	}
	if err == nil {
		return finish, true
	}
	switch {
	case errors.Is(err, journal.ErrReplay):
		c.Header("Cache-Control", "no-store")
		c.Header("X-Idempotency-Replayed", "true")
		c.Status(http.StatusNoContent)
	case errors.Is(err, journal.ErrConflict):
		writeError(c, http.StatusConflict, "invalid_request_error", err.Error())
	case errors.Is(err, journal.ErrScope):
		writeError(c, http.StatusForbidden, "permission_error", "Access denied")
	default:
		writeError(c, http.StatusServiceUnavailable, "server_error", "Execution admission unavailable")
	}
	return finish, false
}

type gatewayPlanCapturer interface {
	CaptureRequestPlan(context.Context, service.GatewayRequestSpec) (context.Context, *service.GatewayRequestPlan)
}

// Each adapter calls this once after parsing the inbound model, before waiting
// or forwarding. It intentionally does not move the platform failover loops.
func beginGatewayRequestPlan(c *gin.Context, capturer gatewayPlanCapturer, key *service.APIKey, model, protocol string, switches int, geminiSwitches ...int) func() {
	return beginRegisteredGatewayRequestPlan(c, capturer, key, model, protocol, switches, nil, geminiSwitches...)
}

func beginRegisteredGatewayRequestPlan(c *gin.Context, capturer gatewayPlanCapturer, key *service.APIKey, model, protocol string, switches int, registration *journal.Registration, geminiSwitches ...int) func() {
	if protocol == "anthropic" && isGeminiMessagesBridge(c) {
		protocol = "gemini"
	}
	finishBudget := installAttemptBudget(c)
	spec := service.GatewayRequestSpec{WorkspaceID: key.WorkspaceID, UserID: key.UserID, APIKeyID: key.ID, Route: c.Request.URL.Path, Protocol: protocol, Model: model, MaxAccountSwitches: switches}
	spec.Registration = registration
	if protocol == "media" && c.Request.Method == http.MethodPost && strings.Contains(c.Request.URL.Path, "/videos/") {
		spec.ExecutionKind = "job"
	}
	if len(geminiSwitches) > 0 {
		spec.MaxAccountSwitchesGemini = geminiSwitches[0]
		if key.Group != nil && key.Group.Platform == service.PlatformGemini {
			spec.MaxAccountSwitches = geminiSwitches[0]
		}
	}
	if key.GroupID != nil {
		spec.BillingGroupID = *key.GroupID
	}
	ctx, plan := capturer.CaptureRequestPlan(c.Request.Context(), spec)
	c.Request = c.Request.WithContext(ctx)
	stop := context.AfterFunc(ctx, plan.Cancel)
	return func() {
		defer finishBudget()
		stop()
		if ctx.Err() != nil {
			plan.Cancel()
		}
		plan.Finalize(c.Writer.Written())
	}
}

func claimGatewayUsage(parent context.Context) bool {
	plan := service.GatewayRequestPlanFromContext(parent)
	return plan == nil || plan.Record()
}
