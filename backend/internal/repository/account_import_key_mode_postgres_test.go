package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
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

// The real account service/repository persists both modes. Only the irrelevant
// empty proxy listing is stubbed, so legacy follows its usual active create path.
type modeImportAdmin struct{ service.AdminService }

func (a modeImportAdmin) ListProxies(context.Context, int, int, string, string, string, string, string) ([]service.Proxy, int64, error) {
	return nil, 0, nil
}

func modeImportRouter(f *credentialSecretFixture, actor int64, repo service.StagedImportReceiptRepository) http.Handler {
	h := adminhandler.NewAccountHandler(modeImportAdmin{queryClearAdmin(f)}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h.SetCredentialDraftService(service.NewAuthPendingIdentityService(f.client))
	h.SetStagedAccountImportService(service.NewStagedAccountImportService(repo, f.encryptor))
	r := gin.New()
	r.POST("/api/v1/admin/accounts/data", func(c *gin.Context) {
		if actor > 0 {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: actor})
		}
		c.Next()
	}, h.ImportData)
	return r
}

func modeBody(t *testing.T, mode string) []byte {
	t.Helper()
	req := stagedPostgresRequest()
	req.Data.Accounts[0].Name = "mode-fixture"
	req.Data.Accounts[0].Type = service.AccountTypeAPIKey
	req.Data.Accounts[0].Credentials = map[string]any{"api_key": "synthetic-import-mode-credential"}
	req.Data.Accounts[0].ProxyDependency = "none"
	req.StageInactive = mode == "staged"
	b, err := json.Marshal(req)
	require.NoError(t, err)
	if mode == "false" {
		b = append([]byte(`{"stage_inactive":false,`), b[1:]...)
	}
	return b
}

func modeCoordinator(t *testing.T, f *credentialSecretFixture) {
	t.Helper()
	before := service.DefaultIdempotencyCoordinator()
	cfg := service.DefaultIdempotencyConfig()
	cfg.ObserveOnly = false
	cfg.FailedRetryBackoff = 0
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(NewIdempotencyRepository(f.client, f.db), cfg))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(before) })
}

