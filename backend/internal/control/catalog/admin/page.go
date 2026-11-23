package admin

import (
	"cmp"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
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

const nestedLimit = 50

var snapshotExpired = &fault.Fault{Status: 410, Reason: "SNAPSHOT_EXPIRED", Message: "Refresh the model catalog and try again."}
var providerFilter = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

type cursor struct {
	Match, Version string
	AsOf           time.Time
	Offset         int
}

func (h *Handler) openCursor(raw, match, field string) (cursor, error) {
	if raw == "" {
		return cursor{}, nil
	}
	var out cursor
	if len(raw) > 1024 {
		return out, fault.Invalid("/" + field)
	}
	body, signature, ok := strings.Cut(raw, ".")
	wire := base64.RawURLEncoding.Strict()
	data, err := wire.DecodeString(body)
	sig, sigErr := wire.DecodeString(signature)
	if !ok || err != nil || sigErr != nil || !hmac.Equal(sig, h.sign(data)) ||
		json.Unmarshal(data, &out) != nil || out.Offset <= 0 || !catalogVersion.MatchString(out.Version) {
		return out, fault.Invalid("/" + field)
	}
	now := time.Now()
	if out.Match != match || out.AsOf.After(now) || now.Sub(out.AsOf) >= catalog.SnapshotTTL {
		return out, snapshotExpired
	}
	return out, nil
}

func (h *Handler) sign(data []byte) []byte {
	mac := hmac.New(sha256.New, h.cursorKey[:])
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}

func window[T any](h *Handler, rows []T, prior cursor, snapshot snapshotMeta, match string, limit int) ([]T, *string, error) {
	if prior.Offset > 0 && prior.Offset >= len(rows) {
		return nil, nil, snapshotExpired
	}
	rows = rows[prior.Offset:]
	if len(rows) <= limit {
		return rows, nil, nil
	}
	data, err := json.Marshal(cursor{Match: match, Version: snapshot.Version, AsOf: snapshot.AsOf, Offset: prior.Offset + limit})
	if err != nil {
		return nil, nil, err
	}
	wire := base64.RawURLEncoding
	next := wire.EncodeToString(data) + "." + wire.EncodeToString(h.sign(data))
	return rows[:limit], &next, nil
}

type listFilter struct {
	Provider, Model, Search, Source, Publication, Availability, Billing, Quick string
}

func parseList(q url.Values) (listFilter, int, error) {
	f, limit := listFilter{}, 50
	for key, values := range q {
		v := values[0]
		switch key {
		case "workspace_id", "project_id", "cursor":
		case "provider":
			if !providerFilter.MatchString(v) {
				return f, 0, fault.Invalid("/provider")
			}
			f.Provider = strings.ToLower(v)
		case "model":
			if len(v) > 256 || strings.TrimSpace(v) != v {
				return f, 0, fault.Invalid("/model")
			}
			f.Model = v
		case "q":
			f.Search = strings.ToLower(strings.TrimSpace(v))
			if len(v) > 200 || f.Search == "" {
				return f, 0, fault.Invalid("/q")
			}
		case "source":
			if !sourceKind(v) {
				return f, 0, fault.Invalid("/source")
			}
			f.Source = v
		case "publication":
			if !slices.Contains([]string{"published", "unpublished", "unknown"}, v) {
				return f, 0, fault.Invalid("/publication")
			}
			f.Publication = v
		case "availability":
			if !slices.Contains([]string{"live", "unavailable", "requires_scope", "unknown"}, v) {
				return f, 0, fault.Invalid("/availability")
			}
			f.Availability = v
		case "billing":
			if !billingState(v) {
				return f, 0, fault.Invalid("/billing")
			}
			f.Billing = v
		case "quick_filter":
			if !slices.Contains([]string{"routable_but_unpriced", "discovered_not_enabled", "listed_unrouted", "group_price_conflict", "pi_only", "stale"}, v) {
				return f, 0, fault.Invalid("/quick_filter")
			}
			f.Quick = v
		case "limit":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 100 || strconv.Itoa(n) != v {
				return f, 0, fault.Invalid("/limit")
			}
			limit = n
		default:
			return f, 0, fault.Invalid("/" + key)
		}
	}
	return f, limit, nil
}

func (f listFilter) matches(d catalog.Detail, counts bindingSummary) bool {
	m := d.Model
	source, availability, billing := projectAxes(m, counts)
	if f.Provider != "" && !strings.EqualFold(f.Provider, m.Provider) ||
		f.Model != "" && f.Model != m.Model ||
		f.Source != "" && !slices.Contains(source.Kinds, f.Source) ||
		f.Publication != "" && f.Publication != availability.Publication ||
		f.Availability != "" && f.Availability != availability.State ||
		f.Billing != "" && f.Billing != billing.State {
		return false
	}
	if f.Search != "" &&
		!catalog.MatchesText(m.Provider+"/"+m.Model, f.Search) &&
		!catalog.MatchesText(m.DisplayName, f.Search) && !catalog.MatchesText(m.ID, f.Search) &&
		!catalog.MatchesText(m.CanonicalID, f.Search) &&
		!slices.ContainsFunc(d.Aliases, func(alias string) bool { return catalog.MatchesText(alias, f.Search) }) {
		return false
	}
	switch f.Quick {
	case "routable_but_unpriced":
		return availability.State == "live" && billing.State == "unpriced"
	case "discovered_not_enabled":
		return counts.hasUnboundCurrent
	case "listed_unrouted":
		return availability.Publication == "published" && availability.State == "unavailable"
	case "group_price_conflict":
		return billing.State == "conflict"
	case "pi_only":
		return len(source.Kinds) == 1 && source.Kinds[0] == catalog.PiAIReference
	case "stale":
		return counts.hasStale || slices.ContainsFunc(d.Provenance, func(p catalog.Provenance) bool { return p.State == "stale" })
	default:
		return true
	}
}

func (h *Handler) list(c *gin.Context, p catalog.Perspective, q url.Values) (any, error) {
	filter, limit, err := parseList(q)
	if err != nil {
		return nil, err
	}
	match := catalog.Digest(struct {
		Perspective catalog.Perspective
		Filter      listFilter
	}{p, filter})
	prior, err := h.openCursor(q.Get("cursor"), match, "cursor")
	if err != nil {
		return nil, err
	}
	snapshot, err := h.readSnapshot(c, p, prior)
	if err != nil {
		return nil, err
	}
	type row struct {
		detail   catalog.Detail
		bindings bindingSummary
	}
	rows := make([]row, 0, len(snapshot.Details))
	for _, d := range snapshot.Details {
		bindings := summarizeBindings(d.Offers)
		if filter.matches(d, bindings) {
			rows = append(rows, row{d, bindings})
		}
	}
	slices.SortFunc(rows, func(a, b row) int {
		x, y := a.detail.Model, b.detail.Model
		return cmp.Or(cmp.Compare(x.Provider, y.Provider), cmp.Compare(x.Model, y.Model), cmp.Compare(x.ID, y.ID))
	})
	meta := newSnapshotMeta(snapshot, p)
	rows, next, err := window(h, rows, prior, meta, match, limit)
	if err != nil {
		return nil, err
	}
	items := make([]modelSummary, 0, len(rows))
	for _, row := range rows {
		items = append(items, projectSummary(row.detail, row.bindings))
	}
	return listResponse{snapshotMeta: meta, Items: items, Next: next}, nil
}

func (h *Handler) readSnapshot(c *gin.Context, p catalog.Perspective, prior cursor) (catalog.Snapshot, error) {
	snapshot, err := h.reader.Snapshot(c.Request.Context(), p, prior.AsOf)
	if err != nil {
		return catalog.Snapshot{}, dependencyFault(err)
	}
	if prior.Version != "" && prior.Version != snapshot.Version {
		return catalog.Snapshot{}, snapshotExpired
	}
	return snapshot, nil
}
