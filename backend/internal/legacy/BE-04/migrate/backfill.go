package migrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	legacy "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/BE-04"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/connections"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
)

type Report struct{ Mapped, Quarantined int }

func Batch(ctx context.Context, db *sql.DB, enc service.SecretEncryptor, limit int) (Report, error) {
	var report Report
	if err := service.RequirePersistentEncryption(enc); err != nil {
		return report, err
	}
	for i := 0; i < limit; i++ {
		result, err := next(ctx, db, enc)
		if err != nil {
			return report, err
		}
		switch result {
		case "mapped":
			report.Mapped++
		case "quarantine":
			report.Quarantined++
		default:
			return report, nil
		}
	}
	return report, nil
}
func next(ctx context.Context, db *sql.DB, enc service.SecretEncryptor) (string, error) {
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	var id, w int64
	var name, provider, auth, proxy, state string
	var data []byte
	var concurrency int
	var complex bool
	var revision time.Time
	e = tx.QueryRowContext(ctx, `SELECT a.id,a.workspace_id,a.name,a.platform,a.type,a.credentials,coalesce(a.proxy_id::text,''),a.concurrency,
 CASE WHEN a.status='active' AND a.schedulable THEN 'active' ELSE 'disabled' END,
 (c.account_id IS NOT NULL AND c.workspace_id<>a.workspace_id) OR a.proxy_group_id IS NOT NULL OR a.parent_account_id IS NOT NULL OR a.extra ?| ARRAY['header_overrides','custom_base_url','custom_base_url_enabled'],a.updated_at
 FROM accounts a LEFT JOIN provider_connections c ON c.account_id=a.id
 LEFT JOIN connection_migration_quarantine q ON q.account_id=a.id
 WHERE a.deleted_at IS NULL AND (c.account_id IS NULL OR c.legacy_dirty) AND q.reviewed_revision IS DISTINCT FROM a.updated_at
 ORDER BY a.id LIMIT 1 FOR UPDATE OF a SKIP LOCKED`).Scan(&id, &w, &name, &provider, &auth, &data, &proxy, &concurrency, &state, &complex, &revision)
	if e == sql.ErrNoRows {
		return "", nil
	}
	if e != nil {
		return "", e
	}
	quarantine := func(reason string) (string, error) {
		_, e := tx.ExecContext(ctx, `INSERT INTO connection_migration_quarantine(account_id,reason,reviewed_revision) VALUES($1,$2,$3)
 ON CONFLICT(account_id) DO UPDATE SET reason=excluded.reason,reviewed_revision=excluded.reviewed_revision,recorded_at=now()`, id, reason, revision)
		if e != nil {
			return "", e
		}
		return "quarantine", tx.Commit()
	}
	if auth == "apikey" {
		auth = "api_key"
	}
	if auth == "bedrock" {
		provider = "bedrock"
	}
	if provider == "vertex" {
		provider = "gemini"
	}
	if complex || len(connections.Schemas(provider, auth)) != 1 || concurrency < 1 || !connections.ValidText(name, 100) {
		return quarantine("CONFIGURATION_REVIEW")
	}
	fields := map[string]any{}
	if json.Unmarshal(data, &fields) != nil {
		return quarantine("INVALID_DOCUMENT")
	}
	safe, secrets := service.SplitCredentialSecrets(fields)
	for k := range safe {
		switch k {
		case "base_url", "model_mapping", service.CredentialSecretReferenceKey:
		default:
			return quarantine("CONFIGURATION_REVIEW")
		}
	}
	endpoint, _ := safe["base_url"].(string)
	if !connections.ValidEndpoint(endpoint) {
		return quarantine("ENDPOINT_REVIEW")
	}
	models := []string{}
	if raw, ok := safe["model_mapping"]; ok {
		mapping, ok := raw.(map[string]any)
		if !ok {
			return quarantine("MODEL_MAPPING_REVIEW")
		}
		for k, v := range mapping {
			s, ok := v.(string)
			if !ok || k != s || !connections.ValidText(k, 256) {
				return quarantine("MODEL_MAPPING_REVIEW")
			}
			models = append(models, k)
		}
		slices.Sort(models)
	}
	if _, _, e = legacy.Proxy(ctx, tx, w, proxy, true); e != nil {
		var f *fault.Fault
		if errors.As(e, &f) {
			return quarantine("PROXY_REVIEW")
		}
		return "", e
	}
	reference, referenced := service.CredentialReference(fields)
	if referenced {
		var m service.CredentialSecret
		var cipher string
		e = tx.QueryRowContext(ctx, `SELECT workspace_id,account_id,reference,version,ciphertext,revoked_at FROM credential_secrets WHERE workspace_id=$1 AND account_id=$2 AND reference=$3`, w, id, reference).Scan(&m.WorkspaceID, &m.AccountID, &m.Reference, &m.Version, &cipher, &m.RevokedAt)
		if e == sql.ErrNoRows {
			return quarantine("INVALID_REFERENCE")
		}
		if e != nil {
			return "", e
		}
		if len(secrets) > 0 || m.Version != service.CredentialVersion(fields) {
			return quarantine("INVALID_REFERENCE")
		}
		if m.RevokedAt != nil {
			state = "disabled"
		} else if _, e = service.ResolveCredentialSecret(ctx, enc, m, cipher); e != nil {
			return quarantine("CREDENTIAL_UNREADABLE")
		}
	} else {
		if len(secrets) == 0 {
			return quarantine("CREDENTIAL_MISSING")
		}
		if _, e = tx.ExecContext(ctx, "SAVEPOINT before_conversion"); e != nil {
			return "", e
		}
		if e = legacy.ChangeSecret(ctx, tx, enc, w, id, 0, secrets, false); e != nil {
			if errors.Is(e, service.ErrCredentialSecretInvalid) {
				if _, e = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT before_conversion"); e != nil {
					return "", e
				}
				return quarantine("CREDENTIAL_UNREADABLE")
			}
			return "", e
		}
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO provider_connections(account_id,workspace_id,name,provider,auth_type,endpoint,proxy_id,model_ids,concurrency,state)
 VALUES($1,$2,$3,$4,$5,$6,nullif($7,'')::bigint,$8,$9,$10) ON CONFLICT(account_id) DO UPDATE SET
 name=excluded.name,provider=excluded.provider,auth_type=excluded.auth_type,endpoint=excluded.endpoint,proxy_id=excluded.proxy_id,
 model_ids=excluded.model_ids,concurrency=excluded.concurrency,state=excluded.state,verified_id=NULL,legacy_dirty=false,version=provider_connections.version+1
 WHERE provider_connections.workspace_id=excluded.workspace_id`, id, w, name, provider, auth, endpoint, proxy, pq.Array(models), concurrency, state)
	if e != nil {
		return "", e
	}
	_, e = tx.ExecContext(ctx, `DELETE FROM connection_migration_quarantine WHERE account_id=$1`, id)
	if e != nil {
		return "", e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO connection_events(workspace_id,account_id,type,version) SELECT workspace_id,account_id,'connection.mapped',version FROM provider_connections WHERE account_id=$1 AND workspace_id=$2`, id, w)
	if e != nil {
		return "", e
	}
	return "mapped", tx.Commit()
}