func modeRows(t *testing.T, f *credentialSecretFixture, key string) int {
	t.Helper()
	var n int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM account_import_key_modes WHERE idempotency_key_hash=$1`, service.HashIdempotencyKey(key)).Scan(&n))
	return n
}

func TestAccountImportModePostgresBothDirections(t *testing.T) {
	for _, first := range []string{"staged", "legacy"} {
		for _, legacyFlag := range []string{"false", "omitted"} {
			t.Run(first+"/"+legacyFlag, func(t *testing.T) {
				f := newCredentialSecretFixture(t)
				modeCoordinator(t, f)
				key := uuid.NewString()
				before := stagedTableCounts(t, f)
				r := modeImportRouter(f, 71, NewStagedImportReceiptRepository())
				one, two := modeBody(t, "staged"), modeBody(t, legacyFlag)
				if first == "legacy" {
					one, two = two, one
				}
				w := receiptCall(r, f.ctx, key, one)
				require.Equal(t, 200, w.Code, w.Body.String())
				after := stagedTableCounts(t, f)
				require.Equal(t, before[0]+1, after[0])
				require.Equal(t, 1, modeRows(t, f, key))
				replay := receiptCall(r, f.ctx, key, one)
				require.Equal(t, 200, replay.Code, replay.Body.String())
				require.Equal(t, "true", replay.Header().Get("X-Idempotency-Replayed"))
				require.JSONEq(t, w.Body.String(), replay.Body.String())
				conflict := receiptCall(r, f.ctx, key, two)
				require.Equal(t, 409, conflict.Code, conflict.Body.String())
				require.Contains(t, conflict.Body.String(), "IDEMPOTENCY_KEY_CONFLICT")
				require.Equal(t, after, stagedTableCounts(t, f), "opposite mode must not enter account writes")
				if first == "legacy" {
					// false and omitted are the same decoded legacy request.
					otherFlag := "false"
					if legacyFlag == "false" {
						otherFlag = "omitted"
					}
					require.Equal(t, "true", receiptCall(r, f.ctx, key, modeBody(t, otherFlag)).Header().Get("X-Idempotency-Replayed"))
				}
				// TTL cleanup can remove the old coordinator result, never the mode.
				_, err := NewIdempotencyRepository(f.client, f.db).DeleteExpired(f.ctx, time.Now().Add(365*24*time.Hour), 10000)
				require.NoError(t, err)
				require.Equal(t, 409, receiptCall(r, f.ctx, key, two).Code)
				require.Equal(t, 1, modeRows(t, f, key))
				// A new key retains normal execution in the opposite mode.
				fresh := receiptCall(r, f.ctx, uuid.NewString(), two)
				require.Equal(t, 200, fresh.Code, fresh.Body.String())
				require.Equal(t, before[0]+2, stagedTableCounts(t, f)[0])
			})
		}
	}
}

type modeFaultRepo struct {
	service.StagedImportReceiptRepository
	onInsert func(context.Context, service.StagedImportIdentity, string) error
}

func (r modeFaultRepo) InsertMode(ctx context.Context, id service.StagedImportIdentity, mode string) error {
	return r.onInsert(ctx, id, mode)
}

func TestAccountImportModePostgresConcurrentBothDirections(t *testing.T) {
	for _, first := range []string{"staged", "legacy"} {
		t.Run(first, func(t *testing.T) {
			f := newCredentialSecretFixture(t)
			modeCoordinator(t, f)
			base := NewStagedImportReceiptRepository()
			key := uuid.NewString()
			before := stagedTableCounts(t, f)
			locked, release := make(chan struct{}), make(chan struct{})
			repo := modeFaultRepo{base, func(ctx context.Context, id service.StagedImportIdentity, mode string) error {
				if err := base.InsertMode(ctx, id, mode); err != nil {
					return err
				}
				close(locked)
				<-release
				return nil
			}}
			one, two := modeBody(t, "staged"), modeBody(t, "false")
			if first == "legacy" {
				one, two = two, one
			}
			winner, loser := make(chan *httptest.ResponseRecorder, 1), make(chan *httptest.ResponseRecorder, 1)
			go func() { winner <- receiptCall(modeImportRouter(f, 71, repo), f.ctx, key, one) }()
			select {
			case <-locked:
			case <-time.After(10 * time.Second):
				t.Fatal("reservation did not acquire lock")
			}
			go func() { loser <- receiptCall(modeImportRouter(f, 71, base), f.ctx, key, two) }()
			select {
			case <-loser:
				close(release)
				t.Fatal("opposite mode bypassed uncommitted reservation lock")
			case <-time.After(150 * time.Millisecond):
			}
			close(release)
			w, l := <-winner, <-loser
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Equal(t, 409, l.Code, l.Body.String())
			require.Equal(t, before[0]+1, stagedTableCounts(t, f)[0])
			require.Equal(t, 1, modeRows(t, f, key))
		})
	}
}

func TestAccountImportModePostgresIsolationAndDisabledCoordinator(t *testing.T) {
	f := newCredentialSecretFixture(t)
	modeCoordinator(t, f)
	key := uuid.NewString()
	w1, w2 := f.createWorkspace(t, "mode-w1"), f.createWorkspace(t, "mode-w2")
	c1, c2 := domain.WithWorkspaceScope(f.ctx, w1), domain.WithWorkspaceScope(f.ctx, w2)
	r := modeImportRouter(f, 71, NewStagedImportReceiptRepository())
	require.Equal(t, 200, receiptCall(r, c1, key, modeBody(t, "staged")).Code)
	require.Equal(t, 200, receiptCall(r, c2, key, modeBody(t, "omitted")).Code)
	for _, ctx := range []context.Context{c1, c2} {
		for _, mode := range []string{"staged", "false"} {
			require.Equal(t, 409, receiptCall(modeImportRouter(f, 72, NewStagedImportReceiptRepository()), ctx, key, modeBody(t, mode)).Code)
		}
	}
	require.Equal(t, 2, modeRows(t, f, key))
	service.SetDefaultIdempotencyCoordinator(nil)
	require.Equal(t, 409, receiptCall(r, c1, key, modeBody(t, "false")).Code, "disabled generic coordinator must not disable shared mode protection")
	newKey := uuid.NewString()
	before := stagedTableCounts(t, f)[0]
	require.Equal(t, 200, receiptCall(r, c1, newKey, modeBody(t, "omitted")).Code)
	require.Equal(t, 409, receiptCall(r, c1, newKey, modeBody(t, "staged")).Code)
	// Legacy no-key behavior stays compatible when the old coordinator allows it.
	require.Equal(t, 200, receiptCall(r, c1, "", modeBody(t, "omitted")).Code)
	require.Equal(t, before+2, stagedTableCounts(t, f)[0])
}

func TestAccountImportModePostgresFailureAndUnknownReservation(t *testing.T) {
	for _, mode := range []string{"staged", "legacy"} {
		for _, fault := range []string{"insert", "commit_before", "commit_after", "business"} {
			t.Run(mode+"/"+fault, func(t *testing.T) {
				f := newCredentialSecretFixture(t)
				modeCoordinator(t, f)
				key := uuid.NewString()
				before := stagedTableCounts(t, f)
				base := NewStagedImportReceiptRepository()
				repo := service.StagedImportReceiptRepository(base)
				if fault != "business" {
					repo = modeFaultRepo{base, func(ctx context.Context, id service.StagedImportIdentity, mode string) error {
						if err := base.InsertMode(ctx, id, mode); err != nil {
							return err
						}
						if fault == "insert" {
							return errors.New("synthetic-mode-failure-detail")
						}
						dbent.TxFromContext(ctx).OnCommit(func(next dbent.Committer) dbent.Committer {
							return dbent.CommitFunc(func(ctx context.Context, tx *dbent.Tx) error {
								if fault == "commit_after" {
									if err := next.Commit(ctx, tx); err != nil {
										return err
									}
								}
								return errors.New("synthetic-mode-commit-detail")
							})
						})
						return nil
					}}
				} else {
					f.client.Account.Use(func(next dbent.Mutator) dbent.Mutator {
						return dbent.MutateFunc(func(context.Context, dbent.Mutation) (dbent.Value, error) { return nil, service.ErrStagedImportFailed })
					})
				}
				w := receiptCall(modeImportRouter(f, 71, repo), f.ctx, key, modeBody(t, mode))
				require.GreaterOrEqual(t, w.Code, 500)
				require.NotContains(t, w.Body.String(), "synthetic-mode-")
				require.NotContains(t, w.Body.String(), "no accounts were imported")
				require.Equal(t, before, stagedTableCounts(t, f))
				if strings.HasPrefix(fault, "commit_") {
					require.Contains(t, w.Body.String(), "ACCOUNT_IMPORT_KEY_OUTCOME_UNKNOWN")
				}
				if fault == "commit_after" || fault == "business" {
					require.Equal(t, 1, modeRows(t, f, key))
					opposite := "staged"
					if mode == "staged" {
						opposite = "legacy"
					}
					require.Equal(t, 409, receiptCall(modeImportRouter(f, 71, base), f.ctx, key, modeBody(t, opposite)).Code)
				} else {
					require.Zero(t, modeRows(t, f, key))
				}
				// A failed business transaction pins the mode, not a failed outcome.
				// Use a fresh client without the injected failure to exercise the
				// existing failed_retryable coordinator path for legacy requests.
				retryFixture := f
				if fault == "business" {
					retryFixture = newCredentialSecretFixture(t)
				}
				retry := receiptCall(modeImportRouter(retryFixture, 71, base), f.ctx, key, modeBody(t, mode))
				require.Equal(t, 200, retry.Code, retry.Body.String())
			})
		}
	}
}

func TestAccountImportModePostgresSecrecyAndLegacyReplay(t *testing.T) {
	f := newCredentialSecretFixture(t)
	modeCoordinator(t, f)
	key := uuid.NewString()
	body := modeBody(t, "staged")
	r := modeImportRouter(f, 71, NewStagedImportReceiptRepository())
	require.Equal(t, 200, receiptCall(r, f.ctx, key, body).Code)
	require.Equal(t, 409, receiptCall(r, f.ctx, key, modeBody(t, "false")).Code)
	var n int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM idempotency_records WHERE idempotency_key_hash=$1`, service.HashIdempotencyKey(key)).Scan(&n))
	require.Zero(t, n, "conflicting legacy payload must not reach the generic fingerprint store")
	for _, table := range []string{"account_import_key_modes", "staged_account_import_receipts"} {
		rows, err := f.db.Query(`SELECT row_to_json(t)::text FROM ` + table + ` t`)
		require.NoError(t, err)
		for rows.Next() {
			var row string
			require.NoError(t, rows.Scan(&row))
			require.NotContains(t, row, "synthetic-import-mode-credential")
			sum := sha256.Sum256(body)
			require.NotContains(t, row, hex.EncodeToString(sum[:]))
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
	}
	// Existing pre-guard LEGACY success records still replay normally after adopting
	// a mode, with the original fingerprint/coordinator contract and no new account.
	legacyKey := uuid.NewString()
	legacyBody := modeBody(t, "false")
	req := adminhandler.DataImportRequest{}
	require.NoError(t, json.Unmarshal(legacyBody, &req))
	coord := service.DefaultIdempotencyCoordinator()
	_, err := coord.Execute(f.ctx, service.IdempotencyExecuteOptions{Scope: "admin.accounts.import_data", ActorScope: "admin:71", Method: "POST", Route: "/api/v1/admin/accounts/data", IdempotencyKey: legacyKey, Payload: req}, func(context.Context) (any, error) { return adminhandler.DataImportResult{AccountCreated: 1}, nil })
	require.NoError(t, err)
	before := stagedTableCounts(t, f)
	invalidActor := receiptCall(modeImportRouter(f, 72, NewStagedImportReceiptRepository()), f.ctx, legacyKey, legacyBody)
	require.Equal(t, 409, invalidActor.Code)
	require.Zero(t, modeRows(t, f, legacyKey), "wrong actor must not poison historical ownership")
	req.Data.Accounts[0].Name = "different-request"
	invalidBody, err := json.Marshal(req)
	require.NoError(t, err)
	require.Equal(t, 409, receiptCall(r, f.ctx, legacyKey, invalidBody).Code)
	require.Zero(t, modeRows(t, f, legacyKey), "different payload must not claim historical ownership")
	w := receiptCall(r, f.ctx, legacyKey, legacyBody)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "true", w.Header().Get("X-Idempotency-Replayed"))
	require.Equal(t, before, stagedTableCounts(t, f))
}

