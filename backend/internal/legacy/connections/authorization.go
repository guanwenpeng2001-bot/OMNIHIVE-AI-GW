package legacy

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"slices"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
)

var identityFields = []string{"account_mode", "expires_at", "token_type", "scope", "org_uuid", "account_uuid", "email_address", "email", "chatgpt_account_id", "chatgpt_user_id", "organization_id", "plan_type", "subscription_expires_at", "client_id", "chatgpt_account_is_fedramp", "project_id", "tier_id", "oauth_type", "sub", "team_id", "subscription_tier", "entitlement_status", "machine_id", "mac_machine_id", "client_version"}

func AttachAuthorization(ctx context.Context, tx *sql.Tx, enc service.SecretEncryptor, w, id, version int64, provider, auth, binding, ref, revision string) error {
	sum := sha256.Sum256([]byte(ref))
	var locked int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM pending_auth_sessions WHERE session_token=$1 AND browser_session_key=$2 AND expires_at>now() AND consumed_at IS NULL FOR UPDATE`, hex.EncodeToString(sum[:]), binding).Scan(&locked)
	if err == sql.ErrNoRows {
		return fault.NotFound
	}
	if err != nil {
		return err
	}
	client := dbent.NewClient(dbent.Driver(entsql.NewDriver(dialect.Postgres, entsql.Conn{ExecQuerier: tx})))
	pending := service.NewAuthPendingIdentityService(client)
	a, current, e := pending.ResolveAccountCredentialDraft(ctx, ref, binding)
	if e != nil {
		return fault.NotFound
	}
	if current != revision || a.Platform != provider || a.Type != AccountType(auth) || a.ProxyGroupID != nil {
		return fault.Invalid("/credential_draft")
	}
	safe, secret := service.SplitCredentialSecrets(a.Credentials)
	if len(secret) == 0 {
		return fault.Invalid("/credential_draft")
	}
	for k := range safe {
		if !slices.Contains(identityFields, k) {
			delete(safe, k)
		}
	}
	data, e := json.Marshal(safe)
	if e != nil {
		return e
	}
	if e = ChangeSecret(ctx, tx, enc, w, id, version, secret, true); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE accounts SET credentials=(credentials-$3::text[])||$4::jsonb WHERE id=$1 AND workspace_id=$2`, id, w, pq.Array(identityFields), string(data))
	if e != nil {
		return e
	}
	return pending.CancelAccountCredentialDraft(ctx, ref, binding)
}
func NativeIdentityFields(ctx context.Context, tx *sql.Tx, w, id int64) (map[string]any, error) {
	var b []byte
	e := tx.QueryRowContext(ctx, `SELECT coalesce((SELECT jsonb_object_agg(k,v) FROM jsonb_each(a.credentials) AS f(k,v) WHERE k=ANY($3::text[]) OR (a.platform='workbuddy' AND k IN ('site','uid','domain','enterprise_id','refresh_expires_at'))),'{}'::jsonb)
 FROM accounts a WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL`, w, id, pq.Array(identityFields)).Scan(&b)
	if e != nil {
		return nil, e
	}
	var fields map[string]any
	e = json.Unmarshal(b, &fields)
	return fields, e
}
