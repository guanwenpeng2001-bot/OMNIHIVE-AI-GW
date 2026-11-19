package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	keycompat "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/BE-01"
	executioncompat "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/BE-06"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/tasks"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// ImageTaskGateway adapts a paid console request to the ordinary native image
// route, including moderation, allowance, credential, scheduler and receipts.
type ImageTaskGateway struct {
	DB            *sql.DB
	Access        identity.AccessEvaluator
	Scopes        identity.GatewayScopes
	Catalog       catalog.CatalogReader
	Keys          *service.APIKeyService
	Subscriptions *service.SubscriptionService
	Images        *OpenAIGatewayHandler
	Objects       service.BackupObjectStore
}

type imageJobJournal struct {
	journal.Store
	request, task string
	db            *sql.DB
	lease         tasks.Lease
	check         func(context.Context, int64) error
}

func (s imageJobJournal) Admit(ctx context.Context, in journal.Admission) (journal.AdmittedExecution, bool, error) {
	if in.Kind != "job" || in.LegacyID != s.task {
		return journal.AdmittedExecution{}, false, journal.ErrScope
	}
	in.RequestID = s.request
	return s.Store.Admit(ctx, in)
}
func (s imageJobJournal) Dispatch(ctx context.Context, e journal.AdmittedExecution, n int, account int64) (journal.Attempt, error) {
	if err := s.check(ctx, account); err != nil {
		return journal.Attempt{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return journal.Attempt{}, err
	}
	defer tx.Rollback()
	var live bool
	err = tx.QueryRowContext(ctx, `SELECT true FROM image_tasks WHERE id=$1 AND request_id=$2 AND lease_owner=$3 AND generation=$4 AND state='running' AND lease_until>clock_timestamp() FOR SHARE`, s.task, s.request, s.lease.Owner, s.lease.Generation).Scan(&live)
	if errors.Is(err, sql.ErrNoRows) {
		return journal.Attempt{}, tasks.LostLease
	}
	if err != nil {
		return journal.Attempt{}, err
	}
	// Retain the row lock through the durable single-use send permit.
	return s.Store.Dispatch(ctx, e, n, account)
}
func (g *ImageTaskGateway) verified(ctx context.Context, a tasks.Scope, id string, account int64) (string, string, error) {
	snap, err := g.Catalog.Snapshot(ctx, catalog.Perspective{Actor: a.ActorID, Workspace: a.WorkspaceID, Project: a.ProjectID, AccessVersion: a.Version, EndpointMetadata: true}, time.Now().UTC())
	if err != nil {
		return "", "", err
	}
	for _, d := range snap.Details {
		if d.Model.ID != id || d.Model.AccessState != "allowed" || d.Model.RouteState != "eligible" {
			continue
		}
		for _, ep := range d.Endpoints {
			if account != 0 && ep.ID != "ep_"+catalog.Digest([2]int64{a.ProjectID, account})[:32] {
				continue
			}
			for _, cap := range ep.Capabilities {
				if cap.Action == "image.generate" && cap.Protocol == "openai_images" && cap.VerifiedAt != nil && cap.State == "fresh" {
					version := snap.Version
					if cap.AdapterVersion != nil {
						version = *cap.AdapterVersion
					}
					return d.Model.Model, version, nil
				}
			}
		}
	}
	return "", "", &fault.Fault{Status: 422, Reason: "UNSUPPORTED_ACTION", Message: "This model has no verified image generation endpoint."}
}

func (g *ImageTaskGateway) Prepare(ctx context.Context, a tasks.Scope, w tasks.Work) (tasks.Prepared, error) {
	var out tasks.Prepared
	if g.Objects == nil || g.Images == nil || g.Images.gatewayService == nil || g.Catalog == nil || g.Scopes == nil || g.Access == nil || g.Keys == nil {
		return out, tasks.Unavailable
	}
	model, version, e := g.verified(ctx, a, w.Model, 0)
	if e != nil {
		return out, e
	}
	var digest string
	var project bool
	e = g.DB.QueryRowContext(ctx, `SELECT c.credential_digest,c.status='project' FROM api_key_credentials c JOIN api_keys k ON k.id=c.api_key_id AND k.workspace_id=c.workspace_id
 WHERE k.workspace_id=$1 AND k.user_id=$2 AND k.id=$3 AND k.deleted_at IS NULL AND c.revoked_at IS NULL AND (c.valid_until IS NULL OR c.valid_until>now())
 AND c.status IN ('project','current') ORDER BY c.id DESC LIMIT 1`, a.WorkspaceID, a.ActorID, w.Key).Scan(&digest, &project)
	if errors.Is(e, sql.ErrNoRows) {
		return out, fault.NotFound
	}
	if e != nil {
		return out, e
	}
	var frozen scope.Authorized
	var k *service.APIKey
	if project {
		p, err := g.Scopes.ProjectCredential(ctx, digest)
		if err != nil {
			return out, err
		}
		if p == nil || !p.Allows("image.generate", []string{model}) {
			return out, fault.Forbidden
		}
		frozen = p.Scope
		k, e = keycompat.ProjectKey(ctx, g.Keys, p)
	} else {
		frozen, e = g.Scopes.LegacyScope(ctx, a.WorkspaceID, w.Key, a.ActorID, digest)
		if e == nil {
			k, e = g.Keys.GetByID(ctx, w.Key)
		}
	}
	if e != nil {
		return out, e
	}
	if k == nil || k.User == nil || k.Group == nil || frozen.ProjectID != a.ProjectID || frozen.ActorID != a.ActorID || frozen.KeyOwnerID != a.ActorID || frozen.FundingUserID != a.ActorID {
		return out, fault.NotFound
	}
	if !service.GroupAllowsImageGeneration(k.Group) || k.Group.Platform != service.PlatformOpenAI {
		return out, fault.Forbidden
	}
	group := k.Group.ID
	ctx = scope.With(domain.WithWorkspaceScope(ctx, a.WorkspaceID), frozen)
	ctx = executioncompat.BindCredential((executioncompat.Validator{DB: g.DB, Scopes: g.Scopes}).Install(ctx), digest, "image.generate", w.IP, project, group)
	if e = journal.Authorize(ctx, frozen, model); e != nil {
		return out, fault.Forbidden
	}
	facts := keycompat.KeyFacts(k, true)
	if f := identity.EvaluateKey(facts); f != nil {
		return out, f
	}
	if k.Group.IsSubscriptionType() && g.Subscriptions == nil {
		return out, tasks.Unavailable
	}
	sub, f := identity.EvaluateFunding(ctx, facts, k.User.Balance > 0, identity.FundingChecks{LimitsFirst: true}, keycompat.Subscriptions(k, g.Subscriptions))
	if f != nil {
		if f.Reason == "INSUFFICIENT_BALANCE" {
			return out, tasks.Budget
		}
		return out, f
	}
	ctx, e = g.Keys.AttachModelAccessPolicy(ctx, k.ID)
	if e != nil {
		return out, e
	}
	js := imageJobJournal{journal.New(g.DB), w.Request, w.ID, g.DB, w.Lease, func(ctx context.Context, id int64) error {
		current, err := g.Access.Evaluate(ctx, a.ActorID, a.WorkspaceID, a.ProjectID, w.Action)
		if err != nil {
			return err
		}
		_, _, err = g.verified(ctx, current, w.Model, id)
		return err
	}}
	spec := service.GatewayRequestSpec{WorkspaceID: a.WorkspaceID, UserID: k.UserID, APIKeyID: k.ID, BillingGroupID: group, Model: model, Route: "/v1/images/generations", Protocol: "images", ExecutionID: w.ID}
	planctx, closePlan := g.Images.gatewayService.CaptureAsyncImageTaskPlan(journal.WithStore(ctx, js), spec, 30*time.Minute)
	ex, e := service.GatewayRequestPlanFromContext(planctx).AdmittedExecution()
	if e != nil || ex.RequestID != w.Request {
		closePlan()
		return out, tasks.Unavailable
	}
	return tasks.Prepared{Version: version, Close: closePlan, Run: func(runctx context.Context) tasks.Outcome {
		stop := context.AfterFunc(runctx, closePlan)
		defer stop()
		payload := map[string]any{"model": model, "prompt": w.Input.Prompt, "n": w.Input.Count}
		if w.Input.Size != "" {
			payload["size"] = w.Input.Size
		}
		data, _ := json.Marshal(payload)
		rec := httptest.NewRecorder()
		bounded := &taskResponse{ResponseRecorder: rec}
		c, _ := gin.CreateTestContext(bounded)
		c.Request = httptest.NewRequestWithContext(planctx, "POST", "/v1/images/generations", bytes.NewReader(data))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Request.RemoteAddr = net.JoinHostPort(w.IP, "0")
		c.Set(string(middleware.ContextKeyAPIKey), k)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: k.UserID, Concurrency: k.User.Concurrency})
		if sub != nil {
			c.Set(string(middleware.ContextKeySubscription), sub)
		}
		g.Images.Images(c)
		if bounded.overflow {
			return tasks.Outcome{State: "failed", Acceptance: "accepted"}
		}
		if rec.Code < 200 || rec.Code >= 300 {
			var sent, uncertain, accepted bool
			err := g.DB.QueryRowContext(runctx, `SELECT coalesce(bool_or(dispatched_at IS NOT NULL),false),coalesce(bool_or(acceptance='unknown'),false),coalesce(bool_or(acceptance='accepted'),false) FROM gateway_execution_attempts WHERE request_id=$1`, w.Request).Scan(&sent, &uncertain, &accepted)
			if err != nil || uncertain {
				return tasks.Outcome{State: "unknown", Acceptance: "unknown"}
			}
			if accepted {
				return tasks.Outcome{State: "failed", Acceptance: "accepted"}
			}
			acceptance := "not_sent"
			if sent {
				acceptance = "rejected"
			}
			return tasks.Outcome{State: "failed", Acceptance: acceptance}
		}
		capture := &taskUploads{objects: g.Objects}
		uploader := service.NewImageResultUploader(capture, "task-artifacts/"+w.ID+"/", 32<<20, nil)
		_, err := uploader.Rewrite(runctx, uuid.NewString(), rec.Body.Bytes())
		if err != nil || len(capture.outputs) == 0 {
			return tasks.Outcome{State: "failed", Acceptance: "accepted"}
		}
		return tasks.Outcome{State: "succeeded", Acceptance: "accepted", Artifacts: capture.outputs}
	}}, nil
}