type modeMarkFailed struct{ service.IdempotencyRepository }

func (modeMarkFailed) MarkSucceeded(context.Context, int64, int, string, time.Time) error {
	return errors.New("synthetic coordinator completion failure")
}

func TestAccountImportModePostgresLegacyCompletionFailure(t *testing.T) {
	f := newCredentialSecretFixture(t)
	modeCoordinator(t, f)
	base := NewIdempotencyRepository(f.client, f.db)
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(modeMarkFailed{base}, service.DefaultIdempotencyConfig()))
	r := modeImportRouter(f, 71, NewStagedImportReceiptRepository())
	key := uuid.NewString()
	before := stagedTableCounts(t, f)[0]
	w := receiptCall(r, f.ctx, key, modeBody(t, "false"))
	require.Equal(t, 503, w.Code)
	require.Equal(t, before+1, stagedTableCounts(t, f)[0], "business transaction committed before the old coordinator failure")
	require.Equal(t, 1, modeRows(t, f, key))
	require.Equal(t, 409, receiptCall(r, f.ctx, key, modeBody(t, "staged")).Code)
	// Preserve the old same-mode in-progress behavior; the new guard neither
	// invents a success receipt for legacy nor starts a duplicate execution.
	retry := receiptCall(r, f.ctx, key, modeBody(t, "omitted"))
	require.Equal(t, 409, retry.Code)
	require.Contains(t, retry.Body.String(), "IDEMPOTENCY_IN_PROGRESS")
	require.Equal(t, before+1, stagedTableCounts(t, f)[0])
	_, err := base.DeleteExpired(f.ctx, time.Now().Add(365*24*time.Hour), 10000)
	require.NoError(t, err)
	require.Equal(t, 409, receiptCall(r, f.ctx, key, modeBody(t, "staged")).Code)
}

