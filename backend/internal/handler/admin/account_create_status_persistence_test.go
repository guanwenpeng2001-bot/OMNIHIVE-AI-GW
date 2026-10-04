package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Use the real create service with a small SQLite persistence adapter, keeping
// PostgreSQL locking/outbox behavior outside these status contract tests.
type createStatusRepository struct {
	service.AdminAccountRepository
	client *dbent.Client
}

func (repo *createStatusRepository) Create(ctx context.Context, account *service.Account) error {
	client := repo.client
	if tx := dbent.TxFromContext(ctx); tx != nil {
		client = tx.Client()
	}
	stored, err := client.Account.Create().SetName(account.Name).SetPlatform(account.Platform).SetType(account.Type).
		SetCredentials(account.Credentials).SetExtra(account.Extra).SetStatus(account.Status).SetSchedulable(account.Schedulable).Save(ctx)
	if err == nil {
		account.ID = stored.ID
	}
	return err
}
func (repo *createStatusRepository) CreateWithAccountGroups(ctx context.Context, account *service.Account, _ []service.AccountGroup) error {
	return repo.Create(ctx, account)
}
func (repo *createStatusRepository) GetByID(ctx context.Context, id int64) (*service.Account, error) {
	stored, err := repo.client.Account.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &service.Account{ID: stored.ID, Name: stored.Name, Platform: stored.Platform, Type: stored.Type, Credentials: stored.Credentials, Status: stored.Status, Schedulable: stored.Schedulable}, nil
}

type createStatusGroups struct{ service.AdminGroupRepository }

func (*createStatusGroups) ListActiveByPlatform(context.Context, string) ([]service.Group, error) {
	return nil, nil
}

type createStatusPrivacyAdmin struct {
	service.AdminService
	privacyCalls int
}

func (admin *createStatusPrivacyAdmin) EnsureOpenAIPrivacy(context.Context, *service.Account) string {
	admin.privacyCalls++
	return ""
}
func (admin *createStatusPrivacyAdmin) EnsureAntigravityPrivacy(context.Context, *service.Account) string {
	admin.privacyCalls++
	return ""
}

func newCreateStatusAdmin(client *dbent.Client) *createStatusPrivacyAdmin {
	repo := &createStatusRepository{client: client}
	admin := service.NewAdminService(nil, nil, nil, &createStatusGroups{}, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	return &createStatusPrivacyAdmin{AdminService: admin}
}

func TestCreateAccountStatusSingleAndBatchPersistence(t *testing.T) {
	for _, batch := range []bool{false, true} {
		for _, status := range []string{"", service.StatusActive, "inactive", service.StatusError, "disabled"} {
			name := "single/" + status
			if batch {
				name = "batch/" + status
			}
			t.Run(name, func(t *testing.T) {
				scheduler := newGrokImportProbeScheduler(1, time.Minute)
				previous := defaultOpenAIImportProbeScheduler
				defaultOpenAIImportProbeScheduler = scheduler
				started := make(chan struct{})
				scheduler.scheduleProbe(999999, func(ctx context.Context) { close(started); <-ctx.Done() })
				<-started
				t.Cleanup(func() {
					scheduler.close()
					require.Eventually(t, func() bool { return snapshotGrokImportProbeScheduler(scheduler).workers == 0 }, time.Second, time.Millisecond)
					defaultOpenAIImportProbeScheduler = previous
				})
				pending, client := credentialDraftTestClient(t)
				admin := newCreateStatusAdmin(client)
				handler := &AccountHandler{adminService: admin, accountTestService: &service.AccountTestService{}}
				handler.SetCredentialDraftService(pending)
				router := gin.New()
				router.POST("/accounts", handler.Create)
				router.POST("/accounts/batch", handler.BatchCreate)
				request := CreateAccountRequest{Name: "status-fixture", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: status, Credentials: map[string]any{"api_key": "test"}}
				var payload any = request
				path := "/accounts"
				if batch {
					// The omitted-status peer proves mixed batches retain active defaults.
					peer := request
					peer.Name, peer.Status = "default-peer", ""
					payload = map[string]any{"accounts": []CreateAccountRequest{request, peer}}
					path += "/batch"
				}
				body, err := json.Marshal(payload)
				require.NoError(t, err)
				httpRequest := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
				httpRequest.Header.Set("Content-Type", "application/json")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httpRequest)
				if status == "disabled" {
					require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
					require.Zero(t, client.Account.Query().CountX(context.Background()))
					return
				}
				require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
				stored := client.Account.Query().AllX(context.Background())
				if batch {
					require.Len(t, stored, 2)
				} else {
					require.Len(t, stored, 1)
				}
				for _, account := range stored {
					want := status
					if want == "" || account.Name == "default-peer" {
						want = service.StatusActive
					}
					require.Equal(t, want, account.Status)
					require.True(t, account.Schedulable)
				}
				require.Zero(t, admin.privacyCalls)
				queued := 0
				if status == "" || status == service.StatusActive {
					queued++
				}
				if batch {
					queued++
				}
				require.Equal(t, queued, snapshotGrokImportProbeScheduler(scheduler).queued)
			})
		}
	}
}

func TestCredentialDraftNewStatusPersistenceAndAfterCommitPrivacy(t *testing.T) {
	for _, status := range []string{"", service.StatusActive, "inactive", service.StatusError, "disabled"} {
		t.Run("status="+status, func(t *testing.T) {
			pending, client := credentialDraftTestClient(t)
			admin := newCreateStatusAdmin(client)
			ctx := context.Background()
			draft, err := pending.IssueAccountCredentialDraft(ctx, "browser", service.PlatformOpenAI, service.AccountTypeOAuth, map[string]any{"access_token": "test"})
			require.NoError(t, err)
			request := CreateAccountRequest{Name: "draft", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: status, CredentialDraft: draft.Reference, CredentialDraftRevision: draft.Revision}
			digest, err := credentialDraftSubmission(request)
			require.NoError(t, err)
			review, err := pending.ReviewAccountCredentialDraft(ctx, draft.Reference, "browser", draft.Revision, digest)
			require.NoError(t, err)
			request.CredentialDraftRevision = review.Revision
			err = pending.RunAccountWriteTransaction(ctx, func(txCtx context.Context) error {
				account, err := confirmCredentialDraftWithBinding(txCtx, pending, admin, "browser", request, nil, false, false, "admin", false)
				if err != nil {
					return err
				}
				want := status
				if want == "" {
					want = service.StatusActive
				}
				require.Equal(t, want, account.Status)
				require.Zero(t, admin.privacyCalls, "privacy must wait for outer commit")
				return nil
			})
			if status == "disabled" {
				require.Error(t, err)
				require.Zero(t, client.Account.Query().CountX(ctx))
				require.Zero(t, admin.privacyCalls)
				return
			}
			require.NoError(t, err)
			stored := client.Account.Query().OnlyX(ctx)
			want := status
			if want == "" {
				want = service.StatusActive
			}
			require.Equal(t, want, stored.Status)
			require.True(t, stored.Schedulable)
			if want == service.StatusActive {
				require.Equal(t, 1, admin.privacyCalls)
			} else {
				require.Zero(t, admin.privacyCalls)
			}
		})
	}
}
