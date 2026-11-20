package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	adminhandler "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/admin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func receiptRouter(f *credentialSecretFixture, actor int64, repo service.StagedImportReceiptRepository) http.Handler {
	h := adminhandler.NewAccountHandler(queryClearAdmin(f), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h.SetCredentialDraftService(service.NewAuthPendingIdentityService(f.client))
	h.SetStagedAccountImportService(service.NewStagedAccountImportService(repo, f.encryptor))
	router := gin.New()
	router.POST("/api/v1/admin/accounts/data", func(c *gin.Context) {
		if actor > 0 {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: actor})
		}
		c.Next()
	}, h.ImportData)
	return router
}

func receiptRequest(ctx context.Context, key string, body []byte) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", key)
	return r
}

func receiptCall(router http.Handler, ctx context.Context, key string, body []byte) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	router.ServeHTTP(w, receiptRequest(ctx, key, body))
	return w
}

func receiptBody(t *testing.T) []byte {
	t.Helper()
	b, err := json.Marshal(stagedPostgresRequest())
	require.NoError(t, err)
	return b
}

func receiptCount(t *testing.T, f *credentialSecretFixture) int {
	t.Helper()
	var n int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM staged_account_import_receipts`).Scan(&n))
	return n
}

type failingReceiptCoordinator struct {
	service.IdempotencyRepository
	calls atomic.Int32
}

func (r *failingReceiptCoordinator) CreateProcessing(_ context.Context, record *service.IdempotencyRecord) (bool, error) {
	r.calls.Add(1)
	record.ID = 1
	return true, nil
}
func (r *failingReceiptCoordinator) MarkSucceeded(context.Context, int64, int, string, time.Time) error {
	r.calls.Add(1)
	return errors.New("coordinator completion unavailable")
}

func TestStagedReceiptPostgresReplayConflictsRetentionAndSecrecy(t *testing.T) {
	f := newCredentialSecretFixture(t)
	key, body := uuid.NewString(), receiptBody(t)
	router := receiptRouter(f, 71, NewStagedImportReceiptRepository())
	before, receipts := stagedTableCounts(t, f), receiptCount(t, f)
	oldCoordinator := service.DefaultIdempotencyCoordinator()
	failing := &failingReceiptCoordinator{}
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(failing, service.DefaultIdempotencyConfig()))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(oldCoordinator) })
	var captured bytes.Buffer
	oldLog, oldSlog := log.Writer(), slog.Default()
	log.SetOutput(&captured)
	slog.SetDefault(slog.New(slog.NewTextHandler(&captured, nil)))
	t.Cleanup(func() { slog.SetDefault(oldSlog); log.SetOutput(oldLog) })
	first := receiptCall(router, f.ctx, key, body)
	require.Equal(t, 200, first.Code, first.Body.String())
	after := stagedTableCounts(t, f)
	require.Equal(t, before[0]+1, after[0])
	require.Equal(t, receipts+1, receiptCount(t, f))
	require.Zero(t, failing.calls.Load(), "staged imports must never call the general coordinator")
	// A stale coordinator processing entry must not obstruct a durable receipt.
	_, err := f.db.Exec(`INSERT INTO idempotency_records(scope,idempotency_key_hash,request_fingerprint,status,expires_at) VALUES('admin.accounts.import_data',$1,$2,'processing',NOW()-INTERVAL '2 days')`, service.HashIdempotencyKey(key), strings.Repeat("0", 64))
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		// New service/handler instances simulate lost process-local state.
		replay := receiptCall(receiptRouter(f, 71, NewStagedImportReceiptRepository()), f.ctx, key, body)
		require.Equal(t, 200, replay.Code, replay.Body.String())
		require.Equal(t, "true", replay.Header().Get("X-Idempotency-Replayed"))
		require.JSONEq(t, first.Body.String(), replay.Body.String())
		_, err = NewIdempotencyRepository(f.client, f.db).DeleteExpired(f.ctx, time.Now().Add(365*24*time.Hour), 10000)
		require.NoError(t, err)
	}
	for _, change := range []string{"token", "metadata", "config", "order-option"} {
		req := stagedPostgresRequest()
		switch change {
		case "token":
			req.Data.Accounts[0].Credentials["access_token"] = "different-synthetic-token"
		case "metadata":
			req.Data.Accounts[0].Name = "changed"
		case "config":
			req.Data.Accounts[0].Credentials["model_mapping"] = map[string]any{"alias": "model"}
		case "order-option":
			v := true
			req.SkipDefaultGroupBind = &v
		}
		b, e := json.Marshal(req)
		require.NoError(t, e)
		w := receiptCall(router, f.ctx, key, b)
		require.Equal(t, 409, w.Code, change)
		require.Contains(t, w.Body.String(), "IDEMPOTENCY_KEY_CONFLICT")
	}
	require.Equal(t, 409, receiptCall(receiptRouter(f, 72, NewStagedImportReceiptRepository()), f.ctx, key, body).Code)
	require.Equal(t, 401, receiptCall(receiptRouter(f, 0, NewStagedImportReceiptRepository()), f.ctx, key, body).Code)
	require.Equal(t, 400, receiptCall(router, f.ctx, "", body).Code)
	require.Equal(t, after, stagedTableCounts(t, f), "replays/conflicts must have no database side effects")

	// Scan complete relevant stored rows (including audit/outbox), not only credentials.
	for _, table := range []string{"accounts", "credential_secrets", "staged_account_import_receipts", "idempotency_records", "audit_logs", "scheduler_outbox"} {
		rows, e := f.db.Query(`SELECT row_to_json(t)::text FROM ` + table + ` t`)
		require.NoError(t, e)
		for rows.Next() {
			var row string
			require.NoError(t, rows.Scan(&row))
			for _, secret := range []string{"test-stage-access", "test-stage-refresh"} {
				require.NotContains(t, row, secret, table)
			}
			sum := sha256.Sum256(body)
			require.NotContains(t, row, hex.EncodeToString(sum[:]), "unkeyed payload digest must only exist inside ciphertext")
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
	}
	for _, secret := range []string{"test-stage-access", "test-stage-refresh"} {
		require.NotContains(t, captured.String(), secret)
	}
	sum := sha256.Sum256(body)
	require.NotContains(t, captured.String(), hex.EncodeToString(sum[:]))
	var formatted bytes.Buffer
	require.NoError(t, json.Indent(&formatted, body, "", "  "))
	w := receiptCall(router, f.ctx, key, formatted.Bytes())
	require.Equal(t, 200, w.Code)
	require.Equal(t, "true", w.Header().Get("X-Idempotency-Replayed"))
}

func TestStagedReceiptPostgresWorkspaceIsolationAndTampering(t *testing.T) {
	f := newCredentialSecretFixture(t)
	key, body := uuid.NewString(), receiptBody(t)
	w1, w2 := f.createWorkspace(t, "receipt-one"), f.createWorkspace(t, "receipt-two")
	c1, c2 := domain.WithWorkspaceScope(f.ctx, w1), domain.WithWorkspaceScope(f.ctx, w2)
	router := receiptRouter(f, 71, NewStagedImportReceiptRepository())
	require.Equal(t, 200, receiptCall(router, c1, key, body).Code)
	require.Equal(t, 200, receiptCall(router, c2, key, body).Code)
	for _, id := range []int64{w1, w2} {
		var n int
		require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM accounts WHERE workspace_id=$1`, id).Scan(&n))
		require.Equal(t, 1, n)
	}
	require.Equal(t, "true", receiptCall(router, c2, key, body).Header().Get("X-Idempotency-Replayed"))
	_, err := f.db.Exec(`UPDATE staged_account_import_receipts SET sealed_receipt=(SELECT sealed_receipt FROM staged_account_import_receipts WHERE workspace_id=$1 AND idempotency_key_hash=$3) WHERE workspace_id=$2 AND idempotency_key_hash=$3`, w1, w2, service.HashIdempotencyKey(key))
	require.NoError(t, err)
	require.Equal(t, 503, receiptCall(router, c2, key, body).Code, "swapped ciphertext cannot authorize another workspace")
	_, err = f.db.Exec(`UPDATE staged_account_import_receipts SET sealed_receipt='corrupt' WHERE workspace_id=$1 AND idempotency_key_hash=$2`, w1, service.HashIdempotencyKey(key))
	require.NoError(t, err)
	require.Equal(t, 503, receiptCall(router, c1, key, body).Code)
}

