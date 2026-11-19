package status

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/lib/pq"
)

const (
	Investigating = "investigating"
	Identified    = "identified"
	Monitoring    = "monitoring"
	Resolved      = "resolved"
)

type IncidentSummary struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	State        string     `json:"state"`
	Severity     string     `json:"severity"`
	ComponentIDs []string   `json:"component_ids"`
	StartedAt    time.Time  `json:"started_at"`
	ResolvedAt   *time.Time `json:"resolved_at"`
}

type Incident struct {
	IncidentSummary
	Updates []IncidentUpdate `json:"updates"`
}

type IncidentUpdate struct {
	At      time.Time `json:"at"`
	State   string    `json:"state"`
	Message string    `json:"message"`
}

type PublishedIncident struct {
	Incident
	Version int64 `json:"version,string"`
}

type CreateIncident struct {
	Title        string   `json:"title"`
	Severity     string   `json:"severity"`
	ComponentIDs []string `json:"component_ids"`
	Message      string   `json:"message"`
}

type PostUpdate struct {
	State   string `json:"state"`
	Message string `json:"message"`
}

type IncidentSeek struct {
	StartedAt time.Time `json:"at"`
	ID        string    `json:"id"`
}

type IncidentPage struct {
	From, To *time.Time
	AsOf     time.Time
	After    *IncidentSeek
	Limit    int
}

func validText(v string, bound int, allowLines bool) bool {
	if v == "" || !utf8.ValidString(v) || utf8.RuneCountInString(v) > bound {
		return false
	}
	return !strings.ContainsFunc(v, func(r rune) bool { return unicode.IsControl(r) && !(allowLines && r == '\n') })
}

func ValidIncidentID(v string) bool {
	if len(v) != 36 {
		return false
	}
	id, err := uuid.Parse(v)
	return err == nil && id.String() == v
}

func (s *Store) CreateIncident(ctx context.Context, actor int64, key string, in CreateIncident) (PublishedIncident, error) {
	in.Title, in.Message = strings.TrimSpace(in.Title), strings.TrimSpace(in.Message)
	if !validText(in.Title, 160, false) {
		return PublishedIncident{}, fault.Invalid("/title")
	}
	if !validText(in.Message, 2000, true) {
		return PublishedIncident{}, fault.Invalid("/message")
	}
	if !slices.Contains([]string{Degraded, Outage, Maintenance}, in.Severity) {
		return PublishedIncident{}, fault.Invalid("/severity")
	}
	if len(in.ComponentIDs) == 0 || len(in.ComponentIDs) > len(components) {
		return PublishedIncident{}, fault.Invalid("/component_ids")
	}
	for _, id := range in.ComponentIDs {
		if !slices.ContainsFunc(components, func(spec componentSpec) bool { return spec.id == id }) {
			return PublishedIncident{}, fault.Invalid("/component_ids")
		}
	}
	slices.Sort(in.ComponentIDs)
	if len(slices.Compact(in.ComponentIDs)) != len(in.ComponentIDs) {
		return PublishedIncident{}, fault.Invalid("/component_ids")
	}
	return s.publish(ctx, actor, "create", key, in, func(tx *sql.Tx, at time.Time) (string, int64, error) {
		id := uuid.NewString()
		_, err := tx.ExecContext(ctx, `INSERT INTO public_status_incidents
		    (id,title,severity,component_ids,state,started_at,updated_at,version)
		    VALUES ($1,$2,$3,$4,'investigating',$5,$5,1)`, id, in.Title, in.Severity, pq.Array(in.ComponentIDs), at)
		if err != nil {
			return "", 0, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO public_status_incident_updates (incident_id,version,published_at,state,message)
		    VALUES ($1,1,$2,'investigating',$3)`, id, at, in.Message)
		return id, 1, err
	})
}

var badTransition = &fault.Fault{Status: 409, Reason: "INVALID_INCIDENT_TRANSITION", Message: "The incident cannot move to that state."}

func incidentStage(state string) int {
	switch state {
	case Investigating:
		return 1
	case Identified:
		return 2
	case Monitoring:
		return 3
	case Resolved:
		return 4
	default:
		return 0
	}
}

func (s *Store) PostUpdate(ctx context.Context, actor int64, id string, expected int64, key string, in PostUpdate) (PublishedIncident, error) {
	if !ValidIncidentID(id) {
		return PublishedIncident{}, fault.NotFound
	}
	in.Message = strings.TrimSpace(in.Message)
	if !validText(in.Message, 2000, true) {
		return PublishedIncident{}, fault.Invalid("/message")
	}
	if incidentStage(in.State) == 0 {
		return PublishedIncident{}, fault.Invalid("/state")
	}
	payload := struct {
		Version int64
		PostUpdate
	}{expected, in}
	return s.publish(ctx, actor, "update/"+id, key, payload, func(tx *sql.Tx, at time.Time) (string, int64, error) {
		var state string
		var version int64
		err := tx.QueryRowContext(ctx, `SELECT state,version FROM public_status_incidents WHERE id = $1`, id).Scan(&state, &version)
		if errors.Is(err, sql.ErrNoRows) {
			return "", 0, fault.NotFound
		}
		if err != nil {
			return "", 0, err
		}
		if version != expected {
			return "", 0, fault.Stale(version)
		}
		if state == Resolved || incidentStage(in.State) < incidentStage(state) {
			return "", 0, badTransition
		}
		_, err = tx.ExecContext(ctx, `UPDATE public_status_incidents SET
		    state = $2, updated_at = $3, version = version + 1,
		    resolved_at = CASE WHEN $2 = 'resolved' THEN $3 ELSE NULL END
		    WHERE id = $1`, id, in.State, at)
		if err != nil {
			return "", 0, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO public_status_incident_updates (incident_id,version,published_at,state,message)
		    VALUES ($1,$2,$3,$4,$5)`, id, version+1, at, in.State, in.Message)
		return id, version + 1, err
	})
}

