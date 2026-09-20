//go:build unit

package handler

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestPaymentWebhookAcknowledgesOnlyVerifiedUnknownOrders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []struct {
		name            string
		register        bool
		databaseFailure bool
		expected        int
	}{
		{"unknown order", true, false, http.StatusOK},
		{"database unavailable", true, true, http.StatusServiceUnavailable},
		{"provider missing", false, false, http.StatusServiceUnavailable},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			database, err := sql.Open("sqlite", "file:"+strings.ReplaceAll(t.Name(), " ", "_")+"?mode=memory&cache=shared")
			require.NoError(t, err)
			_, err = database.Exec("PRAGMA foreign_keys = ON")
			require.NoError(t, err)
			client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, database))))
			t.Cleanup(func() { _ = client.Close() })
			registry := payment.NewRegistry()
			if scenario.register {
				registry.Register(webhookHandlerProviderStub{key: payment.TypeEasyPay, notification: &payment.PaymentNotification{OrderID: "unknown-verified", TradeNo: "trade", Status: payment.NotificationStatusSuccess}})
			}
			paymentService := service.NewPaymentService(client, registry, nil, nil, nil, nil, nil, nil, nil)
			paymentService.EnsureProviders(context.Background())
			if scenario.databaseFailure {
				require.NoError(t, client.Close())
			}
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			defer slog.SetDefault(previous)
			recorder := httptest.NewRecorder()
			request, _ := gin.CreateTestContext(recorder)
			request.Request = httptest.NewRequest(http.MethodPost, "/webhook/easypay", strings.NewReader("out_trade_no=unknown-verified"))
			NewPaymentWebhookHandler(paymentService, registry).EasyPayNotify(request)
			require.Equal(t, scenario.expected, recorder.Code)
			if scenario.expected == http.StatusOK {
				require.Equal(t, "success", recorder.Body.String())
				require.Contains(t, logs.String(), `"level":"WARN"`)
				require.Contains(t, logs.String(), `"outTradeNo":"unknown-verified"`)
			} else {
				require.NotEqual(t, "success", recorder.Body.String())
			}
		})
	}
}