type receiptFaultRepo struct {
	service.StagedImportReceiptRepository
	insert func(context.Context, service.StagedImportIdentity, service.StagedImportReceipt) error
}

func (r receiptFaultRepo) Insert(ctx context.Context, id service.StagedImportIdentity, receipt service.StagedImportReceipt) error {
	return r.insert(ctx, id, receipt)
}

func TestStagedReceiptPostgresAtomicFailureAndCommitUnknown(t *testing.T) {
	for _, mode := range []string{"receipt_insert", "receipt_encrypt", "second_account", "commit_after", "commit_before"} {
		t.Run(mode, func(t *testing.T) {
			f := newCredentialSecretFixture(t)
			key, body := uuid.NewString(), receiptBody(t)
			before, receipts := stagedTableCounts(t, f), receiptCount(t, f)
			base := NewStagedImportReceiptRepository()
			repo := service.StagedImportReceiptRepository(base)
			if mode == "receipt_insert" {
				repo = receiptFaultRepo{base, func(ctx context.Context, id service.StagedImportIdentity, r service.StagedImportReceipt) error {
					if err := base.Insert(ctx, id, r); err != nil {
						return err
					}
					return errors.New("synthetic-secret-driver-detail")
				}}
			}
			if mode == "receipt_encrypt" {
				f.encryptor = &receiptBadEncryptor{f.encryptor}
			}
			if mode == "second_account" {
				req := stagedPostgresRequest()
				second := req.Data.Accounts[0]
				second.Credentials = map[string]any{"access_token": "test-stage-second"}
				req.Data.Accounts = append(req.Data.Accounts, second)
				var err error
				body, err = json.Marshal(req)
				require.NoError(t, err)
				f.accounts.credentialEncryptor = &stagedFailingEncryptor{SecretEncryptor: f.encryptor}
			}
			if strings.HasPrefix(mode, "commit_") {
				f.client.Account.Use(func(next dbent.Mutator) dbent.Mutator {
					return dbent.MutateFunc(func(ctx context.Context, m dbent.Mutation) (dbent.Value, error) {
						if m.Op() == dbent.OpCreate {
							dbent.TxFromContext(ctx).OnCommit(func(next dbent.Committer) dbent.Committer {
								return dbent.CommitFunc(func(ctx context.Context, tx *dbent.Tx) error {
									if mode == "commit_after" {
										if err := next.Commit(ctx, tx); err != nil {
											return err
										}
									}
									return errors.New("synthetic-secret-commit-detail")
								})
							})
						}
						return next.Mutate(ctx, m)
					})
				})
			}
			w := receiptCall(receiptRouter(f, 71, repo), f.ctx, key, body)
			require.GreaterOrEqual(t, w.Code, 500)
			require.NotContains(t, w.Body.String(), "synthetic-secret")
			require.NotContains(t, w.Body.String(), "no accounts were imported")
			if strings.HasPrefix(mode, "commit_") {
				require.Contains(t, w.Body.String(), "STAGED_IMPORT_OUTCOME_UNKNOWN")
			}
			if mode == "commit_after" {
				require.Equal(t, before[0]+1, stagedTableCounts(t, f)[0])
				require.Equal(t, receipts+1, receiptCount(t, f))
				retry := receiptCall(receiptRouter(f, 71, base), f.ctx, key, body)
				require.Equal(t, 200, retry.Code, retry.Body.String())
				require.Equal(t, "true", retry.Header().Get("X-Idempotency-Replayed"))
			} else {
				require.Equal(t, before, stagedTableCounts(t, f))
				require.Equal(t, receipts, receiptCount(t, f))
			}
		})
	}
}

