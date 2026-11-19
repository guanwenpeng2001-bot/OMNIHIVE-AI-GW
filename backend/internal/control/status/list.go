package status

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	status "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/status"
)

const cursorLifetime = 15 * time.Minute

type incidentCursor struct {
	AsOf    time.Time           `json:"at"`
	After   status.IncidentSeek `json:"after"`
	Expires time.Time           `json:"expires"`
	Match   [32]byte            `json:"match"`
}

type incidentPage struct {
	Items      []status.IncidentSummary `json:"items"`
	NextCursor *string                  `json:"next_cursor"`
	AsOf       time.Time                `json:"as_of"`
}

func (h *Handler) incidents(c *gin.Context) {
	q, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		writeFault(c, fault.Invalid("/query"))
		return
	}
	p, err := parsePage(q)
	if err != nil {
		writeFault(c, err)
		return
	}
	now := h.now().UTC()
	view := status.ViewTime(now)
	match := pageFingerprint(p)
	cursor := incidentCursor{Expires: view.Add(cursorLifetime), Match: match}
	if q.Has("cursor") {
		cursor, err = h.decodeCursor(q.Get("cursor"), match, now)
		if err != nil {
			writeFault(c, err)
			return
		}
		p.AsOf, p.After = cursor.AsOf, &cursor.After
	}
	items, asOf, err := h.store.ListIncidents(c.Request.Context(), p, view)
	if err != nil {
		writeFault(c, err)
		return
	}
	out := incidentPage{Items: items, AsOf: asOf}
	modified := asOf
	if len(items) > p.Limit {
		out.Items = items[:p.Limit]
		last := out.Items[len(out.Items)-1]
		cursor.AsOf = asOf
		cursor.After = status.IncidentSeek{StartedAt: last.StartedAt, ID: last.ID}
		next, err := h.encodeCursor(cursor)
		if err != nil {
			writeFault(c, err)
			return
		}
		out.NextCursor = &next
		if issued := cursor.Expires.Add(-cursorLifetime); issued.After(modified) {
			modified = issued
		}
	}
	writePublic(c, out, modified)
}

func parsePage(q url.Values) (status.IncidentPage, error) {
	p := status.IncidentPage{Limit: 25}
	for key, v := range q {
		path := "/" + key
		if len(v) != 1 || v[0] == "" {
			return p, fault.Invalid(path)
		}
		switch key {
		case "from", "to":
			at, err := time.Parse(time.RFC3339Nano, v[0])
			if err != nil || at.Year() < 1 {
				return p, fault.Invalid(path)
			}
			at = at.UTC()
			if key == "from" {
				p.From = &at
			} else {
				p.To = &at
			}
		case "limit":
			n, err := strconv.Atoi(v[0])
			if err != nil || n < 1 || n > 100 || strconv.Itoa(n) != v[0] {
				return p, fault.Invalid(path)
			}
			p.Limit = n
		case "cursor":
		default:
			return p, fault.Invalid("/query")
		}
	}
	if p.From != nil && p.To != nil && !p.From.Before(*p.To) {
		return p, fault.Invalid("/to")
	}
	return p, nil
}

func pageFingerprint(p status.IncidentPage) [32]byte {
	var from, to string
	if p.From != nil {
		from = p.From.Format(time.RFC3339Nano)
	}
	if p.To != nil {
		to = p.To.Format(time.RFC3339Nano)
	}
	return sha256.Sum256([]byte(from + "\n" + to + "\n" + strconv.Itoa(p.Limit)))
}

func (h *Handler) sign(raw []byte) []byte {
	mac := hmac.New(sha256.New, h.cursorKey[:])
	_, _ = mac.Write(raw)
	return mac.Sum(nil)
}

func (h *Handler) encodeCursor(v incidentCursor) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(h.sign(raw)), nil
}

var expired = &fault.Fault{Status: 410, Reason: "SNAPSHOT_EXPIRED", Message: "The incident list cursor has expired."}

func (h *Handler) decodeCursor(encoded string, match [32]byte, now time.Time) (incidentCursor, error) {
	var cursor incidentCursor
	if len(encoded) > 1024 {
		return cursor, fault.Invalid("/cursor")
	}
	raw, sig, found := strings.Cut(encoded, ".")
	data, bodyErr := base64.RawURLEncoding.DecodeString(raw)
	signature, sigErr := base64.RawURLEncoding.DecodeString(sig)
	if !found || bodyErr != nil || sigErr != nil || !hmac.Equal(h.sign(data), signature) ||
		json.Unmarshal(data, &cursor) != nil || cursor.Match != match ||
		cursor.AsOf.IsZero() || cursor.After.StartedAt.IsZero() || cursor.After.StartedAt.After(cursor.AsOf) ||
		!status.ValidIncidentID(cursor.After.ID) || cursor.Expires.IsZero() {
		return cursor, fault.Invalid("/cursor")
	}
	if !now.Before(cursor.Expires) {
		return cursor, expired
	}
	return cursor, nil
}