func (s *Store) publish(ctx context.Context, actor int64, route, key string, payload any,
	write func(*sql.Tx, time.Time) (string, int64, error)) (PublishedIncident, error) {
	var result PublishedIncident
	if actor <= 0 {
		return result, fault.Unauthenticated
	}
	if !validText(key, 200, false) || len(key) > 200 || strings.TrimSpace(key) != key {
		return result, fault.Invalid("/Idempotency-Key")
	}
	keyHash := sha256.Sum256([]byte(key))
	content, err := json.Marshal(payload)
	if err != nil {
		return result, err
	}
	fingerprint := sha256.Sum256(content)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	// Serialize the rare publication writes so the public timestamp also
	// serves as a committed cutoff for incident list cursors.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, incidentLock); err != nil {
		return result, err
	}
	var id string
	var stored []byte
	err = tx.QueryRowContext(ctx, `SELECT incident_id,version,fingerprint FROM status_incident_commands
	    WHERE actor_id = $1 AND route = $2 AND key_hash = $3`, actor, route, keyHash[:]).Scan(&id, &result.Version, &stored)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		var at time.Time
		err = tx.QueryRowContext(ctx, `SELECT greatest(clock_timestamp(), (
		    SELECT updated_at + interval '1 microsecond' FROM public_status_incidents ORDER BY updated_at DESC LIMIT 1))`).Scan(&at)
		if err != nil {
			return result, err
		}
		id, result.Version, err = write(tx, at.UTC())
		if err != nil {
			return result, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO status_incident_commands
		    (actor_id, route, key_hash, fingerprint, incident_id, version) VALUES ($1,$2,$3,$4,$5,$6)`,
			actor, route, keyHash[:], fingerprint[:], id, result.Version)
		if err != nil {
			return result, err
		}
	case err != nil:
		return result, err
	case !slices.Equal(stored, fingerprint[:]):
		return result, fault.Conflict
	}
	result.Incident, _, err = readIncident(ctx, tx, id, result.Version)
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}

const incidentColumns = `i.id::text, i.title, i.state, i.severity, i.component_ids, i.started_at, i.resolved_at`

func scanIncident(scan func(...any) error) (IncidentSummary, error) {
	var i IncidentSummary
	var resolved sql.NullTime
	err := scan(&i.ID, &i.Title, &i.State, &i.Severity, pq.Array(&i.ComponentIDs), &i.StartedAt, &resolved)
	i.StartedAt, i.ResolvedAt = i.StartedAt.UTC(), utcPointer(resolved)
	return i, err
}

func (s *Store) Incident(ctx context.Context, id string) (Incident, time.Time, error) {
	if !ValidIncidentID(id) {
		return Incident{}, time.Time{}, fault.NotFound
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return Incident{}, time.Time{}, err
	}
	defer tx.Rollback()
	out, at, err := readIncident(ctx, tx, id, 0)
	if err != nil {
		return out, at, err
	}
	return out, at, tx.Commit()
}

func readIncident(ctx context.Context, tx *sql.Tx, id string, throughVersion int64) (Incident, time.Time, error) {
	var i Incident
	var err error
	i.IncidentSummary, err = scanIncident(tx.QueryRowContext(ctx, `SELECT `+incidentColumns+`
	    FROM public_status_incidents i WHERE i.id = $1`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return i, time.Time{}, fault.NotFound
	}
	if err != nil {
		return i, time.Time{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT published_at,state,message FROM public_status_incident_updates
	    WHERE incident_id = $1 AND ($2 = 0 OR version <= $2) ORDER BY version`, id, throughVersion)
	if err != nil {
		return i, time.Time{}, err
	}
	defer rows.Close()
	var last IncidentUpdate
	for rows.Next() {
		if err = rows.Scan(&last.At, &last.State, &last.Message); err != nil {
			return i, time.Time{}, err
		}
		last.At = last.At.UTC()
		i.Updates = append(i.Updates, last)
	}
	if err = rows.Err(); err != nil {
		return i, time.Time{}, err
	}
	if len(i.Updates) == 0 {
		return i, time.Time{}, errors.New("published incident has no updates")
	}
	i.State, i.ResolvedAt = last.State, nil
	if last.State == Resolved {
		i.ResolvedAt = &last.At
	}
	return i, last.At, nil
}

