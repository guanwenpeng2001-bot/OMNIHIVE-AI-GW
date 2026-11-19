package policy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math/big"
	"slices"
	"strconv"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
	"github.com/lib/pq"
)

func (s Scenario) validate() error {
	if ID(s.Project) == 0 {
		return fault.Invalid("/project_id")
	}
	if s.Key != "" && ID(s.Key) == 0 {
		return fault.Invalid("/api_key_id")
	}
	if !validText(s.Model, 200) || !validText(s.Action, 100) || !validText(s.Protocol, 100) || !validIDs(s.Required) {
		return fault.Invalid("/scenario")
	}
	for _, n := range []string{s.Input, s.Output} {
		v, err := strconv.ParseUint(n, 10, 63)
		if err != nil || strconv.FormatUint(v, 10) != n {
			return fault.Invalid("/token_bound")
		}
	}
	return nil
}
func (s Scenario) target(w int64) Target {
	return Target{Workspace: w, Project: ID(s.Project), Key: ID(s.Key)}
}
func (s Scenario) fingerprint() ([]byte, error) {
	s.Required = append([]string{}, s.Required...)
	slices.Sort(s.Required)
	return digest(s)
}

// ExactInputRate projects the BE-07 card at the frozen pricing instant and
// context tier. The planner compares unrounded USD per million input tokens.
func ExactInputRate(v pricing.Version, at time.Time, inputBound, label string) (*money.Amount, error) {
	if v.Currency != "USD" {
		return nil, nil
	}
	size, err := money.NewQuantity("token", inputBound)
	if err != nil {
		return nil, err
	}
	unit, err := money.NewQuantity("input_token", "1000000")
	if err != nil {
		return nil, err
	}
	result, err := (pricing.Frozen{Version: v, At: at, Usage: pricing.Usage{Context: size, Label: label, Dimensions: []pricing.Dimension{{Kind: "input", Quantity: unit}}}}).Calculate()
	if err != nil || len(result.Components) != 1 {
		return nil, err
	}
	l := result.Components[0]
	rat, ok := new(big.Rat).SetString(l.Numerator + "/" + l.Denominator)
	if !ok {
		return nil, errors.New("invalid exact price component")
	}
	decimal := rat.FloatString(18)
	check, ok := new(big.Rat).SetString(decimal)
	if !ok || check.Cmp(rat) != 0 {
		return nil, nil
	}
	value, err := money.Parse(decimal)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

// Capture belongs to the trusted candidate producer. Call after BE-01 scope,
// BE-04 verification and BE-07 pricing/funding selection have been resolved.
// The simulator receives only a SELECT interface over these frozen facts.
type SnapshotWriter struct{ DB *sql.DB }

func (w SnapshotWriter) Capture(ctx context.Context, c Caller, s Scenario, g Graph) error {
	if err := s.validate(); err != nil {
		return err
	}
	if !validText(g.Snapshot.ID, 160) || g.Snapshot.AsOf.IsZero() || !g.ExpiresAt.After(g.Snapshot.AsOf) || g.ExpiresAt.Sub(g.Snapshot.AsOf) > 5*time.Minute {
		return fault.Invalid("/snapshot")
	}
	seen := map[string]bool{}
	for _, candidate := range g.Snapshot.Candidates {
		_, ok := g.Facts[candidate.ID]
		if !ok || !validText(candidate.ID, 200) || !validText(candidate.ModelID, 200) || seen[candidate.ID] {
			return fault.Invalid("/candidates")
		}
		seen[candidate.ID] = true
	}
	if len(seen) != len(g.Facts) {
		return fault.Invalid("/candidates")
	}
	raw, err := json.Marshal(g)
	if err != nil {
		return err
	}
	hash, err := s.fingerprint()
	if err != nil {
		return err
	}
	tx, err := w.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = readAccess(ctx, tx, c, s); err != nil {
		return err
	}
	if err = checkConnections(ctx, tx, c, s, g); err != nil {
		return err
	}
	args := []any{g.Snapshot.ID, c.Workspace, ID(s.Project), c.Actor, ID(s.Key), c.AccessVersion, hash, g.Snapshot.AsOf, g.ExpiresAt, string(raw)}
	result, err := tx.ExecContext(ctx, `INSERT INTO routing_candidate_snapshots(id,workspace_id,project_id,actor_id,key_id,scope_version,scenario_digest,as_of,expires_at,graph)
 VALUES ($1,$2,$3,$4,nullif($5::bigint,0),$6,$7,$8,$9,$10) ON CONFLICT(id) DO NOTHING`, args...)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var same bool
		err = tx.QueryRowContext(ctx, `SELECT workspace_id=$2 AND project_id=$3 AND actor_id=$4 AND coalesce(key_id,0)=$5 AND scope_version=$6 AND scenario_digest=$7 AND as_of=$8 AND expires_at=$9 AND graph=$10::jsonb FROM routing_candidate_snapshots WHERE id=$1`, args...).Scan(&same)
		if err != nil {
			return err
		}
		if !same {
			return Conflict
		}
	}
	return tx.Commit()
}

