package connections

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	legacy "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/connections"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/credentials"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
)

type Prober interface {
	ProbeConnection(context.Context, *service.Account, string) service.ConnectionProbe
}
type Store struct {
	db     *sql.DB
	access identity.AccessEvaluator
	enc    service.SecretEncryptor
	probe  Prober
	key    []byte
}

func New(db *sql.DB, access identity.AccessEvaluator, enc service.SecretEncryptor, probe Prober, cfg *config.Config) *Store {
	if native, ok := probe.(*service.AccountTestService); ok {
		probe = compatibleEndpointProber{native: native, policy: cfg.Security.URLAllowlist}
	}
	return &Store{db, access, enc, probe, []byte(cfg.JWT.Secret)}
}

type Command struct {
	Actor, Workspace, ID, Version int64
	Route, Key                    string
	Fingerprint                   []byte
}
type Session struct {
	ctx   context.Context
	tx    *sql.Tx
	store *Store
	Command
	access    identity.Access
	required  []string
	actorRole string
}
type Work func(*Session) (any, error)

func (s *Store) Run(ctx context.Context, c Command, actions []string, fn Work) (any, error) {
	var actorRole string
	edit := !strings.HasPrefix(c.Route, "be04/GET/")
	if edit {
		err := s.db.QueryRowContext(ctx, "SELECT role FROM users WHERE id=$1 AND status='active' AND deleted_at IS NULL", c.Actor).Scan(&actorRole)
		if err == sql.ErrNoRows {
			return nil, fault.NotFound
		}
		if err != nil {
			return nil, err
		}
	}
	a, err := s.access.Evaluate(ctx, c.Actor, c.Workspace, 0, actions[0])
	if err != nil {
		return nil, err
	}
	for _, act := range actions[1:] {
		if err = a.Require(act); err != nil {
			return nil, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	session := &Session{ctx, tx, s, c, a, append([]string{}, actions...), actorRole}
	// Hold only the receipt lock during network I/O. Legacy refresh and rotation
	// share an account-row fence when the verified result is committed.
	if c.Key != "" {
		if !ValidText(c.Key, 200) {
			return nil, fault.Invalid("/Idempotency-Key")
		}
		mac := hmac.New(sha256.New, s.key)
		_, _ = mac.Write([]byte("connections.receipt.v1\x00"))
		_, _ = mac.Write(c.Fingerprint)
		_, _ = mac.Write([]byte(strconv.FormatInt(c.Version, 10)))
		var cached []byte
		err = tx.QueryRowContext(ctx, `INSERT INTO identity_create_receipts(workspace_id,project_scope,actor_id,route,request_key,fingerprint)
   VALUES($1,0,$2,$3,$4,$5) ON CONFLICT(workspace_id,project_scope,actor_id,route,request_key)
   DO UPDATE SET fingerprint=excluded.fingerprint WHERE identity_create_receipts.fingerprint=excluded.fingerprint RETURNING result`, c.Workspace, c.Actor, c.Route, c.Key, mac.Sum(nil)).Scan(&cached)
		if err == sql.ErrNoRows {
			return nil, fault.Conflict
		}
		if err != nil {
			return nil, err
		}
		if cached != nil {
			if err = session.fenceAccess(); err != nil {
				return nil, err
			}
			if err = tx.Commit(); err != nil {
				return nil, err
			}
			var receipt replay
			if err = json.Unmarshal(cached, &receipt); err != nil {
				return nil, err
			}
			for _, act := range receipt.Required {
				if err = a.Require(act); err != nil {
					return nil, err
				}
			}
			return projectReceipt(receipt.Data, a), nil
		}
	}
	value, err := fn(session)
	if err != nil {
		return nil, err
	}
	if edit {
		if e := session.fenceAccess(); e != nil {
			return nil, e
		}
	}
	if c.Key != "" {
		v, e := json.Marshal(value)
		if e != nil {
			return nil, e
		}
		data, e := json.Marshal(replay{session.required, v})
		if e != nil {
			return nil, e
		}
		_, err = tx.ExecContext(ctx, `UPDATE identity_create_receipts SET result=$5 WHERE workspace_id=$1 AND project_scope=0 AND actor_id=$2 AND route=$3 AND request_key=$4`, c.Workspace, c.Actor, c.Route, c.Key, string(data))
		if err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if c.Route != "" && !strings.HasPrefix(c.Route, "be04/GET/") {
		service.InvalidateModelCatalogCaches()
	}
	return value, nil
}

type replay struct {
	Required []string
	Data     json.RawMessage
}

// Identity bumps the workspace epoch on every membership and grant mutation.
func (r *Session) fenceAccess() error {
	var version int64
	var role string
	err := r.tx.QueryRowContext(r.ctx, "SELECT w.scope_version,u.role FROM workspaces w JOIN users u ON u.id=$2 AND u.status='active' AND u.deleted_at IS NULL WHERE w.id=$1 FOR SHARE OF w,u", r.Workspace, r.Actor).Scan(&version, &role)
	if err == sql.ErrNoRows {
		return fault.Forbidden
	}
	if err != nil {
		return err
	}
	if version != r.access.Version || role != r.actorRole {
		return conflict("ACCESS_CHANGED")
	}
	return nil
}
func (r *Session) require(act string) error {
	r.required = append(r.required, act)
	return r.access.Require(act)
}
func projectReceipt(data []byte, a identity.Access) any {
	var v map[string]any
	if json.Unmarshal(data, &v) != nil {
		return nil
	}
	if a.Require("connections.read") != nil {
		switch {
		case v["operation_id"] != nil:
			v["resource_id"], v["result"] = nil, nil
		case v["preview_id"] != nil:
			v["items"], v["adds"], v["removes"] = []any{}, []any{}, []any{}
		case v["max_concurrency"] != nil:
			v["connection_ids"] = []string{}
		default:
			return map[string]any{"version": v["version"], "redactions": []string{"connection", "credential"}}
		}
	}
	if a.Require("credentials.read_metadata") != nil {
		delete(v, "credential")
		if v["schema_version"] != nil {
			v["redactions"] = []string{"credential"}
		}
	}
	if actions, ok := v["allowed_actions"].([]any); ok {
		current := []string{}
		for _, entry := range actions {
			if act, ok := entry.(string); ok && a.Require(act) == nil {
				current = append(current, act)
			}
		}
		v["allowed_actions"] = current
	}
	if a.Require("credentials.read_metadata") != nil {
		if result, ok := v["result"].(map[string]any); ok {
			delete(result, "credential_version")
		}
	}
	return v
}

const selection = `SELECT c.account_id,c.workspace_id,c.provider,c.auth_type,c.name,c.endpoint,coalesce(c.proxy_id::text,''),
 c.model_ids,c.pool_ids,c.concurrency,c.state,c.version,coalesce(k.version,0),k.revoked_at,k.reference,v.snapshot,CASE WHEN k.revoked_at IS NULL THEN substr(md5(k.ciphertext),1,16) END,h.last_four,
 a.extra#>>'{ollama_cloud_usage_snapshot,fetched_at}',a.extra#>>'{ollama_cloud_usage_snapshot,next_refresh_at}',
 a.extra#>>'{ollama_cloud_usage_snapshot,status}',a.extra#>>'{ollama_cloud_usage_snapshot,data,five_hour,used_percent}',
 a.extra->'header_overrides'
 FROM provider_connections c JOIN accounts a ON a.id=c.account_id AND a.workspace_id=c.workspace_id AND a.deleted_at IS NULL AND NOT c.legacy_dirty
 LEFT JOIN credential_secrets k ON k.account_id=c.account_id AND k.workspace_id=c.workspace_id LEFT JOIN connection_credential_versions h ON h.account_id=k.account_id AND h.version=k.version LEFT JOIN connection_verifications v ON v.id=c.verified_id`

type scanner interface{ Scan(...any) error }

func scanConnection(row scanner) (Connection, error) {
	var c Connection
	var version int64
	var revoked *time.Time
	var reference *string
	var fetched, nextRefresh, quotaStatus, used *string
	var fingerprint, lastFour *string
	var snapshot, headers []byte
	var pools []int64
	err := row.Scan(&c.ID, &c.Workspace, &c.Provider, &c.AuthType, &c.Name, &c.Endpoint, &c.NetworkPolicyID, pq.Array(&c.Models), pq.Array(&pools), &c.Limits.Concurrency, &c.State, &c.Version, &version, &revoked, &reference, &snapshot, &fingerprint, &lastFour, &fetched, &nextRefresh, &quotaStatus, &used, &headers)
	if err == sql.ErrNoRows {
		return c, fault.NotFound
	}
	if err != nil {
		return c, err
	}
	if headers != nil {
		if err = json.Unmarshal(headers, &c.HeaderOverrides); err != nil {
			return c, err
		}
	}
	c.Pools = []string{}
	for _, id := range pools {
		c.Pools = append(c.Pools, strconv.FormatInt(id, 10))
	}
	c.SchemaVersion = SchemaVersion
	c.Quota = map[string]any{"state": "unknown", "remaining": nil, "last_success_at": nil}
	if fetched != nil && used != nil {
		observed, e1 := time.Parse(time.RFC3339Nano, *fetched)
		pct, e2 := strconv.ParseFloat(*used, 64)
		if e1 == nil && e2 == nil && pct >= 0 && pct <= 100 {
			state := "stale"
			if nextRefresh != nil && quotaStatus != nil && *quotaStatus == "ok" {
				if next, e := time.Parse(time.RFC3339Nano, *nextRefresh); e == nil && next.After(time.Now()) {
					state = "fresh"
				}
			}
			c.Quota = map[string]any{"state": state, "remaining": strconv.FormatFloat(100-pct, 'f', -1, 64), "unit": "percent", "window": "five_hour", "source": "ollama_cloud_usage_snapshot", "last_success_at": observed.UTC()}
		}
	}
	c.Credential = &credentials.Metadata{Version: version, State: "unset", Fingerprint: fingerprint, LastFour: lastFour}
	if reference != nil {
		c.Credential.State = "unverified"
		if revoked != nil {
			c.Credential.State = "revoked"
		}
	}
	if snapshot != nil {
		if err = json.Unmarshal(snapshot, &c.Verification); err != nil {
			return c, err
		}
		if c.Verification.Expires.After(time.Now()) {
			c.Credential.State = "verified"
		}
	}
	return c, nil
}
func (r *Session) get(id int64, lock bool) (Connection, error) {
	if lock {
		if err := legacy.Lock(r.ctx, r.tx, r.Workspace, id); err != nil {
			return Connection{}, err
		}
	}
	return scanConnection(r.tx.QueryRowContext(r.ctx, selection+" WHERE c.workspace_id=$1 AND c.account_id=$2", r.Workspace, id))
}
func (r *Session) current(lock bool) (Connection, error) {
	c, e := r.get(r.ID, lock)
	if e == nil && r.Version != 0 && c.Version != r.Version {
		e = fault.Stale(c.Version)
	}
	return c, e
}
func (r *Session) Get() (any, error) { c, e := r.current(false); return c.Project(r.access), e }
func (r *Session) Create(in Create) (any, error) {
	if e := in.validateSchema(); e != nil {
		return nil, e
	}
	if e := validatePatch(Patch{Name: &in.Name, Endpoint: &in.Endpoint, NetworkPolicyID: &in.NetworkPolicyID}); e != nil {
		return nil, e
	}
	in.Endpoint = strings.TrimRight(in.Endpoint, "/")
	account := service.Account{Platform: in.Provider, Type: legacy.AccountType(in.AuthType)}
	headers, e := normalizeConnectionHeaders(&account, in.HeaderOverrides)
	if e != nil {
		return nil, e
	}
	if _, _, e := legacy.Proxy(r.ctx, r.tx, r.Workspace, in.NetworkPolicyID, true); e != nil {
		return nil, e
	}
	id, e := legacy.Create(r.ctx, r.tx, r.Workspace, in.Name, in.Provider, in.AuthType)
	if e != nil {
		return nil, e
	}
	_, e = r.tx.ExecContext(r.ctx, `INSERT INTO provider_connections(account_id,workspace_id,name,provider,auth_type,endpoint,proxy_id,concurrency,state) VALUES($1,$2,$3,$4,$5,$6,nullif($7,'')::bigint,3,'draft')`, id, r.Workspace, in.Name, in.Provider, in.AuthType, in.Endpoint, in.NetworkPolicyID)
	if e != nil {
		return nil, e
	}
	if len(headers) != 0 {
		if e = r.saveHeaders(id, headers); e != nil {
			return nil, e
		}
	}
	r.ID = id
	if e = r.event(id, "connection.created", 1); e != nil {
		return nil, e
	}
	return r.Get()
}
func (r *Session) event(id int64, kind string, version int64) error {
	_, err := r.tx.ExecContext(r.ctx, `INSERT INTO connection_events(workspace_id,account_id,actor_id,type,version) VALUES($1,$2,$3,$4,$5)`, r.Workspace, id, r.Actor, kind, version)
	return err
}
func (r *Session) Credential(change credentials.Change) (any, error) {
	c, e := r.current(true)
	if e != nil {
		return nil, e
	}
	values, e := change.Payload(c.AuthType)
	if e != nil {
		return nil, e
	}
	if change.Operation != "keep" {
		if e = r.require("credentials.write"); e != nil {
			return nil, e
		}
		if change.Operation == "set" && c.State == "active" {
			return nil, conflict("ROTATION_REQUIRED")
		}
		if e = legacy.ChangeSecret(r.ctx, r.tx, r.store.enc, r.Workspace, c.ID, c.Credential.Version, values, true); e != nil {
			return nil, e
		}
		if e = r.event(c.ID, "credential."+change.Operation, c.Credential.Version+1); e != nil {
			return nil, e
		}
		c, e = r.get(c.ID, false)
		if e != nil {
			return nil, e
		}
	}
	result := map[string]any{"version": strconv.FormatInt(c.Version, 10)}
	if r.access.Require("connections.read") == nil {
		result["connection_id"] = strconv.FormatInt(c.ID, 10)
		if r.access.Require("credentials.read_metadata") == nil {
			result["credential"] = c.Credential
		}
	}
	return result, nil
}
func (r *Session) operation(id int64, action, state string, result any) (identity.Operation, error) {
	opID := uuid.NewString()
	raw, e := json.Marshal(result)
	if e != nil {
		return identity.Operation{}, e
	}
	_, e = r.tx.ExecContext(r.ctx, `INSERT INTO connection_operations(id,workspace_id,actor_id,account_id,action,state,result) VALUES($1,$2,$3,nullif($4::bigint,0),$5,$6,$7)`, opID, r.Workspace, r.Actor, id, action, state, string(raw))
	if e != nil {
		return identity.Operation{}, e
	}
	return readOp(r.ctx, r.tx, r.Workspace, r.Actor, opID, r.access)
}
func readOp(ctx context.Context, q legacy.DB, w, actor int64, id string, a identity.Access) (identity.Operation, error) {
	var op identity.Operation
	var data []byte
	e := q.QueryRowContext(ctx, `SELECT id::text,state,account_id::text,actor_id,action,result FROM connection_operations WHERE workspace_id=$1 AND id=$2`, w, id).Scan(&op.ID, &op.State, &op.ResourceID, &op.OwnerID, &op.DelegatedAction, &data)
	if errors.Is(e, sql.ErrNoRows) {
		return op, fault.NotFound
	}
	if e != nil {
		return op, e
	}
	if actor != op.OwnerID && a.Require(op.DelegatedAction) != nil {
		return identity.Operation{}, fault.NotFound
	}
	op.StatusURL = "/api/v2/workspaces/" + strconv.FormatInt(w, 10) + "/operations/" + op.ID
	if a.Require("connections.read") == nil {
		if op.DelegatedAction == "credentials.rotate" && a.Require("credentials.read_metadata") != nil {
			var result map[string]any
			if e = json.Unmarshal(data, &result); e != nil {
				return op, e
			}
			delete(result, "credential_version")
			op.Result = result
		} else {
			op.Result = json.RawMessage(data)
		}
	} else {
		op.ResourceID = nil
	}
	if op.State == "failed" {
		op.Error = map[string]string{"reason": "VERIFICATION_FAILED"}
	}
	return op, nil
}
func (s *Store) ReadOperation(ctx context.Context, w, actor int64, id string) (identity.Operation, error) {
	if _, e := uuid.Parse(id); e != nil || len(id) != 36 {
		return identity.Operation{}, fault.NotFound
	}
	a, e := s.access.Evaluate(ctx, actor, w, 0, "")
	if e != nil {
		return identity.Operation{}, e
	}
	return readOp(ctx, s.db, w, actor, id, a)
}

var _ identity.OperationReader = (*Store)(nil)

func (r *Session) checkPools(ids []string) error {
	if ids == nil {
		return nil
	}
	if e := r.require("pools.assign"); e != nil {
		return e
	}
	for _, s := range ids {
		var id int64
		e := r.tx.QueryRowContext(r.ctx, `SELECT id FROM connection_pools WHERE workspace_id=$1 AND id=$2 FOR KEY SHARE`, r.Workspace, s).Scan(&id)
		if e == sql.ErrNoRows {
			return fault.NotFound
		}
		if e != nil {
			return e
		}
	}
	return nil
}
func (r *Session) checkPatch(c Connection, p Patch) error {
	if e := validatePatch(p); e != nil {
		return e
	}
	if p.NetworkPolicyID != nil {
		if _, _, e := legacy.Proxy(r.ctx, r.tx, r.Workspace, *p.NetworkPolicyID, true); e != nil {
			return e
		}
	}
	if c.Credential.Version > 0 && c.Credential.State != "revoked" && (endpointChanged(c.Endpoint, p.Endpoint) || (p.NetworkPolicyID != nil && *p.NetworkPolicyID != c.NetworkPolicyID)) {
		return conflict("CLEAR_CREDENTIAL_BEFORE_EGRESS_CHANGE")
	}
	return r.checkPools(p.Pools)
}
func applyPatch(c *Connection, p Patch) {
	if p.Name != nil {
		c.Name = *p.Name
	}
	if p.Endpoint != nil {
		c.Endpoint = strings.TrimRight(*p.Endpoint, "/")
	}
	if p.NetworkPolicyID != nil {
		c.NetworkPolicyID = *p.NetworkPolicyID
	}
	if p.Models != nil {
		c.Models = p.Models
	}
	if p.Pools != nil {
		c.Pools = p.Pools
	}
	if p.Limits != nil {
		c.Limits = *p.Limits
	}
}
func (r *Session) save(c Connection, state string, clearVerification bool) error {
	_, e := r.tx.ExecContext(r.ctx, `UPDATE provider_connections SET name=$3,endpoint=$4,proxy_id=nullif($5,'')::bigint,model_ids=$6,pool_ids=$7,concurrency=$8,state=$9,version=version+1,verified_id=CASE WHEN $10 THEN NULL ELSE verified_id END WHERE workspace_id=$1 AND account_id=$2`, r.Workspace, c.ID, c.Name, c.Endpoint, c.NetworkPolicyID, pq.Array(c.Models), pq.Array(c.Pools), c.Limits.Concurrency, state, clearVerification)
	return e
}
func (r *Session) saveHeaders(id int64, headers map[string]string) error {
	extra := map[string]any{}
	if len(headers) != 0 {
		extra["header_overrides"] = headers
	}
	data, err := json.Marshal(extra)
	if err != nil {
		return err
	}
	_, err = r.tx.ExecContext(r.ctx, `UPDATE accounts
 SET extra=(coalesce(extra,'{}'::jsonb)-'header_overrides')||$3::jsonb,updated_at=now()
 WHERE workspace_id=$1 AND id=$2`, r.Workspace, id, string(data))
	return err
}
func (r *Session) Patch(p Patch) (any, error) {
	if len(p.Snapshot()) == 0 {
		return nil, fault.Invalid("/patch")
	}
	c, e := r.current(true)
	if e != nil {
		return nil, e
	}
	if e = r.checkPatch(c, p); e != nil {
		return nil, e
	}
	account := service.Account{Platform: c.Provider, Type: legacy.AccountType(c.AuthType)}
	headers, e := normalizeConnectionHeaders(&account, p.HeaderOverrides)
	if e != nil {
		return nil, e
	}
	headersChanged := p.HeaderOverrides != nil && !maps.Equal(headers, c.HeaderOverrides)
	if headersChanged {
		if e = r.saveHeaders(c.ID, headers); e != nil {
			return nil, e
		}
		if e = legacy.Notify(r.ctx, r.tx, c.ID); e != nil {
			return nil, e
		}
	}
	clearVerification := endpointChanged(c.Endpoint, p.Endpoint) ||
		(p.NetworkPolicyID != nil && *p.NetworkPolicyID != c.NetworkPolicyID) || headersChanged
	applyPatch(&c, p)
	if e = r.save(c, "draft", clearVerification); e != nil {
		return nil, e
	}
	if e = r.event(c.ID, "connection.edited", c.Version+1); e != nil {
		return nil, e
	}
	r.Version = 0
	return r.Get()
}
func CanonicalRoute(method, path string) string {
	return strings.Join([]string{"be04", method, path}, "/")
}

type Authorization struct {
	Reference string `json:"credential_draft"`
	Revision  string `json:"revision"`
}

func (r *Session) Attach(in Authorization, binding string) (any, error) {
	c, e := r.current(true)
	if e != nil {
		return nil, e
	}
	if c.State == "active" {
		return nil, conflict("ROTATION_REQUIRED")
	}
	if c.AuthType != "oauth" && c.AuthType != "setup-token" {
		return nil, fault.Invalid("/auth_type")
	}
	if !ValidText(in.Reference, 256) || !ValidText(in.Revision, 256) {
		return nil, fault.Invalid("/credential_draft")
	}
	if e = legacy.AttachAuthorization(r.ctx, r.tx, r.store.enc, r.Workspace, c.ID, c.Credential.Version, c.Provider, c.AuthType, binding, in.Reference, in.Revision); e != nil {
		return nil, e
	}
	if e = r.event(c.ID, "credential.authorized", c.Credential.Version+1); e != nil {
		return nil, e
	}
	r.Version = 0
	return r.Get()
}
