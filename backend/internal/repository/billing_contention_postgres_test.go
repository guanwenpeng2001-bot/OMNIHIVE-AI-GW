//go:build unit

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBillingMixedContentionPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated job_test PostgreSQL")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(32)
	var name string
	require.NoError(t, db.QueryRow(`SELECT current_database()`).Scan(&name))
	require.Equal(t, "job_test", name)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	require.NoError(t, ApplyMigrations(ctx, db))
	for _, tc := range []struct {
		name      string
		payers    int
		admission bool
	}{
		{"single_payer_database_authority", 1, false},
		{"multiple_payers_database_authority", 4, false},
		{"multiple_payers_with_admission", 4, true},
	} {
		t.Run(tc.name, func(t *testing.T) { runBillingMixedContention(t, ctx, db, tc.payers, tc.admission) })
	}
	t.Run("quota_batches", func(t *testing.T) { runBillingQuotaBatches(t, ctx, db) })
}

func runBillingQuotaBatches(t *testing.T, ctx context.Context, db *sql.DB) {
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	quotas := NewUserPlatformQuotaRepository(client)
	var users [2]int64
	t.Cleanup(func() {
		for _, user := range users {
			if user != 0 {
				_, err := db.Exec(`DELETE FROM users WHERE id=$1`, user)
				require.NoError(t, err)
			}
		}
	})
	limit := 1000.0
	var records []UserPlatformQuotaRecord
	var snapshots []UserPlatformQuotaSnapshot
	now := time.Now()
	for i := range users {
		require.NoError(t, db.QueryRow(`INSERT INTO users(email,password_hash) VALUES($1,'test') RETURNING id`, uuid.NewString()+"@quota-batch.test").Scan(&users[i]))
		for _, platform := range []string{"qianwen", "openai"} {
			records = append(records, UserPlatformQuotaRecord{UserID: users[i], Platform: platform, DailyLimitUSD: &limit})
			snapshots = append(snapshots, UserPlatformQuotaSnapshot{UserID: users[i], Platform: platform, DailyUsageUSD: 7, WeeklyUsageUSD: 7, MonthlyUsageUSD: 7, DailyWindowStart: now, WeeklyWindowStart: now, MonthlyWindowStart: now})
		}
	}
	require.NoError(t, quotas.BulkInsertInitial(ctx, records))
	reversedRecords := append([]UserPlatformQuotaRecord(nil), records...)
	reversedSnapshots := append([]UserPlatformQuotaSnapshot(nil), snapshots...)
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		reversedRecords[i], reversedRecords[j] = reversedRecords[j], reversedRecords[i]
		reversedSnapshots[i], reversedSnapshots[j] = reversedSnapshots[j], reversedSnapshots[i]
	}
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for worker := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch worker % 4 {
			case 0:
				errs <- quotas.BulkInsertInitial(ctx, reversedRecords)
			case 1:
				errs <- quotas.BatchSnapshotUsage(ctx, reversedSnapshots, now)
			case 2:
				errs <- quotas.BatchSnapshotUsage(ctx, snapshots, now)
			case 3:
				errs <- quotas.UpsertForUser(ctx, users[worker%2], []UserPlatformQuotaRecord{{Platform: "qianwen", DailyLimitUSD: &limit}, {Platform: "openai", DailyLimitUSD: &limit}})
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	for _, user := range users {
		rows, err := quotas.ListByUser(ctx, user)
		require.NoError(t, err)
		require.Len(t, rows, 2)
		for _, row := range rows {
			require.Equal(t, 7.0, row.DailyUsageUSD)
			require.Equal(t, limit, *row.DailyLimitUSD)
		}
	}
	require.Equal(t, "qianwen", records[0].Platform, "sorting must not mutate caller-owned records")
	require.Equal(t, "qianwen", snapshots[0].Platform, "sorting must not mutate caller-owned snapshots")
}