func currentGraph(ctx context.Context, q queryer, c Caller, s Scenario, requested string) (Graph, error) {
	var g Graph
	hash, err := s.fingerprint()
	if err != nil {
		return g, err
	}
	var raw []byte
	err = q.QueryRowContext(ctx, `SELECT graph FROM routing_candidate_snapshots WHERE workspace_id=$1 AND project_id=$2 AND actor_id=$3 AND coalesce(key_id,0)=$4
 AND scope_version=$5 AND scenario_digest=$6 AND as_of<=clock_timestamp()
 ORDER BY as_of DESC,id DESC LIMIT 1`, c.Workspace, ID(s.Project), c.Actor, ID(s.Key), c.AccessVersion, hash).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return g, Expired
	}
	if err != nil {
		return g, err
	}
	if err = json.Unmarshal(raw, &g); err != nil {
		return g, err
	}
	if !g.ExpiresAt.After(time.Now()) || requested != "" && requested != g.Snapshot.ID {
		return g, Expired
	}
	if err := checkConnections(ctx, q, c, s, g); err != nil {
		return g, err
	}
	if s.Key != "" {
		var mode string
		var ids []string
		err := q.QueryRowContext(ctx, "SELECT model_mode,models FROM project_key_policies WHERE workspace_id=$1 AND project_id=$2 AND api_key_id=$3", c.Workspace, ID(s.Project), ID(s.Key)).Scan(&mode, pq.Array(&ids))
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return g, err
		}
		if err == nil && mode != "inherit" {
			for i, candidate := range g.Snapshot.Candidates {
				if mode != "allow_list" || !slices.Contains(ids, candidate.ModelID) {
					g.Snapshot.Candidates[i].Rejection = "MODEL_DENIED"
				}
			}
		}
	}
	return g, nil
}

func (s *Store) KeyOwner(ctx context.Context, w, p, k int64) (int64, error) {
	return keyOwner(ctx, s.db, w, p, k)
}
func keyOwner(ctx context.Context, q queryer, w, p, k int64) (int64, error) {
	var owner int64
	err := q.QueryRowContext(ctx, `SELECT k.user_id FROM api_keys k JOIN projects d ON d.workspace_id=k.workspace_id AND d.is_default
 LEFT JOIN project_key_policies x ON x.api_key_id=k.id AND x.workspace_id=k.workspace_id
 LEFT JOIN project_resources r ON r.kind='api_keys' AND r.resource_id=k.id AND r.workspace_id=k.workspace_id
 WHERE k.workspace_id=$1 AND k.id=$3 AND coalesce(x.project_id,r.project_id,d.id)=$2 AND k.deleted_at IS NULL
 AND NOT EXISTS(SELECT FROM identity_scope_quarantine WHERE kind='api_keys' AND resource_id=k.id)`, w, p, k).Scan(&owner)
	return owner, missing(err)
}

// Recheck current BE-04 metadata without selecting a reference, secret or URL.
// Scope, verification, proxy and credential changes invalidate the frozen graph.
func checkConnections(ctx context.Context, q queryer, c Caller, s Scenario, g Graph) error {
	type binding struct {
		Account      int64    `json:"account"`
		Verification string   `json:"verification"`
		Pools        []string `json:"pools"`
	}
	var bindings []binding
	for _, f := range g.Facts {
		if f.Connection == nil {
			continue
		}
		if f.Connection.AccountID <= 0 || !isUUID(f.Connection.VerificationID) {
			return fault.Invalid("/connection")
		}
		bindings = append(bindings, binding{f.Connection.AccountID, f.Connection.VerificationID, f.Pools})
	}
	if len(bindings) == 0 {
		return nil
	}
	raw, err := json.Marshal(bindings)
	if err != nil {
		return err
	}
	var stale bool
	err = q.QueryRowContext(ctx, `SELECT EXISTS(SELECT FROM jsonb_to_recordset($1::jsonb) b(account bigint,verification uuid,pools text[])
 WHERE NOT EXISTS(SELECT FROM provider_connections pc
 JOIN accounts a ON a.id=pc.account_id AND a.workspace_id=pc.workspace_id AND a.deleted_at IS NULL AND a.status='active' AND a.schedulable
 JOIN projects d ON d.workspace_id=pc.workspace_id AND d.is_default
 LEFT JOIN project_resources r ON r.workspace_id=pc.workspace_id AND r.kind='accounts' AND r.resource_id=pc.account_id
 JOIN connection_verifications v ON v.workspace_id=pc.workspace_id AND v.account_id=pc.account_id AND v.id=pc.verified_id AND v.eligible AND v.expires_at>clock_timestamp()
 JOIN connection_credential_versions cv ON cv.workspace_id=v.workspace_id AND cv.account_id=v.account_id AND cv.version=v.credential_version AND cv.revoked_at IS NULL
 WHERE pc.account_id=b.account AND pc.workspace_id=$2 AND pc.state='active' AND NOT pc.legacy_dirty AND v.id=b.verification AND coalesce(r.project_id,d.id)=$3
 AND NOT EXISTS(SELECT FROM identity_scope_quarantine q WHERE q.kind='accounts' AND q.resource_id=pc.account_id)
 AND (pc.proxy_id IS NULL AND v.proxy_revision='' OR EXISTS(SELECT FROM proxies p WHERE p.id=pc.proxy_id AND p.workspace_id=pc.workspace_id AND p.status='active' AND p.deleted_at IS NULL AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp()) AND p.updated_at::text=v.proxy_revision))
 AND coalesce(b.pools,'{}') <@ ARRAY(SELECT m.pool_id::text FROM connection_pool_members m WHERE m.workspace_id=pc.workspace_id AND m.account_id=pc.account_id)))`,
		string(raw), c.Workspace, ID(s.Project)).Scan(&stale)
	if err != nil {
		return err
	}
	if stale {
		return Expired
	}
	return nil
}
