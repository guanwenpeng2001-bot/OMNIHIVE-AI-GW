package handler

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	gwprotocol "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/protocol"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/trace"
	executioncompat "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/execution"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/billing/readmodel"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/byok"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ctxkey"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/gatewayerror"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httputil"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/tidwall/gjson"
)

// Limit the global middleware to public gateway routes. Management endpoints
// also live under /api/v1 and must not enter inference admission.
func billableGatewayRoute(c *gin.Context) bool {
	path := c.FullPath()
	if path == "" {
		return false
	}
	switch c.Request.Method {
	case http.MethodGet:
		return path == "/realtime" || path == "/v1/realtime" || path == "/responses" || path == "/v1/responses" || path == "/backend-api/codex/responses" || strings.HasPrefix(path, "/api-ws/v1/")
	case http.MethodPost, http.MethodPut, http.MethodPatch:
	default:
		return false
	}
	if strings.HasPrefix(path, "/api/v1/") {
		for _, prefix := range []string{"services", "apps", "tasks", "files", "batches", "realtime", "indices"} {
			if path == "/api/v1/"+prefix || strings.HasPrefix(path, "/api/v1/"+prefix+"/") {
				return true
			}
		}
		return false
	}
	if strings.HasPrefix(path, "/api/") {
		return false
	}
	if strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/v1beta/") || strings.HasPrefix(path, "/antigravity/") || strings.HasPrefix(path, "/backend-api/codex/") {
		return true
	}
	switch path {
	case "/completions", "/files", "/batches", "/batches/:batch_id/cancel", "/moderations", "/messages/batches", "/messages/batches/:message_batch_id/cancel":
		return true
	}
	return service.GatewayActionForRoute(c.Request.Method, path) != ""
}

func InstallExecutionJournal(db *sql.DB, scopes identity.GatewayScopes) gin.HandlerFunc {
	store := journal.New(db)
	traceStore := trace.NewPostgres(db)
	validator := executioncompat.Validator{DB: db, Scopes: scopes}
	return func(c *gin.Context) {
		if db != nil {
			ctx := service.WithProjectBudgetCoordinator(c.Request.Context(), readmodel.Store{DB: db})
			c.Request = c.Request.WithContext(ctx)
		}
		// Playground dispatches the private native router after submission.
		path := c.FullPath()
		if c.Request.Method == http.MethodPost && (strings.HasSuffix(path, "/playground/runs") || strings.HasSuffix(path, "/requests/:id/replay")) && byok.CheckSharedHeaders(c.Request.Header) != nil {
			rejectSharedFunding(c)
			return
		}
		if billableGatewayRoute(c) {
			c.Request = c.Request.WithContext(journal.RequireStore(c.Request.Context()))
			if byok.CheckSharedHeaders(c.Request.Header) != nil {
				rejectSharedFunding(c)
				return
			}
			if db == nil && journal.From(c.Request.Context()) == nil {
				gatewayerror.Write(c, http.StatusServiceUnavailable, "server_error", "Execution journal unavailable")
				c.Abort()
				return
			}
			if journal.From(c.Request.Context()) == nil {
				ctx := journal.WithStore(c.Request.Context(), store)
				ctx = trace.WithSink(ctx, func(emitCtx context.Context, d trace.DecisionTrace) {
					p := service.GatewayRequestPlanFromContext(emitCtx)
					if p == nil {
						return
					}
					ex, err := p.AdmittedExecution()
					if err != nil || ex.RequestID == "" {
						return
					}
					p.MarkTraceEmitted()
					d.Mode = trace.Execution
					if d.SchemaVersion == 0 {
						d.SchemaVersion = 1
					}
					if d.SnapshotID == "" {
						d.SnapshotID = journal.NewID()
					}
					if d.AsOf.IsZero() {
						d.AsOf = time.Now().UTC()
					}
					if d.PriceVersion == "" {
						d.PriceVersion = ex.Versions.Price
					}
					if d.CatalogVersion == "" {
						d.CatalogVersion = ex.Versions.Model
					}
					writeCtx, cancel := context.WithTimeout(context.WithoutCancel(emitCtx), 5*time.Second)
					defer cancel()
					_ = traceStore.Record(writeCtx, ex, d)
				})
				c.Request = c.Request.WithContext(validator.Install(ctx))
			}
		}
		c.Next()
	}
}

func defaultGatewayConversionPlan(routePath, proto string) (gwprotocol.ConversionPlan, bool) {
	var canonical string
	switch {
	case strings.HasSuffix(routePath, "/chat/completions"):
		canonical = "openai_chat"
	case strings.Contains(routePath, "/responses"):
		canonical = "openai_responses"
	case strings.HasSuffix(routePath, "/messages"):
		canonical = gwprotocol.AnthropicMessages
	case strings.Contains(routePath, "/v1beta/models/"):
		canonical = gwprotocol.GeminiGenerate
	case proto == "openai_chat" || proto == "openai_responses" || proto == gwprotocol.AnthropicMessages || proto == gwprotocol.GeminiGenerate:
		canonical = proto
	default:
		return gwprotocol.ConversionPlan{}, false
	}
	return gwprotocol.ConversionPlan{
		Mode:           gwprotocol.Native,
		SourceProtocol: canonical,
		TargetProtocol: canonical,
		Diagnostics:    []gwprotocol.Diagnostic{},
	}, true
}