type receiptBadEncryptor struct{ service.SecretEncryptor }

func (receiptBadEncryptor) EncryptionKeyConfigured() bool { return true }
func (receiptBadEncryptor) Encrypt(string) (string, error) {
	return "", errors.New("synthetic-secret-encryption-detail")
}

type failedResponseWriter struct{ *httptest.ResponseRecorder }

func (f failedResponseWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestStagedReceiptPostgresResponseFailure(t *testing.T) {
	f := newCredentialSecretFixture(t)
	key, body := uuid.NewString(), receiptBody(t)
	router := receiptRouter(f, 71, NewStagedImportReceiptRepository())
	router.ServeHTTP(failedResponseWriter{httptest.NewRecorder()}, receiptRequest(f.ctx, key, body))
	w := receiptCall(router, f.ctx, key, body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "true", w.Header().Get("X-Idempotency-Replayed"))
}

func TestStagedReceiptPostgresConcurrentRequests(t *testing.T) {
	for _, different := range []bool{false, true} {
		t.Run(map[bool]string{false: "same_payload", true: "different_payload"}[different], func(t *testing.T) {
			f := newCredentialSecretFixture(t)
			key, body := uuid.NewString(), receiptBody(t)
			before := stagedTableCounts(t, f)
			receipts := receiptCount(t, f)
			var ready sync.WaitGroup
			ready.Add(8)
			start := make(chan struct{})
			results := make(chan *httptest.ResponseRecorder, 8)
			for i := 0; i < 8; i++ {
				go func(i int) {
					b := body
					if different && i%2 == 1 {
						b = bytes.ReplaceAll(body, []byte("test-stage-access"), []byte("other-stage-access"))
					}
					router := receiptRouter(f, 71, NewStagedImportReceiptRepository())
					ready.Done()
					<-start
					results <- receiptCall(router, f.ctx, key, b)
				}(i)
			}
			ready.Wait()
			close(start)
			success, replayed, conflict := 0, 0, 0
			for i := 0; i < 8; i++ {
				w := <-results
				switch w.Code {
				case 200:
					success++
					if w.Header().Get("X-Idempotency-Replayed") == "true" {
						replayed++
					}
				case 409:
					conflict++
				default:
					t.Fatalf("unexpected response status %d", w.Code)
				}
			}
			if different {
				require.Equal(t, 4, success)
				require.Equal(t, 4, conflict)
				require.Equal(t, 3, replayed)
			} else {
				require.Equal(t, 8, success)
				require.Equal(t, 7, replayed)
			}
			require.Equal(t, before[0]+1, stagedTableCounts(t, f)[0])
			require.Equal(t, receipts+1, receiptCount(t, f))
		})
	}
}

func TestStagedReceiptPostgresLegacyAndNoTransaction(t *testing.T) {
	f := newCredentialSecretFixture(t)
	key, body := uuid.NewString(), receiptBody(t)
	repo := NewStagedImportReceiptRepository()
	require.Error(t, repo.Lock(f.ctx, service.StagedImportIdentity{}))
	_, err := f.db.Exec(`INSERT INTO idempotency_records(scope,idempotency_key_hash,request_fingerprint,status,expires_at) VALUES('admin.accounts.import_data',$1,$2,'processing',NOW()+INTERVAL '1 day')`, service.HashIdempotencyKey(key), strings.Repeat("0", 64))
	require.NoError(t, err)
	before := stagedTableCounts(t, f)
	w := receiptCall(receiptRouter(f, 71, repo), f.ctx, key, body)
	require.Equal(t, 409, w.Code)
	require.Contains(t, w.Body.String(), "STAGED_IMPORT_LEGACY_OUTCOME_UNKNOWN")
	require.Equal(t, before, stagedTableCounts(t, f))
}

func TestStagedReceiptPostgresCrashWorker(t *testing.T) {
	mode := os.Getenv("STAGED_RECEIPT_CRASH_MODE")
	if mode == "" {
		return
	}
	f := newCredentialSecretFixture(t)
	base := NewStagedImportReceiptRepository()
	repo := service.StagedImportReceiptRepository(base)
	if mode == "before" {
		repo = receiptFaultRepo{base, func(ctx context.Context, id service.StagedImportIdentity, r service.StagedImportReceipt) error {
			if err := base.Insert(ctx, id, r); err != nil {
				return err
			}
			os.Exit(73)
			return nil
		}}
	} else {
		f.client.Account.Use(func(next dbent.Mutator) dbent.Mutator {
			return dbent.MutateFunc(func(ctx context.Context, m dbent.Mutation) (dbent.Value, error) {
				if m.Op() == dbent.OpCreate {
					dbent.TxFromContext(ctx).OnCommit(func(next dbent.Committer) dbent.Committer {
						return dbent.CommitFunc(func(ctx context.Context, tx *dbent.Tx) error {
							if err := next.Commit(ctx, tx); err != nil {
								return err
							}
							os.Exit(74)
							return nil
						})
					})
				}
				return next.Mutate(ctx, m)
			})
		})
	}
	receiptCall(receiptRouter(f, 71, repo), f.ctx, os.Getenv("STAGED_RECEIPT_CRASH_KEY"), receiptBody(t))
	t.Fatal("worker did not exit at fault boundary")
}

func TestStagedReceiptPostgresProcessCrash(t *testing.T) {
	for _, mode := range []string{"before", "after"} {
		t.Run(mode, func(t *testing.T) {
			f := newCredentialSecretFixture(t)
			key := uuid.NewString()
			before, receipts := stagedTableCounts(t, f), receiptCount(t, f)
			ctx, cancel := context.WithTimeout(f.ctx, 30*time.Second)
			defer cancel()
			// Re-exec the current test binary with fixed arguments.
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStagedReceiptPostgresCrashWorker$", "-test.count=1") //nolint:gosec
			cmd.Env = append(os.Environ(), "STAGED_RECEIPT_CRASH_MODE="+mode, "STAGED_RECEIPT_CRASH_KEY="+key)
			err := cmd.Run()
			var exit *exec.ExitError
			require.ErrorAs(t, err, &exit)
			want := 73
			if mode == "after" {
				want = 74
			}
			require.Equal(t, want, exit.ExitCode())
			w := receiptCall(receiptRouter(f, 71, NewStagedImportReceiptRepository()), f.ctx, key, receiptBody(t))
			require.Equal(t, 200, w.Code, w.Body.String())
			if mode == "after" {
				require.Equal(t, "true", w.Header().Get("X-Idempotency-Replayed"))
			} else {
				require.Empty(t, w.Header().Get("X-Idempotency-Replayed"))
			}
			require.Equal(t, before[0]+1, stagedTableCounts(t, f)[0])
			require.Equal(t, receipts+1, receiptCount(t, f))
		})
	}
}

func TestStagedReceiptPostgresRetentionUniqueAndKeys(t *testing.T) {
	f := newCredentialSecretFixture(t)
	workspace := f.createWorkspace(t, "receipt-retention")
	ctx := domain.WithWorkspaceScope(f.ctx, workspace)
	key, body := uuid.NewString(), receiptBody(t)
	router := receiptRouter(f, 71, NewStagedImportReceiptRepository())
	first := receiptCall(router, ctx, key, body)
	require.Equal(t, 200, first.Code, first.Body.String())
	// Delete the resource independently. The key remains spent and returns the
	// historical response rather than recreating the deleted account.
	_, err := f.db.Exec(`UPDATE accounts SET deleted_at=NOW() WHERE workspace_id=$1`, workspace)
	require.NoError(t, err)
	replay := receiptCall(router, ctx, key, body)
	require.Equal(t, 200, replay.Code)
	require.Equal(t, "true", replay.Header().Get("X-Idempotency-Replayed"))
	require.JSONEq(t, first.Body.String(), replay.Body.String())
	var accounts int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM accounts WHERE workspace_id=$1 AND deleted_at IS NULL`, workspace).Scan(&accounts))
	require.Zero(t, accounts)
	_, err = f.db.Exec(`INSERT INTO staged_account_import_receipts SELECT * FROM staged_account_import_receipts WHERE workspace_id=$1 AND idempotency_key_hash=$2`, workspace, service.HashIdempotencyKey(key))
	require.Error(t, err, "database uniqueness must also reject writers that bypass the lock")

	for _, mode := range []string{"missing", "ephemeral", "wrong"} {
		t.Run(mode, func(t *testing.T) {
			original := f.encryptor
			defer func() { f.encryptor = original }()
			switch mode {
			case "missing":
				f.encryptor = nil
			case "ephemeral":
				f.encryptor = &stagedEphemeralEncryptor{SecretEncryptor: original}
			case "wrong":
				f.encryptor = receiptWrongKey{original}
			}
			w := receiptCall(receiptRouter(f, 71, NewStagedImportReceiptRepository()), ctx, key, body)
			if mode == "wrong" {
				require.Equal(t, 503, w.Code)
			} else {
				require.Equal(t, 400, w.Code)
			}
			require.NotContains(t, w.Body.String(), "synthetic-decrypt-detail")
		})
	}
}

type receiptWrongKey struct{ service.SecretEncryptor }

func (receiptWrongKey) EncryptionKeyConfigured() bool { return true }
func (receiptWrongKey) Decrypt(string) (string, error) {
	return "", errors.New("synthetic-decrypt-detail")
}

func TestStagedReceiptPostgresLockWaitAndActorRace(t *testing.T) {
	f := newCredentialSecretFixture(t)
	key, body := uuid.NewString(), receiptBody(t)
	repo := NewStagedImportReceiptRepository()
	id := service.StagedImportIdentity{WorkspaceID: domain.DefaultWorkspaceID, ActorID: 71, Operation: service.StagedImportOperation, KeyHash: service.HashIdempotencyKey(key)}
	tx, err := f.client.Tx(f.ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	require.NoError(t, repo.Lock(service.WithStagedAccountImport(dbent.NewTxContext(f.ctx, tx)), id))
	before := stagedTableCounts(t, f)
	// Different actor must wait on the SAME lock and cannot execute while held.
	started := time.Now()
	w := receiptCall(receiptRouter(f, 72, repo), f.ctx, key, body)
	require.Equal(t, 503, w.Code)
	require.GreaterOrEqual(t, time.Since(started), 4*time.Second)
	require.Less(t, time.Since(started), 15*time.Second)
	require.Equal(t, before, stagedTableCounts(t, f))
	require.NoError(t, tx.Rollback())

	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	for _, actor := range []int64{71, 72} {
		router := receiptRouter(f, actor, NewStagedImportReceiptRepository())
		go func() { <-start; results <- receiptCall(router, f.ctx, key, body) }()
	}
	close(start)
	one, two := <-results, <-results
	require.ElementsMatch(t, []int{200, 409}, []int{one.Code, two.Code})
	require.Equal(t, before[0]+1, stagedTableCounts(t, f)[0])
}