func TestAccountImportModePostgresCrashWorker(t *testing.T) {
	mode := os.Getenv("IMPORT_MODE_CRASH_MODE")
	if mode == "" {
		return
	}
	f := newCredentialSecretFixture(t)
	modeCoordinator(t, f)
	base := NewStagedImportReceiptRepository()
	repo := modeFaultRepo{base, func(ctx context.Context, id service.StagedImportIdentity, mode string) error {
		if err := base.InsertMode(ctx, id, mode); err != nil {
			return err
		}
		if os.Getenv("IMPORT_MODE_CRASH_PHASE") == "before" {
			os.Exit(81)
		}
		dbent.TxFromContext(ctx).OnCommit(func(next dbent.Committer) dbent.Committer {
			return dbent.CommitFunc(func(ctx context.Context, tx *dbent.Tx) error {
				if err := next.Commit(ctx, tx); err != nil {
					return err
				}
				os.Exit(82)
				return nil
			})
		})
		return nil
	}}
	receiptCall(modeImportRouter(f, 71, repo), f.ctx, os.Getenv("IMPORT_MODE_CRASH_KEY"), modeBody(t, mode))
	t.Fatal("crash injection was not reached")
}

func TestAccountImportModePostgresProcessCrash(t *testing.T) {
	for _, mode := range []string{"staged", "legacy"} {
		for _, phase := range []string{"before", "after"} {
			t.Run(mode+"/"+phase, func(t *testing.T) {
				f := newCredentialSecretFixture(t)
				modeCoordinator(t, f)
				key := uuid.NewString()
				before := stagedTableCounts(t, f)
				ctx, cancel := context.WithTimeout(f.ctx, 30*time.Second)
				defer cancel()
				// Re-exec the current test binary with fixed arguments.
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAccountImportModePostgresCrashWorker$", "-test.count=1") //nolint:gosec
				cmd.Env = append(os.Environ(), "IMPORT_MODE_CRASH_MODE="+mode, "IMPORT_MODE_CRASH_PHASE="+phase, "IMPORT_MODE_CRASH_KEY="+key)
				err := cmd.Run()
				var exit *exec.ExitError
				require.ErrorAs(t, err, &exit)
				want := 81
				if phase == "after" {
					want = 82
				}
				require.Equal(t, want, exit.ExitCode())
				require.Equal(t, before, stagedTableCounts(t, f))
				if phase == "after" {
					opposite := "staged"
					if mode == "staged" {
						opposite = "legacy"
					}
					require.Equal(t, 409, receiptCall(modeImportRouter(f, 71, NewStagedImportReceiptRepository()), f.ctx, key, modeBody(t, opposite)).Code)
				} else {
					require.Zero(t, modeRows(t, f, key))
				}
				w := receiptCall(modeImportRouter(f, 71, NewStagedImportReceiptRepository()), f.ctx, key, modeBody(t, mode))
				require.Equal(t, 200, w.Code, w.Body.String())
				require.Equal(t, before[0]+1, stagedTableCounts(t, f)[0])
			})
		}
	}
}

