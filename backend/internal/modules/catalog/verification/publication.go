package verification

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/lib/pq"
)

func (s *Store) Publish(ctx context.Context, actor int64, rawID, key string, expected int64, in Publish) (Publication, error) {
	if err := s.authorize(ctx, actor, PublishGrant); err != nil {
		return Publication{}, err
	}
	if err := validateKey(key); err != nil {
		return Publication{}, err
	}
	targetID, err := prefixedID(rawID, "vt_")
	if err != nil {
		return Publication{}, err
	}
	if in.SunsetAt != nil {
		utc := in.SunsetAt.UTC()
		in.SunsetAt = &utc
	}
	cmd := newCommand(actor, "publish", key, struct {
		Target  string
		Version int64
		Publish
	}{rawID, expected, in})
	tx, err := s.beginMutation(ctx)
	if err != nil {
		return Publication{}, err
	}
	defer tx.Rollback()
	id, err := cmd.replay(ctx, tx)
	if err != nil {
		return Publication{}, err
	}
	if id != uuid.Nil {
		return scanPublication(tx.QueryRowContext(ctx, publicationSelect+" WHERE p.id=$1", id))
	}
	target, err := loadTarget(ctx, tx, targetID)
	if err != nil {
		return Publication{}, err
	}
	cell := target.cellID()
	current, err := currentPublication(ctx, tx, cell)
	if err != nil {
		return Publication{}, err
	}
	var version int64
	if current != nil {
		version = current.Version
	}
	if version != expected {
		return Publication{}, fault.Stale(version)
	}
	now, err := databaseTime(ctx, tx)
	if err != nil {
		return Publication{}, err
	}

	var next Publication
	switch {
	case in.RollbackVersion != nil:
		next, err = rollbackPublication(ctx, tx, target, in, version)
	default:
		next, err = advancePublication(ctx, tx, target, targetID, current, in, now)
	}
	if err != nil {
		return Publication{}, err
	}
	nextTarget, _ := prefixedID(next.TargetID, "vt_")
	var evidenceID uuid.NullUUID
	if next.Evidence != nil {
		evidenceID.UUID, err = prefixedID(next.Evidence.ID, "ve_")
		if err != nil {
			return Publication{}, err
		}
		evidenceID.Valid = true
	}
	var reason *string
	var announced, sunset *time.Time
	if n := next.Deprecation; n != nil {
		reason, announced, sunset = &n.Reason, &n.AnnouncedAt, &n.SunsetAt
	}
	id = uuid.New()
	_, err = tx.ExecContext(ctx, `INSERT INTO catalog_compatibility_publications
		(id,cell_id,target_id,version,maturity,evidence_id,actor_id,published_at,
			deprecation_reason,announced_at,sunset_at,rollback_version)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, id, cell, nextTarget,
		version+1, next.Maturity, evidenceID, actor, now, reason, announced, sunset, next.RollbackVersion)
	if err != nil {
		return Publication{}, err
	}
	if err := cmd.save(ctx, tx, id); err != nil {
		return Publication{}, err
	}
	out, err := scanPublication(tx.QueryRowContext(ctx, publicationSelect+" WHERE p.id=$1", id))
	if err != nil {
		return Publication{}, err
	}
	return out, tx.Commit()
}

func newPublication(t Target, maturity string) Publication {
	return Publication{
		TargetID: t.ID, EntryID: t.EntryID,
		Compatibility: catalog.Compatibility{
			Action: t.Action, Protocol: t.Protocol, AccountMode: t.AccountMode, AdapterVersion: t.AdapterVersion, Maturity: maturity,
		},
	}
}

func rollbackPublication(ctx context.Context, q queryer, target Target, in Publish, currentVersion int64) (Publication, error) {
	if in.Maturity != "" || in.EvidenceID != "" || in.Reason != "" || in.SunsetAt != nil {
		return Publication{}, fault.Invalid("/rollback_version")
	}
	version := *in.RollbackVersion
	if version < 0 || version >= currentVersion {
		return Publication{}, fault.Invalid("/rollback_version")
	}
	out := newPublication(target, candidate)
	if version != 0 {
		var err error
		out, err = scanPublication(q.QueryRowContext(ctx, publicationSelect+" WHERE p.cell_id=$1 AND p.version=$2", target.cellID(), version))
		if errors.Is(err, sql.ErrNoRows) {
			err = fault.NotFound
		}
		if err != nil {
			return Publication{}, err
		}
	}
	out.RollbackVersion = &version
	return out, nil
}

func advancePublication(ctx context.Context, q queryer, target Target, targetID uuid.UUID, current *Publication, in Publish, now time.Time) (Publication, error) {
	prior := candidate
	if current != nil {
		prior = current.Maturity
	}
	out := newPublication(target, in.Maturity)
	switch in.Maturity {
	case experimental, stable:
		allowed := in.Maturity == experimental && (prior == candidate || prior == experimental) ||
			in.Maturity == stable && (prior == experimental || prior == stable)
		if !allowed {
			return Publication{}, badTransition
		}
		if in.Reason != "" || in.SunsetAt != nil {
			return Publication{}, fault.Invalid("/deprecation_reason")
		}
		obs, err := publicationObservation(ctx, q, target, targetID, in.EvidenceID, now, in.Maturity == stable)
		if err != nil {
			return Publication{}, err
		}
		out.Evidence = obs
	case deprecated:
		if prior != stable || current.TargetID != target.ID {
			return Publication{}, badTransition
		}
		if in.EvidenceID != "" || !slices.Contains([]string{"provider_retirement", "adapter_retirement", "superseded"}, in.Reason) {
			return Publication{}, fault.Invalid("/deprecation_reason")
		}
		if in.SunsetAt == nil || !in.SunsetAt.After(now) {
			return Publication{}, fault.Invalid("/sunset_at")
		}
		out.Evidence = current.Evidence
		out.Deprecation = &catalog.DeprecationNotice{Reason: in.Reason, AnnouncedAt: now, SunsetAt: *in.SunsetAt}
	case closed:
		if prior != deprecated || current.TargetID != target.ID || now.Before(current.Deprecation.SunsetAt) {
			return Publication{}, badTransition
		}
		if in.EvidenceID != "" || in.Reason != "" || in.SunsetAt != nil {
			return Publication{}, fault.Invalid("/maturity")
		}
		out.Evidence, out.Deprecation = current.Evidence, current.Deprecation
	default:
		return Publication{}, fault.Invalid("/maturity")
	}
	return out, nil
}

func publicationObservation(ctx context.Context, q queryer, target Target, targetID uuid.UUID, evidenceID string, now time.Time, requireFixture bool) (*catalog.CompatibilityEvidence, error) {
	if evidenceID == "" {
		if requireFixture {
			return nil, evidenceRequired
		}
		return nil, nil
	}
	id, err := prefixedID(evidenceID, "ve_")
	if err != nil {
		return nil, fault.Invalid("/evidence_id")
	}
	obs, err := scanObservation(q.QueryRowContext(ctx, `SELECT `+observationFields+`
		FROM catalog_verification_evidence WHERE id=$1 AND target_id=$2`, id, targetID), target.Matrix, now)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fault.NotFound
	}
	if err != nil {
		return nil, err
	}
	e := obs.CompatibilityEvidence
	if !requireFixture {
		return &e, nil
	}
	if e.Source != liveOrigin || e.State != success || e.Freshness != "fresh" || e.FixtureID == nil {
		return nil, evidenceRequired
	}
	source, err := loadSource(ctx, q, target)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, evidenceRequired
	}
	if err != nil {
		return nil, err
	}
	if !source.implements(target.Matrix) {
		return nil, evidenceRequired
	}
	var matches bool
	err = q.QueryRowContext(ctx, `SELECT id=$2 AND source_revision=$3 FROM catalog_verification_evidence WHERE target_id=$1
		ORDER BY observed_at DESC,id DESC LIMIT 1`, targetID, id, source.revision).Scan(&matches)
	if err != nil {
		return nil, err
	}
	if !matches {
		return nil, evidenceRequired
	}
	return &e, nil
}

const publicationSelect = `SELECT p.version,p.maturity,p.published_at,p.rollback_version,p.target_id,
	t.entry_id,t.action,t.protocol,t.account_mode,t.adapter_version,
	e.id,coalesce(e.origin,''),coalesce(e.state,''),coalesce(e.sample_count,0),
	coalesce(e.window_start,p.published_at),coalesce(e.observed_at,p.published_at),coalesce(e.expires_at,p.published_at),e.fixture_id,
	p.deprecation_reason,p.announced_at,p.sunset_at,statement_timestamp()
	FROM catalog_compatibility_publications p
	JOIN catalog_verification_targets t ON t.id=p.target_id
	LEFT JOIN catalog_verification_evidence e ON e.id=p.evidence_id`

func scanPublication(row scanner) (Publication, error) {
	var p Publication
	var now time.Time
	var target uuid.UUID
	var evidence uuid.NullUUID
	var e catalog.CompatibilityEvidence
	var noticeReason sql.NullString
	var announced, sunset sql.NullTime
	err := row.Scan(&p.Version, &p.Maturity, &p.PublishedAt, &p.RollbackVersion, &target,
		&p.EntryID, &p.Action, &p.Protocol, &p.AccountMode, &p.AdapterVersion,
		&evidence, &e.Source, &e.State, &e.SampleCount, &e.WindowStart, &e.ObservedAt, &e.ExpiresAt, &e.FixtureID,
		&noticeReason, &announced, &sunset, &now)
	if err != nil {
		return Publication{}, err
	}
	p.TargetID, p.PublishedAt = "vt_"+target.String(), p.PublishedAt.UTC()
	if evidence.Valid {
		e.ID = "ve_" + evidence.UUID.String()
		e.WindowStart, e.ObservedAt, e.ExpiresAt = e.WindowStart.UTC(), e.ObservedAt.UTC(), e.ExpiresAt.UTC()
		e = e.At(now)
		p.Evidence = &e
	}
	if noticeReason.Valid {
		p.Deprecation = &catalog.DeprecationNotice{Reason: noticeReason.String, AnnouncedAt: announced.Time.UTC(), SunsetAt: sunset.Time.UTC()}
	}
	return p, nil
}

func currentPublication(ctx context.Context, q queryer, cell string) (*Publication, error) {
	p, err := scanPublication(q.QueryRowContext(ctx, publicationSelect+" WHERE p.cell_id=$1 ORDER BY p.version DESC LIMIT 1", cell))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ReadPublished takes the IDs already selected by the catalog's visibility
// policy. The immutable release controls exactly which observation can appear.
func ReadPublished(ctx context.Context, db *sql.DB, entries []string) (map[string][]catalog.Compatibility, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx, publicationSelect+` WHERE t.entry_id=ANY($1)
		AND p.version=(SELECT max(history.version) FROM catalog_compatibility_publications history WHERE history.cell_id=p.cell_id)
		AND p.maturity<>'candidate'
		ORDER BY t.entry_id,t.action,t.protocol,t.account_mode`, pq.Array(entries))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]catalog.Compatibility)
	for rows.Next() {
		p, err := scanPublication(rows)
		if err != nil {
			return nil, err
		}
		out[p.EntryID] = append(out[p.EntryID], p.Compatibility)
	}
	return out, rows.Err()
}
