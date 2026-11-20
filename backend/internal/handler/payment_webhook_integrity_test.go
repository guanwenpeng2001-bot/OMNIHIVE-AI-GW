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
	"github.com/gin-gonic/gin"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/enttest"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/payment"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestPaymentWebhookAcknowledgesOnlyVerifiedUnknownOrders(test *testing.T) {
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
		test.Run(scenario.name, func(test *testing.T) {
			database, err := sql.Open("sqlite", "file:"+strings.ReplaceAll(test.Name(), " ", "_")+"?mode=memory&cache=shared")
			require.NoError(test, err)
			_, err = database.Exec("PRAGMA foreign_keys = ON")
			require.NoError(test, err)
			client := enttest.NewClient(test, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, database))))
			test.Cleanup(func() { _ = client.Close() })
			registry := payment.NewRegistry()
			if scenario.register {
				registry.Register(webhookHandlerProviderStub{key: payment.TypeEasyPay, notification: &payment.PaymentNotification{OrderID: "unknown-verified", TradeNo: "trade", Status: payment.NotificationStatusSuccess}})
			}
			paymentService := service.NewPaymentService(client, registry, nil, nil, nil, nil, nil, nil, nil)
			paymentService.EnsureProviders(context.Background())
			if scenario.databaseFailure {
				require.NoError(test, client.Close())
			}
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			defer slog.SetDefault(previous)
			recorder := httptest.NewRecorder()
			request, _ := gin.CreateTestContext(recorder)
			request.Request = httptest.NewRequest(http.MethodPost, "/webhook/easypay", strings.NewReader("out_trade_no=unknown-verified"))
			NewPaymentWebhookHandler(paymentService, nil).EasyPayNotify(request)
			require.Equal(test, scenario.expected, recorder.Code)
			if scenario.expected == http.StatusOK {
				require.Equal(test, "success", recorder.Body.String())
				require.Contains(test, logs.String(), `"level":"WARN"`)
				require.Contains(test, logs.String(), `"outTradeNo":"unknown-verified"`)
			} else {
				require.NotEqual(test, "success", recorder.Body.String())
			}
		})
	}
}