func latestPublication(ctx context.Context, tx *sql.Tx) (time.Time, error) {
	var updated sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT (SELECT updated_at
	    FROM public_status_incidents ORDER BY updated_at DESC LIMIT 1)`).Scan(&updated)
	return updated.Time.UTC(), err
}

// ListIncidents returns up to limit+1 summaries so the transport can emit a seek cursor.
func (s *Store) ListIncidents(ctx context.Context, p IncidentPage, view time.Time) ([]IncidentSummary, time.Time, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, time.Time{}, err
	}
	defer tx.Rollback()
	if p.AsOf.IsZero() {
		p.AsOf, err = latestPublication(ctx, tx)
		if err != nil {
			return nil, time.Time{}, err
		}
		if p.AsOf.IsZero() {
			return []IncidentSummary{}, view, tx.Commit()
		}
	}
	var afterAt, afterID any
	if p.After != nil {
		afterAt, afterID = p.After.StartedAt, p.After.ID
	}
	rows, err := tx.QueryContext(ctx, `SELECT i.id::text, i.title, u.state, i.severity, i.component_ids, i.started_at,
	    CASE WHEN u.state = 'resolved' THEN u.published_at END
	  FROM public_status_incidents i
	  CROSS JOIN LATERAL (
	    SELECT published_at, state FROM public_status_incident_updates WHERE incident_id = i.id AND published_at <= $1
	    ORDER BY published_at DESC LIMIT 1
	  ) u
	  WHERE i.started_at <= $1 AND ($2::timestamptz IS NULL OR i.started_at >= $2)
	    AND ($3::timestamptz IS NULL OR i.started_at < $3)
	    AND ($4::timestamptz IS NULL OR (i.started_at,i.id) < ($4,$5::uuid))
	  ORDER BY i.started_at DESC,i.id DESC LIMIT $6`, p.AsOf, p.From, p.To, afterAt, afterID, p.Limit+1)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer rows.Close()
	items := make([]IncidentSummary, 0, p.Limit+1)
	for rows.Next() {
		i, err := scanIncident(rows.Scan)
		if err != nil {
			return nil, time.Time{}, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, err
	}
	return items, p.AsOf, tx.Commit()
}
