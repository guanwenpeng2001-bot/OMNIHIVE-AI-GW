package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
)

type cursor struct {
	Actor, Workspace, Project, After int64
	Kind                             string
	AsOf                             time.Time
}

func (h *Handler) sign(data []byte) []byte {
	m := hmac.New(sha256.New, h.cursorKey)
	_, _ = m.Write([]byte("identity.cursor.v1\x00"))
	_, _ = m.Write(data)
	return m.Sum(nil)
}

func (h *Handler) next(r request, kind string, p domain.Page, last int64) string {
	b, _ := json.Marshal(cursor{r.actor, r.w, r.p, last, kind, p.AsOf})
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(h.sign(b))
}

func (h *Handler) page(r request, kind string) (domain.Page, error) {
	p := domain.Page{AsOf: time.Now().UTC(), Limit: 25}
	q := r.Request.URL.Query()
	for k, vs := range q {
		if (k != "limit" && k != "cursor") || len(vs) != 1 {
			return p, fault.Invalid("/" + k)
		}
	}
	if q.Has("limit") {
		n, err := strconv.Atoi(q.Get("limit"))
		if err != nil || n < 1 || n > 100 {
			return p, fault.Invalid("/limit")
		}
		p.Limit = n
	}
	if !q.Has("cursor") {
		return p, nil
	}
	data, tag, ok := strings.Cut(q.Get("cursor"), ".")
	b, e := base64.RawURLEncoding.DecodeString(data)
	mac, f := base64.RawURLEncoding.DecodeString(tag)
	var token cursor
	if !ok || len(b) > 1024 || e != nil || f != nil || !hmac.Equal(mac, h.sign(b)) || json.Unmarshal(b, &token) != nil || token.Actor != r.actor || token.Workspace != r.w || token.Project != r.p || token.Kind != kind || token.After <= 0 {
		return p, fault.Invalid("/cursor")
	}
	if time.Since(token.AsOf) > 15*time.Minute {
		return p, &fault.Fault{Status: 410, Reason: "SNAPSHOT_EXPIRED", Message: "List cursor has expired."}
	}
	p.After, p.AsOf = token.After, token.AsOf
	return p, nil
}

type list[T any] struct {
	Items []T       `json:"items"`
	Next  *string   `json:"next_cursor"`
	AsOf  time.Time `json:"as_of"`
}

func pageResult[T any](h *Handler, r request, kind string, p domain.Page, data []T, id func(T) int64) list[T] {
	result := list[T]{Items: data, AsOf: p.AsOf}
	if len(data) > p.Limit {
		result.Items = data[:p.Limit]
		token := h.next(r, kind, p, id(result.Items[p.Limit-1]))
		result.Next = &token
	}
	return result
}

func (h *Handler) projects(r request) (any, error) {
	p, err := h.page(r, "projects")
	if err != nil {
		return nil, err
	}
	rows, err := h.store.Projects(r.Request.Context(), r.actor, r.w, p)
	if err != nil {
		return nil, err
	}
	return pageResult(h, r, "projects", p, rows, func(x domain.Project) int64 { return x.ID }), nil
}

func (h *Handler) keys(r request) (any, error) {
	p, err := h.page(r, "keys")
	if err != nil {
		return nil, err
	}
	rows, err := h.store.Keys(r.Request.Context(), r.actor, r.w, r.p, p)
	if err != nil {
		return nil, err
	}
	return pageResult(h, r, "keys", p, rows, func(x domain.Key) int64 { return x.ID }), nil
}

func (h *Handler) workspaces(r request) (any, error) {
	p, err := h.page(r, "workspaces")
	if err != nil {
		return nil, err
	}
	rows, err := h.store.Workspaces(r.Request.Context(), r.actor, p)
	if err != nil {
		return nil, err
	}
	page := pageResult(h, r, "workspaces", p, rows, func(x domain.Workspace) int64 { return x.ID })
	items := []gin.H{}
	for _, w := range page.Items {
		inner := domain.Page{AsOf: p.AsOf, Limit: 25}
		projects, err := h.store.Projects(r.Request.Context(), r.actor, w.ID, inner)
		if err != nil {
			return nil, err
		}
		projectRequest := r
		projectRequest.w = w.ID
		part := pageResult(h, projectRequest, "projects", inner, projects, func(x domain.Project) int64 { return x.ID })
		summaries := []gin.H{}
		for _, x := range part.Items {
			summaries = append(summaries, gin.H{"id": strconv.FormatInt(x.ID, 10), "name": x.Name})
		}
		items = append(items, gin.H{"workspace_id": strconv.FormatInt(w.ID, 10), "name": w.Name, "projects": summaries, "projects_next_cursor": part.Next})
	}
	return list[gin.H]{Items: items, Next: page.Next, AsOf: page.AsOf}, nil
}
