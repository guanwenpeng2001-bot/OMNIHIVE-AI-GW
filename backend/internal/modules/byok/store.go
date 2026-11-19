package byok

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	legacy "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/BE-04"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/connections"
	classes "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/credentials/byok"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
)

type Store struct {
	db      *sql.DB
	enc     service.SecretEncryptor
	probe   connections.Prober
	access  identity.AccessEvaluator
	signing []byte
}

func New(db *sql.DB, enc service.SecretEncryptor, probe connections.Prober, access identity.AccessEvaluator, signing []byte) *Store {
	return &Store{db, enc, probe, access, signing}
}

type Target struct{ Actor, Workspace, Project, ID, Version int64 }
type Command struct {
	Target
	Action, Key  string
	Create       CreateInput
	Policy       Policy
	Secret, Mode string
	Patch        *Patch `json:",omitempty"`
}

const metadata = `c.id,c.workspace_id,c.provider,c.version,c.secret_version,
 CASE WHEN c.state='verified' AND c.verified_until<=clock_timestamp() THEN 'expired' ELSE c.state END,
 c.fingerprint,c.verified_until,c.project_ids,c.member_ids,c.key_ids,c.model_ids,c.priority,c.allow_platform_fallback,c.include_external_in_budget,c.auth_type,c.service_account_ids,
 ARRAY(SELECT model FROM unnest(c.model_ids) model WHERE c.state='verified' AND c.verified_until>clock_timestamp() AND model=ANY(c.verified_models))`
const visible = `c.workspace_id=$1 AND ($2::bigint=0 OR c.project_ids=ARRAY[$2::bigint])`

type scanner interface{ Scan(...any) error }

func scanCredential(row scanner) (Credential, error) {
	var c Credential
	e := row.Scan(&c.ID, &c.Workspace, &c.Provider, &c.Version, &c.SecretVersion, &c.State, &c.Fingerprint, &c.VerifiedUntil,
		pq.Array(&c.Scope.Projects), pq.Array(&c.Scope.Members), pq.Array(&c.Scope.Keys), pq.Array(&c.Scope.Models), &c.Priority, &c.Fallback, &c.Budget, &c.Auth, pq.Array(&c.Scope.ServiceAccounts), pq.Array(&c.VerifiedModels))
	if errors.Is(e, sql.ErrNoRows) {
		e = fault.NotFound
	}
	return c, e
}

type Page struct {
	Items []Credential `json:"items"`
	Next  *string      `json:"next_cursor"`
}

func (s *Store) List(ctx context.Context, t Target, after int64, limit int) (Page, error) {
	if _, e := s.access.Evaluate(ctx, t.Actor, t.Workspace, t.Project, Manage); e != nil {
		return Page{}, e
	}
	if after < 0 || limit < 1 || limit > 100 {
		return Page{}, fault.Invalid("/limit")
	}
	rows, e := s.db.QueryContext(ctx, `SELECT `+metadata+` FROM byok_credentials c WHERE `+visible+` AND c.id>$3 ORDER BY c.id LIMIT $4`, t.Workspace, t.Project, after, limit+1)
	if e != nil {
		return Page{}, e
	}
	defer rows.Close()
	out := []Credential{}
	for rows.Next() {
		v, e := scanCredential(rows)
		if e != nil {
			return Page{}, e
		}
		out = append(out, v)
	}
	var next *string
	if len(out) > limit {
		out = out[:limit]
		cursor := decimalID(out[limit-1].ID)
		next = &cursor
	}
	return Page{Items: out, Next: next}, rows.Err()
}

func current(ctx context.Context, tx *sql.Tx, t Target, lock bool) (Credential, error) {
	query := `SELECT ` + metadata + ` FROM byok_credentials c WHERE ` + visible + ` AND c.id=$3`
	if lock {
		query += " FOR UPDATE OF c"
	}
	c, e := scanCredential(tx.QueryRowContext(ctx, query, t.Workspace, t.Project, t.ID))
	if e == nil && t.Version != c.Version {
		return c, fault.Stale(c.Version)
	}
	return c, e
}

