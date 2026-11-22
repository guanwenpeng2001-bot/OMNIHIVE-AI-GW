package models

import (
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
)

type filter struct {
	Text, Sort, Access, Route string
	Availability              string `json:",omitempty"`
	Values                    map[string][]string
}

type page struct {
	Items   []catalog.Summary `json:"items"`
	Next    *string           `json:"next_cursor"`
	AsOf    time.Time         `json:"as_of"`
	Version string            `json:"catalog_version"`
}

type cursor struct {
	Match, Version, After string
	AsOf                  time.Time
}

var facetValue = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)

func filters(q url.Values, p catalog.Perspective) (filter, int, error) {
	f, limit := filter{Sort: "name", Values: map[string][]string{}}, 25
	for k, values := range q {
		if k == "availability_state" && (len(values) != 1 || (values[0] != "live" && values[0] != "catalog_only")) {
			err := fault.Invalid("/availability_state")
			err.Status = http.StatusBadRequest
			return f, 0, err
		}
		if len(values) == 0 || len(values) > 20 {
			return f, 0, fault.Invalid("/" + k)
		}
		switch k {
		case "provider", "protocol", "action", "modality", "region", "retention", "stability":
			for _, value := range values {
				if !facetValue.MatchString(value) {
					return f, 0, fault.Invalid("/" + k)
				}
				f.Values[k] = append(f.Values[k], strings.ToLower(value))
			}
			slices.Sort(f.Values[k])
			f.Values[k] = slices.Compact(f.Values[k])
			continue
		}
		if len(values) != 1 || values[0] == "" {
			return f, 0, fault.Invalid("/" + k)
		}
		v := values[0]
		switch k {
		case "q":
			if len(v) > 200 {
				return f, 0, fault.Invalid("/" + k)
			}
			f.Text = strings.ToLower(strings.TrimSpace(v))
		case "sort":
			if !slices.Contains([]string{"name", "price_input", "context", "ttft", "availability"}, v) {
				return f, 0, fault.Invalid("/sort")
			}
			f.Sort = v
		case "availability_state":
			f.Availability = v
		case "limit":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 100 || strconv.Itoa(n) != v {
				return f, 0, fault.Invalid("/limit")
			}
			limit = n
		case "accessible", "routable":
			if p.Public() || (v != "true" && v != "false") {
				return f, 0, fault.Invalid("/" + k)
			}
			if k == "accessible" {
				f.Access = v
			} else {
				f.Route = v
			}
		case "cursor":
		default:
			return f, 0, fault.Invalid("/" + k)
		}
	}
	return f, limit, nil
}

func (h *Handler) list(ctx context.Context, p catalog.Perspective, c *gin.Context) (any, error) {
	q, err := parseQuery(c)
	if err != nil {
		return nil, err
	}
	f, limit, err := filters(q, p)
	if err != nil {
		return nil, err
	}
	match := catalog.Digest(struct {
		Perspective catalog.Perspective
		Filter      filter
	}{p, f})
	var continuation cursor
	if token := q.Get("cursor"); token != "" {
		continuation, err = h.openCursor(token)
		if err != nil {
			return nil, err
		}
		if continuation.Match != match || time.Since(continuation.AsOf) > catalog.SnapshotTTL || continuation.AsOf.After(time.Now()) {
			return nil, snapshotExpired
		}
	}
	snapshot, err := h.reader.Snapshot(ctx, p, continuation.AsOf)
	if err != nil {
		return nil, err
	}
	if continuation.Version != "" && continuation.Version != snapshot.Version {
		return nil, snapshotExpired
	}
	items := slices.DeleteFunc(slices.Clone(snapshot.Details), func(item catalog.Detail) bool { return !f.matches(item) })
	slices.SortFunc(items, func(a, b catalog.Detail) int { return compareModels(a, b, f.Sort) })
	if continuation.After != "" {
		start := slices.IndexFunc(items, func(item catalog.Detail) bool { return item.Model.ID == continuation.After })
		if start < 0 {
			return nil, snapshotExpired
		}
		items = items[start+1:]
	}
	out := page{Items: []catalog.Summary{}, AsOf: snapshot.AsOf, Version: snapshot.Version}
	if len(items) > limit {
		next := h.sealCursor(cursor{Match: match, Version: snapshot.Version, After: items[limit-1].Model.ID, AsOf: snapshot.AsOf})
		out.Next = &next
		items = items[:limit]
	}
	for _, item := range items {
		out.Items = append(out.Items, item.Model)
	}
	return out, nil
}

