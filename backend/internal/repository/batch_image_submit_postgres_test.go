//go:build unit

package repository

import (
	"context"
	"database/sql"
	"io"
	"os"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type batchSubmitUploadFailureClient struct {
	service.GeminiBatchClient
	createCalls int
}

func (*batchSubmitUploadFailureClient) UploadJSONL(context.Context, string, string, io.Reader) (*service.GeminiUploadedFile, error) {
	return nil, &service.GeminiAPIError{StatusCode: 401, Message: "unauthorized"}
}

func (c *batchSubmitUploadFailureClient) CreateBatch(context.Context, string, string, string, string) (*service.GeminiBatchJob, error) {
	c.createCalls++
	return nil, nil
}

type batchSubmitAccounts struct{ account service.Account }

type batchSubmitPricing struct{}

func (batchSubmitPricing) BatchImageUnitPrice(context.Context, *service.BatchImageJob) (float64, error) {
	return .25, nil
}

func (a batchSubmitAccounts) GetByID(context.Context, int64) (*service.Account, error) {
	return &a.account, nil
}
func (a batchSubmitAccounts) ListSchedulableByPlatform(context.Context, string) ([]service.Account, error) {
	return []service.Account{a.account}, nil
}
func (a batchSubmitAccounts) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]service.Account, error) {
	return []service.Account{a.account}, nil
}

func TestBatchImageSubmitUploadFailurePostgres(t *testing.T) {
	dsn := os.Getenv("BATCH_SUBMIT_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated fix3_batch_test PostgreSQL")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	var database string
	require.NoError(t, db.QueryRow("SELECT current_database()").Scan(&database))
	require.Equal(t, "fix3_batch_test", database)
	ctx := context.Background()
	require.NoError(t, ApplyMigrations(ctx, db))
	id, err := service.NewBatchImageID()
	require.NoError(t, err)
	var user, key, account int64
	require.NoError(t, db.QueryRow(`INSERT INTO users(email,password_hash,balance) VALUES($1,'test',100) RETURNING id`, id+"@submit.test").Scan(&user))
	require.NoError(t, db.QueryRow(`INSERT INTO api_keys(user_id,key,name) VALUES($1,$2,'submit') RETURNING id`, user, id).Scan(&key))
	require.NoError(t, db.QueryRow(`INSERT INTO accounts(name,platform,type) VALUES($1,'gemini','apikey') RETURNING id`, id).Scan(&account))
	repo := NewBatchImageRepository(db)
	billing := NewUsageBillingRepository(nil, db)
	client := &batchSubmitUploadFailureClient{}
	svc := &service.BatchImagePublicService{
		Repo: repo, BillingRepo: billing, Pricing: batchSubmitPricing{},
		AccountRepo:      batchSubmitAccounts{service.Account{ID: account, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "test-key"}}},
		ProviderRegistry: service.NewBatchImageProviderRegistry(service.NewGeminiAPIBatchImageProvider(client)),
		Config:           &config.Config{BatchImage: config.BatchImageConfig{Enabled: true}},
	}
	_, err = svc.Submit(ctx, service.BatchImageOwner{UserID: user, APIKeyID: key}, service.BatchImageSubmitRequest{
		Model: "gemini-2.5-flash-image", Provider: service.BatchImageProviderGeminiAPI,
		Items: []service.BatchImageSubmitItem{{CustomID: "one", Prompt: "test"}},
	}, "")
	require.ErrorIs(t, err, service.ErrBatchImageProviderSubmitFailed)
	require.Zero(t, client.createCalls)
	var batchID, status, code, hash string
	var hold float64
	require.NoError(t, db.QueryRow(`SELECT batch_id,status,last_error_code,request_hash,hold_amount FROM batch_image_jobs WHERE user_id=$1`, user).Scan(&batchID, &status, &code, &hash, &hold))
	require.Equal(t, "failed", status)
	require.Equal(t, "SUBMISSION_NOT_CREATED", code)
	require.Positive(t, hold)
	for _, receipt := range []string{service.BatchImageHoldRequestID(batchID), service.BatchImageReleaseRequestID(batchID)} {
		var count int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_billing_dedup WHERE request_id=$1`, receipt).Scan(&count))
		require.Equal(t, 1, count)
	}
	result, err := billing.ReleaseBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{RequestID: service.BatchImageReleaseRequestID(batchID), BatchID: batchID, UserID: user, APIKeyID: key, HoldAmount: hold, RequestPayloadHash: hash})
	require.NoError(t, err)
	require.False(t, result.Applied)
	var balance, frozen float64
	require.NoError(t, db.QueryRow(`SELECT balance,frozen_balance FROM users WHERE id=$1`, user).Scan(&balance, &frozen))
	require.Equal(t, float64(100), balance)
	require.Zero(t, frozen)
	var captures int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_billing_dedup WHERE request_id=$1`, service.BatchImageCaptureRequestID(batchID)).Scan(&captures))
	require.Zero(t, captures)
	t.Logf("batch=%s status=%s code=%s reserve=1 release=1 capture=0 balance=%g frozen=%g duplicate_release_applied=%v", batchID, status, code, balance, frozen, result.Applied)
}
