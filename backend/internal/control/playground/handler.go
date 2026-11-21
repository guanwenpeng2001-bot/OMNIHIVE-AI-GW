package playground

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	identityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/protocol"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	pg "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/playground"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/requests"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ip"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type Handler struct {
	Gateway        *Gateway
	TrustForwarded bool
	// PreviewTTL controls explicitly consented text previews in this process.
	// Zero disables retention. The default constructor uses thirty seconds.
	PreviewTTL   time.Duration
	previews     sync.Map
	previewSlots chan struct{}
}

func New(g *Gateway, trustForwarded bool) *Handler {
	return &Handler{Gateway: g, TrustForwarded: trustForwarded, PreviewTTL: 30 * time.Second, previewSlots: make(chan struct{}, 128)}
}
func (h *Handler) store() pg.Store { return pg.Store{DB: h.Gateway.DB} }
func Register(parent *gin.RouterGroup, h *Handler, session ...gin.HandlerFunc) {
	p := parent.Group("/workspaces/:workspace_id/projects/:project_id", append([]gin.HandlerFunc{identityhttp.TenantHeaders}, session...)...)
	p.POST("/playground/runs", middleware.GatewayRoutingConstraints(), h.route("playground.run", h.submit))
	p.GET("/playground/runs/by-idempotency-key", h.route("playground.read", func(c *gin.Context, a identity.Access) error {
		key := c.GetHeader("Idempotency-Key")
		if key == "" || len(key) > 200 {
			return fault.Invalid("/Idempotency-Key")
		}
		r, err := h.store().ByKey(c.Request.Context(), a, key, nil)
		if err == nil {
			h.summary(c, a, r)
		}
		return err
	}))
	p.GET("/playground/runs/:id", h.route("playground.read", func(c *gin.Context, a identity.Access) error {
		r, err := h.store().Detail(c.Request.Context(), a, c.Param("id"))
		if err == nil {
			h.summary(c, a, r)
		}
		return err
	}))
	p.POST("/playground/runs/:id/cancel", h.route("playground.cancel", func(c *gin.Context, a identity.Access) error {
		var v map[string]any
		b, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
		if err != nil || json.Unmarshal(b, &v) != nil || v == nil || len(v) != 0 {
			return fault.Invalid("/body")
		}
		r, err := h.store().Cancel(c.Request.Context(), a, c.Param("id"))
		if err == nil {
			auditRun(c, r)
			success(c, 202, r)
		}
		return err
	}))
	p.POST("/requests/:id/replay", middleware.GatewayRoutingConstraints(), h.route("requests.replay", h.submit))
}
func (h *Handler) route(action string, fn func(*gin.Context, identity.Access) error) gin.HandlerFunc {
	return func(c *gin.Context) {
		sub, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok || sub.UserID <= 0 {
			fail(c, fault.Unauthenticated, nil)
			return
		}
		w, e := strconv.ParseInt(c.Param("workspace_id"), 10, 64)
		p, f := strconv.ParseInt(c.Param("project_id"), 10, 64)
		if e != nil || f != nil || w <= 0 || p <= 0 {
			fail(c, fault.NotFound, nil)
			return
		}
		a, err := h.Gateway.Access.Evaluate(c.Request.Context(), sub.UserID, w, p, action)
		if err == nil || errors.Is(err, fault.Forbidden) {
			middleware.SetAuditExtra(c, map[string]any{"workspace_id": a.WorkspaceID, "project_id": a.ProjectID})
		}
		if err == nil {
			err = fn(c, a)
		}
		if err != nil && !c.Writer.Written() {
			fail(c, err, nil)
		}
	}
}
func success(c *gin.Context, status int, data any) {
	msg := "success"
	if status == 202 {
		msg = "accepted"
	}
	c.JSON(status, gin.H{"code": 0, "message": msg, "data": data})
}
func fail(c *gin.Context, err error, r *pg.Summary) {
	var f *fault.Fault
	if !errors.As(err, &f) {
		f = pg.Unavailable
	}
	category, retry, result := "validation", "never", "validation_failed"
	switch f.Status {
	case 401, 403, 404:
		category, result = "access", "access_denied"
	case 402, 429:
		category, result = "funding", "funding_denied"
	case 409:
		category, result = "conflict", "conflict"
	case 503:
		category, retry, result = "dependency", "retry_later", "dependency_failed"
	}
	var url any
	if r != nil {
		url = r.StatusURL
		retry = "query_status"
	}
	middleware.SetAuditExtra(c, map[string]any{"result": result})
	fields := []gin.H{}
	if f.Field != "" {
		fields = append(fields, gin.H{"path": f.Field, "reason": f.Reason})
	}
	c.AbortWithStatusJSON(f.Status, gin.H{"code": f.Status, "message": f.Message, "reason": f.Reason, "metadata": gin.H{"request_id": c.Writer.Header().Get("X-Request-ID"), "category": category, "retry": retry, "fields": fields, "operation_id": nil, "status_url": url}, "data": nil})
}
func (h *Handler) summary(c *gin.Context, a identity.Access, r pg.Summary) {
	if a.Require("requests.content.read") == nil {
		if value, ok := h.previews.Load(r.Run); ok {
			entry, ok := value.(preview)
			if ok && time.Now().Before(entry.until) {
				r.Retained = true
				r.Preview = entry.text
			}
		}
	}
	auditRun(c, r)
	middleware.SetAuditExtra(c, map[string]any{"text_preview_served": r.Retained})
	success(c, 200, r)
}

