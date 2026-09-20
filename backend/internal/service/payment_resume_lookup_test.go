//go:build unit

package service

import (
	"context"
	"errors"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type paymentResumeLookupProvider struct {
	queryCount   int
	queryStarted chan struct{}
	queryRelease chan struct{}
	afterQuery   func()
}

func (p *paymentResumeLookupProvider) Name() string { return "resume-lookup-provider" }

func (p *paymentResumeLookupProvider) ProviderKey() string { return payment.TypeAlipay }

func (p *paymentResumeLookupProvider) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeAlipay}
}

func (p *paymentResumeLookupProvider) CreatePayment(context.Context, payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	panic("unexpected call")
}

func (provider *paymentResumeLookupProvider) QueryOrder(context.Context, string) (*payment.QueryOrderResponse, error) {
	provider.queryCount++
	if provider.queryStarted != nil {
		close(provider.queryStarted)
		<-provider.queryRelease
	}
	if provider.afterQuery != nil {
		provider.afterQuery()
	}
	return &payment.QueryOrderResponse{Status: payment.ProviderStatusPending}, nil
}

func (p *paymentResumeLookupProvider) VerifyNotification(context.Context, string, map[string]string) (*payment.PaymentNotification, error) {
	panic("unexpected call")
}

func (p *paymentResumeLookupProvider) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	panic("unexpected call")
}

func TestGetPublicOrderByResumeTokenReturnsMatchingOrder(test *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(test)
	user, err := client.User.Create().
		SetEmail("resume@example.com").
		SetPasswordHash("hash").
		SetUsername("resume-user").
		Save(ctx)
	require.NoError(test, err)

	instanceID := "12"
	providerKey := payment.TypeEasyPay
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("RESUME-ORDER").
		SetOutTradeNo("sub2_resume_lookup").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-1").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(instanceID).
		SetProviderKey(providerKey).
		Save(ctx)
	require.NoError(test, err)

	resumeSvc := NewPaymentResumeService([]byte("0123456789abcdef0123456789abcdef"))
	token, err := resumeSvc.CreateToken(ResumeTokenClaims{
		OrderID:            order.ID,
		UserID:             user.ID,
		ProviderInstanceID: instanceID,
		ProviderKey:        providerKey,
		PaymentType:        payment.TypeAlipay,
		CanonicalReturnURL: "https://app.example.com/payment/result",
	})
	require.NoError(test, err)

	svc := &PaymentService{
		entClient:     client,
		resumeService: resumeSvc,
	}

	got, err := svc.GetPublicOrderByResumeToken(ctx, token)
	require.NoError(test, err)
	require.Equal(test, order.ID, got.ID)
	_, err = svc.GetPublicOrderByResumeToken(ctx, token)
	require.ErrorContains(test, err, "already been consumed")
	// A fresh service instance shares the database consumption marker.
	restarted := &PaymentService{entClient: client, resumeService: resumeSvc}
	_, err = restarted.GetPublicOrderByResumeToken(ctx, token)
	require.ErrorContains(test, err, "already been consumed")
	next, err := svc.RenewPaymentResumeToken(token)
	require.NoError(test, err)
	require.NotEqual(test, token, next)
	nextClaims, err := resumeSvc.ParseToken(next)
	require.NoError(test, err)
	originalClaims, err := resumeSvc.ParseToken(token)
	require.NoError(test, err)
	require.Equal(test, originalClaims.ExpiresAt, nextClaims.ExpiresAt)
	_, err = restarted.GetPublicOrderByResumeToken(ctx, next)
	require.NoError(test, err)

}