func runBillingMixedContention(t *testing.T, ctx context.Context, db *sql.DB, payerCount int, admission bool) {
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	users := newUserRepositoryWithSQL(client, db)
	subs := NewUserSubscriptionRepository(client).(*userSubscriptionRepository)
	billing := NewUsageBillingRepository(client, db)
	receipts := NewUsageSettlementReceiptWithQuotaRepository(db).(*usageSettlementReceiptRepository)
	var group, account int64
	require.NoError(t, db.QueryRow(`INSERT INTO groups(name,platform) VALUES($1,'qianwen') RETURNING id`, uuid.NewString()).Scan(&group))
	require.NoError(t, db.QueryRow(`INSERT INTO accounts(name,platform,type) VALUES($1,'qianwen','apikey') RETURNING id`, uuid.NewString()).Scan(&account))
	type payer struct {
		user, key, sub int64
		capture        atomic.Bool
		rows           []*service.UsageSettlementReceipt
		batch          string
	}
	payers := make([]*payer, payerCount)
	t.Cleanup(func() {
		for _, p := range payers {
			if p == nil || p.user == 0 {
				continue
			}
			_, err := db.Exec(`DELETE FROM billing_movement_links WHERE receipt_id IN (SELECT id FROM usage_settlement_receipts WHERE user_id=$1)`, p.user)
			require.NoError(t, err)
			_, err = db.Exec(`DELETE FROM billing_settlement_outbox WHERE receipt_id IN (SELECT id FROM usage_settlement_receipts WHERE user_id=$1)`, p.user)
			require.NoError(t, err)
			_, err = db.Exec(`DELETE FROM usage_settlement_receipts WHERE user_id=$1`, p.user)
			require.NoError(t, err)
			_, err = db.Exec(`DELETE FROM user_platform_quota_events WHERE user_id=$1`, p.user)
			require.NoError(t, err)
			for _, table := range []string{"usage_billing_dedup", "usage_billing_dedup_archive"} {
				_, err := db.Exec("DELETE FROM "+table+" WHERE api_key_id=$1", p.key)
				require.NoError(t, err)
			}
			_, err = db.Exec(`DELETE FROM users WHERE id=$1`, p.user)
			require.NoError(t, err)
		}
		_, err := db.Exec(`DELETE FROM accounts WHERE id=$1`, account)
		require.NoError(t, err)
		_, err = db.Exec(`DELETE FROM groups WHERE id=$1`, group)
		require.NoError(t, err)
	})
	amount := 0.25
	snapshot, err := json.Marshal(service.PricingSnapshot{PricingState: service.PricingStatePriced, Model: "contention", Provider: "qianwen", Mode: "image", Source: "configured", UnitPrice: &amount, Units: 1, RateMultiplier: 1, TimeMultiplier: 1, ReasoningMultiplier: 1, AccountRateMultiplier: 1, TotalCost: amount, ActualCost: amount, ChargedAmount: amount})
	require.NoError(t, err)
	for i := range payers {
		p := &payer{batch: uuid.NewString()}
		payers[i] = p
		require.NoError(t, db.QueryRow(`INSERT INTO users(email,password_hash,balance) VALUES($1,'test',1000) RETURNING id`, uuid.NewString()+"@billing.test").Scan(&p.user))
		require.NoError(t, db.QueryRow(`INSERT INTO api_keys(user_id,key,name,quota) VALUES($1,$2,'mixed',1000) RETURNING id`, p.user, uuid.NewString()).Scan(&p.key))
		require.NoError(t, db.QueryRow(`INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at) VALUES($1,$2,NOW()-INTERVAL '2 days',NOW()-INTERVAL '1 day') RETURNING id`, p.user, group).Scan(&p.sub))
		_, err := db.Exec(`INSERT INTO user_platform_quotas(user_id,platform,daily_limit_usd) VALUES($1,'qianwen',1000)`, p.user)
		require.NoError(t, err)
		for _, hold := range []struct {
			batch  string
			amount float64
		}{{p.batch, 2}, {uuid.NewString(), 20}} {
			_, err := billing.ReserveBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{RequestID: service.BatchImageHoldRequestID(hold.batch), BatchID: hold.batch, UserID: p.user, APIKeyID: p.key, HoldAmount: hold.amount})
			require.NoError(t, err)
		}
		for n := range 24 {
			id := uuid.NewString()
			input := &service.UsageSettlementReceipt{Key: service.UsageSettlementKey{RequestID: id, UserID: p.user, APIKeyID: p.key}, Command: service.UsageBillingCommand{RequestID: id, UserID: p.user, APIKeyID: p.key, AccountID: account, AccountType: service.AccountTypeAPIKey, Model: "contention", BalanceCost: amount, APIKeyQuotaCost: amount, AccountQuotaCost: amount}, Usage: service.UsageLog{RequestID: id, UserID: p.user, APIKeyID: p.key, AccountID: account, Model: "contention", QuotaPlatform: "qianwen", TotalCost: amount, ActualCost: amount, RateMultiplier: 1, PricingState: service.PricingStatePriced, PricingSnapshot: string(snapshot)}, ResolutionState: "resolved", CompletionState: "complete"}
			if n%2 == 1 {
				input.Command.BalanceCost, input.Command.SubscriptionCost = 0, amount
				input.Command.BillingType, input.Usage.BillingType = service.BillingTypeSubscription, service.BillingTypeSubscription
				input.Command.SubscriptionID, input.Usage.SubscriptionID = &p.sub, &p.sub
				input.Usage.GroupID = &group
			}
			row, err := receipts.Persist(ctx, input)
			require.NoError(t, err)
			p.rows = append(p.rows, row)
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, 300)
	start := make(chan struct{})
	run := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := fn(); err != nil {
				errs <- err
			}
		}()
	}
	for _, p := range payers {
		for _, row := range p.rows {
			for range 2 {
				run(func() error {
					if admission {
						_, err := receipts.Settle(ctx, row.Key, row.Command.RequestFingerprint)
						return err
					}
					// Bypass local admission to model independent processes. Only DB
					// locks/idempotency protect these transactions; no retry masks a cycle.
					_, err := receipts.settle(ctx, row.Key, row.Command.RequestFingerprint, false)
					return err
				})
			}
		}
		for range 8 {
			run(func() error {
				tx, err := client.Tx(ctx)
				if err != nil {
					return err
				}
				defer tx.Rollback()
				txCtx := dbent.NewTxContext(ctx, tx)
				if err := users.UpdateBalance(txCtx, p.user, 1); err != nil {
					return err
				}
				if _, err := users.DeductAvailableBalance(txCtx, p.user, 0.4); err != nil {
					return err
				}
				return tx.Commit()
			})
		}
		for _, operation := range []string{"capture", "release"} {
			run(func() error {
				cmd := &service.BatchImageBalanceHoldCommand{RequestID: service.BatchImageReleaseRequestID(p.batch), BatchID: p.batch, UserID: p.user, APIKeyID: p.key, HoldAmount: 2}
				if operation == "capture" {
					cmd.RequestID = service.BatchImageCaptureRequestID(p.batch)
					cmd.ActualAmount = 1
				}
				cmd.Normalize()
				apply := releaseUsageBillingBatchImageBalance
				if operation == "capture" {
					apply = captureUsageBillingBatchImageBalance
				}
				result, err := (&usageBillingRepository{db: db}).applyBatchImageBalanceHoldOnce(ctx, cmd, operation, apply)
				if errors.Is(err, service.ErrUsageBillingRequestConflict) {
					return nil
				}
				if err == nil && operation == "capture" && result.Applied {
					p.capture.Store(true)
				}
				return err
			})
		}
	}
	for range 8 {
		run(func() error { _, err := subs.BatchUpdateExpiredStatus(ctx); return err })
	}
	started := time.Now()
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	for _, p := range payers {
		var balance, frozen, subscriptionUsage, keyUsage float64
		var count int
		require.NoError(t, db.QueryRow(`SELECT balance,frozen_balance FROM users WHERE id=$1`, p.user).Scan(&balance, &frozen))
		expected := 1000.0 - 20 - 3 + 4.8
		if p.capture.Load() {
			expected--
		}
		require.InDelta(t, expected, balance, 0.00000001)
		require.Equal(t, 20.0, frozen, "terminal race must not consume another job's hold")
		require.NoError(t, db.QueryRow(`SELECT daily_usage_usd FROM user_subscriptions WHERE id=$1`, p.sub).Scan(&subscriptionUsage))
		require.Equal(t, 3.0, subscriptionUsage)
		require.NoError(t, db.QueryRow(`SELECT quota_used FROM api_keys WHERE id=$1`, p.key).Scan(&keyUsage))
		require.Equal(t, 6.0, keyUsage)
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_logs WHERE user_id=$1`, p.user).Scan(&count))
		require.Equal(t, 24, count)
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM user_platform_quota_events WHERE user_id=$1`, p.user).Scan(&count))
		require.Equal(t, 12, count)
	}
	var accountUsage float64
	require.NoError(t, db.QueryRow(`SELECT (extra->>'quota_used')::numeric FROM accounts WHERE id=$1`, account).Scan(&accountUsage))
	require.Equal(t, float64(payerCount)*6, accountUsage)
	stats, err := json.Marshal(billingContentionSnapshot())
	require.NoError(t, err)
	t.Logf("mixed %d payers, admission=%t / %d receipts replayed twice + %d recharge/refund transactions + %d terminal operations + 8 subscription batches: %s; contention=%s", payerCount, admission, 24*payerCount, 8*payerCount, 2*payerCount, time.Since(started), stats)

	t.Run("backpressure leaves pending recoverable", func(t *testing.T) {
		p := payers[0]
		input := *p.rows[0]
		input.Key.RequestID = uuid.NewString()
		input.Command.RequestID, input.Command.RequestFingerprint = input.Key.RequestID, ""
		input.Usage.RequestID = input.Key.RequestID
		stored, err := receipts.Persist(ctx, &input)
		require.NoError(t, err)
		release, err := billingAdmission.acquire(ctx, p.user)
		require.NoError(t, err)
		blocked, stop := context.WithTimeout(ctx, 20*time.Millisecond)
		defer stop()
		_, err = receipts.Settle(blocked, stored.Key, stored.Command.RequestFingerprint)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		release()
		pending, err := receipts.Get(ctx, stored.Key)
		require.NoError(t, err)
		require.Equal(t, service.UsageSettlementPending, pending.State)
		restarted := service.NewUsageSettlementService(NewUsageSettlementReceiptWithQuotaRepository(db), nil, nil)
		require.NoError(t, restarted.Recover(ctx))
		settled, err := receipts.Get(ctx, stored.Key)
		require.NoError(t, err)
		require.Equal(t, service.UsageSettlementSettled, settled.State)
	})

	t.Run("refund lock is compatible with usage foreign key", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer tx.Rollback()
		var id int64
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 FOR KEY SHARE`, payers[0].user).Scan(&id))
		limited, stop := context.WithTimeout(ctx, time.Second)
		defer stop()
		_, err = users.DeductAvailableBalance(limited, id, 0.1)
		require.NoError(t, err, fmt.Sprintf("refund must not wait for usage FK reader %d", id))
	})
	t.Run("terminal rollback replay and archive", func(t *testing.T) {
		p := payers[0]
		batch := uuid.NewString()
		cmd := &service.BatchImageBalanceHoldCommand{RequestID: service.BatchImageCaptureRequestID(batch), BatchID: batch, UserID: p.user, APIKeyID: p.key, HoldAmount: 1, ActualAmount: 0.25}
		_, err := billing.CaptureBatchImageBalance(ctx, cmd)
		require.ErrorIs(t, err, service.ErrBatchImageBillingHoldFailed)
		_, err = billing.ReserveBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{RequestID: service.BatchImageHoldRequestID(batch), BatchID: batch, UserID: p.user, APIKeyID: p.key, HoldAmount: 1})
		require.NoError(t, err)
		cmd.ActualAmount = 2
		_, err = billing.CaptureBatchImageBalance(ctx, cmd)
		require.ErrorIs(t, err, service.ErrBatchImageSettlementCostExceedsHold)
		cmd.ActualAmount = 0.25
		result, err := billing.CaptureBatchImageBalance(ctx, cmd)
		require.NoError(t, err)
		require.True(t, result.Applied)
		result, err = billing.CaptureBatchImageBalance(ctx, cmd)
		require.NoError(t, err)
		require.False(t, result.Applied)
		_, err = db.Exec(`INSERT INTO usage_billing_dedup_archive(request_id,api_key_id,request_fingerprint,created_at) SELECT request_id,api_key_id,request_fingerprint,created_at FROM usage_billing_dedup WHERE request_id IN ($1,$2) AND api_key_id=$3`, cmd.RequestID, "batch_image_terminal:"+batch, p.key)
		require.NoError(t, err)
		_, err = db.Exec(`DELETE FROM usage_billing_dedup WHERE request_id IN ($1,$2) AND api_key_id=$3`, cmd.RequestID, "batch_image_terminal:"+batch, p.key)
		require.NoError(t, err)
		result, err = billing.CaptureBatchImageBalance(ctx, cmd)
		require.NoError(t, err)
		require.False(t, result.Applied)
		_, err = billing.ReleaseBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{RequestID: service.BatchImageReleaseRequestID(batch), BatchID: batch, UserID: p.user, APIKeyID: p.key, HoldAmount: 1})
		require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
		// Simulate a historical capture created before the shared terminal key.
		_, err = db.Exec(`DELETE FROM usage_billing_dedup_archive WHERE request_id=$1 AND api_key_id=$2`, "batch_image_terminal:"+batch, p.key)
		require.NoError(t, err)
		_, err = billing.ReleaseBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{RequestID: service.BatchImageReleaseRequestID(batch), BatchID: batch, UserID: p.user, APIKeyID: p.key, HoldAmount: 1})
		require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
	})
}
