package service

import "context"

type unlimitedTPMRepository struct {
	UsageSettlementReceiptRepository
}

func (unlimitedTPMRepository) ReserveTPMAttempt(context.Context, TPMReservationCommand) (*TPMReservation, error) {
	return nil, nil
}
func (unlimitedTPMRepository) SettleTPMAttempt(context.Context, TPMReservationIdentity, int64) error {
	return nil
}
func (unlimitedTPMRepository) ReleaseUnsentTPMAttempt(context.Context, TPMReservationIdentity) error {
	return nil
}
func (unlimitedTPMRepository) MarkTPMAttemptUnknown(context.Context, TPMReservationIdentity) error {
	return nil
}

func installUnlimitedTPMFixture(plan *GatewayRequestPlan) {
	plan.settlement = NewUsageSettlementService(unlimitedTPMRepository{}, nil, nil)
}
