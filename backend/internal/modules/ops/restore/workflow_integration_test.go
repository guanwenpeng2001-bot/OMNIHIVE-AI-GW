//go:build integration

package restore

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"os"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/ops/restore/testfixture"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

type grants struct{}

func (grants) Require(_ context.Context, a Actor) error {
	if a.Session != "verified" {
		return fault.Forbidden
	}
	return nil
}

func (grants) Evaluate(_ context.Context, actor, w, _ int64, _ string) (identity.Access, error) {
	if actor != 9 {
		return identity.Access{}, fault.Forbidden
	}
	return identity.Access{ActorID: actor, WorkspaceID: w, Actions: []string{PreviewAction, ExecuteAction}}, nil
}

type archives struct {
	data         []byte
	snapshots    map[string][]byte
	failSnapshot bool
}

func (a *archives) Record(context.Context, string) (json.RawMessage, error) {
	return json.RawMessage(`{"id":"backup"}`), nil
}
func (a *archives) Open(context.Context, json.RawMessage) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(a.data)), nil
}
func (a *archives) Snapshot(_ context.Context, k string, raw io.ReadCloser) (Snapshot, error) {
	b, err := io.ReadAll(raw)
	if err = errors.Join(err, raw.Close()); err != nil {
		return Snapshot{}, err
	}
	if a.failSnapshot {
		return Snapshot{}, errors.New("fixture snapshot unavailable")
	}
	a.snapshots[k] = b
	return Snapshot{Key: k, Digest: "sha256:" + strings.Repeat("1", 64)}, nil
}

func compress(t *testing.T, raw io.ReadCloser) []byte {
	t.Helper()
	var b bytes.Buffer
	g := gzip.NewWriter(&b)
	_, e := io.Copy(g, raw)
	require.NoError(t, e)
	require.NoError(t, raw.Close())
	require.NoError(t, g.Close())
	return b.Bytes()
}

type alterAfterRestore struct {
	Database
	db *sql.DB
}

func (d alterAfterRestore) Restore(ctx context.Context, r io.Reader) error {
	if e := d.Database.Restore(ctx, r); e != nil {
		return e
	}
	_, e := d.db.ExecContext(ctx, "UPDATE users SET value='different users, same count'")
	return e
}

type crashAfterCommit struct {
	Database
	stop context.CancelFunc
}

func (d crashAfterCommit) Restore(ctx context.Context, script io.Reader) error {
	if err := d.Database.Restore(ctx, script); err != nil {
		return err
	}
	d.stop()
	return nil
}

