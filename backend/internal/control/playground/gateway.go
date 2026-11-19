package playground

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/protocol"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/trace"
	keycompat "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/BE-01"
	execcompat "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/BE-06"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	pg "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/playground"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/policy"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ctxkey"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/logger"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"go.uber.org/zap"
)

// NativeRoutes is a private router over the same adapters and action middleware
// as the public gateway. Only this package can install its selected credential.
type credential struct {
	key *service.APIKey
	sub *service.UserSubscription
}
type credentialTag struct{}

var privateNativeLogger = zap.NewNop()

func NativeRoutes(dispatch *service.GatewayActionDispatch, composite *service.CompositeRouteResolver, action gin.HandlerFunc, common ...gin.HandlerFunc) http.Handler {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		v, ok := c.Request.Context().Value(credentialTag{}).(credential)
		if !ok {
			c.AbortWithStatus(403)
			return
		}
		ctx := ctxkey.WithPrivateGatewayContent(c.Request.Context())
		// Shared handlers use request loggers for parse errors and provider diagnostics.
		c.Request = c.Request.WithContext(logger.IntoContext(ctx, privateNativeLogger))
		c.Set(string(middleware.ContextKeyAPIKey), v.key)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: v.key.UserID, Concurrency: v.key.User.Concurrency})
		c.Set(string(middleware.ContextKeyUserRole), v.key.User.Role)
		if v.sub != nil {
			c.Set(string(middleware.ContextKeySubscription), v.sub)
		}
		c.Next()
	})
	r.Use(common...)
	r.Use(middleware.GroupModelAllowlist())
	v1 := r.Group("/v1", service.CompositeTargetPlatformMiddleware(composite), action)
	v1.POST("/chat/completions", dispatch.Handler("chat"))
	v1.POST("/responses", dispatch.Handler("responses"))
	v1.POST("/messages", dispatch.Handler("messages"))
	r.POST("/v1beta/models/*modelAction", service.CompositeGeminiTargetPlatformMiddleware(composite), action, dispatch.Handler("generate_content"))
	return r
}

type Gateway struct {
	DB            *sql.DB
	Access        identity.AccessEvaluator
	Scopes        identity.GatewayScopes
	Keys          *service.APIKeyService
	Subscriptions *service.SubscriptionService
	Catalog       catalog.CatalogReader
	Policies      *policy.Store
	Plans         *service.GatewayService
	Routes        http.Handler
}
type prepared struct {
	ctx       context.Context
	key       *service.APIKey
	funding   *service.UserSubscription
	frozen    scope.Authorized
	native    pg.Native
	model, ip string
	scenario  policy.Scenario
	decision  trace.DecisionTrace
	account   int64
}

