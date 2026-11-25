package legacy

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type DB interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func AccountType(auth string) string {
	if auth == "api_key" {
		return domain.AccountTypeAPIKey
	}
	return auth
}

func Create(ctx context.Context, tx *sql.Tx, w int64, name, provider, auth string) (int64, error) {
	var id int64
	identity := domain.ResolveAccountTypeIdentity(provider, AccountType(auth))
	err := tx.QueryRowContext(ctx, `INSERT INTO accounts(workspace_id,name,platform,type,status,schedulable) VALUES($1,$2,$3,$4,'disabled',false) RETURNING id`, w, name, identity.Platform(), identity.RequiredAccountType()).Scan(&id)
	return id, err
}

func Lock(ctx context.Context, tx *sql.Tx, w, id int64) error {
	var locked int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, w, id).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return fault.NotFound
	}
	return err
}

func Proxy(ctx context.Context, q DB, w int64, policy string, lock bool) (string, string, error) {
	if policy == "" {
		return "", "", nil
	}
	id, e := strconv.ParseInt(policy, 10, 64)
	if e != nil {
		return "", "", fault.Invalid("/network_policy_id")
	}
	query := `SELECT protocol,host,port,coalesce(username,''),coalesce(password,''),updated_at::text FROM proxies WHERE workspace_id=$1 AND id=$2 AND status='active' AND deleted_at IS NULL AND (expires_at IS NULL OR expires_at>now())`
	if lock {
		query += " FOR SHARE"
	}
	var p service.Proxy
	var revision string
	e = q.QueryRowContext(ctx, query, w, id).Scan(&p.Protocol, &p.Host, &p.Port, &p.Username, &p.Password, &revision)
	if errors.Is(e, sql.ErrNoRows) {
		return "", "", fault.Invalid("/network_policy_id")
	}
	return p.URL(), revision, e
}

func ChangeSecret(ctx context.Context, tx *sql.Tx, enc service.SecretEncryptor, w, id, current int64, values map[string]any, disable bool) error {
	m := service.CredentialSecret{WorkspaceID: w, AccountID: id, Version: current + 1}
	err := tx.QueryRowContext(ctx, `SELECT reference FROM credential_secrets WHERE workspace_id=$1 AND account_id=$2 AND version=$3`, w, id, current).Scan(&m.Reference)
	if errors.Is(err, sql.ErrNoRows) && current == 0 {
		m.Reference = uuid.NewString()
	} else if err != nil {
		return err
	}
	cipher := ""
	if values != nil {
		cipher, err = service.EncryptVerifiedCredential(ctx, enc, m, values)
		if err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "SELECT set_config('omnihive.connection_writer','v2',true)"); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO credential_secrets(workspace_id,account_id,version,reference,ciphertext,revoked_at,api_key_digest)
 VALUES($1,$2,$3,$4,$5,CASE WHEN $6 THEN now() END,$7)
 ON CONFLICT(account_id) DO UPDATE SET version=excluded.version,api_key_digest=excluded.api_key_digest,ciphertext=excluded.ciphertext,revoked_at=excluded.revoked_at,updated_at=now()
 WHERE credential_secrets.reference=excluded.reference AND credential_secrets.version=excluded.version-1`, w, id, m.Version, m.Reference, cipher, values == nil, service.AccountAPIKeyDigest(values))
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return service.ErrCredentialSecretConflict
	}
	if key, ok := values["api_key"].(string); ok && len(key) > 8 {
		suffix := []rune(key)
		if len(suffix) > 8 {
			_, err = tx.ExecContext(ctx, "UPDATE connection_credential_versions SET last_four=$3 WHERE account_id=$1 AND version=$2", id, m.Version, string(suffix[len(suffix)-4:]))
			if err != nil {
				return err
			}
		}
	}
	if err = repository.InstallCredentialVersion(domain.WithWorkspaceScope(ctx, w), tx, m); err != nil {
		return err
	}
	if disable {
		_, err = tx.ExecContext(ctx, "UPDATE accounts SET status='disabled',schedulable=false WHERE workspace_id=$1 AND id=$2", w, id)
	}
	return err
}

func Activate(ctx context.Context, tx *sql.Tx, w, id, actor int64, name, endpoint, proxy string, concurrency int, models []string) error {
	mapping := map[string]string{}
	for _, s := range models {
		mapping[s] = s
	}
	mode := service.ModelSelectionRestricted
	if len(models) == 0 {
		mode = service.ModelSelectionInherit
	}
	selection, e := repository.UpdateModelSelectionInSQL(domain.WithWorkspaceScope(ctx, w), tx, id, service.ModelSelectionConnection, func(account *service.Account) (service.ModelSelectionResult, error) {
		return service.ResolveLockedModelSelection(account, service.ModelSelectionCommand{Mode: mode, Models: models, ModelMapping: mapping, Reason: "Connections activation", ActorID: actor, Source: service.ModelSelectionConnection})
	})
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "SELECT set_config('omnihive.connection_writer','v2',true)"); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE accounts SET name=$3,proxy_id=nullif($4,'')::bigint,concurrency=$5,status='active',schedulable=$8,
 credentials=(credentials-'base_url')||jsonb_strip_nulls(jsonb_build_object('base_url',nullif($6,''))),updated_at=now()
 WHERE workspace_id=$1 AND id=$2`, w, id, name, proxy, concurrency, endpoint, len(models) > 0)
	if e != nil {
		return e
	}
	if selection.Changed {
		return nil
	}
	return Notify(ctx, tx, id)
}
func Notify(ctx context.Context, tx *sql.Tx, id int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO scheduler_outbox(event_type,account_id,payload,created_at) VALUES($1,$2,'{}',now())`, service.SchedulerOutboxEventAccountChanged, id)
	return err
}
