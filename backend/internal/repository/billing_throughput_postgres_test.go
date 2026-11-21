//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
)

func BenchmarkSamePayerSettlementPostgres(b *testing.B) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		b.Skip("set UPSTREAM_JOB_TEST_DSN to job_test")
	}
	db, err := testresources.OpenPostgres(b.Context(), dsn, "job_test")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(32)
	ctx := context.Background()
	if err := ApplyMigrations(ctx, db); err != nil {
		b.Fatal(err)
	}
	repo := NewUsageSettlementReceiptRepository(db)
	amount := 0.000001
	snapshot, err := json.Marshal(service.PricingSnapshot{PricingState: service.PricingStatePriced, Model: "benchmark", Provider: "qianwen", Mode: "image", Source: "configured", UnitPrice: &amount, Units: 1, RateMultiplier: 1, TimeMultiplier: 1, ReasoningMultiplier: 1, TotalCost: amount, ActualCost: amount, ChargedAmount: amount})
	if err != nil {
		b.Fatal(err)
	}
	original := billingAdmission
	defer func() { billingAdmission = original }()
	for _, lanes := range []int{1, original.maxPayerActive} {
		b.Run(fmt.Sprintf("lanes_%d", lanes), func(b *testing.B) {
			billingAdmission = newBillingAdmission(32, 1024, 64)
			billingAdmission.maxPayerActive = lanes
			for iteration := 0; iteration < b.N; iteration++ {
				var userID, keyID, accountID int64
				if err := db.QueryRow(`INSERT INTO users(email,password_hash,balance) VALUES($1,'test',100) RETURNING id`, uuid.NewString()+"@benchmark.test").Scan(&userID); err != nil {
					b.Fatal(err)
				}
				if err := db.QueryRow(`INSERT INTO api_keys(user_id,key,name) VALUES($1,$2,'benchmark') RETURNING id`, userID, uuid.NewString()).Scan(&keyID); err != nil {
					b.Fatal(err)
				}
				if err := db.QueryRow(`INSERT INTO accounts(name,platform,type) VALUES($1,'qianwen','apikey') RETURNING id`, uuid.NewString()).Scan(&accountID); err != nil {
					b.Fatal(err)
				}
				const receipts = 64
				rows := make([]*service.UsageSettlementReceipt, 0, receipts)
				for range receipts {
					requestID := uuid.NewString()
					input := &service.UsageSettlementReceipt{Key: service.UsageSettlementKey{RequestID: requestID, UserID: userID, APIKeyID: keyID}, Command: service.UsageBillingCommand{RequestID: requestID, APIKeyID: keyID, UserID: userID, AccountID: accountID, AccountType: service.AccountTypeAPIKey, Model: "benchmark", BalanceCost: amount}, Usage: service.UsageLog{RequestID: requestID, UserID: userID, APIKeyID: keyID, AccountID: accountID, Model: "benchmark", TotalCost: amount, ActualCost: amount, RateMultiplier: 1, PricingState: service.PricingStatePriced, PricingSnapshot: string(snapshot)}, ResolutionState: "resolved", CompletionState: "complete"}
					stored, err := repo.Persist(ctx, input)
					if err != nil {
						b.Fatal(err)
					}
					rows = append(rows, stored)
				}
				started := time.Now()
				var wg sync.WaitGroup
				errs := make(chan error, receipts)
				for _, row := range rows {
					wg.Add(1)
					go func() {
						defer wg.Done()
						_, err := repo.Settle(ctx, row.Key, row.Command.RequestFingerprint)
						errs <- err
					}()
				}
				wg.Wait()
				close(errs)
				for err := range errs {
					if err != nil {
						b.Fatal(err)
					}
				}
				var settled int
				var balance float64
				if err := db.QueryRow(`SELECT count(*) FROM usage_settlement_receipts WHERE user_id=$1 AND state='settled'`, userID).Scan(&settled); err != nil {
					b.Fatal(err)
				}
				if err := db.QueryRow(`SELECT balance FROM users WHERE id=$1`, userID).Scan(&balance); err != nil {
					b.Fatal(err)
				}
				if settled != receipts || balance != 100-amount*receipts {
					b.Fatalf("settled=%d balance=%f", settled, balance)
				}
				elapsed := time.Since(started)
				b.ReportMetric(float64(receipts)/elapsed.Seconds(), "receipts/s")
				b.Logf("64 same-payer receipts: %s (%.2f receipts/s)", elapsed, float64(receipts)/elapsed.Seconds())
				for _, statement := range []string{
					`DELETE FROM billing_movement_links WHERE receipt_id IN (SELECT id FROM usage_settlement_receipts WHERE user_id=$1)`,
					`DELETE FROM billing_settlement_outbox WHERE receipt_id IN (SELECT id FROM usage_settlement_receipts WHERE user_id=$1)`,
					`DELETE FROM usage_settlement_receipts WHERE user_id=$1`,
					`DELETE FROM usage_billing_dedup WHERE api_key_id IN (SELECT id FROM api_keys WHERE user_id=$1)`,
				} {
					if _, err := db.Exec(statement, userID); err != nil {
						b.Fatal(err)
					}
				}
				if _, err := db.Exec(`DELETE FROM users WHERE id=$1`, userID); err != nil {
					b.Fatal(fmt.Errorf("cleanup user: %w", err))
				}
				if _, err := db.Exec(`DELETE FROM accounts WHERE id=$1`, accountID); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