func (g *Gateway) prepare(ctx context.Context, a identity.Access, s pg.Submit, ip string, replay bool) (prepared, error) {
	var p prepared
	if g == nil || g.DB == nil || g.Plans == nil || g.Routes == nil || g.Keys == nil || g.Scopes == nil || g.Policies == nil || g.Catalog == nil || g.Access == nil {
		return p, pg.Unavailable
	}
	owner, err := g.Policies.KeyOwner(ctx, a.WorkspaceID, a.ProjectID, s.Key)
	if err != nil {
		return p, err
	}
	if owner != a.ActorID && (s.Payer != owner || a.Require("keys.act_as") != nil) {
		return p, fault.NotFound
	}
	if s.Payer != 0 && s.Payer != owner {
		return p, fault.Invalid("/payer_user_id")
	}
	var digest string
	var project bool
	err = g.DB.QueryRowContext(ctx, `SELECT credential_digest,status='project' FROM api_key_credentials WHERE workspace_id=$1 AND api_key_id=$2
 AND status IN ('current','project') AND revoked_at IS NULL AND (valid_until IS NULL OR valid_until>now()) ORDER BY id DESC LIMIT 1`, a.WorkspaceID, s.Key).Scan(&digest, &project)
	if errors.Is(err, sql.ErrNoRows) {
		return p, fault.NotFound
	}
	if err != nil {
		return p, err
	}
	var k *service.APIKey
	var base scope.Authorized
	var keyPolicy *identity.KeyPolicy
	if project {
		pol, e := g.Scopes.ProjectCredential(ctx, digest)
		if e != nil {
			return p, e
		}
		if pol == nil {
			return p, fault.NotFound
		}
		keyPolicy = pol
		base = pol.Scope
		k, err = keycompat.ProjectKey(ctx, g.Keys, pol)
	} else {
		base, err = g.Scopes.LegacyScope(ctx, a.WorkspaceID, s.Key, owner, digest)
		if err == nil {
			k, err = g.Keys.GetByID(ctx, s.Key)
		}
	}
	if err != nil {
		return p, err
	}
	if base.WorkspaceID != a.WorkspaceID || base.ProjectID != a.ProjectID || base.KeyID != s.Key || base.KeyOwnerID != owner || base.FundingUserID != owner || base.Version != a.Version || k == nil || k.User == nil || k.Group == nil {
		return p, fault.NotFound
	}
	snap, err := g.Catalog.Snapshot(ctx, catalog.Perspective{Actor: a.ActorID, Workspace: a.WorkspaceID, Project: a.ProjectID, AccessVersion: a.Version, EndpointMetadata: true}, time.Now().UTC())
	if err != nil {
		return p, err
	}
	model, err := selectModel(snap, s.Model, replay)
	if err != nil {
		return p, err
	}
	native, err := s.Native(model.Model.Model)
	if err != nil {
		return p, err
	}
	ctx = domain.WithWorkspaceScope(ctx, a.WorkspaceID)
	ownerCtx := execcompat.BindCredential((execcompat.Validator{DB: g.DB, Scopes: g.Scopes}).Install(ctx), digest, s.Action, ip, project, k.Group.ID)
	frozen := base
	frozen.ActorID = a.ActorID
	check := func(ctx context.Context, actual scope.Authorized, model string) error {
		if actual != frozen {
			return journal.ErrScope
		}
		action := "playground.run"
		if replay {
			action = "requests.replay"
		}
		fresh, err := g.Access.Evaluate(ctx, a.ActorID, a.WorkspaceID, a.ProjectID, action)
		if err != nil {
			return err
		}
		if fresh.Version != a.Version || fresh.Require("playground.run") != nil || (s.Options.Raw && fresh.Require("playground.raw") != nil) || (owner != a.ActorID && fresh.Require("keys.act_as") != nil) {
			return fault.Forbidden
		}
		// Keep dispatch cancellation while using the credential validator bound to the owner.
		bound := execcompat.BindCredential((execcompat.Validator{DB: g.DB, Scopes: g.Scopes}).Install(ctx), digest, s.Action, ip, project, k.Group.ID)
		return journal.Authorize(bound, base, model)
	}
	if err = journal.Authorize(ownerCtx, base, model.Model.Model); err != nil {
		return p, fault.Forbidden
	}
	facts := keycompat.KeyFacts(k, true)
	if f := identity.EvaluateKey(facts); f != nil {
		return p, f
	}
	if k.Group.IsSubscriptionType() && g.Subscriptions == nil {
		return p, pg.Unavailable
	}
	sub, f := identity.EvaluateFunding(ctx, facts, k.User.Balance > 0, identity.FundingChecks{LimitsFirst: true}, keycompat.Subscriptions(k, g.Subscriptions))
	if f != nil {
		return p, f
	}
	ctx, err = g.Keys.AttachModelAccessPolicy(ctx, k.ID)
	if err != nil {
		return p, err
	}
	ctx = scope.With(context.WithValue(ctx, ctxkey.Group, k.Group), frozen)
	ctx = journal.WithReauthorization(ctx, check)
	policyModel := model.Model.ID
	// Earlier project credentials may explicitly select native model names.
	if keyPolicy != nil && keyPolicy.Mode == "allow_list" && !slices.Contains(keyPolicy.Models, policyModel) && slices.Contains(keyPolicy.Models, model.Model.Model) {
		policyModel = model.Model.Model
	}
	scenario := policy.Scenario{Project: strconv.FormatInt(a.ProjectID, 10), Key: strconv.FormatInt(k.ID, 10), Model: policyModel, Action: s.Action, Protocol: s.Protocol, Input: strconv.Itoa(native.InputBound), Output: strconv.Itoa(native.OutputBound)}
	if native.Tools {
		scenario.Required = []string{"tools"}
	}
	funding := "wallet"
	if sub != nil {
		funding = "subscription"
	}
	decision, account, err := g.route(ctx, frozen, k.Group.ID, scenario, snap.Version, model, funding)
	if err != nil {
		return p, err
	}
	return prepared{ctx, k, sub, frozen, native, model.Model.Model, ip, scenario, decision, account}, nil
}

type runJournal struct {
	journal.Store
	run     pg.Summary
	account int64
	db      *sql.DB
	allowed func(context.Context) error
}

