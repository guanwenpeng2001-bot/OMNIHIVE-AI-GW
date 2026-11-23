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
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/protocol"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/trace"
	keycompat "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/billing/readmodel"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/byok"
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
	price     service.PlaygroundPrice
}

func (g *Gateway) prepare(ctx context.Context, a identity.Access, s pg.Submit, ip string, replay bool) (prepared, error) {
	var p prepared
	for name, value := range s.Input {
		if err := byok.CheckSharedFunding(name, value); err != nil {
			return p, err
		}
	}
	if g == nil || g.Plans == nil || g.Routes == nil || g.Policies == nil {
		return p, pg.Unavailable
	}
	selected, models, model, err := g.checkRunModel(ctx, a, s, time.Now().UTC())
	if err != nil {
		return p, err
	}
	native, err := s.Native(model.Model.Model)
	if err != nil {
		return p, err
	}
	k, frozen := selected.key, selected.scope
	sub, err := g.checkSelectedFunding(selected.ctx, k, ip)
	if err != nil {
		return p, err
	}
	policyModel := model.Model.ID
	// The routing journal also consumes historical native entries while
	// its policy graph is migrated to the effective Key evaluator.
	if rule := selected.models.KeyRestriction; rule != nil && rule.Ceiling != nil && !slices.Contains(rule.Ceiling, policyModel) {
		if i := slices.IndexFunc(rule.Ceiling, models.matches(model)); i >= 0 {
			policyModel = rule.Ceiling[i]
		}
	}
	scenario := policy.Scenario{Project: strconv.FormatInt(a.ProjectID, 10), Key: strconv.FormatInt(k.ID, 10), Model: policyModel, Action: s.Action, Protocol: s.Protocol, Input: strconv.Itoa(native.InputBound), Output: strconv.Itoa(native.OutputBound)}
	if native.Tools {
		scenario.Required = []string{"tools"}
	}
	funding := "wallet"
	if sub != nil {
		funding = "subscription"
	}
	ctx = context.WithValue(selected.ctx, ctxkey.Group, k.Group)
	decision, account, price, err := g.route(ctx, frozen, k, scenario, models.snapshot.Version, model, models.accounts, funding)
	if err != nil {
		return p, err
	}
	check := func(ctx context.Context, actual scope.Authorized, nativeModel string) error {
		if actual != frozen || nativeModel != model.Model.Model {
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
		if fresh.Version != a.Version || fresh.Require("playground.run") != nil ||
			s.Options.Raw && fresh.Require("playground.raw") != nil ||
			s.Options.Retain && fresh.Require("requests.content.read") != nil {
			return fault.Forbidden
		}
		in := s
		in.Model = model.Model.ID
		key, current, actualModel, err := g.checkRunModel(ctx, fresh, in, time.Now().UTC())
		if err != nil {
			return err
		}
		if key.scope != frozen || key.digest != selected.digest || key.key.Group.ID != k.Group.ID ||
			actualModel.Model.Model != nativeModel ||
			!slices.ContainsFunc(actualModel.Endpoints, func(ep catalog.Endpoint) bool { return current.accounts[ep.ID] == account }) {
			return journal.ErrScope
		}
		activeSub, err := g.checkSelectedFunding(ctx, key.key, ip)
		if err != nil {
			return err
		}
		if (activeSub == nil) != (sub == nil) || sub != nil && activeSub.ID != sub.ID {
			return journal.ErrScope
		}
		return nil
	}
	ctx = journal.WithReauthorization(ctx, check)
	return prepared{ctx, k, sub, frozen, native, model.Model.Model, ip, scenario, decision, account, price}, nil
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
	// ordinary dispatch permit. A concurrent cancel cannot slip between them.
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
func (g *Gateway) admit(p *prepared, r pg.Summary, reg *journal.Registration) (context.Context, *service.GatewayRequestPlan, func(), error) {
	if p.funding != nil {
		// Advance subscription windows only after the run wins its claim.
		sub, denial := keycompat.Subscriptions(p.key, g.Subscriptions).Validate(p.ctx, p.funding)
		if denial != nil {
			return nil, nil, nil, denial
		}
		p.funding = sub
	}
	j := runJournal{journal.New(g.DB), r, p.account, g.DB, func(ctx context.Context) error {
		fresh, err := g.Policies.PlanExecution(ctx, p.frozen, p.scenario, service.GatewayRequestRoutingLayer(p.ctx), p.decision.Seed)
		if err != nil {
			return err
		}
		if fresh.Selected == nil && service.HasGatewayRoutingConstraints(p.ctx) {
			return policy.NativePolicyDenied
		}
		if fresh.Selected == nil || fresh.Selected.CandidateID != p.decision.Selected.CandidateID || fresh.Selected.FundingSource != p.decision.Selected.FundingSource {
			return journal.ErrDispatch
		}
		return nil
	}}
	ctx, stop := service.WithAttemptBudget(journal.WithStore(p.ctx, j), 1, service.DefaultGenerationDeadline)
	ctx = service.WithProjectBudgetCoordinator(ctx, readmodel.Store{DB: g.DB})
	ctx = service.WithPrefetchedStickySession(ctx, p.account, p.key.Group.ID, true)
	ctx = service.WithRequiredAccount(ctx, p.account)
	ctx = service.WithPlaygroundPrice(ctx, p.price)

	spec := service.GatewayRequestSpec{ExecutionKind: "playground", Registration: reg, WorkspaceID: p.frozen.WorkspaceID, UserID: p.key.UserID, APIKeyID: p.key.ID, BillingGroupID: p.key.Group.ID, Route: strings.Split(p.native.Path, "?")[0], Action: "text.chat", Protocol: p.native.Protocol, Model: p.model, ModelVersion: p.decision.CatalogVersion, PriceVersion: p.decision.PriceVersion, PricingAt: p.price.At()}
	ctx, plan := g.Plans.CaptureRequestPlan(ctx, spec)
	ex, err := plan.AdmittedExecution()
	closePlan := func() {
		if ctx.Err() != nil {
			plan.Cancel()
		}
		plan.Finalize(false)
		stop()
	}
	if err == nil && ex.RequestID != r.Request {
		err = journal.ErrDispatch
	}
	if err == nil {
		err = trace.NewPostgres(g.DB).Record(ctx, ex, p.decision)
	}
	if err == nil {
		err = plan.ReserveBudget(ctx, service.RequestBudgetEstimate{Model: p.model, Group: p.key.Group, InputTokens: p.native.InputBound, OutputTokens: p.native.OutputBound, RateMultiplier: p.key.Group.RateMultiplier, SkipBalanceHold: p.funding != nil, SkipKeyBudget: !p.key.HasPeriodBudget()})
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
	// The shared native router may execute only the payer and account
	// selected at admission. Tenant attempts require byok.Gateway.Send.
	funding := "wallet"
	if p.funding != nil {
		funding = "subscription"
	}
	if p.decision.Selected == nil || p.decision.Selected.FundingSource != funding {
		return fault.Forbidden
	}
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
