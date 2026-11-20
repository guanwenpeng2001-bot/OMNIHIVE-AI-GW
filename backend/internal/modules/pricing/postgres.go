package pricing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

var ErrVersionConflict = errors.New("price version already exists with different content")

type VersionStore struct {
	DB *sql.DB
}

// Save appends an immutable price version. The caller supplies an authorized,
// server-derived scope key such as a group or channel/model tuple.
func (s VersionStore) Save(ctx context.Context, scope string, v Version) error {
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
	rows, err := s.DB.QueryContext(ctx, `SELECT tariff FROM exact_price_versions WHERE scope_key=$1
		AND effective_from <= $2 AND (effective_until IS NULL OR effective_until > $2)
		ORDER BY effective_from DESC,version_id`, scope, at)
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
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
