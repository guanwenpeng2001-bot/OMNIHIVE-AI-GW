package handler

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Existing transport scenarios use explicitly unlimited authoritative policies.
// TPM rejection cases use a separate repository with configured limits.
type unlimitedTPMRepository struct{ rejection error }

func (repository unlimitedTPMRepository) ReserveTPMAttempt(context.Context, service.TPMReservationCommand) (*service.TPMReservation, error) {
	return nil, repository.rejection
}
func (unlimitedTPMRepository) SettleTPMAttempt(context.Context, service.TPMReservationIdentity, int64) error {
	return nil
}
func (unlimitedTPMRepository) ReleaseUnsentTPMAttempt(context.Context, service.TPMReservationIdentity) error {
	return nil
}
func (unlimitedTPMRepository) MarkTPMAttemptUnknown(context.Context, service.TPMReservationIdentity) error {
	return nil
}

type unlimitedTPMReceiptStub struct {
	unlimitedTPMRepository
	service.UsageSettlementReceiptRepository
}

func (*unlimitedTPMReceiptStub) Persist(context.Context, *service.UsageSettlementReceipt) (*service.UsageSettlementReceipt, error) {
	return nil, errors.New("transport fixture has no financial receipt storage")
}
func (*unlimitedTPMReceiptStub) Get(context.Context, service.UsageSettlementKey) (*service.UsageSettlementReceipt, error) {
	return nil, service.ErrUsageSettlementNotFound
}
