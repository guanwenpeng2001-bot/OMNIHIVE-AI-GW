package connections

import (
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	legacy "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/BE-04"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/credentials"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
)

type Verify struct {
	Checks            []string `json:"checks"`
	CredentialVersion int64    `json:"credential_version,string"`
}
type Rotate struct {
	Expected    int64              `json:"expected_credential_version,string"`
	Replacement credentials.Change `json:"replacement"`
}
type Activate struct {
	Preview      string `json:"preview_id"`
	Verification string `json:"verification_id"`
	Expected     int64  `json:"expected_credential_version,string"`
}
type BulkPreview struct {
	IDs   []string `json:"ids"`
	Patch Patch    `json:"patch"`
}
type BulkApply struct {
	Preview string   `json:"preview_id"`
	IDs     []string `json:"selected_ids"`
}

func (r *Session) Verify(in Verify) (any, error) { return r.verify(in, nil) }
func (r *Session) Rotate(in Rotate) (any, error) {
	if in.Replacement.Operation != "" {
		return nil, fault.Invalid("/replacement/operation")
	}
	in.Replacement.Operation = "set"
	return r.verify(Verify{[]string{"network", "authorization", "discover_models"}, in.Expected}, &in.Replacement)
}
func (r *Session) verify(in Verify, replacement *credentials.Change) (any, error) {
	c, e := r.current(false)
	if e != nil {
		return nil, e
	}
	if c.Credential.Version != in.CredentialVersion {
		return nil, conflict("CREDENTIAL_CHANGED")
	}
	if c.Credential.State == "unset" || c.Credential.State == "revoked" {
		return nil, conflict("CREDENTIAL_UNAVAILABLE")
	}
	if !slices.Contains(in.Checks, "authorization") || !slices.Contains(in.Checks, "network") || len(in.Checks) > 3 {
		return nil, fault.Invalid("/checks")
	}
	for i, s := range in.Checks {
		if (s != "network" && s != "authorization" && s != "discover_models") || slices.Contains(in.Checks[:i], s) {
			return nil, fault.Invalid("/checks")
		}
	}
	var secret map[string]any
	action := "connections.verify"
	if replacement != nil {
		action = "credentials.rotate"
		secret, e = replacement.Payload(c.AuthType)
	} else {
		secret, e = credentials.Resolve(r.ctx, r.tx, r.store.enc, r.Workspace, c.ID, in.CredentialVersion)
	}
	if e != nil {
		return nil, e
	}
	proxy, rev, e := legacy.Proxy(r.ctx, r.tx, r.Workspace, c.NetworkPolicyID, false)
	if e != nil {
		return nil, e
	}
	fields, e := legacy.NativeIdentityFields(r.ctx, r.tx, r.Workspace, c.ID)
	if e != nil {
		return nil, e
	}
	maps.Copy(fields, secret)
	if c.Endpoint != "" {
		fields["base_url"] = c.Endpoint
	}
	native := service.Account{ID: c.ID, WorkspaceID: c.Workspace, Platform: c.Provider, Type: legacy.AccountType(c.AuthType), Credentials: fields}
	phases := []map[string]any{}
	record := func(phase string, version int64) error {
		now := time.Now().UTC()
		_, err := r.tx.ExecContext(r.ctx, "INSERT INTO connection_events(workspace_id,account_id,actor_id,type,version,at) VALUES($1,$2,$3,$4,$5,$6)", r.Workspace, c.ID, r.Actor, "rotation."+phase, version, now)
		phases = append(phases, map[string]any{"phase": phase, "at": now})
		return err
	}
	if replacement != nil {
		if e = record("validating", c.Credential.Version); e != nil {
			return nil, e
		}
	}
	result := r.store.probe.ProbeConnection(r.ctx, &native, proxy)
	latest, e := r.current(true)
	if e != nil {
		return nil, e
	}
	if latest.Version != c.Version || latest.Credential.Version != c.Credential.Version {
		return nil, conflict("STALE_VERIFICATION")
	}
	_, nowRev, e := legacy.Proxy(r.ctx, r.tx, r.Workspace, c.NetworkPolicyID, true)
	if e != nil {
		return nil, e
	}
	if nowRev != rev {
		return nil, conflict("STALE_VERIFICATION")
	}
	// The call to the provider can outlive a grant. Re-evaluate before switching.
	if e = r.fenceAccess(); e != nil {
		return nil, e
	}
	now := time.Now().UTC()
	v := Verification{ID: uuid.NewString(), Models: []string{}, Checks: []Check{}, VerifiedAt: now, Expires: now.Add(24 * time.Hour)}
	eligible := result.Network == "passed" && result.Authorization == "passed"
	for _, s := range in.Checks {
		state := map[string]string{"network": result.Network, "authorization": result.Authorization, "discover_models": result.Discovery}[s]
		check := Check{Name: s, State: state}
		if state != "passed" {
			reason := map[string]string{"failed": "PROBE_FAILED", "unknown": "EVIDENCE_UNAVAILABLE", "unsupported": "CHECK_UNSUPPORTED"}[state]
			check.Reason = &reason
		}
		v.Checks = append(v.Checks, check)
		if s == "discover_models" {
			v.Models = result.Models
		}
	}
	if replacement != nil && eligible && c.State == "active" && len(difference(c.Models, result.Models)) > 0 {
		eligible = false
		reason := "ACTIVE_MODEL_MISSING"
		for i := range v.Checks {
			if v.Checks[i].Name == "discover_models" {
				v.Checks[i].Reason = &reason
				v.Checks[i].State = "failed"
			}
		}
	}
	opState := "succeeded"
	if !eligible {
		opState = "failed"
	}
	if replacement != nil && eligible {
		if e = record("switching", c.Credential.Version); e != nil {
			return nil, e
		}
		if e = legacy.ChangeSecret(r.ctx, r.tx, r.store.enc, r.Workspace, c.ID, c.Credential.Version, secret, false); e != nil {
			return nil, e
		}
		if e = record("draining", c.Credential.Version); e != nil {
			return nil, e
		}
		c, e = r.get(c.ID, false)
		if e != nil {
			return nil, e
		}
		if e = r.event(c.ID, "credential.rotated", c.Credential.Version); e != nil {
			return nil, e
		}
	}
	data, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	if eligible || replacement == nil {
		_, e = r.tx.ExecContext(r.ctx, `INSERT INTO connection_verifications(id,workspace_id,account_id,base_version,credential_version,proxy_revision,eligible,snapshot,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, v.ID, r.Workspace, c.ID, c.Version, c.Credential.Version, rev, eligible, string(data), v.Expires)
		if e != nil {
			return nil, e
		}
		_, e = r.tx.ExecContext(r.ctx, `UPDATE provider_connections SET verified_id=CASE WHEN $3 THEN $4::uuid END WHERE workspace_id=$1 AND account_id=$2`, r.Workspace, c.ID, eligible, v.ID)
		if e != nil {
			return nil, e
		}
	}
	if e = r.event(c.ID, "verification."+opState, c.Credential.Version); e != nil {
		return nil, e
	}
	if replacement != nil {
		phase := "failed"
		if eligible {
			phase = "revoked"
		}
		if e = record(phase, c.Credential.Version); e != nil {
			return nil, e
		}
		return r.operation(c.ID, action, opState, map[string]any{"phase": phase, "phases": phases, "credential_version": strconv.FormatInt(c.Credential.Version, 10), "checks": v.Checks})
	}
	return r.operation(c.ID, action, opState, v)
}
func idsValid(ids []string) bool {
	if len(ids) < 1 || len(ids) > 100 {
		return false
	}
	for i, s := range ids {
		if _, e := ID(s); e != nil || slices.Contains(ids[:i], s) {
			return false
		}
	}
	return true
}
func (r *Session) fresh(c Connection, verification string) error {
	if c.Verification == nil || verification != c.Verification.ID || !c.Verification.Expires.After(time.Now()) {
		return conflict("STALE_VERIFICATION")
	}
	_, rev, e := legacy.Proxy(r.ctx, r.tx, r.Workspace, c.NetworkPolicyID, true)
	if e != nil {
		return e
	}
	var ok bool
	e = r.tx.QueryRowContext(r.ctx, `SELECT EXISTS(SELECT 1 FROM connection_verifications WHERE id=$1 AND workspace_id=$2 AND account_id=$3 AND eligible AND credential_version=$4 AND proxy_revision=$5 AND expires_at>now())`, verification, r.Workspace, c.ID, c.Credential.Version, rev).Scan(&ok)
	if e != nil {
		return e
	}
	if !ok {
		return conflict("STALE_VERIFICATION")
	}
	return nil
}
func (r *Session) applicable(c Connection, p Patch) error {
	if e := r.checkPatch(c, p); e != nil {
		return e
	}
	if c.Verification == nil {
		return conflict("VERIFICATION_REQUIRED")
	}
	if e := r.fresh(c, c.Verification.ID); e != nil {
		return e
	}
	ids := p.Models
	if ids == nil {
		ids = c.Models
	}
	for _, model := range ids {
		if !slices.Contains(c.Verification.Models, model) {
			return conflict("INCOMPATIBLE_PROVIDER")
		}
	}
	return nil
}
func difference(a, b []string) []string {
	out := []string{}
	for _, s := range a {
		if !slices.Contains(b, s) {
			out = append(out, s)
		}
	}
	return out
}
func (r *Session) Preview(ids []string, p Patch) (any, error) {
	if e := validatePatch(p); e != nil {
		return nil, e
	}
	if p.Name != nil || p.Endpoint != nil || p.NetworkPolicyID != nil {
		return nil, fault.Invalid("/patch")
	}
	one := r.ID != 0
	if one {
		ids = []string{strconv.FormatInt(r.ID, 10)}
	} else if !idsValid(ids) {
		return nil, fault.Invalid("/ids")
	}
	out := Preview{ID: uuid.NewString(), Items: []Item{}, Adds: []string{}, Removes: []string{}, Blocked: []string{}, Expires: time.Now().UTC().Add(5 * time.Minute)}
	for _, s := range ids {
		id, _ := ID(s)
		c, e := r.get(id, false)
		if e != nil {
			return nil, e
		}
		if r.Version != 0 && r.Version != c.Version {
			return nil, fault.Stale(c.Version)
		}
		item := Item{ID: s, Base: c.Version, State: "applicable"}
		if c.Verification != nil {
			item.Verification = c.Verification.ID
		}
		e = r.applicable(c, p)
		var f *fault.Fault
		if e != nil {
			if !errors.As(e, &f) || f.Status != 409 {
				return nil, e
			}
			item.State = "blocked"
			item.Reason = f.Reason
		}
		if !one && c.State != "active" {
			item.State = "blocked"
			item.Reason = "CONNECTION_INACTIVE"
		}
		if one {
			out.Base = c.Version
			if p.Pools != nil {
				out.Adds = difference(p.Pools, c.Pools)
				out.Removes = difference(c.Pools, p.Pools)
			}
			if item.Reason != "" {
				out.Blocked = append(out.Blocked, item.Reason)
			}
		}
		out.Items = append(out.Items, item)
	}
	patch, e := json.Marshal(p.Snapshot())
	if e != nil {
		return nil, e
	}
	items, e := json.Marshal(out.Items)
	if e != nil {
		return nil, e
	}
	_, e = r.tx.ExecContext(r.ctx, `INSERT INTO connection_previews(id,workspace_id,actor_id,account_id,patch,items,expires_at) VALUES($1,$2,$3,nullif($4::bigint,0),$5,$6,$7)`, out.ID, r.Workspace, r.Actor, r.ID, string(patch), string(items), out.Expires)
	if e == nil && r.access.Require("connections.read") != nil {
		out.Items = []Item{}
		out.Adds = []string{}
		out.Removes = []string{}
	}
	return out, e
}
func (r *Session) loadPreview(id string) (Patch, []Item, error) {
	var p Patch
	var items []Item
	if _, e := uuid.Parse(id); e != nil {
		return p, nil, fault.Invalid("/preview_id")
	}
	var data, rows []byte
	e := r.tx.QueryRowContext(r.ctx, `SELECT patch,items FROM connection_previews WHERE id=$1 AND workspace_id=$2 AND actor_id=$3 AND coalesce(account_id,0)=$4 AND expires_at>now()`, id, r.Workspace, r.Actor, r.ID).Scan(&data, &rows)
	if e == sql.ErrNoRows {
		return p, nil, conflict("STALE_PREVIEW")
	}
	if e != nil {
		return p, nil, e
	}
	if e = json.Unmarshal(data, &p); e != nil {
		return p, nil, e
	}
	e = json.Unmarshal(rows, &items)
	return p, items, e
}
func (r *Session) apply(c Connection, p Patch) error {
	if e := r.applicable(c, p); e != nil {
		return e
	}
	applyPatch(&c, p)
	var published []string
	if e := r.tx.QueryRowContext(r.ctx, "SELECT ARRAY(SELECT pool_id::text FROM connection_pool_members WHERE workspace_id=$1 AND account_id=$2)", r.Workspace, c.ID).Scan(pq.Array(&published)); e != nil {
		return e
	}
	if len(difference(c.Pools, published))+len(difference(published, c.Pools)) > 0 {
		if e := r.checkPools(c.Pools); e != nil {
			return e
		}
	}
	if e := legacy.Activate(r.ctx, r.tx, r.Workspace, c.ID, c.Name, c.Endpoint, c.NetworkPolicyID, c.Limits.Concurrency, c.Models); e != nil {
		return e
	}
	if e := r.members(c.ID, c.Pools); e != nil {
		return e
	}
	if e := r.save(c, "active", false); e != nil {
		return e
	}
	return r.event(c.ID, "connection.activated", c.Version+1)
}
func (r *Session) members(id int64, ids []string) error {
	_, e := r.tx.ExecContext(r.ctx, `WITH removed AS (DELETE FROM connection_pool_members WHERE workspace_id=$1 AND account_id=$2 AND NOT(pool_id=ANY($3::bigint[])) RETURNING pool_id),
 added AS (INSERT INTO connection_pool_members(workspace_id,account_id,pool_id) SELECT $1,$2,unnest($3::bigint[]) ON CONFLICT DO NOTHING RETURNING pool_id)
 UPDATE connection_pools SET version=version+1 WHERE workspace_id=$1 AND id IN (SELECT pool_id FROM removed UNION SELECT pool_id FROM added)`, r.Workspace, id, pq.Array(ids))
	return e
}
func (r *Session) Activate(in Activate) (any, error) {
	p, items, e := r.loadPreview(in.Preview)
	if e != nil {
		return nil, e
	}
	c, e := r.current(true)
	if e != nil {
		return nil, e
	}
	if len(items) != 1 || items[0].Base != c.Version || items[0].State != "applicable" || items[0].Verification != in.Verification {
		return nil, conflict("STALE_PREVIEW")
	}
	if in.Expected != c.Credential.Version {
		return nil, conflict("CREDENTIAL_CHANGED")
	}
	if e = r.fresh(c, in.Verification); e != nil {
		return nil, e
	}
	if e = r.apply(c, p); e != nil {
		return nil, e
	}
	r.Version = 0
	return r.Get()
}
func (r *Session) BulkApply(in BulkApply) (any, error) {
	if !idsValid(in.IDs) {
		return nil, fault.Invalid("/selected_ids")
	}
	p, items, e := r.loadPreview(in.Preview)
	if e != nil {
		return nil, e
	}
	index := map[string]Item{}
	for _, item := range items {
		index[item.ID] = item
	}
	for _, s := range in.IDs {
		if _, ok := index[s]; !ok {
			return nil, fault.Invalid("/selected_ids")
		}
	}
	if p.Pools != nil {
		if e = r.require("pools.assign"); e != nil {
			return nil, e
		}
	}
	sorted := slices.Clone(in.IDs)
	slices.Sort(sorted)
	for _, s := range sorted {
		item := index[s]
		if item.State == "applicable" {
			id, _ := ID(s)
			c, err := r.get(id, true)
			if err != nil && !errors.Is(err, fault.NotFound) {
				return nil, err
			}
			if err != nil || c.Version != item.Base || c.Verification == nil || c.Verification.ID != item.Verification {
				item.State = "conflict"
				item.Reason = "VERSION_CONFLICT"
			} else {
				err = r.apply(c, p)
				if err != nil {
					var f *fault.Fault
					if !errors.As(err, &f) || f.Status != 409 {
						return nil, err
					}
					item.State = "blocked"
					item.Reason = f.Reason
				} else {
					item.State = "applied"
					item.Version = c.Version + 1
				}
			}
		}
		index[s] = item
	}
	out := make([]Item, 0, len(in.IDs))
	for _, s := range in.IDs {
		out = append(out, index[s])
	}
	return r.operation(0, "connections.activate", "succeeded", map[string]any{"items": out})
}
