package service

import "context"

type accountPlanObservationKey struct{}

// Only the service's upstream-response observer can mark this write. HTTP
// management input cannot turn an account edit into an observation by shape.
func withAccountPlanObservation(ctx context.Context, account *Account) context.Context {
	return context.WithValue(WithAccountRuntimeIdentity(ctx, account), accountPlanObservationKey{}, account.ID)
}

func IsAccountPlanObservation(ctx context.Context, accountID int64) bool {
	observed, ok := ctx.Value(accountPlanObservationKey{}).(int64)
	return ok && accountID > 0 && observed == accountID
}