func (s *Store) Change(ctx context.Context, cmd Command) (json.RawMessage, error) {
	a, e := s.access.Evaluate(ctx, cmd.Actor, cmd.Workspace, cmd.Project, Manage)
	if e != nil {
		return nil, e
	}
	if ((cmd.Action != "policy" && cmd.Action != "patch") || cmd.Key != "") && !connections.ValidText(cmd.Key, 200) {
		return nil, fault.Invalid("/Idempotency-Key")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	route := fmt.Sprintf("byok/v1/%s/%d", cmd.Action, cmd.ID)
	var result json.RawMessage
	if cmd.Key != "" {
		if len(s.signing) == 0 {
			return nil, errors.New("missing receipt signing key")
		}
		doc, e := json.Marshal(cmd)
		if e != nil {
			return nil, e
		}
		mac := hmac.New(sha256.New, s.signing)
		mac.Write([]byte("byok/receipt/v1\x00"))
		mac.Write(doc)
		var cached []byte
		e = tx.QueryRowContext(ctx, `INSERT INTO identity_create_receipts(workspace_id,project_scope,actor_id,route,request_key,fingerprint) VALUES($1,$2,$3,$4,$5,$6)
            ON CONFLICT(workspace_id,project_scope,actor_id,route,request_key) DO UPDATE SET fingerprint=excluded.fingerprint
            WHERE identity_create_receipts.fingerprint=excluded.fingerprint RETURNING result`, cmd.Workspace, cmd.Project, cmd.Actor, route, cmd.Key, mac.Sum(nil)).Scan(&cached)
		if errors.Is(e, sql.ErrNoRows) {
			return nil, fault.Conflict
		}
		if e != nil {
			return nil, e
		}
		result = cached
		if result != nil {
			result, e = completePilotResponse(result)
			if e != nil {
				return nil, e
			}
		}
	}
	if result == nil {
		if _, e = tx.ExecContext(ctx, "SELECT set_config('omnihive.byok_writer','28',true)"); e != nil {
			return nil, e
		}
		v, e := s.change(ctx, tx, cmd)
		if e != nil {
			return nil, e
		}
		if e = saveEvent(ctx, tx, cmd, v); e != nil {
			return nil, e
		}
		result, e = json.Marshal(v)
		if e != nil {
			return nil, e
		}
		if cmd.Key != "" {
			_, e = tx.ExecContext(ctx, `UPDATE identity_create_receipts SET result=$6 WHERE workspace_id=$1 AND project_scope=$2 AND actor_id=$3 AND route=$4 AND request_key=$5`, cmd.Workspace, cmd.Project, cmd.Actor, route, cmd.Key, string(result))
			if e != nil {
				return nil, e
			}
		}
	}
	if e = fenceManager(ctx, tx, cmd.Target, a.Version); e != nil {
		return nil, e
	}
	return result, tx.Commit()
}

// Old idempotency and operation receipts preserve the metadata snapshot as it
// existed at commit. Fill only the fields whose values were fixed in the pilot.
func completePilotResponse(raw json.RawMessage) (json.RawMessage, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	candidate := raw
	if envelope["result"] != nil {
		candidate = envelope["result"]
	}
	var c Credential
	if err := json.Unmarshal(candidate, &c); err != nil {
		return nil, err
	}
	if c.Auth != "" || c.ID == 0 {
		return raw, nil
	}
	c.Auth = "api_key"
	c.Scope.ServiceAccounts = []string{}
	c.VerifiedModels = []string{}
	data, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	if envelope["result"] == nil {
		return data, nil
	}
	envelope["result"] = data
	return json.Marshal(envelope)
}

func fenceManager(ctx context.Context, tx *sql.Tx, t Target, version int64) error {
	// Fence a grant change, including one which raced an upstream verification.
	var ok bool
	e := tx.QueryRowContext(ctx, `SELECT true FROM workspaces w JOIN users u ON u.id=$2 AND u.deleted_at IS NULL AND u.status='active'
        WHERE w.id=$1 AND w.scope_version=$3 AND (u.role='admin' OR EXISTS(
        SELECT FROM workspace_members m WHERE m.workspace_id=w.id AND m.user_id=u.id AND
        (m.role='admin' OR EXISTS(SELECT FROM project_grants g WHERE g.workspace_id=w.id AND g.user_id=u.id AND g.project_id=$4 AND g.action=$5))))
        FOR SHARE OF w,u`, t.Workspace, t.Actor, version, t.Project, Manage).Scan(&ok)
	if errors.Is(e, sql.ErrNoRows) {
		return fault.Forbidden
	}
	return e
}

const keyProject = ` FROM api_keys k JOIN projects d ON d.workspace_id=k.workspace_id AND d.is_default
    LEFT JOIN project_key_policies x ON x.api_key_id=k.id AND x.workspace_id=k.workspace_id
    LEFT JOIN project_resources r ON r.kind='api_keys' AND r.resource_id=k.id AND r.workspace_id=k.workspace_id
    JOIN projects p ON p.workspace_id=k.workspace_id AND p.id=coalesce(x.project_id,r.project_id,d.id) AND p.archived_at IS NULL `
const keyActive = `k.deleted_at IS NULL AND k.status='active' AND (k.expires_at IS NULL OR k.expires_at>clock_timestamp())
    AND NOT EXISTS(SELECT FROM identity_scope_quarantine q WHERE q.kind='api_keys' AND q.resource_id=k.id)
    AND (x.api_key_id IS NULL OR NOT x.member_bound OR x.issued_membership IS NOT NULL)`

func checkGrants(ctx context.Context, tx *sql.Tx, t Target, p Policy) ([]int64, error) {
	if e := p.validate(t.Project); e != nil {
		return nil, e
	}
	var ok bool
	var bindings []int64
	e := tx.QueryRowContext(ctx, `SELECT
    (SELECT count(*) FROM projects WHERE workspace_id=$1 AND archived_at IS NULL AND id=ANY($2::bigint[]))=cardinality($2::bigint[])
    AND (SELECT count(*) `+keyProject+` LEFT JOIN byok_service_accounts sa ON sa.api_key_id=k.id
    JOIN workspace_members sponsor ON sponsor.workspace_id=k.workspace_id AND sponsor.user_id=k.user_id
    WHERE k.workspace_id=$1 AND k.id=ANY($4::bigint[]) AND p.id=ANY($2::bigint[])
    AND ((sa.id IS NULL AND k.user_id=ANY($3::bigint[])) OR
        (sa.id=ANY($5::bigint[]) AND sa.revoked_at IS NULL AND sa.workspace_id=k.workspace_id AND sa.project_id=p.id AND sa.membership_id=sponsor.id))
    AND `+keyActive+`)=cardinality($4::bigint[])
    AND (SELECT count(*) FROM byok_service_accounts sa WHERE sa.workspace_id=$1 AND sa.project_id=ANY($2::bigint[]) AND sa.id=ANY($5::bigint[])
        AND sa.api_key_id=ANY($4::bigint[]) AND sa.revoked_at IS NULL)=cardinality($5::bigint[]),
    ARRAY(SELECT m.id FROM workspace_members m JOIN users u ON u.id=m.user_id AND u.deleted_at IS NULL AND u.status='active' WHERE m.workspace_id=$1 AND m.user_id=ANY($3::bigint[]))`,
		t.Workspace, pq.Array(p.Projects), pq.Array(nonNil(p.Members)), pq.Array(p.Keys), pq.Array(nonNil(p.ServiceAccounts))).Scan(&ok, pq.Array(&bindings))
	if e == nil && (!ok || len(bindings) != len(p.Members)) {
		return nil, fault.Forbidden
	}
	return bindings, e
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func enabled(ctx context.Context, tx *sql.Tx, w int64, provider, auth string) (string, error) {
	var fee string
	var pilot bool
	e := tx.QueryRowContext(ctx, `SELECT service_fee_usd::text,enabled FROM byok_pilot_providers WHERE workspace_id=$1 AND provider=$2 FOR SHARE`, w, provider).Scan(&fee, &pilot)
	if errors.Is(e, sql.ErrNoRows) {
		return "", fault.Forbidden
	}
	if e != nil {
		return "", e
	}
	var permit bool
	e = tx.QueryRowContext(ctx, `SELECT enabled FROM byok_provider_classes WHERE workspace_id=$1 AND provider=$2 AND auth_type=$3`, w, provider, auth).Scan(&permit)
	if errors.Is(e, sql.ErrNoRows) {
		permit = pilot && auth == "api_key"
	} else if e != nil {
		return "", e
	}
	if !permit || !classes.Supported(provider, auth) {
		return "", fault.Forbidden
	}
	return fee, nil
}
func (s *Store) protect(ctx context.Context, w, id, version int64, secret map[string]any) (ref, cipher, fp string, err error) {
	ref = "byok:" + uuid.NewString()
	cipher, err = service.EncryptVerifiedCredential(ctx, s.enc, service.CredentialSecret{WorkspaceID: w, AccountID: id, Version: version, Reference: ref}, secret)
	if err != nil {
		return "", "", "", err
	}
	digest := sha256.Sum256([]byte(cipher))
	return ref, cipher, "fp_" + hex.EncodeToString(digest[:8]), nil
}
func (s *Store) resolve(ctx context.Context, tx *sql.Tx, c Credential) (map[string]any, error) {
	var ref, cipher string
	e := tx.QueryRowContext(ctx, `SELECT reference,ciphertext FROM byok_secret_versions WHERE workspace_id=$1 AND credential_id=$2 AND version=$3 AND retired_at IS NULL`, c.Workspace, c.ID, c.SecretVersion).Scan(&ref, &cipher)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, unavailable
	}
	if e != nil {
		return nil, e
	}
	return service.ResolveCredentialSecret(ctx, s.enc, service.CredentialSecret{WorkspaceID: c.Workspace, AccountID: c.ID, Version: c.SecretVersion, Reference: ref}, cipher)
}
func install(ctx context.Context, tx *sql.Tx, c Credential, ref, cipher string) error {
	_, e := tx.ExecContext(ctx, `INSERT INTO byok_secret_versions(workspace_id,credential_id,version,reference,ciphertext) VALUES($1,$2,$3,$4,$5)`, c.Workspace, c.ID, c.SecretVersion, ref, cipher)
	return e
}
func retire(ctx context.Context, tx *sql.Tx, c Credential) error {
	_, e := tx.ExecContext(ctx, `UPDATE byok_secret_versions SET ciphertext=NULL, retired_at=clock_timestamp() WHERE workspace_id=$1 AND credential_id=$2 AND version=$3 AND retired_at IS NULL`, c.Workspace, c.ID, c.SecretVersion)
	return e
}

func (s *Store) create(ctx context.Context, tx *sql.Tx, t Target, in CreateInput) (Credential, error) {
	var c Credential
	if !classes.Supported(in.Provider, in.Auth) {
		return c, fault.Invalid("/provider")
	}
	if _, e := enabled(ctx, tx, t.Workspace, in.Provider, in.Auth); e != nil {
		return c, e
	}
	secret, e := classes.Payload(in.Provider, in.Auth, in.Secret)
	if e != nil {
		return c, e
	}
	bindings, e := checkGrants(ctx, tx, t, in.Policy)
	if e != nil {
		return c, e
	}
	id, e := legacy.Create(ctx, tx, t.Workspace, "Tenant credential", in.Provider, in.Auth)
	if e != nil {
		return c, e
	}
	ref, cipher, fp, e := s.protect(ctx, t.Workspace, id, 1, secret)
	if e != nil {
		return c, e
	}
	c, e = scanCredential(tx.QueryRowContext(ctx, `INSERT INTO byok_credentials AS c(id,workspace_id,provider,state,fingerprint,project_ids,member_ids,member_bindings,key_ids,model_ids,priority,allow_platform_fallback,include_external_in_budget,auth_type,service_account_ids,self_service)
        VALUES($1,$2,$3,'unverified',$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,true) RETURNING `+metadata, id, t.Workspace, in.Provider, fp, pq.Array(in.Projects), pq.Array(nonNil(in.Members)), pq.Array(bindings), pq.Array(in.Keys), pq.Array(in.Models), in.Priority, in.Fallback, in.Budget, in.Auth, pq.Array(nonNil(in.ServiceAccounts))))
	if e != nil {
		return c, e
	}
	return c, install(ctx, tx, c, ref, cipher)
}
