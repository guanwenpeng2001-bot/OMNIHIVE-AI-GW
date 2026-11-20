//go:build integration

package export

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/audit"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/baggage"
)

type testEgress func(*http.Request) (*http.Response, error)

func (e testEgress) Do(r *http.Request) (*http.Response, error) { return e(r) }

func response(status int) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("prompt-canary / credential-canary"))}
}

type fixture struct {
	store       *Store
	caller      Caller
	project     int64
	destination Destination
	config      Config
}

func setup(t *testing.T, db *sql.DB, egress Egress) fixture {
	t.Helper()
	ctx := t.Context()
	var f fixture
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO workspaces(name) VALUES('export fixture') RETURNING id").Scan(&f.caller.WorkspaceID))
	t.Cleanup(func() {
		_, err := db.Exec("DELETE FROM workspaces WHERE id=$1", f.caller.WorkspaceID)
		require.NoError(t, err)
	})
	require.NoError(t, db.QueryRowContext(ctx, "SELECT id FROM projects WHERE workspace_id=$1 AND is_default", f.caller.WorkspaceID).Scan(&f.project))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id",
		uuid.NewString()+"@example.test").Scan(&f.caller.ActorID))
	_, err := db.ExecContext(ctx, "INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin')",
		f.caller.WorkspaceID, f.caller.ActorID)
	require.NoError(t, err)

	f.store = New(db, postgres.New(db), egress)
	f.destination, err = f.store.RegisterDestination(ctx, f.caller, "registered", "https://collector.example.test/credential-canary")
	require.NoError(t, err)
	f.config, err = f.store.Create(ctx, f.caller, "approved", CreateInput{
		DestinationID: f.destination.ID, DataClasses: []string{timings}, SampleRate: "1", RetentionDays: 7,
	})
	require.NoError(t, err)
	require.Equal(t, stateValidating, f.config.State)
	require.NoError(t, f.store.deliverOne(ctx))
	detail, err := f.store.Get(ctx, f.caller, f.config.ID)
	require.NoError(t, err)
	require.Equal(t, stateActive, detail.State)
	require.Nil(t, detail.LastSuccessAt)
	return f
}

func (f fixture) observe() audit.Observation {
	return audit.Observation{
		Scope: audit.Scope{Actor: f.caller.ActorID, Workspace: f.caller.WorkspaceID, Project: f.project},
		At:    time.Now().UTC(), Write: true, Success: false, Duration: 126 * time.Millisecond,
	}
}

func (f fixture) detail(t *testing.T) Detail {
	t.Helper()
	detail, err := f.store.Get(t.Context(), f.caller, f.config.ID)
	require.NoError(t, err)
	return detail
}

func TestWorkspaceTelemetryPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("requires an isolated job_test PostgreSQL database")
	}
	db, err := testresources.OpenPostgres(t.Context(), dsn, "job_test")
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, repository.ApplyMigrations(t.Context(), db))

	t.Run("scope sampling payload and stable lease", func(t *testing.T) {
		var calls []string
		var expiry time.Time
		f := setup(t, db, testEgress(func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.Equal(t, http.MethodPost, req.Method)
			require.Nil(t, req.GetBody)
			require.Empty(t, req.Header.Get("Authorization"))
			require.Empty(t, req.Header.Get("Baggage"))
			require.Zero(t, baggage.FromContext(req.Context()).Len())
			if req.Header.Get("X-OmniHive-Telemetry-Validation") == "1" {
				require.JSONEq(t, "{}", string(body))
				return response(200), nil
			}
			require.JSONEq(t, `{"operation":"scope.write","success":false,"latency_ms":126}`, string(body))
			expiry, err = time.Parse(time.RFC3339Nano, req.Header.Get("X-OmniHive-Telemetry-Expires-At"))
			require.NoError(t, err)
			id := req.Header.Get("Idempotency-Key")
			_, err = uuid.Parse(id)
			require.NoError(t, err)
			calls = append(calls, id)
			if len(calls) == 1 {
				return nil, errors.New("credential-canary https://collector.example.test")
			}
			return response(200), nil
		}))
		v := f.observe()
		outside := v
		outside.Scope.Actor += 1_000_000
		require.NoError(t, f.store.ingest(t.Context(), outside))
		outside = v
		outside.Scope.Project += 1_000_000
		require.NoError(t, f.store.ingest(t.Context(), outside))
		require.Zero(t, f.detail(t).QueueDepth)

		ctx, stop := context.WithCancel(t.Context())
		defer stop()
		observer := audit.NewObserver()
		ingestErrors := make(chan struct{}, 1)
		go observer.RunWithExport(ctx, nil, nil, f.store.Offer)
		go f.store.RunIngest(ctx, func() {
			select {
			case ingestErrors <- struct{}{}:
			default:
			}
		})
		require.True(t, observer.Offer(v))
		require.Eventually(t, func() bool { return f.detail(t).QueueDepth == 1 }, 3*time.Second, 10*time.Millisecond)
		stop()
		require.Empty(t, ingestErrors)
		require.NoError(t, f.store.ingest(t.Context(), v))
		require.Equal(t, 1, f.detail(t).QueueDepth)
		require.NoError(t, f.store.deliverOne(t.Context()))
		require.WithinDuration(t, v.At.Add(7*24*time.Hour), expiry, time.Microsecond)
		detail := f.detail(t)
		require.Equal(t, 1, detail.QueueDepth)
		require.Equal(t, reasonUnavailable, *detail.LastErrorReason)

		advance := func() {
			t.Helper()
			_, err := db.ExecContext(t.Context(), "UPDATE telemetry_export_jobs SET ready_at=now()-interval '1 second' WHERE export_id=$1 AND slot>0", f.config.ID)
			require.NoError(t, err)
		}
		advance()
		stale, err := f.store.take(t.Context())
		require.NoError(t, err)
		advance()
		current, err := f.store.take(t.Context())
		require.NoError(t, err)
		require.Equal(t, stale.id, current.id)
		require.NotEqual(t, stale.leaseID, current.leaseID)
		require.NoError(t, f.store.deliver(t.Context(), stale))
		require.Len(t, calls, 1)
		require.NoError(t, f.store.deliver(t.Context(), current))
		require.Equal(t, []string{calls[0], calls[0]}, calls)
		detail = f.detail(t)
		require.Zero(t, detail.QueueDepth)
		require.Nil(t, detail.LastErrorReason)
		require.NotNil(t, detail.LastSuccessAt)
		require.NoError(t, f.store.ingest(t.Context(), v))
		require.Zero(t, f.detail(t).QueueDepth)
		var erased bool
		require.NoError(t, db.QueryRow("SELECT slot IS NULL AND operation IS NULL AND success IS NULL AND latency_ms IS NULL FROM telemetry_export_jobs WHERE id=$1",
			current.id).Scan(&erased))
		require.True(t, erased)

		require.NoError(t, f.store.ingest(t.Context(), f.observe()))
		_, err = db.Exec("UPDATE telemetry_export_jobs SET expires_at=now()-interval '1 second' WHERE export_id=$1", f.config.ID)
		require.NoError(t, err)
		require.NoError(t, f.store.deliverOne(t.Context()))
		require.Len(t, calls, 2)
		require.NoError(t, f.store.purgeExpired(t.Context()))
		var count int
		require.NoError(t, db.QueryRow("SELECT count(*) FROM telemetry_export_jobs WHERE export_id=$1", f.config.ID).Scan(&count))
		require.Zero(t, count)
	})

	t.Run("bounded queue and bounded retries", func(t *testing.T) {
		var attempts []string
		f := setup(t, db, testEgress(func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("X-OmniHive-Telemetry-Validation") == "1" {
				return response(204), nil
			}
			attempts = append(attempts, req.Header.Get("Idempotency-Key"))
			return response(http.StatusTooManyRequests), nil
		}))
		off := "0"
		reconfigured, err := f.store.Change(t.Context(), f.caller, f.config.ID, f.config.Version, PatchInput{SampleRate: &off})
		require.NoError(t, err)
		require.NoError(t, f.store.deliverOne(t.Context()))
		require.NoError(t, f.store.ingest(t.Context(), f.observe()))
		require.Zero(t, f.detail(t).QueueDepth)
		full := "1"
		_, err = f.store.Change(t.Context(), f.caller, f.config.ID, reconfigured.Version, PatchInput{SampleRate: &full})
		require.NoError(t, err)
		require.NoError(t, f.store.deliverOne(t.Context()))

		o := f.observe()
		// Make the first record unique and repeatedly due while the others are queued.
		require.NoError(t, f.store.ingest(t.Context(), o))
		targets, err := f.store.sampleTargets(t.Context(), f.caller.WorkspaceID, o.At)
		require.NoError(t, err)
		require.Len(t, targets, 1)
		first := deliveryID(targets[0], o)
		for i := 1; i < maxPending+4; i++ {
			other := o
			other.At = other.At.Add(time.Duration(i) * time.Millisecond)
			require.NoError(t, f.store.ingest(t.Context(), other))
		}
		require.Equal(t, maxPending, f.detail(t).QueueDepth)
		require.EqualValues(t, 4, f.store.Dropped())
		for range maxAttempts {
			// Keep the same job first regardless of how long filling the queue took.
			res, err := db.Exec("UPDATE telemetry_export_jobs SET ready_at='-infinity' WHERE id=$1", first)
			require.NoError(t, err)
			updated, err := res.RowsAffected()
			require.NoError(t, err)
			require.EqualValues(t, 1, updated)
			require.NoError(t, f.store.deliverOne(t.Context()))
		}
		require.Len(t, attempts, maxAttempts)
		for _, id := range attempts {
			require.Equal(t, first.String(), id)
		}
		detail := f.detail(t)
		require.Equal(t, reasonExhausted, *detail.LastErrorReason)
		require.Equal(t, maxPending-1, detail.QueueDepth)
		_, err = db.Exec("UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2", f.caller.WorkspaceID, f.caller.ActorID)
		require.NoError(t, err)
		require.NoError(t, f.store.deliverOne(t.Context()))
		require.Len(t, attempts, maxAttempts)
		var state, reason string
		require.NoError(t, db.QueryRow("SELECT state,last_error_reason FROM telemetry_exports WHERE id=$1", f.config.ID).Scan(&state, &reason))
		require.Equal(t, stateDisabled, state)
		require.Equal(t, reasonApprovalRevoked, reason)
		require.ErrorIs(t, func() error { _, err := f.store.Get(t.Context(), f.caller, f.config.ID); return err }(), fault.Forbidden)
	})

	t.Run("destination revocation drains delivery without using financial tables", func(t *testing.T) {
		started, unblock := make(chan struct{}), make(chan struct{})
		f := setup(t, db, testEgress(func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("X-OmniHive-Telemetry-Validation") == "1" {
				return response(200), nil
			}
			close(started)
			select {
			case <-unblock:
				return response(200), nil
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}))
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer tx.Rollback()
		_, err = tx.ExecContext(ctx, "LOCK TABLE billing_settlement_outbox, usage_settlement_receipts IN ACCESS EXCLUSIVE MODE NOWAIT")
		require.NoError(t, err)
		require.NoError(t, f.store.ingest(ctx, f.observe()))
		require.NoError(t, f.store.ingest(ctx, f.observe()))
		delivered := make(chan error, 1)
		go func() { delivered <- f.store.deliverOne(ctx) }()
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("telemetry delivery waited for a financial lock")
		}

		revoked := make(chan error, 1)
		go func() {
			_, err := f.store.RevokeDestination(ctx, f.caller, f.destination.ID, f.destination.Version)
			revoked <- err
		}()
		select {
		case err := <-revoked:
			t.Fatalf("destination revoked before the in-flight delivery drained: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
		close(unblock)
		require.NoError(t, <-delivered)
		require.NoError(t, <-revoked)
		require.NoError(t, tx.Rollback())
		detail := f.detail(t)
		require.Equal(t, stateDisabled, detail.State)
		require.Equal(t, reasonDestinationRevoked, *detail.LastErrorReason)
		require.EqualValues(t, 2, detail.Version)
		require.Zero(t, detail.QueueDepth)
		var erased bool
		require.NoError(t, db.QueryRow("SELECT endpoint IS NULL FROM telemetry_export_destinations WHERE id=$1", f.destination.ID).Scan(&erased))
		require.True(t, erased)
		require.NoError(t, f.store.deliverOne(t.Context()))
	})
}