// Intermediate base64 responses share the same worker bound as execution.
type taskResponse struct {
	*httptest.ResponseRecorder
	overflow bool
}

func (r *taskResponse) Write(p []byte) (int, error) {
	if r.overflow || r.Body.Len()+len(p) > 48<<20 {
		r.overflow = true
		return 0, io.ErrShortBuffer
	}
	return r.ResponseRecorder.Write(p)
}
func (r *taskResponse) WriteString(s string) (int, error) {
	if r.overflow || r.Body.Len()+len(s) > 48<<20 {
		r.overflow = true
		return 0, io.ErrShortBuffer
	}
	return r.ResponseRecorder.WriteString(s)
}

type taskUploads struct {
	objects service.BackupObjectStore
	outputs []tasks.Output
}

func (u *taskUploads) Save(ctx context.Context, key, mime string, data []byte) (string, error) {
	if len(data) > 32<<20 || len(u.outputs) >= 4 {
		return "", errors.New("image result exceeds task capacity")
	}
	mime = http.DetectContentType(data)
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return "", errors.New("invalid image")
	}
	n, e := u.objects.Upload(ctx, key, bytes.NewReader(data), mime)
	if e != nil {
		return "", e
	}
	if n != int64(len(data)) {
		return "", io.ErrShortWrite
	}
	u.outputs = append(u.outputs, tasks.Output{ID: fmt.Sprintf("image_%d", len(u.outputs)+1), Locator: key, ContentType: mime, Size: n})
	return key, nil
}
