package connections

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

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/lib/pq"
)

type page struct {
	After string
	AsOf  time.Time
	Scope string
}
type listing struct {
	Items []any     `json:"items"`
	Next  *string   `json:"next_cursor"`
	AsOf  time.Time `json:"as_of"`
}

func (r *Session) page(q url.Values, allowed []string) (page, int, error) {
	filters := url.Values{}
	limit := 25
	for k, vs := range q {
		if len(vs) != 1 {
			return page{}, 0, fault.Invalid("/" + k)
		}
		switch k {
		case "cursor":
		case "limit":
			n, e := strconv.Atoi(vs[0])
			if e != nil || n < 1 || n > 100 {
				return page{}, 0, fault.Invalid("/limit")
			}
			limit = n
		default:
			if !slices.Contains(allowed, k) {
				return page{}, 0, fault.Invalid("/" + k)
			}
			filters.Set(k, vs[0])
		}
	}
	scope := strconv.FormatInt(r.Actor, 10) + "/" + r.Route + "?" + filters.Encode()
	p := page{Scope: scope, AsOf: time.Now().UTC()}
	if !q.Has("cursor") {
		return p, limit, nil
	}
	payload, tag, ok := strings.Cut(q.Get("cursor"), ".")
	raw, e := base64.RawURLEncoding.DecodeString(payload)
	signature, bad := base64.RawURLEncoding.DecodeString(tag)
	if !ok || e != nil || bad != nil || len(raw) > 2048 || !hmac.Equal(signature, r.cursorMAC(raw)) || json.Unmarshal(raw, &p) != nil || p.Scope != scope {
		return p, 0, fault.Invalid("/cursor")
	}
	if time.Since(p.AsOf) > 15*time.Minute {
		return p, 0, &fault.Fault{Status: 410, Reason: "SNAPSHOT_EXPIRED", Message: "The list cursor expired."}
	}
	return p, limit, nil
}
func (r *Session) cursorMAC(data []byte) []byte {
	m := hmac.New(sha256.New, r.store.key)
	_, _ = m.Write([]byte("connections.page.v1\x00"))
	_, _ = m.Write(data)
	return m.Sum(nil)
}
func (r *Session) List(kind string, q url.Values) (any, error) {
	fields := map[string][]string{"schemas": {"provider", "auth_type"}, "connections": {"provider", "state", "pool_id"}}
	p, limit, e := r.page(q, fields[kind])
	if e != nil {
		return nil, e
	}
	out := listing{Items: []any{}, AsOf: p.AsOf}
	last := ""
	add := func(id string, v any) {
		if len(out.Items) == limit {
			p.After = last
			b, _ := json.Marshal(p)
			next := base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(r.cursorMAC(b))
			out.Next = &next
			return
		}
		out.Items = append(out.Items, v)
		last = id
	}
	if kind == "schemas" {
		for _, s := range Schemas(q.Get("provider"), q.Get("auth_type")) {
			for k, path := range s.Workflows {
				s.Workflows[k] = strings.ReplaceAll(path, "{workspace_id}", strconv.FormatInt(r.Workspace, 10))
			}
			if s.Provider > p.After {
				add(s.Provider, s)
				if out.Next != nil {
					break
				}
			}
		}
		return out, nil
	}
	after := int64(0)
	if p.After != "" {
		after, e = ID(p.After)
		if e != nil {
			return nil, fault.Invalid("/cursor")
		}
	}
	args := []any{r.Workspace, after, p.AsOf, limit + 1}
	query := ""
	switch kind {
	case "connections":
		if s := q.Get("state"); s != "" && !slices.Contains([]string{"draft", "active", "disabled"}, s) {
			return nil, fault.Invalid("/state")
		}
		pool := q.Get("pool_id")
		if pool != "" {
			if _, e = ID(pool); e != nil {
				return nil, fault.Invalid("/pool_id")
			}
		}
		query = selection + ` WHERE c.workspace_id=$1 AND c.account_id>$2 AND c.created_at<=$3 AND ($5='' OR c.provider=$5) AND ($6='' OR c.state=$6) AND ($7='' OR nullif($7,'')::bigint=ANY(c.pool_ids)) ORDER BY c.account_id LIMIT $4`
		args = append(args, q.Get("provider"), q.Get("state"), pool)
	case "events":
		if _, e = r.current(false); e != nil {
			return nil, e
		}
		query = `SELECT id,type,version,at,actor_id::text FROM connection_events WHERE workspace_id=$1 AND id>$2 AND at<=$3 AND account_id=$5 ORDER BY id LIMIT $4`
		args = append(args, r.ID)
	case "pools":
		query = `SELECT id,name,max_concurrency,version,ARRAY(SELECT m.account_id::text FROM connection_pool_members m JOIN accounts a ON a.id=m.account_id AND a.workspace_id=m.workspace_id AND a.deleted_at IS NULL WHERE m.pool_id=p.id AND m.workspace_id=p.workspace_id ORDER BY m.account_id) FROM connection_pools p WHERE workspace_id=$1 AND id>$2 AND created_at<=$3 ORDER BY id LIMIT $4`
	default:
		return nil, fault.NotFound
	}
	rows, e := r.tx.QueryContext(r.ctx, query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		switch kind {
		case "connections":
			c, err := scanConnection(rows)
			if err != nil {
				return nil, err
			}
			add(strconv.FormatInt(c.ID, 10), c.Project(r.access))
		case "events":
			var v Event
			if e = rows.Scan(&v.ID, &v.Type, &v.Version, &v.At, &v.Actor); e != nil {
				return nil, e
			}
			add(strconv.FormatInt(v.ID, 10), v)
		case "pools":
			var v Pool
			if e = rows.Scan(&v.ID, &v.Name, &v.Max, &v.Version, pq.Array(&v.Connections)); e != nil {
				return nil, e
			}
			if r.access.Require("connections.read") != nil {
				v.Connections = []string{}
			}
			add(strconv.FormatInt(v.ID, 10), v)
		}
	}
	return out, rows.Err()
}

