package audit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/audit"
)

type cursor struct {
	Match    [32]byte
	AsOf, At time.Time
	ID       int64
}

func (h *Handler) mac(b []byte) []byte {
	m := hmac.New(sha256.New, h.secret[:])
	_, _ = m.Write(b)
	return m.Sum(nil)
}
func (h *Handler) encode(v cursor) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(h.mac(b))
}
func (h *Handler) decode(raw string, want [32]byte, now time.Time) (cursor, error) {
	var v cursor
	if len(raw) > 1024 {
		return v, fault.Invalid("/cursor")
	}
	data, tag, ok := strings.Cut(raw, ".")
	b, err := base64.RawURLEncoding.DecodeString(data)
	sig, e := base64.RawURLEncoding.DecodeString(tag)
	if !ok || e != nil || err != nil || !hmac.Equal(sig, h.mac(b)) || json.Unmarshal(b, &v) != nil || v.Match != want || v.ID <= 0 || v.AsOf.After(now) || v.At.After(v.AsOf) {
		return v, fault.Invalid("/cursor")
	}
	if now.Sub(v.AsOf) >= 15*time.Minute {
		return v, &fault.Fault{Status: 410, Reason: "SNAPSHOT_EXPIRED", Message: "List cursor expired."}
	}
	return v, nil
}
func (h *Handler) list(c *gin.Context, k domain.Scope, _ int64) (any, error) {
	return h.page(c, k)
}

func (h *Handler) export(c *gin.Context, k domain.Scope, _ int64) (any, error) {
	if _, err := h.Store.Authorize(c.Request.Context(), k, domain.Export); err != nil {
		return nil, err
	}
	return h.page(c, k)
}

func (h *Handler) page(c *gin.Context, k domain.Scope) (any, error) {
	ctx := c.Request.Context()
	a, err := h.Store.Authorize(ctx, k, domain.Read)
	if err != nil {
		return nil, err
	}
	q, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return nil, fault.Invalid("/query")
	}
	p := domain.Page{Limit: 25, AsOf: time.Now().UTC().Truncate(time.Microsecond)}
	for key, values := range q {
		if len(values) != 1 || values[0] == "" {
			return nil, fault.Invalid("/query")
		}
		v := values[0]
		switch key {
		case "limit":
			p.Limit, err = strconv.Atoi(v)
			if err != nil || p.Limit < 1 || p.Limit > 100 {
				return nil, fault.Invalid("/limit")
			}
		case "resource":
			if !slices.Contains([]string{"request_export", "artifact", "billing_command", "retention", "audit_event", "project"}, v) {
				return nil, fault.Invalid("/resource")
			}
			p.Resource = v
		case "resource_id":
			if identifier(v) == 0 {
				id, err := uuid.Parse(v)
				if err != nil || id.String() != v {
					return nil, fault.Invalid("/resource_id")
				}
			}
			p.ResourceID = v
		case "action":
			p.Action = v
		case "from", "to":
			t, e := time.Parse(time.RFC3339Nano, v)
			if e != nil || t.Year() < 1 {
				return nil, fault.Invalid("/" + key)
			}
			t = t.UTC()
			if key == "from" {
				p.From = &t
			} else {
				p.To = &t
			}
		case "cursor":
		default:
			return nil, fault.Invalid("/query")
		}
	}
	if p.From != nil && p.To != nil && !p.From.Before(*p.To) {
		return nil, fault.Invalid("/to")
	}
	if p.Resource == "" || p.Action == "" {
		if !slices.Contains([]string{"", "request_export", "artifact", "billing_command", "retention", "audit_event", "project"}, p.Resource) {
			return nil, fault.Invalid("/resource")
		}
	}
	b, _ := json.Marshal(struct {
		Scope   domain.Scope
		Version int64
		Filter  domain.Filter
	}{k, a.Version, p.Filter})
	match := sha256.Sum256(b)
	if q.Has("cursor") {
		t, e := h.decode(q.Get("cursor"), match, p.AsOf)
		if e != nil {
			return nil, e
		}
		p.AsOf = t.AsOf
		p.AfterTime = &t.At
		p.AfterID = t.ID
	}
	items, err := h.Store.List(ctx, k, p)
	if err != nil {
		return nil, err
	}
	var next *string
	if len(items) > p.Limit {
		items = items[:p.Limit]
		last := items[len(items)-1]
		s := h.encode(cursor{match, p.AsOf, last.At, last.ID})
		next = &s
	}
	return gin.H{"items": items, "as_of": p.AsOf, "next_cursor": next}, nil
}
