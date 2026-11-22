package pricing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrVersionConflict = errors.New("price version already exists with different content")
var ErrPriceInactive = errors.New("price book has no active publication")

type VersionStore struct {
	DB *sql.DB
}

// Save appends an immutable price version. The caller supplies an authorized,
// server-derived scope key such as a group or channel/model tuple.
func (s VersionStore) Save(ctx context.Context, scope string, v Version) error {
	if err := v.ValidateExecutable(); err != nil {
		return err
	}
	var until *time.Time
	if v.EffectiveUntil != nil {
		t := v.EffectiveUntil.UTC().Truncate(time.Microsecond)
		until = &t
	}
	v.EffectiveFrom, v.EffectiveUntil = v.EffectiveFrom.UTC().Truncate(time.Microsecond), until
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	inserted, err := s.DB.ExecContext(ctx, `INSERT INTO exact_price_versions
		(scope_key, version_id, source, effective_from, effective_until, tariff)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb) ON CONFLICT (scope_key,version_id) DO NOTHING`,
		scope, v.ID, v.Source, v.EffectiveFrom, v.EffectiveUntil, string(raw))
	if err != nil {
		return err
	}
	n, err := inserted.RowsAffected()
	if err != nil || n != 0 {
		return err
	}
	var same bool
	err = s.DB.QueryRowContext(ctx, `SELECT tariff = $3::jsonb FROM exact_price_versions
		WHERE scope_key=$1 AND version_id=$2`, scope, v.ID, string(raw)).Scan(&same)
	if err == nil && !same {
		return ErrVersionConflict
	}
	return err
}

func (s VersionStore) At(ctx context.Context, scope string, at time.Time) ([]Version, error) {
	return VersionsAt(ctx, s.DB, scope, at)
}

type VersionQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// VersionsAt works with a DB or an existing transaction. A managed lineage must
// never fall through to an older open-ended legacy tariff after withdrawal.
func VersionsAt(ctx context.Context, db VersionQueryer, scope string, at time.Time) ([]Version, error) {
	rows, err := db.QueryContext(ctx, `WITH selected AS (SELECT v.tariff FROM exact_price_versions v
		WHERE v.scope_key=$1 AND (
		 EXISTS (SELECT 1 FROM price_book_bindings b WHERE b.scope_key=v.scope_key AND b.version_id=v.version_id
		   AND b.effective_from <= $2 AND (b.effective_until IS NULL OR b.effective_until > $2))
		 OR (NOT EXISTS (SELECT 1 FROM price_book_bindings managed WHERE managed.scope_key=v.scope_key AND managed.effective_from <= $2)
		   AND v.effective_from <= $2 AND (v.effective_until IS NULL OR v.effective_until > $2)))
		ORDER BY v.effective_from DESC,v.version_id)
		SELECT tariff FROM selected
		UNION ALL SELECT NULL::jsonb WHERE NOT EXISTS(SELECT 1 FROM selected)
		 AND EXISTS(SELECT 1 FROM price_book_bindings managed WHERE managed.scope_key=$1 AND managed.effective_from <= $2)`, scope, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Version
	for rows.Next() {
		var raw []byte
		var v Version
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if raw == nil {
			return nil, ErrPriceInactive
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Publish activates a previously saved immutable tariff. Authorization remains
// with the application service; actor is required for the durable audit trail.
// Publications are forward-only. Historical activation intervals cannot be
// retroactively rewritten to change what a customer would have been charged.
func (s VersionStore) Publish(ctx context.Context, scope, version string, actor int64, at time.Time) error {
	if actor <= 0 || scope == "" || version == "" || at.IsZero() {
		return fmt.Errorf("invalid price publication")
	}
	at = at.UTC().Truncate(time.Microsecond)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,339))`, scope); err != nil {
		return err
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT tariff FROM exact_price_versions WHERE scope_key=$1 AND version_id=$2`, scope, version).Scan(&raw); err != nil {
		return err
	}
	var tariff Version
	if err = json.Unmarshal(raw, &tariff); err != nil {
		return err
	}
	if err = tariff.ValidateExecutable(); err != nil {
		return err
	}
	if !tariff.active(at) {
		return fmt.Errorf("tariff is not effective at publication instant")
	}
	var previous int64
	var from time.Time
	var previousVersion string
	err = tx.QueryRowContext(ctx, `SELECT id,effective_from,version_id FROM price_book_bindings
		WHERE scope_key=$1 ORDER BY effective_from DESC LIMIT 1 FOR UPDATE`, scope).Scan(&previous, &from, &previousVersion)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if from.Equal(at) && previousVersion == version {
			return tx.Commit()
		}
		if !at.After(from) {
			return ErrVersionConflict
		}
		if _, err = tx.ExecContext(ctx, `UPDATE price_book_bindings SET effective_until=$2,closed_by=$3,closed_at=now()
			WHERE id=$1 AND (effective_until IS NULL OR effective_until>$2)`, previous, at, actor); err != nil {
			return err
		}
	}
	var supersedes *int64
	if previous > 0 {
		supersedes = &previous
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO price_book_bindings(scope_key,version_id,effective_from,effective_until,published_by,supersedes)
		VALUES($1,$2,$3,$4,$5,$6)`, scope, version, at, tariff.EffectiveUntil, actor, supersedes)
	if err != nil {
		return err
	}
	return tx.Commit()
}
