package tasks

import (
	"net/url"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/tasks"
)

func (h *Handler) page(r request, items bool) (domain.Page, token, error) {
	p := domain.Page{AsOf: time.Now().UTC(), Limit: 25}
	resource := "tasks"
	if items {
		resource = "items/" + r.task
	}
	v := binding(r, resource)
	q, e := url.ParseQuery(r.Request.URL.RawQuery)
	if e != nil {
		return p, v, fault.Invalid("/query")
	}
	filter := url.Values{}
	for k, vs := range q {
		if len(vs) == 0 {
			return p, v, fault.Invalid("/" + k)
		}
		if k != "kind" && k != "state" && k != "model_id" && len(vs) != 1 {
			return p, v, fault.Invalid("/" + k)
		}
		if items && k != "cursor" && k != "limit" {
			return p, v, fault.Invalid("/" + k)
		}
		switch k {
		case "limit":
			n, e := strconv.Atoi(vs[0])
			if e != nil || n < 1 || n > 100 {
				return p, v, fault.Invalid("/limit")
			}
			p.Limit = n
		case "cursor":
			continue
		case "from", "to":
			t, e := time.Parse(time.RFC3339Nano, vs[0])
			if e != nil {
				return p, v, fault.Invalid("/" + k)
			}
			if k == "from" {
				p.From = &t
			} else {
				p.To = &t
			}
		case "kind", "state", "model_id":
			for _, s := range vs {
				if s == "" || len(s) > 255 || (k == "kind" && s != "image") || (k == "state" && !slices.Contains([]string{"queued", "running", "failed", "canceled", "unknown", "succeeded"}, s)) {
					return p, v, fault.Invalid("/" + k)
				}
			}
			sort.Strings(vs)
			switch k {
			case "kind":
				p.Kind = vs
			case "state":
				p.State = vs
			case "model_id":
				p.Model = vs
			}
		default:
			return p, v, fault.Invalid("/" + k)
		}
		filter[k] = vs
	}
	if p.From != nil && p.To != nil && !p.From.Before(*p.To) {
		return p, v, fault.Invalid("/to")
	}
	v.Filters = filter.Encode()
	if q.Has("cursor") {
		tk, ok := h.verify(q.Get("cursor"), v)
		if !ok || tk.After == "" {
			return p, v, fault.Invalid("/cursor")
		}
		if !time.Now().Before(tk.Expires) {
			return p, v, &fault.Fault{Status: 410, Reason: "SNAPSHOT_EXPIRED", Message: "The list cursor expired."}
		}
		p.AsOf, p.AfterTime, p.AfterID = tk.AsOf, tk.AfterTime, tk.After
	}
	v.AsOf = p.AsOf
	v.Expires = p.AsOf.Add(15 * time.Minute)
	return p, v, nil
}
func (h *Handler) list(r request) (any, error) {
	p, t, e := h.page(r, false)
	if e != nil {
		return nil, e
	}
	ds, e := h.Store.List(r.ctx, r.a, p)
	if e != nil {
		return nil, e
	}
	var next *string
	if len(ds) > p.Limit {
		ds = ds[:p.Limit]
		last := ds[len(ds)-1]
		t.After, t.AfterTime = last.ID, last.Created
		s, e := h.sign(t)
		if e != nil {
			return nil, e
		}
		next = &s
	}
	rows := make([]gin.H, 0, len(ds))
	for _, d := range ds {
		rows = append(rows, gin.H{"id": d.ID, "kind": d.Kind, "state": d.State, "stage": d.Stage, "model_id": d.Model, "created_at": d.Created, "updated_at": d.Updated, "progress": d.Progress, "settlement_state": d.Settlement, "allowed_actions": d.Actions})
	}
	return gin.H{"as_of": p.AsOf, "next_cursor": next, "items": rows}, nil
}
func (h *Handler) items(r request) (any, error) {
	p, t, e := h.page(r, true)
	if e != nil {
		return nil, e
	}
	rows, e := h.Store.Items(r.ctx, r.a, r.task, p)
	if e != nil {
		return nil, e
	}
	var next *string
	if len(rows) > p.Limit {
		rows = rows[:p.Limit]
		t.After = rows[len(rows)-1].ID
		s, e := h.sign(t)
		if e != nil {
			return nil, e
		}
		next = &s
	}
	return gin.H{"as_of": p.AsOf, "next_cursor": next, "items": rows}, nil
}
