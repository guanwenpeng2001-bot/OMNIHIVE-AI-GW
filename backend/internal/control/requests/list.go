package requests

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/requests"
)

const cursorLifetime = 15 * time.Minute

type listResult struct {
	Items []domain.Summary `json:"items"`
	Next  *string          `json:"next_cursor"`
	AsOf  time.Time        `json:"as_of"`
}

type cursor struct {
	Match [32]byte
	AsOf  time.Time
	After domain.Seek
}

func (h *Handler) list(r request) (any, error) {
	query, err := url.ParseQuery(r.Request.URL.RawQuery)
	if err != nil {
		return nil, fault.Invalid("/query")
	}
	page, err := parsePage(query)
	if err != nil {
		return nil, err
	}
	match, err := cursorMatch(r, page.Filter)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	page.AsOf = now
	if query.Has("cursor") {
		v, err := h.decodeCursor(query.Get("cursor"), match, now)
		if err != nil {
			return nil, err
		}
		page.AsOf, page.After = v.AsOf, v.After
	}
	items, err := h.store.List(r.Request.Context(), r.view, page)
	if err != nil {
		return nil, err
	}
	out := listResult{Items: items, AsOf: page.AsOf}
	if len(items) > page.Limit {
		out.Items = items[:page.Limit]
		last := out.Items[page.Limit-1]
		next, err := h.encodeCursor(cursor{Match: match, AsOf: page.AsOf, After: domain.Seek{StartedAt: last.StartedAt, ID: last.ID}})
		if err != nil {
			return nil, err
		}
		out.Next = &next
	}
	return out, nil
}

func parsePage(q url.Values) (domain.Page, error) {
	p := domain.Page{Limit: 25}
	for key, values := range q {
		path := "/" + key
		for _, v := range values {
			if v == "" || !utf8.ValidString(v) || strings.ContainsFunc(v, unicode.IsControl) {
				return p, fault.Invalid(path)
			}
		}
		var valuesTarget *[]string
		var allowed []string
		switch key {
		case "key_id":
			if len(values) > 20 {
				return p, fault.Invalid(path)
			}
			for _, v := range values {
				id := positiveID(v)
				if id == 0 {
					return p, fault.Invalid(path)
				}
				p.KeyIDs = append(p.KeyIDs, id)
			}
			slices.Sort(p.KeyIDs)
			p.KeyIDs = slices.Compact(p.KeyIDs)
			continue
		case "model_id":
			valuesTarget = &p.Models
		case "protocol":
			valuesTarget = &p.Protocols
			allowed = []string{"openai_chat", "openai_responses", "anthropic_messages", "gemini_generate",
				"gemini_count_tokens", "gemini_embedding", "openai_images", "openai_embeddings", "openai_audio", "openai_video"}
		case "outcome":
			valuesTarget = &p.Outcomes
			allowed = []string{"admitted", "running", "succeeded", "failed", "interrupted", "canceled", "unknown"}
		case "settlement_state":
			valuesTarget = &p.Settlement
			allowed = []string{"estimated", "reserved", "pending", "settled", "needs_review", "adjusted", "refunded", "unknown"}
		case "funding_source":
			valuesTarget = &p.FundingSources
			allowed = []string{"wallet", "subscription", "byok", "mixed", "unknown"}
		case "reason":
			valuesTarget = &p.Reasons
			allowed = []string{"STREAM_INTERRUPTED", "REQUEST_CANCELED", "ACCEPTANCE_UNKNOWN", "RECEIPT_UNCONFIRMED",
				"UPSTREAM_RATE_LIMITED", "UPSTREAM_ERROR", "REQUEST_REJECTED", "GATEWAY_ERROR"}
		}
		if valuesTarget != nil {
			if len(values) > 20 {
				return p, fault.Invalid(path)
			}
			for _, v := range values {
				if len(v) > 255 || (allowed != nil && !slices.Contains(allowed, v)) {
					return p, fault.Invalid(path)
				}
			}
			slices.Sort(values)
			*valuesTarget = slices.Compact(values)
			continue
		}
		if len(values) != 1 {
			return p, fault.Invalid(path)
		}
		v := values[0]
		switch key {
		case "cursor":
		case "limit":
			n := positiveID(v)
			if n == 0 || n > 100 {
				return p, fault.Invalid(path)
			}
			p.Limit = int(n)
		case "from", "to":
			at, err := time.Parse(time.RFC3339Nano, v)
			if err != nil || at.Year() < 1 {
				return p, fault.Invalid(path)
			}
			at = at.UTC()
			if key == "from" {
				p.From = &at
			} else {
				p.To = &at
			}
		case "timezone":
			if _, err := time.LoadLocation(v); err != nil || v == "Local" || len(v) > 80 {
				return p, fault.Invalid(path)
			}
			p.Timezone = v
		default:
			return p, fault.Invalid(path)
		}
	}
	if p.From != nil && p.To != nil && !p.From.Before(*p.To) {
		return p, fault.Invalid("/to")
	}
	return p, nil
}

func cursorMatch(r request, f domain.Filter) ([32]byte, error) {
	b, err := json.Marshal(struct {
		Actor   int64
		Version int64
		View    domain.Projection
		Filter  domain.Filter
	}{r.actor, r.version, r.view, f})
	return sha256.Sum256(b), err
}

func (h *Handler) sign(data []byte) []byte {
	mac := hmac.New(sha256.New, h.cursorKey[:])
	mac.Write(data)
	return mac.Sum(nil)
}

func (h *Handler) encodeCursor(v cursor) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(h.sign(b)), nil
}

func (h *Handler) decodeCursor(raw string, match [32]byte, now time.Time) (cursor, error) {
	var v cursor
	if len(raw) > 1024 {
		return v, fault.Invalid("/cursor")
	}
	body, tag, ok := strings.Cut(raw, ".")
	b, bodyErr := base64.RawURLEncoding.DecodeString(body)
	signature, tagErr := base64.RawURLEncoding.DecodeString(tag)
	if !ok || bodyErr != nil || tagErr != nil || !hmac.Equal(signature, h.sign(b)) || json.Unmarshal(b, &v) != nil ||
		v.Match != match || v.AsOf.IsZero() || v.AsOf.After(now) || v.After.StartedAt.IsZero() ||
		!v.After.StartedAt.Before(v.AsOf) || !validRequestID(v.After.ID) {
		return v, fault.Invalid("/cursor")
	}
	if now.Sub(v.AsOf) >= cursorLifetime {
		return v, snapshotExpired
	}
	return v, nil
}