func auditRun(c *gin.Context, r pg.Summary) {
	middleware.SetAuditExtra(c, map[string]any{
		"run_id": r.Run, "run_request_id": r.Request, "outcome": r.State, "settlement_state": r.Settlement,
	})
}

type preview struct {
	text  string
	until time.Time
}

func (h *Handler) retain(id, text string) {
	ttl := min(h.PreviewTTL, 5*time.Minute)
	if text == "" || ttl <= 0 {
		return
	}
	select {
	case h.previewSlots <- struct{}{}:
	default:
		return
	}
	h.previews.Store(id, preview{text, time.Now().Add(ttl)})
	time.AfterFunc(ttl, func() { h.previews.Delete(id); <-h.previewSlots })
}
func (h *Handler) submit(c *gin.Context, a identity.Access) error {
	ctx := c.Request.Context()
	if err := a.Require("playground.run"); err != nil {
		return err
	}
	key, parent := c.GetHeader("Idempotency-Key"), c.Param("id")
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, pg.MaxInput))
	if err != nil {
		return fault.Invalid("/input")
	}
	in, fp, err := pg.Parse(body, key, parent)
	if err != nil {
		return err
	}
	if layer := service.GatewayRequestRoutingLayer(ctx); layer != nil {
		fp = sha256.Sum256(append(fp[:], layer.Version...))
	}
	middleware.SetAuditExtra(c, map[string]any{
		"raw_events_requested": in.Options.Raw, "text_preview_requested": in.Options.Retain,
	})
	if in.Options.Raw && a.Require("playground.raw") != nil || in.Options.Retain && a.Require("requests.content.read") != nil {
		return fault.Forbidden
	}
	r, err := h.store().ByKey(ctx, a, key, fp[:])
	if err == nil {
		middleware.SetAuditExtra(c, map[string]any{"admission": "idempotent_replay"})
		h.summary(c, a, r)
		return nil
	}
	if !errors.Is(err, fault.NotFound) {
		return err
	}
	if parent != "" {
		d, e := requests.NewStore(h.Gateway.DB).Detail(ctx, requests.Projection{Scope: requests.Scope{WorkspaceID: a.WorkspaceID, ProjectID: a.ProjectID}}, parent)
		if e != nil {
			return e
		}
		if d.Protocol == nil || d.RequestedModel == nil {
			return fault.Invalid("/replay")
		}
		in.Protocol, in.Model, in.Action = *d.Protocol, *d.RequestedModel, "text.chat"
		var reused bool
		e = h.Gateway.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT FROM playground_runs WHERE request_id::text=$1 AND key_hash=$2)", parent, pg.KeyHash(key)).Scan(&reused)
		if e != nil {
			return e
		}
		if reused {
			return fault.Conflict
		}
	}
	accept := "text/event-stream"
	if !in.Streaming() {
		accept = "application/json"
	}
	if h := c.GetHeader("Accept"); h != "" && c.NegotiateFormat(accept) == "" {
		return &fault.Fault{Status: 406, Reason: "NOT_ACCEPTABLE", Message: "Accept must match the selected stream option."}
	}
	runctx, cancel := context.WithTimeout(ctx, service.DefaultGenerationDeadline)
	defer cancel()
	p, err := h.Gateway.prepare(runctx, a, in, ip.GetSecurityClientIP(c, h.TrustForwarded), parent != "")
	if err != nil {
		middleware.SetAuditExtra(c, map[string]any{"admission": "rejected"})
		return err
	}
	deadline, _ := runctx.Deadline()
	r, won, err := h.store().Claim(ctx, a, in, p.frozen.FundingUserID, key, parent, fp, deadline)
	if err != nil {
		middleware.SetAuditExtra(c, map[string]any{"admission": "rejected"})
		return err
	}
	if !won {
		middleware.SetAuditExtra(c, map[string]any{"admission": "idempotent_replay"})
		h.summary(c, a, r)
		return nil
	}
	// Poll the durable cancel bit even while the native adapter awaits its first byte.
	// The journal checks the same row at the dispatch boundary.
	go h.monitor(runctx, cancel, r.Run)
	planctx, plan, closePlan, err := h.Gateway.admit(p, r, in.Registration)
	if err != nil {
		final, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer stop()
		state, finishErr := h.store().Finish(final, r, false, false, runctx.Err() != nil, 0)
		if finishErr != nil {
			state = "unknown"
		}
		r.State = state
		auditRun(c, r)
		middleware.SetAuditExtra(c, map[string]any{"admission": "rejected"})
		fail(c, err, &r)
		return nil
	}
	defer closePlan()
	middleware.SetAuditExtra(c, map[string]any{"admission": "admitted"})
	c.Header("X-Request-ID", r.Request)
	var seq int64
	var preview bytes.Buffer
	send := func(e pg.Event) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		seq++
		body := make(map[string]any, len(e.Data)+3)
		for k, v := range e.Data {
			body[k] = v
		}
		body["seq"], body["run_id"], body["request_id"] = seq, r.Run, r.Request
		if in.Options.Retain && e.Name == "delta" && body["kind"] == "text" {
			s, _ := body["text"].(string)
			if preview.Len()+len(s) <= 8192 {
				preview.WriteString(s)
			}
		}
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		if err = h.store().Progress(ctx, r.Run, seq); err != nil {
			return err
		}
		if _, err = fmt.Fprintf(c.Writer, "event: %s\nid: %d\ndata: %s\n\n", e.Name, seq, b); err == nil {
			c.Writer.Flush()
		}
		return err
	}
	if in.Streaming() {
		c.Header("Content-Type", "text/event-stream")
		c.Header("X-Accel-Buffering", "no")
		err = send(pg.Event{Name: "started", Data: map[string]any{"protocol": in.Protocol, "price_version": plan.Snapshot().PriceVersion, "payer_user_id": strconv.FormatInt(r.Payer, 10), "status_url": r.StatusURL, "detail_url": r.DetailURL}})
	}
	out := &nativeResponse{header: make(http.Header), decoder: pg.Decoder{Protocol: in.Protocol}, stream: in.Streaming(), raw: in.Options.Raw, emit: send, cancel: cancel, closed: make(chan bool)}
	stopNotify := context.AfterFunc(planctx, func() { close(out.closed) })
	defer stopNotify()
	if err == nil {
		err = h.Gateway.execute(planctx, p, out)
	}
	completed := err == nil && out.err == nil && out.status >= 200 && out.status < 300
	var output json.RawMessage
	if in.Streaming() {
		completed = completed && out.decoder.Completed && !out.decoder.Failed
	} else if completed {
		output, completed = pg.SyncOutput(in.Protocol, out.pending.Bytes())
	}
	final, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	if in.Streaming() && ctx.Err() == nil {
		var data []byte
		if e := h.Gateway.DB.QueryRowContext(final, "SELECT conversion_plan FROM gateway_execution_attempts WHERE request_id=$1 AND conversion_plan IS NOT NULL ORDER BY ordinal LIMIT 1", r.Request).Scan(&data); e == nil {
			var v protocol.ConversionPlan
			if json.Unmarshal(data, &v) == nil {
				for _, d := range v.Diagnostics {
					if send(pg.Event{Name: "diagnostic", Data: map[string]any{"code": d.Reason, "field": d.Field, "severity": d.Severity}}) != nil {
						break
					}
				}
			}
		}
	}
	state, e := h.store().Finish(final, r, completed, out.decoder.Failed, runctx.Err() != nil, seq)
	if e != nil {
		state = "unknown"
	}
	if in.Options.Retain && preview.Len() > 0 {
		h.retain(r.Run, preview.String())
	}
	if latest, e := h.store().Detail(final, a, r.Run); e == nil {
		r = latest
	}
	r.State = state
	auditRun(c, r)
	data := map[string]any{"outcome": state, "settlement_state": r.Settlement, "status_url": r.StatusURL, "detail_url": r.DetailURL}
	name := "finished"
	if state != "succeeded" {
		name = "failed"
		data["reason"] = "EXECUTION_FAILED"
		data["retry"] = "never"
		switch state {
		case "unknown":
			data["reason"] = "OUTCOME_UNKNOWN"
			data["retry"] = "query_status"
		case "canceled":
			data["reason"] = "USER_CANCELED"
		}
	}
	if in.Streaming() {
		if ctx.Err() == nil {
			_ = send(pg.Event{Name: name, Data: data})
		}
		return nil
	}
	data["run_id"], data["request_id"], data["protocol"], data["output"] = r.Run, r.Request, in.Protocol, output
	success(c, 200, data)
	return nil
}
func (h *Handler) monitor(ctx context.Context, cancel context.CancelFunc, id string) {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			ok, err := h.store().Active(ctx, id)
			if err != nil || !ok {
				cancel()
				return
			}
		}
	}
}