func TestGetPublicOrderByResumeTokenRejectsSnapshotMismatch(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user, err := client.User.Create().
		SetEmail("resume-mismatch@example.com").
		SetPasswordHash("hash").
		SetUsername("resume-mismatch-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("RESUME-MISMATCH").
		SetOutTradeNo("sub2_resume_lookup_mismatch").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-2").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID("12").
		SetProviderKey(payment.TypeEasyPay).
		Save(ctx)
	require.NoError(t, err)

	resumeSvc := NewPaymentResumeService([]byte("0123456789abcdef0123456789abcdef"))
	token, err := resumeSvc.CreateToken(ResumeTokenClaims{
		OrderID:            order.ID,
		UserID:             user.ID,
		ProviderInstanceID: "99",
		ProviderKey:        payment.TypeEasyPay,
		PaymentType:        payment.TypeAlipay,
		CanonicalReturnURL: "https://app.example.com/payment/result",
	})
	require.NoError(t, err)

	svc := &PaymentService{
		entClient:     client,
		resumeService: resumeSvc,
	}

	_, err = svc.GetPublicOrderByResumeToken(ctx, token)
	require.Error(t, err)
	require.Equal(t, "INVALID_RESUME_TOKEN", infraerrors.Reason(err))
}

func TestGetPublicOrderByResumeTokenUsesSnapshotAuthorityWhenColumnsDiffer(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user, err := client.User.Create().
		SetEmail("resume-snapshot-authority@example.com").
		SetPasswordHash("hash").
		SetUsername("resume-snapshot-authority-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("RESUME-SNAPSHOT-AUTHORITY").
		SetOutTradeNo("sub2_resume_snapshot_authority").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-snapshot-authority").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID("legacy-column-instance").
		SetProviderKey(payment.TypeAlipay).
		SetProviderSnapshot(map[string]any{
			"schema_version":       2,
			"provider_instance_id": "snapshot-instance",
			"provider_key":         payment.TypeEasyPay,
		}).
		Save(ctx)
	require.NoError(t, err)

	resumeSvc := NewPaymentResumeService([]byte("0123456789abcdef0123456789abcdef"))
	token, err := resumeSvc.CreateToken(ResumeTokenClaims{
		OrderID:            order.ID,
		UserID:             user.ID,
		ProviderInstanceID: "snapshot-instance",
		ProviderKey:        payment.TypeEasyPay,
		PaymentType:        payment.TypeAlipay,
		CanonicalReturnURL: "https://app.example.com/payment/result",
	})
	require.NoError(t, err)

	svc := &PaymentService{
		entClient:     client,
		resumeService: resumeSvc,
	}

	got, err := svc.GetPublicOrderByResumeToken(ctx, token)
	require.NoError(t, err)
	require.Equal(t, order.ID, got.ID)
}

func TestGetPublicOrderByResumeTokenChecksUpstreamForPendingOrder(test *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(test)
	user, err := client.User.Create().
		SetEmail("resume-refresh@example.com").
		SetPasswordHash("hash").
		SetUsername("resume-refresh-user").
		Save(ctx)
	require.NoError(test, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("RESUME-PENDING").
		SetOutTradeNo("sub2_resume_lookup_pending").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-pending").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(test, err)

	resumeSvc := NewPaymentResumeService([]byte("0123456789abcdef0123456789abcdef"))
	token, err := resumeSvc.CreateToken(ResumeTokenClaims{
		OrderID:            order.ID,
		UserID:             user.ID,
		PaymentType:        payment.TypeAlipay,
		CanonicalReturnURL: "https://app.example.com/payment/result",
	})
	require.NoError(test, err)

	registry := payment.NewRegistry()
	provider := &paymentResumeLookupProvider{}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		resumeService:   resumeSvc,
		providersLoaded: true,
	}

	got, err := svc.GetPublicOrderByResumeToken(ctx, token)
	require.NoError(test, err)
	require.Equal(test, order.ID, got.ID)
	require.Equal(test, 1, provider.queryCount)
	_, err = svc.GetPublicOrderByResumeToken(ctx, token)
	require.ErrorContains(test, err, "already been consumed")
	require.Equal(test, 1, provider.queryCount, "replay must not query upstream")

	nextToken, err := svc.RenewPaymentResumeToken(token)
	require.NoError(test, err)
	provider.queryStarted = make(chan struct{})
	provider.queryRelease = make(chan struct{})
	lookupDone := make(chan error, 1)
	go func() { _, lookupError := svc.GetPublicOrderByResumeToken(ctx, nextToken); lookupDone <- lookupError }()
	<-provider.queryStarted
	concurrentClaim := make(chan struct{})
	var observeClaim atomic.Bool
	observeClaim.Store(true)
	client.IdempotencyRecord.Use(func(next dbent.Mutator) dbent.Mutator {
		return dbent.MutateFunc(func(mutationContext context.Context, mutation dbent.Mutation) (dbent.Value, error) {
			if observeClaim.Swap(false) {
				close(concurrentClaim)
			}
			return next.Mutate(mutationContext, mutation)
		})
	})
	replayDone := make(chan error, 1)
	go func() { _, replayError := svc.GetPublicOrderByResumeToken(ctx, nextToken); replayDone <- replayError }()
	<-concurrentClaim
	close(provider.queryRelease)
	require.NoError(test, <-lookupDone)
	require.Error(test, <-replayDone)

	require.Equal(test, 2, provider.queryCount, "concurrent replay must have one executor")
	provider.queryStarted = nil

	retryToken, err := svc.RenewPaymentResumeToken(nextToken)
	require.NoError(test, err)
	cancelledContext, cancelLookup := context.WithCancel(ctx)
	provider.afterQuery = cancelLookup
	_, err = svc.GetPublicOrderByResumeToken(cancelledContext, retryToken)
	require.Error(test, err)
	provider.afterQuery = nil
	_, err = svc.GetPublicOrderByResumeToken(ctx, retryToken)
	require.NoError(test, err, "failed lookup must roll back its claim")
	require.Equal(test, 4, provider.queryCount)

	beforeClaimToken, err := svc.RenewPaymentResumeToken(retryToken)
	require.NoError(test, err)
	var failCreate atomic.Bool
	failCreate.Store(true)
	client.IdempotencyRecord.Use(func(next dbent.Mutator) dbent.Mutator {
		return dbent.MutateFunc(func(mutationContext context.Context, mutation dbent.Mutation) (dbent.Value, error) {
			if mutation.Op().Is(dbent.OpCreate) && failCreate.Swap(false) {
				return nil, errors.New("temporary claim failure")
			}
			return next.Mutate(mutationContext, mutation)
		})
	})
	_, err = svc.GetPublicOrderByResumeToken(ctx, beforeClaimToken)
	require.ErrorContains(test, err, "temporary claim failure")
	require.Equal(test, 4, provider.queryCount)
	_, err = svc.GetPublicOrderByResumeToken(ctx, beforeClaimToken)
	require.NoError(test, err)
	require.Equal(test, 5, provider.queryCount)
}

func TestVerifyOrderPublicRejectsOrderNumberWithoutQueryingProvider(test *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(test)
	user, err := client.User.Create().
		SetEmail("public-verify@example.com").
		SetPasswordHash("hash").
		SetUsername("public-verify-user").
		Save(ctx)
	require.NoError(test, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("PUBLIC-VERIFY").
		SetOutTradeNo("sub2_public_verify_pending").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-public-verify").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(test, err)

	registry := payment.NewRegistry()
	provider := &paymentResumeLookupProvider{}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	got, err := svc.VerifyOrderPublic(ctx, order.OutTradeNo)
	require.Error(test, err)
	require.Nil(test, got)
	require.Equal(test, "RESUME_TOKEN_REQUIRED", infraerrors.Reason(err))
	require.Equal(test, 0, provider.queryCount)
}

func TestVerifyOrderPublicRejectsBlankOutTradeNo(test *testing.T) {
	svc := &PaymentService{
		entClient: newPaymentConfigServiceTestClient(test),
	}

	_, err := svc.VerifyOrderPublic(context.Background(), "   ")
	require.Error(test, err)
	require.Equal(test, "RESUME_TOKEN_REQUIRED", infraerrors.Reason(err))
}