func beginResponsesExecution(c *gin.Context, capturer gatewayPlanCapturer, key *service.APIKey, model string, body []byte, switches int, writeError func(*gin.Context, int, string, string)) (func(), bool) {
	return beginJournaledExecution(c, capturer, key, model, "responses", body, switches, writeError)
}

func beginJournaledExecution(c *gin.Context, capturer gatewayPlanCapturer, key *service.APIKey, model, protocol string, body []byte, switches int, writeError func(*gin.Context, int, string, string), geminiSwitches ...int) (func(), bool) {
	if !prepareGatewayPayload(c, body) {
		return func() {}, false
	}
	reg := journal.RegistrationFrom(c.Request.Context())
	nonstreamText := (protocol == "anthropic" || protocol == "responses" || protocol == "chat_completions") && !gjson.GetBytes(body, "stream").Bool()
	finish := beginRegisteredGatewayRequestPlan(c, capturer, key, model, protocol, switches, reg, nonstreamText, geminiSwitches...)
	return finish, checkGatewayAdmission(c, writeError)
}

func checkGatewayAdmission(c *gin.Context, writeError func(*gin.Context, int, string, string)) bool {
	p := service.GatewayRequestPlanFromContext(c.Request.Context())
	e, err := p.AdmittedExecution()
	if e.RequestID != "" {
		c.Header("X-Execution-ID", e.RequestID)
		c.Header("X-Request-ID", e.RequestID)
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.RequestID, e.RequestID))
	}
	if err == nil {
		return true
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
	c.Abort()
	return false
}

type gatewayPlanCapturer interface {
	CaptureRequestPlan(context.Context, service.GatewayRequestSpec) (context.Context, *service.GatewayRequestPlan)
}

// Each adapter calls this once after parsing the inbound model, before waiting
// or forwarding. It intentionally does not move the platform failover loops.
func beginGatewayRequestPlan(c *gin.Context, capturer gatewayPlanCapturer, key *service.APIKey, model, protocol string, switches int, geminiSwitches ...int) func() {
	if !c.GetBool("gateway_dispatch_input_checked") && journal.From(c.Request.Context()) != nil {
		if body, ok := c.Request.Body.(*httputil.PrereadBody); ok {
			prepareGatewayPayload(c, body.Bytes())
		} else if c.Request.Method == http.MethodGet {
			prepareGatewayPayload(c, nil)
		} else if c.GetHeader("Idempotency-Key") != "" {
			gatewayerror.Write(c, http.StatusServiceUnavailable, "server_error", "Execution input unavailable")
			c.Abort()
		}
	}
	if c.IsAborted() {
		ctx, cancel := context.WithCancel(c.Request.Context())
		cancel()
		c.Request = c.Request.WithContext(ctx)
		return func() {}
	}
	finish := beginRegisteredGatewayRequestPlan(c, capturer, key, model, protocol, switches, journal.RegistrationFrom(c.Request.Context()), false, geminiSwitches...)
	if !checkGatewayAdmission(c, gatewayerror.Write) {
		// Also fence in-flight work if callers propagate the context beyond
		// the ordinary early return.
		ctx, cancel := context.WithCancel(c.Request.Context())
		cancel()
		c.Request = c.Request.WithContext(ctx)
	}
	return finish
}

func beginRegisteredGatewayRequestPlan(c *gin.Context, capturer gatewayPlanCapturer, key *service.APIKey, model, protocol string, switches int, registration *journal.Registration, nonstreamText bool, geminiSwitches ...int) func() {
	if protocol == "anthropic" && isGeminiMessagesBridge(c) {
		protocol = "gemini"
	}
	finishBudget := installAttemptBudget(c)
	spec := service.GatewayRequestSpec{WorkspaceID: key.WorkspaceID, UserID: key.UserID, APIKeyID: key.ID, Route: c.Request.URL.Path, Protocol: protocol, Model: model, MaxAccountSwitches: switches}
	spec.Registration = registration
	spec.NonStreamingText = nonstreamText
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
	reqCtx := c.Request.Context()
	if _, ok := gwprotocol.ConversionPlanFromContext(reqCtx); !ok {
		if convPlan, hasDefault := defaultGatewayConversionPlan(c.Request.URL.Path, protocol); hasDefault {
			reqCtx = gwprotocol.WithConversionPlan(reqCtx, convPlan)
		}
	}
	ctx, plan := capturer.CaptureRequestPlan(reqCtx, spec)
	c.Request = c.Request.WithContext(ctx)
	stop := context.AfterFunc(ctx, plan.Cancel)
	return func() {
		defer finishBudget()
		stop()
		if streamTransportCommitted(c) && c.Writer.Size() > 0 && !gatewayStreamHasOnlyHeartbeats(c) {
			plan.ReviewInterruptedStream(context.WithoutCancel(ctx))
		}
		if nonstreamText && c.Writer.Status() == http.StatusBadGateway {
			plan.ReviewFailedGeneration(context.WithoutCancel(ctx))
		}
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
