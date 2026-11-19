package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/lib/pq"
)

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db} }

func (s *Store) ImportRegistry(ctx context.Context, version string, models []catalog.RegistryModel) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var inserted string
	err = tx.QueryRowContext(ctx, `INSERT INTO catalog_registry_versions(version) VALUES ($1) ON CONFLICT DO NOTHING RETURNING version`, version).Scan(&inserted)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	payload, err := json.Marshal(models)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO catalog_capability_evidence(source,subject,version,maturity,payload)
	   SELECT 'approved_registry', (row->>'Provider')||':'||(row->>'Model'), $1||':'||encode(sha256(convert_to(row::text,'UTF8')),'hex'),
	          row->'Facts'->>'maturity',row
	   FROM jsonb_array_elements($2::jsonb) row ON CONFLICT DO NOTHING`, version, string(payload))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Publish(ctx context.Context, v catalog.Snapshot, registryVersion string, now time.Time) error {
	if err := validatePublic(v); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `WITH published AS (
	    INSERT INTO catalog_publications(version,published_at,expires_at,registry_version,payload)
	    VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING RETURNING version
	), provenance AS (
	    INSERT INTO catalog_capability_evidence(source,subject,version,maturity,payload)
	    SELECT 'legacy_visibility','public',version,'unknown',$6 FROM published
	)
	INSERT INTO catalog_publication_outbox(version) SELECT version FROM published`, v.Version, now, now.Add(time.Hour), registryVersion, string(data), visibilityProvenance(v))
	return err
}

func validatePublic(v catalog.Snapshot) error {
	for _, d := range v.Details {
		if d.Model.Scope != nil || len(d.Endpoints) != 0 || d.Model.AccessState != "requires_scope" ||
			d.Model.RouteState != "requires_scope" || d.Metrics.Requests != nil || d.Metrics.TTFTSamples != nil ||
			d.Metrics.SpeedSamples != nil || d.Metrics.SuccessRate != nil || d.Metrics.TTFT != nil || d.Metrics.Speed != nil ||
			(d.PriceSource != "unavailable" && d.PriceSource != "public_plaza_reference") {
			return fmt.Errorf("catalog publication is outside the public projection")
		}
		for _, f := range d.Facts {
			if f.Source != "approved_registry" {
				return fmt.Errorf("catalog publication includes private evidence")
			}
		}
		for _, c := range d.Capabilities {
			if c.Source != "approved_registry" {
				return fmt.Errorf("catalog publication includes private capability")
			}
		}
	}
	return nil
}

func visibilityProvenance(v catalog.Snapshot) string {
	models := make([]string, 0, len(v.Details))
	for _, d := range v.Details {
		models = append(models, d.Model.ID)
	}
	raw, _ := json.Marshal(models)
	return string(raw)
}

type Resource struct {
	Kind, Platform, AccountType string
	ID                          int64
}

func (s *Store) ProjectResources(ctx context.Context, p catalog.Perspective) ([]Resource, error) {
	rows, err := s.db.QueryContext(ctx, `WITH owned AS (
	    SELECT 'accounts' kind,id,platform,type FROM accounts WHERE workspace_id=$1 AND deleted_at IS NULL
	    UNION ALL SELECT 'groups',id,'','' FROM groups WHERE workspace_id=$1 AND deleted_at IS NULL
	    UNION ALL SELECT 'channels',id,'','' FROM channels WHERE workspace_id=$1
	) SELECT o.kind,o.id,o.platform,o.type FROM owned o
	JOIN projects d ON d.workspace_id=$1 AND d.is_default
	LEFT JOIN project_resources r ON r.workspace_id=$1 AND r.kind=o.kind AND r.resource_id=o.id
	WHERE coalesce(r.project_id,d.id)=$2
	  AND NOT EXISTS (SELECT FROM identity_scope_quarantine q WHERE q.kind=o.kind AND q.resource_id=o.id)
	ORDER BY o.kind,o.id`, p.Workspace, p.Project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Resource{}
	for rows.Next() {
		var resource Resource
		if err := rows.Scan(&resource.Kind, &resource.ID, &resource.Platform, &resource.AccountType); err != nil {
			return nil, err
		}
		out = append(out, resource)
	}
	return out, rows.Err()
}

type EndpointEvidence struct {
	Provider, PrivacyMode, Region string
	ForceGlobal                   bool
	LegacyAttempt                 string
	LegacyResult                  string
	VerificationAt                time.Time
	VerificationExpiry            time.Time
	VerifiedModels                []string
	ConnectionActive              bool
	VerificationFailed            bool
}

func (s *Store) EndpointEvidence(ctx context.Context, p catalog.Perspective, accounts []int64) (map[int64]EndpointEvidence, error) {
	out := make(map[int64]EndpointEvidence, len(accounts))
	if len(accounts) == 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT a.id,a.platform,coalesce(a.extra->>'privacy_mode',''),
	    CASE WHEN a.type='bedrock' THEN coalesce(a.credentials->>'aws_region','') ELSE '' END,
	    coalesce(a.credentials->>'aws_force_global'='true',false),
	    coalesce(a.extra->'upstream_model_catalog_state'->>'attempted_at',''),
	    coalesce(a.extra->'upstream_model_catalog_state'->>'result',''),
	    coalesce(pc.state='active' AND NOT pc.legacy_dirty,false),
	    v.expires_at,coalesce(v.snapshot->>'verified_at',''),coalesce(v.snapshot->'models','[]'::jsonb),
	    coalesce(pc.verified_id IS NULL AND v.id IS NOT NULL,false)
	FROM accounts a
	LEFT JOIN provider_connections pc ON pc.workspace_id=a.workspace_id AND pc.account_id=a.id
	LEFT JOIN LATERAL (
	    SELECT v.id,v.snapshot,v.expires_at
	    FROM connection_verifications v
	    WHERE v.workspace_id=pc.workspace_id AND v.account_id=pc.account_id AND v.eligible
	      AND (v.id=pc.verified_id OR (pc.verified_id IS NULL AND EXISTS (
	          SELECT FROM connection_verifications failed WHERE failed.workspace_id=v.workspace_id AND failed.account_id=v.account_id
	            AND NOT failed.eligible AND failed.base_version=pc.version AND failed.credential_version=v.credential_version
	            AND failed.proxy_revision=v.proxy_revision AND failed.expires_at>v.expires_at)))
	      AND (pc.proxy_id IS NULL AND v.proxy_revision='' OR EXISTS (
	          SELECT FROM proxies proxy WHERE proxy.id=pc.proxy_id AND proxy.workspace_id=pc.workspace_id
	            AND proxy.updated_at::text=v.proxy_revision AND proxy.deleted_at IS NULL AND proxy.status='active'
	            AND (proxy.expires_at IS NULL OR proxy.expires_at>now())))
	      AND EXISTS (SELECT FROM credential_secrets cs WHERE cs.account_id=a.id AND cs.workspace_id=a.workspace_id
	          AND cs.version=v.credential_version AND cs.revoked_at IS NULL)
	    ORDER BY v.expires_at DESC, v.id LIMIT 1
	) v ON pc.state='active' AND NOT pc.legacy_dirty
	WHERE a.workspace_id=$1 AND a.id=ANY($2) AND a.deleted_at IS NULL`, p.Workspace, pq.Array(accounts))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var e EndpointEvidence
		var expires sql.NullTime
		var verifiedAt string
		var models []byte
		if err := rows.Scan(&id, &e.Provider, &e.PrivacyMode, &e.Region, &e.ForceGlobal, &e.LegacyAttempt, &e.LegacyResult,
			&e.ConnectionActive, &expires, &verifiedAt, &models, &e.VerificationFailed); err != nil {
			return nil, err
		}
		e.VerificationExpiry = expires.Time
		e.VerificationAt, _ = time.Parse(time.RFC3339Nano, verifiedAt)
		if err := json.Unmarshal(models, &e.VerifiedModels); err != nil {
			return nil, err
		}
		out[id] = e
	}
	return out, rows.Err()
}
