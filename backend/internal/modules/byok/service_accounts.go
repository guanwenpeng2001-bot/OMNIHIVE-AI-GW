package byok

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
)

// A service account uses a dedicated project key and keeps its original sponsor
// membership. Registering a key takes it out of the ordinary human grant path.
type ServiceAccount struct {
	ID        int64      `json:"id,string"`
	Key       int64      `json:"key_id,string"`
	Project   int64      `json:"project_id,string"`
	Active    bool       `json:"active"`
	RevokedAt *time.Time `json:"revoked_at"`
}

func (s *Store) ServiceAccount(ctx context.Context, t Target, key int64, action string) (ServiceAccount, error) {
	var v ServiceAccount
	if t.Project <= 0 || key <= 0 {
		return v, fault.Invalid("/key_id")
	}
	access, err := s.access.Evaluate(ctx, t.Actor, t.Workspace, t.Project, Manage)
	if err != nil {
		return v, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	if action == "register" || action == "retire" {
		var scoped int64
		err = tx.QueryRowContext(ctx, "SELECT k.id"+keyProject+" WHERE k.id=$1 AND k.workspace_id=$2 AND p.id=$3 FOR UPDATE OF k", key, t.Workspace, t.Project).Scan(&scoped)
		if errors.Is(err, sql.ErrNoRows) {
			return v, fault.NotFound
		}
		if err != nil {
			return v, err
		}
	}
	switch action {
	case "register":
		// Only a key's owner may designate it. Reusing a retired designation requires a fresh key.
		res, err := tx.ExecContext(ctx, `INSERT INTO byok_service_accounts(workspace_id,project_id,api_key_id,membership_id)
    SELECT k.workspace_id,p.id,k.id,m.id `+keyProject+`
    JOIN workspace_members m ON m.workspace_id=k.workspace_id AND m.user_id=k.user_id
    WHERE k.workspace_id=$1 AND p.id=$2 AND k.user_id=$3 AND k.id=$4 AND x.member_bound AND x.issued_membership=m.id AND `+keyActive+`
    ON CONFLICT(api_key_id) DO UPDATE SET api_key_id=excluded.api_key_id
    WHERE byok_service_accounts.revoked_at IS NULL AND byok_service_accounts.membership_id=excluded.membership_id AND byok_service_accounts.project_id=excluded.project_id`, t.Workspace, t.Project, t.Actor, key)
		if err != nil {
			return v, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return v, err
		}
		if n == 0 {
			return v, fault.Forbidden
		}
	case "retire":
		_, err = tx.ExecContext(ctx, `UPDATE byok_service_accounts SET revoked_at=coalesce(revoked_at,clock_timestamp()) WHERE workspace_id=$1 AND project_id=$2 AND api_key_id=$3`, t.Workspace, t.Project, key)
		if err != nil {
			return v, err
		}
	case "service_status":
	default:
		return v, fault.Invalid("/operation")
	}
	err = tx.QueryRowContext(ctx, `SELECT sa.id,sa.api_key_id,sa.project_id,
   (sa.revoked_at IS NULL AND EXISTS(SELECT 1 `+keyProject+`
    JOIN workspace_members m ON m.workspace_id=k.workspace_id AND m.user_id=k.user_id
    JOIN users u ON u.id=m.user_id AND u.deleted_at IS NULL AND u.status='active'
    WHERE k.id=sa.api_key_id AND p.id=sa.project_id AND m.id=sa.membership_id AND `+keyActive+`)),sa.revoked_at
    FROM byok_service_accounts sa WHERE sa.workspace_id=$1 AND sa.project_id=$2 AND sa.api_key_id=$3`, t.Workspace, t.Project, key).Scan(&v.ID, &v.Key, &v.Project, &v.Active, &v.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return v, fault.NotFound
	}
	if err != nil {
		return v, err
	}
	if err = fenceManager(ctx, tx, t, access.Version); err != nil {
		return v, err
	}
	return v, tx.Commit()
}