// Run create and replay in separate invocations around an external crash/restart
// of ONLY the disposable PG18 cluster. This test never controls a database server.
func TestAccountImportModePostgresDatabaseRestart(t *testing.T) {
	phase := os.Getenv("IMPORT_MODE_DB_RESTART_PHASE")
	if phase == "" {
		return
	}
	run := os.Getenv("IMPORT_MODE_DB_RESTART_RUN")
	require.NotEmpty(t, run)
	f := newCredentialSecretFixture(t)
	modeCoordinator(t, f)
	r := modeImportRouter(f, 71, NewStagedImportReceiptRepository())
	for _, mode := range []string{"staged", "legacy"} {
		key := "restart-" + run + "-" + mode
		body := modeBody(t, mode)
		var doc map[string]any
		require.NoError(t, json.Unmarshal(body, &doc))
		data, ok := doc["data"].(map[string]any)
		require.True(t, ok)
		accounts, ok := data["accounts"].([]any)
		require.True(t, ok)
		require.NotEmpty(t, accounts)
		account, ok := accounts[0].(map[string]any)
		require.True(t, ok)
		account["name"] = key
		body, err := json.Marshal(doc)
		require.NoError(t, err)
		w := receiptCall(r, f.ctx, key, body)
		require.Equal(t, 200, w.Code, w.Body.String())
		if phase == "replay" {
			require.Equal(t, "true", w.Header().Get("X-Idempotency-Replayed"))
		}
		doc["stage_inactive"] = mode == "legacy"
		opposite, err := json.Marshal(doc)
		require.NoError(t, err)
		require.Equal(t, 409, receiptCall(r, f.ctx, key, opposite).Code)
		var count int
		require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM accounts WHERE name=$1`, key).Scan(&count))
		require.Equal(t, 1, count)
	}
}