func (f filter) matches(d catalog.Detail) bool {
	m := d.Model
	if f.Availability != "" && m.AvailabilityState != f.Availability {
		return false
	}
	if !catalog.MatchesText(m.Model, f.Text) && !catalog.MatchesText(m.DisplayName, f.Text) && !catalog.MatchesText(m.Provider, f.Text) &&
		!slices.ContainsFunc(d.Aliases, func(alias string) bool { return catalog.MatchesText(alias, f.Text) }) {
		return false
	}
	if f.Access == "true" && m.AccessState != "allowed" || f.Access == "false" && m.AccessState != "denied" ||
		f.Route == "true" && m.RouteState != "eligible" || f.Route == "false" && m.RouteState != "ineligible" {
		return false
	}
	// Action-only browsing includes descriptive registry tasks even when no
	// gateway protocol implements them. A protocol filter still requires the
	// action/protocol pair on the same non-stale capability.
	if len(f.Values["protocol"]) > 0 {
		if !slices.ContainsFunc(d.Capabilities, func(c catalog.Capability) bool {
			return c.State != "stale" && f.any("action", []string{c.Action}) && f.any("protocol", []string{c.Protocol})
		}) {
			return false
		}
	} else if !f.any("action", m.Actions) {
		return false
	}
	if (len(f.Values["region"]) > 0 || len(f.Values["retention"]) > 0) &&
		!slices.ContainsFunc(d.Endpoints, func(e catalog.Endpoint) bool {
			return f.any("region", e.Regions) && f.any("retention", []string{e.Retention})
		}) {
		return false
	}
	return f.any("provider", []string{m.Provider}) && f.modality(m) && f.any("stability", []string{m.Maturity})
}

func (f filter) any(facet string, values []string) bool {
	return len(f.Values[facet]) == 0 || slices.ContainsFunc(values, func(v string) bool {
		return slices.Contains(f.Values[facet], strings.ToLower(v))
	})
}

func compareModels(a, b catalog.Detail, sort string) int {
	x, y := a.Model, b.Model
	var order int
	switch sort {
	case "availability":
		if x.AvailabilityState != y.AvailabilityState {
			if x.AvailabilityState == "live" {
				return -1
			}
			if y.AvailabilityState == "live" {
				return 1
			}
		}
		order = cmp.Compare(strings.ToLower(x.DisplayName), strings.ToLower(y.DisplayName))
	case "name":
		order = cmp.Compare(strings.ToLower(x.DisplayName), strings.ToLower(y.DisplayName))
	case "context":
		order = compareOptional(x.Context, y.Context, func(x, y int64) int { return cmp.Compare(y, x) })
	case "ttft":
		order = compareOptional(a.Metrics.TTFT, b.Metrics.TTFT, cmp.Compare[int64])
	case "price_input":
		// All legacy input rates are USD per one million tokens.
		if x.Input != nil && y.Input != nil {
			order = x.Input.Amount().Compare(y.Input.Amount())
		} else if x.Input != nil {
			order = -1
		} else if y.Input != nil {
			order = 1
		}
	}
	if order == 0 {
		return cmp.Compare(x.ID, y.ID)
	}
	return order
}

func compareOptional[T any](a, b *T, compare func(T, T) int) int {
	switch {
	case a != nil && b != nil:
		return compare(*a, *b)
	case a != nil:
		return -1
	case b != nil:
		return 1
	default:
		return 0
	}
}

func (h *Handler) sealCursor(c cursor) string {
	payload, _ := json.Marshal(c)
	mac := h.signCursor(payload)
	return base64.RawURLEncoding.EncodeToString(append(payload, mac...))
}

func (h *Handler) openCursor(token string) (cursor, error) {
	var c cursor
	if len(token) > 1024 {
		return c, fault.Invalid("/cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(data) <= sha256.Size {
		return c, fault.Invalid("/cursor")
	}
	payload, mac := data[:len(data)-sha256.Size], data[len(data)-sha256.Size:]
	if !hmac.Equal(h.signCursor(payload), mac) || json.Unmarshal(payload, &c) != nil {
		return c, fault.Invalid("/cursor")
	}
	return c, nil
}

func (h *Handler) signCursor(payload []byte) []byte {
	mac := hmac.New(sha256.New, h.cursorKey[:])
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

// Modality families include descriptive actions, even without a live route.
func (f filter) modality(m catalog.Summary) bool {
	wanted := f.Values["modality"]
	if len(wanted) == 0 {
		return true
	}
	for _, raw := range m.Modalities {
		token := strings.ToLower(strings.TrimSpace(raw))
		if slices.Contains(wanted, token) || (token == "speech" && slices.Contains(wanted, "audio")) ||
			(token == "embeddings" && slices.Contains(wanted, "embedding")) {
			return true
		}
	}
	for _, raw := range m.Actions {
		action := strings.ToLower(strings.TrimSpace(raw))
		for _, key := range wanted {
			switch key {
			case "text":
				if action == "chat" || action == "text.chat" || action == "text.completion" {
					return true
				}
			case "image", "video":
				if strings.HasPrefix(action, key) || strings.Contains(action, key+"_generation") {
					return true
				}
			case "embedding", "realtime":
				if strings.Contains(action, key) {
					return true
				}
			case "audio":
				if strings.HasPrefix(action, "audio") || strings.Contains(action, "speech") || strings.Contains(action, "tts") || strings.Contains(action, "stt") {
					return true
				}
			}
		}
	}
	return false
}