func TestFencedRestore(t *testing.T) {
	ctx := t.Context()
	journal, jcfg := testfixture.Database(t, "BE09_TEST_JOURNAL_DSN", "be09_journal_test")
	isolated, icfg := testfixture.Database(t, "BE09_TEST_ISOLATED_DSN", "be09_rehearsal_test")
	require.NoError(t, testresources.ValidatePostgres(os.Getenv("UPSTREAM_JOB_TEST_DSN"), "job_test"))
	prod, pcfg := testfixture.Database(t, "UPSTREAM_JOB_TEST_DSN", "job_test")
	for _, table := range tables {
		_, e := prod.Exec("CREATE TABLE " + table + " (id bigint PRIMARY KEY, value text NOT NULL); INSERT INTO " + table + " VALUES(1,'original')")
		require.NoError(t, e)
	}
	source := repository.NewPgDumper(pcfg, prod)
	raw, e := source.Dump(ctx)
	require.NoError(t, e)
	archive := &archives{data: compress(t, raw), snapshots: map[string][]byte{}}
	s := Service{Journal: journal, Access: grants{}, StepUp: grants{}, Archives: archive, Targets: map[string]Target{Production: {prod, source}, Isolated: {isolated, repository.NewPgDumper(icfg, isolated)}}}
	peer := s
	peer.Journal, e = sql.Open("postgres", jcfg.Database.DSN())
	require.NoError(t, e)
	defer peer.Journal.Close()
	bad := s
	bad.Journal = prod
	require.Error(t, bad.Initialize(ctx))
	require.NoError(t, s.Initialize(ctx))
	require.NoError(t, peer.Initialize(ctx))
	a := Actor{1, 9, "verified"}
	preview := func(key, target string) Preview {
		p, e := s.Preview(ctx, a, key, PreviewInput{BackupID: "backup", Target: target})
		require.NoError(t, e)
		return p
	}
	execute := func(p Preview, key, target, rehearsal string) Operation {
		o, e := s.Execute(ctx, a, key, ExecuteInput{p.ID, target, rehearsal})
		require.NoError(t, e)
		return o
	}
	p := preview("preview-a", Isolated)
	require.Equal(t, p, preview("preview-a", Isolated))
	_, e = s.Preview(ctx, a, "preview-a", PreviewInput{BackupID: "changed", Target: Isolated})
	require.ErrorIs(t, e, fault.Conflict)
	cmd := ExecuteInput{p.ID, Isolated, ""}
	var wg sync.WaitGroup
	ops := make([]Operation, 2)
	errs := make([]error, 2)
	for n, control := range []*Service{&s, &peer} {
		wg.Go(func() { ops[n], errs[n] = control.Execute(ctx, a, "execute-a", cmd) })
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, ops[0].ID, ops[1].ID)
	_, e = s.Execute(ctx, Actor{2, 9, "verified"}, "cross-scope", cmd)
	require.ErrorIs(t, e, fault.NotFound)
	_, e = s.Get(ctx, Actor{1, 2, ""}, ops[0].ID)
	require.ErrorIs(t, e, fault.Forbidden)
	_, e = s.ReadOperation(ctx, 2, 9, ops[0].ID)
	require.ErrorIs(t, e, fault.NotFound)
	_, e = s.Execute(ctx, a, "execute-a", ExecuteInput{"changed", Isolated, ""})
	require.ErrorIs(t, e, fault.Conflict)
	stale, e := s.claim(ctx)
	require.NoError(t, e)
	_, e = peer.claim(ctx)
	require.ErrorIs(t, e, sql.ErrNoRows)
	expire := func(id string) {
		_, e := journal.Exec(`UPDATE restore_journal.operations SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, id)
		require.NoError(t, e)
	}
	early := preview("early", Production)
	expire(stale.ID)
	worked, e := peer.Work(ctx)
	require.NoError(t, e)
	require.True(t, worked)
	current, e := s.Get(ctx, a, stale.ID)
	require.NoError(t, e)
	require.Equal(t, "succeeded", current.State)
	require.EqualValues(t, 2, current.Generation)
	stale.Stage = "verify_counts"
	require.ErrorIs(t, s.finish(ctx, stale, stale.Evidence, ""), ErrLeaseLost)
	require.ErrorIs(t, s.renew(ctx, stale), ErrLeaseLost)
	require.Contains(t, archive.snapshots, fmt.Sprintf("%s/2", stale.ID))
	var value string
	require.NoError(t, isolated.QueryRow("SELECT value FROM users").Scan(&value))
	require.Equal(t, "original", value)
	var n int
	require.NoError(t, isolated.QueryRow("SELECT count(*) FROM pg_namespace WHERE nspname='restore_journal'").Scan(&n))
	require.Zero(t, n)
	_, e = s.Execute(ctx, a, "early", ExecuteInput{early.ID, Production, current.ID})
	require.ErrorIs(t, e, prerequisite)
	prodPreview := preview("fresh", Production)
	_, e = s.Execute(ctx, Actor{1, 9, ""}, "prod", ExecuteInput{prodPreview.ID, Production, current.ID})
	require.ErrorIs(t, e, fault.Forbidden)
	prodOp := execute(prodPreview, "prod", Production, current.ID)
	_, e = prod.Exec("UPDATE users SET value='pre-cutover'")
	require.NoError(t, e)
	worked, e = s.Work(ctx)
	require.True(t, worked)
	require.NoError(t, e)
	completed, e := peer.Get(ctx, a, prodOp.ID)
	require.NoError(t, e)
	require.Equal(t, "succeeded", completed.State)
	require.NoError(t, prod.QueryRow("SELECT value FROM users").Scan(&value))
	require.Equal(t, "original", value)
	require.Contains(t, string(archive.snapshots[fmt.Sprintf("%s/1", prodOp.ID)]), "pre-cutover")
	iso := s.Targets[Isolated]
	s.Targets[Isolated] = Target{iso.DB, alterAfterRestore{iso.Database, iso.DB}}
	badOp := execute(preview("bad", Isolated), "bad", Isolated, "")
	worked, e = s.Work(ctx)
	require.True(t, worked)
	require.NoError(t, e)
	badOp, e = s.Get(ctx, a, badOp.ID)
	require.NoError(t, e)
	require.Equal(t, "failed", badOp.State)
	require.Equal(t, "VERIFICATION_FAILED", badOp.Report.Reason)
	_, e = s.Execute(ctx, a, "unverified", ExecuteInput{preview("refuse", Production).ID, Production, badOp.ID})
	require.ErrorIs(t, e, prerequisite)
	s.Targets[Isolated] = iso
	archive.failSnapshot = true
	noSnapshot := execute(preview("no-snapshot", Isolated), "no-snapshot", Isolated, "")
	worked, e = s.Work(ctx)
	require.True(t, worked)
	require.NoError(t, e)
	noSnapshot, e = s.Get(ctx, a, noSnapshot.ID)
	require.NoError(t, e)
	require.Equal(t, "failed", noSnapshot.State)
	require.Equal(t, "SNAPSHOT_FAILED", noSnapshot.Report.Reason)
	archive.failSnapshot = false
	require.NoError(t, isolated.QueryRow("SELECT value FROM users").Scan(&value))
	require.Equal(t, "different users, same count", value)
	crash := execute(preview("crash", Isolated), "crash", Isolated, "")
	interrupted, stop := context.WithCancel(ctx)
	defer stop()
	s.Targets[Isolated] = Target{iso.DB, crashAfterCommit{iso.Database, stop}}
	worked, e = s.Work(interrupted)
	require.True(t, worked)
	require.ErrorIs(t, e, context.Canceled)
	require.NoError(t, isolated.QueryRow("SELECT value FROM users").Scan(&value))
	require.Equal(t, "original", value)
	expire(crash.ID)
	_, e = peer.claim(ctx)
	require.ErrorIs(t, e, sql.ErrNoRows)
	uncertain, e := peer.Get(ctx, a, crash.ID)
	require.NoError(t, e)
	require.Equal(t, "unknown", uncertain.State)
	require.Equal(t, "applying", uncertain.Stage)

	_, e = s.Execute(ctx, a, "blocked", ExecuteInput{preview("blocked", Isolated).ID, Isolated, ""})
	require.ErrorIs(t, e, busy)
	require.NoError(t, journal.QueryRow("SELECT count(*) FROM restore_journal.events WHERE operation_id=$1 AND state='succeeded'", crash.ID).Scan(&n))
	require.Zero(t, n)
	op, e := peer.ReadOperation(ctx, 1, 9, current.ID)
	require.NoError(t, e)
	require.EqualValues(t, 9, op.OwnerID)
	require.NotNil(t, op.Result)
	require.NoError(t, s.Import(ctx, "legacy-interrupted", json.RawMessage(`{"id":"legacy-interrupted","status":"completed","restore_status":"review_required"}`), true))
	_, e = s.Execute(ctx, a, "legacy-review", ExecuteInput{preview("legacy-review", Production).ID, Production, current.ID})
	require.ErrorIs(t, e, busy)
	expired := preview("expired", Production)
	_, e = journal.Exec("UPDATE restore_journal.previews SET expires_at=$2 WHERE id=$1", expired.ID, time.Now().Add(-time.Hour))
	require.NoError(t, e)
	_, e = s.Execute(ctx, a, "expired", ExecuteInput{expired.ID, Production, current.ID})
	require.ErrorIs(t, e, prerequisite)
}
