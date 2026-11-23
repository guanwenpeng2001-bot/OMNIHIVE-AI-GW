package identitycompat

import (
	"context"
	"errors"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// CheckRunFunding validates live funding without advancing subscription
// windows. Reservation and settlement remain in the native execution path.
func CheckRunFunding(ctx context.Context, key *service.APIKey, subscriptions *service.SubscriptionService) (*service.UserSubscription, error) {
	if !key.Group.IsSubscriptionType() {
		if key.User.Balance > 0 {
			return nil, nil
		}
		return nil, &fault.Fault{Status: 403, Reason: "INSUFFICIENT_BALANCE", Message: "Insufficient account balance"}
	}
	sub, err := subscriptions.ReadRunSubscription(ctx, key.UserID, key.Group.ID)
	if errors.Is(err, service.ErrSubscriptionNotFound) {
		return nil, &fault.Fault{Status: 403, Reason: "SUBSCRIPTION_NOT_FOUND", Message: "No active subscription found for this group"}
	}
	if err != nil {
		return nil, err
	}
	_, err = subscriptions.ValidateAndCheckLimits(sub, key.Group)
	switch {
	case err == nil:
		return sub, nil
	case errors.Is(err, service.ErrDailyLimitExceeded), errors.Is(err, service.ErrWeeklyLimitExceeded), errors.Is(err, service.ErrMonthlyLimitExceeded):
		return nil, &fault.Fault{Status: 429, Reason: "USAGE_LIMIT_EXCEEDED", Message: err.Error()}
	default:
		return nil, &fault.Fault{Status: 403, Reason: "SUBSCRIPTION_INVALID", Message: err.Error()}
	}
}
