//go:build unit

package payments

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestQueriedPaymentCannotAcceptAdjacentMinorUnit(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectQuery("(?s)SELECT p.id,t.payer_id,t.provider_minor.*FOR UPDATE OF p").
		WithArgs(payment.TypeAlipay, "3", "merchant-order").
		WillReturnRows(sqlmock.NewRows([]string{"id", "payer", "minor", "group_id", "type", "status", "trade",
			"currency", "merchant", "app", "captured", "in_grace"}).
			AddRow(1, 2, 10000, nil, payment.OrderTypeBalance, payment.OrderStatusPending, "", "CNY", "", "", false, true))
	mock.ExpectRollback()
	err = (Store{DB: db}).HandleVerified(t.Context(), payment.TypeAlipay, &payment.PaymentNotification{
		EventID: "query/merchant-order/rail-transaction", EffectID: "rail-transaction", InstanceID: "3",
		Currency: "CNY", Minor: 9999, Amount: 100, TradeNo: "rail-transaction",
		OrderID: "merchant-order", Status: payment.ProviderStatusSuccess,
	})
	require.ErrorIs(t, err, ErrMismatch)
	require.NoError(t, mock.ExpectationsWereMet())
}
