//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/stretchr/testify/require"
)

type transactionProxyRepository struct{ ProxyRepository }

func (repository *transactionProxyRepository) Create(_ context.Context, proxy *Proxy) error {
	proxy.ID = 1
	return nil
}

type transactionProxyProber struct{ requests chan string }

func (prober *transactionProxyProber) ProbeProxy(_ context.Context, endpoint string) (*ProxyExitInfo, int64, error) {
	prober.requests <- endpoint
	return nil, 0, errors.New("synthetic probe result")
}

func TestCreateProxyProbeFollowsTransactionOutcome(t *testing.T) {
	for _, outcome := range []string{"rollback", "commit", "ordinary"} {
		t.Run(outcome, func(t *testing.T) {
			_, client := newAuthPendingIdentityServiceTestClient(t)
			ctx := context.Background()
			var transaction *dbent.Tx
			if outcome != "ordinary" {
				var err error
				transaction, err = client.Tx(ctx)
				require.NoError(t, err)
				ctx = dbent.NewTxContext(ctx, transaction)
			}
			prober := &transactionProxyProber{requests: make(chan string, 1)}
			admin := &adminServiceImpl{proxyRepo: &transactionProxyRepository{}, proxyProber: prober}
			_, err := admin.CreateProxy(ctx, &CreateProxyInput{Name: "transaction-proxy", Protocol: "http", Host: "127.0.0.1", Port: 8081})
			require.NoError(t, err)
			if transaction != nil {
				select {
				case <-prober.requests:
					t.Fatal("probe ran before the transaction committed")
				case <-time.After(50 * time.Millisecond):
				}
				if outcome == "rollback" {
					require.NoError(t, transaction.Rollback())
				} else {
					require.NoError(t, transaction.Commit())
				}
			}
			if outcome == "rollback" {
				select {
				case <-prober.requests:
					t.Fatal("rolled-back proxy caused an external request")
				case <-time.After(50 * time.Millisecond):
				}
			} else {
				select {
				case endpoint := <-prober.requests:
					require.Contains(t, endpoint, "127.0.0.1:8081")
				case <-time.After(time.Second):
					t.Fatal("persisted proxy was never probed")
				}
			}
		})
	}
}