type NewPool struct {
	Name        string   `json:"name"`
	Connections []string `json:"connection_ids"`
	Max         int      `json:"max_concurrency"`
}

func (r *Session) CreatePool(in NewPool) (any, error) {
	if !ValidText(in.Name, 100) || in.Max < 1 || in.Max > 10000 {
		return nil, fault.Invalid("/pool")
	}
	if len(in.Connections) > 0 {
		if !idsValid(in.Connections) {
			return nil, fault.Invalid("/connection_ids")
		}
		for _, action := range []string{"pools.assign", "connections.edit", "connections.activate"} {
			if e := r.require(action); e != nil {
				return nil, e
			}
		}
	}
	out := Pool{Name: in.Name, Max: in.Max, Version: 1, Connections: []string{}}
	e := r.tx.QueryRowContext(r.ctx, `INSERT INTO connection_pools(workspace_id,name,max_concurrency) VALUES($1,$2,$3) RETURNING id`, r.Workspace, in.Name, in.Max).Scan(&out.ID)
	if e != nil {
		return nil, e
	}
	ids := slices.Clone(in.Connections)
	slices.Sort(ids)
	for _, s := range ids {
		id, _ := ID(s)
		c, err := r.get(id, true)
		if err != nil {
			return nil, err
		}
		if c.State != "active" {
			return nil, conflict("CONNECTION_INACTIVE")
		}
		c.Pools = append(c.Pools, strconv.FormatInt(out.ID, 10))
		if e = r.save(c, "active", false); e != nil {
			return nil, e
		}
		_, e = r.tx.ExecContext(r.ctx, `INSERT INTO connection_pool_members(workspace_id,pool_id,account_id) VALUES($1,$2,$3)`, r.Workspace, out.ID, id)
		if e != nil {
			return nil, e
		}
		if e = r.event(id, "pool.assigned", c.Version+1); e != nil {
			return nil, e
		}
	}
	if r.access.Require("connections.read") == nil {
		out.Connections = append(out.Connections, in.Connections...)
	}
	return out, nil
}