func (j runJournal) Admit(ctx context.Context, in journal.Admission) (journal.AdmittedExecution, bool, error) {
	in.RequestID = j.run.Request
	in.Kind = "playground"
	in.LegacyID = j.run.Run
	return j.Store.Admit(ctx, in)
}
func (j runJournal) Dispatch(ctx context.Context, e journal.AdmittedExecution, n int, account int64) (journal.Attempt, error) {
	if account != j.account || n != 1 || e.RequestID != j.run.Request {
		return journal.Attempt{}, journal.ErrDispatch
	}
	if err := j.allowed(ctx); err != nil {
		return journal.Attempt{}, err
	}
	// Migration 302 locks the run inside the same transaction as the
	// ordinary BE-06 permit. A concurrent cancel cannot slip between them.
	a, err := j.Store.Dispatch(ctx, e, n, account)
	if err != nil {
		return a, err
	}
	if plan, ok := protocol.ConversionPlanFromContext(ctx); ok {
		data, err := json.Marshal(plan)
		if err != nil {
			return a, err
		}
		_, err = j.db.ExecContext(ctx, "UPDATE gateway_execution_attempts SET conversion_plan=$2 WHERE attempt_id=$1", a.ID, string(data))
		if err != nil {
			return a, err
		}
	}
	return a, nil
}
func (g *Gateway) admit(p prepared, r pg.Summary, reg *journal.Registration) (context.Context, *service.GatewayRequestPlan, func(), error) {
	j := runJournal{journal.New(g.DB), r, p.account, g.DB, func(ctx context.Context) error {
		fresh, err := g.Policies.PlanExecution(ctx, p.frozen, p.scenario, nil, p.decision.Seed)
		if err != nil {
			return err
		}
		if fresh.Selected == nil || fresh.Selected.CandidateID != p.decision.Selected.CandidateID || fresh.Selected.FundingSource != p.decision.Selected.FundingSource {
			return journal.ErrDispatch
		}
		return nil
	}}
	ctx, stop := service.WithAttemptBudget(journal.WithStore(p.ctx, j), 1, service.DefaultGenerationDeadline)
	ctx = service.WithPrefetchedStickySession(ctx, p.account, p.key.Group.ID, true)

	spec := service.GatewayRequestSpec{ExecutionKind: "playground", Registration: reg, WorkspaceID: p.frozen.WorkspaceID, UserID: p.key.UserID, APIKeyID: p.key.ID, BillingGroupID: p.key.Group.ID, Route: strings.Split(p.native.Path, "?")[0], Action: "text.chat", Protocol: p.native.Protocol, Model: p.model, ModelVersion: p.decision.CatalogVersion, PriceVersion: p.decision.PriceVersion}
	ctx, plan := g.Plans.CaptureRequestPlan(ctx, spec)
	ex, err := plan.AdmittedExecution()
	closePlan := func() {
		if ctx.Err() != nil {
			plan.Cancel()
		} else {
			plan.Finalize(false)
		}
		stop()
	}
	if err == nil && ex.RequestID != r.Request {
		err = journal.ErrDispatch
	}
	if err == nil {
		err = trace.NewPostgres(g.DB).Record(ctx, ex, p.decision)
	}
	if err == nil {
		err = plan.ReserveBudget(ctx, service.RequestBudgetEstimate{Model: p.model, InputTokens: p.native.InputBound, OutputTokens: p.native.OutputBound, RateMultiplier: p.key.Group.RateMultiplier, SkipBalanceHold: p.funding != nil})
	}
	if err != nil {
		closePlan()
		if errors.Is(err, service.ErrAPIKeyBudgetExceeded) || errors.Is(err, service.ErrAPIKeyBudgetInsufficientBalance) {
			return nil, nil, nil, &fault.Fault{Status: 402, Reason: "INSUFFICIENT_FUNDS", Message: "The confirmed payer has insufficient budget."}
		}
		return nil, nil, nil, err
	}
	return ctx, plan, closePlan, nil
}
func (g *Gateway) execute(ctx context.Context, p prepared, w http.ResponseWriter) error {
	req, err := http.NewRequestWithContext(context.WithValue(ctx, credentialTag{}, credential{p.key, p.funding}), "POST", p.native.Path, bytes.NewReader(p.native.Body))
	if err != nil {
		return err
	}
	req.RemoteAddr = net.JoinHostPort(p.ip, "0")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	g.Routes.ServeHTTP(w, req)
	return nil
}
